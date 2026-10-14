package heartbeat

import (
	"context"
	"log/slog"
	"time"

	"github.com/opspilot/opspilot-agent/internal/redactor"
	"github.com/opspilot/opspilot-agent/internal/registration"
	"github.com/opspilot/opspilot-agent/internal/remediation"
	"github.com/opspilot/opspilot-agent/internal/transport"
)

// PodInventoryFunc returns the current monitored pod inventory.
type PodInventoryFunc func(ctx context.Context) ([]transport.PodInventoryItem, error)

// ContainerInventoryFunc returns the current monitored container inventory.
type ContainerInventoryFunc func(ctx context.Context) ([]transport.ContainerInventoryItem, error)

// Runner sends periodic heartbeats and applies remote configuration.
type Runner struct {
	client             *transport.Client
	token              string
	interval           time.Duration
	logger             *slog.Logger
	meta               map[string]string
	podInventory       PodInventoryFunc
	containerInventory ContainerInventoryFunc
	applier            remediation.CommandApplier
}

// New creates a heartbeat runner. Inventory/applier may be nil.
func New(
	client *transport.Client,
	token string,
	interval time.Duration,
	logger *slog.Logger,
	meta map[string]string,
	podInventory PodInventoryFunc,
	containerInventory ContainerInventoryFunc,
	applier remediation.CommandApplier,
) *Runner {
	return &Runner{
		client:             client,
		token:              token,
		interval:           interval,
		logger:             logger,
		meta:               meta,
		podInventory:       podInventory,
		containerInventory: containerInventory,
		applier:            applier,
	}
}

// Run blocks until stop is closed.
func (r *Runner) Run(ctx context.Context, stop <-chan struct{}) {
	send := func() {
		req := transport.HeartbeatRequest{
			Status:  "online",
			Version: registration.AgentVersion,
			Meta:    copyMeta(r.meta),
		}
		if r.podInventory != nil {
			pods, err := r.podInventory(ctx)
			if err != nil {
				r.logger.Warn("pod inventory failed", "error", err)
			} else {
				req.Pods = pods
				req.Meta["podCount"] = itoa(len(pods))
			}
		}
		if r.containerInventory != nil {
			containers, err := r.containerInventory(ctx)
			if err != nil {
				r.logger.Warn("container inventory failed", "error", err)
			} else {
				req.Containers = containers
				req.Meta["containerCount"] = itoa(len(containers))
			}
		}

		resp, err := r.client.Heartbeat(ctx, r.token, req)
		if err != nil {
			r.logger.Warn("heartbeat failed", "error", err)
			return
		}
		if resp != nil && resp.Configuration != nil {
			patterns := resp.Configuration.RedactionPatterns
			redactor.SetCustomPatterns(patterns)
			r.logger.Info("applied remote redaction patterns", "count", len(patterns))
		}
		if resp != nil && len(resp.PendingCommands) > 0 {
			r.handleCommands(ctx, resp.PendingCommands)
		}
		r.logger.Debug("heartbeat ok", "pods", len(req.Pods), "containers", len(req.Containers))
	}

	send()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			send()
		}
	}
}

func copyMeta(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (r *Runner) handleCommands(ctx context.Context, cmds []transport.PendingFixCommand) {
	if r.applier == nil {
		r.logger.Warn("received fix commands but remediation applier is nil", "count", len(cmds))
		return
	}
	for _, cmd := range cmds {
		r.logger.Info("applying fix command", "id", cmd.ID, "type", cmd.Type)
		result, err := r.applier.Apply(ctx, cmd)
		status := transport.FixCommandStatusRequest{Status: "succeeded", Result: result}
		if err != nil {
			status = transport.FixCommandStatusRequest{
				Status: "failed",
				Error:  err.Error(),
			}
			r.logger.Warn("fix command failed", "id", cmd.ID, "error", err)
		}
		if reportErr := r.client.ReportFixCommandStatus(ctx, r.token, cmd.ID, status); reportErr != nil {
			r.logger.Warn("fix command status report failed", "id", cmd.ID, "error", reportErr)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
