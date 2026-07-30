package scorecard

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appendN writes n records for reviewer/model into dir, one call to Append per
// record (mirroring one review run each), so LeaderboardRow.Runs accumulates
// exactly n for that (reviewer, model) group.
func appendN(t *testing.T, dir string, n int, reviewer, model string, raisedEach, corroboratedEach int) {
	t.Helper()
	for i := 0; i < n; i++ {
		runID := runIDAt(time.Now(), reviewer+model+string(rune('a'+i)))
		rec := reviewer_(runID, reviewer, model, raisedEach, corroboratedEach)
		require.NoError(t, Append(dir, rec))
	}
}

// reviewer_ avoids colliding with the reviewer() helper's cost/latency params
// aggregate_test.go already defines in this package — trust_test.go only needs
// raised/corroborated, so this thin wrapper fills in benign defaults.
func reviewer_(runID, name, model string, raised, corroborated int) Record {
	return reviewer(runID, name, model, raised, corroborated, 0, 0)
}

func TestTrustPriors_SumsAcrossModels(t *testing.T) {
	dir := t.TempDir()
	appendN(t, dir, 4, "Sasha", "opus", 1, 1)
	appendN(t, dir, 4, "Sasha", "sonnet", 1, 0)
	appendN(t, dir, 4, "Sasha", "haiku", 1, 1)

	rates, err := TrustPriors(dir, 10)
	require.NoError(t, err)
	// 12 summed runs clears a floor of 10 (AC1); rate = (4+0+4)/(4+4+4) = 8/12.
	require.Contains(t, rates, "sasha")
	assert.InDelta(t, 8.0/12.0, rates["sasha"], 1e-9)
}

func TestTrustPriors_BelowMinRunsAbsent(t *testing.T) {
	dir := t.TempDir()
	appendN(t, dir, 3, "Penny", "opus", 1, 1)

	rates, err := TrustPriors(dir, 10)
	require.NoError(t, err)
	_, present := rates["penny"]
	assert.False(t, present, "reviewer below minRuns must be absent, not zero-valued")
}

func TestTrustPriors_AtMinRunsPresent(t *testing.T) {
	dir := t.TempDir()
	appendN(t, dir, 10, "Robin", "opus", 1, 1)

	rates, err := TrustPriors(dir, 10)
	require.NoError(t, err)
	assert.Contains(t, rates, "robin", "reviewer at exactly minRuns must be present (inclusive floor)")
}

func TestTrustPriors_MinRunsZeroOrNegativeAppliesNoFloor(t *testing.T) {
	dir := t.TempDir()
	appendN(t, dir, 1, "Penny", "opus", 1, 1)

	for _, minRuns := range []int{0, -1} {
		rates, err := TrustPriors(dir, minRuns)
		require.NoError(t, err)
		assert.Contains(t, rates, "penny", "minRuns=%d must apply no floor", minRuns)
	}
}

func TestTrustPriors_MixedCaseCollapsesToLowercaseKey(t *testing.T) {
	dir := t.TempDir()
	appendN(t, dir, 1, "Sasha", "opus", 4, 3)
	appendN(t, dir, 1, "SASHA", "sonnet", 6, 1)

	rates, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	assert.Len(t, rates, 1)
	assert.InDelta(t, 0.4, rates["sasha"], 1e-9)
}

func TestTrustPriors_EmptyStoreYieldsEmptyMapNoError(t *testing.T) {
	dir := t.TempDir()

	rates, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	assert.Empty(t, rates)
}

func TestTrustPriors_MissingDirYieldsEmptyMapNoError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	rates, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	assert.Empty(t, rates)
}

func TestTrustPriors_UnreadableDirYieldsEmptyMapNoError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission bits do not block root reads")
	}
	dir := t.TempDir()
	unreadable := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })

	rates, err := TrustPriors(unreadable, 0)
	require.NoError(t, err)
	assert.Empty(t, rates)
}

func TestTrustPriors_ZeroDenominatorYieldsZeroNotNaN(t *testing.T) {
	dir := t.TempDir()
	appendN(t, dir, 1, "Ronin", "opus", 0, 0)

	rates, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	require.Contains(t, rates, "ronin")
	assert.Equal(t, 0.0, rates["ronin"])
	assert.False(t, math.IsNaN(rates["ronin"]))
	assert.False(t, math.IsInf(rates["ronin"], 0))
}
