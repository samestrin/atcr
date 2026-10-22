package localdebt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Epic 36.1 T3: reconciler-error counts on the quality signal ---

// attributed builds a fold record carrying model attribution, for the
// reconciler-count tests.
func attributed(id, ts, status string, reviewers ...string) Record {
	r := foldRec(id, ts, status)
	r.Model = "claude-sonnet-4-6"
	r.Reviewers = reviewers
	return r
}

// rowFor returns the row for (persona, model), failing the test when absent.
func rowFor(t *testing.T, rows []QualityRow, persona, model string) QualityRow {
	t.Helper()
	for _, r := range rows {
		if r.Persona == persona && r.Model == model {
			return r
		}
	}
	t.Fatalf("no row for (%s, %s) in %+v", persona, model, rows)
	return QualityRow{}
}

// TestAggregateQualitySignal_ReconcilerCountsApartFromDismissed is T3's success
// criterion at the aggregation layer: a wontfix of a hallucinated finding raises
// DismissedCount only, and a wrong-merge annotation raises the reconciler count
// only.
func TestAggregateQualitySignal_ReconcilerCountsApartFromDismissed(t *testing.T) {
	hallucinated := attributed("a000000000000001", "2026-06-02T00:00:00Z", StatusWontfix, "bruce")
	merged := attributed("a000000000000002", "2026-06-01T00:00:00Z", "", "bruce")
	recs := []Record{
		attributed("a000000000000001", "2026-06-01T00:00:00Z", "", "bruce"),
		hallucinated,
		merged,
		annotated(merged, "2026-06-10T00:00:00Z", ReconcilerFeedback{Kind: FeedbackWrongMerge, Reason: "two races"}),
	}
	rows := AggregateQualitySignal(recs)
	require.Len(t, rows, 1)
	assert.Equal(t, QualityRow{
		Persona: "bruce", Model: "claude-sonnet-4-6",
		DismissedCount: 1, TerminalOutcomes: 1,
		WrongMergeCount: 1,
	}, rows[0])
}

// TestAggregateQualitySignal_ReconcilerCountsPerKind: each kind lands on its own
// counter, and an annotation alone (on a still-open finding, which is what
// `atcr debt recluster` leaves behind) creates a row with no outcome.
func TestAggregateQualitySignal_ReconcilerCountsPerKind(t *testing.T) {
	var recs []Record
	for i, k := range []string{FeedbackWrongMerge, FeedbackWrongSplit, FeedbackWrongSplit, FeedbackWrongCategory} {
		id := "b00000000000000" + string(rune('1'+i))
		open := attributed(id, "2026-06-01T00:00:00Z", "", "diana")
		recs = append(recs, open, annotated(open, "2026-06-10T00:00:00Z", ReconcilerFeedback{Kind: k, Reason: "r"}))
	}
	rows := AggregateQualitySignal(recs)
	require.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, 1, r.WrongMergeCount)
	assert.Equal(t, 2, r.WrongSplitCount)
	assert.Equal(t, 1, r.WrongCategoryCount)
	assert.Zero(t, r.DismissedCount+r.ConfirmedCount+r.UnreproducibleCount+r.AttemptsExhaustedCount)
	assert.Zero(t, r.TerminalOutcomes, "a reconciler count is not a terminal outcome")
}

// TestAggregateQualitySignal_ReconcilerCountsFollowTheFold: the count is fold
// state, one per finding id — annotating a finding twice counts it once under
// its latest kind, a later resolve that carries the annotation forward still
// counts it beside the outcome, and compaction leaves the count unchanged.
func TestAggregateQualitySignal_ReconcilerCountsFollowTheFold(t *testing.T) {
	open := attributed("c000000000000001", "2026-06-01T00:00:00Z", "", "bruce")
	first := annotated(open, "2026-06-10T00:00:00Z", ReconcilerFeedback{Kind: FeedbackWrongMerge, Reason: "r"})
	second := annotated(open, "2026-06-11T00:00:00Z", ReconcilerFeedback{Kind: FeedbackWrongCategory, Reason: "r"})

	rows := AggregateQualitySignal([]Record{open, first, first})
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].WrongMergeCount, "a repeated annotation counts the finding once")

	rows = AggregateQualitySignal([]Record{open, first, second})
	require.Len(t, rows, 1)
	assert.Zero(t, rows[0].WrongMergeCount)
	assert.Equal(t, 1, rows[0].WrongCategoryCount, "the latest annotation on a finding is the one counted")

	// A resolve copies the effective record wholesale, annotation included.
	resolved := second
	resolved.Timestamp, resolved.Status, resolved.ResolvedAt = "2026-06-12T00:00:00Z", StatusWontfix, "2026-06-12T00:00:00Z"
	rows = AggregateQualitySignal([]Record{open, first, second, resolved})
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].DismissedCount)
	assert.Equal(t, 1, rows[0].WrongCategoryCount)

	dir := t.TempDir()
	for _, r := range []Record{open, first, second, resolved} {
		require.NoError(t, Append(dir, r))
	}
	before, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	want := AggregateQualitySignal(before)
	_, err = Compact(dir, ReadOpts{})
	require.NoError(t, err)
	after, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	assert.Equal(t, want, AggregateQualitySignal(after), "compaction leaves the reconciler count unchanged")
}

// TestAggregateQualitySignal_ReconcilerCountsUseTheOutcomePersonaSet: the
// persona set is the one the outcome counters credit — ModelReviewers, falling
// back to Reviewers — deduplicated and trimmed, and an empty Model is excluded.
func TestAggregateQualitySignal_ReconcilerCountsUseTheOutcomePersonaSet(t *testing.T) {
	fb := ReconcilerFeedback{Kind: FeedbackWrongMerge, Reason: "r"}

	narrowed := attributed("d000000000000001", "2026-06-01T00:00:00Z", "", "bruce", "host")
	narrowed.ModelReviewers = []string{"bruce"}
	legacy := attributed("d000000000000002", "2026-06-01T00:00:00Z", "", "diana", " diana ", "")
	modelless := attributed("d000000000000003", "2026-06-01T00:00:00Z", "", "clark")
	modelless.Model = "  "

	recs := []Record{
		narrowed, annotated(narrowed, "2026-06-10T00:00:00Z", fb),
		legacy, annotated(legacy, "2026-06-10T00:00:00Z", fb),
		modelless, annotated(modelless, "2026-06-10T00:00:00Z", fb),
	}
	rows := AggregateQualitySignal(recs)
	require.Len(t, rows, 2, "host is outside ModelReviewers and clark has no model: %+v", rows)
	assert.Equal(t, 1, rowFor(t, rows, "bruce", "claude-sonnet-4-6").WrongMergeCount)
	assert.Equal(t, 1, rowFor(t, rows, "diana", "claude-sonnet-4-6").WrongMergeCount, "a persona listed twice is credited once")
}
