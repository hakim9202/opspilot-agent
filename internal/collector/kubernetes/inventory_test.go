package kubernetes

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestFormatAgeValues(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{time.Hour, "1h"},
		{25 * time.Hour, "1d"},
	}
	for _, tc := range cases {
		got := formatAge(tc.d)
		if got != tc.want {
			t.Fatalf("age(%v)=%s want %s", tc.d, got, tc.want)
		}
	}
}

func TestFormatPodReady(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "a"}, {Name: "b"}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "a", Ready: true},
				{Name: "b", Ready: false},
			},
		},
	}
	if got := formatPodReady(pod); got != "1/2" {
		t.Fatalf("ready=%s", got)
	}
}

func TestFormatCPUMemory(t *testing.T) {
	cpu := resource.MustParse("25m")
	if got := formatCPUQuantity(cpu); got != "25m" {
		t.Fatalf("cpu=%s", got)
	}
	mem := resource.MustParse("64Mi")
	if got := formatMemoryQuantity(mem); got != "64Mi" {
		t.Fatalf("mem=%s", got)
	}
}
