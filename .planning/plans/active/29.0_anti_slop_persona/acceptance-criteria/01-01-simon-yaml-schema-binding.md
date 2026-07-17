# Acceptance Criteria: `simon.yaml` Strict Schema and Concrete Provider/Model Binding

**Related User Story:** [1: Author the `simon` Persona Unit](../user-stories/01-author-the-simon-persona-unit.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | YAML persona metadata authoring | `personas/community/simon.yaml`, decoded via `gopkg.in/yaml.v3` strict `KnownFields(true)` |
| Test Framework | `go test` + `testify/require` | table-driven, iterates `personas.CommunityNames()` (dynamic via `go:embed community/*.yaml`) |
| Key Dependencies | `internal/registry.ValidateCommunityPersonaYAML`, `personas.CommunityNames()` / `personas.CommunityModel()` | no new dependencies — reuses existing registry validation path |

## Related Files
- `personas/community/simon.yaml` - create: persona metadata binding (`name`, `version`, `description`, `provider: openrouter`, `model`, `persona: simon`, `role: reviewer`)
- `personas/community/sonny.yaml` - reference only: structural template this file is modeled on (`provider: openrouter` / `model: anthropic/claude-sonnet-5` pattern already validated elsewhere in the registry)
- `internal/personas/community_schema_test.go` - test (unmodified): `TestCommunityPersonas_StrictSchema`, `TestCommunityPersonas_NoPlaceholderModel`, `TestCommunityPersonas_HumanNames` auto-iterate the new file via `builtins.CommunityNames()`
- `personas/community.go` - reference only: `CommunityNames()` derives its list from `go:embed community/*.yaml` + `community/*.md`, so `simon.yaml` is auto-discovered at build time with no roster edit

### Related Files (from codebase-discovery.json)
- `personas/community/simon.yaml` - create (`files_to_create`): agent binding metadata (provider, model, persona, role), based on `personas/community/sonny.yaml`
- `personas/community/sonny.yaml` - reference only (`related_files`, high relevance): structural template for the new simon.yaml (provider/model/persona/role fields)
- `internal/personas/community_schema_test.go:41` - test, reference only (`related_files`, high relevance): strict-YAML / no-placeholder-model / human-name gates iterating `builtins.CommunityNames()`; defines the recognized-keys constraint for simon.yaml
- `personas/community.go:38` - reference only (`related_files`, medium relevance): go:embed accessors (`CommunityNames`/`CommunityGet`/`CommunityModel`) that make the persona resolvable once its files are dropped in
- `docs/personas-authoring.md` - reference only (`build_from.primary_file`): YAML schema and recognized-key contract to copy

## Happy Path Scenarios
**Scenario 1: Strict decode accepts the authored YAML**
- **Given** `personas/community/simon.yaml` exists with only recognized keys (`name`, `version`, `description`, `provider`, `model`, `persona`, `role`)
- **When** `registry.ValidateCommunityPersonaYAML("simon", data)` runs (as `TestCommunityPersonas_StrictSchema` does for every `builtins.CommunityNames()` entry)
- **Then** validation returns no error

**Scenario 2: Concrete, non-placeholder provider/model binding**
- **Given** `simon.yaml` sets `provider: openrouter` and `model: <concrete catalog-id>` (e.g. reusing a model id already present in another `personas/community/*.yaml` file, per `sonny.yaml`'s `anthropic/claude-sonnet-5` pattern)
- **When** `TestCommunityPersonas_NoPlaceholderModel` reads `provider`/`model` and checks against the placeholder list (`""`, `todo`, `tbd`, `changeme`, `<model>`, `<provider>`, `xxx`, `placeholder`)
- **Then** both fields are non-empty and match none of the placeholder strings

**Scenario 3: Slug matches human-name convention and YAML `name` equals the slug**
- **Given** the file is named `simon.yaml` and its `name:` field is `simon`
- **When** `TestCommunityPersonas_HumanNames` matches the slug against `^[a-z]+$` and checks it is absent from `retiredRoleSlugs` (`sentinel`, `security`, `reviewer`, `auditor`, `scanner`, `linter`, `critic`, `analyst`, `inspector`, `guardian`, `grader`, `monitor`, `validator`, `enforcer`, `judge`, `skeptic`, `fixer`, `executor`, `reviewerbot`, `checker`, `tracer`, `idiomatic`, `perf`)
- **Then** the regex matches, `simon` is not in the denylist, and `m.Name == "simon"`

## Edge Cases
**Edge Case 1: `provider: openrouter` avoids the local-provider documentation gate**
- **Given** `docs/personas-install.md` enforces an additional ollama-pull-tag gate only for `provider: local`
- **When** `simon.yaml` sets `provider: openrouter` instead
- **Then** no extra documentation-sync gate applies to this persona

**Edge Case 2: `persona:` and `role:` fields are present and explicit rather than relying on schema defaults**
- **Given** the schema defaults `persona` to the agent name and `role` to `reviewer` when omitted
- **When** `simon.yaml` explicitly sets `persona: simon` and `role: reviewer` (matching `sonny.yaml`'s explicit style)
- **Then** strict decode still passes and the values are unambiguous in the file itself (no reliance on implicit defaults for a new, auditable persona)

## Error Conditions
**Error Scenario 1: An unrecognized key would break strict decode**
- Error message: an error whose text contains the unknown field name (e.g. `"line 8: field foobar not found in type ..."`), mirroring `TestValidateCommunityPersonaYAML_RejectsUnknownField`'s assertion pattern
- HTTP status / error code: N/A (Go `error` returned from `ValidateCommunityPersonaYAML`, surfaced as a `go test` failure, not an HTTP path)
- This AC's scope is to avoid triggering this error — `simon.yaml` must contain only the keys listed in Scenario 1

**Error Scenario 2: A placeholder or empty `model`/`provider` would fail `TestCommunityPersonas_NoPlaceholderModel`**
- Error message: `require.NotEmptyf` / `require.NotEqualf` failure text naming persona `"simon"` and the empty/placeholder field
- This AC's scope is to avoid triggering this error — bind a concrete catalog model id, never `""`, `todo`, `tbd`, `changeme`, `<model>`, `<provider>`, `xxx`, or `placeholder`

## Performance Requirements
- **Response Time:** N/A — static file authoring, no runtime request path; `go:embed` resolves the file at compile time with no I/O cost during review execution
- **Throughput:** N/A — single persona metadata file, not a hot path

## Security Considerations
- **Authentication/Authorization:** N/A — no credentials, tokens, or secrets are permitted in the file per `docs/personas-authoring.md` §1's security note; `simon.yaml` carries only catalog metadata and a provider/model binding
- **Input Validation:** Strict `KnownFields(true)` decode is the enforcement mechanism — an unrecognized or out-of-range agent field is a load-time error, never a silent pass-through, so a malicious or malformed key cannot reach the review pipeline undetected

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** The authored `personas/community/simon.yaml` file itself is the test fixture — no separate test-data file is created by this AC (existing table-driven tests in `internal/personas/community_schema_test.go` iterate `builtins.CommunityNames()`, which auto-includes `simon` once the file exists)
**Mock/Stub Requirements:** None — tests read the real embedded file via `go:embed`; no network or LLM calls in this test path

## Definition of Done
**Auto-Verified:**
- [ ] `go test ./internal/personas/... ./internal/registry/...` passes for `TestCommunityPersonas_StrictSchema/simon`, `TestCommunityPersonas_NoPlaceholderModel/simon`, `TestCommunityPersonas_HumanNames/simon`
- [ ] No linting errors (`gofmt`/`go vet` clean on the new file's neighbors — YAML has no Go lint surface itself, but the embed and test files remain untouched and green)
- [ ] Build succeeds (`go build ./...`) — `go:embed community/*.yaml` picks up the new file with no code change required

**Story-Specific:**
- [ ] `personas/community/simon.yaml` contains exactly the allowed key set with no unknown fields
- [ ] `provider: openrouter` and a concrete, non-placeholder `model` value are set
- [ ] `name: simon` / file slug `simon` satisfies `^[a-z]+$` and is absent from `retiredRoleSlugs`

**Manual Review:**
- [ ] Code reviewed and approved
