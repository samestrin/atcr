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
	// unwrapBackend: see the --exec sibling; the backend is decorated with the
	// deferred fallback-engaged notice.
	assert.Same(t, sandbox.Backend(fake), unwrapBackend(b))
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

// TestResolveAutoFixSandbox_InterruptDuringFallbackPreflightIsNotSentineled
// mirrors TestResolveExecBackend_InterruptDuringFallbackPreflightIsNotSentineled
// on the auto-fix resolver: the cross-resolver consistency requirement covers
// the interrupt path too, not just the happy-path branch shape.
func TestResolveAutoFixSandbox_InterruptDuringFallbackPreflightIsNotSentineled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	fake, _, calls := stubOSLevel(t, context.Canceled)
	fake.cancel = cancel // the SIGINT lands while the OS-level preflight runs
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)
	sc.Image = "alpine:3.20"

	b, err := ResolveAutoFixSandbox(ctx, true, sc)

	require.Error(t, err)
	assert.Nil(t, b)
	assert.Equal(t, 1, *calls)
	assert.Equal(t, 1, fake.preflights)
	assert.NotErrorIs(t, err, ErrSandboxNoUsableBackend,
		"a ctrl-C during the OS-level preflight is an interrupt, not a broken sandbox config")
	assert.ErrorIs(t, err, context.Canceled,
		"the cancellation must stay reachable through the chain")
}

// --- TD-019: os-level snapshot pre-check ------------------------------------
//
// Preflight probes against its OWN temp snapshot, but RunSandboxedValidation
// passes the resolved applyTarget — the repository root. The generators apply
// their path guards to that value at Run time, so a repo checked out at $HOME,
// or at a path carrying a profile metacharacter, is refused only on the real
// run: after the fallback was selected AND after --auto-fix already applied its
// patch, surfacing as the opaque "auto-fix sandbox validation could not run".
//
// The pre-check is a SIBLING of ResolveAutoFixSandbox, never a new parameter on
// it: the three pinned regression tests call that resolver in its two-argument
// shape, and a signature change would force edits to all three.

func TestCheckOSLevelSnapshotUsable_RefusesAHomeDirectorySnapshot(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	fake := &fakeOSLevel{}

	err = CheckOSLevelSnapshotUsable(fake, nil, home, true)

	require.Error(t, err, "a repo checked out at $HOME must be refused BEFORE a patch is applied")
	assert.Contains(t, err.Error(), "os-level")
}

func TestCheckOSLevelSnapshotUsable_AcceptsAnOrdinaryRepoPath(t *testing.T) {
	assert.NoError(t, CheckOSLevelSnapshotUsable(&fakeOSLevel{}, nil, t.TempDir(), true))
}

func TestCheckOSLevelSnapshotUsable_IsANoOpForOtherBackends(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	// A nil backend (the --no-sandbox shape) and a docker backend must both be
	// untouched by this check — it exists only to pre-validate what the os-level
	// generators will later be handed.
	assert.NoError(t, CheckOSLevelSnapshotUsable(nil, nil, home, true))
	assert.NoError(t, CheckOSLevelSnapshotUsable(sandbox.NewDockerBackend(sandbox.DefaultDockerConfig()), nil, home, true))
}

// namedBackend is a minimal sandbox.Backend whose Name is whatever the test
// says — the shape a decorating wrapper (auditing, metrics, retrying) takes
// when it reports its own name instead of the wrapped backend's.
type namedBackend struct{ name string }

func (n namedBackend) Name() string { return n.name }

func (n namedBackend) Preflight(context.Context) error { return nil }

func (n namedBackend) Run(context.Context, sandbox.RunSpec) (sandbox.RunResult, error) {
	return sandbox.RunResult{}, errors.New("namedBackend does not execute")
}

// TestCheckOSLevelSnapshotUsable_RefusesAnUnrecognizedBackend pins the
// fail-closed default: a backend the gate cannot positively identify must be
// refused, not silently passed. Dispatching on Name() != "os-level" alone is
// fail-OPEN — any decorator around the OS-level backend reports its own name
// and the snapshot check becomes a no-op with zero test failures.
func TestCheckOSLevelSnapshotUsable_RefusesAnUnrecognizedBackend(t *testing.T) {
	err := CheckOSLevelSnapshotUsable(namedBackend{name: "auditing-decorator"}, nil, t.TempDir(), true)
	require.Error(t, err, "an unrecognized backend must be refused, not silently passed")
	assert.Contains(t, err.Error(), "auditing-decorator",
		"the refusal must name the backend it could not identify")
}

func TestCheckOSLevelSnapshotUsable_RefusesAProfileMetacharacterPath(t *testing.T) {
	// `(`/`)` are metacharacters ONLY in sandbox-exec's interpolated profile
	// DSL (profileSafePath, oslevel_profile.go) — bwrap's argv is discrete
	// elements passed straight to execve, so bwrapArgs has no equivalent
	// rejection and correctly accepts this path on Linux. This exported check
	// dispatches on the real runtime.GOOS (sandbox.CheckSnapshotUsable), not
	// an injectable seam, so the property under test is darwin-only; it went
	// unnoticed on Linux only because CI never ran the integration tag there
	// before this sprint's containment-proof job.
	if runtime.GOOS != "darwin" {
		t.Skip("profile-DSL metacharacter rejection is darwin-specific (sandbox-exec only); bwrap's discrete argv has no equivalent")
	}

	dir := filepath.Join(t.TempDir(), "repo(1)")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	err := CheckOSLevelSnapshotUsable(&fakeOSLevel{}, nil, dir, true)

	require.Error(t, err, "a path the generator would reject must be caught up front")
}

// TestCheckOSLevelSnapshotUsable_BothShapesAreCheckedFaithfully pins the
// `writable` argument and the fix behind it.
//
// The Phase 4 gate found two related defects: flipping the argument left the
// whole suite green, AND the read-only shape was systematically more permissive
// than the run it predicts, because the dry-build created a scratch dir only
// when writable — skipping the generators' ScratchDir guards that a real
// read-only run does exercise. Measured then: os.TempDir() PASSED read-only and
// failed writable. A repo in that blind spot would clear the gate and then fault
// at Run, which is precisely what the pre-check exists to prevent.
//
// Both shapes now refuse it, for the same reason a real run would: the per-run
// scratch dir would land inside the snapshot.
//
// LIMIT OF THIS TEST, stated so it is not read as proving more than it does:
// with the read-only shape now faithful, no measured path produces DIFFERENT
// verdicts for the two values, so hardcoding the argument would not fail here.
// What pins the call sites' intent is the assertion at each of them —
// cli/verify_test.go requires --exec passes false, cli/autofix_test.go's gate
// test covers the --auto-fix side passing true. If a future generator change
// makes the shapes diverge, those call sites are already correct.
func TestCheckOSLevelSnapshotUsable_BothShapesAreCheckedFaithfully(t *testing.T) {
	fake := &fakeOSLevel{}

	for _, writable := range []bool{false, true} {
		err := CheckOSLevelSnapshotUsable(fake, nil, os.TempDir(), writable)
		require.Error(t, err, "writable=%v must refuse a snapshot that would contain the scratch dir", writable)
		assert.Contains(t, err.Error(), "ScratchDir")
	}

	// ...and an ordinary repo path is accepted in both shapes, so the check is
	// not simply refusing everything.
	ordinary := t.TempDir()
	assert.NoError(t, CheckOSLevelSnapshotUsable(fake, nil, ordinary, false))
	assert.NoError(t, CheckOSLevelSnapshotUsable(fake, nil, ordinary, true))
}

// TestCheckOSLevelSnapshotUsable_ForwardsWritableShape pins that the writable
// argument is passed through to sandbox.CheckSnapshotUsable rather than
// hardcoded. TestCheckOSLevelSnapshotUsable_BothShapesAreCheckedFaithfully
// exercises the real generator for both shapes, but only this test can detect
// the argument being silently replaced inside CheckOSLevelSnapshotUsable.
func TestCheckOSLevelSnapshotUsable_ForwardsWritableShape(t *testing.T) {
	fake := &fakeOSLevel{}
	ordinary := t.TempDir()

	var gotWritable bool
	orig := checkSnapshotUsableFn
	checkSnapshotUsableFn = func(cfg sandbox.OSLevelConfig, dir string, writable bool) error {
		gotWritable = writable
		return nil
	}
	t.Cleanup(func() { checkSnapshotUsableFn = orig })

	require.NoError(t, CheckOSLevelSnapshotUsable(fake, nil, ordinary, false))
	assert.False(t, gotWritable, "writable=false must be forwarded to CheckSnapshotUsable")

	require.NoError(t, CheckOSLevelSnapshotUsable(fake, nil, ordinary, true))
	assert.True(t, gotWritable, "writable=true must be forwarded to CheckSnapshotUsable")
}
