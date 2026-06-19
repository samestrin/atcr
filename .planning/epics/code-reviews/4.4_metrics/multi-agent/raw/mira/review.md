<think>
Let me carefully review this diff for the Epic 4.4 metrics and observability work. I need to focus only on issues in files/areas directly related to the sprint plan's stated scope.

In-scope files:
- `cmd/atcr/review.go` - review-level metrics
- `cmd/atcr/review_summary.go` - new file
- `cmd/atcr/review_summary_test.go` - new file
- `internal/metrics/metrics.go` - new file
- `internal/metrics/metrics_test.go` - new file
- `internal/metrics/names.go` - new file
- `internal/metrics/prometheus.go` - new file
- `internal/metrics/prometheus_test.go` - new file
- `internal/fanout/metrics.go` - new file
- `internal/fanout/metrics_review_test.go` - new file
- `internal/fanout/engine_metrics_test.go` - new file
- `internal/fanout/engine.go` - instrumented
- `internal/fanout/review.go` - instrumented
- `internal/fanout/artifacts.go` - recordFindingMetrics call
- `internal/mcp/handlers.go` - handleMetrics
- `internal/mcp/server.go` - tool registration
- `internal/mcp/tools.go` - tool definitions
- `internal/mcp/metrics_test.go` - new file
- `internal/boundaries_test.go` - updated imports

Let me look for issues:

1. **internal/metrics/metrics.go - Percentile calculation**: 
   - The `ceilDiv` function takes `p, n float64` and computes `p / 100 * n`. For p=0, this gives 0, so `ceilDiv` returns 0, then `min(max(0, 1), len)` becomes 1, which is correct.
   - For p=100, n=5: `100/100 * 5 = 5.0`, `int64(5.0) = 5`, no fractional part, returns 5. Then `min(max(5, 1), 5) = 5`, which is the max. Correct.
   - For p=50, n=5: `50/100 * 5 = 2.5`, `int64(2.5) = 2`, 2.5 > 2, returns 3. Then sorted[2] = 3. Correct for nearest-rank.
   - Actually, looking at this more carefully — for the test case with values [5, 1, 4, 2, 3] sorted as [1, 2, 3, 4, 5]:
     - p=0: ceilDiv(0, 5) = 0, max(0, 1) = 1, sorted[0] = 1 ✓
     - p=50: ceilDiv(50, 5) = ceil(2.5) = 3, sorted[2] = 3 ✓
     - p=100: ceilDiv(100, 5) = 5, sorted[4] = 5 ✓
   - Looks correct.

2. **Histogram sorted percentile copy**: The `Percentile` function makes a copy of values and sorts them, which is O(N log N) per call. The renderer's quantiles are called multiple times per histogram per scrape. This could be inefficient. But this is for the Prometheus export, not the hot path. Acceptable.

3. **internal/metrics/metrics.go line 67 - `Observe` while holding mutex**:
   - The histogram mutex is held during `append`, which could be slow but acceptable.

4. **cmd/atcr/review.go line 221-226 - `writeReviewSummary` placement**:
   - Looking at the code, `writeReviewSummary` is called BEFORE the one-shot reconcile/verify, and after the one-line status. The comment says "after the one-line status above and before any one-shot reconcile/verify output". The sprint plan says the summary should be printed at the end of each review. 
   - Looking at the placement, the `if result != nil` is checking that result is not nil, but `result` is the variable name and could be nil. The summary uses `result.Summary.Total`, which is dereferenced, so if result is nil, this would panic. But it's gated by `if result != nil`. OK.
   - However, looking at the metrics — `metrics.DefaultRegistry` is used inside `writeReviewSummary`, and metrics are also recorded by `recordReviewOutcome` inside `fanout.ExecuteReview` (which was called earlier). So the metrics should be populated at this point.
   - Wait, but what if the review happened via MCP server (where `atcr_metrics` tool is exposed)? The MCP server accumulates metrics across reviews. The CLI prints a summary based on the global registry. In CLI mode, only one review runs, so this works fine.

5. **cmd/atcr/review.go - Where is `now` defined?**:
   - The diff shows `time.Since(now)` but I don't see `now := time.Now()` in the diff. It's in the existing code (pre-existing variable).
   - The summary uses `time.Since(now)` but the histogram observation in `fanout.ExecuteReview` uses `time.Since(reviewStart)`. These are different start times! The summary uses the CLI-level start, and the histogram uses the `ExecuteReview` start. This is a minor inconsistency but not a bug per se.

6. **internal/fanout/engine.go - dispatchAgent**:
   - The split from `invokeAgent` to `dispatchAgent` is clean. The metrics wrapper spans all paths uniformly. Good.

7. **internal/fanout/metrics.go - recordAgentOutcome**:
   - `apiCalls := r.Turns; if apiCalls == 0 { apiCalls = 1 }` — But what if `r.Turns` is negative? It shouldn't be, but the check is just `== 0`.
   - What about when `r.Status` is something else (not OK, Failed, or Timeout)? The switch falls through and no agent outcome counter is incremented. But the API calls are still counted. This is actually correct because the API call happens regardless of outcome, but the agent outcome counter requires a known status.
   - Wait, looking at the Result type, are there other Status values? Let me think... `StatusOK`, `StatusFailed`, `StatusTimeout`. Those are the only ones I see in the code.

8. **internal/metrics/prometheus.go - escaping**:
   - The escape function handles `\\`, `"`, and `\n`. This is good. But Prometheus also requires escaping `\n` in label values, which is done.

9. **Race condition in `internal/metrics/metrics.go`**:
   - In `Percentile`, the mutex is held during the copy and sort. This is intentional and correct.

10. **Test isolation**:
    - The test files use `metrics.DefaultRegistry.Reset()` with `t.Cleanup(metrics.DefaultRegistry.Reset)`. This is the standard pattern. Good.

11. **cmd/atcr/review.go - `writeReviewSummary` only when `result != nil`**:
    - What if result is nil? The check is `if result != nil`. This is defensive. But looking at the function flow, `result` is returned by `ExecuteReview` and could be nil only if an error is returned. But the code checks `if err != nil { return err }` before reaching this point. So `result` should never be nil here. The `if result != nil` check is overly defensive, but not a bug.

12. **internal/fanout/review.go - recordReviewOutcome**:
    - `recordReviewOutcome(interrupted, true)` is called in three places (error paths). The interrupt is also passed, so an interrupted error path correctly increments `atcr_reviews_interrupted`.
    - At the end, `recordReviewOutcome(interrupted, false)` is called when all succeeded. But what if `interrupted` is true here? Then it would be counted as interrupted, which is correct.
    - The default case (no failure, no interrupt) increments `atcr_reviews_succeeded`. This is correct.

13. **internal/metrics/prometheus.go - summary format**:
    - Prometheus summaries use the format `<basename>{quantile="x"} value`. The renderer does `fam + withQuantile(inner, q)`. For an unlabeled key, it produces `atcr_review_duration_seconds{quantile="0.5"} 1.5`. For a labeled key, it produces `atcr_agent_duration_seconds{persona="skeptic",quantile="0.5"} 2`. This is correct.

14. **Test for `TestEngineRecordsAgentMetrics` - API call count**:
    - The test asserts `atcr_api_calls_total = 3` for three agents (one OK, one HTTP failure, one timeout). Each agent has `Turns = 0` (default for single-shot), so each gets `apiCalls = 1`. Total = 3. Correct.

15. **Test for `TestRecordAgentOutcome`**:
    - The test asserts `atcr_api_calls_total = 6` for 4 results: 1 + 1 + 3 (tool loop with 3 turns) + 1 = 6. Correct.

16. **`internal/fanout/artifacts.go` - recordFindingMetrics**:
    - Called once in `WritePool` with merged findings. This is in the right place.

17. **Issue: Finding severity normalization**:
    - `recordFindingMetrics` calls `stream.NormalizeSeverity(f.Severity)` to normalize the severity. This is then used as the label value. The label value should be a valid Prometheus label value. Normalized severities should be CRITICAL, HIGH, MEDIUM, LOW (uppercase). The label value escaping is done via `Key`, so even unnormalized values are escaped properly.
    - But what if a finding has a severity that normalizes to something unexpected? The escape function handles arbitrary strings.

18. **Issue: concurrent access to `DefaultRegistry` in tests**:
    - Tests in `engine_metrics_test.go` use `metrics.DefaultRegistry.Reset()` and `t.Cleanup(metrics.DefaultRegistry.Reset)`. These are not parallel tests, so this is fine. But if a test in the same file runs as `t.Parallel()`, this would be a race. The tests here are not parallel, so this is OK.

19. **`internal/metrics/prometheus.go` - `withQuantile` and label order**:
    - The format `persona="skeptic",quantile="0.5"` puts persona first, then quantile. This matches the test expectation. Good.

20. **Missing type assertions / nil checks**:
    - In `recordAgentOutcome`, `r.Status` is checked. If `r` itself is nil (the zero Result), all the checks would still work. But `r.Turns`, `r.ToolCalls`, `r.Err` would all be zero values, so no API call counting and no error counting. This is fine.

21. **Histogram percentile with float precision**:
    - `int(ceilDiv(p, float64(len(sorted))))` - this is a float-to-int conversion. For very large histograms, there could be precision issues, but at 10,000 samples, this is fine.

22. **Histogram mean**:
    - The mean is computed exactly using sum and count. Good.

23. **Counter atomicity**:
    - `atomic.Int64` is used. Good.

24. **Registry mutex**:
    - The registry holds one mutex for both maps. The lock is held only during map lookups. The actual metric operations (Inc, Add, Observe) happen outside this lock. Good design.

25. **`internal/mcp/handlers.go` - handleMetrics**:
    - Returns `nil, MetricsResult{...}, nil` - the first return is `*mcpsdk.CallToolResult` which is nil, the second is the actual result, the third is the error. This is the standard handler signature.

26. **Security concern: `handleMetrics` exposes all metrics**:
    - The sprint plan mentions: "Local-only: do not expose the server publicly." The description includes this warning. But there's no actual authentication on the metrics tool. This is documented as a known risk.

27. **`internal/mcp/tools.go` - `descMetrics`**:
    - The description is well-written. Good.

28. **Test for `TestHandleMetricsContextCancelled`**:
    - The test cancels the context and expects an error. The handler returns `ctx.Err()` which is `context.Canceled`. This works.

29. **`internal/fanout/metrics.go` - `recordReviewOutcome` function name**:
    - There's a function `recordReviewOutcome(interrupted, failed bool)`. But `interrupted` is not necessarily tied to a `bool` in the call sites. Let me check... in `ExecuteReview`, the variable `interrupted` is used. Looking at the call sites:
    ```go
    recordReviewOutcome(interrupted, true)  // err != nil path
    recordReviewOutcome(interrupted, true)  // WriteManifest err path
    recordReviewOutcome(interrupted, true)  // Outcome err path
    recordReviewOutcome(interrupted, false) // success path
    ```
    - If `interrupted` is true and `failed` is also true (error path), the interrupt takes precedence. This is correct per the comment.

30. **Missing observation in WritePool path**:
    - The `recordFindingMetrics` is called in `WritePool`. But `WritePool` is called from `ExecuteReview`. So the metrics are recorded once per review, in the right place.

31. **Memory leak / unbounded growth**:
    - Histograms are capped at 10,000 samples. Counters are int64 (no overflow concern in practice). The registry could grow in number of distinct metric names, but in practice, the set of metric names is bounded by the codebase. Good.

32. **One issue I see: `internal/fanout/metrics.go` - `recordAgentOutcome` is called from `invokeAgent`, but it's in the same package. So it has access to internal functions. Good.

33. **Histogram empty percentile returns 0**:
    - `h.Percentile(50)` on an empty histogram returns 0. This is documented and tested. Good.

34. **Test race in `TestConcurrentCounterAndHistogram`**:
    - The test uses 16 goroutines * 1000 iterations. This is fine for testing atomicity.

35. **Issue: `internal/metrics/metrics.go` - `min` and `max` builtins**:
    - The code uses `min` and `max` (built-in in Go 1.21+). If the project uses an older Go version, this would fail to compile. The other code in the file uses `len(h.values) == maxHistogramSamples` which uses a constant. Need to check Go version, but this is likely a non-issue in a modern codebase.

36. **Issue: `internal/metrics/metrics.go` - `ceilDiv` function**:
    - `int(ceilDiv(p, float64(len(sorted))))` - here `int` is used instead of `int64`. In Go, `int` is platform-dependent (32 or 64 bit). For a 10,000-element histogram, this is fine.

37. **Missing documentation for `Mean()` and `Count()` consistency**:
    - `Mean()` returns `h.sum / float64(h.count)`. `Count()` returns `h.count`. `Sum()` returns `h.sum`. These are all exact, not windowed. Documented.

38. **`internal/metrics/prometheus.go` - missing `# HELP` lines**:
    - The Prometheus text format allows `# HELP <name> <text>` lines, but the renderer doesn't emit them. This is optional and a minor omission.

39. **Test coverage**:
    - The sprint plan requires 100% coverage of counter, histogram, and registry. The tests look comprehensive but I can't easily verify 100% coverage from the diff.

40. **Issue: `cmd/atcr/review.go` - summary not printed in error path**:
    - Looking at the diff: `if err != nil { return err }` happens before the summary. So if the review errored out, no summary is printed. The metrics are still recorded in the registry (via the `defer` in `ExecuteReview` and `recordReviewOutcome`). But the user doesn't see them.
    - Is this intended? The AC3 says "at the end of a CLI review, a summary is printed". If the review errored, the user should probably still see the metrics. But this is a design choice.

41. **Issue: `cmd/atcr/review.go` - summary placement is odd**:
    - The summary is printed BEFORE the one-shot reconcile/verify output. The comment says "Printed after the one-line status above and before any one-shot reconcile/verify output." This is a deliberate placement, not a bug.

42. **Issue: `writeReviewSummary` uses `metrics.DefaultRegistry` indirectly**:
    - Actually, no — it takes `reg *metrics.Registry` as a parameter. The caller passes `metrics.DefaultRegistry`. The function itself doesn't touch the global. Good for testability.

43. **Test: `TestWriteReviewSummary`**:
    - Creates a fresh registry, seeds it, and calls `writeReviewSummary`. Tests the formatting. Good.

44. **Test: `TestWriteReviewSummaryNoFindings`**:
    - Tests that the breakdown suffix is omitted when there are no findings. Good.

45. **`internal/fanout/engine.go` - dispatchAgent is unexported**:
    - The function is `dispatchAgent` (lowercase). It's only called from `invokeAgent` in the same package. This is fine.

46. **Performance: Histogram percentile is O(N log N)**:
    - Each call to `Percentile` makes a copy and sorts. For the Prometheus renderer, 4 quantiles are called per histogram per scrape. This could be optimized by sorting once and then doing 4 lookups, but for 10,000 samples and a few histograms, this is acceptable.

47. **Issue: `internal/metrics/metrics.go` - `Percentile` returns 0 for empty histogram, but the lock is acquired**:
    - Even for the empty case, the mutex is acquired. This is fine for correctness.

48. **Issue: `internal/metrics/prometheus.go` - summary name format**:
    - For labeled histograms, the renderer uses `fam + withQuantile(inner, q)`. The format is correct.

49. **Issue: `internal/metrics/prometheus.go` - `labelSuffix` for empty inner**:
    - For an unlabeled histogram, `labelSuffix("")` returns `""`. So the output is `atcr_review_duration_seconds_sum 10`. Correct.

50. **Issue: `internal/metrics/prometheus.go` - `formatFloat` precision**:
    - `'g'` format with precision `-1` uses the shortest exact representation. For 0, this is "0". For 1.5, this is "1.5". For 10, this is "10". Correct.

51. **Test: `TestFormatFloat`**:
    - Tests 0, 1.5, 10. The cases are: 0 → "0", 1.5 → "1.5", 10 → "10". The Go 'g' format with precision -1: 0 → "0", 1.5 → "1.5", 10 → "10". Correct.

52. **Issue: `internal/mcp/handlers.go` - `handleMetrics` doesn't check `e.dispatcher` or other engine state**:
    - The handler just renders the global registry. It doesn't depend on engine state. Good.

53. **Issue: `internal/mcp/handlers.go` - `handleMetrics` returns `nil` for `*mcpsdk.CallToolResult`**:
    - This is the convention in this codebase (other handlers do the same). Looking at `handleStatus`, it returns `nil, *st, nil`. So `handleMetrics` follows the same pattern. Good.

54. **Issue: `recordReviewOutcome` is not called when `err := WriteManifest` fails inside the deferred block**:
    - Looking at the code, `WriteManifest` is called twice in the error path (once in the deferred block, once in the normal flow). The first call (in the deferred block) is best-effort. The second call (in the normal flow) has `recordReviewOutcome(interrupted, true)` after it. Good.

55. **Issue: `cmd/atcr/review.go` - `time.Since(now)` vs `time.Since(reviewStart)`**:
    - The summary uses `time.Since(now)` where `now` is the CLI-level start. The histogram in `fanout.ExecuteReview` uses `time.Since(reviewStart)` where `reviewStart` is the `ExecuteReview` start. These are different start times, so the summary's "Review completed in X.Xs" might not match the histogram's recorded duration. This is a minor inconsistency.

56. **Issue: `internal/fanout/metrics.go` - `recordReviewOutcome` parameters**:
    - The function signature is `recordReviewOutcome(interrupted, failed bool)`. The parameter order is (interrupted, failed). This is consistent with the comment "interrupt takes precedence over failure". Good.

57. **Issue: `internal/metrics/metrics.go` - `DefaultRegistry` is a global variable**:
    - Global state is generally discouraged, but for a metrics package, it's the standard pattern (think Prometheus's default registry). The package-level `Counter` and `Histogram` functions provide convenient access. Good.

58. **Issue: `internal/metrics/metrics.go` - thread safety of `DefaultRegistry.Reset()`**:
    - `Reset` acquires the mutex and replaces the maps. Concurrent operations on the old maps (which are still referenced by callers) would be unaffected. But if a caller calls `r.Counter(name)` and gets a reference to a counter, then `Reset()` is called, the caller's reference is still valid (the counter object is still in memory). The next call to `r.Counter(name)` would return a NEW counter (with value 0). This is a subtle behavior, but in tests, it's the desired behavior. In production, `Reset()` is never called. Good.

59. **Issue: `internal/metrics/prometheus.go` - sortedKeys on empty map**:
    - `sortedKeys` works on empty maps. Returns empty slice. Good.

60. **Issue: `internal/metrics/metrics.go` - `Name()` on counter and histogram**:
    - Both have `Name()` methods. Used in tests. Good.

61. **Issue: `internal/mcp/handlers.go` - MetricsArgs is empty**:
    - The tool takes no arguments. The schema would be empty. This is fine for a read-only tool.

62. **Issue: `internal/mcp/tools.go` - `ToolMetrics` constant**:
    - Added to the list of tool constants. Good.

63. **Issue: `internal/boundaries_test.go` - updated dependencies**:
    - "fanout" now includes "metrics". "mcp" now includes "metrics". "metrics" is a new leaf package. Good.

64. **Issue: `internal/fanout/engine.go` - removed comment block**:
    - The original comment about tool-enabled agents was removed and moved to the new `dispatchAgent` function. The comment is preserved in the new function. Good.

65. **Issue: `cmd/atcr/review_summary.go` - `severityBreakdown` iterates `severityOrder`**:
    - The order is CRITICAL, HIGH, MEDIUM, LOW. The function checks each in order and appends non-zero counts. Good.

66. **Issue: `cmd/atcr/review_summary.go` - `val` closure**:
    - The `val` closure captures `reg` and returns `reg.Counter(name).Value()`. This is clean.

67. **Issue: `cmd/atcr/review.go` - `writeReviewSummary` is called even when result is nil-checked**:
    - The `if result != nil` check is defensive. As I noted, `result` should never be nil at this point (the error path returns early). But the check is harmless.

68. **Issue: `internal/fanout/metrics.go` - `apiCalls` calculation**:
    - `apiCalls := r.Turns; if apiCalls == 0 { apiCalls = 1 }` — This assumes that a single-shot agent makes exactly 1 API call. If an agent retries (which the plan says "including retries"), the retry count is captured in `r.Turns`. For a tool-loop agent, `r.Turns` is the number of turns. The comment says "a tool-loop agent makes one Chat call per turn". This is correct.

Let me now look at a few more potential issues:

69. **`internal/fanout/metrics.go` line 50 - `var he *llmclient.HTTPStatusError`**:
    - This is a type assertion via `errors.As`. The variable is declared as a pointer, which is the correct pattern for `errors.As`. Good.

70. **`internal/mcp/handlers.go` - handleMetrics context check**:
    - The handler checks `ctx.Err()` at the start. If the context is cancelled, it returns the error. This is consistent with other handlers. Good.

71. **Potential issue: `internal/metrics/prometheus.go` - `metricFamily` for keys with multiple `{`**:
    - `strings.IndexByte(key, '{')` finds the first `{`. If a key has multiple `{` (e.g., a label value containing `{`), the family would be everything before the first `{`. This is correct behavior for Prometheus-style keys.

72. **Potential issue: `internal/metrics/metrics.go` - `Percentile` with `p=0`**:
    - For `p=0`, `ceilDiv(0, n) = 0`, then `min(max(0, 1), n) = 1`, so `sorted[0]` (the minimum). This is the correct behavior for p=0.

73. **Potential issue: `internal/metrics/metrics.go` - `Percentile` with very small `p`**:
    - For `p=0.001`, `ceilDiv(0.001, 100000) = ceil(1) = 1`, then `sorted[0]`. Correct.

74. **Potential issue: `internal/metrics/metrics.go` - `Count()` and `Sum()` are documented as exact**:
    - The comment says "Count returns the exact number of observations." and "Sum returns the exact running sum of every observed value." Good.

75. **Potential issue: `internal/metrics/metrics.go` - `Observe` updates sum and count even when the sample buffer is full**:
    - The sum and count are updated on every call, regardless of whether the sample buffer is full. This is correct.

76. **Potential issue: `internal/metrics/metrics.go` - `Mean()` precision**:
    - `h.sum / float64(h.count)`. For very large counts, this could lose precision, but for typical use, this is fine.

Let me now consider some real issues I might have missed:

77. **Real issue: `cmd/atcr/review.go` - summary not printed on error path**:
    - The summary is only printed when `err == nil`. If the review errors out (e.g., `ExecuteReview` returns an error), the user doesn't see any metrics summary. This might be intentional (the error is the primary signal), but it could be considered a UX issue. However, this is a design choice and the sprint plan says "At the end of each review, print a summary". An errored review is still a "review", so arguably the summary should be printed.
    - Severity: LOW (design choice, not a bug)
    - CATEGORY: maintainability

78. **Real issue: `internal/fanout/metrics.go` - `apiCalls` is counted even for failed/timeout agents**:
    - The API call count includes the failed calls. This is correct ("API calls (including retries)").

79. **Real issue: `internal/fanout/metrics.go` - `recordAgentOutcome` doesn't handle the case where `r.Status` is not one of the three**:
    - If `r.Status` is some unknown value, the switch falls through and no agent outcome counter is incremented. The API calls are still counted. This is a potential silent failure. But the Result type only has three status values, so this is defensive programming.
    - Severity: LOW
    - CATEGORY: error-handling

80. **Real issue: `internal/metrics/prometheus.go` - `sortedKeys` is called twice (once for counters, once for histograms)**:
    - This is fine, just an observation.

81. **Real issue: `internal/metrics/metrics.go` - `Percentile` makes a copy and sorts on every call**:
    - For the Prometheus renderer, this is called 4 times per histogram. For 10,000 samples, this is 4 * 10,000 * log(10,000) = ~530,000 operations per scrape. This is acceptable but could be optimized.
    - Severity: LOW
    - CATEGORY: performance

82. **Real issue: `internal/mcp/handlers.go` - `handleMetrics` doesn't acquire any locks**:
    - `metrics.DefaultRegistry.WritePrometheus()` acquires the registry's mutex internally. The handler doesn't need its own lock. Good.

83. **Real issue: `cmd/atcr/review.go` - `writeReviewSummary` is called even when the review was interrupted**:
    - The summary is printed for interrupted reviews too. This is correct behavior.

84. **Real issue: `cmd/atcr/review.go` - `now` variable is used**:
    - `time.Since(now)` is used. I assume `now` is defined earlier in the function. This is a pre-existing variable.

85. **Real issue: `internal/fanout/metrics.go` - `recordFindingMetrics` returns early if `len(findings) == 0`**:
    - This is a minor optimization. If there are no findings, no metrics are recorded. This is correct.

86. **Real issue: `cmd/atcr/review_summary.go` - `severityBreakdown` always includes all severities with non-zero counts**:
    - The function iterates `severityOrder` and includes all non-zero counts. Good.

87. **Real issue: `internal/fanout/review.go` - `recordReviewOutcome` is called in multiple places**:
    - This is a bit repetitive. The function is called 4 times. A deferred call could simplify this. But the current design is clear and correct.

88. **Real issue: `internal/metrics/metrics.go` - `Histogram(name)` and `Counter(name)` are package-level functions**:
    - These shadow the type names. The types are unexported (`counter`, `histogram`), so the exported functions (`Counter`, `Histogram`) don't conflict. Good.

89. **Real issue: `internal/metrics/prometheus.go` - `formatFloat(0)` returns "0"**:
    - This is correct for Prometheus format.

90. **Real issue: `internal/fanout/metrics.go` - `strconv.Itoa(he.Status)`**:
    - The HTTP status is converted to a string. For status 429, this is "429". The label value is "429". Correct.

Let me now think about what's actually a real issue vs. nitpicks:

Real issues:
1. The `writeReviewSummary` is only called on success path, not on error path. This could be a design issue.
2. The `time.Since(now)` in the summary uses a different start time than the histogram observation. This is a minor inconsistency.
3. The `ceilDiv` function is a custom implementation that could use `math.Ceil`. But the comment explains why it's done this way (no math import).
4. The `Percentile` function is O(N log N) per call. For 4 quantiles per histogram per scrape, this could be optimized by sorting once.

Let me also look for issues I might have missed:

- **Boundary case: `r.Turns` is negative**: Unlikely, but if it were, `apiCalls` would be negative, and `Counter.Add` would decrement the counter. This is a potential bug, but `r.Turns` should never be negative.
  - Severity: LOW
  - CATEGORY: correctness

- **Issue: `internal/fanout/metrics.go` - `recordAgentOutcome` doesn't check for negative `r.ToolCalls`**:
  - Same as above. `r.ToolCalls` should never be negative.
  - Severity: LOW
  - CATEGORY: correctness

- **Issue: `cmd/atcr/review.go` - `now` variable timing**:
  - The summary uses `time.Since(now)` where `now` is the CLI-level start. This includes time spent on dependency resolution, file loading, etc. The histogram in `ExecuteReview` measures only the review execution. The difference is small but non-zero. This is a minor inconsistency.

Let me look at one more thing - the `now` variable in `runReview`:

Looking at the diff, the summary is placed after `if err != nil { return err }`. The `now` variable must be defined somewhere earlier in the function. This is pre-existing code.

Now, let me also consider:

- **Issue: `internal/fanout/review.go` - `recordReviewOutcome(interrupted, true)` is called in 3 error paths**:
  - This is repetitive but correct. A `defer` could simplify this, but the current design is clear.
  - Severity: LOW
  - CATEGORY: maintainability

- **Issue: `internal/mcp/handlers.go` - `handleMetrics` doesn't handle the case where `metrics.DefaultRegistry.WritePrometheus()` returns an error**:
  - `WritePrometheus()` returns a string, not an error. It never fails. Good.

- **Issue: `internal/mcp/handlers.go` - `handleMetrics` doesn't log anything**:
  - The handler doesn't log. Other handlers might log. This is a minor omission but not a bug.

Let me focus on the most actionable issues:

1. **Summary not printed on error path** (LOW, maintainability): The sprint plan says "At the end of each review, print a summary". An errored review is still a review. The summary should be printed regardless of success/failure.

2. **Inconsistent start time for duration** (LOW, correctness): The summary uses `time.Since(now)` while the histogram uses `time.Since(reviewStart)`. These are different start times.

3. **`ceilDiv` custom implementation** (LOW, maintainability): Could use `math.Ceil` instead. But the comment explains the rationale (no math import).

4. **Percentile is O(N log N) per call** (LOW, performance): Could be optimized by caching the sorted slice.

5. **Repetitive `recordReviewOutcome` calls** (LOW, maintainability): Could be simplified with a defer.

6. **API call count doesn't validate `r.Turns >= 0`** (LOW, correctness): Unlikely to be negative, but no check.

7. **Tool call count doesn't validate `r.ToolCalls >= 0`** (LOW, correctness): Unlikely to be negative, but no check.

8. **`recordAgentOutcome` silently ignores unknown statuses** (LOW, error-handling): If a new status is added, the metric won't be incremented. The switch could have a default case that increments a "_other" counter or logs a warning.

Let me also check the test for one thing - the `now` variable. Looking at the diff, the summary uses `time.Since(now)`. This `now` must be defined earlier in the function. Looking at the surrounding code... I don't see the definition in the diff, so it must be pre-existing.

Actually, let me re-examine the diff more carefully for any issues I might have missed.

Looking at `internal/fanout/review.go`:
```go
func ExecuteReview(ctx context.Context, completer Completer, p *PreparedReview) (*ReviewResult, error) {
    metrics.Counter(metrics.NameReviewsTotal).Inc()
    reviewStart := time.Now()
    defer func() {
        metrics.Histogram(metrics.NameReviewDurationSeconds).Observe(time.Since(reviewStart).Seconds())
    }()
    
    poolDir := filepath.Join(p.Dir, "sources", "pool")
    results, stage := runEngine(ctx, completer, p, poolDir)
    // ... error handling ...
    
    // In error paths:
    recordReviewOutcome(interrupted, true)
    
    // In success path:
    recordReviewOutcome(interrupted, false)
}
```

The `defer` for the histogram observation is good. It fires on every return.

The `recordReviewOutcome` calls are placed at each return point. This is repetitive but correct.

One potential issue: if `runEngine` panics, the deferred histogram observation fires, but `recordReviewOutcome` is not called. The metric `atcr_reviews_total` is incremented, but the terminal classification is not. This could leave the metric in an inconsistent state.

Severity: LOW
CATEGORY: error-handling

Fix: Use a named return value and call `recordReviewOutcome` in a defer that reads the named returns.

Actually, looking more carefully, the deferred function captures `reviewStart` by closure. If we want to also call `recordReviewOutcome` in the defer, we need to use named return values:

```go
func ExecuteReview(ctx context.Context, completer Completer, p *PreparedReview) (res *ReviewResult, err error) {
    metrics.Counter(metrics.NameReviewsTotal).Inc()
    reviewStart := time.Now()
</think>
