package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The mark action takes the id as a positional argument — `atcr debt resolve
// <id>` — so the subcommand name no longer stutters against a --resolve flag.
func TestDebtResolve_PositionalIDMarksItem(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	assert.Contains(t, out, "Marked")
	assert.Contains(t, out, rec.ID)
}

// The removed flags are usage errors that name their replacement: --resolve
// <id> became the positional form, and --list's behavior lives on
// `atcr debt list`.
func TestDebtResolve_RemovedFlagsAreUsageErrorsNamingReplacement(t *testing.T) {
	dir := writeDebtStore(t, openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom"))

	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", "someid")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "atcr debt resolve <id>")

	_, err = runDebt(t, "resolve", "--dir", dir, "--list")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, err.Error(), "atcr debt list")
}
