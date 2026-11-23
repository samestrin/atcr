package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The EST-8260 probe: a quoted '~' reaches atcr unexpanded, and --store '~',
// review --output-dir '~' and report --output '~/r.md' created a directory
// literally named "~" in the cwd. atcr now expands "~" and "~/..." itself.

func noLiteralTilde(t *testing.T) {
	t.Helper()
	_, err := os.Lstat("~")
	assert.True(t, os.IsNotExist(err), "a directory literally named ~ was created")
}

func TestOutputDirFromFlags_TildeExpands(t *testing.T) {
	isolate(t)
	home := os.Getenv("HOME")
	cmd := newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "~/reviews/x"}))
	dir, err := outputDirFromFlags(cmd)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "reviews", "x"), dir)

	cmd = newReviewCmd()
	require.NoError(t, cmd.ParseFlags([]string{"--output-dir", "~bob/x"}))
	_, err = outputDirFromFlags(cmd)
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err))
	noLiteralTilde(t)
}

func TestReportCmd_OutputTildeExpands(t *testing.T) {
	isolate(t)
	fixtureReconciled(t, "r", oneFinding)
	home := os.Getenv("HOME")
	require.Equal(t, 0, execCmd(t, "report", "--output", "~/r.md", "r"))
	assert.FileExists(t, filepath.Join(home, "r.md"))
	noLiteralTilde(t)
}

func TestDebtStoreFlag_TildeExpands(t *testing.T) {
	isolate(t)
	home := os.Getenv("HOME")
	require.Equal(t, 0, execCmd(t, "debt", "add", "--store", "~/td", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--severity", "LOW", "--category", "c"))
	_, err := os.Stat(filepath.Join(home, "td"))
	assert.NoError(t, err, "the store is created under HOME")
	noLiteralTilde(t)

	code, out := execCmdCapture(t, "debt", "list", "--store", "~bob/td")
	assert.Equal(t, 2, code, out)
	noLiteralTilde(t)
}
