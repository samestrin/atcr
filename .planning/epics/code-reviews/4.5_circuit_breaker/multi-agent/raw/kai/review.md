The user wants me to review a diff based on a sprint plan. I need to output only pipe-delimited findings in the exact format specified. I should only flag issues in files/areas directly related to the sprint plan's in-scope work.

Let me analyze the diff carefully:

1. `docs/metrics.md` - Out of scope? The sprint plan mentions metrics, but docs updates are not explicitly in scope. The sprint plan says IN scope: `internal/circuitbreaker`, `internal/metrics` gauge primitive, `llmclient`, `internal/fanout`, `internal/verify`, unit + integration tests. Docs are not listed. However, this is just documentation. I should probably not flag this as it's docs and not code issues.

2. `internal/boundaries_test.go` - Updating allowed imports. This seems like a test file checking package dependencies. The addition of `circuitbreaker` imports looks correct. No obvious issues.

3. `internal/circuitbreaker/breaker.go` - The core breaker implementation.
   - Line 130: `Allow()` has a comment warning that a caller must report the outcome exactly once via RecordSuccess, RecordFailure, or ReleaseProbe, otherwise the probe slot leaks. But there's no mechanism to enforce this or handle panics/deferred cleanup. If `dispatch` panics or the caller forgets, the circuit wedges half-open forever. This is a correctness/maintainability issue. However, the code does have `ReleaseProbe` for cancellations. But what about panics in the caller between `Allow()` returning true and recording the result? In `send`, if `dispatch` panics, the defer doesn't clean up the probe. This is a real issue.
   - Wait, looking at `client.go` line 310-330, `send` handles the outcome in a switch. But if `dispatch` panics, the probe slot is leaked. However, `dispatch` doesn't panic in normal Go HTTP operations, it returns errors. But a panic could happen from a nil pointer or something else. The sprint plan mentions "One chokepoint: this covers single-shot, the tool loop, and the verify path". If any path panics, the probe is leaked. This is a potential issue but might be considered edge case. However, the comment explicitly warns about this: "A caller that wins the half-open probe MUST report the outcome exactly once... otherwise the probe slot leaks and the circuit wedges half-open forever." This is by design but fragile. I should flag it as a potential correctness issue? Or is it acceptable given the design? Actually, looking at `send` in `client.go`, if `dispatch` panics, the `send` function panics, and the probe is never released. But Go HTTP clients don't typically panic. Still, it's a risk.
   - Another issue: `Breaker` struct has `provider` string, but in `Registry.Get`, `New` is called with the provider string. The `Registry` map key is also provider string. This looks fine.
   - `refresh()` uses `b.now().Sub(b.openedAt) >= b.cooldown`. If `cooldown` is 0, it would transition immediately. But cooldown is hardcoded to 60s. Not an issue.
   - `open()` sets `openedAt = b.now()` and `failureCount = 0`. In `RecordFailure` for `StateHalfOpen`, it calls `b.open()` then `b.probeInFlight = false`. But `open()` sets `failureCount = 0` and `openedAt`. Then `probeInFlight = false`. This is fine.
   - In `RecordSuccess()` for `StateHalfOpen`, it sets `probeInFlight = false`. Good.
   - What about `Allow()` for `StateHalfOpen`: it sets `probeInFlight = true` and returns true. If the caller then cancels (context.Canceled), `send` calls `ReleaseProbe()`. Good.
   - But wait: in `send`, if `provider == ""`, it calls `dispatch` directly and doesn't touch the breaker. Good.
   - What if `dispatch` returns a non-nil error that is NOT a breaker failure and NOT context.Canceled? Then `default` branch calls `breaker.RecordSuccess()`. This is for 4xx errors. The comment says: "a reply counts as a healthy round-trip — which also closes a half-open probe instead of wedging it." This seems correct per AC10.
   - However, what about context.DeadlineExceeded? That's a timeout. `isBreakerFailure` returns true for `context.DeadlineExceeded` (line 365 in client.go: `{"deadline", context.DeadlineExceeded, true}`). In `send`, `case isBreakerFailure(err):` catches it and calls `RecordFailure()`. Correct.
   - What about `context.Canceled`? In `send`, `case errors.Is(err, context.Canceled):` catches it. Correct.

4. `internal/circuitbreaker/breaker_test.go` - Tests look comprehensive.
   - `TestConcurrentAccessIsRaceFree` uses `time.Millisecond` cooldown. But `newTestBreaker` uses the real `time.Now` for `b.now`, not the test clock. Wait, `newTestBreaker` sets `b.now = clk.now`. So the clock is injected. But in the concurrent test, it doesn't capture the clock return value (`b, _ := newTestBreaker(t, 3, time.Millisecond)`). The clock advances never happen. The test relies on `time.Millisecond` being short enough that state transitions might happen? Actually, since `b.now = clk.now` and `clk` is discarded, `b.now` is the test clock's `now()` function. But nobody advances it. So `refresh()` will never transition from Open to HalfOpen because time doesn't advance. That's fine for a race detector test. It just calls Allow/RecordSuccess/RecordFailure/State randomly. This seems okay.

5. `internal/circuitbreaker/context.go` - Simple context value storage. Looks correct.

6. `internal/circuitbreaker/registry.go` - 
   - `Get` method: double-checked locking pattern. It uses `RLock` then `RUnlock`, then if not found, `Lock`. This is correct but the `defer r.mu.Unlock()` after the write lock is fine.
   - Wait, `if b, ok := r.breakers[provider]; ok { return b }` inside the write lock redeclares `b` and `ok` in the inner scope, then returns `b`. This is correct Go.
   - `Reset` drops the map. Fine.

7. `internal/circuitbreaker/registry_test.go` - Looks fine.

8. `internal/fanout/engine.go` - 
   - Line 446: `ctx = circuitbreaker.NewContext(ctx, a.Provider)`. This is correct.
   - But wait, what about `Agent.Provider`? It's added to the struct. In `buildAgent` and `buildFallbackAgent`, it's populated. Good.

9. `internal/fanout/engine_breaker_test.go` - Tests for AC6 and metrics.
   - `TestInvokeSlot_CircuitOpenTriggersFallback` - uses `newFake()` and `failFor`. The fake completer returns the error. This tests the fallback chain. Good.
   - `TestRecordAgentOutcome_CircuitOpenNotCountedAsAPICall` - checks metrics. Good.

10. `internal/fanout/metrics.go` - 
    - `recordAgentOutcome` now checks for `CircuitOpenError` and sets `apiCalls = 0`. Good.

11. `internal/fanout/review.go` - 
    - `buildAgent` sets `Provider: ac.Provider`. Good.
    - `buildFallbackAgent` sets `Provider: ac.Provider`. Good.

12. `internal/fanout/review_test.go` - 
    - Added `circuitbreaker.DefaultRegistry.Reset()` in `mockProvider`. Good for test isolation.

13. `internal/llmclient/client.go` - 
    - `send` function renamed to `dispatch`, new `send` wraps it.
    - The `isBreakerFailure` function: what about `errors.Is(err, context.DeadlineExceeded)`? It returns true because after the HTTPStatusError check, it falls through to `return true`. But `context.DeadlineExceeded` is not an HTTPStatusError, so it returns `true`. Correct.
    - But what if `err` is `context.Canceled` wrapped in `fmt.Errorf`? `errors.Is(err, context.Canceled)` handles wrapping. Good.
    - What if `err` is an `HTTPStatusError` with status 0? The code says `return he.Status >= 500`. Status 0 would return false. But a transport error might be represented as status 0? The code comment says "No HTTP response reached us: a transport-level failure or a timeout. Both mean the provider is effectively unavailable → count as a breaker failure." But `HTTPStatusError` with status 0 would not be caught here. Wait, is status 0 possible? The existing code might return `HTTPStatusError` with status 0 for transport errors? Let me check the existing client code (not fully shown in diff). The diff shows `isBreakerFailure` but not how `HTTPStatusError` is constructed. If transport errors are wrapped in `HTTPStatusError{Status: 0}`, then `isBreakerFailure` returns false for them! That would be a bug. But the test `TestBreaker_TransportFailureTrips` suggests that a connection refused transport error trips the breaker. How is that error represented? In the test, they close the server and call `c.Complete`. The error from `dispatch` is likely a network error (`*url.Error` or similar), not an `HTTPStatusError`. For network errors, `isBreakerFailure` returns `true` because it falls through to `return true`. Good.
    - However, what if `HTTPStatusError` is used for non-HTTP-response errors with status 0? If the existing code does that, it's a problem. But I don't see the construction of `HTTPStatusError` in the diff. The diff only shows the addition. I shouldn't assume existing bugs unless visible.
    - Wait, I see a potential issue: `send` calls `breaker.Allow()`. If `Allow()` returns true (half-open probe admitted), and then `dispatch` returns a 4xx error, `default` branch calls `breaker.RecordSuccess()`. This closes the half-open probe. But the original request failed with a 4xx error. Is it correct to call that a success for the breaker? Yes, per design: "a reply counts as a healthy round-trip". But wait, the user wanted the API call to fail with a 4xx error, and the breaker treats it as success. That's by design (AC10).
    - Another issue: `send` returns `raw, err` from `dispatch`. But if `breaker.Allow()` returns false, it returns `nil, &CircuitOpenError{Provider: provider}`. The error is constructed but not using `fmt.Errorf` or wrapping. That's fine.
    - In `send`, if `provider != ""` but `breaker.Allow()` is true, and `dispatch` returns an error, the switch handles it. But what if `dispatch` returns `(nil, nil)`? That's impossible but if it did, `err == nil` triggers `RecordSuccess()`. Fine.
    - What if `dispatch` panics? As noted earlier, the probe leaks. This is a real issue. Let me look for evidence in the code. The `send` method has no defer to handle panics. If `dispatch` panics, the half-open probe stays in-flight forever. This means `Allow()` will keep returning false for that provider. This is a correctness issue. I should flag it.
    - Actually, looking more carefully at `client.go`, `dispatch` is the inner function. It performs HTTP requests. HTTP client methods in Go don't panic under normal circumstances. But a nil pointer dereference inside `dispatch` could happen (e.g., `c` is nil, or `body` is nil and something dereferences it? No, body is `[]byte`). The risk is low but non-zero. However, is this in scope? The sprint plan mentions "Race condition in concurrent breaker access" as a risk, but not panics. The code explicitly documents the probe leak risk. Given the explicit design choice and the low likelihood, maybe I shouldn't flag it? But the instructions say "Correctness - Logic errors, race conditions, edge cases". A panic wedge is an edge case. I'll flag it as MEDIUM.
    - Actually, wait. There's another issue. In `client.go` `send`:
      ```go
      raw, err := c.dispatch(ctx, endpoint, key, body)
      switch {
      case err == nil:
          breaker.RecordSuccess()
      case isBreakerFailure(err):
          breaker.RecordFailure()
      case errors.Is(err, context.Canceled):
          breaker.ReleaseProbe()
      default:
          breaker.RecordSuccess()
      }
      return raw, err
      ```
      If `dispatch` returns an error that wraps `context.Canceled` but is also an `HTTPStatusError`? No, `context.Canceled` is checked before `isBreakerFailure`? No, `isBreakerFailure` checks `errors.Is(err, context.Canceled)` first and returns false. Then `send`'s switch checks `errors.Is(err, context.Canceled)` second. But if `err` is `context.Canceled`, `isBreakerFailure` returns false, and then `errors.Is(err, context.Canceled)` is true. So it hits `ReleaseProbe()`. Good.
      But what about `context.DeadlineExceeded`? `isBreakerFailure` returns true for it (falls through to `return true`). So it hits `RecordFailure()`. Good.
      However, what if `err` is both `context.Canceled` AND an `HTTPStatusError{Status: 500}`? Impossible in practice.

14. `internal/llmclient/client_breaker_test.go` - 
    - `TestBreaker_TransportFailureTrips` closes the server. `srv.Client()` is reused. But `httptest.Server.Client()` is a method that returns a client configured for the server. After closing, requests fail. Good.
    - `TestBreaker_RetriesThenTripsPerInvocation` uses `New(WithHTTPClient(srv.Client()), WithRetry(2, time.Millisecond, 1.5))`. This tests that retries happen before breaker recording. Good.

15. `internal/metrics/metrics.go` - 
    - Added `gauge` type with atomic float64 storage using `math.Float64bits`. This is correct and lock-free.
    - `Registry.Reset()` recreates maps. Good.

16. `internal/metrics/prometheus.go` - 
    - Added gauge rendering in `WritePrometheus`. `formatFloat` is used. Need to check if `formatFloat` handles all cases. It's existing code. The gauge output looks correct.
    - One issue: the order of exposition. Counters first, then gauges, then histograms. That's valid.

17. `internal/metrics/names.go` - Added constants. Fine.

18. `internal/verify/invoke.go` - Added `Provider: c.Provider`. Fine.

Now let me look for more subtle issues:

**Issue 1: Probe leak on panic in `send`**
If `c.dispatch` panics while a half-open probe is in-flight, the `probeInFlight` flag is never cleared, and the circuit breaker wedges in half-open state forever. A `defer` with `recover` or a `defer` paired with a closure tracking the need to release could mitigate this, but the current code lacks any panic recovery.
- File: `internal/circuitbreaker/breaker.go` or `internal/llmclient/client.go`? The leak is in `breaker.go`'s state, but the cause is in `client.go`'s failure to guarantee cleanup. Actually, the contract is that the caller of `Allow()` must call `RecordSuccess/RecordFailure/ReleaseProbe`. The `send` function in `client.go` is that caller. If `dispatch` panics, `send` panics, and the guarantee is broken. So the issue is in `client.go:send` not having a defer to release the probe if it panics, or `breaker.go` not providing a way to do this easily. But since `send` is the integration point, I can flag it in `client.go`.
Wait, but `send` doesn't know it got a probe until `Allow()` returns true. If it panics during `dispatch`, it can't run code. A defer would work:
```go
func (c *Client) send(...) {
    ...
    if !breaker.Allow() { ... }
    // We have a probe slot. Ensure it is released if we panic before recording.
    probeResolved := false
    defer func() {
        if !probeResolved {
            breaker.ReleaseProbe()
        }
    }()
    raw, err := c.dispatch(...)
    probeResolved = true
    ...
}
```
This is a valid concern. Is it worth flagging? Yes, it's a correctness issue. Any panic in the HTTP stack (e.g., a buggy transport) would permanently wedge the breaker for that provider. MEDIUM severity.

**Issue 2: `Registry.Get` potential memory leak / unbounded growth**
`Registry.Get` creates a new breaker on first use and stores it forever. `DefaultRegistry` is global. If there are many unique provider names (e.g., dynamic or user-controlled), the map grows unbounded. However, provider names come from `AgentConfig.Provider`, which is from config. The sprint plan says provider identity is the logical provider name. If a user can create arbitrary provider names, this is a memory leak. But typically configs are static. Is this a real issue? The sprint plan says "Provider identity = logical provider name". It's not obviously unbounded. I'll skip this as a false positive or out of scope.

**Issue 3: `isBreakerFailure` and wrapped `HTTPStatusError`**
`isBreakerFailure` uses `errors.As(err, &he)`. If `err` wraps an `HTTPStatusError`, it unwraps correctly. Good.

**Issue 4: `isBreakerFailure` nil check**
`if err == nil { return false }`. Good.

**Issue 5: `Breaker.State()` vs `String()`**
`State.String()` handles unknown state. Good.

**Issue 6: `Breaker.open()` sets `failureCount = 0`**
When transitioning from HalfOpen to Open via `RecordFailure`, `open()` is called, which sets `failureCount = 0`. This is correct because the consecutive-failure run is reset when the cooldown starts.

**Issue 7: `refresh()` called under lock in `State()` and `Allow()`**
`State()` calls `refresh()` under lock. `Allow()` calls `refresh()` under lock. Good.

**Issue 8: `setMetric` called under lock**
`setMetric` is called from `transition()`, which is called from `refresh()` and `open()`. Both hold the lock. `New()` calls `setMetric` before returning, but `b` is not yet shared. Good.

**Issue 9: `metrics.Gauge` in `setMetric`**
`setMetric` calls `metrics.Gauge(...)`. This acquires `metrics.DefaultRegistry.mu`. Since `metrics` never calls back into `circuitbreaker`, no deadlock. Good.

**Issue 10: `TestBreaker_OpensAfterThree500s` and `statusServer`**
`statusServer` writes `{"error":"boom"}` after WriteHeader. For 500, that's fine.

**Issue 11: `noRetryClient` uses `srv.Client()`**
`srv.Client()` returns a client configured to not follow redirects and to skip TLS verification? Actually `httptest.Server.Client()` returns an HTTP client configured to trust the server's TLS certificate and to not follow redirects. It's fine.

**Issue 12: `resetBreakers` uses `DefaultRegistry.Reset()`**
Tests reset the global registry. Good.

**Issue 13: `TestBreaker_TransportFailureTrips`**
`baseURL := srv.URL` then `srv.Close()`. But `client := srv.Client()` before close. The client is still usable. Good.

**Issue 14: `TestBreaker_RetriesThenTripsPerInvocation`**
The comment says "three invocations against a 500 — nine HTTP attempts — trip the breaker after the third invocation." Wait, with 2 retries, `WithRetry(2, ...)` means max 3 attempts per invocation. If the first invocation fails after 3 attempts, `dispatch` returns an error. `send` calls `RecordFailure()` once per invocation. So 3 invocations = 3 failures → breaker opens. The test asserts `hits.Load() == 9`. Good.

**Issue 15: `TestBreaker_CancellationDoesNotTrip`**
`cancel()` before the call. `Complete` will check context. The client should return `context.Canceled`. `isBreakerFailure` returns false for `context.Canceled`. `send` handles it. Good.

**Issue 16: `TestBreaker_429StaysClosed`**
Calls `c.Complete` 5 times. Each time `dispatch` returns 429. `isBreakerFailure` sees `HTTPStatusError{Status: 429}` and returns false. `send` hits `default:` and calls `RecordSuccess()`. Wait, this resets the failure run every time! So even if there were other failures, a 429 resets the run. This is by design ("a reply counts as a healthy round-trip"). But is it correct for a 429 to count as success for the breaker? Yes, per AC10 and the rubber-duck clarification: "ALL 4xx — including 429 and 401 — stay closed". The reasoning is that the server replied, so it's reachable. This is intended.

**Issue 17: `Breaker` zero value**
The comment says "The zero value is not usable — it has no clock." If someone does `var b Breaker` and calls `Allow()`, `b.now` is nil, causing panic. But the code only uses `New()` or `Registry.Get()`, which both use `New()`. So it's not an issue in practice, but the comment is correct.

**Issue 18: `Registry.Reset` not safe with concurrent `Get`**
If `Reset` is called while another goroutine is in `Get`, the read lock is held, then `Reset` acquires write lock? `Reset` uses `Lock`. `Get` uses `RLock` then `RUnlock` then `Lock`. If `Reset` happens between `RUnlock` and `Lock` in `Get`, fine. If `Reset` happens while `Get` holds `RLock`, `Reset` blocks on `Lock`. Then `Get` releases `RLock` and acquires `Lock`... but `Reset` is waiting for `Lock`. Once `RUnlock` happens, either `Reset` or `Get` gets the `Lock`. If `Reset` gets it, it clears the map. Then `Get` gets the lock, and re-checks `breakers[provider]`. It's not found, so it creates a new one. This is safe.

**Issue 19: `Breaker` state transitions and `openedAt`**
`refresh()` compares `b.now().Sub(b.openedAt)`. If `openedAt` is zero (default time.Time), and cooldown is 0, it might transition immediately. But `openedAt` is set by `open()`, and `New()` doesn't call `open()`. If state is somehow Open with zero openedAt, `refresh` would compute `now.Sub(zero)` which is a huge duration, >= cooldown, so it would transition to HalfOpen. But `open()` always sets `openedAt`. So no issue.

**Issue 20: `send` error wrapping for `CircuitOpenError`**
`CircuitOpenError` doesn't implement `Unwrap()`. It's a terminal error. That's fine.

**Issue 21: `isBreakerFailure` returns true for ANY non-nil, non-canceled, non-HTTP<500 error.**
This includes DNS errors, connection refused, `url.Error`, `net.Error`, etc. Also `context.DeadlineExceeded`. This matches AC10.

**Issue 22: `send` default case for 4xx**
The `default` case comment says: "A non-tripping HTTP response (4xx incl. 429/401): the provider replied, so it is reachable. The breaker tracks outages, not auth/rate-limit correctness, so a reply counts as a healthy round-trip — which also closes a half-open probe instead of wedging it."
But what about a 400 Bad Request? That also counts as success. Any error that is not nil, not breaker failure, not context.Canceled, is treated as success. This includes future error types that might indicate provider unhealthiness but aren't transport errors. However, the design explicitly says only 5xx/timeout/transport count.

**Issue 23: `Breaker.Allow()` and `Breaker.State()` consistency**
`Allow()` calls `refresh()` and might transition Open→HalfOpen. `State()` also calls `refresh()`. This is fine.

**Issue 24: `internal/fanout/engine.go` `invokeAgent` context threading**
`ctx = circuitbreaker.NewContext(ctx, a.Provider)` is set. But what if `a.Provider` is empty? The context gets an empty string. `ProviderFromContext` returns empty string. `send` skips breaker. This is the intended no-op behavior for doctor.

**Issue 25: `buildFallbackAgent` Provider**
The comment says "A fallback keys on its OWN provider". It sets `Provider: ac.Provider`. If the fallback agent config uses the same provider as the primary but a different model, it gets the same breaker. If it uses a different provider, it gets a different breaker. This is correct.

**Issue 26: `TestInvokeSlot_CircuitOpenTriggersFallback`**
The fake completer returns `CircuitOpenError`. The engine invokes the slot. `invokeSlot` sees the error. Since `CircuitOpenError` is a permanent failure (not timeout), it triggers fallback. The test asserts `StatusOK`. Good.

**Issue 27: `TestInvokeSlot_CircuitOpenChainFailsAsFailed`**
Both primary and fallback return `CircuitOpenError`. The slot result is `StatusFailed`. Good.

**Issue 28: `TestRecordAgentOutcome_CircuitOpenNotCountedAsAPICall`**
`recordAgentOutcome` checks `var coe *llmclient.CircuitOpenError` then `errors.As(r.Err, &coe)`. This requires `r.Err` to be or wrap a `*CircuitOpenError`. If `r.Err` is not a pointer but a value type `llmclient.CircuitOpenError`, `errors.As` with `&coe` (where `coe` is `*llmclient.CircuitOpenError`) requires the error to implement `error` and be assignable. Since `CircuitOpenError` has pointer receiver for `Error()`, `errors.As` works with the pointer type if the dynamic type is `*CircuitOpenError`. If the dynamic type is `CircuitOpenError` (value), `errors.As` can still assign it to `*CircuitOpenError`? No, `errors.As` requires the target to be a pointer to an interface or a type implementing error. If the target is `**CircuitOpenError`? Wait, the code is:
```go
var coe *llmclient.CircuitOpenError
case errors.As(r.Err, &coe):
```
`&coe` is `**llmclient.CircuitOpenError`. `errors.As` will set `*coe` (which is `coe`, a `*llmclient.CircuitOpenError`) to the error if it matches. If `r.Err` is `*llmclient.CircuitOpenError`, it works. If `r.Err` is `llmclient.CircuitOpenError` (value), `errors.As` will create a pointer to it? Actually, `errors.As` assigns the error to the target. The target must be a pointer to the type we want. Since `CircuitOpenError` implements `error` with a pointer receiver, typically the error value is `*CircuitOpenError`. So this is fine.

**Issue 29: `CircuitOpenError` used in metrics**
`recordAgentOutcome` uses `errors.As(r.Err, &coe)`. In `client.go`, `send` returns `&CircuitOpenError{Provider: provider}`. So it's always a pointer. Good.

**Issue 30: `send` function signature changed?**
The old `send` is renamed to `dispatch`. The new `send` wraps it. Callers of `send` (Complete, Chat) are not shown in the diff, but they presumably still call `send`. This is a rename + wrap. The diff doesn't show the callers, but the existing code should still call `send`. This is fine.

**Issue 31: `breaker_test.go` `newTestBreaker` uses `metrics.DefaultRegistry.Reset()`**
But `metrics.DefaultRegistry` might have gauges from other tests? `t.Cleanup` resets it. Good.

**Issue 32: `registry_test.go` `TestDefaultRegistryUsable`**
Calls `DefaultRegistry.Get("openai")`. This creates a breaker and sets a metric. But `DefaultRegistry.Reset()` is not called in this test. The next test might see leftover metrics? Actually `newTestBreaker` resets the registry. `TestDefaultRegistryUsable` doesn't reset. But `TestRegistryGetCreatesAndShares` uses a fresh `NewRegistry()`. The tests using `DefaultRegistry` call `Reset()` or `newTestBreaker`. `TestDefaultRegistryUsable` doesn't cleanup. This could pollute state for subsequent tests if they use `DefaultRegistry`. However, the tests look mostly isolated. But wait, `TestProviderContextRoundTrip` etc. don't use `DefaultRegistry`. `TestRegistryGetConcurrentSingleInstance` uses `NewRegistry()`. So `TestDefaultRegistryUsable` might leave a breaker for "openai" in the global registry. Then a later test that uses `DefaultRegistry` without resetting might see it. For example, `TestDefaultRegistryUsable` itself is simple. But looking at the test file, there's no other test using `DefaultRegistry` without reset except maybe not shown. This is a minor maintainability issue in tests. But it's test code, and the prompt says focus on diff changes. The diff includes `registry_test.go`. Is this a real issue worth flagging? It's a test hygiene issue. Not severe.

**Issue 33: `client_breaker_test.go` `resetBreakers`**
Calls `circuitbreaker.DefaultRegistry.Reset()`. Good.

**Issue 34: `client_breaker_test.go` `TestBreaker_4xxResetsFailureRun`**
Uses an atomic `code` to switch between 500 and 429. The server reads `code.Load()` for each request. Good.

**Issue 35: `client.go` `send` missing `defer` for probe release on panic**
This is the most concrete issue I can find. Let me verify if there's any other.

**Issue 36: `Breaker` `Allow()` comment vs implementation**
The comment says: "A caller that wins the half-open probe MUST report the outcome exactly once via RecordSuccess, RecordFailure, or ReleaseProbe". But `Allow()` returns true for `StateClosed` as well. In `StateClosed`, the caller does not need to call any of those. The comment is specifically about half-open. But the warning is placed on the `Allow()` method generally. This is a documentation quirk, not a bug.

**Issue 37: `RecordSuccess` in `StateOpen`**
The comment says: "A success observed while open (an in-flight call that started before the trip) only clears the failure run; the cooldown-driven half-open probe still governs reopening." The code sets `b.failureCount = 0`. But it does NOT set `probeInFlight = false`. Why would it? In StateOpen, `probeInFlight` should always be false because `open()` doesn't set it, and `Allow()` in Open returns false without setting it. Only `HalfOpen` sets `probeInFlight`. So if `RecordSuccess` is called while Open, `probeInFlight` is already false. Fine.

**Issue 38: `RecordFailure` in `StateOpen`**
The code does nothing. Good.

**Issue 39: `ReleaseProbe` outside half-open**
It unconditionally sets `probeInFlight = false`. If called in Closed or Open, it's harmless. Good.

**Issue 40: `TestHalfOpenReleaseProbeReadmits`**
Calls `b.ReleaseProbe()` while half-open. Then `b.Allow()` returns true. Then `b.RecordSuccess()` closes. Good.

**Issue 41: `Registry.Reset()` and metrics**
`Registry.Reset()` drops breakers but does not reset metrics. If a breaker was Open, the gauge remains at 1 even though the breaker is gone. A new breaker for the same provider will be created Closed and set gauge to 0. But if `Reset` is called and a different provider name is never used again, the gauge for that provider stays. This is only for tests. Not an issue.

**Issue 42: `Registry.Get` with empty provider string**
Returns a breaker for empty string. This is allowed. `send` checks `provider == ""` and skips breaker entirely. So empty string breaker is never used. Fine.

**Issue 43: `NewContext` with empty string**
Returns a context with empty string. `ProviderFromContext` returns empty. `send` skips. Fine.

**Issue 44: `isBreakerFailure` and `context.Canceled`**
Checks `errors.Is(err, context.Canceled)`. But what about `context.Canceled` wrapped inside an `HTTPStatusError`? If the HTTP client returns an error that wraps both, `errors.Is` handles it. But if the error is a custom type that has `Error()` string but doesn't implement `Is()`, `errors.Is` won't match. However, standard library contexts are unwrapped via `Is()` interface. This is standard.

**Issue 45: `send` function in `client.go`**
The `switch` statement:
```go
switch {
case err == nil:
    breaker.RecordSuccess()
case isBreakerFailure(err):
    breaker.RecordFailure()
case errors.Is(err, context.Canceled):
    breaker.ReleaseProbe()
default:
    breaker.RecordSuccess()
}
```
What if `err` is a breaker failure AND `errors.Is(err, context.Canceled)`? Impossible.
What if `err` is nil but `raw` is nil? `RecordSuccess()` is called. Fine.

**Issue 46: `Breaker.state` int type**
`State` is `int`. `setMetric` casts to `float64(s)`. Good.

**Issue 47: `Breaker` concurrency in `Allow()`**
`Allow()` holds the lock for the entire duration. This is correct but serializes all calls. The comment says "the lock is never the hot path" because provider calls are slower. Fine.

**Issue 48: `TestConcurrentAccessIsRaceFree`**
Uses `time.Millisecond` cooldown. The test clock is injected but not used (discarded). `b.now` is the clock function. Since the clock never advances, `refresh()` never transitions Open→HalfOpen. But goroutines might call `RecordFailure` and transition Closed→Open. Then `Allow()` returns false. The race detector checks for data races. This is fine.

**Issue 49: `client_breaker_test.go` `statusServer`**
`statusServer` takes `hits *atomic.Int64`. It adds 1 per hit. Good.

**Issue 50: `client_breaker_test.go` `noRetryClient`**
`WithRetry(0, time.Millisecond, 1.5)` - retry count 0 means no retries? Or 1 attempt? The test comment says "no retries so each Complete is exactly one HTTP attempt". Looking at the retry logic (not shown), `WithRetry(0, ...)` likely means max 1 attempt (0 retries). This is consistent.

**Issue 51: `client.go` `send` returning raw bytes on circuit open**
If circuit is open, it returns `nil, &CircuitOpenError{Provider: provider}`. The caller (`Complete`/`Chat`) will see nil body and the error. Good.

**Issue 52: `Breaker.threshold` and `failureCount`**
If `threshold` is 0, `RecordFailure` in closed state increments to 1, `1 >= 0` is true, so it opens immediately. But `DefaultThreshold` is 3. Not an issue unless someone calls `New` with 0. The function is unexported usage mostly. Not a concern.

**Issue 53: `Breaker` time comparisons**
`refresh()` uses `b.now().Sub(b.openedAt) >= b.cooldown`. If `cooldown` is negative, it transitions immediately. Not possible with hardcoded 60s.

**Issue 54: `isBreakerFailure` and `HTTPStatusError` with status < 100**
If status is 0 or other non-standard, `he.Status >= 500` is false, falls through to `return true`. This would classify a status 0 as a breaker failure. But is status 0 used? If the HTTP client returns an error without a response, it might not be an `HTTPStatusError`. This is likely fine.

**Issue 55: `engine_breaker_test.go` `TestEngine_ThreadsProviderToContext`**
Uses `pc := &provCapture{}`. `provCapture.Complete` captures the provider from context. Good.

**Issue 56: `engine_breaker_test.go` `TestInvokeSlot_CircuitOpenTriggersFallback`**
`f.failFor["primary"]` is set. But how does `invokeSlot` know to use this fake? The fake completer returns errors based on the model or name? `newFake()` seems to be a test helper. The test compiles, so it's fine.

**Issue 57: `fanout/metrics.go` `recordAgentOutcome`**
The original code:
```go
if errors.Is(r.Err, context.DeadlineExceeded) || errors.Is(r.Err, context.Canceled) {
    apiCalls = 0
} else {
    apiCalls = 1
}
```
New code:
```go
var coe *llmclient.CircuitOpenError
switch {
case errors.Is(r.Err, context.DeadlineExceeded)