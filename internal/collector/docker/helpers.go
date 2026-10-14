package docker

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type watchFilter struct {
	all        bool
	labelKey   string
	labelVal   string
	namePrefix string
}

func parseWatchFilter(spec string) (watchFilter, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return watchFilter{labelKey: "opspilot.watch", labelVal: "true"}, nil
	}
	if spec == "all" {
		return watchFilter{all: true}, nil
	}
	if strings.HasPrefix(spec, "label:") {
		pair := strings.TrimPrefix(spec, "label:")
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return watchFilter{}, fmt.Errorf("invalid OPSPILOT_DOCKER_WATCH label filter %q (want label:key=value)", spec)
		}
		return watchFilter{labelKey: k, labelVal: v}, nil
	}
	if strings.HasPrefix(spec, "name:") {
		prefix := strings.TrimPrefix(spec, "name:")
		if prefix == "" {
			return watchFilter{}, fmt.Errorf("invalid OPSPILOT_DOCKER_WATCH name filter %q", spec)
		}
		return watchFilter{namePrefix: prefix}, nil
	}
	return watchFilter{}, fmt.Errorf("unsupported OPSPILOT_DOCKER_WATCH %q (use all, label:key=value, or name:prefix)", spec)
}

func (f watchFilter) matches(summary containerSummary) bool {
	if f.all {
		return true
	}
	name := containerName(summary)
	if f.namePrefix != "" {
		return strings.HasPrefix(name, f.namePrefix)
	}
	if f.labelKey != "" {
		val, ok := summary.Labels[f.labelKey]
		if !ok {
			return false
		}
		if f.labelVal == "" || f.labelVal == "*" {
			return true
		}
		return val == f.labelVal
	}
	return false
}

func containerName(summary containerSummary) string {
	for _, n := range summary.Names {
		return strings.TrimPrefix(n, "/")
	}
	if len(summary.ID) >= 12 {
		return summary.ID[:12]
	}
	return summary.ID
}

func inspectImage(inspect containerInspect) string {
	if inspect.Config != nil && inspect.Config.Image != "" {
		return inspect.Config.Image
	}
	return inspect.Image
}

func containerStatus(inspect containerInspect) string {
	if inspect.State == nil {
		return "unknown"
	}
	if inspect.State.Health != nil && inspect.State.Health.Status != "" {
		return inspect.State.Health.Status
	}
	return inspect.State.Status
}

func healthStatus(inspect containerInspect) string {
	if inspect.State != nil && inspect.State.Health != nil {
		return inspect.State.Health.Status
	}
	return ""
}

func buildSnapshot(name, service string, inspect containerInspect, status, logSnippet string) map[string]any {
	id := inspect.ID
	if len(id) > 12 {
		id = id[:12]
	}
	snap := map[string]any{
		"version":    1,
		"capturedAt": time.Now().UTC().Format(time.RFC3339),
		"resource": map[string]any{
			"kind": "Container", "name": name, "service": service, "id": id,
		},
		"container": map[string]any{"name": name, "image": inspectImage(inspect)},
	}
	if inspect.State != nil {
		snap["status"] = map[string]any{
			"state":        statusOr(inspect, status),
			"health":       healthStatus(inspect),
			"exitCode":     inspect.State.ExitCode,
			"restartCount": inspect.RestartCount,
			"oomKilled":    inspect.State.OOMKilled,
		}
	}
	if logSnippet != "" {
		snap["logs"] = map[string]any{"snippet": logSnippet, "source": "stderr/stdout"}
	}
	return snap
}

func statusOr(inspect containerInspect, override string) string {
	if override != "" {
		return override
	}
	if inspect.State != nil {
		return inspect.State.Status
	}
	return ""
}

// demuxDockerLogs strips Docker Engine multiplex stream headers from log API output.
func demuxDockerLogs(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	var out []byte
	i := 0
	for i+8 <= len(raw) {
		stream := raw[i]
		if stream != 1 && stream != 2 {
			return raw
		}
		if raw[i+1] != 0 || raw[i+2] != 0 || raw[i+3] != 0 {
			return raw
		}
		size := int(binary.BigEndian.Uint32(raw[i+4 : i+8]))
		i += 8
		if size <= 0 || i+size > len(raw) {
			return raw
		}
		out = append(out, raw[i:i+size]...)
		i += size
	}
	if len(out) > 0 {
		return out
	}
	return raw
}

func sanitizeUTF8(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "")
}

func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int(d.Seconds())
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	min := sec / 60
	if min < 60 {
		return fmt.Sprintf("%dm", min)
	}
	hr := min / 60
	if hr < 24 {
		return fmt.Sprintf("%dh", hr)
	}
	return fmt.Sprintf("%dd", hr/24)
}
