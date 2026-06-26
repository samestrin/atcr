package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	// It answers `info` with a generous host so the cap-fit check (which skips the
	// non-numeric "abc" value) passes and the run step is the one that rejects it.
	fake := writeFakeDocker(t, `if [ "$1" = "info" ]; then
  echo '{"MemTotal": 8589934592, "NCPU": 8}'
  exit 0
fi
if [ "$1" = "run" ]; then
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

// fakeDockerInfoBody answers `docker info` with the given MemTotal (bytes) and
// NCPU, and exits 0 for every other subcommand (version, image inspect, run).
func fakeDockerInfoBody(memTotal int64, ncpu int) string {
	return fmt.Sprintf(`if [ "$1" = "info" ]; then
  echo '{"MemTotal": %d, "NCPU": %d}'
  exit 0
fi
exit 0`, memTotal, ncpu)
}

func TestDockerBackend_Preflight_RejectsMemoryExceedingHost(t *testing.T) {
	fake := writeFakeDocker(t, fakeDockerInfoBody(1<<30, 2)) // 1 GiB, 2 CPUs
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	cfg.Memory = "2g" // exceeds the 1 GiB host
	b := NewDockerBackend(cfg)
	err := b.Preflight(context.Background())
	require.Error(t, err, "configured memory above host MemTotal must fail preflight")
	assert.Contains(t, err.Error(), "memory")
}

func TestDockerBackend_Preflight_RejectsCPUsExceedingHost(t *testing.T) {
	fake := writeFakeDocker(t, fakeDockerInfoBody(8<<30, 2)) // 8 GiB, 2 CPUs
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	cfg.CPUs = "4" // exceeds the 2-CPU host
	b := NewDockerBackend(cfg)
	err := b.Preflight(context.Background())
	require.Error(t, err, "configured cpus above host NCPU must fail preflight")
	assert.Contains(t, err.Error(), "cpus")
}

func TestDockerBackend_Preflight_AcceptsCapsWithinHost(t *testing.T) {
	fake := writeFakeDocker(t, fakeDockerInfoBody(8<<30, 8)) // 8 GiB, 8 CPUs
	cfg := DefaultDockerConfig()                             // 512m / 1.0 cpus — within host
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)
	require.NoError(t, b.Preflight(context.Background()), "caps within host must pass preflight")
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

func TestDockerBackend_Run_TimeoutKillsContainer(t *testing.T) {
	// exec.CommandContext only SIGKILLs the `docker run` CLI on timeout, not the
	// container the daemon runs, so the run must additionally `docker kill` the
	// named container to reclaim its caps. The fake records the kill target.
	marker := filepath.Join(t.TempDir(), "killed")
	t.Setenv("ATCR_KILL_MARKER", marker)
	fake := writeFakeDocker(t, `if [ "$1" = "kill" ]; then
  echo "$2" > "$ATCR_KILL_MARKER"
  exit 0
fi
if [ "$1" = "run" ]; then
  sleep 5
fi
exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"x"},
		SnapshotDir: t.TempDir(),
		Timeout:     150 * time.Millisecond,
	})
	require.NoError(t, err)
	require.True(t, res.TimedOut, "the over-budget run must be flagged TimedOut")

	data, rerr := os.ReadFile(marker)
	require.NoError(t, rerr, "docker kill must be invoked on timeout to reclaim the container")
	assert.Contains(t, string(data), "atcr-sbx-", "kill must target the run's named container")
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
