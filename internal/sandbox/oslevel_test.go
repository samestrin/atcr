package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

// fakeOSLevelExitBody returns a shim body that exits with the code in
// OSLEVEL_EXIT_CODE, mirroring fakeDockerExitBody's DOCKER_EXIT_CODE hook.
func fakeOSLevelExitBody() string {
	return `if [ -n "$OSLEVEL_EXIT_CODE" ]; then
  echo "fake os-level tool exit $OSLEVEL_EXIT_CODE" >&2
  exit "$OSLEVEL_EXIT_CODE"
fi
exit 0`
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
	b := newFakeOSLevelBackend(t, fakeOSLevelExitBody())
	spec := RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()}

	for _, code := range []int{0, 1, 3, 42} {
		t.Run(fmt.Sprintf("exit-%d", code), func(t *testing.T) {
			t.Setenv("OSLEVEL_EXIT_CODE", strconv.Itoa(code))
			res, err := b.Run(context.Background(), spec)
			require.NoError(t, err, "a program exit must never surface as a backend error")
			assert.Equal(t, code, res.ExitCode)
			assert.False(t, res.TimedOut)
		})
	}
}

func TestOSLevelBackendRun_RendersCommandForEvidence(t *testing.T) {
	b := newFakeOSLevelBackend(t, fakeOSLevelExitBody())
	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"go", "test", "./..."},
		SnapshotDir: t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, "go test ./...", res.Command,
		"RunResult.Command feeds the evidence_exec block and the report")
}

// fakeOSLevelSleepBody sleeps past any test deadline, then touches the file named
// by OSLEVEL_MARKER. The marker is the proof of the kill: if it appears, the
// workload outlived its deadline instead of being killed.
func fakeOSLevelSleepBody() string {
	return `sleep 30
touch "$OSLEVEL_MARKER"`
}

func TestOSLevelBackendRun_TimeoutKillsAndReports124(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	t.Setenv("OSLEVEL_MARKER", marker)
	b := newFakeOSLevelBackend(t, fakeOSLevelSleepBody())

	start := time.Now()
	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"sleep", "30"},
		SnapshotDir: t.TempDir(),
		Timeout:     300 * time.Millisecond,
	})
	elapsed := time.Since(start)

	require.NoError(t, err, "a timeout is an outcome, not a backend fault")
	assert.True(t, res.TimedOut)
	assert.Equal(t, timeoutExitCode, res.ExitCode)
	assert.Less(t, elapsed, 10*time.Second, "Run must not hang past its deadline")

	time.Sleep(200 * time.Millisecond)
	assert.NoFileExists(t, marker, "the sandboxed workload must be killed, not left running")
}

func TestOSLevelBackendRun_ParentCancelFoldsIntoTimeout(t *testing.T) {
	// A cancellation must never be misreported as a spurious non-zero exit or a
	// backend fault, matching docker.go:301.
	b := newFakeOSLevelBackend(t, fakeOSLevelSleepBody())
	t.Setenv("OSLEVEL_MARKER", filepath.Join(t.TempDir(), "survived"))

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
	cfg.ToolPath = writeFakeOSLevel(t, fakeOSLevelSleepBody())
	cfg.Timeout = 300 * time.Millisecond
	b := NewOSLevelBackend(cfg)
	t.Setenv("OSLEVEL_MARKER", filepath.Join(t.TempDir(), "survived"))

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
	b := newFakeOSLevelBackend(t, `
i=0
while [ $i -lt 400 ]; do
  echo "stdout-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  echo "stderr-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" >&2
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
	assert.Contains(t, res.Output, "stdout-", "stdout must be captured")
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
