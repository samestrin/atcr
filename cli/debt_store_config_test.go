package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// Epic 92 T4: a debt.store setting moves the store for every reader and the
// writer. These tests run from a repo root (isolate's CWD, marked with .git)
// whose HOME is isolate's empty temp home.

// storeConfigRepo isolates the test, marks the CWD as a repo root and returns
// its absolute path and HOME.
func storeConfigRepo(t *testing.T) (root, home string) {
	t.Helper()
	isolate(t)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(cwd, ".git"), 0o755))
	root, err = filepath.EvalSymlinks(cwd)
	require.NoError(t, err)
	return root, os.Getenv("HOME")
}

func writeProjectStoreConfig(t *testing.T, root, store string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "config.yaml"),
		[]byte("debt:\n  store: '"+store+"'\n"), 0o644))
}

func writeUserStoreConfig(t *testing.T, home, store string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.yaml"),
		[]byte("debt:\n  store: '"+store+"'\n"), 0o644))
}

func addOneDebtItem(t *testing.T, args ...string) {
	t.Helper()
	full := append([]string{"add",
		"--severity", "HIGH", "--file", "internal/x/y.go:12",
		"--problem", "boom", "--fix", "guard it", "--category", "correctness",
		"--est", "30"}, args...)
	out, err := runDebt(t, full...)
	require.NoError(t, err, out)
}

func TestDebtStoreConfig_UnsetKeepsTheDefault(t *testing.T) {
	root, _ := storeConfigRepo(t)
	addOneDebtItem(t)
	require.Len(t, readDebtStore(t, localdebt.DefaultDir(root)), 1)

	out, err := runDebt(t, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "boom")
}

func TestDebtStoreConfig_UserStoreOutsideTheRepo(t *testing.T) {
	root, home := storeConfigRepo(t)
	ext := filepath.Join(t.TempDir(), "planning", "debt")
	writeUserStoreConfig(t, home, ext)

	addOneDebtItem(t)
	require.Len(t, readDebtStore(t, ext), 1, "debt add writes the configured store")
	_, err := os.Stat(localdebt.DefaultDir(root))
	assert.True(t, os.IsNotExist(err), "the default store must stay untouched: %v", err)

	out, err := runDebt(t, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "boom", "debt list reads the configured store")
}

func TestDebtStoreConfig_ProjectStoreAndFlagPrecedence(t *testing.T) {
	root, home := storeConfigRepo(t)
	writeUserStoreConfig(t, home, filepath.Join(t.TempDir(), "user-store"))
	writeProjectStoreConfig(t, root, "planning/debt")

	addOneDebtItem(t)
	require.Len(t, readDebtStore(t, filepath.Join(root, "planning", "debt")), 1, "project config beats user config")

	flagStore := filepath.Join(t.TempDir(), "flag-store")
	addOneDebtItem(t, "--store", flagStore)
	require.Len(t, readDebtStore(t, flagStore), 1, "--store beats project config")
}

func TestDebtStoreConfig_InvalidProjectStoreFailsTheCommand(t *testing.T) {
	root, _ := storeConfigRepo(t)
	writeProjectStoreConfig(t, root, "../escape")

	out, err := runDebt(t, "list")
	require.Error(t, err)
	assert.Contains(t, err.Error()+out, "debt.store")

	_, err = runDebt(t, "add", "--severity", "HIGH", "--file", "a.go:1", "--problem", "p",
		"--fix", "f", "--category", "correctness", "--est", "5")
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(filepath.Dir(root), "escape"))
	assert.True(t, os.IsNotExist(statErr))
	_, statErr = os.Stat(localdebt.DefaultDir(root))
	assert.True(t, os.IsNotExist(statErr), "an invalid setting must not fall back to the default store")
}

func TestDebtStoreConfig_QualitySignalReadsTheConfiguredStore(t *testing.T) {
	root, home := storeConfigRepo(t)
	ext := filepath.Join(t.TempDir(), "debt")
	writeUserStoreConfig(t, home, ext)

	rec := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion,
		RunID:         "2026-07-01T10:00:00Z-seed",
		Timestamp:     "2026-07-01T10:00:00Z",
		Severity:      "LOW", File: "a.go", Line: 1, Problem: "p",
		Reviewers: []string{"bruce"}, Model: "m", Status: "wontfix",
	}
	rec.StampID()
	require.NoError(t, localdebt.Append(ext, rec))

	payload, err := buildQualitySignalPayload(root)
	require.NoError(t, err)
	require.Len(t, payload, 1)
	assert.Equal(t, 1, payload[0].DismissedCount)
}

func TestDebtStoreConfig_SuggestionCarriesTheConfiguredStore(t *testing.T) {
	root, home := storeConfigRepo(t)
	ext := filepath.Join(t.TempDir(), "debt")
	writeUserStoreConfig(t, home, ext)

	got := renderSuggestions(suggestReconcile, invocation{Flags: map[string]string{"repo-root": root}})
	require.NotEmpty(t, got)
	assert.Equal(t, "atcr debt list --store="+carryValue(ext), got[0])

	// An invalid setting renders no --store rather than a wrong one: the bare
	// `debt list` then resolves the setting itself and reports the mistake.
	writeProjectStoreConfig(t, root, "/abs")
	got = renderSuggestions(suggestReconcile, invocation{Flags: map[string]string{"repo-root": root}})
	assert.Equal(t, "atcr debt list", got[0])
}
