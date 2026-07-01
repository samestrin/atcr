package fanout

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Slot resolution must inject an agent's scope categories into its persona
// prompt as a soft focus instruction (Epic 2.2), so a scoped agent sees them and
// an unscoped agent's prompt is unchanged.
func TestBuildOneAgent_InjectsScopeFocus(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	scoped := cfg.Registry.Agents["greta"]
	scoped.Scope = []string{"performance", "efficiency"}
	cfg.Registry.Agents["greta"] = scoped

	payloads := map[string]modePayload{"blocks": {Text: "some diff", FileCount: 1}}
	rng := ReviewRange{Base: "a", Head: "b"}

	gretaAgent, _, err := buildOneAgent(cfg, "greta", payloads, rng, "", "")
	require.NoError(t, err)
	require.Contains(t, gretaAgent.Prompt, "performance", "scoped agent prompt must mention its scope categories")
	require.Contains(t, gretaAgent.Prompt, "efficiency")
	// The invocation prompt the model actually receives must carry it too.
	require.Contains(t, gretaAgent.Invocation.Prompt, "performance")

	kaiAgent, _, err := buildOneAgent(cfg, "kai", payloads, rng, "", "")
	require.NoError(t, err)
	require.NotContains(t, kaiAgent.Prompt, "Review Focus", "unscoped agent prompt must be unchanged")
	// Lock the invocation prompt too — the model actually receives this, so an
	// unscoped agent must not carry scope focus text there either.
	require.NotContains(t, kaiAgent.Invocation.Prompt, "Review Focus", "unscoped agent invocation prompt must be unchanged")
}

// Slot resolution must prepend the sprint-plan SCOPE CONSTRAINT to an agent's
// payload, so the constraint lands in EVERY persona (it renders {{.Payload}}) and
// appears immediately before the diff text (Epic 12.2 AC4). An empty constraint
// leaves the prompt unchanged.
func TestBuildOneAgent_PrependsScopeConstraint(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	const diffToken = "UNIQUE_DIFF_TOKEN_98765"
	const constraint = "## SENTINEL SCOPE CONSTRAINT\nplan body\n\n"
	payloads := map[string]modePayload{"blocks": {Text: diffToken, FileCount: 1}}
	rng := ReviewRange{Base: "a", Head: "b"}

	agent, _, err := buildOneAgent(cfg, "greta", payloads, rng, "", constraint)
	require.NoError(t, err)
	require.Contains(t, agent.Prompt, "SENTINEL SCOPE CONSTRAINT", "constrained agent prompt must carry the scope constraint")
	// The constraint must sit BEFORE the diff text — that placement is the NFR
	// (the model must see the scope before the payload it scopes).
	ci := strings.Index(agent.Prompt, "SENTINEL SCOPE CONSTRAINT")
	di := strings.Index(agent.Prompt, diffToken)
	require.GreaterOrEqual(t, ci, 0)
	require.GreaterOrEqual(t, di, 0)
	require.Less(t, ci, di, "scope constraint must appear before the diff payload")
	// The invocation prompt the model actually receives must carry it too.
	require.Contains(t, agent.Invocation.Prompt, "SENTINEL SCOPE CONSTRAINT")

	// An empty constraint leaves the prompt unchanged (diff-wide review default).
	plain, _, err := buildOneAgent(cfg, "greta", payloads, rng, "", "")
	require.NoError(t, err)
	require.NotContains(t, plain.Prompt, "SENTINEL SCOPE CONSTRAINT", "empty constraint must not alter the prompt")
}
