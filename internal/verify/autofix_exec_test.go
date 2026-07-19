package verify

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDockerRecording returns a POSIX docker shim that appends every invocation's
// argv (space-joined) to a capture file, and the capture file's path. The `info`
// subcommand reports a generous host so validateHostCaps passes; every other
// invocation succeeds. Tests read the capture file to assert which `docker run`
// resource-cap flags the resolver's Preflight built from the SandboxConfig.
func fakeDockerRecording(t *testing.T) (dockerPath, captureFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-docker shim is POSIX-only")
	}
	dir := t.TempDir()
	captureFile = filepath.Join(dir, "docker-args.log")
	body := `echo "$@" >> "` + captureFile + `"
if [ "$1" = "info" ]; then
  echo '{"MemTotal": 8589934592, "NCPU": 8}'
  exit 0
fi
exit 0`
	p := filepath.Join(dir, "docker")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755))
	return p, captureFile
}

// runArgsLine returns the recorded `docker run ...` invocation line (the trivial
// preflight container), or fails the test if none was captured.
func runArgsLine(t *testing.T, captureFile string) string {
	t.Helper()
	data, err := os.ReadFile(captureFile)
	require.NoError(t, err, "docker shim should have recorded at least one invocation")
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "run ") {
			return line
		}
	}
	t.Fatalf("no `docker run` invocation was recorded; captured:\n%s", data)
	return ""
}

func TestResolveAutoFixSandbox_BuildsAndPreflights(t *testing.T) {
	dockerPath, _ := fakeDockerRecording(t)
	sc := &registry.SandboxConfig{
		Backend:     "docker",
		DockerPath:  dockerPath,
		Image:       "alpine:3.20",
		TestCommand: []string{"go", "test", "./..."},
	}
	b, err := ResolveAutoFixSandbox(context.Background(), true, sc)
	require.NoError(t, err)
	require.NotNil(t, b, "a passing preflight must return a ready backend")
	assert.Equal(t, "docker", b.Name())
}

func TestResolveAutoFixSandbox_FullFieldOverrideAppliedBeforePreflight(t *testing.T) {
	dockerPath, capture := fakeDockerRecording(t)
	pids := 128
	secs := 120
	sc := &registry.SandboxConfig{
		Backend:     "docker",
		DockerPath:  dockerPath,
		Image:       "custom-image:9",
		Memory:      "256m",
		CPUs:        "0.5",
		PidsLimit:   &pids,
		TimeoutSecs: &secs,
		TestCommand: []string{"go", "test", "./..."},
	}
	b, err := ResolveAutoFixSandbox(context.Background(), true, sc)
	require.NoError(t, err)
	require.NotNil(t, b)

	run := runArgsLine(t, capture)
	assert.Contains(t, run, "--memory 256m", "Memory override must reach docker run")
	assert.Contains(t, run, "--cpus 0.5", "CPUs override must reach docker run")
	assert.Contains(t, run, "--pids-limit 128", "PidsLimit override must reach docker run")
	assert.Contains(t, run, "custom-image:9", "Image override must reach docker run")
}

func TestResolveAutoFixSandbox_PartialConfigInheritsHardenedDefaults(t *testing.T) {
	dockerPath, capture := fakeDockerRecording(t)
	// Only Image + TestCommand set; Memory/CPUs/PidsLimit/TimeoutSecs left at zero
	// value so DefaultDockerConfig()'s hardened values must survive.
	sc := &registry.SandboxConfig{
		DockerPath:  dockerPath,
		Image:       "alpine:3.20",
		TestCommand: []string{"go", "test", "./..."},
	}
	b, err := ResolveAutoFixSandbox(context.Background(), true, sc)
	require.NoError(t, err)
	require.NotNil(t, b)

	run := runArgsLine(t, capture)
	assert.Contains(t, run, "--memory 512m", "unset Memory must inherit the hardened default")
	assert.Contains(t, run, "--cpus 1.0", "unset CPUs must inherit the hardened default")
	assert.Contains(t, run, "--pids-limit 256", "unset PidsLimit must inherit the hardened default")
}

func TestResolveAutoFixSandbox_PreflightFailureRefuses(t *testing.T) {
	// A docker shim that exits non-zero on every call (daemon unreachable) must
	// make the resolver refuse: nil backend, wrapped error naming "preflight".
	sc := &registry.SandboxConfig{
		DockerPath:  fakeDocker(t, "exit 1"),
		Image:       "alpine:3.20",
		TestCommand: []string{"go", "test"},
	}
	b, err := ResolveAutoFixSandbox(context.Background(), true, sc)
	require.Error(t, err, "a failed preflight must refuse the run")
	assert.Nil(t, b, "no backend may be returned when preflight fails")
	assert.Contains(t, err.Error(), "preflight")
}

func TestResolveAutoFixSandbox_RefusesWhenUnconfigured(t *testing.T) {
	// Inverted posture: the auto-fix default is enabled=true, so a missing
	// [sandbox] block (sc == nil) is a hard refusal, never a silent skip.
	b, err := ResolveAutoFixSandbox(context.Background(), true, nil)
	require.Error(t, err, "sandboxed-by-default must refuse when no sandbox is configured")
	assert.Nil(t, b)
	// The sentinel's purpose is errors.Is-distinguishability from ErrExecNoBackend
	// (the two features have opposite default polarities), so pin that explicitly.
	assert.ErrorIs(t, err, ErrAutoFixSandboxUnconfigured)
	assert.NotErrorIs(t, err, ErrExecNoBackend, "auto-fix's unconfigured sentinel must be distinct from --exec's")
}
