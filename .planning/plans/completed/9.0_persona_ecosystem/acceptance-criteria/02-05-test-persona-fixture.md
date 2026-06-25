# Acceptance Criteria: Test Persona via Fixture Runner

**Related User Story:** [02: Personas CLI: Discovery and Lifecycle](../user-stories/02-personas-cli-discovery-and-lifecycle.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI subcommand | Go / Cobra | `test` sub-subcommand under `newPersonasCmd()` |
| Fixture runner delegation | existing fixture runner from Story 1 | Same runner used by `sentinel`/`tracer`/`idiomatic` bonus personas |
| Exit code mirroring | `os.Exit` / Cobra `RunE` return | Exit code mirrors fixture test outcome (0 = pass, non-zero = fail) |
| Output | `os.Stdout` / `os.Stderr` | Pass/fail result and any fixture diff printed to stdout |
| Test Framework | `go test` / `testify` | Fixture YAML with embedded test cases; temp directory for installed personas |
| Key Dependencies | `github.com/spf13/cobra`, `internal/personas/test.go`, fixture runner from Story 1 | |

## Related Files
- `internal/personas/test.go` - create: `TestPersona(personasDir, name string, runner FixtureRunner) error` — loads persona, delegates to fixture runner
- `cmd/atcr/personas.go` - modify: add `test` sub-subcommand wired to `personas.TestPersona()`
- `cmd/atcr/personas_test.go` - modify: add `TestPersonasTest_*` test cases using fixture YAML and temp directory
- `internal/verify/invoke_test.go` - reference: `httptest.NewServer` pattern for fetch-based fixture tests

### Related Files (from codebase-discovery.json)

- `internal/personas/test.go` — create: fixture runner delegation
- `cmd/atcr/personas.go` — modify: add `test` sub-subcommand
- `cmd/atcr/personas_test.go` — modify: add test cases for `atcr personas test`
- `personas/testdata/*_fixture.patch` — fixture files for built-in bonus personas
- `internal/verify/invoke_test.go` — reference: `httptest.NewServer` pattern

## Happy Path Scenarios

**Scenario 1: Persona fixture passes all test cases**
- **Given** `~/.config/atcr/personas/security/owasp.yaml` is installed and contains a valid `fixture` block with expected inputs/outputs
- **When** the user runs `atcr personas test security/owasp`
- **Then** the fixture runner executes all test cases, stdout prints `"PASS: security/owasp (3/3 cases)"`, and the command exits 0

**Scenario 2: Persona fixture partially fails**
- **Given** the installed persona's fixture has 3 test cases and 1 produces output that does not match the expected value
- **When** `atcr personas test security/owasp` is run
- **Then** stdout prints the failing case diff, a summary like `"FAIL: security/owasp (2/3 cases)"`, and the command exits non-zero (exit code 1)

**Scenario 3: Built-in bonus persona passes fixture**
- **Given** `sentinel` (Story 1 bonus persona) is loaded as a built-in
- **When** `atcr personas test sentinel` is run
- **Then** the same fixture runner used during Story 1 testing executes successfully; exit code mirrors the outcome

## Edge Cases

**Edge Case 1: Persona has no fixture block**
- **Given** the installed persona YAML has no `fixture` section
- **When** `atcr personas test security/owasp` is run
- **Then** stdout prints `"No fixture defined for persona \"security/owasp\""` and the command exits 0 (not an error — persona is valid but untested)

**Edge Case 2: Fixture runner invokes the LLM (or a mock)**
- **Given** the fixture runner is configured to use a stub/mock LLM in tests
- **When** `atcr personas test` runs in CI
- **Then** no live LLM API calls are made; the stub returns deterministic output for comparison against expected fixture values

## Error Conditions

**Error Scenario 1: Persona not installed or not found**
- Error message: `"persona \"security/owasp\" is not installed"`
- Exit code: 1

**Error Scenario 2: Fixture runner returns an internal error (e.g., YAML parse failure)**
- Error message: `"fixture runner error for \"security/owasp\": <underlying error>"`
- Exit code: 1

## Performance Requirements
- **Response Time:** Fixture test completes within 5 seconds per test case when using stub LLM responses; live LLM latency is acceptable
- **Throughput:** Sequential test case execution; no parallelism required within a single `atcr personas test` invocation

## Security Considerations
- **Authentication/Authorization:** Delegates to the same fixture runner as Story 1; no additional auth surface introduced
- **Input Validation:** Persona name validated with the same path-traversal check used by `install` and `remove` before resolving the file path

## Test Implementation Guidance
**Test Type:** UNIT (for `internal/personas/test.go`) + INTEGRATION (for `cmd/atcr/personas_test.go`)
**Test Data Requirements:** Fixture YAML with at least 2 test cases (one passing, one failing); persona YAML without a fixture block; persona name that is not installed
**Mock/Stub Requirements:** Stub `FixtureRunner` interface injected in unit tests returning deterministic pass/fail; temp directory for `PersonasDir()` override

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/... ./internal/personas/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `atcr personas test <name>` exit code mirrors fixture outcome (0 = all pass, 1 = any fail)
- [ ] Personas with no fixture block exit 0 with an informational message
- [ ] Fixture runner is injectable — no live LLM calls in CI test suite
- [ ] Same fixture runner used by Story 1 bonus personas is reused without duplication

**Manual Review:**
- [ ] Code reviewed and approved
