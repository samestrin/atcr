---
id: mem-2026-07-11-836258
question: "Is a test-only diff acceptable when a TD item's root cause is a test-isolation/fixture defect (a subtest that could pass vacuously), not a production behavior bug?"
created: 2026-07-11
last_retrieved: ""
sprints: [20.0_standalone_skill_release]
files: [cmd/atcr/backend_contract_test.go]
tags: [clarifications, sprint-20.0_standalone_skill_release, testing, resolve-td, over-simplification-gate]
retrievals: 0
status: active
type: clarifications
---

# Is a test-only diff acceptable when a TD item's root cause i

## Decision

Approve as complete. When a TD item says a test "does not isolate" a code branch (i.e., the assertion could pass regardless of which branch actually ran), the defect is in the test's fixture setup, not in the production code being exercised. Adding a decoy/negative-control fixture and asserting both the positive and negative outcome (mirroring an already-correct sibling subtest's pattern) is a test-only diff by necessity — there is no production code to change when production behavior was already correct and only the test's power to detect regressions was weak.

Justification:
- cmd/atcr/backend_contract_test.go — the "omitted argument resolves to .atcr/latest" subtest was strengthened by adding a second decoy review and explicitly repointing .atcr/latest at the non-newest review, then asserting the pointed-to review resolved and the decoy did not — mirroring the correct pattern already used in the sibling "bare id" subtest.
- General principle: test-isolation/vacuous-pass TD items (the test passes for the wrong reason) are resolved entirely within the test; a reviewer should distinguish this class from TD items alleging an actual behavioral defect, where a test-only diff would rightly be suspicious.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/backend_contract_test.go
