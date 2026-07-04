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
