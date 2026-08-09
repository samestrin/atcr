package verify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeDocker(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-docker shim is POSIX-only")
	}
	p := filepath.Join(t.TempDir(), "docker")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755))
	return p
}

func TestResolveExecBackend_DisabledIsNoOp(t *testing.T) {
	b, cmd, _, err := ResolveExecBackend(context.Background(), false, &registry.SandboxConfig{TestCommand: []string{"go", "test"}})
	require.NoError(t, err)
	assert.Nil(t, b, "execution off must return a nil backend")
	assert.Nil(t, cmd)
}

func TestResolveExecBackend_RefusesWithoutBackend(t *testing.T) {
	// SC-1: --exec with no sandbox block hard-errors without executing anything.
	_, _, _, err := ResolveExecBackend(context.Background(), true, nil)
	require.ErrorIs(t, err, ErrExecNoBackend)
}

func TestResolveExecBackend_BuildsAndPreflights(t *testing.T) {
	sc := &registry.SandboxConfig{
		Backend: "docker",
		// version/inspect/run succeed; info reports a generous host so the cap-fit
		// check passes.
		DockerPath: fakeDocker(t, `if [ "$1" = "info" ]; then
  echo '{"MemTotal": 8589934592, "NCPU": 8}'
  exit 0
fi
exit 0`),
		TestCommand: []string{"go", "test", "./..."},
	}
	b, cmd, _, err := ResolveExecBackend(context.Background(), true, sc)
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "docker", b.Name())
	assert.Equal(t, []string{"go", "test", "./..."}, cmd)
}

func TestResolveExecBackend_PreflightFailureRefuses(t *testing.T) {
	sc := &registry.SandboxConfig{
		DockerPath:  fakeDocker(t, "exit 1"), // daemon unreachable
		TestCommand: []string{"go", "test"},
	}
	_, _, _, err := ResolveExecBackend(context.Background(), true, sc)
	require.Error(t, err, "a failed preflight must refuse the run")
	assert.Contains(t, err.Error(), "preflight")
}

// TestResolveExecBackend_FullFieldOverrideAppliedBeforePreflight mirrors
// TestResolveAutoFixSandbox_FullFieldOverrideAppliedBeforePreflight on the
// --exec resolver: the two resolvers duplicate the DockerPath/Image/Memory/
// CPUs/PidsLimit wiring byte-for-byte, and only the auto-fix half was pinned —
// deleting a cap line from ResolveExecBackend previously left the suite green
// while running untrusted code at default caps. fakeDockerRecording/
// runArgsLine need no hoisting: both test files are package verify, so the
// shim is already shared.
func TestResolveExecBackend_FullFieldOverrideAppliedBeforePreflight(t *testing.T) {
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
	b, _, timeout, err := ResolveExecBackend(context.Background(), true, sc)
	require.NoError(t, err)
	require.NotNil(t, b)

	run := runArgsLine(t, capture)
	assert.Contains(t, run, "--memory 256m", "Memory override must reach docker run")
	assert.Contains(t, run, "--cpus 0.5", "CPUs override must reach docker run")
	assert.Contains(t, run, "--pids-limit 128", "PidsLimit override must reach docker run")
	assert.Contains(t, run, "custom-image:9", "Image override must reach docker run")
	assert.Equal(t, 120*time.Second, timeout, "TimeoutSecs override must reach the caller")
}

// --- OS-level fallback seam (shared by both resolvers' tests) ---------------

// fakeOSLevel is a sandbox.Backend standing in for the real OS-level backend.
// Substituting at the constructor seam rather than pointing the real backend at
// a shim binary is deliberate: it keeps these resolver tests independent of
// whether sandbox-exec/bwrap exists on the runner, and of internal/sandbox's
// binary-path config surface, which no resolver behavior depends on.
type fakeOSLevel struct {
	preflightErr error
	preflights   int
	// cancel, when set, fires inside Preflight to simulate a SIGINT landing
	// mid-preflight (cli/main.go cancels the root context on the first signal,
	// and the real OS-level preflight spawns sandbox-exec/bwrap, so the window
	// is real). Tests pair it with preflightErr=context.Canceled.
	cancel context.CancelFunc
}

func (f *fakeOSLevel) Name() string { return "os-level" }

func (f *fakeOSLevel) Preflight(context.Context) error {
	f.preflights++
	if f.cancel != nil {
		f.cancel()
	}
	return f.preflightErr
}

func (f *fakeOSLevel) Run(context.Context, sandbox.RunSpec) (sandbox.RunResult, error) {
	return sandbox.RunResult{}, errors.New("fakeOSLevel does not execute")
}

// stubOSLevel swaps the package's OS-level constructor seam for the duration of
// a test and reports what the resolver did with it: the fake it handed back, the
// config it was constructed with, and how many times it was called at all. The
// call count is load-bearing in the negative cases — "Docker succeeded, so the
// fallback was never attempted" is only provable by counting zero constructions,
// not by inspecting the returned backend.
//
// A test that calls this MUST NOT call t.Parallel(): the seam is a package-level
// variable written here and read inside the closure with no synchronization, so
// a parallel caller is a -race data race on the variable that decides which
// sandbox contains untrusted code, with fake leakage across tests as the
// failure mode. Serial is sufficient — these tests spawn one shim each.
func stubOSLevel(t *testing.T, preflightErr error) (*fakeOSLevel, *sandbox.OSLevelConfig, *int) {
	t.Helper()
	fake := &fakeOSLevel{preflightErr: preflightErr}
	var gotCfg sandbox.OSLevelConfig
	calls := 0
	prev := newOSLevelBackendFn
	newOSLevelBackendFn = func(cfg sandbox.OSLevelConfig) sandbox.Backend {
		calls++
		gotCfg = cfg
		return fake
	}
	t.Cleanup(func() { newOSLevelBackendFn = prev })
	return fake, &gotCfg, &calls
}

// fallbackSandboxConfig is an otherwise-valid config whose docker shim fails
// preflight — the only state in which the fallback is even consulted.
func fallbackSandboxConfig(t *testing.T, fallback string) *registry.SandboxConfig {
	t.Helper()
	return &registry.SandboxConfig{
		DockerPath:  fakeDocker(t, "exit 1"),
		Image:       "golang:1.25",
		TestCommand: []string{"go", "test", "./..."},
		Fallback:    fallback,
	}
}

func TestResolveExecBackend_FallbackEngagesOnDockerPreflightFailure(t *testing.T) {
	fake, gotCfg, calls := stubOSLevel(t, nil)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)
	secs := 45
	sc.TimeoutSecs = &secs

	b, cmd, timeout, err := ResolveExecBackend(context.Background(), true, sc)

	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "os-level", b.Name(),
		"the identifier is the stable backend name, never the underlying binary's")
	// unwrapBackend: the resolver now returns the fallback backend wrapped in a
	// pending-warning decorator, so identity is asserted through the wrapper.
	assert.Same(t, sandbox.Backend(fake), unwrapBackend(b))
	assert.Equal(t, []string{"go", "test", "./..."}, cmd)
	assert.Equal(t, 45*time.Second, timeout,
		"the operator's configured timeout must reach the caller on the fallback path too")
	assert.Equal(t, 45*time.Second, gotCfg.Timeout,
		"...and the backend it was constructed with")
	assert.Equal(t, 1, *calls)
	assert.Equal(t, 1, fake.preflights,
		"the fallback backend must be preflighted before it is handed back")
}

func TestResolveExecBackend_FallbackNeverEngagesWhenDockerSucceeds(t *testing.T) {
	_, _, calls := stubOSLevel(t, nil)
	sc := &registry.SandboxConfig{
		DockerPath: fakeDocker(t, `if [ "$1" = "info" ]; then
  echo '{"MemTotal": 8589934592, "NCPU": 8}'
  exit 0
fi
exit 0`),
		TestCommand: []string{"go", "test", "./..."},
		Fallback:    registry.SandboxFallbackOSLevel,
	}

	b, _, _, err := ResolveExecBackend(context.Background(), true, sc)

	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "docker", b.Name())
	assert.Equal(t, 0, *calls,
		"configuring a fallback must have zero effect on a host where Docker works")
}

func TestResolveExecBackend_FallbackNotConfiguredIsUntouched(t *testing.T) {
	// The pinned-regression path, asserted from the other side: with no fallback
	// configured the OS-level constructor must never be reached at all.
	_, _, calls := stubOSLevel(t, nil)
	sc := fallbackSandboxConfig(t, "")

	b, cmd, timeout, err := ResolveExecBackend(context.Background(), true, sc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "preflight")
	assert.Nil(t, b)
	assert.Nil(t, cmd)
	assert.Zero(t, timeout)
	assert.Equal(t, 0, *calls)
}

func TestResolveExecBackend_UnsupportedFallbackValueIsNotAFallback(t *testing.T) {
	// Defense in depth: a config that skipped Validate() must not be able to talk
	// the resolver into a fallback with a value the allowlist would have rejected.
	for _, v := range []string{"always", "docker", "OS-Level", "oslevel", "   "} {
		t.Run(v, func(t *testing.T) {
			_, _, calls := stubOSLevel(t, nil)
			sc := fallbackSandboxConfig(t, v)

			b, _, _, err := ResolveExecBackend(context.Background(), true, sc)

			require.Error(t, err)
			assert.Nil(t, b)
			assert.Equal(t, 0, *calls,
				"only the exact sentinel may engage the fallback")
		})
	}
}

func TestResolveExecBackend_FallbackPreflightFailureRefusesWithoutPartialSuccess(t *testing.T) {
	osCause := errors.New("bwrap: command not found")
	fake, _, calls := stubOSLevel(t, osCause)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)

	b, cmd, timeout, err := ResolveExecBackend(context.Background(), true, sc)

	require.Error(t, err)
	assert.Nil(t, b, "never a non-nil backend alongside a non-nil error")
	assert.Nil(t, cmd)
	assert.Zero(t, timeout)
	assert.Equal(t, 1, *calls)
	assert.Equal(t, 1, fake.preflights)
	assert.ErrorIs(t, err, ErrSandboxNoUsableBackend)
	assert.ErrorIs(t, err, osCause,
		"the os-level cause must stay reachable through the chain, not be flattened to text")
	assert.Contains(t, err.Error(), "docker")
	assert.Contains(t, err.Error(), "os-level")
}

// TestResolveExecBackend_NoFallbackRefusalIsNotSentineled is the other half of
// the byte-identical guarantee: the pinned test proves the message, this proves
// an errors.Is caller cannot mistake the untouched refusal for the new
// combined-failure one. Without it, a CLI branching on the sentinel could
// silently start treating every pre-story refusal as "a fallback was tried".
func TestResolveExecBackend_NoFallbackRefusalIsNotSentineled(t *testing.T) {
	sc := fallbackSandboxConfig(t, "")
	_, _, _, err := ResolveExecBackend(context.Background(), true, sc)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrSandboxNoUsableBackend)
}

// TestResolveExecBackend_CanceledContextDoesNotAttemptFallback pins that an
// interrupt is not read as evidence Docker is unusable. The real fallback
// attempt costs a temp dir and two spawns, and reporting a ctrl-C as "no usable
// sandbox backend" misdirects the operator entirely.
func TestResolveExecBackend_CanceledContextDoesNotAttemptFallback(t *testing.T) {
	_, _, calls := stubOSLevel(t, nil)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b, _, _, err := ResolveExecBackend(ctx, true, sc)

	require.Error(t, err)
	assert.Nil(t, b)
	assert.Equal(t, 0, *calls, "a canceled context must not spend a fallback attempt")
	assert.NotErrorIs(t, err, ErrSandboxNoUsableBackend,
		"an interrupt is not a neither-backend-usable outcome")
}

// TestResolveExecBackend_InterruptDuringFallbackPreflightIsNotSentineled pins
// the post-branch half of "an interrupt is not a neither-backend-usable
// outcome": the ctx.Err() gate covers a ctrl-C arriving BEFORE the OS-level
// preflight, but nothing re-checks cancellation after osBackend.Preflight
// returns. A signal landing inside that window must not surface as
// ErrSandboxNoUsableBackend — the operator pressed ctrl-C; their sandbox
// configuration is not broken.
func TestResolveExecBackend_InterruptDuringFallbackPreflightIsNotSentineled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	fake, _, calls := stubOSLevel(t, context.Canceled)
	fake.cancel = cancel // the SIGINT lands while the OS-level preflight runs
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)

	b, _, _, err := ResolveExecBackend(ctx, true, sc)

	require.Error(t, err)
	assert.Nil(t, b)
	assert.Equal(t, 1, *calls)
	assert.Equal(t, 1, fake.preflights)
	assert.NotErrorIs(t, err, ErrSandboxNoUsableBackend,
		"a ctrl-C during the OS-level preflight is an interrupt, not a broken sandbox config")
	assert.ErrorIs(t, err, context.Canceled,
		"the cancellation must stay reachable through the chain")
}

// TestOSLevelBackendSeam_RealConstructorIsWired exercises the UNSTUBBED seam.
// Every other fallback test replaces it, so without this the production body of
// newOSLevelBackendFn runs in no test at all, and the identifier contract is
// asserted only against the fake's hardcoded literal — leaving a drift between
// sandbox's backend name and registry's config sentinel undetectable. Name() is
// a constant, so this needs no sandbox binary and runs on every platform.
func TestOSLevelBackendSeam_RealConstructorIsWired(t *testing.T) {
	secs := 45
	sc := &registry.SandboxConfig{TimeoutSecs: &secs}
	b := newOSLevelBackendFn(osLevelFallbackConfig(sc))
	require.NotNil(t, b)
	assert.Equal(t, registry.SandboxFallbackOSLevel, b.Name(),
		"the backend's name and the config value operators write must not drift apart")

	ob, ok := b.(*sandbox.OSLevelBackend)
	require.True(t, ok, "the resolver builds a *sandbox.OSLevelBackend")
	assert.Equal(t, 45*time.Second, ob.Timeout(),
		"TimeoutSecs override must reach the backend's per-run budget")
}

func TestResolveExecBackend_FallbackIrrelevantWhenExecDisabled(t *testing.T) {
	_, _, calls := stubOSLevel(t, nil)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)

	b, _, _, err := ResolveExecBackend(context.Background(), false, sc)

	require.NoError(t, err)
	assert.Nil(t, b)
	assert.Equal(t, 0, *calls,
		"the fallback lives entirely inside the execEnabled path")
}

// --- fallback-engagement warning -------------------------------------------

// captureWarnings installs a logger into the context and returns the buffer, so
// the fallback warning can be asserted rather than assumed. It is the only
// operator-visible signal that the isolation model changed, and before this it
// was covered by no test at all.
func captureWarnings(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return log.NewContext(context.Background(), logger), buf
}

func TestFallbackWarning_FiresOnEngagementNamingWhatIsNotEnforced(t *testing.T) {
	stubOSLevel(t, nil)
	ctx, buf := captureWarnings(t)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)
	sc.Memory = "512m"
	sc.CPUs = "1.5"
	pids := 256
	sc.PidsLimit = &pids

	b, _, _, err := ResolveExecBackend(ctx, true, sc)
	require.NoError(t, err)
	// The notice is DEFERRED until the caller commits to the run; emitting here
	// is what a CLI call site does once its own gates pass.
	EmitPendingFallbackWarning(ctx, b)

	out := buf.String()
	assert.Contains(t, out, "os-level sandbox fallback engaged")
	assert.Contains(t, out, "sandbox.memory")
	assert.Contains(t, out, "sandbox.cpus")
	assert.Contains(t, out, "sandbox.pids_limit")
	assert.Contains(t, out, "sandbox.image")
	assert.Equal(t, 1, strings.Count(out, "os-level sandbox fallback engaged"),
		"exactly one warning per engagement")
}

// TestFallbackWarning_NamesLostDefaultsEvenWhenNothingWasConfigured covers the
// common config — image + test_command only — where the explicitly-set list is
// nearly empty yet every hardened Docker default is still discarded.
func TestFallbackWarning_NamesLostDefaultsEvenWhenNothingWasConfigured(t *testing.T) {
	stubOSLevel(t, nil)
	ctx, buf := captureWarnings(t)
	sc := &registry.SandboxConfig{
		DockerPath:  fakeDocker(t, "exit 1"),
		TestCommand: []string{"go", "test"},
		Fallback:    registry.SandboxFallbackOSLevel,
	}

	b, _, _, err := ResolveExecBackend(ctx, true, sc)
	require.NoError(t, err)
	EmitPendingFallbackWarning(ctx, b) // deferred until the caller commits

	out := buf.String()
	assert.Contains(t, out, "uid 65534")
	assert.Contains(t, out, "host system and toolchain directories are readable",
		"the notice must name what this backend genuinely does not isolate; it claimed a host /tmp carve-out that was removed in 5f6a952")
	assert.Contains(t, out, "cap-drop ALL")
}

func TestFallbackWarning_SilentWhenDockerSucceedsOrFallbackFails(t *testing.T) {
	t.Run("docker succeeds", func(t *testing.T) {
		stubOSLevel(t, nil)
		ctx, buf := captureWarnings(t)
		sc := &registry.SandboxConfig{
			DockerPath: fakeDocker(t, `if [ "$1" = "info" ]; then
  echo '{"MemTotal": 8589934592, "NCPU": 8}'
  exit 0
fi
exit 0`),
			TestCommand: []string{"go", "test"},
			Fallback:    registry.SandboxFallbackOSLevel,
		}
		_, _, _, err := ResolveExecBackend(ctx, true, sc)
		require.NoError(t, err)
		assert.NotContains(t, buf.String(), "fallback engaged",
			"a working primary backend must never warn")
	})

	t.Run("fallback also fails", func(t *testing.T) {
		stubOSLevel(t, errors.New("bwrap missing"))
		ctx, buf := captureWarnings(t)
		_, _, _, err := ResolveExecBackend(ctx, true, fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel))
		require.Error(t, err)
		assert.NotContains(t, buf.String(), "fallback engaged",
			"nothing was engaged, so the refusal is the whole signal")
	})
}

func TestFallbackWarning_AutoFixResolverWarnsIdentically(t *testing.T) {
	// Cross-resolver consistency, asserted mechanically rather than by review:
	// an operator must not learn the isolation model changed on --exec and be
	// left uninformed on --auto-fix.
	stubOSLevel(t, nil)
	ctx, buf := captureWarnings(t)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel)
	sc.Image = "alpine:3.20"

	b, err := ResolveAutoFixSandbox(ctx, true, sc)
	require.NoError(t, err)
	EmitPendingFallbackWarning(ctx, b) // deferred until the caller commits
	assert.Contains(t, buf.String(), "os-level sandbox fallback engaged")
	assert.Contains(t, buf.String(), "uid 65534")
}

func TestTruncateCause_BoundsAnUnboundedDaemonMessage(t *testing.T) {
	assert.Equal(t, "", truncateCause(nil))
	short := errors.New("docker daemon unreachable")
	assert.Equal(t, "docker daemon unreachable", truncateCause(short))
	long := errors.New(strings.Repeat("x", 500))
	got := truncateCause(long)
	assert.LessOrEqual(t, len(got), 300+len("… (truncated)"))
	assert.Contains(t, got, "truncated")
}

func TestTruncateCause_TruncatesOnRuneBoundary(t *testing.T) {
	// 299 ASCII bytes plus a 3-byte rune places byte 300 inside the rune.
	long := errors.New(strings.Repeat("x", 299) + strings.Repeat("€", 10))
	got := truncateCause(long)
	assert.True(t, utf8.ValidString(got))
	assert.Contains(t, got, "truncated")
}

// TestOSLevelFallbackConfig_UnsetTimeoutInheritsTheHardenedDefaults covers the
// common config, where sandbox.timeout_secs is absent. Both fallback timeout
// tests set it explicitly, so the default path was asserted nowhere — and the
// Phase 4 gate showed that seeding from a zero OSLevelConfig instead of
// DefaultOSLevelConfig left the whole suite green while returning timeout 0,
// which makes internal/tools refuse EVERY run_tests/run_script call with
// "execution timeout not configured": --exec under the fallback silently stops
// executing anything at all.
func TestOSLevelFallbackConfig_UnsetTimeoutInheritsTheHardenedDefaults(t *testing.T) {
	def := sandbox.DefaultOSLevelConfig()

	got := osLevelFallbackConfig(&registry.SandboxConfig{}) // no TimeoutSecs

	assert.Equal(t, def.Timeout, got.Timeout)
	assert.Positive(t, got.Timeout, "a zero timeout disables execution downstream rather than defaulting it")
	assert.Equal(t, def.MaxOutputBytes, got.MaxOutputBytes)
	assert.Positive(t, got.MaxOutputBytes)
	assert.Equal(t, def.MaxConcurrent, got.MaxConcurrent)
	assert.Positive(t, got.MaxConcurrent)
}

// TestResolveExecBackend_FallbackWithoutConfiguredTimeoutStillExecutes is the
// same guarantee at the resolver's boundary, where the caller consumes it.
func TestResolveExecBackend_FallbackWithoutConfiguredTimeoutStillExecutes(t *testing.T) {
	_, gotCfg, _ := stubOSLevel(t, nil)
	sc := fallbackSandboxConfig(t, registry.SandboxFallbackOSLevel) // no TimeoutSecs

	_, _, timeout, err := ResolveExecBackend(context.Background(), true, sc)

	require.NoError(t, err)
	assert.Equal(t, sandbox.DefaultOSLevelConfig().Timeout, timeout)
	assert.Positive(t, timeout, "the caller must never receive a zero timeout from the fallback path")
	assert.Positive(t, gotCfg.MaxOutputBytes)
}
