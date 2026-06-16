---
id: mem-2026-06-15-c370bd
question: "Why does internal/scorecard write diagnostics to os.Stderr instead of an injected io.Writer, and what would it take to change?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/scorecard/store.go, internal/scorecard/scorecard.go, internal/mcp/handlers.go, cmd/atcr/reconcile.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, scope, diagnostics, mcp-cli-parity, epic-3.4]
retrievals: 0
status: active
type: clarifications
---

# Why does internal/scorecard write diagnostics to os.Stderr i

## Decision

Scorecard store/emit diagnostics intentionally write to process-global os.Stderr (8 sites across internal/scorecard/store.go:95,125,135,190 and internal/scorecard/scorecard.go:120,197,234,240) because the writes are explicitly best-effort ("errors logged, never returned"). Threading an injectable io.Writer through Append/ReadRecords/FindByRunID/Emit/EmitForReconcile is a package-wide signature change touching every caller across two entry-point packages (cmd/atcr and internal/mcp) and crosses the sprint's group-8 file boundary into cmd/atcr. The CLI side has a clean precedent (other cobra commands use cmd.ErrOrStderr()), BUT the MCP path internal/mcp/handlers.go:218 calls scorecard.EmitForReconcile with no cobra cmd in scope, so there is no writer to thread there — the precedent does not transfer to the MCP/CLI-parity bridge (TD-005) that EmitForReconcile exists to keep identical. Decision: this is a designed package-API change for a non-correctness concern, not an inline fix — deferred to new Epic Plan 3.4 (scorecard-diagnostics-writer), which must resolve the MCP no-cmd gap (e.g. a Logger field on an emitter struct) as one change rather than an inline patch.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/store.go
- internal/scorecard/scorecard.go
- internal/mcp/handlers.go
- cmd/atcr/reconcile.go
