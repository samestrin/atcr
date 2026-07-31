package scorecard

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	reclib "github.com/samestrin/atcr/reconcile"
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

// --- ResolveTrustPriors (epic 35.9 T2 wiring) ---

func TestResolveTrustPriors_MissingStoreDegradesToEmptyMap(t *testing.T) {
	// No atcr/scorecard store under a fresh HOME (AC5): degrades to an empty
	// map, never an error, never a blocked caller.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", t.TempDir())

	assert.Empty(t, ResolveTrustPriors())
}

func TestResolveTrustPriors_ReadsTheDefaultStore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", t.TempDir())

	dir, err := DefaultDir()
	require.NoError(t, err)
	appendN(t, dir, DefaultTrustMinRuns, "Trusted", "m", 1, 1)

	rates := ResolveTrustPriors()
	require.Contains(t, rates, "trusted")
	assert.InDelta(t, 1.0, rates["trusted"], 1e-9)
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

// TestTrustPriors_IgnoresNonStrictRuns closes the cross-run feedback loop epic
// 35.9.1's configurable consensus levels opened. reviewerCounts computes
// findings_raised/findings_corroborated from the POST-consensus-filter finding
// set, so the same review yields a different corroboration rate per level: under
// off or lenient the uncorroborated singletons strict would have sidecarred stay
// in the set, inflating raised without raising corroborated. Feeding those rates
// into TrustPriors durably depresses the priors demoteByTrust and trustExempt
// apply on LATER strict runs.
//
// Every historical run predates the levels and was implicitly strict, so an
// EMPTY consensus_level must count as strict — that is what keeps this filter
// from silently discarding a store written before 35.9.1.
func TestTrustPriors_IgnoresNonStrictRuns(t *testing.T) {
	dir := t.TempDir()

	// A reviewer with a clean strict history: every finding corroborated.
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			RunID:                fmt.Sprintf("2026-07-01T00:00:00Z-s%02d", i),
			Reviewer:             "bruce",
			Model:                "m",
			ConsensusLevel:       reclib.ConsensusStrict,
			FindingsRaised:       1,
			FindingsCorroborated: 1,
		}))
	}
	// Then a burst of off-level runs where nothing corroborated. These must not
	// count: their raised counts include singletons a strict run would never
	// have promoted into the finding set at all.
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			RunID:                fmt.Sprintf("2026-07-02T00:00:00Z-o%02d", i),
			Reviewer:             "bruce",
			Model:                "m",
			ConsensusLevel:       reclib.ConsensusOff,
			FindingsRaised:       9,
			FindingsCorroborated: 0,
		}))
	}

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, priors, "bruce")
	assert.InDelta(t, 1.0, priors["bruce"], 0.0001,
		"a non-strict run must not depress the trust prior later strict runs read")
}

// TestTrustPriors_EmptyConsensusLevelCountsAsStrict pins the backward-compatible
// half: a store written before epic 35.9.1 carries no consensus_level at all, and
// every one of those runs was strict by construction (the level did not exist).
// Treating the empty value as non-strict would strand every existing reviewer
// history and silently zero out the trust priors in the field.
func TestTrustPriors_EmptyConsensusLevelCountsAsStrict(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion: SchemaVersion,
			RecordType:    RecordTypeReviewer,
			RunID:         fmt.Sprintf("2026-06-01T00:00:00Z-l%02d", i),
			Reviewer:      "greta",
			Model:         "m",
			// ConsensusLevel deliberately unset: the pre-35.9.1 store shape.
			FindingsRaised:       4,
			FindingsCorroborated: 2,
		}))
	}

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, priors, "greta", "a pre-35.9.1 store must still yield priors")
	assert.InDelta(t, 0.5, priors["greta"], 0.0001)
}

// TestTrustPriors_AllNonStrictYieldsNoPrior is the boundary: a reviewer whose
// ONLY history is non-strict has no trusted measurement, so it is omitted
// entirely rather than reported at a rate computed from level-dependent counts.
// Omission (not a zero) is what lets callers distinguish "no history" from
// "measured zero" — the contract TrustPriors already documents.
func TestTrustPriors_AllNonStrictYieldsNoPrior(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			RunID:                fmt.Sprintf("2026-07-03T00:00:00Z-n%02d", i),
			Reviewer:             "robin",
			Model:                "m",
			ConsensusLevel:       reclib.ConsensusLenient,
			FindingsRaised:       3,
			FindingsCorroborated: 3,
		}))
	}

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	assert.NotContains(t, priors, "robin",
		"a reviewer with only non-strict history has no trusted measurement")
}
