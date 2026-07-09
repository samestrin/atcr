# Acceptance Criteria: Review Path Reads the Locked Model Slug with Zero Endpoint Calls

**Related User Story:** [02: Family/Channel Binding & Resolved Lock](../user-stories/02-family-channel-binding-resolved-lock.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function behavior verification (`renderAgent`) | `internal/fanout/review.go` — no resolver logic exists yet; this AC locks in the read-only, static-field contract before Theme 3 lands |
| Test Framework | Go `testing` + `net/http/httptest` (to prove zero network access) | Mirrors existing fanout test conventions (e.g. `internal/fanout/engine_test.go`) |
| Key Dependencies | None new — `internal/registry`, `internal/llmclient` (already imported by `internal/fanout`) | |

### Related Files (from codebase-discovery.json)
- `internal/fanout/review.go:998` (`renderAgent`) and `:~1057` (`Invocation` construction) — reference/verify, no behavioral change expected: `renderAgent` already builds `llmclient.Invocation{..., Model: ac.Model, ...}` directly from `AgentConfig.Model`. This AC locks that contract in with a regression test asserting `Invocation.Model == ac.Model` regardless of whatever value `ac.Binding` carries (added in AC 02-01), and that a `Binding` value is never substituted, appended to, or used to derive the invoked model string.
- `internal/registry/config.go` (`AgentConfig.Model`, `AgentConfig.Binding`) — reference: `Model` is the sole field consumed on the review path (the "lock"); `Binding` is present on the struct (post AC 02-01) but inert here — no code path in `internal/fanout` may read it.
- `internal/registry/persona.go:47` (`ResolvePersona`) — reference only, **must NOT be modified by this story**: `ResolvePersona` resolves which *prompt template* wins (project > registry/pinned-community > `_base` > embedded); it has no involvement in *which model* is invoked and must stay that way. See Edge Case 1.
- `internal/fanout` — create/modify: add a regression test file (e.g. `lock_test.go` alongside `engine_test.go`) including an `HTTPClient` stub that fails the test if `renderAgent`/`buildSlots` ever issues an HTTP call.
- `internal/personas/client.go:35` (`HTTPClient`) — reference only: the stub/spy used in the regression test mirrors the existing injectable `HTTPClient` interface to prove zero network calls on the review path.

## Happy Path Scenarios
**Scenario 1: `renderAgent` invokes the locked `Model`, never `Binding`**
- **Given** an `AgentConfig` with `Model: "anthropic/claude-opus-4.8"` and `Binding: "anthropic/claude-opus@stable"` set to different, non-overlapping values
- **When** `renderAgent` builds the `Agent`/`Invocation` for a review
- **Then** `Invocation.Model` equals exactly `"anthropic/claude-opus-4.8"` (the `Model` field's value) — `Binding`'s value never appears in the invocation, the prompt, or any log/error message on this path

**Scenario 2: A full review run makes zero network calls to resolve the model**
- **Given** a `ReviewConfig` with one or more agents whose `AgentConfig` carries both `Model` and `Binding`
- **When** the review is prepared and dispatched end-to-end (`PrepareReview` → `buildSlots` → `renderAgent` → the LLM call itself, stubbed)
- **Then** the only outbound HTTP call observed is the actual LLM completion request built from `Invocation.Model`/`Invocation.BaseURL` — no separate request is made to any catalog/models/resolution endpoint before or during that call

## Edge Cases
**Edge Case 1: Resolver logic must not leak into `ResolvePersona`**
- **Given** `internal/registry/persona.go:47`'s `ResolvePersona` function as it exists today (project > registry/pinned-community > `_base` > embedded prompt-resolution chain)
- **When** this story's changes are reviewed/diffed
- **Then** `persona.go` shows **no** modification — the binding/lock layer is strictly upstream of prompt resolution and must never be merged into or invoked from `ResolvePersona`; a future maintainer accidentally routing resolver calls through this function (e.g. to "conveniently" pick a model alongside the prompt) is exactly the risk this edge case guards against, per the story's own Risk table

**Edge Case 2: `Binding` set but `Model` empty (a malformed/incomplete persona)**
- **Given** an `AgentConfig` where `Binding` is set but `Model` is empty (a config that should already fail `validateAgent`'s existing required-field check for `model`)
- **When** `LoadRegistry` validates the registry
- **Then** the existing "required field 'model' is missing" error fires unchanged (`internal/registry/config.go` `validateAgent`) — this story does not relax that requirement or substitute `Binding` as a fallback source for `Model`

**Edge Case 3: Fallback chain (`AgentConfig.Fallback`) still resolves purely through `Model`**
- **Given** a primary agent with a `Fallback` reference to a second agent, both carrying distinct `Model`/`Binding` values
- **When** `buildSlots` constructs the fallback chain
- **Then** each fallback's `Invocation.Model` is its own `Model` field's value — `Binding` plays no role in fallback-chain construction, matching the unmodified pre-story behavior

## Error Conditions
**Error Scenario 1: A test double detects an unexpected network call**
- Error message: test-only assertion failure, e.g. `"unexpected HTTP request to %s during renderAgent — review path must resolve zero endpoints"`
- HTTP status / error code: N/A (this is a test-harness assertion, not a production error path); production code has no resolver call to fail in the first place at this story's state

## Performance Requirements
- **Response Time:** No change — `renderAgent` performs the same in-memory field read (`ac.Model`) it did before this story; the addition of an inert `Binding` field must not add any I/O or network round trip to the review path.
- **Throughput:** Zero additional network calls per review, confirmed by the httptest-based regression test — this is the load-bearing NFR for the epic's reproducibility guarantee (Objective 2).

## Security Considerations
- **Authentication/Authorization:** Not applicable — no new endpoint or credential is introduced on this path.
- **Input Validation:** `Binding`'s value (however malformed) can never reach the wire via this path, since it is never read here — this AC's test coverage is itself the security control: it fails the build if a future change starts consuming `Binding` on the review path, which would reintroduce a live-resolution dependency mid-review and break reproducibility (the story's Risk table, second row).

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION (a narrow end-to-end pass through `PrepareReview`/`buildSlots`/`renderAgent` with a stubbed LLM transport)
**Test Data Requirements:** An `AgentConfig` fixture with distinct `Model`/`Binding` string values (chosen to be trivially distinguishable in assertion failure output, e.g. `"model-value"` vs `"binding-value"`); a minimal `ReviewConfig` sufficient to exercise `buildSlots`.
**Mock/Stub Requirements:** An `http.RoundTripper`/`HTTPClient` stub (or a package-level guard/counter) that records every outbound request made during the test, asserted to contain only the expected LLM completion call(s) built from `Invocation.Model` — no request to any models/catalog-shaped URL.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Regression test proves `Invocation.Model` derives solely from `AgentConfig.Model`, independent of `AgentConfig.Binding`'s value
- [ ] Regression test proves zero outbound network calls beyond the actual LLM invocation occur during `PrepareReview`/`buildSlots`/`renderAgent`
- [ ] Diff review confirms `internal/registry/persona.go` (`ResolvePersona`) is untouched by this story
- [ ] Fallback-chain construction (`AgentConfig.Fallback`) is confirmed unaffected by `Binding`

**Manual Review:**
- [ ] Code reviewed and approved
