package log

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// newBufLogger builds a debug-level text logger over buf for redaction tests.
func newBufLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestWithRedactor_RedactsMessage verifies the record message is scrubbed at the
// sink: a secret-shaped token disappears (AC5) and an absolute path under the
// review root is relativized (AC6).
func TestWithRedactor_RedactsMessage(t *testing.T) {
	root := filepath.Join("tmp", "review-root")
	var buf bytes.Buffer
	logger := WithRedactor(newBufLogger(&buf), NewRedactor(root))

	abs := filepath.Join(root, "internal", "foo.go")
	logger.Info("loaded " + abs + " with key sk-supersecret123")

	out := buf.String()
	if strings.Contains(out, "sk-supersecret123") {
		t.Fatalf("sk- token leaked in message: %q", out)
	}
	if strings.Contains(out, root+string(filepath.Separator)) {
		t.Fatalf("absolute root not relativized in message: %q", out)
	}
	if !strings.Contains(out, filepath.Join("internal", "foo.go")) {
		t.Fatalf("expected relative path in message: %q", out)
	}
}

// TestWithRedactor_RedactsStringAttr verifies string attribute values are
// scrubbed, not just the message.
func TestWithRedactor_RedactsStringAttr(t *testing.T) {
	var buf bytes.Buffer
	logger := WithRedactor(newBufLogger(&buf), NewRedactor(""))

	logger.Warn("provider error", "detail", "token Bearer abc123secret rejected")

	out := buf.String()
	if strings.Contains(out, "abc123secret") {
		t.Fatalf("bearer token leaked in attr value: %q", out)
	}
	if !strings.Contains(out, "Bearer [redacted]") {
		t.Fatalf("expected redaction marker in attr: %q", out)
	}
}

// TestWithRedactor_RedactsErrorAttr verifies error-valued attributes are scrubbed
// by their message (the common `"error", err` shape).
func TestWithRedactor_RedactsErrorAttr(t *testing.T) {
	var buf bytes.Buffer
	logger := WithRedactor(newBufLogger(&buf), NewRedactor(""))

	logger.Error("call failed", "error", errors.New("auth rejected sk-leakedkey999"))

	out := buf.String()
	if strings.Contains(out, "sk-leakedkey999") {
		t.Fatalf("sk- token leaked through error attr: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("expected redaction marker for error attr: %q", out)
	}
}

// TestWithRedactor_PreservesCorrelationAttrs verifies redaction composes with the
// correlation attributes: review_id and agent_name still appear, and a secret in
// the message is still scrubbed, regardless of attachment order.
func TestWithRedactor_PreservesCorrelationAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufLogger(&buf)
	logger = WithReviewID(logger, "2026-06-17_feat")
	logger = WithRedactor(logger, NewRedactor(""))
	logger = WithAgent(logger, "security")

	logger.Info("scanning with sk-topsecret000")

	out := buf.String()
	if !strings.Contains(out, AttrReviewID+"=2026-06-17_feat") {
		t.Fatalf("review_id lost after redactor wrap: %q", out)
	}
	if !strings.Contains(out, AttrAgentName+"=security") {
		t.Fatalf("agent_name lost after redactor wrap: %q", out)
	}
	if strings.Contains(out, "sk-topsecret000") {
		t.Fatalf("secret leaked despite redactor: %q", out)
	}
}

// TestWithRedactor_RedactsGroupedAttrs verifies the handler's WithGroup path and
// the KindGroup recursion in redactAttr: a secret nested inside a group-valued
// attribute, under a WithGroup-scoped logger, is still scrubbed.
func TestWithRedactor_RedactsGroupedAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := WithRedactor(newBufLogger(&buf), NewRedactor("")).WithGroup("req")

	logger.Info("call", slog.Group("auth", "header", "Bearer abc123secret"))

	out := buf.String()
	if strings.Contains(out, "abc123secret") {
		t.Fatalf("secret leaked through grouped attr: %q", out)
	}
	if !strings.Contains(out, "Bearer [redacted]") {
		t.Fatalf("expected redaction inside group: %q", out)
	}
}

// TestWithRedactor_PreservesNonStringAttrs verifies non-string attribute kinds
// (bool, int) pass through unchanged.
func TestWithRedactor_PreservesNonStringAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := WithRedactor(newBufLogger(&buf), NewRedactor(""))

	// meta is a non-error KindAny value: it must pass through unchanged (exercises
	// the KindAny non-error branch of redactAttr).
	meta := struct{ Count int }{Count: 7}
	logger.Info("invoking agent", "tools", true, "turns", 3, "meta", meta)

	out := buf.String()
	if !strings.Contains(out, "tools=true") {
		t.Fatalf("bool attr altered: %q", out)
	}
	if !strings.Contains(out, "turns=3") {
		t.Fatalf("int attr altered: %q", out)
	}
	if !strings.Contains(out, "Count:7") {
		t.Fatalf("non-error any attr altered: %q", out)
	}
}

// TestWithRedactor_PreservesLevelFiltering verifies Enabled delegates to the base
// handler: a debug line is still suppressed when the base level is info.
func TestWithRedactor_PreservesLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger := WithRedactor(base, NewRedactor(""))

	logger.Debug("should be suppressed sk-secret")
	if buf.Len() != 0 {
		t.Fatalf("debug line emitted despite info level: %q", buf.String())
	}
	logger.Info("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Fatalf("info line suppressed: %q", buf.String())
	}
}

// TestWithRedactor_NilSafe verifies nil logger or nil redactor returns the input
// unchanged so callers can wire it unconditionally.
func TestWithRedactor_NilSafe(t *testing.T) {
	if got := WithRedactor(nil, NewRedactor("")); got != nil {
		t.Fatalf("nil logger must return nil, got %v", got)
	}
	var buf bytes.Buffer
	base := newBufLogger(&buf)
	if got := WithRedactor(base, nil); got != base {
		t.Fatalf("nil redactor must return the original logger unchanged")
	}
}
