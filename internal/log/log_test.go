package log

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestLevelFromString_ValidLevels(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"DEBUG", slog.LevelDebug},
		{"Info", slog.LevelInfo},
		{"  warn  ", slog.LevelWarn},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := LevelFromString(c.in)
			if err != nil {
				t.Fatalf("LevelFromString(%q) returned error: %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("LevelFromString(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestLevelFromString_EmptyDefaultsToInfo(t *testing.T) {
	for _, in := range []string{"", "   "} {
		got, err := LevelFromString(in)
		if err != nil {
			t.Fatalf("LevelFromString(%q) returned error: %v", in, err)
		}
		if got != slog.LevelInfo {
			t.Fatalf("LevelFromString(%q) = %v, want info", in, got)
		}
	}
}

func TestLevelFromString_InvalidReturnsError(t *testing.T) {
	got, err := LevelFromString("verbose")
	if err == nil {
		t.Fatalf("LevelFromString(\"verbose\") expected error, got level %v", got)
	}
	if !strings.Contains(err.Error(), "verbose") {
		t.Fatalf("error should name the invalid input, got: %v", err)
	}
}

// TestLevelFromString_InvalidErrorBoundsEchoedInput verifies an oversized
// (externally influenceable) LOG_LEVEL is not echoed verbatim into the error:
// the message bounds the echoed input so a hostile value cannot flood stderr.
func TestLevelFromString_InvalidErrorBoundsEchoedInput(t *testing.T) {
	long := strings.Repeat("x", 500)
	_, err := LevelFromString(long)
	if err == nil {
		t.Fatal("LevelFromString(long) expected error, got nil")
	}
	if strings.Contains(err.Error(), long) {
		t.Fatalf("error echoed the full unbounded input (len %d); want it bounded: %q", len(long), err.Error())
	}
}

// TestNew_InvalidFormatBoundsEchoedInput verifies an oversized --log-format
// value is not echoed verbatim into the error (parity with the LOG_LEVEL cap):
// a hostile value cannot flood stderr.
func TestNew_InvalidFormatBoundsEchoedInput(t *testing.T) {
	long := strings.Repeat("y", 500)
	_, err := New("info", long, io.Discard)
	if err == nil {
		t.Fatal("New with oversized invalid format expected error, got nil")
	}
	if strings.Contains(err.Error(), long) {
		t.Fatalf("error echoed the full unbounded format (len %d); want it bounded: %q", len(long), err.Error())
	}
}

func TestNew_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("info", "text", &buf)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	logger.Info("hello world", "key", "value")
	out := buf.String()
	if !strings.Contains(out, "hello world") {
		t.Fatalf("text output missing message: %q", out)
	}
	if !strings.Contains(out, "key=value") {
		t.Fatalf("text output missing attribute: %q", out)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("text format should not be JSON: %q", out)
	}
}

func TestNew_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("info", "json", &buf)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	logger.Info("structured", "key", "value")
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("JSON format did not emit valid JSON: %v (%q)", err, buf.String())
	}
	if rec["msg"] != "structured" {
		t.Fatalf("JSON output missing msg: %v", rec)
	}
	if rec["key"] != "value" {
		t.Fatalf("JSON output missing attribute: %v", rec)
	}
}

func TestNew_EmptyFormatDefaultsToText(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("", "", &buf)
	if err != nil {
		t.Fatalf("New with empty level/format returned error: %v", err)
	}
	logger.Info("defaulted")
	out := strings.TrimSpace(buf.String())
	if strings.HasPrefix(out, "{") {
		t.Fatalf("empty format should default to text, got: %q", out)
	}
	if !strings.Contains(out, "defaulted") {
		t.Fatalf("output missing message: %q", out)
	}
}

func TestNew_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("warn", "text", &buf)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	logger.Info("should be filtered")
	logger.Debug("also filtered")
	if buf.Len() != 0 {
		t.Fatalf("info/debug should be suppressed at warn level, got: %q", buf.String())
	}
	logger.Warn("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Fatalf("warn message should appear at warn level, got: %q", buf.String())
	}
}

func TestNew_InvalidLevelReturnsError(t *testing.T) {
	if _, err := New("loud", "text", io.Discard); err == nil {
		t.Fatal("New with invalid level expected error")
	}
}

func TestNew_InvalidFormatReturnsError(t *testing.T) {
	_, err := New("info", "xml", io.Discard)
	if err == nil {
		t.Fatal("New with invalid format expected error")
	}
	if !strings.Contains(err.Error(), "xml") {
		t.Fatalf("error should name the invalid format, got: %v", err)
	}
}

func TestNew_NilWriterReturnsError(t *testing.T) {
	_, err := New("info", "text", nil)
	if err == nil {
		t.Fatal("New with nil writer expected error")
	}
}

func TestFromContext_EmptyContext(t *testing.T) {
	logger := FromContext(context.Background())
	if logger == nil {
		t.Fatal("FromContext should never return nil")
	}
	// Must not panic when used.
	logger.Info("safe")
}

func TestFromContext_NilContext(t *testing.T) {
	//nolint:staticcheck // intentionally passing nil to verify nil-safety
	logger := FromContext(nil)
	if logger == nil {
		t.Fatal("FromContext(nil) should return a non-nil discard logger")
	}
	logger.Info("safe")
}

func TestNewContext_FromContext_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	original, err := New("info", "text", &buf)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	ctx := NewContext(context.Background(), original)
	got := FromContext(ctx)
	if got != original {
		t.Fatal("FromContext did not return the stored logger identity")
	}
}

func TestNewContext_NilContext(t *testing.T) {
	var buf bytes.Buffer
	original, err := New("info", "text", &buf)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	//nolint:staticcheck // intentionally passing nil to verify nil-safety
	ctx := NewContext(nil, original)
	if FromContext(ctx) != original {
		t.Fatal("NewContext(nil, logger) should still store the logger")
	}
}

func TestFromContext_DiscardLoggerNoOutput(t *testing.T) {
	// A context that stored a nil logger should fall back to discard, not panic.
	ctx := NewContext(context.Background(), nil)
	logger := FromContext(ctx)
	if logger == nil {
		t.Fatal("FromContext should return discard logger when stored logger is nil")
	}
	// Two misses should return the same cached discard logger (no per-call alloc).
	if FromContext(context.Background()) != FromContext(context.Background()) {
		t.Fatal("discard logger should be cached, not reallocated per call")
	}
}
