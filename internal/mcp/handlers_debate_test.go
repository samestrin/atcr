package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
)

// TestHandleDebate_MissingReconciled: atcr_debate on a review without
// reconciled/findings.json returns the reconcile-first guidance, identical to the
// CLI and atcr_verify.
func TestHandleDebate_MissingReconciled(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := "2026-06-21_nodebate"
	dir := filepath.Join(root, ".atcr", "reviews", id)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sources", "pool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"base":"a","head":"HEAD","roster":["greta"],"partial":false,"stages":["review"]}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "latest"), []byte(id+"\n"), 0o644))
	cs := connectTest(t, root, fakeCompleter{})

	msg := callErr(t, cs, ToolDebate, map[string]any{"id_or_path": id})
	assert.Contains(t, msg, "no reconciled findings found")
	assert.Contains(t, msg, "atcr reconcile")
}

func TestHandleDebate_RequireVerifiedWithoutFailOn(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", Reviewers: []string{"greta"}},
	})
	cs := connectTest(t, root, fakeCompleter{})

	msg := callErr(t, cs, ToolDebate, map[string]any{"id_or_path": id, "requireVerified": true})
	assert.Contains(t, msg, "requireVerified requires failOn")
}

// TestHandleDebate_UnresolvedWithoutRoles: a disputed finding with no skeptic/judge
// roles configured is left unresolved (distinct-model rule, no opt-in) and the
// handler returns a clean tally rather than erroring — failure isolation.
func TestHandleDebate_UnresolvedWithoutRoles(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "HIGH",
			Reviewers: []string{"greta"}, Disagreement: "MEDIUM vs HIGH"},
	})
	cs := connectTest(t, root, fakeCompleter{})

	out := callOK[DebateResult](t, cs, ToolDebate, map[string]any{"id_or_path": id})
	assert.Equal(t, 1, out.Selected)
	assert.Equal(t, 1, out.Unresolved)
	assert.Equal(t, 0, out.Upheld)

	// debate.json is emitted.
	assert.FileExists(t, filepath.Join(root, ".atcr", "reviews", id, "reconciled", "debate.json"))
}
