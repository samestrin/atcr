# Testing with testify [IMPORTANT]

## Overview

Testify is the Go testing toolkit backing the reconcile library's test suite. The plan fixes the foundation as "Go 1.25; standalone nested module (`go.mod` at `./reconcile/`); stdlib-only library + `encoding/json` adapter; testify tests; golangci-lint; GitHub Actions CI." testify ships four sub-packages — assert, require, mock, and suite — but for the library's pure-logic merge/dedupe/cluster tests the `assert` and `require` packages are the workhorses. `mock` (LLM provider mocks) and `suite` (setup/teardown lifecycle hooks) apply to ATCR-internal fan-out engine tests that stay behind, not to the extracted library's deterministic core.

The library ships stdlib-only in every non-test file; testify and other test helpers are confined to `*_test.go`. This is the firm architecture recommendation: "Keep the reconcile/ module strictly stdlib-only in non-test files; testify and other test helpers are allowed only in *_test.go files." Tests are co-located with the code under test in the same package directory, follow the `*_test.go` naming convention, and run under `go test`. Table-driven tests with `t.Run` subtests benefit from assert's clear failure messages.

Extraction is mechanical-move-dominant, validated by the existing test corpus rather than new RED tests. AC#3 requires "all existing ATCR tests pass with zero behavioral change and byte-identical fixtures" — underwritten by the deterministic total-order output, where "sortMerged orders by severity desc, then file, then line so the same input yields byte-identical artifacts — required for a deterministic CI gate." Pure-logic tests move with the library; I/O/gate/validate/pathsuggest tests stay ATCR-internal. A runnable godoc example (AC#5) is added as `reconcile/example_test.go`.

## Key Concepts

### assert — non-fatal assertions
Non-fatal assertions that return `bool` and print friendly failure descriptions. Use `assert` for most test assertions in atcr unit tests.
> Source: [testify.md:assert Package]
> Source: [testify.md:Integration Notes (atcr)]

### require — fatal preconditions
Same functions as `assert`, but terminates the test immediately on failure by calling `t.FailNow()`. Must be called from the goroutine running the test function to avoid race conditions. Use `require` for critical preconditions (e.g., file existence, successful initialization) where test logic cannot continue on failure.
> Source: [testify.md:require Package]
> Source: [testify.md:Integration Notes (atcr)]

### Co-located *_test.go tests
The project's test framework is `go test`, with tests co-located in the same package directory as the code under test (`*_test.go` co-located), following the `*_test.go` naming convention. Existing example tests: `internal/reconcile/disagree_test.go` (BuildDisagreements) and `internal/reconcile/cluster_merge_test.go` (MergeJSONFindings_VerificationPrecedence).
> Source: [codebase-discovery.json:test_patterns]

### Table-driven subtests with t.Run
Table-driven tests with `t.Run` subtests benefit from assert's clear failure messages. This is the established ATCR pattern carried into the library suite.
> Source: [testify.md:Integration Notes (atcr)]

### stdlib-only non-test boundary
"Keep the reconcile/ module strictly stdlib-only in non-test files; testify and other test helpers are allowed only in *_test.go files." The plan's Framework/Technology line reinforces this: "stdlib-only library + `encoding/json` adapter; testify tests."
> Source: [codebase-discovery.json:architecture_recommendations]
> Source: [plan.md:Framework/Technology]

### Tests that move with the library vs stay ATCR-internal
`emit_test.go` (`TestReconcile_TwoReviewersAgreeHighConfidence`) is "Part of the test corpus that proves no behavioral change (AC#3). Pure-logic tests move with the library; I/O/gate/validate/pathsuggest tests stay ATCR-internal." Extraction is "mechanical-move-dominant and validated by the existing test corpus rather than new RED tests (new RED tests apply only to the library `Finding` type, the boundary adapter, and the JSON adapter)."
> Source: [codebase-discovery.json:semantic_matches]
> Source: [plan.md:Feature Analysis Summary]

### AC#3 — byte-identical fixtures
"ATCR imports the module; all existing ATCR tests pass with zero behavioral change and byte-identical fixtures (AC#3)." The mandate rests on deterministic total-order output: "sortMerged orders by severity desc, then file, then line so the same input yields byte-identical artifacts — required for a deterministic CI gate." The implementation strategy adds that "the fixtures across epics must remain byte-identical."
> Source: [plan.md:Planning Success Criteria]
> Source: [codebase-discovery.json:existing_patterns]
> Source: [plan.md:Implementation Strategy]

### AC#5 — runnable godoc example
"Go docs + a runnable example are published in the module README (AC#5)." A new `reconcile/example_test.go` with purpose "Runnable godoc example" is created (`based_on: null`).
> Source: [plan.md:Planning Success Criteria]
> Source: [codebase-discovery.json:files_to_create]

### mock — LLM provider mocks (ATCR-internal)
The `mock` package creates mock objects that implement interfaces, set expectations, and verify interactions. In atcr, use `mock` for LLM provider mocks in fan-out engine tests (per the base system's test pattern); use `httptest.NewServer` with mock handlers to simulate OpenAI-compatible providers. These tests stay ATCR-internal — the library's non-test code is stdlib-only and its pure-logic tests need no mocking.
> Source: [testify.md:mock Package]
> Source: [testify.md:Integration Notes (atcr)]

### suite — optional
The `suite` package structures tests using structs with setup/teardown lifecycle hooks, but does not support parallel tests. It is optional: "atcr tests can use standard `testing.T` with assert/require." The library suite uses standard `testing.T` plus assert/require rather than suite.
> Source: [testify.md:suite Package]
> Source: [testify.md:Integration Notes (atcr)]

## Code Examples

These are the verbatim testify usage examples from the source doc. No reconcile-library-specific test code is invented here — the library's own tests move mechanically from `internal/reconcile/*_test.go` and follow these same assert/require conventions.

### assert — non-fatal assertions

```go
package yours

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestSomething(t *testing.T) {
    // Option 1: Direct function calls
    assert.Equal(t, 123, 123, "they should be equal")
    assert.Nil(t, someObject)

    // Option 2: Create an assert object (recommended for multiple assertions)
    assert := assert.New(t)
    
    assert.Equal(123, 123, "they should be equal")
    assert.NotEqual(123, 456, "they should not be equal")
    
    if assert.NotNil(someObject) {
        // Safe to use someObject here since it's not nil
        assert.Equal("ExpectedValue", someObject.Value)
    }
}
```

> Source: [testify.md:assert Package]

### require — fatal preconditions

```go
import (
    "testing"
    "github.com/stretchr/testify/require"
)

func TestSomethingCritical(t *testing.T) {
    require := require.New(t)
    
    // If this fails, the test stops immediately
    require.NotNil(object, "object must exist to continue")
    require.Equal("value", object.Field) 
}
```

> Source: [testify.md:require Package]

## Quick Reference

| Topic | Detail | Source |
|-------|--------|--------|
| `assert` | Non-fatal assertions returning `bool`; friendly failure descriptions | testify.md:assert Package |
| `require` | Fatal assertions; calls `t.FailNow()` on failure; use for critical preconditions | testify.md:require Package |
| `mock` | Mock objects for interfaces; used for LLM provider mocks in ATCR fan-out tests | testify.md:mock Package |
| `suite` | Setup/teardown lifecycle hooks; optional; no parallel tests | testify.md:suite Package |
| Test framework | `go test` | codebase-discovery.json:test_patterns |
| Test location | Same package directory (`*_test.go` co-located) | codebase-discovery.json:test_patterns |
| Naming convention | `*_test.go` | codebase-discovery.json:test_patterns |
| Subtests | Table-driven tests with `t.Run` subtests | testify.md:Integration Notes (atcr) |
| Non-test dependency policy | stdlib-only; testify and test helpers only in `*_test.go` | codebase-discovery.json:architecture_recommendations |
| Byte-identical mandate (AC#3) | All existing ATCR tests pass, zero behavioral change, byte-identical fixtures | plan.md:Planning Success Criteria |
| Determinism backing AC#3 | sortMerged orders severity desc, then file, then line → byte-identical artifacts | codebase-discovery.json:existing_patterns |
| Runnable example (AC#5) | Go docs + runnable example in module README; new `reconcile/example_test.go` | plan.md:Planning Success Criteria + codebase-discovery.json:files_to_create |
| Tests that move | Pure-logic merge/dedupe/cluster/disagree tests (e.g., `emit_test.go`) | codebase-discovery.json:semantic_matches |
| Tests that stay | I/O/gate/validate/pathsuggest tests (ATCR-internal) | codebase-discovery.json:semantic_matches |
| Example test files | `disagree_test.go`, `cluster_merge_test.go`, `emit_test.go` | codebase-discovery.json:test_patterns + semantic_matches |

## Related Documentation

- [testify.md](../../../../specifications/packages/testify.md) — testify package spec (assert/require/mock/suite APIs and atcr integration notes)
- [go-module-stdlib.md](./go-module-stdlib.md) — sibling doc on the stdlib-only module boundary that testify tests sit inside
- [standard-library.md](../../../../specifications/packages/standard-library.md) — ATCR stdlib subsystem mapping and dependency-tree constraint
- [golangci-lint.md](../../../../specifications/packages/golangci-lint.md) — Go linter configuration and CI integration for module testing
- [codebase-discovery.json](../codebase-discovery.json) — test_patterns, existing_patterns, semantic_matches, files_to_create
- [plan.md](../plan.md) — 8.0 reconciler library plan goal, framework, and planning success criteria (AC#3, AC#5)
- [source.md](./source.md) — documentation source index for the 8.0 reconciler library plan
