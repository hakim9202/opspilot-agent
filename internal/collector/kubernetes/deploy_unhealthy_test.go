package kubernetes

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeploymentUnhealthyReport_skipsCreateFlash(t *testing.T) {
	replicas := int32(1)
	now := time.Date(2026, 8, 6, 15, 40, 30, 0, time.UTC)
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-healthy-c", Namespace: "opspilot-demo"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas:   0,
			UnavailableReplicas: 1,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:               appsv1.DeploymentAvailable,
					Status:             corev1.ConditionFalse,
					Reason:             "MinimumReplicasUnavailable",
					Message:            "Deployment does not have minimum availability.",
					LastTransitionTime: metav1.NewTime(now.Add(-10 * time.Second)),
				},
				{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
					Reason: "NewReplicaSetCreated",
				},
			},
		},
	}
	if _, _, ok := deploymentUnhealthyReport(d, now); ok {
		t.Fatal("expected create/rollout flash to be suppressed")
	}
}

func TestDeploymentUnhealthyReport_emitsAfterGrace(t *testing.T) {
	replicas := int32(2)
	now := time.Date(2026, 8, 6, 15, 45, 0, 0, time.UTC)
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-healthy-c", Namespace: "opspilot-demo"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas:   0,
			UnavailableReplicas: 2,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:               appsv1.DeploymentAvailable,
					Status:             corev1.ConditionFalse,
					Reason:             "MinimumReplicasUnavailable",
					Message:            "Deployment does not have minimum availability.",
					LastTransitionTime: metav1.NewTime(now.Add(-3 * time.Minute)),
				},
				{
					Type:   appsv1.DeploymentProgressing,
					Status: corev1.ConditionTrue,
					Reason: "ReplicaSetUpdated",
				},
			},
		},
	}
	reason, _, ok := deploymentUnhealthyReport(d, now)
	if !ok {
		t.Fatal("expected sustained under-replication to emit")
	}
	if reason != "UnavailableReplicas" {
		t.Fatalf("reason=%q want UnavailableReplicas", reason)
	}
}

func TestDeploymentUnhealthyReport_progressDeadline(t *testing.T) {
	replicas := int32(1)
	now := time.Date(2026, 8, 6, 15, 45, 0, 0, time.UTC)
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "stuck", Namespace: "opspilot-demo"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: 0,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:               appsv1.DeploymentAvailable,
					Status:             corev1.ConditionFalse,
					Reason:             "MinimumReplicasUnavailable",
					LastTransitionTime: metav1.NewTime(now.Add(-5 * time.Second)),
				},
				{
					Type:               appsv1.DeploymentProgressing,
					Status:             corev1.ConditionFalse,
					Reason:             "ProgressDeadlineExceeded",
					Message:            "Progress deadline exceeded",
					LastTransitionTime: metav1.NewTime(now.Add(-1 * time.Second)),
				},
			},
		},
	}
	reason, _, ok := deploymentUnhealthyReport(d, now)
	if !ok {
		t.Fatal("expected ProgressDeadlineExceeded to emit immediately")
	}
	if reason != "ProgressDeadlineExceeded" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestDeploymentUnhealthyReport_healthy(t *testing.T) {
	replicas := int32(1)
	d := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: 1,
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue, Reason: "MinimumReplicasAvailable"},
			},
		},
	}
	if _, _, ok := deploymentUnhealthyReport(d, time.Now().UTC()); ok {
		t.Fatal("healthy deployment must not emit")
	}
}
