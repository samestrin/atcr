# Acceptance Criteria: Custom Community Prompt Resolves via a Single Precedence Chain

**Related User Story:** [01: Community-Canonical Fetch-and-Pin Distribution](../user-stories/01-community-canonical-fetch-and-pin-distribution.md)

> Locks Clarifications **C1/C2/C3** (original-requirements.md, 2026-07-07): a community persona may carry a custom Markdown prompt that MUST resolve at review time through **one** deterministic precedence chain; the prompt travels with the persona as a single self-contained unit (no second delivery path); a fetched prompt is untrusted input.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go persona resolution + install path | the resolver `atcr review` uses to turn an agent's `Persona` ref into prompt text; `internal/personas` install |
| Test Framework | Go `testing` + `httptest.NewServer` mock registry | existing `ATCR_PERSONAS_URL` override pattern |
| Key Dependencies | fetch-and-pin (AC 01-02), index schema (AC 02-01) | reuses the `MaxExecutorSystemPromptLen` cap pattern (`internal/registry/config.go`) |

## Related Files
- persona resolution chain (`ResolvePersona` / the review-time template resolver) - modify: add the pinned-community source into a single documented precedence chain
- `internal/personas/` install path - modify: deliver the persona's prompt as part of the self-contained unit (inline YAML field or a co-located file installed atomically with the YAML) — no second delivery mechanism
- `internal/registry/config.go` - reference: `MaxExecutorSystemPromptLen` cap to mirror for the fetched reviewer prompt
- [AC 01-02: init/quickstart Fetch-and-Pin](01-02-init-quickstart-fetch-and-pin.md) - the pin this AC resolves against

## Happy Path Scenarios
**Scenario 1: A community persona with a custom prompt resolves at review time**
- **Given** a community persona installed with its own Markdown reviewer prompt (not a reference to a built-in)
- **When** `atcr review` resolves that persona
- **Then** the persona's own custom prompt text is used, rendered through the standard template pipeline (variables filled, no leftover `{{ }}`)

**Scenario 2: Single, deterministic precedence chain**
- **Given** the same persona name present as an embedded built-in, an installed community persona, and a project `.atcr/personas` override
- **When** resolution runs
- **Then** exactly one source wins per the documented order (recommended: project override > pinned community > embedded built-in), deterministically, with no ambiguity or double-load

## Edge Cases
**Edge Case 1: Binding-only community persona still works**
- **Given** a community persona that sets `provider`/`model` and references an existing built-in template (carries no custom prompt)
- **When** it resolves
- **Then** the referenced template resolves normally — binding-only remains *valid*, it is just no longer *required* (C1)

**Edge Case 2: Name collision resolves via precedence, never errors ambiguously**
- **Given** a community persona and a built-in with the same name
- **When** resolution runs
- **Then** the precedence chain selects one deterministically — no panic, no ambiguous double-load

## Error Conditions
**Error Scenario 1: Oversized custom prompt rejected (untrusted-input cap)**
- **Given** a fetched persona whose prompt exceeds the length cap (mirroring `MaxExecutorSystemPromptLen`)
- **When** it is installed/loaded
- **Then** it is rejected at load with a descriptive error — not silently truncated, not executed
- Error message: descriptive "persona prompt exceeds maximum length" (or equivalent load-time validation error)

**Error Scenario 2: Custom prompt failing its fixture cannot ship or resolve**
- **Given** a custom prompt that does not pass its render/category fixture
- **When** the fixture gate runs
- **Then** the persona is treated as invalid (HARD gate), consistent with the library's per-persona fixture requirement (C3)

## Performance Requirements
- **Response Time:** Resolution is local (in-memory over already-installed content); no added network calls beyond the existing fetch/pin.
- **Throughput:** N/A (single-user CLI invocation).

## Security Considerations
- **Input Validation:** A fetched prompt is untrusted input becoming a system prompt — length-capped, fixture-gated, and pinned; never resolved unvalidated. This is the C3 prompt-injection guardrail.
- **Authentication/Authorization:** N/A — read-only local resolution over already-installed content.

## Test Implementation Guidance
**Test Type:** UNIT (precedence ordering + length cap) + INTEGRATION (install → resolve → review-render against a mock registry)
**Test Data Requirements:** Mock personas covering: (a) a custom-prompt persona, (b) a binding-only persona, (c) a persona whose name collides with a built-in, (d) a persona whose prompt exceeds the cap.
**Mock/Stub Requirements:** `httptest.NewServer` serving mock persona units; `ATCR_PERSONAS_URL` override per the existing test pattern.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A community persona's custom Markdown prompt resolves at review time via `ResolvePersona`
- [ ] Resolution uses ONE documented precedence chain; collisions are deterministic
- [ ] The prompt travels with the persona as one self-contained unit — no second delivery path (C2)
- [ ] A fetched prompt is length-capped and fixture-gated before it can resolve (C3)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Exact precedence ordering confirmed at `/design-sprint` (the one remaining C2 sub-decision)
