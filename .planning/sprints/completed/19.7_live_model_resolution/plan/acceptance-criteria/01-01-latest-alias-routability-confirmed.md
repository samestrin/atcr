# Acceptance Criteria: `~…-latest` Alias Routability Confirmed via Authenticated Completion Call

**Related User Story:** [01: Catalog Routability Spike & Stable-Channel Heuristic](../user-stories/01-catalog-routability-spike-stable-channel-heuristic.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Design spike / one-time manual verification | Not a production code path; no new resolver code lands in this AC |
| Test Framework | MANUAL — one authenticated call to `https://openrouter.ai/api/v1/chat/completions`, outcome recorded by hand | Not run in CI; CI stays zero-live-network per Epic 19.7 Objective 11 |
| Key Dependencies | `OPENROUTER_API_KEY` (env var, maintainer's own), `net/http` (or `curl`), the `HTTPClient`/retry conventions already proven in `internal/personas/client.go:35` and `client.go:99` | The story explicitly allows a throwaway script following the same conventions rather than new shipped code |

### Related Files (from codebase-discovery.json)
- `internal/personas/client.go:35` — reference only: reuse the `HTTPClient` interface seam for the one-off authenticated completion call.
- `internal/personas/client.go:99` — reference only: reuse the `fetch()` retry/backoff/timeout/size-cap pattern as the template for the spike call.
- `.planning/plans/active/19.7_live_model_resolution/documentation/openrouter-catalog-api.md` — modify: record the live completion-call outcome (status code, response detail, and an explicit statement that `~` alias routability is now independently confirmed by the epic's own spike, not just cited from the quickstart docs).
- `personas/community/index.json:1` — reference only: ground truth for existing vendor-prefixed slugs used to contextualize the spike finding.
- `internal/personas/catalog.go` — create (in later stories Theme 2/3): the future catalog client/resolver file that will consume this spike's finding — noted here so the recorded outcome is written in a form directly usable when that file is authored.

## Happy Path Scenarios
**Scenario 1: `~`-prefixed alias completes successfully**
- **Given** a valid `OPENROUTER_API_KEY` is set in the maintainer's environment
- **When** the maintainer issues one authenticated `POST` to `https://openrouter.ai/api/v1/chat/completions` with `"model": "~openai/gpt-latest"` and a minimal prompt/message body
- **Then** the response is a `200 OK` chat completion (non-empty `choices[0].message.content`), and this success — including the resolved model identity if OpenRouter echoes one back — is recorded verbatim in `documentation/openrouter-catalog-api.md` as confirmation that `~`-prefixed `-latest` aliases are completion-routable, not just catalog-browsing artifacts

**Scenario 2: Finding feeds downstream design**
- **Given** the routability outcome (success or failure) has been recorded
- **When** Stories 2 (family/channel binding) and 3 (hybrid resolver) are later authored
- **Then** they cite this story's recorded finding as their design basis rather than re-deriving or re-assuming alias routability

## Edge Cases
**Edge Case 1: Alias resolves to a different concrete model than expected**
- **Given** the completion call succeeds
- **When** the response (or a follow-up header/field, if OpenRouter exposes one) indicates the alias resolved to a specific concrete slug
- **Then** that resolved slug is recorded alongside the outcome, since it is useful corroborating detail for the hybrid resolver design even though it is not required for a pass/fail verdict

**Edge Case 2: Rate limiting or transient error on the one call**
- **Given** the single authenticated call returns a transient error (e.g. `429` or `5xx`)
- **When** the maintainer is performing the one-time spike
- **Then** the call may be retried manually (mirroring `internal/personas/client.go`'s retriable-status logic at `client.go:78`) until a definitive outcome (success or a non-transient failure) is obtained, and only the definitive outcome is recorded — the transient blip itself is not the finding

## Error Conditions
**Error Scenario 1: `~`-prefixed alias is rejected as an invalid model identifier**
- Error message: whatever OpenRouter's API returns verbatim (e.g. a `400`-class body naming the unrecognized `model` value) — recorded exactly as received, not paraphrased
- HTTP status / error code: `4xx` (non-transient), recorded as a definitive **refutation** of alias routability

**Error Scenario 2: Missing or invalid `OPENROUTER_API_KEY`**
- Error message: `401 Unauthorized` (or equivalent OpenRouter auth-failure body)
- HTTP status / error code: `401` — this is a precondition failure, not a routability finding; the spike must be re-run with a valid key before any outcome is recorded

## Performance Requirements
- **Response Time:** N/A — this is a single manual call, not a latency-sensitive production path; no timeout requirement beyond avoiding an indefinite hang (the `client.go` `fetchTimeout` convention of 30s is a reasonable reference bound if scripted)
- **Throughput:** Exactly one live completion call is made for this AC (per the story's Measurable success criterion) — no batch or repeated-load testing is in scope

## Security Considerations
- **Authentication/Authorization:** `OPENROUTER_API_KEY` is read from the environment only, never hardcoded into any script, commit, or the recorded documentation; the recorded finding must not include the key value itself
- **Input Validation:** N/A — this is an outbound call the maintainer constructs directly, not a code path that accepts external/untrusted input; no new attack surface is introduced

## Test Implementation Guidance
**Test Type:** MANUAL
**Test Data Requirements:** A valid `OPENROUTER_API_KEY`; the request body `{"model": "~openai/gpt-latest", "messages": [...]}` (or an equivalent minimal prompt) as documented in `documentation/openrouter-catalog-api.md`'s Code Examples section
**Mock/Stub Requirements:** None — by design this is the one live-network exception explicitly carved out of the zero-live-network CI discipline (Epic 19.7 Objective 11); it is a manual, one-time, out-of-CI action, not a new test fixture

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (no new automated tests introduced by this AC; existing suite is unaffected)
- [x] No linting errors (N/A if no code file is added; applies only if a throwaway script is committed)
- [x] Build succeeds

**Story-Specific:**
- [x] Exactly one live authenticated completion call was made against a `~…-latest` alias
- [x] The outcome (success with response detail, or definitive failure with status/error) is recorded verbatim in `documentation/openrouter-catalog-api.md`
- [x] The recorded finding is stated in terms Stories 2 and 3 can cite directly as their alias-routability design basis
- [x] No production resolver code path was introduced or altered by this AC (per the story's Constraints)

**Manual Review:**
- [ ] Code reviewed and approved
