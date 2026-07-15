---
id: mem-2026-07-15-309ad5
question: "A finding says local-persona docs already cover model-tag translation manually, but automated AC2 coverage would need a live Ollama endpoint (infeasible in CI). Accept the doc hint as sufficient, add a non-live doc-consistency test, or something else?"
created: 2026-07-15
last_retrieved: ""
sprints: []
files: [docs/personas-install.md, personas/community_test.go, internal/personas/personas_test.go, cmd/atcr/docs_audit_test.go, .planning/technical-debt/README.md]
tags: [clarifications, epic-27.0_local_model_ollama_persona, testing, documentation, td-clarification]
retrievals: 0
status: active
type: clarifications
---

# A finding says local-persona docs already cover model-tag tr

## Decision

Prefer a non-live doc-consistency test over accepting the doc hint alone: assert each `local/<model>` slug in personas/community/index.json has a matching `ollama pull <tag>` string in docs/personas-install.md. This is proportionate — the finding's own fix text already scopes the ask down to exactly this non-live check, explicitly deferring live-endpoint automation as infeasible. The repo already has a proven, zero-network pattern for this (internal/personas/personas_test.go's TestDocs_ModelInMetadataConventionExists and cmd/atcr/docs_audit_test.go's docs-vs-source-of-truth drift suite), so this is a small addition to an established pattern, not new test scaffolding, and no existing test currently ties a local/<model> slug to its doc-cited pull tag (personas/community_test.go has no such check).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- docs/personas-install.md
- personas/community_test.go
- internal/personas/personas_test.go
- cmd/atcr/docs_audit_test.go
- .planning/technical-debt/README.md
