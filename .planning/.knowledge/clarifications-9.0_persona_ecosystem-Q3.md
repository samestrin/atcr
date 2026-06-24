---
id: mem-2026-06-23-0258f9
question: "Should HTTP client endpoints (like a community personas registry URL) be configurable or hardcoded?"
created: 2026-06-23
last_retrieved: ""
sprints: []
files: [internal/ghaction/client.go, internal/llmclient/client.go, internal/registry/config.go]
tags: [clarifications, epic-9.0_persona_ecosystem, architecture, testability, http-client]
retrievals: 0
status: active
type: clarifications
---

# Should HTTP client endpoints (like a community personas regi

## Decision

Use a configurable URL with a hardcoded default constant. The project-wide pattern is a struct field with a named default constant, so tests can point the client at an httptest.NewServer stub. Examples: ghaction.Client.APIURL (internal/ghaction/client.go:24), llmclient.Invocation.BaseURL (internal/llmclient/client.go:127). Provider base_url is also user-configurable in registry YAML (internal/registry/config.go:40). Never hardcode unconditionally — always expose the field for test injection.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/ghaction/client.go
- internal/llmclient/client.go
- internal/registry/config.go
