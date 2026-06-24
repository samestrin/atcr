# Acceptance Criteria: Bundle Test Coverage in bundles_test.go

**Related User Story:** [04: Domain Bundle Installation](../user-stories/04-domain-bundles.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Test file | `internal/personas/bundles_test.go` | Package `personas_test` (black-box) |
| Test framework | `go test` / `testify` (`require`, `assert`) | Table-driven tests |
| HTTP mock | `net/http/httptest` | For integration tests covering the install path via bundle |
| Temp dir | `t.TempDir()` | Isolated config dir per test; auto-cleaned |
| Coverage tool | `go test -cover` | Must reach function-level coverage on all exported resolver symbols |

## Related Files
- `internal/personas/bundles_test.go` - create: full test suite covering all five required scenarios
- `internal/personas/bundles.go` - create: production code under test (resolver, manifest parser, error sentinel)

## Happy Path Scenarios

**Scenario 1: Successful expansion of bundle/django**
- **Given** the `bundles_test.go` test suite runs against the embedded manifests
- **When** `bundles.Resolve("django")` is called in a test
- **Then** the test asserts the returned list equals `[]string{"framework/django-orm", "language/python-types", "security/owasp", "security/secrets"}` exactly (order-insensitive if documented, or order-strict if the manifest defines insertion order)

**Scenario 2: Successful expansion of bundle/go-production**
- **Given** the same test suite
- **When** `bundles.Resolve("go-production")` is called
- **Then** the test asserts the returned list matches the declared go-production manifest exactly

## Edge Cases

**Edge Case 1: Unknown bundle returns typed error**
- **Given** `bundles.Resolve("nonexistent")` is called in a test
- **When** the resolver runs
- **Then** the test asserts `errors.Is(err, bundles.ErrUnknownBundle)` is true and the returned slice is nil

**Edge Case 2: Partial-install skip — some members pre-populated**
- **Given** a temp config dir with `framework/django-orm` and `language/python-types` already present as files
- **When** the install loop (called via the bundle install path) runs for `bundle/django`
- **Then** the test asserts that only `security/owasp` and `security/secrets` are fetched (via `httptest.NewServer` call count); the pre-populated entries are skipped and reported as "already present"

**Edge Case 3: Manifest parse validation — missing required field**
- **Given** an in-memory YAML byte slice with no `name` field
- **When** the internal `parseManifest` function is called (via exported test hook or white-box test in `package personas`)
- **Then** the test asserts a non-nil error containing `"missing required field: name"` and no panic occurs

## Error Conditions

**Error Scenario 1: All five required test scenarios pass under `go test ./internal/personas/...`**
- **Given** the full test suite in `bundles_test.go`
- **When** `go test ./internal/personas/...` is run
- **Then** all tests pass (exit 0), no test panics, and no race conditions under `-race` flag
- Error message: N/A (success case)
- Error code: exit 0

**Error Scenario 2: Test failure reveals resolver regression**
- **Given** a hypothetical change that breaks the django bundle expansion
- **When** `go test ./internal/personas/...` is run
- **Then** the table-driven test for `bundle/django` fails with a clear diff showing expected vs. actual persona list; `go test` exits non-zero

## Performance Requirements
- **Response Time:** The full `bundles_test.go` test suite must complete in < 2 seconds (unit tests are CPU-only; integration tests use `httptest.NewServer` with no real network I/O)
- **Throughput:** Tests run sequentially by default; no `t.Parallel()` required but must not deadlock if added

## Security Considerations
- **Authentication/Authorization:** Tests use `httptest.NewServer` pointed at the configurable `RegistryBaseURL` field — no live external calls; the configurable URL field must be exercised in at least one test to confirm it is not hardcoded
- **Input Validation:** Tests must include at least one adversarial input (e.g., path traversal name, empty string) to confirm the resolver rejects it via `ErrUnknownBundle` without panicking

## Test Implementation Guidance
**Test Type:** UNIT (resolver expansion, error sentinel, manifest parse) + INTEGRATION (partial-install skip with temp dir + `httptest.NewServer`)
**Test Data Requirements:**
- Embedded `django.yaml` and `go-production.yaml` manifests (available via `go:embed` in production code)
- A temp dir created with `t.TempDir()` for integration tests
- In-memory YAML byte slices for parse-validation tests
- `httptest.NewServer` serving stub persona YAML for the two non-pre-populated personas
**Mock/Stub Requirements:**
- `httptest.NewServer` replaces the community repo endpoint; the test sets `bundles.RegistryBaseURL` (or equivalent) to the test server URL before calling the install path

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test -race ./internal/personas/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `bundles_test.go` covers successful expansion of both `bundle/django` and `bundle/go-production`
- [ ] `bundles_test.go` covers unknown bundle error (`errors.Is(err, ErrUnknownBundle)`)
- [ ] `bundles_test.go` covers partial-install skip behavior with a pre-populated temp config dir
- [ ] `bundles_test.go` covers manifest parse validation (missing required field returns descriptive error, not panic)

**Manual Review:**
- [ ] Code reviewed and approved
