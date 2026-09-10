package payload

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func entries(pairs ...any) []FileEntry {
	var out []FileEntry
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, FileEntry{Path: pairs[i].(string), Size: int64(pairs[i+1].(int))})
	}
	return out
}

func keptPaths(kept []FileEntry) []string {
	out := make([]string, len(kept))
	for i, e := range kept {
		out[i] = e.Path
	}
	return out
}

// modeEntries builds entries as (path, size, mode) triples so the escalation-aware
// drop order can be exercised against a mixed payload.
func modeEntries(triples ...any) []FileEntry {
	var out []FileEntry
	for i := 0; i < len(triples); i += 3 {
		out = append(out, FileEntry{
			Path: triples[i].(string),
			Size: int64(triples[i+1].(int)),
			Mode: triples[i+2].(PayloadMode),
		})
	}
	return out
}

// Escalating a file to a higher-context mode replaces its hunks with far more
// text, which makes it the LARGEST entry — so plain largest-first sheds exactly
// the file the escalation heuristic just flagged as hardest to review. The
// escalation-aware order drops the un-escalated files first instead.
func TestBudgetPreferEscalated_KeepsEscalatedFileThatLargestFirstWouldShed(t *testing.T) {
	in := modeEntries(
		"esc.go", 200, ModeFiles, // escalated above the configured diff mode
		"a.go", 40, ModeDiff,
		"b.go", 40, ModeDiff,
	)

	// Plain largest-first sheds the escalated file: that is the defect.
	plain, ptr := ApplyByteBudget(in, 240)
	require.True(t, ptr.Truncated)
	require.Equal(t, []string{"esc.go"}, ptr.FilesDropped,
		"precondition: plain largest-first drops the escalated file")
	require.ElementsMatch(t, []string{"a.go", "b.go"}, keptPaths(plain))

	kept, tr := ApplyByteBudgetPreferEscalated(in, 240, ModeDiff)
	assert.True(t, tr.Truncated)
	assert.Contains(t, keptPaths(kept), "esc.go",
		"the escalated file must survive a shed that can fit it")
	assert.Equal(t, []string{"a.go"}, tr.FilesDropped,
		"an un-escalated file is shed instead, largest-first (then path order) within that group")
}

// The exemption must not turn a survivable shed into an empty payload: when
// keeping the escalated file would drop everything else and still not fit, fall
// back to plain largest-first so the reviewer gets the small files.
func TestBudgetPreferEscalated_FallsBackWhenExemptionWouldDropEverything(t *testing.T) {
	in := modeEntries(
		"esc.go", 200, ModeFiles,
		"a.go", 50, ModeDiff,
	)

	kept, tr := ApplyByteBudgetPreferEscalated(in, 100, ModeDiff)
	assert.True(t, tr.Truncated)
	assert.False(t, tr.AllDropped,
		"falling back to largest-first keeps a.go rather than dropping everything")
	assert.Equal(t, []string{"a.go"}, keptPaths(kept))
	assert.Equal(t, []string{"esc.go"}, tr.FilesDropped)
}

// With nothing escalated the two orderings must agree exactly, so the wiring is
// a no-op on every review where the escalation heuristic did not fire.
func TestBudgetPreferEscalated_MatchesPlainWhenNothingEscalated(t *testing.T) {
	in := modeEntries(
		"a.go", 200, ModeDiff,
		"b.go", 40, ModeDiff,
		"c.go", 40, ModeDiff,
	)

	wantKept, wantTr := ApplyByteBudget(in, 100)
	gotKept, gotTr := ApplyByteBudgetPreferEscalated(in, 100, ModeDiff)

	assert.Equal(t, keptPaths(wantKept), keptPaths(gotKept))
	assert.Equal(t, wantTr, gotTr)
}

// A file rendered BELOW the configured mode (or with no mode at all, as on
// baseline scans) is not escalated and must not be exempted.
func TestBudgetPreferEscalated_OnlyHigherContextModesAreExempt(t *testing.T) {
	in := modeEntries(
		"big.go", 200, PayloadMode(""), // baseline entry: no mode, not escalated
		"a.go", 40, ModeBlocks,
		"b.go", 40, ModeBlocks,
	)

	kept, tr := ApplyByteBudgetPreferEscalated(in, 100, ModeBlocks)
	assert.Equal(t, []string{"big.go"}, tr.FilesDropped,
		"an un-escalated large file is still shed largest-first")
	assert.ElementsMatch(t, []string{"a.go", "b.go"}, keptPaths(kept))
}

// Kept entries keep their input order regardless of the drop ordering used, so
// the rendered payload stays deterministic.
func TestBudgetPreferEscalated_KeptRetainsInputOrder(t *testing.T) {
	in := modeEntries(
		"a.go", 40, ModeDiff,
		"esc.go", 200, ModeFiles,
		"b.go", 40, ModeDiff,
	)

	kept, _ := ApplyByteBudgetPreferEscalated(in, 240, ModeDiff)
	assert.Equal(t, []string{"esc.go", "b.go"}, keptPaths(kept),
		"a.go is shed (un-escalated, alphabetically first at equal size); the rest keep input order")
}

func TestBudget_UnderLimit(t *testing.T) {
	in := entries("a", 60000, "b", 30000, "c", 20000, "d", 10000)
	kept, tr := ApplyByteBudget(in, 200000)
	assert.False(t, tr.Truncated)
	assert.Empty(t, tr.FilesDropped)
	assert.Len(t, kept, 4)
}

func TestBudget_OverLimit_DropsLargestFirst(t *testing.T) {
	// A=60KB B=30KB C=20KB D=10KB, budget 100KB → drop A → keep B,C,D.
	in := entries("A", 60000, "B", 30000, "C", 20000, "D", 10000)
	kept, tr := ApplyByteBudget(in, 100000)
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"A"}, tr.FilesDropped)
	assert.ElementsMatch(t, []string{"B", "C", "D"}, keptPaths(kept))
}

func TestBudget_SingleFileExceeds(t *testing.T) {
	in := entries("big", 15000)
	kept, tr := ApplyByteBudget(in, 10000)
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"big"}, tr.FilesDropped)
	assert.Empty(t, kept)
}

func TestBudget_AllFilesDropped(t *testing.T) {
	in := entries("a", 500, "b", 600, "c", 700)
	kept, tr := ApplyByteBudget(in, 100)
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"a", "b", "c"}, tr.FilesDropped)
	assert.Empty(t, kept)
}

func TestBudget_ZeroIsUnlimited(t *testing.T) {
	in := entries("a", 1_000_000, "b", 2_000_000)
	kept, tr := ApplyByteBudget(in, 0)
	assert.False(t, tr.Truncated)
	assert.Empty(t, tr.FilesDropped)
	assert.Len(t, kept, 2)
}

func TestBudget_ExactFit(t *testing.T) {
	in := entries("a", 30000, "b", 20000)
	kept, tr := ApplyByteBudget(in, 50000)
	assert.False(t, tr.Truncated)
	assert.Len(t, kept, 2)
}

func TestBudget_Deterministic(t *testing.T) {
	in := entries("a", 60000, "b", 30000, "c", 20000, "d", 10000)
	k1, t1 := ApplyByteBudget(in, 100000)
	k2, t2 := ApplyByteBudget(in, 100000)
	assert.Equal(t, t1, t2)
	assert.Equal(t, keptPaths(k1), keptPaths(k2))
}

func TestBudget_TieBreaking(t *testing.T) {
	// Equal sizes: drop the alphabetically-first paths first. Budget fits one.
	in := entries("zeta", 100, "alpha", 100, "mike", 100)
	_, tr := ApplyByteBudget(in, 100)
	// total 300, budget 100 → drop two smallest-by-path: alpha, mike.
	assert.Equal(t, []string{"alpha", "mike"}, tr.FilesDropped)
}

func TestBudget_DuplicatePaths(t *testing.T) {
	// Two entries share a path; each must be accounted for independently.
	in := []FileEntry{
		{Path: "dup", Size: 40000},
		{Path: "dup", Size: 40000},
		{Path: "small", Size: 5000},
	}
	kept, tr := ApplyByteBudget(in, 50000)
	// total 85000 > 50000. Drop largest-first: one dup(40000)→45000 under.
	// One file dropped, the other dup and small remain.
	assert.True(t, tr.Truncated)
	assert.Len(t, tr.FilesDropped, 1)
	var keptBytes int64
	for _, e := range kept {
		keptBytes += e.Size
	}
	assert.LessOrEqual(t, keptBytes, int64(50000))
}

func TestBudget_ZeroSizeFiles(t *testing.T) {
	in := entries("empty", 0, "big", 60000, "small", 10000)
	kept, tr := ApplyByteBudget(in, 50000)
	// Largest-first dropping sheds only the over-budget big file; zero-size
	// files cost nothing and are kept.
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"big"}, tr.FilesDropped)
	assert.ElementsMatch(t, []string{"empty", "small"}, keptPaths(kept))
}

func TestBudget_NegativeBudget(t *testing.T) {
	err := ValidateBudget(-1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "byte budget must be >= 0, got -1")
	assert.NoError(t, ValidateBudget(0))
	assert.NoError(t, ValidateBudget(1000))
}

func TestBudget_NegativeSizeCannotBypassTruncation(t *testing.T) {
	// A negative size must not offset real bytes: the real 60000-byte file
	// exceeds the 50000 budget, so the pass must truncate, never silently
	// keep an over-budget payload.
	in := []FileEntry{
		{Path: "big", Size: 60000},
		{Path: "neg", Size: -59000},
	}
	_, tr := ApplyByteBudget(in, 50000)
	assert.True(t, tr.Truncated)
	assert.NotEmpty(t, tr.FilesDropped)
}

func TestBudget_OverflowCannotBypassTruncation(t *testing.T) {
	// Summing pathological sizes must saturate, not wrap negative — a wrapped
	// total would compare <= budget and skip truncation entirely.
	in := []FileEntry{
		{Path: "huge1", Size: math.MaxInt64},
		{Path: "huge2", Size: math.MaxInt64},
		{Path: "tiny", Size: 100},
	}
	_, tr := ApplyByteBudget(in, 1000)
	assert.True(t, tr.Truncated)
	assert.NotEmpty(t, tr.FilesDropped)
}

// TestBudget_AllDropped_Signal verifies that when every file is removed by the
// budget pass, AllDropped is set on the returned Truncation so callers can
// surface a distinct signal rather than receiving an empty-but-Truncated payload
// with no indication that everything was shed.
func TestBudget_AllDropped_Signal(t *testing.T) {
	// All three files exceed the budget individually.
	in := entries("a", 500, "b", 600, "c", 700)
	kept, tr := ApplyByteBudget(in, 100)
	assert.Empty(t, kept)
	assert.True(t, tr.Truncated)
	assert.True(t, tr.AllDropped, "AllDropped must be true when zero files remain after budget pass")
}

func TestBudget_AllDropped_FalseWhenSomeKept(t *testing.T) {
	// Some files fit: AllDropped must NOT be set.
	in := entries("big", 90000, "small", 5000)
	_, tr := ApplyByteBudget(in, 50000)
	assert.True(t, tr.Truncated)
	assert.False(t, tr.AllDropped, "AllDropped must be false when at least one file is kept")
}

func TestBudget_KeepsMostFiles_DropsLargestFirst(t *testing.T) {
	// The TD-flagged case: sizes [1,2,3,4,90], budget 90. Keep-most-files
	// policy drops the single 90-byte file (generated/lockfile shaped) and
	// keeps the four small source files — not the inverse.
	in := entries("e", 90, "a", 1, "b", 2, "c", 3, "d", 4)
	kept, tr := ApplyByteBudget(in, 90)
	assert.True(t, tr.Truncated)
	assert.Equal(t, []string{"e"}, tr.FilesDropped)
	assert.ElementsMatch(t, []string{"a", "b", "c", "d"}, keptPaths(kept))
}

// The ledger exemption is bounded by the budget. An entry that can never fit
// cannot be funded by shedding entries that do: dropping a 500-byte source file
// to keep a 5000-byte ledger under a 2000-byte budget destroys the review and
// still overruns, and the resulting AllDropped trips ErrPayloadFullyDropped.
// The documented contract — "diff content sheds to fund the ledger" — only holds
// while the ledger is something the budget can actually hold.
func TestBudget_ClaimLedgerLargerThanBudgetShedsLikeAnyEntry(t *testing.T) {
	ledger := newClaimLedgerEntry("CLAIMS BLOCK")
	ledger.Size = 5000 // the fallback re-fit counts the ledger like any other entry
	in := []FileEntry{ledger, {Path: "a.go", Size: 500}}
	kept, tr := ApplyByteBudget(in, 2000)
	assert.Equal(t, []string{"a.go"}, keptPaths(kept), "a file that fits must not be shed to fund a ledger that never fits")
	assert.Equal(t, []string{ClaimLedgerPath}, tr.FilesDropped)
	assert.False(t, tr.AllDropped, "the reviewable file survived, so the payload is not fully dropped")
}

// At exactly the budget the ledger still fits, so the exemption applies and the
// diff content sheds to fund it — the contract in the direction it was written.
func TestBudget_ClaimLedgerExactlyAtBudgetIsKept(t *testing.T) {
	ledger := newClaimLedgerEntry("CLAIMS BLOCK")
	ledger.Size = 2000
	in := []FileEntry{ledger, {Path: "a.go", Size: 500}}
	kept, tr := ApplyByteBudget(in, 2000)
	assert.Equal(t, []string{ClaimLedgerPath}, keptPaths(kept))
	assert.Equal(t, []string{"a.go"}, tr.FilesDropped)
	assert.True(t, tr.AllDropped, "no reviewable file survived")
}

// The shed exemption must key on a sentinel this package sets, never on a path
// string a repository can contain. Angle brackets are illegal in a path only on
// Windows: `<claims>` is a perfectly legal filename on Linux and macOS, so a PR
// that adds or modifies one would otherwise get an unshedable diff entry that is
// also invisible to the reviewable accounting behind AllDropped.
func TestBudget_RepositoryFileNamedLikeTheLedgerIsNotExempt(t *testing.T) {
	in := []FileEntry{
		{Path: ClaimLedgerPath, Size: 5000, Body: "attacker-supplied file content"},
		{Path: "a.go", Size: 10, Body: "a"},
	}
	kept, tr := ApplyByteBudget(in, 5000)
	assert.Equal(t, []string{"a.go"}, keptPaths(kept), "a repository file named <claims> sheds like any other entry")
	assert.Equal(t, []string{ClaimLedgerPath}, tr.FilesDropped)
	assert.False(t, tr.AllDropped, "it is a reviewable file, so it counts in the AllDropped accounting")
}

// Truncated must describe the shed that actually happened, not the arithmetic
// that predicted one. The record is published as status.json's truncated and
// read by internal/benchmark/outcome.go, which maps it to OutcomeIncomplete
// ("saw only a FRACTION of the diff"), and by refitFallbackPayload, which takes
// its re-fit arm on it. A "truncated" record naming nothing dropped reports a
// complete review as incomplete and re-renders a payload it never changed.
//
// Every entry here is shed-exempt, so the total overruns the budget and yet
// nothing can be dropped — the one shape that separates "the sum was too big"
// from "something was actually shed".
func TestBudget_TruncatedReflectsTheShedThatHappened(t *testing.T) {
	// The fixture changed, the invariant did not. This was built from TWO
	// shed-exempt ledger entries that each fitted the budget while their sum did
	// not, giving "the total overruns and yet nothing is shedable".
	//
	// That shape is now UNREACHABLE BY CONSTRUCTION. Epic 35.16.8 added a second
	// synthetic section, so two exempt entries could jointly overrun a small
	// budget and shed every reviewable file to fund sections that left no room for
	// code; the exemption is therefore funded CUMULATIVELY, and the funded set
	// always sums to <= budget. If every entry is exempt and all are funded, the
	// total is within budget and the pass returns before shedding anything. (Two
	// ledgers never occur in production either — TestRangeBuilder_EmitsExactlyOneLedgerEntry
	// pins exactly one; the pair was only ever a device for building this shape.)
	//
	// What must still hold, and is asserted in BOTH directions below: Truncated
	// describes the shed that ACTUALLY happened, never the arithmetic that
	// predicted one. status.json publishes it, internal/benchmark maps it to
	// OutcomeIncomplete, and refitFallbackPayload takes its re-fit arm on it — so
	// a flag set without a corresponding drop reports a complete review as partial
	// and re-renders a payload nothing changed.

	// Direction 1 — nothing shed: the funded section and the code both fit.
	a := newClaimLedgerEntry("CLAIMS A")
	a.Size = 40
	kept, tr := ApplyByteBudget([]FileEntry{a, {Path: "a.go", Size: 5, Body: "a"}}, 50)
	assert.Len(t, kept, 2, "everything fits, so everything survives")
	assert.Empty(t, tr.FilesDropped)
	assert.False(t, tr.Truncated, "nothing was dropped, so nothing was truncated")
	assert.False(t, tr.AllDropped, "the reviewable file survived")

	// Direction 2 — something WAS shed: the record must name it, not just flag it.
	b := newClaimLedgerEntry("CLAIMS B")
	b.Size = 80
	kept2, tr2 := ApplyByteBudget([]FileEntry{b, {Path: "a.go", Size: 10, Body: "a"}}, 50)
	assert.True(t, tr2.Truncated, "an exempt section the budget cannot fund is a real shed")
	assert.Equal(t, []string{ClaimLedgerPath}, tr2.FilesDropped,
		"a truncated record must name what it dropped")
	assert.Equal(t, []string{"a.go"}, keptPaths(kept2),
		"the reviewable file survives; the unfundable section does not displace it")
	assert.False(t, tr2.AllDropped, "reviewable content survived, so this is not an all-dropped payload")
}

// The complement: an ordinary shed still reports Truncated, so the fix above
// cannot have been bought by making the flag never fire.
func TestBudget_OrdinaryShedStillReportsTruncated(t *testing.T) {
	kept, tr := ApplyByteBudget(entries("big.go", 100, "small.go", 10), 20)
	assert.Equal(t, []string{"small.go"}, keptPaths(kept))
	assert.Equal(t, []string{"big.go"}, tr.FilesDropped)
	assert.True(t, tr.Truncated)
}
