package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execute runs the root command with args and returns combined output.
func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestRootCmd_Use(t *testing.T) {
	root := newRootCmd()
	assert.Equal(t, "atcr", root.Use)
}

func TestRootCmd_HelpListsAllSubcommands(t *testing.T) {
	out, err := execute(t, "--help")
	require.NoError(t, err)

	for _, sub := range []string{"review", "reconcile", "verify", "report", "range", "status", "init", "serve", "doctor", "trust", "scorecard", "leaderboard"} {
		assert.Contains(t, out, sub, "help output must list subcommand %q", sub)
	}
}

func TestRootCmd_HasExactlyTwelveSubcommands(t *testing.T) {
	// The ten prior commands plus `scorecard` and `leaderboard` (epic 3.3), the
	// per-run scorecard reader and the aggregated leaderboard.
	root := newRootCmd()
	names := map[string]bool{}
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		names[c.Name()] = true
	}
	assert.Len(t, names, 12)
	for _, sub := range []string{"review", "reconcile", "verify", "report", "range", "status", "init", "serve", "doctor", "trust", "scorecard", "leaderboard"} {
		assert.True(t, names[sub], "subcommand %q must be registered", sub)
	}
}

func TestRootCmd_UnknownSubcommandErrors(t *testing.T) {
	_, err := execute(t, "no-such-command")
	assert.Error(t, err)
}

func TestRootCmd_UnknownSubcommandIsUsageError(t *testing.T) {
	// A typo'd subcommand is a usage error (exit 2) — in CI, exit 1 means
	// "findings at/above threshold", so the two must never conflate.
	_, err := execute(t, "no-such-command")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err))
}

func TestRootCmd_BareInvocationShowsHelp(t *testing.T) {
	out, err := execute(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Usage:")
}

func TestExitCode(t *testing.T) {
	plain := errors.New("boom")
	coded := usageError(plain)

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil error", nil, 0},
		{"plain error", plain, 1},
		{"coded usage error", coded, 2},
		{"wrapped coded error", fmt.Errorf("context: %w", coded), 2},
		{"joined coded error", errors.Join(plain, coded), 2},
		{"explicit zero code", &codedError{code: 0, err: plain}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exitCode(tt.err))
		})
	}
}

func TestFlagRelationships(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"review head without base", []string{"review", "--head", "def"}},
		{"review base with merge-commit", []string{"review", "--base", "abc", "--head", "def", "--merge-commit", "fff"}},
		{"review head with merge-commit", []string{"review", "--head", "def", "--merge-commit", "fff"}},
		{"range head without base", []string{"range", "--head", "def"}},
		{"range head with merge-commit", []string{"range", "--head", "def", "--merge-commit", "fff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := execute(t, tt.args...)
			require.Error(t, err)
			assert.Equal(t, 2, exitCode(err), "flag-group violations are usage errors")
		})
	}
}

func TestUsageErrors_ExitCodeTwo(t *testing.T) {
	t.Run("unknown flag", func(t *testing.T) {
		_, err := execute(t, "review", "--no-such-flag")
		require.Error(t, err)
		assert.Equal(t, 2, exitCode(err))
	})
	t.Run("unexpected positional arg", func(t *testing.T) {
		_, err := execute(t, "init", "unexpected")
		require.Error(t, err)
		assert.Equal(t, 2, exitCode(err))
	})
}

// --- Logging wiring (sprint 4.0, tasks 3.1–3.2) ---------------------------

// TestRootCmd_LogFormatDefault verifies the persistent --log-format flag exists
// on the root command and defaults to "text" (AC2), inherited by all subcommands.
func TestRootCmd_LogFormatDefault(t *testing.T) {
	root := newRootCmd()
	f := root.PersistentFlags().Lookup("log-format")
	require.NotNil(t, f, "root must declare a persistent --log-format flag")
	assert.Equal(t, "text", f.DefValue, "--log-format defaults to text")
}

// TestRootCmd_LogLevelFromEnv verifies LOG_LEVEL is read from the environment.
func TestRootCmd_LogLevelFromEnv(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	assert.Equal(t, "debug", logLevelFromEnv())
}

// TestRootCmd_LogLevelEnvEmptyDefaultsToInfo verifies an unset/blank LOG_LEVEL
// defaults to info (AC1).
func TestRootCmd_LogLevelEnvEmptyDefaultsToInfo(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	assert.Equal(t, "info", logLevelFromEnv())
	t.Setenv("LOG_LEVEL", "   ")
	assert.Equal(t, "info", logLevelFromEnv(), "whitespace-only LOG_LEVEL is treated as unset")
}

// TestSetupLogger_RedactsSecrets verifies the root logger constructed in
// setupLogger scrubs secret-shaped tokens (AC5) at the single construction point,
// so EVERY command (CLI, serve, MCP) inherits one already-redacted logger.
func TestSetupLogger_RedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	root := newRootCmd()
	root.SetContext(context.Background())
	root.SetErr(&buf)
	require.NoError(t, setupLogger(root))

	log.FromContext(root.Context()).Info("token leak", "key", "sk-secret123")

	out := buf.String()
	require.NotContains(t, out, "sk-secret123", "secret-shaped token must be scrubbed at the root logger (AC5)")
	require.Contains(t, out, "[redacted]")
}

// TestPersistentPreRunE_ValidLevelAndFormat verifies setupLogger constructs a
// logger and stores it in the command context (replacing the discard fallback).
func TestPersistentPreRunE_ValidLevelAndFormat(t *testing.T) {
	t.Setenv("LOG_LEVEL", "info")
	root := newRootCmd()
	root.SetContext(context.Background())
	root.SetErr(io.Discard)

	before := log.FromContext(root.Context())
	require.NoError(t, setupLogger(root))
	after := log.FromContext(root.Context())

	require.NotSame(t, before, after, "setupLogger must store a new logger in the context")
}

// TestPersistentPreRunE_InvalidLevel verifies an unparseable LOG_LEVEL is a usage
// error (exit 2) surfaced before any subcommand runs.
func TestPersistentPreRunE_InvalidLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "bogus")
	root := newRootCmd()
	root.SetContext(context.Background())
	root.SetErr(io.Discard)

	err := setupLogger(root)
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "an invalid LOG_LEVEL is a usage error")
}

// TestPersistentPreRunE_InvalidFormat verifies an unknown --log-format value is a
// usage error (exit 2).
func TestPersistentPreRunE_InvalidFormat(t *testing.T) {
	t.Setenv("LOG_LEVEL", "info")
	root := newRootCmd()
	root.SetContext(context.Background())
	root.SetErr(io.Discard)
	require.NoError(t, root.ParseFlags([]string{"--log-format", "bogus"}))

	err := setupLogger(root)
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "an invalid --log-format is a usage error")
}

// --- Graceful shutdown / signal handling (epic 4.1) -----------------------

// stubForceExit replaces the package forceExit and gracefulShutdownTimeout for a
// test, returning a pointer to the captured exit code (-1 until forceExit fires)
// and registering cleanup. The shrunk timeout keeps the handler goroutine from
// blocking for the real 10s grace period.
func stubForceExit(t *testing.T, timeout time.Duration) *int32 {
	t.Helper()
	origExit, origTimeout := forceExit, gracefulShutdownTimeout
	t.Cleanup(func() { forceExit = origExit; gracefulShutdownTimeout = origTimeout })
	var code int32 = -1
	forceExit = func(c int) { atomic.StoreInt32(&code, int32(c)) }
	gracefulShutdownTimeout = timeout
	return &code
}

// TestHandleSignals_CancelsContextOnSignal verifies a single SIGINT cancels the
// root context (AC2/AC3 — the fanout engine drains cooperatively off this signal)
// and prints the graceful-shutdown notice to the writer (AC1).
func TestHandleSignals_CancelsContextOnSignal(t *testing.T) {
	code := stubForceExit(t, 15*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	var buf bytes.Buffer
	handleSignals(sigCh, cancel, &buf)

	sigCh <- syscall.SIGINT

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled after SIGINT")
	}
	assert.ErrorIs(t, ctx.Err(), context.Canceled)

	// Once the grace timer fires forceExit, the goroutine has returned, so reading
	// buf is race-free.
	require.Eventually(t, func() bool { return atomic.LoadInt32(code) == 1 }, time.Second, 5*time.Millisecond)
	assert.Contains(t, buf.String(), "shutting down gracefully", "AC1: graceful notice printed")
}

// TestHandleSignals_ForceExitsAfterGracePeriod verifies that when cooperative
// shutdown overruns the grace period the handler force-exits with code 1 and
// prints the timeout notice (AC7).
func TestHandleSignals_ForceExitsAfterGracePeriod(t *testing.T) {
	code := stubForceExit(t, 10*time.Millisecond)

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	var buf bytes.Buffer
	handleSignals(sigCh, cancel, &buf)

	sigCh <- syscall.SIGTERM // SIGTERM behaves identically to SIGINT

	require.Eventually(t, func() bool { return atomic.LoadInt32(code) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(code), "AC7: force-exit code 1 after grace period")
	assert.Contains(t, buf.String(), "forcing exit", "AC7: timeout notice printed")
}

func TestRootCmd_SubcommandsUseRunE(t *testing.T) {
	// Handlers must return errors (RunE) so exit codes are mapped centrally
	// in main() — no os.Exit inside handlers.
	root := newRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		assert.Nil(t, c.Run, "%s must not use Run", c.Name())
		assert.NotNil(t, c.RunE, "%s must define RunE", c.Name())
	}
}
