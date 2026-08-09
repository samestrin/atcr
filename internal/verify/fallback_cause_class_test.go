package verify

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// daemonDownShim fails `docker version` — the daemon-unreachable class, and the
// only class that means "Docker is unavailable on this host".
func daemonDownShim(t *testing.T) *registry.SandboxConfig {
	t.Helper()
	return &registry.SandboxConfig{
		DockerPath:  fakeDocker(t, `echo "Cannot connect to the Docker daemon at unix:///var/run/docker.sock." >&2; exit 1`),
		Image:       "golang:1.25",
		TestCommand: []string{"go", "test", "./..."},
		Fallback:    registry.SandboxFallbackOSLevel,
	}
}

// configFaultShim answers `docker version` successfully — the daemon IS up — and
// fails only the later `image inspect` step. That is an operator configuration
// fault (the declared base image is not present locally), NOT an unavailable
// Docker, so it must be a hard refusal rather than a silent downgrade to a
// backend that enforces none of the caps the operator wrote down.
func configFaultShim(t *testing.T) *registry.SandboxConfig {
	t.Helper()
	return &registry.SandboxConfig{
		DockerPath: fakeDocker(t, `case "$1" in
  version) exit 0 ;;
  *) echo "Error: No such image: golang:1.25" >&2; exit 1 ;;
esac`),
		Image:       "golang:1.25",
		TestCommand: []string{"go", "test", "./..."},
		Fallback:    registry.SandboxFallbackOSLevel,
	}
}

// TestResolveExecBackend_FallbackOnlyOnDockerUnavailable pins WHICH docker
// preflight failures may engage the os-level fallback.
//
// Both resolvers entered the fallback on ANY non-nil DockerBackend.Preflight
// error. But Preflight fails on several classes that do not mean "Docker is
// unavailable": the base image being absent locally, validateHostCaps rejecting
// the operator's memory/cpus against the host, an invalid scratch_size/work_size,
// and the trivial hardened container failing to run. Downgrading on those is the
// wrong direction — an operator who just TIGHTENED a cap silently got a backend
// that enforces no caps at all, which is the opposite of what they asked for.
func TestResolveExecBackend_FallbackOnlyOnDockerUnavailable(t *testing.T) {
	t.Run("daemon unreachable engages the fallback", func(t *testing.T) {
		_, _, calls := stubOSLevel(t, nil)

		b, _, _, err := ResolveExecBackend(context.Background(), true, daemonDownShim(t))

		require.NoError(t, err, "an unreachable daemon is exactly what the fallback exists for")
		require.NotNil(t, b)
		assert.Equal(t, 1, *calls)
	})

	t.Run("a config fault is a hard refusal, not a downgrade", func(t *testing.T) {
		_, _, calls := stubOSLevel(t, nil)

		b, _, _, err := ResolveExecBackend(context.Background(), true, configFaultShim(t))

		require.Error(t, err, "a missing base image is an operator config fault, not an unavailable Docker")
		assert.Nil(t, b)
		assert.Equal(t, 0, *calls,
			"the os-level backend must never even be constructed for a configuration fault")
		assert.NotErrorIs(t, err, ErrSandboxNoUsableBackend,
			"no fallback was attempted, so this is the single-backend refusal, not the combined one")
	})
}

// TestResolveAutoFixSandbox_FallbackOnlyOnDockerUnavailable is the same rule at
// the --auto-fix resolver. The two are required to behave identically, so the
// classification is asserted at both boundaries rather than assumed shared.
func TestResolveAutoFixSandbox_FallbackOnlyOnDockerUnavailable(t *testing.T) {
	t.Run("daemon unreachable engages the fallback", func(t *testing.T) {
		_, _, calls := stubOSLevel(t, nil)

		b, err := ResolveAutoFixSandbox(context.Background(), true, daemonDownShim(t))

		require.NoError(t, err)
		require.NotNil(t, b)
		assert.Equal(t, 1, *calls)
	})

	t.Run("a config fault is a hard refusal, not a downgrade", func(t *testing.T) {
		_, _, calls := stubOSLevel(t, nil)

		b, err := ResolveAutoFixSandbox(context.Background(), true, configFaultShim(t))

		require.Error(t, err)
		assert.Nil(t, b)
		assert.Equal(t, 0, *calls)
	})
}

// TestErrDockerUnavailable_MarksOnlyTheUnavailableClass asserts the sentinel at
// its source. The resolver tests above observe the CONSEQUENCE; this pins the
// classification itself, so a future Preflight step added in the wrong place
// (wrapping a config fault as "unavailable") fails here with a clear cause
// rather than as a puzzling downgrade two packages away.
func TestErrDockerUnavailable_MarksOnlyTheUnavailableClass(t *testing.T) {
	down := sandbox.NewDockerBackend(dockerCfgFor(t, daemonDownShim(t)))
	err := down.Preflight(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, sandbox.ErrDockerUnavailable)

	faulty := sandbox.NewDockerBackend(dockerCfgFor(t, configFaultShim(t)))
	err = faulty.Preflight(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, sandbox.ErrDockerUnavailable,
		"a missing base image is a configuration fault; marking it unavailable would re-open the downgrade")
}

// dockerCfgFor builds the DockerConfig the resolver would build for sc, so the
// sentinel test exercises the same construction path.
func dockerCfgFor(t *testing.T, sc *registry.SandboxConfig) sandbox.DockerConfig {
	t.Helper()
	cfg := sandbox.DefaultDockerConfig()
	cfg.DockerPath = sc.DockerPath
	cfg.Image = sc.Image
	return cfg
}
