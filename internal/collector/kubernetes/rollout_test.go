package kubernetes

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsDeploymentRollout(t *testing.T) {
	replicas := int32(2)
	oldD := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "api",
			Namespace:  "prod",
			Generation: 1,
			Annotations: map[string]string{
				revisionAnnotation: "1",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "api", Image: "api:1"}},
				},
			},
		},
	}
	newD := oldD.DeepCopy()
	newD.Generation = 2
	newD.Annotations[revisionAnnotation] = "2"
	newD.Spec.Template.Spec.Containers[0].Image = "api:2"

	if !isDeploymentRollout(oldD, newD) {
		t.Fatal("expected rollout detection")
	}
	if isDeploymentRollout(oldD, oldD) {
		t.Fatal("same object should not be a rollout")
	}

	snap := buildDeploymentConfigSnapshot(newD)
	if snap["kind"] != "DeploymentRollout" {
		t.Fatalf("kind=%v", snap["kind"])
	}
	spec, _ := snap["spec"].(map[string]any)
	containers, _ := spec["containers"].([]map[string]any)
	if len(containers) != 1 || containers[0]["image"] != "api:2" {
		t.Fatalf("containers=%v", containers)
	}
	// Ensure we do not dump env.
	if _, ok := containers[0]["env"]; ok {
		t.Fatal("env must not be in snapshot")
	}
}
