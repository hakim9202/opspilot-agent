package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opspilot/opspilot-agent/internal/transport"
)

const maxInventoryPods = 500

// ListMonitoredPods returns a compact inventory of non-system pods the agent watches.
// Includes ready counts and, when metrics-server is available, CPU/memory usage.
func (c *Collector) ListMonitoredPods(ctx context.Context) ([]transport.PodInventoryItem, error) {
	list, err := c.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	metricsByKey := c.fetchPodMetrics(ctx)

	now := time.Now()
	out := make([]transport.PodInventoryItem, 0, len(list.Items))
	for _, pod := range list.Items {
		if isSystemNamespace(pod.Namespace) {
			continue
		}
		restarts := int32(0)
		for _, cs := range pod.Status.ContainerStatuses {
			restarts += cs.RestartCount
		}
		for _, cs := range pod.Status.InitContainerStatuses {
			restarts += cs.RestartCount
		}
		created := pod.CreationTimestamp.Time
		age := ""
		if !created.IsZero() {
			age = formatAge(now.Sub(created))
		}
		item := transport.PodInventoryItem{
			Namespace: pod.Namespace,
			Name:      pod.Name,
			Status:    formatPodStatus(&pod),
			Ready:     formatPodReady(&pod),
			Restarts:  restarts,
			Age:       age,
		}
		if m, ok := metricsByKey[pod.Namespace+"/"+pod.Name]; ok {
			item.CPU = m.cpu
			item.Memory = m.memory
		}
		out = append(out, item)
		if len(out) >= maxInventoryPods {
			break
		}
	}
	return out, nil
}

func formatPodReady(pod *corev1.Pod) string {
	total := len(pod.Spec.Containers)
	if total == 0 {
		return "0/0"
	}
	ready := 0
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
	}
	return fmt.Sprintf("%d/%d", ready, total)
}

func formatPodStatus(pod *corev1.Pod) string {
	if pod.DeletionTimestamp != nil {
		return "Terminating"
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
			return cs.State.Terminated.Reason
		}
	}
	for _, cs := range pod.Status.InitContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return "Init:" + cs.State.Waiting.Reason
		}
	}
	if pod.Status.Phase != "" {
		return string(pod.Status.Phase)
	}
	return "Unknown"
}

func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int(d.Seconds())
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	min := sec / 60
	if min < 60 {
		return fmt.Sprintf("%dm", min)
	}
	hr := min / 60
	if hr < 24 {
		return fmt.Sprintf("%dh", hr)
	}
	days := hr / 24
	return fmt.Sprintf("%dd", days)
}

type podMetricUsage struct {
	cpu    string
	memory string
}

type metricsPodList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Containers []struct {
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

// fetchPodMetrics returns namespace/name → formatted usage. Empty map if metrics API unavailable.
func (c *Collector) fetchPodMetrics(ctx context.Context) map[string]podMetricUsage {
	out := map[string]podMetricUsage{}
	result := c.client.CoreV1().RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/pods").
		Do(ctx)
	raw, err := result.Raw()
	if err != nil {
		return out
	}
	var list metricsPodList
	if err := json.Unmarshal(raw, &list); err != nil {
		return out
	}
	for _, item := range list.Items {
		var cpuTotal, memTotal resource.Quantity
		for _, ctr := range item.Containers {
			if q, err := resource.ParseQuantity(ctr.Usage.CPU); err == nil {
				cpuTotal.Add(q)
			}
			if q, err := resource.ParseQuantity(ctr.Usage.Memory); err == nil {
				memTotal.Add(q)
			}
		}
		key := item.Metadata.Namespace + "/" + item.Metadata.Name
		out[key] = podMetricUsage{
			cpu:    formatCPUQuantity(cpuTotal),
			memory: formatMemoryQuantity(memTotal),
		}
	}
	return out
}

func formatCPUQuantity(q resource.Quantity) string {
	if q.IsZero() {
		return "0"
	}
	// Prefer millicores for readability.
	milli := q.MilliValue()
	if milli < 1000 {
		return fmt.Sprintf("%dm", milli)
	}
	return fmt.Sprintf("%.2f", float64(milli)/1000.0)
}

func formatMemoryQuantity(q resource.Quantity) string {
	if q.IsZero() {
		return "0"
	}
	bytes := q.Value()
	const (
		ki = 1024
		mi = 1024 * 1024
		gi = 1024 * 1024 * 1024
	)
	switch {
	case bytes >= gi:
		return fmt.Sprintf("%.1fGi", float64(bytes)/float64(gi))
	case bytes >= mi:
		return fmt.Sprintf("%.0fMi", float64(bytes)/float64(mi))
	case bytes >= ki:
		return fmt.Sprintf("%.0fKi", float64(bytes)/float64(ki))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
