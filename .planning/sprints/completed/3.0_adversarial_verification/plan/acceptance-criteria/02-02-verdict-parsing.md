# Acceptance Criteria: Verdict Parsing

**Related User Story:** [[02]: Skeptic Invocation & Verdict Parsing](../user-stories/02-skeptic-invocation-verdict-parsing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Verdict Parser | Go package `internal/verify` | `parseVerdict` in `verdict.go` |
| Test Framework | `go test` + `testify` | Table-driven tests covering 7+ cases |
| Key Dependencies | `internal/reconcile` (Verification struct), `encoding/json` | Output type and JSON parsing |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/fanout/engine.go:132` - reference: `Engine` struct (caller of verdict parser via tool-loop results)

- `internal/verify/verdict.go` - create: `parseVerdict(response string) (*reconcile.Verification, error)`
- `internal/reconcile/emit.go` - reference: `Verification` struct (line 36) with `Verdict`, `Skeptic`, `Notes` fields
- `internal/verify/testdata/` - create: test fixtures (`malformed-response.txt`, etc.)

## Happy Path Scenarios
**Scenario 1: Valid JSON with `confirmed` verdict**
- **Given** a response string `{"verdict": "confirmed", "reasoning": "evidence holds up"}`
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "confirmed", Notes: "evidence holds up"}` with nil error

**Scenario 2: Valid JSON with `refuted` verdict**
- **Given** a response string `{"verdict": "refuted", "reasoning": "code path unreachable"}`
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "refuted", Notes: "code path unreachable"}` with nil error

**Scenario 3: Valid JSON with `unverifiable` verdict**
- **Given** a response string `{"verdict": "unverifiable", "reasoning": "insufficient context"}`
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "unverifiable", Notes: "insufficient context"}` with nil error

**Scenario 4: Valid JSON with extra fields**
- **Given** a response string `{"verdict": "confirmed", "reasoning": "ok", "extra_field": "ignored", "confidence": 0.9}`
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "confirmed", Notes: "ok"}` — extra fields silently ignored (standard `json.Unmarshal` behavior)

## Edge Cases
**Edge Case 1: JSON wrapped in markdown fences**
- **Given** a response string `` ```json\n{"verdict": "confirmed", "reasoning": "ok"}\n``` ``
- **When** `parseVerdict` is called
- **Then** the parser extracts the first JSON object from the response and returns `&Verification{Verdict: "confirmed", Notes: "ok"}`

**Edge Case 2: JSON embedded in prose**
- **Given** a response string `Here is my verdict: {"verdict": "refuted", "reasoning": "wrong file"} — hope that helps.`
- **When** `parseVerdict` is called
- **Then** the parser scans for the first `{...}` JSON object and returns `&Verification{Verdict: "refuted", Notes: "wrong file"}`

**Edge Case 3: Valid JSON with empty reasoning**
- **Given** a response string `{"verdict": "confirmed", "reasoning": ""}`
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "confirmed", Notes: ""}` — empty reasoning is valid

**Edge Case 4: Valid JSON with missing reasoning field**
- **Given** a response string `{"verdict": "confirmed"}`
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "confirmed", Notes: ""}` — missing reasoning defaults to empty

## Error Conditions
**Error Scenario 1: Malformed JSON**
- **Given** a response string `{verdict: confirmed}` (not valid JSON)
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "unverifiable", Notes: "malformed_output: {verdict: confirmed}"}` — raw text preserved in Notes

**Error Scenario 2: Invalid verdict enum**
- **Given** a response string `{"verdict": "maybe", "reasoning": "unclear"}`
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "unverifiable", Notes: "invalid_verdict: maybe (raw: {\"verdict\": \"maybe\", \"reasoning\": \"unclear\"})"}`

**Error Scenario 3: Empty response**
- **Given** an empty string `""`
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "unverifiable", Notes: "empty_response"}`

**Error Scenario 4: Response with no JSON object**
- **Given** a response string `I cannot determine the verdict at this time.`
- **When** `parseVerdict` is called
- **Then** returns `&Verification{Verdict: "unverifiable", Notes: "malformed_output: I cannot determine the verdict at this time."}`

## Performance Requirements
- **Response Time:** `parseVerdict` completes in < 1ms for responses up to 10KB
- **Throughput:** No external calls — pure JSON parsing + string operations

## Security Considerations
- **Input Validation:** The parser never executes or evaluates the response content. Malformed input is captured verbatim in Notes.
- **Injection:** Notes content is written to `verification.json` — downstream renderers must escape it (out of scope for this story).

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven tests with >= 11 cases: confirmed, refuted, unverifiable (valid), malformed JSON, invalid enum, empty response, extra fields, markdown-fenced JSON, prose-embedded JSON, empty reasoning, missing reasoning, no-JSON prose
**Mock/Stub Requirements:** None — pure function, no dependencies to mock

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/verify/...`)
- [x] No linting errors (`go vet ./internal/verify/...`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] All 7 original test cases covered (confirmed, refuted, unverifiable, malformed JSON, invalid enum, empty response, extra fields)
- [x] JSON extraction from markdown fences and prose implemented and tested
- [x] >= 95% coverage on `verdict.go`

**Manual Review:**
- [x] Code reviewed and approved
