package registration

import (
	"context"
	"fmt"
	"os"

	"github.com/opspilot/opspilot-agent/internal/config"
	"github.com/opspilot/opspilot-agent/internal/security"
	"github.com/opspilot/opspilot-agent/internal/transport"
)

const AgentVersion = "0.1.0"

// EnsureRegistered loads stored credentials or registers a new agent.
func EnsureRegistered(ctx context.Context, cfg *config.Config, client *transport.Client, store *security.Store) (*security.Credentials, error) {
	creds, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	if creds != nil {
		return creds, nil
	}

	hostname, _ := os.Hostname()
	resp, err := client.Register(ctx, transport.RegisterRequest{
		InstallToken: cfg.AgentToken,
		Hostname:     hostname,
		Environment:  cfg.Environment,
		ClusterName:  cfg.ClusterName,
		Version:      AgentVersion,
		Labels: map[string]string{
			"collector": "kubernetes",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}

	creds = &security.Credentials{
		AgentID:     resp.AgentID,
		AccessToken: resp.AccessToken,
	}
	if err := store.Save(creds); err != nil {
		return nil, fmt.Errorf("save credentials: %w", err)
	}
	return creds, nil
}
