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

// AC10: the file path writes the file and says nothing on stdout — atcr report's
// contract, which has no "wrote it" line either. A status line on the write path
// is noise a redirecting caller never asked for.
func TestDebtDashboard_WritesFileAndIsSilentOnStdout(t *testing.T) {
	dir := writeLocalDebt(t)
	out := filepath.Join(t.TempDir(), "DASHBOARD.md")

	msg, err := runDebt(t, "dashboard", "--dir", dir, "--output", out)
	require.NoError(t, err)
	assert.Empty(t, msg, "--output writes nothing to stdout")

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(got), "# Technical Debt Dashboard")
	assert.Contains(t, string(got), "cmd/atcr/autofix.go:248")
}

// AC10: --output is validated the way atcr report validates it — resolved to an
// absolute, symlink-resolved path and rejected when it lands in a system
// directory, before anything is written.
func TestDebtDashboard_OutputUnderSystemDirIsUsageError(t *testing.T) {
	dir := writeLocalDebt(t)

	_, err := runDebt(t, "dashboard", "--dir", dir, "--output", "/etc/atcr-dashboard.md")
	require.Error(t, err, "a system-directory target must be rejected")
	assert.Equal(t, exitUsage, exitCode(err))
}

// A symlinked PARENT pointing into a system directory must be caught too: the
// bypass resolveOutputPath exists to close is validating the link path while the
// write follows the link.
func TestDebtDashboard_OutputThroughSymlinkedParentIsUsageError(t *testing.T) {
	dir := writeLocalDebt(t)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink("/etc", link))

	_, err := runDebt(t, "dashboard", "--dir", dir, "--output", filepath.Join(link, "d.md"))
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

// AC10: a write failure is a usage/infrastructure error (exit 2), the same
// classification cli/report.go applies to its own disk writes.
func TestDebtDashboard_WriteFailureIsExitTwo(t *testing.T) {
	dir := writeLocalDebt(t)
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	_, err := runDebt(t, "dashboard", "--dir", dir, "--output", filepath.Join(blocker, "d.md"))
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

// The dashboard is a generated artifact routinely written into a directory that
// does not exist yet, which is the one deliberate divergence from report's
// contract. Keep it pinned so a later "parity" cleanup does not remove it.
func TestDebtDashboard_CreatesMissingParentDirectory(t *testing.T) {
	dir := writeLocalDebt(t)
	out := filepath.Join(t.TempDir(), "new", "nested", "DASHBOARD.md")

	_, err := runDebt(t, "dashboard", "--dir", dir, "--output", out)
	require.NoError(t, err)
	assert.FileExists(t, out)
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
