package reconcile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// truncatedScoringDocs are the two published documents that describe how a
// truncated verdict reaches the reviewer's durable score. They are separate
// files with separate audiences (the artifact schema and the verification
// stage), and they drifted apart from the code in the same way once already:
// both said `truncated` drove the precision exclusion while the emitter keyed on
// `trippedBudgets` in a different file with an independent lifecycle, and both
// promised the verdict was "excluded entirely" in a case where the rate was in
// fact published as 0.0.
//
// Live in internal/reconcile per CLAUDE.md: docs/ hosts no Go package, and the
// published reconcile/ module must not assume this repository's layout.
var truncatedScoringDocs = []string{
	"findings-format.md",
	"verification.md",
}

// TestTruncatedScoringDocs_DescribeTheKeyTheScoreActuallyReads pins the first
// claim in both documents.
//
// internal/scorecard resolves the exclusion from findings.json's
// verification.truncated (settledTruncationByKey), falling back to
// verification.json's trippedBudgets only where findings.json says nothing. A
// document that named the snapshot as the key would send a reader to set a field
// that does not move the score.
func TestTruncatedScoringDocs_DescribeTheKeyTheScoreActuallyReads(t *testing.T) {
	for _, name := range truncatedScoringDocs {
		t.Run(name, func(t *testing.T) {
			doc := readDoc(t, name)

			assert.Contains(t, doc, "survived_skeptic_rate",
				"the document must name the metric the exclusion applies to")
			assert.Contains(t, doc, "trippedBudgets",
				"naming the snapshot field explicitly is what stops a reader assuming it is the key")
			assert.True(t,
				strings.Contains(doc, "not on `trippedBudgets`") || strings.Contains(doc, "not `trippedBudgets`"),
				"the document must say trippedBudgets is NOT the key — internal/scorecard keys on findings.json's truncated field")
		})
	}
}

// TestTruncatedScoringDocs_StateTheAllTruncatedOmission pins the second claim.
//
// "Excluded entirely" is only half the rule. When every verdict is truncated the
// ratio is 0/0, and the emitter omits the key rather than publishing 0.0 — a 0.0
// is indistinguishable from an all-refuted reviewer, which is the strongest
// negative signal the metric carries. A document promising exclusion without
// naming the degenerate case describes behaviour that used to be the opposite.
func TestTruncatedScoringDocs_StateTheAllTruncatedOmission(t *testing.T) {
	for _, name := range truncatedScoringDocs {
		t.Run(name, func(t *testing.T) {
			doc := readDoc(t, name)

			assert.Contains(t, doc, "every verdict in a run is truncated",
				"the degenerate case must be named, not left to be inferred from 'excluded entirely'")
			assert.Contains(t, doc, "omitted rather than published as `0.0`",
				"the document must state what the key does in that case — omission, not a zero")
			assert.Contains(t, doc, "all refuted",
				"the reason the omission matters is that a 0.0 reads as an all-refuted reviewer")
		})
	}
}
