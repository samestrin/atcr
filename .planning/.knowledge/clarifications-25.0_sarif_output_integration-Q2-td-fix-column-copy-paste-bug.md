---
id: mem-2026-07-14-8d9480
question: "A technical-debt table row's Problem and Fix columns can be corrupted by copy-paste from an adjacent, unrelated row"
created: 2026-07-14
last_retrieved: ""
sprints: [25.0_sarif_output_integration]
files: [.planning/technical-debt/README.md, internal/report/sarif.go]
tags: [clarifications, sprint-25.0_sarif_output_integration, technical-debt, process, td-table, data-integrity, resolve-td]
retrievals: 0
status: active
type: clarifications skill (resolve-td mode)
---

# A technical-debt table row's Problem and Fix columns can be 

## Decision

When a TD row's Fix column reads as unrelated to its own Problem column (e.g. the Problem is about an empty artifactLocation.uri but the Fix talks about a severity diagnostic in a different function), check the immediately adjacent row(s) in the table for a byte-identical Fix column — this is a data-entry/merge bug (likely from table generation, dedup, or reconciliation tooling), not a real proposal. Example: `.planning/technical-debt/README.md` row for internal/report/sarif.go:178 (empty File → empty uri) had a Fix column verbatim-copied from the adjacent row for the same file:line about sarifLevel's severity fallback — a completely different concern.

Resolution pattern: (1) do not implement the mismatched Fix text, (2) locate the row's original, uncorrupted capture if one exists (e.g. tech-debt-captured.md from the originating sprint, which predates any later reconciliation/merge step) and use ITS Fix language as the source of truth, (3) the concrete remediation is usually a TD-table text correction, not a code change — especially when the actual current behavior is already correct/AC-mandated pass-through and the row was originally captured as accepted, deferred debt.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/technical-debt/README.md
- internal/report/sarif.go
