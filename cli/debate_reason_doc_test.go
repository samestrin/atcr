package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/debate"
)

// TestDocs_DebateReasonTokensMatchTheConstants is the code-anchored half of the
// cross-examination.md reason guard.
//
// The prose half lives in internal/reconcile/skeptic_budget_docs_test.go, which
// can only assert string literals: internal/debate imports internal/reconcile,
// so that package cannot import internal/debate back without a cycle. A literal
// guard is one-directional — it catches a doc revert, but renaming a reason in
// internal/debate leaves it green and the doc silently wrong. cli/ imports
// internal/debate already (cli/debate.go), so the constant comparison belongs
// here. The benchmark outcome vocabulary is split the same way for the same
// reason.
//
// Sprint 35.16.11.2.2.4 Phase 2 added ReasonSeatSilent, which is what made the
// one-directional gap worth closing: a reason token that reaches debate.json and
// no document is one an operator cannot look up.
func TestDocs_DebateReasonTokensMatchTheConstants(t *testing.T) {
	root := repoRootDir(t)
	raw, err := os.ReadFile(filepath.Join(root, "docs", "cross-examination.md"))
	require.NoError(t, err)
	doc := string(raw)

	for _, reason := range []string{
		debate.ReasonNoProposer,
		debate.ReasonInsufficientModels,
		debate.ReasonSeatHalted,
		debate.ReasonSeatSilent,
		debate.ReasonJudgeHalted,
		debate.ReasonUnparseableRuling,
		debate.ReasonHarnessUnavailable,
		debate.ReasonContextCancelled,
		debate.ReasonNoClusterDecision,
	} {
		assert.True(t, strings.Contains(doc, "`"+reason+"`"),
			"docs/cross-examination.md must publish the CURRENT constant %q — a rename in internal/debate that leaves the doc behind is exactly what a literal-only guard misses", reason)
	}
}
