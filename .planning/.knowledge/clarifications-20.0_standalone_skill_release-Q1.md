---
id: mem-2026-07-11-366eea
question: "Should skill/findings-format.md inline the findings contract or point to a public docs URL, given the repo is private?"
created: 2026-07-11
last_retrieved: ""
sprints: [20.0_standalone_skill_release]
files: [skill/findings-format.md, docs/findings-format.md, .planning/sprints/active/20.0_standalone_skill_release/tech-debt-captured.md]
tags: [clarifications, sprint-20.0_standalone_skill_release, documentation, private-repo, standalone-skill]
retrievals: 0
status: active
type: clarifications
---

# Should skill/findings-format.md inline the findings contract

## Decision

Inline now; do not repoint to a public URL. The samestrin/atcr repo is currently private (confirmed via `gh repo view --json isPrivate` -> true), so a public docs URL would 404 for the same reason TD-002 documents for `go install` (private module, not on proxy.golang.org) — it is not a real option until Epic 33.2 makes the repo public. skill/findings-format.md already states the minimal 8-column/9-column contract inline; only the intro line (around line 3) needs rewording so docs/findings-format.md is framed as optional supplementary detail, not a required/dangling reference. Once the repo goes public (Epic 33.2), swap the trailing sentence for a real public URL as a follow-up TD item.

Justification:
- Repo privacy independently verified live via `gh repo view`, corroborated by TD-002 (tech-debt-captured.md) describing the identical failure mode.
- docs/findings-format.md and skill/findings-format.md content confirm the minimal contract is short enough to inline without redefining anything not already present.
- AC 01-03's "verbatim" requirement governed the original Story 1 move only; this TD item was explicitly carved out as a deliberate content decision for a follow-up, so editing this one line does not violate that constraint.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- skill/findings-format.md
- docs/findings-format.md
- .planning/sprints/active/20.0_standalone_skill_release/tech-debt-captured.md
