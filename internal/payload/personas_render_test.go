package payload

import (
	"testing"

	"github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedPersonasRenderAgainstContext verifies every shipped persona (and
// _base) parses and executes against the typed PayloadContext with no unknown
// variables — the TD-005 contract that anchors persona templates to the data
// struct so renderer and templates cannot drift.
func TestEmbeddedPersonasRenderAgainstContext(t *testing.T) {
	ctx := PayloadContext{
		AgentName:   "tester",
		BaseRef:     "main",
		HeadRef:     "feature",
		FileCount:   3,
		PayloadMode: "blocks",
		Payload:     "<sample payload>",
		ScopeRule:   ScopeRule(ModeBlocks),
	}

	base, err := personas.Base()
	require.NoError(t, err)
	names := append(personas.Names(), "")
	for _, name := range names {
		text := base
		if name != "" {
			text, err = personas.Get(name)
			require.NoErrorf(t, err, "loading persona %q", name)
		}
		out, err := RenderPrompt(text, ctx)
		require.NoErrorf(t, err, "rendering persona %q", name)
		assert.Containsf(t, out, "tester", "persona %q should render AgentName", name)
		assert.Containsf(t, out, "<sample payload>", "persona %q should render Payload", name)
		assert.NotContainsf(t, out, "{{", "persona %q left an unrendered action", name)
	}
}
