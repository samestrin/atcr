# Testing Framework & Patterns [IMPORTANT]

## Overview

The atcr project uses Go's standard `testing` package combined with `github.com/stretchr/testify` for assertions, mocking, and test suites. All test files are co-located with source code in the same package directory using the `*_test.go` naming convention. This approach ensures tests have access to both exported and unexported identifiers while maintaining clear separation between production code and test code.

Test organization follows table-driven patterns with `t.Run` subtests for scenario coverage across all subsystems. Provider interaction is mocked using `httptest.NewServer` with atomic counters for concurrency verification, ensuring fan-out parallelism behaves correctly under real-world conditions. The project targets **70% minimum code coverage** across all packages, measured during CI gates.

Fixture strategy relies on `t.TempDir()` for review-directory isolation and fixture git repositories built with `os/exec` in test helpers, enabling deterministic testing of git range resolution, payload building, and reconciliation logic without external dependencies.

## Key Concepts

### Test Framework Stack

atcr combines stdlib testing with testify for richer assertions:

- **Framework**: Go standard `testing` library
- **Assertions**: `github.com/stretchr/testify/assert` for non-fatal checks, `/require` for fatal assertions that halt test execution
- **Mocking**: `httptest.NewServer` for OpenAI-compatible provider mocks; atomic.Int32 high-water mark for concurrency verification
- **Structure**: Table-driven tests with `t.Run` subtests

> Source: [codebase-discovery.json:test_patterns.framework]

> Source: [.planning/specifications/coding-standards.md:Testing Standards]

### Test Location and Naming

Tests live alongside source code in the same package:

- **File naming**: `*_test.go` co-located with the code under test
- **Function naming**: `TestXxx` for unit tests, `TestMain` for package-level setup
- **Package**: Same package as source (e.g., `package gitrange` in `internal/gitrange/resolver_test.go`)
- **Integration tests**: Separated via `//go:build integration` build tag at top of file

> Source: [codebase-discovery.json:test_patterns.test_location]

> Source: [codebase-discovery.json:test_patterns.naming_convention]

### Table-Driven Test Pattern

Table-driven tests with `t.Run` enable comprehensive scenario coverage:

```go
func TestResolver(t *testing.T) {
    tests := []struct {
        name     string
        base     string
        head     string
        wantBase string
        wantErr  bool
    }{
        {"explicit base/head", "main", "feature", "abc123", false},
        {"empty range", "main", "main", "", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test logic
        })
    }
}
```

> Source: [.planning/specifications/coding-standards.md:Test Structure]

### Provider Mocking with httptest

Provider interaction is tested using `httptest.NewServer` with atomic counters for concurrency verification:

- Create mock server with `httptest.NewServer(handler)`
- Point registry `base_url` at `server.URL` to exercise real client code path
- Use `atomic.Int32` to track high-water mark of concurrent requests
- Simulate failure modes: 429 then 200 (retry), persistent 500 (fallback), handler sleep past deadline (timeout)

> Source: [codebase-discovery.json:test_patterns.mocking]

> Source: [.planning/specifications/packages/standard-library.md:testing + net/http/httptest — provider mocks]

### Fixture Strategy

Two types of fixtures are used:

- **Review directories**: `t.TempDir()` creates isolated temporary directories for review output (`manifest.json`, `payload/`, `sources/`, `reconciled/`)
- **Git repositories**: Built with `os/exec` in test helpers to create deterministic commit histories for range resolution testing

> Source: [codebase-discovery.json:test_patterns.fixture_strategy]

### Coverage Target

Minimum 70% code coverage across all packages:

- Enabled in `.planning/.config/config.yaml` with combined coverage tracking
- Measured during CI gates before merge
- Subsystems tracked individually for targeted improvement

> Source: [codebase-discovery.json:test_patterns.coverage_target]

## Code Examples

### Concurrency Verification with Atomic Counter

From the fanout engine test pattern:

```go
var maxConcurrent atomic.Int32
var currentConcurrent atomic.Int32

handler := func(w http.ResponseWriter, r *http.Request) {
    current := currentConcurrent.Add(1)
    defer currentConcurrent.Add(-1)
    
    // Record high-water mark
    for {
        old := maxConcurrent.Load()
        if current <= old || maxConcurrent.CompareAndSwap(old, current) {
            break
        }
    }
    
    time.Sleep(50 * time.Millisecond) // Make parallelism measurable
    w.WriteHeader(http.StatusOK)
}

server := httptest.NewServer(http.HandlerFunc(handler))
defer server.Close()
```

> Source: [codebase-discovery.json:test_patterns.mocking]

### httptest Provider Mock with Retry Simulation

Simulating 429-then-200 retry behavior:

```go
callCount := atomic.Int32{}
handler := func(w http.ResponseWriter, r *http.Request) {
    count := callCount.Add(1)
    if count == 1 {
        w.WriteHeader(http.StatusTooManyRequests)
        return
    }
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]interface{}{
        "choices": []map[string]interface{}{
            {"message": map[string]string{"content": "findings"}},
        },
    })
}
server := httptest.NewServer(http.HandlerFunc(handler))
```

> Source: [.planning/specifications/packages/standard-library.md:testing + net/http/httptest — provider mocks]

### Git Repository Fixture Creation

Building fixture git repos with os/exec:

```go
func createFixtureRepo(t *testing.T, dir string) {
    t.Helper()
    runGit := func(args ...string) {
        cmd := exec.Command("git", args...)
        cmd.Dir = dir
        if err := cmd.Run(); err != nil {
            t.Fatalf("git %v failed: %v", args, err)
        }
    }
    runGit("init")
    runGit("config", "user.email", "test@example.com")
    runGit("config", "user.name", "Test User")
    // Create commits, branches, etc.
}
```

> Source: [codebase-discovery.json:test_patterns.fixture_strategy]

### Review Directory Fixture with t.TempDir

```go
func TestEngine(t *testing.T) {
    reviewDir := t.TempDir()
    
    // Use reviewDir for manifest.json, sources/, reconciled/ output
    engine := NewEngine(reviewDir)
    
    // Verify output structure
    manifestPath := filepath.Join(reviewDir, "manifest.json")
    assert.FileExists(t, manifestPath)
}
```

> Source: [codebase-discovery.json:test_patterns.fixture_strategy]

## Quick Reference

| Aspect | Tool/Pattern | Purpose |
|--------|-------------|---------|
| Test framework | `testing` + `testify` | Assertions, fatal/non-fatal checks |
| Test location | `*_test.go` alongside source | Same package access to unexported identifiers |
| Test naming | `TestXxx`, `t.Run` | Table-driven scenario coverage |
| Provider mocking | `httptest.NewServer` | Zero-network provider simulation |
| Concurrency check | `atomic.Int32` high-water mark | Verify parallel lane behavior |
| Fixtures (dirs) | `t.TempDir()` | Isolated review directory per test |
| Fixtures (git) | `os/exec` in helpers | Deterministic git history creation |
| Integration tests | `//go:build integration` | Separate slow/external-dependency tests |
| Coverage target | 70% | Minimum across all packages |
| Error handling | `require.NoError` for fatal, `assert.Equal` for checks | Clear failure semantics |

## Related Documentation

- [Coding Standards](../../../../specifications/coding-standards.md) - Go coding conventions including test structure requirements
- [Standard Library Usage](../../../../specifications/packages/standard-library.md) - stdlib package patterns for testing and provider mocking
- [Codebase Discovery](../codebase-discovery.json) - Comprehensive test subsystem breakdown and coverage mapping
- [Implementation Standards](../../../../specifications/implementation-standards.md) - Architecture principles driving test design
