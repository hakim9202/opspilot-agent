package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	APIURL      string
	AgentToken  string
	Environment string
	DataDir     string
	ClusterName string

	HeartbeatInterval time.Duration
	BatchInterval     time.Duration
	BatchMaxEvents    int
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		APIURL:            strings.TrimRight(os.Getenv("OPSPILOT_API_URL"), "/"),
		AgentToken:        os.Getenv("OPSPILOT_AGENT_TOKEN"),
		Environment:       getenv("OPSPILOT_ENVIRONMENT", "lab"),
		DataDir:           getenv("OPSPILOT_DATA_DIR", "/var/lib/opspilot"),
		ClusterName:       getenv("OPSPILOT_CLUSTER_NAME", "opspilot-lab"),
		HeartbeatInterval: 10 * time.Second,
		BatchInterval:     10 * time.Second,
		BatchMaxEvents:    50,
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

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
