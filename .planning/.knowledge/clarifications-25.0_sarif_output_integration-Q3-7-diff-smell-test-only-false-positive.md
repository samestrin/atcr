---
id: mem-2026-07-14-f2f5da
question: "A diff-smell/reward-hack gate's \"test-only change\" flag is a false positive when the TD row's own Fix column asked only for test coverage"
created: 2026-07-14
last_retrieved: ""
sprints: [25.0_sarif_output_integration]
files: [internal/report/sarif.go, internal/report/sarif_test.go, .planning/technical-debt/README.md]
tags: [clarifications, sprint-25.0_sarif_output_integration, technical-debt, testing, reward-hack, diff-smell, resolve-td]
retrievals: 0
status: active
type: clarifications skill (resolve-td mode)
---

# A diff-smell/reward-hack gate's "test-only change" flag is a

## Decision

A deterministic diff-smell gate that flags a committed fix as "test-only / potentially over-simplified" is a generic heuristic, not proof of a missing production-code change. Before treating the flag as actionable, check the originating TD row's Fix column: if it explicitly asked only for new/stronger test assertions (not a behavior change), and the production code it targets is already correct and already documented as intended (via an AC's Edge Case / Security Considerations section, or a code comment), then the test-only commit is the correct and complete resolution — mark the row resolved, no further production change needed.

Observed across 5 rows in one sprint (ATCR epic 25.0, SARIF formatter): (1) pinning an existing fallback's exact string instead of a loose NotEmpty check, (2) strengthening a 2-item determinism fixture to 8 items to reliably catch a map-iteration regression, (3) pinning documented case-sensitive (non-normalized) matching behavior, (4) replacing a tautological self-referential assertion (`f(x) == f(x)` via two call sites) with a literal expected-value comparison — this is the correct fix pattern for that specific defect class, not evidence a production bug exists, and (5) adding a previously-untested edge-case shape (e.g. an empty string field) to an existing schema-conformance test's case table. In every case the TD row's own Fix column scope was narrower than "add production logic," and the gate's flag did not match the row's actual ask.

General check before answering "is this test-only fix a reward-hack": (a) read the TD row's Fix column verbatim — does it ask for a test change or a behavior change? (b) read the production code the test exercises — is its current behavior already correct and already justified by an AC/comment? If both hold, the test-only commit is legitimate coverage, not a reward-hack.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/report/sarif.go
- internal/report/sarif_test.go
- .planning/technical-debt/README.md
