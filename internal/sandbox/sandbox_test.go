package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeDocker writes an executable shell script that impersonates the docker
// CLI so Run/Preflight can be exercised deterministically without a real daemon.
// The script's behavior is driven by the first non-flag argument it sees after
// "run"/"image"/"version" and by env vars the test sets.
func writeFakeDocker(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-docker shell shim is POSIX-only")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "docker")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755))
	return p
}

func TestDockerRunArgs_HardeningFlagsPresent(t *testing.T) {
	cfg := DefaultDockerConfig()
	spec := RunSpec{Command: []string{"go", "test", "./..."}, SnapshotDir: "/tmp/snap"}

	args, err := dockerRunArgs(cfg, spec)
	require.NoError(t, err)
	joined := strings.Join(args, " ")

	// Network isolation, read-only rootfs, dropped caps, no-new-privileges.
	assert.Contains(t, joined, "--network none", "must isolate network")
	assert.Contains(t, joined, "--read-only", "rootfs must be read-only")
	assert.Contains(t, joined, "--cap-drop ALL", "all capabilities must be dropped")
	assert.Contains(t, joined, "--security-opt no-new-privileges")
	// Non-root.
	assert.Contains(t, joined, "--user "+cfg.User)
	// Resource caps.
	assert.Contains(t, joined, "--memory "+cfg.Memory)
	assert.Contains(t, joined, "--cpus "+cfg.CPUs)
	assert.Contains(t, joined, "--pids-limit")
	// Snapshot mounted READ-ONLY at /work.
	assert.Contains(t, joined, "/tmp/snap:/work:ro", "snapshot must be a read-only mount")
	// Writable scratch is a tmpfs, not a host mount.
	assert.Contains(t, joined, "--tmpfs /scratch")
	// Ephemeral container.
	assert.Contains(t, joined, "run --rm")
	// The argv command is passed through verbatim after the image.
	assert.Contains(t, joined, "go test ./...")
}

func TestDockerRunArgs_ScriptUsesStdinShell(t *testing.T) {
	cfg := DefaultDockerConfig()
	args, err := dockerRunArgs(cfg, RunSpec{Script: "echo hi\nexit 3\n", SnapshotDir: "/tmp/snap"})
	require.NoError(t, err)
	joined := strings.Join(args, " ")
	// A script body is fed over stdin to `sh -s` — never interpolated into argv.
	assert.Contains(t, joined, "-i")
	assert.Contains(t, joined, "/bin/sh -s")
	assert.NotContains(t, joined, "echo hi", "script body must not appear in argv")
}

func TestRunSpec_Validate(t *testing.T) {
	cases := []struct {
		name string
		spec RunSpec
		ok   bool
	}{
		{"command only", RunSpec{Command: []string{"true"}, SnapshotDir: "/s"}, true},
		{"script only", RunSpec{Script: "true", SnapshotDir: "/s"}, true},
		{"neither", RunSpec{SnapshotDir: "/s"}, false},
		{"both", RunSpec{Command: []string{"true"}, Script: "true", SnapshotDir: "/s"}, false},
		{"no snapshot", RunSpec{Command: []string{"true"}}, false},
		{"relative snapshot", RunSpec{Command: []string{"true"}, SnapshotDir: "rel/dir"}, false},
		{"colon in snapshot (mount injection)", RunSpec{Command: []string{"true"}, SnapshotDir: "/s:ro,z"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.validate()
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestDockerBackend_Name(t *testing.T) {
	b := NewDockerBackend(DefaultDockerConfig())
	assert.Equal(t, "docker", b.Name())
}

func TestDockerBackend_Run_CapturesExitCodeAndOutput(t *testing.T) {
	// Fake docker: print the marker then exit with the code encoded in an env var.
	fake := writeFakeDocker(t, `echo "sandbox-stdout-marker"; exit ${FAKE_EXIT:-0}`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake

	t.Setenv("FAKE_EXIT", "7")
	b := NewDockerBackend(cfg)
	res, err := b.Run(context.Background(), RunSpec{Command: []string{"go", "test"}, SnapshotDir: t.TempDir()})
	require.NoError(t, err, "a non-zero program exit is NOT a backend error")
	assert.Equal(t, 7, res.ExitCode)
	assert.Contains(t, res.Output, "sandbox-stdout-marker")
	assert.False(t, res.TimedOut)
}

func TestDockerBackend_Run_TruncatesOutput(t *testing.T) {
	fake := writeFakeDocker(t, `head -c 100000 /dev/zero | tr '\0' 'x'; exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	cfg.MaxOutputBytes = 1024
	b := NewDockerBackend(cfg)
	res, err := b.Run(context.Background(), RunSpec{Command: []string{"x"}, SnapshotDir: t.TempDir()})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(res.Output), 1024+64, "output must be truncated to the budget")
	assert.Contains(t, res.Output, "truncated")
}

func TestDockerBackend_Run_Timeout(t *testing.T) {
	fake := writeFakeDocker(t, `sleep 5; exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)
	res, err := b.Run(context.Background(), RunSpec{
		Command:     []string{"x"},
		SnapshotDir: t.TempDir(),
		Timeout:     150 * time.Millisecond,
	})
	require.NoError(t, err)
	assert.True(t, res.TimedOut, "an over-budget run must be flagged TimedOut")
}

func TestDockerBackend_Preflight_OK(t *testing.T) {
	// Fake docker that succeeds for version, image inspect, and the trivial run.
	fake := writeFakeDocker(t, `exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)
	assert.NoError(t, b.Preflight(context.Background()))
}

func TestDockerBackend_Preflight_DaemonDown(t *testing.T) {
	// Fake docker whose `version`/`info` call fails => daemon unreachable.
	fake := writeFakeDocker(t, `case "$1" in version|info) exit 1;; esac; exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)
	err := b.Preflight(context.Background())
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "daemon")
}

func TestDockerBackend_Preflight_MissingBinary(t *testing.T) {
	cfg := DefaultDockerConfig()
	cfg.DockerPath = "/nonexistent/docker-binary-xyz"
	b := NewDockerBackend(cfg)
	assert.Error(t, b.Preflight(context.Background()))
}
