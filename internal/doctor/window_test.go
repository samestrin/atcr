package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.1 TD: a context_window_tokens declaration was only observable
// through a failed provider call — and an over-declaration surfaces as an opaque
// HTTP 400, not as a context-window error. The doctor is the pre-flight surface
// that already enumerates every roster agent, so it reports each agent's
// RESOLVED window and the tier it resolved from, making a declaration that took
// effect (and one that contradicts a known model) visible without a real run.

// windowRegistry has one agent declaring a window, one on a table-known model,
// and one on an unknown alias that falls through to the conservative default.
func windowRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	declared := 262144
	return &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		Agents: map[string]registry.AgentConfig{
			"decl":  {Provider: "p", Model: "glm-5.2", ContextWindowTokens: &declared},
			"table": {Provider: "p", Model: "openai/gpt-5.5"},
			"deflt": {Provider: "p", Model: "some-proxy-alias"},
		},
	}
}

func windowRows(t *testing.T, res *Resolution) map[string]AgentTarget {
	t.Helper()
	out := map[string]AgentTarget{}
	for _, at := range res.Agents {
		out[at.Agent] = at
	}
	return out
}

func TestResolve_CarriesResolvedWindowAndSource(t *testing.T) {
	res, err := Resolve(windowRegistry(t), &registry.ProjectConfig{Agents: []string{"decl", "table", "deflt"}})
	require.NoError(t, err)
	rows := windowRows(t, res)

	assert.Equal(t, 262144, rows["decl"].ContextWindowTokens,
		"a declaration must be reported verbatim — that is what proves it took effect")
	assert.Equal(t, payload.WindowSourceDeclaration, rows["decl"].WindowSource)

	assert.Equal(t, 128000, rows["table"].ContextWindowTokens)
	assert.Equal(t, payload.WindowSourceTable, rows["table"].WindowSource)

	assert.Equal(t, 32768, rows["deflt"].ContextWindowTokens,
		"an unknown proxy alias resolves the conservative default")
	assert.Equal(t, payload.WindowSourceDefault, rows["deflt"].WindowSource)
}

func TestResolve_WindowIsPerAgentNotPerTarget(t *testing.T) {
	// Two agents can share one probe target (same provider+model+base_url) while
	// declaring different windows, so the window cannot ride on Target.
	declared := 262144
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		Agents: map[string]registry.AgentConfig{
			"a": {Provider: "p", Model: "glm-5.2", ContextWindowTokens: &declared},
			"b": {Provider: "p", Model: "glm-5.2"},
		},
	}
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a", "b"}})
	require.NoError(t, err)
	require.Len(t, res.Targets, 1, "the two agents must still share one probe target")

	rows := windowRows(t, res)
	assert.Equal(t, 262144, rows["a"].ContextWindowTokens)
	assert.Equal(t, 128000, rows["b"].ContextWindowTokens,
		"the undeclared sibling keeps the table window despite sharing a target")
}

func TestRun_PopulatesWindowColumns(t *testing.T) {
	t.Setenv("K", "key")
	res, err := Resolve(windowRegistry(t), &registry.ProjectConfig{Agents: []string{"decl"}})
	require.NoError(t, err)
	fake := newFake(func(inv llmclient.Invocation) (string, error) { return Marker(testNonce), nil })

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	require.Len(t, rep.Agents, 1)
	assert.Equal(t, 262144, rep.Agents[0].ContextWindowTokens)
	assert.Equal(t, payload.WindowSourceDeclaration, rep.Agents[0].WindowSource)
}

func TestRenderTable_ShowsWindowAndSource(t *testing.T) {
	rep := &Report{Agents: []AgentResult{{
		Agent: "decl", Provider: "p", Model: "glm-5.2", Status: StatusOK,
		ContextWindowTokens: 262144, WindowSource: payload.WindowSourceDeclaration,
	}}}
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, rep))
	out := buf.String()
	assert.Contains(t, out, "WINDOW", "the table must carry a window header")
	assert.Contains(t, out, "262144")
	assert.Contains(t, out, payload.WindowSourceDeclaration,
		"the tier must be visible, not just the number — a declaration and a table hit look identical otherwise")
}

func TestRenderJSON_CarriesWindowFields(t *testing.T) {
	rep := &Report{Agents: []AgentResult{{
		Agent: "decl", Provider: "p", Model: "glm-5.2", Status: StatusOK,
		ContextWindowTokens: 262144, WindowSource: payload.WindowSourceDeclaration,
	}}}
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, rep))

	var got struct {
		Agents []struct {
			ContextWindowTokens int    `json:"context_window_tokens"`
			WindowSource        string `json:"window_source"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got.Agents, 1)
	assert.Equal(t, 262144, got.Agents[0].ContextWindowTokens)
	assert.Equal(t, payload.WindowSourceDeclaration, got.Agents[0].WindowSource)
}
