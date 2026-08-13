package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is an HTTP client for the OpsPilot SaaS API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a transport client.
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// RegisterRequest is the body for agent registration.
type RegisterRequest struct {
	InstallToken string            `json:"installToken"`
	Hostname     string            `json:"hostname"`
	Environment  string            `json:"environment"`
	ClusterName  string            `json:"clusterName"`
	Version      string            `json:"version"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// RegisterResponse is returned after successful registration.
type RegisterResponse struct {
	AgentID     string `json:"agentId"`
	AccessToken string `json:"accessToken"`
}

// HeartbeatRequest is the body for heartbeats.
type HeartbeatRequest struct {
	Status  string             `json:"status"`
	Version string             `json:"version"`
	Meta    map[string]string  `json:"meta,omitempty"`
	Pods    []PodInventoryItem `json:"pods,omitempty"`
}

// PodInventoryItem is a compact monitored-pod row for the SaaS dashboard.
type PodInventoryItem struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Ready     string `json:"ready,omitempty"`
	Restarts  int32  `json:"restarts"`
	Age       string `json:"age"`
	// Optional metrics-server usage (empty when metrics API unavailable).
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}


// AgentConfiguration is remote config returned on heartbeat / config fetch.
type AgentConfiguration struct {
	RedactionPatterns []string `json:"redactionPatterns"`
	RedactionLabels   []string `json:"redactionLabels,omitempty"`
}

// PendingFixCommand is an allowlisted remediation op claimed from SaaS.
type PendingFixCommand struct {
	ID     string            `json:"id"`
	Type   string            `json:"type"`
	Params map[string]string `json:"params"`
}

// HeartbeatResponse is returned by the SaaS heartbeat endpoint.
type HeartbeatResponse struct {
	OK              bool                 `json:"ok"`
	Configuration   *AgentConfiguration  `json:"configuration,omitempty"`
	PendingCommands []PendingFixCommand  `json:"pendingCommands,omitempty"`
}

// FixCommandStatusRequest reports command outcome.
type FixCommandStatusRequest struct {
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Event is a structured incident event.
type Event struct {
	Type       string            `json:"type"`
	Severity   string            `json:"severity"`
	Title      string            `json:"title"`
	Message    string            `json:"message"`
	Namespace  string            `json:"namespace,omitempty"`
	Resource   string            `json:"resource,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	OccurredAt time.Time         `json:"occurredAt"`
	Labels     map[string]string `json:"labels,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	// Snapshot is a frozen evidence pack (CrashLoop/OOM/ImagePull).
	Snapshot map[string]any `json:"snapshot,omitempty"`
}

// BatchRequest wraps a list of events.
type BatchRequest struct {
	Events []Event `json:"events"`
}

// Register calls POST /v1/agents/register.
func (c *Client) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	var resp RegisterResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/agents/register", "", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Heartbeat calls POST /v1/agents/heartbeat and returns remote configuration.
func (c *Client) Heartbeat(ctx context.Context, token string, req HeartbeatRequest) (*HeartbeatResponse, error) {
	var resp HeartbeatResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/agents/heartbeat", token, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// FetchConfig calls GET /v1/agents/config.
func (c *Client) FetchConfig(ctx context.Context, token string) (*AgentConfiguration, error) {
	var resp struct {
		RedactionPatterns []string `json:"redactionPatterns"`
		RedactionLabels   []string `json:"redactionLabels"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/agents/config", token, nil, &resp); err != nil {
		return nil, err
	}
	return &AgentConfiguration{
		RedactionPatterns: resp.RedactionPatterns,
		RedactionLabels:   resp.RedactionLabels,
	}, nil
}

// SendEvents calls POST /v1/events/batch.
func (c *Client) SendEvents(ctx context.Context, token string, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	return c.doJSON(ctx, http.MethodPost, "/v1/events/batch", token, BatchRequest{Events: events}, nil)
}

// ReportFixCommandStatus calls POST /v1/agents/commands/:id/status.
func (c *Client) ReportFixCommandStatus(ctx context.Context, token, commandID string, req FixCommandStatusRequest) error {
	path := "/v1/agents/commands/" + commandID + "/status"
	return c.doJSON(ctx, http.MethodPost, path, token, req, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, string(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
