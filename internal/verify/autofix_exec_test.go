package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
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

// TestResolveAutoFixSandbox_TimeoutOverrideReachesBackend closes AC 02-01 Edge
// Case 2: sandbox.timeout_secs is applied to the backend as a context deadline,
// not a `docker run` argv flag, so the field-override argv assertions above can
// never cover it. Assert the resolver copied the override into the backend's
// per-run budget directly (the only place it is observable).
func TestResolveAutoFixSandbox_TimeoutOverrideReachesBackend(t *testing.T) {
	dockerPath, _ := fakeDockerRecording(t)
	secs := 120
	sc := &registry.SandboxConfig{
		Backend:     "docker",
		DockerPath:  dockerPath,
		Image:       "alpine:3.20",
		TimeoutSecs: &secs,
		TestCommand: []string{"go", "test", "./..."},
	}
	b, err := ResolveAutoFixSandbox(context.Background(), true, sc)
	require.NoError(t, err)
	require.NotNil(t, b)

	db, ok := b.(*sandbox.DockerBackend)
	require.True(t, ok, "the resolver builds a *sandbox.DockerBackend")
	assert.Equal(t, 120*time.Second, db.Timeout(), "TimeoutSecs override must reach the backend's per-run budget")
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

func TestResolveAutoFixSandbox_DisabledIsNoOp(t *testing.T) {
	// enabled=false is the shape Story 3's --no-sandbox produces: a clean no-op
	// symmetric with ResolveExecBackend's disabled path — (nil, nil), with zero
	// Docker config construction and no Preflight subprocess spawned.
	dockerPath, capture := fakeDockerRecording(t)
	cases := []struct {
		name string
		sc   *registry.SandboxConfig
	}{
		{"nil config", nil},
		{"valid config still skipped", &registry.SandboxConfig{
			Backend: "docker", DockerPath: dockerPath, Image: "alpine:3.20",
			TestCommand: []string{"go", "test", "./..."},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := ResolveAutoFixSandbox(context.Background(), false, tc.sc)
			require.NoError(t, err, "disabled path must never error")
			assert.Nil(t, b, "disabled path must return a nil backend")
			// Assert per-case that the (config-supplied) docker shim was never
			// invoked, so a fall-through in either case is attributable here.
			_, statErr := os.Stat(capture)
			assert.True(t, os.IsNotExist(statErr), "disabled path must spawn no docker subprocess")
		})
	}
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

// --- OS-level fallback, mirroring ResolveExecBackend's branch ---------------
//
// These reuse exec_test.go's stubOSLevel/fallbackSandboxConfig helpers on
// purpose. One seam and one fake, shared by both resolvers, is the mechanical
// enforcement of the story's cross-resolver consistency requirement: a second
// seam declared here is exactly how the two branches would drift apart.

func TestResolveAutoFixSandbox_FallbackEngagesOnDockerPreflightFailure(t *testing.T) {
	fake, gotCfg, calls := stubOSLevel(t, nil)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)
	sc.Image = "alpine:3.20"
	secs := 90
	sc.TimeoutSecs = &secs

	b, err := ResolveAutoFixSandbox(context.Background(), true, sc)

	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "os-level", b.Name(),
		"the stable backend identifier, never the underlying binary's name")
	assert.Same(t, sandbox.Backend(fake), b)
	assert.Equal(t, 90*time.Second, gotCfg.Timeout,
		"the operator's sandbox.timeout_secs must reach the fallback backend")
	assert.Equal(t, 1, *calls)
	assert.Equal(t, 1, fake.preflights,
		"the fallback backend must be preflighted before it is handed back")
}

func TestResolveAutoFixSandbox_FallbackNeverEngagesWhenDockerSucceeds(t *testing.T) {
	_, _, calls := stubOSLevel(t, nil)
	dockerPath, _ := fakeDockerRecording(t)
	sc := &registry.SandboxConfig{
		Backend:     "docker",
		DockerPath:  dockerPath,
		Image:       "alpine:3.20",
		TestCommand: []string{"go", "test", "./..."},
		Fallback:    registry.SandboxFallbackOSLevel,
	}

	b, err := ResolveAutoFixSandbox(context.Background(), true, sc)

	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "docker", b.Name())
	assert.Equal(t, 0, *calls,
		"configuring a fallback must have zero effect where Docker works")
}

func TestResolveAutoFixSandbox_UnsupportedFallbackValueIsNotAFallback(t *testing.T) {
	for _, v := range []string{"always", "docker", "OS-Level", "oslevel", "   "} {
		t.Run(v, func(t *testing.T) {
			_, _, calls := stubOSLevel(t, nil)
			sc := fallbackSandboxConfig(t, v)
			sc.Image = "alpine:3.20"

			b, err := ResolveAutoFixSandbox(context.Background(), true, sc)

			require.Error(t, err)
			assert.Nil(t, b)
			assert.Equal(t, 0, *calls, "only the exact sentinel may engage the fallback")
		})
	}
}

func TestResolveAutoFixSandbox_FallbackPreflightFailureRefusesWithoutPartialSuccess(t *testing.T) {
	osCause := errors.New("sandbox-exec: not found")
	fake, _, calls := stubOSLevel(t, osCause)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)
	sc.Image = "alpine:3.20"

	b, err := ResolveAutoFixSandbox(context.Background(), true, sc)

	require.Error(t, err)
	assert.Nil(t, b, "never a non-nil backend alongside a non-nil error")
	assert.Equal(t, 1, *calls)
	assert.Equal(t, 1, fake.preflights)
	assert.ErrorIs(t, err, ErrSandboxNoUsableBackend,
		"the SAME sentinel as ResolveExecBackend — never a second one")
	assert.ErrorIs(t, err, osCause)
	assert.Contains(t, err.Error(), "docker")
	assert.Contains(t, err.Error(), "os-level")
}

func TestResolveAutoFixSandbox_NoFallbackRefusalIsNotSentineled(t *testing.T) {
	sc := fallbackSandboxConfig(t, "")
	sc.Image = "alpine:3.20"
	_, err := ResolveAutoFixSandbox(context.Background(), true, sc)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrSandboxNoUsableBackend,
		"the untouched refusal must stay distinguishable from the combined-failure one")
}

// TestResolveAutoFixSandbox_FallbackIrrelevantWhenDisabled pins that the two
// bypasses stay separate: --no-sandbox (enabled == false) accepts unsandboxed
// host execution, sandbox.fallback substitutes a still-contained backend.
// Neither may imply the other, so a configured fallback must not resurrect a
// backend on the path where the operator explicitly opted out of sandboxing.
func TestResolveAutoFixSandbox_FallbackIrrelevantWhenDisabled(t *testing.T) {
	_, _, calls := stubOSLevel(t, nil)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)

	b, err := ResolveAutoFixSandbox(context.Background(), false, sc)

	require.NoError(t, err)
	assert.Nil(t, b)
	assert.Equal(t, 0, *calls)
}

func TestResolveAutoFixSandbox_CanceledContextDoesNotAttemptFallback(t *testing.T) {
	_, _, calls := stubOSLevel(t, nil)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)
	sc.Image = "alpine:3.20"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b, err := ResolveAutoFixSandbox(ctx, true, sc)

	require.Error(t, err)
	assert.Nil(t, b)
	assert.Equal(t, 0, *calls)
	assert.NotErrorIs(t, err, ErrSandboxNoUsableBackend)
}
