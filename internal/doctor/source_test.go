package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergedRegWithProjectAgent builds a registry where agent "team" is project-tier
// and agent "bruce" is user-tier (source maps set as the merge path would).
func mergedRegWithProjectAgent() *registry.Registry {
	return &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		Agents: map[string]registry.AgentConfig{
			"bruce": {Provider: "p", Model: "m"},
			"team":  {Provider: "p", Model: "m"},
		},
		AgentSource: map[string]registry.EntrySource{
			"bruce": {Tier: registry.SourceUser},
			"team":  {Tier: registry.SourceProject},
		},
	}
}

func TestResolve_CarriesSourceTier(t *testing.T) {
	reg := mergedRegWithProjectAgent()
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"bruce", "team"}})
	require.NoError(t, err)

	src := map[string]string{}
	for _, at := range res.Agents {
		src[at.Agent] = at.Source
	}
	assert.Equal(t, registry.SourceUser, src["bruce"])
	assert.Equal(t, registry.SourceProject, src["team"])
}

func TestRun_PopulatesSource(t *testing.T) {
	t.Setenv("K", "key")
	reg := mergedRegWithProjectAgent()
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"bruce", "team"}})
	require.NoError(t, err)
	fake := newFake(func(inv llmclient.Invocation) (string, error) { return Marker(testNonce), nil })

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	got := map[string]string{}
	for _, ar := range rep.Agents {
		got[ar.Agent] = ar.Source
	}
	assert.Equal(t, registry.SourceProject, got["team"])
	assert.Equal(t, registry.SourceUser, got["bruce"])
}

func TestRenderTable_ShowsSourceColumn(t *testing.T) {
	rep := &Report{Agents: []AgentResult{
		{Agent: "team", Provider: "p", Model: "m", Status: StatusOK, LatencyMS: 10, Source: registry.SourceProject},
	}}
	var b strings.Builder
	RenderTable(&b, rep)
	out := b.String()
	assert.Contains(t, out, "SOURCE")
	assert.Contains(t, out, registry.SourceProject)
}

func TestRenderJSON_IncludesSource(t *testing.T) {
	rep := &Report{Agents: []AgentResult{
		{Agent: "team", Provider: "p", Model: "m", Status: StatusOK, Source: registry.SourceProject},
	}}
	var b strings.Builder
	require.NoError(t, RenderJSON(&b, rep))
	assert.Contains(t, b.String(), `"source": "project"`)
}
