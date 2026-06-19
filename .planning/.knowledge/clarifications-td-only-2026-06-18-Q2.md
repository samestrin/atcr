---
id: mem-2026-06-18-33ffb3
question: "For a drain test asserting a review is not interrupted, is NotEqual(RunInterrupted) sufficient or should Equal(RunInProgress) also be asserted?"
created: 2026-06-18
last_retrieved: ""
sprints: []
files: [internal/mcp/shutdown_test.go, internal/fanout/status.go, internal/mcp/handlers_test.go, internal/registry/config.go]
tags: [td-clarification, td-only, testing, stale-inference, drain-test, assertion-design]
retrievals: 0
status: active
type: td-clarification
---

# For a drain test asserting a review is not interrupted, is N

## Decision

NotEqual(RunInterrupted) is the semantically correct and sufficient assertion for an AC that says "client disconnect must NOT interrupt the review." Equal(RunInProgress) overconstains: it fails for RunStale if timeout_secs + staleGraceSecs has elapsed. In the MCP shutdown tests, stale fires only after StartedAt + timeout_secs + staleGraceSecs (staleGraceSecs=60, status.go:73; default timeout_secs=600, registry/config.go:22) — a 660-second window, impossible in a 200ms drain test. RunCompleted is also impossible when blockingCompleter blocks until context cancellation and serverShutdown=false. If Equal(RunInProgress) is retained for extra specificity, pin timeout_secs: 3600 in the project config written by writeReviewConfig (handlers_test.go:114) with a comment that it must stay >> shutdownDrain to prevent false stale failures.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/mcp/shutdown_test.go
- internal/fanout/status.go
- internal/mcp/handlers_test.go
- internal/registry/config.go
