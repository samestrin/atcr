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

	assert.Equal(t, map[int]struct{}{0: {}}, salvagedSegmentLines(raw, []int{0}))
	assert.Equal(t, map[int]struct{}{4: {}}, salvagedSegmentLines(raw, []int{2}))
	assert.Equal(t, map[int]struct{}{0: {}, 4: {}}, salvagedSegmentLines(raw, []int{0, 2}))
	assert.Empty(t, salvagedSegmentLines(raw, nil),
		"no named bin excludes nothing — the fail-open direction")
	assert.Empty(t, salvagedSegmentLines(raw, []int{9}),
		"an out-of-range bin index names no segment and must not panic")
}

// TestSalvagedSegmentLines_MultiLineSegmentExcludesEveryLine guards the one
// thing a per-segment exclusion must get right: a refused bin is excluded in
// full, not just at its first line.
func TestSalvagedSegmentLines_MultiLineSegmentExcludesEveryLine(t *testing.T) {
	t.Parallel()
	raw := chunkedReview("a\nb\nc", "d\ne")

	assert.Equal(t, map[int]struct{}{0: {}, 1: {}, 2: {}}, salvagedSegmentLines(raw, []int{0}))
	assert.Equal(t, map[int]struct{}{4: {}, 5: {}}, salvagedSegmentLines(raw, []int{1}))
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
	assert.Empty(t, excludedAnchorLines(raw, nil),
		"no refusal on record excludes no line")
	// And honours it when the bit really was set.
	assert.Equal(t, map[int]struct{}{0: {}}, excludedAnchorLines(raw, []int{0}),
		"a real refusal still excludes its bin's segment")
}
