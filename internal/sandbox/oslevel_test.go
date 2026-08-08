package sandbox

import (
	"context"
	"runtime"
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

func TestOSLevelBackend_StubsFailClosed(t *testing.T) {
	// Preflight and Run are stubs until tasks 1.11 and 1.5. Pin them to fail
	// closed now: a partial implementation that returns nil on an unhandled
	// branch would otherwise pass the whole suite, handing a caller a backend
	// that reports success while providing no containment.
	b := NewOSLevelBackend(DefaultOSLevelConfig())
	assert.Error(t, b.Preflight(context.Background()),
		"Preflight must never report success before it verifies the sandbox")
	_, err := b.Run(context.Background(), RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()})
	assert.Error(t, err, "Run must never report success before it sandboxes anything")
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
