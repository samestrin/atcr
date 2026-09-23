package scorecard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// TestResolveTrustPriorsForReview_UnresolvableStoreDirIsNil covers the
// DefaultDir error arm: with no HOME, XDG_CONFIG_HOME or AppData the config dir
// cannot be resolved, and the reconcile path gets no priors and no unmeasured
// count - nil, the same answer ResolveTrustPriors gives, not a read of "".
func TestResolveTrustPriorsForReview_UnresolvableStoreDirIsNil(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("AppData", "")
	_, err := DefaultDir()
	require.Error(t, err, "precondition: the store dir cannot be resolved")

	priors, unmeasured := ResolveTrustPriorsForReview(t.TempDir())
	assert.Nil(t, priors)
	assert.Zero(t, unmeasured)
	assert.Nil(t, ResolveTrustPriors(), "both resolvers agree on an unresolvable dir")
}

func TestResolveTrustPriors_MissingStoreDegradesToEmptyMap(t *testing.T) {
	// No atcr/scorecard store under a fresh HOME (AC5): degrades to an empty
	// map, never an error, never a blocked caller.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", t.TempDir())
	// os.UserConfigDir() reads %AppData% on Windows and ignores HOME/XDG, so
	// redirect it too or the test store lands in the developer's real atcr dir.
	t.Setenv("AppData", t.TempDir())

	assert.Empty(t, ResolveTrustPriors())
}

func TestResolveTrustPriors_ReadsTheDefaultStore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", t.TempDir())
	// os.UserConfigDir() reads %AppData% on Windows and ignores HOME/XDG, so
	// redirect it too or the test store lands in the developer's real atcr dir.
	t.Setenv("AppData", t.TempDir())

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

func TestTrustPriors_PartialReadFailureYieldsEmptyMap(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission bits do not block root reads")
	}
	dir := t.TempDir()
	writeMonthFile(t, dir, "2026-06", recordLine(t, "2026-06-10T10:00:00Z-jun", "bruce"))
	writeMonthFile(t, dir, "2026-07", recordLine(t, "2026-07-10T10:00:00Z-jul", "greta"))
	unreadable := filepath.Join(dir, "2026-07.jsonl")
	require.NoError(t, os.Chmod(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })

	rates, err := TrustPriors(dir, 0)
	require.NoError(t, err, "a best-effort read never returns an error, even on a mid-enumeration failure")
	assert.Empty(t, rates, "a mid-enumeration read failure must not aggregate a truncated store")
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
			Outcome:              outcomeFindings,
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
			Outcome:              outcomeFindings,
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
			Outcome:       outcomeFindings,
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
			Outcome:              outcomeFindings,
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

// TestTrustPriors_UnrecognizedConsensusLevelExcluded pins the fail-safe
// direction for a record whose level is not in the vocabulary — only reachable
// via a hand-edited or corrupted store, since the emitter always stamps a
// canonical level. It is EXCLUDED rather than read as strict: admitting an
// uninterpretable label could let a mislabeled non-strict run depress the priors,
// which is the exact harm this filter exists to prevent, whereas excluding it
// only forgoes some data. (This deliberately differs from consensusFloor, which
// fails safe to strict at reconcile time — there the risk is inverted.)
func TestTrustPriors_UnrecognizedConsensusLevelExcluded(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			Outcome:              outcomeFindings,
			RunID:                fmt.Sprintf("2026-07-04T00:00:00Z-x%02d", i),
			Reviewer:             "alfred",
			Model:                "m",
			ConsensusLevel:       "corrupted",
			FindingsRaised:       2,
			FindingsCorroborated: 2,
		}))
	}

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	assert.NotContains(t, priors, "alfred",
		"a record with an uninterpretable level must not count toward a trust prior")
}

// --- Windowed resolver (epic 35.11 T2) ---

// appendNAt writes n records for reviewer/model into dir stamped at `at`, so the
// records land in the month file `at` names — letting a test place history
// inside or outside a trust window deterministically.
func appendNAt(t *testing.T, dir string, n int, reviewer, model string, raisedEach, corroboratedEach int, at time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		runID := runIDAt(at, fmt.Sprintf("%s-%s-%d", reviewer, model, i))
		require.NoError(t, Append(dir, reviewer_(runID, reviewer, model, raisedEach, corroboratedEach)))
	}
}

// TestTrustPriors_AllHistoryUnchangedAcrossMonths is the compatibility pin for
// cli/personas.go's loadPersonasScores, which calls TrustPriors(dir, 0) directly and must keep
// seeing the WHOLE store. A reviewer whose entire history sits years outside any
// window must still be counted here — TrustPriors is all-history by contract, and
// epic 35.11 windows only ResolveTrustPriors.
func TestTrustPriors_AllHistoryUnchangedAcrossMonths(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	appendNAt(t, dir, 4, "Ancient", "opus", 1, 1, now.AddDate(-2, 0, 0))
	appendNAt(t, dir, 4, "Recent", "opus", 1, 0, now.AddDate(0, 0, -1))

	rates, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	assert.Contains(t, rates, "ancient", "TrustPriors stays all-history: a years-old reviewer is still counted")
	assert.Contains(t, rates, "recent")
	assert.InDelta(t, 1.0, rates["ancient"], 1e-9)
}

// TestTrustPriorsSince_ExcludesMonthsOutsideTheWindow is the core T2 behavior:
// the window drops whole month files before aggregation, so a reviewer active
// only outside it is absent from the priors map.
func TestTrustPriorsSince_ExcludesMonthsOutsideTheWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	appendNAt(t, dir, 4, "Ancient", "opus", 1, 1, now.AddDate(-2, 0, 0))
	appendNAt(t, dir, 4, "Recent", "opus", 1, 1, now.AddDate(0, 0, -1))

	rates, err := trustPriorsSince(dir, 0, defaultTrustWindow, now, nil)
	require.NoError(t, err)
	assert.NotContains(t, rates, "ancient", "a reviewer whose only runs predate the window is absent")
	assert.Contains(t, rates, "recent")
}

// TestTrustPriorsSince_StrictRunsFloorIgnoresLenientRunsInsideTheWindow covers
// the strictRuns x window compounding trust.go's window comment flags: a
// reviewer used mostly under --consensus lenient/off can hold fewer than
// DefaultTrustMinRuns STRICT runs inside the window even while running
// constantly. The windowed read must count only strict runs toward the floor:
// 25 lenient + 5 strict runs inside the window omits the reviewer at
// DefaultTrustMinRuns but includes it at minRuns=5.
func TestTrustPriorsSince_StrictRunsFloorIgnoresLenientRunsInsideTheWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	at := now.AddDate(0, 0, -10) // inside defaultTrustWindow
	for i := 0; i < 25; i++ {
		rec := reviewer_(runIDAt(at, fmt.Sprintf("lenient-%d", i)), "Lenny", "opus", 3, 3)
		rec.ConsensusLevel = reclib.ConsensusLenient
		require.NoError(t, Append(dir, rec))
	}
	for i := 0; i < 5; i++ {
		rec := reviewer_(runIDAt(at, fmt.Sprintf("strict-%d", i)), "Lenny", "opus", 3, 3)
		rec.ConsensusLevel = reclib.ConsensusStrict
		require.NoError(t, Append(dir, rec))
	}

	rates, err := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, now, nil)
	require.NoError(t, err)
	assert.NotContains(t, rates, "lenny",
		"only 5 strict runs inside the window — lenient runs must not top the floor up to DefaultTrustMinRuns")

	rates, err = trustPriorsSince(dir, 5, defaultTrustWindow, now, nil)
	require.NoError(t, err)
	assert.Contains(t, rates, "lenny", "at minRuns=5 the 5 strict runs clear the floor")
}

// TestTrustPriorsSince_WindowCanPushAReviewerBelowMinRuns constructs the
// dangerous shape the epic's window discussion (trust.go, defaultTrustWindow)
// names: a reviewer who clears the min-runs floor over ALL history but falls
// below it inside the window — 25 strict runs eight months out, 5 inside — and
// so silently loses trust exemption/demotion. The all-history read must keep
// the reviewer while the windowed read drops it, proving the omission comes
// from the window and not the floor.
func TestTrustPriorsSince_WindowCanPushAReviewerBelowMinRuns(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	appendNAt(t, dir, 25, "Fading", "opus", 1, 1, now.AddDate(0, -8, 0)) // outside the 180d window
	appendNAt(t, dir, 5, "Fading", "opus", 1, 1, now.AddDate(0, 0, -1))  // inside

	allHistory, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, allHistory, "fading", "30 strict runs over all history clears the floor")

	windowed, err := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, now, nil)
	require.NoError(t, err)
	assert.NotContains(t, windowed, "fading",
		"only 5 strict runs inside the window — below DefaultTrustMinRuns, so the windowed read drops the reviewer")
}

// TestTrustPriorsSince_NoWindowMatchesTrustPriors pins the shared-code-path
// guarantee: since<=0 is exactly the all-history read, so the two paths cannot
// drift.
func TestTrustPriorsSince_NoWindowMatchesTrustPriors(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	appendNAt(t, dir, 4, "Ancient", "opus", 3, 1, now.AddDate(-2, 0, 0))
	appendNAt(t, dir, 4, "Recent", "sonnet", 2, 2, now.AddDate(0, 0, -1))

	windowed, err := trustPriorsSince(dir, 0, 0, now, nil)
	require.NoError(t, err)
	all, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	assert.Equal(t, all, windowed, "since<=0 must equal TrustPriors' all-history result")
}

// TestResolveTrustPriors_IsWindowed is the reconcile-side contract: the four
// epic-35.9 RunReconcile call sites read priors through ResolveTrustPriors, and
// that read is bounded by defaultTrustWindow. The same store read all-history
// (TrustPriors) still surfaces the ancient reviewer, which proves the omission
// comes from the WINDOW and not from the DefaultTrustMinRuns floor.
func TestResolveTrustPriors_IsWindowed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", t.TempDir())
	// os.UserConfigDir() reads %AppData% on Windows and ignores HOME/XDG, so
	// redirect it too or the test store lands in the developer's real atcr dir.
	t.Setenv("AppData", t.TempDir())

	dir, err := DefaultDir()
	require.NoError(t, err)
	outside := time.Now().Add(-defaultTrustWindow).AddDate(0, -2, 0)
	// "Midwindow" sits 150 days back — inside the 180d window by roughly one
	// month. It is the LOWER bound arm: "Recent" alone lands in the current month
	// file, which overlaps any positive window, so without this reviewer the test
	// passes for every window from 1ns to ~208d and pins only that SOME window
	// exists. Narrowing defaultTrustWindow drops this reviewer's month file and
	// fails the Contains below — the epic's stated HIGH risk (AC3) made
	// executable.
	//
	// 150 days is a LITERAL, deliberately not derived from defaultTrustWindow: an
	// anchor written as now-defaultTrustWindow+1mo moves with the constant, so
	// halving the window would move the seed along with it and the test would
	// stay green. The literal is what makes this a magnitude pin.
	inside := time.Now().AddDate(0, 0, -150)
	appendNAt(t, dir, DefaultTrustMinRuns, "Ancient", "opus", 1, 1, outside)
	appendNAt(t, dir, DefaultTrustMinRuns, "Midwindow", "opus", 1, 1, inside)
	appendNAt(t, dir, DefaultTrustMinRuns, "Recent", "opus", 1, 1, time.Now())

	allHistory, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, allHistory, "ancient", "the ancient reviewer clears the min-runs floor over all history")

	rates := ResolveTrustPriors()
	assert.Contains(t, rates, "recent")
	assert.Contains(t, rates, "midwindow",
		"a reviewer one month inside defaultTrustWindow must survive the windowed read — this pins the window's MAGNITUDE, not just its existence")
	assert.NotContains(t, rates, "ancient", "ResolveTrustPriors must not read month files outside defaultTrustWindow")
}

// TestDefaultTrustWindow_NotNarrowedWithoutRemeasurement guards the epic's
// highest risk (AC3): too narrow a window pushes a real reviewer below
// DefaultTrustMinRuns, silently disabling trust exemption/demotion on four hot
// paths. 180d was measured against the live store (2026-07-31: all 11 reviewers
// clearing the floor held 113-120 strict runs at every window from 30d to 365d).
// Narrowing this constant without redoing that measurement is the failure mode
// this test exists to catch.
func TestDefaultTrustWindow_NotNarrowedWithoutRemeasurement(t *testing.T) {
	assert.GreaterOrEqual(t, defaultTrustWindow, 180*24*time.Hour,
		"defaultTrustWindow must stay >= 180d unless re-measured against a real store (epic 35.11 AC3)")
}

// TestDefaultTrustWindow_IsGenerousEnoughForTheMinRunsFloor is the BEHAVIORAL
// half of the guard above. That one compares a constant against its own literal
// and so can only catch a deliberate narrowing of the WINDOW; it says nothing
// about the other half of the interaction, DefaultTrustMinRuns. This one seeds a
// reviewer at a steady low run rate spread across the window and asserts the
// windowed read still returns it, so BOTH failure directions fail here: raising
// DefaultTrustMinRuns above the seeded run count, or narrowing the window until
// fewer of those runs remain inside it. That pairing is what trust.go's
// defaultTrustWindow comment claims is protected.
func TestDefaultTrustWindow_IsGenerousEnoughForTheMinRunsFloor(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	// steadyRuns is a LITERAL 20 — the run count DefaultTrustMinRuns was measured
	// at (trust.go), deliberately NOT written as DefaultTrustMinRuns. Seeding
	// `DefaultTrustMinRuns` records and then asserting against the same constant
	// is `n >= n`: it passes at any value, which is the very tautology this test
	// exists to replace. The literal is what makes raising the floor fail here.
	const steadyRuns = 20
	// stride is likewise a LITERAL 9 days (180d / 20 runs), for the same reason:
	// written as defaultTrustWindow/steadyRuns it would CONTRACT with the window,
	// re-bunching every run inside whatever window remains and passing at 60d.
	// Held fixed, the runs stay spread over ~171 real days, so narrowing the
	// window leaves fewer than the floor inside it and this test fails. Spreading
	// them at all — rather than bunching them into one month file — is also the
	// shape the floor actually endangers: a reviewer active at a steady low rate.
	// The +24h keeps the newest stride at `now` rather than one day past it.
	const stride = 9 * 24 * time.Hour
	for i := 0; i < steadyRuns; i++ {
		at := now.Add(-time.Duration(i) * stride).Add(24 * time.Hour)
		if at.After(now) {
			at = now
		}
		appendNAt(t, dir, 1, "Steady", "opus", 1, 1, at)
	}

	rates, err := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, now, nil)
	require.NoError(t, err)
	assert.Contains(t, rates, "steady",
		"a reviewer holding 20 strict runs spread across defaultTrustWindow must clear the floor — if this fails, the window and the floor have drifted apart and both need re-measuring (epic 35.11 AC3)")
}

// --- Windowed-read benchmark (epic 35.11 T3) ---

// seedMonthlyStore writes perMonth reviewer records into each of the `months`
// consecutive month files ending with the month of `end`, bypassing Append so
// seeding is one write per month rather than one open per record.
func seedMonthlyStore(tb testing.TB, dir string, months, perMonth int, end time.Time) {
	tb.Helper()
	require.NoError(tb, os.MkdirAll(dir, 0o700))
	reviewers := []string{"archer", "brad", "dax", "greta", "kai", "mira", "otto", "pace", "ronin", "vera"}
	for m := 0; m < months; m++ {
		monthStart := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -m, 0)
		var buf bytes.Buffer
		for i := 0; i < perMonth; i++ {
			// i%27 keeps every timestamp inside its own month, so each record lands
			// in the file its stem names.
			ts := monthStart.Add(time.Duration(i%27) * 24 * time.Hour)
			rec := reviewer_(runIDAt(ts, fmt.Sprintf("bench-%d-%d", m, i)), reviewers[i%len(reviewers)], "opus", 3, 2)
			line, err := json.Marshal(rec)
			require.NoError(tb, err)
			buf.Write(line)
			buf.WriteByte('\n')
		}
		path := filepath.Join(dir, monthStart.Format("2006-01")+".jsonl")
		require.NoError(tb, os.WriteFile(path, buf.Bytes(), 0o600))
	}
}

// BenchmarkResolveTrustPriors measures the epic-35.11 win on a 24-month store:
// `windowed_180d` is the shipped ResolveTrustPriors (reads ~7 month files),
// `all_history_pre_35_11` is the pre-epic cost of the same call (TrustPriors at
// DefaultTrustMinRuns, reading all 24). The store is pointed at a temp HOME via
// os.UserConfigDir, the seam TestResolveTrustPriors_ReadsTheDefaultStore already
// uses — so the literal exported call is benchmarked with no production-side
// test hook. CI never passes -bench, so this costs nothing on a routine
// `go test` run; it exists as a regression guard on the cost this epic bounds.
func BenchmarkResolveTrustPriors(b *testing.B) {
	b.Setenv("XDG_CONFIG_HOME", "")
	b.Setenv("HOME", b.TempDir())
	// See the AppData note on the tests above: Windows resolves the store
	// through %AppData%, so the benchmark must redirect it too.
	b.Setenv("AppData", b.TempDir())

	dir, err := DefaultDir()
	require.NoError(b, err)
	seedMonthlyStore(b, dir, 24, 500, time.Now())

	b.Run("windowed_180d", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if len(ResolveTrustPriors()) == 0 {
				b.Fatal("benchmark store must yield priors")
			}
		}
	})

	b.Run("all_history_pre_35_11", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			priors, _ := TrustPriors(dir, DefaultTrustMinRuns)
			if len(priors) == 0 {
				b.Fatal("benchmark store must yield priors")
			}
		}
	})
}

// TestTrustPriors_IgnoresPreUnresolvedDenominatorRuns pins the second era filter,
// which exists for exactly the reason strictRuns does: FindingsRaised changed
// meaning, and a rate computed by summing both meanings is a measurement of
// neither.
//
// Epic 35.16.6.5 put the Tier-4-routed findings into the denominator. Records
// written before it exclude them; records written after include them. The only
// discriminator strictRuns applies is consensus_level, so without this filter a
// reviewer's prior is a blend of two definitions for the whole 180-day window,
// drifting as the old records age out — silently moving trustExempt and
// demoteByTrust with nothing marking the boundary.
//
// The rule is prefer-current, not require-current: a window holding any
// current-era record uses only those, and a window holding none falls back to the
// pre-epic records unchanged (see the companion test below). What is ruled out is
// the MIX, which is the only combination that measures nothing.
func TestTrustPriors_IgnoresPreUnresolvedDenominatorRuns(t *testing.T) {
	dir := t.TempDir()

	// Pre-epic era: no flag, and a flattering rate because phantoms were never
	// counted against this reviewer.
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			Outcome:              outcomeFindings,
			RunID:                fmt.Sprintf("2026-07-01T00:00:00Z-old%02d", i),
			Reviewer:             "bruce",
			Model:                "m",
			ConsensusLevel:       reclib.ConsensusStrict,
			FindingsRaised:       1,
			FindingsCorroborated: 1,
		}))
	}
	// Current era: the same reviewer, with its phantoms now in the denominator.
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:            SchemaVersion,
			RecordType:               RecordTypeReviewer,
			Outcome:                  outcomeFindings,
			RunID:                    fmt.Sprintf("2026-07-02T00:00:00Z-new%02d", i),
			Reviewer:                 "bruce",
			Model:                    "m",
			ConsensusLevel:           reclib.ConsensusStrict,
			RaisedIncludesUnresolved: true,
			FindingsRaised:           4,
			FindingsCorroborated:     1,
		}))
	}

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, priors, "bruce")
	assert.InDelta(t, 0.25, priors["bruce"], 0.0001,
		"only the current-definition records may contribute; blending the two eras gives 0.4")
}

// TestTrustPriors_PreUnresolvedOnlyHistoryStillCounts pins the other half of the
// prefer-current rule: a store that predates the denominator change entirely is
// still used, unchanged.
//
// Requiring the flag would black out every existing reviewer history the moment
// this ships — the same stranding the empty-consensus_level rule above exists to
// avoid — and a pre-epic-only window is at least internally consistent. It is the
// MIX that has no meaning, and the mix is what the test above rules out.
func TestTrustPriors_PreUnresolvedOnlyHistoryStillCounts(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			Outcome:              outcomeFindings,
			RunID:                fmt.Sprintf("2026-07-01T00:00:00Z-old%02d", i),
			Reviewer:             "bruce",
			Model:                "m",
			ConsensusLevel:       reclib.ConsensusStrict,
			FindingsRaised:       1,
			FindingsCorroborated: 1,
		}))
	}
	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, priors, "bruce")
	assert.InDelta(t, 1.0, priors["bruce"], 0.0001,
		"with no current-era record in the window, the pre-epic history is used as it always was")
}

// TestTrustPriors_EraFilterIsPerReviewerNotGlobal pins that the era filter never
// lets ONE reviewer's upgrade black out ANOTHER reviewer's history.
//
// unresolvedEraRuns partitioned the whole record slice at once: if any record
// anywhere carried the flag, every unflagged record was dropped — across all
// reviewers. So the first post-upgrade run, which flags only the reviewers on
// that panel, erased every other reviewer's entire history. Those reviewers left
// `byReviewer` altogether, so they were absent from the prior map at ANY minRuns,
// and absent is not neutral: trustExempt (reconcile/consensus.go:246) reads a
// missing key as "not exempt" and demoteByTrust (consensus.go:263) reads the same
// missing key as "do not demote", so a low-trust phantom-raiser silently stopped
// being demoted — the opposite of what putting phantoms in the denominator was
// for.
//
// The prefer-current rule is per reviewer: each reviewer's own records are asked
// whether THEY hold a current-era one.
func TestTrustPriors_EraFilterIsPerReviewerNotGlobal(t *testing.T) {
	dir := t.TempDir()

	// bruce: pre-epic history only. It has not run since the upgrade.
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			Outcome:              outcomeFindings,
			RunID:                fmt.Sprintf("2026-07-01T00:00:00Z-bruce%02d", i),
			Reviewer:             "bruce",
			Model:                "m",
			ConsensusLevel:       reclib.ConsensusStrict,
			FindingsRaised:       4,
			FindingsCorroborated: 1,
		}))
	}
	// greta: one current-era record, enough to trip the global partition.
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:            SchemaVersion,
			RecordType:               RecordTypeReviewer,
			Outcome:                  outcomeFindings,
			RunID:                    fmt.Sprintf("2026-07-02T00:00:00Z-greta%02d", i),
			Reviewer:                 "greta",
			Model:                    "m",
			ConsensusLevel:           reclib.ConsensusStrict,
			RaisedIncludesUnresolved: true,
			FindingsRaised:           2,
			FindingsCorroborated:     1,
		}))
	}

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)

	require.Contains(t, priors, "bruce",
		"greta crossing into the current era must not erase bruce's history: absent is read as 'no history' by every consumer")
	assert.InDelta(t, 0.25, priors["bruce"], 0.0001,
		"bruce's own records are all pre-epic and internally consistent, so they are used unchanged")
	require.Contains(t, priors, "greta")
	assert.InDelta(t, 0.5, priors["greta"], 0.0001)
}

// TestTrustPriors_PerReviewerPreferCurrentStillExcludesTheMix pins the other half:
// making the partition per reviewer must not weaken it. A reviewer holding BOTH
// eras still contributes only its current-era records — the mix is the one
// combination that measures neither definition.
func TestTrustPriors_PerReviewerPreferCurrentStillExcludesTheMix(t *testing.T) {
	dir := t.TempDir()

	// bruce spans the change: a flattering pre-epic half and a current-era half.
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			Outcome:              outcomeFindings,
			RunID:                fmt.Sprintf("2026-07-01T00:00:00Z-old%02d", i),
			Reviewer:             "bruce",
			Model:                "m",
			ConsensusLevel:       reclib.ConsensusStrict,
			FindingsRaised:       1,
			FindingsCorroborated: 1,
		}))
	}
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:            SchemaVersion,
			RecordType:               RecordTypeReviewer,
			Outcome:                  outcomeFindings,
			RunID:                    fmt.Sprintf("2026-07-02T00:00:00Z-new%02d", i),
			Reviewer:                 "bruce",
			Model:                    "m",
			ConsensusLevel:           reclib.ConsensusStrict,
			RaisedIncludesUnresolved: true,
			FindingsRaised:           4,
			FindingsCorroborated:     1,
		}))
	}
	// carol never crossed over, and must keep its own pre-epic history.
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion:        SchemaVersion,
			RecordType:           RecordTypeReviewer,
			Outcome:              outcomeFindings,
			RunID:                fmt.Sprintf("2026-07-01T00:00:00Z-carol%02d", i),
			Reviewer:             "carol",
			Model:                "m",
			ConsensusLevel:       reclib.ConsensusStrict,
			FindingsRaised:       5,
			FindingsCorroborated: 1,
		}))
	}

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	assert.InDelta(t, 0.25, priors["bruce"], 0.0001,
		"bruce holds both eras, so only its current-era records count; blending gives 0.4")
	assert.InDelta(t, 0.2, priors["carol"], 0.0001,
		"carol holds no current-era record, so its own pre-epic history is used unchanged")
}

// TestUnresolvedEraRuns_SkipsAggregateRecords pins the record-class gate on the
// era pass. Emit stamps the aggregate record with RaisedDenominator = Current,
// and the aggregate's Reviewer is empty — so without a skip it participates in
// the newest-per-reviewer computation under the "" key, and every reviewer
// record with an empty name (era 1, unflagged) shares that key and reads as
// OLDER than the aggregate. trustPriorsSince feeds strictRuns straight into
// unresolvedEraRuns (PublishedSet's ApplyFilters has already dropped aggregates
// on the other call site), so the two call sites disagreed about when aggregate
// records are removed — pass order decided whether an empty-name reviewer's
// history survived.
func TestUnresolvedEraRuns_SkipsAggregateRecords(t *testing.T) {
	agg := Record{
		SchemaVersion: SchemaVersion, RecordType: RecordTypeAggregate,
		RunID: "2026-09-02T00:00:00Z-agg",
		// The aggregate is stamped with the CURRENT definition at emit time.
		RaisedIncludesUnresolved: true,
		RaisedDenominator:        RaisedDenominatorCurrent,
		FindingsRaised:           9,
	}
	emptyNameEra1 := Record{
		SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
		RunID: "2026-09-02T00:00:00Z-anon", Reviewer: "", Model: "m",
		FindingsRaised: 2, // no era markers: definition 1
	}

	got := unresolvedEraRuns([]Record{agg, emptyNameEra1})

	var sawReviewer, sawAggregate bool
	for _, r := range got {
		switch r.RecordType {
		case RecordTypeReviewer:
			sawReviewer = true
		case RecordTypeAggregate:
			sawAggregate = true
		}
	}
	assert.True(t, sawReviewer,
		"the empty-name era-1 reviewer record must survive: the aggregate is not a reviewer and must not define its newest era")
	assert.True(t, sawAggregate,
		"the aggregate record passes through untouched — the era pass is a reviewer-record concern")
}

// TestUnresolvedEraRuns_ExcludesAboveCurrentDenominators pins the drop-and-exclude
// rule for records stamped with a denominator this binary does not know: a
// LEGITIMATE record written by a newer atcr (denominator 4), a corrupt hand-edit
// (999), and a benchmark-suite value (100) are all EXCLUDED from the era window
// rather than clamped into the current cohort and blended. The clamp's
// protective intent survives — a garbage line still cannot delete the reviewer's
// real history — but exclusion no longer re-labels a future era as the current
// one.
func TestUnresolvedEraRuns_ExcludesAboveCurrentDenominators(t *testing.T) {
	mk := func(runID string, denom int) Record {
		return Record{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
			RunID: runID, Reviewer: "bruce", Model: "m",
			RaisedIncludesUnresolved: true, RaisedDenominator: denom,
			FindingsRaised: 2, FindingsCorroborated: 1,
		}
	}
	current := mk("2026-09-02T00:00:00Z-cur", RaisedDenominatorCurrent)

	for name, alien := range map[string]Record{
		"newer-binary record (denominator 4)": mk("2026-09-02T00:00:00Z-n4", RaisedDenominatorCurrent+1),
		"corrupt hand-edit (999)":             mk("2026-09-02T00:00:00Z-c9", 999),
		"benchmark-suite value (100)":         mk("2026-09-02T00:00:00Z-bm", RaisedDenominatorBenchmarkSuite),
	} {
		t.Run(name, func(t *testing.T) {
			got := unresolvedEraRuns([]Record{current, alien})
			require.Len(t, got, 1, "the above-current record must be excluded, not blended into the current cohort")
			assert.Equal(t, RaisedDenominatorCurrent, got[0].RaisedDenominator,
				"the genuine current-era record survives untouched")
		})
	}

	// A reviewer with ONLY above-current records keeps none of them in the era
	// window — an older binary must not blend numbers computed under a rule it
	// does not implement. (It also must not relabel them current: the clamp is
	// gone for this class.)
	t.Run("only-above-current reviewer yields nothing", func(t *testing.T) {
		got := unresolvedEraRuns([]Record{mk("2026-09-02T00:00:00Z-x1", RaisedDenominatorCurrent+1)})
		assert.Empty(t, got)
	})

	// The FIRST loop's exclusion, isolated. Both loops carry the same
	// above-current test, and every case above is satisfied by the second one
	// alone — deleting the first loop's copy leaves them all green.
	//
	// What only the first loop decides is what `newest` becomes. Without its
	// exclusion, raisedDenominatorOf CLAMPS the above-current record to
	// RaisedDenominatorCurrent, so newest[reviewer] reads 3 and the second loop
	// then drops every pre-epic record of that reviewer for being an older era.
	// One garbage line would delete the reviewer's whole real history — precisely
	// the outcome the guard's own comment says exclusion prevents.
	t.Run("an above-current record does not delete the reviewer's pre-epic history", func(t *testing.T) {
		preEpic := func(runID string) Record {
			return Record{
				SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
				RunID: runID, Reviewer: "bruce", Model: "m",
				// No era markers at all: definition 1 (pre-epic).
				FindingsRaised: 2, FindingsCorroborated: 1,
			}
		}
		in := []Record{
			mk("2026-09-02T00:00:00Z-alien", RaisedDenominatorCurrent+1),
			preEpic("2026-08-01T00:00:00Z-p1"),
			preEpic("2026-08-02T00:00:00Z-p2"),
			preEpic("2026-08-03T00:00:00Z-p3"),
		}

		got := unresolvedEraRuns(in)

		require.Len(t, got, 3, "all three pre-epic records must survive — the above-current record must not define this reviewer's newest era")
		for _, r := range got {
			assert.Equal(t, raisedDenominatorPreEpic, raisedDenominatorOf(r),
				"only pre-epic records may survive: %s", r.RunID)
		}
	})
}

// TestTrustPriors_ShieldedCountsDiscountTheRate pins the trust-side answer to the
// doc-shield carve-out: a reviewer can route fabrications through the
// documentation-extension heuristic so they escape FindingsRaised, but they must
// NOT escape the trust prior. The shielded count joins the trust rate's
// denominator — the scorecard/board rate keeps the carve-out, the trust rate
// does not. Without this, a reviewer (or a board gamer) inflates their prior by
// anchoring phantoms on doc-named tokens.
func TestTrustPriors_ShieldedCountsDiscountTheRate(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
			RunID:    fmt.Sprintf("2026-09-01T00:00:00Z-sh%02d", i),
			Reviewer: "gamer", Model: "m",
			ConsensusLevel:           reclib.ConsensusStrict,
			RaisedIncludesUnresolved: true,
			RaisedDenominator:        RaisedDenominatorCurrent,
			FindingsRaised:           2,
			FindingsCorroborated:     2, // everything chargeable corroborated
			FindingsDocShielded:      2, // but two more routed through the doc shield
		}))
	}

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, priors, "gamer")
	assert.InDelta(t, 0.5, priors["gamer"], 0.0001,
		"2 corroborated out of 2 raised + 2 shielded: the shield does not launder phantoms into a clean 1.00 prior")
}

// TestTrustPriors_RoutedErasAreOneWindow pins the removal of the priors blackout
// between raised_denominator 2 and 3.
//
// The two eras are ARITHMETICALLY equivalent for the trust rate: an era-3 record
// partitions the same finding set into FindingsRaised + FindingsDocShielded that
// an era-2 record put entirely into FindingsRaised (scorecard.go partitions
// in.UnresolvedFindings disjointly), and FindingsCorroborated is unchanged. The
// trust denominator counts both halves, so normalising era 3 back to era 2 before
// the prefer-newest pass changes no rate — it only stops the pass from discarding
// a reviewer's whole pre-upgrade window the day its first era-3 record lands.
//
// That window is what keeps the reviewer above DefaultTrustMinRuns, and below the
// floor it drops OUT of the map, where consensus.go reads its absence as "not
// exempt" and "do not demote".
func TestTrustPriors_RoutedErasAreOneWindow(t *testing.T) {
	dir := t.TempDir()

	// Era 2 (RaisedIncludesUnresolved, no denominator field): the bulk of the
	// window. On its own this is DefaultTrustMinRuns-1 runs — under the floor.
	for i := 0; i < DefaultTrustMinRuns-1; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
			RunID:    fmt.Sprintf("2026-08-01T00:00:00Z-era2-%02d", i),
			Reviewer: "bruce", Model: "m",
			ConsensusLevel:           reclib.ConsensusStrict,
			RaisedIncludesUnresolved: true,
			FindingsRaised:           4,
			FindingsCorroborated:     2,
		}))
	}
	// Era 3: a single post-upgrade run, with the doc-shield split populated.
	// 3 chargeable + 1 shielded is the same finding set an era-2 record would
	// have reported as FindingsRaised: 4.
	require.NoError(t, Append(dir, Record{
		SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
		RunID:    "2026-08-20T00:00:00Z-era3-00",
		Reviewer: "bruce", Model: "m",
		ConsensusLevel:           reclib.ConsensusStrict,
		RaisedIncludesUnresolved: true,
		RaisedDenominator:        RaisedDenominatorCurrent,
		FindingsRaised:           3,
		FindingsCorroborated:     2,
		FindingsDocShielded:      1,
	}))

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)

	require.Contains(t, priors, "bruce",
		"one era-3 run must not black out the era-2 window: the two eras measure the same quantity, so the reviewer stays above the min-runs floor")
	assert.InDelta(t, 0.5, priors["bruce"], 0.0001,
		"every run is 2 corroborated out of a 4-finding denominator, in both eras")
}

// TestTrustPriors_PreEpicStillSplitsFromRoutedEras is the other side of the
// normalisation: era 1 is NOT arithmetically equivalent to eras 2 and 3. It
// excludes routed findings from FindingsRaised entirely rather than partitioning
// them, so blending it in would compare a rate over one finding set against a rate
// over a larger one. It must keep splitting exactly as before.
func TestTrustPriors_PreEpicStillSplitsFromRoutedEras(t *testing.T) {
	dir := t.TempDir()

	// A flattering pre-epic half...
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
			RunID:    fmt.Sprintf("2026-07-01T00:00:00Z-pre%02d", i),
			Reviewer: "bruce", Model: "m",
			ConsensusLevel:       reclib.ConsensusStrict,
			FindingsRaised:       4,
			FindingsCorroborated: 4, // rate 1.00 if it were blended in
		}))
	}
	// ...and an era-3 half that is the only thing the priors may measure.
	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
			RunID:    fmt.Sprintf("2026-08-01T00:00:00Z-cur%02d", i),
			Reviewer: "bruce", Model: "m",
			ConsensusLevel:           reclib.ConsensusStrict,
			RaisedIncludesUnresolved: true,
			RaisedDenominator:        RaisedDenominatorCurrent,
			FindingsRaised:           4,
			FindingsCorroborated:     1,
		}))
	}

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, priors, "bruce")
	assert.InDelta(t, 0.25, priors["bruce"], 0.0001,
		"the pre-epic half must stay excluded — 0.625 would be the blend, 1.00 the pre-epic half alone")
}

// TestMergeRoutedEras_PinsEveryElementOfItsGuard covers the four things
// mergeRoutedEras does that only its docstring asserted.
//
// Only the fold (FindingsRaised += FindingsDocShielded) was pinned when the
// function landed. Every other element survived mutation with the whole suite
// green: the above-current exclusion, the non-reviewer skip, the defensive copy,
// and the two bookkeeping writes. This test kills each mutation.
//
// The above-current arm is the one that matters. Relaxing `!=` to `<` normalises
// a record from a NEWER atcr into era 2, which then walks straight past
// unresolvedEraRuns' above-current exclusion and into the trust window — the
// exact smuggling the docstring says must not happen. The consequence is pinned
// end-to-end in TestTrustPriors_AboveCurrentRecordsNeverReachThePrior below.
func TestMergeRoutedEras_PinsEveryElementOfItsGuard(t *testing.T) {
	t.Run("a current-era reviewer record is rewritten whole", func(t *testing.T) {
		in := []Record{{
			RecordType: RecordTypeReviewer,
			Outcome:    outcomeFindings, Reviewer: "bruce",
			RaisedDenominator:   RaisedDenominatorCurrent,
			FindingsRaised:      3,
			FindingsDocShielded: 1,
		}}
		got := mergeRoutedEras(in)

		require.Len(t, got, 1)
		assert.Equal(t, 4, got[0].FindingsRaised,
			"the shielded count folds in — that is what makes a plain t.raised the full trust denominator")
		assert.Equal(t, 0, got[0].FindingsDocShielded,
			"zeroed, so nothing downstream can charge the same finding twice")
		assert.Equal(t, raisedDenominatorAllRouted, got[0].RaisedDenominator,
			"the record now IS an era-2 record and must say so")
		assert.True(t, got[0].RaisedIncludesUnresolved,
			"era 2's own discriminator: a reader falling back to the bool must reach the same era as one reading the int")
	})

	t.Run("the rewritten record stays internally consistent", func(t *testing.T) {
		// The fold moves FindingsRaised, so the two fields DERIVED from it stop
		// agreeing with it unless they move too. Nothing in trustPriorsSince reads
		// either one today, but the value is a Record — a type whose other
		// consumers (cli/scorecard.go's SOLO and CORR% columns, Aggregate's
		// per-record sum) do read them, so a self-contradicting Record is a trap
		// laid for the next caller rather than a harmless omission.
		//
		// The recomputed values are exactly what era 2 reported: a doc-shielded
		// finding was routed, so it is uncorroborated by construction and belongs
		// in solo — the same disjoint partition the fold's equivalence rests on.
		in := []Record{{
			RecordType: RecordTypeReviewer,
			Outcome:    outcomeFindings, Reviewer: "bruce",
			RaisedDenominator:    RaisedDenominatorCurrent,
			FindingsRaised:       3,
			FindingsCorroborated: 2,
			FindingsSolo:         1,
			CorroborationRate:    2.0 / 3.0,
			FindingsDocShielded:  1,
		}}
		got := mergeRoutedEras(in)

		require.Len(t, got, 1)
		require.Equal(t, 4, got[0].FindingsRaised)
		assert.Equal(t, 2, got[0].FindingsSolo,
			"solo is raised minus corroborated, and the folded-in shielded finding was routed, so it is solo")
		assert.InDelta(t, 0.5, got[0].CorroborationRate, 0.0001,
			"2 of 4 under the merged denominator — the stale 0.667 describes a denominator this record no longer has")
	})

	t.Run("an above-current record is left alone", func(t *testing.T) {
		in := []Record{{
			RecordType: RecordTypeReviewer,
			Outcome:    outcomeFindings, Reviewer: "bruce",
			RaisedDenominator:   RaisedDenominatorCurrent + 1,
			FindingsRaised:      7,
			FindingsDocShielded: 2,
		}}
		got := mergeRoutedEras(in)

		require.Len(t, got, 1)
		assert.Equal(t, RaisedDenominatorCurrent+1, got[0].RaisedDenominator,
			"normalising this would smuggle a definition this binary does not implement past unresolvedEraRuns' exclusion")
		assert.Equal(t, 7, got[0].FindingsRaised, "no fold: the equivalence is unproven for this era")
		assert.Equal(t, 2, got[0].FindingsDocShielded)
	})

	t.Run("a non-reviewer record is left alone", func(t *testing.T) {
		// An aggregate record is stamped RaisedDenominator = Current under the
		// EMPTY reviewer name, so it matches the era half of the guard and is
		// excluded by the record-type half alone.
		in := []Record{{
			RecordType:          RecordTypeAggregate,
			RaisedDenominator:   RaisedDenominatorCurrent,
			FindingsRaised:      9,
			FindingsDocShielded: 3,
		}}
		got := mergeRoutedEras(in)

		require.Len(t, got, 1)
		assert.Equal(t, RaisedDenominatorCurrent, got[0].RaisedDenominator)
		assert.Equal(t, 9, got[0].FindingsRaised)
		assert.Equal(t, 3, got[0].FindingsDocShielded)
	})

	t.Run("the caller's slice is never mutated", func(t *testing.T) {
		in := []Record{{
			RecordType: RecordTypeReviewer,
			Outcome:    outcomeFindings, Reviewer: "bruce",
			RaisedDenominator:   RaisedDenominatorCurrent,
			FindingsRaised:      3,
			FindingsDocShielded: 1,
		}}
		want := in[0]

		got := mergeRoutedEras(in)

		require.Len(t, got, 1)
		require.Equal(t, 4, got[0].FindingsRaised, "the copy really was rewritten")
		assert.Equal(t, want, in[0],
			"callers hand in records read from the store and must not see them rewritten underneath")
	})
}

// TestTrustPriors_AboveCurrentRecordsNeverReachThePrior is the consequence half
// of the guard above, measured where it is actually paid.
//
// mergeRoutedEras runs BEFORE unresolvedEraRuns, so it is the last place an
// above-current record can be re-labelled into an era the exclusion no longer
// recognises. With the guard relaxed the record below is normalised to era 2 and
// joins the window, dragging the prior from 0.500 to 0.357 — and a wrong prior
// re-weights demoteByTrust and trustExempt on every later run, which is how real
// findings get filtered out of report.md.
func TestTrustPriors_AboveCurrentRecordsNeverReachThePrior(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < DefaultTrustMinRuns; i++ {
		require.NoError(t, Append(dir, Record{
			SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
			RunID:    fmt.Sprintf("2026-08-01T00:00:00Z-cur%02d", i),
			Reviewer: "bruce", Model: "m",
			ConsensusLevel:           reclib.ConsensusStrict,
			RaisedIncludesUnresolved: true,
			RaisedDenominator:        RaisedDenominatorCurrent,
			FindingsRaised:           10,
			FindingsCorroborated:     5,
		}))
	}
	// One record from a NEWER atcr, under a definition this binary does not
	// implement. A hand-edit or a benchmark-stamped denominator reads the same.
	require.NoError(t, Append(dir, Record{
		SchemaVersion: SchemaVersion, RecordType: RecordTypeReviewer, Outcome: outcomeFindings,
		RunID:    "2026-08-20T00:00:00Z-future",
		Reviewer: "bruce", Model: "m",
		ConsensusLevel:           reclib.ConsensusStrict,
		RaisedIncludesUnresolved: true,
		RaisedDenominator:        RaisedDenominatorCurrent + 1,
		FindingsRaised:           100,
		FindingsCorroborated:     0,
	}))

	priors, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)

	require.Contains(t, priors, "bruce")
	assert.InDelta(t, 0.5, priors["bruce"], 0.0001,
		"the above-current record must be excluded outright — 0.357 is what its 100 raised buys if it is normalised into era 2")
}

// --- Phase 3 (Story 03): the opportunity-set filter-chain link --------------
//
// opportunitySetRuns is its own func([]Record) []Record sibling link, not a
// condition inside the tally loop, per the sprint's Implementation Standards.

// opportunityFilter composes the split opportunity link exactly the way
// trustPriorsSince does — union from the records handed in, then filter — so a
// direct-call test exercises the shipped composition rather than a shape only
// the test knows how to build.
func opportunityFilter(records []Record) []Record {
	return opportunitySetRuns(records, opportunityUnions(records))
}

// oppRec builds a schema-2 reviewer record for the opportunity-link tests.
// oppRec builds one reviewer record for the opportunity-set tests. Every call
// site that passes nil cats means "a SILENT lens", so a nil cats must produce a
// record that actually IS silent: zero raised and outcome clean.
//
// It used to stamp FindingsRaised: 1 unconditionally, which made its "silent"
// fixture a record no emitter could write — a reviewer that raised a finding
// always carries that finding's category unless the word was outside
// reclib.Categories(), and reviewerCategories drops only the word, never the
// count. Nothing read FindingsRaised in this gate, so the inconsistency was
// invisible until TD-032's fix made it the discriminator between "silent, out of
// remit" and "raised findings nobody could attribute". The same rule already
// governs the sibling helper: reviewer() picks outcome clean for a zero-raised
// fixture "or the helper produces a record no emitter could ever write".
func oppRec(runID, reviewer string, cats []string) Record {
	raised, outcome := 1, outcomeFindings
	if len(cats) == 0 {
		raised, outcome = 0, outcomeClean
	}
	return Record{
		SchemaVersion:            SchemaVersion,
		RecordType:               RecordTypeReviewer,
		RunID:                    runID,
		Reviewer:                 reviewer,
		Outcome:                  outcome,
		CategoriesRaised:         cats,
		RaisedIncludesUnresolved: true,
		RaisedDenominator:        RaisedDenominatorCurrent,
		FindingsRaised:           raised,
	}
}

// oppRecRaw builds a record with raised and cats set INDEPENDENTLY, which is the
// one thing oppRec and oppRecUnlabelled between them cannot do.
//
// It exists because that coupling hid a real defect. oppRec derives raised from
// cats (empty cats means a silent record) and oppRecUnlabelled fixes cats empty,
// so no fixture in this file could construct a record that raised NOTHING and
// still contributed a category — and that cell is reachable in production, via
// the category-only ambiguous stream. A gate round changed the disposition of
// exactly that cell and the entire package plus cli/ stayed green.
//
// The shape is emitter-real, not a probe convenience: EmitForReconcile fills
// EmitInput.AmbiguousFindings from res.Ambiguous, and Emit routes that stream
// into reviewerCategories only, never into a reviewerCounts call.
//
// Outcome follows the emitter too, and the rule is narrower than it looks. A
// record that CONTRIBUTED a category keeps outcome "findings" — the agent did
// produce findings and they were routed — so only the raised-nothing AND
// attributed-nothing case is stamped clean. outcomeClean requires
// AgentStatus.FindingsCount <= 0, which is exactly the case that can contribute
// no ambiguous category either, so clean + zero raised + a non-empty
// discriminating category set is a combination no emitter can write.
// opportunityDisposition reads none of this, but a fixture that could not exist
// is how two defects in this file were already hidden once each.
func oppRecRaw(runID, reviewer string, raised int, cats []string) Record {
	r := oppRec(runID, reviewer, []string{"placeholder"})
	r.CategoriesRaised = cats
	r.FindingsRaised = raised
	if raised == 0 && len(cats) == 0 {
		// Genuinely silent: raised nothing and attributed nothing.
		r.Outcome = outcomeClean
	}
	return r
}

// oppRecUnlabelled is the case oppRec deliberately cannot express: a lens that
// RAISED findings whose every CATEGORY the scorer could not use, so the count is
// non-zero while CategoriesRaised is empty. It is TD-032's subject, and keeping
// it a separate helper stops a future edit from quietly reintroducing the
// inconsistent fixture oppRec's comment describes.
func oppRecUnlabelled(runID, reviewer string, raised int) Record {
	r := oppRec(runID, reviewer, []string{"placeholder"})
	r.CategoriesRaised = nil
	r.FindingsRaised = raised
	return r
}

// TestOpportunitySetRuns_DropsOutOfRemitRecords is the epic's headline property
// expressed on the chain: a mapped lens whose remit no reviewer touched on that
// run leaves the denominator entirely.
func TestOpportunitySetRuns_DropsOutOfRemitRecords(t *testing.T) {
	in := []Record{
		oppRec("run-1", "sasha", nil),                     // silent security lens
		oppRec("run-1", "penny", []string{"performance"}), // raised the only category
	}
	out := opportunityFilter(in)

	names := map[string]bool{}
	for _, r := range out {
		names[r.Reviewer] = true
	}
	assert.True(t, names["penny"], "performance was in play, so penny is scored")
	assert.False(t, names["sasha"], "no security category was raised by anyone, so sasha is not scored")
}

// TestOpportunitySetRuns_KeepsASilentLensWhenItsRemitWasInPlay proves membership
// is a property of the CASE, not of what the lens itself said. A security lens
// that stayed silent while another reviewer raised `security` IS scored — that
// silence is a judgement, and scoring it is the whole point of the denominator.
func TestOpportunitySetRuns_KeepsASilentLensWhenItsRemitWasInPlay(t *testing.T) {
	in := []Record{
		oppRec("run-1", "sasha", nil),
		// bruce RAISED security even though security is not in bruce's own
		// remit: CategoriesRaised records what a lens said, not what it is
		// scored on. correctness is what puts bruce itself in remit here.
		oppRec("run-1", "bruce", []string{"security", "correctness"}),
	}
	out := opportunityFilter(in)

	names := map[string]bool{}
	for _, r := range out {
		names[r.Reviewer] = true
	}
	assert.True(t, names["sasha"], "sasha's remit was in play via bruce's finding")
	assert.True(t, names["bruce"], "bruce's remit covers security too")
}

// TestOpportunitySetRuns_UnionIsPerRunNotGlobal pins the grouping key. Unioning
// across the whole store instead of per RunID would make every lens in-remit
// forever after one broad run.
func TestOpportunitySetRuns_UnionIsPerRunNotGlobal(t *testing.T) {
	in := []Record{
		oppRec("run-1", "bruce", []string{"security"}),
		oppRec("run-2", "sasha", nil),
		oppRec("run-2", "penny", []string{"performance"}),
	}
	out := opportunityFilter(in)

	for _, r := range out {
		assert.False(t, r.RunID == "run-2" && r.Reviewer == "sasha",
			"run-1's security category must not make sasha in-remit on run-2")
	}
}

// TestOpportunitySetRuns_UnmappedPersonaPassesThroughUntouched is the
// no-silent-regression guard for C11. vera and the four other registry-only
// lenses have no remit under Option A; dropping their records would remove trust
// scoring for five of thirteen lenses as a side effect of this link. Unmapped
// means "not opportunity-scoped", not "deleted".
func TestOpportunitySetRuns_UnmappedPersonaPassesThroughUntouched(t *testing.T) {
	in := []Record{
		oppRec("run-1", "vera", nil),
		oppRec("run-1", "penny", []string{"performance"}),
	}
	out := opportunityFilter(in)

	names := map[string]bool{}
	for _, r := range out {
		names[r.Reviewer] = true
	}
	assert.True(t, names["vera"],
		"an unmapped lens keeps the behaviour it had before this link existed")
}

// TestOpportunitySetRuns_AggregatesPassThroughUntouched matches the documented
// precedent of eligibleOutcomeRuns and unresolvedEraRuns. An aggregate is not a
// reviewer and carries no remit.
func TestOpportunitySetRuns_AggregatesPassThroughUntouched(t *testing.T) {
	agg := Record{SchemaVersion: SchemaVersion, RecordType: RecordTypeAggregate, RunID: "run-1"}
	out := opportunityFilter([]Record{agg, oppRec("run-1", "sasha", nil)})

	found := false
	for _, r := range out {
		if r.RecordType == RecordTypeAggregate {
			found = true
		}
	}
	assert.True(t, found, "aggregates are never judged on a property they do not carry")
}

// TestOpportunitySetRuns_UnmeasuredRecordsAreNotJudgedAsOutOfRemit is the era
// discipline Phase 2 applied to an empty Outcome, applied here to an absent
// category set. A pre-schema-2 record has no categories because nothing measured
// them — reading that as "out-of-remit for everyone" would silently shrink every
// persona's denominator across the whole back-catalogue.
func TestOpportunitySetRuns_UnmeasuredRecordsAreNotJudgedAsOutOfRemit(t *testing.T) {
	old := oppRec("run-old", "sasha", nil)
	old.SchemaVersion = 1

	out := opportunityFilter([]Record{old})
	require.Len(t, out, 1, "an unmeasured record passes through rather than being judged")
	assert.Equal(t, 1, out[0].SchemaVersion)
}

// TestOpportunitySetRuns_DoesNotMutateItsInput matches every sibling link's
// documented contract: callers hand in records read from the store.
func TestOpportunitySetRuns_DoesNotMutateItsInput(t *testing.T) {
	in := []Record{
		oppRec("run-1", "sasha", nil),
		oppRec("run-1", "penny", []string{"performance"}),
	}
	// DEEP copy. append([]Record{}, in...) copies the structs but shares every
	// CategoriesRaised backing array, so an in-place sort or dedupe inside the
	// link would mutate both sides and this assertion would still pass — the
	// guard would report exactly nothing.
	before := make([]Record, len(in))
	for i, r := range in {
		before[i] = r
		before[i].CategoriesRaised = append([]string(nil), r.CategoriesRaised...)
	}
	opportunityFilter(in)
	assert.Equal(t, before, in, "the input slice is never rewritten")
}

// TestTrustPriors_OutOfRemitRunNeverReachesThePrior is the end-to-end proof that
// the link is actually wired into trustPriorsSince, not merely defined. Without
// the wiring every assertion above passes and the feature ships inert.
func TestTrustPriors_OutOfRemitRunNeverReachesThePrior(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	month := filepath.Join(dir, now.Format("2006-01")+".jsonl")

	var lines []string
	for i := 0; i < 3; i++ {
		runID := fmt.Sprintf("run-%d", i)
		// Only performance is ever raised, so sasha is out-of-remit on every run.
		for _, r := range []Record{
			oppRec(runID, "sasha", nil),
			oppRec(runID, "penny", []string{"performance"}),
		} {
			r.RunID = runID
			b, err := json.Marshal(r)
			require.NoError(t, err)
			lines = append(lines, string(b))
		}
	}
	require.NoError(t, os.WriteFile(month, []byte(strings.Join(lines, "\n")+"\n"), 0o600))

	priors, err := trustPriorsSince(dir, 1, 180*24*time.Hour, now, nil)
	require.NoError(t, err)
	assert.NotContains(t, priors, "sasha",
		"every one of sasha's runs was out-of-remit, so it earns no durable prior")
	assert.Contains(t, priors, "penny")
}

// TestOpportunitySetRuns_ControlOnlyUnionBlacksOutNobody is the regression guard
// for the worst bug this link can have.
//
// `other` and `out-of-scope` are full reclib.Categories() members, so they clear
// the vocabulary gate and make a run's union NON-empty — but neither names a
// topic, so neither matches any remit. Judged, such a run deletes every MAPPED
// lens's record while the unmapped ones keep theirs: a wrong durable score that
// favours precisely the lenses with no remit. ModalCategory returns out-of-scope
// for any cluster whose findings are all out of scope, so this is reachable
// without a single reviewer typing the word.
func TestOpportunitySetRuns_ControlOnlyUnionBlacksOutNobody(t *testing.T) {
	for _, control := range []string{reclib.CategoryOther, reclib.CategoryOutOfScope, reclib.CategoryInvariant} {
		in := []Record{
			oppRec("run-1", "sasha", []string{control}),
			oppRec("run-1", "penny", []string{control}),
			oppRec("run-1", "dax", nil),
		}
		out := opportunityFilter(in)
		assert.Len(t, out, len(in),
			"a union of only %q carries no topic, so every lens must pass through un-scoped", control)
	}
}

// TestOpportunitySetRuns_ControlValuesDoNotMaskARealTopic is the other half of
// the guard above: the exclusion must drop the control values from the union,
// not abandon the whole run the moment one appears.
func TestOpportunitySetRuns_ControlValuesDoNotMaskARealTopic(t *testing.T) {
	in := []Record{
		oppRec("run-1", "sasha", []string{reclib.CategoryOther, "security"}),
		oppRec("run-1", "penny", nil),
	}
	out := opportunityFilter(in)

	names := map[string]bool{}
	for _, r := range out {
		names[r.Reviewer] = true
	}
	assert.True(t, names["sasha"], "security is still a real topic alongside the control value")
	assert.False(t, names["penny"], "no performance-flavoured category was raised")
}

// TestOpportunitySetRuns_UnionSeesReviewersTheOutcomeGateWillDrop pins the chain
// ORDER, which is the difference between scoring a case and scoring a filtered
// view of it.
//
// A truncated reviewer is excluded from trust scoring by eligibleOutcomeRuns —
// it did not get a fair attempt — but it can still have raised findings whose
// categories were recorded, and those categories are evidence about WHAT THE
// CASE WAS. Run this link after the outcome gate and that evidence is gone, so a
// specialist is dropped from a run where its remit demonstrably was in play.
func TestOpportunitySetRuns_UnionSeesReviewersTheOutcomeGateWillDrop(t *testing.T) {
	truncated := oppRec("run-1", "archer", []string{"security"})
	truncated.Outcome = "truncated"

	in := []Record{truncated, oppRec("run-1", "sasha", nil)}
	out := opportunityFilter(in)

	names := map[string]bool{}
	for _, r := range out {
		names[r.Reviewer] = true
	}
	assert.True(t, names["sasha"],
		"the truncated reviewer's category is still evidence the case was a security case")
}

// TestTrustPriors_OpportunityUnionIsTakenBeforeTheOutcomeGate pins the CHAIN
// COMPOSITION, which no direct call to opportunitySetRuns can reach.
//
// The only reviewer that raises a security category on this run is truncated,
// so eligibleOutcomeRuns will drop its record. If the opportunity link runs
// AFTER that gate it never sees the category, sasha looks out-of-remit, and
// sasha earns no prior — the specialist is punished for another lens's hosting
// failure. Composed the other way round, sasha keeps its runs.
func TestTrustPriors_OpportunityUnionIsTakenBeforeTheOutcomeGate(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	month := filepath.Join(dir, now.Format("2006-01")+".jsonl")

	var lines []string
	for i := 0; i < 3; i++ {
		runID := fmt.Sprintf("run-%d", i)
		// The truncated lens is the ONLY source of a security category.
		truncated := oppRec(runID, "archer", []string{"security"})
		truncated.Outcome = "truncated"
		// penny keeps the post-gate union NON-empty, which is what makes this
		// test bite: without it the run would fall into the "no discriminating
		// category" pass-through and sasha would survive either composition,
		// proving nothing about the order.
		penny := oppRec(runID, "penny", []string{"performance"})
		sasha := oppRec(runID, "sasha", nil)
		sasha.Outcome = outcomeClean
		sasha.FindingsRaised = 1
		sasha.FindingsCorroborated = 1
		for _, r := range []Record{truncated, penny, sasha} {
			b, err := json.Marshal(r)
			require.NoError(t, err)
			lines = append(lines, string(b))
		}
	}
	require.NoError(t, os.WriteFile(month, []byte(strings.Join(lines, "\n")+"\n"), 0o600))

	priors, err := trustPriorsSince(dir, 1, 180*24*time.Hour, now, nil)
	require.NoError(t, err)
	assert.Contains(t, priors, "sasha",
		"the truncated lens's category still proves this was a security case, so sasha was in remit")
	assert.NotContains(t, priors, "archer",
		"the truncated record itself is still excluded by the outcome gate")
	assert.Contains(t, priors, "penny", "penny's own remit was in play on every run")
}

// TestOpportunity_SchemaGateIsPinnedToTheIntroducingVersionNotTheMovingOne is
// the guard a `SchemaVersion: SchemaVersion` fixture can never be.
//
// Both halves of the opportunity link skip records below
// categoriesRaisedSinceSchema. Written as `< SchemaVersion` instead, the guard
// is correct only while that constant happens to equal 2 — and TD-030 already
// puts a v3 bump on Phase 4a's table. On that bump every v2 record, each
// carrying a genuinely measured category set, would be reclassified as
// unmeasured: the union would lose its evidence and every v2 record would pass
// through unjudged, switching opportunity scoping off for the whole
// back-catalogue. Every other test in this file builds fixtures with
// `SchemaVersion: SchemaVersion`, so they move with the constant and stay green
// through exactly that regression.
//
// The literal 2 below is therefore deliberate. Do not "tidy" it into the
// constant; that deletes the only thing this test asserts.
func TestOpportunity_SchemaGateIsPinnedToTheIntroducingVersionNotTheMovingOne(t *testing.T) {
	assert.Equal(t, 2, categoriesRaisedSinceSchema,
		"CategoriesRaised was introduced at schema 2; a later field gets its own constant rather than moving this one")

	// A v2 record with a measured category set, built at the LITERAL version.
	measured := oppRec("run-1", "sasha", []string{"security"})
	measured.SchemaVersion = 2
	other := oppRec("run-1", "penny", []string{"performance"})
	other.SchemaVersion = 2

	unions := opportunityUnions([]Record{measured, other})
	require.Contains(t, unions, "run-1",
		"a literal-v2 record must still contribute its categories to the union")
	assert.Contains(t, unions["run-1"], "security")

	// And it must still be JUDGED: dax's remit is untouched by this run, so it
	// is dropped. A record treated as unmeasured would pass through instead.
	dax := oppRec("run-1", "dax", nil)
	dax.SchemaVersion = 2
	out := opportunitySetRuns([]Record{dax}, unions)
	assert.Empty(t, out,
		"a literal-v2 record must be judged, not passed through as unmeasured")
}

// TestTrustPriors_EraIsDecidedBeforeTheOpportunityFilter pins the OTHER end of
// the split link: the filter half must run AFTER unresolvedEraRuns.
//
// FIXTURE PROVENANCE — synthetic, deliberately. The era-1 records below carry
// SchemaVersion 2, an Outcome and CategoriesRaised alongside the era-1
// discriminator (RaisedDenominator 0 / RaisedIncludesUnresolved false) — a
// combination Emit can never write, because it always stamps
// RaisedDenominatorCurrent and RaisedIncludesUnresolved on every record, and a
// genuine era-1 record predates categories_raised (schema 2) and Outcome
// (v2) entirely. This is a future-era stand-in, not an emitter-shaped record:
// the ordering under test lives in unresolvedEraRuns and opportunitySetRuns,
// which read the era discriminator and the category set independently of the
// schema/outcome fields, so the synthetic combination exercises the real link
// order without asserting anything about the impossible fields themselves. If
// a future era bump makes the combination real (era N records judged under a
// newer schema), this comment is the marker to update, and the test's premise
// becomes emitter-real rather than synthetic.
//
// sasha here has a high-scoring era-1 history (in remit, routed phantoms
// EXCLUDED from the denominator) and a current-era history that is out of remit.
// unresolvedEraRuns must see both and pick era 3 as sasha's newest definition,
// dropping the era-1 half; the opportunity filter then drops what is left,
// leaving sasha with no scoreable runs and therefore ABSENT.
//
// Compose it the other way round — opportunity filter first — and the era-3
// records are gone before the era pass runs, so era 1 becomes sasha's "newest"
// definition and its flattering pre-epic rate is published under the current
// one. That is a cross-era blend arrived at through a side door, and it can flip
// a phantom-raising lens from demoted to exempt.
func TestTrustPriors_EraIsDecidedBeforeTheOpportunityFilter(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	month := filepath.Join(dir, now.Format("2006-01")+".jsonl")

	var lines []string
	write := func(r Record) {
		b, err := json.Marshal(r)
		require.NoError(t, err)
		lines = append(lines, string(b))
	}
	for i := 0; i < 3; i++ {
		// Era 1: in remit (a security category is raised on the run) and perfect.
		runID := fmt.Sprintf("old-%d", i)
		old := oppRec(runID, "sasha", []string{"security"})
		old.RaisedIncludesUnresolved = false
		old.RaisedDenominator = 0 // absent discriminator == era 1
		old.FindingsRaised, old.FindingsCorroborated = 4, 4
		write(old)

		// Era 3 (current): out of remit — penny raises the run's only category.
		runID = fmt.Sprintf("new-%d", i)
		write(oppRec(runID, "penny", []string{"performance"}))
		write(oppRec(runID, "sasha", nil))
	}
	require.NoError(t, os.WriteFile(month, []byte(strings.Join(lines, "\n")+"\n"), 0o600))

	priors, err := trustPriorsSince(dir, 1, 180*24*time.Hour, now, nil)
	require.NoError(t, err)
	assert.NotContains(t, priors, "sasha",
		"sasha's newest era is the current one and every current-era run was out of remit, so it has no scoreable history; a present entry here means the era was decided from an opportunity-shrunk record set")
	assert.Contains(t, priors, "penny", "penny's remit was in play on the current-era runs")
}

// TestTrustPriors_OpportunityUnionSurvivesANonStrictRecord pins that the union
// is taken from the RAW records, upstream of strictRuns as well as of the
// outcome gate.
//
// strictRuns is a PER-RECORD filter: one record with an unrecognized
// consensus_level — a hand-edit, or a row from a future atcr — is dropped on its
// own. If the union were taken downstream of it, that single dropped record
// would delete its category from the case, and a specialist is then judged
// out-of-remit on a run where its remit demonstrably was in play. That is the
// identical hazard the outcome-gate ordering above exists to refuse.
func TestTrustPriors_OpportunityUnionSurvivesANonStrictRecord(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	month := filepath.Join(dir, now.Format("2006-01")+".jsonl")

	var lines []string
	for i := 0; i < 3; i++ {
		runID := fmt.Sprintf("run-%d", i)
		// The ONLY source of a security category on this run is a record
		// strictRuns will drop for its unrecognized consensus level.
		nonStrict := oppRec(runID, "archer", []string{"security"})
		nonStrict.ConsensusLevel = "not-a-consensus-level"
		// Keeps the post-strictRuns union non-empty, so the run cannot fall into
		// the no-discriminating-category pass-through and pass by accident.
		penny := oppRec(runID, "penny", []string{"performance"})
		sasha := oppRec(runID, "sasha", nil)
		sasha.Outcome = outcomeClean
		sasha.FindingsRaised, sasha.FindingsCorroborated = 1, 1
		for _, r := range []Record{nonStrict, penny, sasha} {
			b, err := json.Marshal(r)
			require.NoError(t, err)
			lines = append(lines, string(b))
		}
	}
	require.NoError(t, os.WriteFile(month, []byte(strings.Join(lines, "\n")+"\n"), 0o600))

	priors, err := trustPriorsSince(dir, 1, 180*24*time.Hour, now, nil)
	require.NoError(t, err)
	assert.Contains(t, priors, "sasha",
		"the non-strict record's category is still evidence this was a security case, so sasha was in remit")
	assert.NotContains(t, priors, "archer",
		"the non-strict record itself is still excluded by strictRuns")
}

// ---------------------------------------------------------------------------
// Phase 4b — C18's read-time ground-truth half
// ---------------------------------------------------------------------------

// weighted seeds n runs for persona, each raising raisedEach findings of which
// corroboratedEach carried a co-reviewer, and carrying creditEach weighted
// credit under the CURRENT credit era — i.e. what the post-4.5 emitter writes.
func weighted(t *testing.T, dir string, n int, persona string, raisedEach, corroboratedEach int, creditEach float64) {
	t.Helper()
	for i := 0; i < n; i++ {
		rec := reviewer_(runIDAt(time.Now(), fmt.Sprintf("w-%s-%03d", persona, i)), persona, "m1", raisedEach, corroboratedEach)
		rec.WeightedCredit = creditEach
		rec.CreditEra = CreditEraCurrent
		require.NoError(t, Append(dir, rec))
	}
}

// allConfirmed is a ground-truth lookup that reports every named persona as
// fully confirmed, so a test can isolate the ISOLATION half of the split from
// the confirmation half. The count clears minConfirmationOutcomes, since a row
// below that floor is deliberately read as "no ground truth".
func allConfirmed(personas ...string) GroundTruthLookup {
	return func(time.Duration, time.Time) (map[string]Confirmation, error) {
		out := map[string]Confirmation{}
		for _, p := range personas {
			out[strings.ToLower(p)] = Confirmation{Confirmed: minConfirmationOutcomes}
		}
		return out, nil
	}
}

func TestTrustPriorsWithGroundTruth_SoloConfirmedBeatsCorroborated(t *testing.T) {
	// AC 04-01 Scenarios 1-2, end to end. Both lenses raise exactly one finding
	// per run over the same number of runs. "lone" raises it alone (credit 1.0,
	// corroborated 0); "herd" always raises alongside one other lens (credit 0.5,
	// corroborated 1). Under the OLD binary rate the ranking is exactly inverted,
	// which is the failure this sprint exists to fix.
	dir := t.TempDir()
	weighted(t, dir, 20, "Lone", 1, 0, 1.0)
	weighted(t, dir, 20, "Herd", 1, 1, 0.5)

	rates, err := TrustPriorsWithGroundTruth(dir, 10, allConfirmed("lone", "herd"))
	require.NoError(t, err)

	assert.InDelta(t, 1.0, rates["lone"], 1e-9)
	assert.InDelta(t, 0.5, rates["herd"], 1e-9)
	assert.Greater(t, rates["lone"], rates["herd"],
		"an isolated finding that proved real must outrank one four others also raised")
}

func TestTrustPriorsWithGroundTruth_ConfirmationRateScalesTheCredit(t *testing.T) {
	// The read-time half. Identical scorecard evidence, different TD outcomes:
	// three of "shaky"'s findings resolved and one was closed wontfix, so only
	// three quarters of its isolation credit is earned.
	dir := t.TempDir()
	weighted(t, dir, 20, "Shaky", 1, 0, 1.0)

	gt := func(time.Duration, time.Time) (map[string]Confirmation, error) {
		return map[string]Confirmation{"shaky": {Confirmed: 30, Dismissed: 10}}, nil
	}
	rates, err := TrustPriorsWithGroundTruth(dir, 10, gt)
	require.NoError(t, err)
	assert.InDelta(t, 0.75, rates["shaky"], 1e-9)
}

func TestTrustPriorsWithGroundTruth_CountsEveryNonConfirmedOutcomeAgainstTheLens(t *testing.T) {
	// The epic's own reading of the TD lifecycle: resolved means the finding was
	// real; wontfix, unreproducible and attempts-exhausted each mean it probably
	// was not. All three sit in the denominator, which is why Story 01 had to add
	// the last two to the enum before this phase could use them.
	dir := t.TempDir()
	weighted(t, dir, 20, "Mixed", 1, 0, 1.0)

	gt := func(time.Duration, time.Time) (map[string]Confirmation, error) {
		return map[string]Confirmation{"mixed": {
			Confirmed: 5, Dismissed: 5, Unreproducible: 5, AttemptsExhausted: 5,
		}}, nil
	}
	rates, err := TrustPriorsWithGroundTruth(dir, 10, gt)
	require.NoError(t, err)
	assert.InDelta(t, 0.25, rates["mixed"], 1e-9)
}

func TestTrustPriorsWithGroundTruth_PreWeightingRecordsAreExcludedNotAveraged(t *testing.T) {
	// AC 04-01 Scenario 3, second clause. Ten pre-weighting runs carry no credit
	// era and no credit; twenty post-weighting runs each earned a full 1.0.
	// Blended, the rate would be 20/30; excluded, it is 1.0. The wrong answer is
	// the SILENT one, which is why the era marker exists.
	dir := t.TempDir()
	appendN(t, dir, 10, "Elder", "m1", 1, 1) // no CreditEra: pre-weighting
	weighted(t, dir, 20, "Elder", 1, 0, 1.0)

	rates, err := TrustPriorsWithGroundTruth(dir, 10, allConfirmed("elder"))
	require.NoError(t, err)
	assert.InDelta(t, 1.0, rates["elder"], 1e-9,
		"a pre-weighting record must be excluded from the weighted rate, not counted as a measured zero")
}

func TestTrustPriorsWithGroundTruth_AllPreWeightingHistoryDegradesToBinary(t *testing.T) {
	// The rollout case. A store written entirely before this phase has no
	// weighted denominator at all, so the weighted rate is undefined rather than
	// zero — and publishing zero would demote every lens in the panel on upgrade,
	// the exact blackout strictRuns and unresolvedEraRuns refuse to cause.
	dir := t.TempDir()
	appendN(t, dir, 20, "Elder", "m1", 4, 3)

	rates, err := TrustPriorsWithGroundTruth(dir, 10, allConfirmed("elder"))
	require.NoError(t, err)
	assert.InDelta(t, 3.0/4.0, rates["elder"], 1e-9,
		"with no measured credit the rate must fall back to the pre-existing binary one")
}

func TestTrustPriorsWithGroundTruth_MissingSignalDegradesToBinaryNeverInflates(t *testing.T) {
	// AC 04-01 Error Scenario 1, and its security clause: a missing or broken
	// ground-truth signal must degrade TOWARD the prior behaviour, never toward
	// an unearned boost. "Lone" has a binary rate of 0.0 and a weighted rate of
	// 1.0, so every degraded path here is asserted against the LOWER number.
	dir := t.TempDir()
	weighted(t, dir, 20, "Lone", 1, 0, 1.0)

	for name, gt := range map[string]GroundTruthLookup{
		"nil lookup": nil,
		"lookup error": func(time.Duration, time.Time) (map[string]Confirmation, error) {
			return nil, fmt.Errorf("debt store unreadable")
		},
		// A truncated store read can plausibly return BOTH a populated map and an
		// error. A partial ledger is not a smaller true answer — it is a ratio
		// over a subset nobody chose — so the map must be discarded with the
		// error rather than used.
		"lookup error carrying a partial map": func(time.Duration, time.Time) (map[string]Confirmation, error) {
			return map[string]Confirmation{"lone": {Confirmed: 40}}, fmt.Errorf("truncated month file")
		},
		"empty map": func(time.Duration, time.Time) (map[string]Confirmation, error) {
			return map[string]Confirmation{}, nil
		},
		"persona absent from the map": func(time.Duration, time.Time) (map[string]Confirmation, error) {
			return map[string]Confirmation{"someone-else": {Confirmed: 40}}, nil
		},
		"row with no counted outcomes": func(time.Duration, time.Time) (map[string]Confirmation, error) {
			return map[string]Confirmation{"lone": {}}, nil
		},
		// Below minConfirmationOutcomes. One resolved row is not a measurement,
		// and letting it through would hand this lens a factor of 1.0.
		"row below the outcome floor": func(time.Duration, time.Time) (map[string]Confirmation, error) {
			return map[string]Confirmation{"lone": {Confirmed: minConfirmationOutcomes - 1}}, nil
		},
		// The total is POSITIVE, so the total check cannot catch this one. Left
		// unguarded it yields factor -0.5 and a negative prior.
		"negative count inside a positive total": func(time.Duration, time.Time) (map[string]Confirmation, error) {
			return map[string]Confirmation{"lone": {Confirmed: -5, Dismissed: 30}}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			rates, err := TrustPriorsWithGroundTruth(dir, 10, gt)
			require.NoError(t, err)
			assert.InDelta(t, 0.0, rates["lone"], 1e-9,
				"a missing or malformed signal must never inflate a score")
		})
	}
}

func TestTrustPriorsWithGroundTruth_GeneralistDoesNotOutrankSpecialists(t *testing.T) {
	// AC 04-02, epic acceptance criterion 3. bruce is pure overlap: every finding
	// he raises is one a specialist also raised, and he raises nothing of his own.
	// Each specialist additionally holds one finding it raised alone.
	//
	// Under the binary rate bruce scores a perfect 1.00 and every specialist 0.50,
	// which is the backwards ranking the epic names. Weighted, bruce earns 0.5 per
	// finding and the specialists 1.5 across two.
	//
	// vera, pace, brad, archer and ronin have no in-repo persona file, so per C11
	// they are unmapped for opportunity scoping; that is irrelevant here because
	// these fixtures carry no CategoriesRaised and the union is therefore empty
	// for every run. The full roster is used anyway, per AC 04-02's requirement
	// that the fixture not be a toy 2-reviewer case.
	specialists := []string{"greta", "kai", "mira", "dax", "pace", "penny", "vera", "brad", "archer", "otto", "sasha"}
	dir := t.TempDir()
	for _, s := range specialists {
		for i := 0; i < 20; i++ {
			runID := runIDAt(time.Now(), fmt.Sprintf("panel-%s-%03d", s, i))
			// The specialist: one shared finding (0.5) plus one solo (1.0).
			spec := pairReviewer(runID, s, "m1", 2, 1, PairSignal{Peer: "bruce", Agreed: 1})
			spec.WeightedCredit = 1.5
			spec.CreditEra = CreditEraCurrent
			// bruce: the shared finding only.
			gen := pairReviewer(runID, "bruce", "m2", 1, 1, PairSignal{Peer: s, Agreed: 1})
			gen.WeightedCredit = 0.5
			gen.CreditEra = CreditEraCurrent
			require.NoError(t, Append(dir, spec))
			require.NoError(t, Append(dir, gen))
		}
	}
	// AC 04-02 Edge Case 1: ronin only ever co-raises with bruce and holds no solo
	// finding, so it is deliberately EXCLUDED from the "ranks above" assertion —
	// isolation credit alone must not lift a lens that earned none.
	for i := 0; i < 20; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("panel-ronin-%03d", i))
		ronin := pairReviewer(runID, "ronin", "m1", 1, 1, PairSignal{Peer: "bruce", Agreed: 1})
		ronin.WeightedCredit = 0.5
		ronin.CreditEra = CreditEraCurrent
		gen := pairReviewer(runID, "bruce", "m2", 1, 1, PairSignal{Peer: "ronin", Agreed: 1})
		gen.WeightedCredit = 0.5
		gen.CreditEra = CreditEraCurrent
		require.NoError(t, Append(dir, ronin))
		require.NoError(t, Append(dir, gen))
	}

	rates, err := TrustPriorsWithGroundTruth(dir, DefaultTrustMinRuns, allConfirmed(append(specialists, "bruce", "ronin")...))
	require.NoError(t, err)

	require.Contains(t, rates, "bruce")
	for _, s := range specialists {
		require.Contains(t, rates, s)
		assert.Greater(t, rates[s], rates["bruce"],
			"specialist %s must outrank the generalist, which earns its volume purely by overlapping", s)
	}
	assert.InDelta(t, rates["bruce"], rates["ronin"], 1e-9,
		"a specialist with no solo findings earns no isolation credit and must not be lifted above the generalist")
}

func TestPairDisagreements_ExposeTheRatesBehindTheWeightedRanking(t *testing.T) {
	// AC 04-05. Story 4 CONSUMES Story 5's surface rather than rebuilding it
	// (D2), so the per-pair rate behind the ranking above has to be independently
	// queryable from the same store — not buried inside one opaque score.
	dir := t.TempDir()
	coEligible(t, dir, minPairCases, "bruce", "dax", 4, 0)

	pairs, err := PairDisagreements(dir)
	require.NoError(t, err)

	key, ok := PairKey("bruce", "dax")
	require.True(t, ok)
	tally, ok := pairs[key]
	require.True(t, ok, "a pair that co-occurred must be queryable")
	assert.InDelta(t, 0.0, tally.DisagreementRate(), 1e-9)
	assert.True(t, tally.Sufficient, "minPairCases co-eligible runs clears the floor")
	assert.True(t, tally.DropCandidate, "a pair that never splits is the penny test's drop candidate")

	// Edge Case 1: a pair that never co-occurred is ABSENT, never a spurious rate.
	missing, ok := PairKey("bruce", "sasha")
	require.True(t, ok)
	assert.NotContains(t, pairs, missing)
}

func TestTrustPriorsWithGroundTruth_ZeroRaisedReviewerStaysRateZero(t *testing.T) {
	// AC 04-01 Edge Case 3: the existing zero-denominator convention is untouched
	// by the weighting — no divide-by-zero, no panic, no NaN reaching the map
	// reconcile reads.
	dir := t.TempDir()
	weighted(t, dir, 20, "Quiet", 0, 0, 0.0)

	rates, err := TrustPriorsWithGroundTruth(dir, 10, allConfirmed("quiet"))
	require.NoError(t, err)
	require.Contains(t, rates, "quiet")
	assert.False(t, math.IsNaN(rates["quiet"]), "a zero denominator must not produce NaN")
	assert.InDelta(t, 0.0, rates["quiet"], 1e-9)
}

func TestTrustPriors_ContractIsUnchangedByTheWeighting(t *testing.T) {
	// AC 04-04 Scenarios 1 and 3. TrustPriors itself supplies no ground truth, so
	// its numbers and its map[string]float64 shape are exactly what they were —
	// reconcile/consensus.go's trustExempt and demoteByTrust need no call-site
	// change.
	dir := t.TempDir()
	weighted(t, dir, 20, "Lone", 1, 0, 1.0)

	var want map[string]float64
	got, err := TrustPriors(dir, 10)
	require.NoError(t, err)
	want = got // compile-time proof the return type is still map[string]float64
	require.Contains(t, want, "lone")
	assert.InDelta(t, 0.0, want["lone"], 1e-9,
		"without an injected lookup TrustPriors must return the pre-existing binary rate")
}

func TestWeightedCreditByPersona_ExcludesUnmarkedRecordsAndNeverMutatesItsInput(t *testing.T) {
	// AC 04-04 Scenario 2: the new stage follows the existing chain links'
	// convention — pure, non-mutating, independently testable.
	records := []Record{
		func() Record {
			r := reviewer_("run-a", "Dax", "m1", 2, 0)
			r.WeightedCredit = 2.0
			r.CreditEra = CreditEraCurrent
			return r
		}(),
		reviewer_("run-b", "Dax", "m1", 5, 4), // pre-weighting: no era, no credit
		func() Record {
			r := reviewer_("run-c", "Dax", "m1", 1, 0)
			r.WeightedCredit = 1.0
			r.CreditEra = CreditEraCurrent + 1 // above current: measured under a rule this binary does not implement
			return r
		}(),
	}
	before := append([]Record{}, records...)

	got := weightedCreditByPersona(records)
	require.Contains(t, got, "dax")
	assert.InDelta(t, 2.0, got["dax"].credit, 1e-9)
	assert.Equal(t, 2, got["dax"].raised,
		"only the era-marked record contributes to the weighted denominator")
	assert.Equal(t, before, records, "the stage must not mutate its input slice")
}

func TestMergeRoutedEras_PreservesTheWeightedCreditFields(t *testing.T) {
	// mergeRoutedEras rewrites a record to normalise its denominator era. The
	// weighting fields are on the same struct and must ride through untouched, or
	// the fold above would read a rewritten record as pre-weighting.
	r := reviewer_("run-a", "Dax", "m1", 3, 2)
	r.RaisedDenominator = raisedDenominatorRoutedExShield
	r.FindingsDocShielded = 1
	r.WeightedCredit = 1.25
	r.CreditEra = CreditEraCurrent

	out := mergeRoutedEras([]Record{r})
	require.Len(t, out, 1)
	assert.InDelta(t, 1.25, out[0].WeightedCredit, 1e-9)
	assert.Equal(t, CreditEraCurrent, out[0].CreditEra)
}

func TestIsolatedFindingWeight_NotNarrowedWithoutRemeasurement(t *testing.T) {
	// AC 04-03 Error Scenario 1, mirroring
	// TestDefaultTrustWindow_NotNarrowedWithoutRemeasurement and
	// TestMinPairCases_NotNarrowedWithoutRemeasurement.
	//
	// The literal is deliberate: derived from the constant, this test would
	// contract with it and pass at any value.
	assert.InDelta(t, 1.0, isolatedFindingWeight, 1e-9,
		"isolatedFindingWeight is PROVISIONAL and its value is not evidence-backed. "+
			"Moving it requires a fresh measurement against the localdebt ground-truth "+
			"ledger, recorded in the constant's doc comment the way DefaultTrustMinRuns' "+
			"and defaultTrustWindow's are. Update this literal in the same commit.")
}

// ---------------------------------------------------------------------------
// 4.6 — guards the 4.5.A adversarial pass found unpinned
// ---------------------------------------------------------------------------

func TestTrustPriorsWithGroundTruth_WeightedFloorCountsOnlyEraMarkedRuns(t *testing.T) {
	// The floor has to be applied to the sample the NUMBER came from. Nineteen
	// pre-weighting runs plus one measured run clears a twenty-run floor on the
	// caller's own tally while the weighted rate is computed from that single
	// run — and one perfect run scores 1.0, which clears reconcile's exemption
	// threshold outright. This is the ordinary state of the store on first
	// upgrade, not a contrived one.
	dir := t.TempDir()
	appendN(t, dir, 19, "Fresh", "m1", 4, 0) // pre-weighting: binary rate 0.0
	weighted(t, dir, 1, "Fresh", 1, 0, 1.0)  // the single measured run

	rates, err := TrustPriorsWithGroundTruth(dir, 20, allConfirmed("fresh"))
	require.NoError(t, err)
	require.Contains(t, rates, "fresh")
	assert.InDelta(t, 0.0, rates["fresh"], 1e-9,
		"one era-marked run must not satisfy a twenty-run floor and buy a maximal prior")
}

func TestTrustPriorsWithGroundTruth_IneligibleRunsDoNotReEnterThroughTheNumerator(t *testing.T) {
	// The weighted fold reads the FILTERED slice. Read off the raw records
	// instead, a single exploratory `--consensus off` reconcile would durably
	// move the prior every later strict run consults — the anti-gaming boundary
	// strictRuns exists to hold.
	dir := t.TempDir()
	weighted(t, dir, 20, "Gamed", 1, 0, 0.0) // measured, earned nothing

	// The credit stays INSIDE the per-finding bound weightedCreditByPersona
	// enforces. An out-of-bound value would be dropped by that guard instead, and
	// the test would pass while proving nothing about the filter chain.
	lenient := reviewer_(runIDAt(time.Now(), "lenient-run"), "Gamed", "m1", 20, 0)
	lenient.ConsensusLevel = "off"
	lenient.WeightedCredit = 20.0
	lenient.CreditEra = CreditEraCurrent
	require.NoError(t, Append(dir, lenient))

	rates, err := TrustPriorsWithGroundTruth(dir, 10, allConfirmed("gamed"))
	require.NoError(t, err)
	assert.InDelta(t, 0.0, rates["gamed"], 1e-9,
		"a non-strict run must not contribute to the weighted numerator")
}

func TestWeightedCreditByPersona_DropsCreditThatNoEmitterCouldHaveWritten(t *testing.T) {
	// The store is plain user-writable JSONL. Credit above raised x
	// isolatedFindingWeight, or below zero, was not written by this emitter — and
	// left in, a hand-edited weighted_credit of 50 becomes a prior of 50.0, which
	// clears every threshold reconcile has.
	withCredit := func(runID string, raised int, credit float64, era int) Record {
		r := reviewer_(runID, "Dax", "m1", raised, 0)
		r.WeightedCredit = credit
		r.CreditEra = era
		return r
	}
	// The CEILING moved to scrubForgedCredit, which runs earlier in the chain
	// where FindingsDocShielded is still separate — see
	// TestScrubForgedCredit_UnMeasuresRatherThanDropsAndKeepsHonestRecords and
	// TestTrustPriors_ForgedCreditCannotReachThePriorThroughTheEraMerge. What
	// stays here are the two checks that need no pre-merge knowledge.
	for name, tc := range map[string]Record{
		"negative credit":     withCredit("run-neg", 4, -5.0, CreditEraCurrent),
		"negative era marker": withCredit("run-era", 4, 4.0, -7),
	} {
		t.Run(name, func(t *testing.T) {
			assert.NotContains(t, weightedCreditByPersona([]Record{tc}), "dax",
				"a record this emitter could not have written must not reach the tally")
		})
	}

	// The bound is inclusive, so an all-solo record at exactly the maximum stays.
	kept := weightedCreditByPersona([]Record{withCredit("run-max", 4, 4.0, CreditEraCurrent)})
	require.Contains(t, kept, "dax")
	assert.InDelta(t, 4.0, kept["dax"].credit, 1e-9)
}

func TestWeightedCreditByPersona_IgnoresNonReviewerRecords(t *testing.T) {
	// An aggregate row carries no Reviewer, so today it would only pollute the
	// "" key — benign by accident of another file rather than by anything this
	// function asserts.
	agg := Record{
		SchemaVersion:  SchemaVersion,
		RecordType:     RecordTypeAggregate,
		Reviewer:       "Dax",
		FindingsRaised: 4,
		WeightedCredit: 4.0,
		CreditEra:      CreditEraCurrent,
	}
	assert.Empty(t, weightedCreditByPersona([]Record{agg}),
		"only reviewer records carry a per-lens weighted credit")
}

func TestConfirmationFactor_RejectsANegativeCountInsideAPositiveTotal(t *testing.T) {
	// The total check cannot catch this: -5 + 30 is 30. Unguarded it yields
	// factor -0.1667 and a NEGATIVE prior in the map reconcile reads.
	_, ok := confirmationFactor(Confirmation{Confirmed: -5, Dismissed: 30})
	assert.False(t, ok, "a negative count must read as no ground truth, never as a negative factor")
}

func TestMinConfirmationOutcomes_NotNarrowedWithoutRemeasurement(t *testing.T) {
	// The literal is deliberate, matching TestMinPairCases_NotNarrowedWithoutRemeasurement.
	assert.Equal(t, 20, minConfirmationOutcomes,
		"minConfirmationOutcomes is PROVISIONAL and borrowed from DefaultTrustMinRuns. "+
			"Moving it requires a fresh measurement against the localdebt ground-truth "+
			"ledger, recorded in the constant's doc comment. Update this literal in the same commit.")
}

func TestGroundTruthLookup_ReceivesTheSameWindowTheRecordReadUsed(t *testing.T) {
	// ResolveTrustPriors bounds the scorecard read to defaultTrustWindow. An
	// un-windowed lookup would scale that 180-day isolation credit by an all-time
	// confirmation ratio — two different populations multiplied as though they
	// were the same one.
	dir := t.TempDir()
	weighted(t, dir, 20, "Lone", 1, 0, 1.0)

	var gotSince time.Duration
	var gotNow time.Time
	gt := func(since time.Duration, now time.Time) (map[string]Confirmation, error) {
		gotSince, gotNow = since, now
		return map[string]Confirmation{"lone": {Confirmed: 40}}, nil
	}
	now := time.Now()
	_, err := trustPriorsSince(dir, 10, defaultTrustWindow, now, gt)
	require.NoError(t, err)

	assert.Equal(t, defaultTrustWindow, gotSince)
	assert.Equal(t, now, gotNow)
}

func TestKeptForTrust_IsTheSameChainBothSurfacesRun(t *testing.T) {
	// The chain used to be spelled out verbatim in trust.go and pairtally.go.
	// A sixth link added to one would silently not reach the other, re-admitting
	// through the pair surface every run the trust gates had just excluded.
	records := []Record{
		reviewer_("run-a", "Dax", "m1", 2, 1),
		func() Record { r := reviewer_("run-b", "Dax", "m1", 2, 1); r.ConsensusLevel = "off"; return r }(),
		func() Record { r := reviewer_("run-c", "Greta", "m1", 2, 1); r.Outcome = "truncated"; return r }(),
	}
	before := append([]Record{}, records...)

	kept := keptForTrust(records)
	require.Len(t, kept, 1, "the lenient run and the truncated run must both be gone")
	assert.Equal(t, "run-a", kept[0].RunID)
	assert.Equal(t, before, records, "the chain must not mutate its input")
}

func TestTrustPriorsWithGroundTruth_WeightedFloorHoldsEvenWithNoCallerFloor(t *testing.T) {
	// cli/personas.go calls TrustPriors(dir, 0) deliberately, to render every
	// persona it has any history for. On that path a `minRuns > 0 &&` guard is
	// switched off entirely, and one era-marked run would publish a full
	// weighted rate.
	dir := t.TempDir()
	weighted(t, dir, 1, "Solo", 1, 0, 1.0)

	rates, err := TrustPriorsWithGroundTruth(dir, 0, allConfirmed("solo"))
	require.NoError(t, err)
	require.Contains(t, rates, "solo")
	assert.InDelta(t, 0.0, rates["solo"], 1e-9,
		"one measured run must not publish a weighted rate even when the caller asked for no floor")
}

func TestTrustPriors_ForgedCreditCannotReachThePriorThroughTheEraMerge(t *testing.T) {
	// THIS TEST RUNS THE WHOLE CHAIN ON PURPOSE. An earlier version called
	// weightedCreditByPersona directly with a hand-built record, which skipped
	// mergeRoutedEras — and the merge is precisely what defeated the bound it
	// was asserting. It passed while the gap was open end to end.
	//
	// The forgery: 2 raised findings plus 3 doc-shielded ones (pre-merge those
	// are separate, so the honest emit-time ceiling is 2.0), with a hand-edited
	// credit of 5.0. After the merge the record reads raised=5, shielded=0, so a
	// bound taken THERE is 5.0 and the forgery passes — publishing a maximal
	// prior of 1.0 for a lens whose binary rate is 0.0. Asked before the merge,
	// the same bound is 2.0 and the forgery is scrubbed.
	dir := t.TempDir()
	for i := 0; i < DefaultTrustMinRuns; i++ {
		r := reviewer_(runIDAt(time.Now(), fmt.Sprintf("forged-%03d", i)), "Forger", "m1", 2, 0)
		r.RaisedDenominator = raisedDenominatorRoutedExShield
		r.FindingsDocShielded = 3
		r.WeightedCredit = 5.0
		r.CreditEra = CreditEraCurrent
		require.NoError(t, Append(dir, r))
	}

	rates, err := TrustPriorsWithGroundTruth(dir, DefaultTrustMinRuns, allConfirmed("forger"))
	require.NoError(t, err)
	require.Contains(t, rates, "forger")
	assert.InDelta(t, 0.0, rates["forger"], 1e-9,
		"forged credit must be un-measured, leaving the pre-existing binary rate")
}

func TestScrubForgedCredit_UnMeasuresRatherThanDropsAndKeepsHonestRecords(t *testing.T) {
	// The scrub must not become a second attack surface: a forged float may not
	// delete a real run from the trust denominator, only from the weighted one.
	forged := reviewer_("run-a", "Dax", "m1", 2, 1)
	forged.FindingsDocShielded = 3
	forged.WeightedCredit = 5.0
	forged.CreditEra = CreditEraCurrent

	// Exactly at the honest ceiling, WITH a large shielded count beside it.
	// Pre-merge a doc-shielded finding is counted INSTEAD of being counted in
	// FindingsRaised, so all 4 raised could have earned credit and this record
	// must survive. An earlier version of the bound subtracted the shielded
	// count here and would have scrubbed it.
	honest := reviewer_("run-b", "Dax", "m1", 4, 0)
	honest.FindingsDocShielded = 3
	honest.WeightedCredit = 4.0
	honest.CreditEra = CreditEraCurrent

	negative := reviewer_("run-c", "Dax", "m1", 4, 0)
	negative.WeightedCredit = -1.0
	negative.CreditEra = CreditEraCurrent

	in := []Record{forged, honest, negative}
	before := append([]Record{}, in...)
	out := scrubForgedCredit(in)

	require.Len(t, out, 3, "a forged record is un-measured, never dropped")
	assert.Equal(t, 2, out[0].FindingsRaised, "the binary counters must be untouched")
	assert.Equal(t, 1, out[0].FindingsCorroborated)
	assert.Zero(t, out[0].CreditEra, "the forged record reads as pre-weighting")
	assert.Zero(t, out[0].WeightedCredit)

	assert.Equal(t, CreditEraCurrent, out[1].CreditEra,
		"a record exactly at the ceiling, with shielded findings beside it, stays measured")
	assert.InDelta(t, 4.0, out[1].WeightedCredit, 1e-9)

	assert.Zero(t, out[2].CreditEra, "negative credit is impossible at any era")
	assert.Equal(t, before, in, "the link must not mutate its input")
}

func TestResolveTrustPriorsWithGroundTruth_KeepsResolveTrustPriorsBehaviourOnNil(t *testing.T) {
	// The seam Phase 5 needs is additive: passing nil must be byte-identical to
	// the no-argument entry point every production caller already uses.
	//
	// THE STORE IS SEEDED. An earlier version pointed HOME at an empty temp dir,
	// so both sides read nothing and the test compared two empty maps — it could
	// not see a divergent floor or window and a mutation that changed the floor
	// survived it.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AppData", home)
	dir, err := DefaultDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	appendN(t, dir, DefaultTrustMinRuns+5, "Sasha", "m1", 4, 3)
	// BELOW the floor, so a divergent floor changes the KEY SET and not merely a
	// rate. Without this second persona a mutation swapping DefaultTrustMinRuns
	// for 1 survives, since both floors admit Sasha at the same rate.
	appendN(t, dir, 2, "Quiet", "m1", 4, 1)

	got := ResolveTrustPriorsWithGroundTruth(nil)
	require.Contains(t, got, "sasha", "the fixture must actually reach the store")
	assert.InDelta(t, 3.0/4.0, got["sasha"], 1e-9)
	assert.NotContains(t, got, "quiet", "the DefaultTrustMinRuns floor must still apply")
	assert.Equal(t, ResolveTrustPriors(), got)
}

func TestTrustPriors_RoutedPhantomsCannotBeLaunderedIntoWeightedCredit(t *testing.T) {
	// THE SECOND DOOR. FindingsRaised counts chargeable Tier-4-routed phantoms,
	// and credit is emitted only from the real findings — so a bound of
	// FindingsRaised alone is loose by exactly the routed count, and that gap is
	// WIDEST for the reviewer carrying the most fabrication evidence. One real
	// finding plus three phantoms bounds at 4.0 against an honest ceiling of 1.0.
	dir := t.TempDir()
	for i := 0; i < DefaultTrustMinRuns; i++ {
		r := reviewer_(runIDAt(time.Now(), fmt.Sprintf("laundered-%03d", i)), "Ghost", "m1", 4, 0)
		r.FindingsRouted = 3 // of the 4 raised, 3 were phantoms
		r.WeightedCredit = 4.0
		r.CreditEra = CreditEraCurrent
		require.NoError(t, Append(dir, r))
	}

	rates, err := TrustPriorsWithGroundTruth(dir, DefaultTrustMinRuns, allConfirmed("ghost"))
	require.NoError(t, err)
	require.Contains(t, rates, "ghost")
	assert.InDelta(t, 0.0, rates["ghost"], 1e-9,
		"credit above the non-routed ceiling must be un-measured, leaving the binary rate")
}

func TestEmit_StampsTheRoutedCountBesideTheCredit(t *testing.T) {
	// The read-time ceiling is computed from FindingsRouted, so a record that
	// carries the era must carry the count too or it is bounded too loosely.
	dir := t.TempDir()
	in := EmitInput{
		RunID:     pairRunID("r-routed-count"),
		Reviewers: map[string]ReviewerMeta{"dax": {Model: "m1", Outcome: outcomeFindings}},
		Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "real", Reviewers: []string{"dax"}},
		},
		UnresolvedFindings: []Finding{
			{File: "g1.go", Line: 1, Problem: "phantom", Reviewers: []string{"dax"}},
			{File: "g2.go", Line: 2, Problem: "phantom", Reviewers: []string{"dax"}},
		},
	}
	require.NoError(t, Emit(in, EmitOpts{Dir: dir}))

	got := reviewerRecordsByName(t, dir)["dax"]
	assert.Equal(t, 3, got.FindingsRaised)
	assert.Equal(t, 2, got.FindingsRouted, "the routed count must be persisted, not recomputed")
	assert.InDelta(t, 1.0, got.WeightedCredit, 1e-9)

	// The emitted record must sit exactly AT its own ceiling, not above it —
	// this is the invariant scrubForgedCredit relies on.
	bound := float64(got.FindingsRaised-got.FindingsRouted) * maxPerFindingCredit(isolatedFindingWeight)
	assert.LessOrEqual(t, got.WeightedCredit, bound)
}

func TestMaxPerFindingCredit_CoversTheCorroboratedBranchToo(t *testing.T) {
	// reviewerCounts scales ONLY the solo branch by isolatedFindingWeight; a
	// corroborated finding contributes 1/distinctCount, up to 0.5 for a pair. A
	// ceiling derived from the constant alone is therefore too tight the moment
	// the constant is re-measured below 0.5 — and it would scrub honest
	// corroborated-heavy records silently.
	// The floor is exercised at hypothetical weights, because at the constant's
	// current 1.0 the max() is indistinguishable from returning the weight — a
	// guard no mutation could kill, and therefore no guard at all.
	for name, tc := range map[string]struct{ weight, want float64 }{
		"weight above the floor": {1.0, 1.0},
		"weight at the floor":    {0.5, 0.5},
		"weight below the floor": {0.4, 0.5},
		"weight far below":       {0.05, 0.5},
		"weight well above":      {2.0, 2.0},
	} {
		t.Run(name, func(t *testing.T) {
			assert.InDelta(t, tc.want, maxPerFindingCredit(tc.weight), 1e-9)
		})
	}
	assert.GreaterOrEqual(t, maxPerFindingCredit(isolatedFindingWeight), isolatedFindingWeight,
		"the solo branch is governed by the constant")

	// Two findings, each corroborated by exactly one peer: credit 1.0 over 2
	// raised, which is 0.5 per finding — the corroborated branch's maximum.
	_, corroborated, credit := reviewerCounts("dax", []Finding{
		{Reviewers: []string{"dax", "bruce"}},
		{Reviewers: []string{"dax", "greta"}},
	})
	require.Equal(t, 2, corroborated)
	assert.InDelta(t, 1.0, credit, 1e-9)
	assert.LessOrEqual(t, credit, 2*maxPerFindingCredit(isolatedFindingWeight),
		"an honest record must never exceed its own ceiling")
}

// --- Phase 5 (Story 06) regression guards and TD-032 pin ---

func TestTrustPriors_RecordSurvivesAModelRepoint(t *testing.T) {
	// AC 06-02 and epic acceptance criterion 5. The registry churn this guards
	// against is real and recurring — qwen3.6-plus -> qwen3.7-plus,
	// nemotron-3-ultra-550b pulled, mellum2 deprecated — and a persona repointed
	// at a new model must keep the standing it earned under the old one.
	//
	// It is pinned against the ACTUAL trustPriorsSince loop and its
	// normalizeReviewerName(row.Reviewer) key, not a hand-built equivalent: the
	// property depends on Aggregate grouping by (Reviewer, Model) and the loop
	// summing those rows under one persona key, so a mock of the loop would
	// assert the mock rather than the behaviour.
	dir := t.TempDir()
	// Ten runs under the retired model, ten under its replacement. Neither half
	// clears the floor of twenty alone; the persona does only if they sum.
	// ASYMMETRIC halves (AC 06-02 DoD): an averaging implementation computes
	// (0.75 + 10/60)/2 = 0.4583 here, while the summing implementation this
	// surface specifies computes 40/100 = 0.40 — the assertion can tell them
	// apart. A symmetric (4,3)/(4,3) split could not: both give 0.75.
	appendN(t, dir, 10, "Greta", "qwen3.6-plus", 4, 3)
	appendN(t, dir, 10, "Greta", "qwen3.7-plus", 6, 1)

	rates, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Contains(t, rates, "greta",
		"a persona's twenty runs must clear the floor as one record, not as two ten-run model records")
	assert.InDelta(t, 0.40, rates["greta"], 1e-9,
		"the repoint must SUM the two models' evidence: 40 corroborated of 100 raised, whichever model ran them")

	// The same persona under a THIRD, never-before-seen model still reads as one
	// record rather than resetting to a cold start.
	appendN(t, dir, 5, "Greta", "kimi-k3", 4, 3)
	rates, err = TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	// 55 corroborated of 120 raised (40 + 15 over 100 + 20).
	assert.InDelta(t, 55.0/120.0, rates["greta"], 1e-9)
}

func TestTrustPriors_CasingOfAModelRepointNeverSplitsAPersona(t *testing.T) {
	// The half of AC 06-02 that a same-casing fixture cannot catch. The store
	// carries the panel's original casing (registry.yaml agent names are free
	// text), so a persona re-registered as "GRETA" must land on the same key.
	// This is the mutation normalizeReviewerName exists to survive.
	dir := t.TempDir()
	// ASYMMETRIC halves, same reason as the model-repoint test above: an
	// averaging implementation reads (0.75 + 10/60)/2, summing reads 0.40.
	appendN(t, dir, 10, "Greta", "m1", 4, 3)
	appendN(t, dir, 10, "GRETA", "m2", 6, 1)

	rates, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.Len(t, rates, 1, "one persona, one key — casing must not fork the record")
	require.Contains(t, rates, "greta")
	assert.InDelta(t, 0.40, rates["greta"], 1e-9)
}

func TestTrustPriors_NewLensIsOmittedNotStarvedAtZero(t *testing.T) {
	// AC 06-03 and epic acceptance criterion 6. The cold-start death spiral this
	// prevents: a lens with no record must not be down-weighted into never being
	// dispatched, or it can never earn one.
	//
	// The mechanism is ABSENCE, not a floor value. reconcile/consensus.go does a
	// plain map lookup in both consumers (trustExempt returns false on !ok,
	// demoteByTrust returns m unchanged on !ok), so an absent lens reverts to the
	// neutral baseline. A present-at-zero lens would instead sit at or below
	// trustLowThreshold and be demoted to ConfLow on every singleton — the exact
	// starvation the criterion forbids. The reconcile half of this is pinned by
	// reconcile/trust_test.go's TestTrustPriors_AbsentReviewerNoOp.
	dir := t.TempDir()
	appendN(t, dir, 25, "Bruce", "m1", 4, 4)
	appendN(t, dir, 3, "Newlens", "m1", 2, 0)

	rates, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)

	rate, present := rates["newlens"]
	assert.False(t, present,
		"a below-floor lens must be ABSENT, never present at a punitive zero")
	assert.Zero(t, rate, "the comma-ok zero value is what an absent lookup yields in reconcile")
	require.Contains(t, rates, "bruce", "a lens that cleared the floor is unaffected")

	// The map handed to reconcile is exactly this one, so the absence travels.
	var opts reclib.Options
	opts.TrustPriors = rates
	_, exempt := opts.TrustPriors["newlens"]
	assert.False(t, exempt)
}

func TestTrustPriors_JunkLabelledRaiserIsStillChargedForItsFindings(t *testing.T) {
	// TD-032's pinning test, on the RATE rather than on the explainability
	// surface — the escape it closes is a scoring escape, so the proof has to be
	// a number demoteByTrust would read.
	//
	// The lens raises three findings per junk run and none of them is
	// corroborated. Before the fix its record was deleted from the tally (its
	// unreadable labels put it out of every remit), so those uncorroborated
	// findings never reached the denominator and bad labelling was
	// self-exculpating. After the fix the rate must FALL.
	dir := t.TempDir()
	clean := t.TempDir()

	seed := func(d string, withJunk bool) {
		for i := 0; i < 20; i++ {
			rec := reviewer_(runIDAt(time.Now(), fmt.Sprintf("good-%03d", i)), "Dax", "m1", 1, 1)
			rec.CategoriesRaised = []string{reclib.CategoryTesting}
			require.NoError(t, Append(d, rec))
		}
		if !withJunk {
			return
		}
		for i := 0; i < 10; i++ {
			runID := runIDAt(time.Now(), fmt.Sprintf("junk-%03d", i))
			// CategoriesRaised empty, matching what reviewerCategories writes for
			// a reviewer whose every CATEGORY was outside reclib.Categories().
			junk := reviewer_(runID, "Dax", "m1", 3, 0)
			require.NoError(t, Append(d, junk))
			other := reviewer_(runID, "Pace", "m1", 1, 0)
			other.CategoriesRaised = []string{reclib.CategoryPerformance}
			require.NoError(t, Append(d, other))
		}
	}
	seed(dir, true)
	seed(clean, false)

	withJunk, err := TrustPriors(dir, 10)
	require.NoError(t, err)
	without, err := TrustPriors(clean, 10)
	require.NoError(t, err)

	assert.InDelta(t, 1.0, without["dax"], 1e-9, "the control: twenty corroborated of twenty raised")
	assert.Less(t, withJunk["dax"], without["dax"],
		"thirty uncorroborated findings under unreadable labels must lower the rate, not vanish from it")
	// 20 corroborated of (20 + 30) raised.
	assert.InDelta(t, 20.0/50.0, withJunk["dax"], 1e-9)
}

// TestOpportunitySetRuns_UnscopeableRaiserIsKeptWhileSilentLensIsDropped is
// TD-032's fix and its boundary in one assertion pair, at the filter level.
//
// Both lenses contribute nothing to the run's union and both are out of remit
// for the only topic raised. They must be treated DIFFERENTLY, and the
// difference is the only thing separating a closed escape from a broken
// guarantee: the one that RAISED findings is charged (else bad labelling
// exculpates a phantom-raiser), the one that stayed SILENT is dropped (else a
// specialist is penalised for correct silence, breaking epic AC 1).
func TestOpportunitySetRuns_UnscopeableRaiserIsKeptWhileSilentLensIsDropped(t *testing.T) {
	in := []Record{
		oppRec("run-1", "penny", []string{"performance"}), // the only topic in play
		oppRec("run-1", "sasha", nil),                     // silent, out of remit
		oppRecUnlabelled("run-1", "dax", 3),               // raised 3, none attributable
	}
	out := opportunityFilter(in)

	names := map[string]bool{}
	for _, r := range out {
		names[r.Reviewer] = true
	}
	assert.True(t, names["penny"], "performance was in play")
	assert.False(t, names["sasha"],
		"a lens that raised NOTHING on an out-of-remit run is correctly silent and leaves the denominator")
	assert.True(t, names["dax"],
		"a lens that RAISED findings the scorer could not attribute must stay chargeable (TD-032)")
}

func TestOpportunityDisposition_IsTheOnePredicateBothSurfacesRead(t *testing.T) {
	// The extraction's whole justification. opportunitySetRuns and
	// ExplainTrustPriors must not each carry their own copy of this rule, or the
	// explanation names a reason the chain never acted on — a divergence no test
	// of either surface alone can see.
	union := map[string]struct{}{"performance": {}}

	assert.Equal(t, dispCounted, opportunityDisposition(oppRec("r", "penny", []string{"performance"}), union))
	assert.Equal(t, dispOutOfRemit, opportunityDisposition(oppRec("r", "sasha", nil), union))
	assert.Equal(t, dispUnscopeable, opportunityDisposition(oppRecUnlabelled("r", "dax", 2), union))
	assert.Equal(t, dispCounted, opportunityDisposition(oppRec("r", "vera", nil), union),
		"an unmapped registry-only lens is never opportunity-scoped (C11)")
	assert.Equal(t, dispCounted, opportunityDisposition(oppRec("r", "sasha", nil), nil),
		"an empty union means nobody had scopeable evidence — refuse to guess")
}

// TestOpportunityDisposition_EmptyUnionRaiserIsKeptAndAnnotated is the Q11
// re-scope (2026-09-22): the empty-union branch refused to guess for the TALLY
// but also stayed SILENT for the EXPLANATION, so a mapped lens that raised
// findings on a run nobody could scope passed through un-annotated — the
// null case ("nobody raised anything" vs "every category fell outside the
// vocabulary" vs "all non-discriminating") stayed unmeasurable from the
// surface. The re-scope keeps the tally identical (dispUnscopeable is kept by
// opportunitySetRuns, so the prior is unchanged) and moves only the
// annotation: raised>0 on an empty union is exactly dispUnscopeable's meaning
// ("raised findings, contributed nothing to the union"), so the branch returns
// it for a mapped lens. Genuine silence (zero-raised, the pin above) and the
// unmapped lenses (invariant preserved) still answer dispCounted.
func TestOpportunityDisposition_EmptyUnionRaiserIsKeptAndAnnotated(t *testing.T) {
	assert.Equal(t, dispUnscopeable, opportunityDisposition(oppRecUnlabelled("r", "dax", 2), nil),
		"a mapped lens that RAISED findings on an unscopeable run is kept AND annotated — the null case must be measurable")
	assert.Equal(t, dispCounted, opportunityDisposition(oppRecUnlabelled("r", "vera", 2), nil),
		"the unmapped invariant survives the re-scope: vera is never opportunity-scoped, never annotated as unlabelled")
}

// TestOpportunitySetRuns_ARaiserIsNeverDroppedForBeingOutOfItsOwnRemit REVERSES
// a property this file previously pinned as intended, and the reversal is
// recorded here rather than slipped in.
//
// The old test — TestOpportunitySetRuns_DropsAnOutOfRemitLensThatDidRaiseFindings,
// added in task 5.3 to close a coverage hole the 5.2.A review found by mutation —
// asserted the opposite: that a mapped lens raising a discriminating
// out-of-its-own-remit category leaves the denominator. That was the shipped
// behaviour and it was wrong, proved by probe at the 5.5 phase gate. dax's remit
// is testing and error-handling. Against a 20-run honest baseline scoring 1.00,
// 20 further runs of five uncorroborated findings each labelled `security` left
// dax at 1.00 — a hundred phantoms charged nothing — while the same phantoms
// under an unrecognised word dropped dax to 0.17. Labelling a phantom CORRECTLY
// was more exculpating than labelling it as gibberish.
//
// The mutation coverage the old test bought is NOT lost; it moves. The mutant it
// killed (`if r.FindingsRaised > 0 { return dispCounted }` at the top of
// opportunityDisposition) is now close to the intended behaviour, so the guard
// that matters is the opposite one: that a SILENT lens is still dropped. That is
// asserted below and in
// TestOpportunitySetRuns_UnscopeableRaiserIsKeptWhileSilentLensIsDropped.
func TestOpportunitySetRuns_ARaiserIsNeverDroppedForBeingOutOfItsOwnRemit(t *testing.T) {
	in := []Record{
		oppRec("run-1", "penny", []string{"performance"}),
		// sasha's remit is security. performance is a real, discriminating
		// vocabulary word outside it — the exact shape that used to buy a free
		// pass for every finding on the record.
		oppRec("run-1", "sasha", []string{"performance"}),
		// And the silent lens on the same run, which must STILL be dropped:
		// that is the guarantee epic acceptance criterion 1 actually makes, and
		// the boundary this change must not cross.
		oppRec("run-1", "dax", nil),
	}
	out := opportunityFilter(in)

	names := map[string]bool{}
	for _, r := range out {
		names[r.Reviewer] = true
	}
	assert.True(t, names["penny"], "performance is penny's remit")
	assert.True(t, names["sasha"],
		"a lens that RAISED findings is not silent; it stays accountable for them even out of remit")
	assert.False(t, names["dax"],
		"a lens that raised NOTHING on an out-of-remit run is correctly silent and still leaves the denominator")

	assert.Equal(t, dispCounted,
		opportunityDisposition(oppRec("run-1", "sasha", []string{"performance"}),
			map[string]struct{}{"performance": {}}),
		"an out-of-remit RAISER is counted, not annotated: no exclusion happened and no reason label applies")
}

func TestTrustPriors_OutOfRemitPhantomsAreChargedNotForgiven(t *testing.T) {
	// The phase-gate probe, turned into a permanent guard, on the RATE rather
	// than the filter — the escape it closes ended at reconcile's trustExempt,
	// so the proof has to be a number demoteByTrust would read.
	//
	// Both stores give dax the same 20 honest, fully-corroborated runs. One adds
	// 20 runs of five uncorroborated findings each, labelled `security`: real
	// vocabulary, discriminating, outside dax's remit. Before this change those
	// records were dropped and dax stayed at a perfect 1.00, clearing
	// reconcile's trustHighThreshold of 0.7 for blanket exemption.
	seed := func(t *testing.T, withPhantoms bool) string {
		t.Helper()
		dir := t.TempDir()
		for i := 0; i < 20; i++ {
			runID := runIDAt(time.Now(), fmt.Sprintf("honest-%03d", i))
			r := reviewer_(runID, "Dax", "m1", 1, 1)
			r.CategoriesRaised = []string{reclib.CategoryTesting}
			require.NoError(t, Append(dir, r))
		}
		if !withPhantoms {
			return dir
		}
		for i := 0; i < 20; i++ {
			runID := runIDAt(time.Now(), fmt.Sprintf("phantom-%03d", i))
			p := reviewer_(runID, "Dax", "m1", 5, 0)
			p.CategoriesRaised = []string{reclib.CategorySecurity}
			require.NoError(t, Append(dir, p))
		}
		return dir
	}

	clean, err := TrustPriors(seed(t, false), 10)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, clean["dax"], 1e-9, "the control: twenty corroborated of twenty raised")

	withPhantoms, err := TrustPriors(seed(t, true), 10)
	require.NoError(t, err)
	// 20 corroborated of (20 + 100) raised.
	assert.InDelta(t, 20.0/120.0, withPhantoms["dax"], 1e-9,
		"a hundred uncorroborated out-of-remit findings must reach the denominator")
	assert.Less(t, withPhantoms["dax"], 0.3,
		"and must drop the lens below reconcile's trustHighThreshold, not leave it exempt at 1.00")
}

func TestTrustPriors_OutOfRemitCorroborationAlsoRaisesAPrior(t *testing.T) {
	// The OTHER half of the 5.5 narrowing, which the change's first telling
	// described only in the charging direction. Keeping an out-of-remit raiser
	// puts it in the NUMERATOR as well as the denominator, so out-of-remit work
	// that the panel agreed with now lifts a prior where it used to be dropped.
	//
	// That is the honest consequence of the rule and not a leak to patch: a lens
	// cannot be accountable for its out-of-lane mistakes and unrewarded for its
	// out-of-lane hits by the same gate. It is pinned here because a reader who
	// met only the phantom probe would expect this direction to be impossible.
	dir := t.TempDir()
	// Twenty in-remit runs, none corroborated: the floor case, rate 0.0.
	for i := 0; i < 20; i++ {
		r := reviewer_(runIDAt(time.Now(), fmt.Sprintf("inremit-%03d", i)), "Dax", "m1", 1, 0)
		r.CategoriesRaised = []string{reclib.CategoryTesting}
		require.NoError(t, Append(dir, r))
	}
	floor, err := TrustPriors(dir, 10)
	require.NoError(t, err)
	assert.InDelta(t, 0.0, floor["dax"], 1e-9,
		"the control: twenty raised, none corroborated")

	// Twenty more runs of five CORROBORATED findings each, all labelled
	// security — real vocabulary, outside dax's remit.
	for i := 0; i < 20; i++ {
		r := reviewer_(runIDAt(time.Now(), fmt.Sprintf("outremit-%03d", i)), "Dax", "m1", 5, 5)
		r.CategoriesRaised = []string{reclib.CategorySecurity}
		require.NoError(t, Append(dir, r))
	}
	lifted, err := TrustPriors(dir, 10)
	require.NoError(t, err)
	// 100 corroborated of 120 raised.
	assert.InDelta(t, 100.0/120.0, lifted["dax"], 1e-9)
	assert.Greater(t, lifted["dax"], floor["dax"],
		"out-of-remit corroboration must raise the prior, symmetrically with out-of-remit phantoms lowering it")
}

func TestOpportunityDisposition_ZeroRaisedContributorIsDropped(t *testing.T) {
	// THE CELL NO OTHER FIXTURE IN THIS FILE COULD BUILD, and the reason a gate
	// round was able to flip it with the whole package green.
	//
	// A lens that raised nothing but contributed a discriminating out-of-remit
	// category — the shape the ambiguous stream really produces — is DROPPED.
	// Keeping it was tried and reverted: a zero-raised record carries no
	// denominator, so keeping it cannot improve the rate, but it does add a run
	// toward the minRuns floor, which buys publication. See
	// TestTrustPriors_ZeroRaisedContributionsCannotBuyTheFloor for that half.
	union := map[string]struct{}{"performance": {}}

	assert.Equal(t, dispOutOfRemit,
		opportunityDisposition(oppRecRaw("r", "sasha", 0, []string{"performance"}), union),
		"zero raised, contributed an OUT-OF-REMIT topic: dropped")
	assert.Equal(t, dispCounted,
		opportunityDisposition(oppRecRaw("r", "penny", 0, []string{"performance"}), union),
		"zero raised, contributed an IN-REMIT topic: kept, via the remit test")
	assert.Equal(t, dispOutOfRemit,
		opportunityDisposition(oppRecRaw("r", "sasha", 0, nil), union),
		"zero raised, contributed nothing: dropped, unchanged")
	assert.Equal(t, dispCounted,
		opportunityDisposition(oppRecRaw("r", "sasha", 2, []string{"performance"}), union),
		"the boundary: the SAME record with findings is kept")
}

func TestTrustPriors_ZeroRaisedContributionsCannotBuyTheFloor(t *testing.T) {
	// The consequence that made the reverted hoist worse than the complaint it
	// answered. A zero-raised record moves neither side of the ratio, so the
	// obvious reading is that keeping it is harmless. It is not: trustPriorsSince
	// tallies t.runs over every KEPT record and compares that to minRuns, so a
	// hundred evidence-free records can carry a lens over a floor its real
	// history does not reach — and publish it at a rate computed from five runs.
	//
	// 1.0000 is above reconcile's trustHighThreshold (0.7), so the published
	// value is not merely optimistic, it is blanket trustExempt.
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		r := reviewer_(runIDAt(time.Now(), fmt.Sprintf("honest-%03d", i)), "Dax", "m1", 1, 1)
		r.CategoriesRaised = []string{reclib.CategoryTesting}
		require.NoError(t, Append(dir, r))
	}
	rates, err := TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	require.NotContains(t, rates, "dax", "the control: five runs do not clear a twenty-run floor")

	// A hundred zero-raised out-of-lane contributions, exactly as the ambiguous
	// stream writes them.
	for i := 0; i < 100; i++ {
		runID := runIDAt(time.Now(), fmt.Sprintf("phantom-%03d", i))
		p := reviewer_(runID, "Dax", "m1", 0, 0)
		p.CategoriesRaised = []string{reclib.CategorySecurity}
		require.NoError(t, Append(dir, p))
		// Another lens keeps the union discriminating and non-empty.
		other := reviewer_(runID, "Sasha", "m1", 1, 0)
		other.CategoriesRaised = []string{reclib.CategorySecurity}
		require.NoError(t, Append(dir, other))
	}

	rates, err = TrustPriors(dir, DefaultTrustMinRuns)
	require.NoError(t, err)
	assert.NotContains(t, rates, "dax",
		"evidence-free records must not carry a lens over the floor into a published 1.0000 prior")
}

// TestTrustPriorsAndDetails_AgreesWithTheTwoPublicFaces pins the single-read
// entry point cli/personas.go consumes: one ReadSince must yield exactly the
// maps TrustPriors and ExplainTrustPriors produce when each reads the store
// itself. Divergence here would mean `personas list --scores` renders numbers
// computed from a different record set than the two public faces document.
func TestTrustPriorsAndDetails_AgreesWithTheTwoPublicFaces(t *testing.T) {
	dir := t.TempDir()
	appendN(t, dir, 4, "Sasha", "opus", 2, 1)
	appendN(t, dir, 4, "bruce", "opus", 1, 1)

	wantRates, err := TrustPriors(dir, 0)
	require.NoError(t, err)
	wantDetails, err := ExplainTrustPriors(dir, 0)
	require.NoError(t, err)

	gotRates, gotDetails, err := TrustPriorsAndDetails(dir, 0)
	require.NoError(t, err)
	assert.Equal(t, wantRates, gotRates,
		"the combined read must produce TrustPriors' rates exactly")
	assert.Equal(t, wantDetails, gotDetails,
		"the combined read must produce ExplainTrustPriors' details exactly")
}

// TestTrustPriorsAndDetails_MissingStoreIsFailNeutral pins the combined entry
// point's contract on a missing store: empty maps and nil error, the same
// fail-neutral posture both public faces document.
func TestTrustPriorsAndDetails_MissingStoreIsFailNeutral(t *testing.T) {
	rates, details, err := TrustPriorsAndDetails(filepath.Join(t.TempDir(), "absent"), 0)
	require.NoError(t, err)
	require.NotNil(t, rates)
	require.NotNil(t, details)
	assert.Empty(t, rates)
	assert.Empty(t, details)
}

func TestScrubForgedCredit_LeavesAnAboveCurrentEraRecordUntouched(t *testing.T) {
	// An above-current era record was measured under a rule this binary does
	// not implement, so the CURRENT era's credit bound does not apply to it —
	// judging it against the current bound and zeroing it rewrites a record
	// whose honest ceiling is not computable here. Every sibling era gate
	// (unresolvedEraRuns, weightedCreditByPersona, pairTallies) excludes
	// above-current records rather than clamping them.
	above := reviewer_("run-a", "Dax", "m1", 2, 1)
	above.WeightedCredit = 5.0
	above.CreditEra = CreditEraCurrent + 1
	in := []Record{above}
	before := append([]Record{}, in...)
	out := scrubForgedCredit(in)
	assert.Equal(t, before, out,
		"an above-current record's bound is not computable here — leave it untouched")
}

func TestOpportunityUnions_AnAboveCurrentDenominatorRecordContributesNoTopic(t *testing.T) {
	// unresolvedEraRuns excludes a record whose RaisedDenominator exceeds
	// RaisedDenominatorCurrent because it was computed under a definition this
	// binary does not implement. The union loop must apply the same exclusion:
	// a record this binary refuses to score must not decide which OTHER lenses
	// get scored on that run — one discriminating category from such a record
	// flips the union non-empty and deletes every silent out-of-remit lens's
	// record from the denominator.
	r := oppRec("run-1", "sasha", []string{"security"})
	r.RaisedDenominator = RaisedDenominatorCurrent + 1
	unions := opportunityUnions([]Record{r})
	assert.NotContains(t, unions, "run-1",
		"an era-uninterpretable record must not contribute topic evidence")
}

func TestOpportunity_AnUnrecognisedCategoryIsNotATopic(t *testing.T) {
	// A stored categories_raised value this binary does not recognise (a newer
	// pin wrote it; reclib/ is separately versioned) must not count as a
	// discriminating topic. Under the bare denylist test it flipped the run's
	// union from empty to non-empty, matched no entry in personaRemit, and
	// dropped every mapped lens that raised nothing on that run — the exact
	// blackout TestPersonaRemit_EveryDiscriminatingCategoryHasALens exists to
	// prevent, reached from outside the vocabulary the test can see.
	junk := oppRec("run-1", "sasha", []string{"reclib-word-this-pin-never-had"})
	unions := opportunityUnions([]Record{junk})
	assert.NotContains(t, unions, "run-1",
		"an out-of-vocabulary word is not topic evidence — the union stays empty")

	// Chain level: the silent mapped lens on that run must take the documented
	// refuse-to-guess pass-through, not be deleted from the denominator.
	dax := oppRec("run-1", "dax", nil)
	out := opportunitySetRuns([]Record{junk, dax}, unions)
	assert.Contains(t, out, dax,
		"an empty union means nobody raised a scorable topic — pass the run through un-scoped")
}

func TestRemitFor_ReturnsTheTableSliceWithoutACopy(t *testing.T) {
	// opportunityDisposition runs per record in a chain the risk profile calls
	// performance-critical; RemitCategories' defensive copy per call is the
	// cost the non-copying accessor removes. The exported API must keep
	// copying — it crosses a package boundary where the caller could mutate.
	cats, ok := remitFor("dax")
	require.True(t, ok)
	direct := personaRemit["dax"]
	assert.Equal(t, reflect.ValueOf(direct).Pointer(), reflect.ValueOf(cats).Pointer(),
		"remitFor must return the table's own slice, not a defensive copy")
	pub, ok := RemitCategories("dax")
	require.True(t, ok)
	assert.NotEqual(t, reflect.ValueOf(direct).Pointer(), reflect.ValueOf(pub).Pointer(),
		"RemitCategories remains the copying exported API")
}

// Records written before Record.Outcome existed carry none, and the outcome gate
// drops them. After an upgrade that silently empties the priors map for lenses
// whose history is mostly pre-outcome. The count names that loss so a reconcile
// log line can show it; it never changes the priors themselves.
func TestResolveTrustPriorsAndUnmeasured_CountsLensesOnlyTheOutcomeGateDrops(t *testing.T) {
	dir := t.TempDir()
	appendN(t, dir, DefaultTrustMinRuns, "Pace", "m1", 1, 1) // stamped outcome: in the map
	for i := 0; i < DefaultTrustMinRuns; i++ {
		r := reviewer_(runIDAt(time.Now(), fmt.Sprintf("legacy-%03d", i)), "Dax", "m1", 1, 1)
		r.Outcome = ""
		require.NoError(t, Append(dir, r))
	}
	for i := 0; i < DefaultTrustMinRuns-1; i++ { // below the floor either way: not counted
		r := reviewer_(runIDAt(time.Now(), fmt.Sprintf("thin-%03d", i)), "Mira", "m1", 1, 1)
		r.Outcome = ""
		require.NoError(t, Append(dir, r))
	}

	priors, unmeasured := resolveTrustPriorsAndUnmeasured(dir, time.Now(), nil)
	want, err := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, time.Now(), nil)
	require.NoError(t, err)
	assert.Equal(t, want, priors, "the priors must be exactly what ResolveTrustPriors returns")
	assert.Contains(t, priors, "pace")
	assert.NotContains(t, priors, "dax")
	assert.Equal(t, 1, unmeasured, "dax clears the floor except for its missing outcomes; mira never would")
}

// TestWeightedRate_UnmeasurableConfirmationFallsBackToBinary pins the !ok arm
// after confirmationFactor: a confirmation below minConfirmationOutcomes, or one
// carrying a negative count, is not a measurement, so the weighted branch must
// return the binary rate rather than multiply the credit by a zero factor.
func TestWeightedRate_UnmeasurableConfirmationFallsBackToBinary(t *testing.T) {
	w := weightedTally{credit: 5, raised: 10, runs: DefaultTrustMinRuns}
	binary := ratio(3, 10)
	for name, c := range map[string]Confirmation{
		"below the outcome floor": {Dismissed: minConfirmationOutcomes - 1},
		"negative count":          {Confirmed: -1, Dismissed: minConfirmationOutcomes + 5},
	} {
		assert.InDelta(t, binary, weightedRate(3, 10, w, c, 0), 1e-9, name)
	}
}

// TestOpportunitySetRuns_KeepsWhatItCannotJudge pins the keep-unjudged guard:
// an aggregate record and a pre-v2 record carry no per-lens remit evidence, so
// both survive even on a run whose union is wholly out of the lens's remit,
// while the same silent lens at v2 is dropped there.
func TestOpportunitySetRuns_KeepsWhatItCannotJudge(t *testing.T) {
	run := runIDAt(time.Now(), "offremit")
	union := map[string]map[string]struct{}{run: {reclib.CategoryPerformance: {}}}

	current := reviewer_(run, "Dax", "m1", 0, 0)
	preEra := reviewer_(run, "Dax", "m1", 0, 0)
	preEra.SchemaVersion = categoriesRaisedSinceSchema - 1
	agg := reviewer_(run, "Dax", "m1", 0, 0)
	agg.RecordType = RecordTypeAggregate

	require.Empty(t, opportunitySetRuns([]Record{current}, union),
		"precondition: a silent v2 lens on an out-of-remit run is dropped")
	assert.Len(t, opportunitySetRuns([]Record{preEra}, union), 1, "a pre-v2 record is never judged")
	assert.Len(t, opportunitySetRuns([]Record{agg}, union), 1, "an aggregate record is never judged")
}
