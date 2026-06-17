# Task 02: Secret and Path Redaction Helpers

**Source:** Plan 4.0 – Debt Item #2
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
ATCR has no single redaction point for log output. Every new error site that logs an error string must be audited independently for leaked API keys, bearer tokens, and absolute repo paths. Without a shared redaction layer at the sink, secrets can reach stderr/CI logs at any level — including debug.

## Solution Overview
Create `internal/log/redact.go` with reusable redaction helpers that scrub secrets and render absolute paths relative to the review root. These helpers are called at the log sink level so every record — regardless of log level — passes through redaction before emission.

Secret redaction reuses the existing `bearerTokenPattern` and `skKeyPattern` regexes from `internal/llmclient/client.go:redactErrorSnippet`, extending them to also scrub exact-match API key values (including URL-encoded forms). Path redaction strips a known review-root prefix from any path-shaped substring in the log message.

## Technical Implementation
### Steps
1. Create `internal/log/redact.go` with the following:
   - A `Redactor` struct that holds the review root (optional) and a list of exact-match secret values.
   - `NewRedactor(reviewRoot string, secrets ...string) *Redactor` constructor.
   - `func (r *Redactor) Redact(msg string) string` — applies all redaction passes in order: exact secret match (literal + URL-encoded), bearer token regex, sk- key regex, absolute path relativization.
   - Package-level compiled regexes `bearerTokenPattern` and `skKeyPattern` mirroring those in `internal/llmclient/client.go`.
   - `redactSecrets(msg string, secrets []string) string` — exact-match replacement of each secret value (literal and `url.QueryEscape` form) with `[redacted]`.
   - `redactTokens(msg string) string` — regex replacement of bearer/sk- patterns.
   - `redactPaths(msg string, root string) string` — replaces occurrences of `root` (when non-empty and absolute) with the relative equivalent or `[root]`.

2. Add `Redact` to the sink's record-processing pipeline so every emitted record passes through it. This is a hook called by `internal/log/log.go` (Task 01) before the handler writes the record.

3. Ensure the redactor is safe for concurrent use (regex compilation at package level, no mutable state in `Redact`).

## Files to Create/Modify
- `internal/log/redact.go` – **create**: redaction helpers (`Redactor`, `NewRedactor`, `Redact`, secret/token/path functions)

## Documentation Links
- [Secret and Path Redaction](../documentation/secret-path-redaction.md)
- [Testing Patterns](../documentation/testing-patterns.md)

## Related Files (from codebase-discovery.json)
- `internal/llmclient/client.go:redactErrorSnippet` — existing redaction regexes and exact-match logic to mirror
- `internal/llmclient/client_test.go:TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens` — existing regression test to mirror

## Success Criteria
- [ ] `internal/log/redact.go` compiles and exports `Redactor`, `NewRedactor`, and `Redact`.
- [ ] A known API key value passed to `NewRedactor` never appears in `Redact` output, even when URL-encoded.
- [ ] Bearer tokens (any value after `Bearer `) are replaced with `Bearer [redacted]` regardless of the configured secrets list.
- [ ] `sk-`-prefixed tokens are replaced with `[redacted]` regardless of the configured secrets list.
- [ ] Absolute paths matching the review root are rendered relative (or as `[root]`) when a review root is configured.
- [ ] When no review root is configured, path redaction is a no-op (empty string passes through unchanged).
- [ ] `Redactor` is safe for concurrent use from multiple goroutines.
- [ ] Redaction applies at the sink level — no log record at any level (including debug) bypasses it.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestRedact_ExactSecretMatch` — a configured secret value is replaced with `[redacted]`.
- `TestRedact_URLEncodedSecretMatch` — a secret containing `/`, `+`, `=` is scrubbed in both literal and URL-encoded forms.
- `TestRedact_BearerTokenAnyValue` — `Bearer <any-token>` is replaced with `Bearer [redacted]` even when the token is not in the secrets list.
- `TestRedact_SKKeyPattern` — `sk-<any-value>` is replaced with `[redacted]` even when not in the secrets list.
- `TestRedact_ForeignBearerAndSKTokens` — mirrors `TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens`: a provider echo of `Bearer sk-OTHER-leaked-99` is fully scrubbed.
- `TestRedact_AbsolutePathRelativized` — a message containing the review root prefix has it replaced with the relative form.
- `TestRedact_PathRedactionNoRoot` — when review root is empty, absolute paths pass through unchanged.
- `TestRedact_PathRedactionNoMatch` — when the message does not contain the root, it passes through unchanged.
- `TestRedact_MultiplePassesCompose` — a message containing a secret AND an absolute path has both redacted in a single call.
- `TestRedact_EmptyMessage` — empty string returns empty string without panic.
- `TestRedact_ConcurrentSafety` — multiple goroutines call `Redact` on the same `Redactor` simultaneously without races (run with `-race`).
- `TestRedact_SecretNotInMessage` — when the secret is not present, the message is returned unchanged.
- `TestRedact_NoSecretsConfigured` — with no secrets, only token-pattern and path redaction apply.

**Test Files:**
- `internal/log/redact_test.go`

## Risk Mitigation
| Risk | Mitigation |
|------|-----------|
| Redaction regex misses a new secret shape | Bearer and sk- patterns are generic (match any value), catching unknown tokens. Exact-match catches the configured key. CI scan for key-shaped strings in test logs as defense-in-depth. |
| Performance cost of regex on every log record | Compiled once at package level. Redaction runs only on records that pass the level filter, not on hot paths. |
| Path redaction false-positive on partial prefix match | Only exact prefix matches of the review root are replaced (use `strings.HasPrefix` or `strings.ReplaceAll` on the full root string, not substrings). |
| URL-encoded secret differs across encoders | Use `url.QueryEscape` to match Go's HTTP form encoding. Document that other encoding forms (e.g., percent-encoding with uppercase hex) are out of scope for this task. |

## Dependencies
- Task 01 (core-logging-api) — provides the `internal/log` package structure and sink interface that `redact.go` plugs into.

## Definition of Done
- [ ] `internal/log/redact.go` created with `Redactor`, `NewRedactor`, `Redact`.
- [ ] `internal/log/redact_test.go` created with all unit tests listed above.
- [ ] `go test ./internal/log/...` passes with `-race` flag.
- [ ] `go vet ./internal/log/...` clean.
- [ ] All existing tests (`go test ./...`) continue to pass — no regressions.
- [ ] Redaction helpers are integration-ready for Task 01's sink pipeline.
