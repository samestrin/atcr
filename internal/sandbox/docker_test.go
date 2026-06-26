package sandbox

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDockerExitBody returns a shell body for writeFakeDocker that exits with
// the code stored in DOCKER_EXIT_CODE when invoked as `docker run ...`.
func fakeDockerExitBody() string {
	return `if [ "$1" = "run" ]; then
  if [ -n "$DOCKER_EXIT_CODE" ]; then
    echo "fake docker exit $DOCKER_EXIT_CODE" >&2
    exit "$DOCKER_EXIT_CODE"
  fi
  exit 0
fi
exit 0`
}

func TestDockerBackendRun_RuntimeExitCodesAreBackendErrors(t *testing.T) {
	fakeDocker := writeFakeDocker(t, fakeDockerExitBody())
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fakeDocker
	cfg.MaxConcurrent = 1
	b := NewDockerBackend(cfg)

	spec := RunSpec{
		Command:     []string{"true"},
		SnapshotDir: t.TempDir(),
	}

	for _, code := range []int{125, 126, 127} {
		t.Run(fmt.Sprintf("exit-%d", code), func(t *testing.T) {
			t.Setenv("DOCKER_EXIT_CODE", fmt.Sprintf("%d", code))
			_, err := b.Run(context.Background(), spec)
			if err == nil {
				t.Fatalf("exit %d: expected backend error, got nil", code)
			}
			if !strings.Contains(err.Error(), "runtime error") {
				t.Fatalf("exit %d: expected error to mention runtime error, got %q", code, err.Error())
			}
		})
	}
}

func TestDockerBackend_Preflight_CatchesInvalidCPUs(t *testing.T) {
	// Fake docker that fails the `run` subcommand only when it sees `--cpus abc`.
	fake := writeFakeDocker(t, `if [ "$1" = "run" ]; then
  found=0
  for arg in "$@"; do
    if [ "$arg" = "--cpus" ]; then found=1; fi
    if [ "$found" = "1" ] && [ "$arg" = "abc" ]; then exit 1; fi
  done
fi
exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	cfg.CPUs = "abc"
	b := NewDockerBackend(cfg)
	err := b.Preflight(context.Background())
	require.Error(t, err, "preflight must exercise the real docker run args so invalid caps fail fast")
	assert.Contains(t, err.Error(), "preflight")
}

func TestDockerBackendRun_SignalDeathsAreBackendErrors(t *testing.T) {
	fakeDocker := writeFakeDocker(t, fakeDockerExitBody())
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fakeDocker
	cfg.MaxConcurrent = 1
	b := NewDockerBackend(cfg)

	spec := RunSpec{
		Command:     []string{"true"},
		SnapshotDir: t.TempDir(),
	}

	for _, code := range []int{137, 139} {
		t.Run(fmt.Sprintf("exit-%d", code), func(t *testing.T) {
			t.Setenv("DOCKER_EXIT_CODE", fmt.Sprintf("%d", code))
			_, err := b.Run(context.Background(), spec)
			if err == nil {
				t.Fatalf("exit %d: expected backend error, got nil", code)
			}
			if !strings.Contains(err.Error(), "killed by signal") {
				t.Fatalf("exit %d: expected error to mention signal death, got %q", code, err.Error())
			}
		})
	}
}

func TestDockerBackendRun_WorkloadExitCodesAreResults(t *testing.T) {
	fakeDocker := writeFakeDocker(t, fakeDockerExitBody())
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fakeDocker
	cfg.MaxConcurrent = 1
	b := NewDockerBackend(cfg)

	spec := RunSpec{
		Command:     []string{"false"},
		SnapshotDir: t.TempDir(),
	}

	for _, tc := range []struct {
		code int
		want int
	}{
		{0, 0},
		{1, 1},
		{42, 42},
	} {
		t.Run(fmt.Sprintf("exit-%d", tc.code), func(t *testing.T) {
			t.Setenv("DOCKER_EXIT_CODE", fmt.Sprintf("%d", tc.code))
			res, err := b.Run(context.Background(), spec)
			if err != nil {
				t.Fatalf("exit %d: expected nil error, got %v", tc.code, err)
			}
			if res.ExitCode != tc.want {
				t.Fatalf("exit %d: expected ExitCode %d, got %d", tc.code, tc.want, res.ExitCode)
			}
		})
	}
}

func TestDockerBackend_StructLiteral_AppliesConcurrencyCap(t *testing.T) {
	fakeDocker := writeFakeDocker(t, fakeDockerExitBody())
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fakeDocker
	// Build the backend as a struct literal, bypassing NewDockerBackend, so sem
	// starts nil. The Critical-rated resource-abuse mitigation (MaxConcurrent) must
	// still apply rather than failing open.
	b := &DockerBackend{cfg: cfg}
	spec := RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir()}

	_, err := b.Run(context.Background(), spec)
	require.NoError(t, err)
	require.NotNil(t, b.sem, "a struct-literal backend must still enforce the concurrency cap, not fail open")
	assert.Equal(t, cfg.MaxConcurrent, cap(b.sem), "the live cap must match the configured MaxConcurrent")
}

func TestDockerBackend_DockerCmd_ContextCancelNotTimeout(t *testing.T) {
	fake := writeFakeDocker(t, `sleep 5; exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := b.dockerCmd(ctx, 5*time.Second, "version")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
	assert.NotContains(t, err.Error(), "timed out", "cancellations must not be reported as timeouts")
}
