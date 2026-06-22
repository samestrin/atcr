---
id: mem-2026-06-22-bdf8bf
question: "How does executor attribution work in the Epic 7.3 GitHub Action inline comments (AC3)?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/verify/executor.go, internal/stream/parser.go, internal/reconcile/emit.go]
tags: [clarifications, epic-7.3_github_action_pr_integration, architecture, executor, attribution, findings-format]
retrievals: 0
status: active
type: clarifications
---

# How does executor attribution work in the Epic 7.3 GitHub Ac

## Decision

Epic 7.0 is already landed. Executor attribution is NOT a separate column — it is embedded in the EVIDENCE field as the token `"; fix by <name>"` (e.g. `"Found by bruce, greta; confidence HIGH; fix by opus"`), written at `internal/verify/executor.go:138`. The 9-column findings.txt wire format has no 10th executor column (confirmed at `internal/stream/parser.go:26-29`). The GitHub Action should parse the `"fix by <name>"` token from EVIDENCE to populate "Suggested by" when present, and omit the clause entirely when that token is absent. findings.json also exposes the same data via the `Evidence` field.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/executor.go
- internal/stream/parser.go
- internal/reconcile/emit.go
