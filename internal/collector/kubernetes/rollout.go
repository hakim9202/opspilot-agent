package kubernetes

import (
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/opspilot/opspilot-agent/internal/redactor"
	"github.com/opspilot/opspilot-agent/internal/transport"
)

const revisionAnnotation = "deployment.kubernetes.io/revision"

// inspectDeploymentRollout emits a config snapshot when a Deployment rolls out.
func (c *Collector) inspectDeploymentRollout(oldD, newD *appsv1.Deployment) {
	if newD == nil || isSystemNamespace(newD.Namespace) {
		return
	}
	if !isDeploymentRollout(oldD, newD) {
		return
	}

	revision := newD.Annotations[revisionAnnotation]
	generation := newD.Generation
	dedupeKey := fmt.Sprintf("deploy:%s/%s:rollout:%d:%s", newD.Namespace, newD.Name, generation, revision)

	desired := int32(1)
	if newD.Spec.Replicas != nil {
		desired = *newD.Spec.Replicas
	}

	snap := buildDeploymentConfigSnapshot(newD)
	reason := "DeploymentRollout"
	if revision != "" {
		reason = fmt.Sprintf("revision %s", revision)
	}

	meta := map[string]string{
		"generation":  fmt.Sprintf("%d", generation),
		"desired":     fmt.Sprintf("%d", desired),
		"updated":     fmt.Sprintf("%d", newD.Status.UpdatedReplicas),
		"available":   fmt.Sprintf("%d", newD.Status.AvailableReplicas),
		"unavailable": fmt.Sprintf("%d", newD.Status.UnavailableReplicas),
		"cause":       reason,
	}
	if revision != "" {
		meta["revision"] = revision
	}
	if imgs := containerImages(newD); len(imgs) > 0 {
		meta["images"] = imgs
	}

	msg := fmt.Sprintf(
		"deployment rolled out generation=%d revision=%s images=%s",
		generation,
		revisionOrDash(revision),
		meta["images"],
	)

	c.emitOnce(dedupeKey, transport.Event{
		Type:       "DeploymentRollout",
		Severity:   "info",
		Title:      fmt.Sprintf("Deployment %s/%s rollout", newD.Namespace, newD.Name),
		Message:    redactor.Redact(msg),
		Namespace:  newD.Namespace,
		Resource:   "deployment/" + newD.Name,
		Reason:     reason,
		OccurredAt: time.Now().UTC(),
		Labels:     redactor.RedactMap(newD.Labels),
		Metadata:   meta,
		Snapshot:   snap,
	})
}

func isDeploymentRollout(oldD, newD *appsv1.Deployment) bool {
	if oldD == nil || newD == nil {
		return false
	}
	// Spec change → new generation.
	if newD.Generation > oldD.Generation {
		return true
	}
	// Controller bumping rollout revision.
	oldRev := oldD.Annotations[revisionAnnotation]
	newRev := newD.Annotations[revisionAnnotation]
	if newRev != "" && newRev != oldRev {
		return true
	}
	// Image/template change without generation bump (rare but possible in some controllers).
	if containerImages(oldD) != containerImages(newD) {
		return true
	}
	return false
}

func buildDeploymentConfigSnapshot(d *appsv1.Deployment) map[string]any {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}

	strategy := string(d.Spec.Strategy.Type)
	containers := make([]map[string]any, 0, len(d.Spec.Template.Spec.Containers))
	for _, ctn := range d.Spec.Template.Spec.Containers {
		item := map[string]any{
			"name":  ctn.Name,
			"image": ctn.Image,
		}
		if len(ctn.Ports) > 0 {
			ports := make([]int32, 0, len(ctn.Ports))
			for _, p := range ctn.Ports {
				ports = append(ports, p.ContainerPort)
			}
			item["ports"] = ports
		}
		// Intentionally omit env/envFrom/command args — often contain secrets.
		containers = append(containers, item)
	}

	resource := map[string]any{
		"kind":      "Deployment",
		"name":      d.Name,
		"namespace": d.Namespace,
	}
	if d.UID != "" {
		resource["uid"] = string(d.UID)
	}

	revision := d.Annotations[revisionAnnotation]
	rollout := map[string]any{
		"generation": d.Generation,
		"strategy":   strategy,
	}
	if revision != "" {
		rollout["revision"] = revision
	}

	evidence := []string{
		fmt.Sprintf("generation=%d", d.Generation),
		fmt.Sprintf("replicas=%d", desired),
	}
	if revision != "" {
		evidence = append(evidence, "revision="+revision)
	}
	if imgs := containerImages(d); imgs != "" {
		evidence = append(evidence, "images="+imgs)
	}

	return map[string]any{
		"version":    1,
		"capturedAt": time.Now().UTC().Format(time.RFC3339),
		"kind":       "DeploymentRollout",
		"resource":   resource,
		"rollout":    rollout,
		"spec": map[string]any{
			"replicas":   desired,
			"containers": containers,
		},
		"status": map[string]any{
			"updatedReplicas":      d.Status.UpdatedReplicas,
			"readyReplicas":        d.Status.ReadyReplicas,
			"availableReplicas":    d.Status.AvailableReplicas,
			"unavailableReplicas":  d.Status.UnavailableReplicas,
			"observedGeneration":   d.Status.ObservedGeneration,
		},
		"labels":   redactor.RedactMap(d.Labels),
		"evidence": evidence,
	}
}

func containerImages(d *appsv1.Deployment) string {
	if d == nil {
		return ""
	}
	parts := make([]string, 0, len(d.Spec.Template.Spec.Containers))
	for _, ctn := range d.Spec.Template.Spec.Containers {
		parts = append(parts, fmt.Sprintf("%s=%s", ctn.Name, ctn.Image))
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += "," + parts[i]
	}
	return out
}

func revisionOrDash(rev string) string {
	if rev == "" {
		return "-"
	}
	return rev
}
