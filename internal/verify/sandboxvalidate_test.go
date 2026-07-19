package verify

// Tests for Story 1 (Sprint 32.0) — routing the --auto-fix post-apply validation
// command through internal/sandbox instead of the host os/exec path.
//
//   - AC 01-01 (dispatch): a supplied sandbox.Backend receives a RunSpec built from
//     the caller's argv/dir/timeout, exactly once, with the pre-apply guards
//     (empty argv, missing dir) still short-circuiting before any Backend.Run.
//   - AC 01-02 (translation): the pure sandbox.RunResult -> ValidationResult mapping
//     honours the translation-gap table (combined Output -> Stdout only, TimedOut
//     direct without leaking exit 124, backend fault -> StartError, truncation
//     flags left false).
//
// Unlike localvalidate_test.go (real short-lived commands on the host path), these
// use a Go-level fake sandbox.Backend that records the RunSpec it received and
// returns a canned RunResult/error — no Docker, no host process spawned.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSandboxBackend records the RunSpec it received and returns a caller-configured
// RunResult/error, so a test can assert which spec the dispatch adapter built and
// how many times it dispatched — without ever spawning a container.
type fakeSandboxBackend struct {
	name    string
	preErr  error
	result  sandbox.RunResult
	runErr  error
	delay   time.Duration
	calls   int
	gotSpec sandbox.RunSpec
}

func (f *fakeSandboxBackend) Name() string { return f.name }

func (f *fakeSandboxBackend) Preflight(context.Context) error { return f.preErr }

func (f *fakeSandboxBackend) Run(_ context.Context, spec sandbox.RunSpec) (sandbox.RunResult, error) {
	f.calls++
	f.gotSpec = spec
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	return f.result, f.runErr
}

// --- AC 01-01: sandbox-routed command dispatch ---------------------------

func TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec(t *testing.T) {
	dir := t.TempDir()
	fb := &fakeSandboxBackend{name: "fake", result: sandbox.RunResult{ExitCode: 0, Output: "build ok"}}
	argv := []string{"go", "build", "./..."}
	timeout := 90 * time.Second

	res, err := RunSandboxedValidation(context.Background(), fb, argv, dir, timeout)
	require.NoError(t, err)

	assert.Equal(t, 1, fb.calls, "Backend.Run must be called exactly once")
	assert.Equal(t, argv, fb.gotSpec.Command, "argv must be forwarded into RunSpec.Command")
	assert.Equal(t, dir, fb.gotSpec.SnapshotDir, "dir must be forwarded byte-for-byte into RunSpec.SnapshotDir")
	assert.Equal(t, timeout, fb.gotSpec.Timeout, "timeout must be forwarded exactly, not silently defaulted here")
	assert.Empty(t, fb.gotSpec.Script, "Script must never be populated on the argv path (RunSpec exactly-one invariant)")
	assert.True(t, res.Passed(), "exit 0 with no timeout and no fault must pass")
	assert.Equal(t, "build ok", res.Stdout, "combined RunResult.Output routes into Stdout")
	assert.Empty(t, res.Stderr, "Stderr is left empty on the sandbox path (documented stream-collapse)")
	assert.False(t, res.StdoutTruncated, "sandbox reports no per-stream truncation signal — flag left false, not guessed")
	assert.False(t, res.StderrTruncated, "sandbox reports no per-stream truncation signal — flag left false, not guessed")
}

func TestRunSandboxedValidation_EmptyArgvShortCircuitsBeforeBackend(t *testing.T) {
	fb := &fakeSandboxBackend{name: "fake"}
	res, err := RunSandboxedValidation(context.Background(), fb, nil, t.TempDir(), 5*time.Second)
	require.Error(t, err, "empty argv must not attempt to dispatch")
	assert.Equal(t, 0, fb.calls, "Backend.Run must never be called for empty argv")
	assert.NotNil(t, res.StartError)
	assert.False(t, res.Passed())
}

func TestRunSandboxedValidation_MissingDirShortCircuitsBeforeBackend(t *testing.T) {
	fb := &fakeSandboxBackend{name: "fake"}
	res, err := RunSandboxedValidation(context.Background(), fb, []string{"go", "build"}, "/nonexistent/dir/atcr-sbx", 5*time.Second)
	require.Error(t, err, "a missing working directory must short-circuit before any dispatch")
	assert.Equal(t, 0, fb.calls, "Backend.Run must never be called when dir is missing")
	assert.NotNil(t, res.StartError)
	assert.Contains(t, res.StartError.Error(), "validation working directory does not exist", "error must name the directory, matching the host path")
	assert.False(t, res.Passed())
}

func TestRunSandboxedValidation_MeasuresDurationAroundRun(t *testing.T) {
	fb := &fakeSandboxBackend{name: "fake", result: sandbox.RunResult{ExitCode: 0}, delay: 5 * time.Millisecond}
	res, err := RunSandboxedValidation(context.Background(), fb, []string{"true"}, t.TempDir(), 5*time.Second)
	require.NoError(t, err)
	assert.Positive(t, res.Duration, "adapter must measure wall-clock Duration around Backend.Run (never left zero — AC 01-02 EC4)")
}

func TestRunSandboxedValidation_BackendFaultIsCannotValidateBranch(t *testing.T) {
	fb := &fakeSandboxBackend{name: "fake", runErr: errors.New("docker daemon unreachable")}
	res, err := RunSandboxedValidation(context.Background(), fb, []string{"go", "build"}, t.TempDir(), 5*time.Second)
	require.Error(t, err, "a backend fault must surface as a non-nil returned error (the cannot-validate branch)")
	assert.NotNil(t, res.StartError, "a backend fault maps to StartError, not a fabricated non-zero exit")
	assert.False(t, res.Passed())
}

// --- AC 01-02: RunResult -> ValidationResult translation ------------------

// TestTranslateRunResult pins the pure struct-to-struct mapping per the
// translation-gap table: combined Output -> Stdout only, TimedOut direct with the
// conventional exit 124 NOT surfaced as a competing program failure, a Go error
// from Run -> StartError (the "cannot validate" branch, never a fabricated exit),
// and the truncation flags left false since the sandbox carries no per-stream
// signal. Duration is deliberately not set here — the dispatch adapter measures it
// around Backend.Run (covered by TestRunSandboxedValidation_MeasuresDurationAroundRun).
func TestTranslateRunResult(t *testing.T) {
	backendFault := errors.New("docker run: runtime error (exit 125)")
	cases := []struct {
		name       string
		rr         sandbox.RunResult
		runErr     error
		wantExit   int
		wantStdout string
		wantTimed  bool
		wantStart  bool
		wantPassed bool
	}{
		{
			name:       "clean pass exit 0",
			rr:         sandbox.RunResult{ExitCode: 0, Output: "build ok"},
			wantExit:   0,
			wantStdout: "build ok",
			wantPassed: true,
		},
		{
			name:       "ran and failed exit 1 is not a Go error",
			rr:         sandbox.RunResult{ExitCode: 1, Output: "build failed: pkg/x.go:3"},
			wantExit:   1,
			wantStdout: "build failed: pkg/x.go:3",
			wantPassed: false,
		},
		{
			name:       "timed out with conventional 124 does not surface a competing exit",
			rr:         sandbox.RunResult{ExitCode: 124, Output: "deadline", TimedOut: true},
			wantExit:   0,
			wantStdout: "deadline",
			wantTimed:  true,
			wantPassed: false,
		},
		{
			name:       "backend fault with zero result maps to StartError",
			rr:         sandbox.RunResult{},
			runErr:     backendFault,
			wantExit:   0,
			wantStart:  true,
			wantPassed: false,
		},
		{
			name:       "backend fault with partial result maps to StartError and keeps partial output",
			rr:         sandbox.RunResult{ExitCode: 125, Output: "partial before fault"},
			runErr:     backendFault,
			wantExit:   0,
			wantStdout: "partial before fault",
			wantStart:  true,
			wantPassed: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := translateRunResult(tc.rr, tc.runErr)
			assert.Equal(t, tc.wantExit, res.ExitCode)
			assert.Equal(t, tc.wantStdout, res.Stdout, "combined Output routes into Stdout")
			assert.Empty(t, res.Stderr, "sandbox path never populates Stderr (documented stream-collapse)")
			assert.Equal(t, tc.wantTimed, res.TimedOut)
			assert.False(t, res.StdoutTruncated, "no per-stream truncation signal — left false")
			assert.False(t, res.StderrTruncated, "no per-stream truncation signal — left false")
			if tc.wantStart {
				assert.Error(t, res.StartError, "a backend fault must surface as StartError, not a program exit")
			} else {
				assert.NoError(t, res.StartError)
			}
			assert.Equal(t, tc.wantPassed, res.Passed())
		})
	}
}
