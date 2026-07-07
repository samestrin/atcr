# Acceptance Criteria: Fixture Test Asserts Bound Model in Structured Metadata

**Related User Story:** [06: Authoring Contract Enforcement for Model Metadata and Human Names](../user-stories/06-authoring-contract-enforcement.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/personas/test.go`) | Extends `TemplateFixtureRunner.RunFixture` / `FixtureOutcome` |
| Test Framework | Go `testing` package, table-driven tests | No LLM call, no network — matches existing fixture-test philosophy |
| Key Dependencies | `gopkg.in/yaml.v3` (persona YAML decode), existing `builtins` package, `internal/registry` (`AgentConfig`-shaped `Provider`/`Model` fields) | No new external dependency |

## Related Files
- `internal/personas/test.go` - modify: `TemplateFixtureRunner.RunFixture` gains a bound-model-metadata assertion that runs for every persona it resolves (built-in via `builtins.Get`/`isBuiltin`, and community via YAML decode of the installed persona file), without altering the existing `isBuiltin(name)` branch's pass/fail semantics for template-render fixture checks
- `internal/personas/test_test.go` - create: table-driven tests covering a built-in persona (model always present via embedded metadata), a community persona YAML with a populated `model` field (assertion passes), and a community persona YAML with an empty/missing `model` field (assertion fails with a clear, attributable error)
- `internal/personas/list.go` - reference only: `personaFileMeta` currently decodes only `Version`/`Language` from a community YAML — this AC's community-model lookup either extends this struct or adds a sibling decode step in `test.go`, without changing `List`'s existing output shape
- `personas/testdata/` - reference only: no new fixture `.patch` files required by this AC (the assertion is on structured metadata already loaded via the persona YAML, not on the diff fixture content)

## Happy Path Scenarios
**Scenario 1: Built-in persona always passes the model-metadata assertion**
- **Given** a built-in persona name resolved via `isBuiltin(name)` (e.g. `bruce`)
- **When** `TemplateFixtureRunner.RunFixture("bruce")` runs
- **Then** the existing template-render fixture check still executes unchanged, and the new model-metadata assertion also passes (built-in personas' bound model is known via their registry agent configuration), with no regression to the existing `FixtureOutcome{HasFixture: true, Passed: 1, Total: 1}` result shape for a passing fixture

**Scenario 2: Community persona with a populated `model` field passes the assertion**
- **Given** a community persona YAML installed under the personas directory with `provider: openrouter` and `model: anthropic/claude-3.7-sonnet`
- **When** `TemplateFixtureRunner.RunFixture("<community-persona-name>")` runs
- **Then** the model-metadata assertion passes (the persona's parsed `Model` field is non-empty), and `go test ./...` is green with this assertion active

**Scenario 3: `go test ./...` passes end-to-end with the extended assertion active**
- **Given** the full existing personas test suite plus the new bound-model assertion
- **When** `go test ./...` runs
- **Then** all tests pass, confirming the new assertion is compatible with every currently-shipped built-in and community persona fixture

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
- **Response Time:** Negligible overhead — one additional YAML field check (already-parsed struct field access, or at most one additional `os.ReadFile` + `yaml.Unmarshal` of an already-installed persona file) per `RunFixture` call; no measurable regression versus the current template-render-only path.
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
- [ ] `TemplateFixtureRunner.RunFixture` asserts the bound `model` is present in structured metadata for every persona it resolves (built-in and community)
- [ ] The existing `isBuiltin(name)` branch's template-render pass/fail semantics are unchanged
- [ ] A persona with a missing/blank `model` field fails the fixture assertion with a clear, attributable error
- [ ] `go test ./...` passes with the extended assertion active and performs no network access

**Manual Review:**
- [ ] Code reviewed and approved
