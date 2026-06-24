---
id: mem-2026-06-23-8b369a
question: "Is a test-only assertion fix acceptable for TestNoPathValidationFieldsInOutput when the diff-smell gate flags it as over-simplified, or is an implementation-side guard needed?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [reconcile/adapter/json/adapter_test.go, reconcile/adapter/json/adapter.go, reconcile/finding.go, internal/reconcile/adapter/adapter.go]
tags: [clarifications, sprint-8.0_reconciler_library, testing, architecture, diff-smell, path-validation, structural-exclusion, json-adapter]
retrievals: 0
status: active
type: clarifications
---

# Is a test-only assertion fix acceptable for TestNoPathValida

## Decision

Test-only correction is fully acceptable and sufficient. reconcile.Finding (reconcile/finding.go:17-32) carries no PathValid/PathWarning/PathSuggestion/ClusterMerged fields — a compile-time guarantee. The encoder at adapter.go:133 calls stdjson.Marshal(encodeEnvelope{...}) which can only serialise fields that exist on the struct; path-validation fields cannot physically reach the adapter output. The diff-smell gate "test-only" flag is a false positive when the production code was never broken: the fix correctly changes the assertion from Go field-name strings to snake_case JSON wire names (path_valid etc.), which is the right thing to check on serialised output. A runtime guard (e.g., iterating JSON keys and rejecting unexpected names) would be speculative over-engineering against a threat the type system eliminates at compile time. The only hits for path_valid/path_warning/path_suggestion/cluster_merged in ./reconcile/ are comments and the test assertion — no struct field or JSON tag uses these names.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/adapter/json/adapter_test.go
- reconcile/adapter/json/adapter.go
- reconcile/finding.go
- internal/reconcile/adapter/adapter.go
