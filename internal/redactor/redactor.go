package redactor

import (
	"regexp"
	"strings"
	"sync"
)

var (
	bearerRE    = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9\-._~+/]+=*`)
	tokenRE     = regexp.MustCompile(`(?i)(token|api[_-]?key|secret|password|authorization)\s*[:=]\s*["']?([^\s"',;]+)`)
	jwtRE       = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	connStrRE   = regexp.MustCompile(`(?i)(postgres(?:ql)?|mysql|mongodb(?:\+srv)?)://[^\s]+`)
	hexSecretRE = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)

	customMu  sync.RWMutex
	customREs []*regexp.Regexp
)

// SetCustomPatterns replaces customer-defined regex patterns from SaaS config.
// Invalid patterns are skipped.
func SetCustomPatterns(patterns []string) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		compiled = append(compiled, re)
	}
	customMu.Lock()
	customREs = compiled
	customMu.Unlock()
}

// Redact replaces likely secrets in free-form text.
func Redact(s string) string {
	if s == "" {
		return s
	}
	out := bearerRE.ReplaceAllString(s, "${1}[REDACTED]")
	out = tokenRE.ReplaceAllString(out, "${1}=[REDACTED]")
	out = jwtRE.ReplaceAllString(out, "[REDACTED_JWT]")
	out = connStrRE.ReplaceAllString(out, "[REDACTED_CONNECTION_STRING]")
	out = hexSecretRE.ReplaceAllString(out, "[REDACTED_HEX]")

	customMu.RLock()
	customs := customREs
	customMu.RUnlock()
	for _, re := range customs {
		out = re.ReplaceAllString(out, "[REDACTED]")
	}
	return out
}

// RedactMap redacts all string values in a map.
func RedactMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "token") || strings.Contains(lk, "secret") ||
			strings.Contains(lk, "password") || strings.Contains(lk, "key") {
			out[k] = "[REDACTED]"
			continue
		}
		out[k] = Redact(v)
	}
	return out
}
