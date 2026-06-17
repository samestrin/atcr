# Secret and Path Redaction `CRITICAL`

## Overview

ATCR must never emit registry API keys, bearer tokens, or absolute repo paths in log output. Redaction is enforced at the sink level so that no log level — including debug — can leak secrets to disk or stdout.

The existing `internal/llmclient/client.go:redactErrorSnippet` function already scrubs bearer and `sk-`-prefixed tokens from provider error bodies. The new `internal/log` package reuses those regexes and adds path redaction relative to the review root.

Path redaction requires the review root to be available to the logger. The API provides a mechanism (e.g., `WithRoot(logger, root)` or a context value) to supply this root so absolute paths can be rendered relative to it.

## Key Concepts

### Secret Redaction at the Sink

Secrets are matched by known key names and value shapes (bearer tokens, `sk-` prefixed keys) and replaced before the record is emitted. Provider-specific error bodies are logged at debug level only.

> Source: [original-requirements.md:Redaction and Security]

### Existing Redaction Regexes

The `redactErrorSnippet` function in `internal/llmclient/client.go` (line 342) already defines the patterns:

> Source: [internal/llmclient/client.go:redactErrorSnippet]

```go
snippet = bearerTokenPattern.ReplaceAllString(snippet, "Bearer [redacted]")
snippet = skKeyPattern.ReplaceAllString(snippet, "[redacted]")
```

### Existing Regression Tests

`TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens` in `internal/llmclient/client_test.go` (line 329) validates the existing redaction. New `internal/log` redaction tests should mirror these cases.

> Source: [internal/llmclient/client_test.go:TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens]

### Path Redaction Requires Review Root

Rendering absolute paths relative to the review root requires the root path to be available to the logger. The `internal/log` API must provide a way to set the root (e.g., `WithRoot(logger, root)` or a context value).

> Source: [codebase-discovery.json:INTEGRATION_GAPS]

### Redaction API Location

Redaction helpers will live in `internal/log/redact.go`.

> Source: [plan.md:Technical Planning Notes — Security]

```go
// internal/log/redact.go
// Redaction helpers for secrets and absolute paths
```

## Code Examples

### Secret Scrubbing (existing pattern)

From `internal/llmclient/client.go:redactErrorSnippet`:

```go
snippet = bearerTokenPattern.ReplaceAllString(snippet, "Bearer [redacted]")
snippet = skKeyPattern.ReplaceAllString(snippet, "[redacted]")
```

> Source: [internal/llmclient/client.go:redactErrorSnippet]

## Quick Reference

| Concern | Approach | Source |
|---------|----------|--------|
| Secret redaction location | Sink level | plan.md |
| Secret patterns | `bearerTokenPattern`, `skKeyPattern` | internal/llmclient/client.go |
| Provider error bodies | Debug level only | plan.md |
| Path redaction | Relative to review root | codebase-discovery.json |
| Root injection | `WithRoot(logger, root)` or context value | codebase-discovery.json |
| Redaction helpers file | `internal/log/redact.go` | plan.md |
| Existing test to mirror | `TestComplete_ErrorBodyRedactsForeignBearerAndSKTokens` | internal/llmclient/client_test.go |

## Related Documentation

- [Structured Logging Plan](../README.md)
- [Core Logging Package](core-logging-package.md)
- [Error Classification System](error-classification-system.md)
- [Request Correlation](request-correlation.md)
