---
id: mem-2026-07-14-b1bd4d
question: "A technical-debt row's Fix column can describe deferred/future work, not an immediate mandate to apply now"
created: 2026-07-14
last_retrieved: ""
sprints: [25.0_sarif_output_integration]
files: [.planning/technical-debt/README.md, internal/report/sarif.go]
tags: [clarifications, sprint-25.0_sarif_output_integration, technical-debt, process, td-table, resolve-td]
retrievals: 0
status: active
type: clarifications skill (resolve-td mode)
---

# A technical-debt row's Fix column can describe deferred/futu

## Decision

When a TD row's Fix column proposes a behavior change but the same row's intent_note (or the linked AC/sprint-plan) says the current behavior is an intentional, accepted trade-off, the Fix column should be read as aspirational future-state text, not an instruction to act now. Example: TD-002 (internal/report/sarif.go's sarifRules) proposed defaulting empty Category to "uncategorized," but AC 01-03 EC2 explicitly mandates pass-through and the sprint's own Phase 1 adversarial review had already accepted-and-deferred this exact issue with "Fix in: future sprint." Applying the Fix column literally would have broken a currently-passing, in-spec test and contradicted the AC.

General pattern: before treating a TD row's Fix column as a mandate, check (1) whether the row has an intent_note recording a different disposition, (2) whether the cited AC explicitly documents the current behavior as correct/in-scope, and (3) whether the original capture (tech-debt-captured.md, if present) frames the fix as "future sprint" rather than "resolve now." If any of these contradict the Fix column, the row is correctly resolved by confirming/documenting current behavior, not by implementing the Fix column's proposal — and no AC update is needed unless the future change is actually adopted.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/technical-debt/README.md
- internal/report/sarif.go
