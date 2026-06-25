---
id: mem-2026-06-23-4691b2
question: "Where should a new optional language-scope field live in the persona/agent schema?"
created: 2026-06-23
last_retrieved: ""
sprints: []
files: [internal/registry/config.go, internal/payload/scope.go, personas/personas.go, internal/registry/persona.go]
tags: [clarifications, epic-9.0_persona_ecosystem, schema, AgentConfig, persona, registry]
retrievals: 0
status: active
type: clarifications
---

# Where should a new optional language-scope field live in the

## Decision

Add a new Language []string field on AgentConfig in registry.yaml — do NOT reuse Scope []string (it is fully occupied: it means prompt-injection focus categories injected via payload.ScopeFocus(), internal/registry/config.go:294-303). Do NOT use YAML frontmatter in .md persona files — those are raw Go text/template strings with no frontmatter parser (personas/personas.go:12-41). The AgentConfig pattern (optional, omitempty, nil=no constraint, validated at load) is the correct precedent, mirroring how Scope was added in Epic 2.2.

RESOLVED 2026-06-24 — canonical stored form is WITHOUT leading dot, lowercased (e.g. ["go","ts"]). YAML accepts both "go" and ".go"; applyDefaults canonicalizes each entry (trim space, strip one leading dot, lowercase), mirroring the existing MinSeverity/Role load-time canonicalization. validateAgent rejects empty entries + control chars (mirror the Scope guard at config.go:673-679) but enforces no known-language allow-list (forgiving for third-party persona authors). Matching strips the dot + lowercases filepath.Ext(finding.File) via normalizeExt so both sides compare in the same canonical form.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
- internal/payload/scope.go
- personas/personas.go
- internal/registry/persona.go
