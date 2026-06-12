---
id: mem-2026-06-11-f6c763
question: "What is the effective roster for `atcr doctor` — does `serial_agents` exist as a schema field, and how does it relate to `rate_limited`?"
created: 2026-06-11
last_retrieved: ""
sprints: []
files: [internal/registry/project.go, internal/registry/config.go]
tags: [clarifications, epic-1.2_model_endpoint_selftest, architecture, schema, registry]
retrievals: 0
status: active
type: clarifications
---

# What is the effective roster for `atcr doctor` — does `ser

## Decision

Both mechanisms coexist at different schema layers. `ProjectConfig.SerialAgents []string` (project-config layer, `.atcr/config.yaml`) is a real first-class field for the serial execution lane. `AgentConfig.RateLimited bool` (registry-config layer, `registry.yaml`) is a per-agent annotation that is a separate concern. The effective roster for `atcr doctor` = union of `cfg.Agents + cfg.SerialAgents` (both ProjectConfig lanes) + all agents reachable via `fallback` chains in the registry. No new schema field is needed. ValidateAgainst (project.go:112-137) walks both lanes and enforces no agent appears in both. See internal/registry/project.go:31 (SerialAgents field), internal/registry/project.go:112-137 (ValidateAgainst), internal/registry/config.go:59 (RateLimited field), internal/registry/config.go:82-83 (Registry struct has no serial_agents at registry level).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/project.go
- internal/registry/config.go
