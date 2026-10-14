package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// AgentMode selects which collectors run.
type AgentMode string

const (
	ModeKubernetes AgentMode = "kubernetes"
	ModeDockerHost AgentMode = "docker-host"
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	APIURL      string
	AgentToken  string
	Environment string
	DataDir     string
	ClusterName string
	AgentMode   AgentMode
	Service     string

	// Docker-host mode
	DockerHost  string
	DockerWatch string
	PollSeconds int

	HeartbeatInterval time.Duration
	BatchInterval     time.Duration
	BatchMaxEvents    int
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	mode := AgentMode(strings.ToLower(strings.TrimSpace(getenv("OPSPILOT_AGENT_MODE", string(ModeKubernetes)))))
	switch mode {
	case ModeKubernetes, ModeDockerHost:
	default:
		return nil, fmt.Errorf("OPSPILOT_AGENT_MODE must be kubernetes or docker-host (got %q)", mode)
	}

	cfg := &Config{
		APIURL:            strings.TrimRight(os.Getenv("OPSPILOT_API_URL"), "/"),
		AgentToken:        os.Getenv("OPSPILOT_AGENT_TOKEN"),
		Environment:       getenv("OPSPILOT_ENVIRONMENT", "lab"),
		DataDir:           getenv("OPSPILOT_DATA_DIR", "/var/lib/opspilot"),
		ClusterName:       getenv("OPSPILOT_CLUSTER_NAME", "opspilot-lab"),
		AgentMode:         mode,
		Service:           strings.TrimSpace(os.Getenv("OPSPILOT_SERVICE")),
		DockerHost:        getenv("OPSPILOT_DOCKER_HOST", "unix:///var/run/docker.sock"),
		DockerWatch:       strings.TrimSpace(os.Getenv("OPSPILOT_DOCKER_WATCH")),
		PollSeconds:       30,
		HeartbeatInterval: 10 * time.Second,
		BatchInterval:     10 * time.Second,
		BatchMaxEvents:    50,
	}

	if cfg.DockerWatch == "" && mode == ModeDockerHost {
		cfg.DockerWatch = "label:opspilot.watch=true"
	}

	if v := os.Getenv("OPSPILOT_POLL_SECONDS"); v != "" {
		if sec, err := time.ParseDuration(v + "s"); err == nil && sec >= 5*time.Second {
			cfg.PollSeconds = int(sec / time.Second)
		}
	}

	if v := os.Getenv("OPSPILOT_HEARTBEAT_SECONDS"); v != "" {
		if sec, err := time.ParseDuration(v + "s"); err == nil && sec >= 5*time.Second {
			cfg.HeartbeatInterval = sec
		}
	}

	if cfg.APIURL == "" {
		return nil, fmt.Errorf("OPSPILOT_API_URL is required")
	}
	if cfg.AgentToken == "" {
		return nil, fmt.Errorf("OPSPILOT_AGENT_TOKEN is required")
	}
	return cfg, nil
}

func (c *Config) CollectorLabel() string {
	return string(c.AgentMode)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
