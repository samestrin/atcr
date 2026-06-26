package verify

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeDocker(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-docker shim is POSIX-only")
	}
	p := filepath.Join(t.TempDir(), "docker")
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755))
	return p
}

func TestResolveExecBackend_DisabledIsNoOp(t *testing.T) {
	b, cmd, _, err := ResolveExecBackend(context.Background(), false, &registry.SandboxConfig{TestCommand: []string{"go", "test"}})
	require.NoError(t, err)
	assert.Nil(t, b, "execution off must return a nil backend")
	assert.Nil(t, cmd)
}

func TestResolveExecBackend_RefusesWithoutBackend(t *testing.T) {
	// SC-1: --exec with no sandbox block hard-errors without executing anything.
	_, _, _, err := ResolveExecBackend(context.Background(), true, nil)
	require.ErrorIs(t, err, ErrExecNoBackend)
}

func TestResolveExecBackend_BuildsAndPreflights(t *testing.T) {
	sc := &registry.SandboxConfig{
		Backend: "docker",
		// version/inspect/run succeed; info reports a generous host so the cap-fit
		// check passes.
		DockerPath: fakeDocker(t, `if [ "$1" = "info" ]; then
  echo '{"MemTotal": 8589934592, "NCPU": 8}'
  exit 0
fi
exit 0`),
		TestCommand: []string{"go", "test", "./..."},
	}
	b, cmd, _, err := ResolveExecBackend(context.Background(), true, sc)
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "docker", b.Name())
	assert.Equal(t, []string{"go", "test", "./..."}, cmd)
}

func TestResolveExecBackend_PreflightFailureRefuses(t *testing.T) {
	sc := &registry.SandboxConfig{
		DockerPath:  fakeDocker(t, "exit 1"), // daemon unreachable
		TestCommand: []string{"go", "test"},
	}
	_, _, _, err := ResolveExecBackend(context.Background(), true, sc)
	require.Error(t, err, "a failed preflight must refuse the run")
	assert.Contains(t, err.Error(), "preflight")
}
