<think>
Let me analyze the diff for issues related to the sprint plan (Epic 4.5: Circuit Breaker and Provider Health Tracking).

The diff touches:
1. `docs/metrics.md` - Documentation update for circuit breaker metric
2. `internal/boundaries_test.go` - Dependency allowlist updates
3. `internal/circuitbreaker/breaker.go` - New circuit breaker implementation
4. `internal/circuitbreaker/breaker_test.go` - Breaker tests
5. `internal/circuitbreaker/context.go` - Provider context helpers
6. `internal/circuitbreaker/registry.go` - Registry
7. `internal/circuitbreaker/registry_test.go` - Registry tests
8. `internal/fanout/engine.go` - Engine integration with provider context
9. `internal/fanout/engine_breaker_test.go` - Engine breaker tests
10. `internal/fanout/metrics.go` - Metrics updates for CircuitOpenError
11. `internal/fanout/review.go` - Setting Provider on Agent
12. `internal/fanout/review_test.go` - Test isolation
13. `internal/llmclient/client.go` - Circuit breaker integration in client
14. `internal/llmclient/client_breaker_test.go` - Client breaker tests
15. `internal/metrics/metrics.go` - Gauge primitive
16. `internal/metrics/metrics_test.go` - Gauge tests
17. `internal/metrics/names.go` - New metric name constant
18. `internal/metrics/prometheus.go` - Prometheus exposition for gauges
19. `internal/metrics/prometheus_test.go` - Prometheus gauge tests
20. `internal/verify/invoke.go` - Setting Provider on skeptic agent

Let me look for issues:

1. **`internal/circuitbreaker/breaker.go` - `State.String()` for StateOpen is `"open"`, but the gauge value 1 represents open. The docstring says "0/1/2", but the spec mentions state labels. Let me check... AC7 says `atcr_circuit_breaker_state` metric reflects current state per provider. The metric is a gauge with values 0/1/2. This seems consistent.

2. **`internal/circuitbreaker/breaker.go` - `RecordFailure` while `StateHalfOpen`**: The code does `b.open()` then `b.probeInFlight = false`. But the order is: `open()` is called which calls `transition(StateOpen)`, then `b.probeInFlight = false`. The `b.failureCount = 0` is set inside `open()`. That looks fine.

3. **`internal/circuitbreaker/breaker.go` - `RecordSuccess` while half-open**: The code does `b.transition(StateClosed); b.failureCount = 0; b.probeInFlight = false`. Wait, the test `TestOpenSuccessDoesNotClose` expects: open state, then success called, should stay open. In the code, `RecordSuccess` only transitions in the `StateHalfOpen` case, and in the default case it just resets failureCount. So if state is Open, RecordSuccess hits the default case and doesn't transition. Good.

4. **`internal/circuitbreaker/breaker.go` - `RecordFailure` while `StateOpen`**: The default case is empty (no-op). So a failure while open doesn't extend the cooldown. The test `TestOpenFailureDoesNotExtendCooldown` verifies this. Good.

5. **`internal/llmclient/client.go` - `isBreakerFailure` for `context.DeadlineExceeded`**: It uses `errors.Is` for `context.Canceled` (which returns false), but then if the error is `context.DeadlineExceeded` and not wrapped in HTTPStatusError, the fallback `return true` is reached. Good.

   Wait, but `context.DeadlineExceeded` is NOT a `context.Canceled` in Go. `errors.Is(err, context.Canceled)` only matches `context.Canceled` (or wrapped). And `context.DeadlineExceeded` returns `true` from the `return true` at the end. So this is correct.

6. **`internal/llmclient/client.go` - `send` function**: When the provider is empty, it calls `c.dispatch` directly, bypassing the breaker. But what about recording success/failure for the no-provider case? If we bypass, we never call RecordSuccess/RecordFailure. That's intentional - "an empty provider (e.g. `atcr doctor`'s direct `llmclient.Complete` calls) makes the breaker a no-op — correct, since a diagnostic must probe every endpoint regardless of circuit state." This matches the spec.

7. **`internal/llmclient/client.go` - `send` function default case**: The default case for non-tripping HTTP errors (4xx) calls `breaker.RecordSuccess()`. The comment says "A non-tripping HTTP response (4xx incl. 429/401): the provider replied, so it is reachable. The breaker tracks outages, not auth/rate-limit correctness, so a reply counts as a healthy round-trip — which also closes a half-open probe instead of wedging it."

   Wait, this is a problem! A 4xx response means the server replied, but calling `RecordSuccess` will reset the failure run. This is actually consistent with the test `TestBreaker_4xxResetsFailureRun`. However, this means a 401 from a misconfigured server would never accumulate enough failures to trip the breaker, even if the misconfiguration is permanent. The intent is that auth errors are permanent and provider-wide tripping would be incorrect. The current behavior is that a 4xx "resets" the failure count, which means a misconfigured key would never trip the breaker (it would stay closed forever).

   Actually, looking more carefully: the spec says "4xx auth errors are permanent and provider-wide tripping would be incorrect." So not tripping on 4xx is correct. But the current implementation calls `RecordSuccess` on 4xx, which RESETS the failure run. This is problematic - a series of 500s interrupted by a 429 would never trip the breaker, even if the underlying issue is genuine. The test `TestBreaker_4xxResetsFailureRun` actually verifies this behavior.

   This might be intentional or might be a bug. The comment says "the breaker tracks outages, not auth/rate-limit correctness, so a reply counts as a healthy round-trip." This is a design decision: 4xx = reachable = healthy. The test confirms it. So while counterintuitive, it's the documented design.

8. **`internal/llmclient/client.go` - HTTPStatusError check**: The `isBreakerFailure` checks `errors.As(err, &he)` and then `he.Status >= 500`. But what if the error is wrapped multiple times? `errors.As` should still find it. Good.

9. **`internal/fanout/engine.go` - Provider context threading**: The comment says "Empty provider no-ops the breaker." This is correct.

10. **`internal/circuitbreaker/breaker.go` - The `DefaultRegistry` is initialized lazily on first use. But in `New()`, the breaker calls `b.setMetric(StateClosed)` which writes to the metrics. This means the gauge is registered with the provider name immediately, even if no requests are made. That's fine.

11. **`internal/metrics/prometheus.go` - Gauge format**: `formatFloat(gauges[k].Value())` - this is reused from counter formatting. Let me check what `formatFloat` does... It's defined in prometheus.go. It should work for gauge values too since they're floats.

12. **`internal/fanout/metrics.go` - CircuitOpenError handling**: The metrics code now uses `errors.As` to check for `CircuitOpenError` and sets `apiCalls = 0`. This is a switch statement now. The logic is: cancellation/deadline → 0 calls; CircuitOpenError → 0 calls; default → 1 call. This looks correct.

13. **`internal/circuitbreaker/registry.go` - Race condition in `Get`**: The code does RLock, check, RUnlock, then Lock, re-check, create. This is the standard double-checked locking pattern. Good.

14. **`internal/llmclient/client_breaker_test.go` - `TestBreaker_RetriesThenTripsPerInvocation`**: This test has retries=2 (meaning 3 attempts per invocation). It expects 3 invocations × 3 attempts = 9 HTTP hits. The comment says "the breaker records one outcome per agent invocation (after that invocation's own retry budget), not per HTTP attempt." This is correct because `send` calls `dispatch` (which does the retry loop), and only records the final outcome.

    But wait - if retries are enabled, the dispatch loop retries within the same `send` call. So if 3 invocations each do 3 attempts, that's 9 total HTTP requests. The breaker sees 3 failures (one per invocation), so it trips after 3 invocations. Good.

15. **`internal/llmclient/client.go` - `isBreakerFailure` and `context.DeadlineExceeded`**: The function returns `true` for `context.DeadlineExceeded` because it's not a `context.Canceled` and not an `HTTPStatusError`. The test `TestIsBreakerFailureClassification` confirms this. But is this correct? A timeout means the provider didn't respond in time, which is a breaker failure. Yes, correct.

16. **`internal/circuitbreaker/breaker.go` - `State.String()`**: The test `TestStateString` expects "half-open" for `StateHalfOpen`. The String() method returns "half-open". Good.

17. **`internal/circuitbreaker/breaker.go` - Comment inconsistency**: The comment says "numeric value (0/1/2) is what the gauge stores; this is for logs and the String() metric note." But there's no "String() metric note" - the gauge stores the numeric value. This is just a minor comment issue.

18. **`internal/circuitbreaker/breaker.go` - `New` function**: It calls `b.setMetric(StateClosed)` which writes to the metrics registry. But if `New` is called inside a test that hasn't reset the metrics registry, the gauge might already exist from a previous test. The test `newTestBreaker` calls `metrics.DefaultRegistry.Reset()` first, so this is handled.

19. **`internal/llmclient/client.go` - `isBreakerFailure` for non-HTTPStatusError**: If the error is not an `HTTPStatusError` and not `context.Canceled`, it returns `true`. This means ALL transport errors (connection refused, DNS failure, EOF, etc.) count as breaker failures. This is the extended AC10 behavior. Good.

20. **`internal/circuitbreaker/breaker.go` - `Allow` when StateClosed and probeInFlight is true**: This is a potential issue. If a probe was in flight (from half-open) and the circuit was somehow closed (via RecordSuccess), the probeInFlight is set to false in RecordSuccess. So this shouldn't be an issue. But what if there's a race? The mutex protects this. Good.

21. **`internal/circuitbreaker/breaker.go` - `RecordSuccess` in `StateOpen` case**: The default case just resets `failureCount = 0`. The comment says "A success observed while open (an in-flight call that started before the trip) only clears the failure run; the cooldown-driven half-open probe still governs reopening." This is correct.

22. **`internal/fanout/engine.go` - `invokeAgent` context threading**: The context is updated with `circuitbreaker.NewContext(ctx, a.Provider)`. But what if `a.Provider` is empty? The context will carry an empty string, and `ProviderFromContext` will return "". The breaker will be a no-op. Good.

23. **`internal/fanout/review.go` - `buildFallbackAgent`**: The fallback agent gets `Provider: ac.Provider` from the fallback's own config. The comment says "A fallback keys on its OWN provider: if it uses a different provider than the primary, it gets that provider's breaker (so a fallback can succeed while the primary's circuit is open)." This is correct.

24. **`internal/verify/invoke.go` - `buildSkepticAgent`**: Sets `Provider: c.Provider` from the skeptic config. Good.

25. **`internal/circuitbreaker/breaker.go` - Lock ordering concern**: The comment says "The metrics call takes the registry's own lock briefly; metrics never calls back into this package, so there is no lock-ordering cycle." Let me check: `metrics.Gauge(...)` calls `DefaultRegistry.Gauge(name)` which takes the registry's mutex. The breaker holds its own mutex. If another goroutine holds the metrics registry lock and tries to get the breaker... but metrics doesn't call back into circuitbreaker, so no cycle. Good.

26. **`internal/llmclient/client.go` - `send` function and the default case for 4xx**: As I noted above, calling `RecordSuccess` on 4xx means a series of 500s interrupted by a 429 will never accumulate to 3 consecutive failures. The test `TestBreaker_4xxResetsFailureRun` explicitly tests this. This is a design decision documented in the code. While it could be argued that a 4xx should be a "neutral" outcome (not success, not failure, just a different kind of error), the current behavior is that 4xx = reachable = healthy round-trip.

    Actually, let me reconsider. The spec says: "a breaker failure is 5xx OR a timeout OR a connection-level transport error (connection refused/reset, EOF, DNS). ALL 4xx — including 429 and 401 — stay closed (a 4xx means the server replied; categorically distinct from a transport-layer failure where no response arrived)."

    The spec says 4xx should "stay closed" - it doesn't say they should reset the failure run. But the implementation calls `RecordSuccess` which resets the run. This could be seen as over-correction: a 4xx should neither count as a failure nor as a success - it should be neutral. The current behavior means that if a provider is having intermittent 500s but also returns 429s, the breaker will never trip.

    However, the test `TestBreaker_4xxResetsFailureRun` is named to suggest this is intentional ("4xx resets the failure run"). And the comment in the code says "A non-tripping HTTP response (4xx incl. 429/401): the provider replied, so it is reachable. The breaker tracks outages, not auth/rate-limit correctness, so a reply counts as a healthy round-trip."

    This is a design decision. I'll flag it as a MEDIUM concern because the behavior (4xx resets the failure run) is not explicitly mandated by the spec and could lead to the breaker never tripping in mixed-error scenarios. But it might be intentional.

    Actually, looking at the test more carefully: the test is titled "4xxResetsFailureRun" and the comment says "(Independent-review fix: this also closes a half-open probe instead of wedging it.)" This suggests it was added as a fix during review. So it's an intentional design choice.

    I'll note this as a LOW or MEDIUM finding - the 4xx behavior of resetting the failure run is a design choice that could mask genuine outages if errors are mixed.

27. **`internal/circuitbreaker/breaker.go` - `State.String()` for StateOpen**: Returns "open". The gauge value for open is 1. The spec says values 0/1/2 for closed/open/half-open. Consistent.

28. **`internal/circuitbreaker/breaker.go` - `New` calls `setMetric` before the breaker is returned**: This means the gauge is registered immediately. If the breaker is created but never used, the gauge will still show 0. This is fine.

29. **`internal/circuitbreaker/breaker.go` - `RecordFailure` in `StateHalfOpen`**: Calls `b.open()` which sets `openedAt = b.now()` and `failureCount = 0`, then sets `b.probeInFlight = false`. The order is: transition to Open, set openedAt, reset failureCount, then clear probeInFlight. This is correct.

30. **`internal/llmclient/client.go` - `send` function order**: `breaker.Allow()` is called, then `c.dispatch()`. If Allow returns false, we return `CircuitOpenError` immediately. If Allow returns true (possibly as the half-open probe), we call dispatch. After dispatch, we record the outcome. This is correct.

    But wait - what if the context is cancelled between `Allow` and `dispatch`? The `dispatch` function will return `context.Canceled`. Then the switch case `errors.Is(err, context.Canceled)` calls `breaker.ReleaseProbe()`. This correctly releases the probe slot. Good.

31. **`internal/llmclient/client.go` - `send` function and `isBreakerFailure`**: The function checks `isBreakerFailure(err)` which returns true for 5xx, timeouts, and transport errors. It returns false for 4xx and `context.Canceled`. The switch in `send` handles `context.Canceled` separately (calls `ReleaseProbe`), and the default case handles 4xx (calls `RecordSuccess`). This is correct.

    But there's a subtle issue: `isBreakerFailure` returns `false` for `context.Canceled`, so the `isBreakerFailure` case won't be hit for cancellation. The switch's `errors.Is(err, context.Canceled)` case handles it. Good.

    What about `context.DeadlineExceeded`? `isBreakerFailure` returns `true` for it (since it's not a `context.Canceled` and not an `HTTPStatusError`). So the `isBreakerFailure` case will be hit, and `RecordFailure` will be called. This is correct - a deadline exceeded is a provider health signal.

32. **`internal/metrics/metrics.go` - Gauge and Reset**: The `Reset` function now also clears gauges. Good.

33. **`internal/metrics/prometheus.go` - Gauge grouping**: The code groups gauges by family (stripping labels) using `metricFamily(k)`. Then it sorts the keys within each family. This matches the counter and histogram handling. Good.

34. **`internal/metrics/prometheus.go` - Gauge value formatting**: Uses `formatFloat(gauges[k].Value())`. The `formatFloat` function should handle float64 values. Good.

35. **`internal/circuitbreaker/breaker.go` - `transition` and metrics**: Every state transition pushes to the gauge. This includes transitions triggered by `Allow()` (via `refresh()` which calls `transition(StateHalfOpen)`). So when the cooldown elapses and someone calls `Allow()`, the gauge is updated to 2 (half-open). Good.

    But what if nobody calls `Allow()` or `State()` after the cooldown elapses? The gauge stays at 1 (open) even though the circuit is effectively half-open. This is a minor issue - the gauge could be stale. But it's a lazy transition design, and the spec doesn't require eager timer-based transitions.

36. **`internal/circuitbreaker/breaker.go` - `State()` method**: Calls `refresh()` which updates the state if cooldown elapsed. This means `State()` always returns the correct state. But the gauge might not be updated until `Allow()` or `State()` is called. This is a minor staleness issue for the gauge.

    Actually, `refresh()` calls `transition(StateHalfOpen)` which calls `setMetric()`. So if `State()` is called after the cooldown, the gauge is updated. But if nobody calls `State()` or `Allow()`, the gauge stays at 1. This is acceptable for a lazy design.

37. **`internal/fanout/engine_breaker_test.go` - `TestRecordAgentOutcome_CircuitOpenNotCountedAsAPICall`**: This test calls `recordAgentOutcome` directly with a `CircuitOpenError`. It checks that `atcr_api_calls_total` stays at 0 and `atcr_agents_failed` becomes 1. This is correct.

38. **`internal/llmclient/client_breaker_test.go` - `TestBreaker_RetriesThenTripsPerInvocation`**: This test verifies that with retries=2, 3 invocations result in 9 HTTP hits and the breaker trips. But wait - `WithRetry(2, ...)` means 2 retries = 3 total attempts. So 3 invocations × 3 attempts = 9 hits. The test expects `hits.Load() == 9`. Let me check: `New(WithHTTPClient(h), WithRetry(2, time.Millisecond, 1.5))`. The `WithRetry` signature would be `(retries, initialBackoff, factor)` or `(maxRetries, initialBackoff, factor)`. If it's "retries" (additional attempts), then 2 retries = 3 total. If it's "maxRetries" (total attempts), then 2 = 2 total. 

    Looking at the test name "RetriesThenTripsPerInvocation" and the comment "3 attempts/invocation", it seems like `WithRetry(2, ...)` means 2 additional retries = 3 total attempts. So 3 invocations × 3 attempts = 9 hits. The test expects 9. Good.

    But this is a potential confusion point. The `WithRetry` parameter naming should be clear. If it's ambiguous, it could lead to bugs. But this is pre-existing code, not new in this diff.

39. **`internal/circuitbreaker/breaker.go` - `State` comment**: The `String()` method comment says "this is for logs and the String() metric note." But there's no "String() metric note" in the code. This is a minor comment inaccuracy.

40. **`internal/circuitbreaker/breaker.go` - `Allow` method comment**: The comment says "A time-elapsed open circuit is first rolled forward to half-open so the cooldown is honoured lazily without a timer." This is correct - `refresh()` is called at the start of `Allow()`.

41. **`internal/circuitbreaker/breaker.go` - `RecordSuccess` in `StateOpen`**: The default case only resets `failureCount`. It doesn't clear `probeInFlight`. But if we're in `StateOpen`, `probeInFlight` should already be false (it was cleared when the circuit was opened or when a probe was released). Let me check: in `open()`, `probeInFlight` is not explicitly cleared. In `RecordFailure` for `StateHalfOpen`, it sets `b.probeInFlight = false` after calling `open()`. In `ReleaseProbe`, it sets `b.probeInFlight = false`. 

    When does `probeInFlight` get set to true? Only in `Allow()` when state is `StateHalfOpen` and no probe is in flight. When does it get set to false? In `RecordSuccess` for `StateHalfOpen`, in `RecordFailure` for `StateHalfOpen`, and in `ReleaseProbe`. 

    So if the circuit transitions from `StateHalfOpen` to `StateOpen` (via `RecordFailure`), `probeInFlight` is set to false. If it transitions from `StateHalfOpen` to `StateClosed` (via `RecordSuccess`), `probeInFlight` is set to false. If it transitions from `StateOpen` to `StateHalfOpen` (via `refresh()`), `probeInFlight` is NOT explicitly cleared. But it should already be false from the previous state.

    Wait, what if: circuit is Open, `refresh()` is called (cooldown elapsed), transitions to HalfOpen. At this point, `probeInFlight` is false (it was false when the circuit was Open). Then `Allow()` is called, sees HalfOpen and `probeInFlight == false`, sets `probeInFlight = true`, returns true. Good.

    So the state is consistent. Good.

42. **`internal/llmclient/client.go` - `send` and the `errors.Is(err, context.Canceled)` case**: This case calls `breaker.ReleaseProbe()`. But `ReleaseProbe` is a no-op outside of half-open. If the circuit is closed and the call is cancelled, `ReleaseProbe` does nothing. The `failureCount` is not reset. This is correct - a cancellation in the closed state shouldn't affect the failure run.

    But wait - if the circuit is closed and the call is cancelled, the switch hits the `context.Canceled` case and calls `ReleaseProbe`. It does NOT call `RecordFailure` (good) and does NOT call `RecordSuccess` (good). The breaker state is unchanged. This is correct.

    But what about `context.DeadlineExceeded`? It's not `context.Canceled`, so it hits the `isBreakerFailure` case (which returns `true` for it), and `RecordFailure` is called. This is correct - a timeout is a provider health signal.

43. **`internal/llmclient/client.go` - `send` and the default case for 4xx**: As discussed, 4xx calls `RecordSuccess` which resets the failure run. This is a design decision. The test `TestBreaker_4xxResetsFailureRun` confirms this.

    Potential issue: if a provider is misconfigured (wrong API key → 401), every call returns 401. The breaker stays closed forever. This is intentional per the spec ("4xx auth errors are permanent and provider-wide tripping would be incorrect"). So this is correct behavior, even if it seems counterintuitive.

44. **`internal/circuitbreaker/breaker.go` - `State.String()` comment**: "An out-of-range value renders 'unknown' rather than panicking." This is a good defensive practice. Good.

45. **`internal/metrics/metrics.go` - Gauge and atomic operations**: The gauge uses `atomic.Uint64` to store the float64 bits. This is lock-free and safe for concurrent use. Good.

46. **`internal/metrics/prometheus.go` - Gauge exposition**: The gauge output format is `<name> <value>\n` for unlabeled and `<name>{label="value"} <value>\n` for labeled. This matches Prometheus exposition format. Good.

47. **`internal/circuitbreaker/breaker.go` - `New` and metrics registration**: Every call to `New` registers a gauge for the provider. If a provider is never used (breaker created but no requests), the gauge shows 0 (closed). This is fine.

48. **`internal/circuitbreaker/registry.go` - `DefaultRegistry`**: It's a package-level variable. The test `TestDefaultRegistryUsable` uses it and cleans up with `Reset`. The `mockProvider` function in `review_test.go` also resets it. Good test isolation.

49. **`internal/fanout/review_test.go` - `mockProvider` reset**: The comment explains why the global registry needs per-test isolation. This is good defensive testing.

50. **`internal/circuitbreaker/breaker.go` - `RecordFailure` in `StateOpen`**: The default case is empty (no-op). This means a failure while open doesn't extend the cooldown. The test `TestOpenFailureDoesNotExtendCooldown` verifies this. Good.

Let me also check for potential issues with the `isBreakerFailure` function:

51. **`isBreakerFailure` and wrapped `context.Canceled`**: `errors.Is(err, context.Canceled)` returns true for `fmt.Errorf("x: %w", context.Canceled)`. The test `TestIsBreakerFailureClassification` confirms this. Good.

52. **`isBreakerFailure` and wrapped `HTTPStatusError`**: `errors.As(err, &he)` finds the `HTTPStatusError` in a wrapped chain. The test confirms this with `"wrapped 500"`. Good.

53. **`isBreakerFailure` and `context.DeadlineExceeded`**: It's not `context.Canceled` and not an `HTTPStatusError`, so it returns `true`. The test confirms this with `"deadline"`. Good.

54. **`isBreakerFailure` and generic errors**: A generic `errors.New("connection refused")` is not `context.Canceled` and not an `HTTPStatusError`, so it returns `true`. The test confirms this with `"transport"`. Good.

Now let me think about potential issues I might have missed:

55. **Lock ordering**: The breaker holds its mutex and then calls `setMetric` which calls `metrics.Gauge` which takes the metrics registry mutex. If another goroutine holds the metrics registry mutex and tries to call into the breaker... but metrics doesn't call into the breaker. So no cycle. Good.

56. **The `send` function in `llmclient/client.go`**: What happens if the context is cancelled while the breaker check is happening? The `Allow` call is synchronous and quick. The `dispatch` call checks the context. If cancelled before `dispatch`, `dispatch` returns `context.Canceled`. If cancelled during `dispatch`, the HTTP client returns an error (which might be `context.Canceled` or a transport error). The switch handles both cases. Good.

57. **The `fanout/engine.go` `invokeAgent`**: It calls `circuitbreaker.NewContext(ctx, a.Provider)`. If `a.Provider` is empty, the context carries an empty string. The `send` function in `llmclient` checks `if provider == ""` and bypasses the breaker. Good.

58. **The `fanout/review.go` `buildAgent`**: It sets `Provider: ac.Provider`. The `ac` is the agent config. If `ac.Provider` is empty, the agent has no provider, and the breaker is a no-op for that agent. This might be intentional for agents that don't use a provider (e.g., local tools). Good.

59. **The `verify/invoke.go` `buildSkepticAgent`**: Sets `Provider: c.Provider` from the skeptic config. Good.

60. **Potential issue: The `send` function calls `breaker.Allow()` which might transition the state from Open to HalfOpen (via `refresh()`). This transition updates the gauge. But the `Allow` method also might set `probeInFlight = true`. What if the caller never reports back (e.g., the dispatch panics)? The probe slot leaks. The comment in `Allow` warns about this: "A caller that wins the half-open probe MUST report the outcome exactly once via RecordSuccess, RecordFailure, or ReleaseProbe — otherwise the probe slot leaks and the circuit wedges half-open forever."

    This is a known design constraint. The `ReleaseProbe` method exists for cases like caller cancellation. But what about panics? If `dispatch` panics, the probe slot leaks. This could be a reliability concern, but it's a pre-existing design trade-off. Go's defer/recover could be used, but that's not implemented here.

61. **Potential issue: The `Registry.Get` method creates a new `Breaker` with `New()`, which calls `setMetric(StateClosed)`. This writes to the metrics registry. If the metrics registry is being read concurrently (e.g., for Prometheus exposition), the registry mutex is held briefly. No deadlock since the breaker doesn't hold its mutex during `New`. Good.

62. **Potential issue: The `fanout/metrics.go` `recordAgentOutcome` function now uses a switch statement. The default case sets `apiCalls = 1`. But what about other error types that should also result in 0 calls? For example, if a new error type is added in the future. The current code only handles `context.DeadlineExceeded`, `context.Canceled`, and `CircuitOpenError`. Other errors default to 1 call. This is correct for the current design.

63. **Potential issue: The `llmclient/client.go` `send` function has a default case that calls `RecordSuccess` for 4xx errors. But what about other non-HTTPStatusError, non-Canceled errors that are not transport failures? For example, a JSON parsing error. The current code would call `RecordSuccess` for it (since `isBreakerFailure` returns `true` for non-HTTPStatusError, non-Canceled errors, which means the `isBreakerFailure` case would be hit, not the default). Wait, let me re-read:

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

If `isBreakerFailure` returns `true` (for transport errors, timeouts, 5xx), `RecordFailure` is called. If it returns `false` (for `context.Canceled`), the next case is checked. If the error is `context.Canceled`, `ReleaseProbe` is called. Otherwise (4xx, or any other error that `isBreakerFailure` returns `false` for), the default case calls `RecordSuccess`.

Wait, `isBreakerFailure` returns `false` only for `nil` and `context.Canceled`. For all other errors (4xx HTTPStatusError, transport errors, timeouts, JSON errors, etc.), it returns `true`. So the `isBreakerFailure` case would be hit for almost everything except `context.Canceled`. The default case would only be hit for... nothing? Let me re-check.

`isBreakerFailure` logic:
- `err == nil` → `false`
- `errors.Is(err, context.Canceled)` → `false`
- `errors.As(err, &he)` → `he.Status >= 500` (so 5xx → `true`, 4xx → `false`)
- otherwise → `true`

So `isBreakerFailure` returns `false` for: nil, `context.Canceled`, and 4xx HTTPStatusError. It returns `true` for: 5xx HTTPStatusError, timeouts, transport errors, and any other non-HTTPStatusError error.

In the `send` switch:
- `err == nil` → `RecordSuccess` ✓
- `isBreakerFailure(err)` is `true` (5xx, timeout, transport) → `RecordFailure` ✓
- `isBreakerFailure(err)` is `false`, but `errors.Is(err, context.Canceled)` is `true` → `ReleaseProbe` ✓
- `isBreakerFailure(err)` is `false` (4xx), and not `context.Canceled` → default → `RecordSuccess` ✓

So the default case is only hit for 4xx errors. The logic is correct.

64. **Potential issue: What if `isBreakerFailure` returns `false` for some other error type in the future?** The default case would call `RecordSuccess`, which might not be appropriate. But this is a current-design concern, not a bug.

65. **Potential issue: The `send` function comment says "A non-tripping HTTP response (4xx incl. 429/401): the provider replied, so it is reachable. The breaker tracks outages, not auth/rate-limit correctness, so a reply counts as a healthy round-trip — which also closes a half-open probe instead of wedging it." This is a design decision. The behavior is tested in `TestBreaker_4xxResetsFailureRun`.

    However, there's a subtle issue: if a provider is consistently returning 4xx (e.g., a misconfigured API key), the breaker will never trip, and every agent will independently hit the provider and get a 4xx. This wastes time and API quota, which is exactly what the circuit breaker is supposed to prevent. The spec says "4xx auth errors are permanent and provider-wide tripping would be incorrect" - but "incorrect" here means "not what we want." The design is intentional.

    This is a design decision, not a bug. I'll note it as a potential concern but not a definite issue.

66. **Potential issue: The `breaker.New` function calls `setMetric(StateClosed)` which writes to the metrics registry. If the breaker is created in a test that hasn't initialized the metrics registry, this could panic. But `metrics.DefaultRegistry` is initialized at package load time. Good.

67. **Potential issue: The `Registry.Get` method calls `New()` which calls `setMetric()`. This happens while holding the registry's write lock. The `setMetric` call takes the metrics registry's lock. If another goroutine holds the metrics registry lock and tries to get the breaker... but metrics doesn't call into the registry. So no deadlock. Good.

68. **Potential issue: The `fanout/engine.go` `invokeAgent` function calls `circuitbreaker.NewContext(ctx, a.Provider)`. This creates a new context. The original `ctx` is not modified. This is correct - contexts are immutable in Go.

69. **Potential issue: The `fanout/engine_breaker_test.go` `TestInvokeSlot_CircuitOpenChainFailsAsFailed` test uses `f.failFor["primary"]` and `f.failFor["fb"]` to simulate failures. The `newFake()` function creates a `fakeCompleter`. The test expects `StatusFailed` when both primary and fallback fail with `CircuitOpenError`. This tests AC6.

70. **Potential issue: The `fanout/engine_breaker_test.go` `TestInvokeSlot_CircuitOpenTriggersFallback` test expects the fallback to answer when the primary's circuit is open. The fallback uses a different provider ("anthropic" vs "openai"). The test expects `StatusOK` and `FallbackUsed = true`. This is correct.

71. **Potential issue: The `internal/metrics/prometheus.go` gauge output format**: The code uses `formatFloat(gauges[k].Value())`. Let me check what `formatFloat` does for integer values. If the gauge value is 1 (an integer), `formatFloat` should output "1" (or "1.0" or "1e+00"). The test `TestWritePrometheusGauge` expects `atcr_circuit_breaker_state{provider="openai"} 1`. So `formatFloat(1.0)` should return "1". Good.

72. **Potential issue: The `internal/metrics/prometheus.go` gauge grouping**: The code groups gauges by family using `metricFamily(k)`. This function strips labels to get the base metric name.
</think>
