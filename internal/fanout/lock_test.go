package fanout

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AC 02-02: review path reads the locked Model, zero endpoint calls -------
//
// These are regression-lock tests, not behavior-driving ones. Epic 19.7's
// reproducibility guarantee (Objective 2) rests on the review path resolving the
// model from the static AgentConfig.Model field (the "lock") with NO catalog /
// resolution endpoint call — ever. renderAgent/buildFallbackAgent already do
// exactly this; the value of these tests is that they FAIL the build if a future
// change ever starts consuming AgentConfig.Binding on the review path or adds a
// live resolution call, which would silently break reproducibility. The distinct,
// non-overlapping Model vs Binding sentinel values make any such leak obvious.

// lockCfg builds a single-primary ReviewConfig whose agent carries distinct
// Model (the lock) and Binding (inert) sentinel values, so any code that wrongly
// substitutes Binding for Model is caught by a trivially distinguishable value.
func lockCfg(srvURL string) *ReviewConfig {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: srvURL}},
		Agents: map[string]registry.AgentConfig{
			"greta": {Provider: "p", Model: "model-greta", Binding: "binding-greta", Persona: "greta", Temperature: ptrF(0.7)},
		},
	}
	return &ReviewConfig{
		Registry:    reg,
		Project:     &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings:    registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
		PersonaDirs: registry.PersonaDirs{},
	}
}

// TestReviewPath_InvocationModelIsLockNotBinding covers AC 02-02 Scenario 1: the
// rendered Invocation.Model equals AgentConfig.Model exactly, and the Binding
// value appears nowhere in the rendered agent (prompt, invocation, cache key).
func TestReviewPath_InvocationModelIsLockNotBinding(t *testing.T) {
	cfg := lockCfg("http://unused.invalid")
	payloads := map[string]modePayload{"blocks": {Text: "diff body", FileCount: 1}}

	agent, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)

	assert.Equal(t, "model-greta", agent.Invocation.Model,
		"Invocation.Model must be the locked AgentConfig.Model, not derived from Binding")

	// The Binding sentinel must not leak anywhere on the review path.
	assert.NotEqual(t, "binding-greta", agent.Invocation.Model)
	assert.NotContains(t, agent.Prompt, "binding-greta", "Binding must never reach the rendered prompt")
	assert.NotContains(t, agent.Invocation.Prompt, "binding-greta", "Binding must never reach the invocation prompt")
	assert.NotContains(t, agent.CacheKey, "binding-greta", "Binding must not feed the diff-cache key")
}

// TestReviewPath_FallbackModelIsLockNotBinding covers AC 02-02 Edge Case 3: a
// fallback agent's Invocation.Model derives from its OWN Model field, never from
// Binding — Binding plays no role in fallback-chain construction either.
func TestReviewPath_FallbackModelIsLockNotBinding(t *testing.T) {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "ATCR_TEST_KEY", BaseURL: "http://unused.invalid"}},
		Agents: map[string]registry.AgentConfig{
			"greta": {Provider: "p", Model: "model-greta", Binding: "binding-greta", Persona: "greta", Temperature: ptrF(0.7), Fallback: "kai"},
			"kai":   {Provider: "p", Model: "model-kai", Binding: "binding-kai", Persona: "kai", Temperature: ptrF(0.7)},
		},
	}
	cfg := &ReviewConfig{
		Registry:    reg,
		Project:     &registry.ProjectConfig{Agents: []string{"greta"}},
		Settings:    registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600},
		PersonaDirs: registry.PersonaDirs{},
	}
	payloads := map[string]modePayload{"blocks": {Text: "diff body", FileCount: 1}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", false)
	require.NoError(t, err)
	require.Len(t, slots, 1)
	require.Len(t, slots[0].Fallbacks, 1, "greta's fallback chain must produce one fallback (kai)")

	fb := slots[0].Fallbacks[0]
	assert.Equal(t, "model-kai", fb.Invocation.Model,
		"fallback Invocation.Model must be the fallback's OWN locked Model, not any Binding")
	assert.NotEqual(t, "binding-kai", fb.Invocation.Model)
	assert.NotEqual(t, "binding-greta", fb.Invocation.Model)
}

// TestReviewPath_ZeroCatalogEndpointToResolveModel covers AC 02-02 Scenario 2:
// an end-to-end review makes exactly the LLM completion call(s) built from
// Invocation.Model and NO separate catalog/models/resolution request. Every
// recorded request must carry the locked model in its body — never the Binding
// value — and no request path may be catalog-shaped.
func TestReviewPath_ZeroCatalogEndpointToResolveModel(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	circuitbreaker.DefaultRegistry.Reset()
	t.Cleanup(circuitbreaker.DefaultRegistry.Reset)

	var (
		mu     sync.Mutex
		paths  []string
		models []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		models = append(models, req.Model)
		mu.Unlock()
		content := "CRITICAL|auth.go:3|Unchecked call|Guard it|security|15|b() unchecked"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	repo, base, head := initRepo(t)
	cfg := lockCfg(srv.URL)

	_, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, models, "the review must have made at least one completion call")
	for i, m := range models {
		assert.Equal(t, "model-greta", m,
			"every outbound completion must use the locked Model, never the Binding value (request %d)", i)
		assert.NotEqual(t, "binding-greta", m)
	}
	// No request path may be catalog/models/resolution-shaped: the review path
	// resolves the model from the static lock, not from any endpoint.
	for _, p := range paths {
		lp := strings.ToLower(p)
		assert.NotContains(t, lp, "/models", "review path must not hit a models/catalog endpoint (got %q)", p)
		assert.NotContains(t, lp, "catalog", "review path must not hit a catalog endpoint (got %q)", p)
	}
}
