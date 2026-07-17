---
id: mem-2026-07-17-ba8496
question: "Should single-lens community personas (like simon, category=`bloat`) pin their prompt's Output Format CATEGORY column to their fixed lens value instead of the shared \"one lowercase word\" house style?"
created: 2026-07-17
last_retrieved: ""
sprints: [29.0_anti_slop_persona]
files: [docs/personas-authoring.md, personas/community/simon.md, personas/community_test.go, .planning/sprints/active/29.0_anti_slop_persona/plan/acceptance-criteria/01-02-simon-md-template-structure-focus.md, .planning/sprints/active/29.0_anti_slop_persona/tech-debt-captured.md]
tags: [clarifications, sprint-29.0_anti_slop_persona, architecture, process, personas]
retrievals: 0
status: active
type: clarifications
---

# Should single-lens community personas (like simon, category=

## Decision

No — every community persona's Output Format CATEGORY rule must stay the generic "one lowercase word" phrasing; pinning it is a house-wide convention decision that would need to apply to all personas uniformly (or not at all), never to one persona in isolation.

Justification:
- All 14 community persona .md files contain the byte-identical Rules sentence "CATEGORY is one lowercase word" (verified via grep across personas/community/*.md) — no persona is pinned today.
- docs/personas-authoring.md:100-119 defines this phrasing as the project-wide authoring contract and states "Keep the column format byte-for-byte — the reconciler parses it."
- The category word only needs to appear in the persona's own prompt template (Role/Focus/Output Format example) — that's the actual enforcement mechanism (fixture/category test gate), not runtime output pinning.
- plan/acceptance-criteria/01-02-simon-md-template-structure-focus.md Scenario 1 requires the 7-column Output Format contract to be byte-for-byte identical to the canonical sonny.md contract.
- No test gate in personas/community_test.go (or elsewhere) parses emitted/rendered output and asserts CATEGORY equals the persona's registry Category field — so leaving the generic phrasing carries no correctness regression.
- Sprint 29.0's own TD-001 (tech-debt-captured.md) reached the same conclusion: pinning simon alone would deviate from the established registry convention; any future change must be a house-wide decision applied to all personas in a dedicated persona-authoring polish sprint.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- docs/personas-authoring.md
- personas/community/simon.md
- personas/community_test.go
- .planning/sprints/active/29.0_anti_slop_persona/plan/acceptance-criteria/01-02-simon-md-template-structure-focus.md
- .planning/sprints/active/29.0_anti_slop_persona/tech-debt-captured.md
