package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// osLevelBackend must satisfy Backend exactly as DockerBackend does
// (sandbox.go:122). oslevel.go carries the production assertion; this one keeps
// a signature regression attributable to the test suite as well.
var _ Backend = (*osLevelBackend)(nil)

func TestOSLevelBackend_Name_StableOnStructLiteralAndConstructor(t *testing.T) {
	// Name() is called from hot logging paths and must not depend on
	// constructor-populated state (AC 01-01 Scenario 2 / Edge Case 1).
	assert.Equal(t, osLevelBackendName, (&osLevelBackend{}).Name(),
		"bare struct literal must still report the backend identifier")
	assert.Equal(t, osLevelBackendName, NewOSLevelBackend(DefaultOSLevelConfig()).Name())
}

func TestOSLevelBackendName_MatchesFallbackConfigValue(t *testing.T) {
	// The identifier is how an operator connects `sandbox.fallback: os-level`
	// (Phase 4's registry.SandboxFallbackOSLevel) to the backend named in
	// diagnostics. Pinned here so a rename cannot silently break that link.
	assert.Equal(t, "os-level", osLevelBackendName)
}

func TestDefaultOSLevelConfig_BoundsAreNonZero(t *testing.T) {
	// Scoped deliberately: this asserts the three bounds OSLevelConfig actually
	// carries, NOT that the defaults are "safe" in the package-contract sense —
	// there are no memory/CPU/PID/uid caps to assert (TD-001).
	cfg := DefaultOSLevelConfig()
	assert.Positive(t, cfg.Timeout, "an unbounded default timeout is a resource-exhaustion risk")
	assert.Positive(t, cfg.MaxOutputBytes)
	assert.Positive(t, cfg.MaxConcurrent)
}

func TestNewOSLevelBackend_ZeroValueConfigGetsSafeDefaults(t *testing.T) {
	// Mirrors NewDockerBackend's zero-value filling (docker.go:92-104): callers
	// must not have to pre-populate every field (AC 01-01 Scenario 3).
	def := DefaultOSLevelConfig()
	b := NewOSLevelBackend(OSLevelConfig{})
	require.NotNil(t, b)

	assert.Equal(t, def.Timeout, b.cfg.Timeout)
	assert.Equal(t, def.MaxOutputBytes, b.cfg.MaxOutputBytes)
	assert.Equal(t, def.MaxConcurrent, b.cfg.MaxConcurrent)
	assert.Equal(t, def.Timeout, b.Timeout())
	// The semaphore must be sized from the FLOORED value. Sizing it from the
	// caller's pre-floor zero would leave an unbuffered channel that deadlocks
	// the first Run, while b.cfg.MaxConcurrent still asserted green.
	assert.Equal(t, def.MaxConcurrent, cap(b.sem))
}

func TestNewOSLevelBackend_FloorsNonPositiveConfig(t *testing.T) {
	// A negative/zero timeout on an OS-level sandbox is the same
	// resource-exhaustion risk NewDockerBackend floors at docker.go:98-100.
	def := DefaultOSLevelConfig()
	b := NewOSLevelBackend(OSLevelConfig{
		Timeout:        -1 * time.Second,
		MaxOutputBytes: -1,
		MaxConcurrent:  -3,
	})
	require.NotNil(t, b)

	assert.Equal(t, def.Timeout, b.cfg.Timeout)
	assert.Equal(t, def.MaxOutputBytes, b.cfg.MaxOutputBytes)
	assert.Equal(t, def.MaxConcurrent, b.cfg.MaxConcurrent)
	assert.Equal(t, def.MaxConcurrent, cap(b.sem))
}

func TestNewOSLevelBackend_PreservesExplicitConfig(t *testing.T) {
	// Flooring must apply only to non-positive values — an operator's explicit
	// settings survive the constructor untouched.
	b := NewOSLevelBackend(OSLevelConfig{
		ToolPath:       "/opt/custom/bwrap",
		Timeout:        7 * time.Second,
		MaxOutputBytes: 99,
		MaxConcurrent:  2,
	})
	require.NotNil(t, b)

	assert.Equal(t, "/opt/custom/bwrap", b.cfg.ToolPath)
	assert.Equal(t, 7*time.Second, b.cfg.Timeout)
	assert.Equal(t, 99, b.cfg.MaxOutputBytes)
	assert.Equal(t, 2, b.cfg.MaxConcurrent)
}

func TestOSLevelBackend_ToolPath_ExplicitAbsoluteOverrideWins(t *testing.T) {
	// The override is the test-shim seam AND the field an operator config would
	// populate — i.e. the seam through which containment could be swapped for a
	// no-op — so its precedence is asserted explicitly rather than assumed.
	b := NewOSLevelBackend(OSLevelConfig{ToolPath: "/opt/custom/bwrap"})
	got, err := b.toolPath()
	require.NoError(t, err)
	assert.Equal(t, "/opt/custom/bwrap", got)
}

func TestOSLevelBackend_ToolPath_RejectsNonAbsoluteOverride(t *testing.T) {
	// A relative or bare name would be resolved through $PATH at spawn time,
	// letting any user-writable $PATH entry shadow the real sandbox binary.
	for _, bad := range []string{"bwrap", "./bwrap", "../bin/sandbox-exec"} {
		t.Run(bad, func(t *testing.T) {
			b := NewOSLevelBackend(OSLevelConfig{ToolPath: bad})
			got, err := b.toolPath()
			require.Error(t, err, "a non-absolute tool_path must be rejected")
			assert.Empty(t, got)
			assert.Contains(t, err.Error(), "must be absolute")
		})
	}
}

func TestOSLevelBackend_ToolPath_EmptyOverrideDelegatesToPlatform(t *testing.T) {
	b := NewOSLevelBackend(OSLevelConfig{})
	got, err := b.toolPath()
	want, wantErr := osLevelToolFor(runtime.GOOS)
	if wantErr != nil {
		// Unsupported host: the error must propagate, never a silent fallback.
		require.Error(t, err)
		assert.Empty(t, got)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestOSLevelBackend_FailsClosedWithoutAUsableSandbox(t *testing.T) {
	// A backend that reports success while providing no containment is the
	// exact --no-sandbox exposure this epic removes, so both entry points are
	// pinned to fail closed rather than trusted to.
	b := NewOSLevelBackend(DefaultOSLevelConfig())

	// Preflight is a stub until task 1.11; it must error until it genuinely
	// verifies the sandbox, never return a permissive nil.
	assert.Error(t, b.Preflight(context.Background()),
		"Preflight must never report success before it verifies the sandbox")

	// Run refuses a malformed spec before spawning anything (AC 01-03 Edge
	// Case 1). This assertion outlives the stub window.
	_, err := b.Run(context.Background(), RunSpec{})
	assert.Error(t, err, "Run must reject a spec with neither Command nor Script")
}

// writeFakeOSLevel writes an executable shell script that impersonates the
// platform sandboxing binary (sandbox-exec / bwrap) so Run and Preflight can be
// exercised deterministically on any host, with neither real tool installed.
// It mirrors writeFakeDocker (sandbox_test.go:25); the returned path is absolute,
// which cfg.ToolPath requires.
func writeFakeOSLevel(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake os-level shell shim is POSIX-only")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "fake-os-sandbox")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755))
	return p
}

// fakeOSLevelExitBody returns a shim body that exits with code.
//
// The value is baked into the script rather than read from an env var (as
// fakeDockerExitBody does with DOCKER_EXIT_CODE) because Run hands the workload
// an explicit environment allowlist — an env-var hook would be scrubbed away
// before the shim ever saw it. That scrub is the point; see
// TestOSLevelBackendRun_DoesNotLeakParentEnvironment.
func fakeOSLevelExitBody(code int) string {
	return fmt.Sprintf(`echo "fake os-level tool exit %d" >&2
exit %d`, code, code)
}

func newFakeOSLevelBackend(t *testing.T, body string) *osLevelBackend {
	t.Helper()
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = writeFakeOSLevel(t, body)
	cfg.MaxConcurrent = 1
	return NewOSLevelBackend(cfg)
}

func TestOSLevelBackendRun_NormalExitIsResultNotError(t *testing.T) {
	// Backend.Run's contract (sandbox.go:112-114): a non-zero program exit is
	// reported via ExitCode, never as a Go error.
	spec := RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()}

	for _, code := range []int{0, 1, 3, 42} {
		t.Run(fmt.Sprintf("exit-%d", code), func(t *testing.T) {
			b := newFakeOSLevelBackend(t, fakeOSLevelExitBody(code))
			res, err := b.Run(context.Background(), spec)
			require.NoError(t, err, "a program exit must never surface as a backend error")
			assert.Equal(t, code, res.ExitCode)
			assert.False(t, res.TimedOut)
		})
	}
}

func TestOSLevelBackendRun_RendersCommandForEvidence(t *testing.T) {
	b := newFakeOSLevelBackend(t, fakeOSLevelExitBody(0))
	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"go", "test", "./..."},
		SnapshotDir: t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, "go test ./...", res.Command,
		"RunResult.Command feeds the evidence_exec block and the report")
}

// fakeOSLevelSleepBody sleeps, then touches the file named by OSLEVEL_MARKER.
//
// The sleep is deliberately SHORTER than the observation window the kill test
// waits out: the marker must be reachable in the absence of a kill, otherwise
// asserting its absence proves nothing. A background subshell writes a second
// marker so the assertion also covers a grandchild that the direct-child kill
// alone would leave running — the case the process group exists for.
func fakeOSLevelSleepBody(marker string) string {
	return fmt.Sprintf(`( sleep 2; touch %q ) &
sleep 2
touch %q`, marker+".child", marker)
}

func TestOSLevelBackendRun_TimeoutKillsWholeProcessGroupAndReports124(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	b := newFakeOSLevelBackend(t, fakeOSLevelSleepBody(marker))

	start := time.Now()
	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"sleep", "2"},
		SnapshotDir: t.TempDir(),
		Timeout:     300 * time.Millisecond,
	})
	elapsed := time.Since(start)

	require.NoError(t, err, "a timeout is an outcome, not a backend fault")
	assert.True(t, res.TimedOut)
	assert.Equal(t, timeoutExitCode, res.ExitCode)
	assert.Less(t, elapsed, 10*time.Second, "Run must not hang past its deadline")

	// Outlast the shim's own 2s sleep. Both markers would exist by now if the
	// workload had been allowed to finish, so their absence is real evidence of
	// the kill rather than a race the test always wins.
	time.Sleep(3 * time.Second)
	assert.NoFileExists(t, marker, "the sandboxed workload must be killed, not left running")
	assert.NoFileExists(t, marker+".child",
		"a forked grandchild must be reaped too — that is what the process group is for")
}

func TestOSLevelBackendRun_ParentCancelFoldsIntoTimeout(t *testing.T) {
	// A cancellation must never be misreported as a spurious non-zero exit or a
	// backend fault, matching docker.go:301.
	b := newFakeOSLevelBackend(t, fakeOSLevelSleepBody(filepath.Join(t.TempDir(), "survived")))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	res, err := b.Run(ctx, RunSpec{Command: []string{"sleep", "30"}, SnapshotDir: t.TempDir()})
	require.NoError(t, err)
	assert.True(t, res.TimedOut)
	assert.Equal(t, timeoutExitCode, res.ExitCode)
}

func TestOSLevelBackendRun_ZeroSpecTimeoutFallsBackToConfig(t *testing.T) {
	// docker.go:252-255 equivalent: an unset RunSpec.Timeout uses cfg.Timeout.
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = writeFakeOSLevel(t, fakeOSLevelSleepBody(filepath.Join(t.TempDir(), "survived")))
	cfg.Timeout = 300 * time.Millisecond
	b := NewOSLevelBackend(cfg)

	start := time.Now()
	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"sleep", "30"},
		SnapshotDir: t.TempDir(),
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, res.TimedOut, "cfg.Timeout must bound a run with no spec timeout")
	assert.Less(t, elapsed, 10*time.Second)
}

func TestOSLevelBackendRun_TruncatesCombinedOutput(t *testing.T) {
	// Both streams are captured and bounded, mirroring docker.go:279-287.
	b := newFakeOSLevelBackend(t, `echo "stdout-marker"
echo "stderr-marker" >&2
i=0
while [ $i -lt 400 ]; do
  echo "filler-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  i=$((i + 1))
done
exit 0`)
	b.cfg.MaxOutputBytes = 512

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"noisy"},
		SnapshotDir: t.TempDir(),
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(res.Output), 512, "output must be truncated to MaxOutputBytes")
	assert.Contains(t, res.Output, "truncated")
	// Both markers are emitted first, so they sit inside the retained prefix.
	// Asserting stderr explicitly means dropping `cmd.Stderr = lw` — which would
	// silently erase every diagnostic from a failing workload — fails the test.
	assert.Contains(t, res.Output, "stdout-marker", "stdout must be captured")
	assert.Contains(t, res.Output, "stderr-marker", "stderr must be captured")
}

func TestOSLevelRunArgs_ModesAndValidation(t *testing.T) {
	cfg := DefaultOSLevelConfig()
	snap := t.TempDir()

	t.Run("command mode passes argv through verbatim", func(t *testing.T) {
		args, err := osLevelRunArgs(cfg, RunSpec{Command: []string{"go", "test", "./..."}, SnapshotDir: snap})
		require.NoError(t, err)
		assert.Equal(t, []string{"go", "test", "./..."}, args)
	})

	t.Run("script mode feeds sh -s over stdin", func(t *testing.T) {
		args, err := osLevelRunArgs(cfg, RunSpec{Script: "echo hi", SnapshotDir: snap})
		require.NoError(t, err)
		assert.Equal(t, []string{"/bin/sh", "-s"}, args)
		assert.NotContains(t, strings.Join(args, " "), "echo hi",
			"the script body must never reach argv — it travels as stdin data")
	})

	t.Run("rejects a malformed spec before building anything", func(t *testing.T) {
		for name, spec := range map[string]RunSpec{
			"neither command nor script": {SnapshotDir: snap},
			"both command and script":    {Command: []string{"true"}, Script: "echo hi", SnapshotDir: snap},
			"missing snapshot dir":       {Command: []string{"true"}},
			"relative snapshot dir":      {Command: []string{"true"}, SnapshotDir: "relative/path"},
		} {
			t.Run(name, func(t *testing.T) {
				args, err := osLevelRunArgs(cfg, spec)
				require.Error(t, err)
				assert.Nil(t, args)
			})
		}
	})
}

// fakeOSLevelEchoStdinBody makes the shim cat its stdin, so a test can observe
// exactly what Run streams to `sh -s` — the only way to assert stdin content.
func fakeOSLevelEchoStdinBody() string { return `cat` }

func TestOSLevelBackendRun_ScriptTravelsAsStdinNotArgv(t *testing.T) {
	// The injection-safety guarantee (sandbox.go:51-56) is that a script body is
	// program source delivered over stdin, never a shell argument.
	b := newFakeOSLevelBackend(t, fakeOSLevelEchoStdinBody())
	script := "echo 'quoted; $(whoami) `id`'"

	res, err := b.Run(context.Background(), RunSpec{Script: script, SnapshotDir: t.TempDir()})
	require.NoError(t, err)
	assert.Contains(t, res.Output, script, "the script body must reach the tool over stdin")

	args, err := osLevelRunArgs(b.cfg, RunSpec{Script: script, SnapshotDir: t.TempDir()})
	require.NoError(t, err)
	for _, a := range args {
		assert.NotContains(t, a, "whoami", "no script token may be interpolated into argv")
	}
}

func TestOSLevelBackendRun_RunsInsideTheSnapshotDir(t *testing.T) {
	// Leaving cmd.Dir unset would run model-authored code in atcr's own working
	// directory — the operator's live repo — rather than the snapshot.
	snap := t.TempDir()
	b := newFakeOSLevelBackend(t, `pwd`)

	res, err := b.Run(context.Background(), RunSpec{Command: []string{"pwd"}, SnapshotDir: snap})
	require.NoError(t, err)
	// macOS resolves TempDir under /private; compare resolved paths.
	wantDir, err := filepath.EvalSymlinks(snap)
	require.NoError(t, err)
	gotDir, err := filepath.EvalSymlinks(strings.TrimSpace(res.Output))
	require.NoError(t, err)
	assert.Equal(t, wantDir, gotDir)
}

func TestOSLevelBackendRun_DoesNotLeakParentEnvironment(t *testing.T) {
	// Inheriting os.Environ() would hand every credential in the operator's
	// shell to LLM-generated code. Neither sandbox-exec nor bwrap scrubs the
	// environment on its own, so the allowlist has to hold here.
	t.Setenv("ATCR_TEST_FAKE_SECRET", "super-secret-token")
	b := newFakeOSLevelBackend(t, `env`)

	res, err := b.Run(context.Background(), RunSpec{Command: []string{"env"}, SnapshotDir: t.TempDir()})
	require.NoError(t, err)
	assert.NotContains(t, res.Output, "super-secret-token",
		"a parent-environment secret must never reach the sandboxed workload")
	assert.NotContains(t, res.Output, "ATCR_TEST_FAKE_SECRET")
	assert.Contains(t, res.Output, "PATH=", "the allowlist must still provide PATH")
}

func TestOSLevelBackendRun_RefusesWritableOverlay(t *testing.T) {
	// Writable asks for an ephemeral copy so the snapshot stays read-only. This
	// backend has no overlay yet, and silently ignoring the flag would run the
	// workload directly against the operator's snapshot.
	b := newFakeOSLevelBackend(t, fakeOSLevelExitBody(0))
	_, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"true"},
		SnapshotDir: t.TempDir(),
		Writable:    true,
	})
	require.Error(t, err, "an unhonored isolation flag must fail closed, not be ignored")
	assert.Contains(t, err.Error(), "Writable")
}

func TestOSLevelBackendRun_ConcurrencyCapAppliesToStructLiteral(t *testing.T) {
	// A struct-literal backend bypasses NewOSLevelBackend and leaves sem nil;
	// the lazy alloc must still enforce a cap rather than failing open
	// (mirrors TestDockerBackend_StructLiteral_AppliesConcurrencyCap).
	b := &osLevelBackend{cfg: OSLevelConfig{
		ToolPath:       writeFakeOSLevel(t, fakeOSLevelExitBody(0)),
		Timeout:        5 * time.Second,
		MaxOutputBytes: 1024,
		MaxConcurrent:  2,
	}}
	_, err := b.Run(context.Background(), RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
	require.NoError(t, err)
	assert.Equal(t, 2, cap(b.sem), "the nil-sem bypass must not silently disable the cap")
}

func TestOSLevelToolFor_SupportedPlatforms(t *testing.T) {
	// Platform branching is internal to internal/sandbox/ — callers never
	// switch on runtime.GOOS (AC 01-01 Edge Case 2). Taking the GOOS as a
	// parameter makes every platform's decision assertable from any host.
	darwin, err := osLevelToolFor("darwin")
	require.NoError(t, err)
	assert.Equal(t, "sandbox-exec", darwin)

	linux, err := osLevelToolFor("linux")
	require.NoError(t, err)
	assert.Equal(t, "bwrap", linux)
}

func TestOSLevelToolFor_UnsupportedPlatformFailsClosed(t *testing.T) {
	// .goreleaser.yaml ships windows/amd64 and windows/arm64 while CI is
	// ubuntu-latest only, so nothing but this test guards the windows path
	// before a release tag (AC 01-01 Edge Case 3). A stub that silently
	// succeeded would hand callers a backend with no containment at all — the
	// exact --no-sandbox exposure this epic removes.
	for _, goos := range []string{"windows", "plan9", "js"} {
		t.Run(goos, func(t *testing.T) {
			tool, err := osLevelToolFor(goos)
			require.Error(t, err, "unsupported platform must fail closed")
			assert.Empty(t, tool)
			assert.Contains(t, err.Error(), "not supported on "+goos)
		})
	}
}
