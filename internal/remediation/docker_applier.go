package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/opspilot/opspilot-agent/internal/transport"
)

// DockerRemediator runs allowlisted fixes against the local Docker Engine.
type DockerRemediator interface {
	RestartContainer(ctx context.Context, name string) (string, error)
	RecreateWithImage(ctx context.Context, name, image string) (string, error)
}

// DockerApplier executes docker-host fix commands.
type DockerApplier struct {
	rem    DockerRemediator
	logger *slog.Logger
}

// NewDockerApplier creates a docker-host remediation applier.
func NewDockerApplier(rem DockerRemediator, logger *slog.Logger) *DockerApplier {
	return &DockerApplier{rem: rem, logger: logger}
}

// Apply runs one pending docker fix command.
func (a *DockerApplier) Apply(ctx context.Context, cmd transport.PendingFixCommand) (string, error) {
	switch cmd.Type {
	case "restart_container":
		return a.restartContainer(ctx, cmd.Params)
	case "set_container_image":
		return a.setContainerImage(ctx, cmd.Params)
	default:
		return "", fmt.Errorf("unsupported docker fix type %q", cmd.Type)
	}
}

func (a *DockerApplier) restartContainer(ctx context.Context, params map[string]string) (string, error) {
	name := strings.TrimSpace(params["container"])
	if name == "" {
		return "", fmt.Errorf("container is required")
	}
	msg, err := a.rem.RestartContainer(ctx, name)
	if err != nil {
		return "", err
	}
	a.logger.Info("docker remediation applied", "type", "restart_container", "result", msg)
	return msg, nil
}

func (a *DockerApplier) setContainerImage(ctx context.Context, params map[string]string) (string, error) {
	name := strings.TrimSpace(params["container"])
	image := strings.TrimSpace(params["image"])
	if name == "" || image == "" {
		return "", fmt.Errorf("container and image are required")
	}
	if strings.ContainsAny(image, " \t\n") {
		return "", fmt.Errorf("invalid image reference")
	}
	msg, err := a.rem.RecreateWithImage(ctx, name, image)
	if err != nil {
		return "", err
	}
	a.logger.Info("docker remediation applied", "type", "set_container_image", "result", msg)
	return msg, nil
}
