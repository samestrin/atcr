---
id: mem-2026-06-30-aa0f9f
question: "Should out-of-scope-tagged findings be exempt from a patch-grounding drop?"
created: 2026-06-30
last_retrieved: ""
sprints: []
files: [personas/_base.md, internal/payload/scope.go, internal/reconcile/gate.go, internal/reconcile/merge.go]
tags: [clarifications, epic-14.1_verification_grounding, scope]
retrievals: 0
status: active
type: clarifications
---

# Should out-of-scope-tagged findings be exempt from a patch-g

## Decision

Exempt findings tagged `out-of-scope` from any patch-grounding/AC3-style drop that forbids commentary outside the diff's +/- lines. `out-of-scope` is a deliberate, AC-traced (AC 06-04), gate-integrated feature: personas/_base.md instructs reviewers (tool-assisted mode) to tag pre-existing issues in unchanged code with category=out-of-scope; internal/payload/scope.go routes them so the reconciler annotates rather than drops; internal/reconcile/gate.go ties the category to AC 06-04 ("Findings annotated out-of-scope never count... that exclusion takes precedence"); internal/reconcile/merge.go's CategoryOutOfScope is "annotated rather than promoted: kept in artifacts, counted in summaries, listed in a separate report section, excluded from severity gate" with fail-closed modal voting (ModalCategory) so a reviewer majority can't manufacture an out-of-scope dodge. Dropping these findings uniformly would silently delete existing, tested product behavior that is orthogonal to the hallucination problem (a correctly-tagged out-of-scope finding is real and grounded, just intentionally outside the diff).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- personas/_base.md
- internal/payload/scope.go
- internal/reconcile/gate.go
- internal/reconcile/merge.go
