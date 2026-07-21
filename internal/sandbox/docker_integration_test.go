//go:build integration

package sandbox

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_DockerBackend_WritableOverlayWorkIsWritable is the daemon-backed
// end-to-end proof that the fake-shim unit test cannot be (see the note on
// TestDockerBackend_Run_WritableOverlayArgvShapeReachable): against a REAL tmpfs it
// proves, for both Command and Script modes, that the non-root sandbox user can write
// into the ephemeral /work overlay — the payload writes a file and reads it back, so a
// non-writable /work (the mode=0755-vs-1777 daemon default, EROFS, empty mount) would
// fail the run instead of silently passing. It is the regression anchor for the
// mode=1777 tmpfs fix and the /src-read-only EROFS sibling's writable counterpart.
func TestIntegration_DockerBackend_WritableOverlayWorkIsWritable(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not available in PATH")
	}

	cfg := DefaultDockerConfig()
	b := NewDockerBackend(cfg)

	cases := []struct {
		name string
		spec RunSpec
	}{
		{
			// Command mode: cwd is /work after the seed+cd setup. The non-root sandbox
			// user writes into the ephemeral /work tmpfs and reads it back — impossible
			// unless /work is genuinely writable (mode=1777).
			name: "command mode",
			spec: RunSpec{
				Command:     []string{"sh", "-c", "echo written > /work/proof.txt && cat /work/proof.txt"},
				SnapshotDir: t.TempDir(),
				Writable:    true,
			},
		},
		{
			// Script mode: the copy step is prepended to the stdin body, then the script
			// runs from /work — same write+readback proof over the Script path.
			name: "script mode",
			spec: RunSpec{
				Script:      "echo written > /work/proof.txt\ncat /work/proof.txt\n",
				SnapshotDir: t.TempDir(),
				Writable:    true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := b.Run(context.Background(), tc.spec)
			require.NoError(t, err, "a reachable Writable:true run must not be a backend fault")
			require.Equal(t, 0, res.ExitCode, "the non-root sandbox user must write into /work (mode=1777); output: %s", res.Output)
			assert.Contains(t, res.Output, "written", "the payload's write into the ephemeral /work tmpfs must be observable end-to-end")
		})
	}
}

func TestIntegration_DockerBackend_WritableOverlaySrcIsReadOnlyEROFS(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker binary not available in PATH")
	}

	cfg := DefaultDockerConfig()
	b := NewDockerBackend(cfg)

	spec := RunSpec{
		Command:     []string{"sh", "-c", "touch /src/cant-write-here 2>&1"},
		SnapshotDir: t.TempDir(),
		Writable:    true,
	}

	res, err := b.Run(context.Background(), spec)
	require.NoError(t, err, "docker run execution should complete without backend error")
	assert.NotEqual(t, 0, res.ExitCode, "writing to /src should fail because /src is mounted read-only")
	assert.Contains(t, res.Output, "Read-only file system", "error output should report EROFS")
}
