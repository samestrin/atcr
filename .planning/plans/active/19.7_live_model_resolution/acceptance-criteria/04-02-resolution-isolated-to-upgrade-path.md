# Acceptance Criteria: Resolver and Models-Endpoint Calls Occur Only Inside `Upgrade()` — Never on the Review Hot Path

**Related User Story:** [04: Reproducible Upgrade with Before→After Lock Reporting](../user-stories/04-reproducible-upgrade-before-after-lock-reporting.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Architectural guardrail test + code-isolation review | Enforces Epic 19.6's locked C3 posture: no silent runtime model change ever occurs |
| Test Framework | `testing` + `testify`, plus a static/structural check (e.g. an import-boundary or call-graph assertion) alongside behavioral tests | Behavioral test exercises `internal/registry.ResolvePersona()` (`internal/registry/persona.go:47`) and the fan-out review path and asserts zero HTTP calls to the models endpoint |
| Key Dependencies | `internal/registry/persona.go`'s `ResolvePersona()`, `internal/fanout/review.go` (review invocation path), the `HTTPClient` fake/spy already used across `internal/personas/*_test.go` | No new production dependency; this AC is purely a negative-space guarantee |

### Related Files (from codebase-discovery.json)
- `internal/personas/upgrade_test.go` — modify: add a test asserting the resolver/catalog client is constructed and invoked exactly once per `Upgrade()` call, and only from within `Upgrade()`.
- `internal/registry/persona.go:47` (`ResolvePersona()`) — reference only (no modification in this AC): `ResolvePersona()` must remain untouched — a passing test proves it never constructs or calls the resolver/catalog client.
- `internal/fanout/review.go` — reference only (no modification in this AC): the review invocation path must never import or call the resolver/catalog client package.
- `cmd/atcr/personas_test.go` — modify: add/extend an integration test using a spy `HTTPClient` across a full `personas test`, `personas list`, and `personas upgrade` sequence, asserting the models-endpoint request is observed only during the `upgrade` invocation.
- `internal/personas/client.go:35` (`HTTPClient`) — reference only: the spy/recording `HTTPClient` implementation used in the isolation tests mirrors this existing interface.
- `internal/personas/catalog.go` — reference: the resolver/catalog client package that must only be invoked from `Upgrade()`'s call chain.

## Happy Path Scenarios
**Scenario 1: `atcr personas upgrade` is the sole caller of the resolver and models endpoint**
- **Given** a spy `HTTPClient` that records every outbound request's URL/host
- **When** the maintainer runs `atcr personas upgrade anthony`
- **Then** exactly the expected requests occur (persona-unit fetch plus the catalog/models-endpoint fetch used by the resolver), and both originate from within `Upgrade()`'s call chain — no other package initiates a models-endpoint request during this invocation

**Scenario 2: Review path never touches the resolver, regardless of persona or channel**
- **Given** a persona bound to `@stable` or `@latest` (any of the 10 roster personas)
- **When** `internal/registry.ResolvePersona()` is exercised directly (unit test) and, separately, a full review fan-out is exercised (integration test) with the same spy `HTTPClient`
- **Then** zero requests to the OpenRouter models/catalog endpoint are recorded in either path — `ResolvePersona()` only reads local persona files (per its existing `dirs.Project`/`dirs.Registry` file-resolution chain at `persona.go:60`), never network

## Edge Cases
**Edge Case 1: A future contributor adds a convenience resolver call to `ResolvePersona()` or persona load**
- **Given** the guardrail test exists and asserts zero endpoint calls outside `Upgrade()`
- **When** a hypothetical change adds a resolver invocation to `ResolvePersona()` or any persona-load path
- **Then** the guardrail test fails deterministically (spy records an unexpected request, or a call-count assertion on the resolver/catalog constructor exceeds one-per-upgrade), catching the regression before merge

**Edge Case 2: `--all` upgrade still respects the isolation — one resolver invocation per persona, not one shared background client leaking into other commands**
- **Given** `atcr personas upgrade --all` upgrades multiple personas in one process invocation
- **When** the run completes
- **Then** the resolver/catalog client is constructed and invoked per-persona strictly inside the `runPersonaUpgrades()` loop's call into `Upgrade()`, with no package-level/global client instance that a subsequent, unrelated command invocation in the same process could reuse or trigger

## Error Conditions
**Error Scenario 1: Test harness detects an out-of-band endpoint call**
- Error message: test failure output identifying the unexpected recorded request (method, URL/host) and which code path triggered it
- HTTP status / error code: N/A — this is a test-time assertion failure, not a runtime HTTP error; the test must fail loudly (not skip or warn) so CI blocks the merge

**Error Scenario 2: Resolver package is imported from a disallowed location**
- Error message: N/A (compile-time/lint or code-review finding, not a runtime error) — flagged as a review/lint concern if a future PR adds an import of the resolver/catalog package into `internal/registry` or `internal/fanout`
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — this AC is a correctness/isolation guarantee, not a latency-sensitive path; the review hot path's performance profile must be unchanged (zero added network calls) by this story
- **Throughput:** Zero models-endpoint or resolver calls per review invocation, verified for both single-persona review and any batch/fan-out review scenario exercised by existing `internal/fanout` tests

## Security Considerations
- **Authentication/Authorization:** Keeping resolution off the review hot path also keeps the OpenRouter API key/network dependency out of the review request path entirely — reviews remain runnable offline/air-gapped except for the actual model-completion call itself, unaffected by this story
- **Input Validation:** N/A — this AC asserts absence of a call path, not new input handling

## Test Implementation Guidance
**Test Type:** UNIT (isolation assertion on `ResolvePersona()` with a spy client) + INTEGRATION (full CLI sequence across `test`/`list`/`upgrade` with a shared spy `HTTPClient` asserting call provenance)
**Test Data Requirements:** A spy/recording `HTTPClient` implementation (extend or reuse the fakes already present in `internal/personas/client_test.go`) capturing URL/host per call plus a lightweight call-site tag or counter
**Mock/Stub Requirements:** No live network in CI (per Epic 19.7 Objective 11, zero-live-network); the spy client fully replaces the transport, and the test package must import neither `internal/registry` from the resolver package nor vice versa in a way that would defeat the isolation check

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A test with a spy `HTTPClient` proves the models/catalog endpoint is called only from within `Upgrade()`'s call chain, for both single-name and `--all` invocations
- [ ] A test exercising `internal/registry.ResolvePersona()` directly proves zero resolver/catalog-client construction or invocation on that path
- [ ] A test exercising the review fan-out path (`internal/fanout/review.go`) proves zero resolver/catalog-client invocation
- [ ] No import of the resolver/catalog client package exists in `internal/registry/persona.go` or `internal/fanout/review.go`

**Manual Review:**
- [ ] Code reviewed and approved
