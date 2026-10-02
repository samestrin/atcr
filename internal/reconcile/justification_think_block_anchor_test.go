package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD internal/reconcile/justification.go:322 — a citation the model wrote inside
// its DISCARDED draft must never be the preferred justification source.
//
// buildAnchorIndex scans every line of the raw review.md, and beatsMatch breaks
// equal-tier ties toward the EARLIEST line. A leading <think> block sits at the top
// of the file, so its citation outranked the real prose below it: extractSection then
// published draft text as the finding's justification, with source_report.line
// pointing into the block. That is forged provenance, not a worse excerpt — a
// prompt-injected reviewer reply could route invented prose into a published field
// that downstream TD resolution treats as the reviewer's reasoning.
//
// The defense excludes the leading run's lines from the index rather than stripping
// the narrative: source_report.line stays a pointer into the artifact a reader opens
// (the decision recorded in justification_think_parity_test.go), so nothing has to be
// rebased.
func TestStampJustifications_LeadingThinkBlockCitationNeverWins(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "<think>\n"+ // line 1
		"Draft: **`internal/auth/token.go:42`** — the draft reasoning I abandoned.\n"+ // line 2
		"</think>\n"+ // line 3
		"# Host review\n"+ // line 4
		"\n"+ // line 5
		"## Findings\n"+ // line 6
		"1. **`internal/auth/token.go:42` — JWT signature not verified.** The real narrative.\n") // line 7

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"host"}}}
	stampJustifications(jf, reviewDir)

	require.NotNil(t, jf[0].SourceReport, "the real prose below the block must still match")
	assert.Equal(t, 7, jf[0].SourceReport.Line,
		"the citation inside the discarded draft must not outrank the real prose")
	assert.Equal(t, "Findings", jf[0].SourceReport.Section,
		"a draft-anchored match reports the wrong section too")
	assert.Contains(t, jf[0].Justification, "The real narrative")
	assert.NotContains(t, jf[0].Justification, "abandoned",
		"abandoned draft reasoning must never be published as the reviewer's justification")
}

// The companion: a review.md that is ENTIRELY a leading think block has no committed
// prose at all, so it must contribute no justification rather than publishing its
// draft.
func TestStampJustifications_ThinkOnlyReviewContributesNoJustification(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", "<think>\n"+
		"**`internal/auth/token.go:42`** — draft only, never committed.\n"+
		"</think>\n")

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"host"}}}
	stampJustifications(jf, reviewDir)

	assert.Empty(t, jf[0].Justification, "a reply that is only a draft has nothing to cite")
	assert.Nil(t, jf[0].SourceReport)
}

// TestStampJustifications_LeadingThinkBlockInALaterChunkIsAlsoExcluded is the
// chunked case the exclusion above could not see.
//
// review.md is the marker-JOINED concatenation of a chunked persona's bins, but
// the findings parser calls SplitThink PER CHUNK (fanout's Result.parseFindings
// over chunkContents). leadingDraftLines ran SplitThink ONCE on the whole joined
// file, so when chunk 1 opens with ordinary prose it returns 0 — and chunk 2's
// leading think block, which the parser DID refuse, is indexed as an ordinary
// candidate anchor. beatsMatch breaks equal-tier ties toward the earliest line,
// and a draft citation in chunk 2 still outranks real prose further down.
//
// The rest of this file already scopes its scans per segment (chunkSegmentBounds,
// mirroring the per-chunk parse); this scan is the one that did not.
func TestStampJustifications_LeadingThinkBlockInALaterChunkIsAlsoExcluded(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", ""+
		"# Host review\n"+ // 1  chunk 1: ordinary prose, no leading run
		"\n"+ // 2
		"Nothing to report in this chunk.\n"+ // 3
		chunkBoundaryLine+"\n"+ // 4  engine framing
		"<think>\n"+ // 5  chunk 2 OPENS with a run the parser refused
		"Draft: **`internal/auth/token.go:42`** — the draft reasoning I abandoned.\n"+ // 6
		"</think>\n"+ // 7
		"## Findings\n"+ // 8
		"1. **`internal/auth/token.go:42` — JWT signature not verified.** The real narrative.\n") // 9

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"host"}}}
	stampJustifications(jf, reviewDir)

	require.NotNil(t, jf[0].SourceReport, "the real prose in chunk 2 must still match")
	assert.Equal(t, 9, jf[0].SourceReport.Line,
		"a draft citation in a LATER chunk's leading run must not outrank the real prose")
	assert.Contains(t, jf[0].Justification, "The real narrative")
	assert.NotContains(t, jf[0].Justification, "abandoned",
		"the parser refused chunk 2's leading run, so the anchor index must refuse it too")
}

// And the first chunk's own run must still be excluded when a LATER chunk has
// none — the per-segment walk must not stop after the first segment it clears.
func TestStampJustifications_LeadingThinkBlockInTheFirstOfTwoChunksIsExcluded(t *testing.T) {
	reviewDir := t.TempDir()
	writeReview(t, reviewDir, "host", ""+
		"<think>\n"+ // 1  chunk 1 opens with a refused run
		"Draft: **`internal/auth/token.go:42`** — abandoned.\n"+ // 2
		"</think>\n"+ // 3
		"Nothing to report in this chunk.\n"+ // 4
		chunkBoundaryLine+"\n"+ // 5
		"## Findings\n"+ // 6  chunk 2: ordinary prose
		"1. **`internal/auth/token.go:42` — JWT signature not verified.** The real narrative.\n") // 7

	jf := []JSONFinding{{File: "internal/auth/token.go", Line: 42, Reviewers: []string{"host"}}}
	stampJustifications(jf, reviewDir)

	require.NotNil(t, jf[0].SourceReport)
	assert.Equal(t, 7, jf[0].SourceReport.Line)
	assert.NotContains(t, jf[0].Justification, "abandoned")
}
