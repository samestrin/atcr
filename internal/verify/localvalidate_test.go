package verify

// RED tests for Story 2 (configurable local validation) — AC 02-01, 02-02, 02-03.
//
// The runner shells out via exec.CommandContext with a bounded timeout, captures
// exit code / stdout / stderr / duration into a ValidationResult, and exposes a
// conservative Passed() gate (exit 0 AND not timed out AND started cleanly). Tests
// use real short-lived commands (true/false/sleep/head/printf via sh), matching
// internal/verify/exec_test.go conventions — no exec mocking.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixtures (true/false/sh) are not portable to Windows")
	}
}

// --- AC 02-01: command runner --------------------------------------------

func TestRunConfiguredValidation_Success(t *testing.T) {
	skipOnWindows(t)
	res, err := RunConfiguredValidation(context.Background(), []string{"true"}, t.TempDir(), 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.True(t, res.Passed())
	assert.False(t, res.TimedOut)
	assert.Nil(t, res.StartError)
}

func TestRunConfiguredValidation_NonZeroExitIsFailureNotError(t *testing.T) {
	skipOnWindows(t)
	res, err := RunConfiguredValidation(context.Background(), []string{"false"}, t.TempDir(), 5*time.Second)
	require.NoError(t, err, "a completed run that exits non-zero is not a Go error")
	assert.NotEqual(t, 0, res.ExitCode)
	assert.False(t, res.Passed())
	assert.Nil(t, res.StartError)
}

func TestRunConfiguredValidation_TimeoutIsHardFailure(t *testing.T) {
	skipOnWindows(t)
	start := time.Now()
	res, err := RunConfiguredValidation(context.Background(), []string{"sleep", "10"}, t.TempDir(), 150*time.Millisecond)
	require.NoError(t, err)
	assert.True(t, res.TimedOut, "timeout must be a distinct, hard failure")
	assert.False(t, res.Passed())
	assert.Less(t, time.Since(start), 5*time.Second, "process must be killed at the deadline, not run to completion")
}

func TestRunConfiguredValidation_CommandNotFoundIsDistinct(t *testing.T) {
	res, err := RunConfiguredValidation(context.Background(), []string{"atcr-nonexistent-binary-zzz"}, t.TempDir(), 5*time.Second)
	require.Error(t, err, "a command that cannot start is a distinct error class, not a non-zero exit")
	assert.NotNil(t, res.StartError)
	assert.False(t, res.Passed())
	assert.False(t, res.TimedOut)
}

func TestRunConfiguredValidation_EmptyArgvRefused(t *testing.T) {
	res, err := RunConfiguredValidation(context.Background(), nil, t.TempDir(), 5*time.Second)
	require.Error(t, err, "empty argv must not attempt to execute")
	assert.False(t, res.Passed())
}

func TestRunConfiguredValidation_RunsInConfiguredDir(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("in-dir"), 0o644))
	res, err := RunConfiguredValidation(context.Background(), []string{"cat", "marker.txt"}, dir, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "in-dir", res.Stdout, "command must run with the configured working directory")
}

// --- AC 02-02: result capture --------------------------------------------

func TestRunConfiguredValidation_CapturesExitStdoutStderrDuration(t *testing.T) {
	skipOnWindows(t)
	res, err := RunConfiguredValidation(context.Background(),
		[]string{"sh", "-c", "printf out; printf err 1>&2; exit 3"}, t.TempDir(), 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 3, res.ExitCode)
	assert.Equal(t, "out", res.Stdout)
	assert.Equal(t, "err", res.Stderr)
	assert.Positive(t, res.Duration)
	assert.False(t, res.Passed())
}

func TestRunConfiguredValidation_EmptyOutputNotNilPanic(t *testing.T) {
	skipOnWindows(t)
	res, err := RunConfiguredValidation(context.Background(), []string{"true"}, t.TempDir(), 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "", res.Stdout)
	assert.Equal(t, "", res.Stderr)
	assert.True(t, res.Passed())
}

func TestRunConfiguredValidation_NonUTF8OutputDoesNotPanic(t *testing.T) {
	skipOnWindows(t)
	res, err := RunConfiguredValidation(context.Background(),
		[]string{"sh", "-c", `printf '\377\376'`}, t.TempDir(), 5*time.Second)
	require.NoError(t, err)
	assert.Len(t, res.Stdout, 2, "raw non-UTF8 bytes preserved as-is, no panic")
	assert.True(t, res.Passed())
}

func TestRunConfiguredValidation_HugeOutputTruncated(t *testing.T) {
	skipOnWindows(t)
	// 2 MB of zero bytes to stdout; the runner must bound its capture.
	res, err := RunConfiguredValidation(context.Background(),
		[]string{"head", "-c", "2000000", "/dev/zero"}, t.TempDir(), 10*time.Second)
	require.NoError(t, err)
	assert.True(t, res.StdoutTruncated, "pathological output must be truncated, not buffered unbounded")
	assert.Less(t, len(res.Stdout), 2000000, "captured output is capped below the emitted size")
	assert.True(t, res.Passed())
}

func TestRunConfiguredValidation_TimeoutPreservesPartialOutput(t *testing.T) {
	skipOnWindows(t)
	res, err := RunConfiguredValidation(context.Background(),
		[]string{"sh", "-c", "printf partial; sleep 10"}, t.TempDir(), 250*time.Millisecond)
	require.NoError(t, err)
	assert.True(t, res.TimedOut)
	assert.Contains(t, res.Stdout, "partial", "output captured before the deadline is retained")
}

func TestRunConfiguredValidation_NoFileMutation(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o644))
	before, err := os.ReadDir(dir)
	require.NoError(t, err)

	_, err = RunConfiguredValidation(context.Background(), []string{"true"}, dir, 5*time.Second)
	require.NoError(t, err)

	after, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, after, len(before), "validation must not create/modify/delete files")
}

// --- AC 02-03: conservative pass/fail gate -------------------------------

func TestValidationResult_Passed(t *testing.T) {
	cases := []struct {
		name string
		res  ValidationResult
		want bool
	}{
		{"exit 0", ValidationResult{ExitCode: 0}, true},
		{"exit 1", ValidationResult{ExitCode: 1}, false},
		{"exit 127", ValidationResult{ExitCode: 127}, false},
		{"timed out (exit 0)", ValidationResult{ExitCode: 0, TimedOut: true}, false},
		{"start error", ValidationResult{StartError: errors.New("nope")}, false},
		{"exit 0 with scary stderr", ValidationResult{ExitCode: 0, Stderr: "error: fail failed ERROR"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.res.Passed())
		})
	}
}

// --- AC 02-01: command resolution (Go convenience default / hard refusal) --

func TestResolveValidateCommand(t *testing.T) {
	goRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(goRoot, "go.mod"), []byte("module x\n"), 0o644))
	nonGoRoot := t.TempDir()

	t.Run("configured command wins", func(t *testing.T) {
		got, err := ResolveValidateCommand([]string{"make", "check"}, nonGoRoot)
		require.NoError(t, err)
		assert.Equal(t, []string{"make", "check"}, got)
	})

	t.Run("empty falls back to go default when go.mod present", func(t *testing.T) {
		got, err := ResolveValidateCommand(nil, goRoot)
		require.NoError(t, err)
		assert.Equal(t, []string{"go", "build", "./..."}, got)
	})

	t.Run("empty list falls back to go default when go.mod present", func(t *testing.T) {
		got, err := ResolveValidateCommand([]string{}, goRoot)
		require.NoError(t, err)
		assert.Equal(t, []string{"go", "build", "./..."}, got)
	})

	t.Run("no command and no go.mod is a hard refusal", func(t *testing.T) {
		_, err := ResolveValidateCommand(nil, nonGoRoot)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "validate_command")
	})
}
