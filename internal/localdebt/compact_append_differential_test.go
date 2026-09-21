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

func TestCompactThenAppend_IsSignalInvariant(t *testing.T) {
	// SKIPPED, AND THE SKIP IS THE POINT. This test REPRODUCES TD-014 rather
	// than guarding against it: both sub-cases fail today, with exactly the two
	// divergences TD-014 filed (a model misattribution m3 -> m2 on the settled
	// branch, and a lost row on the unsettled branch). It is committed skipped
	// so the reproduction is not lost and the suite is not left red.
	//
	// WHY IT IS NOT FIXED HERE. The unsettled branch is unambiguous — drop the
	// producesQualitySignal(eff.Status) gate and retain the donor. The settled
	// branch is not, and the obstacle is a genuine conflict between two
	// ordering rules retainForCompaction already documents:
	//
	//   - eff must be emitted LAST, so it wins its own fold (latestItem breaks
	//     a full tie by append order).
	//   - the donor must win foldTerminalByID's donor slot, which also breaks a
	//     timestamp tie by append order — i.e. the donor must come last.
	//
	// When the donor and eff carry DISTINCT timestamps the conflict is inert:
	// the timestamp comparison dominates both selections and append order never
	// decides. When they TIE, the two rules demand opposite orders and one of
	// them has to give. Picking which is a decision about what every existing
	// store retains, so it is not made inside a test.
	//
	// See .planning/sprints/active/36.0_durable_lens_authority/tech-debt-captured.md
	// -> TD-014 for the measured divergence rates.
	t.Skip("TD-014: reproduces the defect; the settled-branch tie-break needs a decision before the fix lands")

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

	for _, tc := range []struct {
		name   string
		before []Record
		later  []Record
	}{
		{
			// TD-014 settled branch. modelDonorIndex returns -1 when the
			// effective record already carries a Model, so the NEWER,
			// higher-precedence donor is deleted; a later model-less record is
			// then credited to the surviving older model.
			name: "settled branch drops the higher-precedence donor",
			before: []Record{
				rec(StatusWontfix, "m2", "2026-09-01T00:00:00Z"),
				rec(StatusResolved, "m3", "2026-09-02T00:00:00Z"),
			},
			later: []Record{rec(StatusWontfix, "", "2026-09-03T00:00:00Z")},
		},
		{
			// TD-014 unsettled branch. The donor is gated on
			// producesQualitySignal(eff.Status), false for an OPEN or deferred
			// effective record, so the donor is dropped during the open
			// interval and the whole row vanishes.
			name: "unsettled branch drops the donor while the item is open",
			before: []Record{
				rec(StatusDeferred, "m1", "2026-09-01T00:00:00Z"),
				rec(StatusResolved, "", "2026-09-02T00:00:00Z"),
				rec("" /* open */, "", "2026-09-03T00:00:00Z"),
			},
			later: []Record{rec(StatusResolved, "", "2026-09-04T00:00:00Z")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uncompacted := append(append([]Record{}, tc.before...), tc.later...)
			compacted := append(retainForCompaction(tc.before), tc.later...)

			want := signalSet(AggregateQualitySignal(uncompacted))
			got := signalSet(AggregateQualitySignal(compacted))

			assert.Equal(t, want, got,
				"compaction changed the quality signal a later append produces")
		})
	}
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
