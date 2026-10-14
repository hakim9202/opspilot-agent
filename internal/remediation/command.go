package remediation

import (
	"context"

	"github.com/opspilot/opspilot-agent/internal/transport"
)

// CommandApplier executes allowlisted fix commands from the SaaS.
type CommandApplier interface {
	Apply(ctx context.Context, cmd transport.PendingFixCommand) (string, error)
}
