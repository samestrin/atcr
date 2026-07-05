---
id: mem-2026-07-05-e0bdb7
question: "Audit/history ledger records write unconditionally with empty optional fields, never skip"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [internal/history/capture.go, internal/gitrange/resolver.go]
tags: [clarifications, epic-19.1_audit_trail, implementation, ledger-design]
retrievals: 0
status: active
type: clarifications
---

# Audit/history ledger records write unconditionally with empt

## Decision

When an append-only ledger record has an optional field that can't always be resolved (e.g. PR number on a local, non-CI review run), still write the record with that field empty/zero rather than skipping the write. Precedent: internal/gitrange/resolver.go:56 uses `omitempty` for DefaultBranch when unknown, and Epic 19.0's history.RecordReview only skips writing when there is genuinely nothing to persist (no pool findings file) — never to omit a record for a real run just because one optional field is unavailable. This keeps "one record per run" invariants literally true and keeps downstream query commands (e.g. `--pr` filters) simple, since they just find nothing for non-matching runs instead of needing a "was this even logged" edge case.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/history/capture.go
- internal/gitrange/resolver.go
