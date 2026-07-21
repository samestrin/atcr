//go:build integration

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
