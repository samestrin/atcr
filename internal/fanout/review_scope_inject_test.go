package fanout

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// buildAgent must inject an agent's scope categories into its persona prompt as
// a soft focus instruction (Epic 2.2), so a scoped agent sees them and an
// unscoped agent's prompt is unchanged.
func TestBuildAgent_InjectsScopeFocus(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	scoped := cfg.Registry.Agents["greta"]
	scoped.Scope = []string{"performance", "efficiency"}
	cfg.Registry.Agents["greta"] = scoped

	payloads := map[string]modePayload{"blocks": {Text: "some diff", FileCount: 1}}
	rng := ReviewRange{Base: "a", Head: "b"}

	gretaAgent, _, err := buildAgent(cfg, "greta", payloads, rng, "")
	require.NoError(t, err)
	require.Contains(t, gretaAgent.Prompt, "performance", "scoped agent prompt must mention its scope categories")
	require.Contains(t, gretaAgent.Prompt, "efficiency")
	// The invocation prompt the model actually receives must carry it too.
	require.Contains(t, gretaAgent.Invocation.Prompt, "performance")

	kaiAgent, _, err := buildAgent(cfg, "kai", payloads, rng, "")
	require.NoError(t, err)
	require.NotContains(t, kaiAgent.Prompt, "Review Focus", "unscoped agent prompt must be unchanged")
	// Lock the invocation prompt too — the model actually receives this, so an
	// unscoped agent must not carry scope focus text there either.
	require.NotContains(t, kaiAgent.Invocation.Prompt, "Review Focus", "unscoped agent invocation prompt must be unchanged")
}
