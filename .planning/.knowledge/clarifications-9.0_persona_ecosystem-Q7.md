---
id: mem-2026-06-24-97aae1
question: "bundle/ guard placement: should Install reject bundle/ names via a guard in install.go or centralize via Resolve in bundles.go?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/personas/install.go, internal/personas/bundles.go, internal/personas/bundles_test.go]
tags: [clarifications, sprint-9.0_persona_ecosystem, architecture, install, bundles, defense-in-depth]
retrievals: 0
status: active
type: clarifications skill — resolve-td NEEDS_REVIEW Item 7
---

# bundle/ guard placement: should Install reject bundle/ names

## Decision

The guard in install.go is not a duplication of bundle-routing logic — it is a distinct safety boundary ("defense in depth") that prevents a bundle/-prefixed name from ever reaching the single-persona HTTP fetch path. The two concerns are architecturally separate: bundles.go owns "what personas are in bundle X," install.go owns "this function must never accept a bundle name." Moving the rejection into Resolve/bundles.go (Option A) would be a category error — Resolve is not called by Install at all, and adding that call just to reject a prefix creates a dependency in the wrong direction. The existing doc-comment at install.go:17-18 already documents this design intent, and a dedicated test asserts the rejection at the correct layer. No code change is warranted; the comment is the resolution. Justification: internal/personas/install.go:17-21 (doc-comment records design intent); internal/personas/bundles.go:49-68 (Resolve takes a bare bundle name, never called from Install); internal/personas/bundles_test.go:250-254 (test covers rejection at the guard site).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/install.go
- internal/personas/bundles.go
- internal/personas/bundles_test.go
