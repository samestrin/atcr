# Acceptance Criteria: Fixture Test Asserts Bound Model in Structured Metadata

**Related User Story:** [06: Authoring Contract Enforcement for Model Metadata and Human Names](../user-stories/06-authoring-contract-enforcement.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md), [testing-mock-registry.md](../documentation/testing-mock-registry.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/personas/test.go`) | Extends `TemplateFixtureRunner.RunFixture` / `FixtureOutcome` |
| Test Framework | Go `testing` package, table-driven tests | No LLM call, no network — matches existing fixture-test philosophy |
| Key Dependencies | `gopkg.in/yaml.v3` (persona YAML decode), existing `builtins` package, `internal/registry` (`AgentConfig`-shaped `Provider`/`Model` fields) | No new external dependency |

### Related Files (from codebase-discovery.json)
- `internal/personas/test.go` (`TemplateFixtureRunner.RunFixture`, `FixtureOutcome`) — modify: add a bound-model-metadata assertion scoped to COMMUNITY/LIBRARY personas ONLY (the non-`isBuiltin` / `HasFixture:false` community path). Embedded built-ins are EXEMPT and the assertion is SKIPPED for them (they are model-agnostic per C2 — `personas/personas.go` embeds only `.md`, carries no `provider`/`model`, and `internal/personas/test.go` resolves them with no agent/registry config in scope). The existing `isBuiltin(name)` branch's pass/fail semantics are unaltered.
- `internal/personas/test_test.go` — create: table-driven tests covering (1) a built-in persona where the model-metadata assertion is SKIPPED (not asserted to "pass" — built-ins carry no model), (2) a community persona YAML with a populated `model` field (assertion passes), and (3) a community persona YAML with an empty/missing `model` field (assertion fails).
- `internal/personas/list.go` (`personaFileMeta`, lines 38-47, 117-172) — reference: existing metadata decode struct; this AC's community-model lookup either extends it or adds a sibling decode step without changing `List`'s output shape.
- `docs/personas-authoring.md` — reference: authoring contract that requires `provider`/`model`.


## Happy Path Scenarios
**Scenario 1: Built-in persona is EXEMPT from the model-metadata assertion (assertion skipped, not "passed")**
- **Given** a built-in persona name resolved via `isBuiltin(name)` (e.g. `bruce`)
- **When** `TemplateFixtureRunner.RunFixture("bruce")` runs
- **Then** the existing template-render fixture check still executes unchanged, and the new model-metadata assertion is SKIPPED for the built-in — NOT asserted to pass. Embedded built-ins are model-agnostic (C2): `personas/personas.go` embeds only the `.md`, they carry no `provider`/`model`, and `internal/personas/test.go` has no agent/registry configuration in scope from which to derive a bound model. The earlier framing ("built-in personas' bound model is known via their registry agent configuration") was false and is removed — the runner cannot and does not read registry agent config, so requiring a bound model for built-ins would be unimplementable. The `FixtureOutcome{HasFixture: true, Passed: 1, Total: 1}` result shape for a passing built-in fixture is unchanged

**Scenario 2: Community persona with a populated `model` field passes the assertion**
- **Given** a community persona YAML installed under the personas directory with `provider: openrouter` and `model: anthropic/claude-3.7-sonnet`
- **When** `TemplateFixtureRunner.RunFixture("<community-persona-name>")` runs
- **Then** the model-metadata assertion passes (the persona's parsed `Model` field is non-empty), and `go test ./...` is green with this assertion active

**Scenario 3: `go test ./...` passes end-to-end with the extended assertion active**
- **Given** the full existing personas test suite plus the new bound-model assertion
- **When** `go test ./...` runs
- **Then** all tests pass, confirming the new assertion is compatible with every currently-shipped built-in and community persona fixture

**Scenario 4: Loading state — the model-metadata assertion completes without perceptible delay**
- **Given** a community persona YAML with a populated `model` field
- **When** `TemplateFixtureRunner.RunFixture` executes the model-metadata check
- **Then** the check completes in less than 10 milliseconds per persona, with no network or LLM call performed

## Edge Cases
**Edge Case 1: Community persona resolved for the first time (previous `HasFixture: false` short-circuit)**
- **Given** a community persona name that, prior to this story, caused `RunFixture` to return `FixtureOutcome{HasFixture: false}` immediately
- **When** the extended runner processes the same name after this story
- **Then** the model-metadata assertion is applied as an additive check (per the story's implementation guidance: "add the model-metadata assertion... as a new lightweight check that runs for community personas where `HasFixture` currently short-circuits to `false`"), and the outcome reflects whether the persona's `model` field is present, not a blanket `HasFixture: false`

**Edge Case 2: Built-in fixture path is untouched by the new check**
- **Given** the existing `isBuiltin(name)` branch's template-render logic (`payload.RenderPrompt` + `strings.Contains(out, "{{")`)
- **When** the model-metadata assertion is added
- **Then** the built-in branch's existing pass/fail logic and `FixtureOutcome` values for a passing/failing template render are unchanged byte-for-byte — the new assertion is additive, not a replacement, per the story's risk mitigation ("keep the `isBuiltin(name)` branch fully separate")

**Edge Case 3: Assertion is purely structural — no network or LLM call**
- **Given** the story's constraint that the new assertion must be enforceable with no network access
- **When** the model-metadata check runs
- **Then** it only inspects the already-parsed persona struct's `Model` field (present/non-empty) and performs no HTTP call, no provider API lookup, and no verification that the model id is a real, currently-served model

## Error Conditions
**Error Scenario 1: Persona YAML with a missing or blank `model` field fails the fixture assertion**
- **Given** a persona YAML that (hypothetically, bypassing the existing strict-decode required-field validation at install time) has an empty `model:` value reaching the fixture runner
- **When** `RunFixture` runs against that persona
- **Then** the fixture assertion fails with a clear, attributable error identifying the persona name and the missing field, e.g. `fmt.Errorf("persona %q: bound model missing from structured metadata", name)`, distinct from the existing template-unrendered-action failure path
- HTTP status / error code: N/A (Go test/CLI exit-code failure, not an HTTP path — `atcr personas test <name>` should exit non-zero)

**Error Scenario 2: Persona name resolves to neither a built-in nor an installed community persona**
- **Given** a name that is not `isBuiltin` and has no installed YAML under the personas directory
- **When** `TestPersona`/`RunFixture` is invoked
- **Then** the existing not-found error path (per `TestPersona`'s doc comment: "It errors if the persona is neither a built-in nor installed") is preserved unchanged — this AC does not alter resolution-failure behavior, only the bound-model assertion for personas that do resolve

## Performance Requirements
- **Response Time:** Negligible overhead — one additional YAML field check (already-parsed struct field access, or at most one additional `os.ReadFile` + `yaml.Unmarshal` of an already-installed persona file) per `RunFixture` call; no measurable regression versus baseline (≤1% wall-time difference in `go test ./...`) versus the current template-render-only path.
- **Throughput:** N/A (single-persona CLI/test invocation, not a service).

## Security Considerations
- **Authentication/Authorization:** N/A — reads already-installed local persona files; no new trust boundary introduced.
- **Input Validation:** The `model` field is treated as an opaque non-empty string for this assertion (existence/non-emptiness only); no execution, interpolation, or network dispatch of the field value, consistent with the existing no-network fixture principle.

## Test Implementation Guidance
**Test Type:** UNIT (`internal/personas/test_test.go`, no LLM/network)
**Test Data Requirements:** A built-in persona name (e.g. `bruce`) resolved via the embedded `builtins` package; an in-memory or `t.TempDir()`-backed community persona YAML with `provider`/`model` populated; a second community persona YAML with `model: ""` or the key omitted, to exercise the failure path
**Mock/Stub Requirements:** None beyond `t.TempDir()` for the community-persona fixture files — no HTTP mocking, no LLM stubs, consistent with the existing `TemplateFixtureRunner` design (`FixtureRunner` interface remains injectable for CLI-level tests that want to avoid touching the filesystem at all)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `TemplateFixtureRunner.RunFixture` asserts the bound `model` is present in structured metadata for COMMUNITY/LIBRARY personas it resolves; embedded model-agnostic built-ins are EXEMPT (the assertion is skipped for them, not asserted to pass) — aligns with AC7's library-persona intent
- [ ] This assertion is the AC7 enforcement gate referenced by AC 02-02 (the authoring contract's model-in-structured-metadata convention)
- [ ] The existing `isBuiltin(name)` branch's template-render pass/fail semantics are unchanged
- [ ] A persona with a missing/blank `model` field fails the fixture assertion with a clear, attributable error
- [ ] `go test ./...` passes with the extended assertion active and performs no network access

**Manual Review:**
- [ ] Code reviewed and approved
