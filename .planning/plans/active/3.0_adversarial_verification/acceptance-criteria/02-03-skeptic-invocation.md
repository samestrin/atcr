# Acceptance Criteria: Skeptic Invocation via Tool Loop

**Related User Story:** [[02]: Skeptic Invocation & Verdict Parsing](../user-stories/02-skeptic-invocation-verdict-parsing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Skeptic Invoker | Go package `internal/verify` | `invokeSkeptic` in `invoke.go` |
| Tool Loop | `internal/fanout` | `Engine.invokeToolLoop` (loop.go:81) |
| Test Framework | `go test` + `testify` | Fake `ChatCompleter` and `toolDispatcher` |
| Key Dependencies | `internal/fanout` (Engine, ChatCompleter, Agent, Result), `internal/registry` (AgentConfig) | Reused infrastructure |

## Related Files
- `internal/verify/invoke.go` - create: `invokeSkeptic(ctx, skeptic, prompt, cc, disp) (*reconcile.Verification, error)`
- `internal/fanout/loop.go` - reference: `invokeToolLoop` (line 81) — reused unchanged
- `internal/fanout/engine.go` - reference: `Engine` struct (line 132), `Agent` struct (line 48), `Result` struct (line 95)
- `internal/registry/config.go` - reference: `AgentConfig` struct (line 56), `RoleSkeptic` constant (line 37)

## Happy Path Scenarios
**Scenario 1: Skeptic confirms a finding**
- **Given** a skeptic `AgentConfig` with `Role: "skeptic"`, a valid prompt string, a fake `ChatCompleter` that returns `{"verdict": "confirmed", "reasoning": "evidence valid"}` as the final message, and a valid `toolDispatcher`
- **When** `invokeSkeptic` is called
- **Then** returns `&Verification{Verdict: "confirmed", Notes: "evidence valid", Skeptic: <agent_name>}` with nil error

**Scenario 2: Skeptic refutes a finding**
- **Given** a fake `ChatCompleter` that returns `{"verdict": "refuted", "reasoning": "false positive"}` as the final message
- **When** `invokeSkeptic` is called
- **Then** returns `&Verification{Verdict: "refuted", Notes: "false positive", Skeptic: <agent_name>}` with nil error

**Scenario 3: Skeptic uses tools before concluding**
- **Given** a fake `ChatCompleter` that first returns a tool_call (e.g., `read_file`), then after receiving the tool result returns `{"verdict": "confirmed", "reasoning": "verified via file read"}`
- **When** `invokeSkeptic` is called
- **Then** the tool loop executes the tool call, the second response is parsed, and the result is `&Verification{Verdict: "confirmed", Notes: "verified via file read", Skeptic: <agent_name>}`

## Edge Cases
**Edge Case 1: Provider error during Chat**
- **Given** a fake `ChatCompleter` that returns an error on the first `Chat` call (e.g., "rate limit exceeded")
- **When** `invokeSkeptic` is called
- **Then** returns `&Verification{Verdict: "unverifiable", Notes: <explanation including error>, Skeptic: <agent_name>}` — error is NOT propagated to the caller

**Edge Case 2: Context timeout**
- **Given** a context with a deadline that expires before the `ChatCompleter` responds, and a fake that blocks until context is cancelled
- **When** `invokeSkeptic` is called
- **Then** returns `&Verification{Verdict: "unverifiable", Notes: <timeout explanation>, Skeptic: <agent_name>}` — context error is captured in the envelope

**Edge Case 3: Skeptic returns malformed output**
- **Given** a fake `ChatCompleter` that returns `I don't know` as the final content (not JSON)
- **When** `invokeSkeptic` is called
- **Then** `parseVerdict` produces `unverifiable` with `malformed_output` notes, and `invokeSkeptic` returns that `Verification` — the finding is not dropped

**Edge Case 4: Budget tripped (max_turns)**
- **Given** a skeptic `AgentConfig` with `MaxTurns: 2`, and a fake `ChatCompleter` that keeps returning tool_calls beyond 2 turns
- **When** `invokeSkeptic` is called
- **Then** returns `&Verification{Verdict: "unverifiable", Notes: <budget trip explanation>, Skeptic: <agent_name>}`

## Error Conditions
**Error Scenario 1: Nil context**
- **Given** `ctx = nil`
- **When** `invokeSkeptic` is called
- **Then** returns a non-nil `error` (programming error guard — panic or explicit check)

**Error Scenario 2: Nil ChatCompleter**
- **Given** `cc = nil`
- **When** `invokeSkeptic` is called
- **Then** returns a non-nil `error` (programming error guard)

**Error Scenario 3: Nil toolDispatcher**
- **Given** `disp = nil`
- **When** `invokeSkeptic` is called
- **Then** returns a non-nil `error` (programming error guard)

## Performance Requirements
- **Response Time:** `invokeSkeptic` overhead beyond the tool loop is < 1ms (prompt is already constructed; only Agent construction and Result-to-Verification conversion are new)
- **Throughput:** Skeptic invocations are sequential per-finding (parallelism across findings is a future story concern)

## Security Considerations
- **Authentication/Authorization:** The skeptic uses the same `ChatCompleter` (and thus the same provider API key) as Epic 2.0 reviewers. No new auth paths.
- **Input Validation:** Programming errors (nil args) return `error`. Runtime failures are captured in the `Verification` envelope.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fake `ChatCompleter` implementations: (1) returns confirmed JSON, (2) returns refuted JSON, (3) returns error, (4) returns malformed output, (5) blocks until context cancel. Fake `toolDispatcher` for tool-call scenarios.
**Mock/Stub Requirements:** `ChatCompleter` interface (mock), `toolDispatcher` interface (mock), `registry.AgentConfig` (construct directly)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/verify/...`)
- [ ] No linting errors (`go vet ./internal/verify/...`)
- [ ] Build succeeds (`go build ./...`)
- [ ] No import cycle between `internal/verify` and `internal/fanout` (`go build ./...` verifies)

**Story-Specific:**
- [ ] `invokeSkeptic` never propagates runtime errors to the caller — all failures captured in `Verification`
- [ ] Only programming errors (nil args) return `error`
- [ ] `Verification.Skeptic` is populated with the agent name in all paths
- [ ] Tool loop is reused unchanged from `internal/fanout`

**Manual Review:**
- [ ] Code reviewed and approved
