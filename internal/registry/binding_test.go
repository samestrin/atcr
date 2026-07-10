package registry

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// --- AC 02-01: AgentConfig.Binding schema extension -------------------------
//
// The persona-YAML-facing `binding` field a future resolver (Theme 3) will read
// to compute a new Model value at `atcr personas upgrade` time. In Phase 2 it is
// additive and inert: decoded permissively, omitempty on the wire, alongside
// Fallback/Payload. AgentConfig.Model itself is unchanged — it already IS the lock.

// TestAgentConfig_BindingDecodes proves a persona YAML declaring `binding:`
// decodes into AgentConfig.Binding, with Model left independent (AC 02-01 S2).
func TestAgentConfig_BindingDecodes(t *testing.T) {
	const doc = "provider: openrouter\nmodel: anthropic/claude-opus-4.8\nbinding: anthropic/claude-opus@stable\n"
	var ac AgentConfig
	require.NoError(t, yaml.Unmarshal([]byte(doc), &ac))
	assert.Equal(t, "anthropic/claude-opus@stable", ac.Binding)
	assert.Equal(t, "anthropic/claude-opus-4.8", ac.Model, "Model (the lock) is independent of Binding")
}

// TestAgentConfig_BindingOmitemptyTag pins the yaml tag: `binding,omitempty`,
// matching the additive convention already used for Fallback/Payload.
func TestAgentConfig_BindingOmitemptyTag(t *testing.T) {
	rt := reflect.TypeOf(AgentConfig{})
	f, ok := rt.FieldByName("Binding")
	require.True(t, ok, "AgentConfig must have a Binding field")
	assert.Equal(t, "binding,omitempty", f.Tag.Get("yaml"),
		"Binding must carry yaml:\"binding,omitempty\"")
}

// TestAgentConfig_BindingAbsentDecodesEmpty proves an existing (pre-epic) persona
// YAML with no `binding:` key decodes Binding as "" with no error — back-compat.
func TestAgentConfig_BindingAbsentDecodesEmpty(t *testing.T) {
	const doc = "provider: openrouter\nmodel: anthropic/claude-opus-4.8\n"
	var ac AgentConfig
	require.NoError(t, yaml.Unmarshal([]byte(doc), &ac))
	assert.Equal(t, "", ac.Binding, "absent binding decodes as the zero value")
}
