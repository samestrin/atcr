We are given a diff and a sprint plan. We must review the diff for issues that are in scope (related to the sprint plan) and output findings in TD_STREAM format.

The sprint plan is about implementing a circuit breaker for provider health tracking (Epic 4.5). The in-scope files and areas are:

- `internal/circuitbreaker` package (Breaker and Registry)
- Circuit breaker integration in `internal/llmclient/client.go`
- `CircuitOpenError` type for fail-fast signaling
- Fanout engine treats `CircuitOpenError` as permanent failure (triggers fallback)
- Metrics: `atcr_circuit_breaker_state` gauge (labels: provider, state)
- Unit tests for breaker state transitions
- Integration test: simulate provider failure, verify circuit opens

We are to ignore any issues that are not in the above scope.

Let's go through the diff and look for issues in the in-scope files.

We'll break down the diff by file:

1. `docs/metrics.md`: This is documentation. We are to check if it correctly documents the new metric. However, note that the sprint plan does not explicitly mention documentation, but the metric is in scope. We should check for correctness.

2. `internal/boundaries_test.go`: This file updates the allowed internal imports. We see additions for "circuitbreaker" in several places. This is in scope because it relates to the new package.

3. `internal/circuitbreaker/breaker.go`: New file - the core circuit breaker implementation.

4. `internal/circuitbreaker/breaker_test.go`: New file - tests for the breaker.

5. `internal/circuitbreaker/context.go`: New file - context helpers for provider.

6. `internal/circuitbreaker/registry.go`: New file - registry for breakers.

7. `internal/circuitbreaker/registry_test.go`: New file - tests for registry.

8. `internal/fanout/engine.go`: Modified to import circuitbreaker and add Provider field to Agent, and to thread provider via context.

9. `internal/fanout/engine_breaker_test.go`: New file - tests for circuit breaker integration in fanout.

10. `internal/fanout/metrics.go`: Modified to handle CircuitOpenError in recording agent outcome (not counting as API call).

11. `internal/fanout/review.go`: Modified to set Provider field when building agents.

12. `internal/fanout/review_test.go`: Modified to reset circuit breaker registry in tests.

13. `internal/llmclient/client.go`: Modified to integrate circuit breaker (check Allow, record outcomes, etc.)

14. `internal/llmclient/client_breaker_test.go`: New file - tests for circuit breaker in llmclient.

15. `internal/metrics/metrics.go`: Modified to add gauge type and related functions.

16. `internal/metrics/metrics_test.go`: Modified to add tests for gauge.

17. `internal/metrics/names.go`: Modified to add constants for the new metric and label.

18. `internal/metrics/prometheus.go`: Modified to handle gauges in Prometheus exposition.

19. `internal/metrics/prometheus_test.go`: Modified to add tests for gauge exposition.

20. `internal/verify/invoke.go`: Modified to set Provider field when building skeptic agent.

Now, we must look for issues (bugs, violations of best practices, etc.) in these changes that are in scope.

We'll go file by file and note any issues.

However, note: the problem says to output ONLY the findings in the TD_STREAM format. We must not output anything else.

Let's start by examining the changes for potential issues.

We are looking for:
- Security vulnerabilities
- Error handling issues
- Performance issues
- Correctness issues
- Maintainability issues

But note: we are only to report issues that are in the scope of the sprint plan (the circuit breaker work).

We'll now look at each changed file in the diff and note any issues.

Due to the large number of files, we'll focus on the most critical parts and look for obvious mistakes.

Let's begin:

### `docs/metrics.md`
We added a section for circuit breaker metric. The description says:
```
| `atcr_circuit_breaker_state{provider}` | gauge | Per-provider circuit state: `0` closed (normal), `1` open (failing fast), `2` half-open (probing recovery). A provider trips open after 3 consecutive breaker-failures (5xx, timeout, or connection-level transport error; 4xx including 429/401 never count) and returns to half-open after a 60s cooldown. |
```

This seems correct and matches the sprint plan. No issue found.

### `internal/boundaries_test.go`
We are adding "circuitbreaker" to the allowed internal imports for several packages. This is necessary for the new package. However, we must check if the imports are correct.

Specifically, we see:
- For "circuitbreaker": {"metrics"} -> This is correct because the breaker uses metrics to set the gauge.
- For "llmclient": {"registry", "errors", "circuitbreaker"} -> Correct, because llmclient uses circuitbreaker.
- For "fanout": {"llmclient", "registry", "stream", "payload", "tools", "log", "metrics", "circuitbreaker"} -> Correct, because fanout now uses circuitbreaker (for context and breaker via llmclient? Actually, note: in fanout/engine.go we import circuitbreaker and use it to set context. Also, the llmclient uses circuitbreaker. So fanout indirectly uses circuitbreaker via llmclient, but we also have a direct use in engine.go for NewContext. So it's correct.

No issue found.

### `internal/circuitbreaker/breaker.go`
This is the core implementation. Let's look for issues.

We note:
- The `Breaker` struct has a `probeInFlight` field to gate half-open to a single probe.
- The `Allow` method: 
   - If closed: return true.
   - If half-open: if probeInFlight is true, return false; else set probeInFlight to true and return true.
   - If open: return false.
- The `RecordSuccess` method: 
   - If half-open: transition to closed, reset failureCount, set probeInFlight to false.
   - Else (closed or open): reset failureCount.
- The `RecordFailure` method:
   - If closed: increment failureCount, if >= threshold then open.
   - If half-open: open and set probeInFlight to false.
   - If open: do nothing.
- The `ReleaseProbe` method: sets probeInFlight to false (only meaningful in half-open).
- The `State` method: calls refresh() (which transitions open to half-open if cooldown elapsed) and returns state.
- The `refresh` method: if state is open and now().Sub(openedAt) >= cooldown, transition to half-open.
- The `open` method: transition to open, set openedAt to now(), reset failureCount.
- The `transition` method: set state and call setMetric.
- The `setMetric` method: sets the gauge for the provider to the float64 of the state.

Potential issues:

1. In `RecordSuccess`: when in half-open, we set `probeInFlight = false`. This is correct because the probe is done (success). However, note that the `Allow` method sets `probeInFlight = true` when it admits the probe. So on success, we reset it.

2. In `RecordFailure` for half-open: we call `open()` and set `probeInFlight = false`. The `open` method does:
   - transition to open
   - set openedAt = now()
   - reset failureCount
   Then we set probeInFlight to false. This is correct.

3. However, note that in the `open` method we reset the failureCount. This is done so that when we later transition from half-open to closed (on success) we start with a clean failure count. But also, when we are in open and get a failure (which we ignore) we don't want to accumulate failures. So resetting on open is correct.

4. The `ReleaseProbe` method: it sets probeInFlight to false. This is for when we win the probe but then the caller cancels (so we don't record success or failure). We release the probe so the next caller can try. This is correct.

5. The `State` method: it calls `refresh()` which may change the state from open to half-open. This is correct because we want the state to be up-to-date.

6. The `setMetric` method: we are setting the gauge to the float64 of the state. The state is an int (0,1,2). This is correct.

But note: the gauge is set on every state transition. However, note that the `State` method (which is called by the user) does not set the metric. Only the `transition` method sets the metric. And `refresh` calls `transition` when it changes state. So the metric is updated on every state change. This is correct.

However, there is a potential issue: the `setMetric` method is called under the mutex (because it's only called from `transition` which is under mutex). But the `metrics.Gauge` function returns a gauge that is safe for concurrent use? We look at the metrics package: the `Gauge` method in the registry returns a gauge, and the gauge's `Set` method uses an atomic.Uint64. So it is safe for concurrent use. So no issue.

But wait: the `setMetric` method is:
```go
func (b *Breaker) setMetric(s State) {
	metrics.Gauge(metrics.Key(metrics.NameCircuitBreakerState, metrics.LabelProvider, b.provider)).Set(float64(s))
```
This is called under the mutex of the breaker. However, the metrics registry also has a mutex. This could lead to a lock order issue if we are not careful. However, note that the metrics registry's mutex is taken in the `Gauge` function (which is called here) and then the gauge's Set is lock-free. So we are taking the breaker's mutex and then the metrics registry's mutex. This is acceptable as long as we always take the breaker's mutex first and then the metrics registry's mutex. We must check if there are any other places that take the metrics registry's mutex and then the breaker's mutex.

Looking at the metrics package: the `Gauge` function in the registry takes the registry's mutex, then creates or returns a gauge, then releases the mutex. Then we call Set on the gauge (which is lock-free). So the order is:
   breaker.mu -> registry.mu (in Gauge) -> then release registry.mu -> then gauge.Set (lock-free)

This is safe because we never hold two mutexes at the same time? Actually, we hold breaker.mu and then we take registry.mu (in Gauge) and then release it. So we have nested mutexes: breaker.mu then registry.mu. This is acceptable if we always take them in that order.

But note: what if another goroutine is trying to do the opposite? For example, in the metrics package, if we have a function that takes the registry mutex and then tries to access a breaker? We don't see any such function in the metrics package. The metrics package doesn't know about circuitbreaker. So it should be safe.

However, we note that the `metrics.Gauge` function is called from multiple places (not just circuitbreaker). But as long as we don't have a cycle, it's okay.

But there is a potential issue: the `metrics.DefaultRegistry.Reset()` function (used in tests) takes the registry mutex and then clears the gauges. Meanwhile, a breaker might be trying to set a gauge. This is only in tests, and the sprint plan says tests are in scope. However, the Reset function is only called in tests and the breaker is not used in production during tests? Actually, in tests we reset the registry to isolate tests. This is acceptable.

But note: in the breaker's `setMetric`, we are calling `metrics.Gauge` which may create the gauge if it doesn't exist. This is under the breaker's mutex. Meanwhile, the metrics registry's `Gauge` function takes the registry mutex. So we have:

   breaker.mu -> registry.mu (in metrics.Gauge)

This is the same order as above. So no deadlock.

However, we note that the `metrics.Gauge` function in the metrics package is:
```go
func (r *Registry) Gauge(name string) *gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.gauges[name]
	if !ok {
		g = &gauge{name: name}
		r.gauges[name] = g
	}
	return g
}
```
So it locks the registry mutex, then does some work, then unlocks. So we are holding breaker.mu and then we take registry.mu. This is consistent.

But what if we have two breakers for two different providers trying to set their metrics at the same time? They would each take their own breaker mutex and then try to take the registry mutex. The registry mutex is shared, so they would block on the registry mutex. This is acceptable and not a deadlock.

So no issue found in breaker.go.

### `internal/circuitbreaker/breaker_test.go`
This is the test file. We look for issues in the tests that might indicate a problem in the implementation, but note: we are to report issues in the code, not in the tests unless the test is wrong and we are to fix the test? However, the problem says to review the diff. The diff includes the test file. We are to look for issues in the test file that are in scope? But note: the sprint plan includes unit tests as in scope. So we should check the tests for correctness.

However, the problem says: "Output ONLY pipe-delimited findings". And we are to output issues. If we find an issue in the test file, we should report it? But note: the test file is part of the change and is in scope.

Let's look for obvious mistakes in the tests.

We see:
- The test `TestOpensAfterThresholdFailures`: it records 3 failures and checks that the state is open. This is correct.
- The test `TestOpenFailsFastBeforeCooldown`: it sets threshold=1, records one failure (so opens), then advances time by 59 seconds (less than cooldown of 60s) and checks that Allow returns false. This is correct.
- The test `TestTransitionsToHalfOpenAfterCooldown`: similar, advances by 60s and checks state is half-open. Correct.
- The test `TestHalfOpenAdmitsSingleProbe`: after cooldown, the first Allow returns true (and sets probeInFlight), the second returns false. Correct.
- The test `TestHalfOpenSuccessCloses`: after cooldown, becomes half-open (by Allow), then RecordSuccess -> state becomes closed. Correct.
- The test `TestHalfOpenFailureReopens`: after cooldown, becomes half-open, then RecordFailure -> state becomes open, and then we check that after 59s (so total 119s from the first failure) it is still open, then after 1 more second (120s) it becomes half-open. This tests that the cooldown is restarted on a half-open failure. Correct.
- The test `TestClosedSuccessResetsFailureRun`: records two failures, then a success (which resets the failure count), then two more failures -> state should be closed (because only two failures in the new run). Correct.
- The test `TestOpenSuccessDoesNotClose`: records a failure (open), then a success (while open) -> state should remain open. Correct.
- The test `TestOpenFailureDoesNotExtendCooldown`: records a failure (open at t0), then after 30s records another failure (ignored), then after another 30s (total 60s) expects half-open. This tests that the cooldown is not extended by failures observed while open. Correct.
- The test `TestConcurrentAccessIsRaceFree`: runs concurrent goroutines calling Allow, RecordSuccess, RecordFailure, and State. No assertion on final state, just checks for no race and no panics. This is a good test.
- The test `TestHalfOpenReleaseProbeReadmits`: tests that releasing a probe (without verdict) allows another probe. Correct.
- The test `TestReleaseProbeNoOpWhenClosed`: releasing probe when closed is a no-op. Correct.

We don't see any obvious mistakes in the tests.

### `internal/circuitbreaker/context.go`
This file defines the context key and helper functions.

```go
// providerKey is the unexported context key for the per-call provider name, so
// no other package can collide with or overwrite it.
type providerKey struct{}

// NewContext returns a context carrying the logical provider name for the call.
// The fan-out engine sets it once per agent invocation; llmclient.send reads it
// to key the per-provider breaker. An empty provider (or a context with no value)
// disables the breaker for that call — which is the correct behaviour for a
// diagnostic like `atcr doctor` that must probe every endpoint regardless of
// circuit state.
func NewContext(ctx context.Context, provider string) context.Context {
	return context.WithValue(ctx, providerKey{}, provider)
}

// ProviderFromContext returns the provider name set by NewContext, or "" when no
// provider was attached (the breaker then no-ops for that call).
func ProviderFromContext(ctx context.Context) string {
	p, _ := ctx.Value(providerKey{}).(string)
	return p
}
```

This looks correct. The key is unexported (so no collision). The functions are straightforward.

No issue found.

### `internal/circuitbreaker/registry.go`
This file defines the registry for breakers.

```go
// Registry holds one Breaker per provider, created on first use with the default
// threshold and cooldown. A provider's breaker is shared across every agent and
// goroutine that uses that provider, so provider health is tracked once (per
// provider) rather than independently per agent — the whole point of the epic.
// Safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	breakers map[string]*Breaker
}

// NewRegistry creates an empty registry. Tests use a fresh registry for
// isolation; production uses the package-global DefaultRegistry.
func NewRegistry() *Registry {
	return &Registry{breakers: make(map[string]*Breaker)}
}

// Get returns the breaker for provider, creating a closed one with the default
// tuning on first use. The same provider name always returns the same instance,
// so collaborating call sites share one circuit. The common already-exists case
// takes only a read lock; creation upgrades to a write lock and re-checks so a
// concurrent creator cannot produce two breakers for one provider.
func (r *Registry) Get(provider string) *Breaker {
	r.mu.RLock()
	b, ok := r.breakers[provider]
	r.mu.RUnlock()
	if ok {
		return b
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-check under the write lock: another goroutine may have created it
	// between the RUnlock above and the Lock here.
	if b, ok := r.breakers[provider]; ok {
		return b
	}
	b = New(provider, DefaultThreshold, DefaultCooldown)
	r.breakers[provider] = b
	return b
}

// Reset drops every breaker. It exists for test isolation; production never
// resets — the process-wide registry accumulates per-provider circuits for the
// life of the run.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.breakers = make(map[string]*Breaker)
}

// DefaultRegistry is the process-wide registry the llmclient integration reads
// to find each provider's circuit.
var DefaultRegistry = NewRegistry()
```

This is a standard double-checked locking pattern for a map. It is correct.

However, note: the `New` function is called with `DefaultThreshold` and `DefaultCooldown`. These are defined in breaker.go as:
```go
const (
	DefaultThreshold = 3
	DefaultCooldown  = 60 * time.Second
)
```
This matches the sprint plan.

No issue found.

### `internal/circuitbreaker/registry_test.go`
Tests for the registry. We look for issues.

We see:
- `TestRegistryGetCreatesAndShares`: tests that getting the same provider returns the same instance.
- `TestRegistryGetDistinctProviders`: tests that different providers get different breakers.
- `TestRegistryReset`: tests that Reset clears the registry.
- `TestDefaultRegistryUsable`: tests that the default registry works.
- `TestRegistryGetConcurrentSingleInstance`: tests that concurrent Get calls for the same provider return the same instance.
- `TestProviderContextRoundTrip`: tests that NewContext and ProviderFromContext work.
- `TestProviderFromContextAbsentIsEmpty`: tests that ProviderFromContext on empty context returns empty string.
- `TestProviderContextEmptyString`: tests that an empty provider string is preserved.

All these tests look correct.

No issue found.

### `internal/fanout/engine.go`
We added:
- Import of "github.com/samestrin/atcr/internal/circuitbreaker"
- Added a `Provider` field to the `Agent` struct.
- In `invokeAgent`, we set the context with the provider: `ctx = circuitbreaker.NewContext(ctx, a.Provider)`

We also changed the `Agent` struct to have the fields in a different order? Actually, we see:
```go
type Agent struct {
-	Name        string
+	Name string
+	// Provider is the logical provider name (the registry Providers map key). The
+	// engine threads it onto the request context in invokeAgent so llmclient.send
+	// can key the per-provider circuit breaker (Epic 4.5). It is intentionally NOT
+	// a field on Invocation: BaseURL is a lossy proxy for provider identity (two
+	// providers can share a base_url; trailing-slash variants splinter one). Empty
+	// (direct construction / a fallback that did not set it) no-ops the breaker.
+	Provider    string
 	Invocation  llmclient.Invocation
 	Prompt      string
 	PayloadMode string
```
We changed the order of the fields? Actually, we moved the `Name` field to be first and then added `Provider` after it. But note: the original had `Name` and then we changed it to `Name string` (without the extra space) and then added `Provider`. This is just a formatting change and does not affect correctness.

However, note: the comment says that the Provider field is intentionally NOT a field on Invocation. This is correct per the sprint plan.

The change in `invokeAgent` to set the context with the provider is correct.

But note: we are setting the context for every agent invocation. This covers single-shot, tool loop, and verify (as per the comment). This is correct.

No issue found.

### `internal/fanout/engine_breaker_test.go`
This is a new test file for the fanout engine and circuit breaker.

We see:
- `TestEngine_ThreadsProviderToContext`: tests that the engine threads the Agent.Provider onto the context so that llmclient can see it. This uses a fake Completer that captures the provider from the context. Correct.
- `TestInvokeSlot_CircuitOpenTriggersFallback`: tests that when the primary agent's circuit is open (returning CircuitOpenError), the engine uses the fallback. Correct.
- `TestInvokeSlot_CircuitOpenChainFailsAsFailed`: tests that when both primary and fallback are circuit open, the result is StatusFailed and the error is CircuitOpenError. Correct.
- `TestRecordAgentOutcome_CircuitOpenNotCountedAsAPICall`: tests that a CircuitOpenError does not increment the API call counter (because no HTTP request was made). This uses the `recordAgentOutcome` function from `internal/fanout/metrics.go` (which we see changed in the same diff). Correct.

No issue found.

### `internal/fanout/metrics.go`
We changed the `recordAgentOutcome` function to not count CircuitOpenError as an API call.

Original code:
```go
	if apiCalls < 1 {
		// Turns < 1 covers both the single-shot path (Turns==0, one provider
		// round-trip) and a corrupt negative value. Context cancellation/deadline
		// before the first HTTP call means no actual request was made; in that case
		// keep apiCalls at 0. Otherwise treat as a single-shot: 1 call.
		if errors.Is(r.Err, context.DeadlineExceeded) || errors.Is(r.Err, context.Canceled) {
			apiCalls = 0
		} else {
			apiCalls = 1
		}
	}
```

Changed to:
```go
	if apiCalls < 1 {
		// Turns < 1 covers both the single-shot path (Turns==0, one provider
		// round-trip) and a corrupt negative value. Some terminal errors mean no
		// actual request was made — context cancellation/deadline before the first
		// HTTP call, or a circuit-open fail-fast (Epic 4.5, AC2: no request made) —
		// so keep apiCalls at 0. Otherwise treat as a single-shot: 1 call.
		var coe *llmclient.CircuitOpenError
		switch {
		case errors.Is(r.Err, context.DeadlineExceeded) || errors.Is(r.Err, context.Canceled):
			apiCalls = 0
		case errors.As(r.Err, &coe):
			apiCalls = 0
		default:
			apiCalls = 1
		}
	}
```

This change is correct: we now also set apiCalls to 0 if the error is a CircuitOpenError.

However, note: the comment says "Some terminal errors mean no actual request was made — context cancellation/deadline before the first HTTP call, or a circuit-open fail-fast". This is correct.

But note: what about other errors that might not make a request? For example, if there is an error in building the request? The function is called with `r.Turns` which is the number of turns (which for single-shot is 0? or 1?). We don't have the full context, but the change is in line with the sprint plan's AC2: "When the circuit is open, API calls fail immediately with CircuitOpenError (no HTTP request made)."

So this change is correct.

No issue found.

### `internal/fanout/review.go`
We set the `Provider` field when building the primary agent and the fallback agent.

In `buildAgent`:
```go
	return Agent{
		Name:            name,
+		Provider:        ac.Provider,
		Prompt:          prompt,
		PayloadMode:     mode,
		Truncation:      mp.Truncation,
```
In `buildFallbackAgent`:
```go
	return Agent{
		Name: name,
+		// A fallback keys on its OWN provider: if it uses a different provider than
+		// the primary, it gets that provider's breaker (so a fallback can succeed
+		// while the primary's circuit is open).
+		Provider:    ac.Provider,
		Prompt:      primary.Prompt,
		PayloadMode: primary.PayloadMode,
		Truncation:  primary.Truncation,
```

This is correct: each agent (primary and fallback) uses its own provider's breaker.

No issue found.

### `internal/fanout/review_test.go`
We added:
```go
	circuitbreaker.DefaultRegistry.Reset()
	t.Cleanup(circuitbreaker.DefaultRegistry.Reset)
```
in the `mockProvider` function. This is to isolate the circuit breaker state between tests. This is correct because the registry is global.

No issue found.

### `internal/llmclient/client.go`
This is a critical file. We added the circuit breaker integration.

We added:
- Import of "github.com/samestrin/atcr/internal/circuitbreaker" and "errors"
- Changed the `send` method to wrap the dispatch with circuit breaker logic.

Let's look at the new `send` method:

```go
func (c *Client) send(ctx context.Context, endpoint, key string, body []byte) ([]byte, error) {
	provider := circuitbreaker.ProviderFromContext(ctx)
	if provider == "" {
		return c.dispatch(ctx, endpoint, key, body)
	}
	breaker := circuitbreaker.DefaultRegistry.Get(provider)
	if !breaker.Allow() {
		return nil, &CircuitOpenError{Provider: provider}
	}
	raw, err := c.dispatch(ctx, endpoint, key, body)
	// Every branch reports the outcome to the breaker exactly once: a half-open
	// probe MUST be resolved (RecordSuccess/RecordFailure/ReleaseProbe) or the
	// probe slot leaks and the circuit wedges half-open forever.
	switch {
	case err == nil:
		// A successful round-trip: closes a half-open probe, resets the run.
		breaker.RecordSuccess()
	case isBreakerFailure(err):
		// 5xx, timeout, or transport failure: trips/advances the failure run.
		breaker.RecordFailure()
	case errors.Is(err, context.Canceled):
		// The caller cancelled mid-call — nothing was learned about the provider.
		// Release any half-open probe without a verdict (do not count it).
		breaker.ReleaseProbe()
	default:
		// A non-tripping HTTP response (4xx incl. 429/401): the provider replied,
		// so it is reachable. The breaker tracks outages, not auth/rate-limit
		// correctness, so a reply counts as a healthy round-trip — which also
		// closes a half-open probe instead of wedging it.
		breaker.RecordSuccess()
	}
	return raw, err
}
```

We also added the `CircuitOpenError` type and the `isBreakerFailure` function.

Now, let's look for issues:

1. In the `send` method, we call `breaker.Allow()` to check if we can proceed. If not, we return a `CircuitOpenError`. This is correct per AC2.

2. We then call `c.dispatch` to make the actual request (with retries, etc.). Note: the dispatch method is the old send method (now renamed). This is correct.

3. After the dispatch, we handle the outcome:
   - If no error: RecordSuccess.
   - If isBreakerFailure(err): RecordFailure.
   - If context.Canceled: ReleaseProbe.
   - Else (which includes 4xx errors): RecordSuccess.

This matches the sprint plan's AC10: 4xx errors (including 429 and 401) do not count as breaker failures and are treated as a success (for the breaker).

However, note: what about other errors that are not covered by the above? For example, what if the dispatch returns an error that is not a breaker failure, not context.Canceled, and not nil? We treat it as a success for the breaker. This is correct per the sprint plan: only 5xx, timeout, and transport errors count as breaker failures.

But note: the `isBreakerFailure` function is defined as:
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
	// No HTTP response reached us: a transport-level failure or a timeout. Both
	// mean the provider is effectively unavailable → count as a breaker failure.
	return true
}
```

This function returns true for:
   - Any non-nil error that is not context.Canceled and not an HTTPStatusError with status < 500.
   - This includes:
        * HTTPStatusError with status >= 500 -> true
        * Any other error (like connection refused, timeout, etc.) -> true

This matches the sprint plan's AC10 (as clarified): 5xx, timeouts, and connection-level transport errors count; 4xx do not.

However, note: what about an error that is wrapped? The `errors.As` will unwrap until it finds an HTTPStatusError or fails. This is correct.

But note: what if the error is an HTTPStatusError with status 4xx? Then `he.Status >= 500` is false, so we fall through to the final `return true`? No, wait: we return true only if we don't have an HTTPStatusError or if we do and the status is <500? Actually, no:

The code:
```go
	var he *HTTPStatusError
	if errors.As(err, &he) {
		return he.Status >= 500
	}
	return true
```

So if we have an HTTPStatusError, we return true only if the status is >=500. If the status is <500 (like 4xx), we return false? No, wait: we return the result of `he.Status >= 500`. For 4xx, that is false. Then we do NOT fall through to the `return true` because we returned inside the if block.

So for an HTTPStatusError with status 4xx, we return false.

For any other error (non-HTTPStatusError and non-nil and non-context.Canceled), we return true.

This is correct.

But note: what about an error that is context.DeadlineExceeded? That is not context.Canceled and not an HTTPStatusError, so we return true. This is correct because a timeout counts as a breaker failure.

Now, what about an error that is context.Canceled? We already checked for that and returned false.

So the `isBreakerFailure` function is correct.

However, note: in the `send` method, we have a case for `errors.Is(err, context.Canceled)` and we call `breaker.ReleaseProbe()`. This is correct.

But what about context.DeadlineExceeded? It falls into the `isBreakerFailure` case (because it's not context.Canceled and not an HTTPStatusError) so we call `breaker.RecordFailure()`. This is correct.

Now, note: the `send` method returns the raw response and error from `c.dispatch`. We do not modify them. This is correct.

Potential issue: what if the `dispatch` method returns an error that is not nil, but we are in a half-open probe and we don't record the outcome correctly? We have a switch that covers all cases. We are confident.

However, note: the comment says: "Every branch reports the outcome to the breaker exactly once". We have:
   - nil: RecordSuccess
   - isBreakerFailure: RecordFailure
   - context.Canceled: ReleaseProbe
   - else: RecordSuccess

This covers all possibilities.

But note: what if the error is both context.Canceled and something else? The `errors.Is` checks for exact match? Actually, `errors.Is` checks if the error is or wraps the target. So if the error wraps context.Canceled, we catch it in the `errors.Is(err, context.Canceled)` case. This is correct.

Similarly, for `isBreakerFailure`, we check for context.Canceled at the top and return false, so we don't misclassify a canceled error as a breaker failure.

One more thing: the `send` method is called by `Complete` and `Chat`. We changed the name of the old `send` to `dispatch`. We must check that we didn't break anything else.

We see that at the end of the file, we have:
```go
// dispatch performs the request with the retry/backoff schedule and returns the
// raw 200 response body for the caller to parse. It is the inner retry loop
// wrapped by send (which adds the circuit breaker), shared by Complete (single
 // message) and Chat (multi-turn with tools): both feed it a pre-marshalled body
 // and decode the bytes themselves, so the retry, redirect, key-redaction, and
 // size-cap semantics stay identical across the two call shapes.
-func (c *Client) send(ctx context.Context, endpoint, key string, body []byte) ([]byte, error) {
+func (c *Client) dispatch(ctx context.Context, endpoint, key string, body []byte) ([]byte, error) {
```
We renamed the method from `send` to `dispatch`. And we updated the callers? We don't see any other callers in this diff, but note: the `Complete` and `Chat` methods are not shown in the diff. However, we assume they were updated to call `dispatch`? Actually, no: we see that the `send` method is now the wrapper, and the old `send` is now `dispatch`. So the `Complete` and `Chat` methods (which are not in this diff) must still be calling `send`. And we have changed `send` to be the new wrapper. So that is correct.

But note: we must check that the `Complete` and `Chat`