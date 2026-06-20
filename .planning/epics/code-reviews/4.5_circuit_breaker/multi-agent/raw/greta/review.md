The user wants me to review a diff based on a provided sprint plan and output findings in a specific TD_STREAM format.
I am Greta, a security-focused reviewer.
I need to check for vulnerabilities like injection, auth bypass, data exposure, input validation, etc.
The sprint plan is about implementing a circuit breaker for LLM providers.

Let's scan the diff for security issues.

1. `internal/circuitbreaker/context.go`:
   ```go
   type providerKey struct{}
   func NewContext(ctx context.Context, provider string) context.Context {
       return context.WithValue(ctx, providerKey{}, provider)
   }
   func ProviderFromContext(ctx context.Context) string {
       p, _ := ctx.Value(providerKey{}).(string)
       return p
   }
   ```
   This is standard Go context usage. No obvious security issue.

2. `internal/circuitbreaker/breaker.go`:
   - `Breaker` struct uses `sync.Mutex`.
   - `Allow()`, `RecordSuccess()`, `RecordFailure()`, `State()` all lock/unlock.
   - `setMetric` calls `metrics.Gauge(...).Set(...)`.
   - No obvious injection or auth issues.

3. `internal/llmclient/client.go`:
   - `send` wraps `dispatch` with circuit breaker.
   - `isBreakerFailure` checks `HTTPStatusError` and `context.Canceled`.
   - `CircuitOpenError` type.
   - Looks standard.

4. `internal/fanout/engine.go`:
   - Adds `Provider` field to `Agent`.
   - Threads provider into context: `ctx = circuitbreaker.NewContext(ctx, a.Provider)`.
   - `a.Provider` comes from `ac.Provider` (AgentConfig).
   - No obvious security issue.

5. `internal/metrics/metrics.go`:
   - Adds `gauge` type.
   - Uses `atomic.Uint64`.
   - `Set` and `Value` use `math.Float64bits`.
   - No obvious security issue.

6. `internal/boundaries_test.go`:
   - Updates allowed imports.
   - No security issue.

7. `docs/metrics.md`:
   - Documentation update.
   - No security issue.

Wait, let's look closer at `internal/circuitbreaker/breaker.go` -> `setMetric`:
```go
func (b *Breaker) setMetric(s State) {
	metrics.Gauge(metrics.Key(metrics.NameCircuitBreakerState, metrics.LabelProvider, b.provider)).Set(float64(s))
}
```
`b.provider` is set during `New`. It comes from `Registry.Get(provider)` or directly `New`.
In `Registry.Get`, `provider` is passed from `fanout` or `llmclient`.
In `fanout/engine.go`, `a.Provider` comes from `ac.Provider` (config).
Is there any risk of provider name injection into metrics?
Prometheus metric labels are generally sanitized, but if `b.provider` contains special characters, it might break metric exposition or cause issues. However, provider names usually come from config files. The sprint plan says "Provider identity = logical provider name". It's likely controlled. Not a high-severity issue unless untrusted input is passed.

Let's check `internal/llmclient/client.go` -> `isBreakerFailure`:
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
This correctly handles `context.DeadlineExceeded` as a failure (returns true), which matches AC10 ("timeouts ... count toward the breaker").
It handles `context.Canceled` as not a failure.
It handles 4xx as not a failure.
It handles 5xx as a failure.
It handles transport errors as failures.
Looks correct per spec.

Let's check `internal/fanout/metrics.go`:
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
Prevents counting circuit-open as an API call. Correct.

What about `internal/circuitbreaker/breaker.go` `Allow()` method?
```go
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refresh()
	switch b.state {
	case StateClosed:
		return true
	case StateHalfOpen:
		if b.probeInFlight {
			return false
		}
		b.probeInFlight = true
		return true
	default: // StateOpen
		return false
	}
}
```
Thread-safe.

Is there any issue with `context.WithValue` using an unexported type? It's best practice. `type providerKey struct{}` is unexported. Good.

Let's look at `internal/llmclient/client.go` `send`:
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
	// ...
}
```
If `provider` is not empty, it gets the breaker.
Wait, `CircuitOpenError` is returned. Does the caller handle it correctly? Yes, fanout checks for it.

Are there any potential issues with the `metrics` package?
`internal/metrics/metrics.go` adds `gauge` type.
```go
type gauge struct {
	name string
	bits atomic.Uint64
}
func (g *gauge) Set(v float64) { g.bits.Store(math.Float64bits(v)) }
func (g *gauge) Value() float64 { return math.Float64frombits(g.bits.Load()) }
```
Safe.

Let's check `internal/circuitbreaker/breaker.go` `ReleaseProbe`:
```go
func (b *Breaker) ReleaseProbe() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probeInFlight = false
}
```
This is called when `context.Canceled` happens during a half-open probe. It correctly releases the slot without recording success/failure. This prevents wedging. Good.

Wait, is there a missing check for `context.Canceled` in `isBreakerFailure`? It checks `errors.Is(err, context.Canceled)`. But what about `context.DeadlineExceeded`? The comment says "timeouts ... count toward the breaker". `isBreakerFailure` returns `true` for `context.DeadlineExceeded` because it falls through to `return true`. That's correct per spec.

Let's look at `internal/fanout/engine_breaker_test.go`:
Tests look solid.

Is there any issue with `internal/circuitbreaker/registry.go`?
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
Standard double-checked locking pattern for Go maps with RWMutex. Correct.

What about `internal/llmclient/client.go` -> `send` function signature change?
It was `send`, now it's `send` (wraps `dispatch`). The diff shows:
```go
-func (c *Client) send(ctx context.Context, endpoint, key string, body []byte) ([]byte, error) {
+func (c *Client) dispatch(ctx context.Context, endpoint, key string, body []byte) ([]byte, error) {
```
And a new `send` is added. This is fine.

Let's check for any potential data exposure or logging issues.
`CircuitOpenError.Error()` returns `fmt.Sprintf("circuit breaker open for provider %q: failing fast without an API call", e.Provider)`.
It logs the provider name. Not sensitive.

Is there any issue with `internal/circuitbreaker/context.go`?
`ProviderFromContext` returns `""` if not found. This is used to bypass the breaker for `doctor` command. Correct.

Wait, look at `internal/llmclient/client.go` `send`:
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
If `err` is `context.Canceled`, `isBreakerFailure` returns `false` (checked first in the function). So it goes to `case errors.Is(err, context.Canceled):`. Correct.
What if `err` is a wrapped `context.Canceled`? `errors.Is` handles it.
What if `err` is `context.DeadlineExceeded`? `isBreakerFailure` returns `true`, so it goes to `RecordFailure()`. Correct.

One minor thing: `isBreakerFailure` checks `errors.As(err, &he)`. If `he.Status >= 500`, returns true.
If `err` is a wrapped `HTTPStatusError`, `errors.As` works.
If `err` is something else, returns true.
This means *any* non-HTTP, non-canceled error is a breaker failure. This includes DNS errors, connection refused, etc. Matches AC10.

Is there any security issue with the `metrics` gauge implementation?
`gauge` uses `atomic.Uint64`. `Set` and `Value` are lock-free.
But `Registry.Gauge` uses `mu.Lock()`. That's fine for creation.
`WritePrometheus` copies maps under lock, then iterates. Safe.

Let's check `internal/fanout/engine.go` changes:
```go
	ctx = circuitbreaker.NewContext(ctx, a.Provider)
```
`a.Provider` is set from `ac.Provider` in `review.go`.
`ac` is `AgentConfig`. It comes from user config.
If a user puts malicious characters in `Provider` name, could it break metrics?
Prometheus labels allow alphanumeric and `_`, `-`, `.`. But usually, provider names are like "openai", "anthropic".
The code doesn't sanitize `b.provider` before passing to `metrics.Key`.
`metrics.Key` likely just concatenates strings.
If `b.provider` contains `"` or `{`, it might break Prometheus exposition format.
`internal/metrics/prometheus.go`:
```go
	fmt.Fprintf(&b, "%s %s\n", k, formatFloat(gauges[k].Value()))
```
`k` is the key. If `k` contains `{provider="..."}`, it's already formatted by `Key`.
Let's check `metrics.Key` (not in diff, but implied). Usually it does `fmt.Sprintf("%s{%s=%q}", name, label, value)`.
If `value` contains `"`, it might break.
But this is a configuration issue, not a direct vulnerability from external input. Usually considered LOW or OUT OF SCOPE unless explicitly untrusted. The sprint plan says "Provider identity = logical provider name". I'll skip unless it's clearly a vulnerability.

Wait, look at `internal/circuitbreaker/breaker.go`:
```go
func (b *Breaker) setMetric(s State) {
	metrics.Gauge(metrics.Key(metrics.NameCircuitBreakerState, metrics.LabelProvider, b.provider)).Set(float64(s))
}
```
If `b.provider` is empty string, `metrics.Key` will produce `atcr_circuit_breaker_state{provider=""}`. That's valid Prometheus.

Let's check `internal/llmclient/client_breaker_test.go`:
`resetBreakers` uses `t.Cleanup`. Good.

Is there any issue with `internal/fanout/engine.go` `invokeAgent`?
```go
	ctx = circuitbreaker.NewContext(ctx, a.Provider)
```
It overwrites any existing provider context? `context.WithValue` creates a new context. If `invokeAgent` is called recursively or in a loop, it just sets it for that agent. That's fine.

What about `internal/circuitbreaker/breaker.go` `open()` method?
```go
func (b *Breaker) open() {
	b.transition(StateOpen)
	b.openedAt = b.now()
	b.failureCount = 0
}
```
Resets failure count. Correct.

Let's look at `internal/metrics/metrics.go` `gauge` type:
```go
type gauge struct {
	name string
	bits atomic.Uint64
}
```
It's unexported. `Registry.Gauge` returns `*gauge`. Wait, `*gauge` is unexported type. Can it be returned from an exported method? Yes, but callers can only use its methods if they are exported. `Set`, `Value`, `Name` are exported. This is fine in Go.

Wait, `internal/metrics/metrics.go` line 153:
```go
type gauge struct {
```
It's unexported. But `func (r *Registry) Gauge(name string) *gauge` returns it.
This is valid Go, but sometimes linters complain. Not a security issue.

Let's check `internal/circuitbreaker/breaker.go` `Allow()` documentation:
"A caller that wins the half-open probe MUST report the outcome exactly once via RecordSuccess, RecordFailure, or ReleaseProbe — otherwise the probe slot leaks and the circuit wedges half-open forever."
The implementation in `llmclient/send` covers all paths: `err == nil`, `isBreakerFailure`, `context.Canceled`, `default`.
Is there any other error path? `dispatch` returns `([]byte, error)`.
If `dispatch` panics, it's not caught. But panics are not normal control flow.
What if `dispatch` returns an error that is wrapped `context.Canceled`? `errors.Is` catches it.
What if `dispatch` returns a custom error that wraps `context.Canceled`? `errors.Is` catches it.
Looks robust.

Is there any issue with `internal/fanout/engine_breaker_test.go`?
`f.failFor["primary"] = &llmclient.CircuitOpenError{Provider: "openai"}`
The fake client returns this error. The test checks fallback. Good.

Let's check `internal/llmclient/client.go` `send` again:
```go
	provider := circuitbreaker.ProviderFromContext(ctx)
	if provider == "" {
		return c.dispatch(ctx, endpoint, key, body)
	}
	breaker := circuitbreaker.DefaultRegistry.Get(provider)
	if !breaker.Allow() {
		return nil, &CircuitOpenError{Provider: provider}
	}
	raw, err := c.dispatch(ctx, endpoint, key, body)
```
If `breaker.Allow()` returns false, it returns `CircuitOpenError`.
Does it record anything? No. That's correct for AC2.

One potential issue: `isBreakerFailure` checks `errors.As(err, &he)`.
If `err` is `&HTTPStatusError{Status: 499}`, it falls through to `return true`.
Is 499 a client-side error or server-side? Usually client closed connection. It's not 5xx. But the code treats it as a breaker failure. This might be slightly aggressive, but probably fine. Not a security vulnerability.

Wait, look at `internal/circuitbreaker/breaker.go` `RecordSuccess`:
```go
	case StateHalfOpen:
		b.transition(StateClosed)
		b.failureCount = 0
		b.probeInFlight = false
	default: // StateClosed, StateOpen
		b.failureCount = 0
```
If `RecordSuccess` is called while `StateOpen`, it just resets `failureCount`. It does NOT close the circuit. This matches the design: "A success observed while open ... only clears the failure run; the cooldown-driven half-open probe still governs reopening." Correct.

Look at `internal/circuitbreaker/breaker.go` `RecordFailure`:
```go
	case StateHalfOpen:
		b.open()
		b.probeInFlight = false
```
Correct.

Is there any concurrency issue with `metrics.Gauge`?
`metrics.Gauge` takes a lock in `Registry.Gauge` for creation, then returns the `*gauge`.
`gauge.Set` uses `atomic.Uint64`. Safe.
`metrics.DefaultRegistry` is global. Safe.

Let's check `internal/fanout/metrics.go` `recordAgentOutcome`:
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
This correctly avoids counting circuit-open as an API call.

What about `internal/circuitbreaker/context.go`?
`providerKey` is unexported struct. Good practice to prevent context key collisions.

I will scan for any missing error handling or potential panics.
`internal/metrics/prometheus.go` `WritePrometheus`:
```go
	gauges := make(map[string]*gauge, len(r.gauges))
	for k, g := range r.gauges {
		gauges[k] = g
	}
```
Safe.

`internal/circuitbreaker/breaker.go` `setMetric`:
Calls `metrics.Gauge(...)`. If `metrics` package panics on invalid key, it would crash. But `metrics.Key` is standard.

Let's look at `internal/llmclient/client.go` `isBreakerFailure`:
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
What if `err` is `context.DeadlineExceeded`? `errors.Is` returns false. `errors.As` returns false. Returns `true`. Correct.
What if `err` is `&url.Error{Err: context.DeadlineExceeded}`? `errors.Is` checks wrapped errors, so it returns false. `errors.As` returns false. Returns `true`. Correct.
Actually, `url.Error` wrapping `context.DeadlineExceeded` is common in HTTP clients. `errors.Is(err, context.DeadlineExceeded)` will be true. So it returns false. Wait, the code says:
```go
	if errors.Is(err, context.Canceled) {
		return false
	}
```
It doesn't check `context.DeadlineExceeded` here. So it falls through to `return true`. This means timeouts ARE breaker failures, which matches AC10. Good.

Is there any issue with `internal/fanout/engine.go` `invokeAgent`?
```go
	ctx = circuitbreaker.NewContext(ctx, a.Provider)
```
If `a.Provider` is empty, `NewContext` sets it to `""`. `ProviderFromContext` returns `""`. `send` bypasses breaker. Correct.

One thing: `internal/circuitbreaker/breaker.go` `Allow()` says:
"A caller that wins the half-open probe MUST report the outcome exactly once via RecordSuccess, RecordFailure, or ReleaseProbe — otherwise the probe slot leaks and the circuit wedges half-open forever."
In `llmclient/send`, the switch covers all outcomes of `dispatch`. But what if `dispatch` panics? The probe slot leaks. This is a known limitation of this pattern without `defer` recovery or explicit probe management. However, panics are fatal anyway. I'll note it as a LOW maintainability/correctness issue if needed, but it's probably fine.

Wait, `internal/circuitbreaker/breaker.go` `ReleaseProbe` is called only on `context.Canceled`.
What if the context is canceled *after* `dispatch` returns but before the switch? Not possible, `dispatch` is synchronous.
What if `dispatch` returns an error that is *both* a breaker failure and canceled? `errors.Is(err, context.Canceled)` is checked first in `send`'s switch. So it goes to `ReleaseProbe()`. Correct.

Let's check `internal/llmclient/client_breaker_test.go` `TestBreaker_CancellationDoesNotTrip`:
It cancels context before call. `c.Complete` calls `send`. `send` calls `dispatch`. `dispatch` likely checks context early or immediately returns canceled. `send` gets `context.Canceled`, calls `ReleaseProbe()`. Breaker stays closed. Correct.

Is there any issue with `internal/metrics/metrics.go` `gauge` type being unexported but returned from `Gauge()`?
It's fine, but it means external packages can't declare `*gauge` variables, only use the interface/methods. Not a security issue.

I will look for any hardcoded secrets or credentials in the diff. None.
Any logging of sensitive data? `CircuitOpenError.Error()` logs provider name. Fine.
Any SQL/Command injection? None.
Any path traversal? None.
Any auth bypass? None.

Wait, `internal/circuitbreaker/breaker.go` `setMetric`:
```go
	metrics.Gauge(metrics.Key(metrics.NameCircuitBreakerState, metrics.LabelProvider, b.provider)).Set(float64(s))
```
If `b.provider` contains a newline or special char, it might break Prometheus exposition. But provider names are from config. I'll skip.

Let's check `internal/fanout/engine.go` `Agent` struct:
```go
type Agent struct {
	Name string
	Provider    string
	Invocation  llmclient.Invocation
	Prompt      string
	PayloadMode string
```
`Provider` is added. It's used in `invokeAgent`.
In `review.go` `buildAgent`:
```go
		Provider:        ac.Provider,
```
`ac` is `*ReviewConfig.AgentConfig`. It comes from YAML/config.
If a user specifies an empty provider, it no-ops. Correct.

One potential issue: `internal/circuitbreaker/breaker.go` `refresh()`:
```go
func (b *Breaker) refresh() {
	if b.state == StateOpen && b.now().Sub(b.openedAt) >= b.cooldown {
		b.transition(StateHalfOpen)
	}
}
```
It's called inside `Allow()` and `State()`. Both hold `b.mu`. Safe.

What about `internal/circuitbreaker/registry.go` `Get`?
Double-checked locking. Safe.

I think the code is very well written and secure. The sprint plan is strictly followed.
Are there any findings?
Maybe a LOW issue on `isBreakerFailure` treating *all* non-HTTP, non-canceled errors as breaker failures. This includes things like JSON decode errors if they happen before HTTP response? But `dispatch` handles HTTP response and returns `HTTPStatusError` for non-200. If it's a JSON decode error, it means the provider returned 200 but invalid JSON. Should that trip the breaker? Probably not, it's a client-side or provider bug, not an outage. But the code says "No HTTP response reached us: a transport-level failure or a timeout." Actually, if `dispatch` returns a non-HTTP error, it's likely transport or timeout. I'll assume it's fine.

Wait, `internal/llmclient/client.go` `send`:
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
The `default` case catches 4xx errors. It calls `RecordSuccess()`. This is intentional per AC10 ("a reply counts as a healthy round-trip — which also closes a half-open probe instead of wedging it.").
But wait, if a 4xx error occurs during a half-open probe, it closes the circuit. Is that correct?
AC10 says: "4xx responses do NOT trip the circuit: 429 rate-limits are owned by Epic 4.6's backoff and must not open the breaker until that epic's retries are exhausted; 4xx auth errors are permanent and provider-wide tripping would be incorrect."
It doesn't explicitly say 4xx should *close* a half-open circuit. But logically, if the provider responds (even with 4xx), it's reachable. Closing the circuit on 4xx during half-open prevents wedging. The comment says: "which also closes a half-open probe instead of wedging it." This is a design choice, not a bug.

I will flag one LOW issue regarding potential metric label injection if provider names are not sanitized, but it's likely out of scope or very low.
Actually, I'll look for something more concrete.
`internal/circuitbreaker/breaker.go` line 186: `metrics.Gauge(metrics.Key(...))`
If `b.provider` is empty, `metrics.Key` might produce `atcr_circuit_breaker_state{provider=""}`. Valid.
If `b.provider` contains `"`, it breaks Prometheus format. `metrics.Key` likely escapes or formats it. I'll assume `metrics.Key` handles it.

Let's check `internal/fanout/engine_breaker_test.go`:
`f.failFor["primary"] = &llmclient.CircuitOpenError{Provider: "openai"}`
`newFake()` is not in the diff, but it's a test helper.

I think the code is clean. I will output nothing if there are no issues, as per instructions: "If you find no issues, output nothing at all."
But wait, the prompt says "Review the following diff and identify any issues." and "If you find no issues, output nothing at all."
I should double check for any subtle issues.
`internal/llmclient/client.go` `send`:
```go
	provider := circuitbreaker.ProviderFromContext(ctx)
	if provider == "" {
		return c.dispatch(ctx, endpoint, key, body)
	}
```
If `provider` is empty, it bypasses the breaker. This is correct per design.

`internal/circuitbreaker/breaker.go` `Allow()`:
```go
	case StateHalfOpen:
		if b.probeInFlight {
			return false
		}
		b.probeInFlight = true
		return true
```
If `Allow()` returns true for half-open, the caller becomes the probe.
What if the caller's request takes a long time? Other callers fail fast. This is intentional.

One thing: `internal/circuitbreaker/breaker.go` `ReleaseProbe()`:
```go
func (b *Breaker) ReleaseProbe() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probeInFlight = false
}
```
It doesn't check `b.state == StateHalfOpen`. The comment says "Outside half-open it is a no-op". But it just sets `probeInFlight = false`. If called when closed, it just sets false (which is already false). If called when open, same. It's safe.

I will look at `internal/metrics/metrics.go` `gauge`:
```go
type gauge struct {
	name string
	bits atomic.Uint64
}
```
It's unexported. `Registry.Gauge` returns `*gauge`. This is fine.

Actually, I notice `internal/circuitbreaker/breaker.go` imports `github.com/samestrin/atcr/internal/metrics`.
`internal/metrics` imports nothing from `circuitbreaker`. No cycle.

I think the code is solid. I will generate no output, as there are no security or critical issues found.
Wait, let's verify the exact output format requirement: "Output ONLY pipe-delimited findings, one finding per line... If you find no issues, output nothing at all."
I will output nothing.

But wait, is there any issue with `internal/llmclient/client.go` `isBreakerFailure`?
```go
	if errors.As(err, &he) {
		return he.Status >= 500
	}
	return true
```
If `err` is a custom error that wraps `context.Canceled` but also implements some other interface, `errors.Is` catches it.
What if `err` is `&net.OpError`? `errors.As` fails, returns `true`. Correct.
What if `err` is `io.EOF`? Returns `true`. Correct.

I'm confident. I will output nothing.
Wait, let's reconsider `internal/circuitbreaker/breaker.go` `setMetric`.
`metrics.Gauge(metrics.Key(metrics.NameCircuitBreakerState, metrics.LabelProvider, b.provider)).Set(float64(s))`
If `b.provider` is not sanitized, it could break Prometheus exposition. This is a LOW maintainability/security issue if provider names come from untrusted sources. But they come from config. I'll skip.

One minor thing: `internal/fanout/engine.go` `invokeAgent` adds `ctx = circuitbreaker.NewContext(ctx, a.Provider)`.
If `a.Provider` is empty, it sets empty string. `ProviderFromContext` returns empty. `send` bypasses. Correct.

I will produce no output.
But to be safe, I'll check if there's any missing error handling in `send`:
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
If `dispatch` returns a wrapped error that is both `context.Canceled` and a breaker failure? `errors.Is` checks first. So it releases probe. Correct.
If `dispatch` returns a 4xx error wrapped in something else? `errors.As` in `isBreakerFailure` checks wrapped errors. So it catches it. Correct.

Everything looks correct and secure. I will output nothing.
Wait, the prompt says "If you find no issues, output nothing at all."
I will strictly follow that.
Let's double check the diff for any obvious mistakes.
`internal/circuitbreaker/breaker.go` line 153: `b.transition(StateClosed)` -> `b.failureCount = 0` -> `b.probeInFlight = false`. Correct.
`internal/circuitbreaker/breaker.go` line 166: `b.open()` -> `b.probeInFlight = false`. Correct.
`internal/circuitbreaker/breaker.go` line 179: `b.probeInFlight = false`. Correct.

All good.
I will output nothing.
Actually, I'll quickly verify `internal/metrics/metrics.go` gauge implementation:
`g.bits.Store(math.Float64bits(v))`
`math.Float64frombits(g.bits.Load())`
This is standard and safe.

One thing: `internal/circuitbreaker/breaker.go` `setMetric` is called from `transition`, which is called from `open`, `RecordSuccess`, `refresh`. All hold `b.mu`. `metrics.Gauge` takes `r.mu` lock. No deadlock because `metrics` never calls back into `circuitbreaker`. The comment says: "The metrics call takes the registry's own lock briefly; metrics never calls back into this package, so there is no lock-ordering cycle." Correct.

I am ready. Outputting nothing.
Wait, is there any issue with `internal/fanout/engine_breaker_test.go` using `newFake()`? It's not in the diff, but it's a test file. The diff shows `f.failFor["primary"] = ...`. It's fine.

I will output nothing.
But to be absolutely sure, I'll check `internal/llmclient/client.go` `send` function signature change.
The diff shows:
```go
-func (c *Client) send(ctx context.Context, endpoint, key string, body []byte) ([]byte, error) {
+func (c *Client) dispatch(ctx context.Context, endpoint, key string, body []byte) ([]byte, error) {
```
And a new `send` is added. This is fine.

Final decision: No findings. Output nothing.
Wait, the prompt says "Output ONLY pipe-delimited findings... If you find no issues, output nothing at all."
I will comply.
Actually, let's look at `internal/circuitbreaker/breaker.go` `Allow()` comment:
"A caller that wins the half-open probe MUST report the outcome exactly once via RecordSuccess, RecordFailure, or ReleaseProbe — otherwise the probe slot leaks and the circuit wedges half-open forever."
This is a contract. The implementation in `llmclient` honors it. If a future caller forgets, it wedges. This is a design risk, but documented. Not a bug in this diff.

I will output nothing.
One last check: `internal/metrics/names.go` adds `NameCircuitBreakerState` and `LabelProvider`. Correct.
`internal/boundaries_test.go` updates imports. Correct.
All changes align with the sprint plan. No security vulnerabilities found.
Proceeding with empty output. 
Actually, I'll generate an empty string as requested.
Wait, the system might expect at least a newline or something? "If you find no issues, output nothing at all." -> I will literally output nothing.
Done. 
Let's produce the response. 
(Self-Correction/Refinement during thought)
Is there any chance `isBreakerFailure` incorrectly classifies `context.DeadlineExceeded`?
The code:
```go
	if errors.Is(err, context.Canceled) {
		return false
	}
```
It does NOT check `context.DeadlineExceeded`. So it falls through to `return true`.
AC10 says: "timeouts ... count toward the breaker". So returning `true` for `context.DeadlineExceeded` is CORRECT.
What about `context.Canceled`? AC10 says: "Caller-initiated cancellation (`context.Canceled`) records nothing." So returning `false` is CORRECT.
The logic is sound.

I will output nothing.
