---
id: mem-2026-06-24-40a98d
question: "What is the specified behavior for isNewer when both local and remote persona versions are non-semver strings (e.g. both-non-semver case in atcr personas upgrade)?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/personas/upgrade.go]
tags: [clarifications, sprint-9.0_persona_ecosystem, scope, implementation]
retrievals: 0
status: active
type: clarifications /resolve-td session 2026-06-24
---

# What is the specified behavior for isNewer when both local a

## Decision

Specified/intended behavior — no fix needed. When both local and remote versions fail semver parsing, isNewer returns (local != remote), so any version difference is treated as an upgrade. This is AC 02-06 Edge Case 1: "treats any version change as newer when semver parse fails." Recorded in tech-debt-captured.md TD-009: "this is the specified behavior, not a defect." Valid semver on both sides compares structurally; when exactly one side is valid semver, isNewer returns false (treat as up-to-date). The both-non-semver any-difference=upgrade behavior should be closed as accepted/specified in TD tracking, not fixed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/upgrade.go
