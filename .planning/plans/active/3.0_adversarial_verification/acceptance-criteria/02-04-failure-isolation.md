# Acceptance Criteria: Failure Isolation — Finding Never Dropped

**Related User Story:** [[02]: Skeptic Invocation & Verdict Parsing](../user-stories/02-skeptic-invocation-verdict-parsing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Error Handling | Go package `internal/verify` | `invokeSkeptic` in `invoke.go` wraps all runtime failures |
| Structured Logging | `fmt.Fprintf(os.Stderr, ...)` or `log/slog` | Failures visible in logs even though not propagated |
| Test Framework | `go test` + `testify` | Assert non-nil Verification on every failure path |
| Key Dependencies | `internal/reconcile` (Verification), `internal/fanout` (Result) | Failure modes originate from fanout layer |

## Related Files
- `internal/verify/invoke.go` - modify: all runtime failure paths return `*Verification` with `verdict="unverifiable"`
- `internal/fanout/engine.go` - reference: `Result.Status` values (StatusOK, StatusFailed, StatusTimeout) determine failure classification
- `internal/reconcile/emit.go` - reference: `Verification` struct (line 36) — `Notes` field carries failure explanation

## Happy Path Scenarios
**Scenario 1: All skeptics fail for a finding — finding still has a verdict**
- **Given** a finding and a skeptic `AgentConfig` whose `ChatCompleter` always returns errors
- **When** `invokeSkeptic` is called
- **Then** returns a non-nil `*Verification` with `Verdict: "unverifiable"`, `Notes` containing the error explanation, and `Skeptic` set to the agent name — the finding is NOT dropped

**Scenario 2: Skeptic times out — finding gets unverifiable verdict**
- **Given** a skeptic with `TimeoutSecs: 1` and a `ChatCompleter` that blocks for 5 seconds
- **When** `invokeSkeptic` is called with a context
- **Then** returns `*Verification{Verdict: "unverifiable", Notes: <timeout explanation>, Skeptic: <agent_name>}`

**Scenario 3: Skeptic returns empty response — finding gets unverifiable verdict**
- **Given** a `ChatCompleter` that returns an empty string as the final content
- **When** `invokeSkeptic` is called
- **Then** `parseVerdict` produces `unverifiable` with `Notes: "empty_response"`, and `invokeSkeptic` returns that Verification

## Edge Cases
**Edge Case 1: Skeptic returns JSON with empty verdict field**
- **Given** a `ChatCompleter` that returns `{"verdict": "", "reasoning": "I have no opinion"}`
- **When** `invokeSkeptic` is called
- **Then** returns `*Verification{Verdict: "unverifiable", Notes: <invalid_verdict explanation>}` — empty verdict is not a valid enum value

**Edge Case 2: Result status is StatusFailed with partial content**
- **Given** a `ChatCompleter` that returns an error but `Result.Content` has partial JSON from a previous turn
- **When** `invokeSkeptic` is called
- **Then** the function uses the error path, returning `unverifiable` — partial content is NOT parsed (the error status takes precedence)

**Edge Case 3: Multiple consecutive tool calls all fail**
- **Given** a `toolDispatcher` that returns errors for every tool call, and a `ChatCompleter` that keeps requesting tools
- **When** `invokeSkeptic` is called
- **Then** the tool loop eventually halts (max_turns or budget), and `invokeSkeptic` returns `unverifiable` with an explanatory `Notes`

## Error Conditions
**Error Scenario 1: Structured logging on failure**
- **Given** a skeptic invocation that fails due to a provider error
- **When** `invokeSkeptic` captures the failure in the Verification envelope
- **Then** a structured log message is written (to stderr or slog) containing: skeptic name, finding identifier (if available), error class (provider_error / timeout / malformed_output)

**Error Scenario 2: Programming errors still propagate**
- **Given** `invokeSkeptic` is called with `nil` context
- **When** the function executes
- **Then** a non-nil `error` is returned (not captured in Verification) — programming errors are distinguished from runtime failures

## Performance Requirements
- **Response Time:** Failure detection adds no significant overhead beyond the tool loop's own timeout/budget mechanics
- **Throughput:** Each finding is processed independently; one finding's failure does not block others (future story concern for parallelism)

## Security Considerations
- **Authentication/Authorization:** No new auth paths. Failure logging must not include API keys or provider secrets (only error messages from the provider SDK, which typically redact them).
- **Input Validation:** The `Notes` field may contain raw LLM output or provider error messages. Downstream consumers must treat it as untrusted text.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Failure-inducing fakes: (1) ChatCompleter that returns errors, (2) ChatCompleter that blocks until context cancel, (3) ChatCompleter that returns empty string, (4) ChatCompleter that returns invalid verdict enum
**Mock/Stub Requirements:** `ChatCompleter` (mock), `toolDispatcher` (mock). Assert that `*Verification` is ALWAYS non-nil for runtime failures, and `error` is ALWAYS non-nil for programming errors.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/verify/...`)
- [ ] No linting errors (`go vet ./internal/verify/...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Test asserts non-nil `*Verification` on every runtime failure path (provider error, timeout, malformed output, empty response, budget trip)
- [ ] Test asserts nil `*Verification` is impossible for runtime failures
- [ ] Structured logging present on all failure paths (verified by log capture in tests or code review)
- [ ] `error` return is non-nil ONLY for programming errors (nil args)

**Manual Review:**
- [ ] Code reviewed and approved
