---
id: mem-2026-06-11-7dae42
question: "Should project provider/agent definitions live in a dedicated .atcr/registry.yaml or as new top-level providers:/agents: sections inside the existing .atcr/config.yaml?"
created: 2026-06-11
last_retrieved: ""
sprints: []
files: [internal/registry/config.go, internal/registry/project.go, internal/registry/persona.go, internal/registry/doc.go]
tags: [clarifications, epic-1.3_project_registry_overlay, architecture, registry]
retrievals: 0
status: active
type: clarifications
---

# Should project provider/agent definitions live in a dedicate

## Decision

Use a dedicated .atcr/registry.yaml. The existing .atcr/config.yaml is a pure "selection + settings" file (roster names, payload mode, timeout, fail_on) with no definition data; mixing definitions into it would break the clean structural parallel the current two-file model encodes. A separate file mirrors the user-level filename, maps onto an independent loader, and lets ValidateAgainst() merge the two registries before checking roster references — without touching ProjectConfig or its validation path.

Justification:
- internal/registry/config.go:81-93 — Registry holds Providers + Agents. ProjectConfig (project.go:27-38) holds only roster names + shared settings. Adding providers:/agents: keys to ProjectConfig would require extending decodeStrictYAML's strict-field set and conflating two conceptually distinct roles in one struct.
- internal/registry/project.go:51 — DefaultProjectConfigYAML() writes the comment "# Roster entries must match agent names in ~/.config/atcr/registry.yaml." This defines config.yaml as a consumer of the registry, not a contributor. Extending it with definitions inverts that contract.
- internal/registry/project.go:109-126 — ValidateAgainst(reg *Registry) takes a fully-loaded *Registry and checks roster names against reg.Agents. With a separate .atcr/registry.yaml, the call site merges project registry into user registry first, then calls ValidateAgainst — no change to the method itself.
- internal/registry/persona.go:14-17 — PersonaDirs already uses the "separate project-level file, project shadows user" pattern for personas. .atcr/registry.yaml follows that exact precedent.
- internal/registry/doc.go:1-4 — Package doc explicitly describes the two-tier model. A third file extends the tiers cleanly; folding definitions into config.yaml blurs the tier semantics.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
- internal/registry/project.go
- internal/registry/persona.go
- internal/registry/doc.go
