package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebtDashboard_Wiring(t *testing.T) {
	cmd := newDebtCmd()
	var has bool
	for _, c := range cmd.Commands() {
		if c.Name() == "dashboard" {
			has = true
		}
	}
	assert.True(t, has, "debt has a dashboard subcommand")
}

func TestDebtDashboard_WritesFile(t *testing.T) {
	items := writeItems(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	msg, err := runDebt(t, "dashboard", "--items", items, "--out", out)
	require.NoError(t, err)
	assert.Contains(t, msg, out)

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(got), "# Technical Debt Dashboard")
	assert.Contains(t, string(got), "cmd/atcr/autofix.go:248")
}

func TestDebtDashboard_Stdout(t *testing.T) {
	items := writeItems(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	msg, err := runDebt(t, "dashboard", "--items", items, "--out", out, "--stdout")
	require.NoError(t, err)
	assert.Contains(t, msg, "# Technical Debt Dashboard")
	assert.NoFileExists(t, out, "--stdout must not write the file")
}

func TestDebtDashboard_CheckMissingFileIsError(t *testing.T) {
	items := writeItems(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	_, err := runDebt(t, "dashboard", "--items", items, "--out", out, "--check")
	require.Error(t, err, "--check on a missing dashboard must fail")
}

func TestDebtDashboard_CheckDetectsDriftThenClean(t *testing.T) {
	items := writeItems(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	// Generate, then --check should pass (up to date).
	_, err := runDebt(t, "dashboard", "--items", items, "--out", out)
	require.NoError(t, err)
	_, err = runDebt(t, "dashboard", "--items", items, "--out", out, "--check")
	require.NoError(t, err, "freshly generated dashboard is up to date")

	// Mutate the on-disk file; --check should now detect drift.
	require.NoError(t, os.WriteFile(out, []byte("stale content\n"), 0o644))
	_, err = runDebt(t, "dashboard", "--items", items, "--out", out, "--check")
	require.Error(t, err, "a stale dashboard must fail --check")
}

func TestDebtDashboard_CheckAndStdoutAreMutuallyExclusive(t *testing.T) {
	items := writeItems(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	_, err := runDebt(t, "dashboard", "--items", items, "--out", out, "--check", "--stdout")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

func TestDebtDashboard_CheckSkipsSync(t *testing.T) {
	items := writeItems(t)
	readme := filepath.Join(t.TempDir(), "README.md")
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	// Generate the dashboard that matches the existing items.
	_, err := runDebt(t, "dashboard", "--items", items, "--out", out)
	require.NoError(t, err)

	// A conflicting README: if SyncShards ran, items/ would be overwritten.
	content := "# TD\n\n" +
		"### [2026-07-01] From Sprint: demo\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
		"|-------|---|----------|------|---------|-----|----------|-------------|--------|\n" +
		"| 1 | [ ] | HIGH | pkg/x.go:1 | boom | fixit | correctness | 15 | code-review |\n"
	require.NoError(t, os.WriteFile(readme, []byte(content), 0o644))

	before, err := os.ReadDir(items)
	require.NoError(t, err)

	_, err = runDebt(t, "dashboard", "--items", items, "--readme", readme, "--out", out, "--check", "--sync")
	require.NoError(t, err, "--check --sync must skip sync and pass when dashboard matches items")

	after, err := os.ReadDir(items)
	require.NoError(t, err)
	require.Equal(t, len(before), len(after), "SyncShards must be skipped when --check is set")
}
