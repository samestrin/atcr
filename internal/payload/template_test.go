package payload

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleCtx() PayloadContext {
	return PayloadContext{
		AgentName:   "bruce",
		BaseRef:     "main",
		HeadRef:     "feature",
		FileCount:   5,
		PayloadMode: "diff",
		Payload:     "<diff content>",
		ScopeRule:   ScopeRule(ModeDiff),
	}
}

// AC 01-06 S3 / Story 6 seam: ToolsEnabled gates {{if .ToolsEnabled}} sections
// so a tool agent can render tool-aware guidance while non-tool agents do not.
func TestRenderPrompt_ToolsEnabledGatesSection(t *testing.T) {
	const tmpl = "base{{if .ToolsEnabled}} TOOLS{{end}}"

	on := sampleCtx()
	on.ToolsEnabled = true
	out, err := RenderPrompt(tmpl, on)
	require.NoError(t, err)
	assert.Equal(t, "base TOOLS", out)

	off := sampleCtx() // ToolsEnabled defaults to false
	out, err = RenderPrompt(tmpl, off)
	require.NoError(t, err)
	assert.Equal(t, "base", out)
}

func TestRenderPrompt_AllVars(t *testing.T) {
	tmpl := "Agent {{.AgentName}} reviews {{.FileCount}} files from {{.BaseRef}} to {{.HeadRef}} in {{.PayloadMode}} mode.\nScope: {{.ScopeRule}}\n{{.Payload}}"
	out, err := RenderPrompt(tmpl, sampleCtx())
	require.NoError(t, err)
	assert.Contains(t, out, "Agent bruce reviews 5 files from main to feature in diff mode.")
	assert.Contains(t, out, "<diff content>")
	assert.Contains(t, out, "Stay on the diff")
}

func TestRenderPrompt_DiffMode(t *testing.T) {
	ctx := sampleCtx()
	ctx.ScopeRule = ScopeRule(ModeDiff)
	out, err := RenderPrompt("{{.ScopeRule}}", ctx)
	require.NoError(t, err)
	assert.Contains(t, out, "changed regions")
}

func TestRenderPrompt_FilesMode(t *testing.T) {
	ctx := sampleCtx()
	ctx.PayloadMode = "files"
	ctx.ScopeRule = ScopeRule(ModeFiles)
	out, err := RenderPrompt("{{.ScopeRule}}", ctx)
	require.NoError(t, err)
	assert.Contains(t, out, "pre-existing")
	assert.Contains(t, out, "out-of-scope")
}

func TestRenderPrompt_EmptyPayload(t *testing.T) {
	ctx := sampleCtx()
	ctx.Payload = ""
	ctx.FileCount = 0
	out, err := RenderPrompt("files={{.FileCount}} [{{.Payload}}]", ctx)
	require.NoError(t, err)
	assert.Equal(t, "files=0 []", out)
}

func TestRenderPrompt_UnknownVar(t *testing.T) {
	_, err := RenderPrompt("hello {{.UnknownVar}}", sampleCtx())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown variable 'UnknownVar'")
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "UnknownVar", re.Field)
	assert.False(t, re.IsParse())
}

func TestRenderPrompt_ParseError(t *testing.T) {
	_, err := RenderPrompt("oops {{.Payload", sampleCtx())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse persona prompt template")
	var re *RenderError
	require.ErrorAs(t, err, &re)
	assert.True(t, re.IsParse())
}

func TestRenderPrompt_NoPayloadVars(t *testing.T) {
	out, err := RenderPrompt("just static text, no vars", sampleCtx())
	require.NoError(t, err)
	assert.Equal(t, "just static text, no vars", out)
}

func TestRenderPrompt_PayloadWithTemplateSyntax(t *testing.T) {
	ctx := sampleCtx()
	ctx.Payload = "code with {{ braces }} and {{.NotAVar}}"
	out, err := RenderPrompt("{{.Payload}}", ctx)
	require.NoError(t, err)
	// Payload is injected as data, never re-parsed.
	assert.Equal(t, "code with {{ braces }} and {{.NotAVar}}", out)
}

func TestRenderPrompt_PayloadWithDirectivesNotExecuted(t *testing.T) {
	// The real threat model: a reviewer's diff (untrusted) lands in {{.Payload}}.
	// Even template directives there must round-trip verbatim, never execute.
	ctx := sampleCtx()
	ctx.Payload = `{{define "x"}}EVIL{{end}}{{template "x"}}{{if true}}Y{{end}}`
	out, err := RenderPrompt("{{.Payload}}", ctx)
	require.NoError(t, err)
	assert.Equal(t, ctx.Payload, out)
	assert.NotContains(t, out, "EVILEVIL")
}

func TestScopeRule_FilesModeMentionsPreExisting(t *testing.T) {
	assert.Contains(t, ScopeRule(ModeFiles), "pre-existing")
}

func TestScopeRule_DiffBlocksConstrainToChanges(t *testing.T) {
	assert.Contains(t, ScopeRule(ModeDiff), "changed regions")
	assert.Contains(t, ScopeRule(ModeBlocks), "changed regions")
	// diff and blocks share the same conservative rule.
	assert.Equal(t, ScopeRule(ModeDiff), ScopeRule(ModeBlocks))
}

func TestDocs_PayloadModesExists(t *testing.T) {
	data, err := os.ReadFile("../../docs/payload-modes.md")
	require.NoError(t, err, "docs/payload-modes.md must exist")
	content := string(data)
	for _, want := range []string{"diff", "blocks", "files", "Default", "token"} {
		assert.Truef(t, strings.Contains(content, want), "docs/payload-modes.md missing %q", want)
	}
}
