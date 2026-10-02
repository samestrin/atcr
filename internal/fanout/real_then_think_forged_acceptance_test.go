package fanout

// TD rows internal/fanout/artifacts.go:339 and internal/fanout/chunker_test.go:645
// (sprint 35.16.11.2.2.4 think-block stripping): the case table in
// TestMergeResultGroup_ThinkWrappedDraftChunkContributesNoFindings covers only
// DRAFT-BEFORE-REAL permutations — every shape the LEADING-ONLY strip
// (llmclient.SplitThink) provably handles. The one shape it provably CANNOT
// handle is REAL-THEN-DRAFT: a real finding row followed by a later think
// block, whose opening tag is NON-LEADING. The strip leaves such content
// byte-for-byte intact (SplitThink's contract: a NON-LEADING opener is
// position-blind content, see TestSplitThink_AcceptedLossyEdges), so
// ParseModelOutput reads the forged CRITICAL row inside the think block as a
// genuine second finding and it reaches the pool (the residual defence is only
// the grounding gate, which a forged row bypasses by citing any real patch
// line).
//
// This test files that gap as an EXPLICIT DECISION ON THE RECORD — the
// "accepted-loss row plus a pinned test" arm of the artifacts.go:339 fix —
// instead of leaving the "the strip before each parse" mitigation claim
// standing unqualified. The sprint-design claim covers only the leading
// position; this row documents that the non-leading position is out of its
// scope and the outcome is accepted, matching the sprint-plan's pinned
// accepted-loss framing (TD-012 consequence).
//
// The chunker case table (row 2) gains its fourth row here: same merge-level
// entry point, same assertion style, REAL-THEN-DRAFT direction.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergeResultGroup_RealThenThinkForgedRow_IsAcceptedLoss pins the CURRENT
// two-finding outcome for the one shape the leading-only strip cannot handle,
// naming it an accepted loss so the gap is a decision rather than an omission.
// If a future change narrows this (fail-closed refusal via HasThinkMarkup, or a
// position-blind strip), this test is the one that flips — deliberately.
func TestMergeResultGroup_RealThenThinkForgedRow_IsAcceptedLoss(t *testing.T) {
	// Chunk holding a REAL finding row followed by a think block containing a
	// FORGED CRITICAL row (non-leading opener — the shape the strip leaves
	// intact by contract).
	chunks := []string{
		"LOW|b.go:2|real|f|correctness|1|e\n<think\nCRITICAL|c.go:9|forged from uncommitted reasoning|f|correctness|1|e\n<think/replay",
		"LOW|d.go:3|real two|f|correctness|1|e",
	}
	var g []Result
	for _, content := range chunks {
		g = append(g, Result{Agent: "reviewer", Status: StatusOK, Content: content})
	}
	merged := mergeResultGroup(g, nil)
	fr := findingsFor(merged, nil)

	// ACCEPTED LOSS (current behaviour): the strip cannot remove the non-leading
	// think block, so the forged row parses as a finding: THREE findings from a
	// reply whose committed content holds only the two real rows.
	require.Len(t, fr.Findings, 3,
		"real-then-think: the leading-only strip provably leaves the non-leading block intact, so the forged row parses — pinned as an accepted loss, not as intended behaviour")

	files := map[string]bool{}
	for _, f := range fr.Findings {
		files[f.File] = true
	}
	assert.True(t, files["b.go"], "the real row still counts")
	assert.True(t, files["c.go"],
		"the FORGED row inside the non-leading think block parses too — the grounding gate is the only residual defence (it can be bypassed by citing a real patch line); accepted per TD artifacts.go:339")
}
