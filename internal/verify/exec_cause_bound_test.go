package verify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noisyDaemonTail is the marker at the END of the oversized stderr blob the
// shims below emit. Asserting on the TAIL rather than the head is what makes
// these tests discriminating: truncateCause keeps the first 300 bytes, so a
// marker at the head would survive truncation and the assertion would fail for
// a correctly-bounded message.
const noisyDaemonTail = "DAEMON-STDERR-TAIL-MUST-NOT-REACH-STDERR"

// noisyDockerShim emits ~3 KiB of stderr ending in noisyDaemonTail, then fails.
// dockerCmd embeds the daemon's raw stderr verbatim (internal/sandbox/docker.go),
// so this reproduces the measured shape: an unbounded daemon message carried
// inside the preflight error.
//
// The filler loop is POSIX shell rather than `seq`/`head -c`, matching
// oslevel_test.go's shim bodies — the suite runs on macOS and Linux runners.
func noisyDockerShim(t *testing.T) string {
	t.Helper()
	return fakeDocker(t, `i=0
while [ $i -lt 100 ]; do
  printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx' >&2
  i=$((i + 1))
done
printf '`+noisyDaemonTail+`' >&2
exit 1`)
}

func noisyFallbackConfig(t *testing.T) *registry.SandboxConfig {
	t.Helper()
	return &registry.SandboxConfig{
		DockerPath:  noisyDockerShim(t),
		Image:       "golang:1.25",
		TestCommand: []string{"go", "test", "./..."},
		Fallback:    registry.SandboxFallbackOSLevel,
	}
}

// TestResolveExecBackend_NeitherUsableCauseIsBounded closes the gap between what
// truncateCause is documented to bound and where it was actually applied.
//
// truncateCause bounds the docker cause on the WARNING path
// (warnOSLevelFallbackEngaged), but the neither-backend-usable return %w-wrapped
// the same error untruncated — and runMain prints a returned error straight to
// stderr, so the daemon's full raw stderr and `docker run` argv (absolute host
// paths, a DOCKER_HOST endpoint) reached the terminal and CI logs on exactly the
// failure path an operator pastes into a bug report.
//
// The sentinel and both causes must survive: this is the one path with two
// distinct causes to tell apart, so bounding the TEXT must not flatten the
// CHAIN.
func TestResolveExecBackend_NeitherUsableCauseIsBounded(t *testing.T) {
	osCause := errors.New("bwrap: command not found")
	stubOSLevel(t, osCause)

	_, _, _, err := ResolveExecBackend(context.Background(), true, noisyFallbackConfig(t))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), noisyDaemonTail,
		"the docker daemon's raw stderr must be bounded before it reaches runMain's terminal print")
	assert.Contains(t, err.Error(), "truncated",
		"the bound must be visible, so a reader knows the cause was elided rather than empty")

	// Chain intact — bounding the rendered text must not cost errors.Is.
	assert.ErrorIs(t, err, ErrSandboxNoUsableBackend)
	assert.ErrorIs(t, err, osCause,
		"the os-level cause must stay reachable through the chain, not be flattened to text")
	assert.Contains(t, err.Error(), "docker",
		"the operator must still learn which backend failed first")
	assert.Contains(t, err.Error(), "os-level")
}

// TestBoundedCause_RendersTruncatedButKeepsTheChain pins the property that makes
// boundedCause the right tool and errors.Join the wrong one: the rendered text
// is bounded WHILE errors.Is still reaches the wrapped cause. Asserted directly
// on the type because the resolver tests above can only observe it indirectly —
// the real docker preflight builds its own error with no sentinel to match on,
// so "the docker cause is still reachable" is unprovable at that boundary.
func TestBoundedCause_RendersTruncatedButKeepsTheChain(t *testing.T) {
	sentinel := errors.New("docker-side sentinel")
	long := fmt.Errorf("%w: %s", sentinel, strings.Repeat("y", 900)+noisyDaemonTail)

	b := boundedCause{long}

	assert.NotContains(t, b.Error(), noisyDaemonTail, "the rendered form must be bounded")
	assert.Contains(t, b.Error(), "truncated")
	assert.ErrorIs(t, b, sentinel, "errors.Is must still reach the wrapped cause")
	assert.ErrorIs(t, b, long)

	// The property must survive one more %w wrap, which is how both resolvers
	// actually use it.
	wrapped := fmt.Errorf("--exec preflight failed: docker: %w", b)
	assert.NotContains(t, wrapped.Error(), noisyDaemonTail)
	assert.ErrorIs(t, wrapped, sentinel)
}

// TestResolveAutoFixSandbox_NeitherUsableCauseIsBounded is the same guarantee at
// the --auto-fix resolver. The two resolvers are required to behave identically
// (an operator who learns what --exec does on a Docker-less host must not find
// --auto-fix different), so the bound is asserted at both boundaries rather than
// assumed to have been applied consistently.
func TestResolveAutoFixSandbox_NeitherUsableCauseIsBounded(t *testing.T) {
	osCause := errors.New("bwrap: command not found")
	stubOSLevel(t, osCause)

	_, err := ResolveAutoFixSandbox(context.Background(), true, noisyFallbackConfig(t))

	require.Error(t, err)
	assert.NotContains(t, err.Error(), noisyDaemonTail)
	assert.Contains(t, err.Error(), "truncated")
	assert.ErrorIs(t, err, ErrSandboxNoUsableBackend)
	assert.ErrorIs(t, err, osCause)
}

// TestNeitherUsableError_KeepsTheDockerUnavailableSentinel pins the interaction
// between two independently-correct changes: boundedCause wraps the docker cause
// so its TEXT is bounded, and ErrDockerUnavailable is the sentinel that decides
// whether the fallback may engage at all. A caller asking "was Docker
// unavailable?" about a returned neither-usable error must still get an answer —
// boundedCause sits directly between it and the sentinel, and a switch to
// %v-formatting or errors.Join there would sever it silently, with nothing else
// in the suite noticing.
func TestNeitherUsableError_KeepsTheDockerUnavailableSentinel(t *testing.T) {
	stubOSLevel(t, errors.New("bwrap: command not found"))

	_, _, _, err := ResolveExecBackend(context.Background(), true, noisyFallbackConfig(t))

	require.Error(t, err)
	assert.ErrorIs(t, err, sandbox.ErrDockerUnavailable,
		"the unavailable-class sentinel must survive boundedCause on the returned error")
	assert.ErrorIs(t, err, ErrSandboxNoUsableBackend)
	assert.NotContains(t, err.Error(), noisyDaemonTail,
		"and the text must still be bounded — both properties at once is the point")
}

// TestResolveExecBackend_SingleBackendRefusalIsUnchanged is the paired negative
// control. The no-fallback refusal is a pre-existing path with ONE cause and a
// pinned message; bounding the combined path must not silently start truncating
// it. Without this, "apply truncation everywhere" would look equally green.
func TestResolveExecBackend_SingleBackendRefusalIsUnchanged(t *testing.T) {
	sc := noisyFallbackConfig(t)
	sc.Fallback = "" // no fallback configured — the untouched refusal

	_, _, _, err := ResolveExecBackend(context.Background(), true, sc)

	require.Error(t, err)
	assert.Contains(t, err.Error(), noisyDaemonTail,
		"the single-backend refusal is deliberately unchanged; truncating it here would be scope creep")
	assert.NotErrorIs(t, err, ErrSandboxNoUsableBackend)
	assert.True(t, strings.HasPrefix(err.Error(), "--exec preflight failed: "))
}
