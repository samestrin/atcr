---
id: mem-2026-07-22-ac07e0
question: "Relaxing an overly-strict test assertion (require.Equalf → require.GreaterOrEqualf) flagged by resolve-td's diff-smell gate as test_only — is this a legitimate fix or does it mask a hidden production defect?"
created: 2026-07-22
last_retrieved: ""
sprints: [33.0_final_documentation_sweep]
files: [internal/personas/test.go, internal/personas/community_fixture_test.go]
tags: [clarifications, sprint-33.0_final_documentation_sweep, testing, Go, resolve-td, diff-smell-gate]
retrievals: 0
status: active
type: clarifications
---

# Relaxing an overly-strict test assertion (require.Equalf →

## Decision

The test-only fix is correct; no production code change is warranted. Example: TemplateFixtureRunner.RunFixture / renderFixture (internal/personas/test.go:160-174) has exactly two return points, both hardcoding Total: 1 — there is no code path today that produces Total > 1, so relaxing require.Equalf(t, 1, ...) to require.GreaterOrEqualf(t, out.Passed, 1, ...) is currently a behavioral no-op against production semantics; it only removes a brittle implicit cardinality assumption from the test's contract, and the unchanged require.Equalf(t, out.Total, out.Passed, ...) line still enforces full-pass. General pattern: before accepting a diff-smell-flagged test-only relaxation, confirm the assertion being relaxed doesn't currently gate any observed production behavior (i.e., the relaxation only broadens tolerance for a case the production code cannot yet produce) — that distinguishes a legitimate test-clarity fix from a reward-hack weakening of a real guarantee.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/personas/test.go
- internal/personas/community_fixture_test.go
