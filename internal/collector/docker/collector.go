package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/opspilot/opspilot-agent/internal/config"
	"github.com/opspilot/opspilot-agent/internal/redactor"
	"github.com/opspilot/opspilot-agent/internal/transport"
)

const (
	logTailLines  = 20
	logMaxBytes   = 2048
	maxContainers = 500
	apiVersion    = "v1.47"
)

// Emitter receives structured incident events.
type Emitter func(event transport.Event)

// Collector polls Docker Engine for watched container health and failures.
type Collector struct {
	logger    *slog.Logger
	emit      Emitter
	client    *http.Client
	baseURL   string
	cfg       *config.Config
	host      string
	watch     watchFilter
	mu        sync.Mutex
	seen      map[string]time.Time
	dedupeTTL time.Duration
	lastRest  map[string]int
}

type containerSummary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Labels  map[string]string `json:"Labels"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Image   string            `json:"Image"`
	Created int64             `json:"Created"`
}

type containerInspect struct {
	ID           string           `json:"Id"`
	Created      string           `json:"Created"`
	Image        string           `json:"Image"`
	RestartCount int              `json:"RestartCount"`
	Config       *containerConfig `json:"Config"`
	State        *containerState  `json:"State"`
}

type containerConfig struct {
	Image  string            `json:"Image"`
	Labels map[string]string `json:"Labels"`
}

type containerState struct {
	Status    string       `json:"Status"`
	Running   bool         `json:"Running"`
	ExitCode  int          `json:"ExitCode"`
	Error     string       `json:"Error"`
	OOMKilled bool         `json:"OOMKilled"`
	Health    *healthState `json:"Health"`
}

type healthState struct {
	Status string `json:"Status"`
}

// New builds a Docker collector.
func New(logger *slog.Logger, emit Emitter, cfg *config.Config) (*Collector, error) {
	socketPath, err := socketPathFromHost(cfg.DockerHost)
	if err != nil {
		return nil, err
	}
	filter, err := parseWatchFilter(cfg.DockerWatch)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Collector{
		logger:    logger,
		emit:      emit,
		client:    &http.Client{Transport: transport, Timeout: 30 * time.Second},
		baseURL:   "http://docker",
		cfg:       cfg,
		host:      host,
		watch:     filter,
		seen:      make(map[string]time.Time),
		dedupeTTL: 15 * time.Minute,
		lastRest:  make(map[string]int),
	}, nil
}

func socketPathFromHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" || host == "unix:///var/run/docker.sock" {
		return "/var/run/docker.sock", nil
	}
	if strings.HasPrefix(host, "unix://") {
		return strings.TrimPrefix(host, "unix://"), nil
	}
	return "", fmt.Errorf("unsupported OPSPILOT_DOCKER_HOST %q (only unix:// sockets supported)", host)
}

// Start polls until stop is closed.
func (c *Collector) Start(ctx context.Context, stop <-chan struct{}) error {
	interval := time.Duration(c.cfg.PollSeconds) * time.Second
	c.logger.Info("docker collector starting",
		"host", c.host,
		"watch", c.cfg.DockerWatch,
		"interval", interval,
	)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	c.poll(ctx)
	for {
		select {
		case <-stop:
			return nil
		case <-ticker.C:
			c.poll(ctx)
		}
	}
}

func (c *Collector) poll(ctx context.Context) {
	containers, err := c.listWatched(ctx)
	if err != nil {
		c.logger.Warn("docker list failed", "error", err)
		return
	}
	for _, ctr := range containers {
		c.inspectContainer(ctx, ctr)
	}
}

func (c *Collector) inspectContainer(ctx context.Context, summary containerSummary) {
	name := containerName(summary)
	service := c.serviceName(summary)
	inspect, err := c.inspect(ctx, summary.ID)
	if err != nil {
		c.logger.Debug("container inspect failed", "name", name, "error", err)
		return
	}
	restarts := inspect.RestartCount
	c.checkRestartLoop(summary.ID, name, service, restarts, inspect)
	state := inspect.State
	if state == nil {
		return
	}
	if state.Health != nil && state.Health.Status == "unhealthy" {
		c.emitHealthIncident(name, service, inspect, state.Health.Status)
	}
	if state.OOMKilled {
		c.emitOOMIncident(ctx, name, service, inspect)
		return
	}
	if state.Status == "exited" && state.ExitCode != 0 {
		c.emitExitIncident(ctx, name, service, inspect, state.ExitCode, state.Error)
	}
}

func (c *Collector) checkRestartLoop(id, name, service string, restarts int, inspect containerInspect) {
	prev, ok := c.lastRest[id]
	c.lastRest[id] = restarts
	if !ok || restarts <= prev || restarts < 3 {
		return
	}
	c.emitOnce(fmt.Sprintf("container:%s:RestartLoop", name), transport.Event{
		Type: "ContainerRestartLoop", Severity: "warning", Source: "docker", Service: service, Host: c.host,
		Title:    fmt.Sprintf("Container %s restart loop (%d restarts)", name, restarts),
		Message:  fmt.Sprintf("Container %s has restarted %d times", name, restarts),
		Resource: "container/" + name, Reason: "RestartLoop",
		Metadata: map[string]string{"restartCount": fmt.Sprintf("%d", restarts), "status": containerStatus(inspect), "image": inspectImage(inspect)},
		Snapshot: buildSnapshot(name, service, inspect, "", ""),
	})
}

func (c *Collector) emitHealthIncident(name, service string, inspect containerInspect, health string) {
	c.emitOnce(fmt.Sprintf("container:%s:Unhealthy", name), transport.Event{
		Type: "ContainerUnhealthy", Severity: "warning", Source: "docker", Service: service, Host: c.host,
		Title:    fmt.Sprintf("Container %s unhealthy", name),
		Message:  fmt.Sprintf("Docker health check reports %s for %s", health, name),
		Resource: "container/" + name, Reason: "Unhealthy",
		Metadata: map[string]string{"health": health, "status": containerStatus(inspect), "image": inspectImage(inspect)},
		Snapshot: buildSnapshot(name, service, inspect, health, ""),
	})
}

func (c *Collector) emitOOMIncident(ctx context.Context, name, service string, inspect containerInspect) {
	logSnippet := c.fetchLogSnippet(ctx, inspect.ID)
	c.emitOnce(fmt.Sprintf("container:%s:OOMKilled", name), transport.Event{
		Type: "ContainerOOMKilled", Severity: "critical", Source: "docker", Service: service, Host: c.host,
		Title:    fmt.Sprintf("Container %s OOMKilled", name),
		Message:  fmt.Sprintf("Container %s was killed by the kernel (OOM)", name),
		Resource: "container/" + name, Reason: "OOMKilled",
		Metadata: map[string]string{"status": "OOMKilled", "image": inspectImage(inspect)},
		Snapshot: buildSnapshot(name, service, inspect, "OOMKilled", logSnippet),
	})
}

func (c *Collector) emitExitIncident(ctx context.Context, name, service string, inspect containerInspect, exitCode int, errMsg string) {
	logSnippet := c.fetchLogSnippet(ctx, inspect.ID)
	msg := fmt.Sprintf("Container %s exited with code %d", name, exitCode)
	if errMsg != "" {
		msg += ": " + redactor.Redact(errMsg)
	}
	c.emitOnce(fmt.Sprintf("container:%s:Exited:%d", name, exitCode), transport.Event{
		Type: "ContainerExited", Severity: "warning", Source: "docker", Service: service, Host: c.host,
		Title:    fmt.Sprintf("Container %s exited (%d)", name, exitCode),
		Message:  msg,
		Resource: "container/" + name, Reason: "Exited",
		Metadata: map[string]string{"exitCode": fmt.Sprintf("%d", exitCode), "status": "exited", "image": inspectImage(inspect)},
		Snapshot: buildSnapshot(name, service, inspect, "exited", logSnippet),
	})
}

func (c *Collector) emitOnce(key string, event transport.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if t, ok := c.seen[key]; ok && now.Sub(t) < c.dedupeTTL {
		return
	}
	for k, t := range c.seen {
		if now.Sub(t) > c.dedupeTTL {
			delete(c.seen, k)
		}
	}
	c.seen[key] = now
	if event.OccurredAt.IsZero() {
		event.OccurredAt = now.UTC()
	}
	if event.Service == "" && c.cfg.Service != "" {
		event.Service = c.cfg.Service
	}
	c.logger.Info("incident detected", "type", event.Type, "resource", event.Resource, "source", event.Source)
	c.emit(event)
}

func (c *Collector) fetchLogSnippet(ctx context.Context, id string) string {
	path := fmt.Sprintf("%s/%s/containers/%s/logs?stdout=1&stderr=1&tail=%d", c.baseURL, apiVersion, id, logTailLines)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return ""
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, logMaxBytes))
	if err != nil {
		return ""
	}
	text := sanitizeUTF8(string(demuxDockerLogs(raw)))
	return redactor.Redact(strings.TrimSpace(text))
}

func (c *Collector) listWatched(ctx context.Context) ([]containerSummary, error) {
	path := fmt.Sprintf("%s/%s/containers/json?all=1", c.baseURL, apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("docker list: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var list []containerSummary
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	out := make([]containerSummary, 0, len(list))
	for _, ctr := range list {
		if c.watch.matches(ctr) {
			out = append(out, ctr)
		}
	}
	return out, nil
}

func (c *Collector) inspect(ctx context.Context, id string) (containerInspect, error) {
	path := fmt.Sprintf("%s/%s/containers/%s/json", c.baseURL, apiVersion, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return containerInspect{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return containerInspect{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return containerInspect{}, fmt.Errorf("docker inspect: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out containerInspect
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return containerInspect{}, err
	}
	return out, nil
}

func (c *Collector) serviceName(summary containerSummary) string {
	if c.cfg.Service != "" {
		return c.cfg.Service
	}
	if v := summary.Labels["opspilot.service"]; v != "" {
		return v
	}
	return containerName(summary)
}

// ListMonitoredContainers returns watched containers for heartbeat inventory.
func (c *Collector) ListMonitoredContainers(ctx context.Context) ([]transport.ContainerInventoryItem, error) {
	list, err := c.listWatched(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	now := time.Now()
	out := make([]transport.ContainerInventoryItem, 0, len(list))
	for _, summary := range list {
		inspect, err := c.inspect(ctx, summary.ID)
		if err != nil {
			continue
		}
		created := parseCreated(inspect.Created, summary.Created)
		out = append(out, transport.ContainerInventoryItem{
			Name:     containerName(summary),
			Status:   containerStatus(inspect),
			Health:   healthStatus(inspect),
			Restarts: inspect.RestartCount,
			Age:      formatAge(now.Sub(created)),
			Image:    inspectImage(inspect),
			Service:  c.serviceName(summary),
		})
		if len(out) >= maxContainers {
			break
		}
	}
	return out, nil
}

func parseCreated(iso string, unixSec int64) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, iso); err == nil {
		return t
	}
	if unixSec > 0 {
		return time.Unix(unixSec, 0)
	}
	return time.Now()
}
