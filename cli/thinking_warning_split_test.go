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

// TD cli/doctor.go:367: when the probe sent preserve_thinking and the provider
// rejected the declaration, the on-polarity remedy must name "retry without
// preserve_thinking" FIRST — the current copy sends the operator after
// thinking_style/model, the wrong knobs when the flagged culprit is the
// preserve flag itself.
func TestThinkingNotHonoredWarnings_NamesPreserveThinkingRemedyFirst(t *testing.T) {
	rep := &doctor.Report{Agents: []doctor.AgentResult{
		{Agent: "erin", Model: "model-e", ThinkingStatus: doctor.ThinkingNotHonored, ThinkingDeclared: registry.ThinkingOn, ThinkingPreserve: registry.ThinkingOn},
		{Agent: "frank", Model: "model-f", ThinkingStatus: doctor.ThinkingNotHonored, ThinkingDeclared: registry.ThinkingOn},
	}}

	lines := thinkingNotHonoredWarnings(rep)
	require.Len(t, lines, 1)
	// The preserve remedy is named before the style/model remedies.
	preserveIdx := strings.Index(lines[0], "retry without preserve_thinking")
	require.Greater(t, preserveIdx, -1, "warning must name 'retry without preserve_thinking'")
	styleIdx := strings.Index(lines[0], "thinking_style")
	require.Greater(t, styleIdx, -1, "warning keeps the style/model remedies")
	require.Less(t, preserveIdx, styleIdx, "preserve remedy comes first")
	assert.Contains(t, lines[0], "erin (model-e)")
	assert.Contains(t, lines[0], "frank (model-f)")
}

// Agents that did NOT send preserve_thinking must keep the current copy —
// no preserve remedy injected.
func TestThinkingNotHonoredWarnings_NoPreserveRemedyWhenFlagUnsent(t *testing.T) {
	rep := &doctor.Report{Agents: []doctor.AgentResult{
		{Agent: "gina", Model: "model-g", ThinkingStatus: doctor.ThinkingNotHonored, ThinkingDeclared: registry.ThinkingOn},
	}}
	lines := thinkingNotHonoredWarnings(rep)
	require.Len(t, lines, 1)
	assert.NotContains(t, lines[0], "preserve_thinking")
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
