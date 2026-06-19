# Metrics

atcr records operational metrics for every review it runs (Epic 4.4). The same
underlying counters and histograms power two surfaces:

- The **end-of-review CLI summary** printed after `atcr review`.
- The **`atcr_metrics` MCP tool**, which exports the full registry in Prometheus
  text exposition format.

All metrics are cumulative since the process started. In one-shot CLI mode that
is a single review; in `atcr serve` mode the registry accumulates across every
review the server runs.

## Metric catalog

### Review counters

| Metric | Type | Description |
|--------|------|-------------|
| `atcr_reviews_total` | counter | Reviews started. |
| `atcr_reviews_succeeded` | counter | Reviews that completed with at least one agent succeeding. |
| `atcr_reviews_failed` | counter | Reviews where every agent failed or persistence faulted. |
| `atcr_reviews_interrupted` | counter | Reviews cut off by SIGINT/SIGTERM (takes precedence over failed). |
| `atcr_review_duration_seconds` | histogram | Engine-level fan-out + persistence time per review. |

The invariant `atcr_reviews_total == succeeded + failed + interrupted` holds for
every review that exits normally.

### Agent counters

| Metric | Type | Description |
|--------|------|-------------|
| `atcr_agents_total` | counter | Agent invocations (each slot attempt, including fallbacks). |
| `atcr_agents_succeeded` | counter | Agent invocations that returned a usable result. |
| `atcr_agents_failed` | counter | Agent invocations that failed. |
| `atcr_agents_timed_out` | counter | Agent invocations cut off by the per-review timeout. |
| `atcr_agent_duration_seconds` | histogram | Wall-clock per agent invocation. |

### API and tool counters

| Metric | Type | Description |
|--------|------|-------------|
| `atcr_api_calls_total` | counter | Provider round-trips (one per turn for tool-loop agents, one for single-shot, including retries). |
| `atcr_api_errors_total{status}` | counter | API errors, labeled by HTTP status code. |
| `atcr_tool_calls_total` | counter | Tool calls made by tool-using agents. |

### Finding counters

| Metric | Type | Description |
|--------|------|-------------|
| `atcr_findings_total` | counter | Findings kept after guardrails (min-severity floor + max-findings cap). |
| `atcr_findings_by_severity{severity}` | counter | Kept findings, labeled `CRITICAL`/`HIGH`/`MEDIUM`/`LOW` (anything outside that set is bucketed as `UNKNOWN`). |

## End-of-review CLI summary

After a review, `atcr review` prints a four-line summary reflecting that review
alone (the post-review counters minus a pre-review baseline), so the numbers are
correct even when the registry accumulates across reviews:

```
Total elapsed: 12.4s
Agents: 3/4 succeeded, 1 failed, 0 timed out
API calls: 9
Findings: 5 (2 HIGH, 3 MEDIUM)
```

- **Total elapsed** is CLI-level wall-clock (config load through completion). It
  is intentionally wider than `atcr_review_duration_seconds`, which times only
  the engine fan-out window, so the two values rarely match exactly.
- **Agents X/Y** draws both numerator and denominator from per-attempt counts,
  so a slot whose primary fails and fallback succeeds reads `1/2`, not `1/1`.
- The severity breakdown lists only non-zero severities, high to low.

The summary prints on the fresh-review path, the `--resume` path, and the
all-agents-failed path (where the breakdown is most useful), so an operator sees
it on every completed review.

## The `atcr_metrics` MCP tool

`atcr serve` exposes the registry through the `atcr_metrics` MCP tool. The epic's
AC4 originally named an HTTP `/metrics` endpoint, but `atcr serve` is a stdio
JSON-RPC server with no HTTP listener, so metrics are surfaced as a tool instead.
Because the transport is stdio, access is **local-only** — there is no network
listener to authenticate against.

The tool takes no arguments and returns Prometheus text exposition format:

- Counters render as `# TYPE <family> counter` followed by one line per key.
- Histograms render as `# TYPE <family> summary`: a line per quantile
  (`0.5`, `0.9`, `0.95`, `0.99`), then `<family>_sum` and `<family>_count`.

Output is deterministic (families and keys sorted) so it is stable for scraping
and tests.
