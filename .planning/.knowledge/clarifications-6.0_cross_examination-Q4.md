---
id: mem-2026-06-21-e15d90
question: "How should judge rulings (uphold/overturn/split) map to CI gate behavior for the debate stage?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/reconcile/gate.go, internal/reconcile/emit.go]
tags: [clarifications, epic-6.0_cross_examination, architecture, gate-semantics, debate, CI]
retrievals: 0
status: active
type: clarifications
---

# How should judge rulings (uphold/overturn/split) map to CI g

## Decision

The proposed mapping is correct and requires no new gate logic. IsFailing at gate.go:96-113 is a two-axis predicate on Verification.Verdict and f.Severity: overturn writes verdict:"refuted" and slots into the existing refuted exclusion (gate.go:104); uphold writes verdict:"confirmed" and satisfies --require-verified (gate.go:110); split overwrites Severity with the judge's settled value before Emit writes findings.json and the finding evaluates normally. The challenge_survived bool is added to the Verification struct as omitempty at emit.go:40-44 — pre-6.0 findings remain byte-identical on marshal.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/gate.go
- internal/reconcile/emit.go
