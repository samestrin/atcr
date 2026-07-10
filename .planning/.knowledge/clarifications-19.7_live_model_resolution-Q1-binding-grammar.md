---
id: mem-2026-07-09-21fb6d
question: "Binding-string grammar: alias-shaped family typos defeat the fail-closed pin fallback (Sprint 19.7)"
created: 2026-07-09
last_retrieved: ""
sprints: [19.7_live_model_resolution]
files: [internal/personas/catalog.go]
tags: [clarifications, sprint-19.7_live_model_resolution, architecture, go, parsing]
retrievals: 0
status: active
type: clarifications
---

# Binding-string grammar: alias-shaped family typos defeat the

## Decision

When disambiguating a persona's raw `binding:` string into `Binding{Family, Channel, Pin}` by table lookup (channel-less string matches a known family table → Family; no match → treated as an explicit Pin), the "fail-closed on typo" guarantee only holds for bare-token families (e.g. `deepseek`, `qwen`, `glm` — no `/`): a typo fails `validateResolvedSlug`'s "/" check immediately with a clear error. It does NOT hold for vendor/tier-shaped families (e.g. `anthropic/claude-opus`) because they already contain `/` — a typo'd variant passes slug validation and is silently accepted as a valid-looking pin, only failing later as a downstream API error instead of the resolver's immediate "no strategy found" error. This is a structural gap in any string-based type-disambiguation scheme where one branch's "valid shape" test (contains `/`) overlaps with another branch's key format. Closing it requires either an explicit sigil (`pin:`) or a mandatory `@channel` suffix for family bindings — both trade away shorthand/expressiveness.
- internal/personas/catalog.go:131-141 (Family grammar contract for Phase 4's binding-string parser)
- internal/personas/catalog.go:148-166 (aliasTable keyed vendor/tier with `/`; vendorPrefixTable keyed bare token, no `/`)
- internal/personas/catalog.go:329-345 (validateResolvedSlug — requires a non-empty segment on both sides of the first `/`)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/catalog.go
