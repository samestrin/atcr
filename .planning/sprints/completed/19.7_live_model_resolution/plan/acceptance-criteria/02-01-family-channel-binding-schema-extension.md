# Acceptance Criteria: Family/Channel Binding Schema Extension with Back-Compat Decode

**Related User Story:** [02: Family/Channel Binding & Resolved Lock](../user-stories/02-family-channel-binding-resolved-lock.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct field additions (`PersonaIndexEntry`, `AgentConfig`) | `internal/personas/search.go`, `internal/registry/config.go` |
| Test Framework | Go `testing` + `encoding/json` + `gopkg.in/yaml.v3` | Table-driven decode assertions, mirroring 19.6's Provider/Model extension tests |
| Key Dependencies | `encoding/json`, `gopkg.in/yaml.v3` (stdlib/already-vendored) | No new external dependency |

### Related Files (from codebase-discovery.json)
- `internal/personas/search.go:14` (`PersonaIndexEntry`) — modify: add `Binding string `json:"binding,omitempty"`` following the exact convention already used for `Provider`/`Model`/`Tasks`/`Tags` — additive, `omitempty`, no `KnownFields`/`DisallowUnknownFields` enforcement at this decode site.
- `internal/personas/search_test.go` — modify: extend `TestPersonaIndexEntry_DecodesFullNewShape` (or add a sibling test) to cover `binding`; add/extend `TestFetchIndex_OldShapeFixtureDecodes` and `TestFetchIndex_MixedShapeDecodes` so a pre-epic (no `binding` key) payload still decodes `Binding` as `""` with no error.
- `internal/registry/config.go:291` (`AgentConfig`) — modify: add `Binding string `yaml:"binding,omitempty"`` alongside `Fallback`/`Payload` — the persona-YAML-facing field a future resolver (Theme 3/Story 3) will read to compute a new `Model` value at `atcr personas upgrade` time. `AgentConfig.Model` itself is NOT renamed or restructured: `Model` already IS the field that carries the resolved concrete slug ("the lock") to the wire — it was seeded in Epic 19.6 and requires no new field or migration to serve that role.
- `internal/registry/validate.go:68-77` (`communityPersonaFile`) — reference only, no modification expected: `communityPersonaFile` inlines `AgentConfig` (`yaml:",inline"`) and is decoded with `KnownFields(true)` in `ValidateCommunityPersonaYAML`. Because `Binding` is added directly to the inlined `AgentConfig`, it becomes a recognized key for the strict community-YAML decode automatically — confirm this with a regression test (see Edge Case 3) rather than assuming it silently.
- `internal/personas/unit.go:95` (`writePersonaUnit`) — reference only, no modification expected: it writes the fetched YAML byte-for-byte with no field-level transcoding, so a `binding:` key present in fetched community YAML is persisted through `InstallUnit`/`Upgrade` automatically, with zero code change to this function.
- `personas/community_test.go` — modify: extend `TestCommunityIndex_Registration` so that when an entry carries a `binding` value it is compared for equality against the source YAML's `binding:` key, keeping the new field in sync via the same test that already guards `Provider`/`Model`/`Description`.
- `personas/community/index.json:1` — modify (optional, illustrative): may add a `binding` key mirroring the eventual family/channel target for each persona; this is authoring metadata for Theme 3's resolver, not a requirement of this AC.

## Happy Path Scenarios
**Scenario 1: New-shape `index.json` entry with `binding` decodes**
- **Given** an `index.json` entry `{"name":"anthony","version":"1.0.0","description":"...","path":"anthony.yaml","provider":"openrouter","model":"anthropic/claude-opus-4.8","binding":"anthropic/claude-opus@stable","tasks":["architecture-review"],"tags":["architecture"]}`
- **When** the entry is unmarshaled into `PersonaIndexEntry`
- **Then** `Binding` equals `"anthropic/claude-opus@stable"` and every other field decodes as before (unchanged from 19.6's shape)

**Scenario 2: Persona YAML declaring `binding:` decodes through the strict community-YAML path**
- **Given** a community persona YAML document containing `provider: openrouter`, `model: anthropic/claude-opus-4.8`, and `binding: anthropic/claude-opus@stable`
- **When** `ValidateCommunityPersonaYAML` decodes it via `communityPersonaFile`
- **Then** decoding succeeds with no "unknown field" error, and the resulting `AgentConfig.Binding` equals `"anthropic/claude-opus@stable"`

**Scenario 3: Marshal round-trip includes `binding` only when set**
- **Given** a `PersonaIndexEntry` value with `Binding` populated
- **When** the struct is marshaled to JSON
- **Then** the output contains the `binding` key with the expected value; a value with `Binding` left as the zero value (`""`) omits the key entirely (`omitempty`)

## Edge Cases
**Edge Case 1: Old-shape `index.json` entry (no `binding` key) decodes cleanly**
- **Given** a pre-epic-shape `index.json` entry containing only `name`/`version`/`description`/`path` (or the 19.6 shape adding `provider`/`model`/`tasks`/`tags`, but no `binding`)
- **When** the entry is unmarshaled into `PersonaIndexEntry`
- **Then** `Binding` decodes as the zero value `""` with no error — mirroring `TestFetchIndex_OldShapeFixtureDecodes`'s existing assertions for `Provider`/`Model`

**Edge Case 2: Mixed-shape array (one entry with `binding`, one without) decodes independently**
- **Given** an `index.json` array mixing one old-shape entry and one entry carrying `binding`
- **When** the array is unmarshaled
- **Then** each entry's `Binding` value reflects only its own JSON, with no cross-entry interference (mirroring `TestFetchIndex_MixedShapeDecodes`)

**Edge Case 3: A genuinely unknown key (not `binding`) still fails the strict community-YAML gate**
- **Given** a community persona YAML document containing a key that is neither a recognized `AgentConfig` field, a `communityPersonaFile` catalog-only key, nor `binding`
- **When** `ValidateCommunityPersonaYAML` decodes it
- **Then** decoding still fails with an "unknown field" error — proving the `Binding` schema addition did not accidentally widen `KnownFields(true)` enforcement beyond the one new key

**Edge Case 4: `writePersonaUnit` persists a `binding:` key with zero code change**
- **Given** fetched community YAML bytes containing a `binding:` key
- **When** `InstallUnit` (or `Upgrade`, via the shared `writePersonaUnit` tail) writes the unit to disk
- **Then** the on-disk YAML file contains the `binding:` key byte-for-byte identical to the fetched source — no transformation, defaulting, or stripping occurs, confirming the shared paired-write tail requires no modification for this story

## Error Conditions
**Error Scenario 1: Malformed JSON/YAML in an entry**
- **Given** an `index.json` or persona-YAML payload with a syntax error
- **When** the existing decode path attempts to parse it
- **Then** the existing error path (unchanged by this story) is returned — this story only extends the target struct's shape, not error handling

## Performance Requirements
- **Response Time:** One additional optional string field adds negligible decode overhead; no measurable regression (≤1% wall-time difference in `go test ./...`) versus the pre-story struct for index sizes in the hundreds of entries.
- **Throughput:** No change to `FetchIndex`'s or `ValidateCommunityPersonaYAML`'s existing fetch/decode throughput characteristics.

## Security Considerations
- **Authentication/Authorization:** Not applicable — pure data-shape change with no auth surface.
- **Input Validation:** `Binding` is decoded permissively as an opaque string, exactly like `Provider`/`Model`. This story intentionally does NOT add format validation (e.g., `vendor/family@channel` shape, control-character rejection) for `Binding` — the field is never read on any prompt-rendering or execution path in this story (see AC 02-02), so it carries no injection surface yet. Format validation and control-character guarding (mirroring `Scope`/`Language`/`Persona`'s existing guards in `validateAgent`) become the resolver's (Theme 3/Story 3) responsibility before it is ever consumed to compute a new `Model` value.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Inline JSON/YAML literals covering: full new-shape entry with `binding`, old-shape entry missing `binding`, mixed-shape array, and a community-YAML document with `binding` plus one with a genuinely unrecognized key.
**Mock/Stub Requirements:** None for the decode tests (pure `encoding/json.Unmarshal`/`yaml.Unmarshal`); the `writePersonaUnit` persistence check (Edge Case 4) reuses the existing `httptest.NewServer`-backed `HTTPClient` fixture pattern already used by `internal/personas/unit_test.go`.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `PersonaIndexEntry` gains `Binding string `json:"binding,omitempty"``; `AgentConfig` gains `Binding string `yaml:"binding,omitempty"``, with no changes to existing field names/tags
- [ ] Old-shape (pre-epic) `index.json`/persona-YAML payloads without `binding` decode with `Binding == ""` and no error
- [ ] `ValidateCommunityPersonaYAML`'s `KnownFields(true)` gate accepts `binding` while still rejecting a genuinely unrecognized key
- [ ] `writePersonaUnit` persists a `binding:` key present in fetched YAML with zero modification to `unit.go`

**Manual Review:**
- [ ] Code reviewed and approved
