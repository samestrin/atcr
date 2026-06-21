Let me analyze the diff carefully against the sprint plan scope.

The sprint plan is about implementing diff caching for incremental reviews. The key components are:
1. Cache package (key derivation, store)
2. Integration into fan-out read/write path
3. `--no-cache` flag
4. Size-based LRU eviction with config override
5. Cache settings in registry/project config

Let me review each file in scope:

**internal/cache/key.go** - New file for cache key derivation
- `HashText` returns sha256 hex digest
- `Key` function joins promptHash, model, and tuning with NUL separator

Looking at the `Key` function:
```go
func Key(promptHash, model, tuning string) string {
	h := sha256.New()
	h.Write([]byte(promptHash))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(tuning))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
```

The NUL separator is used to prevent boundary ambiguity. But wait - the NUL byte could appear in `model` or `tuning` strings. The comment says "which cannot appear in a hex digest, a model id, or a numeric tuning token" - but model IDs could theoretically contain NUL bytes. However, this is extremely unlikely in practice and the test `TestKey_DomainSeparation` covers the basic case. This is a minor concern but not a real issue since model IDs and tuning tokens are controlled inputs.

**internal/cache/store.go** - New file for cache storage

Looking at the `Store`:
- `Get` method reads from disk, validates key match, refreshes mtime
- `Put` method writes atomically, then evicts
- `evict` method does LRU eviction based on mtime

Potential issues:

1. In `Get`, the mtime refresh happens inside the mutex, which is good. But there's a TOCTOU concern - between reading the file and refreshing mtime, another goroutine could have evicted it. But since we hold the mutex, this is actually safe.

2. In `evict`, it reads directory entries and their info. The `de.Info()` call could fail for entries that were deleted between `ReadDir` and `Info()`, but it's handled with `continue`.

3. The `fileName` function validates hex64 to prevent path traversal - good security practice.

4. Looking at `Get` more carefully:
```go
func (s *Store) Get(key string) (string, bool, error) {
	...
	s.mu.Lock()
	defer s.mu.Unlock()
	...
	_ = os.Chtimes(path, now, now)
	return e.Content, true, nil
}
```
The mtime refresh is best-effort, which is documented. This is fine.

5. In `Put`:
```go
func (s *Store) Put(key, content string) error {
	...
	s.mu.Lock()
	defer s.mu.Unlock()
	...
	if err := atomicfs.WriteJSON(path, entry{Key: key, Content: content}); err != nil {
		return err
	}
	s.evict()
	return nil
}
```
The eviction happens after the write. If the write succeeds but eviction fails (e.g., ReadDir error), it's best-effort. This is documented and acceptable.

6. One potential issue: in `evict`, the method reads ALL entries and sorts them. For a large cache (50MB), this could be slow if there are many small files. But with 50MB cap and typical review outputs being a few KB each, this is at most ~10,000 files, which is manageable.

**internal/fanout/engine.go** - Cache integration

Looking at `invokeCachedSingleShot`:
```go
func (e *Engine) invokeCachedSingleShot(ctx context.Context, a Agent) Result {
	if e.cache == nil || a.CacheKey == "" {
		return e.invokeSingleShot(ctx, a)
	}
	key := a.CacheKey
	if !e.cacheNoRead {
		content, hit, err := e.cache.Get(key)
		if err != nil {
			log.FromContext(ctx).Warn("diff cache read failed; calling live", "agent", a.Name, "err", err)
		} else if hit {
			...
			return Result{...CacheHit: true}
		}
	}
	r := e.invokeSingleShot(ctx, a)
	if r.Status == StatusOK {
		if err := e.cache.Put(key, r.Content); err != nil {
			log.FromContext(ctx).Warn("diff cache write failed", "agent", a.Name, "err", err)
		}
	}
	return r
}
```

This looks correct. Cache hits return a synthesized result, misses call live and store on success.

One thing I notice: the `Result` returned on cache hit doesn't include `FallbackFrom` or other fields that might be relevant. But since a cache hit means the same prompt+model was used before, this should be fine.

Wait, looking more carefully at the cache hit result:
```go
return Result{
	Agent:       a.Name,
	Content:     content,
	Status:      StatusOK,
	PayloadMode: a.PayloadMode,
	Truncation:  a.Truncation,
	MinSeverity: a.MinSeverity,
	MaxFindings: a.MaxFindings,
	Model:       a.Invocation.Model,
	CacheHit:    true,
}
```

This doesn't set `TokensIn`, `TokensOut`, or `CallRecords`. The comment says "zero token usage and no CallRecords" which is correct for a cache hit. But could this cause issues downstream if something expects these fields to be populated? Looking at the status tracking, `statusFor` copies `CacheHit` from the result, and the metrics wrapper should handle zero values gracefully.

**internal/fanout/review.go** - Cache wiring in review path

Looking at `diffCacheKey`:
```go
func diffCacheKey(prompt, model string, temperature *float64) string {
	temp := "default"
	if temperature != nil {
		temp = strconv.FormatFloat(*temperature, 'g', -1, 64)
	}
	return cache.Key(cache.HashText(prompt), model, temp)
}
```

This looks correct. The temperature is converted to a string for the key.

Looking at `buildAgent`:
```go
CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature),
```

And `buildFallbackAgent`:
```go
CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```

Wait - in `buildFallbackAgent`, it uses `primary.Prompt` but the fallback agent's own `ac.Model` and `ac.Temperature`. This is correct per the comment: "a fallback reviews the SAME rendered prompt as the primary but on its OWN model and temperature."

But wait - does the fallback agent actually use the same prompt? Let me check... The comment says "a fallback reviews the SAME rendered prompt as the primary" - this is a design decision. If the fallback uses a different prompt, this would be a bug. But looking at the code, `primary.Prompt` is the rendered prompt, and the fallback agent uses the same prompt text. So this seems correct.

Actually, I need to look more carefully. In `buildFallbackAgent`, does the fallback agent actually use `primary.Prompt`? Let me look at the Invocation:

```go
Invocation: llmclient.Invocation{
    BaseURL:     prov.BaseURL,
    APIKeyEnv:   prov.APIKeyEnv,
    ...
}
```

I don't see the Prompt field being set in the Invocation shown in the diff. But this is a partial view - the full function likely sets it. The cache key uses `primary.Prompt`, which should match what the fallback actually sends. If the fallback modifies the prompt (e.g., adds a note about being a fallback), the cache key would be wrong. But the comment says it reviews the SAME rendered prompt, so this should be fine.

**internal/registry/precedence.go** - Settings resolution

Looking at the CacheMaxBytes resolution:
```go
if reg != nil {
    ...
    if reg.CacheMaxBytes != nil {
        s.CacheMaxBytes = *reg.CacheMaxBytes
    }
    ...
}
if proj != nil {
    ...
    if proj.CacheMaxBytes != nil {
        s.CacheMaxBytes = *proj.CacheMaxBytes
    }
}
```

This correctly applies registry first, then project overrides. The pointer pattern ensures explicit 0 survives. This looks correct.

**internal/registry/config.go** - Registry validation

```go
if r.CacheMaxBytes != nil && *r.CacheMaxBytes < 0 {
    errs = append(errs, fmt.Errorf("cache_max_bytes must be >= 0 (0 = unbounded), got %d", *r.CacheMaxBytes))
}
```

This validates negative values. Correct.

**internal/registry/project.go** - Project config

```go
if cfg.CacheMaxBytes != nil && *cfg.CacheMaxBytes < 0 {
    return nil, fmt.Errorf("%s: cache_max_bytes must be >= 0 (0 = unbounded)", base)
}
```

Correct validation.

Now let me look for actual issues:

1. **Race condition in Store.Get + Store.Put**: Both methods acquire `s.mu`, so there's no race. Good.

2. **Error handling in Store**: The `Get` method swallows corrupt entry errors (returns miss, not error). This is documented as intentional self-healing. Fine.

3. **Path traversal protection**: `fileName` validates hex64. Good.

4. **LRU eviction correctness**: The eviction sorts by mtime and removes oldest first. `Get` refreshes mtime. This is correct LRU behavior.

5. **Cache key for fallback agents**: Uses `primary.Prompt` with fallback's model/temp. This seems correct per the design.

6. **Tool agents not cached**: The code explicitly routes tool agents through `invokeDegraded` or `invokeToolLoop`, bypassing `invokeCachedSingleShot`. Correct.

7. **--no-cache semantics**: `cacheNoRead` bypasses reads but still writes. Looking at `invokeCachedSingleShot`:
```go
if !e.cacheNoRead {
    // read from cache
}
r := e.invokeSingleShot(ctx, a)
if r.Status == StatusOK {
    // write to cache
}
```
This correctly implements "bypass read, still write". Good.

8. **Resume path**: In `resume.go`, the cache is wired for resumed agents too:
```go
cache:       cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes),
cacheNoRead: req.NoCache,
```
This looks correct - resumed agents also benefit from cache writes.

Let me look for any actual bugs or issues...

Looking at `internal/cache/store.go` more carefully:

The `evict` method:
```go
func (s *Store) evict() {
	if s.maxBytes <= 0 {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	...
	for _, f := range files {
		if total <= s.maxBytes {
			break
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
		}
	}
}
```

One issue: if `os.Remove` fails (e.g., permission denied), the entry stays but `total` doesn't decrease, so the loop continues to the next entry. This is fine - the entry will be retried on next eviction.

Another potential issue: the `evict` method is called after every `Put`. If many agents write concurrently (which they do in fan-out), each `Put` triggers a full directory scan + sort. But since `Put` holds the mutex, these are serialized. For a 50MB cache with typical review outputs, this should be fast enough.

Wait, actually there's a potential performance concern: `evict` reads ALL entries and sorts them on EVERY Put. If there are 1000 entries, that's 1000 stat calls + a sort on every write. But the sprint plan says "contention is negligible in practice — a review fans out to a handful of agents" so this is acceptable.

Let me look at the `fileNameFor` function:
```go
func fileNameFor(key string) string {
	name, ok := fileName(key)
	if !ok {
		panic("cache: malformed key in test: " + key)
	}
	return name
}
```

This is a test-only helper that panics on malformed keys. It's only used in tests. Fine.

Now let me check if there are any issues I'm missing...

Looking at `internal/fanout/cache_test.go`:
```go
func cacheableSlot(name, model, prompt string) Slot {
	return Slot{Primary: Agent{
		Name:        name,
		PayloadMode: "blocks",
		CacheKey:    diffCacheKey(prompt, model, nil),
		Invocation:  llmclient.Invocation{Model: model, Prompt: prompt},
	}}
}
```

This test helper creates a slot with a cache key. The tests look comprehensive.

Looking at `TestEngine_DifferentTemperatureMissesCache`:
```go
hot, cold := 0.9, 0.1
mk := func(temp *float64) Slot {
    return Slot{Primary: Agent{
        ...
        CacheKey:    diffCacheKey("same prompt", "m", temp),
        Invocation:  llmclient.Invocation{Model: "m", Prompt: "same prompt", Temperature: temp},
    }}
}
```

This tests that different temperatures produce different cache keys. Good.

Now, let me think about edge cases and potential issues:

1. **Empty prompt**: If `prompt` is empty, `HashText("")` still produces a valid hash. The cache would store an entry for an empty prompt. This is technically correct but unlikely in practice.

2. **Concurrent access to the same key**: Two agents with the same cache key would both try to `Put` the same file. Since `Put` uses `atomicfs.WriteJSON` and holds the mutex, this is safe - the second write overwrites the first.

3. **Cache directory permissions**: `os.MkdirAll(s.dir, 0o755)` creates the directory with 0o755. This is fine for a local cache.

4. **Large cache entries**: If a single entry exceeds `maxBytes`, it will be written and then immediately evicted on the next `Put`. This is wasteful but not incorrect.

Actually, wait - let me re-examine the eviction logic:
```go
if total <= s.maxBytes {
    return
}
```
After writing a new entry, if total > maxBytes, it evicts. But the newly written entry could be the one evicted if it's the oldest (which it shouldn't be since it was just written). Since `Put` writes the file and then calls `evict`, the new file's mtime is the most recent, so it won't be evicted first. Good.

But what if the new entry alone exceeds `maxBytes`? The eviction loop would evict everything else and still be over the cap, but the new entry stays because it has the newest mtime. This is the correct behavior - you don't want to evict the entry you just wrote.

Let me look for any security issues...

The `fileName` function validates that the key is a proper sha256 hex digest before using it as a filename. This prevents path traversal. Good.

The cache stores reviewer output, which is text from the LLM. This is stored as JSON and read back. No injection risk since it's just stored as a string value in JSON.

Let me check if there are any issues with the `atomicfs.WriteJSON` call... I don't have the implementation, but based on the name and usage, it should write atomically (temp file + rename). This is the correct pattern for cache writes.

Now, let me look at one more thing - the `log.FromContext(ctx)` calls in `invokeCachedSingleShot`:
```go
log.FromContext(ctx).Warn("diff cache read failed; calling live", "agent", a.Name, "err", err)
```

This logs the error but continues. The error is not swallowed - it's logged and the code falls through to a live call. This is correct error handling for a cache - a cache failure should degrade gracefully.

Let me also check the `statusFor` function in `artifacts.go`:
```go
CacheHit: r.CacheHit,
```

This correctly propagates the `CacheHit` flag to the status. Good.

OK, I think I've found a few potential issues:

1. **In `internal/cache/store.go`, the `evict` method doesn't handle the case where `de.Info()` returns an error that's not `fs.ErrNotExist`**. Actually, it does - it uses `continue` to skip entries where `Info()` fails. This is best-effort and documented.

2. **In `internal/fanout/engine.go`, the cache hit result doesn't set `FallbackFrom`**. But since a cache hit means the primary agent's output was cached, `FallbackFrom` should be empty, which is the default. Fine.

3. Looking more carefully at `buildFallbackAgent`:
```go
CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```

Wait, I need to check if `primary.Prompt` is actually set. Looking at the `Agent` struct, there's a `Prompt` field (implied by `primary.Prompt`). In `buildAgent`, the prompt is rendered and presumably stored in the Agent. But I don't see `Prompt` being set in the `Agent` struct in the diff... Let me look again.

Actually, looking at the diff more carefully, the `Agent` struct has fields like `Invocation` which contains `Prompt`. But `buildFallbackAgent` uses `primary.Prompt` - is this a field on `Agent` or on `Invocation`?

Looking at the code:
```go
CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```

And in `buildAgent`:
```go
CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature),
```

In `buildAgent`, `prompt` is a local variable (the rendered prompt). In `buildFallbackAgent`, it uses `primary.Prompt`. This implies `Agent` has a `Prompt` field. But I don't see it in the diff... It might be a pre-existing field. Let me check if this is correct.

Actually, looking at the `Invocation` struct, it has a `Prompt` field. So `primary.Prompt` might refer to `primary.Invocation.Prompt`. But the code says `primary.Prompt`, not `primary.Invocation.Prompt`. This could be:
1. A field on `Agent` that's not shown in the diff (pre-existing)
2. A bug where it should be `primary.Invocation.Prompt`

Looking at the `Agent` struct in the diff, I see fields like `Name`, `PayloadMode`, `CacheKey`, `Invocation`, etc. I don't see a `Prompt` field directly on `Agent`. But the diff only shows the added fields, not the full struct. So `Prompt` could be a pre-existing field on `Agent`.

Actually, wait - looking at the test:
```go
Invocation:  llmclient.Invocation{Model: model, Prompt: prompt},
```

The `Prompt` is on `Invocation`, not on `Agent` directly. So `primary.Prompt` in `buildFallbackAgent` might be a compilation error if `Agent` doesn't have a `Prompt` field. But since this is a diff that presumably compiles (there are tests), `Agent` must have a `Prompt` field. Let me assume it's a pre-existing field.

Hmm, but actually, looking at the `buildAgent` function more carefully:

```go
CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature),
Invocation: llmclient.Invocation{
    ...
    Prompt:     prompt,
    ...
},
```

So `prompt` is a local variable used both for the cache key and the invocation. In `buildFallbackAgent`, it uses `primary.Prompt` - if `Agent` has a `Prompt` field that mirrors `Invocation.Prompt`, this is correct. If not, it should be `primary.Invocation.Prompt`.

Since the code presumably compiles (there are tests), I'll assume `Agent.Prompt` exists. But this could be a maintainability issue - having both `Agent.Prompt` and `Agent.Invocation.Prompt` could lead to them getting out of sync.

Actually, I just realized I should focus on real issues, not speculative ones. Let me look for concrete bugs.

Let me re-examine the `Key` function:
```go
func Key(promptHash, model, tuning string) string {
	h := sha256.New()
	h.Write([]byte(promptHash))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(tuning))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
```

The NUL separator prevents boundary ambiguity between fields. But what if `model` contains a NUL byte? Then `model = "a\x00b"` would produce the same hash as `promptHash = "sha256:a"`, `model = "b"`, `tuning = ""`. However, model IDs are controlled inputs that don't contain NUL bytes, so this is not a real concern.

Let me look at the `diffCacheKey` function again:
```go
func diffCacheKey(prompt, model string, temperature *float64) string {
	temp := "default"
	if temperature != nil {
		temp = strconv.FormatFloat(*temperature, 'g', -1, 64)
	}
	return cache.Key(cache.HashText(prompt), model, temp)
}
```

The `prompt` is hashed first with `HashText`, then the hash (which is "sha256:<64hex>") is passed to `Key` along with `model` and `temp`. The `Key` function then hashes these together. This is correct - the prompt hash is a fixed-length, NUL-free string.

Wait, but `promptHash` passed to `Key` is `"sha256:<64hex>"` - it contains a colon. The NUL separator between `promptHash` and `model` prevents ambiguity. But what if `model` is `"sha256:..."` - could that cause issues? No, because the NUL separator ensures `promptHash\x00model\x00tuning` is unambiguous regardless of content (as long as no field contains NUL, which is true for hex digests and model IDs).

Actually, wait - the comment says "which cannot appear in a hex digest, a model id, or a numeric tuning token". But `promptHash` is `"sha256:<hex>"`, which contains a colon, not just hex. The NUL separator is between the full `promptHash` string (including the "sha256:" prefix) and the model. This is fine because NUL can't appear in any of these strings.

OK, I think the code is generally well-written. Let me look for any remaining issues...

One thing I notice: in `internal/fanout/review.go`, the `diffCacheKey` function is defined but I should check if it's used correctly in `buildAgent` and `buildFallbackAgent`.

In `buildAgent`:
```go
CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature),
```

In `buildFallbackAgent`:
```go
CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```

Wait, `primary.Prompt` - is this the rendered prompt or the raw prompt? In `buildAgent`, `prompt` is the rendered prompt (after persona resolution and template expansion). If `primary.Prompt` stores the rendered prompt, then the fallback's cache key correctly uses the same rendered prompt. But if `primary.Prompt` is something else, this could be a bug.

Since I can't see the full `Agent` struct, I'll assume `Prompt` stores the rendered prompt. This seems correct based on the comment.

Let me check one more thing - the `PrepareResume` function:
```go
cache:       cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes),
cacheNoRead: req.NoCache,
```

This creates a new store for the resume path. The store is rooted at the same `.atcr/cache` directory, so cached entries from prior runs are available. This is correct.

But wait - in `PrepareReview`:
```go
revCache := cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes)
return &PreparedReview{..., cache: revCache, cacheNoRead: req.NoCache}, nil
```

And in `PrepareResume`:
```go
cache:       cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes),
cacheNoRead: req.NoCache,
```

Both create a new store pointing to the same directory. This is fine - they're used in different code paths (fresh review vs. resume).

Let me now look for any issues with the test files...

In `internal/fanout/cache_test.go`:
```go
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
```

This test verifies the end-to-end caching behavior. It looks correct.

But wait - `reviewReq(repo, repo, base, head)` - what does this function return? It's a test helper that creates a `ReviewRequest`. The first `repo` is probably the repo path and the second is the root. If `Root` is set to `repo`, then the cache directory is `<repo>/.atcr/cache`. This is correct.

Actually, I just realized something. The test `TestRunReview_SecondRunServedFromCache` asserts that the second run makes no new live calls. But for this to work, the cache key must be the same across runs. The cache key is derived from the rendered prompt, model, and temperature. If the prompt includes any non-deterministic elements (like timestamps or random IDs), the cache would miss. But since the test passes (presumably), the prompt must be deterministic.

Let me think about whether there are any real issues to report...

Actually, I think I found one potential issue. In `internal/cache/store.go`, the `Get` method:

```go
func (s *Store) Get(key string) (string, bool, error) {
	name, ok := fileName(key)
	if !ok {
		return "", false, nil // malformed key — treat as a miss
	}
	path := filepath.Join(s.dir, name)

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(path)
	...
}
```

The `fileName` call is outside the mutex. This is fine because `fileName` is a pure function with no shared state. But it's a minor inconsistency - the path computation is outside the lock while the file access is inside. Not a bug, just a style note.

Let me look for any actual correctness issues...

Actually, I want to look at one more thing. In `invokeCachedSingleShot`:

```go
if r.Status == StatusOK {
    if err := e.cache.Put(key, r.Content); err != nil {
        log.FromContext(ctx).Warn("diff cache write failed", "agent", a.Name, "err", err)
    }
}
```

Only `StatusOK` results are cached. What about `StatusTruncated`? If a review was truncated (too many findings), should it be cached? Looking at the status constants, `StatusOK` is the success status. If `StatusTruncated` is a different status, truncated reviews wouldn't be cached, which might be intentional (truncated results might not be complete). But I don't have enough context to determine if this is a bug.

Actually, looking at the sprint plan: "Cache unit = per-agent raw review output (fanout Result.Content)". The plan says to cache the raw review output. If a review is truncated, the raw output is still valid - it's just shorter. But the code only caches `StatusOK` results. This might be intentional - a truncated result might indicate a problem that should be re-reviewed.

Hmm, but looking at the code more carefully, `StatusOK` might include truncated results. Let me check... Actually, I don't have the full status definitions, so I can't be sure. The test `TestEngine_FailedReviewIsNotCached` only tests `StatusFailed`, not `StatusTruncated`. This could be a gap, but it's not clearly a bug.

Let me look at one more potential issue. In the `evict` method:

```go
sort.Slice(files, func(i, j int) bool { return files[i].mtime.Before(files[j].mtime) })
```

If two files have the same mtime (possible on filesystems with low timestamp resolution), the sort order is unspecified. This means either could be evicted first. This is not a bug - LRU with equal timestamps is inherently ambiguous, and either choice is valid.

OK, I think I've been thorough enough. Let me look for any remaining issues...

Actually, I just noticed something in `internal/fanout/engine.go`:

```go
// invokeCachedSingleShot wraps the single-shot path with the diff cache (Epic
// 5.2). It is the only cache integration point: tool agents (live or degraded)
// never reach it.
```

But looking at `dispatchAgent`:
```go
func (e *Engine) dispatchAgent(ctx context.Context, a Agent) Result {
    ...
    if a.Tools {
        if e.dispatcher != nil && e.chatCompleter != nil {
            return e.invokeToolLoop(ctx, a, cc, e.dispatcher)
        }
        // A degraded tool agent runs single-shot but is intentionally NOT cached
        return e.invokeDegraded(ctx, a)
    }
    return e.invokeCachedSingleShot(ctx, a)
}
```

Wait, the degraded tool agent calls `invokeDegraded`, not `invokeCachedSingleShot`. So degraded tool agents are NOT cached. This is correct per the comment. But what about the `invokeDegraded` function - does it call `invokeSingleShot` internally? If so, it bypasses the cache wrapper. Let me check...

I don't see `invokeDegraded` in the diff, so it's a pre-existing function. Based on the comment, it runs single-shot but is not cached. This is correct.

Let me now check if there are any issues with the `--no-cache` flag wiring...

In `cmd/atcr/review.go`:
```go
cmd.Flags().Bool("no-cache", false, "bypass the diff cache read and force a fresh review; fresh results are still written back to .atcr/cache")
```

And:
```go
NoCache:    boolFlag(cmd, "no-cache"),
```

This correctly wires the `--no-cache` flag to the `ReviewRequest.NoCache` field. Good.

In `internal/fanout/review.go`:
```go
NoCache bool
```

And in `PrepareReview`:
```go
cacheNoRead: req.NoCache,
```

This correctly passes the `NoCache` flag to the engine. Good.

OK, I think I've done a thorough review. Let me summarize the issues I found:

1. Actually, I'm not sure I found any real issues. The code looks well-written and follows the sprint plan's design decisions. Let me look one more time...

Wait, I think I see a potential issue. In `internal/cache/store.go`, the `evict` method:

```go
func (s *Store) evict() {
	if s.maxBytes <= 0 {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return // best-effort; reclaim on a later Put
	}
	...
	for _, f := range files {
		if total <= s.maxBytes {
			break
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
		}
	}
}
```

If `os.ReadDir` fails (e.g., directory doesn't exist yet), the method returns silently. But `Put` creates the directory with `os.MkdirAll` before calling `evict`, so the directory should exist. If `MkdirAll` succeeds but `ReadDir` fails, that would be unusual. This is best-effort and documented.

Actually, wait. Looking at `Put`:
```go
func (s *Store) Put(key, content string) error {
	...
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.dir, name)
	if err := atomicfs.WriteJSON(path, entry{Key: key, Content: content}); err != nil {
		return err
	}
	s.evict()
	return nil
}
```

`MkdirAll` is called inside the mutex, then `WriteJSON`, then `evict`. The directory is guaranteed to exist when `evict` is called. Good.

Hmm, let me look at one more thing. The `atomicfs.WriteJSON` function - does it create the file with appropriate permissions? I don't have the implementation, but based on the codebase pattern, it should. The cache files contain reviewer output, which is not sensitive (it's the same content that would be in the review artifacts).

OK, I think I need to look at this from a different angle. Let me check for any issues with the test coverage...

The tests cover:
- Key derivation (determinism, format, domain separation)
- Store put/get round-trip
- Distinct keys don't collide
- Miss on unknown key
- Corrupt entry self-heals
- LRU eviction
- Get refreshes recency
- Malformed key rejection (path traversal)
- Concurrent put/get
- Cache hit replays without API call
- --no-cache bypasses read but still writes
- Different model misses cache
- Different prompt misses cache
- Different temperature misses cache
- Failed review is not cached
- No cache key is never cached
- Tool agent never cached
- End-to-end second run served from cache
- --no-cache request still calls live

This is comprehensive test coverage. I don't see any gaps that would indicate a bug.

Let me look one more time at the actual code for any subtle issues...

Actually, I want to look at the `Key` function's domain separation more carefully:

```go
func Key(promptHash, model, tuning string) string {
	h := sha256.New()
	h.Write([]byte(promptHash))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(tuning))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
```

The test `TestKey_DomainSeparation` checks:
```go
a := Key("aa", "bb", "cc")
b := Key("a", "abb", "cc")
c := Key("aa", "b", "bcc")
assert.NotEqual(t, a, b)
assert.NotEqual(t, a, c)
assert.NotEqual(t, b, c)
```

With NUL separators:
- a = hash("aa\x00bb\x00cc")
- b = hash("a\x00abb\x00cc")
- c = hash("aa\x00b\x00bcc")

These are indeed different because the NUL bytes create unambiguous boundaries. Good.

But wait - what if `promptHash` itself contains a NUL byte? `promptHash` is `"sha256:<64hex>"`, which doesn't contain NUL. And `model` and `tuning` are controlled inputs that don't contain NUL. So the domain separation is correct.

OK, I'm now fairly confident there are no critical issues. Let me look for any minor issues...

1. In `internal/cache/store.go`, the `fileNameFor` function is exported (lowercase, so actually unexported) but only used in tests. It's a test helper that panics on malformed keys. This is fine for test-only code.

2. In `internal/fanout/engine.go`, the `reviewCache` interface is defined with `Get` and `Put` methods. The `*cache.Store` type satisfies this interface. This is a clean abstraction that keeps the engine decoupled from the storage implementation.

3. In `internal/registry/precedence.go`, the `CacheMaxBytes` resolution follows the same pattern as other settings. The pointer pattern ensures explicit 0 survives default application. This is correct.

Actually, wait. I want to look at one more thing. In the `ResolveSettings` function:

```go
if reg != nil {
    applyTier(&s, reg.PayloadMode, reg.TimeoutSecs, reg.PayloadByteBudget, reg.MaxParallel)
    if reg.CacheMaxBytes != nil {
        s.CacheMaxBytes = *reg.CacheMaxBytes
    }
    ...
}
if proj != nil {
    applyTier(&s, proj.PayloadMode, proj.TimeoutSecs, proj.PayloadByteBudget, proj.Max