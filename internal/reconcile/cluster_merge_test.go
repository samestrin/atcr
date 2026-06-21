package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMergeJSONFindings_UnionsMembers: collapsing two gray-zone member records
// (the judge ruled "merge") applies the same field rules as Merge over the
// already-reconciled JSONFinding shape — severity max with a disagreement
// annotation, longest problem/fix, max est, unioned+sorted reviewers, and HIGH
// confidence once two distinct reviewers are present.
func TestMergeJSONFindings_UnionsMembers(t *testing.T) {
	group := []JSONFinding{
		{Severity: "MEDIUM", File: "a.go", Line: 10, Problem: "off by one in loop", Fix: "use <=", Category: "correctness", EstMinutes: 15, Evidence: "ev1", Reviewers: []string{"alice"}, Confidence: "MEDIUM"},
		{Severity: "HIGH", File: "a.go", Line: 10, Problem: "loop boundary error causes overflow", Fix: "bound", Category: "correctness", EstMinutes: 30, Evidence: "ev2", Reviewers: []string{"bob"}, Confidence: "MEDIUM"},
	}
	m := MergeJSONFindings(group)
	assert.Equal(t, "HIGH", m.Severity)
	assert.Equal(t, "MEDIUM vs HIGH", m.Disagreement)
	assert.Equal(t, "loop boundary error causes overflow", m.Problem)
	assert.Equal(t, []string{"alice", "bob"}, m.Reviewers)
	assert.Equal(t, ConfHigh, m.Confidence)
	assert.Equal(t, 30, m.EstMinutes)
	assert.Equal(t, "a.go", m.File)
	assert.Equal(t, 10, m.Line)
}

// TestMergeJSONFindings_SingleIsIdentity: a one-member group has nothing to
// collapse and is returned unchanged (the apply path never unions fewer than two
// records, but the helper must be total).
func TestMergeJSONFindings_SingleIsIdentity(t *testing.T) {
	f := JSONFinding{Severity: "LOW", File: "x.go", Line: 1, Problem: "p", Reviewers: []string{"a"}, Confidence: "MEDIUM"}
	assert.Equal(t, f, MergeJSONFindings([]JSONFinding{f}))
}
