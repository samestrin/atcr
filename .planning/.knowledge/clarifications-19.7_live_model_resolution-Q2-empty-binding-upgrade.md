---
id: mem-2026-07-09-3cd9c4
question: "Additive optional binding field must fully gate new resolver logic, not partially (Sprint 19.7 Upgrade())"
created: 2026-07-09
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [internal/personas/upgrade.go, internal/personas/catalog.go]
tags: [clarifications, sprint-19.7_live_model_resolution, architecture, go, backward-compat]
retrievals: 0
status: active
type: clarifications
---

# Additive optional binding field must fully gate new resolver

## Decision

When adding new opt-in behavior gated by an additive/omitempty schema field (like persona `Binding`), the new logic must be skipped ENTIRELY when the field is empty/absent — not attempted-then-caught. Concretely: `internal/personas/upgrade.go:27-68`'s `Upgrade()` has zero `.Binding` references today; since an empty `Binding.Family` matches neither `aliasTable` nor `vendorPrefixTable`, calling the resolver unconditionally would hard-fail `atcr personas upgrade` for every persona shipping no binding (currently all 10) with "no alias, pin, or vendor-prefix strategy found." The fix isn't just a valid design choice — it's a hard backward-compat requirement (AC7 "backward-compatible with existing on-disk personas" + zero-migration). This generalizes a repeat pattern in this codebase (echoes the TD-006/TD-007 two-call-site drift history): an additive field's consumer must explicitly branch on "field absent → old code path, untouched" rather than assume the new logic degrades gracefully on its own.
- internal/personas/upgrade.go:27-68 (Upgrade() — no .Binding reference; existing isNewer/write path)
- internal/personas/catalog.go:176-199 (ResolveModel — empty Family matches no table, returns error)
- .planning/sprints/active/19.7_live_model_resolution/plan/acceptance-criteria/04-01-upgrade-resolves-advances-lock-slug-report.md and 04-02-resolution-isolated-to-upgrade-path.md (both silent on the bindingless-persona case — confirmed via grep, zero mentions)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/upgrade.go
- internal/personas/catalog.go
