# LLM Client & Fan-out Engine [CRITICAL]

## Overview

The LLM client and fan-out engine form the core execution layer of atcr, enabling parallel code review by multiple AI personas. The system uses Go's standard library exclusively—no provider-specific SDKs—to implement an OpenAI-compatible chat client that fans out code changes to six embedded reviewer personas (bruce, greta, kai, mira, dax, otto) simultaneously.

The fan-out engine orchestrates these agents through two execution lanes: a parallel lane for concurrent invocations using `sync.WaitGroup` with context cancellation safety, and a serial lane for rate-limited agents that checks the global timeout before each call. Each agent receives its own derived context with per-agent timeouts, ensuring that individual slow responders cannot block the entire review process. The system implements partial-success semantics—an error is only returned if ALL agents fail—and writes per-agent status files (`status.json`) alongside merged findings.

Retry logic handles transient provider failures (429/500/502/503/504) with exponential backoff starting at ~500ms delay with 1.5x multiplier, preventing premature exhaustion of agent timeouts while respecting provider rate limits.

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json section]

> Source: [.planning/plans/active/1.0_atcr_core/codebase-discovery.json:existing_patterns section on standard-library.md lines 8-78]

## Key Concepts

### OpenAI-Compatible Client

The LLM client posts JSON directly to `${base_url}/chat/completions` with an Authorization Bearer header resolved from environment variables at invoke time. No provider-specific SDKs are used—only `net/http` and `encoding/json` from the standard library.

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

```go
// Build requests with http.NewRequestWithContext(ctx, ...) so the per-agent
// and global timeouts cancel in-flight calls.
req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", body)
if err != nil {
    return nil, fmt.Errorf("create request: %w", err)
}
req.Header.Set("Authorization", "Bearer "+key)
```

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

### Retry Logic

Transient provider errors trigger retry with exponential backoff. The policy retries on HTTP status codes 429, 500, 502, 503, and 504 with an initial delay of approximately 500ms and a 1.5x backoff multiplier. This prevents retries from exhausting the agent timeout while handling common failure modes gracefully.

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

```go
// Separate the transport timeout from the operation timeout:
// http.Client{Timeout: 120 * time.Second} guards a single HTTP exchange;
// the agent/global deadlines live in the context.
client := &http.Client{Timeout: 120 * time.Second}
```

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

### Context Hierarchy

A two-level context hierarchy manages timeouts: one `context.WithTimeout` wraps the entire review (global timeout), and each agent invocation derives its own `context.WithTimeout` from it (per-agent timeout). This ensures individual slow agents cannot block the entire review while maintaining a hard upper bound on total execution time.

> Source: [.planning/specifications/packages/standard-library.md:context + sync — fan-out engine]

### Parallel Lane Execution

The parallel lane spawns one goroutine per agent, coordinated with `sync.WaitGroup`. Critically, the WaitGroup must always drain even when the context is cancelled—no goroutine leaks are permitted. Every agent receives a `status.json` file (ok | failed | timeout) regardless of outcome.

> Source: [.planning/specifications/packages/standard-library.md:context + sync — fan-out engine]

### Serial Lane Execution

After the parallel lane completes, a plain loop processes rate-limited agents (those with `rate_limited: true` in their configuration). The loop checks `ctx.Err()` before each invocation and skips remaining agents with status "timeout" once the deadline passes.

> Source: [.planning/specifications/packages/standard-library.md:context + sync — fan-out engine]

### Partial-Success Semantics

An error is only returned if ALL agents fail. Individual agent failures are recorded in their respective `status.json` files but do not abort the review. The system merges available findings from successful agents into the pool output, allowing the reconciler to work with whatever subset of reviewers completed successfully.

> Source: [.planning/plans/active/1.0_atcr_core/codebase-discovery.json:architecture_notes section]

### Per-Agent Status Tracking

Each agent writes a `status.json` file recording its outcome: "ok" for successful completion, "failed" for non-retryable errors or persistent failures after retries exhausted, or "timeout" when the per-agent or global deadline is exceeded. This enables post-hoc analysis of which reviewers participated in a given review.

> Source: [.planning/plans/active/1.0_atcr_core/codebase-discovery.json:architecture_notes section]

## Code Examples

### Request Construction with Context

```go
// From .planning/specifications/packages/standard-library.md:
// Build requests with http.NewRequestWithContext(ctx, ...) so the per-agent
// and global timeouts cancel in-flight calls.
req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", body)
if err != nil {
    return nil, fmt.Errorf("create request: %w", err)
}
req.Header.Set("Authorization", "Bearer "+key)
```

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

### Transport Timeout Separation

```go
// From .planning/specifications/packages/standard-library.md:
// Separate the transport timeout from the operation timeout:
// http.Client{Timeout: 120 * time.Second} guards a single HTTP exchange;
// the agent/global deadlines live in the context (base-system pattern).
client := &http.Client{Timeout: 120 * time.Second}
```

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

### Response Body Cleanup

```go
// From .planning/specifications/packages/standard-library.md:
// Always defer resp.Body.Close() and drain bodies on retry paths
// to allow connection reuse.
defer resp.Body.Close()
```

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

### Git Command Execution

```go
// From .planning/specifications/packages/standard-library.md:
// exec.CommandContext(ctx, "git", "-C", repo, ...) everywhere,
// so a cancelled run never leaves orphaned git processes.
cmd := exec.CommandContext(ctx, "git", "-C", repo, "diff", baseRef+".."+headRef)
```

> Source: [.planning/specifications/packages/standard-library.md:os/exec — git interaction]

### Persona Template Rendering

```go
// From .planning/specifications/packages/standard-library.md:
// Use text/template, never html/template (no HTML escaping in prompts).
// template.New(name).Option("missingkey=error") — decide and document;
// the base system treats broken templates as missing so the resolution
// chain falls through to the next layer.
tmpl, err := template.New(name).Option("missingkey=error").Parse(text)
```

> Source: [.planning/specifications/packages/standard-library.md:text/template — persona prompt rendering]

### WaitGroup Drain Safety

```go
// From .planning/specifications/packages/standard-library.md:
// Parallel lane: one goroutine per agent, coordinated with sync.WaitGroup;
// always drain the WaitGroup even when the context is cancelled —
// no goroutine leaks, every agent gets a status.json (ok | failed | timeout).
var wg sync.WaitGroup
for _, agent := range agents {
    wg.Add(1)
    go func(a Agent) {
        defer wg.Done()
        // ... agent invocation with derived context ...
    }(agent)
}
wg.Wait()
```

> Source: [.planning/specifications/packages/standard-library.md:context + sync — fan-out engine]

### Provider Mock Testing

```go
// From .planning/specifications/packages/standard-library.md:
// httptest.NewServer(handler) plays an OpenAI-compatible provider;
// the registry under test points base_url at server.URL, so the real
// client code path (auth, retry, decode) is exercised with zero network.
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Verify parallelism: increment atomic.Int32 on entry, decrement on exit,
    // record high-water mark for concurrency verification.
}))
```

> Source: [.planning/specifications/packages/standard-library.md:testing + net/http/httptest — provider mocks]

## Quick Reference

| Component | Stdlib Package | Purpose |
|-----------|---------------|---------|
| HTTP client | `net/http` | POST to OpenAI-compatible `/chat/completions` endpoint |
| JSON encoding | `encoding/json` | Serialize requests, deserialize responses |
| Git commands | `os/exec` | Range resolution, diff generation, function-context expansion |
| Prompt rendering | `text/template` | Render persona prompts with payload variables |
| Global timeout | `context` | Wrap entire review with deadline |
| Per-agent timeout | `context` | Derive sub-contexts for each agent invocation |
| Goroutine coordination | `sync.WaitGroup` | Parallel lane synchronization (must drain on cancel) |
| Concurrent result collection | `sync.Mutex` or buffered channel | Guard shared result slice |
| Provider mocks | `net/http/httptest` | Test server for retry/concurrency verification |
| Atomic counters | `sync/atomic.Int32` | High-water mark for parallelism verification in tests |

| Retry Status Codes | Initial Delay | Backoff Multiplier |
|-------------------|--------------|-------------------|
| 429, 500, 502, 503, 504 | ~500ms | 1.5x |

| Agent Status Values | Meaning |
|--------------------|---------|
| `ok` | Agent completed successfully with findings |
| `failed` | Non-retryable error or persistent failure after retries exhausted |
| `timeout` | Per-agent or global deadline exceeded |

| Execution Lane | Behavior |
|---------------|----------|
| Parallel | One goroutine per agent via `sync.WaitGroup`, drains on cancel |
| Serial | Loop with `ctx.Err()` check before each `rate_limited: true` agent |

## Related Documentation

- [Standard Library Usage](../../../../specifications/packages/standard-library.md) — Comprehensive stdlib package assignments for all atcr subsystems
- [Codebase Discovery Report](../codebase-discovery.json) — Architecture patterns and integration points
- [Implementation Standards](../../../../specifications/implementation-standards.md) — Black-box interfaces, single responsibility, primitive-first design principles
- [Coding Standards](../../../../specifications/coding-standards.md) — Go coding conventions (receiver names, error wrapping, table-driven tests)
- [Testify Package Spec](../../../../specifications/packages/testify.md) — Assertions, mocks, and test suite patterns for provider-mock unit tests
- [Plan Document](../plan.md) — Fan-out engine task breakdown (task 7) and LLM client task (task 6)
- [Package Recommendations](file:///Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/1.0_atcr_core/package-recommendations.md) — Third-party dependency rationale (cobra, yaml.v3, mcp-go-sdk)
