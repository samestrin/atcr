package localdebt

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD-014: compaction is signal-invariant across a SINGLE compaction, but not
// across compaction followed by a later append.
//
// Nothing in the suite exercised that sequence, which is why the defect
// survived four gate passes on the single-compaction property. Compaction runs
// automatically inside PersistForReconcile on every `atcr reconcile`, so the
// sequence is the ordinary one, not an exotic one — and both failure modes are
// silent and permanent.
//
// THE PROPERTY UNDER TEST: for any record set R and any later append L,
//
//	AggregateQualitySignal(R + L) == AggregateQualitySignal(compact(R) + L)
//
// Phase 4 makes AggregateQualitySignal the durable per-(persona, model)
// ground-truth signal behind the lens score, so a row lost or misattributed
// here is a lens durably mis-scored.
//
// TD-014's two mechanisms are tested SEPARATELY because only one of them is
// fixed. The unsettled branch is a live guard; the settled branch is a skipped
// reproduction. Keeping them in one table would have meant skipping the guard
// too, leaving the fixed half unprotected.

// signalKey renders a QualityRow as a comparable string so two aggregations can
// be diffed by value regardless of group order.
func signalKey(r QualityRow) string {
	return fmt.Sprintf("%s|%s|d=%d|c=%d|u=%d|a=%d|n=%d",
		r.Persona, r.Model, r.DismissedCount, r.ConfirmedCount,
		r.UnreproducibleCount, r.AttemptsExhaustedCount, r.TerminalOutcomes)
}

func signalSet(rows []QualityRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, signalKey(r))
	}
	sort.Strings(out)
	return out
}

// diffRecord builds one record of a single id's history.
func diffRecord(id, status, model, ts string) Record {
	return Record{
		SchemaVersion: SchemaVersion,
		ID:            id,
		Status:        status,
		Model:         model,
		Timestamp:     ts,
		Reviewers:     []string{"vera"},
		File:          "a.go",
		Line:          1,
		Problem:       "p",
		Fix:           "f",
		Category:      "correctness",
	}
}

// assertSignalInvariant is the property itself: compacting a history must not
// change the quality signal a LATER append produces from it.
func assertSignalInvariant(t *testing.T, before, later []Record) {
	t.Helper()
	uncompacted := append(append([]Record{}, before...), later...)
	compacted := append(retainForCompaction(before), later...)

	assert.Equal(t,
		signalSet(AggregateQualitySignal(uncompacted)),
		signalSet(AggregateQualitySignal(compacted)),
		"compaction changed the quality signal a later append produces")
}

// TestCompactThenAppend_UnsettledBranchKeepsTheDonor is a LIVE GUARD on the half
// of TD-014 that is fixed.
//
// The donor used to be gated on producesQualitySignal(eff.Status), which is
// false for an OPEN or `deferred` effective record — so the attribution was
// dropped for the whole interval an item sat open, and the row vanished
// entirely when it was finally closed by a model-less record. That is a LOST
// ROW, not a misattributed one: the outcome disappears from the ground-truth
// signal the durable lens score is built on.
func TestCompactThenAppend_UnsettledBranchKeepsTheDonor(t *testing.T) {
	const id = "abc123"
	assertSignalInvariant(t,
		[]Record{
			diffRecord(id, StatusDeferred, "m1", "2026-09-01T00:00:00Z"),
			diffRecord(id, StatusResolved, "", "2026-09-02T00:00:00Z"),
			diffRecord(id, "", "", "2026-09-03T00:00:00Z"), // open
		},
		[]Record{diffRecord(id, StatusResolved, "", "2026-09-04T00:00:00Z")},
	)
}

// TestCompactThenAppend_SettledBranchDropsAHigherPrecedenceDonor REPRODUCES the
// half of TD-014 that is NOT fixed. It is committed skipped so the reproduction
// survives without leaving the suite red.
//
// modelDonorIndex returns -1 when the effective record already carries a Model,
// so a NEWER, higher-precedence donor is deleted: {wontfix@T1 m2, resolved@T2
// m3} compacts to {wontfix m2}, and a later model-less wontfix is then credited
// to m2 instead of m3. A MISATTRIBUTION rather than a loss, which is the worse
// of the two for a per-(persona, model) score.
//
// WHY IT IS NOT FIXED. retainForCompaction already documents two ordering rules
// that point opposite ways:
//
//   - eff must be emitted LAST, so it wins its own fold (latestItem breaks a
//     full tie by append order).
//   - the donor must win foldTerminalByID's donor slot, which ALSO breaks a
//     timestamp tie by append order — i.e. the donor must come last.
//
// With distinct timestamps the conflict is inert: the timestamp comparison
// dominates both selections and append order never decides. On an exact tie the
// two rules cannot both hold, and choosing which one gives is a decision about
// what every existing store retains. Deciding it needs the identify-eff-by-
// position change already filed as TD-003 for Phase 6, and a live store with
// terminal records to measure against — which does not exist yet (the store
// holds 363 records and zero terminal ones as of 2026-09-20).
//
// Ruled Option A by Sam, 2026-09-20: fix the lost-row half now, leave this one
// open with its reproduction in place.
func TestCompactThenAppend_SettledBranchDropsAHigherPrecedenceDonor(t *testing.T) {
	t.Skip("TD-014 (open half): the settled-branch tie-break needs a decision before the fix lands")

	const id = "abc123"
	assertSignalInvariant(t,
		[]Record{
			diffRecord(id, StatusWontfix, "m2", "2026-09-01T00:00:00Z"),
			diffRecord(id, StatusResolved, "m3", "2026-09-02T00:00:00Z"),
		},
		[]Record{diffRecord(id, StatusWontfix, "", "2026-09-03T00:00:00Z")},
	)
}

func TestCompactThenAppend_RetentionBoundStillHolds(t *testing.T) {
	// TD-014's fix widens what compaction retains, so the documented bound is
	// the thing most at risk from it. Asserted directly rather than assumed.
	const id = "abc123"
	rec := func(status, model, ts string) Record {
		return Record{
			SchemaVersion: SchemaVersion,
			ID:            id,
			Status:        status,
			Model:         model,
			Timestamp:     ts,
			Reviewers:     []string{"vera"},
			File:          "a.go",
			Line:          1,
			Problem:       "p",
			Fix:           "f",
			Category:      "correctness",
		}
	}

	for _, group := range [][]Record{
		{rec(StatusWontfix, "m2", "2026-09-01T00:00:00Z"), rec(StatusResolved, "m3", "2026-09-02T00:00:00Z")},
		{rec(StatusDeferred, "m1", "2026-09-01T00:00:00Z"), rec(StatusResolved, "m2", "2026-09-02T00:00:00Z"), rec("" /* open */, "", "2026-09-03T00:00:00Z")},
		{
			rec(StatusDeferred, "m1", "2026-09-01T00:00:00Z"),
			rec(StatusResolved, "m2", "2026-09-02T00:00:00Z"),
			rec(StatusUnreproducible, "m3", "2026-09-03T00:00:00Z"),
			rec(StatusAttemptsExhausted, "m4", "2026-09-04T00:00:00Z"),
			rec("" /* open */, "", "2026-09-05T00:00:00Z"),
		},
	} {
		retained := retainForCompaction(group)
		require.LessOrEqual(t, len(retained), 3,
			"compaction must retain at most 3 records per id; got %d", len(retained))
	}
}

func TestCompactThenAppend_CompactionStaysIdempotent(t *testing.T) {
	// Widening retention must not make compaction oscillate: compacting an
	// already-compacted group has to be a no-op, or `debt list` shows a
	// different justification after every reconcile.
	const id = "abc123"
	group := []Record{
		{SchemaVersion: SchemaVersion, ID: id, Status: StatusDeferred, Model: "m1", Timestamp: "2026-09-01T00:00:00Z", Reviewers: []string{"vera"}, File: "a.go", Line: 1, Problem: "p", Fix: "f", Category: "correctness"},
		{SchemaVersion: SchemaVersion, ID: id, Status: StatusResolved, Model: "m2", Timestamp: "2026-09-02T00:00:00Z", Reviewers: []string{"vera"}, File: "a.go", Line: 1, Problem: "p", Fix: "f", Category: "correctness"},
		{SchemaVersion: SchemaVersion, ID: id, Status: "", Model: "", Timestamp: "2026-09-03T00:00:00Z", Reviewers: []string{"vera"}, File: "a.go", Line: 1, Problem: "p", Fix: "f", Category: "correctness"},
	}

	once := retainForCompaction(group)
	twice := retainForCompaction(once)
	assert.Equal(t, once, twice, "compaction must be idempotent")
}
