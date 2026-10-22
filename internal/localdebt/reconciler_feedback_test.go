package localdebt

import (
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Epic 36.1 T1: the reconciler-feedback annotation ---

// annotated returns a copy of rec appended later with a reconciler-feedback
// annotation: the shape a recluster writer appends for an existing finding (the
// effective record, re-stamped, status untouched).
func annotated(rec Record, ts string, fb ReconcilerFeedback) Record {
	rec.Timestamp = ts
	rec.RunID = ts + "-recluster"
	rec.Occurrences, rec.CountedThrough, rec.FirstSeen = 0, "", ""
	rec.ReconcilerFeedback = &fb
	return rec
}

func TestRecord_ReconcilerFeedbackFieldOmitempty(t *testing.T) {
	f, ok := reflect.TypeOf(Record{}).FieldByName("ReconcilerFeedback")
	require.True(t, ok, "Record must have a ReconcilerFeedback field")
	assert.Equal(t, "*localdebt.ReconcilerFeedback", f.Type.String())
	assert.Equal(t, "reconciler_feedback,omitempty", f.Tag.Get("json"))
}

func TestReconcilerFeedback_KindConstants(t *testing.T) {
	assert.Equal(t, "wrong-merge", FeedbackWrongMerge)
	assert.Equal(t, "wrong-split", FeedbackWrongSplit)
	assert.Equal(t, "wrong-category", FeedbackWrongCategory)
	for _, k := range []string{FeedbackWrongMerge, FeedbackWrongSplit, FeedbackWrongCategory} {
		assert.True(t, IsKnownFeedbackKind(k), k)
	}
	for _, k := range []string{"", "split", "wrong_merge", "Wrong-Merge", " wrong-merge", "resolved"} {
		assert.False(t, IsKnownFeedbackKind(k), "%q must not be a known kind", k)
	}
	assert.True(t, FeedbackKindTakesPartner(FeedbackWrongSplit))
	assert.False(t, FeedbackKindTakesPartner(FeedbackWrongMerge))
	assert.False(t, FeedbackKindTakesPartner(FeedbackWrongCategory))
	assert.False(t, FeedbackKindTakesPartner("bogus"))
}

// TestRecord_NoAnnotation_WireShapeUnchanged pins the pre-field wire shape byte
// for byte: the line below was marshaled from this record before the annotation
// existed.
func TestRecord_NoAnnotation_WireShapeUnchanged(t *testing.T) {
	r := sampleRecord("2026-06-14T10:00:00Z-abc123")
	r.Model = "m"
	r.ModelReviewers = []string{"bruce"}
	r.Status = StatusResolved
	r.ResolvedAt = "2026-06-15T00:00:00Z"
	b, err := json.Marshal(r)
	require.NoError(t, err)
	const pre = `{"schema_version":3,"id":"2cbd0d4955f36040","run_id":"2026-06-14T10:00:00Z-abc123","ts":"2026-06-14T10:00:00Z","severity":"HIGH","file":"internal/scorecard/store.go","line":89,"problem":"(Append) Concurrent writers may tear JSONL lines","fix":"Issue exactly one os.Write per record under O_APPEND","category":"correctness","est_minutes":30,"evidence":"Scorecard comment notes POSIX atomic-append guarantee","reviewers":["bruce","host"],"confidence":"HIGH","model":"m","model_reviewers":["bruce"],"justification":"One record marshaled and written in a single Write call.","source_report":{"path":"sources/bruce/review.md","line":42,"section":"Concurrency concerns"},"status":"resolved","resolved_at":"2026-06-15T00:00:00Z"}`
	assert.Equal(t, pre, string(b))
	assert.NotContains(t, string(b), "reconciler_feedback")
}

func TestRecord_Annotation_WireShape(t *testing.T) {
	r := foldRec("aaaaaaaaaaaaaaaa", "2026-06-14T10:00:00Z", "")
	r.ReconcilerFeedback = &ReconcilerFeedback{Kind: FeedbackWrongSplit, With: "0123456789abcdef", Reason: "same bug"}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"reconciler_feedback":{"kind":"wrong-split","with":"0123456789abcdef","reason":"same bug"}`)
	assert.Equal(t, SchemaVersion, 3, "the annotation is additive within v3: no schema bump")

	r.ReconcilerFeedback.With = ""
	b, err = json.Marshal(r)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"reconciler_feedback":{"kind":"wrong-split","reason":"same bug"}`, "an absent partner is omitted")
}

// TestReconcilerFeedback_RoundTripsThroughWriteReadAndFold is T1's first
// success criterion.
func TestReconcilerFeedback_RoundTripsThroughWriteReadAndFold(t *testing.T) {
	dir := t.TempDir()
	open := sampleRecord("2026-06-14T10:00:00Z-abc123")
	fb := ReconcilerFeedback{Kind: FeedbackWrongMerge, Reason: "two distinct races in one finding"}
	note := annotated(open, "2026-06-20T00:00:00Z", fb)
	require.NoError(t, Append(dir, open))
	require.NoError(t, Append(dir, note))

	recs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, note, recs[1], "the annotated record reads back exactly as written")

	folded := FoldRecords(recs)
	require.Len(t, folded, 1)
	require.NotNil(t, folded[0].ReconcilerFeedback)
	assert.Equal(t, fb, *folded[0].ReconcilerFeedback)

	_, err = Compact(dir, ReadOpts{})
	require.NoError(t, err)
	recs, err = ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	folded = FoldRecords(recs)
	require.Len(t, folded, 1)
	require.NotNil(t, folded[0].ReconcilerFeedback, "compaction keeps the annotation on the effective record")
	assert.Equal(t, fb, *folded[0].ReconcilerFeedback)
}

// TestReconcilerFeedback_FoldOutcomeUnchanged: appending an annotated copy of a
// finding's effective record leaves everything the fold reports about the
// finding — status, settledness, occurrence count, first sighting — as it was.
// The annotated copy is a marker, not a sighting: counting it would raise a
// regression that never happened.
func TestReconcilerFeedback_FoldOutcomeUnchanged(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaa"
	fb := ReconcilerFeedback{Kind: FeedbackWrongCategory, Reason: "security, not style"}
	cases := map[string][]Record{
		"open":    {foldRec(id, "2026-06-01T00:00:00Z", "")},
		"regress": {foldRec(id, "2026-06-01T00:00:00Z", ""), foldRec(id, "2026-06-02T00:00:00Z", StatusResolved), foldRec(id, "2026-06-03T00:00:00Z", "")},
	}
	for _, st := range []string{StatusResolved, StatusDeferred, StatusWontfix, StatusUnreproducible, StatusAttemptsExhausted} {
		cases[st] = []Record{foldRec(id, "2026-06-01T00:00:00Z", ""), foldRec(id, "2026-06-02T00:00:00Z", st)}
	}
	for name, base := range cases {
		t.Run(name, func(t *testing.T) {
			before := FoldRecords(base)
			require.Len(t, before, 1)
			with := append(append([]Record{}, base...), annotated(before[0], "2026-06-10T00:00:00Z", fb))
			after := FoldRecords(with)
			require.Len(t, after, 1)
			assert.Equal(t, before[0].Status, after[0].Status)
			assert.Equal(t, IsSettledStatus(before[0].Status), IsSettledStatus(after[0].Status))
			assert.Equal(t, IsClosedStatus(before[0].Status), IsClosedStatus(after[0].Status))
			assert.Equal(t, before[0].Occurrences, after[0].Occurrences)
			assert.Equal(t, before[0].FirstSeen, after[0].FirstSeen)
			assert.Equal(t, before[0].CountedThrough, after[0].CountedThrough)
			require.NotNil(t, after[0].ReconcilerFeedback)
		})
	}
}

// TestReconcilerFeedback_ManualFilingAnnotationIsNotASighting: a hand-filed
// item with a status is a sighting (isSighting), but an annotated copy of it is
// not, even though it inherits Origin and has no ResolvedAt.
func TestReconcilerFeedback_ManualFilingAnnotationIsNotASighting(t *testing.T) {
	filed := foldRec("aaaaaaaaaaaaaaaa", "2026-06-01T00:00:00Z", StatusDeferred)
	filed.Origin = OriginManual
	require.True(t, isSighting(filed))
	note := annotated(filed, "2026-06-10T00:00:00Z", ReconcilerFeedback{Kind: FeedbackWrongMerge, Reason: "r"})
	assert.False(t, isSighting(note))
	assert.Equal(t, 1, FoldRecords([]Record{filed, note})[0].Occurrences)
}

// TestReconcilerFeedback_QualitySignalUnchanged is T1's third success
// criterion: AggregateQualitySignal returns the same rows with and without an
// annotation on the same findings — both when the annotation rides the records
// themselves and when an annotated copy is appended to each.
func TestReconcilerFeedback_QualitySignalUnchanged(t *testing.T) {
	mk := func(id, ts, status string) Record {
		r := foldRec(id, ts, status)
		r.Model = "claude-sonnet-4-6"
		r.Reviewers = []string{"bruce", "host"}
		r.ModelReviewers = []string{"bruce"}
		return r
	}
	base := []Record{
		mk("a000000000000001", "2026-06-01T00:00:00Z", ""),
		mk("a000000000000001", "2026-06-02T00:00:00Z", StatusWontfix),
		mk("a000000000000002", "2026-06-01T00:00:00Z", ""),
		mk("a000000000000002", "2026-06-02T00:00:00Z", StatusResolved),
		mk("a000000000000003", "2026-06-01T00:00:00Z", ""),
		mk("a000000000000003", "2026-06-02T00:00:00Z", StatusUnreproducible),
		mk("a000000000000004", "2026-06-01T00:00:00Z", ""),
		mk("a000000000000004", "2026-06-02T00:00:00Z", StatusAttemptsExhausted),
		mk("a000000000000005", "2026-06-01T00:00:00Z", ""),
		mk("a000000000000005", "2026-06-02T00:00:00Z", StatusDeferred),
		mk("a000000000000006", "2026-06-01T00:00:00Z", ""),
	}
	want := AggregateQualitySignal(base)
	require.NotEmpty(t, want)

	fb := ReconcilerFeedback{Kind: FeedbackWrongMerge, Reason: "r"}
	inline := make([]Record, len(base))
	for i, r := range base {
		f := fb
		r.ReconcilerFeedback = &f
		inline[i] = r
	}
	assert.Equal(t, want, AggregateQualitySignal(inline), "annotation on the same records")

	appended := append([]Record{}, base...)
	for _, eff := range FoldRecords(base) {
		appended = append(appended, annotated(eff, "2026-06-10T00:00:00Z", fb))
	}
	assert.Equal(t, want, AggregateQualitySignal(appended), "annotated copy appended per finding")
}

// TestAppend_ReconcilerFeedbackWriteGate: the store boundary refuses a malformed
// annotation the way it refuses an off-enum status, so no writer can persist one.
func TestAppend_ReconcilerFeedbackWriteGate(t *testing.T) {
	base := sampleRecord("2026-06-14T10:00:00Z-abc123")
	const partner = "0123456789abcdef"
	bad := map[string]ReconcilerFeedback{
		"empty kind":                {Reason: "r"},
		"unknown kind":              {Kind: "split", Reason: "r"},
		"non-canonical kind":        {Kind: "Wrong-Merge", Reason: "r"},
		"empty reason":              {Kind: FeedbackWrongMerge},
		"blank reason":              {Kind: FeedbackWrongMerge, Reason: "  \t"},
		"partner on wrong-merge":    {Kind: FeedbackWrongMerge, With: partner, Reason: "r"},
		"partner on wrong-category": {Kind: FeedbackWrongCategory, With: partner, Reason: "r"},
		"short partner":             {Kind: FeedbackWrongSplit, With: "0123abcd", Reason: "r"},
		"long partner":              {Kind: FeedbackWrongSplit, With: partner + "00", Reason: "r"},
		"uppercase partner":         {Kind: FeedbackWrongSplit, With: strings.ToUpper(partner), Reason: "r"},
		"non-hex partner":           {Kind: FeedbackWrongSplit, With: "0123456789abcdeg", Reason: "r"},
		"padded partner":            {Kind: FeedbackWrongSplit, With: " " + partner, Reason: "r"},
		"self partner":              {Kind: FeedbackWrongSplit, With: base.ID, Reason: "r"},
	}
	for name, fb := range bad {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			r := base
			f := fb
			r.ReconcilerFeedback = &f
			err := Append(dir, r)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "reconciler feedback")
			recs, rerr := ReadAll(dir, ReadOpts{})
			require.NoError(t, rerr)
			assert.Empty(t, recs, "a refused record never reaches disk")
		})
	}

	good := []ReconcilerFeedback{
		{Kind: FeedbackWrongMerge, Reason: "r"},
		{Kind: FeedbackWrongSplit, Reason: "r"},
		{Kind: FeedbackWrongSplit, With: partner, Reason: "r"},
		{Kind: FeedbackWrongCategory, Reason: "r"},
	}
	for _, fb := range good {
		dir := t.TempDir()
		r := base
		f := fb
		r.ReconcilerFeedback = &f
		require.NoError(t, Append(dir, r), "%+v", fb)
	}
}

func TestIsWellFormedFindingID(t *testing.T) {
	r := sampleRecord("2026-06-14T10:00:00Z-abc123")
	assert.True(t, IsWellFormedFindingID(r.ID), "a StampID id is well formed")
	for _, id := range []string{"", "0123", "0123456789ABCDEF", "0123456789abcdeg", "0123456789abcdef0"} {
		assert.False(t, IsWellFormedFindingID(id), id)
	}
}

// TestSummary_ProjectsReconcilerFeedbackKind: `atcr debt dashboard` aggregates
// from summaries, never full records (epic 35.21), so the summary pass must carry
// the annotation's kind for the dashboard to count reconciler corrections
// (Epic 36.1 T4). The fold keeps it on the effective summary exactly as
// FoldRecords keeps the annotation on the effective record; an unannotated
// record projects an empty kind.
func TestSummary_ProjectsReconcilerFeedbackKind(t *testing.T) {
	dir := t.TempDir()
	open := sampleRecord("2026-06-14T10:00:00Z-abc123")
	other := sampleRecord("2026-06-14T10:00:00Z-def456")
	other.Problem = "an unannotated finding"
	other.StampID()
	note := annotated(open, "2026-06-20T00:00:00Z", ReconcilerFeedback{Kind: FeedbackWrongMerge, Reason: "two races"})
	require.NoError(t, Append(dir, open))
	require.NoError(t, Append(dir, other))
	require.NoError(t, Append(dir, note))

	sums, err := ReadSummaries(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, sums, 3)
	assert.Empty(t, sums[0].ReconcilerFeedbackKind, "an unannotated record projects no kind")
	assert.Equal(t, FeedbackWrongMerge, sums[2].ReconcilerFeedbackKind)

	byID := map[string]Summary{}
	for _, s := range FoldSummaries(sums) {
		byID[s.ID] = s
	}
	require.Len(t, byID, 2)
	assert.Equal(t, FeedbackWrongMerge, byID[open.ID].ReconcilerFeedbackKind, "the fold keeps the annotation")
	assert.Equal(t, open.Status, byID[open.ID].Status, "and leaves the status as it was")
	assert.Empty(t, byID[other.ID].ReconcilerFeedbackKind)
}

// TestDecodeSummary_ReconcilerFeedbackTypeParity: reconciler_feedback is an
// object on Record, so a line carrying it as another JSON type is skipped by
// both decoders, never accepted by the summary path alone (see summaryLine).
func TestDecodeSummary_ReconcilerFeedbackTypeParity(t *testing.T) {
	line := []byte(`{"schema_version":3,"id":"aaaaaaaaaaaaaaaa","run_id":"r","reconciler_feedback":"wrong-merge"}`)
	_, okSum := decodeSummary(line, "s", io.Discard)
	_, recOutcome := decodeRecord(line, "s", io.Discard)
	assert.False(t, okSum)
	assert.Equal(t, recOutcome == decodeOK, okSum)
}
