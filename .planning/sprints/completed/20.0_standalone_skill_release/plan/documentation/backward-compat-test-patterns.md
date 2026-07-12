# Backward-Compatibility Contract Test Patterns

`[IMPORTANT]`

## Overview

The AC3 backward-compat contract test proves that the `docs/code-review-backend.md` `--output-dir` + reconcile contract stays stable, protecting the external claude-prompts private skills that depend on it. This is a repo-local test, not a new harness: it must be written as a standard Go `*_test.go` file using the stdlib `testing` package plus `testify` assertions, following the exact conventions already established by `cmd/atcr/review_test.go` and `cmd/atcr/reconcile_test.go` (codebase-discovery.json > existing_patterns > "Cobra subcommand + _test.go pairing"). No custom test runner, no separate fixture DSL — the same table-driven, subtest-based style used across the rest of `cmd/atcr/`.

Three conventions from the source docs anchor the test's mechanics. First, provider interaction (if the test needs to exercise the fan-out/review path rather than just reconcile) is mocked with `net/http/httptest.NewServer`, pointing the registry's `base_url` at `server.URL` so the real client code path is exercised with zero network (standard-library.md > "testing + net/http/httptest — provider mocks"). Second, any git fixture repository the test needs is built with `os/exec` in a test helper — never through a shell — mirroring the base-system pattern already used for git interaction in atcr (standard-library.md > "os/exec — git interaction"). Third, assertions use `testify`'s `assert`/`require` split deliberately: `require` for preconditions the rest of the test cannot proceed without (e.g., fixture setup succeeding, the reconciled directory existing), `assert` for the actual contract checks so multiple independent assertions can all report failures in a single run (testify.md > "Integration Notes (atcr)").

The substantive thing the test must assert is the reconcile id-or-path resolution contract documented in adversarial-verification-interface.md: a bare review id resolves to `.atcr/reviews/<id>/`, a path is used as-is, and an omitted argument resolves to `.atcr/latest`. This resolution rule is shared by `atcr reconcile` and `atcr verify` (adversarial-verification-interface.md explicitly says `atcr verify` "follows the same resolution rules as `atcr reconcile`"), so the contract test should exercise all three resolution branches against `--output-dir` to confirm none of them have drifted, since external private-skill consumers hardcode assumptions about this exact behavior.

## Key Concepts

### Table-driven tests with `t.Run` subtests and `t.TempDir()` fixtures

Table-driven tests with subtests (`t.Run`) are the established pattern for range decision trees, stream parsing, and reconciler merge rules; `t.TempDir()` provides per-test review-directory fixtures that are automatically cleaned up.

> Source: [standard-library.md: testing + net/http/httptest — provider mocks]

### `httptest.NewServer` for provider mocks

`httptest.NewServer(handler)` plays an OpenAI-compatible provider; the registry under test points `base_url` at `server.URL`, so the real client code path (auth, retry, decode) is exercised with zero network. Provider failure modes (429 then 200 retry path, persistent 500 fallback-agent path, handler sleep past deadline for timeout status) can each be simulated per test.

> Source: [standard-library.md: testing + net/http/httptest — provider mocks]

### `os/exec` conventions for git fixture repos

Git interaction uses `exec.CommandContext(ctx, "git", "-C", repo, ...)` everywhere, so a cancelled run never leaves orphaned git processes. Invocations must never go through a shell (`sh -c`) — argv is passed directly since ref names and paths are arguments, immune to shell injection. Fixture git repos for tests are built with `os/exec` in test helpers, per the base-system pattern.

> Source: [standard-library.md: os/exec — git interaction]

### testify `assert` vs `require` — when to use each

`assert` provides non-fatal assertions that return a `bool` and print friendly failure descriptions, letting a test continue and report multiple failures. `require` provides the same functions but calls `t.FailNow()` on failure, terminating the test immediately — reserved for critical preconditions where subsequent test logic depends on the assertion passing (e.g., file existence, successful initialization), and it must be called from the goroutine running the test function to avoid race conditions. In atcr, `assert` is used for most test assertions, while `require` is reserved for critical preconditions where the test logic cannot continue on failure.

> Source: [testify.md: require Package] and [testify.md: Integration Notes (atcr)]

### Reconcile id-or-path resolution contract

`atcr verify [id-or-path]` documents that `id-or-path` is optional and "follows the same resolution rules as `atcr reconcile`":
- bare review id → `.atcr/reviews/<id>/`
- path → used as-is
- omitted → `.atcr/latest`

This is the exact resolution behavior the new contract test's assertions must mirror when validating `--output-dir` + reconcile stability, since it is the contract external private-skill consumers rely on.

> Source: [adversarial-verification-interface.md: CLI Command: `atcr verify` > Form]

## Code Examples

The following code appears verbatim in the source documentation and illustrates the assertion and mocking primitives the new test should use — no new code has been invented beyond what these docs already show.

### assert usage (testify.md)

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

> Source: [testify.md: assert Package > Usage Example]

### require usage (testify.md)

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

> Source: [testify.md: require Package > Usage Example]

## Quick Reference

| Test concern | stdlib / testify API or pattern | Source |
|---|---|---|
| Fixture setup (temp review dirs) | `t.TempDir()` | standard-library.md: testing + net/http/httptest — provider mocks |
| Fixture setup (git repos) | `exec.CommandContext(ctx, "git", "-C", repo, ...)` in a test helper | standard-library.md: os/exec — git interaction |
| Test structure | Table-driven tests with `t.Run` subtests | standard-library.md: testing + net/http/httptest — provider mocks |
| Assertions (non-fatal, multiple per test) | `github.com/stretchr/testify/assert` | testify.md: assert Package; Integration Notes (atcr) |
| Assertions (critical preconditions) | `github.com/stretchr/testify/require` | testify.md: require Package; Integration Notes (atcr) |
| Provider mocking | `httptest.NewServer(handler)`, point `base_url` at `server.URL` | standard-library.md: testing + net/http/httptest — provider mocks |
| Git interaction in fixtures | `os/exec`, argv only, never `sh -c` | standard-library.md: os/exec — git interaction |
| id-or-path resolution to assert | bare id → `.atcr/reviews/<id>/`; path → as-is; omitted → `.atcr/latest` | adversarial-verification-interface.md: CLI Command: `atcr verify` > Form |

## Related Documentation

- [../../../../specifications/packages/standard-library.md](../../../../specifications/packages/standard-library.md) — stdlib conventions for `testing`, `net/http/httptest`, and `os/exec` used across atcr's test suite.
- [../../../../specifications/packages/testify.md](../../../../specifications/packages/testify.md) — testify `assert`/`require` API and atcr integration notes.
- [../../../../specifications/design-concepts/adversarial-verification-interface.md](../../../../specifications/design-concepts/adversarial-verification-interface.md) — documents the id-or-path resolution rules shared by `atcr reconcile` and `atcr verify` that the new contract test must validate.

Codebase pattern sources to follow (per codebase-discovery.json):
- `cmd/atcr/review_test.go` — example `_test.go` exercising CLI behavior including `--output-dir` cases.
- `cmd/atcr/reconcile_test.go` — co-located reconcile subcommand test to pair the new contract test alongside.
- `internal/fanout/reviewdir.go` — implements `ReviewsRoot` and the output-directory resolution logic underlying the `--output-dir` contract.
