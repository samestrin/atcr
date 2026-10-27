package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A registry whose agent pairs preserve_thinking: on with replay_reasoning: off
// loads with a validation warning. LoadMergedRegistry prints it; the quiet
// loader, used by display-only readers such as the report's reviewer roles
// (Epic 83.0 T5), loads the same registry and prints nothing, so a report run
// does not repeat the review's load warnings.
func TestLoadMergedRegistryQuiet_SuppressesLoadWarnings(t *testing.T) {
	regPath := writeUserRegistry(t, "providers:\n  p:\n    api_key_env: K\nagents:\n"+
		thinkingAgent(ThinkingOn, "", ThinkingStyleQwen)+
		"    preserve_thinking: on\n    replay_reasoning: off\n")

	loud := captureThinkingWarnings(t)
	_, err := LoadMergedRegistry(regPath, t.TempDir())
	require.NoError(t, err)
	require.NotEmpty(t, loud.String(), "fixture must trigger a load warning, or the quiet case below proves nothing")

	quiet := captureThinkingWarnings(t)
	reg, err := LoadMergedRegistryQuiet(regPath, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, quiet.String(), "the quiet loader must not print load warnings")
	require.Contains(t, reg.Agents, "myagent")
	assert.Equal(t, "myagent", reg.Agents["myagent"].Persona, "defaults still apply")
}
