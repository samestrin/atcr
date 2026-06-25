---
id: mem-2026-06-24-4e0e03
question: "Does writing a persona .md file to ~/.config/atcr/personas/ make it immediately available to the running review pipeline without extra wiring?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/fanout/review.go, internal/registry/persona.go]
tags: [clarifications, sprint-9.0_persona_ecosystem, architecture, registry, persona-resolution, install]
retrievals: 0
status: active
type: clarifications
---

# Does writing a persona .md file to ~/.config/atcr/personas/ 

## Decision

Yes. fanout/review.go:143 wires PersonaDirs.Registry = filepath.Join(filepath.Dir(regPath), "personas") at ReviewConfig construction time, resolving to ~/.config/atcr/personas/ for the default registry location. registry/persona.go:58-70 (ResolvePersona) reads any .md file in that directory on the next invocation. No changes to persona.go or fanout/review.go are needed when adding install/lifecycle functionality — writing the file is sufficient. The PersonaDirs.Registry field is the live integration point that already exists.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/registry/persona.go
