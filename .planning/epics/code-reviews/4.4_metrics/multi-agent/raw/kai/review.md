The user wants a code review of a specific diff, scoped by a sprint plan (Epic 4.4: Metrics and Observability). I need to review only files/areas directly related to the sprint plan tasks.

Scope In-Scope:
- `internal/metrics` package (Counter, Histogram, Registry, Prometheus export)
- Metrics collection in fanout engine (agent invocations, API calls)
- Metrics collection in CLI (review duration, findings)
- Summary printed at end of CLI review
- `/metrics` endpoint in MCP server mode (Prometheus text format) -> implemented as `atcr_metrics` MCP tool
- Unit tests for counter, histogram, and registry
- Integration test: run a review, verify metrics collected

Files in diff:
- `cmd/atcr/review.go` - CLI review, prints summary
- `cmd/atcr/review_summary.go` - new file, prints summary
- `cmd/atcr/review_summary_test.go` - tests for summary
- `internal/boundaries_test.go` - import boundaries, not in-scope for issues (it's a test infra file ensuring deps, though related to metrics package existing). The sprint plan doesn't mention modifying boundaries_test specifically, but adding metrics to allowlist is part of the integration. Not a code issue.
- `internal/fanout/artifacts.go` - WritePool calls recordFindingMetrics
- `internal/fanout/engine.go` - invokeAgent instrumentation
- `internal/fanout/engine_metrics_test.go` - tests
- `internal/fanout/metrics.go` - new file, recordAgentOutcome etc.
- `internal/fanout/metrics_review_test.go` - tests
- `internal/fanout/review.go` - ExecuteReview instrumentation
- `internal/mcp/handlers.go` - handleMetrics
- `internal/mcp/metrics_test.go` - tests
- `internal/mcp/server.go` - registers tool
- `internal/mcp/server_test.go` - count update
- `internal/mcp/tools.go` - constants/structs
- `internal/mcp/tools_test.go` - test names
- `internal/metrics/metrics.go` - core package
- `internal/metrics/metrics_test.go` - tests
- `internal/metrics/names.go` - constants
- `internal/metrics/prometheus.go` - renderer
- `internal/metrics/prometheus_test.go` - tests

I need to look for issues in these files.

Let's analyze each file:

1. `cmd/atcr/review.go`:
   - Adds call to `writeReviewSummary(cmd.OutOrStdout(), metrics.DefaultRegistry, time.Since(now), result.Summary.Total)`
   - `result` is checked for nil before calling.
   - Is there a potential issue? `result.Summary.Total` is used. `result` is not nil, so `result.Summary` should be safe? The `Summary` field might be zero value but likely populated by `runReview`. Not an obvious bug.
   - `time.Since(now)` - `now` was defined earlier in the function. That's fine.

2. `cmd/atcr/review_summary.go`:
   - `severityOrder` is a package-level var. Fine.
   - `writeReviewSummary` takes `reg *metrics.Registry`, `elapsed`, `totalAgents`.
   - `val := func(name string) int64 { return reg.Counter(name).Value() }` - if the counter doesn't exist, `reg.Counter` creates it (get-or-create). So `Value()` returns 0. Safe.
   - It ignores Fprintf errors with `_, _ = ...`. This is a minor maintainability/correctness concern but common for UI output. The sprint plan doesn't require strict error handling on stdout writes.
   - `severityBreakdown`: iterates over `severityOrder`, gets counters. If counter missing, creates it and gets 0. Only appends if > 0. Fine.
   - Potential issue: The format string in `fmt.Fprintf(w, "Findings: %d%s\n", val(metrics.NameFindingsTotal), severityBreakdown(reg))` - if `severityBreakdown` returns string starting with space, it's fine.
   - Wait, look at `severityBreakdown`: returns `" (" + ... + ")"` or `""`. So output is `Findings: 5 (2 HIGH, 3 MEDIUM)`. Matches AC3 example.

3. `cmd/atcr/review_summary_test.go`:
   - Looks fine.

4. `internal/fanout/engine.go`:
   - `invokeAgent` now wraps `dispatchAgent`.
   - `metrics.Counter(metrics.NameAgentsTotal).Inc()` - increments total before dispatch.
   - `start := time.Now()`
   - `r := e.dispatchAgent(ctx, a)`
   - `metrics.Histogram(metrics.NameAgentDurationSeconds).Observe(time.Since(start).Seconds())`
   - `recordAgentOutcome(r)`
   - What if `dispatchAgent` panics? The histogram wouldn't be observed. No `defer` is used here. The original design in sprint plan said `defer func() { ... }()`. Here it's not deferred. If `dispatchAgent` returns normally, it's fine. If it panics, the metric is lost and the program crashes anyway. Not a major issue. But wait, what if there's an early return somewhere? `dispatchAgent` always returns a Result. So direct sequence is fine.
   - However, the `metrics.Histogram` and `recordAgentOutcome` are after `dispatchAgent`. If `dispatchAgent` panics, they don't run. But panics are fatal-ish.

5. `internal/fanout/metrics.go`:
   - `recordAgentOutcome`:
     - `StatusOK` -> `NameAgentsSucceeded`. Good.
     - `StatusFailed` -> `NameAgentsFailed`. Good.
     - `StatusTimeout` -> `NameAgentsTimedOut`. Good.
     - `apiCalls := r.Turns; if apiCalls == 0 { apiCalls = 1 }` - assumes single shot is always 1 call. What about degraded tool path? The comment says single-shot/degraded path: one provider call, Turns unset. If `Turns` is 0, it's treated as 1. If Turns > 0 (tool loop), uses Turns. This matches comment: "a tool-loop agent makes one Chat call per turn (Result.Turns)". Wait, does `Result.Turns` represent the number of provider calls? The comment says "counting from Result.Turns there reflects real round-trips". So `Turns` is the number of turns. If a tool loop agent had 3 turns, that means 3 API calls? Or 4? Usually a turn means one back-and-forth. Let's trust the domain logic that Turns maps 1:1 to API calls.
     - `errors.As(r.Err, &he)` - unwrapping to HTTPStatusError. Good.
     - `metrics.Counter(metrics.Key(...)).Inc()` - fine.
     - `if r.ToolCalls > 0` - adds tool calls. Good.
   - `recordFindingMetrics`:
     - Iterates over `findings []stream.Finding`.
     - `sev := stream.NormalizeSeverity(f.Severity)` - what if `NormalizeSeverity` doesn't exist or returns unexpected values? We don't have that file in diff. Assume it exists.
     - `metrics.Counter(metrics.Key(...)).Inc()` - fine.
   - `recordReviewOutcome`:
     - `case interrupted: metrics.Counter(metrics.NameReviewsInterrupted).Inc()`
     - `case failed: metrics.Counter(metrics.NameReviewsFailed).Inc()`
     - `default: metrics.Counter(metrics.NameReviewsSucceeded).Inc()`
     - This is correct per precedence.

6. `internal/fanout/review.go`:
   - `ExecuteReview`:
     - `metrics.Counter(metrics.NameReviewsTotal).Inc()` - good.
     - `reviewStart := time.Now()`
     - `defer func() { metrics.Histogram(...).Observe(...) }()` - good, deferred.
     - Then several return points:
       - `return nil, err` after `recordReviewOutcome(interrupted, true)` - good.
       - `return nil, err` after `WriteManifest` failure with `recordReviewOutcome(interrupted, true)` - good.
       - `return res, outErr` after `recordReviewOutcome(interrupted, true)` - good.
       - `return res, nil` after `recordReviewOutcome(interrupted, false)` - good.
     - Wait, there's a subtle bug! Look at the structure:
       ```go
       defer func() {
           metrics.Histogram(metrics.NameReviewDurationSeconds).Observe(time.Since(reviewStart).Seconds())
       }()

       // ...

       if err != nil {
           // ...
           recordReviewOutcome(interrupted, true)
           return nil, err
       }

       // ...

       if err := WriteManifest(...); err != nil {
           recordReviewOutcome(interrupted, true)
           return nil, err
       }

       // ...

       if _, outErr := Outcome(results); outErr != nil {
           recordReviewOutcome(interrupted, true)
           return res, outErr
       }
       recordReviewOutcome(interrupted, false)
       return res, nil
       ```
       The `defer` for histogram observation will fire AFTER the function returns, which means it will record the duration correctly regardless of path. That's fine.
       However, what about `interrupted`? It's passed to `recordReviewOutcome`. Where is `interrupted` set? Looking at the surrounding code not fully in diff, but in `ExecuteReview`, there is likely an `interrupted` boolean derived from context cancellation. The diff shows `recordReviewOutcome(interrupted, true)` and `recordReviewOutcome(interrupted, false)`. This seems correct.

7. `internal/mcp/handlers.go`:
   - `handleMetrics`:
     - Checks `ctx.Err()` - good.
     - `metrics.DefaultRegistry.WritePrometheus()` - fine.
   - Is there any issue with `WritePrometheus` being called under the registry lock while metrics might be updated? `WritePrometheus` locks `r.mu` for the entire rendering. Counters use atomic operations outside the lock, but histogram `Observe` locks its own `mu`. If a histogram is observed during `WritePrometheus` (which holds `r.mu`), can it deadlock? No, `WritePrometheus` only locks `r.mu` (Registry mutex). Histogram methods lock `h.mu` (histogram mutex). They are different locks. However, `WritePrometheus` reads `r.counters` and `r.histograms` maps under `r.mu`, and then calls `r.counters[k].Value()` (atomic, safe) and `r.histograms[k].Percentile()`, `Sum()`, `Count()` which lock `h.mu`. This is safe; no lock ordering violation because `h.mu` is not held when acquiring `r.mu`.
   - Wait, what about concurrent modification of `r.counters` map? `Counter()` in Registry locks `r.mu` to modify the map. `WritePrometheus` locks `r.mu` for the entire duration. So `Counter()` calls from other goroutines will block until `WritePrometheus` finishes. That's acceptable for a metrics scrape.

8. `internal/metrics/metrics.go`:
   - `counter` uses `atomic.Int64`. Good.
   - `histogram`:
     - `Observe` locks `h.mu`.
     - Appends to `values` until `maxHistogramSamples`, then ring buffer.
     - `Percentile` locks `h.mu`, copies `h.values` to `sorted`, sorts.
     - `ceilDiv` implementation:
       ```go
       func ceilDiv(p, n float64) float64 {
           x := p / 100 * n
           t := float64(int64(x))
           if x > t {
               return t + 1
           }
           return t
       }
       ```
       Wait, this is `ceil(p/100 * n)`. But `p` is passed as a percentile (e.g., 50) and `n` is `float64(len(sorted))`. So it's `ceil(50/100 * 5) = ceil(2.5) = 3`. Then `rank := min(max(int(ceilDiv(...)), 1), len(sorted))`. `rank = 3`. `sorted[rank-1]` = `sorted[2]` (0-indexed). For sorted `[1,2,3,4,5]`, index 2 is `3`. That matches nearest-rank.
       However, look at the clamping in `Percentile`:
       ```go
       p = min(max(p, 0), 100)
       rank := min(max(int(ceilDiv(p, float64(len(sorted)))), 1), len(sorted))
       ```
       This looks correct.
     - But wait, `Percentile` takes `p float64` where `p` is 0-100. `summaryQuantiles` are `[]float64{0.5, 0.9, 0.95, 0.99}`. In `WritePrometheus`, it calls `h.Percentile(q*100)`. So `q` is 0.5, passed as `50.0` to Percentile. Good.
     - `Mean()`: `h.sum / float64(h.count)`. Good.
     - `Registry.Reset()`: allocates new maps. Good.
     - Package-level `Counter(name)` returns `*counter`. Wait, `counter` is an unexported type. The return type of `Counter(name)` is `*counter`. But in Go, you cannot return an unexported type from an exported function if it's meant to be used by external packages? Actually, you CAN return an unexported type from an exported function. Callers can use the methods defined on it, but cannot name the type. This is legal Go, but often considered bad API design. However, the sprint plan explicitly says: "The concrete counter/histogram types are unexported because that name space is owned by those accessor functions; callers hold the returned values fluently and never name the type." This is an intentional design decision. Not a bug.
     - `maxHistogramSamples` is 10000.
     - `histogram.Observe` ring buffer:
       ```go
       if !h.full {
           h.values = append(h.values, v)
           if len(h.values) == maxHistogramSamples {
               h.full = true
               h.next = 0
           }
           return
       }
       h.values[h.next] = v
       h.next = (h.next + 1) % maxHistogramSamples
       ```
       This looks correct.

9. `internal/metrics/prometheus.go`:
   - `escapeLabelValue`:
     ```go
     func escapeLabelValue(v string) string {
         v = strings.ReplaceAll(v, `\`, `\\`)
         v = strings.ReplaceAll(v, `"`, `\"`)
         v = strings.ReplaceAll(v, "\n", `\n`)
         return v
     }
     ```
     Is this sufficient for Prometheus text format? Prometheus text format also requires escaping `\n`, but what about other characters? The Prometheus spec says only `\`, `"`, and `\n` need escaping. This is correct.
   - `WritePrometheus`:
     - Locks `r.mu`.
     - `families` map groups counters by family.
     - Then iterates `sortedKeys(families)`.
     - For each family, prints `# TYPE %s counter\n`.
     - Then sorts `keys` (the slice of full keys in that family) and prints each.
     - Wait: `keys := families[fam]` - this is a slice from the map. Then `sort.Strings(keys)` - this sorts the slice in-place. Since `families` values are slices stored in the map, this mutates the slices inside the map. But since this is a local map built within the function, that's fine.
     - For histograms: same pattern.
     - `_, inner := splitLabels(k)` - `inner` is the label text without braces.
     - `withQuantile(inner, q)`: if inner is empty, returns `{quantile="0.5"}`. If inner is not empty, returns `{inner,quantile="0.5"}`.
       But wait, what if `inner` is `status="429"`? Then it returns `{status="429",quantile="0.5"}`. That looks valid.
     - `labelSuffix(inner)`: if empty, returns `""`. Else returns `"{" + inner + "}"`. So for `_sum` and `_count`, it appends the label set. Good.
     - Wait, for histograms with labels, the Prometheus output format for summary quantiles usually puts the quantile inside the label set, which is done here. The `_sum` and `_count` lines should have the same labels (except quantile). This is done via `labelSuffix(inner)`. Good.
     - One issue: The `metricFamily` function splits on first `{`. What if a metric name itself contains a `{`? That's impossible for valid metric names.
     - Another issue: `WritePrometheus` renders histograms as `summary` type. But Prometheus summaries are usually pre-computed quantiles over a time window, and the `_sum` and `_count` are for the summary. Here they are using `_sum` and `_count` of the histogram data. This is acceptable for exposition, though technically it's a bit of a mismatch. The sprint plan says "histograms render as summary". This is intentional.
     - Wait, there's a correctness issue in `WritePrometheus`:
       ```go
       families := make(map[string][]string, len(r.counters))
       for k := range r.counters {
           f := metricFamily(k)
           families[f] = append(families[f], k)
       }
       ```
       This iterates over `r.counters` map. Since `r.mu` is held, this is safe.
       Then `for _, fam := range sortedKeys(families) { ... keys := families[fam]; sort.Strings(keys); ... }`.
       However, after the counters block, the histograms block:
       ```go
       histFamilies := make(map[string][]string, len(r.histograms))
       for k := range r.histograms { ... }
       ```
       What if a counter and a histogram share the same family name? E.g., `atcr_x_total` as both a counter and a histogram? Unlikely by convention, but if it happened, the TYPE lines would conflict. Not an issue in practice given the naming scheme.
     - Potential issue: In `WritePrometheus`, for histograms, it prints:
       ```go
       fmt.Fprintf(&b, "%s%s %s\n", fam, withQuantile(inner, q), formatFloat(h.Percentile(q*100)))
       ```
       Wait, `withQuantile` already includes the braces. And `fam` is the family name. So for an unlabeled histogram, `fam` = `atcr_review_duration_seconds`, `withQuantile("", 0.5)` = `{quantile="0.5"}`. Output: `atcr_review_duration_seconds{quantile="0.5"} 1.5`. Good.
       For a labeled histogram, e.g., key is `atcr_agent_duration_seconds{persona="skeptic"}`. `fam` = `atcr_agent_duration_seconds`. `inner` = `persona="skeptic"`. `withQuantile` = `{persona="skeptic",quantile="0.5"}`. Output: `atcr_agent_duration_seconds{persona="skeptic",quantile="0.5"} 2`. Good.
       Then for sum: `fmt.Fprintf(&b, "%s_sum%s %s\n", fam, labelSuffix(inner), formatFloat(h.Sum()))`.
       `labelSuffix(inner)` = `{persona="skeptic"}`. Output: `atcr_agent_duration_seconds_sum{persona="skeptic"} 2`. Good.
       Wait, is `atcr_agent_duration_seconds_sum` correct? In Prometheus, the metric name for sum is `metric_name_sum`. If the metric has labels, they are appended: `metric_name_sum{label="value"}`. That seems correct. However, Prometheus best practice is that the `quantile` label is on the summary metric lines, and `_sum` and `_count` are separate metrics with the same labels. This matches.

     - Another issue: `formatFloat` uses `strconv.FormatFloat(f, 'g', -1, 64)`. This can produce `NaN` or `Inf` for invalid values. If `h.Percentile` returns NaN (can it?), then output is `NaN`. But `Percentile` returns 0 for empty, and computed from sorted floats. If values are normal, it's fine.

     - Wait, there is a subtle bug in `Percentile` rank calculation for small samples:
       `rank := min(max(int(ceilDiv(p, float64(len(sorted)))), 1), len(sorted))`
       `ceilDiv(50, 2)` = `ceil(0.5 * 2) / wait.
       Let's re-evaluate `ceilDiv`:
       `x := p / 100 * n`
       For p=50, n=2: x = 1.0. t = 1.0. x > t is false. returns 1.0.
       rank = int(1.0) = 1. sorted[0]. That means for 2 samples, P50 is the first element. Nearest-rank method says rank = ceil(P/100 * N). For P=50, N=2: rank = ceil(1) = 1. So the 1st element. That seems correct per nearest-rank.
       But what about p=0? `x = 0`. `t = 0`. `x > t` false. returns 0. `int(0) = 0`. Then `max(0, 1)` = 1. So rank=1. Sorted[0]. That's the minimum. Good.
       What about p=100? x = n. t = n (if n is integer-valued float). x > t false. returns n. rank = n. sorted[n-1]. Good.
       What about p=99, n=1? x = 0.99. t = 0. x > t true. returns 1. rank=1. sorted[0]. Good.
       What about p=1, n=100? x=1.0. t=1.0. returns 1. rank=1. sorted[0]. Good.

     - Wait, is there a data race or concurrency issue?
       `WritePrometheus` holds `r.mu`. But `r.counters` map entries can be accessed concurrently via `Counter()` which also locks `r.mu`. That's fine.
       However, `r.histograms[k].Percentile()` locks `h.mu`. While `WritePrometheus` holds `r.mu`, another goroutine could call `r.Histogram(name).Observe()` which locks `r.mu` (briefly for lookup/insert) then `h.mu`. Since `WritePrometheus` holds `r.mu` for the entire duration, any call to `Histogram()` or `Counter()` that needs to create a new entry will block. But existing histogram `Observe` only locks `h.mu`, not `r.mu`. So observations can proceed concurrently with `WritePrometheus` for existing metrics. Only new metric creation is blocked. That's fine.

10. `internal/metrics/names.go`:
    - Just constants. Fine.

11. `internal/fanout/engine_metrics_test.go`:
    - `TestEngineRecordsAgentMetrics`: resets DefaultRegistry. Tests against global state. Not parallel, as noted. Good.
    - Checks various counters. Good.
    - `TestRecordAgentOutcome`: tests the helper directly. Good.

12. `internal/fanout/metrics_review_test.go`:
    - `TestWritePoolRecordsFindingMetrics`: uses `okResult` helper. Not shown in diff, assume exists.
    - `TestRunReviewRecordsReviewMetrics`: integration test. Uses `mockProvider(t)`. Resets DefaultRegistry. Good.
    - `TestRecordReviewOutcome`: covers branches. Good.

13. `internal/mcp/handlers.go` / `internal/mcp/server.go` / etc.:
    - Adding `atcr_metrics` tool. Fine.
    - `handleMetrics` returns `MetricsResult` with `Format: "prometheus"`. Good.

Now, let's look for issues that are real and actionable.

Potential Issue 1: `internal/fanout/engine.go` `invokeAgent` does not use `defer` for histogram observation.
If `dispatchAgent` panics, the duration histogram is not observed. However, more importantly, if the code were modified later to add an early return, it could be missed. But currently `dispatchAgent` returns a Result and there are no early returns between start and observe. Still, using `defer` is safer and matches the sprint plan design. But is this a real issue? It's a correctness/maintainability issue: "Duration metric missed on panic or future early return". Let's check if `dispatchAgent` can panic. It can, if there's a bug. But generally, metrics should use `defer` to ensure observation on all exit paths. I'll flag this as LOW or MEDIUM. Since panics are fatal and there are no current early returns, it's a LOW maintainability issue. But the sprint plan explicitly showed `defer` in the sample code. The implementation deviated. I'd say LOW.

Potential Issue 2: `internal/metrics/prometheus.go` `WritePrometheus` doesn't escape `fam` or label names.
Wait, `fam` is derived from `metricFamily(key)` which splits on `{`. The `key` is the registry key. If a user creates a metric with a name containing curly braces or quotes, the output could be malformed. But `Key()` is the intended way to create labeled keys, and metric names are constants. Not a real issue.

Potential Issue 3: `cmd/atcr/review_summary.go` `writeReviewSummary` computes `totalAgents` from parameter but doesn't validate it's non-zero. If 0, output says "Agents: 0/0 succeeded...". That's fine.

Potential Issue 4: `internal/metrics/metrics.go` `histogram.Observe` ring buffer overwrite logic.
When `h.full` is true, `h.next` starts at 0. It writes at 0, then 1, etc. This correctly overwrites oldest first. No bug.

Potential Issue 5: `internal/metrics/metrics.go` `Percentile` uses `make([]float64, len(h.values))` and `copy`. This allocates on every call. For a metrics scrape, this could be expensive if there are many histograms with many samples. But `maxHistogramSamples` is 10000, and `copy` + `sort` is O(N log N) with N <= 10000. This is acceptable for a scrape.

Potential Issue 6: `internal/metrics/prometheus.go` `WritePrometheus` does not include `# HELP` lines.
Prometheus text format usually includes `# HELP` lines, but they are optional. The sprint plan doesn't mention them. Not an issue.

Potential Issue 7: In `internal/fanout/review.go`, the `recordReviewOutcome` is called manually before each return. Is there any path where it's missed?
Looking at `ExecuteReview`:
- `return nil, err` after `recordReviewOutcome(interrupted, true)` - covered.
- `return nil, err` after `WriteManifest` failure with `recordReviewOutcome` - covered.
- `return res, outErr` after `recordReviewOutcome(interrupted, true)` - covered.
- `return res, nil` after `recordReviewOutcome(interrupted, false)` - covered.
- Are there any other return statements in the function not shown in diff? The diff shows the full function body. It seems all returns are covered.
But wait, what if `runEngine` returns and then the code panics before reaching the first return? `defer` for histogram handles duration, but `recordReviewOutcome` wouldn't be called. However, panics are exceptional.

Potential Issue 8: `internal/fanout/metrics.go` `recordFindingMetrics` uses `stream.NormalizeSeverity`. If this function doesn't exist or behaves unexpectedly, it could be a bug. But we don't have that code.

Potential Issue 9: `internal/metrics/prometheus.go` `withQuantile` and `labelSuffix` for histograms with labels.
In Prometheus text format, for a summary metric with labels plus a quantile, the `quantile` label should be part of the label set. The current code does:
`atcr_agent_duration_seconds{persona="skeptic",quantile="0.5"} 2`
This is correct.
For `_sum` and `_count`:
`atcr_agent_duration_seconds_sum{persona="skeptic"} 2`
Wait, the metric name is `atcr_agent_duration_seconds_sum`. Is that right? In Prometheus, for a Summary metric, the base name is `atcr_agent_duration_seconds`. The `_sum` and `_count` suffixes are appended to the base name. So `atcr_agent_duration_seconds_sum` is correct. However, if there are labels, they should be appended to that: `atcr_agent_duration_seconds_sum{persona="skeptic"}`. That is what the code does. Good.

But wait, look at the `withQuantile` function:
```go
func withQuantile(inner string, q float64) string {
    if inner == "" {
        return fmt.Sprintf(`{quantile="%s"}`, formatFloat(q))
    }
    return fmt.Sprintf(`{%s,quantile="%s"}`, inner, formatFloat(q))
}
```
And `labelSuffix`:
```go
func labelSuffix(inner string) string {
    if inner == "" {
        return ""
    }
    return "{" + inner + "}"
}
```
For histograms, the `_sum` and `_count` lines use `labelSuffix(inner)`. This is fine.

Potential Issue 10: In `internal/metrics/prometheus.go`, `WritePrometheus` locks `r.mu` for the entire rendering. If there are many metrics, this could block new metric creation for a long time. But acceptable.

Potential Issue 11: In `internal/metrics/metrics.go`, the `histogram` type has `mu sync.Mutex`. `Observe` locks it. `Percentile` locks it. `Mean`, `Sum`, `Count` lock it. This is fine. But `Count()` and `Sum()` could use atomics for the count and sum to avoid locking, but the sprint plan's risk section says "histograms use mutex only for percentile calculation". Here `Mean`, `Sum`, `Count` also lock `h.mu`. This is a slight deviation but not a bug. However, `Count()` and `Sum()` are called by `WritePrometheus` while holding `r.mu`. So `WritePrometheus` holds `r.mu`, then calls `h.Count()` which locks `h.mu`. If another goroutine is in `Observe` holding `h.mu` and somehow tries to acquire `r.mu` (which it doesn't), no deadlock. So safe.

Potential Issue 12: In `internal/metrics/metrics_test.go` `TestConcurrentCounterAndHistogram`, it checks `r.Histogram("lat").Count()` and `r.Counter("hits").Value()` after `wg.Wait()`. This is fine.

Potential Issue 13: In `cmd/atcr/review.go`, the call to `writeReviewSummary` happens after `if err != nil { return err }` (all-agents-failed). So if all agents failed, the summary is still printed. That's probably intended. But the summary says "Agents: X/Y succeeded". If all failed, X=0. That's fine.

Wait, let's re-read the `runReview` function in `cmd/atcr/review.go`:
```go
if err != nil {
    return err // all-agents-failed → exit 1, artifacts preserved
}

// End-of-review metrics summary...
if result != nil {
    writeReviewSummary(...)
}
```
So if `err != nil` (all agents failed), it returns BEFORE printing the summary. Is that intended? The sprint plan says: "At the end of a CLI review, a summary is printed with duration, agent success count, and finding count." If all agents failed, is it still "end of review"? Yes, the review ended, but the function returns early. However, the summary might still be useful. But wait, the sprint plan AC3 says "At the end of a CLI review, a summary is printed". This probably means every review. But the current code prints it only when `err == nil` (not all-agents-failed). Wait, is `err != nil` only for all-agents-failed? The comment says so. But there might be other errors after that. The summary is printed before reconcile/verify output. If all agents failed, the summary is skipped. Is this a bug? It's arguably a missing summary in a failure case. But the sprint plan scope says "Summary printed at end of CLI review." It doesn't explicitly say "only on success". However, the diff might be correct because `result` might be nil or incomplete if all agents failed. But the summary uses `result.Summary.Total` and registry values. If `result` is not nil, it works. The check `if result != nil` guards it.

But looking closely at the diff context:
```go
	if err != nil {
		return err // all-agents-failed → exit 1, artifacts preserved
	}

+	// End-of-review metrics summary...
+	if result != nil {
+		writeReviewSummary(...)
+	}
```
If `err != nil`, the function returns before the summary. So on total failure, no summary. Is this an issue? The sprint plan says "At the end of each review, print a summary". An all-agents-failed review is still a completed review. The metrics registry still holds data (agents failed, api calls made, etc.). Skipping the summary seems like a bug or at least a deviation from AC3. However, the comment says "Printed after the one-line status above and before any one-shot reconcile/verify output." The "one-line status above" might refer to a status log printed earlier. If all agents failed, that status is returned as error. Maybe the summary should still print? I'd flag this as a MEDIUM correctness issue because the summary is a key acceptance criterion and should probably print even when all agents fail, as long as `result` is available. But wait, if `err != nil`, `result` might still be non-nil (artifacts preserved). The code checks `if result != nil`. So moving the summary before the error check, or adding it in the error path too, would satisfy AC3 better. But is it a "real issue"? Yes, missing summary on failure path.

Actually, looking at the comment in the diff: "Printed after the one-line status above and before any one-shot reconcile/verify output." The "one-line status above" might be the error return? No, it's before the return. Let me look at the context above in `cmd/atcr/review.go` (not fully shown). There might be a log line. Regardless, skipping the summary on error means operators don't see the metrics for failed reviews, which is exactly when they need them most (to debug provider issues). So this is a valid finding.

Wait, but `result` is used. If `err != nil`, does `result` contain anything? The comment says "all-agents-failed → exit 1, artifacts preserved". So `result` might be non-nil but the function returns `err`. The summary would still be useful. I think this is a legitimate MEDIUM issue.

Let's look for other issues.

Potential Issue 14: `internal/fanout/engine.go` `invokeAgent`:
```go
metrics.Counter(metrics.NameAgentsTotal).Inc()
start := time.Now()
r := e.dispatchAgent(ctx, a)
metrics.Histogram(metrics.NameAgentDurationSeconds).Observe(time.Since(start).Seconds())
recordAgentOutcome(r)
return r
```
If `dispatchAgent` returns because context is cancelled (timeout), `time.Since(start)` measures until cancellation. That's correct. But `recordAgentOutcome` classifies based on `r.Status`. If status is `StatusTimeout`, it's counted as timed_out. Good.

But what about the `NameAgentsTotal` counter? It's incremented for every invocation, including slot-level fallbacks? The comment says "Slot-level fallbacks are separate invokeAgent calls and so are counted independently". Good.

Potential Issue 15: `internal/metrics/prometheus.go` `escapeLabelValue` only handles `\`, `"`, and `\n`. Prometheus text format also technically allows escaping other characters but only requires those three. This is fine.

Potential Issue 16: `internal/metrics/metrics.go` `Registry.Counter` and `Registry.Histogram` return unexported types. This is a Go API design issue. External packages in the same module can use them (since they are in the same module but different package? No, unexported type means the type name is not accessible outside the package. But methods on it are accessible. Wait, if `Counter` returns `*counter`, and `counter` is unexported, then code in package `main` (cmd/atcr) receives a value of type `*metrics.counter` (which it cannot name). It can call `.Value()`, `.Inc()`, etc. This is legal Go. Is it an issue? The sprint plan explicitly decided this. So no.

Potential Issue 17: `internal/metrics/prometheus.go` `WritePrometheus` uses `fmt.Fprintf` which can return errors, but they are ignored. However, writing to `strings.Builder` never returns an error. So `_, _` is fine.

Potential Issue 18: `cmd/atcr/review_summary.go` `writeReviewSummary` ignores `fmt.Fprintf` errors. Writing to `io.Writer` can fail (e.g., pipe broken). Ignoring errors might mean the summary is silently truncated. However, this is standard for CLI output. Not a strong issue.

Potential Issue 19: `internal/fanout/metrics.go` `recordFindingMetrics`:
```go
func recordFindingMetrics(findings []stream.Finding) {
    if len(findings) == 0 {
        return
    }
    metrics.Counter(metrics.NameFindingsTotal).Add(int64(len(findings)))
    for _, f := range findings {
        sev := stream.NormalizeSeverity(f.Severity)
        metrics.Counter(metrics.Key(metrics.NameFindingsBy