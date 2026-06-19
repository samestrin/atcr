---
id: mem-2026-06-18-9ec1ad
question: "Should the false-interrupted TOCTOU race at internal/fanout/review.go:361 be fixed (gate interrupted on agent cancellation), deferred to a design plan, or accepted as benign and closed won't-fix?"
created: 2026-06-18
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go:361, internal/fanout/status.go:212-217, .planning/epics/completed/4.1.2_mcp-detached-review-interrupt.md, .planning/epics/code-reviews/4.1.2_mcp-detached-review-interrupt/claude/review.md]
tags: [td-clarification, td-only, architecture, correctness, interrupt-detection, fanout, won't-fix]
retrievals: 0
status: active
type: td-clarification
---

# Should the false-interrupted TOCTOU race at internal/fanout/

## Decision

Accept as benign and close won't-fix. The race window is nanosecond-scale — the gap between runEngine returning and the ctx.Err() check at review.go:361 is a handful of instructions. A SIGINT arriving in that window stamps a fully-completed run as RunInterrupted, but the consequence is harmless: resume reads the pool, sees all agents present, and no-ops. The gating fix (require interrupted && anyAgentCancelled) is semantically correct for both CLI and MCP callers but touches CLI-shared review.go and was explicitly flagged "out of scope — separate design" in the TD row. The post-merge code reviewer independently called it a microscopic window. AC4 for epic 4.1.2 covers the inverse direction only ("no false completed for an interrupted run") and is not violated. Disposition: mark [/] with annotation "(Won't fix: nanosecond-scale TOCTOU between runEngine return and ctx.Err() check; false-interrupted outcome benign — resume no-ops a complete run; gating fix touches CLI-shared review.go, explicitly flagged separate design scope; AC4 covers inverse direction only)"

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go:361
- internal/fanout/status.go:212-217
- .planning/epics/completed/4.1.2_mcp-detached-review-interrupt.md
- .planning/epics/code-reviews/4.1.2_mcp-detached-review-interrupt/claude/review.md
