# Request Correlation (Review ID and Agent Name)

**Priority:** IMPORTANT

## Overview

ATCR currently emits diagnostic logs without any correlation identifiers. When multiple reviews run concurrently or a single review fans out across several agents, there is no way to trace which log lines belong to which review or which agent. The structured logging initiative introduces a shared `internal/log` package that threads a review ID and an agent name through every log line, making logs grep-able and traceable.

Two new functions — `WithReviewID` and `WithAgent` — wrap a `*slog.Logger` so that every subsequent log call automatically includes the correlation attributes. A context-based logger (`FromContext` / `NewContext`) is required so these values propagate across package boundaries without explicit parameter passing.

The integration touches three layers: the CLI entry point (`cmd/atcr/review.go`) sets the review ID after `PrepareReview` resolves it; the fanout engine (`internal/fanout/engine.go`) sets the agent name before each agent invocation; and the skeptic path (`internal/verify/invoke.go`) must wire the shared logger so skeptic failures appear in the same log stream.

## Key Concepts

### Correlation API

> Source: [original-requirements.md:Core Design — Correlation API]

The `internal/log` package exposes two functions that derive a new `*slog.Logger` with an added attribute:

- `WithReviewID(logger, reviewID)` — attaches the review ID to every log line.
- `WithAgent(logger, agentName)` — attaches the agent name to every log line.

Both return a new logger; they do not mutate the input. Callers chain them as needed.

### Review ID Threading

> Source: [codebase-discovery.json:integration_points]

After `fanout.PrepareReview` returns in `cmd/atcr/review.go:runReview` (line 72), `prep.ID` is available. At that point the caller invokes `log.WithReviewID(logger, prep.ID)` and places the resulting logger back into the context so all downstream logs carry the review ID.

> Source: [codebase-discovery.json:integration_gaps]

A timing gap exists: payload building happens inside `PrepareReview`, but `WithReviewID` is called after it returns. `BuildEntries` must either read the logger from a context set earlier or `PrepareReview` must accept a logger parameter so payload-level logs also include the review ID.

### Agent Name Threading

> Source: [codebase-discovery.json:integration_points]

In `internal/fanout/engine.go:invokeAgent`, at the start of each agent invocation, the code calls `log.WithAgent(logger, a.Name)` to attach the agent name. The `Agent` struct (line 57) already carries a `Name` field, so no structural change is needed on the agent side.

### Logger Injection into the Engine

> Source: [codebase-discovery.json:integration_gaps]

`fanout.Engine` has no `*slog.Logger` field. `ExecuteReview` and `invokeSkeptic` construct the engine without a logger. A `WithLogger` option (or equivalent) must be added before `log.WithAgent` can be called inside `invokeAgent`.

### Context Logger Requirement

> Source: [codebase-discovery.json:integration_gaps]

`WithReviewID` and `WithAgent` are new capabilities that require a context-aware logger (`FromContext` / `NewContext`) to propagate values across packages. No such mechanism currently exists in the codebase.

### Skeptic Logger Wiring

> Source: [codebase-discovery.json:semantic_matches]

`internal/verify/invoke.go:invokeSkeptic` (line 49) constructs a throwaway `fanout.Engine` per skeptic invocation. It needs logger wiring so skeptic failures flow through the shared correlated log stream rather than being lost or duplicated.

## Code Examples

> Source: [original-requirements.md:Core Design — Correlation API]

```go
// internal/log/correlation.go
package log

// WithReviewID returns a logger that includes the review ID in every log line.
func WithReviewID(logger *slog.Logger, reviewID string) *slog.Logger

// WithAgent returns a logger that includes the agent name in every log line.
func WithAgent(logger *slog.Logger, agentName string) *slog.Logger
```

## Quick Reference

| Function / Concept | Location | Purpose |
|---|---|---|
| `log.WithReviewID` | `internal/log/correlation.go` | Attach review ID to logger |
| `log.WithAgent` | `internal/log/correlation.go` | Attach agent name to logger |
| `runReview` | `cmd/atcr/review.go:72` | Calls `WithReviewID` after `PrepareReview` |
| `Agent` struct | `internal/fanout/engine.go:57` | Carries `Name` for `WithAgent` |
| `invokeAgent` | `internal/fanout/engine.go` | Calls `WithAgent` before LLM call |
| `invokeSkeptic` | `internal/verify/invoke.go:49` | Needs logger wiring for skeptic logs |
| `ExecuteReview` | `internal/fanout/review.go` | Must pass logger to `NewEngine` via `WithLogger` |

## Related Documentation

- [Structured Logging Plan](../README.md)
- [Core Logging Package](core-logging-package.md)
- [Error Classification System](error-classification-system.md)
- [Secret and Path Redaction](secret-path-redaction.md)
