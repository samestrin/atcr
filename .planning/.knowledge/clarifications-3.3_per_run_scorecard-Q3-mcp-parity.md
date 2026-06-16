---
id: mem-2026-06-15-99ae20
question: "Should atcr_reconcile MCP handler emit local scorecards, or is the scorecard store intentionally CLI-only?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/mcp/handlers.go, cmd/atcr/reconcile.go, internal/scorecard/scorecard.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, mcp, scorecard, cli-mcp-parity, TD-005]
retrievals: 0
status: active
type: clarifications
---

# Should atcr_reconcile MCP handler emit local scorecards, or 

## Decision

MCP-driven reconciles SHOULD emit local scorecards — the store is not intentionally CLI-only. The codebase has an explicit parity contract: handleVerify's comment in handlers.go states "MCP and CLI emit identical artifacts for the same input," and the shared gate-threshold resolver is the documented pattern for cross-entry-point parity. original-requirements.md says "automatically at the end of atcr reconcile" with no CLI-only qualifier. The fix is: extract emitScorecard(reviewDir, reconcileRes) into a shared helper called from both cmd/atcr/reconcile.go and internal/mcp/handlers.go handleReconcile after RunReconcile succeeds — mirrors the shared gate-threshold resolver pattern (registry.ResolveGateThreshold) so the two layers cannot fork. Must land before Phase 5 docs: documenting that "atcr reconcile writes a scorecard" while MCP silently skips it is actively misleading. Tracked as TD-005 in sprint 3.3_per_run_scorecard's tech-debt-captured.md.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/mcp/handlers.go
- cmd/atcr/reconcile.go
- internal/scorecard/scorecard.go
