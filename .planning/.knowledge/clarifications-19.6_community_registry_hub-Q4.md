---
id: mem-2026-07-08-759821
question: "Should bundle installs enforce strict ValidateCommunityPersonaYAML or stay on permissive ValidateAgentYAML?"
created: 2026-07-08
last_retrieved: ""
sprints: [19.6_community_registry_hub]
files: [internal/personas/install.go, internal/personas/bundles.go]
tags: [clarifications, sprint-19.6_community_registry_hub, security, personas, go]
retrievals: 0
status: active
type: clarifications
---

# Should bundle installs enforce strict ValidateCommunityPerso

## Decision

Enforce strict ValidateCommunityPersonaYAML (option a) — this is already what the shipped code does today (InstallBundle calls Install, which always validates strictly), so no code change is needed. Bundle members are fetched from the same untrusted network source as standalone installs (same threat model per Clarification C3), and the two shipped bundles (django, go-production) already conform to the strict schema. The only gap (TD-012) is a missing bundle-specific strict-decode reject test — add that, don't relax validation.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/install.go
- internal/personas/bundles.go
