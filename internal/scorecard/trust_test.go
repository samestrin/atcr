package scorecard

import (
	"bytes"
	"encoding/json"
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
// cli/personas.go:44, which calls TrustPriors(dir, 0) directly and must keep
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

	rates, err := trustPriorsSince(dir, 0, defaultTrustWindow, now)
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

	rates, err := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, now)
	require.NoError(t, err)
	assert.NotContains(t, rates, "lenny",
		"only 5 strict runs inside the window — lenient runs must not top the floor up to DefaultTrustMinRuns")

	rates, err = trustPriorsSince(dir, 5, defaultTrustWindow, now)
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

	windowed, err := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, now)
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

	windowed, err := trustPriorsSince(dir, 0, 0, now)
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

	rates, err := trustPriorsSince(dir, DefaultTrustMinRuns, defaultTrustWindow, now)
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
