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
	"os"
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

func TestRunSandboxedValidation_TimeoutIsNotCannotValidateBranch(t *testing.T) {
	// A sandbox timeout is a completed hard failure, NOT a backend fault: the
	// dispatch fn must return (res, nil) with StartError nil and Passed() false,
	// so the call site takes its !res.Passed() branch, never verr != nil.
	fb := &fakeSandboxBackend{name: "fake", result: sandbox.RunResult{ExitCode: 124, Output: "deadline", TimedOut: true}}
	res, err := RunSandboxedValidation(context.Background(), fb, []string{"go", "build"}, t.TempDir(), 5*time.Second)
	require.NoError(t, err, "a timeout is not a Go error — it is a completed hard failure")
	assert.Nil(t, res.StartError, "a timeout must not be mislabeled as a cannot-validate start error")
	assert.True(t, res.TimedOut)
	assert.Zero(t, res.ExitCode, "conventional timeout code 124 must not surface as a competing exit")
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
			// Not just 124: any non-zero exit reported alongside TimedOut (e.g. 137
			// SIGKILL/OOM, 143 SIGTERM) must be suppressed, proving the rule is
			// "TimedOut suppresses every ExitCode", not "special-case 124".
			name:       "timed out with 137 (SIGKILL) still suppresses the exit code",
			rr:         sandbox.RunResult{ExitCode: 137, Output: "killed", TimedOut: true},
			wantExit:   0,
			wantStdout: "killed",
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
			assert.Zero(t, res.Duration, "the pure mapping never sets Duration — the dispatch adapter measures it")
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

// --- AC 01-01: empty/relative dir delegation fails closed via the backend ---

// TestRunSandboxedValidation_EmptyDirFailsClosedThroughRealBackend drives the
// empty-dir delegation end-to-end through a real DockerBackend backed by the
// fake-docker recording shim: the adapter deliberately does NOT stat-guard an
// empty dir (see the dir != "" branch comment), so rejection must come from the
// backend's RunSpec.validate() — surfacing as StartError + !Passed() + a non-nil
// returned error BEFORE any container spawn (the shim records every invocation,
// so a missing capture file proves no docker subprocess ran).
func TestRunSandboxedValidation_EmptyDirFailsClosedThroughRealBackend(t *testing.T) {
	dockerPath, capture := fakeDockerRecording(t)
	cfg := sandbox.DefaultDockerConfig()
	cfg.DockerPath = dockerPath
	backend := sandbox.NewDockerBackend(cfg)

	res, err := RunSandboxedValidation(context.Background(), backend, []string{"go", "build"}, "", 5*time.Second)
	require.Error(t, err, "an empty dir must fail closed via the backend's RunSpec.validate()")
	assert.NotNil(t, res.StartError, "empty SnapshotDir rejection maps to StartError, the cannot-validate branch")
	assert.False(t, res.Passed())
	_, statErr := os.Stat(capture)
	assert.True(t, os.IsNotExist(statErr), "no docker subprocess may spawn for a spec RunSpec.validate() rejects")
}

// TestRunSandboxedValidation_RelativeDirFailsClosedThroughRealBackend drives a
// relative dir that EXISTS past the adapter's os.Stat guard, so rejection must
// again come from the backend's RunSpec.validate() absolute-path requirement —
// the same StartError + !Passed() fail-closed outcome, again with zero spawns.
func TestRunSandboxedValidation_RelativeDirFailsClosedThroughRealBackend(t *testing.T) {
	dockerPath, capture := fakeDockerRecording(t)
	cfg := sandbox.DefaultDockerConfig()
	cfg.DockerPath = dockerPath
	backend := sandbox.NewDockerBackend(cfg)

	// Chdir into a scratch tree so "relsnap" is a relative path that exists and
	// therefore passes the adapter's stat guard, isolating the backend's check.
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir("relsnap", 0o755))

	res, err := RunSandboxedValidation(context.Background(), backend, []string{"go", "build"}, "relsnap", 5*time.Second)
	require.Error(t, err, "a relative dir must fail closed via the backend's RunSpec.validate()")
	assert.NotNil(t, res.StartError, "relative SnapshotDir rejection maps to StartError, the cannot-validate branch")
	assert.False(t, res.Passed())
	_, statErr := os.Stat(capture)
	assert.True(t, os.IsNotExist(statErr), "no docker subprocess may spawn for a spec RunSpec.validate() rejects")
}
