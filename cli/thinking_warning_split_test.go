package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/doctor"
	"github.com/samestrin/atcr/internal/registry"
)

// TD cli/doctor.go:255 (TD-017, re-attempted per user clarification 2026-09-27,
// scope (a)): the "thinking not honored" stderr warning must be split by the
// DECLARED polarity. A declared `off` that was ignored leaves a runaway
// thinker, so a larger max_tokens is a real remedy; a declared `on`/level that
// produced no signal will not think harder with more budget, so only another
// style or model is left. The old single warning suggested "a larger
// max_tokens or a different model" to both groups.
func TestThinkingNotHonoredWarnings_SplitByDeclaredPolarity(t *testing.T) {
	rep := &doctor.Report{Agents: []doctor.AgentResult{
		{Agent: "alice", Model: "model-a", ThinkingStatus: doctor.ThinkingNotHonored, ThinkingDeclared: registry.ThinkingOff},
		{Agent: "bob", Model: "model-b", ThinkingStatus: doctor.ThinkingNotHonored, ThinkingDeclared: registry.ThinkingLevelLow},
		{Agent: "carol", Model: "model-c", ThinkingStatus: doctor.ThinkingNotHonored, ThinkingDeclared: registry.ThinkingOn},
		{Agent: "dave", Model: "model-d", ThinkingStatus: doctor.ThinkingHonored, ThinkingDeclared: registry.ThinkingOn},
	}}

	lines := thinkingNotHonoredWarnings(rep)
	require.Len(t, lines, 2, "one warning line per declared polarity")

	joined := strings.Join(lines, "\n")
	// The off-polarity line keeps the max_tokens remedy and names only the off-declared agents.
	assert.Contains(t, joined, "a larger max_tokens")
	assert.Contains(t, joined, "alice (model-a)")
	// The on-polarity line drops the max_tokens remedy and names only the on-polarity agents.
	assert.Contains(t, joined, "another thinking_style or a different model")
	assert.Contains(t, joined, "bob (model-b)")
	assert.Contains(t, joined, "carol (model-c)")
	assert.NotContains(t, joined, "dave (model-d)", "honored agents are never named")
}

func TestThinkingNotHonoredWarnings_SinglePolarity(t *testing.T) {
	offOnly := &doctor.Report{Agents: []doctor.AgentResult{
		{Agent: "alice", Model: "model-a", ThinkingStatus: doctor.ThinkingNotHonored, ThinkingDeclared: registry.ThinkingOff},
	}}
	lines := thinkingNotHonoredWarnings(offOnly)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "a larger max_tokens")
	assert.Contains(t, lines[0], "alice (model-a)")

	onOnly := &doctor.Report{Agents: []doctor.AgentResult{
		{Agent: "bob", Model: "model-b", ThinkingStatus: doctor.ThinkingNotHonored, ThinkingDeclared: registry.ThinkingOn},
	}}
	lines = thinkingNotHonoredWarnings(onOnly)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "another thinking_style or a different model")
	assert.NotContains(t, lines[0], "larger max_tokens")
}
