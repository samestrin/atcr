---
id: mem-2026-07-19-133dfa
question: "Deciding whether duplicated small helper functions across packages warrant a shared internal/pathutil-style extraction now"
created: 2026-07-19
last_retrieved: ""
sprints: [32.0_sandbox_execution_environment]
files: [cmd/atcr/home.go, internal/tools/dispatch.go, internal/log/redact.go, cmd/atcr/quickstart.go]
tags: [clarifications, sprint-learning, 32.0_sandbox_execution_environment, process, maintainability, scope-deferral]
retrievals: 0
status: active
type: clarifications
---

# Deciding whether duplicated small helper functions across pa

## Decision

Before treating "N copies of a similar idiom" as a mechanical extraction, check whether the copies are actually drop-in identical — differing arity, return shape (error vs no error), and underlying algorithm (e.g. filepath.Rel vs strings.ReplaceAll vs inverse expand-not-relativize) mean a real shared helper needs a small package with multiple distinct exported functions, i.e. genuine interface design, not a copy-paste dedup. Combine that with a sprint/plan's declared COMPONENTS_TOUCHED scope: if the duplicate copies live in packages outside the current sprint's scope, deferring the unification (rather than scheduling it as an in-sprint task) is the correct call regardless of how easy the extraction would be.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/home.go
- internal/tools/dispatch.go
- internal/log/redact.go
- cmd/atcr/quickstart.go
