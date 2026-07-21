package sandbox

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/log"
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

func TestDockerRunArgs_WritableTempEnv(t *testing.T) {
	// go test (and most runners) must be able to write a build cache / temp under
	// the read-only rootfs; these env vars point them at the writable /scratch.
	args, err := dockerRunArgs(DefaultDockerConfig(), RunSpec{Command: []string{"go", "test"}, SnapshotDir: "/tmp/snap"})
	require.NoError(t, err)
	joined := strings.Join(args, " ")
	assert.Contains(t, joined, "HOME=/scratch")
	assert.Contains(t, joined, "TMPDIR=/scratch")
	assert.Contains(t, joined, "GOCACHE=/scratch")
}

func TestNewDockerBackend_ConcurrencyCap(t *testing.T) {
	cfg := DefaultDockerConfig()
	cfg.MaxConcurrent = 3
	assert.Equal(t, 3, cap(NewDockerBackend(cfg).sem), "the semaphore must bound concurrency to MaxConcurrent")
	// An unset/zero MaxConcurrent gets a positive default (never an unbounded backend).
	assert.Positive(t, cap(NewDockerBackend(DockerConfig{}).sem))
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

// assertAdjacent asserts that flag appears in args immediately followed by value
// as two distinct argv elements, so a malformed single-token "flag value" mount
// cannot satisfy a mere substring check.
func assertAdjacent(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return
		}
	}
	t.Fatalf("expected argv to contain adjacent elements %q %q, got %v", flag, value, args)
}

func TestDockerRunArgs_WritableFalseGoldenArgv(t *testing.T) {
	// AC 02-01 Scenario 3: the Writable:false (default) argv must equal the
	// pre-story argv element-for-element — same length, order, and values — so a
	// future edit to the false branch that drifts the mount is caught as an exact
	// slice mismatch, not merely a substring miss. This anchors --exec's read-only
	// guarantee: Writable defaults false, and this path stays byte-identical.
	cfg := DefaultDockerConfig()
	spec := RunSpec{Command: []string{"go", "test", "./..."}, SnapshotDir: "/tmp/snap"}

	args, err := dockerRunArgs(cfg, spec)
	require.NoError(t, err)

	want := []string{
		"run", "--rm",
		"--network", "none",
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--user", cfg.User,
		"--memory", cfg.Memory,
		"--cpus", cfg.CPUs,
		"--pids-limit", strconv.Itoa(cfg.PidsLimit),
		"--tmpfs", "/scratch:rw,exec,size=" + cfg.ScratchSize,
		"-e", "HOME=/scratch",
		"-e", "TMPDIR=/scratch",
		"-e", "XDG_CACHE_HOME=/scratch/.cache",
		"-e", "GOCACHE=/scratch/.gocache",
		"-e", "GOTMPDIR=/scratch",
		"--workdir", "/work",
		"-v", "/tmp/snap:/work:ro",
		cfg.Image,
		"go", "test", "./...",
	}
	assert.Equal(t, want, args, "Writable:false argv must be byte-identical to the pre-story shape")
}

func TestDockerRunArgs_WritableTrueMountsSrcROAndWorkTmpfs(t *testing.T) {
	// AC 02-02: Writable:true mounts the snapshot read-only at /src (not /work) and
	// adds a writable /work tmpfs, mirroring the /scratch tmpfs pattern exactly.
	cfg := DefaultDockerConfig()
	cfg.WorkSize = "128m"
	spec := RunSpec{Command: []string{"npm", "run", "build"}, SnapshotDir: "/tmp/snap", Writable: true}

	args, err := dockerRunArgs(cfg, spec)
	require.NoError(t, err)
	joined := strings.Join(args, " ")

	// Snapshot is read-only at /src, and the old /work:ro bind form is gone.
	assert.Contains(t, joined, "/tmp/snap:/src:ro", "snapshot must be mounted read-only at /src")
	assert.NotContains(t, joined, "/tmp/snap:/work:ro", "the /work:ro bind must not survive in the writable path")

	// A real writable /work tmpfs backs the overlay under the unchanged --read-only rootfs.
	assert.Contains(t, joined, "--read-only", "the global read-only rootfs flag stays present")
	assert.Contains(t, joined, "--tmpfs /work:rw,exec,size=128m", "/work must be a writable tmpfs sized by cfg.WorkSize")

	// The pre-existing /scratch tmpfs and --workdir /work are untouched and additive.
	assert.Contains(t, joined, "--tmpfs /scratch:rw,exec,size="+cfg.ScratchSize, "the /scratch tmpfs stays unchanged")
	assert.Contains(t, joined, "--workdir /work", "--workdir still targets the writable /work")

	// Element-form: each mount spec is two adjacent argv elements, so a malformed
	// single-token mount cannot pass. The /src bind takes the vacated /work:ro slot.
	assertAdjacent(t, args, "-v", "/tmp/snap:/src:ro")
	assertAdjacent(t, args, "--tmpfs", "/work:rw,exec,size=128m")
}

func TestDockerRunArgs_WritableTrueEmptyWorkSize(t *testing.T) {
	// AC 02-02 Edge Case 3: an empty cfg.WorkSize (a caller bypassing
	// DefaultDockerConfig) must not panic or error at argv-build time — Docker
	// rejects the malformed flag at run time, consistent with "no new validation".
	cfg := DefaultDockerConfig()
	cfg.WorkSize = ""
	spec := RunSpec{Command: []string{"true"}, SnapshotDir: "/tmp/snap", Writable: true}

	args, err := dockerRunArgs(cfg, spec)
	require.NoError(t, err, "empty WorkSize must not error at argv-build time")
	assert.Contains(t, strings.Join(args, " "), "--tmpfs /work:rw,exec,size=", "emits an empty size, not a crash")
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

func TestRunSpec_WritableDefaultsToFalse(t *testing.T) {
	// The opt-in writable overlay must default off: a zero-value RunSpec is
	// read-only, matching every existing caller's current behavior.
	assert.False(t, RunSpec{}.Writable, "Writable zero value must be false (read-only default)")

	// An explicit opt-in round-trips and does not interact with validate()'s
	// exactly-one-of-Command/Script or SnapshotDir invariants.
	spec := RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir(), Writable: true}
	assert.True(t, spec.Writable, "explicit Writable:true must round-trip")
	assert.NoError(t, spec.validate(), "Writable:true must not affect validate()")
	spec.Writable = false
	assert.NoError(t, spec.validate(), "Writable:false must not affect validate()")
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
	// Fake docker that succeeds for version, image inspect, info, and the trivial
	// run. `info` reports a generous host so the cap-fit check passes.
	fake := writeFakeDocker(t, `if [ "$1" = "info" ]; then
  echo '{"MemTotal": 8589934592, "NCPU": 8}'
  exit 0
fi
exit 0`)
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

func TestTruncate_ReservesMarkerSpaceAndReportsCorrectDrop(t *testing.T) {
	s := strings.Repeat("a", 100)
	limit := 20
	result := truncate(s, limit)
	assert.LessOrEqual(t, len(result), limit, "truncated result must not exceed limit")
	assert.Contains(t, result, "truncated")

	// Multibyte rune at the boundary must not be split.
	s2 := strings.Repeat("é", 50) // 2 bytes each
	result2 := truncate(s2, 21)
	assert.LessOrEqual(t, len(result2), 21)
	assert.True(t, utf8.ValidString(result2), "result must be valid UTF-8")
}

func TestDockerBackendRun_EmitsAuditLog(t *testing.T) {
	fake := writeFakeDocker(t, `echo "ok"; exit 0`)
	cfg := DefaultDockerConfig()
	cfg.DockerPath = fake
	b := NewDockerBackend(cfg)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx := log.NewContext(context.Background(), logger)

	_, err := b.Run(ctx, RunSpec{Command: []string{"go", "test"}, SnapshotDir: t.TempDir()})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "sandbox exec")
	assert.Contains(t, out, "command=\"go test\"")
	assert.Contains(t, out, "exit_code=0")
	assert.Contains(t, out, "backend=docker")
}
