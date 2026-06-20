The user wants me to review a diff and identify issues, outputting findings in a specific pipe-delimited format (TD_STREAM). I need to strictly follow the scope constraint from the sprint plan (Epic 4.6: Robust Rate-Limit & Backoff Handling).

Scope IN scope:
- max_retries + initial_backoff_ms config fields on Registry + AgentConfig (YAML + validation mirroring TimeoutSecs)
- Settings resolution with embedded defaults (5 / 500ms) + Effective* override methods
- Wiring resolved retry settings into production llmclient.New(WithRetry(...)) construction at review/resume paths
- Threading resolved Settings into production review/resume client construction
- The existing engine in dispatch() at internal/llmclient/client.go:408
- internal/registry/config.go, internal/registry/precedence.go, internal/fanout/engine.go, internal/fanout/review.go, internal/verify/invoke.go, docs/registry.md, tests

Scope OUT of scope:
- Separate RetryMiddleware type
- exposing backoff_factor / max_backoff_ms
- changing provider SDKs
- any change to circuit-breaker boundary
- changing defaultMaxRetries = 2 constant (unless...)
- doctor/serve bare-New() fallback unless those paths already resolve Settings

Let's analyze the diff carefully.

Files changed:
1. docs/registry.md
2. internal/fanout/engine.go
3. internal/fanout/retry_wiring_test.go (new)
4. internal/fanout/review.go
5. internal/llmclient/client.go
6. internal/llmclient/retry_override.go (new)
7. internal/llmclient/retry_override_test.go (new)
8. internal/registry/config.go
9. internal/registry/precedence.go
10. internal/registry/retry_config_test.go (new)
11. internal/verify/invoke.go
12. internal/verify/invoke_test.go

Let's look for issues:

**internal/fanout/engine.go:**
- invokeAgent adds `llmclient.WithRetryOverride(ctx, a.MaxRetries, time.Duration(a.InitialBackoffMs)*time.Millisecond)` when `a.InitialBackoffMs > 0`.
- This means if InitialBackoffMs is 0 (unset or explicitly 0), the override is not applied.
- But what if someone sets MaxRetries=0 and InitialBackoffMs=500? According to the code, `InitialBackoffMs > 0` would trigger the override, which is correct.
- What if someone sets MaxRetries=5 and InitialBackoffMs=0? The override is not applied, and the client default is used. But if they explicitly set InitialBackoffMs=0, it might be invalid (validation says 1..30000). However, the Agent struct uses `int`, not `*int`, so a bare Agent (doctor/direct construction) leaves it 0. That's the documented sentinel. This seems okay.
- But wait: if a per-agent override sets `MaxRetries` but leaves `InitialBackoffMs` unset (nil), then `EffectiveInitialBackoffMs` returns the global setting (500), so `InitialBackoffMs` will be > 0, and the override is applied. That's fine.
- If a per-agent override sets `InitialBackoffMs` explicitly but leaves `MaxRetries` unset (nil), then `EffectiveMaxRetries` returns global (5), and `InitialBackoffMs` will be > 0, override applied. Fine.
- The comment says "InitialBackoffMs > 0 is the 'configured' sentinel". This means a doctor/direct construction Agent with InitialBackoffMs=0 won't apply the override. That's intended.

**internal/llmclient/client.go:**
- `dispatch` now reads override from context.
- It does `delay = clampBackoff(delay)` before the loop. This clamps the initial delay to maxBackoff (30s). The comment explains this is so the first retry sleep respects maxBackoff. But wait: `delay` is used for the sleep `if attempt > 0`. On attempt 1 (the first retry), it sleeps `delay` (which is clamped). Then after sleeping, if it continues, `delay = clampBackoff(time.Duration(float64(delay) * defaultBackoffFactor))`. So the first sleep uses the clamped initial delay. This seems correct.
- However, what if `maxRetries` from the context override is huge? The code uses `maxRetries` correctly.
- One issue: `if o, ok := retryOverrideFromContext(ctx); ok { maxRetries = o.maxRetries; delay = o.initialBackoff }`. There's no clamping of `maxRetries`. But the registry validation ensures it's within 0..10, so it's bounded.
- Wait: `clampBackoff(delay)` is called. But what if `delay` is 0 (e.g., test uses 1ms)? 1ms is within 30s, so it's fine.
- The `lastErr` handling: previously the loop used `c.maxRetries`. Now it uses `maxRetries`. On transport errors, it checks `attempt < maxRetries`. On 5xx/retryable status, same. This looks correct.
- But what about the `isBreakerFailure` logic? The sprint plan says: "The terminal-failure → breaker boundary is unchanged: an exhausted 429 returns Transient wrapping HTTPStatusError{Status: 429}; isBreakerFailure returns false (429 < 500) so send() calls RecordSuccess()". Looking at the diff, `client.go` changes only in `dispatch`. The `send` method (around line 354) is not shown in the diff. We can't review what we don't see. The diff shows `isBreakerFailure` is unchanged. So no issue to flag there.

**internal/llmclient/retry_override.go:**
- `WithRetryOverride` clamps negative maxRetries to 0. This is good.
- It uses an unexported struct key. Good.
- The `retryOverride` struct stores `maxRetries` and `initialBackoff`. Good.

**internal/registry/config.go:**
- `AgentConfig` gets `MaxRetries *int` and `InitialBackoffMs *int`.
- `Registry` gets `MaxRetries *int` and `InitialBackoffMs *int`.
- Validation in `validate()` checks registry tier: `MaxRetries` within 0..MaxRetriesCap, `InitialBackoffMs` within 1..MaxInitialBackoffMs.
- Validation in `validateAgent` checks agent tier: same ranges.
- `EffectiveMaxRetries` and `EffectiveInitialBackoffMs` methods added. Good.

**internal/registry/precedence.go:**
- Constants added: `DefaultMaxRetries = 5`, `MaxRetriesCap = 10`, `DefaultInitialBackoffMs = 500`, `MaxInitialBackoffMs = 30000`.
- `Settings` struct gets `MaxRetries int` and `InitialBackoffMs int`.
- `ResolveSettings` applies defaults: `MaxRetries: DefaultMaxRetries`, `InitialBackoffMs: DefaultInitialBackoffMs`.
- Then applies registry tier via explicit if-checks (not through `applyTier` because project tier doesn't carry them). Good.
- Post-resolution validation checks `s.MaxRetries` and `s.InitialBackoffMs` ranges.
- Also checks per-agent overrides directly in `reg.Agents`. This is defense-in-depth. Good.
- Wait: the per-agent validation iterates `sortedKeys(reg.Agents)`. What if `reg` is nil? It checks `if reg != nil` before the loop. Good.

**internal/verify/invoke.go:**
- `buildSkepticAgent` sets `MaxRetries: derefInt(c.MaxRetries)` and `InitialBackoffMs: derefInt(c.InitialBackoffMs)`.
- `derefInt` returns 0 for nil. Since the engine only applies override if `InitialBackoffMs > 0`, a nil budget keeps the client default. This matches the comment.

**Tests:**
- Various tests added. They seem fine.

Now, looking for issues:

1. **Potential issue in `internal/fanout/engine.go`**: The `invokeAgent` method uses `InitialBackoffMs > 0` as the sentinel to apply the override. However, `MaxRetries` is also part of the override. If `InitialBackoffMs` is globally set to, say, 500, and an agent explicitly sets `MaxRetries=0` but leaves `InitialBackoffMs` unset (inheriting 500), then `InitialBackoffMs` is 500 (>0), and the override is applied with `MaxRetries=0`. That's correct.
   But what if an agent explicitly sets `MaxRetries=10` and explicitly sets `InitialBackoffMs=0`? The validation in `validateAgent` would reject `InitialBackoffMs=0` because it must be within `1..30000`. So `InitialBackoffMs` cannot be 0 if it comes from config.
   However, what if it's a bare Agent constructed directly (doctor/direct)? `InitialBackoffMs` is 0, so no override is applied. That's intended.
   Is there any case where we want to apply an override with `MaxRetries` but not `InitialBackoffMs`? The override struct requires both. Since they are always resolved together via `Effective*`, if one is set, the other is resolved (either explicitly or inherited). So they should both be non-zero (assuming global defaults). This seems okay.

2. **Potential issue in `internal/registry/precedence.go`**: The `applyTier` function is used for `PayloadMode`, `TimeoutSecs`, `PayloadByteBudget`, `MaxParallel`. The retry tunables are applied manually after. This is fine.

3. **Potential issue in `internal/llmclient/client.go`**: The `clampBackoff(delay)` is called before the loop. The existing code likely had `delay := c.initialBackoff` and then inside the `attempt > 0` block, `delay = clampBackoff(time.Duration(float64(delay) * defaultBackoffFactor))`. So previously, the first sleep used the unclamped `c.initialBackoff`. Now it clamps it. This is an improvement and within scope (protecting against misconfigured initial_backoff). No issue.

4. **Potential issue in `internal/llmclient/client.go`**: The `honorExact` flag is reset to false at the top of the loop? Let's trace:
   ```go
   honorExact := false
   for attempt := 0; attempt <= maxRetries; attempt++ {
       if attempt > 0 {
           sleepFor := delay
           if !honorExact {
               sleepFor = jitter(sleepFor)
           }
           // ... sleep ...
           honorExact = false // Is it reset? The diff doesn't show this line.
       }
       // ... do request ...
       case retryableStatus[status]:
           if attempt < maxRetries {
               if retryAfter > 0 {
                   delay = retryAfter
                   honorExact = true
               } else {
                   delay = clampBackoff(time.Duration(float64(delay) * defaultBackoffFactor))
                   honorExact = false // ??? 
               }
           }
   ```
   The diff shows the `dispatch` function but doesn't show the full body, only the changed parts. It's hard to tell if `honorExact` is properly reset. But since the diff is a patch, the lines not shown are unchanged. The existing code presumably handles it. Since we only review changed lines, and no change was made to the `honorExact` logic other than it now being declared before the loop (it was likely there before), I shouldn't flag this unless the diff introduces a bug. The diff doesn't show modification of the `honorExact` handling inside the loop.

Wait, looking closely at the diff for `internal/llmclient/client.go`:
```go
 func (c *Client) dispatch(ctx context.Context, endpoint, key string, body []byte) ([]byte, error) {
 	var lastErr error
+	// The retry budget and base delay come from the client by default, but a
+	// per-call override on the context (Epic 4.6: the fan-out's resolved
+	// per-agent max_retries / initial_backoff_ms) takes precedence. The 1.5x
+	// factor and the maxBackoff cap stay fixed implementation constants.
+	maxRetries := c.maxRetries
 	delay := c.initialBackoff
+	if o, ok := retryOverrideFromContext(ctx); ok {
+		maxRetries = o.maxRetries
+		delay = o.initialBackoff
+	}
+	// Clamp the starting delay so even the FIRST retry sleep respects maxBackoff:
+	// every subsequent delay is clamped after the ×factor step, but without this
+	// an out-of-range base (a misconfigured initial_backoff or a direct
+	// WithRetryOverride) would sleep its full unclamped duration on attempt 1.
+	delay = clampBackoff(delay)
 	// honorExact is set when the next sleep is a server-advertised Retry-After
 	// cooldown, which must be slept verbatim (neither jittered down nor clamped).
 	honorExact := false
-	for attempt := 0; attempt <= c.maxRetries; attempt++ {
+	for attempt := 0; attempt <= maxRetries; attempt++ {
 		if attempt > 0 {
 			sleepFor := delay
 			if !honorExact {
@@ -435,7 +449,7 @@ func (c *Client) dispatch(ctx context.Context, endpoint, key string, body []byte
 				return nil, err
 			}
 			lastErr = err
-			if attempt < c.maxRetries {
+			if attempt < maxRetries {
 				continue
 			}
 			// Transport-level exhaustion (connection reset, EOF, DNS) is transient:
@@ -449,7 +463,7 @@ func (c *Client) dispatch(ctx context.Context, endpoint, key string, body []byte
 			return payload, nil
 		case retryableStatus[status]:
 			lastErr = httpStatusError(status, string(payload))
-			if attempt < c.maxRetries {
+			if attempt < maxRetries {
 				// Honor a server-advertised cooldown (Retry-After) over the fixed
 				// backoff when present; otherwise keep the exponential schedule.
 				if retryAfter > 0 {
```
The rest of the function is not shown. So the `honorExact` reset logic is unchanged.

Now, is there any correctness issue?

- **MEDIUM or HIGH**: `internal/fanout/engine.go` and `internal/verify/invoke.go` set `InitialBackoffMs` and `MaxRetries` on the Agent. But in `internal/fanout/review.go` `buildAgent` and `buildFallbackAgent`, the fields are set correctly.

- **Potential issue**: `buildFallbackAgent` in `internal/fanout/review.go` sets `MaxRetries` and `InitialBackoffMs` from the fallback agent's config. The comment says "Retry/backoff follow the fallback's OWN config (Epic 4.6), like TimeoutSecs". This seems correct.

- **Potential issue in tests**: `internal/fanout/retry_wiring_test.go` uses `ptrInt` and `ptrF` helper functions. Are these defined in the package? Looking at the test file, it uses `ptrF(0.7)` and `ptrInt(9)`. If these helpers aren't defined in `fanout` package tests, it won't compile. However, `review_test.go` or similar might define them. The diff doesn't show them. But this is a test compilation issue, which might be a real issue if they aren't defined. Wait, the file is `retry_wiring_test.go` in package `fanout`. If `ptrInt` and `ptrF` are not defined in that package, it's a compile error. However, I don't see their definition in the diff. Are they in other test files? Usually test helpers are in `_test.go` files in the same package. They might already exist. I cannot assume they don't exist. But let's look: `internal/fanout/review.go` uses `derefInt`, `derefInt64`, etc. It doesn't use `ptrInt`. The test `review_test.go` might have `intPtr`. Wait, the new test file uses `ptrInt` and `ptrF`. The existing code in `internal/verify/invoke_test.go` uses `intPtr` and `int64Ptr`. So the naming convention differs. If `fanout` tests already have `intPtr`, then `ptrInt` is undefined. This could be a real issue.

But I need to be careful: the diff might not include the helper definitions if they are in existing files not modified. Let's look at the test code:
```go
"greta": {Provider: "p", Model: "m", Persona: "greta", Temperature: ptrF(0.7)},
"kai": {Provider: "p", Model: "m2", Persona: "kai", Temperature: ptrF(0.7), MaxRetries: ptrInt(9)},
```
If `ptrF` and `ptrInt` are not defined in the `fanout` package tests, this file won't compile. Is there any evidence? The diff doesn't show them. Usually Go test files in the same package share helpers. I should check if the diff shows other test files in `fanout` package. It doesn't. However, `internal/registry/retry_config_test.go` uses `intPtr` (e.g., `intPtr(9)`). So `intPtr` exists in `registry` package tests. `ptrInt` in `fanout` tests might be a typo for `intPtr`? Or maybe `ptrInt` exists. This is uncertain. Since the instruction says "No false positives: Only report real issues", I should be careful. But if I'm pretty sure it's a naming mismatch, I could flag it. However, looking at `internal/fanout/review.go`, there is no `ptrInt` used in production code. It's purely a test helper concern.

Actually, looking at `internal/registry/retry_config_test.go`:
```go
withOwn := AgentConfig{MaxRetries: intPtr(9)}
```
So `registry` tests use `intPtr`.

`internal/fanout/retry_wiring_test.go` uses `ptrInt`. This is suspicious but not confirmed as an error since we can't see the whole codebase. Maybe `fanout` package tests have `ptrInt` defined in another file. I'll hold off on this unless I'm fairly certain.

- **Potential issue**: `internal/fanout/retry_wiring_test.go` defines `retryCfg()` which returns `&ReviewConfig{... Settings: registry.Settings{... MaxRetries: 5, InitialBackoffMs: 500}}`. But `ReviewConfig` is likely defined in the `fanout` package. It uses `registry.Settings`. That's fine.

- **Potential issue**: `buildAgent` in `internal/fanout/review.go` now returns `MaxRetries` and `InitialBackoffMs`. In `internal/fanout/engine.go`, `invokeAgent` checks `a.InitialBackoffMs > 0`. However, `buildAgent` sets these from `EffectiveMaxRetries` and `EffectiveInitialBackoffMs`. Since `EffectiveInitialBackoffMs` returns the resolved value (which is at least 1 because validation ensures 1..30000), `InitialBackoffMs` will always be > 0 for agents built from config. So the override will always be applied for config-built agents. This means the client's default retry budget is never used for production review agents. This is intended because the default is 2, but the resolved Settings default is 5. The production path should use 5.

But wait: what about `buildFallbackAgent`? It also sets `InitialBackoffMs` from `EffectiveInitialBackoffMs`. Since the fallback config is validated, it will be >= 1 if set, or inherited from global (which is validated to >= 1). So `InitialBackoffMs` > 0. Good.

What about `verify/invoke.go` `buildSkepticAgent`? It uses `derefInt(c.MaxRetries)` and `derefInt(c.InitialBackoffMs)`. `derefInt` on nil returns 0. So if the skeptic config doesn't set these, `InitialBackoffMs` will be 0, and the override won't be applied in `invokeAgent`. This means the skeptic uses the client's default budget (2) instead of the resolved global budget (5). Is this intended?
Looking at the comment in `invoke.go`:
```go
// A nil pointer becomes 0; the engine applies the override only when
// InitialBackoffMs > 0, so an unset budget keeps the shared client's own
// default (consistent with TimeoutSecs → 0 meaning "parent deadline only").
```
So for `TimeoutSecs`, a 0 means "use the global context deadline only". Similarly here, an unset budget means "use the client's own default". But the sprint plan says: "Default max_retries = 5 is applied at the embedded/Settings tier in ResolveSettings, NOT by changing the defaultMaxRetries = 2 constant in client.go. The constant stays the naked-New() fallback (doctor self-test, bare/test clients); production review/resume paths get 5 by threading resolved Settings into WithRetry() at construction."

However, `buildSkepticAgent` is in `verify/invoke.go`, which is part of the verify epic (Epic 3.0). The sprint plan says: "Thread resolved Settings into the production review/resume client construction; leave the doctor/serve bare-New() fallback on the built-in 2 default unless those paths already resolve Settings."

Does the verify path resolve Settings? The skeptic agent is built from `Skeptic` struct, which has a `Config` field of type `registry.AgentConfig`. But it doesn't pass `registry.Settings` to `buildSkepticAgent`. It only derefs the raw config values. So if `c.MaxRetries` is nil, it becomes 0, and the override is not applied. This means the skeptic uses the client's default (2 retries) instead of the global default (5).

Is this a bug? The comment says it's "consistent with TimeoutSecs → 0 meaning 'parent deadline only'". For TimeoutSecs, `derefInt(c.TimeoutSecs)` returns 0 if nil, and `invokeAgent` checks `a.TimeoutSecs > 0` before applying a per-agent timeout. So if the skeptic doesn't set a timeout, it uses the parent context. Similarly, the comment argues that not setting retry tunables means using the client default.

But the sprint plan says production review/resume paths should get the resolved Settings default of 5. The verify path is part of production review? It's adversarial verification. The sprint plan specifically mentions "production review/resume paths". Does verify count? It's invoked during review. The sprint plan says "leave the doctor/serve bare-New() fallback on the built-in 2 default unless those paths already resolve Settings." The verify path doesn't seem to resolve Settings. However, the sprint plan's scope says: "IN scope: ... wiring resolved retry settings into the production llmclient.New(WithRetry(...)) construction at the review/resume paths." It doesn't explicitly mention verify path.

But there's another issue: `buildSkepticAgent` sets `TimeoutSecs: derefInt(c.TimeoutSecs)`. In `invokeAgent`, `TimeoutSecs > 0` triggers a context with timeout. If the skeptic has no timeout, it uses the engine's timeout (since the engine passes a context with timeout). For retries, the engine's context carries the timeout. If the skeptic doesn't set `InitialBackoffMs`, `invokeAgent` won't apply an override. The client will use its default budget (2). But the global setting might be 5. This is inconsistent: other agents get 5, but skeptic gets 2.

Is this within scope? The sprint plan says "IN scope: wiring resolved retry settings into the production review/resume paths". The verify path might be considered a production path. The comment in the code explicitly says this is intentional. But is it correct per the epic? The epic says all LLM calls route through the retry middleware. The verify path makes LLM calls. It should probably also use the resolved settings.

However, the code doesn't have access to `Settings` in `buildSkepticAgent`. It only has the `Skeptic` config. The `Skeptic` struct might not carry resolved Settings. To fix this, `buildSkepticAgent` would need access to `Settings` to call `EffectiveMaxRetries` and `EffectiveInitialBackoffMs`. But currently it uses `derefInt`.

Wait, looking at `buildAgent` in `review.go`, it receives `cfg *ReviewConfig`, which has `cfg.Settings`. So it can resolve effective values. `buildSkepticAgent` receives `skeptic Skeptic` and `prompt string`. The `Skeptic` struct might have an embedded `AgentConfig` but not `Settings`. This seems like a design gap.

But is it a bug? The code comment explicitly justifies it: "A nil pointer becomes 0; the engine applies the override only when InitialBackoffMs > 0, so an unset budget keeps the shared client's own default (consistent with TimeoutSecs → 0 meaning 'parent deadline only')."

Given that the sprint plan's scope explicitly says "production review/resume paths", and verify is arguably a separate path (Epic 3.0), and the code seems intentionally designed this way, I might not flag this as a bug. However, the sprint plan's AC says "All LLM calls route through the retry middleware." The verify path is an LLM call. If the global default is 5, but the skeptic gets 2 because of bare client defaults, that seems like a correctness issue.

But I need to be careful about scope. The sprint plan says: "IN scope: max_retries + initial_backoff_ms config fields on Registry + AgentConfig... wiring resolved retry settings into the production llmclient.New(WithRetry(...)) construction at the review/resume paths." The verify path is in `internal/verify/invoke.go`, which is shown in the diff. The diff specifically adds the forwarding with `derefInt`. This suggests the author intentionally chose not to resolve Settings for the skeptic. Should I flag it? It might be out of scope or a design choice.

Let's look at the `Engine` in `fanout/engine.go`. The `Engine` is initialized with a client. The client is built at the review/resume path with `WithRetry(EffectiveMaxRetries, EffectiveInitialBackoffMs)`. So all agents that don't apply an override will use the client's default budget. Wait! This is key.

If the shared client is built at the top level with `WithRetry(globalSettings.EffectiveMaxRetries, globalSettings.EffectiveInitialBackoffMs, defaultBackoffFactor)`, then the client's default budget is already 5 and 500ms. Then `invokeAgent` only needs to apply an override when the per-agent values differ from the client default.

But looking at the diff, where is the client construction? The diff doesn't show the top-level client construction (e.g., in `cmd/review.go` or wherever). The sprint plan says: "Thread resolved Settings into the production review/resume client construction; leave the doctor/serve bare-New() fallback on the built-in 2 default." The diff shows `internal/fanout/retry_wiring_test.go` building a client with `WithRetry(0, time.Millisecond, 1.5)`. But the production wiring is not shown in the diff. Is it in the diff? Let me check the file list again. The diff only shows the files listed. There is no `cmd/` or `main.go` file in the diff.

Wait, if the production client construction is NOT in the diff, then it's not part of the changes being reviewed. The scope says "Only flag issues in files/areas directly related to the work items below." If the production client construction is not in the diff, I shouldn't review it. But the sprint plan says wiring resolved settings into production construction is IN scope. If the diff doesn't include it, maybe it's already done or not part of this commit? But the user gave me this diff to review. I should only flag issues in the diff.

However, there's a problem: `internal/fanout/engine.go`'s `invokeAgent` applies an override only when `InitialBackoffMs > 0`. If the shared client is built with `WithRetry(5, 500ms, 1.5)`, then all agents would get 5/500 by default. If an agent has no override, `InitialBackoffMs` from `buildAgent` will be 500 (>0), so `invokeAgent` will apply an override with the same values. That's redundant but harmless.

But what if the shared client is NOT built with resolved settings? Then agents without per-agent overrides (or with nil values) will rely on the client default. The diff doesn't show the client construction. I can't tell. But the diff does show `buildAgent` always setting `InitialBackoffMs` to the effective value (which is >= 500 by default). So `invokeAgent` will always apply an override for config-built agents. This means the client's default is never used for regular agents. Good.

For `buildSkepticAgent`, it sets `InitialBackoffMs` to `derefInt(c.InitialBackoffMs)`. If the skeptic config doesn't set it, it's 0. So no override. It uses the client's default. If the client is built with resolved settings (5/500), the skeptic gets 5/500. If the client is built with bare defaults (2/500?), the skeptic gets 2/500. Wait, the client default `initialBackoff` is `defaultInitialBackoff = 500 * time.Millisecond` (likely constant). The `maxRetries` default is `defaultMaxRetries = 2`. So the bare client gets 2 retries and 500ms base.

So the skeptic would get 2 retries if the client is bare. But if the client is built with `WithRetry(5, ...)`, the skeptic gets 5 retries. So the behavior depends on how the client is built. Since the diff doesn't show client construction, I can't assume it's wrong. The comment in `invoke.go` says "so an unset budget keeps the shared client's own default". If the shared client is already wired with global settings, this is fine.

Is there any issue in the diff itself? Let's look for subtle bugs.

**Issue 1: `internal/fanout/review.go` `buildFallbackAgent`**
It sets `MaxRetries: ac.EffectiveMaxRetries(cfg.Settings)` and `InitialBackoffMs: ac.EffectiveInitialBackoffMs(cfg.Settings)`. But `ac` here is the fallback agent's config. `cfg.Settings` is the global settings. This is correct.

**Issue 2: `internal/registry/precedence.go` `ResolveSettings`**
It validates per-agent overrides:
```go
for _, name := range sortedKeys(reg.Agents) {
    a := reg.Agents[name]
    if a.MaxRetries != nil && (*a.MaxRetries < 0 || *a.MaxRetries > MaxRetriesCap) {
```
Wait, `sortedKeys` is used. Is `sortedKeys` defined? It probably is. But what about `reg.Agents` iteration order? Using `sortedKeys` makes it deterministic. Good.

**Issue 3: `internal/llmclient/retry_override.go`**
The `retryOverrideKey` is an unexported empty struct. Good.
But `WithRetryOverride` doesn't validate `initialBackoff`. It could be negative or zero. However, `invokeAgent` only calls it when `InitialBackoffMs > 0`, and registry validation ensures it's positive. But what if someone calls `WithRetryOverride` directly with `0` or negative? The function accepts it. `dispatch` clamps the delay, so 0 would be passed to `clampBackoff`. If `clampBackoff` has a minimum of 1ms? Probably not; it likely just caps at maxBackoff. So a 0 delay means no sleep on retry. This might be okay for tests. Not a real issue.

**Issue 4: `internal/fanout/engine.go`**
The `invokeAgent` method:
```go
if a.InitialBackoffMs > 0 {
    ctx = llmclient.WithRetryOverride(ctx, a.MaxRetries, time.Duration(a.InitialBackoffMs)*time.Millisecond)
}
```
This is done after the timeout context is created:
```go
if a.TimeoutSecs > 0 {
    ctx, cancel = context.WithTimeout(ctx, time.Duration(a.TimeoutSecs)*time.Second)
    defer cancel()
}
```
And before the logger is set. That's fine.

But what about the shared client? If the client was built with `WithRetry(0, ...)`, and the agent has `MaxRetries: 3, InitialBackoffMs: 1`, the override applies. Good.

**Issue 5: `internal/registry/config.go` validation**
The error messages use `MaxRetriesCap` and `MaxInitialBackoffMs`. They don't show the actual bounds in the format string for `MaxRetries`? Actually:
```go
errs = append(errs, fmt.Errorf("max_retries must be within 0..%d", MaxRetriesCap))
```
That's fine.

**Issue 6: `internal/registry/precedence.go`**
The validation at the end of `ResolveSettings`:
```go
if s.MaxRetries < 0 || s.MaxRetries > MaxRetriesCap {
    return Settings{}, fmt.Errorf("max_retries must be within 0..%d, got %d", MaxRetriesCap, s.MaxRetries)
}
if s.InitialBackoffMs <= 0 || s.InitialBackoffMs > MaxInitialBackoffMs {
    return Settings{}, fmt.Errorf("initial_backoff_ms must be within 1..%d, got %d", MaxInitialBackoffMs, s.InitialBackoffMs)
}
```
Then the per-agent check:
```go
if a.MaxRetries != nil && (*a.MaxRetries < 0 || *a.MaxRetries > MaxRetriesCap) {
    return Settings{}, fmt.Errorf("agent %q: max_retries must be within 0..%d, got %d", name, MaxRetriesCap, *a.MaxRetries)
}
if a.InitialBackoffMs != nil && (*a.InitialBackoffMs <= 0 || *a.InitialBackoffMs > MaxInitialBackoffMs) {
    return Settings{}, fmt.Errorf("agent %q: initial_backoff_ms must be within 1..%d, got %d", name, MaxInitialBackoffMs, *a.InitialBackoffMs)
}
```
Wait, the first check (`a.MaxRetries != nil && ...`) uses `*a.MaxRetries > MaxRetriesCap`. What if `a.MaxRetries` is nil? It skips. Good.

But what if `reg` is directly constructed with an agent that has `MaxRetries: intPtr(5)` but `InitialBackoffMs: intPtr(0)`? The per-agent check catches it.

**Issue 7: `internal/llmclient/client.go` `dispatch`**
There's a subtle bug here. The loop:
```go
for attempt := 0; attempt <= maxRetries; attempt++ {
    if attempt > 0 {
        sleepFor := delay
        if !honorExact {
            sleepFor = jitter(sleepFor)
        }
        // ... sleep ...
    }
    // ... request ...
    case retryableStatus[status]:
        lastErr = httpStatusError(status, string(payload))
        if attempt < maxRetries {
            if retryAfter > 0 {
                delay = retryAfter
                honorExact = true
            } else {
                delay = clampBackoff(time.Duration(float64(delay) * defaultBackoffFactor))
                honorExact = false // probably
            }
            continue
        }
        // exhausted
}
```
The `delay` variable is updated after the request. On attempt 0, if it fails with retryable status, it sets `delay` for the next attempt. On attempt 1 (`attempt > 0`), it sleeps using `delay`, then makes the request. If that fails, it updates `delay` again.

With the new code, `delay` is initialized to the override's `initialBackoff` (or client default), then clamped. So attempt 1 sleep is clamped. Then if it fails again, `delay = clampBackoff(delay * 1.5)`. So attempt 2 sleep is also clamped. This is correct.

But wait: the `jitter` function. If `honorExact` is true, it doesn't jitter. If it's false, it jitters. The initial `delay = clampBackoff(delay)` might be a deterministic value. The first retry sleeps `jitter(clamped delay)`. That's fine.

**Issue 8: `internal/fanout/retry_wiring_test.go`**
```go
func retryCfg() *ReviewConfig {
    return &ReviewConfig{
        Registry: &registry.Registry{...},
        Project:  &registry.ProjectConfig{Agents: []string{"greta"}},
        Settings: registry.Settings{PayloadMode: "blocks", TimeoutSecs: 600, MaxRetries: 5, InitialBackoffMs: 500},
    }
}
```
Wait, `Settings` is embedded in `ReviewConfig`. But `ResolveSettings` returns `Settings`. Is `ReviewConfig.Settings` already populated? In production, `ReviewConfig` is likely built by resolving settings. For the test, it manually sets it. This is fine.

But in `TestBuildAgent_PropagatesRetryFields`:
```go
kai, _, err := buildAgent(cfg, "kai", payloads, ReviewRange{Base: "a", Head: "b"})
```
`cfg` has `Project: &registry.ProjectConfig{Agents: []string{"greta"}}`. It does NOT include "kai" in the project agents. Does `buildAgent` require the agent to be in the project config? Looking at the function signature, it takes `cfg *ReviewConfig` and `name string`. It likely looks up `cfg.Registry.Agents[name]`. It might also check `cfg.Project.Agents`? I don't know. If it does, this test might fail because "kai" is not in `cfg.Project.Agents`. But the test only checks `cfg.Project` for "greta". Actually, `retryCfg()` sets `Project.Agents` to `["greta"]`. Then it calls `buildAgent(cfg, "kai", ...)`. If `buildAgent` validates that the agent is in the project roster, this would fail. However, `buildAgent` might not do that; perhaps the roster check is done elsewhere. Without seeing `buildAgent`'s full implementation, it's hard to say. But `buildAgent` is in `review.go`, and the diff doesn't show it checking `cfg.Project.Agents`. So it's probably fine.

**Issue 9: `internal/registry/retry_config