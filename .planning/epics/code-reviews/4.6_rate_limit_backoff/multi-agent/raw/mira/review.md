<think>
Let me analyze this diff carefully against the sprint plan for Epic 4.6 (Robust Rate-Limit & Backoff Handling).

The sprint plan is clear:
- IN scope: `max_retries` + `initial_backoff_ms` config fields on Registry + AgentConfig (YAML + validation mirroring `TimeoutSecs`); Settings resolution with embedded defaults (5 / 500ms) + `Effective*` override methods; wiring resolved retry settings into the production `llmclient.New(WithRetry(...))` construction at the review/resume paths.
- OUT of scope: a separate `RetryMiddleware` type; exposing `backoff_factor` / `max_backoff_ms`; changing provider SDKs; any change to the circuit-breaker boundary; changing the `defaultMaxRetries = 2` constant.

Let me review the diff:

1. `docs/registry.md` - Documentation update - in scope
2. `internal/fanout/engine.go` - Engine code changes with new Agent fields and override threading - in scope
3. `internal/fanout/retry_wiring_test.go` - New tests - in scope
4. `internal/fanout/review.go` - buildAgent changes with Effective methods - in scope
5. `internal/llmclient/client.go` - dispatch updates to read override - in scope
6. `internal/llmclient/retry_override.go` - New file for context-based override - this is part of wiring
7. `internal/llmclient/retry_override_test.go` - New tests - in scope
8. `internal/registry/config.go` - Config struct changes with new fields and validation - in scope
9. `internal/registry/precedence.go` - Settings resolution and validation - in scope
10. `internal/registry/retry_config_test.go` - New tests - in scope
11. `internal/verify/invoke.go` - Skeptic build updates to forward retry fields - in scope
12. `internal/verify/invoke_test.go` - Test updates - in scope

Now let me look for actual issues:

**Issue 1: `internal/llmclient/client.go` dispatch logic**
Looking at the dispatch:
```go
delay := c.initialBackoff
if o, ok := retryOverrideFromContext(ctx); ok {
    maxRetries = o.maxRetries
    delay = o.initialBackoff
}
// Clamp the starting delay
delay = clampBackoff(delay)
```

The delay is clamped even when there's an override, but not when the client default is used? Let me re-read. The clamp is applied to `delay` in both cases. OK.

But wait - the `honorExact` flag is set to false at the start. After a 503/429 response with `retryAfter > 0`, the `honorExact` flag is set. But after that, the flag is NOT reset to false on the next iteration. Let me look at the original code more carefully...

Looking at the diff:
```go
honorExact := false
for attempt := 0; attempt <= maxRetries; attempt++ {
    if attempt > 0 {
        sleepFor := delay
        if !honorExact {
```

The `honorExact` variable is defined OUTSIDE the loop. If a server returns `Retry-After` on the first 429, then on the second attempt, the `honorExact` is still true, so it would try to sleep the Retry-After again. But wait, after the sleep, the code resets it? Let me think again about the original code structure...

Actually I need to look at the full dispatch function to understand the original code structure. The diff shows partial changes, so I need to consider what's happening. The `honorExact` variable logic likely has a reset somewhere. This isn't shown in the diff, so I can't fully review it. Let me check the existing code.

Wait, this is a diff and I only have what's shown. The code outside the diff (the body of the for loop) might be unaltered. So `honorExact` reset logic, if any, is not changed by this diff. Not really a new issue introduced.

**Issue 2: `internal/llmclient/client.go` - delay variable shadowing/conflation**
Looking at the dispatch function:
```go
delay := c.initialBackoff
if o, ok := retryOverrideFromContext(ctx); ok {
    maxRetries = o.maxRetries
    delay = o.initialBackoff
}
delay = clampBackoff(delay)
```

The `clampBackoff` is called on the override delay. But this is actually a NEW addition in this diff, so this is good - the comment says it's to handle out-of-range base values.

But wait - in the test, the test uses `time.Millisecond` (1ms) as the initial backoff. The clamp to maxBackoff wouldn't affect this. The test uses 503s which aren't 429s, so no Retry-After header. So the clamp should be fine.

**Issue 3: Test time issue**
Looking at `TestInvokeAgent_AppliesRetryOverride`:
```go
client := llmclient.New(llmclient.WithHTTPClient(srv.Client()), llmclient.WithRetry(0, time.Millisecond, 1.5))
...
a := Agent{
    ...
    MaxRetries:       3,
    InitialBackoffMs: 1,
}
```

InitialBackoffMs = 1, so delay is 1ms. With factor 1.5, attempts are at: 1ms, 1.5ms, 2.25ms... very fast, OK.

**Issue 4: `internal/registry/precedence.go` - Default of 0 issue**
Looking at `applyTier` call:
```go
applyTier(&s, reg.PayloadMode, reg.TimeoutSecs, reg.PayloadByteBudget, reg.MaxParallel)
// Retry tunables live only at the registry (global) tier and the agent
// tier (Epic 4.6) — the project tier intentionally does not carry them,
// so they are overlaid here rather than through applyTier.
if reg.MaxRetries != nil {
    s.MaxRetries = *reg.MaxRetries
}
if reg.InitialBackoffMs != nil {
    s.InitialBackoffMs = *reg.InitialBackoffMs
}
```

This handles 0 retries correctly (the test `TestResolveSettings_RegistryRetryZeroRetriesSurvives` validates this).

**Issue 5: `internal/registry/precedence.go` - Project tier skip for retry**
The documentation says retry tunables are NOT carried by `.atcr/config.yaml` (project tier). The code correctly handles this by only overlaying from reg. Good.

**Issue 6: `internal/registry/config.go` validation** - looks correct
**Issue 7: `internal/registry/precedence.go` validation** - looks correct

**Issue 8: Engine code - condition for applying override**
```go
if a.InitialBackoffMs > 0 {
    ctx = llmclient.WithRetryOverride(ctx, a.MaxRetries, time.Duration(a.InitialBackoffMs)*time.Millisecond)
}
```

This uses `InitialBackoffMs > 0` as the "configured" sentinel. But what if the user explicitly sets `initial_backoff_ms: 1` (the minimum)? That works because 1 > 0. But what if they set `initial_backoff_ms: 0`? It would be rejected by validation, so it shouldn't reach here. Good.

What about `MaxRetries = 0`? This is a valid single-attempt config. The override is applied when `InitialBackoffMs > 0`, and `MaxRetries = 0` is correctly passed through. Good.

But there's a subtle issue: what if a buildAgent-resolved agent has `InitialBackoffMs > 0` (say 500) and `MaxRetries = 0`? Then the override carries `MaxRetries=0` to the dispatch, which loops `for attempt := 0; attempt <= 0` - a single attempt. Good.

**Issue 9: Verify invoke test forwarding**
Looking at `internal/verify/invoke.go`:
```go
MaxRetries:       derefInt(c.MaxRetries),
InitialBackoffMs: derefInt(c.InitialBackoffMs),
```

A nil pointer becomes 0. But what about the comment in `engine.go`:
```go
// InitialBackoffMs > 0 is the
// "configured" sentinel: a bare Agent (doctor/direct construction) leaves it
// 0 and keeps the client's own default budget. MaxRetries 0 is a valid
// budget (single attempt) and is applied whenever InitialBackoffMs is set.
```

So if a skeptic has `MaxRetries` configured but `InitialBackoffMs` unset, the engine won't apply the override. The skeptic's `MaxRetries=0` (from `derefInt(nil)`) means the engine sees `InitialBackoffMs=0` and falls back to client default. This is a potential silent bug: a skeptic's `max_retries` override might not take effect if `initial_backoff_ms` is unset.

Wait, but the skeptic uses `derefInt` which means both fields need to be set in the skeptic config. If a user sets only `max_retries` in the skeptic config, `MaxRetries=4` and `InitialBackoffMs=0`, the engine will NOT apply the override. The skeptic will use the client default.

This is inconsistent with the design described in the sprint plan: "Retry tunables live at **both** tiers, per-agent overriding global, mirroring the existing `TimeoutSecs` pattern".

But the sprint plan says the engine should thread both `MaxRetries` and `InitialBackoffMs` per-agent. The current implementation requires both to be set. This is a real bug - the comment in engine.go says "MaxRetries 0 is a valid budget (single attempt) and is applied whenever InitialBackoffMs is set" but it should be applied whenever MaxRetries is set, not just when InitialBackoffMs is set.

Actually, the comment says "MaxRetries 0 is a valid budget (single attempt) and is applied whenever InitialBackoffMs is set." - this is the rule. But for non-zero MaxRetries, the same logic applies. The issue is: if a user sets MaxRetries=4 (override) and InitialBackoffMs is unset (inherits global default 500), then:
- `buildSkepticAgent` does: `derefInt(c.MaxRetries)` -> 4, `derefInt(c.InitialBackoffMs)` -> 0 (nil)
- `invokeAgent` checks `a.InitialBackoffMs > 0` which is FALSE (it's 0)
- The override is NOT applied, so the skeptic uses client default budget

This is indeed a bug. The user can set `max_retries` on the skeptic but it won't take effect if they don't also set `initial_backoff_ms`.

Wait, looking at it more carefully - in the fanout, `buildAgent` does:
```go
MaxRetries:       ac.EffectiveMaxRetries(cfg.Settings),
InitialBackoffMs: ac.EffectiveInitialBackoffMs(cfg.Settings),
```

This uses the Effective* methods that fall back to Settings. So in fanout, `a.InitialBackoffMs` is always set to the resolved global value if the agent doesn't override. So the check `a.InitialBackoffMs > 0` works for fanout.

But in verify, the skeptic build uses:
```go
MaxRetries:       derefInt(c.MaxRetries),
InitialBackoffMs: derefInt(c.InitialBackoffMs),
```

Which is a 0 for unset, NOT the resolved global default. So for verify, the override won't apply if the skeptic config doesn't have BOTH fields set.

This is a real bug in `internal/verify/invoke.go`. The fix would be to also fall back to the global settings. But wait - does verify have access to Settings? Let me think... the verify function doesn't have a Settings struct, it just has the Skeptic. So it would need to either:
1. Pass Settings through
2. Default to a sensible value
3. Use the same Effective* method but that requires a Settings value

Actually, looking at the test:
```go
sk.Config.MaxRetries = intPtr(4)
sk.Config.InitialBackoffMs = intPtr(200)
```

The test sets both. So this bug is not caught by the test. The test would pass even with the bug.

This is a real correctness issue: a skeptic with only `max_retries` set won't have its override applied.

Hmm, but is this strictly in-scope for the sprint plan? The sprint plan says:
- "Thread resolved `Settings` into the production review/resume client construction; leave the doctor/serve bare-`New()` fallback on the built-in `2` default unless those paths already resolve Settings."

The skeptic path is similar to a doctor/direct construction path. The plan doesn't explicitly say "skip verify" but it also says "unless those paths already resolve Settings." Verify doesn't currently resolve Settings for retry, so the bare client default applies.

But the issue isn't about the client default - it's about the skeptic's per-agent override not being honored because the engine's check `InitialBackoffMs > 0` is false when only one field is set.

Actually, thinking about this more carefully - the engine.go comment says:
```
// InitialBackoffMs > 0 marks a buildAgent-resolved budget
```

For buildAgent, both are always set (via Effective* methods). For verify, the derefInt means both can be 0. So the engine's check correctly identifies a "buildAgent-resolved" agent. For verify agents, the check is wrong because:
- `MaxRetries: derefInt(c.MaxRetries)` -> 0 if nil
- `InitialBackoffMs: derefInt(c.InitialBackoffMs)` -> 0 if nil
- Both 0 means no override is applied

So if a skeptic has only `max_retries` configured:
- `MaxRetries = 4`, `InitialBackoffMs = 0`
- Engine: `a.InitialBackoffMs > 0` is false → no override
- Skeptics retry budget: client default (likely 5 from Settings... or wait, the client is built with WithRetry. The doctor path might not be the skeptic path.)

Looking at how the skeptic is invoked, I'd need to see the actual call site to know how the client is built. This is getting complex. Let me note this as a potential issue and move on.

Actually, the simpler view: The verify invoke test only tests the case where both fields are set. If a user has only `max_retries` in their skeptic config (a valid configuration per YAML), the override won't apply silently. This is a correctness issue.

Let me also check: when neither field is set in the skeptic config, the engine won't apply an override. The client default applies. That's actually correct behavior - the user didn't configure a budget for the skeptic.

When both are set, the engine applies the override. Correct.

When only one is set (which is a valid YAML), the engine won't apply the override. The MaxRetries=0 (or whatever) from derefInt would have been ignored anyway. But the user's intent to override is silently lost. This is a correctness/maintainability issue.

Fix: Either use the Effective* pattern (requires Settings access), or check both fields for non-zero, or pass the skeptic config through differently.

**Issue 10: `internal/llmclient/client.go` - TestRetryOverride_AbsentUsesClientDefault uses time.Millisecond**
This is fine, just note it.

**Issue 11: `internal/llmclient/client.go` - clampBackoff and the override path**
```go
delay := c.initialBackoff
if o, ok := retryOverrideFromContext(ctx); ok {
    maxRetries = o.maxRetries
    delay = o.initialBackoff
}
delay = clampBackoff(delay)
```

The clamp is applied to BOTH paths now. Previously, the client default `c.initialBackoff` was already created via WithRetry which presumably doesn't clamp. So this is a change. But it's a safety improvement per the comment.

But wait - what if the override delay is 0? The clamp would likely return 0. Then the first retry sleeps 0. That's fine for tests but might cause a tight loop. Actually 0 sleep is fine.

**Issue 12: `internal/llmclient/retry_override.go` - InitialBackoff not validated**
The `WithRetryOverride` doesn't validate `initialBackoff` is non-negative. If someone passes a negative duration, the clampBackoff might return... well, clampBackoff is in client.go (not shown), but presumably it would handle negative. The Engine code does:
```go
if a.InitialBackoffMs > 0 {
    ctx = llmclient.WithRetryOverride(ctx, a.MaxRetries, time.Duration(a.InitialBackoffMs)*time.Millisecond)
}
```

So InitialBackoffMs is validated upstream to be positive via the registry. The verify path uses `derefInt` which could be 0. 0 ms = 0 duration, which the override would carry. But the engine wouldn't apply it because `a.InitialBackoffMs > 0` is false. So the override never sees 0 from either path.

If `WithRetryOverride` is called directly with a negative duration, that's a bug. But it's not exposed externally in this diff. Low priority.

**Issue 13: `internal/fanout/engine.go` - `if a.InitialBackoffMs > 0` is the wrong sentinel**
As discussed above. This is the same as Issue 9.

Actually, let me re-read the comment in engine.go:
```go
// Retry/backoff (Epic 4.6): the agent's effective retry budget and base
// delay (ms), resolved by buildAgent from the per-agent override layered over
// the global Settings. invokeAgent threads them onto the call context so the
// shared client's dispatch honors them. InitialBackoffMs > 0 is the
// "configured" sentinel: a bare Agent (doctor/direct construction) leaves it
// 0 and keeps the client's own default budget. MaxRetries 0 is a valid
// budget (single attempt) and is applied whenever InitialBackoffMs is set.
```

The comment says "a bare Agent (doctor/direct construction) leaves it 0". But the verify path also leaves it 0 when the skeptic config doesn't have it. The verify path is NOT a "bare" path - it's a configured skeptic. So the sentinel is wrong for the verify path.

The right fix: check if either field is non-default (e.g., `a.MaxRetries != 0 || a.InitialBackoffMs > 0`). But actually, the proper fix is to make verify use the same Effective* pattern, but verify doesn't have access to Settings.

OK let me also look at whether the verify path is in scope. The verify test is in `internal/verify/invoke_test.go` and the file `internal/verify/invoke.go` is also touched. The verify path is part of the larger review system, and the sprint plan says "wire retry settings into the production review/resume paths." Verify might or might not be in scope - it's not explicitly mentioned. The verify invoke.go was modified, so it must be in scope.

**Issue 14: `internal/registry/precedence.go` - Per-agent validation could be cleaner**
The per-agent retry validation in `ResolveSettings` is duplicated with `validate()`. This is a code duplication, but it's intentional defense-in-depth per the comment. Not a real issue.

**Issue 15: Test coverage for InitialBackoffMs > 0 sentinel with MaxRetries=0**
What if an agent has `max_retries: 0` and `initial_backoff_ms: 500`? Then `InitialBackoffMs > 0` is true, override is applied with `MaxRetries=0`. The dispatch loop runs `for attempt := 0; attempt <= 0` - one attempt. This is correct.

**Issue 16: Edge case - what if InitialBackoffMs is set but MaxRetries isn't set in verify?**
In verify:
- `MaxRetries: derefInt(c.MaxRetries)` = 0 (nil)
- `InitialBackoffMs: derefInt(c.InitialBackoffMs)` = 200 (set)
- Engine: `a.InitialBackoffMs > 0` true, applies override with `MaxRetries=0, InitialBackoffMs=200ms`
- Dispatch: single attempt (MaxRetries=0)
- The user's `initial_backoff_ms: 200` is honored, but their `max_retries` is forced to 0 (effectively the client default if the client has WithRetry, or single attempt)

Hmm, the user's `initial_backoff_ms` is 200, but the skeptic only gets one attempt. So the backoff is irrelevant. Not really a bug, just a weird outcome.

**Issue 17: Time issue with the test using 1ms backoff**
The tests use very small backoffs (1ms). In CI environments, this could be flaky. But this is a test concern, not a code review concern per se.

**Issue 18: Documentation issue with `initial_backoff_ms: 500` in registry.md**
The doc says:
```
+max_retries: 5
+initial_backoff_ms: 500
```

This is in the registry.yaml example. The plan says registry tier can have these. Good.

But the AC says "Max retries and base delays are configurable" - and they're exposed at registry + agent tiers. Good.

**Issue 19: `internal/llmclient/retry_override.go` - maxRetries=-1 in WithRetryOverride is clamped, but initialBackoff isn't**
```go
func WithRetryOverride(ctx context.Context, maxRetries int, initialBackoff time.Duration) context.Context {
    if maxRetries < 0 {
        maxRetries = 0
    }
    return context.WithValue(ctx, retryOverrideKey{}, retryOverride{
        maxRetries:     maxRetries,
        initialBackoff: initialBackoff,
    })
}
```

`maxRetries < 0` is clamped to 0, but `initialBackoff` can be any value (including negative). The TestRetryOverride_NegativeBudgetClamped test only tests the maxRetries clamping. If someone passes a negative duration, the dispatch could see a negative sleep. Minor issue, not in practice reached.

**Issue 20: `internal/registry/precedence.go` - sortedKeys function**
The diff uses `sortedKeys` in:
```go
if reg != nil {
    for _, name := range sortedKeys(reg.Agents) {
```

I need to check if `sortedKeys` exists in the file. The diff doesn't show its definition, but it's likely pre-existing. Not a new issue.

**Issue 21: Clamp behavior for the override delay**
```go
delay = clampBackoff(delay)
```

This is called after both the client default path and the override path. If the override is 30s (30000ms, the max), and maxBackoff is also 30s, this is fine. If override is 0, the clamp returns 0. This is good.

**Issue 22: The clamp happens once before the loop, but the comment says "every subsequent delay is clamped after the ×factor step"**
This is consistent with the existing behavior, but is it correct? The clamp is called once at the start, but each iteration's `delay` is multiplied by the factor. The clamp inside the loop happens via the `clampBackoff(delay)` call within the sleepFor calculation (which I can't see, but presumably exists). So the initial clamp ensures the FIRST sleep is within bounds, and subsequent sleeps are clamped after the multiplication. This is the intent per the comment.

**Issue 23: race condition in the test using `srv.URL` directly with the http client**
The test creates a server and uses `srv.Client()`. The http.Client might have connection caching. With 503 responses, the client might retry on its own. Let me think... actually, the standard `http.Client` from `srv.Client()` does NOT retry by default. So this should be fine.

**Issue 24: `internal/fanout/retry_wiring_test.go` - the ptrF function**
The test uses `ptrF` and `ptrInt` - I need to check if these exist. Likely pre-existing helpers.

**Issue 25: `internal/registry/retry_config_test.go` - intPtr**
The test uses `intPtr` which is likely pre-existing.

OK let me focus on the most significant issues:

1. **Sentinel bug in engine.go + verify invoke.go**: The check `a.InitialBackoffMs > 0` for applying the override is fragile. If a skeptic has only `max_retries` set (a valid YAML), the override won't apply. This is a silent correctness bug.

2. **Negative initialBackoff not validated in WithRetryOverride**: Minor, but inconsistent with maxRetries handling.

3. **The clamp is applied to the override delay, but the comment says "out-of-range base (a misconfigured initial_backoff or a direct WithRetryOverride) would sleep its full unclamped duration on attempt 1."** - This is a defensive fix. But what if the override delay is set to a valid value like 500ms, and the client.maxBackoff is also 30s? The clamp doesn't change it. Good. But if the override is 45s, it gets clamped to 30s. The user might be surprised. Minor issue.

Actually wait, let me re-check the sprint plan's statement:
"Expose **only** `max_retries` and `initial_backoff_ms`. `backoff_factor` (1.5) and the `maxBackoff` cap stay as implementation constants — the AC does not require tuning them."

So the maxBackoff is intentionally an implementation constant. The clamp behavior should not be tunable. The clamp is fine.

4. **The `initialBackoffMs=0` rejection in validate()**: 
```go
if r.InitialBackoffMs != nil && (*r.InitialBackoffMs <= 0 || *r.InitialBackoffMs > MaxInitialBackoffMs) {
```

The validation rejects 0 with `<= 0`. So users can't set `initial_backoff_ms: 0` in YAML. But the design says "1..30000". So this is consistent with the documentation.

5. **The engine override application is conditional on `InitialBackoffMs > 0`**. This is the key issue. If a user sets only `max_retries: 4` in their skeptic config (a valid YAML per the schema), and the skeptic's `initial_backoff_ms` is unset, then:
   - `buildSkepticAgent` does: `MaxRetries=4, InitialBackoffMs=0`
   - `invokeAgent` checks `a.InitialBackoffMs > 0` which is FALSE
   - Override is NOT applied
   - Skeptics retry budget falls back to client default

   This is a correctness bug. The user's `max_retries: 4` is silently ignored.

Let me also check: the `Effective*` methods handle the "unset" case by falling back to Settings. So in `buildAgent`, both fields are always set. But `buildSkepticAgent` doesn't use Effective* methods - it uses `derefInt`. This asymmetry is the root cause.

Fix: Use `Effective*` methods in `buildSkepticAgent`. But that requires Settings access, which verify doesn't have. Alternative: Pass resolved settings into verify, or use a different sentinel.

Actually, the simpler fix is to change the engine's check to be: "apply the override when either MaxRetries or InitialBackoffMs indicates a configured budget." Or: make verify use Settings.

Let me also consider: the test `TestInvokeAgent_AppliesRetryOverride` uses `a.MaxRetries: 3, a.InitialBackoffMs: 1`. So both are set. The test passes. But the test doesn't cover the "only MaxRetries set" case for verify.

This is a real issue worth flagging.

6. **Documentation issue**: The registry.md mentions the field is "inherited" when unset. But the `initial_backoff_ms` is shown to "be within `1..30000`". So users can configure 1ms, but the engine's `a.InitialBackoffMs > 0` check still works for 1. OK.

7. **The clamp on delay** - if the override has `InitialBackoffMs: 1` (1ms), and maxBackoff is 30s, the clamp is 1ms. Fine.

Let me also look at the doc:
"## Provider fields" - says the table describes the agent fields. The new entries are added to the agent fields table, not provider fields. The diff is at line 66-68, which is after the provider fields section, so they're in the right place.

Actually wait, looking at the diff more carefully:
```
+ | `max_retries` | inherited | ...
+ | `initial_backoff_ms` | inherited | ...
```

These are added to a table that includes `rate_limited`, `fallback`, `payload`. These are agent-level fields. So the additions are correct.

8. **Test coverage gap**: The test `TestInvokeAgent_AppliesRetryOverride` covers the "both set" case. The "only MaxRetries set" case is not covered. Could be a maintainability issue (test gap).

9. **No coverage of the case where `MaxRetries: 0, InitialBackoffMs: 500`**: This is a valid configuration (single attempt). The test `TestBuildAgent_PropagatesRetryFields` uses MaxRetries=5/9 and InitialBackoffMs=500. The case `MaxRetries: 0` is not covered for the engine path. Actually `TestEffectiveMaxRetries` covers it at the Effective* level. So the unit is covered, but the integration through engine is not.

10. **`internal/llmclient/client.go` - clamping behavior change**: The clamp is now called unconditionally. Previously, if `c.initialBackoff` was set via WithRetry, the clamp might not have been called. Now it is. This could be a behavior change for existing clients. But the change is to clamp values that were already supposed to be clamped, so it's a safety improvement. No new bug.

OK let me also think about the test for the new `dispatch` logic:

The test `TestRetryOverride_AbsentUsesClientDefault`:
```go
c := New(WithHTTPClient(srv.Client()), WithRetry(2, time.Millisecond, 1.5))
out, err := c.Complete(context.Background(), Invocation{BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m"})
require.NoError(t, err)
assert.Equal(t, "ok", out)
assert.Equal(t, int32(3), calls.Load())
```

With `WithRetry(2, ...)`, the client budget is 2 retries (3 total attempts). The server returns 503 for the first 2 calls, then OK on call 3. So 3 calls total. This matches the assertion.

Wait, but the dispatch loop is `for attempt := 0; attempt <= maxRetries; attempt++`. With `maxRetries=2`, attempts are 0, 1, 2 (3 total). After 2 503s, on attempt 2, the call succeeds. Total 3 calls. Good.

But the assertion `assert.Equal(t, int32(3), calls.Load())` says exactly 3 calls. The test should work.

11. **The `TestRetryOverride_LowersBudgetBelowClientDefault` test**:
```go
c := New(WithHTTPClient(srv.Client()), WithRetry(5, time.Millisecond, 1.5))
ctx := WithRetryOverride(context.Background(), 0, time.Millisecond)
```

Client budget is 5, override is 0. So override wins, single attempt. The server always returns 503. So 1 call, then error. Assertion: 1 call. Good.

12. **The `TestRetryOverride_RaisesBudgetAboveClientDefault` test**:
```go
c := New(WithHTTPClient(srv.Client()), WithRetry(0, time.Millisecond, 1.5))
ctx := WithRetryOverride(context.Background(), 3, time.Millisecond)
```

Client budget is 0, override is 3. Override wins, 3 retries (4 total attempts). Server returns 503 for first 2, then OK on call 3. So 3 calls. Assertion: 3 calls. Good.

13. **What about the call to WithRetry with 0 retries and 0 initial backoff?**:
The test `WithRetry(0, time.Millisecond, 1.5)` is what the tests use. This means the client is built with zero retry budget. The override then drives retries.

OK I think the main real issue is the sentinel bug for verify. Let me also look for any other issues.

14. **Documentation inconsistency**: The registry.md says the chain is:
> agent-level field > `registry.yaml` (global) > embedded default

But the precedence.go code uses the standard `applyTier` chain (CLI > project > registry > embedded) for other fields. The retry tunables are deliberately NOT in the CLI or project tier, but they're added to the same Settings struct. The documentation explicitly says this:
> They are deliberately **not** carried by `.atcr/config.yaml` or a CLI flag

The code correctly handles this by overlaying from reg directly, not from proj or CLI. Good.

15. **The test for the skeptic doesn't test the engine path**:
The test `TestBuildSkepticAgent_ForwardsProviderAndBudgets` only tests that the Agent struct is built correctly. It doesn't test that the engine applies the override. The fanout tests cover the engine path. So there's an integration gap.

16. **InitialBackoffMs > 0 sentinel issue summary**:
   - In `buildAgent`, the Effective* methods always set InitialBackoffMs to a positive value (default 500).
   - In `buildSkepticAgent`, the `derefInt` returns 0 for unset.
   - In `invokeAgent`, the check `a.InitialBackoffMs > 0` distinguishes "configured" vs "bare".
   - For skeptic, if user sets only `max_retries`, the override is silently dropped.
   - This is a correctness issue.

17. **Possible fix for the sentinel issue**:
   - Change the engine check to: `if a.MaxRetries != 0 || a.InitialBackoffMs > 0`
   - Or: Use a bool flag on Agent to indicate "configured"
   - Or: Make verify use Effective* methods (requires Settings access)

Looking at this more, I think the best fix is to change the engine's check. The original intent (per the comment) was to detect "bare" agents. But the implementation conflates "bare" with "partially configured". A more robust check would be to see if the agent has a "configured" budget, regardless of which field signals it.

Actually, looking at it again, the simplest fix is to make the skeptic path go through the Effective* methods. The skeptic config validation should resolve through Settings like other paths. But this is a larger change.

A minimal fix: in the engine, check `if a.MaxRetries != 0 || a.InitialBackoffMs > 0`. But wait, what if a skeptic has explicit `max_retries: 0` and no `initial_backoff_ms`? Then `MaxRetries=0, InitialBackoffMs=0`, and the check fails. The override is not applied. The user wanted single-attempt. The client default applies.

Actually if `MaxRetries=0` is the user's intent (single attempt), the client default is probably also 0 or higher. If the client has `WithRetry(5, ...)`, the user gets 5 retries, not single. So `MaxRetries=0` (single attempt) is not honored.

Hmm, this is tricky. The cleanest design is:
- Always thread the resolved Settings to the call
- Allow per-agent override

If verify doesn't have access to Settings, the cleanest fix is to pass Settings to verify.

OK I think I've spent enough time on this. Let me consolidate findings.

Key findings:
1. **Sentinel bug for verify skeptic path** (correctness): The engine's `if a.InitialBackoffMs > 0` check is too restrictive. If a skeptic config has only `max_retries` set (not `initial_backoff_ms`), the override is silently dropped.

2. **WithRetryOverride doesn't validate initialBackoff** (maintainability/minor): The function clamps maxRetries but not initialBackoff.

3. **Test coverage gap** (maintainability): The case "skeptic with only max_retries configured" is not tested.

4. **Code duplication of per-agent validation** (maintainability, but explicit defense-in-depth): The validation in `ResolveSettings` duplicates `validateAgent`. Per the comment, this is intentional. Not a real issue.

Let me also double-check the test for clamping. The test `TestRetryOverride_RaisesBudgetAboveClientDefault` uses `time.Millisecond` for backoff. The clamp wouldn't affect 1ms. The test passes. But the test doesn't verify the clamp behavior. Minor.

Let me also think about the documentation. The registry.md has the example:
```
# Retry/backoff (Epic 4.6) — registry (global) tier only; not carried by project config or CLI.
max_retries: 5
initial_backoff_ms: 500
```

But the default is set at the embedded tier in `
</think>
