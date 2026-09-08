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
	// Fenced code blocks in these sections hold shell examples whose comments
	// start with "#". Treating one as a heading truncates the section before the
	// table it exists to reach, and the check then fails for a reason that has
	// nothing to do with the doc it guards.
	inFence := false
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(lines[i], "#") {
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

// docParagraph returns the blank-line-delimited block of prose containing want.
// A paragraph is the unit that drifts for a prose claim, the same way a table row
// is for a field: a caveat added two paragraphs down is not one a reader of THIS
// paragraph will see. docBullet cannot serve here — these blocks are wrapped
// bold-lead paragraphs, not list items.
func docParagraph(t *testing.T, doc, want string) string {
	t.Helper()
	for _, block := range strings.Split(doc, "\n\n") {
		if strings.Contains(block, want) {
			return block
		}
	}
	t.Fatalf("docs has no paragraph containing %q", want)
	return ""
}

// TestScorecardDoc_ConditionalFieldsParagraphNamesBothOmissions pins the prose
// that sits under the field-reference table.
//
// The paragraph gave verification's absence as the ONLY reason the three keys are
// omitted. internal/scorecard/scorecard.go has a second, narrower one: verification
// ran, but v+r == 0, so survived_skeptic_rate alone is dropped while both counts
// still ship. A paragraph that names one case and says "these three" positively
// denies the other.
func TestScorecardDoc_ConditionalFieldsParagraphNamesBothOmissions(t *testing.T) {
	section := docSection(t, readDoc(t, "scorecard.md"), "### Field reference")
	para := docParagraph(t, section, "Conditional verification fields")

	assert.Contains(t, para, "second",
		"the paragraph must announce that there is more than one omission case")
	assert.Contains(t, para, "survived_skeptic_rate",
		"the second case applies to one named key, not to all three — the paragraph must say which")
	assert.Contains(t, para, "findings_verified + findings_refuted",
		"\"second\" alone is satisfied by any sentence using the word — the paragraph must state the condition the emitter gates on")
}

// TestScorecardDoc_PublicEnvelopeRowStatesTheAllTruncatedOmission pins the row in
// the PUBLIC submission envelope — a different table, with a different audience,
// from the stored-record row above.
//
// The row named exactly one reason for absence ("no verification ran for the
// group") and made it load-bearing: "the omission is the disambiguator".
// internal/scorecard/export.go now also omits the key for a group where
// verification DID run and no countable verdict survived — a stored 0.0 used to
// satisfy its len(storedRates) > 0 branch and is no longer produced. A board
// consumer reading absence as "no verify stage" is wrong under the new behaviour,
// so the row has to name both causes.
//
// This is a scorecard.md-only check rather than an entry in truncatedScoringDocs:
// that list carries assertions about `trippedBudgets` and the debate hand-off,
// which are the verification stage's contract and have no business being demanded
// of the submission-envelope schema.
func TestScorecardDoc_PublicEnvelopeRowStatesTheAllTruncatedOmission(t *testing.T) {
	section := docSection(t, readDoc(t, "scorecard.md"), "### `atcr leaderboard --export [--output path]`")
	row := docTableRow(t, section, "survived_skeptic_rate")

	assert.Contains(t, row, "truncated",
		"absence now has a second cause — a group whose verdicts were all truncated — and the row must name it")
	assert.NotContains(t, row, "The omission is the disambiguator.",
		"omission no longer disambiguates 'no verification' from 'verification ran'; leaving the claim tells a board consumer to read absence wrongly")
	assert.Contains(t, row, "no countable verdict",
		"naming truncation is not enough — the row must give the reading absence now supports, or a consumer keeps the old one")
}

// TestVerificationDoc_DebateSyncClaimIsScopedToTheCaveat pins how far the debate
// hand-off actually reaches.
//
// TestTruncatedScoringDocs_DescribeTheKeyTheScoreActuallyReads asserts only that
// the word "debate" appears, so it cannot see this: the document said a ruling
// "restores the verdict to the score as well as to the report". It does not.
// syncVerificationTruncation clears the tool_budget_bytes entry, and runDebate
// deliberately never rewrites verification.json's `verdict` field (see the scope
// note at internal/debate/debate.go:271) — so an OVERTURNED ruling is still
// counted under the stale verify-stage verdict. The sync restores the finding to
// the ratio; it does not restore the judge's verdict to it.
func TestVerificationDoc_DebateSyncClaimIsScopedToTheCaveat(t *testing.T) {
	doc := readDoc(t, "verification.md")
	para := docParagraph(t, doc, "kept in step from the debate side")

	assert.NotContains(t, para, "restores the verdict to the score",
		"debate never rewrites verification.json's verdict field, so a ruling cannot restore the verdict to the score")
	assert.Contains(t, para, "verdict it is counted under",
		"the paragraph must say WHICH verdict the score still uses, or a reader assumes the judge's")
}
