package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildPodEvidenceSnapshot(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns"},
		Spec:       corev1.PodSpec{NodeName: "node-1"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	cs := corev1.ContainerStatus{Name: "app", Image: "busybox:1.36", RestartCount: 2}
	meta := map[string]string{
		"exitCode":         "2",
		"waitingReason":    "CrashLoopBackOff",
		"terminatedReason": "Error",
		"restartCount":     "2",
	}
	snap := buildPodEvidenceSnapshot(pod, cs, "CrashLoopBackOff", "Error (exit 2)", meta, "FATAL: db down")
	if snap["version"] != 1 {
		t.Fatalf("version=%v", snap["version"])
	}
	status, _ := snap["status"].(map[string]any)
	if status["cause"] != "Error (exit 2)" {
		t.Fatalf("cause=%v", status["cause"])
	}
	logs, _ := snap["logs"].(map[string]any)
	if logs["snippet"] != "FATAL: db down" {
		t.Fatalf("logs=%v", logs)
	}
	ev, _ := snap["evidence"].([]string)
	if len(ev) < 2 {
		t.Fatalf("expected evidence lines, got %v", ev)
	}
}
