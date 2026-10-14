package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/opspilot/opspilot-agent/internal/collector/docker"
	"github.com/opspilot/opspilot-agent/internal/collector/kubernetes"
	"github.com/opspilot/opspilot-agent/internal/config"
	"github.com/opspilot/opspilot-agent/internal/heartbeat"
	"github.com/opspilot/opspilot-agent/internal/queue"
	"github.com/opspilot/opspilot-agent/internal/registration"
	"github.com/opspilot/opspilot-agent/internal/remediation"
	"github.com/opspilot/opspilot-agent/internal/security"
	"github.com/opspilot/opspilot-agent/internal/transport"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config error", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	stop := ctx.Done()

	store, err := security.NewStore(cfg.DataDir)
	if err != nil {
		logger.Error("credential store error", "error", err)
		os.Exit(1)
	}

	client := transport.New(cfg.APIURL)
	creds, err := registration.EnsureRegistered(ctx, cfg, client, store)
	if err != nil {
		logger.Error("registration failed", "error", err)
		os.Exit(1)
	}
	logger.Info("agent registered", "agentId", creds.AgentID, "mode", cfg.AgentMode)

	q, err := queue.New(cfg.DataDir, creds.AccessToken)
	if err != nil {
		logger.Error("queue init failed", "error", err)
		os.Exit(1)
	}

	emit := func(event transport.Event) {
		if err := q.Enqueue(event); err != nil {
			logger.Error("enqueue failed", "error", err)
		}
	}

	go q.RunFlusher(stop, cfg.BatchInterval, cfg.BatchMaxEvents, func(events []transport.Event) error {
		if err := client.SendEvents(ctx, creds.AccessToken, events); err != nil {
			logger.Warn("event batch send failed", "error", err, "count", len(events))
			return err
		}
		logger.Info("event batch sent", "count", len(events))
		return nil
	})

	meta := map[string]string{
		"collector": string(cfg.AgentMode),
		"mode":      string(cfg.AgentMode),
	}
	if cfg.Service != "" {
		meta["service"] = cfg.Service
	}

	switch cfg.AgentMode {
	case config.ModeDockerHost:
		runDockerHost(ctx, stop, logger, cfg, client, creds, emit, meta)
	case config.ModeKubernetes:
		runKubernetes(ctx, stop, logger, cfg, client, creds, emit, meta)
	default:
		logger.Error("unsupported agent mode", "mode", cfg.AgentMode)
		os.Exit(1)
	}
	logger.Info("agent shut down")
}

func runKubernetes(
	ctx context.Context,
	stop <-chan struct{},
	logger *slog.Logger,
	cfg *config.Config,
	client *transport.Client,
	creds *security.Credentials,
	emit func(transport.Event),
	meta map[string]string,
) {
	collector, err := kubernetes.New(logger, emit)
	if err != nil {
		logger.Error("kubernetes collector init failed", "error", err)
		os.Exit(1)
	}

	go heartbeat.New(
		client,
		creds.AccessToken,
		cfg.HeartbeatInterval,
		logger,
		meta,
		collector.ListMonitoredPods,
		nil,
		remediation.New(collector.Client(), logger),
	).Run(ctx, stop)

	logger.Info("opspilot agent starting",
		"version", registration.AgentVersion,
		"api", cfg.APIURL,
		"environment", cfg.Environment,
		"cluster", cfg.ClusterName,
		"mode", cfg.AgentMode,
	)

	if err := collector.Start(ctx, stop); err != nil {
		logger.Error("collector stopped with error", "error", err)
		os.Exit(1)
	}
}

func runDockerHost(
	ctx context.Context,
	stop <-chan struct{},
	logger *slog.Logger,
	cfg *config.Config,
	client *transport.Client,
	creds *security.Credentials,
	emit func(transport.Event),
	meta map[string]string,
) {
	collector, err := docker.New(logger, emit, cfg)
	if err != nil {
		logger.Error("docker collector init failed", "error", err)
		os.Exit(1)
	}

	go heartbeat.New(
		client,
		creds.AccessToken,
		cfg.HeartbeatInterval,
		logger,
		meta,
		nil,
		collector.ListMonitoredContainers,
		remediation.NewDockerApplier(collector, logger),
	).Run(ctx, stop)

	logger.Info("opspilot agent starting",
		"version", registration.AgentVersion,
		"api", cfg.APIURL,
		"environment", cfg.Environment,
		"cluster", cfg.ClusterName,
		"mode", cfg.AgentMode,
		"dockerWatch", cfg.DockerWatch,
	)

	if err := collector.Start(ctx, stop); err != nil {
		logger.Error("collector stopped with error", "error", err)
		os.Exit(1)
	}
}
