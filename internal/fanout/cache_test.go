package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/cache"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// cacheableSlot builds a non-serial, non-tool slot whose primary carries the
// diff-cache key derived from the rendered prompt (which subsumes payload +
// persona + scope) and the model.
func cacheableSlot(name, model, prompt string) Slot {
	return Slot{Primary: Agent{
		Name:        name,
		PayloadMode: "blocks",
		CacheKey:    diffCacheKey(prompt, model, "", nil, "", defaultMaxTokens, "", "", "", ""),
		Invocation:  llmclient.Invocation{Model: model, Prompt: prompt},
	}}
}

func TestEngine_CacheHitReplaysWithoutAPICall(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	slot := cacheableSlot("reviewer", "m", "the rendered prompt")

	// First run: cold cache -> one live call, result is written to cache.
	r1 := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	require.Len(t, r1, 1)
	assert.Equal(t, StatusOK, r1[0].Status)
	assert.Equal(t, "review by m", r1[0].Content)
	assert.False(t, r1[0].CacheHit, "cold cache cannot be a hit")
	assert.Equal(t, 1, f.callCount("m"))

	// Second run: same key -> served from cache, NO new API call.
	r2 := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	require.Len(t, r2, 1)
	assert.Equal(t, StatusOK, r2[0].Status)
	assert.Equal(t, "review by m", r2[0].Content)
	assert.True(t, r2[0].CacheHit, "warm cache must replay")
	assert.Equal(t, 1, f.callCount("m"), "cache hit must not make another API call")
}

func TestEngine_NoCacheBypassesReadButStillWrites(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	slot := cacheableSlot("reviewer", "m", "the rendered prompt")

	// Seed the cache.
	NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	require.Equal(t, 1, f.callCount("m"))

	// --no-cache (cacheNoRead=true): must bypass the read and make a live call...
	r := NewEngine(f, WithCache(store, true)).Run(context.Background(), []Slot{slot})
	assert.False(t, r[0].CacheHit, "no-cache must not replay")
	assert.Equal(t, 2, f.callCount("m"), "no-cache bypasses the cached entry and calls live")

	// ...and still refresh the entry, so a subsequent normal run hits.
	r2 := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	assert.True(t, r2[0].CacheHit, "no-cache run must still write fresh results")
	assert.Equal(t, 2, f.callCount("m"), "the refreshed entry is now served")
}

func TestEngine_DifferentModelMissesCache(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	NewEngine(f, WithCache(store, false)).Run(context.Background(),
		[]Slot{cacheableSlot("a", "m1", "same rendered prompt")})

	// Same payload+persona but a different model -> distinct key -> live call.
	r := NewEngine(f, WithCache(store, false)).Run(context.Background(),
		[]Slot{cacheableSlot("b", "m2", "same rendered prompt")})
	assert.False(t, r[0].CacheHit)
	assert.Equal(t, 1, f.callCount("m2"))
}

// TestEngine_DifferentPromptMissesCache is the regression guard for the
// independent-review HIGH finding: the key derives from the FULL rendered prompt
// (which embeds the per-agent scope focus, persona, and refs), so two runs of the
// same model whose prompts differ — e.g. a changed --scope — must NOT collide.
func TestEngine_DifferentPromptMissesCache(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	NewEngine(f, WithCache(store, false)).Run(context.Background(),
		[]Slot{cacheableSlot("a", "m", "prompt with scope:security")})

	// Same model, different rendered prompt (scope changed) -> distinct key -> live.
	r := NewEngine(f, WithCache(store, false)).Run(context.Background(),
		[]Slot{cacheableSlot("a", "m", "prompt with scope:performance")})
	assert.False(t, r[0].CacheHit, "a changed prompt (e.g. scope) must not replay a stale review")
	assert.Equal(t, 2, f.callCount("m"))
}

// TestEngine_DifferentTemperatureMissesCache guards the temperature MEDIUM
// finding: temperature changes the LLM output, so it is folded into the key.
func TestEngine_DifferentTemperatureMissesCache(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	hot, cold := 0.9, 0.1
	mk := func(temp *float64) Slot {
		return Slot{Primary: Agent{
			Name:        "a",
			PayloadMode: "blocks",
			CacheKey:    diffCacheKey("same prompt", "m", "", temp, "", defaultMaxTokens, "", "", "", ""),
			Invocation:  llmclient.Invocation{Model: "m", Prompt: "same prompt", Temperature: temp},
		}}
	}
	NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{mk(&hot)})
	r := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{mk(&cold)})
	assert.False(t, r[0].CacheHit, "a temperature change must invalidate the cache entry")
	assert.Equal(t, 2, f.callCount("m"))
}

// TestEngine_DifferentProviderMissesCache guards the cross-provider collision:
// two agents sharing a model id + rendered prompt + temperature but served by
// different backends (BaseURLs) must NOT collide on one cache entry, or the
// second backend would replay the first's review. The resolved backend is folded
// into the key alongside temperature.
func TestEngine_DifferentProviderMissesCache(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	mk := func(baseURL string) Slot {
		return Slot{Primary: Agent{
			Name:        "a",
			PayloadMode: "blocks",
			CacheKey:    diffCacheKey("same prompt", "m", baseURL, nil, "", defaultMaxTokens, "", "", "", ""),
			Invocation:  llmclient.Invocation{Model: "m", Prompt: "same prompt", BaseURL: baseURL},
		}}
	}
	NewEngine(f, WithCache(store, false)).Run(context.Background(),
		[]Slot{mk("https://api.provider-a.test/v1")})

	// Same model+prompt+temperature, different backend -> distinct key -> live.
	r := NewEngine(f, WithCache(store, false)).Run(context.Background(),
		[]Slot{mk("https://api.provider-b.test/v1")})
	assert.False(t, r[0].CacheHit, "same model+prompt+temp on a different backend must not replay")
	assert.Equal(t, 2, f.callCount("m"))
}

// TestDiffCacheKey_SizingTokenDistinguishesRegimes guards the Epic 19.10 F7
// contract: the per-agent effective-budget/chunk-plan token is folded into the
// key, so two calls identical in prompt/model/backend/temperature but sized under
// different regimes produce DIFFERENT keys — while the "0:0"/empty sentinel
// collapses to the pre-F7 key so no existing on-disk entry is invalidated.
func TestDiffCacheKey_SizingTokenDistinguishesRegimes(t *testing.T) {
	// Backward-compat: empty and the "0:0" no-sizing sentinel both reduce to the
	// exact pre-F7 (baseURL+temperature-only) key.
	base := diffCacheKey("p", "m", "", nil, "", defaultMaxTokens, "", "", "", "")
	assert.Equal(t, base, diffCacheKey("p", "m", "", nil, "0:0", defaultMaxTokens, "", "", "", ""),
		`"0:0" (no per-agent sizing) must collapse to the pre-F7 key`)

	// A real sizing token changes the key, and two distinct regimes never collide —
	// even though prompt/model/backend/temperature are identical across all three.
	sizedA := diffCacheKey("p", "m", "", nil, "100000:0", defaultMaxTokens, "", "", "", "")  // bulk, 100KB budget
	sizedB := diffCacheKey("p", "m", "", nil, "50000:200", defaultMaxTokens, "", "", "", "") // chunked, 50KB budget, 200-line chunks
	assert.NotEqual(t, base, sizedA, "a real sizing regime must change the key")
	assert.NotEqual(t, sizedA, sizedB, "different sizing regimes must produce different keys")
}

// TestEngine_DifferentSizingMissesCache is the AC7 integration regression
// (mirrors TestEngine_DifferentProviderMissesCache): two slots with identical
// prompt/model/backend/temperature but different per-agent sizing tokens must NOT
// share one cache entry, or a per-agent-sized payload would be served a stale
// full-payload review produced under a different sizing regime. This test fails
// against the pre-F7 4-arg diffCacheKey — there the two keys would be identical
// (same prompt/model/backend/temp), so the second run would replay a cache HIT.
func TestEngine_DifferentSizingMissesCache(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	mk := func(sizing string) Slot {
		return Slot{Primary: Agent{
			Name:        "a",
			PayloadMode: "blocks",
			CacheKey:    diffCacheKey("same prompt", "m", "", nil, sizing, defaultMaxTokens, "", "", "", ""),
			Invocation:  llmclient.Invocation{Model: "m", Prompt: "same prompt"},
		}}
	}
	// First run sizes under one regime and warms the cache.
	NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{mk("100000:0")})

	// Same prompt/model/backend/temp, DIFFERENT sizing regime -> distinct key -> live.
	r := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{mk("50000:200")})
	assert.False(t, r[0].CacheHit, "a per-agent-sized payload must not be served a stale full-payload cache hit")
	assert.Equal(t, 2, f.callCount("m"))
}

// The output cap is a per-agent, per-run input to the review (max_tokens on the
// agent, --max-tokens on the run) and it changes what comes back: a cap too small to
// hold the findings block returns an empty or half-written review. It must therefore
// be part of the key.
//
// The sizing token is NOT a proxy for it. appliedByteBudget clamps to
// payload_byte_budget, so whenever the global cap binds — the ordinary case on a
// large window — two very different output caps derive the SAME sizing token. An
// operator who adds max_tokens to fix an empty review then replays the cached empty
// review and concludes the setting does nothing.
func TestDiffCacheKey_ResolvedOutputCapChangesTheKey(t *testing.T) {
	base := diffCacheKey("p", "m", "", nil, "", defaultMaxTokens, "", "", "", "")
	assert.Equal(t, base, diffCacheKey("p", "m", "", nil, "0:0", defaultMaxTokens, "", "", "", ""),
		"an agent at the embedded default cap must keep its pre-existing on-disk key")
	assert.NotEqual(t, base, diffCacheKey("p", "m", "", nil, "", 32000, "", "", "", ""),
		"a declared max_tokens must invalidate the entry the default-capped run wrote")

	// The clamped case the sizing token cannot see: identical token, different cap.
	const clamped = "524288:0" // both caps derive a budget above payload_byte_budget
	assert.NotEqual(t,
		diffCacheKey("p", "m", "", nil, clamped, 8192, "", "", "", ""),
		diffCacheKey("p", "m", "", nil, clamped, 32000, "", "", "", ""),
		"two output caps that clamp to one sizing token must still key apart")
}

// The integration half, mirroring TestEngine_DifferentSizingMissesCache: raising the
// cap must reach the provider, not the cache.
func TestEngine_DifferentMaxTokensMissesCache(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	mk := func(maxTokens int) Slot {
		return Slot{Primary: Agent{
			Name:        "a",
			PayloadMode: "blocks",
			CacheKey:    diffCacheKey("same prompt", "m", "", nil, "524288:0", maxTokens, "", "", "", ""),
			Invocation:  llmclient.Invocation{Model: "m", Prompt: "same prompt", MaxTokens: &maxTokens},
		}}
	}
	NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{mk(8192)})

	r := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{mk(32000)})
	assert.False(t, r[0].CacheHit,
		"raising max_tokens to fix a truncated review must re-run it, not replay the truncated one")
	assert.Equal(t, 2, f.callCount("m"))
}

func TestEngine_FailedReviewIsNotCached(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	f.failFor["m"] = assertFailErr
	slot := cacheableSlot("reviewer", "m", "prompt p")

	r1 := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	require.Equal(t, StatusFailed, r1[0].Status)

	// A failure must not populate the cache; the next run calls live again.
	delete(f.failFor, "m")
	r2 := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	assert.False(t, r2[0].CacheHit, "a failed review must never be cached")
	assert.Equal(t, 2, f.callCount("m"))
}

// TestEngine_NoCacheKeyIsNeverCached: an agent with no payload digest (e.g. a
// directly-constructed Agent) bypasses the cache entirely even when a store is
// wired, so it always calls live.
func TestEngine_NoCacheKeyIsNeverCached(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	slot := Slot{Primary: Agent{Name: "x", Invocation: llmclient.Invocation{Model: "m"}}}

	NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	r := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	assert.False(t, r[0].CacheHit)
	assert.Equal(t, 2, f.callCount("m"), "an agent without a cache key always calls live")
}

// countingProvider is a mock OpenAI server that counts the chat-completion
// requests it serves, so a test can assert a second review made zero live calls.
func countingProvider(t *testing.T, hits *int64) *httptest.Server {
	t.Helper()
	resetBreakers(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(hits, 1)
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		content := "CRITICAL|auth.go:3|Unchecked call|Guard it|security|15|b() unchecked"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRunReview_SecondRunServedFromCache is the end-to-end proof of Epic 5.2:
// re-running a review over an unchanged diff makes zero LLM calls because every
// agent's output is replayed from .atcr/cache.
func TestRunReview_SecondRunServedFromCache(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	var hits int64
	srv := countingProvider(t, &hits)
	cfg := twoAgentConfig(srv.URL)

	// First run: cold cache -> one live call per agent (two agents).
	_, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	assert.Equal(t, int64(2), atomic.LoadInt64(&hits), "cold run calls each agent live")

	// Second run, same repo/range/root -> everything served from .atcr/cache.
	res2, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.Equal(t, 2, res2.Summary.Succeeded, "cached agents still count as succeeded")
	assert.Equal(t, int64(2), atomic.LoadInt64(&hits), "warm run makes no new live calls")

	assert.DirExists(t, filepath.Join(repo, ".atcr", "cache"))
}

// TestRunReview_NoCacheRequestStillCallsLive verifies the request-level
// --no-cache wiring: NoCache=true bypasses the warm cache and calls live.
func TestRunReview_NoCacheRequestStillCallsLive(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := initRepo(t)
	var hits int64
	srv := countingProvider(t, &hits)
	cfg := twoAgentConfig(srv.URL)

	_, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	require.Equal(t, int64(2), atomic.LoadInt64(&hits))

	noCacheReq := reviewReq(repo, repo, base, head)
	noCacheReq.NoCache = true
	_, err = RunReview(context.Background(), llmclient.New(), cfg, noCacheReq)
	require.NoError(t, err)
	assert.Equal(t, int64(4), atomic.LoadInt64(&hits), "--no-cache bypasses the warm cache and calls live")
}

// TestEngine_ToolAgentNeverCached locks the Epic 5.2 scope boundary: a
// tool-enabled agent (here degraded to single-shot because the fake completer is
// not a ChatCompleter) must always call live and never replay from cache, since
// its output depends on live code reads, not just the payload.
func TestEngine_ToolAgentNeverCached(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	slot := cacheableSlot("reviewer", "m", "the prompt")
	slot.Primary.Tools = true // routes through invokeDegraded, bypassing the cache

	r1 := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	require.Equal(t, StatusOK, r1[0].Status)
	assert.False(t, r1[0].CacheHit)
	assert.True(t, r1[0].ToolsDegraded, "tool agent degraded without a ChatCompleter")

	r2 := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
	assert.False(t, r2[0].CacheHit, "a tool agent must never replay from cache")
	assert.Equal(t, 2, f.callCount("m"), "tool agent calls live every run")
}

var assertFailErr = errAssertFail{}

type errAssertFail struct{}

func (errAssertFail) Error() string { return "synthetic failure" }

// Sprint 35.16.11.2.1 (TD-003): response_format changes the response — a JSON-mode
// reply is a bare object, not a fenced array — so declaring or removing it must
// not replay a review cached in the other mode. Unset collapses to the
// pre-existing key, so no on-disk entry written before the field existed is
// invalidated.
func TestDiffCacheKey_ResponseFormatChangesTheKey(t *testing.T) {
	base := diffCacheKey("p", "m", "", nil, "", defaultMaxTokens, "", "", "", "")
	declared := diffCacheKey("p", "m", "", nil, "", defaultMaxTokens, "json_object", "", "", "")
	assert.NotEqual(t, base, declared, "declaring response_format must miss the undeclared entry")
	assert.Equal(t, cache.Key(cache.HashText("p"), "m", "default"), base,
		"an undeclared agent keeps its pre-existing on-disk key")

	cfg := toolCfg()
	payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}
	before, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	g := cfg.Registry.Agents["greta"]
	g.ResponseFormat = registry.ResponseFormatJSONObject
	cfg.Registry.Agents["greta"] = g
	after, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
	require.NoError(t, err)
	assert.NotEqual(t, before.CacheKey, after.CacheKey, "the built agent's key follows its own declaration")
	// NotEqual alone cannot see the response_format ARGUMENT: declaring json_object
	// also swaps the ## Output Format section, so the hashed prompt differs too, and
	// a diffCacheKey call site that drops the suffix stays green. Recompute the key
	// from the built prompt with and without the declaration — only the suffixed
	// form may match, which pins the wiring at review.go's call site.
	sizing := fmt.Sprintf("%d:%d", after.EffectiveBudget, after.chunkMaxLines)
	assert.Equal(t, after.CacheKey,
		diffCacheKey(after.Prompt, after.Invocation.Model, after.Invocation.BaseURL,
			after.Invocation.Temperature, sizing, after.ResolvedMaxTokens, registry.ResponseFormatJSONObject, "", "", ""),
		"the built key is the response_format-suffixed form of this exact prompt")
	assert.NotEqual(t, after.CacheKey,
		diffCacheKey(after.Prompt, after.Invocation.Model, after.Invocation.BaseURL,
			after.Invocation.Temperature, sizing, after.ResolvedMaxTokens, "", "", "", ""),
		"the same prompt without the suffix must produce a different key")
}

// TD-005: the fallback's cache key follows the FALLBACK's own response_format and
// ignores the primary's, like its Invocation does.
func TestDiffCacheKey_FallbackKeysOnItsOwnResponseFormat(t *testing.T) {
	build := func(primaryRF, fallbackRF string) Agent {
		cfg := toolCfg()
		g := cfg.Registry.Agents["greta"]
		g.ResponseFormat = primaryRF
		cfg.Registry.Agents["greta"] = g
		k := cfg.Registry.Agents["kai"]
		k.ResponseFormat = fallbackRF
		cfg.Registry.Agents["kai"] = k
		payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}
		primary, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
		require.NoError(t, err)
		fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
		require.NoError(t, err)
		return fb
	}
	const jo = registry.ResponseFormatJSONObject
	assert.NotEqual(t, build("", "").CacheKey, build("", jo).CacheKey, "declaring the fallback must change its key")
	assert.Equal(t, build("", "").CacheKey, build(jo, "").CacheKey, "declaring only the primary must not change the fallback's key")

	// Same separation as the primary test above: the fallback prompt hash changes
	// with the swap, so only a recomputed same-prompt comparison pins the suffix
	// argument at the fallback call site.
	fb := build("", jo)
	fbSizing := fmt.Sprintf("%d:%d", fb.EffectiveBudget, fb.chunkMaxLines)
	assert.Equal(t, fb.CacheKey,
		diffCacheKey(fb.Prompt, fb.Invocation.Model, fb.Invocation.BaseURL,
			fb.Invocation.Temperature, fbSizing, fb.ResolvedMaxTokens, jo, "", "", ""),
		"the fallback key is the response_format-suffixed form of its own prompt")
	assert.NotEqual(t, fb.CacheKey,
		diffCacheKey(fb.Prompt, fb.Invocation.Model, fb.Invocation.BaseURL,
			fb.Invocation.Temperature, fbSizing, fb.ResolvedMaxTokens, "", "", "", ""),
		"the fallback's own prompt without the suffix must produce a different key")
}

// Sprint 35.16.11.2.2 AC 04-01: each declared thinking key folds into the tuning
// token as its own NUL-separated clause after rf=, in th/tl/ts order. An empty
// key appends nothing, so an undeclared agent keeps its pre-existing on-disk key.
func TestDiffCacheKey_ThinkingTokens(t *testing.T) {
	hash := cache.HashText("p")
	cases := []struct {
		name                 string
		rf, th, level, style string
		want                 string
	}{
		{"undeclared keeps the pre-existing key", "", "", "", "", "default"},
		{"thinking alone", "", "off", "", "", "default\x00th=off"},
		{"level and style without thinking", "", "", "low", "reasoning_effort", "default\x00tl=low\x00ts=reasoning_effort"},
		{"all three after rf", "json_object", "on", "low", "anthropic",
			"default\x00rf=json_object\x00th=on\x00tl=low\x00ts=anthropic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diffCacheKey("p", "m", "", nil, "", defaultMaxTokens, tc.rf, tc.th, tc.level, tc.style)
			assert.Equal(t, cache.Key(hash, "m", tc.want), got)
		})
	}
}

// withThinking returns cfg with agent name's thinking keys set.
//
// For an anthropic declaration with thinking on it also reconciles the agent's
// other keys to what validateThinking accepts at load (temperature 1 or unset,
// supports_function_calling false, and a max_tokens above the level's budget), so
// the fixture is a config a registry would actually load instead of one the load
// rejects on two counts. Assertions on Thinking/level/style and cache-key tokens
// are unaffected.
func withThinking(cfg *ReviewConfig, name, thinking, level, style string) {
	a := cfg.Registry.Agents[name]
	a.Thinking, a.ThinkingLevel, a.ThinkingStyle = thinking, level, style
	if style == registry.ThinkingStyleAnthropic && (thinking == registry.ThinkingOn || level != "") {
		a.Temperature = nil
		a.SupportsFC = false
		if budget := registry.ThinkingBudgetTokens(thinking, level, style); budget > 0 && (a.MaxTokens == nil || *a.MaxTokens <= budget) {
			mt := budget * 2
			a.MaxTokens = &mt
		}
	}
	cfg.Registry.Agents[name] = a
}

// Sprint 35.16.11.2.2 AC 04-01: the primary's Invocation carries its own
// declaration verbatim, and its cache key is the thinking-suffixed form of its
// own prompt. The recompute pins the arguments at review.go's call site: a
// thinking declaration does not change the prompt, so a site that drops the
// suffix would otherwise still produce a stable key.
func TestRenderAgent_PrimaryCarriesAndKeysOnItsOwnThinking(t *testing.T) {
	payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}
	build := func(thinking, level, style string) Agent {
		cfg := toolCfg()
		withThinking(cfg, "greta", thinking, level, style)
		a, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
		require.NoError(t, err)
		return a
	}
	recompute := func(a Agent, thinking, level, style string) string {
		sizing := fmt.Sprintf("%d:%d", a.EffectiveBudget, a.chunkMaxLines)
		return diffCacheKey(a.Prompt, a.Invocation.Model, a.Invocation.BaseURL, a.Invocation.Temperature,
			sizing, a.ResolvedMaxTokens, a.Invocation.ResponseFormat, thinking, level, style)
	}

	declared := build("on", "low", "anthropic")
	assert.Equal(t, "on", declared.Invocation.Thinking)
	assert.Equal(t, "low", declared.Invocation.ThinkingLevel)
	assert.Equal(t, "anthropic", declared.Invocation.ThinkingStyle)
	assert.Equal(t, recompute(declared, "on", "low", "anthropic"), declared.CacheKey,
		"the primary keys on its own thinking declaration")
	assert.NotEqual(t, recompute(declared, "", "", ""), declared.CacheKey)

	undeclared := build("", "", "")
	assert.Empty(t, undeclared.Invocation.Thinking)
	assert.Empty(t, undeclared.Invocation.ThinkingLevel)
	assert.Empty(t, undeclared.Invocation.ThinkingStyle)
	assert.Equal(t, recompute(undeclared, "", "", ""), undeclared.CacheKey,
		"an undeclared primary keeps the byte-identical pre-existing key")
}

// Sprint 35.16.11.2.2 AC 04-02: the fallback sends and keys on ITS OWN thinking
// declaration, never its primary's, in both directions.
func TestBuildFallbackAgent_CarriesAndKeysOnItsOwnThinking(t *testing.T) {
	type decl struct{ thinking, level, style string }
	build := func(primary, fallback decl) Agent {
		cfg := toolCfg()
		withThinking(cfg, "greta", primary.thinking, primary.level, primary.style)
		withThinking(cfg, "kai", fallback.thinking, fallback.level, fallback.style)
		payloads := map[string]modePayload{"blocks": {Text: "x", FileCount: 1}}
		p, _, err := buildOneAgent(cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
		require.NoError(t, err)
		fb, _, err := buildFallbackAgent(cfg, p, "kai", true, fallbackRefit{})
		require.NoError(t, err)
		return fb
	}
	recompute := func(fb Agent, d decl) string {
		sizing := fmt.Sprintf("%d:%d", fb.EffectiveBudget, fb.chunkMaxLines)
		return diffCacheKey(fb.Prompt, fb.Invocation.Model, fb.Invocation.BaseURL, fb.Invocation.Temperature,
			sizing, fb.ResolvedMaxTokens, fb.Invocation.ResponseFormat, d.thinking, d.level, d.style)
	}
	primaryDecl := decl{"on", "high", "anthropic"}
	fallbackDecl := decl{"off", "", "template_kwargs"}

	t.Run("divergent declarations", func(t *testing.T) {
		fb := build(primaryDecl, fallbackDecl)
		assert.Equal(t, "off", fb.Invocation.Thinking)
		assert.Empty(t, fb.Invocation.ThinkingLevel, "the primary's level must not leak")
		assert.Equal(t, "template_kwargs", fb.Invocation.ThinkingStyle)
		assert.Equal(t, recompute(fb, fallbackDecl), fb.CacheKey)
	})
	t.Run("primary declares, fallback does not", func(t *testing.T) {
		fb := build(primaryDecl, decl{})
		assert.Empty(t, fb.Invocation.Thinking)
		assert.Empty(t, fb.Invocation.ThinkingLevel)
		assert.Empty(t, fb.Invocation.ThinkingStyle)
		assert.Equal(t, recompute(fb, decl{}), fb.CacheKey, "no primary-only thinking token in the fallback key")
	})
	t.Run("fallback declares, primary does not", func(t *testing.T) {
		fb := build(decl{}, fallbackDecl)
		assert.Equal(t, "off", fb.Invocation.Thinking)
		assert.Equal(t, "template_kwargs", fb.Invocation.ThinkingStyle)
		assert.Equal(t, recompute(fb, fallbackDecl), fb.CacheKey)
		assert.NotEqual(t, recompute(fb, decl{}), fb.CacheKey)
	})
}

// TestWithThinkingFixtures_AreLoadable pins the sprint's thinking fixtures to the
// real load validator. The anthropic-thinking-on fixtures set greta's declaration
// while her roster config carries Temperature 0.7 and (in toolCfg) SupportsFC true
// — a combination validateThinking rejects at load on two counts. These tests
// exercised wiring for a config that could never load; this test fails until the
// helper (or the roster) makes the declaration match what a registry load accepts.
func TestWithThinkingFixtures_AreLoadable(t *testing.T) {
	decls := []struct{ thinking, level, style string }{
		{"on", "low", "anthropic"},  // TestRenderAgent_PrimaryCarriesAndKeysOnItsOwnThinking
		{"on", "high", "anthropic"}, // TestBuildFallbackAgent_CarriesAndKeysOnItsOwnThinking + the refit arm
	}
	rosters := map[string]*ReviewConfig{
		"toolCfg": toolCfg(),
		"sizing":  declaredWindowRoster(t, 128000),
	}
	for rname, cfg := range rosters {
		for _, d := range decls {
			withThinking(cfg, "greta", d.thinking, d.level, d.style)
			data, err := yaml.Marshal(cfg.Registry.Agents["greta"])
			require.NoError(t, err)
			err = registry.ValidateAgentYAML("greta", data)
			assert.NoError(t, err, "roster %s: greta with thinking=%q level=%q style=%q must be a loadable declaration; validateAgent said: %v", rname, d.thinking, d.level, d.style, err)
		}
	}
}
