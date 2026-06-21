<think>
Let me analyze this diff carefully. It's a large change implementing Epic 5.2: Diff Caching & Incremental Reviews.

Let me go through the key files and identify any issues:

1. **internal/cache/key.go** - Defines `Key` and `HashText` functions
   - Uses NUL separator - good
   - Returns "sha256:<hex>" format - good
   - The `tuning` parameter is described as "temperature" in comments but the function signature says `tuning` - this is fine, it's the temperature

2. **internal/cache/store.go** - The cache store
   - Uses mutex - good
   - Has path traversal protection via `fileName` validation - good
   - LRU eviction by mtime - good
   - Self-heals corrupt entries - good
   - One concern: in `evict()`, it doesn't refresh mtime after writing a new entry, but the file's mtime is naturally the write time so this is fine
   - Issue: the evict() function doesn't sort or filter correctly - wait, it does sort by mtime
   - Issue: `evict()` only checks files, not subdirectories, but cache files are all .json
   - Issue: race condition where `evict` runs while a parallel Get might be reading? But the mutex protects this
   - Potential issue: `Chtimes` is best-effort, but if it fails, the entry is treated as older and may be evicted. This is documented as acceptable.

3. **internal/fanout/engine.go** - Engine changes
   - `invokeCachedSingleShot` properly handles the cache
   - Falls through to `invokeSingleShot` on cache error - good
   - Only caches OK results - good
   - The Result struct's CacheHit field is added
   - Wait - one thing: the engine doesn't pass `Log` field. Let me look more carefully...
   - The Result doesn't have a `Log` field, but the synthesized result returns a Result with a Content, Status, etc. Looking at the existing code...

4. **internal/fanout/review.go** - Review changes
   - `diffCacheKey` function - looks correct
   - `buildAgent` includes CacheKey - good
   - `buildFallbackAgent` uses primary's prompt with fallback's model/temperature - good
   - Issue: In `buildFallbackAgent`, the `Prompt` field is the primary's prompt but used in diffCacheKey. Let me check if `Agent.Prompt` exists...

5. **internal/fanout/resume.go** - Resume changes
   - Wires the diff cache for resume path
   - Issue: The `p` variable in PrepareResume needs to be checked - the cache field addition looks correct

6. **internal/registry/config.go** - Registry config
   - `CacheMaxBytes *int64` added
   - Negative validation - good

7. **internal/registry/project.go** - Project config
   - `CacheMaxBytes *int64` added
   - Negative validation - good
   - DefaultProjectConfigYAML updated - good

8. **internal/registry/precedence.go** - Precedence resolution
   - `CacheMaxBytes int64` added to Settings
   - Negative validation - good
   - Applies registry then project tier - good

9. **cmd/atcr/review.go** - CLI flag
   - `--no-cache` flag added
   - Defaults to false - good
   - Mutually exclusive with `--resume`? Let me check... The diff doesn't add this constraint. Looking at the documentation, `--no-cache` should be a separate flag. But is it valid to use with `--resume`? Looking at the diff, it doesn't add a mutual exclusion, but probably should be allowed with resume (since resume reuses cache too).

10. **internal/fanout/status.go** - Status field
    - `CacheHit` field added with `omitempty` - good

Let me look more carefully for actual issues:

### Potential Issues

1. **buildFallbackAgent uses `primary.Prompt`** - This is in internal/fanout/review.go around line 772-780. Let me look at the Agent struct... The diff added `CacheKey` to Agent. But does Agent have a `Prompt` field? Let me search... Actually, looking at the code, the primary Agent's prompt is set elsewhere. The cache key uses `primary.Prompt` which must exist on the Agent struct. Looking at the existing engine code, I see `a.Invocation.Prompt` used in diffCacheKey, so the prompt is on Invocation. So `primary.Prompt` would need to be the Invocation.Prompt, not a direct field.

Wait, let me re-read the buildFallbackAgent code:
```go
CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```

The `primary` is an `Agent` struct. The diff shows that `buildFallbackAgent` returns an Agent. Looking at the Agent struct, the Prompt field would be on the Invocation. So `primary.Prompt` may not exist - this could be a compile error.

Actually, looking at the original code structure, the Agent struct has:
```go
Invocation: llmclient.Invocation{
    BaseURL:     prov.BaseURL,
    APIKeyEnv:   prov.APIKeyEnv,
    ...
}
```

So the Prompt is `primary.Invocation.Prompt`. The diff uses `primary.Prompt` which would only work if Agent had a Prompt field directly. Let me look at the rest of the file more carefully...

Looking at `buildAgent`, the function takes `cfg *ReviewConfig, name string, payloads map[string]modePayload, rng ReviewRange` and uses `ac` (agent config) to build the Invocation. The prompt is built inside buildAgent and goes into Invocation. So `primary.Prompt` is likely a compile error unless there's a `Prompt` field on Agent that's added in the diff.

Looking at the diff more carefully, in `buildFallbackAgent`:
```go
CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```

For this to compile, `primary` (of type `Agent`) would need a `Prompt` field. The diff doesn't add such a field. So this is likely a compile error.

Wait, maybe the original code already had a `Prompt` field on Agent that was being used elsewhere? Let me check the existing buildFallbackAgent code patterns... Actually, looking at the diff, it says "buildFallbackAgent" but the full code isn't shown. The diff only shows what's changed. So the original code might have had `primary.Prompt` or similar.

Actually, looking at the diff more carefully, it only shows context around the `CacheKey` line. The line `CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),` could compile if `Agent` had a `Prompt` field. But based on the `Invocation` pattern, prompts are usually on `Invocation.Prompt`.

This is potentially a HIGH severity issue if it's a compile error.

Hmm, but I should not flag something as an issue if I can't verify it. Let me look at the diff again...

Actually, I think I need to be careful. The diff snippet shows:
```go
MinSeverity: primary.MinSeverity,
MaxFindings: primary.MaxFindings,
// Diff-cache key (Epic 5.2): a fallback reviews the SAME rendered prompt as
// the primary but on its OWN model and temperature, so it keys on the
// primary's prompt with the fallback's model/temperature — a substitute
// model must not collide with the primary's cache entry.
CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```

The `primary.Prompt` access requires `Prompt` to be a field on `Agent`. If the original Agent struct doesn't have this, it's a compile error.

Looking at the existing Agent struct in the diff (around line 105-115), it doesn't show a `Prompt` field. The only fields shown are:
- (some fields I can't see in the diff)
- MinSeverity string
- MaxFindings *int
- CacheKey string

So `primary.Prompt` would not compile. This is a HIGH severity issue.

But wait - this is a code review and the diff is supposed to compile. Maybe there's context I'm missing. Let me look at `buildAgent` more carefully:

```go
CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature),
```

Here `prompt` is a local variable in `buildAgent` - this works. But in `buildFallbackAgent`, `primary.Prompt` is used. This would require `Agent.Prompt` to exist.

Hmm, looking at the test file `cache_test.go`:
```go
slot := Slot{Primary: Agent{
    Name:        name,
    PayloadMode: "blocks",
    CacheKey:    diffCacheKey(prompt, model, nil),
    Invocation:  llmclient.Invocation{Model: model, Prompt: prompt},
}}
```

So in tests, the prompt is in `Invocation.Prompt`. This confirms that `Agent` has `Invocation` field with `Prompt` inside, not a top-level `Prompt`.

So `primary.Prompt` in `buildFallbackAgent` would not compile. This is a HIGH severity issue.

But wait, maybe the original code also had a `Prompt` field added to Agent that I'm missing from the diff. The diff is showing context lines. Let me see if `primary.Prompt` is in the original code...

Actually, looking at the diff format, the lines shown with ` ` (space prefix) are context lines from the original. So:
```go
		MinSeverity: primary.MinSeverity,
		MaxFindings: primary.MaxFindings,
		// Diff-cache key (Epic 5.2): a fallback reviews the SAME rendered prompt as
		// the primary but on its OWN model and temperature, so it keys on the
		// primary's prompt with the fallback's model/temperature — a substitute
		// model must not collide with the primary's cache entry.
		CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```

The `// Diff-cache key` and `CacheKey:` lines are added (have `+` prefix in diff). The `MinSeverity` and `MaxFindings` lines are context.

So `primary.Prompt` is new in this diff. This requires `Agent` to have a `Prompt` field. But the Agent struct in the diff doesn't show one being added.

OK let me look at the diff once more for the Agent struct:

```go
type Agent struct {
    ...
    // constraint follows the slot, like the persona prompt).
    MinSeverity string
    MaxFindings *int
    
    // CacheKey is the diff-cache key (Epic 5.2) for this agent's review call,
    ...
    CacheKey string
}
```

So the Agent struct as shown has MinSeverity, MaxFindings, and CacheKey. No `Prompt` field.

This is a HIGH severity issue: the code references `primary.Prompt` but `Agent` doesn't have a `Prompt` field. The prompt is on `Invocation.Prompt`.

Actually wait, let me reconsider. The diff may not show the entire Agent struct. There may be a Prompt field that already exists. Let me see if I can find any other reference to `primary.Prompt` in the file...

Hmm, I can't be 100% sure without seeing the full file. But based on the patterns I see, this is suspicious.

2. **Race condition in cache store between read and evict**: When `Get` reads a file, then `Put` calls `evict` which deletes it, the read could fail. But the mutex protects this. So no race.

3. **Cache file written but not yet visible**: The store uses `atomicfs.WriteJSON` which is presumably atomic. So this should be fine.

4. **No TTL on cache entries**: This is documented as a design decision - no TTL.

5. **`atomicfs.WriteJSON` is called with `entry` struct but the store interface `reviewCache.Put` takes `(key, content string)`**: The store doesn't know about the `entry` struct, so there's a separation. The `cache.Store.Put` accepts `(key, content)` and wraps them in the entry struct. The engine's `reviewCache.Put` matches this. Good.

6. **`buildFallbackAgent` prompt**: The diff says the fallback reviews the SAME prompt as the primary, but uses `primary.Prompt`. If the primary has its own prompt rendered, then the fallback should use that same prompt. But the comment says "the primary's prompt with the fallback's model/temperature" - this is what we want. The cache key is on (primary's prompt, fallback's model, fallback's temp). Good design.

7. **`invokeCachedSingleShot` returns synthesized Result without certain fields**: Let me check the existing Result struct fields vs the synthesized one:
- Agent: set
- Content: set
- Status: set to StatusOK
- PayloadMode: set
- Truncation: set
- MinSeverity: set
- MaxFindings: set
- Model: set
- CacheHit: set to true

But there might be other fields. Looking at the existing Result struct, fields include:
- Agent
- Content
- Status
- PayloadMode
- Truncation
- MinSeverity
- MaxFindings
- Model
- ToolsDegraded
- ToolsRequested
- TrippedBudgets
- CacheHit (new)
- TokensIn, TokensOut, CostUSD
- CallRecords
- Err

The synthesized result has zero values for TokensIn, TokensOut, CostUSD, CallRecords. This is documented as intentional ("zero token usage and no CallRecords").

But wait - the test `TestEngine_CacheHitReplaysWithoutAPICall` checks `r1[0].Content` is "review by m" - so the content from cache is used. Good.

8. **No error if `cache` is set but `entry` write fails**: The `Put` returns an error, and the engine logs a warning. This is fine.

9. **`OnCacheKey` for tool agents**: The comment says "Tool agents carry a key too but the engine never caches them". This means the engine will still call `cache.Get` for tool agents? No wait, looking at the code, `invokeCachedSingleShot` is only called from `dispatchAgent` for non-tool, non-fallback-path cases. The tool path calls `invokeToolLoop` or `invokeDegraded` directly. So tool agents never reach `invokeCachedSingleShot`. Good.

But wait, the comment in `buildAgent` says "Tool agents carry a key too but the engine never caches them (they read live code), so setting it unconditionally is safe." This is documenting that even though the key is set, the engine won't use it for tool agents. Good.

10. **Missing --no-cache description somewhere**: The flag is added to the CLI help. Good.

11. **Missing `WithCache` call from `cache` parameter being possibly nil**: The diff checks `if p.cache != nil` before calling `WithCache`. Good.

12. **Fallback path doesn't have cache in resume**: Looking at `PrepareResume`, it creates a cache too. Good.

13. **What about fanout paths that aren't `review.go`?** Looking at the diff, `review.go` is the main one. The diff doesn't touch other fanout callers (like reconcile, etc.) which don't use cache. The cache is only for the review path. Good.

14. **`CacheKey` is the SHA256 digest, not the model id alone**: So even if the model id contains special characters, they're hex. Good.

15. **`buildFallbackAgent` issue with `primary.Prompt`**: Already noted above. HIGH severity if not compiling.

16. **Concurrent map access in `failFor` map in tests**: Looking at the test code:
```go
f.failFor["m"] = assertFailErr
...
delete(f.failFor, "m")
```
If multiple goroutines access `f.failFor`, this would race. But tests are sequential, so this is fine. Not a real issue.

17. **`TestRunReview_SecondRunServedFromCache` doesn't reset the cache between tests**: Each test uses `t.TempDir()` so the cache is fresh per test. Good.

18. **`isHex64` doesn't handle uppercase hex**: The function checks for `c < 'a' || c > 'f'` (lowercase only). What if a caller uses uppercase? `Key()` produces lowercase because `hex.EncodeToString` uses lowercase. But if someone constructs a key manually, uppercase would fail. This is probably intentional for consistency, but could be a footgun. LOW severity.

19. **`atomicfs.WriteJSON` for cache entries**: The entry is small (key + content). Atomic write ensures consistency. Good.

20. **Error handling in `Get` after `ReadFile` fails for reasons other than not-exist**: Returns the error. The engine logs and falls through. Good.

21. **`TestStore_RejectsMalformedKeys` doesn't check the directory was never created**: It checks `os.Stat(filepath.Join(t.TempDir(), "etc"))` but this is in t.TempDir() not the cache dir. The check should be in the cache dir, not a sibling. Let me re-read...

```go
_, statErr := os.Stat(filepath.Join(t.TempDir(), "etc"))
assert.True(t, os.IsNotExist(statErr))
```

This checks that `t.TempDir()/etc` doesn't exist. But this is a sibling, not a path that would be created by path traversal. The actual check should be that the cache dir doesn't contain anything. This test is checking the wrong path - it should be checking that nothing in the cache dir matches a traversal pattern. But honestly, since `fileName` validates strictly to 64 lowercase hex chars + ".json", the only file that could exist would be a valid one. So this test is just verifying that nothing was written. The check `t.TempDir()/etc` is misplaced - it should be `dir/etc` to be in the cache dir. But this is a minor test issue. LOW severity.

Wait, actually `t.TempDir()` is the parent of `dir` (the cache dir). If the path traversal had worked, a file would have been written to `t.TempDir()/etc/passwd` (since `../../etc/passwd` from inside `dir` would resolve to `t.TempDir()/etc/passwd`). So the test is checking the right thing - if path traversal had succeeded, `t.TempDir()/etc` would exist. Actually no - the test uses `sha256:../../etc/passwd` which means the digest is `../../etc/passwd`. From `dir = t.TempDir()/cache`, the path would be `t.TempDir()/cache/../../etc/passwd` = `t.TempDir()/etc/passwd`. So the check `t.TempDir()/etc` (checking for the directory) is OK, but checking for the file `t.TempDir()/etc/passwd` would be more thorough. This is a minor test issue.

22. **Time-based test flakiness**: The eviction tests use `time.Sleep(10 * time.Millisecond)` to stagger mtimes. This could be flaky on slow systems. But for tests, this is acceptable.

23. **Mutex held during `evict` calls**: The `evict` does file I/O (ReadDir, Info, Remove) while holding the mutex. This blocks other operations. Documented as acceptable. OK.

24. **`os.Chtimes` on every read**: This updates mtime for every Get. If the cache is hit frequently, this is many syscalls. Could be a performance issue. But documented as acceptable. Not flagging.

25. **`evict` doesn't account for subdirectories**: It filters by `.json` suffix and `IsDir()`. The cache dir shouldn't have subdirectories. OK.

26. **Cache value size not bounded per-entry**: An entry could be 100MB if the review is huge. This is bounded by the total cap (50MB by default) so no individual entry can exceed 50MB. But all entries could be one huge one and the cap would be exceeded by just one entry. The eviction logic would only delete the oldest until under cap. If the new entry is bigger than the cap, the cap is exceeded indefinitely. Hmm, but in practice, reviews are bounded. Not a real issue for now. LOW.

Wait, actually let me think about this. The evict function:
```go
if total <= s.maxBytes {
    return
}
// ... evict until total <= s.maxBytes
```

If the new entry is larger than maxBytes, the loop will:
1. Total > maxBytes
2. Sort by mtime, oldest first
3. Try to delete oldest, decrement total
4. Continue until total <= maxBytes OR no more files

If the new entry is the newest and biggest, it won't be deleted (it's not the oldest). But after deleting everything else, total = size of new entry. If size of new entry > maxBytes, the loop ends with total > maxBytes. This is a bug.

But in practice, this shouldn't happen because reviews are small. Not flagging as a real issue. LOW.

27. **`os.Chtimes` doesn't preserve the original atime/mtime if it fails**: Best-effort. Documented as acceptable.

28. **Fallback doesn't share cache with primary correctly**: The fallback's CacheKey is derived from (primary's prompt, fallback's model, fallback's temp). So if the primary succeeds and caches under (primary's prompt, primary's model, primary's temp), and then the fallback runs, it caches under a different key. So both are cached. On a re-run, the primary hits its cache, and the fallback... wait, the fallback wouldn't run on re-run if the primary succeeds. So this is fine.

But what if the primary fails (timeout, etc.) and the fallback is tried? The fallback's output is cached under (primary's prompt, fallback's model, fallback's temp). On a re-run where the primary succeeds again, the primary hits its cache. If the primary fails again, the fallback tries, and... it hits the fallback's cache. So both are independently cached. Good.

But wait, the test `TestEngine_CacheHitReplaysWithoutAPICall` only tests the primary. There's no test for fallback cache behavior. Not flagging as missing test - that's a test coverage issue, not a bug.

29. **`buildFallbackAgent` uses `primary.Prompt`**: Already discussed. This is suspicious.

Actually, let me re-examine this. In Go, if `primary` is type `Agent` and you access `primary.Prompt`, the compiler will complain that `Prompt` is not a field of `Agent`. So if this is in the diff, it would be a compile error. The fact that the diff exists suggests this is the actual code... but it would fail to compile.

UNLESS `Agent` has a `Prompt` field that was added in a previous diff or in this same diff. Let me search the diff one more time...

Looking at the diff:
- `internal/fanout/engine.go` shows the Agent struct:
```go
type Agent struct {
    // (some fields)
    // constraint follows the slot, like the persona prompt).
    MinSeverity string
    MaxFindings *int

    // CacheKey is the diff-cache key (Epic 5.2) for this agent's review call,
    // derived by buildAgent/buildFallbackAgent from the FULL rendered prompt
    ...
    CacheKey string
}
```

The comment says "derived by buildAgent/buildFallbackAgent from the FULL rendered prompt". This implies the prompt is computed in those functions and passed to diffCacheKey. The CacheKey field is stored on the Agent.

In `buildAgent`:
```go
CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature),
```
Here `prompt` is a local variable.

In `buildFallbackAgent`:
```go
CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```
Here `primary.Prompt` is used. For this to compile, `primary` (type `Agent`) must have a `Prompt` field.

Looking at the Agent struct shown, there's no `Prompt` field. So this is a compile error.

UNLESS there's a hidden field I can't see. The diff format ` @@ -642,6 +685,11 @@ func buildAgent(...)` shows that the function context is `func buildAgent`, and the lines shown are in `buildAgent`. The CacheKey line at +685 is in buildAgent. The CacheKey at +772 is in buildFallbackAgent.

So in buildFallbackAgent, the only way `primary.Prompt` compiles is if Agent has a Prompt field. Looking at the Agent struct context shown in engine.go, it doesn't.

I think this is a real compile error. HIGH severity.

But wait, I should double-check. Maybe the `Prompt` field was already on Agent and just not shown in the diff context. The diff for engine.go shows:
```
@@ -46,6 +46,16 @@ type ChatCompleter interface {
```
which is around the ChatCompleter interface, and:
```
@@ -105,6 +115,16 @@ type Agent struct {
```
which is around the Agent struct.

Looking at the Agent struct additions:
```go
type Agent struct {
    // (existing fields - not shown in diff)
    // constraint follows the slot, like the persona prompt).
    MinSeverity string
    MaxFindings *int

    // CacheKey is the diff-cache key (Epic 5.2) for this agent's review call,
    ...
    CacheKey string
}
```

So the visible fields in the diff are MinSeverity, MaxFindings, CacheKey (added). I don't see a `Prompt` field. The comment "constraint follows the slot, like the persona prompt" might be referring to MinSeverity's documentation, not a Prompt field.

OK, I'm fairly confident this is a compile error. HIGH severity.

Hmm wait, I realize I should look for any reference to `primary.Prompt` in the original code. If it was there before, then the original code already had this field. Let me think...

The diff shows:
```go
		MinSeverity: primary.MinSeverity,
		MaxFindings: primary.MaxFindings,
+		// Diff-cache key (Epic 5.2): a fallback reviews the SAME rendered prompt as
+		// the primary but on its OWN model and temperature, so it keys on the
+		// primary's prompt with the fallback's model/temperature — a substitute
+		// model must not collide with the primary's cache entry.
+		CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
```

The `+` lines are added. So `primary.Prompt` is new in this diff. This means the original code didn't have it. And the diff doesn't add a Prompt field to Agent.

So this is HIGH severity: the diff references `primary.Prompt` but `Agent` doesn't have a `Prompt` field, and no such field is added in this diff.

Wait, but what if there are other parts of the diff I'm missing? Let me search the entire diff for any reference to a `Prompt` field on `Agent`...

Searching the diff... I see `Invocation.Prompt` in tests and `prompt` (local var) in `buildAgent`. No `Agent.Prompt` field. So this is indeed a bug.

30. **`buildFallbackAgent` may not have access to `primary` with all needed fields**: The `primary` is passed in as an argument. It would have the fields that `buildAgent` set, including `CacheKey` (if added). But the prompt... `primary` would have `Invocation.Prompt` if buildAgent set it. So the fix is to use `primary.Invocation.Prompt`.

OK so this is a real issue. HIGH severity.

31. **Test that uses `f.failFor["m"]`**: This is a map access that might not be initialized in `newFake()`. Let me check... the test does `f.failFor["m"] = assertFailErr`. If `f.failFor` is nil, this would panic. But the `newFake` constructor probably initializes it. Not flagging without seeing the full code.

32. **Potential issue with `r1[0].CacheHit` in `TestEngine_NoCacheBypassesReadButStillWrites`**: After seeding the cache, the test runs with `cacheNoRead=true`. It expects `r[0].CacheHit == false`. But the test also calls `NewEngine(f, WithCache(store, true)).Run(...)` - this creates a new engine. Is the cache store shared? Yes, the same `store` is passed. The cache is content-addressed, so the entry from the first run is still there. With `cacheNoRead=true`, the engine skips the read, so it doesn't hit the cache. Then it calls live and writes. Good.

But wait, the comment says "no-cache run must still write fresh results". The test checks this by re-running with `cacheNoRead=false` and verifying the cache hits. Good.

33. **`TestRunReview_SecondRunServedFromCache` initial run sets up cache, then second run hits cache**: This is the end-to-end test. But there's a potential issue: between runs, the `cacheStore` is on disk. If the test was run multiple times in a flaky manner, this could break. But each test uses t.TempDir() so it's isolated. Good.

34. **`twoAgentConfig` helper**: The test uses this. If the helper creates agents with different prompts, the cache might not hit. Let me think... If both agents review the same diff, they'd have different prompts (different personas, etc.), so different cache keys. That's expected.

35. **`maxTokensPtr` constant**: Not relevant to cache.

36. **Documentation of `cache_max_bytes` says "Default 50 MiB"**: The actual default is 50 * 1024 * 1024 = 52428800 bytes = 50 MiB. Documentation matches.

37. **Tool agents never reach `invokeCachedSingleShot`**: Confirmed. Good.

38. **`buildFallbackAgent` doesn't include `PayloadMode`**: Looking at the diff, it includes `PayloadMode` already? Let me check the diff... yes, the existing code includes it. Not a new concern.

39. **`buildFallbackAgent` doesn't include `Truncation`**: Same as above, existing.

40. **Possible nil pointer dereference in `BuildFallbackAgent`**: If `ac.Temperature` is nil and the function tries to use it... but the `diffCacheKey` function handles nil temperature. Good.

41. **What's the deal with the `--no-cache` flag in `cmd/atcr/review.go`**: It's defined and parsed. The boolFlag pattern is consistent with `--force`. Good.

42. **`--no-cache` and `--resume` interaction**: The diff doesn't add a mutual exclusion. So `--no-cache` works with `--resume`. This means a resume can also bypass the cache. Looking at `PrepareResume`, the `cacheNoRead` is set from `req.NoCache`. So a resumed review with --no-cache would bypass cache reads. Good.

But wait, the README says `--no-cache` is a per-run bypass. It doesn't say it conflicts with --resume. So the lack of mutual exclusion is correct.

43. **`--no-cache` and `--force` interaction**: The diff doesn't add a mutual exclusion. So both can be set. `--force` overwrites the review directory. `--no-cache` bypasses cache. These don't conflict. Good.

44. **`ReviewRequest` struct has `NoCache` field**: Added correctly.

45. **`cacheSettings` test verifies negative values are rejected**: Good.

46. **`cacheSettings` test verifies the default is 50 MiB**: Good.

47. **`cacheSettings` test verifies project overrides registry**: Good.

48. **`cacheSettings` test verifies explicit 0 is unbounded**: Good.

49. **What if `cache_max_bytes` is unset in both project and registry?**: Falls through to `DefaultCacheMaxBytes = 50 MiB`. Good.

50. **`atomicfs.WriteJSON` not visible in this diff**: It's an existing function. The cache uses it. Good.

51. **`evict()` uses `os.ReadDir` and ignores errors**: If the cache dir doesn't exist, `ReadDir` returns an error, and `evict` returns. This is the case when no entries exist. The `os.MkdirAll` in `Put` ensures the dir exists. But `Get` doesn't create the dir. So `ReadDir` could fail in `evict` if called from `Get`... but `evict` is only called from `Put`. Good.

52. **`Put` calls `MkdirAll` then `WriteJSON` then `evict`**: If `MkdirAll` fails, returns error. If `WriteJSON` fails, returns error. If `evict` has an issue, it returns nil (best effort). Good.

53. **Concurrency of `evict`**: Held under mutex. Good.

54. **`Get` is called concurrently with `Put`**: Held under mutex. Good.

55. **The AgentStatus field `CacheHit` is JSON-serialized with `omitempty`**: A live result will have `cache_hit:false`, but omitempty excludes it. So a live review's status.json doesn't change shape. Good. A cached result will have `cache_hit:true`. Good.

56. **`Result.CacheHit` is also omitempty in `statusFor`**: Looking at the diff:
```go
CacheHit: r.CacheHit,
```
This is added to the status struct construction. The status struct's field has `omitempty`. Good.

57. **`TestEngine_DifferentTemperatureMissesCache` test creates a `mk` function that uses a closure to capture temp**: This is fine.

58. **The `invokeCachedSingleShot` returns a Result without `TrippedBudgets`, `ToolsDegraded`, etc.**: These are zero values. A cached result doesn't have these, which is correct.

But wait, the synthesized Result doesn't have a `Truncation` field set to the primary's truncation. Let me re-read:
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

`Truncation` is set to `a.Truncation`. So the truncation indicator is preserved. Good.

But what about `TrippedBudgets`? This is a list of budget names that were tripped. A cached result wouldn't have this (the budgets were tripped during the original call). But on a cache hit, we should still indicate that the cached result was truncated if the payload was truncated. Hmm, this might be a concern - the `Truncation` field is preserved, but `TrippedBudgets` is not.

Looking at the code, `Truncation` is a single field that says the payload was truncated. `TrippedBudgets` is a list of specific budget names. If the payload was truncated, the cached result should still show that.

This is a minor correctness issue. The cached result might not show `TrippedBudgets` even if the cached result was truncated. But it does show `Truncation`, so this is partially preserved.

Actually, looking more carefully, `Truncation` and `TrippedBudgets` are related but distinct. `Truncation` indicates the payload was truncated. `TrippedBudgets` indicates which specific budgets were tripped. A cached result should preserve both if the original was truncated.

This is a minor correctness issue. MEDIUM severity? But it's edge case.

Wait, the synthesized Result doesn't include the `Truncation` value of the cached result. Actually, it does - it uses `a.Truncation` which is the agent's truncation. But the cached result might have had a different truncation (e.g., it was a non-truncated version, or vice versa). Actually, since the cache key includes the prompt (which is what was sent to the model), the truncation affects the prompt, so a different truncation = different prompt = different cache key. So `a.Truncation` is consistent with the cached result. Good.

But `TrippedBudgets` is a slice. The zero value is nil. The cached result might have had non-zero `TrippedBudgets`. This is lost on cache hit. Hmm.

Actually, the `Truncation` field tells the caller that the result was truncated. `TrippedBudgets` is more detailed. The cached result still has the truncation indicator, just not the detailed list.

This is a minor issue but probably acceptable. Not flagging.

59. **`invokeCachedSingleShot` doesn't include `Log` field**: Wait, the Result struct doesn't have a `Log` field. Looking at the diff for the Result struct:
```go
type Result struct {
    // (existing fields)
    CacheHit bool
    
    // Per-agent usage accounting
    Model string
    TokensIn int
    TokensOut int
    CostUSD float64
    CallRecords []CallRecord
    
    Err error
}
```

No `Log` field. So this is fine.

60. **The Result struct in the diff context is partial**: I see `CacheHit`, `Model`, `TokensIn/Out`, `CostUSD`, `CallRecords`, `Err`. But there might be more fields I'm not seeing. Not flagging.

61. **`buildAgent` adds `CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature)`**: The `prompt` is a local var in buildAgent. The ac.Model and ac.Temperature are agent config. Good.

But wait, what about the build agent's invocation? The `Invocation.Prompt` is set to `prompt` separately. So `CacheKey` is based on the same `prompt` that goes into the invocation. Good.

62. **`buildAgent` cache key includes `ac.Model` and `ac.Temperature`, not the resolved provider model**: If `ac.Model` is the configured model but the provider uses
</think>
