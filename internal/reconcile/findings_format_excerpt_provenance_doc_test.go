package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Two published claims in docs/findings-format.md's parsing contract had drifted
// from the code, both in the direction that misleads an operator debugging a
// "missing" excerpt:
//
//   - "`review.md` always holds the raw reply, block included" was unqualified,
//     but a line exactly equal to the engine's chunk-boundary delimiter is
//     neutralised before the write (fanout prefers an annotated copy, because a
//     model-forged delimiter would reshape how reconcile splits the merged file).
//   - "Omitted when no `review.md` section references the finding's FILE:LINE"
//     enumerated only one omission cause, while a salvaged unchunked source is
//     refused whole by the producer and can therefore never supply an excerpt.
//
// Both are CODE-ANCHORED: each doc claim is asserted beside the implementation
// that makes it true, so reverting the behaviour or the sentence fails here. The
// convention for doc-vs-code drift tests is internal/reconcile/ (no Go package
// lives under docs/, and the published reconcile/ module must not assume this
// repo's layout) — see justification_record_boundary_test.go.
func TestFindingsFormatDoc_ExcerptProvenanceClaimsMatchTheCode(t *testing.T) {
	doc := readRepoFile(t, "../../docs/findings-format.md")
	artifacts := readRepoFile(t, "../../internal/fanout/artifacts.go")
	justification := readRepoFile(t, "../../internal/reconcile/justification.go")

	t.Run("the chunk-boundary neutralisation exception is stated", func(t *testing.T) {
		line := docLineContaining(t, doc, ffParseContractMarker)
		assert.Contains(t, line, "a line exactly equal to the engine's chunk-boundary delimiter is neutralised",
			"`review.md` holds the raw reply EXCEPT for the neutralised delimiter line, and the doc must say so")

		// Code anchor: the unchunked write path that performs the neutralisation the
		// doc names. If the neutralisation ever moved, the sentence above would
		// describe a guarantee nothing enforces.
		assert.Contains(t, artifacts, "content = neutraliseChunkBoundary(content)",
			"the doc's neutralisation exception is only true while a writer still neutralises the delimiter")
	})

	t.Run("the anchor half of the raw-vs-stripped disagreement is stated", func(t *testing.T) {
		line := docLineContaining(t, doc, "- `justification` — the narrative section extracted")
		assert.Contains(t, line, "durable one",
			"the doc disclosed only the elision half, which is excerpt quality; the anchor half is what persists")
		assert.Contains(t, line, "buildAnchorIndex",
			"and it must name the mechanism that actually closes it, not just describe the risk")

		// Code anchor: the exclusion the disclosure depends on. If the draft-line
		// skip leaves buildAnchorIndex, the doc claim becomes a promise nothing keeps.
		justification := readRepoFile(t, "../../internal/reconcile/justification.go")
		assert.Contains(t, justification, "if _, draft := narratives[ni].draftLines[li]; draft {",
			"the anchor-half disclosure is only true while buildAnchorIndex still skips refused-run lines")
	})

	t.Run("the salvaged-source omission cause is stated", func(t *testing.T) {
		line := docLineContaining(t, doc, "- `justification` — the narrative section extracted")
		assert.Contains(t, line, "omitted when the source's `status.json` says the reply was salvaged",
			"a salvaged unchunked source can never yield an excerpt, so it is a second omission cause the doc must name")

		// Code anchor: the producer's refusal, which is what makes the omission
		// cause structural rather than incidental.
		assert.Contains(t, justification, "salvaged && len(salvagedBins) == 0",
			"the doc's second omission cause is the producer's whole-source salvage refusal; deleting that arm would make the sentence false")
	})
}
