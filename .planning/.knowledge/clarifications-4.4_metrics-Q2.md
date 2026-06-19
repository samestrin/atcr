---
id: mem-2026-06-19-006eea
question: "Should the MCP metrics handler add token auth, or document local-only as the accepted control?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/mcp/handlers.go, internal/mcp/server.go]
tags: []
retrievals: 0
status: active
type: project
---

# Should the MCP metrics handler add token auth, or document l

## Decision

Document the local-only posture as the accepted security control and close the item — do not add a token check. atcr serve binds only &mcpsdk.StdioTransport{} (os.Stdin/os.Stdout) with no http.ListenAndServe or net.Listen anywhere in internal/mcp or cmd (internal/mcp/server.go:57,63-66). The cited handler handleMetrics already documents it is stdio-only with no HTTP listener (internal/mcp/handlers.go:421-423). The epic's Risk table and recorded 2026-06-19 clarification both establish local-only as the accepted control and mark an HTTP /metrics listener OUT of scope. A token check presupposes a networked transport that does not exist, so it would be speculative work (violates minimum-code/no-speculation). Reopen for token validation only if a networked transport is ever introduced.</answer>
<parameter name="tags">clarifications, epic-4.4_metrics, architecture, security, scope, mcp, stdio-transport

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/mcp/handlers.go
- internal/mcp/server.go
