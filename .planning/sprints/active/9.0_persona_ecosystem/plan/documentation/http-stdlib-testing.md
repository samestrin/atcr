# HTTP & Standard Library Testing ![IMPORTANT](https://img.shields.io/badge/priority-IMPORTANT-orange)

## Overview

ATCR's HTTP layer is built entirely on Go's standard library (`net/http` + `encoding/json`) with no provider SDKs — a deliberate Epic 1.0 constraint to keep the dependency tree small. The OpenAI-compatible client POSTs JSON to `${base_url}/chat/completions`, constructs requests with `http.NewRequestWithContext` so per-agent and global timeouts cancel in-flight calls, and applies a retry policy (429/500/502/503/504) with ~500 ms initial delay and 1.5× backoff. All retries drain response bodies to allow connection reuse.

For the `atcr personas` CLI (Sprint 9.0, T2), a new `internal/personas` package provides install/search/upgrade subcommands that fetch from a configurable community-repo URL (default: `https://raw.githubusercontent.com/atcr/personas/main`). The fetch path is exercised in tests using `httptest.NewServer` — no live external calls are permitted in CI. Installed personas land in `~/.config/atcr/personas/`.

The test strategy across all HTTP-touching code follows a single rule: `httptest.NewServer(handler)` stands in for every remote provider or registry. The handler is pointed at via a configurable `base_url` or `RegistryBaseURL` field, so the real client code path — auth, retry, decode — executes against a local server with zero network dependency. Concurrency is verified observably by recording an `atomic.Int32` high-water mark inside the handler.

## Key Concepts

### net/http + encoding/json — Provider Client

- Build requests with `http.NewRequestWithContext(ctx, ...)` so the per-agent and global timeouts cancel in-flight calls.
- Separate the transport timeout from the operation timeout: `http.Client{Timeout: 120 * time.Second}` guards a single HTTP exchange; the agent/global deadlines live in the context.
- Auth: `req.Header.Set("Authorization", "Bearer "+key)` with the key resolved from the provider's `api_key_env` at invoke time.
- Retry policy: retry on 429/500/502/503/504 with ~500 ms initial delay and 1.5× backoff so retries don't exhaust the agent timeout; other 4xx fail immediately.
- JSON: struct-tagged request/response types with `json.Marshal`/`json.Unmarshal`. Decode into a minimal envelope (choices → message → content); ignore unknown fields by default to tolerate provider-specific extras.
- Always `defer resp.Body.Close()` and drain bodies on retry paths to allow connection reuse.

> Source: [standard-library.md: net/http + encoding/json — provider client]

### internal/personas Package — Community Registry Client

- New package for install/list/search/upgrade logic with a configurable `RegistryBaseURL` constant (default: `https://raw.githubusercontent.com/atcr/personas/main`).
- All HTTP fetch tests use `httptest.NewServer` — no live network calls in CI.
- `internal/personas/client.go` — HTTP client for fetching from the configurable community repo URL (install, search, upgrade).
- `internal/personas/client_test.go` — tests using `httptest.NewServer` to exercise install/search/upgrade without live network calls.

> Source: [codebase-discovery.json: internal/personas package]

### httptest.NewServer — Provider and Registry Mocks

- `httptest.NewServer(handler)` plays an OpenAI-compatible provider; the registry under test points `base_url` at `server.URL`, so the real client code path (auth, retry, decode) is exercised with zero network.
- Verify parallelism observably: the handler increments an `atomic.Int32` on entry, decrements on exit, and records the high-water mark; small handler delays (20–50 ms) make concurrency measurable without flaky timing assertions.
- Simulate provider failure modes per test: 429 then 200 (retry path), persistent 500 (fallback-agent path), handler sleep past the agent deadline (timeout status path).

> Source: [standard-library.md: testing + net/http/httptest — provider mocks]

### context + sync — Fan-Out Engine

- One `context.WithTimeout` wraps the whole review (global timeout); each agent invocation derives its own `context.WithTimeout` from it (per-agent timeout).
- Parallel lane: one goroutine per agent, coordinated with `sync.WaitGroup`; **always drain the WaitGroup even when the context is cancelled** — no goroutine leaks, every agent gets a `status.json` (ok | failed | timeout).
- Serial lane: a plain loop after the parallel lane, checking `ctx.Err()` before each invocation; skip remaining agents with status "timeout" once the deadline passes.
- Distinguish `context.DeadlineExceeded`/`context.Canceled` from provider errors when classifying agent status.
- Shared result collection: either a mutex-guarded slice or a buffered channel sized to the agent count.

> Source: [standard-library.md: context + sync — fan-out engine]

### httptest.NewServer Pattern for External Fetch (Codebase)

External HTTP calls in tests use `httptest.NewServer` to serve static JSON/YAML responses, pointed at a configurable URL field. No live external calls in unit tests. This pattern matches the epic's decision: fetch source is configurable, default is a constant, tested via `httptest`.

> Source: [codebase-discovery.json: Pattern "httptest.NewServer for external fetch", files: internal/verify/invoke_test.go]

### Table-Driven Tests and Fixtures

- Table-driven tests with subtests (`t.Run`) for the range decision tree, stream parsing, and reconciler merge rules.
- `t.TempDir()` for review-directory fixtures.
- Fixture git repos built with `os/exec` in test helpers.

> Source: [standard-library.md: testing + net/http/httptest — provider mocks]

## Code Examples

No verbatim code examples appear in the source documents for this topic. Examples are described structurally; refer to `internal/verify/invoke_test.go` for the canonical `httptest.NewServer` pattern in this codebase.

## Quick Reference

| Concern | Package / Tool | Rule |
|---------|---------------|------|
| Provider HTTP calls | `net/http` + `encoding/json` | `http.NewRequestWithContext`; no provider SDKs |
| Transport timeout | `http.Client{Timeout: 120s}` | Guards a single HTTP exchange only |
| Operation timeout | `context.WithTimeout` | Per-agent and global; propagated through context |
| Auth header | `req.Header.Set(...)` | Key resolved from `api_key_env` at invoke time |
| Retry policy | Custom retry loop | 429/500/502/503/504; 500 ms initial, 1.5× backoff; other 4xx fail immediately |
| Body cleanup | `defer resp.Body.Close()` + drain | Required on all retry paths |
| Concurrency | `sync.WaitGroup` + `context` | Always drain WaitGroup; never leak goroutines |
| Result sharing | Mutex-guarded slice or buffered channel | Per-agent files on disk are the authoritative store |
| HTTP mocking | `httptest.NewServer` | **Mandatory** — no live external calls in any test |
| Parallelism assertion | `atomic.Int32` high-water mark | 20–50 ms handler delay makes concurrency measurable |
| Failure simulation | Handler returns 429/500/sleep | Exercises retry, fallback-agent, and timeout paths |
| personas fetch URL | `RegistryBaseURL` constant | Configurable; default `https://raw.githubusercontent.com/atcr/personas/main` |
| personas install path | `~/.config/atcr/personas/` | Standard XDG config location |
| Test fixtures | `t.TempDir()` + `os/exec` git | Review-directory and repo fixtures |

## Related Documentation

- [pkg.go.dev/net/http](https://pkg.go.dev/net/http)
- [pkg.go.dev/encoding/json](https://pkg.go.dev/encoding/json)
- [pkg.go.dev/net/http/httptest](https://pkg.go.dev/net/http/httptest)
- [pkg.go.dev/context](https://pkg.go.dev/context)
- [pkg.go.dev/sync](https://pkg.go.dev/sync)
- [pkg.go.dev/testing](https://pkg.go.dev/testing)
- [pkg.go.dev/os/exec](https://pkg.go.dev/os/exec)
- `internal/verify/invoke_test.go` — canonical `httptest.NewServer` example in this codebase
- `internal/personas/client.go` — HTTP client for community registry (to be created, T2)
- `internal/personas/client_test.go` — `httptest.NewServer`-based tests for registry fetch (to be created, T2)
- `.planning/plans/active/9.0_persona_ecosystem/documentation/source.md` — plan source document
