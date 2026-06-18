---
id: mem-2026-06-18-9d4f1f
question: "How to add structured Warn logging to a private Go function without threading a *slog.Logger through an exported API and its test call sites?"
created: 2026-06-18
last_retrieved: ""
sprints: []
files: [internal/fanout/resume.go, internal/fanout/resume_test.go]
tags: [td-clarification, td-only, error-handling, observability, slog, logging, go-patterns]
retrievals: 0
status: active
type: clarifications/td-only/2026-06-18
---

# How to add structured Warn logging to a private Go function 

## Decision

Return a typed error from the private function (only for genuine failures, not for expected ok=false cases), then call slog.Warn at the caller (the exported function) with path/error context. This keeps the exported signature unchanged — no cascade to test call sites — while satisfying structured-log greppability. Use Go's package-level slog.Warn rather than threading a *slog.Logger when no context is available at the call site. Example: agentStatusName changed to (string, bool, error) where error is set only on os.ReadFile/json.Unmarshal failures; CompletedAgents calls slog.Warn("corrupt agent status", "path", path, "err", err) on non-nil error.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/resume.go
- internal/fanout/resume_test.go
