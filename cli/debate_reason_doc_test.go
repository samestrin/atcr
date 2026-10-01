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

// docBullet returns the single "- " bullet containing want, docTableRow the
// single table row whose first cell names field; both fail loudly on 0 or >1
// matches. Duplicated from internal/reconcile/skeptic_budget_docs_test.go
// (unexported there), for the same reason that package reads its docs itself:
// the published reconcile module must not assume this repo's layout, and cli
// must not reach into its test helpers. The strict-count contract matters here
// precisely because this guard previously asserted against the WHOLE document,
// so a token drifting into any unrelated section still satisfied it.
func docBullet(t *testing.T, doc, want string) string {
	t.Helper()
	var matches []string
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, want) {
			matches = append(matches, line)
		}
	}
	require.Len(t, matches, 1,
		"the guard must pin exactly one bullet (0 = the anchor drifted or was reworded; >1 = the anchor is too generic)")
	return matches[0]
}

func docTableRow(t *testing.T, doc, field string) string {
	t.Helper()
	prefix := "| `" + field + "` |"
	var matches []string
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, prefix) {
			matches = append(matches, line)
		}
	}
	require.Len(t, matches, 1,
		"the guard must pin exactly one table row (0 = the anchor drifted or was reworded; >1 = the anchor is too generic)")
	return matches[0]
}

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
//
// The assertions are SCOPED, not document-wide: the seat reasons belong to the
// "Per-seat budgets" bullet — the same unit the prose half pins — and the full
// vocabulary to the debate.json artifact row, the place the bullet itself
// points to. A token moved into an unrelated section must fail here, not pass
// because it still appears somewhere in the document.
func TestDocs_DebateReasonTokensMatchTheConstants(t *testing.T) {
	root := repoRootDir(t)
	docPath := filepath.Join(root, "docs", "cross-examination.md")
	// repoRootDir ascends to the nearest go.mod, so a module split — the stated
	// reason for hoisting cli/ top-level — silently retargets this read and
	// would skip the guard. Fail loudly instead.
	require.FileExists(t, docPath)
	raw, err := os.ReadFile(docPath)
	require.NoError(t, err)
	doc := string(raw)

	seatBullet := docBullet(t, doc, "Per-seat budgets")
	vocabRow := docTableRow(t, doc, "reconciled/debate.json")

	for _, reason := range []string{debate.ReasonSeatHalted, debate.ReasonSeatSilent, debate.ReasonSeatSuppressed} {
		assert.Contains(t, seatBullet, "`"+reason+"`",
			"the Per-seat budgets bullet must publish the CURRENT constant %q — a rename in internal/debate that leaves the bullet behind is exactly what a literal-only guard misses", reason)
	}
	for _, reason := range []string{
		debate.ReasonSeatHalted,
		debate.ReasonSeatSilent,
		debate.ReasonSeatSuppressed,
		debate.ReasonJudgeHalted,
		debate.ReasonUnparseableRuling,
		debate.ReasonEmptyRuling,
		debate.ReasonHarnessUnavailable,
		debate.ReasonContextCancelled,
		debate.ReasonNoClusterDecision,
		debate.ReasonNoProposer,
		debate.ReasonInsufficientModels,
	} {
		assert.Contains(t, vocabRow, "`"+reason+"`",
			"the debate.json artifact row must publish the CURRENT constant %q — the bullet defers the complete vocabulary to this row, so a missing token leaves an operator without a lookup", reason)
	}
}
