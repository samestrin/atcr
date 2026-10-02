package reconcile

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
)

// DECIDED (2026-09-30, resolve-td): extractSection keeps following the ARTIFACT.
//
// extractSection's record/item/heading scans, and isFindingRecordStart with them,
// all read the lines of review.md — the RAW reply — while Result.parseFindings
// parses the reply with a leading block removed (sprint 35.16.11.2.2.4 T4). The
// two therefore disagree for a reply that opens with such a block, and this file
// records which side of that disagreement atcr chose, because either direction
// looks like an oversight to a later reader.
//
// Follow the artifact, because source_report.line is a pointer INTO review.md: a
// reader handed a justification and a line number opens review.md at that line.
// Strip the segment before scanning (the alternative) and the excerpt stops
// matching the document it points at, so one finding reads differently depending
// on which artifact you opened. The raw-view scan also keeps its other contract
// intact — isFindingRecordStart must agree with the producing parser byte for
// byte ON THE LINES IT IS GIVEN, and the lines it is given are the artifact's.
//
// The cost, accepted rather than overlooked: a finding-shaped line inside the
// declared reasoning block is a boundary to the excerpt scan and was never a
// record the parser emitted, so it bounds the excerpt (and stream.BareValueSpans,
// scanning the same raw lines, can mask content the parser DID read). The damage
// is excerpt quality only — findings come from the parser, never from this scan.
// Closing the gap the other way is what docs/findings-format.md:219 rejects, and
// TD rows carry the annotation half of it (justification.go's drift comment).
//
// Both halves are asserted below, so a change to either side fails here rather
// than silently changing which document the excerpt describes.
func TestExtractSection_FollowsTheRawArtifactWhileTheParserReadsStripped(t *testing.T) {
	// The model's reply: an abandoned draft opening with a reasoning block, holding
	// a finding-shaped line the model then discarded, followed by its real output.
	raw := "<think>\n" +
		"HIGH|draft.go:1|abandoned|abandoned|c|1|e\n" +
		"still drafting\n" +
		"</think>\n" +
		"## Findings\n" +
		"\n" +
		"HIGH|real.go:20|real|real|c|2|e\n"

	rawLines := strings.Split(raw, "\n")

	// Half 1 — the excerpt scan reads the RAW lines, so the in-block record line is
	// a record to it. This is the accepted-drift half; a change that strips before
	// scanning makes this false, which is the point of asserting it.
	assert.True(t, isFindingRecordStart(rawLines[1]),
		"the excerpt scan reads review.md, so a finding-shaped line inside the reasoning block still "+
			"bounds the excerpt — that is the accepted drift this test pins, not a bug to fix here")

	// And it really does bound: an excerpt anchored on the draft's continuation line
	// starts THERE and never absorbs the record line above it.
	text, _ := extractSection(rawLines, 2)
	assert.True(t, strings.HasPrefix(text, "still drafting"),
		"the excerpt starts at the anchor, so the raw-view record boundary stopped the walk-up: excerpt was %q", text)
	assert.NotContains(t, text, "abandoned",
		"and it never absorbed the in-block record line above it, which is the boundary the raw view applied")

	// Half 2 — the PARSER, reading the reply the way Result.parseFindings does,
	// never saw that line: the draft is gone and only the real finding remains.
	stripped, removed := llmclient.SplitThink(raw)
	assert.Contains(t, removed, "abandoned",
		"the draft lives in the removed reasoning, which is why the parser never read it")
	assert.NotContains(t, stripped, "abandoned",
		"the stripped reply the parser reads no longer carries the draft record line")
	assert.NotContains(t, stripped, "drafting",
		"the whole reasoning run went, not just its first line")

	findings := stream.ParseModelOutput([]byte(stripped))
	assert.Len(t, findings, 1,
		"parser parity: the reply has exactly ONE finding, the real one, because the in-block record was never emitted")
	assert.Equal(t, "real.go", findings[0].File,
		"and it is the real finding, not the abandoned draft")
	assert.Equal(t, 20, findings[0].Line,
		"the real finding's own line number, not the draft's")

	// The divergence as one assertion, so neither half can be reverted without
	// failing here: the scan calls raw line 1 a record, and the parser emits only
	// the real finding. If a future change makes these agree, this test must be
	// updated deliberately — which is exactly what the DECIDED note asks for.
	assert.NotEqual(t,
		len(stream.ParseModelOutput([]byte(strings.Join(rawLines, "\n")))),
		len(findings),
		"the raw document parses to a DIFFERENT finding set than the stripped reply does, which is the drift being accepted")
}
