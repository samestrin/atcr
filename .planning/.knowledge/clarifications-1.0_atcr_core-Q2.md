---
id: mem-2026-06-11-e7fc24
question: "How is host-review findings format kept equivalent to pool agent findings format, and can it be verified statically?"
created: 2026-06-11
last_retrieved: ""
sprints: [1.0_atcr_core]
files: [internal/stream/parser.go, skill/SKILL.md, skill_test.go, internal/stream/parser_test.go]
tags: []
retrievals: 0
status: active
type: project
---

# How is host-review findings format kept equivalent to pool a

## Decision

Host and pool findings format equivalence is a structural invariant, not a runtime property: both flow through the single shared ParseSource parser against one Version constant (internal/stream/parser.go:14, "# atcr-findings/v1") and PerSourceColumns=8 (parser.go:27). There is no separate "host format" to drift. The only authorship difference is who fills REVIEWER: pool agents emit 7 columns and the engine appends REVIEWER (parser.go:76,104-110); the host writes the full 8-column row with REVIEWER=host (skill/SKILL.md:67). Therefore a live provider run cannot reveal a format divergence the shared parser precludes — static verification (TestSkill_HostFindingsFormat at skill_test.go:62-67 + internal/stream/parser_test.go) is sufficient for AC 05-02 Scenario 5's format intent. A live end-to-end run validates orchestration, not format, and is a separate optional maintainer action under the no-keys boundary.</answer>
<parameter name="tags">clarifications, sprint-1.0_atcr_core, architecture, findings-format, stream-parser, testing

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/stream/parser.go
- skill/SKILL.md
- skill_test.go
- internal/stream/parser_test.go
