package reconcile

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SourceCategories rides JSONFindings so a Tier-4-routed record in
// res.Unresolved still carries each lens's own category, but it is in-process
// provenance: findings.json and unresolved.json serialize exactly as before.
func TestJSONFinding_SourceCategoriesCopiedButNotSerialized(t *testing.T) {
	m := Merged{
		Finding:          Finding{File: "a.go", Line: 1, Problem: "p", Category: "performance", Reviewers: []string{"bruce", "greta"}},
		SourceCategories: []SourceCategory{{Reviewer: "bruce", Category: "security"}, {Reviewer: "greta", Category: "performance"}},
	}
	got := Result{Findings: []Merged{m}}.JSONFindings()
	require.Len(t, got, 1)
	assert.Equal(t, m.SourceCategories, got[0].SourceCategories)

	b, err := json.Marshal(got[0])
	require.NoError(t, err)
	assert.NotContains(t, string(b), "source", "provenance must not reach the wire")
	assert.NotContains(t, string(b), "security", "bruce's own word is not serialized")
}
