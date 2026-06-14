# Acceptance Criteria: Test Coverage & CI Integration

**Related User Story:** [[02]: Skeptic Invocation & Verdict Parsing](../user-stories/02-skeptic-invocation-verdict-parsing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Test Framework | `go test` + `testify` | Table-driven tests matching existing project patterns |
| Coverage Tool | `go test -coverprofile` | >= 95% on `skeptic.go` and `verdict.go` |
| Test Fixtures | `internal/verify/testdata/` | JSON fixtures for findings and responses |
| Key Dependencies | `internal/fanout` (fake ChatCompleter/Dispatcher), `internal/reconcile` (Verification) | Test doubles for isolation |

## Related Files
- `internal/verify/verdict_test.go` - create: table-driven tests for `parseVerdict` (>= 11 cases)
- `internal/verify/verify_test.go` - create: table-driven tests for `invokeSkeptic` and `buildSkepticPrompt`
- `internal/verify/testdata/true-finding.json` - create: fixture for a confirmed-verdict scenario
- `internal/verify/testdata/false-finding.json` - create: fixture for a refuted-verdict scenario
- `internal/verify/testdata/malformed-response.txt` - create: fixture for malformed parser input
- `internal/verify/testdata/mock-skeptic.go` - create: shared fake `ChatCompleter` for skeptic tests

## Happy Path Scenarios
**Scenario 1: All verdict_test.go cases pass**
- **Given** the test file `internal/verify/verdict_test.go` with table-driven subtests for: confirmed, refuted, unverifiable, malformed JSON, invalid verdict enum, empty response, extra fields, markdown-fenced JSON, prose-embedded JSON, empty reasoning, missing reasoning
- **When** `go test ./internal/verify/...` is run
- **Then** all subtests pass

**Scenario 2: All verify_test.go cases pass**
- **Given** the test file `internal/verify/verify_test.go` with subtests for: single skeptic confirms, single skeptic refutes, budget tripped → unverifiable, provider error → unverifiable, malformed output → unverifiable, prompt determinism, budget forwarding
- **When** `go test ./internal/verify/...` is run
- **Then** all subtests pass

**Scenario 3: Coverage meets threshold**
- **Given** all tests in `internal/verify/...`
- **When** `go test -coverprofile=cover.out ./internal/verify/...` is run
- **Then** coverage on `skeptic.go` >= 95% and coverage on `verdict.go` >= 95%

## Edge Cases
**Edge Case 1: Test fixture loading from testdata/**
- **Given** test fixtures in `internal/verify/testdata/`
- **When** tests use `os.ReadFile` or `testing/fstest` to load them
- **Then** fixtures are loaded relative to the test file (using `testdata/` convention)

**Edge Case 2: Concurrent test safety**
- **Given** multiple test subtests running in parallel (via `t.Parallel()`)
- **When** `go test ./internal/verify/...` is run
- **Then** no data races detected (`go test -race ./internal/verify/...` passes)

## Error Conditions
**Error Scenario 1: Coverage below threshold**
- **Given** a code change that drops coverage on `verdict.go` below 95%
- **When** `go test -coverprofile=cover.out ./internal/verify/...` is run
- **Then** the coverage report shows the drop (CI gate enforcement is a future story concern; this story ensures the coverage is achieved)

**Error Scenario 2: Build failure from import cycle**
- **Given** `internal/verify` imports `internal/fanout`
- **When** `go build ./...` is run
- **Then** no import cycle error (`fanout` must NOT import `verify`)

## Performance Requirements
- **Test Execution Time:** `go test ./internal/verify/...` completes in < 10 seconds (no real LLM calls — all fakes)
- **Test Isolation:** Each test is independent; no shared mutable state between subtests

## Security Considerations
- **Test Data:** Fixtures contain synthetic findings and responses — no real API keys or production data
- **No Network Calls:** All tests use fake `ChatCompleter` and `toolDispatcher` — no outbound HTTP

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- `testdata/true-finding.json` — a `JSONFinding` with severity="high", a clear problem, and evidence
- `testdata/false-finding.json` — a `JSONFinding` for a non-issue (to test refuted verdicts)
- `testdata/malformed-response.txt` — various malformed LLM responses (not valid JSON, valid JSON with bad enum, empty, etc.)
- `mock-skeptic.go` — shared `fakeChatCompleter` struct with configurable responses and error injection

**Mock/Stub Requirements:**
- `fakeChatCompleter` — implements `ChatCompleter`, returns preconfigured `ChatResponse` values or errors per call
- `fakeDispatcher` — implements `toolDispatcher`, returns preconfigured `ToolResult` values or errors
- Both must be safe for concurrent use if `t.Parallel()` is used

**Test Matrix:**

| File | Function | Cases | Type |
|------|----------|-------|------|
| `verdict_test.go` | `parseVerdict` | 11+ | Table-driven |
| `verify_test.go` | `buildSkepticPrompt` | 4+ | Table-driven |
| `verify_test.go` | `invokeSkeptic` | 7+ | Table-driven |

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/verify/...`)
- [ ] No linting errors (`go vet ./internal/verify/...`)
- [ ] Build succeeds (`go build ./...`)
- [ ] Race detector clean (`go test -race ./internal/verify/...`)
- [ ] Coverage >= 95% on `skeptic.go` and `verdict.go`

**Story-Specific:**
- [ ] `verdict_test.go` covers all 7 original cases plus edge cases (>= 11 subtests)
- [ ] `verify_test.go` covers: confirms, refutes, budget trip, provider error, malformed output, prompt determinism, budget forwarding (>= 7 subtests)
- [ ] Test fixtures in `testdata/` are loadable and used by at least one test
- [ ] No real LLM calls in any test (all fakes)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Test names are descriptive and follow `TestFunctionName_scenario` convention
