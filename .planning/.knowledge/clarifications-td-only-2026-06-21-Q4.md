---
id: mem-2026-06-21-baa262
question: "How should render.go source the human-readable \"File not found\" label — from PathNotFoundWarning const, Title-case at render, or hardcoded?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/report/render.go, internal/reconcile/emit.go, internal/stream/validate.go]
tags: [td-clarification, td-only, regression-risk, PathWarning, PathNotFoundWarning, render, emit, golden-tests]
retrievals: 0
status: active
type: clarifications skill, td-only mode, 2026-06-21
---

# How should render.go source the human-readable "File not fou

## Decision

Keep the hardcoded "File not found" (Title-case) in both render.go and emit.go, but add a conditional guard. The design intentionally has two layers: stream.PathNotFoundWarning = "file not found" (lowercase) is the machine-readable JSON contract value stored in findings.json; "File not found" is the separate human display string. The correct fix for future-proofing is to render the hardcoded label when f.PathWarning == stream.PathNotFoundWarning, else fall back to esc(f.PathWarning). Unconditional esc(f.PathWarning) substitution collapses these layers, breaks 4+ golden tests, and produces lowercase output inconsistent with emit.go. Never change stream.PathNotFoundWarning to Title-case — it is a machine-readable field value, not a display string.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/report/render.go
- internal/reconcile/emit.go
- internal/stream/validate.go
