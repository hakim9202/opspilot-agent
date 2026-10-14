package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type hostConfig struct {
	Binds         []string                 `json:"Binds"`
	PortBindings  map[string][]portBinding `json:"PortBindings"`
	RestartPolicy map[string]any           `json:"RestartPolicy"`
	NetworkMode   string                   `json:"NetworkMode"`
}

type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type inspectForRecreate struct {
	Name       string           `json:"Name"`
	Config     *containerConfig `json:"Config"`
	HostConfig *hostConfig      `json:"HostConfig"`
	State      *containerState  `json:"State"`
}

type createBody struct {
	Image      string            `json:"Image"`
	Env        []string          `json:"Env,omitempty"`
	Cmd        []string          `json:"Cmd,omitempty"`
	Entrypoint []string          `json:"Entrypoint,omitempty"`
	WorkingDir string            `json:"WorkingDir,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
	HostConfig *hostConfig       `json:"HostConfig,omitempty"`
}

type createResponse struct {
	ID string `json:"Id"`
}

// RestartContainer starts or restarts a container by name.
func (c *Collector) RestartContainer(ctx context.Context, name string) (string, error) {
	id, state, err := c.findContainer(ctx, name)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(state, "exited") || strings.EqualFold(state, "created") || strings.EqualFold(state, "dead") {
		if err := c.dockerPost(ctx, fmt.Sprintf("%s/%s/containers/%s/start", c.baseURL, apiVersion, id), nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("started container %s", name), nil
	}
	if err := c.dockerPost(ctx, fmt.Sprintf("%s/%s/containers/%s/restart?t=5", c.baseURL, apiVersion, id), nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("restarted container %s", name), nil
}

// RecreateWithImage removes and recreates a container with a new image, preserving basic host config.
func (c *Collector) RecreateWithImage(ctx context.Context, name, image string) (string, error) {
	if strings.TrimSpace(image) == "" {
		return "", fmt.Errorf("image is required")
	}
	id, _, err := c.findContainer(ctx, name)
	if err != nil {
		return "", err
	}
	rawInspect, err := c.inspectRaw(ctx, id)
	if err != nil {
		return "", fmt.Errorf("inspect container: %w", err)
	}
	inspect, err := c.inspectFull(ctx, id)
	if err != nil {
		return "", err
	}
	_ = c.dockerPost(ctx, fmt.Sprintf("%s/%s/containers/%s/stop?t=5", c.baseURL, apiVersion, id), nil)
	if err := c.dockerRequest(ctx, http.MethodDelete, fmt.Sprintf("%s/%s/containers/%s?v=1", c.baseURL, apiVersion, id), nil); err != nil {
		return "", fmt.Errorf("remove container: %w", err)
	}

	body := createBody{Image: image, HostConfig: inspect.HostConfig}
	if inspect.Config != nil {
		body.Labels = inspect.Config.Labels
	}
	if rawInspect.Config != nil {
		body.Env = rawInspect.Config.Env
		body.Cmd = rawInspect.Config.Cmd
		body.Entrypoint = rawInspect.Config.Entrypoint
		body.WorkingDir = rawInspect.Config.WorkingDir
		if body.Labels == nil {
			body.Labels = rawInspect.Config.Labels
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	createURL := fmt.Sprintf("%s/%s/containers/create?name=%s", c.baseURL, apiVersion, name)
	var created createResponse
	if err := c.dockerJSON(ctx, http.MethodPost, createURL, payload, &created); err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	if err := c.dockerPost(ctx, fmt.Sprintf("%s/%s/containers/%s/start", c.baseURL, apiVersion, created.ID), nil); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}
	return fmt.Sprintf("recreated container %s with image %s", name, image), nil
}

func (c *Collector) findContainer(ctx context.Context, name string) (id, state string, err error) {
	path := fmt.Sprintf("%s/%s/containers/json?all=1", c.baseURL, apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("docker list: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var list []containerSummary
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", "", err
	}
	want := strings.TrimPrefix(strings.TrimSpace(name), "/")
	for _, ctr := range list {
		if containerName(ctr) == want {
			return ctr.ID, ctr.State, nil
		}
	}
	return "", "", fmt.Errorf("container %q not found", name)
}

func (c *Collector) inspectFull(ctx context.Context, id string) (inspectForRecreate, error) {
	var out inspectForRecreate
	path := fmt.Sprintf("%s/%s/containers/%s/json", c.baseURL, apiVersion, id)
	if err := c.dockerJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return out, err
	}
	return out, nil
}

type rawContainerConfig struct {
	Env        []string          `json:"Env"`
	Cmd        []string          `json:"Cmd"`
	Entrypoint []string          `json:"Entrypoint"`
	WorkingDir string            `json:"WorkingDir"`
	Labels     map[string]string `json:"Labels"`
}

type rawInspect struct {
	Config *rawContainerConfig `json:"Config"`
}

func (c *Collector) inspectRaw(ctx context.Context, id string) (rawInspect, error) {
	var out rawInspect
	path := fmt.Sprintf("%s/%s/containers/%s/json", c.baseURL, apiVersion, id)
	if err := c.dockerJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Collector) dockerPost(ctx context.Context, url string, body []byte) error {
	return c.dockerRequest(ctx, http.MethodPost, url, body)
}

func (c *Collector) dockerRequest(ctx context.Context, method, url string, body []byte) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: %s", method, resp.Status, strings.TrimSpace(string(b)))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *Collector) dockerJSON(ctx context.Context, method, url string, body []byte, out any) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: %s", method, resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
