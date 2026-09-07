package reconcile

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readDoc loads one docs/*.md file for the drift checks below. The published
// reconcile/ module must not assume this repo's layout, which is why these live
// in internal/reconcile/ — see CLAUDE.md and the precedent in
// justification_record_boundary_test.go.
func readDoc(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../../docs/" + name)
	require.NoError(t, err)
	return string(data)
}

// docSection returns the single line of a markdown table whose first cell names
// field. A row is the unit that drifts here: a caveat added three rows away is
// not the caveat a reader of THIS row will ever see.
func docTableRow(t *testing.T, doc, field string) string {
	t.Helper()
	prefix := "| `" + field + "` |"
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("docs table has no row for %q", field)
	return ""
}

// TestDocs_ToolBudgetBytesRowStatesTheSkepticClamp pins the registry.md row
// against internal/verify/invoke.go's skepticToolBudget.
//
// The row said "0 (unlimited)" and "0 = unlimited" with no caveat, while the
// skeptic lane clamps a 0 budget to the window-derived ceiling — and the
// context_window_tokens row in the SAME table already described that clamp, so
// the table contradicted itself. internal/payload/tools_persona_test.go asserts
// the literal "0 = unlimited" is present, so a green test pinned the half-false
// claim; that assertion is deliberately left standing, because the phrase is
// still true of the three lanes that do not clamp.
func TestDocs_ToolBudgetBytesRowStatesTheSkepticClamp(t *testing.T) {
	doc := readDoc(t, "registry.md")
	row := docTableRow(t, doc, "tool_budget_bytes")

	assert.Contains(t, row, "0 = unlimited",
		"the unqualified default is still the truth in the review, debate and executor lanes")
	assert.Contains(t, row, "skeptic",
		"the row must name the lane whose behaviour departs from the default it states")
	assert.Contains(t, row, "context_window_tokens",
		"a reader needs to know WHICH declaration triggers the clamp")
	assert.Contains(t, row, "EffectiveByteBudget",
		"naming the derivation is what makes the ceiling checkable rather than folklore")
}

// docBullet returns the single "- **`field`**" bullet naming want, or the first
// bullet containing want when the bullet is titled in prose rather than by field.
func docBullet(t *testing.T, doc, want string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("docs has no bullet containing %q", want)
	return ""
}

// TestDocs_VerificationPerFindingBudgetsMatchTheSkepticLane pins
// docs/verification.md's Cost Controls bullet against internal/verify/invoke.go.
//
// The bullet said a skeptic "reuses the reviewer tool-loop budgets: max_turns,
// tool_budget_bytes, and timeout_secs" and that "a tripped budget yields
// unverifiable". Both statements acquired exceptions in the same lane: the tool
// budget is clamped to the declared window, and a trip on THAT derived ceiling
// no longer voids the verdict. The bullet also never named max_tokens, which the
// lane forwards to the provider. A reader could not learn any of the three.
func TestDocs_VerificationPerFindingBudgetsMatchTheSkepticLane(t *testing.T) {
	doc := readDoc(t, "verification.md")
	bullet := docBullet(t, doc, "Per-finding budgets")

	assert.Contains(t, bullet, "max_tokens",
		"the lane forwards the output cap to the provider; a budget list that omits it is incomplete")
	assert.Contains(t, bullet, "context_window_tokens",
		"the tool budget is no longer reused verbatim — a declared window clamps it")
	assert.Contains(t, bullet, "EffectiveByteBudget",
		"naming the derivation is what lets a reader check the ceiling instead of guessing it")
	assert.NotContains(t, bullet, "A tripped budget yields `unverifiable`, never a dropped finding.",
		"the unqualified claim is false for a trip on the derived ceiling, which truncates without voiding the verdict")
}
