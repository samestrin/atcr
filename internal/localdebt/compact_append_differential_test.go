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

// TestCompactThenAppend_SettledBranchKeepsAHigherPrecedenceDonor is a LIVE GUARD
// on the settled-branch half of TD-014.
//
// modelDonorIndex used to return -1 when the effective record already carried a
// Model, so a NEWER donor was deleted: {wontfix@T1 m2, resolved@T2 m3} compacted
// to {wontfix m2}, and a later model-less wontfix was then credited to m2 instead
// of m3. A MISATTRIBUTION rather than a loss, which is the worse of the two for a
// per-(persona, model) score.
func TestCompactThenAppend_SettledBranchKeepsAHigherPrecedenceDonor(t *testing.T) {
	const id = "abc123"
	assertSignalInvariant(t,
		[]Record{
			diffRecord(id, StatusWontfix, "m2", "2026-09-01T00:00:00Z"),
			diffRecord(id, StatusResolved, "m3", "2026-09-02T00:00:00Z"),
		},
		[]Record{diffRecord(id, StatusWontfix, "", "2026-09-03T00:00:00Z")},
	)
}

// TestCompactThenAppend_ExactTieKeepsTheEffectiveRecord pins the tie-break ruled
// by Sam on 2026-09-24 (option A: status wins). retainForCompaction has two
// ordering rules that point opposite ways on an EXACT timestamp tie between the
// effective record and a model donor: eff must be emitted last to win its own
// fold, and the donor must be emitted last to win foldTerminalByID's donor slot.
// Both break the tie by append order, so only one can hold. The effective record
// wins: a wrong status changes what `debt list` shows and what `debt resolve`
// touches, while a wrong model credit on an exact tie moves one trust score.
func TestCompactThenAppend_ExactTieKeepsTheEffectiveRecord(t *testing.T) {
	const id = "abc123"
	const ts = "2026-09-01T00:00:00Z"
	before := []Record{
		diffRecord(id, StatusWontfix, "m2", ts),
		diffRecord(id, StatusResolved, "m3", ts),
	}
	later := []Record{diffRecord(id, "", "", "2026-09-02T00:00:00Z")} // a re-detection

	uncompacted := FoldRecords(append(append([]Record{}, before...), later...))
	compacted := FoldRecords(append(retainForCompaction(before), later...))
	require.Len(t, uncompacted, 1)
	require.Len(t, compacted, 1)
	assert.Equal(t, uncompacted[0].Status, compacted[0].Status)
	assert.Equal(t, uncompacted[0].Model, compacted[0].Model)

	// The before-set itself also folds to the same effective record.
	eff, kept := FoldRecords(before)[0], FoldRecords(retainForCompaction(before))[0]
	assert.Equal(t, eff.Status, kept.Status)
	assert.Equal(t, eff.Model, kept.Model)
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
		// The four-record case: rationale trail, the re-detection's latest
		// closed record, that record's model donor, and the effective record.
		{
			rec(StatusUnreproducible, "m1", "2026-09-01T00:00:00Z"),
			rec(StatusResolved, "m2", "2026-09-02T00:00:00Z"),
			rec(StatusAttemptsExhausted, "", "2026-09-03T00:00:00Z"),
			rec("" /* open */, "", "2026-09-04T00:00:00Z"),
		},
	} {
		retained := retainForCompaction(group)
		require.LessOrEqual(t, len(retained), 4,
			"compaction must retain at most 4 records per id; got %d", len(retained))
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
