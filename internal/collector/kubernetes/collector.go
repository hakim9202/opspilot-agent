package kubernetes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/opspilot/opspilot-agent/internal/redactor"
	"github.com/opspilot/opspilot-agent/internal/transport"
)

const (
	crashLogTailLines = 20
	crashLogMaxBytes  = 2048
)

// Emitter receives structured incident events.
type Emitter func(event transport.Event)

// Collector watches pods, deployments, and events via SharedInformerFactory.
type Collector struct {
	logger  *slog.Logger
	emit    Emitter
	client  kubernetes.Interface
	factory informers.SharedInformerFactory

	mu        sync.Mutex
	seen      map[string]time.Time
	dedupeTTL time.Duration
}

// New builds a collector using in-cluster config, falling back to kubeconfig.
func New(logger *slog.Logger, emit Emitter) (*Collector, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		kubeCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
		cfg, err = kubeCfg.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("kubernetes config: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	factory := informers.NewSharedInformerFactory(client, 30*time.Second)
	return &Collector{
		logger:    logger,
		emit:      emit,
		client:    client,
		factory:   factory,
		seen:      make(map[string]time.Time),
		dedupeTTL: 15 * time.Minute,
	}, nil
}

// Client returns the underlying Kubernetes client.
func (c *Collector) Client() kubernetes.Interface {
	return c.client
}

// Start registers informer handlers and begins watching until stop is closed.
func (c *Collector) Start(ctx context.Context, stop <-chan struct{}) error {
	_ = ctx
	podInformer := c.factory.Core().V1().Pods().Informer()
	depInformer := c.factory.Apps().V1().Deployments().Informer()
	eventInformer := c.factory.Core().V1().Events().Informer()

	_, _ = podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if pod, ok := obj.(*corev1.Pod); ok {
				c.inspectPod(pod)
			}
		},
		UpdateFunc: func(_, newObj any) {
			if pod, ok := newObj.(*corev1.Pod); ok {
				c.inspectPod(pod)
			}
		},
	})

	_, _ = depInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if d, ok := obj.(*appsv1.Deployment); ok {
				c.inspectDeployment(d)
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldD, okOld := oldObj.(*appsv1.Deployment)
			newD, okNew := newObj.(*appsv1.Deployment)
			if okOld && okNew {
				c.inspectDeploymentRollout(oldD, newD)
			}
			if okNew {
				c.inspectDeployment(newD)
			}
		},
	})

	_, _ = eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if e, ok := obj.(*corev1.Event); ok {
				c.inspectEvent(e)
			}
		},
		UpdateFunc: func(_, newObj any) {
			if e, ok := newObj.(*corev1.Event); ok {
				c.inspectEvent(e)
			}
		},
	})

	c.factory.Start(stop)
	synced := c.factory.WaitForCacheSync(stop)
	for typ, ok := range synced {
		if !ok {
			return fmt.Errorf("informer cache sync failed for %v", typ)
		}
	}
	c.logger.Info("kubernetes informers synced")
	<-stop
	return nil
}

func (c *Collector) inspectPod(pod *corev1.Pod) {
	if isSystemNamespace(pod.Namespace) {
		return
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			waitingReason := cs.State.Waiting.Reason
			switch waitingReason {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull":
				c.emitCrashOrPullIncident(pod, cs, waitingReason)
			}
		}
		if cs.LastTerminationState.Terminated != nil {
			term := cs.LastTerminationState.Terminated
			if term.Reason == "OOMKilled" || term.ExitCode == 137 {
				c.emitOOMIncident(pod, cs, term)
			}
		}
	}
}

func (c *Collector) emitCrashOrPullIncident(pod *corev1.Pod, cs corev1.ContainerStatus, waitingReason string) {
	waitingMsg := cs.State.Waiting.Message
	meta := map[string]string{
		"container":     cs.Name,
		"phase":         string(pod.Status.Phase),
		"restartCount":  fmt.Sprintf("%d", cs.RestartCount),
		"waitingReason": waitingReason,
		"status":        waitingReason,
	}
	putContainerSpecMeta(meta, pod, cs)
	if waitingMsg != "" {
		meta["waitingMessage"] = redactor.Redact(waitingMsg)
	}

	// Prefer lastState.terminated (underlying crash); also record current terminated if present.
	term := cs.LastTerminationState.Terminated
	if term != nil {
		putTerminatedMeta(meta, term, "")
		if !term.FinishedAt.IsZero() {
			meta["finishedAt"] = term.FinishedAt.UTC().Format(time.RFC3339)
		}
	}
	if cur := cs.State.Terminated; cur != nil {
		putTerminatedMeta(meta, cur, "current")
	}

	cause := crashCause(waitingReason, term, waitingMsg)
	meta["cause"] = cause

	msg := fmt.Sprintf("container %s: %s (status %s)", cs.Name, cause, waitingReason)
	if waitingMsg != "" && waitingReason != "CrashLoopBackOff" {
		msg = fmt.Sprintf("%s — %s", msg, waitingMsg)
	}

	if waitingReason == "CrashLoopBackOff" || waitingReason == "Error" {
		if snippet := c.fetchCrashLogSnippet(pod.Namespace, pod.Name, cs.Name); snippet != "" {
			meta["logSnippet"] = snippet
			msg = fmt.Sprintf("%s | last logs: %s", msg, compactLogLine(snippet))
		}
	}

	incidentType := mapWaitingType(waitingReason)
	snap := buildPodEvidenceSnapshot(pod, cs, incidentType, cause, meta, meta["logSnippet"])

	// Reason is the human-facing "why"; type/status keep the K8s waiting state.
	c.emitOnce(fmt.Sprintf("pod:%s/%s:%s:%s", pod.Namespace, pod.Name, cs.Name, waitingReason), transport.Event{
		Type:       incidentType,
		Severity:   "critical",
		Title:      fmt.Sprintf("Pod %s/%s: %s", pod.Namespace, pod.Name, cause),
		Message:    redactor.Redact(msg),
		Namespace:  pod.Namespace,
		Resource:   "pod/" + pod.Name,
		Reason:     cause,
		OccurredAt: time.Now().UTC(),
		Labels:     redactor.RedactMap(pod.Labels),
		Metadata:   meta,
		Snapshot:   snap,
	})
}

func (c *Collector) emitOOMIncident(pod *corev1.Pod, cs corev1.ContainerStatus, term *corev1.ContainerStateTerminated) {
	cause := formatTerminatedCause(term)
	meta := map[string]string{
		"container":    cs.Name,
		"phase":        string(pod.Status.Phase),
		"exitCode":     fmt.Sprintf("%d", term.ExitCode),
		"restartCount": fmt.Sprintf("%d", cs.RestartCount),
		"cause":        cause,
		"status":       "OOMKilled",
	}
	putContainerSpecMeta(meta, pod, cs)
	if term.Reason != "" {
		meta["terminatedReason"] = term.Reason
	}
	if term.Message != "" {
		meta["terminatedMessage"] = redactor.Redact(term.Message)
	}
	if !term.FinishedAt.IsZero() {
		meta["finishedAt"] = term.FinishedAt.UTC().Format(time.RFC3339)
	}

	msg := fmt.Sprintf("container %s terminated: %s", cs.Name, cause)
	if snippet := c.fetchCrashLogSnippet(pod.Namespace, pod.Name, cs.Name); snippet != "" {
		meta["logSnippet"] = snippet
		msg = fmt.Sprintf("%s | last logs: %s", msg, compactLogLine(snippet))
	}

	snap := buildPodEvidenceSnapshot(pod, cs, "OOMKilled", cause, meta, meta["logSnippet"])

	c.emitOnce(fmt.Sprintf("pod:%s/%s:%s:OOMKilled:%d", pod.Namespace, pod.Name, cs.Name, term.FinishedAt.Unix()), transport.Event{
		Type:       "OOMKilled",
		Severity:   "critical",
		Title:      fmt.Sprintf("Pod %s/%s: %s", pod.Namespace, pod.Name, cause),
		Message:    redactor.Redact(msg),
		Namespace:  pod.Namespace,
		Resource:   "pod/" + pod.Name,
		Reason:     cause,
		OccurredAt: time.Now().UTC(),
		Labels:     redactor.RedactMap(pod.Labels),
		Metadata:   meta,
		Snapshot:   snap,
	})
}

func putTerminatedMeta(meta map[string]string, term *corev1.ContainerStateTerminated, prefix string) {
	key := func(k string) string {
		if prefix == "" {
			return k
		}
		return prefix + strings.ToUpper(k[:1]) + k[1:]
	}
	meta[key("exitCode")] = fmt.Sprintf("%d", term.ExitCode)
	if term.Reason != "" {
		meta[key("terminatedReason")] = term.Reason
	}
	if term.Message != "" {
		meta[key("terminatedMessage")] = redactor.Redact(term.Message)
	}
}

// crashCause prefers lastState.terminated over the waiting backoff name.
func crashCause(waitingReason string, term *corev1.ContainerStateTerminated, waitingMsg string) string {
	if term != nil {
		return formatTerminatedCause(term)
	}
	switch waitingReason {
	case "ImagePullBackOff", "ErrImagePull":
		if waitingMsg != "" {
			// Keep short — full message stays in metadata/message.
			return waitingReason
		}
		return waitingReason
	default:
		return waitingReason
	}
}

func formatTerminatedCause(term *corev1.ContainerStateTerminated) string {
	if term == nil {
		return "Unknown"
	}
	reason := term.Reason
	if reason == "" {
		reason = "Error"
	}
	if reason == "OOMKilled" || term.ExitCode == 137 {
		return fmt.Sprintf("OOMKilled (exit %d)", term.ExitCode)
	}
	return fmt.Sprintf("%s (exit %d)", reason, term.ExitCode)
}

// fetchCrashLogSnippet returns a redacted, size-capped tail of recent container logs.
// Tries previous container instance first (post-crash), then current.
func (c *Collector) fetchCrashLogSnippet(namespace, podName, container string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tail := int64(crashLogTailLines)
	for _, previous := range []bool{true, false} {
		opts := &corev1.PodLogOptions{
			Container: container,
			TailLines: &tail,
			Previous:  previous,
		}
		req := c.client.CoreV1().Pods(namespace).GetLogs(podName, opts)
		stream, err := req.Stream(ctx)
		if err != nil {
			continue
		}
		limited := io.LimitReader(stream, crashLogMaxBytes+1)
		raw, readErr := io.ReadAll(limited)
		_ = stream.Close()
		if readErr != nil || len(raw) == 0 {
			continue
		}
		if len(raw) > crashLogMaxBytes {
			raw = raw[len(raw)-crashLogMaxBytes:]
			if i := strings.IndexByte(string(raw), '\n'); i >= 0 && i+1 < len(raw) {
				raw = raw[i+1:]
			}
		}
		snippet := strings.TrimSpace(string(raw))
		if snippet == "" {
			continue
		}
		return redactor.Redact(snippet)
	}
	return ""
}

func compactLogLine(snippet string) string {
	lines := strings.Split(snippet, "\n")
	var nonEmpty []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	// Prefer the last non-empty line for the inline message.
	last := nonEmpty[len(nonEmpty)-1]
	if len(last) > 160 {
		return last[:157] + "..."
	}
	return last
}

// How long Available=False / MinimumReplicasUnavailable must persist before we
// raise DeploymentUnhealthy. Suppresses create/rollout flash noise.
const deployUnhealthyGrace = 2 * time.Minute

// deploymentUnhealthyReport decides whether a deployment is worth surfacing.
// Transient MinimumReplicasUnavailable during a normal rollout is ignored until
// the Available=False condition has been true longer than deployUnhealthyGrace,
// or Progressing becomes False (e.g. ProgressDeadlineExceeded).
func deploymentUnhealthyReport(d *appsv1.Deployment, now time.Time) (reason, message string, ok bool) {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	available := d.Status.AvailableReplicas
	if desired <= 0 || available >= desired {
		return "", "", false
	}

	var availFalse, progFalse *appsv1.DeploymentCondition
	for i := range d.Status.Conditions {
		cond := &d.Status.Conditions[i]
		if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionFalse {
			availFalse = cond
		}
		if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
			progFalse = cond
		}
	}

	if progFalse != nil {
		msg := fmt.Sprintf("available replicas %d/%d (unavailable=%d)", available, desired, d.Status.UnavailableReplicas)
		if progFalse.Message != "" {
			msg = fmt.Sprintf("%s — %s", msg, progFalse.Message)
		}
		r := progFalse.Reason
		if r == "" {
			r = "DeploymentUnhealthy"
		}
		return r, msg, true
	}

	if availFalse == nil && d.Status.UnavailableReplicas == 0 {
		return "", "", false
	}

	// Rollout still progressing: only report after grace on Available=False.
	if availFalse != nil && availFalse.Reason == "MinimumReplicasUnavailable" {
		since := availFalse.LastTransitionTime.Time
		if since.IsZero() || now.Sub(since) < deployUnhealthyGrace {
			return "", "", false
		}
	} else if availFalse == nil {
		// Status lag without clear condition — treat as transient.
		return "", "", false
	}

	condReason, condMessage := "UnavailableReplicas", ""
	if availFalse != nil {
		if availFalse.Reason != "" && availFalse.Reason != "MinimumReplicasUnavailable" {
			condReason = availFalse.Reason
		} else if availFalse.Reason == "MinimumReplicasUnavailable" {
			// Sustained under-replication past grace — distinct from create flash.
			condReason = "UnavailableReplicas"
		}
		condMessage = availFalse.Message
	}
	msg := fmt.Sprintf("available replicas %d/%d (unavailable=%d)", available, desired, d.Status.UnavailableReplicas)
	if condMessage != "" {
		msg = fmt.Sprintf("%s — %s", msg, condMessage)
	}
	return condReason, msg, true
}

func (c *Collector) inspectDeployment(d *appsv1.Deployment) {
	if isSystemNamespace(d.Namespace) {
		return
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	available := d.Status.AvailableReplicas
	reason, msg, ok := deploymentUnhealthyReport(d, time.Now().UTC())
	if !ok {
		return
	}
	meta := map[string]string{
		"desired":     fmt.Sprintf("%d", desired),
		"available":   fmt.Sprintf("%d", available),
		"unavailable": fmt.Sprintf("%d", d.Status.UnavailableReplicas),
		"cause":       reason,
	}
	c.emitOnce(fmt.Sprintf("deploy:%s/%s:unhealthy", d.Namespace, d.Name), transport.Event{
		Type:       "DeploymentUnhealthy",
		Severity:   "warning",
		Title:      fmt.Sprintf("Deployment %s/%s unhealthy", d.Namespace, d.Name),
		Message:    msg,
		Namespace:  d.Namespace,
		Resource:   "deployment/" + d.Name,
		Reason:     reason,
		OccurredAt: time.Now().UTC(),
		Labels:     redactor.RedactMap(d.Labels),
		Metadata:   meta,
	})
}

func (c *Collector) inspectEvent(e *corev1.Event) {
	if e.Type != corev1.EventTypeWarning {
		return
	}
	involved := e.InvolvedObject
	// Ignore noisy control-plane / system namespaces for MVP signal quality.
	if isSystemNamespace(involved.Namespace) {
		return
	}
	reason := e.Reason
	switch reason {
	case "BackOff", "Failed", "FailedCreate", "FailedMount":
		// Elevate known failure signals tied to workloads.
	default:
		if !strings.Contains(strings.ToLower(reason), "fail") &&
			!strings.Contains(strings.ToLower(e.Message), "crashloop") &&
			!strings.Contains(strings.ToLower(e.Message), "imagepull") {
			return
		}
	}

	kind := strings.ToLower(involved.Kind)
	if kind != "pod" && kind != "deployment" && kind != "replicaset" {
		return
	}

	// Stable key: do not include Count/message (those change every BackOff tick).
	c.emitOnce(fmt.Sprintf("event:%s/%s:%s", involved.Namespace, involved.Name, reason), transport.Event{
		Type:       "KubernetesWarning",
		Severity:   "warning",
		Title:      fmt.Sprintf("%s %s/%s: %s", involved.Kind, involved.Namespace, involved.Name, reason),
		Message:    redactor.Redact(e.Message),
		Namespace:  involved.Namespace,
		Resource:   fmt.Sprintf("%s/%s", kind, involved.Name),
		Reason:     reason,
		OccurredAt: e.LastTimestamp.Time.UTC(),
		Metadata: map[string]string{
			"count":  fmt.Sprintf("%d", e.Count),
			"source": e.Source.Component,
			"cause":  reason,
		},
	})
}

func (c *Collector) emitOnce(key string, event transport.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if t, ok := c.seen[key]; ok && now.Sub(t) < c.dedupeTTL {
		return
	}
	// Opportunistic cleanup
	for k, t := range c.seen {
		if now.Sub(t) > c.dedupeTTL {
			delete(c.seen, k)
		}
	}
	c.seen[key] = now
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now.UTC()
	}
	c.logger.Info("incident detected", "type", event.Type, "resource", event.Resource, "namespace", event.Namespace, "reason", event.Reason)
	c.emit(event)
}

func mapWaitingType(reason string) string {
	switch reason {
	case "CrashLoopBackOff":
		return "CrashLoopBackOff"
	case "ImagePullBackOff", "ErrImagePull":
		return "ImagePullBackOff"
	default:
		return reason
	}
}

// putContainerSpecMeta copies image + memory requests/limits from the pod template
// so SaaS can prefill Apply suggested fix without hardcoding demo values.
func putContainerSpecMeta(meta map[string]string, pod *corev1.Pod, cs corev1.ContainerStatus) {
	if cs.Image != "" {
		meta["image"] = cs.Image
	}
	for _, ctn := range pod.Spec.Containers {
		if ctn.Name != cs.Name {
			continue
		}
		if ctn.Image != "" {
			meta["image"] = ctn.Image
		}
		if ctn.Resources.Limits != nil {
			if q, ok := ctn.Resources.Limits[corev1.ResourceMemory]; ok {
				meta["memoryLimit"] = q.String()
			}
		}
		if ctn.Resources.Requests != nil {
			if q, ok := ctn.Resources.Requests[corev1.ResourceMemory]; ok {
				meta["memoryRequest"] = q.String()
			}
		}
		break
	}
}

func isSystemNamespace(ns string) bool {
	switch ns {
	case "kube-system", "kube-public", "kube-node-lease", "local-path-storage", "opspilot-system":
		return true
	default:
		return false
	}
}
