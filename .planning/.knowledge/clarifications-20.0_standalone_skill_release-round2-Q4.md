---
id: mem-2026-07-11-c00c1f
question: "Where should a ground-truth command list live to avoid drift between cmd/atcr's Cobra registry (package main) and skill/SKILL.md's routing table, without an import cycle?"
created: 2026-07-11
last_retrieved: ""
sprints: [20.0_standalone_skill_release]
files: [cmd/atcr/docs_audit_test.go, skill/skill_test.go, cmd/atcr/main.go]
tags: [clarifications, sprint-20.0_standalone_skill_release, architecture, testing, resolve-td]
retrievals: 0
status: active
type: clarifications
---

# Where should a ground-truth command list live to avoid drift

## Decision

Don't create a new shared importable package (e.g. internal/cli) just to satisfy a test — that requires a production-code change to cmd/atcr/main.go merely to expose an API for a test to consume. Instead, add a NEW test inside cmd/atcr's own test package (package main, so it can freely call newRootCmd()) that reads skill/SKILL.md straight off disk (the same way cmd/atcr/docs_audit_test.go already reads docs/*.md and README.md off disk via its repoRootDir(t) helper) and asserts bidirectional set-equality between the real command names and the backtick-quoted `atcr <name>` references in SKILL.md. This closes the same gap with zero new packages, zero import-cycle risk, and zero production-code changes, by reusing an already-proven convention in this exact codebase (Epic 15.0's docs_audit_test.go) rather than inventing a new one.

Justification:
- cmd/atcr/main.go is package main; Go forbids importing a main package, which is why the original TD suggestion needed a new internal/cli package.
- cmd/atcr/docs_audit_test.go already has canonicalCommands() (walks newRootCmd().Commands() as ground truth) and repoRootDir(t) (ascends to find go.mod, then reads arbitrary repo files off disk) — both directly reusable for a SKILL.md cross-check with no new import.
- General principle for this codebase: when a cross-package ground-truth check is needed and one side is package main, prefer a filesystem-based test-only cross-check inside package main's own test suite over introducing a new shared package — it's cheaper, avoids import-cycle questions entirely, and follows established precedent (docs_audit_test.go, Epic 15.0).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/docs_audit_test.go
- skill/skill_test.go
- cmd/atcr/main.go
