# Acceptance Criteria: `index.json` Registration and Full Test-Gate Pass

**Related User Story:** [2: Fixture Authoring & Test-Gate Integration](../user-stories/02-fixture-authoring-test-gate-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | JSON manifest entry (`PersonaIndexEntry`) + CLI structural check | `personas/community/index.json` is a JSON array; `atcr personas test simon` is a real CLI subcommand (`cmd/atcr/personas.go`) |
| Test Framework | `go test` + `testify/require`/`assert` | `personas/community_test.go`, `internal/personas/search_test.go`, `internal/registry/persona_test.go`, `internal/personas/community_schema_test.go` |
| Key Dependencies | `encoding/json`, `gopkg.in/yaml.v3` | `TestCommunityIndex_Registration` parses `index.json` and cross-checks against the persona's YAML |

## Related Files
- `personas/community/index.json` - modify: append a `simon` `PersonaIndexEntry` (`name`, `version`, `description`, `path`, `provider`, `model`, `tasks`, `tags`) matching the array shape of the existing 13 entries (e.g. `anthony`)
- `personas/community_test.go` - consumer: `TestCommunityIndex_Registration` (line ~332), `TestCommunityPersonas_DistinctTaskScoping` (line ~312)
- `internal/personas/search_test.go` - consumer: `verifyCommunityIndex` (line ~30) re-asserts provider/model non-empty and equal to `simon.yaml`, independent of the roster
- `cmd/atcr/personas_test.go` - consumer: exercises the `personas test <slug>` subcommand path (line ~643 pattern) that `atcr personas test simon` invokes manually

### Related Files (from codebase-discovery.json)
- `personas/community/index.json` - modify (`files_to_modify`, minor scope): append simon's `PersonaIndexEntry`; provider/model/description must match simon.yaml exactly, `path` must be exactly `"simon.yaml"`, and `tasks[0]` must be distinct from every existing entry's `tasks[0]`
- `personas/community_test.go:332` - consumer (`related_files`, high relevance): `TestCommunityIndex_Registration` asserts provider/model/description byte-match the source YAML and restricts provider to the sanctioned routing keys `openrouter`/`local` (provider=local requires a `local/` model prefix; any non-local provider forbids it)
- `internal/personas/search_test.go:30` - consumer (`related_files`, medium relevance): `verifyCommunityIndex` re-asserts provider/model equality per entry against the real repo index, independent of the roster
- `internal/personas/test.go` - reference only (`related_files`, high relevance): `TemplateFixtureRunner.RunFixture` powers the manual `atcr personas test simon` no-LLM proof (exactly one passing case expected)

## Happy Path Scenarios
**Scenario 1: index.json entry mirrors simon.yaml exactly**
- **Given** `personas/community/simon.yaml` has `name: simon`, a `provider` (either `openrouter` or `local`), a `model`, and a `description`
- **When** a matching `index.json` array entry is added with `"name": "simon"`, `"path": "simon.yaml"`, and `"provider"`/`"model"`/`"description"` copied byte-for-byte from `simon.yaml`
- **Then** `TestCommunityIndex_Registration`'s per-persona subtest for `simon` passes all of: `require.Equalf(t, p.Slug, e.Name, ...)`, `require.Equalf(t, p.Slug+".yaml", e.Path, ...)`, `require.Equalf(t, ym.Provider, e.Provider, ...)`, `require.Equalf(t, ym.Model, e.Model, ...)`, `require.Equalf(t, ym.Description, e.Description, ...)`

**Scenario 2: tasks[0] is a fresh, unclaimed primary task tag**
- **Given** the 13 already-claimed primary task tags (architecture-review, correctness-review, api-review, validation-review, concurrency-review, resource-review, performance-review, type-safety-review, dependency-review, observability-review, secrets-review, duplication-review, invariant-review)
- **When** `simon`'s `index.json` entry sets `"tasks": ["bloat-review", ...]` (or another fresh name such as `slop-review`) as `tasks[0]`, with non-empty `tags`
- **Then** `TestCommunityPersonas_DistinctTaskScoping` (line ~312) adds `simon` to the `seen` map without triggering `t.Fatalf("personas %q and %q share primary task %q ...")`, and `require.NotEmptyf(t, e.Tasks, ...)` passes

**Scenario 3: Full test-gate suite passes green**
- **Given** AC 02-01 (fixture), AC 02-02 (roster), and this AC (index.json) are all complete
- **When** `go test ./personas/... ./internal/personas/... ./internal/registry/...` is run
- **Then** the command exits 0 with no failing tests, including `TestCommunityAccessors`, `TestCommunityPersonas_FixtureAndPromptCategory`, `TestCommunityPersonas_SlugConsistency`, `TestCommunityPersonas_Differentiation`, `TestCommunityPersonas_DistinctCategories`, `TestCommunityPersonas_DistinctTaskScoping`, `TestCommunityIndex_Registration`, `TestTemplateFixtureRunner_CommunityPersonasPass`, `TestCommunityPersonas_StrictSchema`, `TestCommunityPersonas_NoPlaceholderModel`, `TestCommunityPersonas_HumanNames`, and `verifyCommunityIndex`-backed tests in `internal/personas/search_test.go`

**Scenario 4: Manual no-LLM structural proof succeeds**
- **Given** `simon` is fully registered and its fixture exists
- **When** an operator runs `atcr personas test simon` from the CLI
- **Then** the command succeeds (exit 0) and reports a passing fixture case, using only local structural checks (`TemplateFixtureRunner.RunFixture`) with no LLM/network call — the same no-LLM proof path used by every other community persona (`cmd/atcr/personas.go`'s `personasFixtureRunner`)

## Edge Cases
**Edge Case 1: provider requires a `local/` model prefix when provider is `local`**
- **Given** `simon.yaml` sets `provider: local`
- **When** the `index.json` entry's `model` field is checked
- **Then** the model string must carry a `local/` prefix (per `TestCommunityIndex_Registration`'s provider-restricted rule); if `provider` is instead `openrouter`, the `local/` prefix must be absent — a provider/model prefix mismatch fails the registration cross-check

**Edge Case 2: index.json entry count drifts from roster count**
- **Given** the roster (`communityPersonas`, AC 02-02) has exactly 14 entries after `simon` is added
- **When** `TestCommunityIndex_Registration` builds `byStem` from `index.json` and checks `require.Lenf(t, byStem, len(communityPersonas), ...)`
- **Then** `index.json` must also contain exactly 14 entries (one per persona, keyed by path stem) — adding only the roster row without the matching `index.json` entry (or vice versa) fails this length check

**Edge Case 3: tags left empty**
- **Given** the `PersonaIndexEntry.Tags` field
- **When** `simon`'s entry is authored with an empty `"tags": []`
- **Then** this deviates from the Edge Case 2 requirement noted in the `TestCommunityIndex_Registration` doc comment ("tasks/tags populated") — `tags` must be non-empty, mirroring every existing entry (e.g. `anthony`'s `["architecture", "coupling", "claude", "frontier-vendor"]`)

## Error Conditions
**Error Scenario 1: index.json entry missing for simon**
- Error message: `"persona %q has no index entry"` from `TestCommunityIndex_Registration` (`require.Truef(t, ok, ...)`)
- HTTP status / error code: N/A (Go test failure)

**Error Scenario 2: provider/model/description drift from simon.yaml**
- Error message: `"provider drift for %q"`, `"model drift for %q"`, or `"description drift for %q"` from `TestCommunityIndex_Registration` (line ~367-369)
- HTTP status / error code: N/A (Go test failure)

**Error Scenario 3: tasks[0] collides with a claimed tag**
- Error message: `"personas %q and %q share primary task %q — lenses must be differentiated"` from `TestCommunityPersonas_DistinctTaskScoping` (`t.Fatalf`)
- HTTP status / error code: N/A (Go test failure)

**Error Scenario 4: `atcr personas test simon` fails structurally**
- Error message: `"No fixture defined for persona"` (if AC 02-01's fixture is missing) or a non-nil error surfaced by `personasFixtureRunner`/`RunFixture` (`cmd/atcr/personas.go`)
- HTTP status / error code: N/A (CLI non-zero exit code)

## Performance Requirements
- **Response Time:** `go test ./personas/... ./internal/personas/... ./internal/registry/...` must complete within existing CI test-suite time budgets — no new slow I/O or network calls are introduced (all checks are local file reads/JSON/YAML parses)
- **Throughput:** `atcr personas test simon` must complete near-instantly (no LLM round trip) since `RunFixture` is a purely local, no-LLM structural check

## Security Considerations
- **Authentication/Authorization:** N/A — `index.json` is a static in-repo manifest, not a runtime-writable file; no auth surface
- **Input Validation:** `verifyCommunityIndex` (`internal/personas/search_test.go:30`) defends against path traversal by rejecting an absolute `Path` or a `..`-escaping join relative to `personasRoot` — `simon`'s `path` field must be the plain relative filename `simon.yaml`, matching every other entry, so it cannot trip this guard

## Test Implementation Guidance
**Test Type:** INTEGRATION (cross-file consistency checks spanning `index.json`, `simon.yaml`, and the Go roster) plus one manual E2E CLI smoke check
**Test Data Requirements:** `simon.yaml`/`simon.md` (Story 1), `simon_fixture.patch` (AC 02-01), the `communityPersonas` roster row (AC 02-02) must all exist before this AC's tests can pass — this is intentionally the last integration point per the story's "atomic change set" implementation note
**Mock/Stub Requirements:** None — all checks run against real committed files with no LLM or network mocking required

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `personas/community/index.json` contains exactly one `simon` entry with `name`/`path`/`provider`/`model`/`description` byte-matching `simon.yaml`, and non-empty `tasks`/`tags`
- [ ] `tasks[0]` is a fresh primary task tag not among the 13 already-claimed values (e.g. `bloat-review`)
- [ ] `go test ./personas/... ./internal/personas/... ./internal/registry/...` passes with zero failures
- [ ] `atcr personas test simon` succeeds manually as a no-LLM structural proof

**Manual Review:**
- [ ] Code reviewed and approved
