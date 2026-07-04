---
id: mem-2026-07-03-81fe17
question: "Does td-migrate generate overwrite the technical-debt README, and where should new generated TD views be written?"
created: 2026-07-03
last_retrieved: ""
sprints: []
files: []
tags: [clarifications, epic-18.0_technical_debt_tooling, architecture, technical-debt, tdmigrate]
retrievals: 0
status: active
type: clarifications
---

# Does td-migrate generate overwrite the technical-debt README

## Decision

`td-migrate generate` renders a per-item ToC table (row per finding, verbatim fields) to STDOUT ONLY — it deliberately never overwrites .planning/technical-debt/README.md, precisely to avoid two tools fighting over that file. Any new generated TD artifact (e.g. an aggregated dashboard with counts by component/severity/age + a top-priority list) should therefore be written to its OWN distinct file, not README.md. Recommended convention: a flat sibling file with an all-caps generated name (e.g. .planning/technical-debt/DASHBOARD.md) that signals "machine-generated, do not hand-edit". The only existing aggregated view today is the hand-maintained Stats table at README.md top.

Evidence:
- internal/tdmigrate/generate.go:18-55 (per-item ToC render)
- internal/tdmigrate/run.go:24-25,112-134 (generate writes to stdout only, never overwrites README)
- .planning/technical-debt/README.md:5-14 (hand-maintained Stats aggregation)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
