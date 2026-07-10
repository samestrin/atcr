---
id: mem-2026-07-09-174ca1
question: "Should `internal/personas/upgrade.go:179`'s `parseBinding` uniformly validate channel syntax (via `normalizeChannel`) for alias-bound bindings the way it does for scan-family bindings, closing the \"typo channel silently accepted on alias path\" gap?"
created: 2026-07-09
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [internal/personas/upgrade.go, internal/personas/catalog.go, internal/personas/catalog_test.go, .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md]
tags: [clarifications, sprint-19.7_live_model_resolution, correctness, resolver, binding-grammar]
retrievals: 0
status: active
type: clarifications
---

# Should `internal/personas/upgrade.go:179`'s `parseBinding` u

## Decision

No — this is tested, deliberate behavior, not a bug. Phase 3's adversarial review (sprint-plan.md §3.14.A, Epic 19.7) explicitly found this exact gap and landed a permanent regression test in §3.15 (commit 62ee3c29): `TestResolveModel_InvalidChannel_IgnoredOnAliasAndPin`, which pins "an unrecognized channel on an alias/pin binding is IGNORED (code correct — alias/pin short-circuit before validation)." AC 03-01/03-05 establish "channel is irrelevant to the alias path" for valid @stable/@latest values; the adversarial review extended that intentionally to invalid/typo channel literals too — alias and pin bindings short-circuit in ResolveModel before normalizeChannel runs, by design. Applying uniform validation would break this named passing test. Before recommending "uniform validation" fixes for channel/binding-grammar asymmetries in this resolver, grep sprint-plan.md for the actual adversarial-review task (not just the TD README's cited "§X.Y" section, which can point to an unrelated GREEN-implementation task) and check for a named test pinning the asymmetry as intentional.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/upgrade.go
- internal/personas/catalog.go
- internal/personas/catalog_test.go
- .planning/sprints/active/19.7_live_model_resolution/sprint-plan.md
