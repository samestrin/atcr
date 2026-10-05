package mcp

import (
	"encoding/json"
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

// The atcr_debate result must publish the withheld count SEPARATELY from the cap
// overflow. The two carry OPPOSITE remedies — raising debate.max_items recovers a cap
// overflow, while a withheld item is not recoverable at ANY cap value
// (withholdExhausted runs before selection) — so a single conflated integer told an
// MCP client that raising the cap would debate an item already permanently removed
// (TD internal/mcp/handlers.go:871).
func TestHandleDebate_PublishesWithheldSeparatelyFromOverflow(t *testing.T) {
	root := t.TempDir()
	writeReviewConfig(t, root)
	f := reconcile.JSONFinding{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "HIGH",
		Reviewers: []string{"greta"}, Disagreement: "MEDIUM vs HIGH",
	}
	id := verifyReviewFixture(t, root, []reconcile.JSONFinding{f})

	// A prior debate.json already left this item unresolved at the ceiling, so
	// withholdExhausted drops it before selection and records it as overflow-with-a-
	// reason. Its Reason is what distinguishes it from a cap overflow.
	df := map[string]any{
		"schema_version": 3,
		"items": []map[string]any{{
			"file": f.File, "line": f.Line, "kind": "severity_split", "problem": f.Problem,
			"outcome": "unresolved", "reason": "seat_silent", "unresolved_attempts": 3,
		}},
	}
	raw, err := json.Marshal(df)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atcr", "reviews", id, "reconciled", "debate.json"), raw, 0o644))

	cs := connectTest(t, root, fakeCompleter{})
	out := callOK[DebateResult](t, cs, ToolDebate, map[string]any{"id_or_path": id})

	assert.Equal(t, 1, out.Withheld, "the exhausted item is published under withheld")
	assert.Equal(t, 0, out.Overflow, "and NOT under overflow: no max_items cap skipped it")
}
