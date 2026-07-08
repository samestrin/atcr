# Testing Patterns: testify + httptest Mock Registry [IMPORTANT]

## Overview

This plan moves the community-persona channel to a fetched, in-repo source (`samestrin/atcr`, not compiled into the binary), so its tests cannot depend on live network access until that repo is public. The base repo's established pattern — `httptest.NewServer(handler)` standing in for a real HTTP endpoint while the real client code path (auth, retry, decode) executes unmodified — carries over directly: the registry under test points its URL at `server.URL` instead of the real `atcr-persona-registry` endpoint, with zero network calls in CI.

`internal/personas/client.go` already implements this pattern for atcr: a minimal `HTTPClient` interface (`Do(req) (*http.Response, error)`) satisfied by `*http.Client`, with a swappable package-level client (`personasClient` in `cmd/atcr/personas.go`) that tests redirect via `ATCR_PERSONAS_URL` pointed at an `httptest.NewServer`. Every AC1/AC2/AC6 test for this plan (model/provider search, backward-compatible old-shape `index.json` parsing) must follow this exact existing pattern rather than inventing a new one.

The new `internal/personas/search_test.go` file — covering Provider/Model/Tasks/Tags filtering, case-insensitive model/provider matching, and backward compatibility with old-shape `index.json` — extends the table-driven style already used in `internal/personas/personas_test.go`. Per the base repo's test pattern, table-driven tests with `t.Run` subtests are the standard structure for this kind of decision-tree/filtering logic, and testify's `assert`/`require` packages are the standard assertion layer for atcr unit tests, giving these new subtests clear, expressive failure messages without new test infrastructure.

## Key Concepts

- **Injectable HTTP client for network code**: a minimal `HTTPClient` interface satisfied by `*http.Client`, with the concrete client held in a swappable package-level variable so tests can substitute it.
  > Source: [standard-library.md] (carried over from codebase pattern in internal/personas/client.go, cmd/atcr/personas.go)

- **`httptest.NewServer(handler)` as a mock provider/registry**: the code under test points its base URL (here, `ATCR_PERSONAS_URL`) at `server.URL`, exercising the real auth/retry/decode code path with zero real network calls.
  > Source: [standard-library.md]

- **Table-driven tests with `t.Run` subtests**: the established structure for range decision trees, stream parsing, and reconciler merge rules — the same structure `search_test.go` should use for Provider/Model/Tasks/Tags filter-matrix coverage.
  > Source: [standard-library.md]

- **`assert` package for non-fatal, expressive assertions**: `assert.Equal`, `assert.NotEqual`, `assert.Nil`, `assert.NotNil`, `assert.True`, `assert.NoError` — used for most test assertions in atcr unit tests, including new search filtering tests.
  > Source: [testify.md]

- **`require` package for fatal preconditions**: same functions as `assert`, but calls `t.FailNow()` immediately on failure; used where subsequent test logic cannot continue (e.g., a fixture file or fetched index must exist before filtering logic can be exercised). Must be called from the goroutine running the test function to avoid race conditions.
  > Source: [testify.md]

- **`mock` package for interaction-based mocks**: `On(methodName, args...)`, `Return(values...)`, `AssertExpectations(t)`, `Called(args...)` — used for LLM provider mocks in fan-out engine tests per the base system's pattern; available if persona search/fetch logic needs an interaction-verified mock beyond a plain `httptest.NewServer`.
  > Source: [testify.md]

- **`suite` package (optional)**: struct-based test organization with `SetupTest`/`TearDownTest` lifecycle hooks; does not support parallel tests. atcr tests can use standard `testing.T` with assert/require instead — the suite package is not required for this plan's tests.
  > Source: [testify.md]

## Code Examples

Non-fatal assertions (assert package):

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

Fatal preconditions (require package):

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

Interaction-based mock object (mock package):

```go
package yours

import (
    "testing"
    "github.com/stretchr/testify/mock"
)

// 1. Define the mocked object
type MyMockedObject struct {
    mock.Mock
}

// 2. Implement the method being mocked
func (m *MyMockedObject) DoSomething(number int) (bool, error) {
    args := m.Called(number)
    return args.Bool(0), args.Error(1)
}

// 3. Write the test
func TestSomething(t *testing.T) {
    testObj := new(MyMockedObject)

    // Set expectations
    testObj.On("DoSomething", 123).Return(true, nil)

    // Use placeholder for dynamic/unpredictable arguments
    // testObj.On("DoSomething", mock.Anything).Return(true, nil)

    // Call the code under test
    targetFuncThatDoesSomethingWithObj(testObj)

    // Verify expectations were met
    testObj.AssertExpectations(t)
}
```

## Quick Reference

| Tool / Package | Purpose | Use in this plan |
|---|---|---|
| `httptest.NewServer(handler)` | Stand in for a real HTTP endpoint | Mock the persona registry index/fetch endpoint via `ATCR_PERSONAS_URL` |
| `internal/personas/client.go` `HTTPClient` interface | Injectable HTTP client | Already wired for tests to swap in a mock/test server |
| `cmd/atcr/personas.go` `personasClient` | Swappable package-level client var | Point at `httptest` server URL in tests |
| `testify/assert` | Non-fatal assertions | Most assertions in `search_test.go` and existing `personas_test.go` |
| `testify/require` | Fatal preconditions | Precondition checks before filter-matrix subtests run |
| `testify/mock` | Interaction-based mocking | Optional; used for LLM provider mocks in fan-out engine tests |
| `testify/suite` | Struct-based test suites (optional) | Not required — standard `testing.T` + assert/require suffices |
| Table-driven tests + `t.Run` | Structured coverage of a filter/decision matrix | Provider/Model/Tasks/Tags filtering, case-insensitivity, old-shape `index.json` backward compatibility |

## Related Documentation

- [../../../../specifications/packages/standard-library.md](../../../../specifications/packages/standard-library.md)
- [../../../../specifications/packages/testify.md](../../../../specifications/packages/testify.md)
