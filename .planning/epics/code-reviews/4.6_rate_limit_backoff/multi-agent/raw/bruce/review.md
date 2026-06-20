<think>
Let me carefully review this diff for Epic 4.6: Robust Rate-Limit & Backoff Handling. I need to focus on:

1. The retry override mechanism in `internal/llmclient/retry_override.go`
2. The dispatch changes in `internal/llmclient/client.go`
3. The config changes in `internal/registry/config.go` and `internal/registry/precedence.go`
4. The fanout engine changes
5. The verify invoke changes
6. The documentation changes
7. The test files

Let me trace through the logic systematically.

**Sprint Plan Analysis:**

The sprint plan is clear about:
- IN scope: `max_retries` + `initial_backoff_ms` config fields on Registry + AgentConfig (YAML + validation mirroring `TimeoutSecs`)
- Settings resolution with embedded defaults (5 / 500ms) + `Effective*` override methods
- Wiring resolved retry settings into the production `llmclient.New(WithRetry(...))` construction at the review/resume paths
- OUT of scope: a separate `RetryMiddleware` type; exposing `backoff_factor` / `max_backoff_ms`; changing provider SDKs; any change to the circuit-breaker boundary; changing the `defaultMaxRetries = 2` constant

Wait — the sprint plan says "wiring resolved retry settings into the production `llmclient.New(WithRetry(...))` construction at the review/resume paths." But the actual diff uses a different approach — a context-based `WithRetryOverride` rather than passing retry settings to `WithRetry` at client construction.

Looking at the diff more carefully:
- `internal/fanout/engine.go`: invokes `llmclient.WithRetryOverride(ctx, a.MaxRetries, time.Duration(a.InitialBackoffMs)*time.Millisecond)` on the context
- `internal/llmclient/retry_override.go`: implements `WithRetryOverride` as context-based

This is a deviation from the sprint plan which said to use `WithRetry(maxRetries, initialBackoff, factor)` at client construction time. The plan said:
> "Thread resolved `Settings` into the production review/resume client construction; leave the doctor/serve bare-`New()` fallback on the built-in `2` default unless those paths already resolve Settings."

But the actual diff:
1. Uses a per-call context override mechanism
2. The client is NOT constructed with `WithRetry()` at the review/resume paths
3. The `defaultMaxRetries = 2` constant in `client.go` is effectively NOT used because the engine always provides an override (when `a.InitialBackoffMs > 0`)

Let me check the original `dispatch()` to see if `c.maxRetries` and `c.initialBackoff` are still the defaults.

Looking at the diff:
```go
maxRetries := c.maxRetries
delay := c.initialBackoff
if o, ok := retryOverrideFromContext(ctx); ok {
    maxRetries = o.maxRetries
    delay = o.initialBackoff
}
```

So the override is optional — if not present, client defaults apply. Good.

But wait — the production review/resume paths should now be passing retry settings via `WithRetry()` at construction. The plan says: "Thread resolved `Settings` into the production review/resume client construction."

The diff doesn't do that. Instead, it uses `WithRetryOverride` per-call. This is a design deviation.

Hmm, but the diff might still be valid. Let me check if there's a corresponding change to the production client construction. Looking at the diff, I don't see any change to where the client is constructed in the review/resume paths.

This is a HIGH concern — the plan says to thread resolved Settings into `llmclient.New(WithRetry(...))` at construction. The diff doesn't do that. The plan also says the constant `defaultMaxRetries = 2` should stay as the bare-`New()` fallback for doctor/test. But the plan's preferred wiring is via `WithRetry` at construction, not via `WithRetryOverride` per call.

Actually, re-reading the plan:
> "Thread resolved `Settings` into the production review/resume client construction; leave the doctor/serve bare-`New()` fallback on the built-in `2` default unless those paths already resolve Settings."

So the production paths should be constructed with `WithRetry()`, and doctor/serve stays bare. The diff doesn't do this — the production paths use `WithRetryOverride` instead. This is a deviation from the plan.

But this is a design choice — both approaches could work. Let me focus on actual bugs/issues.

Now let me look at specific issues:

**Issue 1: `clampBackoff` function is called but not defined in the diff**

The diff has:
```go
delay = clampBackoff(delay)
```

Is `clampBackoff` defined somewhere? Let me check the diff... I don't see it being added. This might exist in the existing `client.go` file. Let me assume it exists. If it doesn't, this would be a compile error.

Actually, looking at the diff more carefully, it references `clampBackoff` but doesn't define it. If it doesn't exist in the original `client.go`, this is a compile error. Let me check if this is likely a pre-existing function or if the diff is missing a definition.

Given the context: "every subsequent delay is clamped after the ×factor step", this suggests there IS a clampBackoff function. The diff is adding an extra call to it.

**Issue 2: The InitialBackoffMs > 0 check in engine.go**

```go
if a.InitialBackoffMs > 0 {
    ctx = llmclient.WithRetryOverride(ctx, a.MaxRetries, time.Duration(a.InitialBackoffMs)*time.Millisecond)
}
```

The comment says: "InitialBackoffMs > 0 is the 'configured' sentinel: a bare Agent (doctor/direct construction) leaves it 0 and keeps the client's own default budget. MaxRetries 0 is a valid budget (single attempt) and is applied whenever InitialBackoffMs is set."

This is interesting. So if `a.InitialBackoffMs == 0` and `a.MaxRetries != 0`, the override is NOT applied, meaning the client defaults are used. But the client is constructed with `llmclient.New()` (bare), which uses `defaultMaxRetries = 2`. So a doctor self-test with bare Agent would get 2 retries.

But what if `a.InitialBackoffMs == 0` and `a.MaxRetries == 0`? Then no override is applied, and the client default (2 retries) would be used. Is that the intended behavior? Probably yes — if you didn't set `InitialBackoffMs`, you're leaving it at the default and shouldn't be overriding anything.

But wait — the `buildAgent` function resolves `EffectiveInitialBackoffMs`, which returns the Settings value if not set on the agent. The default Settings value is `DefaultInitialBackoffMs = 500`. So `buildAgent` would set `InitialBackoffMs = 500` for any agent built from config.

But for a bare `Agent` (doctor, direct construction), `InitialBackoffMs` is 0 (Go's zero value). That's the sentinel for "no override".

This seems intentional and correct.

**Issue 3: The `verify/invoke.go` change**

```go
MaxRetries:       derefInt(c.MaxRetries),
InitialBackoffMs: derefInt(c.InitialBackoffMs),
```

`derefInt` likely returns 0 if the pointer is nil. So if `c.MaxRetries` is nil, `a.MaxRetries` becomes 0. Then the engine check `if a.InitialBackoffMs > 0` would be false (since `a.InitialBackoffMs` is also 0), and no override is applied. The shared client default (2 retries) is used.

Wait, but the comment says: "A nil pointer becomes 0; the engine applies the override only when InitialBackoffMs > 0, so an unset budget keeps the shared client's own default (consistent with TimeoutSecs → 0 meaning 'parent deadline only')."

Hmm, but if the user explicitly sets `max_retries: 0` on a skeptic config, this would be 0, and the check `a.InitialBackoffMs > 0` would only be true if `initial_backoff_ms` is also set. If the user sets `max_retries: 0` and `initial_backoff_ms: 500`, then `a.InitialBackoffMs = 500` and the override is applied with `MaxRetries=0`. Good.

But what if the user sets `max_retries: 3` on a skeptic but doesn't set `initial_backoff_ms`? Then `MaxRetries=3`, `InitialBackoffMs=0` (from nil pointer deref), and no override is applied. This means the skeptic's 3 retry budget is ignored!

This could be a HIGH severity bug. The skeptic config has `MaxRetries` set, but it doesn't take effect because `InitialBackoffMs` is 0.

Wait, but the test:
```go
sk.Config.MaxRetries = intPtr(4)
sk.Config.InitialBackoffMs = intPtr(200)
```

The test sets BOTH fields. So the test wouldn't catch the case where only one is set.

Let me re-check: in `buildSkepticAgent`, `c` is a `Skeptic` config (which presumably has `MaxRetries *int` and `InitialBackoffMs *int` fields). If `c.MaxRetries` is non-nil but `c.InitialBackoffMs` is nil, then `a.MaxRetries = 3` and `a.InitialBackoffMs = 0`. The engine check fails, and no override is applied.

This is a bug. The fix would be to use a different sentinel or to always apply the override when either field is set.

Actually, looking at the code flow more carefully:
- `buildAgent` calls `ac.EffectiveMaxRetries(cfg.Settings)` and `ac.EffectiveInitialBackoffMs(cfg.Settings)` which return the resolved values (always non-zero if the agent goes through the config path).
- `buildSkepticAgent` calls `derefInt(c.MaxRetries)` and `derefInt(c.InitialBackoffMs)` which return 0 for nil pointers.

So the skeptic path is different from the agent path. In the skeptic path, the resolved values aren't used — the raw config values are used directly. This means:
1. If the skeptic config doesn't set `MaxRetries`, `a.MaxRetries = 0` (not the resolved default of 5).
2. If the skeptic config doesn't set `InitialBackoffMs`, `a.InitialBackoffMs = 0`.

The engine's `if a.InitialBackoffMs > 0` check would then not trigger, and the client default (2 retries) would be used instead of the resolved default (5 retries).

This is a bug — the skeptic's retry budget doesn't use the resolved Settings defaults. The fix would be to resolve through `EffectiveMaxRetries` / `EffectiveInitialBackoffMs` in the skeptic path too, or to use a different mechanism for the skeptic path.

Actually, wait. Let me re-read the skeptic config. Does it have its own `MaxRetries` and `InitialBackoffMs` fields, or does it use the agent config fields? The diff only shows `derefInt(c.MaxRetries)` and `derefInt(c.InitialBackoffMs)`, so `c` must have these fields.

Looking at the test:
```go
sk.Config.MaxRetries = intPtr(4)
sk.Config.InitialBackoffMs = intPtr(200)
```

So `sk.Config` has `MaxRetries *int` and `InitialBackoffMs *int` fields. But there's no `EffectiveMaxRetries` method on the skeptic config (because the diff doesn't add one).

The fix would be to resolve through Settings, similar to how the agent does it.

**Issue 4: Plan deviation — using `WithRetryOverride` instead of `WithRetry` at construction**

The sprint plan says: "Thread resolved `Settings` into the production review/resume client construction; leave the doctor/serve bare-`New()` fallback on the built-in `2` default unless those paths already resolve Settings."

The diff doesn't do this. Instead, it adds a per-call `WithRetryOverride` mechanism. This is a design deviation from the plan.

But this isn't necessarily a bug — it could be a valid alternative approach. However, it contradicts the plan's stated approach.

**Issue 5: Missing production client construction update**

The plan says to thread resolved Settings into `llmclient.New(WithRetry(...))` at the review/resume paths. The diff doesn't do this. This means the production paths still use the bare `llmclient.New()` with `defaultMaxRetries = 2`.

Wait, but the engine now applies `WithRetryOverride` on the context, so the client default (2) is effectively overridden to the per-agent budget. So functionally, the production paths get the correct retry budget.

But this is still a plan deviation. The plan said to do it at construction time, not at call time.

Actually, re-reading the diff: the engine only applies the override when `a.InitialBackoffMs > 0`. For the skeptic path, as I noted above, `a.InitialBackoffMs` would be 0 if the skeptic config doesn't set it, so the override wouldn't be applied.

**Issue 6: `clampBackoff` is not defined in the diff**

The diff calls `clampBackoff(delay)` but doesn't define it. This could be a pre-existing function. If it doesn't exist, this is a compile error.

Actually, looking at the original `dispatch()` function (which I'm inferring from the diff), it probably has a `clampBackoff` function already. The diff is just adding an extra call to it. So this is likely fine.

**Issue 7: The `a.MaxRetries` could be negative if set to a negative value**

Wait, the validation in `internal/registry/config.go` ensures `MaxRetries` is within `0..10`. And `EffectiveMaxRetries` returns the resolved value. So `a.MaxRetries` should always be >= 0.

But what about the skeptic path? `derefInt(c.MaxRetries)` could return a negative value if `c.MaxRetries` is set to a negative number. The skeptic config validation might catch this, but the diff doesn't show the skeptic validation.

Actually, the skeptic config likely has its own validation. The diff doesn't show it, so I'll assume it validates correctly.

**Issue 8: Documentation says `0..10` but the cap is `MaxRetriesCap = 10`**

The documentation says:
> "Must be within `0..10` (`0` = a single attempt, no retry)."

The validation says `0..10`. This is consistent.

**Issue 9: The `backoff_factor` (1.5) and `maxBackoff` cap are mentioned in docs as implementation constants**

The docs say:
> "the schedule grows exponentially from here (×1.5, capped at 30s)"

The `MaxInitialBackoffMs = 30000` constant is 30s. This is consistent.

**Issue 10: The `Registry` and `AgentConfig` validate functions are called by `LoadRegistry`, but the `ResolveSettings` function also has its own validation**

The `ResolveSettings` function re-validates the retry tunables, which is good for defense-in-depth. But it also re-validates per-agent retry overrides, which is also good.

However, the `sortedKeys` function is called. Does it exist? The diff doesn't define it. This could be a pre-existing function in the registry package.

**Issue 11: The `intPtr` function used in tests**

The test files use `intPtr`. Does this function exist? Looking at the existing code, there's likely an `intPtr` helper. The diff doesn't define it, so it must be pre-existing.

Wait, the fanout test uses `ptrF` and `ptrInt`:
```go
"greta": {Provider: "p", Model: "m", Persona: "greta", Temperature: ptrF(0.7)},
"kai": {Provider: "p", Model: "m2", Persona: "kai", Temperature: ptrF(0.7), MaxRetries: ptrInt(9)},
```

These must be pre-existing helper functions.

**Issue 12: The `loadRegistryWith` and `writeRegistry` and `resolve` helper functions in `retry_config_test.go`**

These must be pre-existing helper functions. The diff doesn't define them.

**Issue 13: The `okResponse` and `testKey` in `retry_override_test.go`**

These must be pre-existing helper functions/variables.

**Issue 14: The `clampBackoff` function**

Wait, let me look more carefully at the diff. The diff adds:
```go
delay = clampBackoff(delay)
```

But there's no `clampBackoff` function definition in the diff. This is either a pre-existing function or a missing definition.

Looking at the comment: "every subsequent delay is clamped after the ×factor step, but without this an out-of-range base (a misconfigured initial_backoff or a direct WithRetryOverride) would sleep its full unclamped duration on attempt 1."

This suggests there IS a `clampBackoff` function that clamps delays to `maxBackoff`. The diff is adding an extra call to it for the initial delay.

**Issue 15: The `dispatch()` function still has the old comment about `c.maxRetries`**

The diff replaces `c.maxRetries` with `maxRetries` (the local variable), but the comment at the top of `dispatch()` might still reference the old behavior. Let me check...

The diff adds a new comment block explaining the override mechanism. The old comments are presumably still there but refer to the same logic. This is fine.

**Issue 16: The `Retry-After` header is always honored**

The docs say: "The `Retry-After` header is always honored regardless of this value."

The dispatch logic shows:
```go
if retryAfter > 0 {
    honorExact = true
    sleepFor = time.Duration(retryAfter) * time.Second
}
```

This is independent of the retry budget. The `Retry-After` is always honored when present. This is correct.

**Issue 17: The `Engine` type's `invokeAgent` function — what about the doctor path?**

The doctor uses a bare `Engine` without retry fields. The engine check `if a.InitialBackoffMs > 0` would be false, and the client default (2 retries) is used. This is consistent with the plan.

**Issue 18: The `buildFallbackAgent` function**

The diff adds retry fields to `buildFallbackAgent`:
```go
MaxRetries:       ac.EffectiveMaxRetries(cfg.Settings),
InitialBackoffMs: ac.EffectiveInitialBackoffMs(cfg.Settings),
```

This is correct — the fallback uses the fallback agent's own config for retry budget.

**Issue 19: The `EffectiveMaxRetries` and `EffectiveInitialBackoffMs` methods**

These return the agent's own value if set, otherwise the resolved Settings value. This is consistent with `EffectiveTimeoutSecs`.

**Issue 20: The `Registry` validation**

The `validate()` function checks `MaxRetries` and `InitialBackoffMs` ranges. This is good.

**Issue 21: The `validateAgent` function**

The `validateAgent` function checks per-agent retry ranges. This is good.

**Issue 22: The `ResolveSettings` function re-validates**

The `ResolveSettings` function re-validates the resolved values and per-agent values. This is good for defense-in-depth.

**Issue 23: The `sortedKeys` function**

The `ResolveSettings` function calls `sortedKeys(reg.Agents)`. Does this function exist? It must be pre-existing in the registry package.

**Summary of Issues:**

1. **HIGH: Skeptic path doesn't use resolved Settings defaults** — `buildSkepticAgent` uses `derefInt(c.MaxRetries)` which returns 0 for nil pointers, so the skeptic's retry budget doesn't use the resolved Settings default of 5 retries. This means the skeptic would use the client default (2 retries) instead of the resolved default (5 retries).

2. **MEDIUM: Plan deviation — using `WithRetryOverride` instead of `WithRetry` at construction** — The plan says to thread resolved Settings into `llmclient.New(WithRetry(...))` at construction. The diff uses a per-call `WithRetryOverride` instead.

3. **MEDIUM: Skeptic's `MaxRetries` is ignored if `InitialBackoffMs` is not set** — The engine check `if a.InitialBackoffMs > 0` means that if a skeptic sets `max_retries: 3` but doesn't set `initial_backoff_ms`, the override is not applied.

4. **LOW: `clampBackoff` is not defined in the diff** — This might be a pre-existing function. If not, it's a compile error.

5. **LOW: The `defaultMaxRetries = 2` constant is now effectively dead code** — All production paths use `WithRetryOverride`, so the client default is only used for bare `New()` calls (doctor, tests). The plan explicitly says to keep the constant as the bare-`New()` fallback, so this is intentional.

Let me also check for other issues:

**Issue 24: The `Engine` type's `invokeAgent` function — what if `a.MaxRetries` is 0 and `a.InitialBackoffMs > 0`?**

In this case, the override IS applied with `MaxRetries=0`. This means a single attempt with no retry. This is the correct behavior for `max_retries: 0`.

**Issue 25: The `Engine` type's `invokeAgent` function — what if `a.MaxRetries > 0` and `a.InitialBackoffMs == 0`?**

In this case, the override is NOT applied. The client default (2 retries) is used. This is a bug if the user explicitly sets `max_retries: 3` on an agent but doesn't set `initial_backoff_ms`.

Wait, but `buildAgent` resolves through `EffectiveInitialBackoffMs`, which returns the resolved Settings value (500ms by default). So `a.InitialBackoffMs` would be 500 for any agent built through `buildAgent`.

The issue is only with the skeptic path, where `derefInt(c.InitialBackoffMs)` returns 0 for nil pointers.

**Issue 26: The `buildSkepticAgent` function doesn't use `EffectiveMaxRetries` / `EffectiveInitialBackoffMs`**

This is the root cause of the skeptic path bug. The fix would be to resolve through Settings, similar to how the agent does it.

But wait — does the skeptic config have access to Settings? Looking at the diff, `buildSkepticAgent` takes a `Skeptic` and a `prompt`. It doesn't have access to Settings. So the fix would be to pass Settings to `buildSkepticAgent`, or to use the shared client default.

Actually, the skeptic path might not need to use the resolved Settings defaults. The skeptic config has its own `MaxRetries` and `InitialBackoffMs` fields. If the user doesn't set them, the skeptic uses the client default (2 retries). This might be the intended behavior.

But the comment in the diff says: "A nil pointer becomes 0; the engine applies the override only when InitialBackoffMs > 0, so an unset budget keeps the shared client's own default (consistent with TimeoutSecs → 0 meaning 'parent deadline only')."

So the intent is that an unset skeptic budget keeps the client default. This is consistent with TimeoutSecs (0 means "use parent deadline only").

But the issue is: if the user sets `max_retries: 3` on a skeptic but doesn't set `initial_backoff_ms`, the skeptic's 3 retry budget is ignored. This is a bug.

The fix would be to change the engine check to `if a.MaxRetries > 0 || a.InitialBackoffMs > 0` or to always apply the override when `a.MaxRetries` is explicitly set.

Wait, but how does the engine know if `a.MaxRetries` was explicitly set or is just the Go zero value? The Agent struct uses `int`, not `*int`. So there's no way to distinguish "explicit 0" from "unset" at the engine level.

This is a design issue. The Agent struct should use `*int` for retry fields, or there should be a separate flag.

Actually, looking at the Agent struct:
```go
MaxRetries       int
InitialBackoffMs int
```

These are plain `int`s. The `buildAgent` function sets them to the resolved values (always non-zero). The `buildSkepticAgent` function sets them to the dereffed values (0 for nil). A bare Agent has them as 0 (Go zero value).

The engine check `if a.InitialBackoffMs > 0` is meant to distinguish "configured" from "bare". But this means:
- Bare Agent: `InitialBackoffMs = 0` → no override
- Agent from config: `InitialBackoffMs = 500` (or whatever the resolved value is) → override applied
- Skeptic with `MaxRetries` set but `InitialBackoffMs` not set: `MaxRetries = 3`, `InitialBackoffMs = 0` → no override (BUG)

The fix would be to either:
1. Change the Agent struct to use `*int` for retry fields
2. Change the engine check to also check `a.MaxRetries`
3. Change `buildSkepticAgent` to always set both fields (e.g., to the resolved Settings values)

Option 3 is the cleanest fix. The skeptic config should resolve through Settings, similar to how the agent does it.

But this requires passing Settings to `buildSkepticAgent`, which is a larger change.

Actually, the simpler fix is: in `buildSkepticAgent`, if `c.MaxRetries` is nil, set `a.MaxRetries` to a sentinel value (like -1) and check for that in the engine. Or, always apply the override when `a.MaxRetries` is set (but this requires distinguishing "explicit 0" from "unset").

Hmm, this is getting complex. Let me flag this as a MEDIUM severity issue.

**Issue 27: The `clampBackoff` call on the initial delay**

The diff adds:
```go
delay = clampBackoff(delay)
```

This is called on the initial delay. The comment says: "every subsequent delay is clamped after the ×factor step, but without this an out-of-range base (a misconfigured initial_backoff or a direct WithRetryOverride) would sleep its full unclamped duration on attempt 1."

This is a good fix. But I need to verify that `clampBackoff` exists in the original `client.go`. If it doesn't, this is a compile error.

Let me assume it exists for now. If it doesn't, it's a HIGH severity issue.

**Issue 28: The `dispatch()` function's loop condition**

The diff changes:
```go
for attempt := 0; attempt <= c.maxRetries; attempt++ {
```
to:
```go
for attempt := 0; attempt <= maxRetries; attempt++ {
```

And changes:
```go
if attempt < c.maxRetries {
```
to:
```go
if attempt < maxRetries {
```

These are consistent with the local `maxRetries` variable. Good.

**Issue 29: The `EffectiveMaxRetries` method doesn't handle the case where `s.MaxRetries` is out of range**

The `ResolveSettings` function validates `s.MaxRetries` against the range. So `s.MaxRetries` should always be in range when `EffectiveMaxRetries` is called. Good.

**Issue 30: The `Registry` and `AgentConfig` validation in `validate()` and `validateAgent()`**

These are called by `LoadRegistry`. The `ResolveSettings` function also has its own validation. This is defense-in-depth.

But wait — `ResolveSettings` re-validates per-agent values, but it iterates over `reg.Agents` using `sortedKeys`. Does this function exist? It must be pre-existing.

**Issue 31: The `intPtr` function in the test**

The test uses `intPtr`. Does this function exist? It must be pre-existing in the test file or a test helper.

**Issue 32: The `ptrF` and `ptrInt` functions in the fanout test**

These must be pre-existing helper functions.

**Issue 33: The `resolve` and `loadRegistryWith` and `writeRegistry` functions in the registry test**

These must be pre-existing helper functions.

**Issue 34: The `okResponse` and `testKey` in the llmclient test**

These must be pre-existing helper functions/variables.

**Summary of Findings:**

Let me now compile the findings:

1. **HIGH: Skeptic path bug** — `buildSkepticAgent` uses `derefInt(c.MaxRetries)` which returns 0 for nil pointers. If a skeptic sets `max_retries: 3` but doesn't set `initial_backoff_ms`, the engine check `if a.InitialBackoffMs > 0` fails, and the override is not applied. The skeptic's retry budget is ignored.

2. **MEDIUM: Plan deviation** — The plan says to thread resolved Settings into `llmclient.New(WithRetry(...))` at construction. The diff uses a per-call `WithRetryOverride` instead. This is a design deviation.

3. **MEDIUM: `clampBackoff` is not defined in the diff** — The diff calls `clampBackoff(delay)` but doesn't define it. This might be a pre-existing function, but if not, it's a compile error.

4. **LOW: The `defaultMaxRetries = 2` constant is now effectively dead code** — All production paths use `WithRetryOverride`, so the client default is only used for bare `New()` calls. The plan explicitly says to keep the constant as the bare-`New()` fallback, so this is intentional.

Actually, let me reconsider issue 1. The skeptic path is in `internal/verify/invoke.go`, which is in scope for this sprint (it's part of the retry/backoff wiring). The bug is that the skeptic's `MaxRetries` is ignored if `InitialBackoffMs` is not set.

But wait — the skeptic config might always set both fields together, or the validation might require both. Let me check...

The diff doesn't show the skeptic config validation. The skeptic config might have its own validation that requires both fields. If so, the bug is less severe.

But the diff also doesn't show the skeptic config struct. So I can't verify this.

Let me flag this as a MEDIUM severity issue, since it's a potential bug that depends on the skeptic config structure.

Actually, looking at the test:
```go
sk.Config.MaxRetries = intPtr(4)
sk.Config.InitialBackoffMs = intPtr(200)
```

The test sets both fields. This suggests the skeptic config has both fields, but the test doesn't cover the case where only one is set.

Let me flag this as a MEDIUM severity issue.

**Issue 35: The `a.MaxRetries` value in the skeptic path is not the resolved value**

In `buildSkepticAgent`, `a.MaxRetries = derefInt(c.MaxRetries)`. If `c.MaxRetries` is nil, `a.MaxRetries = 0`. This means the skeptic uses the client default (2 retries) instead of the resolved Settings default (5 retries).

The fix would be to resolve through `EffectiveMaxRetries(cfg.Settings)` in the skeptic path. But this requires passing Settings to `buildSkepticAgent`.

Actually, looking at the skeptic path more carefully, the skeptic config might not have access to the resolved Settings. The skeptic is built from a `Skeptic` struct, which might not carry the resolved Settings.

The fix would be to either:
1. Pass Settings to `buildSkepticAgent`
2. Use the shared client default (2 retries) for skeptics
3. Change the engine to always apply the override when `a.MaxRetries` is set (requires distinguishing "explicit 0" from "unset")

The current behavior (option 2) is consistent with the comment: "an unset budget keeps the shared client's own default".

But the issue is: if the user sets `max_retries: 3` on a skeptic but doesn't set `initial_backoff_ms`, the skeptic's 3 retry budget is ignored. This is a bug.

The fix would be to change the engine check to `if a.MaxRetries > 0 || a.InitialBackoffMs > 0` or to always apply the override when `a.MaxRetries` is set.

Actually, the cleanest fix is to change the Agent struct to use `*int` for retry fields. But this is a larger change.

Let me flag this as a MEDIUM severity issue.

**Issue 36: The `Engine` type's `invokeAgent` function — the `a.MaxRetries` value for a bare Agent**

A bare Agent has `MaxRetries = 0` and `InitialBackoffMs = 0` (Go zero values). The engine check `if a.InitialBackoffMs > 0` is false, so no override is applied. The client default (2 retries) is used. This is correct for a bare Agent.

**Issue 37: The `buildAgent` function always sets `InitialBackoffMs` to the resolved value**

`buildAgent` calls `ac.EffectiveInitialBackoffMs(cfg.Settings)`, which returns the resolved Settings value (500ms by default). So `a.InitialBackoffMs` is always non-zero for agents built through `buildAgent`. The engine check always passes, and the override is always applied. This is correct.

**Issue 38: The `buildFallbackAgent` function always sets `InitialBackoffMs` to the resolved value**

Similar to `buildAgent`, `buildFallbackAgent` resolves through `EffectiveInitialBackoffMs`. So the override is always applied. This is correct.

**Final Summary:**

Let me compile the findings:

1. **HIGH: Skeptic path bug** — `buildSkepticAgent` uses `derefInt(c.MaxRetries)` which returns 0 for nil pointers. If a skeptic sets `max_retries: 3` but doesn't set `initial_backoff_ms`, the engine check `if a.InitialBackoffMs > 0` fails, and the override is not applied. The skeptic's retry budget is ignored.

2. **MEDIUM: Plan deviation** — The plan says to thread resolved Settings into `llmclient.New(WithRetry(...))` at construction. The diff uses a per-call `WithRetryOverride` instead. This is a design deviation from the stated plan.

3. **MEDIUM: `clampBackoff` is not defined in the diff** — The diff calls `clampBackoff(delay)` but doesn't define it. This might be a pre-existing function, but if not, it's a compile error.

4. **LOW: The skeptic's `MaxRetries` doesn't use the resolved Settings default** — When a skeptic config doesn't set `MaxRetries`, the skeptic uses the client default (2 retries) instead of the resolved Settings default (5 retries). This is consistent with the comment but might not be the intended behavior.

Actually, let me reconsider the `clampBackoff` issue. The diff says:
```go
delay = clampBackoff(delay)
```

If `clampBackoff` doesn't exist, this is a compile error. But the diff is being submitted for review, so presumably the code compiles. This means `clampBackoff` must exist in the original `client.go`.

Let me not flag this as an issue, since it's likely a pre-existing function.

Wait, but what if `clampBackoff` is a new function being added in this diff? Let me check the diff again...

The diff doesn't define `clampBackoff`. It only calls it. So it must be pre-existing. I'll assume it's pre-existing and not flag it.

Actually, I realize I should check if `clampBackoff` is a function that exists in the codebase. The comment says "every subsequent delay is clamped after the ×factor step", which suggests there IS a clampBackoff function that clamps delays. Let me assume it's pre-existing.

OK let me also check the `honorExact` variable. The diff doesn't change it, but it's used in the loop. Let me verify...

The diff shows:
```go
honorExact := false
for attempt := 0; attempt <= maxRetries; attempt++ {
    if attempt > 0 {
        sleepFor := delay
        if !honorExact {
```

The `honorExact` variable is set in the case `retryableStatus[status]`:
```go
if retryAfter > 0 {
    honorExact = true
    sleepFor = time.Duration(retryAfter) * time.Second
}
```

This is in the existing code (not shown in the diff but implied). The `honorExact` variable carries over from one iteration to the next, so a server-advertised `Retry-After` is honored on the next retry.

This is correct behavior.

Now let me also check the `lastErr` variable. The diff shows:
```go
var lastErr error
```

This is initialized to nil.
</think>
