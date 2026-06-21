---
id: mem-2026-06-21-b709b9
question: "How should SecretValues expose diagnostics when an APIKeyEnv resolves empty or below the minimum length floor — via a logger/ctx parameter or a different mechanism?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/fanout/secrets.go, internal/mcp/handlers.go, cmd/atcr/review.go, cmd/atcr/resume.go]
tags: [td-clarification, td-only, observability, SecretValues, logger, api-design, fanout, secrets]
retrievals: 0
status: active
type: clarifications skill, td-only mode, 2026-06-21
---

# How should SecretValues expose diagnostics when an APIKeyEnv

## Decision

Return a []string of warning messages as a second return value from SecretValues() rather than adding a logger or context parameter. SecretValues currently imports only "os" and is a pure value-resolution helper; threading a *slog.Logger or context.Context would introduce a new package dependency and violate the single-responsibility boundary. Each of the three call sites (mcp/handlers.go:248 via e.logger(), cmd/atcr/review.go:207, cmd/atcr/resume.go:127) already holds its own logger and can log the returned warnings immediately. Warning strings should describe the slot (e.g., "APIKeyEnv FOO resolved empty, skipping redaction slot") but never include the secret value.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/secrets.go
- internal/mcp/handlers.go
- cmd/atcr/review.go
- cmd/atcr/resume.go
