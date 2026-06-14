# Go Standard Library Usage

**Go Version:** 1.25+
**Official Docs:** [pkg.go.dev/std](https://pkg.go.dev/std)
**Tier:** Reference (consolidated)
**Last Updated:** June 10, 2026

---

Everything in atcr beyond the five direct third-party dependencies (cobra, mcp-go-sdk, yaml.v3, testify, jsonschema-go) is intentionally standard library (Epic 1.0 constraint: keep the dependency tree small). This file documents which stdlib packages carry which subsystem and the conventions that apply.

| Package | atcr subsystem |
|---------|----------------|
| `net/http` + `encoding/json` | OpenAI-compatible chat client and retry logic |
| `os/exec` | git interaction (range resolution, diffs, function-context) |
| `text/template` | persona prompt rendering with payload vars |
| `context`, `sync` | fan-out timeouts, parallel/serial lanes |
| `testing` + `net/http/httptest` | provider mocks for fan-out tests |

---

## net/http + encoding/json — provider client

[pkg.go.dev/net/http](https://pkg.go.dev/net/http) · [pkg.go.dev/encoding/json](https://pkg.go.dev/encoding/json)

The OpenAI-compatible client is a plain `http.Client` POSTing JSON to `${base_url}/chat/completions` — no provider SDKs, per the plan's constraints.

- Build requests with `http.NewRequestWithContext(ctx, ...)` so the per-agent and global timeouts cancel in-flight calls.
- Separate the transport timeout from the operation timeout: `http.Client{Timeout: 120 * time.Second}` guards a single HTTP exchange; the agent/global deadlines live in the context (base-system pattern).
- Auth: `req.Header.Set("Authorization", "Bearer "+key)` with the key resolved from the provider's `api_key_env` at invoke time.
- Retry policy (carried over, must not be lost): retry on 429/500/502/503/504 with ~500 ms initial delay and 1.5× backoff so retries don't exhaust the agent timeout; other 4xx fail immediately.
- JSON: struct-tagged request/response types with `json.Marshal`/`json.Unmarshal`. Decode into a minimal envelope (choices → message → content); ignore unknown fields by default, which tolerates provider-specific extras.
- Always `defer resp.Body.Close()` and drain bodies on retry paths to allow connection reuse.

## os/exec — git interaction

[pkg.go.dev/os/exec](https://pkg.go.dev/os/exec)

atcr shells out to git like the base system — no `go-git`.

- `exec.CommandContext(ctx, "git", "-C", repo, ...)` everywhere, so a cancelled run never leaves orphaned git processes.
- Capture stdout and stderr separately; on non-zero exit, surface trimmed stderr in the error (git's messages are the diagnostic).
- Used for: `rev-parse`, `symbolic-ref` (default-branch detection), `merge-base`, `rev-list --count` (empty-range check), `diff base..head`, `diff --function-context` (blocks payload), `rev-parse --is-shallow-repository` (shallow-clone guard).
- Never build git invocations through a shell (`sh -c`) — pass argv directly; ref names and paths are arguments, immune to shell injection.
- Diff output is written to disk verbatim (no trimming) so reviewers and the payload builder see unmodified git output.

## text/template — persona prompt rendering

[pkg.go.dev/text/template](https://pkg.go.dev/text/template)

Persona prompts (`<agent>.md`, `_base.md`, embedded defaults) are text/template documents rendered with payload vars (`{{.Payload}}`, `{{.PayloadMode}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, `{{.AgentName}}`, large-payload guidance vars).

- Use `text/template`, never `html/template` (no HTML escaping in prompts).
- `template.New(name).Option("missingkey=error")` (or `.Option("missingkey=zero")` deliberately) — decide and document; the base system treats broken templates as missing so the resolution chain falls through to the next layer. Preserve that: parse errors demote to the fallback rather than aborting the run.
- Conditional sections via `{{if .LargeDiff}}...{{end}}` carry over for per-payload-mode guidance.

## context + sync — fan-out engine

[pkg.go.dev/context](https://pkg.go.dev/context) · [pkg.go.dev/sync](https://pkg.go.dev/sync)

The concurrency rules carried over from the base system:

- One `context.WithTimeout` wraps the whole review (global timeout); each agent invocation derives its own `context.WithTimeout` from it (per-agent timeout).
- Parallel lane: one goroutine per agent, coordinated with `sync.WaitGroup`; **always drain the WaitGroup even when the context is cancelled** — no goroutine leaks, every agent gets a `status.json` (ok | failed | timeout).
- Serial lane: a plain loop after the parallel lane, checking `ctx.Err()` before each invocation; skip remaining agents with status "timeout" once the deadline passes.
- Distinguish `context.DeadlineExceeded`/`context.Canceled` from provider errors when classifying agent status.
- Shared result collection: either a mutex-guarded slice or a buffered channel sized to the agent count; results are per-agent files on disk anyway, so in-memory sharing stays minimal.

## testing + net/http/httptest — provider mocks

[pkg.go.dev/testing](https://pkg.go.dev/testing) · [pkg.go.dev/net/http/httptest](https://pkg.go.dev/net/http/httptest)

The base repo's test pattern, carried over:

- `httptest.NewServer(handler)` plays an OpenAI-compatible provider; the registry under test points `base_url` at `server.URL`, so the real client code path (auth, retry, decode) is exercised with zero network.
- Verify parallelism observably: the handler increments an `atomic.Int32` on entry, decrements on exit, and records the high-water mark; small handler delays (20–50 ms) make concurrency measurable without flaky timing assertions.
- Simulate provider failure modes per test: 429 then 200 (retry path), persistent 500 (fallback-agent path), handler sleep past the agent deadline (timeout status path).
- Table-driven tests with subtests (`t.Run`) for the range decision tree, stream parsing, and reconciler merge rules; `t.TempDir()` for review-directory fixtures; fixture git repos built with `os/exec` in test helpers (base-system pattern).

---
**Note:** Standard-library packages need no installation or version pinning beyond the Go toolchain version in go.mod (`go 1.24`).
