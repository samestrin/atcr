# Acceptance Criteria: Bonus Persona Prompt Content

**Related User Story:** [01: Bonus Built-In Domain Personas](../user-stories/01-bonus-built-in-domain-personas.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Persona definition files | Markdown (`text/template` syntax) | Embedded via `go:embed *.md` in `personas/personas.go` |
| Template variables | Go `text/template` | Must include `{{.Diff}}`, `{{.ScopeFocus}}`, and other slots used by existing personas |
| Reference template | `personas/bruce.md` | Structural blueprint; each bonus persona must match its section layout |

## Related Files
- `personas/sentinel.md` - create: security-focused persona covering OWASP Top 10, injection, secrets leakage
- `personas/tracer.md` - create: performance-focused persona covering N+1 queries, memory leaks, allocation hot paths
- `personas/idiomatic.md` - create: Go idiom persona covering error handling, goroutine leaks, stdlib misuse

### Related Files (from codebase-discovery.json)

- `personas/bruce.md` — structural blueprint for new persona markdown
- `personas/sentinel.md` — create: security-focused persona
- `personas/tracer.md` — create: performance-focused persona
- `personas/idiomatic.md` — create: Go idiom persona
- `personas/personas.go:16` — `names` slice registration for embedded personas

## Happy Path Scenarios

**Scenario 1: sentinel.md covers OWASP / injection / secrets domains**
- **Given** `personas/sentinel.md` is present in the `personas/` directory
- **When** its raw content is read and parsed
- **Then** the system prompt section references at least two of: OWASP Top 10, SQL injection, command injection, hardcoded secrets / API key leakage; the severity rubric maps findings to Critical/High/Medium/Low; the output format block specifies the expected finding structure

**Scenario 2: tracer.md covers N+1 / memory / allocation domains**
- **Given** `personas/tracer.md` is present
- **When** its raw content is read and parsed
- **Then** the system prompt references at least two of: N+1 query pattern, memory leaks, allocation hot paths, escape analysis; the severity rubric and output format sections are present and follow the bruce.md structure

**Scenario 3: idiomatic.md covers Go error handling / goroutine leaks / stdlib misuse**
- **Given** `personas/idiomatic.md` is present
- **When** its raw content is read and parsed
- **Then** the system prompt references at least two of: ignored error returns, goroutine leaks, improper use of `sync` primitives, stdlib function misuse; the rubric and output format sections are present

**Scenario 4: All bonus personas render without template errors**
- **Given** a valid payload with all required template variables (`Diff`, `ScopeFocus`, etc.)
- **When** each bonus persona template is executed via `text/template`
- **Then** rendering completes without error and produces a non-empty string

## Edge Cases

**Edge Case 1: Template variable slot missing from persona file**
- **Given** a persona `.md` file omits a required template variable slot (e.g., `{{.Diff}}`)
- **When** the template is rendered with a standard payload
- **Then** `text/template` returns a parse/execution error (test catches this in `TestGet_BonusPersonasNonEmpty`)

**Edge Case 2: Persona file contains only whitespace / comments**
- **Given** a bonus `.md` file is accidentally left empty or whitespace-only
- **When** `personas.Get("sentinel")` is called
- **Then** the returned prompt string is empty, causing `TestGet_BonusPersonasNonEmpty` to fail

**Edge Case 3: Persona file uses unsupported template function**
- **Given** a persona author adds a template function call not registered in the template executor
- **When** the template is parsed at startup
- **Then** `go:embed` load succeeds but template execution fails with a `template: undefined function` error — caught by render tests

## Error Conditions

**Error Scenario 1: Persona file not found by `go:embed`**
- If a `.md` file listed in `names` is not committed to `personas/`, the binary fails to compile
- Error message: `personas/personas.go: pattern *.md: no matching files found` (build failure)

**Error Scenario 2: Malformed `text/template` syntax in persona file**
- Error message: `template: sentinel:N: unexpected "..." in operand`
- Caught at: template parse time (first call to `personas.Get("sentinel")`) or a startup parse if templates are pre-compiled

## Performance Requirements
- **Response Time:** Template rendering for a bonus persona must complete in < 50 ms for a 500-line diff payload
- **Throughput:** No constraint — called once per review session, not in a hot path

## Security Considerations
- **Authentication/Authorization:** Persona files are embedded at compile time; no runtime file access, no path traversal risk
- **Input Validation:** Template execution must use `text/template` (not `html/template` auto-escaping) consistent with existing personas; the `{{.Diff}}` payload is developer-controlled source code, not user-supplied web input
- **Sensitive content:** `sentinel.md` system prompt must not include real secrets or credentials as examples; fixture files in `testdata/` use synthetic hardcoded values (e.g., `FAKE_API_KEY_1234`)

## Test Implementation Guidance
**Test Type:** UNIT

**Test Data Requirements:**
- A minimal synthetic payload struct with all template variable slots populated (can reuse the same struct already used for existing persona tests)
- The three `.md` files themselves are the test data (embedded in the test binary)

**Mock/Stub Requirements:** None

**Suggested test structure:**
```go
func TestBonusPersonas_TemplateRenders(t *testing.T) {
    payload := personas.Payload{
        Diff:       "--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+new",
        ScopeFocus: "security",
    }
    for _, name := range []string{"sentinel", "tracer", "idiomatic"} {
        rendered, err := personas.Render(name, payload)
        if err != nil {
            t.Fatalf("Render(%q) error: %v", name, err)
        }
        if strings.TrimSpace(rendered) == "" {
            t.Fatalf("Render(%q) produced empty output", name)
        }
    }
}
```

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./personas/...`)
- [x] No linting errors (`golangci-lint run ./personas/...`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `sentinel.md`, `tracer.md`, and `idiomatic.md` each contain a system prompt section, severity rubric, output format block, and all required template variable slots matching `bruce.md` structure
- [x] Each persona's system prompt explicitly names its target domain patterns (as specified in Scenario 1–3 above)
- [x] All three templates render without error when given a valid payload

**Manual Review:**
- [ ] Code reviewed and approved
