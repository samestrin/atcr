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

// fakeDockerCatStdinBody makes the fake docker CLI `cat` its stdin to stdout when
// invoked as `docker run ...`, so a test can observe the exact script body Run
// streams to `/bin/sh -s` — the only way to assert stdin content daemon-free.
func fakeDockerCatStdinBody() string {
	return `if [ "$1" = "run" ]; then
  cat
  exit 0
fi
exit 0`
}

func TestDockerBackend_Run_ScriptModeWritablePrependsCopyStep(t *testing.T) {
	// AC 03-02 Scenario 1: Writable:true Script mode prepends the fixed copy-step line
	// to the stdin script body — setup line first, then spec.Script verbatim.
	fake := writeFakeDocker(t, fakeDockerCatStdinBody())
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)

	res, err := b.Run(context.Background(), RunSpec{
		Script:      "echo hi\nexit 3\n",
		SnapshotDir: t.TempDir(),
		Writable:    true,
	})
	require.NoError(t, err)
	assert.Equal(t, "cp -R /src/. /work/ || exit 125; cd /work\necho hi\nexit 3\n", res.Output,
		"Writable:true Script mode must prepend the copy step to the stdin script body")
}

func TestDockerBackend_Run_ScriptModeReadOnlyStdinUnchanged(t *testing.T) {
	// AC 03-02 Scenario 2: Writable:false Script mode streams spec.Script verbatim over
	// stdin — no cp/cd prefix, identical to current behavior (regression anchor).
	fake := writeFakeDocker(t, fakeDockerCatStdinBody())
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)

	res, err := b.Run(context.Background(), RunSpec{
		Script:      "echo hi\nexit 3\n",
		SnapshotDir: t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, "echo hi\nexit 3\n", res.Output,
		"Writable:false Script mode stdin must be the script body verbatim")
}

func TestRenderCommand_UnaffectedByWritable(t *testing.T) {
	// AC 02-03 Scenario 3: renderCommand is display-only evidence — it renders the
	// caller's ORIGINAL command/script, never the injected cp -R setup step, for both
	// Writable values, so the evidence trail stays readable.
	cmd := RunSpec{Command: []string{"npm", "run", "build"}, SnapshotDir: "/tmp/snap"}
	cmdW := cmd
	cmdW.Writable = true
	assert.Equal(t, "npm run build", renderCommand(cmd))
	assert.Equal(t, "npm run build", renderCommand(cmdW), "Writable must not leak the setup step into command evidence")
	assert.NotContains(t, renderCommand(cmdW), "cp -R")

	scr := RunSpec{Script: "npm test\n", SnapshotDir: "/tmp/snap"}
	scrW := scr
	scrW.Writable = true
	want := "/bin/sh -s <<'EOF'\nnpm test\n\nEOF"
	assert.Equal(t, want, renderCommand(scr))
	assert.Equal(t, want, renderCommand(scrW), "Writable must not alter the Script-mode heredoc evidence")
	assert.NotContains(t, renderCommand(scrW), "cp -R")
}

func TestDockerBackend_Run_WritableOverlayWriteProof(t *testing.T) {
	// AC 02-03 Scenarios 4-5: prove the Writable:true Run path is reachable and
	// functional end-to-end (not merely present in argv) for BOTH Command and Script
	// modes, and that the snapshot side stays read-only. The fake docker shim stands in
	// for the container: it first refuses to proceed unless Run actually built the
	// Writable overlay argv (/src:ro bind + /work tmpfs), then emulates the payload's
	// write under the writable /work overlay by creating a marker file.
	workMarker := filepath.Join(t.TempDir(), "writable-overlay-marker.txt")
	t.Setenv("ATCR_WORK_MARKER", workMarker)

	fake := writeFakeDocker(t, `if [ "$1" = "run" ]; then
  case "$*" in
    *:/src:ro*) : ;;
    *) echo "missing /src:ro bind" >&2; exit 90 ;;
  esac
  case "$*" in
    *"--tmpfs /work:rw"*) : ;;
    *) echo "missing /work tmpfs" >&2; exit 90 ;;
  esac
  echo built > "$ATCR_WORK_MARKER" || exit 91
  exit 0
fi
exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)

	cases := []struct {
		name string
		spec RunSpec
	}{
		{"command mode", RunSpec{Command: []string{"npm", "run", "build"}, SnapshotDir: t.TempDir(), Writable: true}},
		{"script mode", RunSpec{Script: "mkdir -p target && echo built > target/out\n", SnapshotDir: t.TempDir(), Writable: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(workMarker) // reset between modes; absent on first iteration
			res, err := b.Run(context.Background(), tc.spec)
			require.NoError(t, err, "a reachable Writable:true run is not a backend fault")
			require.Equal(t, 0, res.ExitCode, "the Writable:true payload must succeed (ExitCode 0, no EROFS); shim exits 90-92 on a broken overlay")
			data, rerr := os.ReadFile(workMarker)
			require.NoError(t, rerr, "the payload's write under the writable /work overlay must be observable, proving the mount path is functional")
			assert.Equal(t, "built\n", string(data))
		})
	}
}

func TestDockerBackend_Run_WritableOverlaySrcIsReadOnly(t *testing.T) {
	// Verifies that Writable:true mounts SnapshotDir at /src with :ro read-only flag.
	fake := writeFakeDocker(t, `if [ "$1" = "run" ]; then
  case "$*" in
    *:/src:ro*) exit 0 ;;
    *) echo "missing /src:ro bind" >&2; exit 1 ;;
  esac
fi
exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)

	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"touch", "/src/foo"},
		SnapshotDir: t.TempDir(),
		Writable:    true,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
}

func TestDefaultDockerConfig_WorkSizeDefault(t *testing.T) {
	cfg := DefaultDockerConfig()
	// WorkSize backs the writable /work overlay's tmpfs; it must have a sane
	// non-empty default sized for a full source-tree copy, strictly larger than
	// ScratchSize's build-cache-sized "64m".
	assert.NotEmpty(t, cfg.WorkSize, "WorkSize must have a sane default")
	work, err := parseDockerMemory(cfg.WorkSize)
	require.NoError(t, err, "WorkSize must be a valid docker size string")
	scratch, err := parseDockerMemory(cfg.ScratchSize)
	require.NoError(t, err, "ScratchSize must be a valid docker size string")
	assert.Greater(t, work, scratch, "WorkSize must be larger than ScratchSize for a full source-tree copy")
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

// TestDockerBackend_Run_CancellationClassIsTimedOut pins that BOTH cancellation-class
// context ends of a Run() — a deadline exceeded AND a parent-context cancellation —
// are folded into a TimedOut RunResult (exit 124, nil error), never a spurious
// program exit or backend fault/StartError. It mirrors the host path's
// belt-and-suspenders handling in localvalidate.go:127 and is the Run()-level
// counterpart to TestDockerBackend_DockerCmd_ContextCancelNotTimeout above (which
// pins the DIFFERENT dockerCmd/Preflight error-string path, unchanged here). The
// "deadline exceeded" row already passed before epic 32.2 Task 2; the "context
// canceled" row is the one the fold added (AC3).
func TestDockerBackend_Run_CancellationClassIsTimedOut(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T) context.Context
		timeout time.Duration
	}{
		{
			// A short RunSpec timeout: the backend's own deadline fires first.
			name:    "deadline exceeded",
			setup:   func(*testing.T) context.Context { return context.Background() },
			timeout: 150 * time.Millisecond,
		},
		{
			// A long RunSpec timeout but the parent context is cancelled mid-run, so
			// runCtx.Err() is context.Canceled (not DeadlineExceeded).
			name: "context canceled",
			setup: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				go func() {
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()
				return ctx
			},
			timeout: 5 * time.Second,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The fake answers `kill` instantly so the on-timeout container reap does
			// not itself block on the sleeping `run`.
			fake := writeFakeDocker(t, `if [ "$1" = "kill" ]; then exit 0; fi
if [ "$1" = "run" ]; then sleep 5; fi
exit 0`)
			cfg := DefaultDockerConfig()
			cfg.DockerPath = fake
			b := NewDockerBackend(cfg)

			res, err := b.Run(tc.setup(t), RunSpec{
				Command:     []string{"x"},
				SnapshotDir: t.TempDir(),
				Timeout:     tc.timeout,
			})
			require.NoError(t, err, "a cancellation-class run end must not surface as a backend fault/StartError")
			require.True(t, res.TimedOut, "deadline OR cancellation must be folded into TimedOut")
			require.Equal(t, timeoutExitCode, res.ExitCode, "the timeout exit code (124) must be set, not a signal-death code")
		})
	}
}
