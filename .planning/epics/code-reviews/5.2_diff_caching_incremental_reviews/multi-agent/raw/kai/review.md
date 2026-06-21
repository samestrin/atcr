The user wants me to review a diff and identify issues, outputting findings in a specific pipe-delimited format. I need to strictly follow the scope constraint from the sprint plan.

Let me analyze the diff carefully, focusing only on files/areas related to Epic 5.2 (Diff Caching & Incremental Reviews).

Scope boundaries from sprint plan:
- IN scope: cache package, key derivation, integration into the fan-out read/write path, `--no-cache` flag, size-based LRU eviction with config override.
- OUT of scope: re-architecting fan-out into per-file API calls; caching the verification/skeptic stage.

Files in the diff:
1. README.md - mentions --no-cache flag. This is in scope (documentation of the flag).
2. cmd/atcr/review.go - adds --no-cache flag. In scope.
3. cmd/atcr/review_test.go - tests for --no-cache flag. In scope.
4. docs/registry.md - documents cache_max_bytes. In scope.
5. internal/boundaries_test.go - adds cache package import boundaries. In scope.
6. internal/cache/key.go - key derivation. In scope.
7. internal/cache/key_test.go - tests for key derivation. In scope.
8. internal/cache/store.go - cache store with LRU eviction. In scope.
9. internal/cache/store_test.go - tests for store. In scope.
10. internal/fanout/artifacts.go - adds CacheHit to status. In scope.
11. internal/fanout/cache_test.go - integration tests for cache in fanout. In scope.
12. internal/fanout/engine.go - integrates cache into engine. In scope.
13. internal/fanout/resume.go - wires cache for resumed agents. In scope.
14. internal/fanout/review.go - adds diffCacheKey, ReviewRequest.NoCache, PreparedReview cache fields. In scope.
15. internal/fanout/status.go - adds CacheHit to AgentStatus. In scope.
16. internal/registry/cache_settings_test.go - tests for cache settings precedence. In scope.
17. internal/registry/config.go - adds CacheMaxBytes to Registry. In scope.
18. internal/registry/precedence.go - adds CacheMaxBytes to Settings and resolution. In scope.
19. internal/registry/project.go - adds CacheMaxBytes to ProjectConfig and defaults. In scope.

Now I need to find actual issues in these changes.

Let me go through each file carefully.

**internal/cache/store.go:**
- `Get` locks mutex, reads file, unmarshals, checks key match, updates mtime with `os.Chtimes`, returns.
- `Put` locks mutex, creates dir, writes JSON via atomicfs, calls evict.
- `evict` reads dir, filters .json files, gets info, sorts by mtime, removes oldest until under cap.
- Potential issue: In `evict`, when it removes files, it uses `os.Remove(f.path)`. If `os.Remove` fails, it just continues. But it subtracts `f.size` from total ONLY if `err == nil`. That's correct.
- Potential issue: `evict` ignores errors from `os.ReadDir` - returns silently. That's fine, best effort.
- Wait, looking at `Get`: it does `s.mu.Lock()` and holds it during `os.ReadFile`. If the cache entry is large, this blocks all other cache operations. The comment says "Contention is negligible in practice", but this is a design choice, not necessarily a bug. However, holding a global mutex during file I/O is generally not great, but given the expected low concurrency and local disk, it might be acceptable. The sprint plan doesn't mention anything about performance requirements for the cache. I'll note this but maybe as LOW or not at all since they explicitly chose simpler locking.
- In `Get`, if `json.Unmarshal` fails or key mismatch, it removes the file and returns miss. That's good self-healing.
- In `Get`, the `os.Chtimes` error is ignored. That's fine, it's best-effort for LRU.
- In `Put`, `os.MkdirAll` is called while holding the lock. Fine.
- In `evict`, there's a subtle issue: `info, err := de.Info()` can fail. It `continue`s on error. But `de.Info()` might not return an error for a file that exists. Seems okay.
- Wait, `evict` sorts by `mtime`. It uses `files[i].mtime.Before(files[j].mtime)`. This sorts oldest first. Then it evicts oldest first. That's correct for LRU.
- However, `evict` only considers `.json` files. What if there are other files in the directory? They are ignored for size calculation. That's fine.
- But what if `total` is calculated incorrectly? `total += info.Size()` for each .json file. Then if total > maxBytes, it removes files and subtracts. This could be slightly off due to filesystem block sizes vs actual file sizes, but the cap is approximate anyway. The sprint says "total-size cap" so file size is reasonable.
- Wait, looking at `fileName`: it validates hex64. Good.
- Is there any issue with `atomicfs.WriteJSON`? I don't know the implementation, but presumably it writes atomically.

**internal/cache/key.go:**
- `HashText` returns `sha256:` prefix + hex.
- `Key` joins promptHash, model, tuning with NUL bytes and hashes again.
- Wait, the comment says "joined with a NUL separator (which cannot appear in a hex digest, a model id, or a numeric tuning token)". But `tuning` is not necessarily a numeric tuning token in the implementation. Looking at `diffCacheKey` in `internal/fanout/review.go`:
  ```go
  temp := "default"
  if temperature != nil {
      temp = strconv.FormatFloat(*temperature, 'g', -1, 64)
  }
  return cache.Key(cache.HashText(prompt), model, temp)
  ```
  So `tuning` can be a string like "0.9". NUL cannot appear in that either since it's from `FormatFloat`. So that's fine.
- But wait, the comment in `key.go` says "Key derives the content-addressed cache key for one reviewer call from the digest of the full rendered prompt ... and a tuning token (the temperature)." But then it says `promptHash is pre-computed via HashText so the large prompt text is hashed once at agent-build time rather than on every lookup.` Then `Key(promptHash, model, tuning)` hashes promptHash + NUL + model + NUL + tuning.
- The third parameter is described as "tuning token (the temperature)" but it's just a string parameter named `tuning`. In `diffCacheKey`, it's called with `temp` which is the temperature string. But in `buildAgent` and `buildFallbackAgent`, `diffCacheKey(prompt, ac.Model, ac.Temperature)` is called where `ac.Temperature` is a `*float64`. Wait, but `diffCacheKey` takes `temperature *float64`. So `ac.Temperature` is passed directly. That seems correct.
- Wait, the `Key` function signature is `Key(promptHash, model, tuning string)`. In `diffCacheKey`, the third arg is converted to string via `strconv.FormatFloat`. Good.

**internal/fanout/engine.go:**
- `invokeCachedSingleShot`: if `e.cache == nil || a.CacheKey == ""` -> live call.
- Then if `!e.cacheNoRead`, try cache get. If error, log warning and fall through. If hit, return synthesized Result with `CacheHit: true`.
- If miss or no-read, call `invokeSingleShot`. If status OK, try to cache.Put. If Put errors, log warning.
- This looks solid.
- Wait, the synthesized Result on cache hit sets `Model: a.Invocation.Model`. But what about other fields like `TokensIn`, `TokensOut`, `CallRecords`? They are zero values which is correct. But what about `Cost`? It's not set, so zero. That's correct per design (no cost for cache hit).
- However, there's a subtle correctness issue: the returned `Result` on cache hit doesn't set `FallbackFrom`. That's fine because a cache hit wouldn't be a fallback. But what if the original result that was cached was from a fallback agent? Actually, the cache key for a fallback agent includes the fallback's model and temperature, and the primary's prompt. So if the primary fails and fallback succeeds, the fallback's result is cached under the fallback's key. On replay, `Agent` structure carries the fallback agent's info, so `FallbackFrom` should be empty because the fallback agent itself is the primary in its slot? Wait, looking at the structure, a fallback agent is built with `buildFallbackAgent` which sets `Name`, `Invocation`, etc. The `FallbackFrom` field in Result is set in `invokeAgent` when a fallback is actually invoked. But `invokeCachedSingleShot` is called from `dispatchAgent` -> `invokeAgent`? Let me trace.
- Actually, `dispatchAgent` is called for each slot. Looking at the engine code (not fully shown but implied), `dispatchAgent` handles primary and fallback. But `invokeCachedSingleShot` is only called in `dispatchAgent` for the final agent dispatch. Wait, `dispatchAgent` is:
  ```go
  func (e *Engine) dispatchAgent(ctx context.Context, a Agent) Result {
      // tool loop check
      // degraded check
      return e.invokeCachedSingleShot(ctx, a)
  }
  ```
  Wait, but where is the fallback handled? It's probably in the caller of `dispatchAgent`, like in a loop that tries primary then fallback. But `dispatchAgent` takes `a Agent`, so if it's the fallback, `a` is the fallback agent. The cache key for the fallback is computed in `buildFallbackAgent` using its own model and temperature but the primary's prompt. So the cache key is unique to the fallback. On cache hit, `Result` has `StatusOK` and no `FallbackFrom`. That's correct because the fallback agent is being run directly from cache; the fact that it's a fallback is implicit in the slot structure, but `FallbackFrom` in Result is typically set when a live fallback occurs. If we replay a cached fallback result, does `FallbackFrom` matter? The `statusFor` function in artifacts.go includes `FallbackFrom`. If a fallback result is cached and replayed, `FallbackFrom` would be empty in the replay, but the slot structure knows it was a fallback position. However, `FallbackFrom` is about the result of the live call - if the primary failed and fallback was used. If we cache a fallback result and replay it, we might lose the information that this was originally a fallback. But the cache key is for the fallback agent itself, so on a subsequent run, if the primary is also cached, we'd never reach the fallback. If the primary fails (not cached? or cache miss?), and the fallback is cached, we'd replay the fallback from cache. But `FallbackFrom` would be empty. Is this an issue?
  
  Actually, looking more carefully at how `FallbackFrom` is set - it's not shown in the diff, but in the original code, `FallbackFrom` is likely set when the engine tries primary, it fails, then tries fallback. If the fallback result comes from cache, the engine code that wraps `dispatchAgent` probably doesn't set `FallbackFrom` because `dispatchAgent` just returns the result. But wait, if the primary is not cached or fails, and then the fallback is dispatched, `dispatchAgent` returns a Result. The caller might set `FallbackFrom` on that result. But in `invokeCachedSingleShot`, if it's a cache hit, the result comes back with no `FallbackFrom`. If the caller sets `FallbackFrom` on the result after `dispatchAgent` returns, that's fine. But if `dispatchAgent` is only called for the fallback agent (because the primary was already handled elsewhere), then `FallbackFrom` might not be set.
  
  However, this is getting into code paths not fully visible in the diff. The diff only shows `dispatchAgent` calling `invokeCachedSingleShot` directly at the end. Before the diff, it called `invokeSingleShot`. So the fallback logic is likely outside `dispatchAgent`. I shouldn't flag something I can't verify.

Let me look for more concrete issues.

**internal/cache/store.go:**
- In `Get`, when a corrupt entry is found, it removes the file and returns miss. But it holds the lock while doing `os.Remove`. That's fine.
- In `Get`, the defensive key mismatch check: `e.Key != key`. If the filename matches but the stored key doesn't, it removes the file. This guards against filename collisions. Good.
- Wait, `fileName` returns `digest + ".json"`. Since the digest is hex64, it's 64 chars + 5 = 69 chars. On some filesystems there might be length limits, but 69 chars is fine.

**internal/fanout/review.go:**
- `diffCacheKey` function:
  ```go
  func diffCacheKey(prompt, model string, temperature *float64) string {
      temp := "default"
      if temperature != nil {
          temp = strconv.FormatFloat(*temperature, 'g', -1, 64)
      }
      return cache.Key(cache.HashText(prompt), model, temp)
  }
  ```
  Wait! The sprint plan says: "Cache key = sha256(payload_bytes) + model_id + sha256(persona_text). Agent name is redundant once persona content + model are hashed. byte-budget/truncation is captured implicitly (different budget → different payload text → different hash)."
  
  But in the implementation, `diffCacheKey` uses `cache.HashText(prompt)` where `prompt` is the FULL RENDERED PROMPT. The comment says: "the prompt subsumes payload + persona + the per-agent scope focus + the base/head refs". So `promptHash` is `sha256(prompt)`. Then it passes `model` and `temp` as the other two components. But `cache.Key` hashes these three together. So the final key is `sha256(sha256(prompt) + NUL + model + NUL + temp)`. That's slightly different from `sha256(payload_bytes) + model_id + sha256(persona_text)` mentioned in the sprint plan, but the sprint plan says "key = sha256(payload_bytes) + model_id + sha256(persona_text)" as the conceptual formula, and the implementation uses the rendered prompt which embeds all of that. The sprint plan clarifications say: "Cache a whole per-agent review result keyed on the full payload". And: "Cache key = sha256(payload_bytes) + model_id + sha256(persona_text)". But then in the code, they key on the rendered prompt instead. The code comment explains: "Keying on the rendered prompt (not the raw payload/persona) is deliberate: the prompt subsumes payload + persona + the per-agent scope focus + the base/head refs". This is a deliberate design choice and seems correct for the scope.
  
  However, looking at `buildAgent`:
  ```go
  CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature),
  ```
  Here `prompt` is the rendered prompt string.
  
  And in `buildFallbackAgent`:
  ```go
  CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
  ```
  This uses the primary's prompt but the fallback's model and temperature. This matches the comment: "a fallback reviews the SAME rendered prompt as the primary but on its OWN model and temperature". Good.

- But wait! In `buildAgent`, `ac.Temperature` might be nil. If nil, `diffCacheKey` uses "default". However, `ac.Temperature` comes from agent config. What if the actual temperature used in the invocation differs from the config temperature? Looking at `buildAgent`, `ac.Temperature` is passed to `llmclient.Invocation{..., Temperature: ac.Temperature}`. So the invocation uses the same temperature. Good.

- Wait, `diffCacheKey` takes `temperature *float64`. But `ac.Temperature` is a `*float64`? Looking at the agent config structure - not shown in diff, but it's passed directly. Assuming yes.

- In `PrepareReview`:
  ```go
  revCache := cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes)
  ```
  If `cfg.Settings.CacheMaxBytes` is 0 (unbounded), `cache.NewStore` gets `maxBytes=0`. In `store.go`, `evict` returns immediately if `maxBytes <= 0`. Good.

- In `runEngine`, `p.cache` is wired. What if `p.cache` is nil? The code checks `if p.cache != nil`. Good.

- In `PrepareResume`, similar wiring:
  ```go
  cache:       cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes),
  cacheNoRead: req.NoCache,
  ```
  Wait, `PrepareResume` returns a `PreparedReview` (or whatever type `p` is). It sets `p.cache` and `p.cacheNoRead`. But looking at `PrepareResume`, it returns `(p, info, nil)`. The struct being constructed seems to be a resume-specific struct or maybe `PreparedReview`. Looking at the diff:
  ```go
  p := &PreparedReview{...} // implicitly
  ```
  Actually the diff shows:
  ```go
  p := &...{
      Repo: ...,
      Head: ...,
      manifest: m,
      cache: ...,
      cacheNoRead: ...,
  }
  ```
  Wait, looking carefully at the diff for `internal/fanout/resume.go`:
  ```go
  p := &PreparedResume{ // or whatever? Actually it says p, info, nil and the struct has fields Repo, Head, manifest.
  ```
  Actually the diff shows:
  ```go
  	p := &...{
  		Repo:        req.Repo,
  		Head:        req.Head,
  		manifest:    m,
  		// Wire the diff cache...
  		cache:       cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes),
  		cacheNoRead: req.NoCache,
  	}
  ```
  Wait, `cacheNoRead: req.NoCache` but `req` in `PrepareResume` might not have `NoCache`? Looking at the diff for `resume.go`, it adds `cache` and `cacheNoRead` fields. But the type of `p` isn't fully clear. However, `PrepareResume` signature is `func PrepareResume(ctx context.Context, cfg *ReviewConfig, reviewDir string, req ReviewRequest) (...)`. So `req` is `ReviewRequest` which now has `NoCache` field. Good.

**internal/registry/precedence.go:**
- `ResolveSettings` applies defaults, then registry, then project.
- For `CacheMaxBytes`, it uses `if reg.CacheMaxBytes != nil { s.CacheMaxBytes = *reg.CacheMaxBytes }` and similarly for project.
- Then later it validates `if s.CacheMaxBytes < 0 { return ..., error }`.
- But wait, in `applyTier`, the four standard fields are applied. `CacheMaxBytes` is not in `applyTier` because it's a separate field. That's fine.
- However, there's a potential issue: `applyTier` handles `reg.PayloadMode`, `reg.TimeoutSecs`, `reg.PayloadByteBudget`, `reg.MaxParallel`. `CacheMaxBytes` is handled separately. This is consistent with the comment that says "CacheMaxBytes lives at the registry (global) and project tiers only, like the retry tunables". Good.

**internal/registry/project.go:**
- `DefaultProjectConfigYAML` now includes `cache_max_bytes: 52428800` (50 MiB). Good.
- `LoadProjectConfig` validates `CacheMaxBytes >= 0`. Good.

**internal/fanout/cache_test.go:**
- `TestRunReview_SecondRunServedFromCache` uses `RunReview` with `reviewReq(repo, repo, base, head)`. It asserts `assert.DirExists(t, filepath.Join(repo, ".atcr", "cache"))`. But `RunReview` might create the cache dir only on first Put. The store creates dir lazily on first Put. So if all agents hit cache? Actually first run is cold, so Put happens. Dir should exist. Fine.
- `countingProvider` resets circuit breaker. Good.
- `TestEngine_DifferentTemperatureMissesCache` uses `hot, cold := 0.9, 0.1` and `mk(&hot)`. The key derives from temperature. Good.
- But wait, `TestEngine_DifferentTemperatureMissesCache` calls `NewEngine(f, WithCache(store, false)).Run(...)`. It passes `Slot{Primary: Agent{..., CacheKey: diffCacheKey("same prompt", "m", temp), Invocation: ...}}`. But `diffCacheKey` takes `temperature *float64`. In the test, `temp` is a `*float64` pointing to `hot` or `cold`. But `diffCacheKey` inside the test uses the local `temp` variable. Actually in the test code:
  ```go
  mk := func(temp *float64) Slot {
      return Slot{Primary: Agent{
          Name:        "a",
          PayloadMode: "blocks",
          CacheKey:    diffCacheKey("same prompt", "m", temp),
          Invocation:  llmclient.Invocation{Model: "m", Prompt: "same prompt", Temperature: temp},
      }}
  }
  ```
  So the cache key is built with the temperature pointer. Good.

**Potential issues I can identify:**

1. **Security/Correctness in cache/store.go `evict`**: The eviction only looks at `.json` files. If a non-`.json` file exists in the cache dir, it's ignored for size calculation. However, `Put` writes `.json` files via `atomicfs.WriteJSON`. But what about temp files from `atomicfs.WriteJSON`? If atomicfs writes a temp file that doesn't have `.json` extension or gets interrupted, it might leave junk. But that's an atomicfs issue, not this code's issue. The eviction would ignore non-.json files, meaning the total size calculation might be slightly under the true dir size, but the cap is for cache entries, not strictly total directory size. The comment says "Total on-disk size is capped at maxBytes via least-recently-used eviction" and "Each entry is one JSON file named by the key digest under dir." So it only counts JSON files. This seems intentional.

2. **Race condition / concurrency issue in cache/store.go**: `Get` updates mtime with `os.Chtimes` while holding the mutex. On some systems, `Chtimes` might be slow. But as noted, they explicitly chose a single mutex. Not a bug.

3. **Potential data loss / correctness issue in `PrepareResume`**: Looking at `internal/fanout/resume.go`, it constructs a new cache store for the resumed review. But if a review is resumed, the original review might have already populated the cache. Creating a new store pointing to the same directory is fine (it's just a struct with dir path). However, the resume flow might call agents that already ran. The cache would serve them if they were previously cached. But the resume is supposed to run only pending agents. If a pending agent's result was already in cache from a previous run, should it be served from cache? The sprint plan says: "Wire the diff cache for resumed agents too (Epic 5.2): a resumed agent's fresh output is written back so a later full run benefits — matching the 'fresh results are always written' contract — rather than being re-called." Wait, the comment says "rather than being re-called" but that seems to imply it's using cache to avoid re-calling? Actually the comment says "a resumed agent's fresh output is written back so a later full run benefits". Hmm. The comment in the code says:
  ```go
  // Wire the diff cache for resumed agents too (Epic 5.2): a resumed agent's
  // fresh output is written back so a later full run benefits — matching the
  // "fresh results are always written" contract — rather than being re-called.
  ```
  This wording is a bit confusing. If it's read from cache, it's not "fresh output". But the cache is wired for both read and write. If an agent is pending and its result is already in cache, on resume it would be served from cache and not re-called. Is that desired? The comment says "rather than being re-called" suggesting they want to avoid re-calling. But if the previous run failed and we're resuming, maybe we want to re-call? Actually, if the previous run cached a successful result for an agent, and then the review crashed during reconciliation, resuming should not need to re-call that agent. Using cache is correct and efficient.

4. **Issue with `CacheMaxBytes` default in `ResolveSettings`**: The code sets `CacheMaxBytes: DefaultCacheMaxBytes` in the default settings. Then if registry has a value, it applies. Then if project has a value, it applies. But what about `applyTier`? `applyTier` is called with `reg.TimeoutSecs`, `reg.PayloadByteBudget`, etc. For project too. `CacheMaxBytes` is handled separately. That's fine.

5. **Bug in `diffCacheKey` / `Key`**: `cache.Key` uses `h.Write([]byte(promptHash))`. `promptHash` is already a string like "sha256:abc...". Then it writes a NUL, then model, then NUL, then tuning. So the inner data is `sha256:<hex>\x00<model>\x00<tuning>`. Then this is hashed to produce the final key. This is fine.

6. **Issue in `internal/fanout/engine.go` `invokeCachedSingleShot`**: On cache hit, it returns a `Result` with `Status: StatusOK`. But what if the original cached result had `StatusOK` but was actually a fallback result? Wait, the cache key for fallback is different (fallback model/temperature). So if the fallback is replayed, it returns `StatusOK`. The outer caller in the engine probably checks if primary succeeded, and if not, tries fallback. If the fallback result is cached, `dispatchAgent` returns it directly. But does the outer loop know that the fallback succeeded? It would just see `StatusOK`. That's fine.

7. **Real issue**: In `internal/fanout/engine.go`, the `invokeCachedSingleShot` function returns a synthesized Result on cache hit. But it does NOT set `ContentType` or other fields that might be expected. Looking at the Result struct, `ContentType` is not in the diff but maybe exists. The diff shows Result has `Content string`, `Status Status`, etc. The synthesized result sets `Content`, `Status`, `PayloadMode`, `Truncation`, `MinSeverity`, `MaxFindings`, `Model`, `CacheHit`. But what about `Cost`? It's 0. That's correct. But what about `Duration`? It would be 0. Is that used for metrics? The metrics wrapper around `invokeAgent` might record duration. A cache hit would have near-zero duration. That's actually correct.

8. **More serious issue**: In `internal/fanout/review.go`, `diffCacheKey` is called in `buildAgent` with `ac.Temperature`. But in `buildFallbackAgent`, it's called with `ac.Temperature` where `ac` is the fallback agent config. Wait, looking at the code:
  ```go
  func buildFallbackAgent(cfg *ReviewConfig, primary Agent, name string) (Agent, error) {
      // ... finds ac (fallback agent config) ...
      CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
  }
  ```
  Is `ac.Temperature` the fallback's temperature? Yes. But what if `ac.Temperature` is nil? Then `diffCacheKey` uses "default". That's fine. But wait, the `primary.Prompt` is used. However, `primary.Prompt` is the full rendered prompt. But in `buildAgent`, the prompt is rendered with the primary's persona and scope. The fallback uses the same prompt but its own model. That's correct per comment.

9. **Potential issue in `internal/cache/store.go` `fileName`**: The function returns `digest + ".json"`. But it checks `isHex64(digest)`. However, `strings.TrimPrefix(key, "sha256:")` will return the original string if the prefix is not present. Then `digest == key` check catches that. But what if key is `sha256:` (empty digest)? Then `digest == ""`, and `isHex64("")` returns false. Good. What if key is `sha256:abc`? `digest == "abc"`, `isHex64` returns false. Good. What if key is `SHA256:ABC...` (uppercase)? `TrimPrefix` is case-sensitive, so it won't trim. Then `digest == key` is true (since prefix didn't match), returns false. Good. So case sensitivity is enforced.

10. **Issue in `internal/cache/store.go` `Get`**: It validates the key via `fileName`. If the key is malformed, it returns miss. But in `Put`, it returns error. This asymmetry might be slightly odd but not a bug. The sprint plan says malformed key should be refused. In `Get`, treating it as a miss is safe.

11. **Issue in `internal/fanout/review.go` `PrepareReview`**: `cfg.Settings.CacheMaxBytes` might be negative if directly constructed bypassing validation. But `ResolveSettings` validates it. `PrepareReview` uses it directly. If somehow negative, `cache.NewStore` would be created with negative maxBytes. In `evict`, `maxBytes <= 0` disables eviction. So a negative value would disable eviction instead of erroring. But `ResolveSettings` prevents negative values from reaching here. However, tests or direct callers might bypass. The `cache.NewStore` comment says "maxBytes <= 0 disables eviction". So negative is treated as unbounded. That's consistent with the escape hatch, though the validation rejects negatives earlier.

12. **Big potential issue**: In `internal/fanout/review.go`, `diffCacheKey` uses `primary.Prompt` for fallback agents. But `primary.Prompt` is the rendered prompt. However, looking at the `Agent` struct, `Prompt` field is not shown in the diff for the Agent struct. Wait, let me check the Agent struct in `engine.go`:
  ```go
  type Agent struct {
      // ... fields shown ...
      // Prompt is not in the struct shown in the diff!
  }
  ```
  Wait, the diff for `engine.go` shows the `Agent` struct with fields like `Name`, `PayloadMode`, `Invocation`, etc. But `Prompt` is not listed in the diff. However, `buildAgent` returns `Agent` and `buildFallbackAgent` uses `primary.Prompt`. This implies `Agent` already had a `Prompt` field before this diff, or it's added elsewhere. Since the diff doesn't show adding `Prompt` to `Agent`, it must be pre-existing. So `primary.Prompt` exists.

13. **Wait, I found something!** In `internal/fanout/review.go`, `diffCacheKey` is defined:
  ```go
  func diffCacheKey(prompt, model string, temperature *float64) string {
      temp := "default"
      if temperature != nil {
          temp = strconv.FormatFloat(*temperature, 'g', -1, 64)
      }
      return cache.Key(cache.HashText(prompt), model, temp)
  }
  ```
  But then in `buildAgent` it's called:
  ```go
  CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature),
  ```
  And in `buildFallbackAgent`:
  ```go
  CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
  ```
  The issue: `ac.Temperature` might be nil. If nil, `diffCacheKey` uses "default". But what if the LLM client default temperature is not some fixed value but depends on the provider? The invocation will use whatever default the provider has, while the cache key uses "default". If two providers have different default temperatures but the same model name? No, model names are different. But if the same model has a nil temperature in config, the client uses its own default (maybe 1.0 or 0.7). If later the user explicitly sets temperature to that same value, the cache key would change from "default" to "0.7" (or whatever), causing an unnecessary cache miss. However, this is by design: nil means "let the client decide", and explicit value means "use this value". If the client default changes, the output might change, so "default" is arguably safer. But the sprint plan says "temperature (the tuning token)" is part of the key. It doesn't mention nil handling. Using "default" for nil seems reasonable and is unlikely to cause a bug.

14. **Security issue in `internal/fanout/cache_test.go`**: The test `TestRunReview_SecondRunServedFromCache` sets `t.Setenv("ATCR_TEST_KEY", "secret")` and uses a fake HTTP server. But the test writes actual cache entries to disk under `t.TempDir()`. The content is synthetic. No real security issue.

15. **Correctness issue in `internal/fanout/engine.go`**: The synthesized cache hit result does not set `Invocation` field on the Result. But `Result` doesn't have an `Invocation` field in the diff. It has `Model` string. The `AgentStatus` is built from `Result` in `statusFor`. Looking at `artifacts.go`, `statusFor(r Result, fr findingsResult)` builds `AgentStatus`. It uses `r.FallbackFrom`, `r.Err`, `r.Content`, etc. It doesn't seem to need anything else. But wait, `AgentStatus` has a `CacheHit` field now added. On cache hit, `r.CacheHit` is true, so `statusFor` sets `CacheHit: true`. Good.

16. **Wait, I see a potential bug in `internal/cache/store.go` `evict`**: 
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
  If `os.Remove` fails (e.g., permission denied), `total` is not decremented, and the loop continues to the next file. But if removal keeps failing, it might loop through all files and still be over cap. That's fine, it does its best. Not a bug.

17. **Potential issue: Cache dir permissions**:
  In `Store.Put`:
  ```go
  if err := os.MkdirAll(s.dir, 0o755); err != nil {
      return err
  }
  ```
  This creates the cache dir with 0755. That's world-readable. The cache contains review outputs (which might contain code snippets from the diff). But the diff itself is already in the repo. However, if the review output contains sensitive findings or if the cache is on a shared system, 0755 might be too permissive. But `.atcr/` is typically in the project root which is user-owned. Still, other files in `.atcr/` might be 0755 too. The sprint plan doesn't mention permissions. Not a strong issue.

18. **Potential correctness issue in `internal/fanout/resume.go`**: 
  The diff shows:
  ```go
  cache:       cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes),
  cacheNoRead: req.NoCache,
  ```
  But the struct literal is missing a trailing comma on the previous line `manifest: m,`? Wait, looking at the diff:
  ```go
  		Repo:        req.Repo,
  		Head:        req.Head,
  		manifest:    m,
+		// Wire the diff cache for resumed agents too (Epic 5.2): a resumed agent's
+		// fresh output is written back so a later full run benefits — matching the
+		// "fresh results are always written" contract — rather than being re-called.
+		cache:       cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes),
+		cacheNoRead: req.NoCache,
  	}
  ```
  This is fine. Go allows trailing commas in struct literals.

19. **Wait! Found a real issue in `internal/fanout/review.go`**:
  Looking at `buildAgent`, it sets:
  ```go
  CacheKey: diffCacheKey(prompt, ac.Model, ac.Temperature),
  ```
  And then later:
  ```go
  Invocation: llmclient.Invocation{
      BaseURL:     prov.BaseURL,
      APIKeyEnv:   prov.APIKeyEnv,
      Model:       ac.Model,
      Prompt:      prompt,
      Temperature: ac.Temperature,
      MaxTokens:   maxTokensPtr(),
  },
  ```
  But `diffCacheKey` uses `ac.Model` and `ac.Temperature`. However, the `llmclient.Invocation` also has `MaxTokens`. The `diffCacheKey` comment says: "MaxTokens is constant across review agents (defaultMaxTokens), so it is intentionally omitted." That's fine.

  But what about `buildFallbackAgent`?
  ```go
  CacheKey: diffCacheKey(primary.Prompt, ac.Model, ac.Temperature),
  Invocation: llmclient.Invocation{
      BaseURL:     prov.BaseURL,
      APIKeyEnv:   prov.APIKeyEnv,
      Model:       ac.Model,
      Prompt:      primary.Prompt,
      Temperature: ac.Temperature,
      MaxTokens:   maxTokensPtr(),
  },
  ```
  The fallback uses `ac.Model` and `ac.Temperature` from the fallback config. That's correct.

20. **Wait! I see