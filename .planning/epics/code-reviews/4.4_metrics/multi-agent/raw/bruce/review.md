<think>
Let me carefully analyze the diff and identify any issues, keeping in mind the scope constraint from the sprint plan.

The sprint plan covers Epic 4.4: Metrics and Observability. The scope includes:
- `internal/metrics` package
- fanout instrumentation
- CLI review instrumentation + end-of-review summary
- `atcr_metrics` MCP tool
- unit + integration tests

Let me look for issues in the relevant files:

1. **cmd/atcr/review.go** - CLI integration with metrics
2. **cmd/atcr/review_summary.go** - new file, end-of-review summary
3. **internal/metrics/metrics.go** - core metrics package
4. **internal/metrics/prometheus.go** - Prometheus export
5. **internal/metrics/names.go** - metric name constants
6. **internal/fanout/metrics.go** - fanout metrics
7. **internal/fanout/engine.go** - fanout engine changes
8. **internal/fanout/review.go** - review metrics
9. **internal/fanout/artifacts.go** - finding metrics
10. **internal/mcp/handlers.go** - metrics MCP tool
11. **internal/mcp/server.go** - tool registration
12. **internal/mcp/tools.go** - tool definitions

Let me trace through the code carefully:

**Issue 1: `name` shadowing in fanout/metrics.go**
The `name` parameter shadows... wait, looking again, the `recordFindingMetrics` function uses `sev` for the severity, but let me check more carefully. No, this looks fine.

**Issue 2: `recordAgentOutcome` API call counting**
Looking at `internal/fanout/metrics.go`:
```go
apiCalls := r.Turns
if apiCalls == 0 {
    apiCalls = 1 // single-shot/degraded path: one provider call, Turns unset
}
metrics.Counter(metrics.NameAPICallsTotal).Add(int64(apiCalls))
```

This counts API calls based on `r.Turns`. But what about retries? A tool-loop agent might retry failed turns. Looking at the engine code path, `Turns` reflects the number of completed turns. This seems okay for tracking what happened.

But wait - looking at the test:
```go
recordAgentOutcome(Result{Status: StatusTimeout, ToolCalls: 4, Turns: 3})                      // 3 API calls (tool loop)
```

When an agent times out, the call count includes incomplete turns. This might be intended since the API was called.

**Issue 3: `recordReviewOutcome` ordering in review.go**
Looking at the diff for `internal/fanout/review.go`:
- Line 392: `recordReviewOutcome(interrupted, true)` - records failed
- Line 408: `recordReviewOutcome(interrupted, true)` - records failed
- Line 417: `recordReviewOutcome(interrupted, true)` - records failed
- Line 420: `recordReviewOutcome(interrupted, false)` - records succeeded

The `recordReviewOutcome(interrupted, true)` calls could record `interrupted` for an interrupted review, which seems correct.

But wait - looking at `recordReviewOutcome`:
```go
func recordReviewOutcome(interrupted, failed bool) {
    switch {
    case interrupted:
        metrics.Counter(metrics.NameReviewsInterrupted).Inc()
    case failed:
        metrics.Counter(metrics.NameReviewsFailed).Inc()
    default:
        metrics.Counter(metrics.NameReviewsSucceeded).Inc()
    }
}
```

This records only one of three states. The call at line 392 has `interrupted` potentially true. The calls at 408, 417 use `interrupted` too. But wait - is `interrupted` reset after the first check? Let me look more carefully at the original code.

Looking at the diff in review.go, around the area:
```go
if canceled := errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded); canceled {
    // ... interrupted logic
    if err != nil {
        // ... write manifest
        recordReviewOutcome(interrupted, true)
        return nil, err
    }
}
```

Hmm, the `interrupted` variable's lifetime is unclear without seeing the full function. But the pattern is consistent with the existing code's `interrupted` variable.

**Issue 4: `WritePool` calls `recordFindingMetrics` but only after merge**
Looking at `internal/fanout/artifacts.go`:
```go
// Findings metrics (Epic 4.4): count the raw findings the agents emitted, in
// total and by severity, before they are merged to disk.
recordFindingMetrics(merged)
```

This is after `statuses` is computed. The `merged` variable contains the merged findings. Looking at the surrounding code, this seems fine.

But wait - in the test `TestWritePoolRecordsFindingMetrics`, it uses `okResult` helper which appears to be a test helper. The test verifies that findings are counted correctly.

**Issue 5: Histogram `Percentile` clamping**
Looking at `histogram.Percentile`:
```go
p = min(max(p, 0), 100)
```

The `min` and `max` builtins work with `float64` since Go 1.21. This is fine for the codebase which uses Go 1.21+.

**Issue 6: Histogram `Observe` ring buffer**
The ring buffer implementation:
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

**Issue 7: `formatFloat` for percentages**
The Prometheus format spec wants `0.5` to be rendered as `0.5` for quantiles. `strconv.FormatFloat(0.5, 'g', -1, 64)` should give "0.5". This looks correct.

**Issue 8: Review summary in review.go**
Looking at the `writeReviewSummary` call:
```go
if result != nil {
    writeReviewSummary(cmd.OutOrStdout(), metrics.DefaultRegistry, time.Since(now), result.Summary.Total)
}
```

The `now` variable must be the start of the review. Let me check what's around that. This is added after `return err // all-agents-failed → exit 1, artifacts preserved`. So this is only printed on the success path or when `err` is non-nil? Wait - the line says `return err` so if `err` is non-nil, the function returns and never reaches the summary. But if `err` is nil, execution continues. The summary is only printed on the non-error path. But what about the failed case where the caller gets an error?

Wait - looking more carefully:
```go
return err // all-agents-failed → exit 1, artifacts preserved
```

This returns `err`. The `//` comment says "all-agents-failed → exit 1, artifacts preserved". So this line returns when there's an error. The code below would only execute when the previous line is NOT reached (i.e., err is nil at that point).

But the new code says:
```go
if result != nil {
    writeReviewSummary(...)
}
```

This suggests that `result` could be nil even on the success path. Hmm. Looking at the original code in review.go, there's likely a `result` variable that's set before this point. If the result is nil but we got here, we'd skip the summary. But should we print the summary even when the review failed? Looking at the spec:
- AC3: "At the end of a CLI review, a summary is printed with duration, agent success count, and finding count."

It seems like the summary should be printed even on failure. But the new code only prints it if `result != nil`. Let me check if this is an issue.

Actually, looking again - if the `return err` is reached, the function exits. The new code only runs if execution falls through. So `result` should be non-nil in the success case. But what if there's an edge case where result is nil but we proceed?

This might be a minor issue but the result should be non-nil here. Let me check if this is worth flagging.

**Issue 9: `recordFindingMetrics` called from `WritePool` but findings already in `merged`**
Looking at artifacts.go more carefully - the findings passed to `recordFindingMetrics(merged)` are the merged findings. But the metric is "raw findings emitted by agents". This counts the merged set, not the raw pre-merge set. If findings are deduplicated, the metric would undercount. But the comment in the code says "Raw per-agent findings (the metric definition is 'emitted by agents')". 

Looking at the test `TestWritePoolRecordsFindingMetrics`, it uses two agents each emitting findings, and the test expects the total count. So in the test, the findings from each agent are kept distinct. But in production, the findings could be merged/deduplicated before this call.

Let me look at the `WritePool` function more carefully... Actually, I can see from the test name "TestWritePoolRecordsFindingMetrics" and the input data that this is testing the behavior with multiple agents emitting the same severity. The test expects 2 HIGH findings to be counted.

**Issue 10: Potential issue with severity normalization**
In `recordFindingMetrics`:
```go
sev := stream.NormalizeSeverity(f.Severity)
```

What if `stream.NormalizeSeverity` returns an empty string for an unknown severity? Then the counter key would be `atcr_findings_by_severity{severity=""}`. This could lead to a metric with an empty severity label. Not sure if this is a real concern, but worth checking.

**Issue 11: Security issue with label value**
The `escapeLabelValue` function handles the Prometheus escape sequences. But what if the label value contains a null byte or other control characters? Let me check the Prometheus spec... The escapes listed (backslash, double-quote, newline) are the standard ones. This is probably sufficient.

**Issue 12: Race condition in `WritePrometheus`**
Looking at the histogram section of `WritePrometheus`:
```go
for _, k := range keys {
    h := r.histograms[k]
    _, inner := splitLabels(k)
    for _, q := range summaryQuantiles {
        fmt.Fprintf(&b, "%s%s %s\n", fam, withQuantile(inner, q), formatFloat(h.Percentile(q*100)))
    }
    fmt.Fprintf(&b, "%s_sum%s %s\n", fam, labelSuffix(inner), formatFloat(h.Sum()))
    fmt.Fprintf(&b, "%s_count%s %d\n", fam, labelSuffix(inner), h.Count())
}
```

The `r.mu` is held during this, so the histograms map isn't changing. But the histogram methods (Percentile, Sum, Count) take their own mutex. This is fine.

**Issue 13: Review metrics ordering**
In `ExecuteReview`, the review duration histogram is deferred:
```go
reviewStart := time.Now()
defer func() {
    metrics.Histogram(metrics.NameReviewDurationSeconds).Observe(time.Since(reviewStart).Seconds())
}()
```

The deferred function always runs, even when panicking. Good.

**Issue 14: `recordReviewOutcome` called multiple times potentially**
Looking at the code paths in `ExecuteReview`:
- After WriteManifest failure: `recordReviewOutcome(interrupted, true)` 
- After Outcome returns error: `recordReviewOutcome(interrupted, true)` 
- At the end (success): `recordReviewOutcome(interrupted, false)`

Could there be a path where multiple `recordReviewOutcome` calls happen? Looking at the function, each path has a return, so only one should execute. But the `interrupted` variable could change between calls. Let me trace through more carefully.

Actually looking at the diff, the interrupted variable is used in three places. In the first one, it's set to true if a cancellation occurred. In the later paths, it might still be true. The `recordReviewOutcome` correctly prioritizes `interrupted` over `failed`.

But wait - in the first path, `interrupted` is set to `true` (from the diff context), and then the function returns with `recordReviewOutcome(interrupted, true)`. In the second path, the variable `interrupted` should be from the outer scope. If the manifest write fails but the review wasn't interrupted, the recordReviewOutcome would call with `interrupted=false, failed=true` which records `failed`. This is correct.

**Issue 15: Summary printed twice?**
Wait - looking at the code, after `result != nil` and `writeReviewSummary`, there's more code. The `// One-shot mode: reconcile in-process...` comment indicates more processing happens. Is the summary printed before the reconcile? The comment says "Printed after the one-line status above and before any one-shot reconcile/verify output."

Let me look at the test for this... Actually, I don't see this in the diff for tests. The summary helper has its own test. The placement in `runReview` is correct based on the comment.

**Issue 16: `result.Summary.Total` is the total agents, not the total succeeded**
Looking at `writeReviewSummary`:
```go
fmt.Fprintf(w, "Agents: %d/%d succeeded, %d failed, %d timed out\n",
    val(metrics.NameAgentsSucceeded), totalAgents,
    val(metrics.NameAgentsFailed), val(metrics.NameAgentsTimedOut))
```

`val(metrics.NameAgentsSucceeded)` reads the metric, but `totalAgents` is `result.Summary.Total`. Are these the same? Let me check the Summary struct... it should have Total and Succeeded fields. The metric `atcr_agents_succeeded` counts from the fanout, while `result.Summary.Total` is the total agents. These should match if all agents completed.

But what about interrupted reviews or failed reviews where not all agents completed? The metric might be lower than the total. This is a potential inconsistency.

**Issue 17: `recordFindingMetrics` only counts non-zero severities**
Looking at the code:
```go
if len(findings) == 0 {
    return
}
metrics.Counter(metrics.NameFindingsTotal).Add(int64(len(findings)))
for _, f := range findings {
    sev := stream.NormalizeSeverity(f.Severity)
    metrics.Counter(metrics.Key(metrics.NameFindingsBySeverity, metrics.LabelSeverity, sev)).Inc()
}
```

This only counts findings with non-nil severity. The test doesn't exercise the case of findings with unknown severity. Minor concern.

**Issue 18: `review_summary_test.go` missing case for no agents**
The test `TestWriteReviewSummaryNoFindings` uses `time.Second` and `3` agents. But all counters are 0. The expected output is "Findings: 0\n". But the format is "Findings: %d%s\n" with severityBreakdown returning "" when no findings. So the output should be "Findings: 0\n". This matches the test.

**Issue 19: Histogram percentile test for empty**
`TestHistogramObservePercentileMean`:
```go
if got := h.Percentile(50); got != 0 {
    t.Fatalf("empty Percentile(50) = %v, want 0", got)
}
```

This tests the empty case. The implementation:
```go
if len(h.values) == 0 {
    return 0
}
```

Wait, this checks `len(h.values) == 0` but if the histogram has been observed and the values have been overwritten, the values array is non-empty. But what if `count` is 0 and values is non-empty (which shouldn't happen)? Actually, if the buffer is full and we wrap, values is always full. So this check is safe.

But what about edge case: `h.full` is true, `len(h.values)` is `maxHistogramSamples` (not 0). So this check works.

But wait - what if we observed values that are all the same? `Percentile(50)` returns the same value. That's expected.

**Issue 20: Histogram percentile implementation for full ring**
The ring buffer implementation looks correct, but let me trace through:
- Start: values is empty, full is false
- After 10000 observations: values is full (10000 elements), full is true, next is 0
- Observation 10001: values[0] is overwritten, next becomes 1
- After 10005 observations: the oldest 5 are lost, the most recent 10000 are retained

For percentile calculation, we sort the values array. The ring order doesn't matter for percentile (it's a multiset, not a sequence). So this is correct.

**Issue 21: `WritePrometheus` after Reset**
`TestRegistryReset`:
```go
r.Reset()
if got := r.Counter("c").Value(); got != 0 {
```

After Reset, the counters map is replaced. New Counter calls create fresh counters. This is correct.

**Issue 22: `summaryQuantiles` for histogram in prometheus output**
The quantiles are `0.5, 0.9, 0.95, 0.99`. These are reasonable defaults.

But wait - there's a subtle issue with `formatFloat` for these values:
- `0.5` → `"0.5"` (correct)
- `0.9` → `"0.9"` (correct)
- `0.95` → `"0.95"` (correct)
- `0.99` → `"0.99"` (correct)

Looks good.

**Issue 23: Histogram with NaN values**
If someone calls `h.Observe(math.NaN())`, the sum becomes NaN, and the percentile calculation would include NaN values. Sorting a slice with NaN is undefined behavior. This is an edge case not tested, but worth noting.

**Issue 24: Histogram race condition in `Observe`**
The `Observe` method takes `h.mu` and updates `h.sum`, `h.count`, and `h.values`. This is properly synchronized. No race condition.

**Issue 25: `recordAgentOutcome` in the dispatch path**
The metrics are recorded AFTER the dispatch:
```go
r := e.dispatchAgent(ctx, a)
metrics.Histogram(metrics.NameAgentDurationSeconds).Observe(time.Since(start).Seconds())
recordAgentOutcome(r)
return r
```

If `dispatchAgent` panics, neither the histogram nor the outcome is recorded. The `defer` was removed. This is a behavioral change from the original code (where the metrics were inline, not deferred). Actually wait, in the original diff, the code was inline, not deferred. Let me re-read:

```go
metrics.Counter(metrics.NameAgentsTotal).Inc()
start := time.Now()
r := e.dispatchAgent(ctx, a)
metrics.Histogram(metrics.NameAgentDurationSeconds).Observe(time.Since(start).Seconds())
recordAgentOutcome(r)
return r
```

So the histogram observation and outcome recording are NOT deferred. If `dispatchAgent` panics, these are skipped. This is acceptable for a panic case.

**Issue 26: `WritePool` recording findings before write**
The `recordFindingMetrics(merged)` is called BEFORE the findings are written to disk. This is fine because the findings are already in memory. But what if the writing fails after the metrics are recorded? The metrics would be recorded but the findings wouldn't be persisted. This is a minor inconsistency but not necessarily a bug.

**Issue 27: `TestRunReviewRecordsReviewMetrics` requires external setup**
This test requires `initRepo`, `mockProvider`, `twoAgentConfig`, `reviewReq` helpers that aren't shown in the diff. These should exist in the test file already.

**Issue 28: `TestEngineRecordsAgentMetrics` requires `newFake`, `agentSlot`**
Similar to above. These should exist.

**Issue 29: `executeReview` interrupt logic**
The `interrupted` variable's initialization. Let me look at the original code more carefully... The diff shows:
```go
if canceled := errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded); canceled {
    if err != nil {
        // ...
        recordReviewOutcome(interrupted, true)
        return nil, err
    }
}
```

But where is `interrupted` declared? Looking at the diff context, `interrupted` must be a variable in the function scope, likely declared before this `if` block. The diff doesn't show its declaration but uses it. The compilation should fail if it's not declared.

**Issue 30: Test for severity breakdown ordering**
The test verifies severities are in CRITICAL, HIGH, MEDIUM, LOW order. The `severityOrder` variable is defined in the same order. The test creates only HIGH and MEDIUM severities and expects " (2 HIGH, 3 MEDIUM)" - this matches.

**Issue 31: `writeReviewSummary` doesn't print duration when total is 0**
If `result.Summary.Total` is 0, the output is "Agents: 0/0 succeeded, ...". This is fine.

**Issue 32: `EscapeLabelValue` for invalid UTF-8**
Prometheus labels should be valid UTF-8. If a value contains invalid UTF-8, the output might be malformed. Not tested.

**Issue 33: `name` parameter shadowing in `Key`**
The function `Key(name, label, value string)` - the parameter name is `name`. This shadows the built-in... no, Go doesn't have a `name` built-in. This is fine.

**Issue 34: `Engine` has `(e *Engine)` receiver**
Looking at the dispatch function:
```go
func (e *Engine) dispatchAgent(ctx context.Context, a Agent) Result {
```

The receiver `e` is used. Good.

**Issue 35: Histogram values are not trimmed when full**
The ring buffer overwrites old values. This is the intended behavior. No issue.

OK let me focus on what's actually likely to be wrong:

1. **Major: `recordReviewOutcome` is called with `interrupted` variable that may not be initialized at the function entry, but used in the deferred histogram observation. Wait, no - the `interrupted` is a local variable in the function. Let me look again.

Actually I don't see the `interrupted` variable declaration in the diff. The diff only shows additions. The original function likely declares `interrupted := false` or similar at the top. So this is fine.

2. **Possible issue: `recordFindingMetrics` is called even when `len(findings) == 0` early return.**
Wait, looking at the code:
```go
func recordFindingMetrics(findings []stream.Finding) {
    if len(findings) == 0 {
        return
    }
    ...
}
```

This is fine. The early return prevents unnecessary work.

3. **Possible issue: `recordReviewOutcome` called in `ExecuteReview` for every error path - but might be skipped if WriteManifest panics.**
The metrics are not deferred, so if anything panics, they're skipped. This is consistent with the design.

4. **The metrics `atcr_agents_succeeded` and `result.Summary.Total` could be inconsistent in error cases.**
This is a real concern. The summary in `writeReviewSummary` reads from the metric registry, which is the global state. If the fanout reports `atcr_agents_succeeded=8` but `result.Summary.Total=10`, the display would be "8/10 succeeded". This is fine if we trust both numbers are correct.

5. **`review.go` summary placement:**
The new code is placed after the early return for `err`:
```go
return err // all-agents-failed → exit 1, artifacts preserved
}

// New code:
if result != nil {
    writeReviewSummary(...)
}
```

Wait - the new code is placed AFTER the `return err` line, so it only executes when `err == nil`. But what if `err != nil` AND the user still wants a summary? Looking at the comment "exit 1, artifacts preserved" - on failure, the function exits with status 1. The summary is for the success case only.

But the metric `atcr_reviews_succeeded` vs `atcr_reviews_failed` is recorded in `ExecuteReview`. On failure, `atcr_reviews_failed` is incremented. The CLI summary only prints on success. This is a deliberate design choice based on the AC3 wording.

6. **Histogram `Mean` could be misleading when count overflows int64**
`h.sum / float64(h.count)` - if count overflows int64, this gives wrong result. But 10,000 samples per histogram (the cap) means count is at most 10,000 per observation site. Multiple invocations over a long-lived process could accumulate, but unlikely to overflow int64.

7. **`recordReviewOutcome` doesn't validate input - what if both interrupted and failed are false?**
```go
switch {
case interrupted:
    metrics.Counter(metrics.NameReviewsInterrupted).Inc()
case failed:
    metrics.Counter(metrics.NameReviewsFailed).Inc()
default:
    metrics.Counter(metrics.NameReviewsSucceeded).Inc()
}
```

If both are false, `atcr_reviews_succeeded` is incremented. This is the correct behavior.

8. **`recordReviewOutcome` is called in error paths but `interrupted` could be `true` even if the error isn't an interrupt error**
This is intentional - the `interrupted` variable tracks whether the context was cancelled. If so, the review is considered interrupted regardless of the specific error.

9. **Test concern: `TestRunReviewRecordsReviewMetrics` may fail if it requires `initRepo`, `mockProvider`, etc., to be available.**
These helpers are likely defined elsewhere. The test relies on them. This is fine if the helpers exist.

10. **`internal/boundaries_test.go` changes are just allowlist updates - this is correct for the new dependencies.**

11. **Histogram percentile issue with rounding**
The `ceilDiv` function:
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

For p=50, n=5: x = 0.5 * 5 = 2.5, t = 2.0, x > t, return 3.0
Then `rank := int(3.0) = 3`. This is correct for nearest-rank percentile.

For p=100, n=5: x = 1.0 * 5 = 5.0, t = 5.0, x == t, return 5.0
rank = 5, returns sorted[4] which is the 5th value. Correct.

For p=0, n=5: x = 0 * 5 = 0, t = 0, x == t, return 0
rank = max(0, 1) = 1, returns sorted[0]. Correct.

For p=50, n=2: x = 0.5 * 2 = 1.0, t = 1.0, x == t, return 1.0
rank = 1, returns sorted[0]. But the median of 2 values is between sorted[0] and sorted[1]. With nearest-rank, the 50th percentile of 2 values is the 1st value. This is the "nearest-rank" definition. Correct.

12. **Test: `TestHistogramBoundedSamples` accesses `h.values` directly**
```go
if got := len(h.values); got != maxHistogramSamples {
```

This accesses the unexported field directly. Since the test is in the same package, this is allowed. The test verifies the cap is enforced.

OK let me think about what's actually problematic:

Looking at the changes to `cmd/atcr/review.go`:
```go
if result != nil {
    writeReviewSummary(cmd.OutOrStdout(), metrics.DefaultRegistry, time.Since(now), result.Summary.Total)
}
```

Wait, what if `result` is nil? The function continues to the reconcile logic below. Looking at the code, `result` is declared earlier as `result, err := ExecuteReview(...)` or similar. If `err` is non-nil, the function returns. So when we reach the new code, `err` is nil and `result` should be non-nil. The `if result != nil` check is defensive but probably unnecessary.

Actually, this is a concern. If `result` is nil but err is nil (shouldn't happen, but if it did), the summary is skipped. This is a code smell. Should probably remove the nil check or handle the nil case.

Let me look at the actual function structure. Without seeing the full function, I'll trust the diff. The `if result != nil` is defensive.

Now let me look at the WritePool test more carefully:
```go
pool := filepath.Join(t.TempDir(), "pool")
results := []Result{
    okResult("greta", "CRITICAL|auth.go:42|Token|Fix|security|15|ev\nHIGH|main.go:88|Leak|Fix|concurrency|30|ev"),
    okResult("kai", "HIGH|x.go:1|p|f|cat|10|ev"),
}
_, err := WritePool(pool, results)
require.NoError(t, err)

check("atcr_findings_total", 3) // 2 from greta + 1 from kai
check(metrics.Key("atcr_findings_by_severity", "severity", "HIGH"), 2) // 1 from greta + 1 from kai
check(metrics.Key("atcr_findings_by_severity", "severity", "CRITICAL"), 1) // 1 from greta
```

This test relies on `okResult` helper existing and creating the right Result structure. The test depends on test helpers.

**Potential issue: The `recordFindingMetrics` is called before the WritePool write, but if the write fails, the metrics were already incremented.** Wait, no, looking at the code:
```go
// Findings metrics (Epic 4.4): count the raw findings the agents emitted, in
// total and by severity, before they are merged to disk.
recordFindingMetrics(merged)

// Merged pool findings (8-col, REVIEWER per row) for downstream convenience.
if err := writeFindings(filepath.Join(poolDir, findingsFile), merged); err != nil {
    return Summary{}, err
}
```

The metrics are recorded BEFORE the write. If the write fails, the function returns the error and the metrics are still incremented. This is a minor inconsistency - if the findings are not persisted, the metrics still report them.

Is this a real bug? The metrics are about "what agents emitted" - the actual number of findings the agents produced. The persistence is a separate concern. So the metrics should be correct regardless of whether persistence succeeds.

OK let me look at one more thing - the `RecordReviewOutcome` test:
```go
func TestRecordReviewOutcome(t *testing.T) {
    metrics.DefaultRegistry.Reset()
    t.Cleanup(metrics.DefaultRegistry.Reset)

    recordReviewOutcome(false, false) // success
    recordReviewOutcome(false, true)  // failed
    recordReviewOutcome(true, true)   // interrupted wins over failed
    ...
}
```

The test verifies the switch precedence. Correct.

Now let me look at the `WritePrometheus` again for any issues:

```go
histFamilies := make(map[string][]string, len(r.histograms))
for k := range r.histograms {
    f := metricFamily(k)
    histFamilies[f] = append(histFamilies[f], k)
}
for _, fam := range sortedKeys(histFamilies) {
    fmt.Fprintf(&b, "# TYPE %s summary\n", fam)
    keys := histFamilies[fam]
    sort.Strings(keys)
    for _, k := range keys {
        h := r.histograms[k]
        _, inner := splitLabels(k)
        for _, q := range summaryQuantiles {
            fmt.Fprintf(&b, "%s%s %s\n", fam, withQuantile(inner, q), formatFloat(h.Percentile(q*100)))
        }
        fmt.Fprintf(&b, "%s_sum%s %s\n", fam, labelSuffix(inner), formatFloat(h.Sum()))
        fmt.Fprintf(&b, "%s_count%s %d\n", fam, labelSuffix(inner), h.Count())
    }
}
```

Wait - the quantile lines use `fam` instead of `k`. This is correct for Prometheus format - the family name is used, and labels are appended. For an unlabeled histogram, `fam == k`, so the output is the same. For a labeled histogram, `fam` is just the family name without labels, and `withQuantile(inner, q)` adds the labels.

Looking at `withQuantile`:
```go
func withQuantile(inner string, q float64) string {
    if inner == "" {
        return fmt.Sprintf(`{quantile="%s"}`, formatFloat(q))
    }
    return fmt.Sprintf(`{%s,quantile="%s"}`, inner, formatFloat(q))
}
```

For `inner = `persona="skeptic"``, the output is `{persona="skeptic",quantile="0.5"}`. The full line is:
`atcr_agent_duration_seconds{persona="skeptic",quantile="0.5"} <value>`

This is correct Prometheus format.

OK I think I've analyzed enough. Let me think about the actual issues:

1. **The summary is only printed on success** - this is per AC3 wording, not necessarily a bug.
2. **Severity normalization could produce empty string** - edge case, minor.
3. **`recordFindingMetrics` called even if persistence fails** - minor inconsistency.
4. **`recordReviewOutcome` is called with `interrupted` variable that may be in unexpected state** - depends on function structure.

Looking again at the histogram with NaN - if `Observe(math.NaN())` is called, the histogram becomes corrupt. Should add a guard.

Looking at the test for `TestHistogramBoundedSamples`:
```go
const n = maxHistogramSamples + 5
for i := 0; i < n; i++ {
    h.Observe(float64(i))
}
```

This observes 10005 values. The cap is 10000, so the last 5 values should be retained. The test checks `Percentile(100) == n-1 = 10004`, which is the last value. This is correct.

But wait - the implementation overwrites `values[h.next]` and increments `next`. After 5 overwrites, the values array contains 0..10004 in some ring order. The max is still 10004. So `Percentile(100)` returns 10004. Correct.

Let me also check the histogram for the case where `p` is invalid (like exactly 0.5 for even N). The ceilDiv calculation:
- p=50, n=10: x = 0.5 * 10 = 5.0, return 5.0, rank = 5, returns sorted[4]
- p=50, n=5: x = 0.5 * 5 = 2.5, return 3.0, rank = 3, returns sorted[2]

For n=10, the median is typically the average of sorted[4] and sorted[5]. With nearest-rank, we return sorted[4]. This is the "lower median" convention. Different libraries use different conventions. Not necessarily a bug, but worth noting.

Looking at the `formatFloat` function:
```go
func formatFloat(f float64) string {
    return strconv.FormatFloat(f, 'g', -1, 64)
}
```

For `0.5`, this gives "0.5". For `0.9`, this gives "0.9". Good.

But for `1.0`, this gives "1". For `0.95`, this gives "0.95". Good.

What about `1e-7`? It would give "1e-07". Is this valid Prometheus? Yes, scientific notation is valid.

OK I think I've analyzed enough. Let me write the findings.

Actually, let me look at one more potential issue: the `WriteReviewSummary` function. The `totalAgents` parameter is `result.Summary.Total`. But the actual count from the metric is `atcr_agents_succeeded`. The total agents might be more than the succeeded ones. The display is "X/Y succeeded" which is the count of succeeded over total. This is correct.

But what if `result.Summary.Total` includes agents that were never invoked (e.g., roster that couldn't be scheduled)? Then the count would be wrong. The metric is the source of truth for what was actually invoked.

Hmm, looking at the comment in `writeReviewSummary`:
```go
// writeReviewSummary prints the end-of-review metrics summary (Epic 4.4 AC3):
// duration, agent success/failure/timeout counts, API calls, and findings with a
// severity breakdown. It reads the counts from reg — in production the
// process-global DefaultRegistry, which (a CLI process runs exactly one review)
// holds this review's totals — so the summary is a direct readout of the same
// metrics the MCP server exports. reg is a parameter so the helper is unit
// testable against a seeded registry.
```

The summary reads from the registry. The totalAgents parameter is just for display. The actual counts come from the metrics.

OK let me focus on the most actionable issues:

1. The summary is only printed on success - this matches AC3 but might be surprising on failure.
2. No tests for NaN handling in histogram
3. Severity normalization edge case

Let me also think about the metrics package from a security perspective:
- Label values are escaped, so injection is prevented. ✓
- Counters are atomic. ✓
- Histograms have bounded memory. ✓

Performance concerns:
- `Write
</think>
