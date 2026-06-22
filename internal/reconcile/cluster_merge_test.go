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

// TestMergeJSONFindings_VerificationPrecedence: a confirmed member verdict wins
// over a refuted one (confirmed > unverifiable > refuted) so a real issue is never
// masked by a refuted sibling, and every member's skeptic provenance is unioned.
func TestMergeJSONFindings_VerificationPrecedence(t *testing.T) {
	group := []JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 5, Problem: "issue A", Reviewers: []string{"alice"},
			Verification: &Verification{Verdict: VerdictRefuted, Skeptic: "sk1", Notes: "disproved A"}},
		{Severity: "HIGH", File: "a.go", Line: 5, Problem: "issue B longer", Reviewers: []string{"bob"},
			Verification: &Verification{Verdict: VerdictConfirmed, Skeptic: "sk2", Notes: "confirmed B", ChallengeSurvived: true}},
	}
	m := MergeJSONFindings(group)
	require.NotNil(t, m.Verification)
	assert.Equal(t, VerdictConfirmed, m.Verification.Verdict, "confirmed must win over refuted")
	assert.True(t, m.Verification.ChallengeSurvived)
	assert.Equal(t, "sk1, sk2", m.Verification.Skeptic, "skeptic provenance from all members is unioned")
}

// TestMergeJSONFindings_SkepticSplitAndDeduplicated: each member's Skeptic is a
// comma-joined list of voter names. mergeVerification must split those lists,
// deduplicate individual names, and omit empty tokens (e.g. trailing commas).
func TestMergeJSONFindings_SkepticSplitAndDeduplicated(t *testing.T) {
	group := []JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 5, Problem: "issue A", Reviewers: []string{"alice"},
			Verification: &Verification{Verdict: VerdictRefuted, Skeptic: "sk1,sk2,", Notes: "disproved A"}},
		{Severity: "HIGH", File: "a.go", Line: 5, Problem: "issue B longer", Reviewers: []string{"bob"},
			Verification: &Verification{Verdict: VerdictConfirmed, Skeptic: "sk2, sk3", Notes: "confirmed B"}},
	}
	m := MergeJSONFindings(group)
	require.NotNil(t, m.Verification)
	assert.Equal(t, "sk1, sk2, sk3", m.Verification.Skeptic, "individual skeptic names are split, deduped, and empty tokens omitted")
}

// TestMergeJSONFindings_NoVerificationStaysNil: with no member carrying a block,
// the merged record has no verification (a non-debated finding stays byte-stable).
func TestMergeJSONFindings_NoVerificationStaysNil(t *testing.T) {
	group := []JSONFinding{
		{Severity: "LOW", File: "a.go", Line: 5, Problem: "a", Reviewers: []string{"x"}},
		{Severity: "LOW", File: "a.go", Line: 5, Problem: "ab", Reviewers: []string{"y"}},
	}
	assert.Nil(t, MergeJSONFindings(group).Verification)
}

// TestMergeJSONFindings_PreservesMemberDisagreementLowerBound: a member is itself
// a reconciled record that may already carry a wider "<lo> vs <hi>" span than its
// scalar Severity. Merging a member annotated "LOW vs HIGH" (scalar HIGH) with a
// MEDIUM member must keep LOW as the lower bound, not narrow it to "MEDIUM vs HIGH"
// from the scalar severities alone (TD merge.go:282).
func TestMergeJSONFindings_PreservesMemberDisagreementLowerBound(t *testing.T) {
	group := []JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 10, Problem: "issue A with the longer problem text", Reviewers: []string{"alice"}, Disagreement: "LOW vs HIGH"},
		{Severity: "MEDIUM", File: "a.go", Line: 10, Problem: "issue B", Reviewers: []string{"bob"}},
	}
	m := MergeJSONFindings(group)
	assert.Equal(t, "HIGH", m.Severity)
	assert.Equal(t, "LOW vs HIGH", m.Disagreement, "a member's pre-existing wider span must not be narrowed to the scalar-severity range at cluster merge")
}

// TestMergeJSONFindings_PreservesSiblingPathSuggestion: PathWarning is
// file-existence keyed (identical across same-file members) but PathSuggestion
// (Epic 5.4) is set only on a candidate-index-corrected member. When the corrected
// member is ordered AFTER a clean member, the merged record must still carry the
// sibling's warning + suggestion rather than blindly taking group[0]'s empty
// fields, and path_valid must stay consistent with the surviving warning
// (TD merge.go:297 and merge.go:296 — members may even span lines under drift).
func TestMergeJSONFindings_PreservesSiblingPathSuggestion(t *testing.T) {
	group := []JSONFinding{
		{Severity: "LOW", File: "a.go", Line: 10, Problem: "clean member ordered first", Reviewers: []string{"alice"}, PathValid: true},
		{Severity: "LOW", File: "a.go", Line: 12, Problem: "flagged member carries the correction", Reviewers: []string{"bob"}, PathWarning: "path a.go not found under repo root", PathSuggestion: "real/a.go"},
	}
	m := MergeJSONFindings(group)
	assert.Equal(t, "path a.go not found under repo root", m.PathWarning, "a sibling's hallucinated-path warning must survive the merge")
	assert.Equal(t, "real/a.go", m.PathSuggestion, "a sibling's candidate-index correction must survive the merge")
	assert.False(t, m.PathValid, "path_valid must stay consistent with the surviving warning, not blindly take group[0]'s true")
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
