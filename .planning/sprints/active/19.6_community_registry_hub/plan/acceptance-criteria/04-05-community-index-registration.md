# Acceptance Criteria: `personas/community/index.json` Registration

**Related User Story:** [04: Model-Indexed Persona Library Authoring](../user-stories/04-model-indexed-persona-library-authoring.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | JSON data file + Go test | `personas/community/index.json`, decoded via `internal/personas.PersonaIndexEntry` (Story 2's schema extension) |
| Test Framework | Go `testing` + `encoding/json` | Table-driven decode/consistency assertions |
| Key Dependencies | `internal/personas.PersonaIndexEntry` (Story 2); `encoding/json` (stdlib) | No new external dependency |

### Related Files (from codebase-discovery.json)
- `personas/community/index.json` — create: one entry per authored persona (10+ total) with `name`, `version`, `description`, `path`, `provider`, `model`, and optional `tasks`/`tags` populated.
- `personas/community_test.go` — create: test decoding `index.json` and cross-checking each entry's `provider`/`model` against the corresponding persona YAML.
- `internal/personas/search.go` (`PersonaIndexEntry`) — reference: the struct that `index.json` entries decode into.
- `personas/community/<slug>.yaml` — reference: source of truth for `provider`/`model` values that must match the index.


## Happy Path Scenarios
**Scenario 1: Every authored persona has exactly one `index.json` entry**
- **Given** the 10 authored community personas (6 frontier + 4 flat-rate)
- **When** `personas/community/index.json` is decoded into `[]PersonaIndexEntry`
- **Then** the resulting slice has exactly one entry per persona, with `path` pointing at that persona's YAML file

**Scenario 2: `provider`/`model` in the index match the persona's own YAML**
- **Given** an `index.json` entry for a given persona
- **When** its `Provider`/`Model` fields are compared against the same fields parsed from that persona's YAML at `path`
- **Then** the values are identical strings — no drift between the discovery index and the source of truth

**Scenario 3: Index entries are discoverable by structured field, not only free text**
- **Given** the completed `index.json`
- **When** a consumer (e.g. AC3's `--model`/`--provider` search) filters entries by `Provider`/`Model`
- **Then** every authored persona is reachable via at least one structured `provider`/`model` filter value

**Scenario 4: Empty `index.json` array is rejected by the consistency test**
- **Given** a `personas/community/index.json` containing `[]`
- **When** the consistency test runs
- **Then** the test fails with a clear message stating the index contains no entries, preventing a shipped empty index

**Scenario 5: Slug is consistent across index `name`/`path`, the `.md` prompt, and the resolvable persona name**
- **Given** an in-repo `personas/community/index.json` entry (Q4) and its persona files
- **When** the consistency test derives the slug from the entry's `name` and the stem of its `path`, and cross-checks the co-located `<slug>.md` and the name `ResolvePersona` looks up
- **Then** all agree on one slug string: `name` == stem(`path`) == `<slug>.md` basename == the `<persona>.md` `ResolvePersona` resolves (`internal/registry/persona.go:64`), and the slug passes `validateName` (`internal/registry/persona.go:111`). This guarantees `ResolvePersona` (which looks up `<persona>.md`) never receives an index entry whose registered name cannot resolve to its prompt file.

## Edge Cases
**Edge Case 1: `path` value resolves to a real file**
- **Given** an `index.json` entry's `path` field
- **When** the referenced file is resolved relative to `personas/community/`
- **Then** the file exists on disk (the persona YAML is actually committed, not just registered)

**Edge Case 2: Optional `tasks`/`tags` fields are populated where warranted**
- **Given** a persona task-scoped to a specific review lens (e.g. architecture/logic review for a reasoning-heavy model)
- **When** its `index.json` entry is inspected
- **Then** `tasks` and/or `tags` reflect that scoping (e.g. `["architecture-review"]`) so structured search (Story 3) can surface it beyond a bare provider/model match

## Error Conditions
**Error Scenario 1: `index.json` entry references a persona not on disk**
- **Given** an `index.json` entry whose `path` does not resolve to a committed YAML file
- **When** the consistency test runs
- **Then** the test fails, naming the offending entry and its missing path

**Error Scenario 2: `provider`/`model` drift between index and YAML**
- **Given** an `index.json` entry with `model: "claude-opus-4"` while the corresponding YAML has `model: "claude-opus-4-1"`
- **When** the consistency test compares the two
- **Then** the test fails, identifying the mismatched persona and both conflicting values

**Error Scenario 3: Malformed `index.json`**
- **Given** an `index.json` with a JSON syntax error
- **When** the consistency test attempts to decode it
- **Then** the test fails with a clear decode error rather than silently skipping validation

## Performance Requirements
- **Response Time:** Decoding and cross-checking a 10-entry `index.json` against 10 YAML files completes in well under 1 second in the test suite.
- **Throughput:** N/A (test-time only; runtime fetch performance is covered by Story 1/2's AC files, not this content-authoring AC).

## Security Considerations
- **Authentication/Authorization:** N/A — `index.json` is static repository content with no auth surface.
- **Input Validation:** `index.json` is decoded via the existing permissive `encoding/json` unmarshal into `PersonaIndexEntry` (per Story 2); no new validation is introduced at this layer, but the content-consistency check (index vs. YAML) is enforced at test time to prevent silent drift.

## Test Implementation Guidance
**Test Type:** UNIT (decode + cross-file consistency check, no network)
**Test Data Requirements:** The committed `personas/community/index.json` plus all 10 committed persona YAML files
**Mock/Stub Requirements:** None — pure filesystem read + JSON/YAML decode, no HTTP mocking needed for this AC (network-level fetch of the index is covered by Story 1's AC files)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `index.json` has exactly one entry per authored persona, `path` resolving to a real committed file
- [ ] Every entry's `provider`/`model` matches the corresponding YAML's `provider`/`model` exactly
- [ ] Each entry's slug is consistent across `name`, stem(`path`), the `<slug>.md` prompt, and the `ResolvePersona`-resolvable name, and passes `validateName`
- [ ] Task-scoped personas carry `tasks`/`tags` reflecting their review lens

**Manual Review:**
- [ ] Code reviewed and approved
