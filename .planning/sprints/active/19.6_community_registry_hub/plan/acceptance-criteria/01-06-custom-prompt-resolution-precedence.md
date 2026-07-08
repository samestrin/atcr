# Acceptance Criteria: Custom Community Prompt Resolves via a Single Precedence Chain

**Related User Story:** [01: Community-Canonical Fetch-and-Pin Distribution](../user-stories/01-community-canonical-fetch-and-pin-distribution.md)

> Locks Clarifications **C1/C2/C3** (original-requirements.md, 2026-07-07): a community persona may carry a custom Markdown prompt that MUST resolve at review time through **one** deterministic precedence chain; the prompt travels with the persona as a single self-contained unit (no second delivery path); a fetched prompt is untrusted input.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go persona resolution + install path | **extend the EXISTING resolver** `internal/registry.ResolvePersona` (`internal/registry/persona.go:46`, called at review time from `internal/fanout/review.go:999`) — do NOT author a second resolver in `internal/personas`; `internal/personas` owns install only |
| Test Framework | Go `testing` + `httptest.NewServer` mock registry | existing `ATCR_PERSONAS_URL` override pattern |
| Key Dependencies | fetch-and-pin (AC 01-02), index schema (AC 02-01) | reuses the `MaxExecutorSystemPromptLen` cap pattern (`internal/registry/config.go`) |

## Related Files
- `internal/registry/persona.go:46` (`ResolvePersona`, the existing review-time chain resolving `PersonaDirs{Project, Registry}` at `internal/fanout/review.go:150-162`) - modify: **extend** this single chain so the pinned-community source resolves a self-contained community unit; do not create a parallel resolver
- `internal/personas/paths.go` (`PersonasDir()`) - modify: reconcile the community install dir with the resolver's `Registry` personas dir (`filepath.Dir(DefaultRegistryPath())/personas` = `~/.config/atcr/personas`). Today `PersonasDir()` returns `os.UserConfigDir()/atcr/personas`, which on darwin is `~/Library/Application Support/atcr/personas` — a **different** directory, so a fetched persona would never land on the resolution chain. They MUST be the same directory.
- `internal/personas/` install path - modify: deliver the persona's prompt as **a co-located `<name>.md` file installed atomically with the `<name>.yaml`** (the C2 self-contained unit; single prompt format — `.md` — shared with embedded built-ins, no second delivery mechanism, no inline-YAML prompt-extraction path)
- `internal/registry/config.go` - reference: `MaxExecutorSystemPromptLen` (=4096, `config.go:83`) cap to mirror for the fetched reviewer prompt
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

**Edge Case 3: Installed community persona is on the resolution chain (dir reconciliation)**
- **Given** a community persona installed via fetch-and-pin on macOS (`darwin`)
- **When** `atcr review` resolves it
- **Then** it is found — the install dir (`PersonasDir()`) and the resolver's `Registry` personas dir are the **same** directory (`~/.config/atcr/personas`); a cross-platform test on darwin asserts install-dir == resolve-dir

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

**Error Scenario 3: Template metacharacters in an untrusted fetched prompt are not expanded**
- **Given** a fetched persona whose prompt text contains template directives (e.g. `{{.Payload}}`, `{{ range }}`) authored by the untrusted remote
- **When** the prompt is rendered through the standard template pipeline
- **Then** those directives in the fetched body are treated as literal text (escaped) or the persona is rejected at load — the fetched prompt is never allowed to drive template expansion or reach unintended variables (C3 injection guardrail)
- Error message: descriptive load-time rejection, or provably-literal rendering; a fixture asserts a `{{ }}`-bearing fetched prompt does not expand

## Performance Requirements
- **Response Time:** Resolution is local (in-memory over already-installed content); no added network calls beyond the existing fetch/pin.
- **Throughput:** N/A (single-user CLI invocation).

## Security Considerations
- **Input Validation:** A fetched prompt is untrusted input becoming a system prompt — length-capped (mirroring `MaxExecutorSystemPromptLen`=4096), fixture-gated, pinned, and its template metacharacters (`{{ }}`) not expanded (Error Scenario 3); never resolved unvalidated. This is the C3 prompt-injection guardrail.
- **Transport:** The default/live fetch is HTTPS-only (the `RegistryBaseURL` constant is `https://`); an `ATCR_PERSONAS_URL` override may use `http` only for a local/mock registry in tests.
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
- [ ] A community persona's custom Markdown prompt resolves at review time via the **extended** `internal/registry.ResolvePersona` (one resolver, not a second one in `internal/personas`)
- [ ] The community install dir == the resolver's `Registry` personas dir (`~/.config/atcr/personas`), verified on darwin
- [ ] Resolution uses ONE documented precedence chain; collisions are deterministic
- [ ] The prompt travels with the persona as one self-contained unit — a co-located `<name>.md` installed atomically with `<name>.yaml`; no second delivery path (C2)
- [ ] A fetched prompt is length-capped, fixture-gated, and `{{ }}`-safe before it can resolve (C3)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Precedence ordering **LOCKED**: project `.atcr/personas` override > pinned community (`~/.config/atcr/personas`) > embedded built-in (per C2; matches the existing `PersonaDirs{Project, Registry}` order in `internal/fanout/review.go:150-162`)
