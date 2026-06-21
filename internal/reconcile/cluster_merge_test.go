package reconcile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestJSONFinding_ClusterMergedOmitempty: the Epic 6.1 idempotency marker is
// omitempty, so a non-merged record serializes byte-identically to a pre-6.1
// findings.json (the field is absent), and only a merged record carries it.
func TestJSONFinding_ClusterMergedOmitempty(t *testing.T) {
	plain, err := json.Marshal(JSONFinding{Severity: "LOW", File: "x.go", Line: 1, Problem: "p"})
	require.NoError(t, err)
	assert.NotContains(t, string(plain), "cluster_merged", "non-merged record must omit the field")

	merged, err := json.Marshal(JSONFinding{Severity: "LOW", File: "x.go", Line: 1, Problem: "p", ClusterMerged: true})
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(merged), `"cluster_merged":true`), "a merged record must carry the flag")
}
