# Acceptance Criteria: Fan-out Agent Execution

**Related User Story:** [01: CLI Review Workflow](../user-stories/01-cli-review-workflow.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Fan-out Engine | Go sync package | WaitGroup for parallel lane |
| HTTP Client | net/http | OpenAI-compatible API calls |
| Rate Limiting | golang.org/x/time/rate | serial lane rate limiter |
| Test Framework | testify + httptest | mock HTTP servers |

## Related Files
- `internal/fanout/engine.go` - create: parallel + serial lane execution, fallback chain, timeout context
- `internal/fanout/engine_test.go` - create: tests for concurrent execution and failure modes
- `internal/llmclient/client.go` - create: HTTP client for OpenAI-compatible chat completions API
- `internal/stream/parser.go` - create: SSE stream parser for LLM responses
- `internal/fanout/status.go` - create: per-agent `status.json` writer with `ok | failed | timeout | parse_error` outcomes

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [LLM Client & Fan-out](../documentation/llm-client-fanout.md) — Authoritative spec for the OpenAI-compatible client, retry policy, parallel/serial lanes, partial-success semantics, per-agent artifacts.
- [Findings Format v1](../documentation/findings-format.md) — 8-col per-source format; the engine appends `REVIEWER` to model output (7-col → 8-col). Models never self-attribute.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — How the fan-out's `findings.txt` and `status.json` feed the reconciler's source-discovery rule.

### Spec alignment notes

- **Models emit 7 columns** (no `REVIEWER`); the engine appends the `REVIEWER` field when writing per-source `findings.txt`. Per `plan.md` clarifications (2026-06-10).
- **Severity rubric** uses `CRITICAL|HIGH|MEDIUM|LOW` directly in persona prompts; not blocking/significant/minor with implicit translation.
- Retry policy: 429/500/502/503/504 only, ~500ms initial delay, 1.5× backoff. Other 4xx fail immediately. Retry budget must not exhaust the per-agent or global timeout.
- API keys resolved from env vars at invoke time (not load time). Agent names sanitized via `filepath.Base` against path traversal.
- Per-agent `status.json`: `{"agent", "status", "findings_count", "duration_ms", "payload_mode", "truncated", "files_dropped"}`. `partial: true` in `summary.json` when ≥1 agent fails but ≥1 succeeds.

## Happy Path Scenarios

**Scenario 1: Parallel agent invocation**
- **Given** 3 agents configured in roster with `lane: parallel`
- **When** fan-out engine starts
- **Then** all 3 agents receive the same diff payload concurrently via sync.WaitGroup

**Scenario 2: Serial rate-limited invocation**
- **Given** 2 agents configured with `lane: serial` and `rate_limit: 10rps`
- **When** fan-out engine starts
- **Then** agents are invoked sequentially with rate limiting applied between calls

**Scenario 3: Per-agent artifacts written**
- **Given** agent "reviewer-a" completes successfully
- **When** fan-out engine writes results
- **Then** artifacts written to `sources/pool/raw/agent/reviewer-a/`: `review.md`, `findings.txt`, `status.json`

**Scenario 4: status.json records outcome**
- **Given** agent completes with 5 findings
- **When** status.json is written
- **Then** file contains: `{"agent": "reviewer-a", "status": "success", "findings_count": 5, "duration_ms": 3200}`

## Edge Cases

**Edge Case 1: Global timeout exceeded**
- **Given** `--timeout 60s` flag and agent takes 65 seconds
- **When** context deadline is reached
- **Then** agent call is cancelled; status.json records `status: "timeout"`; other agents continue

**Edge Case 2: Agent returns non-200 HTTP status**
- **Given** LLM API returns 503 Service Unavailable
- **When** client receives response
- **Then** retry up to 2 times with exponential backoff; if still failing, status.json records `status: "error"`

**Edge Case 3: Fallback chain exhausted**
- **Given** primary agent has 2 fallbacks; all 3 return errors
- **When** fallback chain completes
- **Then** status.json records `status: "failed"` with last error message; partial flag set

**Edge Case 4: Mixed parallel and serial lanes**
- **Given** 2 agents in parallel lane, 1 agent in serial lane
- **When** fan-out engine executes
- **Then** parallel agents run concurrently; serial agent runs after parallel lane completes (or independently based on design)

## Error Conditions

**Error Scenario 1: All agents fail**
- Error message: "all agents failed: reviewer-a (timeout), reviewer-b (connection refused), reviewer-c (401 unauthorized)"
- Exit code: 1

**Error Scenario 2: Invalid API key**
- Error message: "agent reviewer-a: authentication failed (HTTP 401)"
- Exit code: 1 (if all agents fail with auth)

**Error Scenario 3: Malformed response from LLM**
- Error message: "agent reviewer-a: failed to parse response: unexpected EOF"
- Recorded in status.json as `status: "parse_error"`

## Performance Requirements
- **Response Time:** Parallel lane completes within max(single agent time) + 500ms overhead
- **Throughput:** Supports up to 10 concurrent agent calls without resource exhaustion

## Security Considerations
- **Authentication/Authorization:** API keys loaded from registry.yaml per-provider; passed via `Authorization: Bearer` header
- **Input Validation:** Diff payload sanitized before inclusion in LLM prompt; no shell metacharacters injected
- **Timeout Enforcement:** Global context timeout prevents runaway requests

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Mock LLM responses (SSE streams), sample diff payloads, roster configurations with various lane/rate_limit combos
**Mock/Stub Requirements:** httptest.Server for LLM API mocking; configurable response delays for timeout tests; custom round-tripper for rate limit testing

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Parallel lane uses sync.WaitGroup for concurrent agent invocation
- [ ] Serial lane respects rate limiting between calls
- [ ] Per-agent artifacts (review.md, findings.txt, status.json) written to correct paths
- [ ] Global timeout context cancels in-flight requests
- [ ] Fallback chain attempts alternatives before marking agent as failed
- [ ] Partial-success semantics: review succeeds if ≥1 agent succeeds

**Manual Review:**
- [ ] Code reviewed and approved
