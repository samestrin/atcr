package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

	// Phase 1 generates no containment arguments, so the backend cannot isolate
	// anything and Preflight refuses at its step-0 gate. Asserting the sentinel
	// rather than "some error" keeps this honest: on a host lacking the platform
	// binary entirely, a bare require.Error would be satisfied by the
	// missing-binary path and would prove nothing about containment.
	err := b.Preflight(context.Background())
	require.Error(t, err, "Preflight must never report a usable sandbox before it verifies one")
	assert.ErrorIs(t, err, ErrOSLevelNoContainment)

	// Run refuses a malformed spec before spawning anything (AC 01-03 Edge
	// Case 1).
	_, runErr := b.Run(context.Background(), RunSpec{})
	assert.Error(t, runErr, "Run must reject a spec with neither Command nor Script")
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

// --- AC 01-03: backend-fault error taxonomy -------------------------------

func TestOSLevelClassifyToolExit_LinuxKeysOnBwrapDiagnostic(t *testing.T) {
	// MEASURED against bubblewrap 0.9.0: every bwrap fault exits 1 and prints a
	// "bwrap: " diagnostic (or "usage: bwrap" when given no command). bwrap does
	// NOT reserve 125/126/127 — it passes the workload's status through verbatim
	// — so the exit code alone cannot separate the two and the diagnostic is the
	// only reliable signal.
	faults := []struct {
		code   int
		output string
	}{
		{1, "bwrap: setting up uid map: Permission denied"},
		{1, "bwrap: Unknown option --not-a-real-flag"},
		{1, "bwrap: execvp /nonexistent-binary-xyz: No such file or directory"},
		{1, "usage: bwrap [OPTIONS...] [--] COMMAND [ARGS...]"},
	}
	for _, f := range faults {
		isFault, reason := classifyToolExit("linux", f.code, f.output)
		assert.True(t, isFault, "a bwrap diagnostic marks a tool fault: %q", f.output)
		assert.NotEmpty(t, reason, "a fault must carry a reason for the wrapped error")
	}

	// Workload statuses bwrap passed through verbatim must stay results. 126 and
	// 127 especially: they are sh's not-executable/not-found codes and the most
	// common outcome for a command referencing an uninstalled tool.
	for _, code := range []int{1, 2, 3, 42, 124, 125, 126, 127} {
		isFault, _ := classifyToolExit("linux", code, "/bin/sh: 1: nosuchcmd: not found")
		assert.False(t, isFault, "linux exit %d without a bwrap diagnostic is the workload's own status", code)
	}
}

func TestOSLevelClassifyToolExit_DarwinUsesSysexitsAndDiagnostic(t *testing.T) {
	// MEASURED on macOS 25.2: sandbox-exec returns sysexits values for its own
	// failures and passes the workload's status through otherwise.
	//
	// Exit 64 is why the exit codes are checked and not only the message: its
	// output is "Usage: sandbox-exec ..." and contains no "sandbox-exec:" token
	// at all, so a message-only rule would let a run that never applied a
	// profile be reported as a merely-failing workload.
	faults := []struct {
		code   int
		output string
	}{
		{64, "Usage: sandbox-exec [options] command [args]"},
		{65, "sandbox-exec: syntax error: expecting ')'"},
		{71, "sandbox-exec: execvp() of '/bin/echo' failed: Operation not permitted"},
	}
	for _, f := range faults {
		isFault, reason := classifyToolExit("darwin", f.code, f.output)
		assert.True(t, isFault, "sandbox-exec exit %d is a tool fault", f.code)
		assert.NotEmpty(t, reason)
	}

	// A diagnostic at an unexpected code is still a fault.
	isFault, _ := classifyToolExit("darwin", 3, "sandbox-exec: sandbox_apply: Operation not permitted")
	assert.True(t, isFault)

	// Ordinary workload failures stay results.
	for _, code := range []int{1, 2, 3, 42, 127} {
		isFault, _ := classifyToolExit("darwin", code, "FAIL\tgithub.com/example/pkg\t0.1s")
		assert.False(t, isFault, "darwin exit %d with no sandbox-exec diagnostic is a workload result", code)
	}

	isFault, _ = classifyToolExit("darwin", 0, "")
	assert.False(t, isFault, "a clean exit is never a fault")
}

func TestOSLevelClassifyToolExit_SignalRangeIsAlwaysFault(t *testing.T) {
	// bwrap propagates 128+signal for a signal-killed child, and once Phase 2
	// lands a policy kill arrives the same way. A kernel kill is not a program
	// result, so it is a fault on every platform (docker.go:323-326 parity).
	for _, goos := range []string{"linux", "darwin"} {
		for _, code := range []int{137, 159, 128} {
			isFault, reason := classifyToolExit(goos, code, "")
			assert.True(t, isFault, "%s exit %d is a signal death", goos, code)
			assert.Contains(t, reason, "signal")
		}
	}
}

func TestOSLevelClassifyToolExit_UnknownPlatformFailsClosed(t *testing.T) {
	// An exit condition on a platform with no classification rule is ambiguous,
	// and ambiguity resolves to a fault — never to a silently successful run
	// (sandbox.go:5-14's containment-first contract).
	isFault, reason := classifyToolExit("windows", 1, "")
	assert.True(t, isFault, "an unclassifiable non-zero exit must fail closed")
	assert.NotEmpty(t, reason)

	isFault, _ = classifyToolExit("windows", 0, "")
	assert.False(t, isFault, "a clean exit needs no classification rule")
}

func TestOSLevelBackendRun_ToolFaultIsWrappedErrorNotExitCode(t *testing.T) {
	// Driven as a table over BOTH platforms via the goos seam, so the darwin
	// rule is exercised on ubuntu CI and the linux rule on a macOS dev box —
	// neither branch is dead code on the machine that could have tested it.
	cases := map[string]struct {
		goos     string
		exitCode int
		stderr   string
	}{
		"linux bwrap setup failure": {"linux", 1, "bwrap: setting up uid map: Permission denied"},
		"linux bwrap usage":         {"linux", 1, "usage: bwrap [OPTIONS...] [--] COMMAND [ARGS...]"},
		"darwin sandbox-exec usage": {"darwin", 64, "Usage: sandbox-exec [options] command [args]"},
		"darwin bad profile":        {"darwin", 65, "sandbox-exec: syntax error: expecting ')'"},
		"darwin execvp denied":      {"darwin", 71, "sandbox-exec: execvp() of '/bin/echo' failed"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			b := newFakeOSLevelBackend(t, fmt.Sprintf("echo %q >&2\nexit %d", tc.stderr, tc.exitCode))
			b.goos = tc.goos

			res, err := b.Run(context.Background(), RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
			require.Error(t, err, "a sandbox-tool fault must surface as an error")
			assert.Zero(t, res.ExitCode, "a tool fault must never be folded into the workload's exit code")
			assert.False(t, res.TimedOut, "a fault must not be laundered through the timeout path")

			// The cause must stay reachable: a real %w wrap, never a %v string.
			var ee *exec.ExitError
			assert.True(t, errors.As(err, &ee), "the underlying *exec.ExitError must survive the wrap")
		})
	}
}

func TestOSLevelBackendRun_WorkloadExitStaysAResultOnBothPlatforms(t *testing.T) {
	// The inverse guard: an ordinary failing workload — including sh's 126/127
	// for an uninstalled tool — must stay evidence, not be escalated to a hard
	// backend fault that aborts the review.
	for _, goos := range []string{"linux", "darwin"} {
		for _, code := range []int{1, 2, 126, 127} {
			t.Run(fmt.Sprintf("%s-exit-%d", goos, code), func(t *testing.T) {
				b := newFakeOSLevelBackend(t, fmt.Sprintf("echo '/bin/sh: 1: nosuchcmd: not found' >&2\nexit %d", code))
				b.goos = goos

				res, err := b.Run(context.Background(), RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
				require.NoError(t, err, "a workload exit must never surface as a backend error")
				assert.Equal(t, code, res.ExitCode)
			})
		}
	}
}

func TestOSLevelBackendRun_ClassifiesOnUntruncatedOutput(t *testing.T) {
	// The fault diagnostic must survive a small display budget. Classifying the
	// truncated string would let MaxOutputBytes decide a security question: the
	// "sandbox-exec:" token is destroyed below ~41 bytes, silently turning a
	// containment failure into a reported workload exit.
	b := newFakeOSLevelBackend(t, `echo "sandbox-exec: syntax error: expecting ')'" >&2
exit 65`)
	b.goos = "darwin"
	b.cfg.MaxOutputBytes = 16

	res, err := b.Run(context.Background(), RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
	require.Error(t, err, "a tiny output budget must not hide a sandbox fault")
	assert.Zero(t, res.ExitCode)
	assert.LessOrEqual(t, len(res.Output), 16, "display truncation still applies to the reported output")
}

func TestOSLevelBackendRun_SpawnFailureIsWrappedError(t *testing.T) {
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = "/nonexistent/os-sandbox-binary-xyz"
	b := NewOSLevelBackend(cfg)

	res, err := b.Run(context.Background(), RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
	require.Error(t, err)
	// AC 01-03 Scenario 2 words this as "returns RunResult{}". The result-
	// bearing fields are indeed zero, but Command stays populated because it is
	// a rendering of the REQUEST, not an outcome — and DockerBackend does the
	// same on its spawn-failure path (docker.go:337 returns res, not RunResult{}).
	// Story 1's whole premise is mirroring that backend, so parity wins over the
	// AC's literal wording; the property that matters — no outcome a caller
	// could mistake for a real run — holds either way.
	assert.Zero(t, res.ExitCode, "a spawn failure yields no exit code")
	assert.False(t, res.TimedOut)
	assert.Empty(t, res.Output)
	assert.True(t, errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound),
		"the underlying cause must be reachable through the wrap, got %v", err)
}

func TestOSLevelBackendRun_ValidateRejectsBeforeAnySpawn(t *testing.T) {
	// spec.validate() must run before the tool is invoked at all, so a malformed
	// spec can never reach a process (AC 01-03 Edge Case 1).
	marker := filepath.Join(t.TempDir(), "spawned")
	b := newFakeOSLevelBackend(t, fmt.Sprintf("touch %q\nexit 0", marker))

	for name, spec := range map[string]RunSpec{
		"neither command nor script": {SnapshotDir: t.TempDir()},
		"both command and script":    {Command: []string{"true"}, Script: "echo hi", SnapshotDir: t.TempDir()},
		"relative snapshot dir":      {Command: []string{"true"}, SnapshotDir: "relative/path"},
		"colon in snapshot dir":      {Command: []string{"true"}, SnapshotDir: "/tmp/a:b"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := b.Run(context.Background(), spec)
			require.Error(t, err)
			assert.NoFileExists(t, marker, "no process may be spawned for an invalid spec")
		})
	}
}

func TestOSLevelBackendRun_SignalDeathIsFaultNotExitCode(t *testing.T) {
	// A signal death reports ExitCode() == -1; surfacing that as a program
	// result would present an impossible exit status as a real one.
	b := newFakeOSLevelBackend(t, `kill -9 $$`)

	res, err := b.Run(context.Background(), RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
	require.Error(t, err, "a signal-killed run is a backend fault")
	assert.Zero(t, res.ExitCode, "a signal death must never escape as a workload exit code")
	assert.False(t, res.TimedOut)
	assert.Contains(t, err.Error(), "signal")
}

// --- AC 01-04: Preflight fail-closed ---------------------------------------

// withContainment makes Preflight's step-0 containment gate pass, so a test can
// exercise steps 1-3. Phase 2 makes osLevelContainmentArgs non-empty for real
// and this helper disappears.
func withContainment(t *testing.T, b *osLevelBackend) *osLevelBackend {
	t.Helper()
	orig := osLevelContainmentArgs
	osLevelContainmentArgs = func(string, OSLevelConfig, RunSpec) ([]string, error) {
		return []string{"--fake-containment"}, nil
	}
	t.Cleanup(func() { osLevelContainmentArgs = orig })
	return b
}

// fakeOSLevelExecBody makes the shim behave like a real sandboxing tool: it
// execs the workload argv it was handed. A shim that merely `exit 0`s would not
// satisfy Preflight's nonce probe — which is the point of that probe.
func fakeOSLevelExecBody() string { return `exec "$@"` }

func TestOSLevelBackend_Preflight_RefusesWhileThereIsNoContainment(t *testing.T) {
	// Phase 1 generates no containment arguments, so the backend cannot isolate
	// anything. Preflight gates --exec, so it must refuse rather than report a
	// usable sandbox — otherwise model-authored code would run uncontained.
	b := newFakeOSLevelBackend(t, fakeOSLevelExecBody())

	err := b.Preflight(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOSLevelNoContainment)
}

func TestOSLevelBackend_Preflight_MissingBinary(t *testing.T) {
	// Direct analog of TestDockerBackend_Preflight_MissingBinary
	// (sandbox_test.go:440), the template this AC names.
	cfg := DefaultOSLevelConfig()
	cfg.ToolPath = "/nonexistent/sandbox-tool-xyz"
	b := withContainment(t, NewOSLevelBackend(cfg))

	err := b.Preflight(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox preflight")
	assert.True(t, errors.Is(err, os.ErrNotExist),
		"the underlying cause must survive the wrap, got %v", err)
}

func TestOSLevelBackend_Preflight_PassesWhenSandboxActuallyRunsTheWorkload(t *testing.T) {
	b := withContainment(t, newFakeOSLevelBackend(t, fakeOSLevelExecBody()))
	require.NoError(t, b.Preflight(context.Background()))

	// A successful Preflight pins the verified absolute path without disturbing
	// the operator's own configuration.
	assert.NotEmpty(t, b.pinnedTool)
	assert.Equal(t, b.cfg.ToolPath, b.pinnedTool)
}

func TestOSLevelBackend_Preflight_RejectsToolThatNeverRunsTheWorkload(t *testing.T) {
	// A binary that swallows its argv and exits 0 is indistinguishable from a
	// working sandbox if Preflight only checks the exit status — and it would
	// contain nothing. The nonce probe is what catches it.
	b := withContainment(t, newFakeOSLevelBackend(t, `exit 0`))

	err := b.Preflight(context.Background())
	require.Error(t, err, "a tool that never executes the command must not pass preflight")
	assert.ErrorIs(t, err, errPreflightTrivialRun)
	assert.Contains(t, err.Error(), "never executed the command")
	assert.Empty(t, b.pinnedTool, "a failed preflight must not pin anything")
}

func TestOSLevelBackend_Preflight_UnsupportedPlatformFailsClosed(t *testing.T) {
	// Nothing — not even an operator-set tool_path — may make the backend
	// usable on a platform with no sandboxing mechanism.
	b := withContainment(t, newFakeOSLevelBackend(t, fakeOSLevelExecBody()))
	b.goos = "windows"

	err := b.Preflight(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported on windows")
}

func TestOSLevelBackend_Preflight_ChecksRunInDocumentedOrder(t *testing.T) {
	// Binary presence -> host prerequisite -> trivial run, each short-circuiting
	// (docker.go:346-398's ordered early-return pattern). Asserted by making the
	// FIRST check fail and proving the later ones never ran.
	var ran []string
	b := withContainment(t, newFakeOSLevelBackend(t, fakeOSLevelExecBody()))
	b.cfg.ToolPath = "/nonexistent/sandbox-tool-xyz"
	b.prerequisiteCheck = func(context.Context) error {
		ran = append(ran, "prerequisite")
		return nil
	}

	require.Error(t, b.Preflight(context.Background()))
	assert.Empty(t, ran, "the host prerequisite must not be checked once the binary is missing")
}

func TestOSLevelBackend_Preflight_PrerequisiteFailureIsSpecificAndBeforeTrivialRun(t *testing.T) {
	// A real bwrap user-namespace failure cannot be forced through a shell shim,
	// so the prerequisite check is an injectable seam. The marker file proves
	// the trivial run never happened once the prerequisite failed.
	marker := filepath.Join(t.TempDir(), "trivial-run-happened")
	b := withContainment(t, newFakeOSLevelBackend(t, fmt.Sprintf("touch %q\nexec \"$@\"", marker)))
	b.prerequisiteCheck = func(context.Context) error {
		return errors.New("unprivileged user namespaces are disabled")
	}

	err := b.Preflight(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unprivileged user namespaces",
		"the error must name the unmet prerequisite, not a generic failure")
	assert.NoFileExists(t, marker, "the trivial run must not be attempted after a failed prerequisite")
}

func TestOSLevelBackend_Preflight_TrivialRunFailureFailsPreflight(t *testing.T) {
	// Preflight must not report success on binary presence alone
	// (docker.go:382-397).
	b := withContainment(t, newFakeOSLevelBackend(t, `echo "sandbox-exec: syntax error: expecting ')'" >&2
exit 65`))
	b.goos = "darwin"

	err := b.Preflight(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trivial run")
	assert.Empty(t, b.pinnedTool)
}

func TestOSLevelBackend_Preflight_ResolvesOnlyFromTrustedDirs(t *testing.T) {
	// exec.LookPath would honour $PATH verbatim, and Go maps an empty PATH
	// element to "." — the repository under review. A binary planted there must
	// never become "the sandbox".
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bwrap"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", dir+":")

	b := withContainment(t, NewOSLevelBackend(DefaultOSLevelConfig()))
	b.goos = "linux"

	_, err := b.resolveToolPath()
	require.Error(t, err, "a $PATH-planted binary must not be accepted as the sandbox tool")
	assert.Contains(t, err.Error(), "not found in")
}

func TestCheckHostPrerequisite_LinuxUsernsSysctls(t *testing.T) {
	// Table-driven against an injected /proc root so every branch is assertable
	// from a macOS dev box, not only on whichever Linux host CI happens to use.
	write := func(t *testing.T, rel, content string, mode os.FileMode) string {
		t.Helper()
		root := t.TempDir()
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), mode))
		return root
	}
	const maxNS = "proc/sys/user/max_user_namespaces"

	t.Run("positive value passes", func(t *testing.T) {
		root := write(t, maxNS, "642130\n", 0o644)
		assert.NoError(t, checkHostPrerequisite(context.Background(), "linux", root))
	})
	t.Run("zero is refused", func(t *testing.T) {
		root := write(t, maxNS, "0\n", 0o644)
		err := checkHostPrerequisite(context.Background(), "linux", root)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "user namespaces are disabled")
	})
	t.Run("absent knob is not a failure", func(t *testing.T) {
		assert.NoError(t, checkHostPrerequisite(context.Background(), "linux", t.TempDir()))
	})
	t.Run("unparseable value fails closed", func(t *testing.T) {
		root := write(t, maxNS, "not-a-number\n", 0o644)
		err := checkHostPrerequisite(context.Background(), "linux", root)
		require.Error(t, err, "an unreadable gate is not evidence the gate is open")
		assert.Contains(t, err.Error(), "cannot parse")
	})
	t.Run("unreadable file fails closed", func(t *testing.T) {
		root := write(t, maxNS, "1\n", 0o000)
		err := checkHostPrerequisite(context.Background(), "linux", root)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot read")
	})
	t.Run("non-linux needs no check", func(t *testing.T) {
		root := write(t, maxNS, "0\n", 0o644)
		assert.NoError(t, checkHostPrerequisite(context.Background(), "darwin", root))
	})
	t.Run("honours context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.Error(t, checkHostPrerequisite(ctx, "linux", t.TempDir()))
	})
}

func TestOSLevelBackend_PreflightAndRunAreRaceFree(t *testing.T) {
	// Preflight writes the pinned path while Run reads it. The type is built for
	// concurrent use, so that write must be synchronized — a torn read would
	// decide which binary is spawned to contain untrusted code. Meaningful under
	// `go test -race`.
	b := withContainment(t, newFakeOSLevelBackend(t, fakeOSLevelExecBody()))
	b.cfg.MaxConcurrent = 4
	spec := RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = b.Preflight(context.Background()) }()
		go func() { defer wg.Done(); _, _ = b.Run(context.Background(), spec) }()
	}
	wg.Wait()
}
