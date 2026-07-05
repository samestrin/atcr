---
id: mem-2026-07-04-c57210
question: "What stable id/fingerprint should identify a finding across review runs for atcr's history trend tracking?"
created: 2026-07-04
last_retrieved: ""
sprints: []
files: [internal/verify/emit_findings.go, internal/debate/emit.go, reconcile/merge.go, reconcile/finding.go]
tags: [clarifications, epic-19.0_finding_history, implementation]
retrievals: 0
status: active
type: clarifications
---

# What stable id/fingerprint should identify a finding across 

## Decision

Derive the id as a short hash over File+Line+Problem only (drop Severity from the key), matching the existing debate.itemID sha256-truncated-hex pattern. This aligns with the codebase's two existing finding-identity conventions and avoids breaking "same finding across runs" continuity when severity is re-settled by debate/verify.

Justification:
- internal/verify/emit_findings.go:18-22 (FindingKey{File, Line, Problem}) and internal/debate/emit.go:35-39 both define "same finding" as file+line+problem — neither includes Severity.
- internal/debate/emit.go:93-96 (itemID) already implements "stable short hash": sha256.Sum256(File+"\x00"+Line+"\x00"+Kind+"\x00"+Problem) truncated to 8 bytes hex — a ready-made template.
- Severity is mutably rewritten after a finding is first produced (internal/debate/emit.go:122-124; reconcile/merge.go:42 documents severity as a merge-computed "max" across reviewers) — keying on Severity would silently mint a new id whenever severity is later re-settled, defeating trend tracking.
- reconcile/finding.go:18-32 confirms File, Line, Problem are core wire fields always present (no omitempty).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/emit_findings.go
- internal/debate/emit.go
- reconcile/merge.go
- reconcile/finding.go
