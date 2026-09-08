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
// internal/scorecard reads verification.json's trippedBudgets, NOT the truncated
// flag on findings.json that report.md renders — the scorecard is emitted during
// reconcile, which rebuilds findings.json from sources/ and strips its
// verification blocks first. A document naming truncated as the key would send a
// reader to set a field that cannot move the score.
func TestTruncatedScoringDocs_DescribeTheKeyTheScoreActuallyReads(t *testing.T) {
	for _, name := range truncatedScoringDocs {
		t.Run(name, func(t *testing.T) {
			doc := readDoc(t, name)

			assert.Contains(t, doc, "survived_skeptic_rate",
				"the document must name the metric the exclusion applies to")
			assert.Contains(t, doc, "trippedBudgets",
				"the score's actual key must be named, not left implied by the truncated field beside it")
			assert.True(t,
				strings.Contains(doc, "the score reads `trippedBudgets`") ||
					strings.Contains(doc, "keys on `trippedBudgets`"),
				"the document must say the SCORE reads trippedBudgets — naming truncated as the key is the drift this guards")
			assert.Contains(t, doc, "debate",
				"a reader told the two artifacts hold the same fact must also be told what keeps them in step")
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

// docSection returns the body of the named markdown heading, up to the next
// heading of any level.
//
// docs/scorecard.md documents `survived_skeptic_rate` in TWO tables — the stored
// record under "### Field reference" and the public submission envelope under the
// `--export` heading — and the two now have deliberately different presence
// rules. docTableRow returns the FIRST matching row, so a check that does not
// name its section pins whichever table happens to come first and silently stops
// guarding the other.
func docSection(t *testing.T, doc, heading string) string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("docs has no heading %q", heading)
	}
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "#") {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

// TestScorecardDoc_StoredRateStatesItsOwnCondition pins the stored record's
// survived_skeptic_rate row.
//
// The row used to say "Conditional, same as above" — i.e. present exactly when
// findings_verified and findings_refuted are. internal/scorecard/scorecard.go
// gates the rate on `v+r > 0` while setting both counts under hasVerification, so
// the three keys are no longer one set: a store consumer that reads them as a set
// finds two of them. The row has to carry its own condition or the reader is told
// something the emitter does not do.
func TestScorecardDoc_StoredRateStatesItsOwnCondition(t *testing.T) {
	section := docSection(t, readDoc(t, "scorecard.md"), "### Field reference")
	row := docTableRow(t, section, "survived_skeptic_rate")

	assert.NotContains(t, row, "same as above",
		"the rate no longer shares findings_verified/findings_refuted's condition — deferring to them is the drift this guards")
	assert.Contains(t, row, "findings_verified + findings_refuted",
		"the row must name the countable-verdict condition the emitter actually gates on")
}
