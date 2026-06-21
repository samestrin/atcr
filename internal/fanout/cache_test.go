package fanout

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/cache"
	"github.com/samestrin/atcr/internal/circuitbreaker"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cacheableSlot builds a non-serial, non-tool slot whose primary carries the
// payload/persona digests the cache keys on.
func cacheableSlot(name, model, payload, persona string) Slot {
	return Slot{Primary: Agent{
		Name:        name,
		PayloadMode: "blocks",
		PayloadHash: cache.HashText(payload),
		PersonaHash: cache.HashText(persona),
		Invocation:  llmclient.Invocation{Model: model},
	}}
}

func TestEngine_CacheHitReplaysWithoutAPICall(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	slot := cacheableSlot("reviewer", "m", "the diff payload", "the persona")

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
	slot := cacheableSlot("reviewer", "m", "the diff payload", "the persona")

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
		[]Slot{cacheableSlot("a", "m1", "same payload", "same persona")})

	// Same payload+persona but a different model -> distinct key -> live call.
	r := NewEngine(f, WithCache(store, false)).Run(context.Background(),
		[]Slot{cacheableSlot("b", "m2", "same payload", "same persona")})
	assert.False(t, r[0].CacheHit)
	assert.Equal(t, 1, f.callCount("m2"))
}

func TestEngine_FailedReviewIsNotCached(t *testing.T) {
	store := cache.NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	f := newFake()
	f.failFor["m"] = assertFailErr
	slot := cacheableSlot("reviewer", "m", "p", "persona")

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
	circuitbreaker.DefaultRegistry.Reset()
	t.Cleanup(circuitbreaker.DefaultRegistry.Reset)
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
	slot := cacheableSlot("reviewer", "m", "payload", "persona")
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
