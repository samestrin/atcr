---
id: mem-2026-06-26-eb0038
question: "For migration round-trip tests, is semantic equivalence (same item set + field values) sufficient, or is byte-identity required?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [.planning/epics/active/12.1_technical_debt_format_migration.md]
tags: [clarifications, epic-12.1_technical_debt_format_migration, testing, migration, round-trip]
retrievals: 0
status: active
type: clarifications
---

# For migration round-trip tests, is semantic equivalence (sam

## Decision

Semantic equivalence is the correct bar — byte-identity is neither achievable nor meaningful for migration round-trips where the generator normalizes presentation (e.g., re-clusters by path theme). A Go round-trip test asserting the same item set and field values is sufficient. No committed artifact (e.g., README.generated.md) is required — it would become stale noise and adds no gate the test does not already enforce. If a one-time human-inspectable diff is useful during a migration run, a --dry-run stdout flag on the generate command is preferable to a committed file. Evidence: AC2 in 12.1 specifies "verified by full round-trip: table → shards → table" (the mechanism, not the artifact); the generate command "may group by path theme for presentation," confirming byte-identity is impossible. The adversarial fixture corpus already guards against silent data corruption.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/epics/active/12.1_technical_debt_format_migration.md
