package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --store is the canonical name for the TD-store selector on the debt command
// family (and quality-report); --dir remains a deprecated hidden alias that
// resolves identically. The store path, the review subtree, and the skill
// export destination are three unrelated concepts and no longer share one flag
// name.
func TestDebtStoreFlag_StoreCanonicalDirAlias(t *testing.T) {
	dir := t.TempDir()

	codeStore, outStore := execCmdCapture(t, "debt", "list", "--store", dir)
	require.Equal(t, 0, codeStore, outStore)

	codeDir, outDir := execCmdCapture(t, "debt", "list", "--dir", dir)
	require.Equal(t, 0, codeDir, outDir)

	_, helpOut := execCmdCapture(t, "debt", "list", "--help")
	require.Contains(t, helpOut, "--store")
	require.NotContains(t, helpOut, "--dir ")
}

// --scope is the canonical name for the review subtree selector; --dir remains
// a deprecated hidden alias resolving identically.
func TestReviewScopeFlag_CanonicalDirAlias(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal"), 0o755))

	scopeCmd := newReviewCmd()
	require.NoError(t, scopeCmd.ParseFlags([]string{"--scope", "internal"}))
	gotScope, err := validateDirFlag(scopeCmd, root)
	require.NoError(t, err)

	aliasCmd := newReviewCmd()
	require.NoError(t, aliasCmd.ParseFlags([]string{"--dir", "internal"}))
	gotAlias, err := validateDirFlag(aliasCmd, root)
	require.NoError(t, err)

	require.Equal(t, gotAlias, gotScope)
	assert.Equal(t, "internal", gotScope)

	_, helpOut := execCmdCapture(t, "review", "--help")
	require.Contains(t, helpOut, "--scope")
}

// --dest is the canonical name for the skill-export destination; --dir remains
// a deprecated hidden alias resolving identically.
func TestSkillExportFlag_DestCanonicalDirAlias(t *testing.T) {
	_, helpOut := execCmdCapture(t, "skill", "export", "--help")
	require.Contains(t, helpOut, "--dest")

	destDir := filepath.Join(t.TempDir(), "via-dest")
	code, out := execCmdCapture(t, "skill", "export", "--dest", destDir)
	require.Equal(t, 0, code, out)

	aliasDir := filepath.Join(t.TempDir(), "via-dir")
	code, out = execCmdCapture(t, "skill", "export", "--dir", aliasDir)
	require.Equal(t, 0, code, out)
}
