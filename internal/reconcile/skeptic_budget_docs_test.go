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

	// The reservation clauses. Asserted as load-bearing PHRASES rather than whole
	// sentences, so ordinary rewording does not break the guard but losing the
	// meaning does.
	assert.Contains(t, row, "half the window's input room",
		"the reservation is capped at half the input room — a row that omits the cap describes a clamp the lane stopped performing")
	assert.Contains(t, row, "at or below the prompt overhead",
		"payload.EffectiveByteBudget returns 0 on effectiveTokens <= 0, so a window EXACTLY equal to the overhead also derives nothing")
	assert.NotContains(t, row, "floored at",
		"reservedOutputTokens DEFAULTS to the built-in 8192 when max_tokens is unset; it never floors, so max_tokens: 100 really does reserve 100")
}

// TestDocs_ContextWindowRowDoesNotRestateTheSkepticClamp pins the
// context_window_tokens row against the tool_budget_bytes row in the same table.
//
// Both rows described the same clamp, in different words, and they drifted: row
// 239 was corrected while row 71 kept an EffectiveByteBudget(model, declaration,
// max_tokens) formula that understates the reservation for every agent declaring
// no max_tokens and never mentions the half-room cap at all. Two descriptions of
// one behaviour is the drift mechanism itself, so the row now points at the
// other rather than restating it.
func TestDocs_ContextWindowRowDoesNotRestateTheSkepticClamp(t *testing.T) {
	doc := readDoc(t, "registry.md")
	row := docTableRow(t, doc, "context_window_tokens")

	assert.Contains(t, row, "tool_budget_bytes",
		"the row must still tell a reader WHICH budget the declaration bounds in the skeptic lane")
	assert.NotContains(t, row, "EffectiveByteBudget(",
		"a second copy of the formula is what drifted — this row cross-references the tool_budget_bytes row instead")
	assert.NotContains(t, row, "floored",
		"the reservation is a default, not a floor, in every row that mentions it")
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

	// The same two clauses registry.md's tool_budget_bytes row carries. Asserting
	// them in BOTH documents is what stops the pair drifting apart again: the
	// downstream sweep that corrected the registry row left this bullet behind,
	// and the guard passed anyway because it asserted neither clause.
	assert.Contains(t, bullet, "half the window's input room",
		"the reservation is capped at half the input room — without the cap the bullet promises a clamp the lane stopped performing")
	assert.NotContains(t, bullet, "floored at",
		"reservedOutputTokens DEFAULTS to the built-in 8192 when max_tokens is unset; neither this lane nor the review lane floors")
	assert.NotContains(t, bullet, "so tool output cannot walk a small-window skeptic past its own window",
		"the promise is what drifted: state the cap that makes it true, not the outcome alone")
}

// TestDocs_CrossExaminationPerSeatBudgetsMatchTheDebateLane pins
// docs/cross-examination.md's Cost Controls bullet against
// internal/debate/protocol.go.
//
// The bullet listed max_turns, tool_budget_bytes and timeout_secs as what a seat
// reuses. protocol.go also forwards max_tokens, and — unlike the skeptic lane —
// does NOT clamp tool_budget_bytes to a declared window (it calls derefInt64
// verbatim). The asymmetry between the two tool-using verification lanes is the
// kind a reader can only discover by reading both files, so it is stated here.
func TestDocs_CrossExaminationPerSeatBudgetsMatchTheDebateLane(t *testing.T) {
	doc := readDoc(t, "cross-examination.md")
	bullet := docBullet(t, doc, "Per-seat budgets")

	assert.Contains(t, bullet, "max_tokens",
		"the lane forwards the output cap to the provider; a budget list that omits it is incomplete")
	assert.Contains(t, bullet, "context_window_tokens",
		"the asymmetry with the skeptic lane is only discoverable if the clamp that does NOT apply here is named")
	assert.Contains(t, bullet, "skeptic",
		"naming the lane that behaves differently is what makes the asymmetry findable")
}
