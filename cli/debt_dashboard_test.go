package cli

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

// AC10 mechanism: --output defaults to empty (stdout), and the old private-tree
// DASHBOARD.md default is gone. --stdout is retired with it — stdout is no
// longer an opt-in mode, it is the default.
func TestDebtDashboard_OutputDefaultsToStdout(t *testing.T) {
	sub := debtSubcommand(t, newDebtCmd(), "dashboard")

	out := sub.Flags().Lookup("output")
	require.NotNil(t, out, "dashboard registers --output")
	assert.Equal(t, "", out.DefValue, "an empty --output means stdout")

	assert.Nil(t, sub.Flags().Lookup("out"), "--out is renamed to --output")
	assert.Nil(t, sub.Flags().Lookup("stdout"), "--stdout is retired; stdout is the default")
	assert.Nil(t, sub.Flags().Lookup("sync"), "--sync is retired with the README store")
	assert.Nil(t, sub.Flags().Lookup("readme"), "--readme is retired with the README store")
	assert.Nil(t, sub.Flags().Lookup("items"), "--items is retired with the shard store")
}

func TestDebtDashboard_StdoutIsTheDefault(t *testing.T) {
	dir := writeLocalDebt(t)
	unwritten := filepath.Join(t.TempDir(), "DASHBOARD.md")

	msg, err := runDebt(t, "dashboard", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, msg, "# Technical Debt Dashboard")
	assert.Contains(t, msg, "cmd/atcr/autofix.go:248")
	assert.NoFileExists(t, unwritten, "the default path writes no file")
}

func TestDebtDashboard_WritesFile(t *testing.T) {
	dir := writeLocalDebt(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	msg, err := runDebt(t, "dashboard", "--dir", dir, "--output", out)
	require.NoError(t, err)
	assert.Contains(t, msg, out)

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(got), "# Technical Debt Dashboard")
	assert.Contains(t, string(got), "cmd/atcr/autofix.go:248")
}

func TestDebtDashboard_CheckMissingFileIsError(t *testing.T) {
	dir := writeLocalDebt(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	_, err := runDebt(t, "dashboard", "--dir", dir, "--output", out, "--check")
	require.Error(t, err, "--check on a missing dashboard must fail")
}

func TestDebtDashboard_CheckWithoutOutputIsUsageError(t *testing.T) {
	dir := writeLocalDebt(t)
	_, err := runDebt(t, "dashboard", "--dir", dir, "--check")
	require.Error(t, err, "--check has no file to compare against without --output")
	assert.Equal(t, exitUsage, exitCode(err))
}

func TestDebtDashboard_CheckDetectsDriftThenClean(t *testing.T) {
	dir := writeLocalDebt(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	// Generate, then --check should pass (up to date).
	_, err := runDebt(t, "dashboard", "--dir", dir, "--output", out)
	require.NoError(t, err)
	_, err = runDebt(t, "dashboard", "--dir", dir, "--output", out, "--check")
	require.NoError(t, err, "freshly generated dashboard is up to date")

	// Mutate the on-disk file; --check should now detect drift.
	require.NoError(t, os.WriteFile(out, []byte("stale content\n"), 0o644))
	_, err = runDebt(t, "dashboard", "--dir", dir, "--output", out, "--check")
	require.Error(t, err, "a stale dashboard must fail --check")
}

// --check is deterministic against store content, not the clock: mutating the
// store is what makes a previously-clean check fail.
func TestDebtDashboard_CheckFailsAfterStoreMutation(t *testing.T) {
	recs := debtSampleRecords()
	dir := writeLocalDebt(t, recs...)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	_, err := runDebt(t, "dashboard", "--dir", dir, "--output", out)
	require.NoError(t, err)
	_, err = runDebt(t, "dashboard", "--dir", dir, "--output", out, "--check")
	require.NoError(t, err)

	_, err = runDebt(t, "add", "--dir", dir,
		"--severity", "CRITICAL", "--file", "new.go:1", "--problem", "fresh", "--fix", "f", "--category", "c")
	require.NoError(t, err)

	_, err = runDebt(t, "dashboard", "--dir", dir, "--output", out, "--check")
	require.Error(t, err, "a store mutation must be detected as drift")
}
