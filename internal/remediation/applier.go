package remediation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/opspilot/opspilot-agent/internal/transport"
)

// Applier executes allowlisted fix commands from the SaaS.
type Applier struct {
	client kubernetes.Interface
	logger *slog.Logger
}

// New creates a remediation applier.
func New(client kubernetes.Interface, logger *slog.Logger) *Applier {
	return &Applier{client: client, logger: logger}
}

// Apply runs one pending command and returns a human result summary.
func (a *Applier) Apply(ctx context.Context, cmd transport.PendingFixCommand) (string, error) {
	switch cmd.Type {
	case "set_deployment_memory":
		return a.setDeploymentMemory(ctx, cmd.Params)
	case "set_deployment_image":
		return a.setDeploymentImage(ctx, cmd.Params)
	case "restart_deployment":
		return a.restartDeployment(ctx, cmd.Params)
	case "scale_deployment":
		return a.scaleDeployment(ctx, cmd.Params)
	default:
		return "", fmt.Errorf("unsupported fix type %q", cmd.Type)
	}
}

func (a *Applier) setDeploymentMemory(ctx context.Context, params map[string]string) (string, error) {
	ns := strings.TrimSpace(params["namespace"])
	deploy := strings.TrimSpace(params["deployment"])
	container := strings.TrimSpace(params["container"])
	limit := strings.TrimSpace(params["memoryLimit"])
	request := strings.TrimSpace(params["memoryRequest"])
	if ns == "" || deploy == "" || container == "" || limit == "" {
		return "", fmt.Errorf("namespace, deployment, container, memoryLimit are required")
	}
	if request == "" {
		request = limit
	}
	if _, err := resource.ParseQuantity(limit); err != nil {
		return "", fmt.Errorf("invalid memoryLimit: %w", err)
	}
	if _, err := resource.ParseQuantity(request); err != nil {
		return "", fmt.Errorf("invalid memoryRequest: %w", err)
	}

	d, err := a.client.AppsV1().Deployments(ns).Get(ctx, deploy, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get deployment: %w", err)
	}
	idx := containerIndex(d.Spec.Template.Spec.Containers, container)
	if idx < 0 {
		return "", fmt.Errorf("container %q not found on deployment %s/%s", container, ns, deploy)
	}

	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []map[string]any{{
						"name": container,
						"resources": map[string]any{
							"limits": map[string]string{
								"memory": limit,
							},
							"requests": map[string]string{
								"memory": request,
							},
						},
					}},
				},
			},
		},
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return "", err
	}
	if _, err := a.client.AppsV1().Deployments(ns).Patch(
		ctx, deploy, types.StrategicMergePatchType, data, metav1.PatchOptions{},
	); err != nil {
		return "", fmt.Errorf("patch deployment memory: %w", err)
	}
	msg := fmt.Sprintf("set %s/%s container %s memory limit=%s request=%s", ns, deploy, container, limit, request)
	a.logger.Info("remediation applied", "type", "set_deployment_memory", "result", msg)
	return msg, nil
}

func (a *Applier) setDeploymentImage(ctx context.Context, params map[string]string) (string, error) {
	ns := strings.TrimSpace(params["namespace"])
	deploy := strings.TrimSpace(params["deployment"])
	container := strings.TrimSpace(params["container"])
	image := strings.TrimSpace(params["image"])
	if ns == "" || deploy == "" || container == "" || image == "" {
		return "", fmt.Errorf("namespace, deployment, container, image are required")
	}
	if strings.ContainsAny(image, " \t\n") {
		return "", fmt.Errorf("invalid image reference")
	}

	d, err := a.client.AppsV1().Deployments(ns).Get(ctx, deploy, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get deployment: %w", err)
	}
	idx := containerIndex(d.Spec.Template.Spec.Containers, container)
	if idx < 0 {
		return "", fmt.Errorf("container %q not found on deployment %s/%s", container, ns, deploy)
	}

	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []map[string]any{{
						"name":  container,
						"image": image,
					}},
				},
			},
		},
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return "", err
	}
	if _, err := a.client.AppsV1().Deployments(ns).Patch(
		ctx, deploy, types.StrategicMergePatchType, data, metav1.PatchOptions{},
	); err != nil {
		return "", fmt.Errorf("patch deployment image: %w", err)
	}
	msg := fmt.Sprintf("set %s/%s container %s image=%s", ns, deploy, container, image)
	a.logger.Info("remediation applied", "type", "set_deployment_image", "result", msg)
	return msg, nil
}

func (a *Applier) restartDeployment(ctx context.Context, params map[string]string) (string, error) {
	ns := strings.TrimSpace(params["namespace"])
	deploy := strings.TrimSpace(params["deployment"])
	if ns == "" || deploy == "" {
		return "", fmt.Errorf("namespace and deployment are required")
	}
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return "", err
	}
	if _, err := a.client.AppsV1().Deployments(ns).Patch(
		ctx, deploy, types.StrategicMergePatchType, data, metav1.PatchOptions{},
	); err != nil {
		return "", fmt.Errorf("restart deployment: %w", err)
	}
	msg := fmt.Sprintf("restarted deployment %s/%s", ns, deploy)
	a.logger.Info("remediation applied", "type", "restart_deployment", "result", msg)
	return msg, nil
}

func (a *Applier) scaleDeployment(ctx context.Context, params map[string]string) (string, error) {
	ns := strings.TrimSpace(params["namespace"])
	deploy := strings.TrimSpace(params["deployment"])
	replicasRaw := strings.TrimSpace(params["replicas"])
	if ns == "" || deploy == "" || replicasRaw == "" {
		return "", fmt.Errorf("namespace, deployment, replicas are required")
	}
	var replicas int32
	if _, err := fmt.Sscanf(replicasRaw, "%d", &replicas); err != nil {
		return "", fmt.Errorf("invalid replicas: %w", err)
	}
	if replicas < 0 || replicas > 50 {
		return "", fmt.Errorf("replicas out of allowed range 0..50")
	}
	patch := map[string]any{
		"spec": map[string]any{
			"replicas": replicas,
		},
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return "", err
	}
	if _, err := a.client.AppsV1().Deployments(ns).Patch(
		ctx, deploy, types.StrategicMergePatchType, data, metav1.PatchOptions{},
	); err != nil {
		return "", fmt.Errorf("scale deployment: %w", err)
	}
	msg := fmt.Sprintf("scaled deployment %s/%s to %d replicas", ns, deploy, replicas)
	a.logger.Info("remediation applied", "type", "scale_deployment", "result", msg)
	return msg, nil
}

func containerIndex(containers []corev1.Container, name string) int {
	for i, c := range containers {
		if c.Name == name {
			return i
		}
	}
	return -1
}
