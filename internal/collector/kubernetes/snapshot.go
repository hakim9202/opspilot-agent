package kubernetes

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/opspilot/opspilot-agent/internal/redactor"
)

// buildPodEvidenceSnapshot freezes redacted evidence for CrashLoop / OOM / ImagePull.
func buildPodEvidenceSnapshot(
	pod *corev1.Pod,
	cs corev1.ContainerStatus,
	incidentType string,
	cause string,
	meta map[string]string,
	logSnippet string,
) map[string]any {
	status := map[string]any{
		"type":  incidentType,
		"phase": string(pod.Status.Phase),
		"cause": cause,
	}
	for _, k := range []string{
		"waitingReason", "terminatedReason", "exitCode", "restartCount",
		"waitingMessage", "terminatedMessage", "finishedAt", "status",
	} {
		if v := meta[k]; v != "" {
			status[k] = v
		}
	}

	container := map[string]any{
		"name": cs.Name,
	}
	if cs.Image != "" {
		container["image"] = cs.Image
	}
	if cs.ImageID != "" {
		container["imageId"] = truncate(cs.ImageID, 120)
	}
	if v := meta["image"]; v != "" {
		container["image"] = v
	}
	if v := meta["memoryLimit"]; v != "" {
		container["memoryLimit"] = v
	}
	if v := meta["memoryRequest"]; v != "" {
		container["memoryRequest"] = v
	}

	resource := map[string]any{
		"kind":      "Pod",
		"name":      pod.Name,
		"namespace": pod.Namespace,
	}
	if pod.UID != "" {
		resource["uid"] = string(pod.UID)
	}
	if pod.Spec.NodeName != "" {
		resource["nodeName"] = pod.Spec.NodeName
	}

	snap := map[string]any{
		"version":    1,
		"capturedAt": time.Now().UTC().Format(time.RFC3339),
		"resource":   resource,
		"status":     status,
		"container":  container,
	}

	if logSnippet != "" {
		snap["logs"] = map[string]any{
			"snippet": redactor.Redact(logSnippet),
		}
	}

	evidence := []string{}
	if cause != "" {
		evidence = append(evidence, "cause="+cause)
	}
	if meta["exitCode"] != "" {
		evidence = append(evidence, "exitCode="+meta["exitCode"])
	}
	if meta["waitingReason"] != "" {
		evidence = append(evidence, "waitingReason="+meta["waitingReason"])
	}
	if meta["terminatedReason"] != "" {
		evidence = append(evidence, "terminatedReason="+meta["terminatedReason"])
	}
	if logSnippet != "" {
		evidence = append(evidence, "log="+compactLogLine(logSnippet))
	}
	if len(evidence) > 0 {
		snap["evidence"] = evidence
	}

	return snap
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func snapshotSummary(snap map[string]any) string {
	if snap == nil {
		return ""
	}
	ev, _ := snap["evidence"].([]string)
	if len(ev) == 0 {
		return fmt.Sprintf("snapshot v1 @ %v", snap["capturedAt"])
	}
	return fmt.Sprintf("%d evidence fields", len(ev))
}
