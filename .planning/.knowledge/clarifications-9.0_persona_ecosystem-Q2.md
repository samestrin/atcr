---
id: mem-2026-06-24-119fb7
question: "How should internal/personas library functions surface per-file read/parse errors: log via a standard logger, write to os.Stderr, or return alongside results?"
created: 2026-06-24
last_retrieved: ""
sprints: [9.0_persona_ecosystem]
files: [internal/personas/list.go]
tags: [clarifications, sprint-9.0_persona_ecosystem, error-handling, implementation]
retrievals: 0
status: active
type: clarifications /resolve-td session 2026-06-24
---

# How should internal/personas library functions surface per-f

## Decision

Return errors alongside results — do not write to os.Stderr from library functions and do not use a standard logger (none exists in internal/personas). The established pattern: listCommunity returns ([]PersonaMeta, error) and List propagates a walk error alongside gathered rows; the CLI caller (cmd/atcr/personas.go) writes warnings to stderr. For per-file failures, accumulate errors in a closure slice inside the WalkDir callback and return them joined alongside valid rows. This mirrors the unreadable-dir warning pattern documented in tech-debt-captured.md TD-011 ("surface per-row read/parse failures as a stderr warning mirroring the unreadable-dir warning").

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/list.go
