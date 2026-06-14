---
id: mem-2026-06-13-741bfa
question: "When an epic's terminology references old skill-based primitives (llm-tools registry, td-stream.txt, bob2-backup), does that mean it landed in the wrong repo?"
created: 2026-06-13
last_retrieved: ""
sprints: []
files: [internal/registry/config.go]
tags: [clarifications, epic-2.2_code_review_fanout_hardening, scope, architecture]
retrievals: 0
status: active
type: clarifications
---

# When an epic's terminology references old skill-based primit

## Decision

No — translate the epic's concepts onto atcr's primitives. The failure modes (identity drift, role drift, volume floods) are real atcr problems. atcr uses ~/.config/atcr/registry.yaml (confirmed at internal/registry/config.go:121-127), findings.txt, and personas (bruce/dax/greta/kai/mira/otto). The three new registry constraint fields (scope, min_severity, max_findings) are absent from internal/registry/config.go (no hits in grep) and need to be added in Go. Terminology mismatch in the epic prose is cosmetic; the underlying problem statement is valid for this repo.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
