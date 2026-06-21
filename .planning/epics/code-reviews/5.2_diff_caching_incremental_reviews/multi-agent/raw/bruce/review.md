<think>
Let me analyze this diff systematically. The diff is about implementing diff caching for the review fan-out (Epic 5.2). Let me go through each file and check for issues.

Let me look at the key files:

1. `internal/cache/key.go` - Cache key derivation
2. `internal/cache/store.go` - Cache store implementation
3. `internal/fanout/engine.go` - Engine integration with cache
4. `internal/fanout/review.go` - Review integration
5. `internal/fanout/resume.go` - Resume integration
6. `internal/registry/precedence.go` - Settings resolution
7. `internal/registry/config.go` - Registry config validation
8. `internal/registry/project.go` - Project config
9. `cmd/atcr/review.go` - CLI flag

Let me trace through carefully:

### Cache Key (`internal/cache/key.go`)

The Key function takes (promptHash, model, tuning). In the diff, `diffCacheKey` in fanout/review.go:

```go
func diffCacheKey(prompt, model string, temperature *float64) string {
    temp := "default"
    if temperature != nil {
        temp = strconv.FormatFloat(*temperature, 'g', -1, 64)
    }
    return cache.Key(cache.HashText(prompt), model, temp)
}
```

The key is derived from: prompt hash + model + temperature. But the sprint plan says:

> Cache key = sha256(payload_bytes) + model_id + sha256(persona_text)

This is a deliberate change in the implementation. Let me re-read:

> Keying on the rendered prompt (not the raw payload/persona) is deliberate: the prompt subsumes payload + persona + the per-agent scope focus + the base/head refs, so any input that changes what the model sees invalidates the entry.

OK, so the implementation takes the rendered prompt instead. This is reasonable, but it differs from the sprint plan. Let me check if this is within the scope.

The sprint plan clarifications say:
> Cache key = sha256(payload_bytes) + model_id + sha256(persona_text). Agent name is redundant once persona content + model are hashed.

The implementation instead uses: sha256(rendered_prompt) + model + temperature. The sprint plan also says "byte-budget/truncation is captured implicitly (different budget → different payload text → different hash)" which is consistent with using the rendered prompt that incorporates these.

But wait, the key composition differs: the plan said persona_text hash, implementation uses temperature. This could be considered a deviation.

However, the plan also says "Keying on the rendered prompt is what guarantees a scope or persona change invalidates the entry instead of silently replaying a stale review." This is a deliberate decision documented in the code.

### Cache Store (`internal/cache/store.go`)

The Store uses mtime for LRU eviction. Looking at the evict function:

```go
sort.Slice(files, func(i, j int) bool { return files[i].mtime.Before(files[j].mtime) })
```

This sorts oldest first for eviction. Then in Get, it refreshes mtime:

```go
// Refresh recency for LRU eviction (best-effort: a touch failure only makes
// the entry a slightly earlier eviction candidate, never a correctness bug).
now := time.Now()
_ = os.Chtimes(path, now, now)
```

Wait - on certain filesystems with coarse timestamp resolution, multiple Gets in quick succession may have the same mtime, defeating the recency refresh. But this is not a critical issue.

**Issue**: Looking at the eviction loop more carefully:

```go
for _, f := range files {
    if total <= s.maxBytes {
        break
    }
    if err := os.Remove(f.path); err == nil {
        total -= f.size
    }
}
```

If `os.Remove` fails (e.g., concurrent external removal), the `total` is not decremented, but the loop continues. This could lead to infinite iteration? No, because the loop iterates over all files. The issue is that `total` could remain > `s.maxBytes` after the loop if some removals failed. But this is acceptable since this is best-effort.

**Issue with mtime refresh**: The comment says "Refresh recency for LRU eviction" but the Get operation refreshes mtime BEFORE returning the content. This means that on a cache hit, we get the content AND we touch the file. However, if multiple readers hit at roughly the same time, the mtime refresh only updates to the current time which may not differ from the original. Let me think...

Actually, this is a subtle issue. `os.Chtimes` updates the atime and mtime to `now`. If `now` happens to be the same as the file's current mtime (due to filesystem resolution), the touch is a no-op. This is OK for correctness because the file is still in the cache.

**Issue**: In `evict()`, the files are sorted by mtime but entries that failed validation/parsing would also be in the dir. However, they are filtered by `*.json` suffix.

**Critical Issue**: Let me look at the Put function:

```go
func (s *Store) Put(key, content string) error {
    name, ok := fileName(key)
    if !ok {
        return errors.New("cache: refusing to store under a malformed key")
    }
    ...
    if err := atomicfs.WriteJSON(path, entry{Key: key, Content: content}); err != nil {
        return err
    }
    s.evict()
    return nil
}
```

The eviction happens AFTER the write, and the eviction only deletes files in `s.dir`. So when we write a new entry that's very large, we might temporarily exceed `maxBytes` before evicting. This is OK.

**Potential Issue with concurrent Put/Get**: The mutex protects all ops, so concurrent access is safe. But the Get refreshes mtime inside the lock, which is fine.

**Race condition concern**: When Put completes, it has called `evict()`. But if another goroutine called Get and is about to refresh mtime on an entry that was just evicted... wait, that can't happen because Get holds the lock for its entire duration, including the mtime refresh. And Put holds the lock too. So no race.

### Fanout Engine (`internal/fanout/engine.go`)

```go
func (e *Engine) invokeCachedSingleShot(ctx context.Context, a Agent) Result {
    if e.cache == nil || a.CacheKey == "" {
        return e.invokeSingleShot(ctx, a)
    }
    key := a.CacheKey
    if !e.cacheNoRead {
        content, hit, err := e.cache.Get(key)
        ...
    }
    r := e.invokeSingleShot(ctx, a)
    if r.Status == StatusOK {
        if err := e.cache.Put(key, r.Content); err != nil {
            ...
        }
    }
    return r
}
```

**Potential Issue**: On a cache hit, the code constructs a Result with `CacheHit: true`, but doesn't set `TokensIn`, `TokensOut`, `CallRecords`, etc. The comment says "with zero token usage and no CallRecords". This is intentional and correct.

**Potential Issue**: On a cache hit, the Result has `Truncation: a.Truncation, MinSeverity: a.MinSeverity, MaxFindings: a.MaxFindings`. But it doesn't include other fields like `FallbackFrom`. Is this a bug? Let me check what invokeSingleShot does:

Looking at invokeSingleShot (which I don't have full source for, but from context):
- It returns a Result with various fields populated.

When cache hit, the synthesized Result only has fields explicitly set. Other fields are zero values. If downstream code relies on `FallbackFrom`, `Truncation`, etc., the cache hit would have different (zero) values than a live result.

This might be a real issue. Let me look more carefully at what fields are populated in a live Result vs. a cached Result.

Actually, the code only populates a subset of fields. This could be a correctness issue.

Let me check - on the live call result `r`, the cached version only stores `r.Content`. So when we replay, we don't have access to the full Result fields from the original call. If the original call had specific `FallbackFrom`, `Truncation`, etc., the replay loses them.

But wait, the keys are content-addressed, so if the prompt/model/etc. are the same, the Truncation, MinSeverity, MaxFindings would also be the same. The only field that's truly different is `FallbackFrom` (which is set when a fallback agent is used) - but fallbacks have their own cache key.

Actually looking at the code more carefully:
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

The Result populates fields from the Agent (which is the input to the call). Fields that are computed during the call (TokensIn, TokensOut, Latency, CallRecords, etc.) are zero values. This seems intentional and OK.

### Resume (`internal/fanout/resume.go`)

```go
cache:       cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes),
cacheNoRead: req.NoCache,
```

This wires cache for resumed reviews. Good.

### Settings Resolution (`internal/registry/precedence.go`)

The CacheMaxBytes is added to Settings and ResolveSettings. Let me check the default:

```go
CacheMaxBytes:     DefaultCacheMaxBytes,
```

`DefaultCacheMaxBytes = 50 * 1024 * 1024 = 52428800` which matches the documented default.

The validation:
```go
if s.CacheMaxBytes < 0 {
    return Settings{}, fmt.Errorf("cache_max_bytes must be >= 0 (0 = unbounded), got %d", s.CacheMaxBytes)
}
```

This is OK.

### Registry validation (`internal/registry/config.go`)

```go
if r.CacheMaxBytes != nil && *r.CacheMaxBytes < 0 {
    errs = append(errs, fmt.Errorf("cache_max_bytes must be >= 0 (0 = unbounded), got %d", *r.CacheMaxBytes))
}
```

OK.

### Project config (`internal/registry/project.go`)

```go
if cfg.CacheMaxBytes != nil && *cfg.CacheMaxBytes < 0 {
    return nil, fmt.Errorf("%s: cache_max_bytes must be >= 0 (0 = unbounded)", base)
}
```

OK.

### Issues I'm finding:

1. **`invokeCachedSingleShot` falls back to `invokeSingleShot` when `a.CacheKey == ""`, but the comment says "An agent with no payload digest... bypasses the cache entirely even when a store is wired, so it always calls live."** This is consistent. Good.

2. **Cache `Get` refreshes mtime even on a defensive key mismatch recovery path.** Wait, looking at the code:
```go
if jerr := json.Unmarshal(data, &e); jerr != nil || e.Key != key {
    _ = os.Remove(path)
    return "", false, nil
}
// Refresh recency for LRU eviction...
```
The mtime refresh happens AFTER the validation. So if a corrupt entry is detected, it's removed without refreshing mtime. Good.

3. **Issue: `Put` writes the file but doesn't refresh mtime**. When you Put a new entry, the file has the current time as mtime (because it was just written). So it's already "most recent". OK.

4. **`invokeCachedSingleShot` returns the cached content without running post-processing or any other steps** that `invokeSingleShot` might do. Let me check if there are any side effects in invokeSingleShot that would matter for replay.

Without seeing invokeSingleShot's full code, but based on the comment "It is the only cache integration point", I'll assume this is OK.

5. **Issue with `CacheHit` flag**: When a cache hit is replayed, `Result.CacheHit = true`. But looking at `invokeAgent` (which wraps `invokeSingleShot`/`invokeCachedSingleShot`), it might also do metrics accounting. The cached path skips that? Or is invokeCachedSingleShot called from invokeAgent? Let me think...

Looking at the diff:
```go
return e.invokeCachedSingleShot(ctx, a)
```

And:
```go
return e.invokeDegraded(ctx, a)
```

So invokeCachedSingleShot replaces invokeSingleShot in the dispatch. But what about invokeAgent? The comment says:
> the agent is still counted by the invokeAgent metrics wrapper (it produced a result), but no provider round-trip is recorded

OK so invokeAgent wraps it. Let me check if `invokeAgent` is the public function or internal. From the diff, it seems `dispatchAgent` calls `invokeCachedSingleShot` which is at the level that wraps the single-shot path.

Actually, looking at the code path:
```go
func (e *Engine) dispatchAgent(ctx context.Context, a Agent) Result {
    ...
    if a.Tools {
        if !degraded {
            return e.invokeToolLoop(ctx, a, cc, e.dispatcher)
        }
        return e.invokeDegraded(ctx, a)
    }
    return e.invokeCachedSingleShot(ctx, a)  // CHANGED from invokeSingleShot
}
```

So `invokeAgent` (which presumably wraps for metrics) is called somewhere, not `dispatchAgent` directly. Let me see... actually I don't see `dispatchAgent` in the diff for the wiring. Let me re-check.

Looking at the diff for engine.go, I see the change at line 522-ish:
```go
return e.invokeCachedSingleShot(ctx, a)
```

This replaces `return e.invokeSingleShot(ctx, a)`. So `invokeAgent` is the outer wrapper, and `dispatchAgent` is internal. `invokeAgent` calls `dispatchAgent` which calls `invokeCachedSingleShot`.

Hmm, but there's a tricky issue. The `invokeAgent` does metrics tracking. If a cached hit returns without API call, then:
- No `CallRecords` are added
- No tokens are accounted
- But the cache hit is still counted as a success

This is intentional and correct per the design.

6. **Issue with `Result.CacheHit` in the synthesized result**: Looking at the synthesized Result on cache hit:
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

This Result doesn't include `Truncated` (a boolean indicating if truncation happened) or other post-processing flags. Wait, `Truncation` here is `Truncated` on the Result type? Let me check...

Looking at the Agent struct, `Truncation` is on the Agent. On the Result struct, there's `Truncated bool` (likely). The cached path sets `Truncation: a.Truncation` which might not be the right mapping.

Actually wait, looking at the field name, the Agent field is `Truncation` (what was truncated) and Result has `Truncated bool` (whether truncation occurred). These are different concepts. The cached path might be assigning the wrong field type, or maybe Truncation is the same field name. I can't tell without the full source.

Let me focus on what I can verify.

7. **Issue: `TestEngine_NoCacheBypassesReadButStillWrites`**

The test expects:
- After seeding with `--no-cache=false`: 1 call
- After `WithCache(store, true)` (no-read): 2 calls (because it bypasses read)
- After `WithCache(store, false)` again: 2 calls total (served from cache)

The last assertion checks `r2[0].CacheHit == true`. This requires that the no-cache run ALSO wrote the result. The code:
```go
if r.Status == StatusOK {
    if err := e.cache.Put(key, r.Content); err != nil {
        ...
    }
}
```

Yes, this writes regardless of `cacheNoRead`. Good.

But wait - the test counts `f.callCount("m") == 2` after the no-cache run, and expects `r2[0].CacheHit == true`. This means the no-cache run wrote to the cache, and the next read found it. Good.

8. **Issue with `Put` and `fileName`**: Looking at:
```go
func fileName(key string) (string, bool) {
    digest := strings.TrimPrefix(key, "sha256:")
    if digest == key || !isHex64(digest) {
        return "", false
    }
    return digest + ".json", true
}
```

The check `digest == key` is true when `key` doesn't start with "sha256:" (because TrimPrefix returns the original string unchanged when prefix doesn't match). This catches malformed keys.

But what about uppercase hex? `isHex64` only accepts lowercase:
```go
if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
    return false
}
```

`sha256:` + uppercase hex would be rejected. Since `hex.EncodeToString` produces lowercase, this is consistent.

9. **Potential issue with concurrent Get during Put**: The Put holds the mutex. If a Get is in progress when Put completes, the Get might see a partially-written file. But since WriteJSON is atomic (write-to-temp-then-rename), and Put holds the mutex, this isn't an issue.

Wait, but the Get also holds the mutex. So Get and Put can't run concurrently. Good.

10. **Potential issue with `Result.CacheHit` field default value**: If a `Result` is constructed without explicitly setting `CacheHit`, it defaults to `false`. This is the desired behavior for live results. Good.

11. **Critical issue: `Agent.CacheKey` is set unconditionally in `buildAgent`** but `Engine` doesn't use it for tool agents (it bypasses cache for tool agents). The comment in `buildAgent` says:
> Tool agents carry a key too but the engine never caches them

This is fine because the key is just unused for tool agents.

12. **`buildFallbackAgent` sets `CacheKey` from primary's prompt + fallback's model/temperature.** This is correct because the fallback reviews the same content but on a different model.

13. **Issue: When a review is "serial" (in serial_agents) vs parallel, does the cache work for both?** Looking at the dispatch logic - all non-tool agents go through `invokeCachedSingleShot`. So yes.

14. **`TestEngine_CacheHitReplaysWithoutAPICall`** - The test asserts `r1[0].CacheHit == false` and `r2[0].CacheHit == true`. The second run uses the same slot, same prompt, same model. So the cache should hit.

But wait, look at the slot construction:
```go
return Slot{Primary: Agent{
    Name:        name,
    PayloadMode: "blocks",
    CacheKey:    diffCacheKey(prompt, model, nil),
    Invocation:  llmclient.Invocation{Model: model, Prompt: prompt},
}}
```

The `CacheKey` is `diffCacheKey(prompt, model, nil)`. So temperature is "default" (string). Same for both runs. Good.

15. **`invokeCachedSingleShot` doesn't include `Truncated`, `Error`, etc. fields on cache hit.** On cache hit, these would be zero values. If a cached result was generated without truncation but the current run has different truncation settings... wait, but truncation is deterministic on the payload. And Truncation comes from the Agent, so it's the same. OK.

But what if the original cached result WAS truncated (because the original payload was larger)? Then when replayed, we get the truncated content, but the new run might have a different payload size. However, the cache key is based on the rendered prompt, which already incorporates the payload. So if the payload changes, the cache key changes. So this isn't an issue.

16. **`invokeCachedSingleShot` on cache hit doesn't set `CallRecords`, `TokensIn`, `TokensOut`, `Latency`, etc.** This is intentional. The comment explains.

17. **Possible issue: `Result.TokensIn` and `TokensOut` on cache hit**: If downstream code computes total cost/savings, a cache hit would report zero tokens used. This might be the intended behavior, but it could surprise downstream consumers who expected to see tokens always.

Looking at the comment:
> The replayed result carries the cached Content with zero token usage and no CallRecords, so the run's API-call and cost metrics reflect that no provider round-trip happened.

OK, this is intentional.

18. **`TestEngine_FailedReviewIsNotCached`**: This test verifies that on a failure, the cache is not populated. Good.

19. **Issue with `f.failFor` being added and removed**: The fake's failFor map is mutated, and the second run expects it to succeed. The order is:
1. Set `f.failFor["m"] = err`
2. Run with fail
3. Delete `f.failFor["m"]`
4. Run again - should succeed and call live

Yes, this is correct.

20. **Issue: In `internal/fanout/cache_test.go`, the line `delete(f.failFor, "m")`** - this is inside the test, not in production code. OK.

21. **Potential race in TestStore_ConcurrentPutGet**: 16 goroutines each put and get a different key. The mutex serializes all operations. Should work.

22. **`TestStore_CorruptEntrySelfHeals`**: Writes garbage at the cache path, then calls Get. The Get should detect corruption (json.Unmarshal fails), remove the file, and return miss. Then stat shows the file is gone.

But wait - the path is `filepath.Join(dir, fileNameFor(k))`. `fileNameFor` is the test helper that calls `fileName(k)`. For `Key(HashText("p"), "m", HashText("x"))`, `fileName` returns `"<64hex>.json"`. So the path is `<dir>/<64hex>.json`.

OK.

23. **`TestStore_RejectsMalformedKeys`**: Tests that malformed keys are rejected. Good.

But wait - one of the bad keys is `"sha256:" + strings.Repeat("z", 64)`. This has the right length and prefix but uses 'z' which is not hex. `isHex64` checks each char is 0-9 or a-f. 'z' is rejected. Good.

24. **Issue: In `TestStore_RejectsMalformedKeys`, after rejecting malformed keys, it asserts**:
```go
_, statErr := os.Stat(filepath.Join(t.TempDir(), "etc"))
assert.True(t, os.IsNotExist(statErr))
```

But the temp dir might not be created yet. `t.TempDir()` returns a path, but the dir might not exist on disk. `os.Stat` on a non-existent dir would return an error. OK.

But wait, what if a previous test created an "etc" subdir? `t.TempDir()` should give a unique dir per test. So this should be fine.

25. **`TestRunReview_SecondRunServedFromCache`**: This is an end-to-end test. It expects 2 hits after the first run (2 agents = 2 calls), and still 2 hits after the second run (served from cache).

But there's a subtle issue: `circuitbreaker.DefaultRegistry.Reset()` is called inside `countingProvider`, and `t.Cleanup(circuitbreaker.DefaultRegistry.Reset)` resets after the test. But between calls within the same test, the circuit breaker might accumulate state from the first call and trip on the second... actually no, because a successful call wouldn't trip it.

26. **`TestEngine_DifferentPromptMissesCache`**: Tests that a changed prompt misses the cache. The test uses different prompts but the same agent name. The cache key includes the prompt hash, so different prompts produce different keys. Good.

27. **`TestEngine_DifferentTemperatureMissesCache`**: Tests that a different temperature misses the cache. The test passes temperature as a pointer. `diffCacheKey` uses `strconv.FormatFloat(*temperature, 'g', -1, 64)` for non-nil. For nil, it uses "default". So different temps produce different keys. Good.

But wait, what about the precision? `FormatFloat(0.9, 'g', -1, 64)` and `FormatFloat(0.9, 'g', -1, 64)` should produce the same string. And `FormatFloat(0.1, 'g', -1, 64)` differs. OK.

28. **`TestEngine_NoCacheKeyIsNeverCached`**: Tests that an Agent without `CacheKey` is never cached. The agent has `CacheKey = ""` (default). The check `if e.cache == nil || a.CacheKey == ""` bypasses the cache. Good.

29. **`NewStore` creates a Store without checking the dir is writable**. This is OK because Put does `os.MkdirAll` lazily.

30. **Issue: `evict()` doesn't handle the case where `s.dir` doesn't exist (e.g., never Put'd)**. Looking at:
```go
entries, err := os.ReadDir(s.dir)
if err != nil {
    return
}
```

If dir doesn't exist, ReadDir returns an error (not ErrNotExist necessarily, but the code returns early on any error). So evict is a no-op on a non-existent dir. Good.

31. **Issue: **`evict()` runs after EVERY Put**, even if the new entry's size doesn't exceed `maxBytes`. This is a performance concern - if the cache is small and maxBytes is large, every Put triggers a full dir scan. But it's a fast scan, and `total <= s.maxBytes` short-circuits. OK, acceptable.

32. **`atomicfs.WriteJSON` is used for the write** - this provides atomic write semantics. Good.

33. **Issue: `CacheHit` field on Result - if invokeAgent tracks metrics, does it differentiate between cache hits and live calls?** Looking at the test:
```go
r2 := NewEngine(f, WithCache(store, false)).Run(context.Background(), []Slot{slot})
```
And `assert.Equal(t, 1, f.callCount("m"), "cache hit must not make another API call")`.

So `callCount` only counts live API calls, not cache hits. Good.

34. **`invokeCachedSingleShot` doesn't use `WithRetry`** or other options that `invokeSingleShot` might use. Since it just calls invokeSingleShot internally, retry options are inherited. Good.

35. **Issue: In `invokeCachedSingleShot`, the cache `Get` is called BEFORE the actual single-shot call.** This means on a cache hit, we save the LLM call. Good.

36. **Issue: On cache miss, after calling live and storing, if the cache write fails, we still return the result.** This is correct behavior.

37. **`invokeCachedSingleShot` doesn't handle the case where `a.CacheKey` is empty** by checking more carefully:
```go
if e.cache == nil || a.CacheKey == "" {
    return e.invokeSingleShot(ctx, a)
}
```
Good - falls through to invokeSingleShot.

38. **Critical issue: The cache stores `Result.Content` as a string, but for content that includes non-UTF8 characters or unusual formatting, JSON marshaling might lose precision or escape characters.** The Content is JSON-encoded. When decoded back, it's the same string. OK.

But wait - JSON encoding/decoding should be lossless for valid UTF-8 strings. Content from LLM responses should be UTF-8.

39. **Issue: Cache key uses temperature but not other tuning parameters** (top_p, frequency_penalty, etc.). Looking at `diffCacheKey`:
```go
func diffCacheKey(prompt, model string, temperature *float64) string {
```

Only temperature is included. The sprint plan said:
> Cache key = sha256(payload_bytes) + model_id + sha256(persona_text)

The implementation deviates by including temperature instead of persona_text hash. But the rendered prompt already subsumes the persona. So the persona hash is implicit in the prompt hash. The temperature is an explicit addition that the plan didn't mention.

This could be a deviation, but it's intentional based on the implementation comments.

40. **Issue: Cache key doesn't include `seed`** if the model supports it. Same as above - if seed affects output, the cache would be incorrect. But the sprint plan didn't mention seed.

41. **`TestStore_GetRefreshesRecency`**: This test depends on mtime resolution. On Linux, mtime resolution is nanoseconds, so 10ms sleeps should be enough. On macOS or Windows with coarser resolution, this test might be flaky. But for CI on Linux, it's OK.

42. **Issue: `Put` returns an error if the dir creation fails.** But what if the dir already exists with the wrong permissions? `os.MkdirAll(s.dir, 0o755)` won't change permissions on existing dir. So if a user sets `cache_dir` to a read-only dir, writes would fail later. OK, acceptable.

43. **`invokeCachedSingleShot` returns a Result without populating the `FallbackFrom` field even when the agent is a fallback.** Looking at the synthesized result:
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

`FallbackFrom` is not set. If the agent is a fallback, the live result would set `FallbackFrom`. But the cache hit doesn't. This could be a real issue.

Actually wait, fallbacks have their own cache key (see `buildFallbackAgent`), so the cache for a fallback is keyed on (primary's prompt, fallback's model, fallback's temp). When replayed, we know it's a fallback from the key composition, but the `FallbackFrom` field isn't populated.

Hmm, but `FallbackFrom` is set by `dispatchAgent` based on whether this is a fallback invocation. Let me check...

Actually I don't have access to the full source of dispatchAgent. But if dispatchAgent (or some wrapper) sets `FallbackFrom` after the inner call, then it would be preserved. Let me look at the flow:

```
dispatchAgent -> invokeCachedSingleShot -> Result (or invokeSingleShot -> Result)
```

If dispatchAgent wraps the result and adds FallbackFrom, then it's fine. But if invokeSingleShot sets it, then the cached path skips it.

This is a potential issue but I can't verify without more context.

44. **`TestEngine_DifferentPromptMissesCache` regression guard comment says "the independent-review HIGH finding"**. This suggests there was a previous review finding about this. The current implementation addresses it.

45. **Issue: `cacheMaxBytes` validation in `config.go` only validates negative values, but the embedded default is non-negative.** The pointer indirection allows 0 (unbounded). OK.

46. **`TestPrecedence_CacheMaxBytesExplicitZeroIsUnbounded`**: Verifies that 0 is treated as "unbounded" (parity with payload_byte_budget and max_parallel). The test asserts:
```go
assert.Equal(t, int64(0), s.CacheMaxBytes, "explicit 0 overrides the embedded default (unbounded)")
```

This verifies that an explicit 0 in the config is preserved through default application. Good.

47. **Issue: In `precedence.go`, the `applyTier` function doesn't include CacheMaxBytes**. Instead, CacheMaxBytes is overlaid directly. The comment explains why (the retry tunables pattern). OK.

48. **`TestRunReview_NoCacheRequestStillCallsLive`**: Verifies that `NoCache=true` in the request bypasses cache reads. The test counts 4 hits after two runs (2 cold + 2 with no-cache). But wait, what about the cached version of the no-cache run? Looking at the assertion:
```go
assert.Equal(t, int64(4), atomic.LoadInt64(&hits), "--no-cache bypasses the warm cache and calls live")
```

So 2 cold + 2 no-cache = 4 hits. The no-cache run still writes fresh results, but the test doesn't verify that. The test is focused on the read bypass.

49. **`TestEngine_ToolAgentNeverCached`**: Verifies that a tool agent (with `Tools: true`) is never cached, even when it has a cache key. Looking at the dispatch:
```go
if a.Tools {
    ...
    return e.invokeDegraded(ctx, a)  // or invokeToolLoop
}
return e.invokeCachedSingleShot(ctx, a)
```

Tool agents go to `invokeDegraded` or `invokeToolLoop`, not `invokeCachedSingleShot`. So they're not cached. Good.

50. **Issue: The `Tools` field is being set on the agent, but I don't see the dispatch logic that checks `a.Tools`.** Looking at the diff again:
```go
if a.Tools {
    if cc, ok := e.completer.(ChatCompleter); ok && !degraded {
        return e.invokeToolLoop(ctx, a, cc, e.dispatcher)
    }
}
return e.invokeDegraded(ctx, a)
```

Wait, this isn't shown in the diff. Let me look at the actual diff context...

Actually the diff shows:
```go
if a.Tools {
    ...
    return e.invokeToolLoop(ctx, a, cc, e.dispatcher)
}
// A degraded tool agent runs single-shot but is intentionally NOT cached:
return e.invokeDegraded(ctx, a)
}
- return e.invokeSingleShot(ctx, a)
+ return e.invokeCachedSingleShot(ctx, a)
```

Hmm, the diff shows that the call to invokeSingleShot was replaced with invokeCachedSingleShot. So the original dispatch had something like:
- If tool: invokeToolLoop or invokeDegraded
- Else: invokeSingleShot

The new code:
- If tool: invokeToolLoop
- Else (degraded or non-tool): invokeDegraded OR invokeCachedSingleShot

Wait, this is confusing. Let me re-read the diff:

```
-	return e.invokeSingleShot(ctx, a)
+		// A degraded tool agent runs single-shot but is intentionally NOT cached:
+		// it was configured to read live code via the tool loop, so its output is
+		// not a pure function of the payload and a payload-keyed cache could serve
+		// a stale degraded answer. Tool agents always call live.
+		return e.invokeDegraded(ctx, a)
+	}
+	return e.invokeCachedSingleShot(ctx, a)
```

So the structure is:
- If tool enabled: invokeToolLoop or invokeDegraded (single line before)
- Else: invokeCachedSingleShot

This means invokeDegraded is now in the tool branch (not the else branch). Good. Tool agents (whether tool-loop or degraded) bypass the cache.

51. **Issue: The cache key doesn't include `max_tokens`.** Looking at `diffCacheKey`:
```go
func diffCacheKey(prompt, model string, temperature *float64) string {
```

`MaxTokens` is not included. The comment in the code says "MaxTokens is constant across review agents (defaultMaxTokens), so it is intentionally omitted." But what if a user customizes `MaxTokens`? Then the cache key would be wrong.

Let me check... `MaxTokens` comes from the agent config. Looking at `buildAgent`:
```go
MaxTokens: derefInt(ac.MaxTokens),
```

And `maxTokensPtr()` returns a default of 8192. If an agent has `max_tokens: 4096` in config, that would be set. If two agents have different max_tokens but the same prompt+model+temp, they'd have the same cache key but different output behaviors.

Actually, max_tokens affects whether the output gets truncated. So caching an output from max_tokens=8192 and serving it when max_tokens=4096 would be incorrect.

Hmm, but the prompt itself might also be different (if the prompt rendering includes max_tokens somewhere). Let me check...

Actually, I don't have visibility into the prompt rendering. But typically the prompt doesn't include max_tokens. So the cache key should include max_tokens if it's configurable per-agent.

This is a potential bug. Let me flag it as a finding.

Wait, looking at `llmclient.Invocation`, MaxTokens is a field. And it's set in buildAgent. So it's configurable per agent. The cache key should include it.

But the comment in the code explicitly says "MaxTokens is constant across review agents (defaultMaxTokens), so it is intentionally omitted." This is a deliberate decision, but it might be incorrect.

Let me check the actual behavior. If two agents have different max_tokens and same prompt/model/temp:
- They have the same cache key
- The first to run gets cached
- The second serves from cache, but its max_tokens is different

If max_tokens=8192 was used to generate, the content is the full response (up to 8192 tokens). When served to an agent with max_tokens=4096, the content would be the longer one, which is wrong (the user expected the response to be limited to 4096 tokens).

Wait, but max_tokens is a CLIENT-side parameter, not affecting the model output content directly. It limits the response length. So if cached content was 6000 tokens, serving it to an agent with max_tokens=4096 would actually violate the user's intent.

So this IS a potential correctness issue.

52. **Issue: `NewStore` doesn't validate
</think>
