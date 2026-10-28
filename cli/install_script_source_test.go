package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGoScript stands in for the go toolchain: it answers the `go env` queries
// install.sh makes and records each `go install` call (working directory and
// arguments) to $FAKE_GO_LOG instead of building anything.
const fakeGoScript = `#!/usr/bin/env bash
case "$1 $2" in
  "env GOVERSION") echo go1.25.0; exit 0 ;;
  "env GOBIN") echo "$FAKE_GOBIN"; exit 0 ;;
  "env GOPATH") echo "$FAKE_GOBIN"; exit 0 ;;
esac
if [ "$1" = "install" ]; then echo "$PWD|$*" >>"$FAKE_GO_LOG"; exit 0; fi
echo "fake-go: unexpected call: $*" >&2; exit 97
`

// runInstallScriptWithFakeGo runs script under bash with the fake go first on
// PATH and returns the single recorded `go install` call as "<dir>|<args>".
func runInstallScriptWithFakeGo(t *testing.T, script string) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	fakeDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeDir, "go"), []byte(fakeGoScript), 0o755))
	logPath := filepath.Join(t.TempDir(), "go.log")

	cmd := exec.Command(bash, script)
	cmd.Dir = t.TempDir() // the caller's working directory must not matter
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env,
		"PATH="+fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GO_LOG="+logPath,
		"FAKE_GOBIN="+t.TempDir(),
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "install.sh failed:\n%s", out)

	log, err := os.ReadFile(logPath)
	require.NoError(t, err, "install.sh never ran go install:\n%s", out)
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	require.Len(t, lines, 1, "install.sh runs go install exactly once")
	return lines[0]
}

// TestInstallScript_FromCloneBuildsTheCheckout: run from a clone, install.sh
// installs the clone's own code (`go install ./cmd/atcr` in the checkout root),
// not the latest published release.
func TestInstallScript_FromCloneBuildsTheCheckout(t *testing.T) {
	root, err := filepath.Abs("..")
	require.NoError(t, err)

	call := runInstallScriptWithFakeGo(t, filepath.Join(root, "install.sh"))
	dir, args, _ := strings.Cut(call, "|")
	gotDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	wantDir, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	assert.Equal(t, wantDir, gotDir, "go install runs in the checkout root")
	assert.Equal(t, "install ./cmd/atcr", args)
}

// TestInstallScript_OutsideCloneInstallsLatest: a copy of install.sh that does
// not sit in an atcr checkout installs the latest published release.
func TestInstallScript_OutsideCloneInstallsLatest(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "install.sh"))
	require.NoError(t, err)
	script := filepath.Join(t.TempDir(), "install.sh")
	require.NoError(t, os.WriteFile(script, src, 0o755))

	call := runInstallScriptWithFakeGo(t, script)
	_, args, _ := strings.Cut(call, "|")
	assert.Equal(t, "install github.com/samestrin/atcr/cmd/atcr@latest", args)
}
