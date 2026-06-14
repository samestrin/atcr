# Acceptance Criteria: Skeptic Prompt Construction

**Related User Story:** [[02]: Skeptic Invocation & Verdict Parsing](../user-stories/02-skeptic-invocation-verdict-parsing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Prompt Builder | Go package `internal/verify` | `buildSkepticPrompt` in `skeptic.go` |
| Test Framework | `go test` + `testify` | Table-driven tests |
| Key Dependencies | `internal/reconcile` (JSONFinding), `internal/payload` (FileEntry) | Input types |

## Related Files
- `internal/verify/skeptic.go` - create: `buildSkepticPrompt(finding reconcile.JSONFinding, entries []payload.FileEntry) string`
- `internal/reconcile/emit.go` - reference: `JSONFinding` struct (line 59) defines input shape
- `internal/payload/budget.go` - reference: `FileEntry` struct (line 11) provides code context

## Happy Path Scenarios
**Scenario 1: Prompt contains all required sections**
- **Given** a `JSONFinding` with severity="high", problem="nil dereference", fix="add nil check", evidence="line 42", confidence="high" and two `FileEntry` values with path+body content
- **When** `buildSkepticPrompt(finding, entries)` is called
- **Then** the returned string contains: (1) adversarial role framing ("adversarial skeptic" / "try to disprove"), (2) all finding detail fields (problem, fix, evidence, severity, confidence), (3) code context from both file entries (path + body in fenced code blocks), (4) tool-access instructions, (5) JSON verdict envelope spec with `{"verdict": "confirmed|refuted|unverifiable", "reasoning": "..."}`

**Scenario 2: Prompt is deterministic**
- **Given** the same `JSONFinding` and `[]FileEntry` inputs
- **When** `buildSkepticPrompt` is called twice
- **Then** both calls return byte-identical strings

**Scenario 3: Prompt with empty file entries**
- **Given** a valid `JSONFinding` and an empty `[]FileEntry{}` slice
- **When** `buildSkepticPrompt` is called
- **Then** the prompt is still well-formed with all sections present (role framing, finding details, verdict spec) but the code context section is empty or omitted gracefully

## Edge Cases
**Edge Case 1: Finding with empty optional fields**
- **Given** a `JSONFinding` with empty `Fix` and empty `Evidence` strings
- **When** `buildSkepticPrompt` is called
- **Then** the prompt renders without those fields but remains well-formed and includes the verdict envelope spec

**Edge Case 2: File entry with large body**
- **Given** a `FileEntry` with a 50KB `Body` string
- **When** `buildSkepticPrompt` is called
- **Then** the full body is included (caller is responsible for pre-truncation per the story contract)

**Edge Case 3: File entry with special characters in path or body**
- **Given** a `FileEntry` with backticks, HTML entities, or unicode in `Path` and `Body`
- **When** `buildSkepticPrompt` is called
- **Then** the content is included verbatim (the prompt is for an LLM, not rendered in HTML)

## Error Conditions
**Error Scenario 1: Zero-value JSONFinding**
- **Given** a `JSONFinding{}` with all zero/empty fields
- **When** `buildSkepticPrompt` is called
- **Then** the function returns a valid prompt string (not empty, not panicking) — it includes role framing and verdict spec even with no finding details

## Performance Requirements
- **Response Time:** `buildSkepticPrompt` completes in < 1ms for typical inputs (10 findings, 5 file entries)
- **Throughput:** No allocations beyond the returned string; uses `strings.Builder` internally

## Security Considerations
- **Input Validation:** No validation required — the function is a pure string builder. The caller is responsible for pre-truncated file entries and valid finding data.
- **Injection:** The prompt is consumed by an LLM, not rendered in HTML or shell. No injection risk from finding content.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven tests with: (1) fully populated finding + entries, (2) empty finding, (3) empty entries, (4) finding with special characters
**Mock/Stub Requirements:** None — pure function, no dependencies to mock

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/verify/...`)
- [ ] No linting errors (`go vet ./internal/verify/...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `buildSkepticPrompt` is a pure function with no side effects
- [ ] Prompt contains all 5 required sections: role framing, finding details, code context, tool instructions, verdict envelope spec
- [ ] Determinism verified by test (same input → identical output)
- [ ] >= 95% coverage on `skeptic.go`

**Manual Review:**
- [ ] Code reviewed and approved
