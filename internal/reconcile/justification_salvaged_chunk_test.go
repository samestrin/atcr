package reconcile

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The persona-wide `salvaged` bit is an OR-fold over a chunked persona's bins,
// which internal/fanout/status.go states outright: "it can be true beside a
// non-zero findings count: one bin was refused and its siblings were kept.
// SalvagedChunks below is what tells the two apart."
//
// internal/fanout/engine.go's parseFindings honours that per bin, and
// docs/findings-format.md publishes it ("a salvaged chunk contributes nothing,
// while its sibling chunks' findings are kept"). So the clean bins' findings are
// parsed, reconciled and shipped — and withholding the WHOLE narrative on the
// persona-wide bit strips justification and source_report off exactly those real
// findings. localdebt seeds seen[id] for every open id, so no later reconcile can
// append a corrected record: the provenance is lost permanently.
//
// These tests pin the three cases the pair can express.

// writeSalvagedChunkStatus marks a source leaf salvaged and names WHICH bins
// were refused, the way internal/fanout's salvagedChunkIndices does.
func writeSalvagedChunkStatus(t *testing.T, reviewDir, leaf string, bins []int) {
	t.Helper()
	dir := filepath.Join(reviewDir, "sources", leaf)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	parts := make([]string, 0, len(bins))
	for _, b := range bins {
		parts = append(parts, strconv.Itoa(b))
	}
	body := `{"agent":"` + leaf + `","status":"ok","salvaged":true,"salvaged_chunks":[` +
		strings.Join(parts, ",") + `]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status.json"), []byte(body), 0o644))
}

// chunkedReview joins segments with the engine's own delimiter, the way
// fanout.joinChunkContents does, so the segment indices the test writes into
// salvaged_chunks line up with the bins on the wire.
func chunkedReview(segments ...string) string {
	return strings.Join(segments, "\n"+chunkBoundaryLine+"\n")
}

// TestStampJustifications_SalvagedBinDoesNotWithholdItsCleanSibling is the
// defect. Bin 0 is promoted chain-of-thought citing the exact FILE:LINE; bin 1
// is a real reviewer's prose about the same finding. Before the fix the whole
// file was skipped and the finding shipped with no provenance at all.
func TestStampJustifications_SalvagedBinDoesNotWithholdItsCleanSibling(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "dax", chunkedReview(
		"Maybe **`internal/auth/token.go:42`** is the spot. Still weighing it.",
		"## Findings\n\nThe signature check at `internal/auth/token.go:42` accepts an unsigned token.",
	))
	writeSalvagedChunkStatus(t, reviewDir, "dax", []int{0})

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"dax"}}}
	stampJustifications(jf, reviewDir)

	require.NotEmpty(t, jf[0].Justification,
		"the clean sibling bin is a real narrative and must still supply provenance")
	assert.Contains(t, jf[0].Justification, "accepts an unsigned token",
		"the excerpt must come from the clean bin")
	assert.NotContains(t, jf[0].Justification, "Still weighing it",
		"the refused bin's reasoning must never become published provenance")
}

// TestStampJustifications_EveryBinSalvagedContributesNothing is the complement:
// when the pair names every bin there is no clean sibling, so the narrative is
// withheld whole — the same outcome as the unchunked case.
func TestStampJustifications_EveryBinSalvagedContributesNothing(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "dax", chunkedReview(
		"Perhaps **`internal/auth/token.go:42`** is wrong.",
		"Or maybe `internal/auth/token.go:42` is fine after all.",
	))
	writeSalvagedChunkStatus(t, reviewDir, "dax", []int{0, 1})

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"dax"}}}
	stampJustifications(jf, reviewDir)

	assert.Empty(t, jf[0].Justification, "every bin refused means no narrative at all")
	assert.Nil(t, jf[0].SourceReport, "and no source_report back-reference into it")
}

// TestStampJustifications_SalvagedWithoutChunkIndicesWithholdsWholeFile pins the
// branch that must NOT narrow: an unchunked persona's status.json carries the
// bit with no salvaged_chunks, so there is no bin index to exclude by and the
// whole reply is promoted reasoning. Narrowing here on an absent list would
// index the entire refused file — the original defect, inverted.
func TestStampJustifications_SalvagedWithoutChunkIndicesWithholdsWholeFile(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", ""+
		"Let me work through this.\n"+
		"I think **`internal/auth/token.go:42`** is where the check belongs.\n")
	writeSalvagedStatus(t, reviewDir, "host")

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"host"}}}
	stampJustifications(jf, reviewDir)

	assert.Empty(t, jf[0].Justification,
		"no bin index means the persona-wide refusal stands for the whole file")
}

// TestSalvagedSegmentLines pins the segment arithmetic directly, including the
// bin-to-segment correspondence the whole fix rests on: joinChunkContents emits
// one segment per content-bearing bin in order, and salvagedChunkIndices indexes
// that same filtered list.
func TestSalvagedSegmentLines(t *testing.T) {
	t.Parallel()
	// Segment 0 = line 0, delimiter = line 1, segment 1 = line 2, etc.
	raw := chunkedReview("zero", "one", "two")

	assertSegLines := func(bins []int, want map[int]struct{}) {
		t.Helper()
		got, desynced := salvagedSegmentLines(strings.Split(raw, "\n"), bins)
		assert.False(t, desynced, "every named bin is accountable in a three-segment reply")
		assert.Equal(t, want, got)
	}
	assertSegLines([]int{0}, map[int]struct{}{0: {}})
	assertSegLines([]int{2}, map[int]struct{}{4: {}})
	assertSegLines([]int{0, 2}, map[int]struct{}{0: {}, 4: {}})

	nilLines, nilDesynced := salvagedSegmentLines(strings.Split(raw, "\n"), nil)
	assert.Empty(t, nilLines, "no named bin excludes nothing — the unchunked persona")
	assert.False(t, nilDesynced, "and names no desync")

	// An out-of-range index no longer excludes nothing silently: it is a desynced
	// pair, and the caller withholds the whole file on it
	// (TD internal/reconcile/justification.go:303).
	oorLines, oorDesynced := salvagedSegmentLines(strings.Split(raw, "\n"), []int{9})
	assert.Empty(t, oorLines, "an unaccountable index still names no segment and must not panic")
	assert.True(t, oorDesynced, "but it must be REPORTED, not treated as a clean reply")
}

// TestSalvagedSegmentLines_MultiLineSegmentExcludesEveryLine guards the one
// thing a per-segment exclusion must get right: a refused bin is excluded in
// full, not just at its first line.
func TestSalvagedSegmentLines_MultiLineSegmentExcludesEveryLine(t *testing.T) {
	t.Parallel()
	raw := chunkedReview("a\nb\nc", "d\ne")

	zero, zeroDesynced := salvagedSegmentLines(strings.Split(raw, "\n"), []int{0})
	assert.False(t, zeroDesynced)
	assert.Equal(t, map[int]struct{}{0: {}, 1: {}, 2: {}}, zero)

	one, oneDesynced := salvagedSegmentLines(strings.Split(raw, "\n"), []int{1})
	assert.False(t, oneDesynced)
	assert.Equal(t, map[int]struct{}{4: {}, 5: {}}, one)
}

// TestSourceSalvage_ReadsBothFields pins the decode. The bit alone cannot
// distinguish a one-bin refusal from a whole-persona one, which is the ambiguity
// that produced the defect.
func TestSourceSalvage_ReadsBothFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(body string) string {
		sub := filepath.Join(dir, strconv.Itoa(len(body)))
		require.NoError(t, os.MkdirAll(sub, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(sub, statusFileName), []byte(body), 0o644))
		return filepath.Join(sub, reviewFileName)
	}

	salvaged, chunks := sourceSalvage(write(`{"salvaged":true,"salvaged_chunks":[1,3]}`))
	assert.True(t, salvaged)
	assert.Equal(t, []int{1, 3}, chunks)

	salvaged, chunks = sourceSalvage(write(`{"salvaged":true}`))
	assert.True(t, salvaged)
	assert.Empty(t, chunks, "an unchunked persona names no bin")

	salvaged, chunks = sourceSalvage(write(`{"status":"ok"}`))
	assert.False(t, salvaged)
	assert.Empty(t, chunks)

	// FAIL-OPEN on an unreadable or malformed status.json, unchanged: withholding
	// a real reviewer's justification on a read error would trade a rare forged
	// excerpt for a common missing one.
	salvaged, chunks = sourceSalvage(write(`{not json`))
	assert.False(t, salvaged)
	assert.Empty(t, chunks)

	salvaged, chunks = sourceSalvage(filepath.Join(dir, "absent", reviewFileName))
	assert.False(t, salvaged)
	assert.Empty(t, chunks)
}

// writeChunkStatusWithoutSalvageBit names bins but does NOT set the bit — the
// shape a hand-edited status.json produces, and the one the `!salvaged` guard
// exists for. internal/fanout never writes it: salvagedChunkIndices derives the
// list from chunkSalvaged, so a non-empty list always arrives with the OR-fold
// set. That is exactly why no test reached the guard.
func writeChunkStatusWithoutSalvageBit(t *testing.T, reviewDir, leaf string, bins []int) {
	t.Helper()
	dir := filepath.Join(reviewDir, "sources", leaf)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	parts := make([]string, 0, len(bins))
	for _, b := range bins {
		parts = append(parts, strconv.Itoa(b))
	}
	body := `{"agent":"` + leaf + `","status":"ok","salvaged_chunks":[` +
		strings.Join(parts, ",") + `]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status.json"), []byte(body), 0o644))
}

// TestStampJustifications_ChunkIndicesWithoutTheBitDoNotWithhold pins the
// `if !salvaged { salvagedBins = nil }` guard through stampJustifications.
//
// A bin list without the bit is not a refusal record. Honouring it would let a
// hand-edited status.json strip justification and source_report off a HEALTHY
// reviewer's findings — the same permanent provenance loss as the defect this
// file's first test covers, in the opposite direction, and unrecoverable for the
// same reason: localdebt seeds seen[id] for every open id.
//
// The finding's only anchor sits in segment 0, which is the bin the list names,
// so the assertion fails the moment the guard stops clearing the list. Neither
// sibling test reaches it — both write the bit — which is why neutralising the
// guard left the whole repo green (TD internal/reconcile/justification.go:213).
func TestStampJustifications_ChunkIndicesWithoutTheBitDoNotWithhold(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "dax", chunkedReview(
		"## Findings\n\nThe signature check at `internal/auth/token.go:42` accepts an unsigned token.",
		"## Findings\n\nUnrelated: `internal/log/sink.go:9` drops the writer.",
	))
	writeChunkStatusWithoutSalvageBit(t, reviewDir, "dax", []int{0})

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"dax"}}}
	stampJustifications(jf, reviewDir)

	require.NotEmpty(t, jf[0].Justification,
		"a bin list with no salvage bit is not a refusal; the narrative must still supply provenance")
	assert.Contains(t, jf[0].Justification, "accepts an unsigned token",
		"the excerpt must come from the bin the unbacked list named")
	require.NotNil(t, jf[0].SourceReport,
		"a healthy narrative must keep its source_report back-reference")
}

// TestExcludedAnchorLines_IgnoresBinsWhenNothingWasRefused pins the same guard
// one level down, at the function that consumes the cleared list. Paired with
// the case where the bit IS set, so the test states the discrimination rather
// than just one side of it.
func TestExcludedAnchorLines_IgnoresBinsWhenNothingWasRefused(t *testing.T) {
	t.Parallel()
	raw := chunkedReview("zero", "one")

	// collectReviewNarratives passes nil once the guard has cleared the list.
	none, noneDesynced := excludedAnchorLines(raw, nil)
	assert.Empty(t, none, "no refusal on record excludes no line")
	assert.False(t, noneDesynced, "and is not a desynced pair")

	// And honours it when the bit really was set.
	bin0, bin0Desynced := excludedAnchorLines(raw, []int{0})
	assert.Equal(t, map[int]struct{}{0: {}}, bin0,
		"a real refusal still excludes its bin's segment")
	assert.False(t, bin0Desynced)

	// An unaccountable index propagates the desync rather than excluding nothing.
	oor, oorDesynced := excludedAnchorLines(raw, []int{5})
	assert.True(t, oorDesynced, "the caller must learn it cannot narrow by this list")
	assert.Nil(t, oor, "and must not be handed a partial exclusion set to index by")
}

// TestStampJustifications_BinIndexNamingNoSegmentWithholdsWholeFile is the
// desync case, and the one input shape on which the per-bin narrowing was
// strictly WORSE than the whole-file skip it replaced.
//
// The bit is set and the list names bin 1, but the review.md has a single
// segment — so there is no bin 1 to exclude, the forward walk excluded nothing,
// and every line of a promoted chain-of-thought became a candidate anchor.
// matchNarrative ranks by tier before reviewer, so that reasoning line outranks a
// real reviewer's prose and is published as the finding's provenance into
// localdebt's append-only store, where no later reconcile can replace it.
//
// A list the content cannot account for is evidence the pair is desynced, not
// evidence that nothing was refused — fanout never writes one (salvagedChunkIndices
// returns nil on misalignment, and joinChunkContents emits exactly one segment per
// bin). Fail CLOSED here, mirroring parseFindings' own misalignment arm at
// internal/fanout/engine.go:612 (TD internal/reconcile/justification.go:303).
func TestStampJustifications_BinIndexNamingNoSegmentWithholdsWholeFile(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "dax", ""+
		"Let me think about this.\n"+
		"Maybe **`internal/auth/token.go:42`** is the spot. Still weighing it.\n")
	writeSalvagedChunkStatus(t, reviewDir, "dax", []int{1})

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"dax"}}}
	stampJustifications(jf, reviewDir)

	assert.Empty(t, jf[0].Justification,
		"a bin index the content cannot account for means the pair is desynced; withhold rather than publish reasoning")
	assert.Nil(t, jf[0].SourceReport,
		"and emit no source_report back-reference into a reply that may be promoted reasoning")
}

// TestSalvagedSegmentLines_ReportsDesync pins the signal the caller withholds
// on, separately from the lines it excludes, so a future caller cannot read the
// empty map as "nothing was refused".
func TestSalvagedSegmentLines_ReportsDesync(t *testing.T) {
	t.Parallel()
	raw := chunkedReview("zero", "one")

	lines, desynced := salvagedSegmentLines(strings.Split(raw, "\n"), []int{1})
	assert.False(t, desynced, "bin 1 of a two-segment reply is accountable")
	assert.Equal(t, map[int]struct{}{2: {}}, lines)

	_, desynced = salvagedSegmentLines(strings.Split(raw, "\n"), []int{2})
	assert.True(t, desynced, "one past the last segment is a desynced pair")

	_, desynced = salvagedSegmentLines(strings.Split(raw, "\n"), []int{9})
	assert.True(t, desynced, "far out of range is the same desync, not a no-op")

	_, desynced = salvagedSegmentLines(strings.Split(raw, "\n"), []int{-1})
	assert.True(t, desynced, "a negative index names no segment either")

	_, desynced = salvagedSegmentLines(strings.Split(raw, "\n"), []int{0, 7})
	assert.True(t, desynced, "ONE unaccountable index in an otherwise valid list is still a desync")

	lines, desynced = salvagedSegmentLines(strings.Split(raw, "\n"), nil)
	assert.False(t, desynced, "no named bin is not a desync — it is the unchunked persona")
	assert.Empty(t, lines)

	// A single-segment review.md is the shape that produced the defect: bin 0 is
	// accountable, anything above it is not.
	single := "only one segment here"
	_, desynced = salvagedSegmentLines(strings.Split(single, "\n"), []int{0})
	assert.False(t, desynced)
	_, desynced = salvagedSegmentLines(strings.Split(single, "\n"), []int{1})
	assert.True(t, desynced, "the proven defect input must now report desync")
}

// TD internal/reconcile/justification.go:441: excludedAnchorLines split the same
// raw TWICE whenever salvagedBins was non-empty — once inside salvagedSegmentLines
// (justification.go:401) and again inside draftLineSet (justification.go:503) —
// and the fast-path label claimed the duplication was removed when it only hid it
// on the bins==nil branch. The hoist is strictly non-worse AND removes the double
// Split on the real salvaged-chunk path.
//
// This pins the observable contract that must hold either side of the refactor:
// excludedAnchorLines returns the union of the refused leading run and every line
// of a salvaged bin, for both a nil and a non-empty bin list.
func TestExcludedAnchorLines_UnionIsUnchangedByTheHoist(t *testing.T) {
	t.Parallel()
	raw := chunkedReview("zero", "one", "two")

	nilOut, nilDesynced := excludedAnchorLines(raw, nil)
	assert.False(t, nilDesynced, "no named bin is not a desync")
	// Only draftLineSet contributes on the nil path (salvagedSegmentLines returns
	// early), so the result must equal draftLineSet alone.
	assert.Equal(t, draftLineSet(strings.Split(raw, "\n")), nilOut,
		"a nil bin list excludes exactly the draft leading run")

	out, desynced := excludedAnchorLines(raw, []int{2})
	assert.False(t, desynced)
	// Segment 2 is line 4; the union must include it.
	assert.Contains(t, out, 4,
		"a salvaged bin's every line is excluded, not just its first")
	assert.Equal(t, draftLineSet(strings.Split(raw, "\n")), map[int]struct{}{}, "fixture sanity: no draft run here")
}

// The desync signal must keep short-circuiting, whichever signature the two
// helpers carry.
func TestExcludedAnchorLines_DesyncStillShortCircuits(t *testing.T) {
	t.Parallel()
	out, desynced := excludedAnchorLines(chunkedReview("a"), []int{9})
	assert.True(t, desynced, "an unaccountable bin index is a desynced pair")
	assert.Nil(t, out, "and nothing is excluded on a desync — the caller withholds the whole file")
}

// benchAnchorRaw builds a two-segment review for the excludedAnchorLines
// benchmarks. Both paths must pay exactly ONE strings.Split over it after the
// hoist; before it, the salvaged-bins path paid two.
func benchAnchorRaw() string {
	seg := strings.Repeat("some review prose line\n", 200)
	return seg + chunkBoundaryLine + "\n" + seg
}

// BenchmarkExcludedAnchorLines_NilBins is the unchunked path (bins==nil), where
// salvagedSegmentLines returns early.
func BenchmarkExcludedAnchorLines_NilBins(b *testing.B) {
	raw := benchAnchorRaw()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		excludedAnchorLines(raw, nil)
	}
}

// BenchmarkExcludedAnchorLines_SalvagedBins is the real salvaged-chunk path, the
// one that used to split the same raw twice (TD
// internal/reconcile/justification.go:441).
func BenchmarkExcludedAnchorLines_SalvagedBins(b *testing.B) {
	raw := benchAnchorRaw()
	bins := []int{0}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		excludedAnchorLines(raw, bins)
	}
}

// TestStampJustifications_OnlyAbandonedBinsAreExcluded: a chunked persona whose
// bins 1 and 2 salvaged, bin 1 on a stop reason. Bin 1 is a finished answer and
// stays indexed; only bin 2 (abandoned) is excluded.
func TestStampJustifications_OnlyAbandonedBinsAreExcluded(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "dax", chunkedReview(
		"## Findings\n\nNothing at the other file `internal/other.go:1` worth noting.",
		"## Findings\n\nThe check at `internal/auth/token.go:42` accepts an unsigned token.",
		"Maybe **`internal/auth/session.go:7`** is the spot. Still weighing it.",
	))
	dir := filepath.Join(reviewDir, "sources", "dax")
	require.NoError(t, os.WriteFile(filepath.Join(dir, statusFileName), []byte(
		`{"agent":"dax","status":"ok","salvaged":true,"salvaged_chunks":[1,2],"salvaged_on_stop_chunks":[1]}`), 0o644))

	jf := []JSONFinding{
		{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"dax"}},
		{File: "internal/auth/session.go", Line: 7, Reviewers: []string{"dax"}},
	}
	stampJustifications(jf, reviewDir)

	assert.Contains(t, jf[0].Justification, "accepts an unsigned token",
		"bin 1 salvaged on a stop reason: its prose is a finished answer and stays indexed")
	assert.Empty(t, jf[1].Justification, "bin 2 was abandoned: its lines stay excluded")
	assert.Nil(t, jf[1].SourceReport)
}

// Every salvaged bin on a stop reason leaves nothing abandoned, so nothing is
// withheld — and the empty remainder must not read as "salvaged with no bin
// index", which would withhold the whole file.
func TestStampJustifications_EveryBinOnStopWithholdsNothing(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "dax", chunkedReview(
		"## Findings\n\nThe check at `internal/auth/token.go:42` accepts an unsigned token.",
		"## Findings\n\nNo issue at `internal/other.go:1`.",
	))
	dir := filepath.Join(reviewDir, "sources", "dax")
	require.NoError(t, os.WriteFile(filepath.Join(dir, statusFileName), []byte(
		`{"agent":"dax","status":"ok","salvaged":true,"salvaged_chunks":[0],"salvaged_on_stop_chunks":[0]}`), 0o644))

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"dax"}}}
	stampJustifications(jf, reviewDir)

	assert.Contains(t, jf[0].Justification, "accepts an unsigned token")
}

// TestSourceSalvage_SubtractsStopReasonSalvages pins the decode of the stop-reason
// keys: sourceSalvage reports only ABANDONED salvages.
func TestSourceSalvage_SubtractsStopReasonSalvages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(body string) string {
		sub := filepath.Join(dir, strconv.Itoa(len(body)))
		require.NoError(t, os.MkdirAll(sub, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(sub, statusFileName), []byte(body), 0o644))
		return filepath.Join(sub, reviewFileName)
	}

	salvaged, chunks := sourceSalvage(write(`{"salvaged":true,"salvaged_on_stop":true}`))
	assert.False(t, salvaged, "an unchunked stop-reason salvage is not abandoned")
	assert.Empty(t, chunks)

	salvaged, chunks = sourceSalvage(write(`{"salvaged":true,"salvaged_chunks":[1,2],"salvaged_on_stop_chunks":[1]}`))
	assert.True(t, salvaged)
	assert.Equal(t, []int{2}, chunks, "only the abandoned bin remains")

	salvaged, chunks = sourceSalvage(write(`{"salvaged":true,"salvaged_chunks":[0,3],"salvaged_on_stop_chunks":[3,0]}`))
	assert.False(t, salvaged, "every salvaged bin on a stop reason leaves nothing abandoned")
	assert.Empty(t, chunks)

	salvaged, chunks = sourceSalvage(write(`{"salvaged_on_stop":true}`))
	assert.False(t, salvaged, "the reason key without the bit is not a refusal record")
	assert.Empty(t, chunks)
}
