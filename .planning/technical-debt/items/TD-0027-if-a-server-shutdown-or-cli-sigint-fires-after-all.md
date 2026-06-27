---
id: TD-0027
order: 27
section: '[2026-06-18] From Sprint: epic-4.1.2'
date: "2026-06-18"
group: U
status: deferred
severity: MEDIUM
file: internal/fanout/review.go:361
category: CORRECTNESS
est_minutes: "30"
source: execute-epic-independent
reviewers: ""
confidence: ""
has_review_cols: false
---

## Problem

If a server shutdown (or CLI SIGINT) fires after all agents already succeeded but before ExecuteReview's interrupted := errors.Is(ctx.Err(), context.Canceled) check, a fully-completed run is stamped Interrupted=true and status.go:216 overrides RunCompleted to RunInterrupted (a false interrupted; inverse of AC4). Pre-existing in the CLI-shared path, newly reachable via MCP shutdown.

## Fix

Gate the interrupted marker on at least one agent ending in StatusTimeout/cancelled rather than purely on parent ctx.Err()==Canceled. NOTE: touches CLI-shared review.go (out of scope for epic 4.1.2's MCP-only change); window is microscopic and outcome benign (resume no-ops a complete run) - separate design. (WON'T-FIX 2026-06-18: --resume self-healing via ClearInterrupted (resume.go:220) already recovers a stale interrupted-on-complete; revisit in a backlog sprint if insufficient)
