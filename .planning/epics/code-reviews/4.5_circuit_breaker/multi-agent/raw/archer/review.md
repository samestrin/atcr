Let me carefully review this diff against the sprint plan scope. The sprint is Epic 4.5: Circuit Breaker and Provider Health Tracking.

In-scope files:
- `internal/circuitbreaker` package (breaker.go, registry.go, context.go, tests)
- `internal/llmclient/client.go` integration
- `internal/fanout/engine.go` (CircuitOpenError as permanent failure)
- `internal/fanout/metrics.go` (recordAgentOutcome)
- `internal/fanout/review.go` (buildAgent, buildFallbackAgent)
- `internal/verify/invoke.go` (provider threading)
- `internal/metrics` (gauge primitive, names)
- docs/metrics.md
- boundaries_test.go (allowed imports)

Let me go through each file carefully.

### internal/circuitbreaker/breaker.go

Let me look at the logic carefully.

1. `Allow()` - returns true if closed, or half-open with no probe in flight. Sets probeInFlight=true when returning true in half-open.

2. `RecordSuccess()` - in half-open, transitions to closed, resets failureCount, clears probe. In closed/open, resets failureCount.

Wait, there's a subtle issue. In `RecordSuccess()`, when state is `StateOpen` (a pre-trip in-flight call returning success), it resets failureCount to 0. But the comment says "A success observed while open (an in-flight call that started before the trip) only clears the failure run." That's fine.

But wait — what about the probe? If a call was allowed when the circuit was closed (not a probe), and then the circuit opened due to other failures, and then this call succeeds... the probeInFlight wasn't set by this call. So we don't need to clear it. OK.

Actually, let me think about a race. Consider:
- Call A: Allow() returns true (closed). probeInFlight stays false.
- Call B: RecordFailure() × 3 → circuit opens.
- Call A: RecordSuccess() while state is open → resets failureCount (already 0 from open()), does nothing to probeInFlight (stays false). OK.

3. `RecordFailure()` - in closed, increments failureCount, opens at threshold. In half-open, opens and clears probe. In open, ignored.

4. `ReleaseProbe()` - clears probeInFlight. Used for cancellation.

Now, there's a potential issue with `RecordSuccess` when called from a non-probe call while in half-open. Let me think...

Actually, the design is: only the probe caller (the one that won Allow() in half-open) should call RecordSuccess/RecordFailure/ReleaseProbe. But what if a closed-state call's RecordSuccess comes in while state is half-open?

Scenario:
- Circuit closed. Call A: Allow() → true (closed, not a probe).
- Failures happen, circuit opens.
- Cooldown elapses, circuit → half-open (via refresh on next Allow/State).
- Call B: Allow() → true (half-open, becomes probe, probeInFlight=true).
- Call A completes: RecordSuccess() → state is half-open → transitions to closed, clears probeInFlight.

Hmm, this is a race. Call A was not the probe, but its success closes the circuit and clears the probe slot. Call B (the actual probe) is still in flight. When Call B completes and calls RecordSuccess(), state is now closed → just resets failureCount. That's... actually OK-ish? The circuit closes based on Call A's success. But Call A started before the circuit opened, so its success might be stale. But the comment says "A success observed while open... only clears the failure run; the cooldown-driven half-open probe still governs reopening." But here the success is observed while HALF-OPEN, not open. So it closes the circuit.

Actually this is a real edge case but probably acceptable — a success is a success. The design assumes the probe is the only one calling RecordSuccess in half-open, but that's not strictly enforced. This is a minor correctness concern but probably not worth flagging as it's an inherent property of the design and the comment acknowledges in-flight calls.

Let me look more carefully at actual bugs.

Looking at `isBreakerFailure`:
```go
func isBreakerFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var he *HTTPStatusError
	if errors.As(err, &he) {
		return he.Status >= 500
	}
	return true
}
```

This returns true for `context.DeadlineExceeded`. Is that intended? The comment says "a 5xx response, a timeout, or a connection-level transport error". A deadline exceeded is a timeout. The test confirms `{"deadline", context.DeadlineExceeded, true}`. OK, that's intended.

But wait — what about a caller-set deadline that's very short? That would count as a breaker failure. The sprint plan says "timeouts" count. A context.DeadlineExceeded could be a caller-imposed deadline, not a server timeout. But the design treats them the same. This is probably acceptable per the sprint plan.

Now, there's a subtle issue: in `send()`:
```go
case errors.Is(err, context.Canceled):
    breaker.ReleaseProbe()
```

But `isBreakerFailure` already returns false for `context.Canceled`. So the switch order matters. Let me check:
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

Since `isBreakerFailure(context.Canceled)` is false, the canceled case is reached. OK.

But what about `context.DeadlineExceeded`? `isBreakerFailure` returns true for it, so it goes to RecordFailure. That's intended (timeout).

Now, what about a wrapped `context.Canceled` inside another error? `errors.Is(err, context.Canceled)` would still match if it's wrapped. But `isBreakerFailure` checks `errors.Is(err, context.Canceled)` too and returns false. So wrapped canceled → ReleaseProbe. OK.

Hmm, but what if err is a wrapped combination — e.g., a transport error that also wraps context.Canceled? Like `fmt.Errorf("dial: %w", context.Canceled)` — unlikely but possible. Then isBreakerFailure returns false (Canceled), and ReleaseProbe is called. That seems fine.

Now let me look at the `default` branch in `send()`:
```go
default:
    // A non-tripping HTTP response (4xx incl. 429/401): the provider replied,
    // so it is reachable. ... counts as a healthy round-trip — which also
    // closes a half-open probe instead of wedging it.
    breaker.RecordSuccess()
```

So a 4xx calls RecordSuccess. This means a 429 resets the failure run. The test `TestBreaker_4xxResetsFailureRun` confirms this. Per AC10, 4xx doesn't trip the breaker. But does it reset the failure run? The sprint plan says "4xx errors — including 429 rate-limits — are NOT counted as breaker failures". It doesn't explicitly say they reset the run. But treating a 4xx as a healthy round-trip (resetting the run) is a design choice. Is it correct?

Hmm. Consider: provider returns 500, 500, 429, 500, 500, 500. With reset-on-4xx: the run after the 429 is 3 → opens. Without reset: 5 failures total but the run... actually the breaker counts CONSECUTIVE failures. A 429 between 500s — is the provider "back"? A 429 means the server is up but rate-limiting. One could argue either way. The design chose reset. This is a judgment call, probably acceptable. Not flagging.

Actually wait, let me reconsider. The sprint plan AC10 says 429 "must not open the breaker until that epic's retries are exhausted". Resetting the failure run on 429 means a provider that's returning 500s interspersed with 429s would never trip. Is that a problem? If a provider is returning 500s, it's failing. A 429 in between doesn't mean it recovered. But this is a design decision and the test locks it. I'll note it as a possible correctness concern but it's borderline. Actually, the comment explicitly justifies it: "the provider replied, so it is reachable." I'll leave it — it's a defensible design choice and within scope of the epic's intent.

Let me look for actual bugs now.

### Registry Get - potential issue

```go
func (r *Registry) Get(provider string) *Breaker {
	r.mu.RLock()
	b, ok := r.breakers[provider]
	r.mu.RUnlock()
	if ok {
		return b
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.breakers[provider]; ok {
		return b
	}
	b = New(provider, DefaultThreshold, DefaultCooldown)
	r.breakers[provider] = b
	return b
}
```

This is the standard double-checked locking pattern. Looks correct.

### breaker.go - `open()` resets failureCount

```go
func (b *Breaker) open() {
	b.transition(StateOpen)
	b.openedAt = b.now()
	b.failureCount = 0
}
```

When opening from closed (via RecordFailure reaching threshold), failureCount is reset to 0. Then in half-open, on failure, `open()` is called again, resetting failureCount (already 0). OK.

But wait — when the circuit is half-open and a failure reopens it, the cooldown is restarted (openedAt = now). That matches AC5. Good.

### Potential issue: `RecordSuccess` in half-open doesn't reset `openedAt`

Not needed — closing the circuit. OK.

### Let me check the `send` function's handling when provider is empty

```go
provider := circuitbreaker.ProviderFromContext(ctx)
if provider == "" {
    return c.dispatch(ctx, endpoint, key, body)
}
```

Good — no-op for doctor.

### Metrics gauge - is there a thread-safety issue?

The gauge uses atomic uint64 for the value. Set and Value are lock-free. The Registry's Gauge() method uses a mutex to create-or-get. Once you have a *gauge, Set/Value are atomic. That's fine.

But in `breaker.go`, `setMetric` is called while holding `b.mu`:
```go
func (b *Breaker) setMetric(s State) {
	metrics.Gauge(metrics.Key(...)).Set(float64(s))
}
```

`metrics.Gauge` calls `DefaultRegistry.Gauge(name)` which takes the registry mutex. `b.mu` is held. Could there be a lock-ordering issue? The comment says "metrics never calls back into this package, so there is no lock-ordering cycle." That's true. OK.

### Now let me look at the fanout engine integration

```go
ctx = circuitbreaker.NewContext(ctx, a.Provider)
```

This is in `invokeAgent`. Good — one chokepoint.

### fanout/metrics.go - recordAgentOutcome

```go
var coe *llmclient.CircuitOpenError
switch {
case errors.Is(r.Err, context.DeadlineExceeded) || errors.Is(r.Err, context.Canceled):
    apiCalls = 0
case errors.As(r.Err, &coe):
    apiCalls = 0
default:
    apiCalls = 1
}
```

This looks correct. A CircuitOpenError means no HTTP call was made.

### Let me check the `Agent` struct field alignment

```go
type Agent struct {
	Name     string
	Provider string
	Invocation  llmclient.Invocation
	Prompt      string
	PayloadMode string
	...
}
```

The gofmt alignment looks a bit off — `Name` and `Provider` are aligned together, then `Invocation` etc. are aligned together. This is actually fine for gofmt — adjacent groups. Not a real issue.

### Let me look for actual bugs more carefully.

Looking at `breaker.go` line by line again...

In `Allow()`:
```go
case StateHalfOpen:
    if b.probeInFlight {
        return false
    }
    b.probeInFlight = true
    return true
```

OK.

In `RecordSuccess()`:
```go
case StateHalfOpen:
    b.transition(StateClosed)
    b.failureCount = 0
    b.probeInFlight = false
```

OK.

In `RecordFailure()`:
```go
case StateHalfOpen:
    b.open()
    b.probeInFlight = false
```

`open()` calls `transition(StateOpen)` which sets state and metric, then sets openedAt and failureCount=0. Then probeInFlight=false. OK.

Wait — `open()` sets `b.failureCount = 0`. But in half-open, failureCount was already 0 (reset when opened from closed). So no issue.

### Now, a subtle issue: what if `RecordSuccess` is called when state is `StateOpen`?

```go
default: // StateClosed, StateOpen
    b.failureCount = 0
```

It resets failureCount but doesn't change state. The comment says this is for a pre-trip in-flight call. But it doesn't clear probeInFlight. That's fine because probeInFlight should be false when open (it's only set in half-open Allow, and cleared on RecordSuccess/RecordFailure/ReleaseProbe).

Actually wait — is there a scenario where probeInFlight is true while state is open? Let me think:
- Half-open, probe admitted (probeInFlight=true).
- RecordFailure → open(), probeInFlight=false. OK.
- RecordSuccess → closed, probeInFlight=false. OK.
- ReleaseProbe → probeInFlight=false. OK.

So when transitioning out of half-open, probeInFlight is always cleared. When in open state, probeInFlight is false. Good.

But what about: half-open, probe admitted. Then `refresh()` is called... no, refresh only transitions open→half-open, not half-open→anything. OK.

### Let me check the `State()` method

```go
func (b *Breaker) State() State {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.refresh()
    return b.state
}
```

`refresh()` transitions open→half-open if cooldown elapsed. But it doesn't set probeInFlight (already false). OK.

### Now let me look at a potential issue with the `default` case in `send()` calling RecordSuccess for 4xx.

Actually, I realize there might be a real issue here. Let me re-read AC10:

"4xx responses do NOT trip the circuit: 429 rate-limits are owned by Epic 4.6 (backoff + Retry-After) and must not open the breaker until that epic's retries are exhausted"

The current implementation treats 4xx as RecordSuccess (resets the failure run). But AC10 says 4xx "must not open the breaker until that epic's retries are exhausted". If a 429 resets the failure run, then during a period of intermittent 500s and 429s, the breaker might never open. Is that a violation?

Hmm. The sprint plan's clarification says: "ALL 4xx — including 429 and 401 — stay closed (a 4xx means the server replied; categorically distinct from a transport-layer failure where no response arrived)." It says "stay closed" — meaning don't trip. It doesn't explicitly say "reset the failure run." But treating a 4xx as a healthy round-trip (resetting the run) is the implementation's choice. The test `TestBreaker_4xxResetsFailureRun` locks this behavior.

Is this a correctness issue? Consider: a provider is returning 500, 500, 429, 500, 500, 500. With reset: after the 429, the run resets, so it takes 3 more 500s to open. Without reset: 500, 500 (run=2), 429 (no-op, run stays 2), 500 (run=3 → open). So without reset, it opens after 4 calls; with reset, after 6.

The epic's success criterion is "Provider outages are detected within 3 API calls." If 429s are interspersed, it takes more than 3. But 429s mean the provider is up (rate-limiting), so maybe it's not a full outage. This is a judgment call.

Actually, I think this is a legitimate design decision and the comment justifies it. I won't flag it. The test locks the behavior, and it's defensible.

### Let me look at the `dispatch` rename

The old `send` is renamed to `dispatch`, and a new `send` wraps it. Let me check that all callers of `send` still work. The diff shows `send` is called by Complete and Chat (mentioned in comments). Since `send` now wraps `dispatch`, and the signature is the same, callers still call `send`. Good.

### Let me check for any issue with the `CircuitOpenError` type

```go
type CircuitOpenError struct {
    Provider string
}
```

It's a struct, not a pointer. The fanout checks `errors.As(r.Err, &coe)` where `coe` is `*llmclient.CircuitOpenError`. The error returned is `&CircuitOpenError{...}` (pointer). `errors.As` works with pointer-to-pointer. OK.

But wait — does `CircuitOpenError` implement `Unwrap` or `Is`? No. And `errors.Is(err, context.Canceled)` won't match it. Good.

Is `CircuitOpenError` a permanent failure in the fanout engine? The sprint plan says "the engine treats it as a permanent failure (not retryable)". Let me check how the fanout classifies it. The test `TestInvokeSlot_CircuitOpenTriggersFallback` confirms it triggers fallback. And `TestInvokeSlot_CircuitOpenChainFailsAsFailed` confirms it's StatusFailed, not StatusTimeout. 

But wait — how does the engine classify CircuitOpenError as StatusFailed vs StatusTimeout? Let me look at the engine code... it's not in the diff. The diff only shows the test. The engine's classification logic must already handle it (any non-timeout error is StatusFailed). The comment in metrics.go says "a CircuitOpenError from the primary is a permanent (non-timeout) failure." Since CircuitOpenError is not context.DeadlineExceeded, it classifies as StatusFailed. That relies on existing classification logic. The test confirms it works. OK.

### Let me look at potential issues in the metrics gauge exposition

In `prometheus.go`:
```go
gaugeFamilies := make(map[string][]string, len(gauges))
for k := range gauges {
    f := metricFamily(k)
    gaugeFamilies[f] = append(gaugeFamilies[f], k)
}
for _, fam := range sortedKeys(gaugeFamilies) {
    fmt.Fprintf(&b, "# TYPE %s gauge\n", fam)
    keys := gaugeFamilies[fam]
    sort.Strings(keys)
    for _, k := range keys {
        fmt.Fprintf(&b, "%s %s\n", k, formatFloat(gauges[k].Value()))
    }
}
```

This looks correct. One TYPE header per family, sorted keys. Good.

### Now, let me look at a potential real issue.

In `breaker.go`, the `New` function:
```go
func New(provider string, threshold int, cooldown time.Duration) *Breaker {
    b := &Breaker{
        provider:  provider,
        state:     StateClosed,
        threshold: threshold,
        cooldown:  cooldown,
        now:       time.Now,
    }
    b.setMetric(StateClosed)
    return b
}
```

`setMetric` is called without holding `b.mu`. But at construction, no one else has a reference to `b` yet, so it's safe. But `setMetric` calls `metrics.Gauge(...).Set(...)` which is fine without the breaker mutex (the gauge has its own atomic). The breaker mutex isn't needed for setMetric itself. The comment says "Caller must hold b.mu" for setMetric, but in `New` it's called without the lock. This is technically a violation of the stated contract but not a real bug (no concurrency at construction). Minor.

Actually, is there a real concern? `setMetric` reads `b.provider` (immutable after construction) and calls metrics. No state read that requires the lock. The "caller must hold b.mu" comment is overly conservative. Not a real issue.

### Let me look at the test file `breaker_test.go` for the `newTestBreaker` function

```go
func newTestBreaker(t *testing.T, threshold int, cooldown time.Duration) (*Breaker, *clock) {
    t.Helper()
    metrics.DefaultRegistry.Reset()
    t.Cleanup(metrics.DefaultRegistry.Reset)
    clk := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
    b := New("openai", threshold, cooldown)
    b.now = clk.now
    return b, clk
}
```

`b.now = clk.now` is set AFTER `New`. But `New` calls `setMetric` which doesn't use `now`. And `b.now` is only used in `refresh()` and `open()`. Since no transitions happen between `New` and the assignment, this is fine. But it's a bit ugly — the clock is injected after construction. Not a bug.

### Let me look for a real correctness issue...

Hmm, let me think about the half-open probe and `RecordSuccess` from a non-probe call again.

Actually, I realize there's a more subtle issue. In `send()`:

```go
breaker := circuitbreaker.DefaultRegistry.Get(provider)
if !breaker.Allow() {
    return nil, &CircuitOpenError{Provider: provider}
}
raw, err := c.dispatch(ctx, endpoint, key, body)
switch {
case err == nil:
    breaker.RecordSuccess()
...
```

The `Allow()` and `RecordSuccess/RecordFailure/ReleaseProbe` are NOT atomic with respect to each other. Between `Allow()` returning true and the Record call, the state could have changed (another goroutine opened the circuit, etc.). But that's inherent to the circuit breaker pattern — the breaker tracks outcomes, not in-flight calls. The probe gating is the only concurrency-sensitive part, and it's handled by `probeInFlight`. OK.

But here's a real concern: what if `Allow()` returns true in the closed state (not a probe), and then by the time the call completes, the circuit is half-open (due to other failures + cooldown)? Then `RecordSuccess()` would close the circuit, treating this non-probe call as the probe verdict. This is the race I mentioned earlier. 

Is this a real problem? The call DID succeed, so closing the circuit is reasonable. The call started when the circuit was healthy (closed), and it succeeded. The fact that other calls failed in the meantime doesn't invalidate this success. Closing the circuit on a success is the right call. So this is actually correct behavior, not a bug.

### Let me look at the `isBreakerFailure` function more carefully for the `context.DeadlineExceeded` case.

`context.DeadlineExceeded` → `isBreakerFailure` returns true → `RecordFailure()`. 

But what if the deadline was set by the CALLER (e.g., the agent's timeout), not the server? A caller-imposed deadline expiring doesn't mean the provider is down — it means the call took too long, which COULD be a provider issue (slow) or a caller issue (tight deadline). The sprint plan says "timeouts" count. The test confirms `context.DeadlineExceeded` → true. This is intended.

But wait — there's a subtlety. The `dispatch` function has its own retry logic with timeouts. If the CLIENT's timeout (from the agent config) fires, that's `context.DeadlineExceeded`. If the HTTP client's timeout fires, that might be a different error (e.g., `*url.Error` wrapping `context.DeadlineExceeded`). `errors.Is(err, context.DeadlineExceeded)` would still match via `errors.Is`. And `isBreakerFailure` returns true for it. OK, intended.

### Let me look at whether there's a missing `errors.Is(err, context.Canceled)` check that could be an issue.

In `isBreakerFailure`:
```go
if errors.Is(err, context.Canceled) {
    return false
}
```

This catches wrapped cancellations. Good. But what if the error is a transport error that occurred because the context was canceled (e.g., `*url.Error{Err: context.Canceled}`)? `errors.Is` unwraps, so it would match `context.Canceled` and return false. Good — a cancellation-induced transport error is still a cancellation.

### Now let me look at the fanout review_test.go change

```go
circuitbreaker.DefaultRegistry.Reset()
t.Cleanup(circuitbreaker.DefaultRegistry.Reset)
```

This resets the global registry before each test that uses `mockProvider`. Good for isolation. But is it in `mockProvider` which is called by many tests? Yes. So every test that uses mockProvider resets. Good.

### Let me look at the `verify/invoke.go` change

```go
Provider: c.Provider,
```

This threads the provider from the skeptic config. Good — in scope per the sprint plan.

### Let me look at `boundaries_test.go`

The allowed imports are updated:
- `circuitbreaker`: `{"metrics"}` — circuitbreaker imports metrics. Correct.
- `llmclient`: adds `circuitbreaker`. Correct.
- `fanout`: adds `circuitbreaker`. Correct.

But wait — `verify` is NOT updated to add `circuitbreaker`. Let me check: verify's allowed imports are `{"reconcile", "stream", "registry", "fanout", "payload", "tools", "llmclient", "atomicfs", "log"}`. The verify change only adds `Provider: c.Provider` to a fanout.Agent struct — it doesn't import circuitbreaker. It accesses `c.Provider` (from skeptic.Config) and passes it to fanout.Agent. So verify doesn't need to import circuitbreaker. Correct.

### Now let me look for any actual bugs I might have missed.

Let me re-examine the `send` function's switch statement:

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

The `default` case handles 4xx errors (non-tripping HTTP responses). It calls `RecordSuccess()`. But wait — is a 4xx really a "success"? The comment says "the provider replied, so it is reachable." But `RecordSuccess()` in the half-open state CLOSES the circuit. So a 429 in half-open would close the circuit. Is that correct?

Consider: circuit is half-open (provider was down). Probe is admitted. Provider returns 429 (rate-limited). Should the circuit close? The provider IS reachable (it responded), but it's rate-limiting. Closing the circuit means all agents resume calling it, and they'll all get 429s. Epic 4.6 handles 429 backoff. So closing the circuit on a 429 in half-open means "provider is back up, resume calls, let 4.6 handle rate-limiting." That's... defensible. The provider is reachable. The sprint plan says 429s "must not open the breaker" — closing on a 429 doesn't open it. And the comment explicitly says this closes the half-open probe "instead of wedging it." OK, defensible.

But actually, is this the RIGHT behavior? If the provider was down (returning 500s or transport errors), and now it's returning 429s, it HAS recovered (it's up and responding, just rate-limiting). So closing the circuit is correct. Good.

### Let me check if there's an issue with the `CircuitOpenError` not being recognized by the retry logic in `dispatch`.

Wait — `CircuitOpenError` is returned by `send`, not `dispatch`. The `send` function checks the breaker BEFORE calling `dispatch`. So `dispatch` never sees `CircuitOpenError`. And `send` returns `CircuitOpenError` directly without calling `dispatch`. Good.

But does the retry logic in `dispatch` try to retry a `CircuitOpenError`? No — `dispatch` is only called when the breaker allows. `CircuitOpenError` is returned before `dispatch` is called. Good.

### Let me look at whether `Complete` or `Chat` call `send` or `dispatch`.

The diff shows `send` is renamed to `dispatch`, and a new `send` wraps it. The callers (Complete, Chat) call `send`. Let me verify... the diff doesn't show the Complete/Chat bodies, but the comment says "It is shared by Complete (single message) and Chat (multi-turn with tools)." So they call `send`. Since `send` now wraps `dispatch` with the breaker, the breaker is engaged for both. Good.

### Let me look for a potential issue: does the breaker interact correctly with the retry loop?

The `send` function calls `Allow()` once, then calls `dispatch` (which does retries), then records the outcome once. So the breaker records ONE outcome per `send` call, regardless of how many HTTP attempts `dispatch` made. The test `TestBreaker_RetriesThenTripsPerInvocation` confirms this: 3 invocations × 3 attempts = 9 HTTP hits, but the breaker trips after 3 invocations (not 3 attempts). This matches the sprint plan's design: "The breaker records one outcome per agent invocation (after that invocation's own retry budget)."

But wait — is this the right granularity? If each invocation retries 3 times, and the breaker needs 3 failures to open, that's 9 HTTP calls before the breaker opens. The sprint plan's success criterion says "Provider outages are detected within 3 API calls." But with retries, it's 3 invocations × 3 attempts = 9 API calls. Hmm.

Actually, re-reading the sprint plan: "After 3 consecutive API failures for a provider, the circuit opens." And "Provider outages are detected within 3 API calls." But the implementation records per-invocation (post-retry), not per-API-call. So 3 invocations with 3 retries each = 9 API calls before opening. Is this a violation of AC1?

AC1 says "After 3 consecutive API failures for a provider, the circuit opens." The implementation opens after 3 consecutive INVOCATION failures (post-retry). Each invocation is 3 API attempts. So it's 9 API failures, not 3.

Hmm, but the sprint plan's clarification says: "The 3-attempt per-call retry budget + conservative 3-consecutive-failure threshold absorb transient blips." And the test `TestBreaker_RetriesThenTripsPerInvocation` explicitly tests this behavior and the comment says "the breaker records one outcome per agent invocation (after that invocation's own retry budget), not per HTTP attempt."

So this is an intentional design decision documented in the sprint plan's clarifications. The breaker records per-invocation, not per-HTTP-attempt. This means "3 API calls" in the success criterion really means "3 invocations." It's a defensible interpretation — the retry budget absorbs transient blips, and 3 consecutive failed invocations (each exhausting retries) is a strong signal of outage.

I won't flag this as it's an explicit design decision.

### Let me look for any real bug...

Actually, let me look at the `ReleaseProbe` function more carefully:

```go
func (b *Breaker) ReleaseProbe() {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.probeInFlight = false
}
```

This unconditionally sets `probeInFlight = false`. But what if the state is NOT half-open? The comment says "Outside half-open it is a no-op (there is no probe to release)." But it still sets `probeInFlight = false`. If `probeInFlight` was already false (which it should be outside half-open), this is a no-op. But what if somehow `probeInFlight` is true while closed? That shouldn't happen given the state machine. So it's fine.

But wait — there's a subtle issue. If `ReleaseProbe` is called when the state is `StateClosed` (e.g., a call was allowed when closed, then the circuit opened and closed again, and now the original call cancels), it sets `probeInFlight = false`. But `probeInFlight` should already be false in closed state. So no harm.

What if `ReleaseProbe` is called when the state is `StateOpen`? Same — `probeInFlight` is false. No harm.

What if `ReleaseProbe` is called when the state is `StateHalfOpen` but the caller was NOT the probe (the caller was allowed when closed, and the circuit transitioned to half-open)? Then `probeInFlight` might be true (set by the actual probe caller) or false. If the actual probe is still in flight, `ReleaseProbe` from a non-probe caller would clear `probeInFlight`, allowing another probe. That's a bug!

Wait, let me think about this more carefully.

Scenario:
1. Circuit closed. Call A: `Allow()` → true (closed, probeInFlight stays false).
2. Call B: `Allow()` → true (closed).
3. Call B fails. RecordFailure × 3 → circuit opens.
4. Cooldown elapses. Circuit → half-open (via refresh).
5. Call C: `Allow()` → true (half-open, probeInFlight = true). Call C is the probe.
6. Call A cancels (context.Canceled). `send` calls `ReleaseProbe()`.
7. `ReleaseProbe` sets `probeInFlight = false`.
8. Call D: `Allow()` → true (half-open, probeInFlight was false, now true). Call D is ALSO a probe!
9. Now two probes are in flight. Call C and Call D both complete.
10. Call C: RecordSuccess → closes circuit, probeInFlight = false.
11. Call D: RecordSuccess → state is closed, resets failureCount. No harm? Or Call D: RecordFailure → state is closed, increments failureCount. 

Hmm, this is a real concurrency issue. `ReleaseProbe` from a non-probe caller can clear the probe slot, allowing a second probe. But is this scenario realistic?

Call A was allowed when the circuit was closed. For Call A to call `ReleaseProbe`, it must have gotten a `context.Canceled` error. The circuit must have transitioned to half-open while Call A was in flight. This requires: Call A started (closed), other calls failed and opened the circuit, cooldown elapsed (60s), Call A is still in flight (canceled). 

Actually, if Call A is canceled, `dispatch` returns quickly (context canceled). So Call A's `ReleaseProbe` is called shortly after cancellation. For the circuit to be half-open at that point, 60s must have elapsed since the circuit opened. If Call A was in flight for 60+ seconds before being canceled, this is possible but unlikely (most calls have timeouts < 60s).

But even if it happens, the consequence is: two probes are admitted instead of one. Both complete and record outcomes. The worst case is:
- Probe 1 (Call C) succeeds → closes circuit.
- Probe 2 (Call D) fails → state is closed, RecordFailure increments failureCount to 1. Not a big deal — it's just one failure.

Or:
- Probe 1 (Call C) fails → reopens circuit.
- Probe 2 (Call D) succeeds → state is open, RecordSuccess resets failureCount (no-op since already 0). Circuit stays open. Not ideal but not harmful.

So the consequence is minor — at most an extra probe is admitted. The circuit still converges to the correct state. This is a minor correctness issue, not a showstopper.

Actually, wait. Let me reconsider. The real issue is that `ReleaseProbe` doesn't check whether the caller is actually the probe. It blindly clears `probeInFlight`. A non-probe caller calling `ReleaseProbe` could clear a real probe's slot.

But in the `send` function, `ReleaseProbe` is only called when `errors.Is(err, context.Canceled)`. And the caller only calls `ReleaseProbe` if it was allowed (Allow returned true). If the caller was allowed when closed, it's not a probe. If it was allowed when half-open, it IS the probe.

The issue is: the caller doesn't know whether it's the probe. It just calls `ReleaseProbe` on cancellation. If the circuit transitioned from closed to half-open while the call was in flight, the caller might clear someone else's probe slot.

This is a real but minor issue. The probability is low (requires 60s+ in-flight call that gets canceled), and the consequence is minor (an extra probe admitted). I could flag it as a LOW correctness issue.

Actually, let me reconsider the severity. The sprint plan says "Half-open allows a SINGLE in-flight probe; concurrent agents fail fast until it resolves." If `ReleaseProbe` from a non-probe clears the slot, this invariant is violated — two probes can be in flight. But the consequence is bounded (one extra probe), and the circuit still converges. I'll flag it as LOW