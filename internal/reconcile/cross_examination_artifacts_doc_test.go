package reconcile

import (
	"strings"
	"testing"
)

// docs/cross-examination.md's reconciled/debate.json Artifacts row is wrong on three
// counts after the mask and carry changes:
//
//  1. `judge_think_markup` is described as refusing "a judge that merely quotes the
//     tag" — the masking in debate.go deliberately REVERSES that, so a reply whose
//     only tag sits inside a JSON string value is now parsed.
//  2. The same token now covers a SECOND cause: the ambiguous-unopened-closer arm,
//     which the doc does not mention at all.
//  3. The overflow-record shape says each record carries a `reason` plus
//     `unresolved_attempts` "when the cap was not the cause" — but emit.go now
//     writes `unresolved_attempts` onto CAP records too, which carry no reason.
//
// BIDIRECTIONAL: each doc needle pins the corrected claim, each code needle pins the
// arm that makes it true.
func TestCrossExaminationDoc_ArtifactsRowMatchesTheJudgeGuardsAndOverflowShape(t *testing.T) {
	doc := readRepoFile(t, "../../docs/cross-examination.md")
	debate := readRepoFile(t, "../../internal/debate/debate.go")
	emit := readRepoFile(t, "../../internal/debate/emit.go")

	// (1) A quoted tag is deliberately ADMITTED, not refused.
	if strings.Contains(doc, "also refuses a judge that merely quotes the tag") {
		t.Errorf("the Artifacts row still claims judge_think_markup refuses a judge that merely quotes the tag — " +
			"masking deliberately admits that reply")
	}
	if !strings.Contains(doc, "quotes the tag inside a JSON string value is deliberately admitted") {
		t.Errorf("the Artifacts row must state that a quoted tag is deliberately admitted, not refused")
	}

	// (2) The token covers two causes, and the second is named.
	for _, needle := range []string{"ambiguous", "closer"} {
		if !strings.Contains(doc, needle) {
			t.Errorf("the Artifacts row must disclose that judge_think_markup now covers the ambiguous-unopened-closer arm (%q)", needle)
		}
	}
	if !strings.Contains(debate, "SectionAmbiguous") {
		t.Errorf("debate.go must still route the ambiguous-unopened-closer arm to judge_think_markup")
	}

	// (3) unresolved_attempts may appear on a cap record, which carries no reason.
	if strings.Contains(doc, "when the cap was not the cause, a `reason` plus the `unresolved_attempts` behind it") {
		t.Errorf("the overflow-record shape is stale: unresolved_attempts now appears on cap records too, which carry no reason")
	}
	if !strings.Contains(doc, "may appear with or without a `reason`") {
		t.Errorf("the Artifacts row must say unresolved_attempts may appear with or without a reason")
	}
	if !strings.Contains(emit, "UnresolvedAttempts: attempts[FindingKey{") {
		t.Errorf("overflowItems must still carry the attempt count onto a cap-overflow record")
	}
}
