package redactor_test

import (
	"strings"
	"testing"

	"github.com/opspilot/opspilot-agent/internal/redactor"
)

func TestBuiltinRedaction(t *testing.T) {
	in := `Authorization: Bearer abc.def.ghi password=supersecret`
	out := redactor.Redact(in)
	if strings.Contains(out, "supersecret") {
		t.Fatalf("expected secrets redacted, got %q", out)
	}
}

func TestCustomPatterns(t *testing.T) {
	redactor.SetCustomPatterns([]string{`employeeNumber=[A-Z][0-9]+`})
	defer redactor.SetCustomPatterns(nil)

	in := `user employeeNumber=A12345 failed login`
	out := redactor.Redact(in)
	if strings.Contains(out, "A12345") {
		t.Fatalf("expected custom pattern redacted, got %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker, got %q", out)
	}
}
