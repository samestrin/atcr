Let me analyze this diff carefully against the sprint plan scope.

The sprint plan (Epic 4.6) is about exposing the existing retry engine via config (`max_retries` + `initial_backoff_ms`) at both global (Registry) and per-agent (AgentConfig) tiers, mirroring the `TimeoutSecs` pattern, with Settings resolution and wiring into production paths.

Files in scope:
- `internal/registry/config.go` - structs, validation, Effective* methods ✓
- `internal/registry/precedence.go` - constants, Settings, ResolveSettings ✓
- `internal/llmclient/client.go` - dispatch changes for override ✓
- `internal/llmclient/retry_override.go` - new context override ✓
- `internal/fanout/engine.go` - Agent struct + invokeAgent wiring ✓
- `internal/fanout/review.go` - buildAgent + buildFallbackAgent ✓
- `internal/verify/invoke.go` - skeptic agent wiring ✓
- Tests for the above
- docs/registry.md - documentation

Let me look for issues:

1. **`internal/fanout/engine.go` invokeAgent**: The override is applied only when `a.InitialBackoffMs > 0`. The comment says "InitialBackoffMs > 0 marks a buildAgent-resolved budget". But what if someone configures `initial_backoff_ms: 0`? Well, validation rejects `initial_backoff_ms <= 0`, so that's fine for the configured path. But the sentinel logic means a bare Agent with MaxRetries set but InitialBackoffMs=0 won't apply. That's documented behavior.

2. **`internal/verify/invoke.go` buildSkepticAgent**: Uses `derefInt(c.MaxRetries)` and `derefInt(c.InitialBackoffMs)`. If the skeptic config has MaxRetries set but InitialBackoffMs unset (nil → 0), then InitialBackoffMs=0 means the override is NOT applied (because `a.InitialBackoffMs > 0` check in invokeAgent). So the skeptic's MaxRetries would be ignored. This is a potential correctness issue — if a skeptic sets only `max_retries` without `initial_backoff_ms`, the retry override won't be applied at all.

Wait, let me check the skeptic config. The skeptic config comes from `c.MaxRetries` and `c.InitialBackoffMs`. Let me check if there's validation on the skeptic config side. Looking at the verify package... I don't see validation for skeptic retry fields in this diff. But is that in scope? The sprint plan mentions AgentConfig validation but skeptic config might be separate.

Actually, let me look more carefully. The skeptic config (`c`) — what type is it? It's likely a different config struct in the verify package. The diff shows `sk.Config.MaxRetries` and `sk.Config.InitialBackoffMs` being set in the test. 

The issue: if a skeptic sets `max_retries: 4` but leaves `initial_backoff_ms` unset, then `derefInt(c.InitialBackoffMs)` returns 0, and the invokeAgent check `if a.InitialBackoffMs > 0` is false, so no override is applied, and the skeptic's `max_retries: 4` is silently ignored. The client default is used instead.

This is a real correctness issue. The sentinel approach (InitialBackoffMs > 0 means "configured") breaks when only one of the two fields is set. In the main review path (`buildAgent`), both fields are always resolved together via `EffectiveMaxRetries` and `EffectiveInitialBackoffMs`, so both get values from Settings (which has defaults). But in the skeptic path, `derefInt` on nil pointers gives 0, so a partially-configured skeptic loses its retry budget.

Actually wait — is the skeptic path in scope? The sprint plan says "Thread resolved Settings into the production review/resume client construction." The skeptic path is part of the verify flow. Let me check... The diff modifies `internal/verify/invoke.go`, so it's in the diff. And the sprint plan says "All LLM calls route through the retry middleware" as an AC. The skeptic makes LLM calls. So this is in scope.

But is this actually a bug? Let me think about it more carefully.

In `buildSkepticAgent`:
```go
MaxRetries:       derefInt(c.MaxRetries),       // nil → 0
InitialBackoffMs: derefInt(c.InitialBackoffMs), // nil → 0
```

If skeptic config has `max_retries: 4` but no `initial_backoff_ms`, then:
- MaxRetries = 4
- InitialBackoffMs = 0

In `invokeAgent`:
```go
if a.InitialBackoffMs > 0 {
    ctx = llmclient.WithRetryOverride(ctx, a.MaxRetries, ...)
}
```
Since InitialBackoffMs = 0, the override is NOT applied. The client's own default (which could be 0 if built with WithRetry(0,...)) is used. So the skeptic's `max_retries: 4` is silently dropped.

This is a real correctness issue. The fix would be to either:
- Use a pointer/sentinel for MaxRetries too, or
- Use a different sentinel (e.g., MaxRetries >= 0 && InitialBackoffMs > 0), or
- Resolve both from Settings like buildAgent does (but skeptic might not have access to Settings)

Actually, looking at the skeptic path more carefully — does the skeptic have access to resolved Settings? The `buildSkepticAgent` function takes `(skeptic Skeptic, prompt string)` — no Settings. So it can't resolve the effective values the same way.

This is a design issue. The skeptic path uses `derefInt` which gives 0 for unset fields, and the sentinel check `InitialBackoffMs > 0` means partially-configured skeptics lose their retry budget.

3. **`internal/llmclient/client.go` dispatch**: The `delay = clampBackoff(delay)` is applied to the initial delay. But then in the loop, `sleepFor := delay` and on the first retry (attempt=1), it sleeps `delay` (the clamped initial). Then after sleeping, `delay = nextBackoff(delay, factor)` which applies the factor and clamps again. This seems correct.

Wait, let me trace through the loop more carefully:

```go
delay = clampBackoff(delay)  // clamp initial
honorExact := false
for attempt := 0; attempt <= maxRetries; attempt++ {
    if attempt > 0 {
        sleepFor := delay
        if !honorExact {
            sleepFor = applyJitter(delay)
        }
        // sleep sleepFor
        // ... after sleep, if not honorExact:
        delay = nextBackoff(delay, factor)  // delay *= factor, clamp
        honorExact = false
    }
    // make request
}
```

So on attempt 0: no sleep, make request.
On attempt 1: sleep `delay` (initial, clamped), then `delay = nextBackoff(delay, factor)`.
On attempt 2: sleep `delay` (initial * factor, clamped), then `delay = nextBackoff(delay, factor)`.

This looks correct. The initial delay is used for the first retry sleep.

But wait — when `honorExact` is true (Retry-After was set), after sleeping the exact Retry-After value, the code does:
```go
delay = nextBackoff(delay, factor)
honorExact = false
```
So after honoring a Retry-After, it goes back to the exponential schedule using the PREVIOUS `delay` (not the Retry-After value). That seems intentional — Retry-After is a one-shot exact sleep, then back to the schedule.

4. **`internal/registry/precedence.go` ResolveSettings**: The per-agent validation loop uses `sortedKeys(reg.Agents)`. I need to check if `sortedKeys` exists. It's likely a helper in the package. Since this is existing code (used elsewhere), it's probably fine.

5. **`internal/registry/config.go` validation**: The registry-level validation checks `*r.MaxRetries < 0 || *r.MaxRetries > MaxRetriesCap`. The agent-level checks `*a.MaxRetries < 0 || *a.MaxRetries > MaxRetriesCap`. Both seem consistent.

6. **`internal/fanout/engine.go` Agent struct**: The comment says "MaxRetries 0 is a valid budget (single attempt) and is applied whenever InitialBackoffMs is set." But in `invokeAgent`, the check is `if a.InitialBackoffMs > 0`. So if InitialBackoffMs > 0 and MaxRetries = 0, the override IS applied with maxRetries=0 (single attempt). That's correct.

But what about the skeptic path where `derefInt(c.InitialBackoffMs)` returns 0 for nil? As discussed in point 2, this is the issue.

7. **`internal/llmclient/retry_override.go`**: `WithRetryOverride` clamps negative maxRetries to 0. But it doesn't validate/clamp `initialBackoff`. A negative initialBackoff would be passed through. In `dispatch`, `delay = clampBackoff(delay)` would clamp it. Let me check what `clampBackoff` does — it likely clamps to `[0, maxBackoff]`. If initialBackoff is negative, clampBackoff would set it to 0 (or min). Then `applyJitter(0)` might produce 0 or negative sleep. Actually, `time.Sleep(0)` is fine (no-op). But this is an edge case from direct API misuse, not from config (config validates > 0).

Actually, looking at the `WithRetryOverride` in the fanout path: `time.Duration(a.InitialBackoffMs)*time.Millisecond` where `a.InitialBackoffMs` is validated to be > 0 by config validation. So in production, this is fine.

8. **`internal/verify/invoke.go`**: The comment says "A nil pointer becomes 0; the engine applies the override only when InitialBackoffMs > 0, so an unset budget keeps the shared client's own default." This is the design decision. But the issue is: if a skeptic sets `max_retries` but not `initial_backoff_ms`, the max_retries is silently dropped. 

Let me check if the skeptic config has validation. Looking at the diff, I don't see validation for skeptic retry fields. But the skeptic config struct isn't shown in the diff — it might already exist with its own validation. Let me check what `c` is in `buildSkepticAgent`. The function signature is `buildSkepticAgent(skeptic Skeptic, prompt string)`. `skeptic.Config` is some config type. The diff adds `MaxRetries` and `InitialBackoffMs` fields to it (presumably), but I don't see the struct definition in the diff. 

Actually, looking at the test:
```go
sk.Config.MaxRetries = intPtr(4)
sk.Config.InitialBackoffMs = intPtr(200)
```
Both are set in the test. But in production, a user might set only one.

Hmm, but is the skeptic config even user-configurable for retry fields? The skeptic config might be part of the registry's verify block. Let me check... The sprint plan says "IN scope: max_retries + initial_backoff_ms config fields on Registry + AgentConfig (YAML + validation mirroring TimeoutSecs)." The skeptic config is NOT mentioned. So adding retry fields to the skeptic config might be out of scope, or it might be a natural extension.

Actually, looking more carefully at the diff for `internal/verify/invoke.go`, it's adding `MaxRetries` and `InitialBackoffMs` to the skeptic agent construction. But where are these fields defined on the skeptic config type? They must be added somewhere. Let me check if there's a struct definition change... I don't see one in the diff. So either:
- The fields already exist on the skeptic config type (unlikely since this is new in Epic 4.6)
- The fields are being added in a part of the diff not shown
- The skeptic config type uses a different struct that already has these fields

Wait, looking at the test file `internal/verify/invoke_test.go`:
```go
sk.Config.MaxRetries = intPtr(4)
sk.Config.InitialBackoffMs = intPtr(200)
```
This compiles, so the fields must exist on whatever type `sk.Config` is. But I don't see the struct definition change in the diff. This might mean the struct already has these fields (maybe from a shared type), or the diff is incomplete.

Actually, looking at the skeptic config — it might reuse `AgentConfig` or a similar struct that already has these fields from the changes in `config.go`. If the skeptic config type embeds or is `AgentConfig`, then the fields are already added in the `config.go` changes.

Let me focus on the real issues:

**Issue 1 (MEDIUM): Skeptic retry override silently dropped when only max_retries is set**

In `buildSkepticAgent`, `derefInt(c.InitialBackoffMs)` returns 0 when the skeptic's `initial_backoff_ms` is unset. The `invokeAgent` check `if a.InitialBackoffMs > 0` then skips the override entirely, silently dropping the skeptic's `max_retries` setting. Unlike `buildAgent` which resolves both fields from Settings (guaranteeing both are set), the skeptic path uses raw `derefInt` which yields 0 for unset fields.

Actually, let me reconsider. Is this really a bug? The comment explicitly says "an unset budget keeps the shared client's own default (consistent with TimeoutSecs → 0 meaning 'parent deadline only')." So the design intent is: if the skeptic doesn't configure retry, use the client default. But the problem is: if the skeptic configures ONLY `max_retries` (not `initial_backoff_ms`), the max_retries is silently dropped. That's the bug.

But wait — does the skeptic config even allow setting just `max_retries`? If the skeptic config type is `AgentConfig`, then yes, it allows setting just `max_retries` (both are independent pointer fields). And there's no validation requiring both to be set together.

Actually, the same issue exists in the main review path too, but it's masked because `buildAgent` resolves both from Settings:
```go
MaxRetries:       ac.EffectiveMaxRetries(cfg.Settings),       // always gets a value
InitialBackoffMs: ac.EffectiveInitialBackoffMs(cfg.Settings), // always gets a value
```
Since Settings always has both defaults (5 and 500), both are always > 0, so the override is always applied. The skeptic path doesn't have this safety net.

This is a real MEDIUM correctness issue.

**Issue 2 (LOW): clampBackoff on initial delay may silently reduce configured value**

In `dispatch`, `delay = clampBackoff(delay)` clamps the initial delay. If a user configures `initial_backoff_ms: 30000` (the max allowed), and `maxBackoff` is also 30s, this is fine. But if `maxBackoff` were ever less than `MaxInitialBackoffMs`, the configured value would be silently reduced. Currently they're both 30s, so this is not an active bug, just a potential future issue. I'll skip this.

**Issue 3: Check if `sortedKeys` is available**

The `sortedKeys` function is used in `ResolveSettings`. If it doesn't exist, this would be a compile error. Since this is likely existing code (used elsewhere in the package), I'll assume it exists. Not an issue.

**Issue 4: The `honorExact` flag and Retry-After interaction with override**

When a Retry-After header is present, `honorExact` is set to true, and the exact Retry-After value is slept. After that sleep, `delay = nextBackoff(delay, factor)` and `honorExact = false`. This means the exponential schedule continues from where it was, not from the Retry-After value. This seems intentional and correct.

But wait — there's a subtle issue. When the override changes `delay` to the per-agent value, and then a Retry-After is received, the code does:
```go
if retryAfter > 0 {
    sleepFor = retryAfter
    honorExact = true
}
```
But `retryAfter` is computed from the response header. The `sleepFor` is set to `retryAfter`, but then:
```go
if !honorExact {
    sleepFor = applyJitter(delay)
}
```
Since `honorExact` is true, `sleepFor` stays as `retryAfter`. Good.

Then after sleeping:
```go
delay = nextBackoff(delay, factor)
honorExact = false
```
This advances the exponential schedule. On the next retry, if no Retry-After, it uses the advanced `delay`. This seems correct.

**Issue 5: Check the `WithRetryOverride` for initialBackoff validation**

`WithRetryOverride` clamps negative `maxRetries` to 0 but doesn't validate `initialBackoff`. A negative `initialBackoff` would be passed to `dispatch` where `clampBackoff` handles it. This is fine for the production path (config validates > 0), but a direct caller could pass negative. Low severity since it's clamped downstream.

Let me also check: does the `WithRetryOverride` function need to handle `initialBackoff == 0`? In the fanout path, `invokeAgent` checks `a.InitialBackoffMs > 0` before calling `WithRetryOverride`, so 0 never reaches it. But a direct caller could pass 0, which would result in `delay = 0`, clamped to 0, and `applyJitter(0)` might produce 0 or negative. `time.Sleep(0)` is a no-op, so retries would be instant. Not a crash, but potentially undesirable. Low severity.

Let me also look at the test files for any issues...

**Test file `retry_wiring_test.go`**: The test `TestInvokeAgent_AppliesRetryOverride` sets up a server that fails 2 times then succeeds, with `MaxRetries: 3`. It expects 3 calls (1 + 2 retries). With `maxRetries=3`, the loop runs `attempt <= 3`, so attempts 0, 1, 2, 3 = 4 total. But the server fails on calls 1 and 2, succeeds on call 3. So calls = 3. Wait: `failUntil=2` means calls 1 and 2 return 503, call 3 returns 200. With `maxRetries=3`:
- attempt 0: call 1 (503), retry
- attempt 1: call 2 (503), retry  
- attempt 2: call 3 (200), success
Total calls = 3. The assertion `assert.Equal(t, int32(3), calls.Load())` is correct. But the comment says "1 + 2 retries from the override budget" — with maxRetries=3, you'd get up to 3 retries (4 total attempts). The test happens to succeed on the 3rd call, so only 2 retries were needed. The comment is slightly misleading but the test is correct.

Actually wait, `failUntil=2`:
```go
if calls.Add(1) <= int32(failUntil) {
    w.WriteHeader(http.StatusServiceUnavailable)
    return
}
```
Call 1: calls=1, 1 <= 2, 503
Call 2: calls=2, 2 <= 2, 503
Call 3: calls=3, 3 > 2, success

So 3 calls total, 2 failures + 1 success. With maxRetries=3, the budget allows up to 4 attempts. The test succeeds on attempt 3 (the 3rd call). The assertion checks 3 calls. Correct.

**Test file `retry_override_test.go`**: `TestRetryOverride_RaisesBudgetAboveClientDefault` — client has 0 retries, override has 3. Server fails 2 times then succeeds. Expects 3 calls. With maxRetries=3:
- attempt 0: call 1 (503), retry
- attempt 1: call 2 (503), retry
- attempt 2: call 3 (200), success
3 calls. Correct.

**Test file `retry_config_test.go`**: Looks comprehensive. Tests defaults, overrides, zero survival, validation, etc.

Let me re-examine the skeptic issue more carefully.

In `internal/verify/invoke.go`:
```go
MaxRetries:       derefInt(c.MaxRetries),
InitialBackoffMs: derefInt(c.InitialBackoffMs),
```

Where `c` is the skeptic's config. If `c.MaxRetries` is set but `c.InitialBackoffMs` is nil:
- MaxRetries = e.g., 4
- InitialBackoffMs = 0

Then in `invokeAgent`:
```go
if a.InitialBackoffMs > 0 {
    ctx = llmclient.WithRetryOverride(ctx, a.MaxRetries, time.Duration(a.InitialBackoffMs)*time.Millisecond)
}
```
InitialBackoffMs = 0, so the override is NOT applied. The client's own default budget is used. The skeptic's `max_retries: 4` is silently ignored.

This is a real bug. The fix would be to use a sentinel that doesn't conflate "unset initial_backoff" with "don't apply override". For example, use a `*int` pointer in the Agent struct, or use a separate boolean field, or resolve both from Settings like buildAgent does.

Actually, let me check if the skeptic config even has these fields. Looking at the diff, I see the test sets `sk.Config.MaxRetries` and `sk.Config.InitialBackoffMs`. But I don't see where these fields are defined on the skeptic config type. Let me check if the skeptic config type is defined in the diff...

The diff doesn't show the skeptic config struct definition. It might be using `AgentConfig` directly, or it might be a separate type. If it's `AgentConfig`, then the fields are already added in `config.go`. If it's a separate type, the fields must be added somewhere not shown.

Given the test compiles, the fields exist. Let me assume they're on a config type that has `MaxRetries *int` and `InitialBackoffMs *int`.

The issue stands: if a skeptic configures only `max_retries`, the override is not applied because `InitialBackoffMs` is 0 (from `derefInt(nil)`).

Let me also check: does the skeptic config have validation? If there's validation requiring both fields to be set together, the issue is mitigated. But I don't see such validation in the diff.

Actually, looking at the verify package more carefully — the skeptic config might be part of the registry's verify block, which is validated by `Registry.validate()`. But I don't see retry validation for the verify/skeptic config in the diff. The validation added is only for `Registry` and `AgentConfig`, not for skeptic configs.

Wait, but is the skeptic config user-configurable? If skeptics are defined in the registry's verify block, then yes. Let me check the registry config... The `Verify` block in the registry has skeptic configs. If the skeptic config type has `MaxRetries` and `InitialBackoffMs` fields, they should be validated. But I don't see validation for them in the diff.

Hmm, but the sprint plan says "IN scope: max_retries + initial_backoff_ms config fields on Registry + AgentConfig (YAML + validation mirroring TimeoutSecs)." The skeptic config is NOT AgentConfig — it's a separate type in the verify package. So adding retry fields to the skeptic config might be out of scope, or it might be a natural extension.

Actually, looking at the verify package code, the skeptic config might use a different struct. Let me look at what `c` is in `buildSkepticAgent`:

```go
func buildSkepticAgent(skeptic Skeptic, prompt string) fanout.Agent {
    c := skeptic.Config
    ...
    MaxRetries:       derefInt(c.MaxRetries),
    InitialBackoffMs: derefInt(c.InitialBackoffMs),
```

So `c` is `skeptic.Config`, which is some config type. The fields `MaxRetries` and `InitialBackoffMs` are being read from it. If this config type is defined in the verify package and not in the diff, then the fields were added elsewhere (maybe in a prior commit or in a part of the diff not shown).

Given the scope constraint, I should focus on what's in the diff. The skeptic path IS in the diff. The issue with `derefInt` and the `InitialBackoffMs > 0` sentinel is real.

Let me also check for another issue: in `buildSkepticAgent`, the skeptic doesn't have access to `Settings`, so it can't resolve effective values. This means the skeptic path can't inherit the global retry budget — it either uses the skeptic's own config or the client default. This is different from the main review path where `buildAgent` resolves from Settings. This might be intentional (skeptic has its own config scope), but it means the global `max_retries: 5` doesn't apply to skeptics unless they explicitly set it.

Actually, this could be a design issue. The sprint plan says "All LLM calls route through the retry middleware" and "A chunk is only marked as failed after max_retries is exhausted." If skeptics don't inherit the global retry budget, they might fail before exhausting retries. But the skeptic path might have its own retry handling, or the client default might be sufficient.

Let me focus on the clearest issues:

1. **Skeptic partially-configured retry silently dropped** — MEDIUM correctness
2. Let me look for other issues...

**Issue with `derefInt`**: Let me check what `derefInt` does. It likely dereferences an `*int` returning 0 for nil. This is standard. The issue is the interaction with the `InitialBackoffMs > 0` sentinel.

**Issue with the `clampBackoff` call**: The new code adds `delay = clampBackoff(delay)` before the loop. This clamps the initial delay. But the existing code (before the diff) didn't do this — it used `c.initialBackoff` directly. Wait, let me re-read the diff:

```go
maxRetries := c.maxRetries
delay := c.initialBackoff
if o, ok := retryOverrideFromContext(ctx); ok {
    maxRetries = o.maxRetries
    delay = o.initialBackoff
}
// Clamp the starting delay...
delay = clampBackoff(delay)
```

Before the diff, the code was:
```go
delay := c.initialBackoff
```
And `c.initialBackoff` was already set via `WithRetry()`, which likely clamps it. So the new `clampBackoff` is added for the override path (where the override value might not be clamped). This is fine — it's defense-in-depth. But it also applies to the non-override path, which is redundant but harmless.

Actually, wait. The comment says "without this an out-of-range base (a misconfigured initial_backoff or a direct WithRetryOverride) would sleep its full unclamped duration on attempt 1." This is correct — if someone passes a very large `initialBackoff` via `WithRetryOverride`, the first retry sleep would be unclamped without this line. The fix is correct.

Let me also check: is `clampBackoff` an existing function? It must be, since it's called without being defined in the diff. It's likely already used in `nextBackoff`. Yes, this is existing code.

**Issue with `honorExact` reset**: After the override is applied, `honorExact` is still initialized to `false`. This is correct — the override only changes the budget and initial delay, not the Retry-After honoring logic.

Let me look at one more thing: the `retryOverrideFromContext` type assertion:

```go
func retryOverrideFromContext(ctx context.Context) (retryOverride, bool) {
    o, ok := ctx.Value(retryOverrideKey{}).(retryOverride)
    return o, ok
}
```

If the context value is not set, `ctx.Value(retryOverrideKey{})` returns `nil`, and the type assertion `nil.(retryOverride)` returns the zero value and `false`. This is correct — `retryOverride` is a struct type, so the zero value is `{0, 0}`. The `ok` is false, so the caller doesn't use it.

Wait, actually: `ctx.Value(retryOverrideKey{})` returns `interface{}` (or `any`). If the key is not found, it returns `nil`. Then `nil.(retryOverride)` — does this work? In Go, `nil.(T)` where T is a struct type returns the zero value of T and `false`. Yes, this is correct.

Let me also check the `retryOverrideKey` type:
```go
type retryOverrideKey struct{}
```
This is an unexported empty struct type, used as a context key. This is the standard Go pattern for avoiding context key collisions. Good.

Now let me look at the docs:

**`docs/registry.md`**: The docs say "Retry/backoff (Epic 4.6) — registry (global) tier only; not carried by project config or CLI." and "An agent's effective budget is threaded onto each call so per-agent overrides take effect without rebuilding the shared client." This matches the implementation.

The docs also say `max_retries` must be within `0..10` and `initial_backoff_ms` must be within `1..30000`. The code uses `MaxRetriesCap = 10` and `MaxInitialBackoffMs = 30000`. Matches.

Let me now also check if there are any issues with the fallback agent:

In `buildFallbackAgent`:
```go
MaxRetries:       ac.EffectiveMaxRetries(cfg.Settings),
InitialBackoffMs: ac.EffectiveInitialBackoffMs(cfg.Settings),
```
This resolves from the fallback's own config, which is correct per the comment. Both fields are always resolved (from Settings if not overridden), so both are > 0, and the override is always applied. Good.

OK, let me also think about whether there's an issue with the skeptic config not having validation. If the skeptic config allows `max_retries` and `initial_backoff_ms` but doesn't validate them, a user could set `max_retries: -5` on a skeptic, which would be passed to `derefInt` → -5, then to `WithRetryOverride` which clamps to 0. So the clamping in `WithRetryOverride` catches it. But `initial_backoff_ms: -100` would be passed as `time.Duration(-100) * time.Millisecond` = -100ms. Then `clampBackoff` would clamp it. So the downstream clamping handles it. But it's still a validation gap — the user gets no error message, just silent clamping.

But is skeptic config validation in scope? The sprint plan says "max_retries + initial_backoff_ms config fields on Registry + AgentConfig (YAML + validation mirroring TimeoutSecs)." The skeptic config is neither Registry nor AgentConfig. So validation for skeptic retry fields might be out of scope. But the wiring (forwarding them) IS in the diff. I'll note the validation gap as LOW since downstream clamping handles it.

Actually, let me reconsider. The skeptic config type — is it `AgentConfig`? If the skeptic uses `AgentConfig`, then the validation in `validateAgent` would apply. Let me check... The test sets `sk.Config.MaxRetries` and `sk.Config.InitialBackoffMs`. If `sk.Config` is of type `AgentConfig`, then these fields are the ones added in `config.go`, and `validateAgent` would validate them. But `validateAgent` is called for agents in the registry's `Agents` map, not for skeptic configs. Skeptic configs are in the `Verify` block.

I think the skeptic config is a separate type, not `AgentConfig`. So the validation doesn't apply. But without seeing the struct definition, I can't be sure. Let me not flag this since I can't confirm it.

Let me finalize my findings:

1. **MEDIUM - Skeptic retry override silently dropped when only max_retries is configured**: In `buildSkepticAgent`, `derefInt(c.InitialBackoffMs)` returns 0 when unset, and `invokeAgent`'s `if a.InitialBackoffMs > 0` check skips the override, silently dropping the skeptic's `max_retries`. Unlike `buildAgent` which resolves both from Settings, the skeptic path has no fallback.

Actually, let me reconsider the severity. The skeptic config fields are new (added as part of this epic). If a user sets `max_retries: 4` on a skeptic but doesn't set `initial_backoff_ms`, the max_retries is silently ignored. This is a real correctness issue. But how likely is this? The skeptic config might require both fields, or the documentation might say "set both." But there's no validation requiring both.

I'll rate this MEDIUM.

2. Let me check one more thing: the `derefInt` function. Is it the same as `derefMaxTurns`? Looking at the code:
```go
MaxTurns:        derefMaxTurns(ac.MaxTurns),
ToolBudgetBytes: derefInt64(ac.ToolBudgetBytes),
```
And in the skeptic:
```go
MaxTurns:        derefInt(c.MaxTurns),
```
Wait, the main path uses `derefMaxTurns` but the skeptic path uses `derefInt`. These might be different functions. `derefMaxTurns` might apply a default (e.g., 10), while `derefInt` just dereferences (0 for nil). Let me check...

Actually, looking at the main path:
```go
MaxTurns:        derefMaxTurns(ac.MaxTurns),
```
And the skeptic path:
```go
MaxTurns:        derefInt(c.MaxTurns),
```
These are different functions. `derefMaxTurns` likely applies a default, while `derefInt` just dereferences. This is existing code, not part of this diff's changes. The retry fields in the skeptic path use `derefInt`, which is correct for the new fields (no default needed — 0 means "unset/use client default").

OK, I'm confident in my finding. Let me also look for any other issues...

**Issue with `WithRetryOverride` and `initialBackoff == 0`**: If `WithRetryOverride` is called with `initialBackoff = 0` (which happens when a skeptic has `max_retries` set but `initial_backoff_ms` unset), the override IS set on the context with `initialBackoff = 0`. Then in `dispatch`:
```go
delay = o.initialBackoff  // 0
delay = clampBackoff(delay)  // clampBackoff(0) = ?
```
What does `clampBackoff(0)` return? If it clamps to `[minBackoff, maxBackoff]`, it might return `minBackoff`. If it clamps to `[0, maxBackoff]`, it returns 0. If 0, then `applyJitter(0)` might return 0 or negative. `time.Sleep(0)` is a no-op, so retries would be instant. But this path is only reached if `invokeAgent` calls `WithRetryOverride`, which requires `a.InitialBackoffMs > 0`. So this path is NOT reached when `InitialBackoffMs == 0`. The issue is that the override is NOT applied at all, not that it's applied with 0.

Wait, I need to re-read. The issue is:
- Skeptic has `max_retries: 4`, `initial_backoff_ms` unset
- `derefInt(c.MaxRetries)` = 4, `derefInt(c.InitialBackoffMs)` = 0
- In `invokeAgent`: `if a.InitialBackoffMs > 0` → false (0 is not > 0)
- Override NOT applied
- Client's own default budget used (could be 0 if built with `WithRetry(0, ...)`)
- Skeptic's `max_retries: 4` silently ignored

Yes, this is the issue. The fix would be to use a different sentinel or resolve both fields from a shared default.

Actually, I realize there might be another issue. Let me check the production client construction. The sprint plan says "Thread resolved Settings into the production review/resume client construction." Where is this done? I see the override being applied per-call in `invokeAgent`, but I don't see the shared client being constructed with `WithRetry(5, 500ms, 1.5)` in production. The sprint plan says:

"Production reads Settings.MaxRetries via WithRetry()"

But in the diff, I don't see where the production client is constructed with `WithRetry()`. The `invokeAgent` applies the override per-call, which is the per-agent budget. But what about the shared client's default? If the shared client is still built with bare `New()` (default maxRetries=2), and the override is only applied when `InitialBackoffMs > 0`, then:

- For agents resolved via `buildAgent`: both fields are resolved from Settings (5, 500), so override is applied. Good