package payload

import (
	"os"
	"strings"
	"testing"

	"github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allPersonaTexts returns every shipped persona template plus _base, keyed by
// name, so the tool-guidance assertions cover the whole set.
func allPersonaTexts(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	base, err := personas.Base()
	require.NoError(t, err)
	out["_base"] = base
	for _, name := range personas.Names() {
		text, err := personas.Get(name)
		require.NoErrorf(t, err, "loading persona %q", name)
		out[name] = text
	}
	return out
}

func toolCtx(enabled bool) PayloadContext {
	return PayloadContext{
		AgentName: "tester", BaseRef: "main", HeadRef: "feature",
		FileCount: 3, PayloadMode: "blocks", Payload: "<payload>",
		ScopeRule: ScopeRule(ModeBlocks), ToolsEnabled: enabled,
	}
}

// AC 06-01 S1 + AC 06-02 S1/S2: every persona, rendered with ToolsEnabled=true,
// surfaces the tool-availability line, the evidence-citation rule, and the
// scope guard.
func TestPersona_ToolGuidanceRendersWhenEnabled(t *testing.T) {
	for name, text := range allPersonaTexts(t) {
		out, err := RenderPrompt(text, toolCtx(true))
		require.NoErrorf(t, err, "rendering persona %q", name)
		assert.Containsf(t, out, "read_file, grep, and list_files", "persona %q missing tool-availability line", name)
		assert.Containsf(t, out, "file path and line numbers", "persona %q missing evidence-citation rule", name)
		assert.Containsf(t, out, "tools widen evidence gathering, not review scope", "persona %q missing scope guard", name)
		assert.Containsf(t, out, "out-of-scope", "persona %q missing out-of-scope tag rule", name)
	}
}

// AC 06-01 S2/S3 + AC 06-02 S3: with ToolsEnabled=false the tool-aware guidance
// is absent — single-shot agents render exactly as in 1.0.
func TestPersona_ToolGuidanceOmittedWhenDisabled(t *testing.T) {
	for name, text := range allPersonaTexts(t) {
		out, err := RenderPrompt(text, toolCtx(false))
		require.NoErrorf(t, err, "rendering persona %q", name)
		assert.NotContainsf(t, out, "read_file, grep, and list_files", "persona %q leaked tool guidance when disabled", name)
		assert.NotContainsf(t, out, "tools widen evidence gathering, not review scope", "persona %q leaked scope guard when disabled", name)
	}
}

// AC 06-01 Edge Case 1: a PayloadContext that never sets ToolsEnabled (zero
// value) omits the tool guidance.
func TestPersona_ToolGuidanceDefaultsOff(t *testing.T) {
	base, err := personas.Base()
	require.NoError(t, err)
	out, err := RenderPrompt(base, PayloadContext{
		AgentName: "x", BaseRef: "a", HeadRef: "b", FileCount: 1,
		PayloadMode: "blocks", Payload: "p", ScopeRule: ScopeRule(ModeBlocks),
	})
	require.NoError(t, err)
	assert.NotContains(t, out, "read_file, grep, and list_files")
}

// AC 06-03 S1-S4 + Edge 1: registry.md documents the tool fields as active with
// defaults/validation, supports_function_calling, and a backward-compat note.
func TestDocs_RegistryDocumentsToolFields(t *testing.T) {
	data, err := os.ReadFile("../../docs/registry.md")
	require.NoError(t, err)
	content := string(data)
	for _, want := range []string{
		"max_turns", "tool_budget_bytes", "supports_function_calling",
		"0 = unlimited", "active in 2.0",
	} {
		assert.Containsf(t, content, want, "docs/registry.md missing %q", want)
	}
}

// AC 06-04 S1/S2: payload-modes.md documents payload-as-starting-point semantics
// and restates the scope rule for tool agents.
func TestDocs_PayloadModesToolAgentSection(t *testing.T) {
	data, err := os.ReadFile("../../docs/payload-modes.md")
	require.NoError(t, err)
	content := string(data)
	for _, want := range []string{"starting point", "read_file", "out-of-scope"} {
		assert.Containsf(t, content, want, "docs/payload-modes.md missing %q", want)
	}
}

// AC 06-04 S3 + Edge 1: README documents the 3-10× cost guidance and links to
// docs/registry.md for the budget fields.
func TestDocs_ReadmeCostGuidance(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "3-10×", "README missing the 3-10× cost guidance")
	assert.Contains(t, content, "docs/registry.md", "README must link to docs/registry.md for budgets")
	assert.Truef(t, strings.Contains(content, "tool"), "README should mention tool agents")
}
