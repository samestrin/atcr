---
id: mem-2026-06-25-b1bfeb
question: "How should a new execution-stage evidence block be added to the findings JSON schema, and where does it land in the codebase?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [internal/reconcile/emit.go, reconcile/finding.go, reconcile/verification.go, reconcile/confidence.go, internal/reconcile/gate.go, internal/report/render.go, internal/verify/emit_findings.go]
tags: [clarifications, epic-11.0_executing_reviewers, architecture, schema, findings-json, evidence-exec, JSONFinding, omitempty]
retrievals: 0
status: active
type: clarifications
---

# How should a new execution-stage evidence block be added to 

## Decision

Use the same additive/omitempty pattern every prior epic has used. Add a *EvidenceExec struct field to JSONFinding in internal/reconcile/emit.go:62 with json:"evidence_exec,omitempty". The struct fields are: command string, exit_code int, output_excerpt string. Do NOT add to the public library reconcile/finding.go or reconcile/verification.go — ATCR-specific concerns all live in JSONFinding only (see PathValid, FixWarning, cluster_id as precedent). The "VERIFIED by definition" semantics already work: ConfidenceForVerdict at reconcile/confidence.go:18 promotes VerdictConfirmed to VERIFIED; the executor stamps both Verification.Verdict=confirmed,skeptic="repro" and the EvidenceExec block. No new confidence tier or gate predicate needed; IsFailing at internal/reconcile/gate.go:96 handles VERIFIED correctly today. Report badge ("Reproduced") = new branch in writeSkepticBlock at internal/report/render.go:365, keyed on f.EvidenceExec != nil. Three open design choices: (a) embedded vs top-level struct placement; (b) label vs enum for badge (strongly prefer label — new enum value changes the library Verification type); (c) output_excerpt length cap location.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/emit.go
- reconcile/finding.go
- reconcile/verification.go
- reconcile/confidence.go
- internal/reconcile/gate.go
- internal/report/render.go
- internal/verify/emit_findings.go
