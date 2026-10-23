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

// TestAggregateQualitySignal_UnknownFeedbackKindCreatesNoRow: the write gate
// refuses an off-vocabulary kind, but a hand-edited store can still carry one;
// it counts nowhere and must not create an all-zero row.
func TestAggregateQualitySignal_UnknownFeedbackKindCreatesNoRow(t *testing.T) {
	open := attributed("e000000000000001", "2026-06-01T00:00:00Z", "", "bruce")
	rows := AggregateQualitySignal([]Record{open, annotated(open, "2026-06-10T00:00:00Z", ReconcilerFeedback{Kind: "split", Reason: "r"})})
	assert.Empty(t, rows)
}

// --- Epic 36.1 F1: the annotation survives a re-detection and compaction ---

// redetectedAfterRecluster is the store F1 names: a finding, the recluster
// annotation on it, then the finding resolved (a copy of the annotated
// effective record, as `debt resolve` writes it) and re-detected by reconcile
// as a status-less sighting carrying no annotation. The re-detection is the
// newest record, so it wins rule 2 of the fold.
func redetectedAfterRecluster() []Record {
	open := attributed("f000000000000001", "2026-06-01T00:00:00Z", "", "bruce")
	marked := annotated(open, "2026-06-10T00:00:00Z", ReconcilerFeedback{Kind: FeedbackWrongMerge, Reason: "two races"})
	resolved := marked
	resolved.Timestamp, resolved.RunID = "2026-06-11T00:00:00Z", "2026-06-11T00:00:00Z-resolved"
	resolved.Status, resolved.ResolvedAt = StatusResolved, "2026-06-11T00:00:00Z"
	regressed := attributed("f000000000000001", "2026-06-20T00:00:00Z", "", "bruce")
	return []Record{open, marked, resolved, regressed}
}

// TestAggregateQualitySignal_ReconcilerCountSurvivesARedetection: the plainest
// form — finding, annotation, newer plain sighting — still counts the
// developer's feedback, and so does a resolved finding that regresses.
func TestAggregateQualitySignal_ReconcilerCountSurvivesARedetection(t *testing.T) {
	open := attributed("f000000000000002", "2026-06-01T00:00:00Z", "", "bruce")
	marked := annotated(open, "2026-06-10T00:00:00Z", ReconcilerFeedback{Kind: FeedbackWrongSplit, Reason: "one race"})
	sighting := attributed("f000000000000002", "2026-06-20T00:00:00Z", "", "bruce")

	rows := AggregateQualitySignal([]Record{open, marked, sighting})
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].WrongSplitCount, "a newer plain sighting must not drop the annotation")

	rows = AggregateQualitySignal(redetectedAfterRecluster())
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].WrongMergeCount, "a regressed finding keeps its reconciler feedback")
}

// TestFoldRecords_CarriesTheNewestAnnotationOfTheGroup: the folded record of a
// re-detected id carries the newest annotation in its group, and its status is
// still the re-detection's (the annotation never moves the fold).
func TestFoldRecords_CarriesTheNewestAnnotationOfTheGroup(t *testing.T) {
	recs := redetectedAfterRecluster()
	later := annotated(recs[1], "2026-06-12T00:00:00Z", ReconcilerFeedback{Kind: FeedbackWrongCategory, Reason: "r"})
	recs = append(recs[:2:2], later, recs[2], recs[3])

	folded := FoldRecords(recs)
	require.Len(t, folded, 1)
	assert.Equal(t, "", folded[0].Status, "the re-detection still wins the fold")
	assert.Equal(t, "2026-06-20T00:00:00Z", folded[0].Timestamp)
	require.NotNil(t, folded[0].ReconcilerFeedback)
	assert.Equal(t, FeedbackWrongCategory, folded[0].ReconcilerFeedback.Kind, "the newest annotation is carried")
	assert.Equal(t, 2, folded[0].Occurrences, "an annotation is still not a sighting")
}

// TestFoldSummaries_CarriesTheNewestAnnotationOfTheGroup is the summary path's
// half of the same rule, read from disk the way the dashboard reads it.
func TestFoldSummaries_CarriesTheNewestAnnotationOfTheGroup(t *testing.T) {
	dir := t.TempDir()
	for _, r := range redetectedAfterRecluster() {
		require.NoError(t, Append(dir, r))
	}
	sums, err := ReadSummaries(dir, ReadOpts{})
	require.NoError(t, err)
	folded := FoldSummaries(sums)
	require.Len(t, folded, 1)
	assert.Equal(t, "", folded[0].Status)
	assert.Equal(t, FeedbackWrongMerge, folded[0].ReconcilerFeedbackKind)
}

// TestCompact_KeepsTheAnnotationOfARedetectedFinding: compaction must not
// discard the annotation for good. The signal, the summary fold and the
// occurrence count are the same before and after, and a second compaction
// changes nothing.
func TestCompact_KeepsTheAnnotationOfARedetectedFinding(t *testing.T) {
	dir := t.TempDir()
	for _, r := range redetectedAfterRecluster() {
		require.NoError(t, Append(dir, r))
	}
	before, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	wantRows := AggregateQualitySignal(before)
	wantFold := FoldRecords(before)

	for pass := 1; pass <= 2; pass++ {
		_, err = Compact(dir, ReadOpts{})
		require.NoError(t, err)
		after, err := ReadAll(dir, ReadOpts{})
		require.NoError(t, err)
		assert.Equal(t, wantRows, AggregateQualitySignal(after), "pass %d: the reconciler count survives compaction", pass)
		gotFold := FoldRecords(after)
		require.Len(t, gotFold, 1)
		require.NotNil(t, gotFold[0].ReconcilerFeedback, "pass %d", pass)
		assert.Equal(t, FeedbackWrongMerge, gotFold[0].ReconcilerFeedback.Kind, "pass %d", pass)
		assert.Equal(t, wantFold[0].Occurrences, gotFold[0].Occurrences, "pass %d: occurrences unchanged", pass)
		assert.Equal(t, wantFold[0].Status, gotFold[0].Status, "pass %d", pass)

		sums, err := ReadSummaries(dir, ReadOpts{})
		require.NoError(t, err)
		assert.Equal(t, FeedbackWrongMerge, FoldSummaries(sums)[0].ReconcilerFeedbackKind, "pass %d", pass)
	}
}

// TestCompact_RetainedRecordsCarryTheNewestAnnotation: under rule 1 a retained
// record can be newer than the effective one. Here an older wontfix wins the
// fold, a newer resolved record (kept as trail and donor) holds an older
// verdict, and the newest verdict sits on an open record compaction drops. The
// fold must report the newest verdict before and after compaction.
func TestCompact_RetainedRecordsCarryTheNewestAnnotation(t *testing.T) {
	wontfix := attributed("f000000000000003", "2026-06-01T00:00:00Z", StatusWontfix, "bruce")
	wontfix.ResolvedAt, wontfix.Justification = "2026-06-01T00:00:00Z", "hallucinated"
	older := attributed("f000000000000003", "2026-06-03T00:00:00Z", StatusResolved, "bruce")
	older.ResolvedAt, older.Justification = "2026-06-03T00:00:00Z", "fixed"
	older.ReconcilerFeedback = &ReconcilerFeedback{Kind: FeedbackWrongMerge, Reason: "r"}
	newest := annotated(attributed("f000000000000003", "2026-06-04T00:00:00Z", "", "bruce"),
		"2026-06-04T00:00:00Z", ReconcilerFeedback{Kind: FeedbackWrongCategory, Reason: "r"})

	dir := t.TempDir()
	for _, r := range []Record{wontfix, older, newest} {
		require.NoError(t, Append(dir, r))
	}
	before, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	folded := FoldRecords(before)
	require.Len(t, folded, 1)
	require.Equal(t, StatusWontfix, folded[0].Status, "rule 1: the wontfix wins")
	require.Equal(t, FeedbackWrongCategory, folded[0].ReconcilerFeedback.Kind)
	wantRows := AggregateQualitySignal(before)

	_, err = Compact(dir, ReadOpts{})
	require.NoError(t, err)
	after, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	got := FoldRecords(after)
	require.Len(t, got, 1)
	assert.Equal(t, StatusWontfix, got[0].Status)
	require.NotNil(t, got[0].ReconcilerFeedback)
	assert.Equal(t, FeedbackWrongCategory, got[0].ReconcilerFeedback.Kind, "an older verdict on a retained record must not win")
	assert.Equal(t, wantRows, AggregateQualitySignal(after))
	sums, err := ReadSummaries(dir, ReadOpts{})
	require.NoError(t, err)
	assert.Equal(t, FeedbackWrongCategory, FoldSummaries(sums)[0].ReconcilerFeedbackKind)
}
