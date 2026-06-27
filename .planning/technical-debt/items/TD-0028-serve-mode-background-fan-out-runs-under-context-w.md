---
id: TD-0028
order: 28
section: '[2026-06-17] From Sprint: epic-4.1'
date: "2026-06-17"
group: U
status: deferred
severity: MEDIUM
file: internal/mcp/handlers.go:88
category: REGRESSION_RISK
est_minutes: "60"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

Serve-mode background fan-out runs under context.WithoutCancel(ctx) so a SIGINT to the MCP server never cancels or marks an in-flight detached review interrupted; it is allowed to finish (intended MCP design) but never gets the interrupted marker CLI mode promises. (Deferred: Epic Plan 4.1.2)

## Fix

Decide whether detached MCP reviews should be marked interrupted on server shutdown; if so, thread a cancellable/interrupt-aware context or post-hoc marker into the background review path — a separate design from this CLI-focused epic.
