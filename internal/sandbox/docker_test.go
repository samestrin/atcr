package sandbox

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
