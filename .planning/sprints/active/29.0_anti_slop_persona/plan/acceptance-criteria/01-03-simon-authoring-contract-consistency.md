# Acceptance Criteria: `simon` Unit is Self-Consistent with the Authoring Contract and Auto-Discovered by the Registry Test Suite

**Related User Story:** [1: Author the `simon` Persona Unit](../user-stories/01-author-the-simon-persona-unit.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cross-file consistency (no new code) | `simon.yaml` + `simon.md` as a matched pair, discovered via `go:embed` in `personas/community.go` |
| Test Framework | `go test` across `internal/personas/...` and `internal/registry/...` | existing table-driven suites iterate `personas.CommunityNames()` — no test-file edits in this story |
| Key Dependencies | `personas.CommunityNames()`, `personas.CommunityGet()`, `personas.CommunityModel()`, `personas.CommunityFixture()` (`personas/community.go`) | `CommunityFixture` is the one call this story's files do NOT yet satisfy (fixture lands in Story 2) |

## Related Files
- `personas/community/simon.yaml` - reference (already created by AC 01-01): must stay in lockstep with `simon.md`'s prompt content and its own `provider`/`model` fields
- `personas/community/simon.md` - reference (already created by AC 01-02): must stay in lockstep with `simon.yaml`'s `persona: simon` binding
- `internal/personas/community_fixture_test.go` - test (unmodified): `TestTemplateFixtureRunner_CommunityPersonasPass` — **documented, expected gap**: this test iterates `builtins.CommunityNames()`, which now includes `simon`, but `personas/community/testdata/simon_fixture.patch` does not exist until Story 2; this subtest is expected to FAIL until Story 2 lands, and that gap is explicitly out of this AC's scope to close
- `internal/personas/test_test.go` - test (unmodified): `TestRunFixture_CommunityAssertsBoundModel` — same expected-fail gap as above for the same fixture-dependency reason; the bound-model half of this test (non-empty `model` in structured metadata) is already satisfiable by AC 01-01's `simon.yaml`
- `internal/personas/community_schema_test.go` - test (unmodified): `TestCommunityPersonas_StrictSchema`, `TestCommunityPersonas_NoPlaceholderModel`, `TestCommunityPersonas_HumanNames`, `TestPinnedModelIsLockZeroMigration` all auto-cover `simon` once the files exist, and are expected to PASS (covered individually by AC 01-01)
- `internal/registry/persona_test.go` - test (unmodified): `TestValidateFetchedPersonaPrompt_AllEmbeddedCommunityPersonasPass` auto-covers `simon` and is expected to PASS (covered individually by AC 01-02)
- `docs/personas-authoring.md` - reference only: the full authoring contract this AC verifies `simon` against end-to-end (not modified by this story)

### Related Files (from codebase-discovery.json)
- `personas/community/simon.yaml` - reference (`files_to_create`, created by AC 01-01): agent binding half of the matched pair
- `personas/community/simon.md` - reference (`files_to_create`, created by AC 01-02): prompt half of the matched pair
- `personas/community.go:38` - reference only (`related_files`, medium relevance): go:embed accessors (`CommunityNames`/`CommunityGet`/`CommunityFixture`/`CommunityModel`) that auto-discover simon with no roster or index edit
- `internal/personas/community_fixture_test.go` - test, reference only (`related_files`, high relevance): embedded-set fixture gate with the documented expected-fail gap until Story 2's fixture lands
- `internal/personas/community_schema_test.go` - test, reference only (`related_files`, high relevance): strict-schema / no-placeholder / human-name gates expected to PASS for simon
- `internal/registry/persona_test.go:305` - test, reference only (`related_files`, medium relevance): fetched-prompt guardrail gate expected to PASS for simon
- `docs/personas-authoring.md` - reference only (`build_from.primary_file`): the end-to-end authoring contract

## Happy Path Scenarios
**Scenario 1: `simon` is auto-discovered with no roster or registration edit**
- **Given** `personas/community/simon.yaml` and `personas/community/simon.md` exist on disk
- **When** the package is built (`go build`/`go test`), `go:embed community/*.yaml` and `go:embed community/*.md` in `personas/community.go` re-embed the directory contents
- **Then** `personas.CommunityNames()` returns a sorted slice that includes `"simon"` with zero code changes to `community.go`, `community_test.go`, or `index.json` (those remain untouched, per this story's explicit non-scope)

**Scenario 2: The non-fixture-dependent registry gates pass for `simon` without a persona-specific carve-out**
- **Given** `simon.yaml`/`simon.md` satisfy AC 01-01 and AC 01-02
- **When** `go test ./internal/personas/... ./internal/registry/...` runs the full suite
- **Then** every table-driven test that does NOT require `CommunityFixture("simon")` passes for the `simon` subtest with no skip, no special-case branch, and no test-file diff in this story

**Scenario 3: `simon.yaml`'s `provider`/`model` and `simon.md`'s prompt content agree with each other**
- **Given** `simon.yaml` declares `persona: simon` (or omits it, defaulting to the agent name `simon`)
- **When** the registry resolves the community persona unit as a matched triple (`personas.CommunityGet("simon")` for the prompt, `personas.CommunityModel("simon")` for the bound model)
- **Then** both resolve successfully and refer to the same logical persona — no orphaned `.yaml` without a matching `.md`, or vice versa

## Edge Cases
**Edge Case 1: Fixture-dependent tests fail predictably, not silently, until Story 2**
- **Given** this story deliberately does not create `personas/community/testdata/simon_fixture.patch`
- **When** `TestTemplateFixtureRunner_CommunityPersonasPass/simon` and `TestRunFixture_CommunityAssertsBoundModel/simon` run
- **Then** both fail with a clear, attributable error from `TemplateFixtureRunner.RunFixture("simon")` (missing fixture), not a panic or a silently-skipped test — this is the expected, documented state of `go test ./...` after Story 1 alone and is resolved by Story 2, not by this AC

**Edge Case 2: `index.json` is untouched, so `simon` is invisible to `atcr personas search` until Story 2**
- **Given** this story explicitly excludes `personas/community/index.json` registration
- **When** a maintainer runs `atcr personas search` or the index-consistency gate described in `docs/personas-authoring.md` §5
- **Then** `simon` does not appear in search results and no index-consistency test references it — this is expected and does not indicate a broken authoring state, since the persona unit and the catalog listing are intentionally decoupled across stories

## Error Conditions
**Error Scenario 1: A `simon.yaml`/`simon.md` mismatch (e.g. `persona:` field pointing at a nonexistent prompt name) would break resolution**
- Error message: `"no embedded community persona %q"` from `personas.CommunityGet` (`personas/community.go:60`) if `persona:` in `simon.yaml` does not match `simon.md`'s filename stem
- HTTP status / error code: N/A (Go `error`, surfaced as a resolution failure or `go test` failure, not an HTTP path)
- This AC's scope is to avoid triggering this error — omit `persona:` (defaults to the agent name) or set it explicitly to `simon`, matching the co-located `simon.md` filename

**Error Scenario 2: Premature fixture-test expectations mislabeled as this story's failure**
- Error message: `TestTemplateFixtureRunner_CommunityPersonasPass/simon` failing with "no embedded community fixture for persona \"simon\"" (from `personas.CommunityFixture`, `personas/community.go:70`)
- This is NOT a defect in this story's deliverable — it is the expected, scoped-out state until Story 2 adds `simon_fixture.patch`; do not attempt to author a fixture file to silence this failure within this story

## Performance Requirements
- **Response Time:** N/A — build-time embed resolution only, no runtime request path introduced by this AC
- **Throughput:** N/A — single persona pair added to an existing embedded set of 13 community personas

## Security Considerations
- **Authentication/Authorization:** N/A — no new auth surface; consistent with the existing community-persona trust model (untrusted Registry tier, guarded by `ValidateFetchedPersonaPrompt` per AC 01-02)
- **Input Validation:** Cross-file consistency (matched `.yaml`/`.md` pair, no orphan) is itself a validation concern — an orphaned half of the pair would either be invisible (unresolvable persona) or fail loudly (missing embed match), never silently misroute a review to the wrong prompt

## Test Implementation Guidance
**Test Type:** INTEGRATION (cross-file, exercises the existing registry/personas test suites as an integration point; no new test code is written by this story)
**Test Data Requirements:** The `simon.yaml` + `simon.md` pair authored by AC 01-01/01-02; explicitly no `simon_fixture.patch` in this story's scope
**Mock/Stub Requirements:** None — all assertions run against the real embedded filesystem (`go:embed`); no LLM or network call in any of the referenced test paths

## Definition of Done
**Auto-Verified:**
- [x] `go test ./internal/personas/... ./internal/registry/...` run in full; the specific `simon` subtests of `TestCommunityPersonas_StrictSchema`, `TestCommunityPersonas_NoPlaceholderModel`, `TestCommunityPersonas_HumanNames`, `TestPinnedModelIsLockZeroMigration`, `TestValidateFetchedPersonaPrompt_AllEmbeddedCommunityPersonasPass` pass
- [x] No linting errors (`gofmt`/`go vet` clean — no Go source is added or modified by this story)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `personas.CommunityNames()` includes `"simon"` with zero edits to `community.go`, `community_test.go`, or `index.json`
- [x] `simon.yaml` and `simon.md` resolve as a matched pair via `CommunityGet("simon")` / `CommunityModel("simon")`
- [x] The two fixture-dependent subtests (`TestTemplateFixtureRunner_CommunityPersonasPass/simon`, `TestRunFixture_CommunityAssertsBoundModel/simon`) are confirmed to fail for the expected reason (missing fixture) and this gap is explicitly logged as deferred to Story 2, not silently ignored

**Manual Review:**
- [ ] Code reviewed and approved
