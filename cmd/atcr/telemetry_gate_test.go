package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/telemetry"
)

// boolPtr is a tiny helper for the *bool config axis in the matrix tests.
func boolPtr(b bool) *bool { return &b }

// TestTelemetryEnabledFromEnv covers ATCR_TELEMETRY parsing: unset/blank and
// unparseable values fail open to enabled; the strconv.ParseBool falsy set
// disables (AC 02-01 EC1/EC2). The env var names the ENABLED state directly —
// the inverse boolean direction of ATCR_DISABLE_AST_GROUPING.
func TestTelemetryEnabledFromEnv(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
		want bool
	}{
		{"unset defaults enabled", false, "", true},
		{"blank defaults enabled", true, "", true},
		{"whitespace defaults enabled", true, "   ", true},
		{"zero disables", true, "0", false},
		{"false disables", true, "false", false},
		{"f disables", true, "f", false},
		{"F disables", true, "F", false},
		{"False disables", true, "False", false},
		{"FALSE disables", true, "FALSE", false},
		{"one enables", true, "1", true},
		{"true enables", true, "true", true},
		{"unparseable fails open to enabled", true, "maybe", true},
		{"disabled-word fails open to enabled", true, "disabled", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("ATCR_TELEMETRY", tc.val)
			} else {
				_ = os.Unsetenv("ATCR_TELEMETRY")
			}
			assert.Equal(t, tc.want, telemetryEnabledFromEnv())
		})
	}
}

// TestTelemetryEnabled_FourWayMatrix locks the strict OR-disables truth table:
// telemetry is enabled ONLY when BOTH surfaces agree; a nil config field is
// neutral and can never out-rank a disabling env var (AC 02-03 EC1/EC2).
func TestTelemetryEnabled_FourWayMatrix(t *testing.T) {
	cases := []struct {
		name       string
		envEnabled bool
		cfg        *bool
		want       bool
	}{
		{"env enabled, config nil -> enabled", true, nil, true},
		{"env enabled, config true -> enabled", true, boolPtr(true), true},
		{"env enabled, config false -> disabled", true, boolPtr(false), false},
		{"env disabled, config nil -> disabled", false, nil, false},
		{"env disabled, config true -> disabled (env wins, no override)", false, boolPtr(true), false},
		{"env disabled, config false -> disabled", false, boolPtr(false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, telemetryEnabled(tc.envEnabled, tc.cfg))
		})
	}
}

// writeAtcrConfig writes a .atcr/config.yaml in the current working directory
// (tests chdir into a temp dir via isolate), so the cwd-relative gate reads it.
func writeAtcrConfig(t *testing.T, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte(content), 0o644))
}

// TestTelemetryGate_EnvAndConfig proves the per-run gate combines the live env
// var and the persisted cwd config: either surface disabling is sufficient and
// final, a malformed config value fails safe to disabled (never re-enables), and
// an absent config with no env var is enabled by default.
func TestTelemetryGate_EnvAndConfig(t *testing.T) {
	t.Run("no env, no config -> enabled", func(t *testing.T) {
		isolate(t)
		_ = os.Unsetenv("ATCR_TELEMETRY")
		assert.True(t, telemetryGate())
	})
	t.Run("env=0 alone disables", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "0")
		assert.False(t, telemetryGate())
	})
	t.Run("config false alone disables (no env)", func(t *testing.T) {
		isolate(t)
		_ = os.Unsetenv("ATCR_TELEMETRY")
		writeAtcrConfig(t, "agents: [bruce]\ntelemetry: false\n")
		assert.False(t, telemetryGate())
	})
	t.Run("env=0 beats config true (disabled wins)", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "0")
		writeAtcrConfig(t, "agents: [bruce]\ntelemetry: true\n")
		assert.False(t, telemetryGate())
	})
	t.Run("both enabled -> enabled", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "1")
		writeAtcrConfig(t, "agents: [bruce]\ntelemetry: true\n")
		assert.True(t, telemetryGate())
	})
	t.Run("malformed config value fails safe to disabled", func(t *testing.T) {
		isolate(t)
		_ = os.Unsetenv("ATCR_TELEMETRY")
		writeAtcrConfig(t, "agents: [bruce]\ntelemetry: maybe\n")
		assert.False(t, telemetryGate(), "a corrupt telemetry value must never re-enable telemetry")
	})
}

// countingDoRequest installs a doRequest seam that counts outbound sends without
// touching the network (bypassing TLS), and returns the counter + a restore.
// This is the "counting send-hook" AC 02-01's strictness requirement names.
func countingDoRequest(t *testing.T) *int32 {
	t.Helper()
	var n int32
	restore := telemetry.SetDoRequestForTest(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		atomic.AddInt32(&n, 1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	t.Cleanup(restore)
	return &n
}

// runReconcileGated drives runReconcile with a non-empty-endpoint telemetry
// client injected into the context, then drains the fire-and-forget goroutine so
// the send count is deterministic. Returns nothing — callers assert on the hook.
func runReconcileGated(t *testing.T, client *telemetry.Client, args ...string) {
	t.Helper()
	logger, err := log.New("info", "text", io.Discard)
	require.NoError(t, err)
	cmd := newReconcileCmd()
	ctx := telemetry.NewContext(log.NewContext(context.Background(), logger), client)
	cmd.SetContext(ctx)
	cmd.SetOut(io.Discard)
	cmd.SetErr(new(bytes.Buffer))
	require.NoError(t, cmd.ParseFlags(args))
	_ = runReconcile(cmd, cmd.Flags().Args())
	client.Wait() // drain so the send count is race-free
}

// TestReconcile_TelemetryGate_EndToEnd proves the gate actually suppresses a
// real Send at the runReconcile call site: zero outbound requests when disabled
// by either surface (no goroutine spawned), exactly one when both surfaces
// enable. The injected client points at a non-empty https endpoint so the
// zero-count proves the GATE stopped it, not the empty-endpoint no-op.
func TestReconcile_TelemetryGate_EndToEnd(t *testing.T) {
	const endpoint = "https://telemetry.test/ingest"

	t.Run("ATCR_TELEMETRY=0 -> zero requests", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "0")
		fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
		hits := countingDoRequest(t)
		runReconcileGated(t, telemetry.New(endpoint), "r")
		assert.Equal(t, int32(0), atomic.LoadInt32(hits), "disabled by env: no telemetry request may fire")
	})

	t.Run("config telemetry:false -> zero requests (no env)", func(t *testing.T) {
		isolate(t)
		_ = os.Unsetenv("ATCR_TELEMETRY")
		writeAtcrConfig(t, "agents: [bruce]\ntelemetry: false\n")
		fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
		hits := countingDoRequest(t)
		runReconcileGated(t, telemetry.New(endpoint), "r")
		assert.Equal(t, int32(0), atomic.LoadInt32(hits), "disabled by persisted config: no telemetry request may fire")
	})

	t.Run("enabled -> exactly one request", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "1")
		fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
		hits := countingDoRequest(t)
		runReconcileGated(t, telemetry.New(endpoint), "r")
		assert.Equal(t, int32(1), atomic.LoadInt32(hits), "enabled: exactly one telemetry request fires")
	})
}
