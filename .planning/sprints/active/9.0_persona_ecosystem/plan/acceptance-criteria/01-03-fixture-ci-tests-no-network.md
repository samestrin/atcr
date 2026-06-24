# Acceptance Criteria: Fixture-Based CI Tests (No Network)

**Related User Story:** [01: Bonus Built-In Domain Personas](../user-stories/01-bonus-built-in-domain-personas.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Test fixtures | `.patch` / `.diff` files | Committed to `personas/testdata/`; loaded via `os.ReadFile` |
| Test framework | `go test` / `testing` stdlib | No external test runners; no live network calls |
| Fixture loading | `os.ReadFile` | Deterministic; no generation at test time |

## Related Files
- `personas/testdata/sentinel_fixture.patch` - create: minimal diff containing SQL string concatenation or hardcoded API key — unambiguous trigger for `sentinel`
- `personas/testdata/tracer_fixture.patch` - create: minimal diff containing an ORM/DB call inside a loop — unambiguous trigger for `tracer`
- `personas/testdata/idiomatic_fixture.patch` - create: minimal diff containing an ignored `error` return — unambiguous trigger for `idiomatic`
- `personas/personas_test.go` - modify: add three fixture-based test functions, one per bonus persona

### Related Files (from codebase-discovery.json)

- `personas/testdata/sentinel_fixture.patch` — create: SQL concatenation / hardcoded secret fixture
- `personas/testdata/tracer_fixture.patch` — create: ORM-in-loop fixture
- `personas/testdata/idiomatic_fixture.patch` — create: ignored error return fixture
- `personas/personas_test.go` — modify: add fixture-based test functions

## Happy Path Scenarios

**Scenario 1: sentinel fixture triggers a security finding**
- **Given** `personas/testdata/sentinel_fixture.patch` contains a synthetic SQL string concatenation (e.g., `query := "SELECT * FROM users WHERE id = " + userInput`)
- **When** the sentinel persona template is rendered with the fixture as `{{.Diff}}` and the output is evaluated
- **Then** the rendered output contains at least one finding whose category string matches `security`, `injection`, or `OWASP`; no live network calls are made

**Scenario 2: tracer fixture triggers a performance finding**
- **Given** `personas/testdata/tracer_fixture.patch` contains a synthetic DB call inside a `for` loop (e.g., `for _, id := range ids { db.Find(&user, id) }`)
- **When** the tracer persona template is rendered with the fixture as `{{.Diff}}`
- **Then** the rendered output contains at least one finding whose category string matches `performance`, `N+1`, or `allocation`; no live network calls

**Scenario 3: idiomatic fixture triggers a Go idiom finding**
- **Given** `personas/testdata/idiomatic_fixture.patch` contains a synthetic ignored error return (e.g., `val, _ := strconv.Atoi(s)`)
- **When** the idiomatic persona template is rendered with the fixture as `{{.Diff}}`
- **Then** the rendered output contains at least one finding whose category string matches `idiomatic`, `error handling`, or `goroutine`; no live network calls

**Scenario 4: All fixture tests pass in `go test ./personas/...` with zero network calls**
- **Given** CI runs `go test ./personas/... -count=1` in an environment with no outbound network access
- **When** the test suite completes
- **Then** all fixture tests pass with exit code 0; no DNS resolution or TCP connections are attempted

## Edge Cases

**Edge Case 1: Fixture file not committed to repo**
- **Given** a fixture `.patch` file is missing from `personas/testdata/`
- **When** the corresponding test calls `os.ReadFile("testdata/sentinel_fixture.patch")`
- **Then** the test fails immediately with `os.ReadFile: no such file or directory` — caught in pre-merge CI

**Edge Case 2: Fixture triggers findings but in an unexpected category**
- **Given** a fixture is authored with an ambiguous pattern that the persona routes to a different category
- **When** the test asserts on the category string
- **Then** the assertion fails, signaling the fixture needs to be made more unambiguous; the test does not silently pass

**Edge Case 3: Rendered output is non-empty but contains zero structured findings**
- **Given** the persona template renders successfully but the LLM-style prompt produces only a prose summary with no finding blocks
- **When** the test checks for category-string presence
- **Then** the test fails, ensuring the fixture is canonical enough to produce at minimum one structured finding block

**Edge Case 4: Fixture file is a valid `.diff` but contains only whitespace changes**
- **Given** a fixture accidentally contains only blank-line diffs
- **When** the persona renders it
- **Then** the rendered output likely contains no findings; the test fails, prompting the author to add a meaningful code pattern

## Error Conditions

**Error Scenario 1: `os.ReadFile` permission denied on fixture file**
- This indicates a file-permission issue in the repo; the test fails with:
  `open testdata/sentinel_fixture.patch: permission denied`
- Resolution: ensure fixture files are committed with mode `0644`

**Error Scenario 2: Fixture-based test inadvertently makes a network call**
- If the persona rendering path uses an HTTP client, `go test -run TestSentinelFixture` will hang or fail in restricted CI
- Prevention: ensure `personas.Render()` is a pure template execution with no HTTP clients; mock any I/O dependencies in tests

## Performance Requirements
- **Response Time:** Each fixture-based test must complete in < 500 ms (template rendering only; no LLM inference in unit tests)
- **Throughput:** The full `go test ./personas/...` suite must complete in < 10 s in CI

## Security Considerations
- **Fixture content:** Synthetic sensitive values in fixture files (API keys, SQL patterns) must be clearly fake (e.g., `FAKE_API_KEY_00000000`, no real credentials); commit scanning must not flag them
- **No network access:** Tests must not make outbound connections; if the persona rendering path ever acquires an HTTP dependency, a `httptest` mock or `t.Setenv("HTTP_PROXY", "")` guard must be added
- **Authentication/Authorization:** Test files in `personas/testdata/` are read-only committed assets; no runtime writes to `testdata/` from tests

## Test Implementation Guidance
**Test Type:** INTEGRATION (fixture-driven, in-process — no network, no external services)

**Test Data Requirements:**
- `personas/testdata/sentinel_fixture.patch` — SQL concatenation or hardcoded key pattern
- `personas/testdata/tracer_fixture.patch` — ORM-in-loop pattern
- `personas/testdata/idiomatic_fixture.patch` — ignored `error` return pattern

**Mock/Stub Requirements:** None — rendering is in-process template execution

**Suggested test structure:**
```go
func testFixture(t *testing.T, personaName, fixturePath, expectCategory string) {
    t.Helper()
    patch, err := os.ReadFile(fixturePath)
    if err != nil {
        t.Fatalf("read fixture: %v", err)
    }
    payload := personas.Payload{Diff: string(patch), ScopeFocus: personaName}
    output, err := personas.Render(personaName, payload)
    if err != nil {
        t.Fatalf("Render(%q): %v", personaName, err)
    }
    if !strings.Contains(strings.ToLower(output), expectCategory) {
        t.Fatalf("Render(%q) output does not contain category %q:\n%s", personaName, expectCategory, output)
    }
}

func TestSentinelFixture(t *testing.T) {
    testFixture(t, "sentinel", "testdata/sentinel_fixture.patch", "injection")
}
func TestTracerFixture(t *testing.T) {
    testFixture(t, "tracer", "testdata/tracer_fixture.patch", "n+1")
}
func TestIdiomaticFixture(t *testing.T) {
    testFixture(t, "idiomatic", "testdata/idiomatic_fixture.patch", "error")
}
```

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./personas/...`)
- [x] No linting errors (`golangci-lint run ./personas/...`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] Three fixture files exist in `personas/testdata/` and are committed to the repo with mode `0644`
- [x] `TestSentinelFixture`, `TestTracerFixture`, and `TestIdiomaticFixture` each assert on a category-specific string in the rendered output
- [x] The full `go test ./personas/...` suite passes in a network-restricted environment (verified in CI without outbound access)
- [x] No test in `personas/` makes live HTTP or DNS calls (verified by CI network policy or test audit)

**Manual Review:**
- [ ] Code reviewed and approved
