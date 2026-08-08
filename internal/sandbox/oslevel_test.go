package sandbox

import (
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

func TestDefaultOSLevelConfig_IsSafe(t *testing.T) {
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
