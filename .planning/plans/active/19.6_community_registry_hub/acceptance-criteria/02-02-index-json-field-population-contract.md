# Acceptance Criteria: index.json Field Population Contract

**Related User Story:** [02: Structured Model Metadata Schema](../user-stories/02-structured-model-metadata-schema.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (schema contract) | No in-repo `index.json` generator exists — the community repo's publish pipeline builds `index.json` externally |
| Test Framework | N/A (documentation) + Go unit test cross-check | `internal/personas/search_test.go` verifies the documented shape matches the decodable struct |
| Key Dependencies | None | Additive doc + struct field reference only |

## Related Files
- `docs/personas-authoring.md` - modify: add a subsection documenting the `index.json` entry schema (`name`, `version`, `description`, `path`, `provider`, `model`, `tasks`, `tags`) and state that `provider`/`model` MUST be lifted verbatim from the persona YAML's required `provider`/`model` keys, with `tasks`/`tags` sourced from the YAML when present
- `docs/personas-authoring.md` - modify: extend the "Contribution checklist" (§4) with a checklist item confirming the published `index.json` entry for the persona carries non-empty `provider`/`model` matching the persona YAML
- `internal/personas/search.go` - reference only: the extended `PersonaIndexEntry` struct (from AC 02-01) is the single source of truth for the documented field names/types
- `internal/personas/search_test.go` - modify/create: a test asserting the documented example `index.json` entry in `docs/personas-authoring.md` (or an equivalent fixture) decodes into `PersonaIndexEntry` with `Provider`/`Model` matching the persona YAML's `provider`/`model` values

## Happy Path Scenarios
**Scenario 1: Documented schema matches the Go struct**
- **Given** the `index.json` schema subsection added to `docs/personas-authoring.md`
- **When** a maintainer compares its documented field list (`name`, `version`, `description`, `path`, `provider`, `model`, `tasks`, `tags`) against `PersonaIndexEntry` in `internal/personas/search.go`
- **Then** every documented field name and JSON key matches the struct's `json` tags exactly, with `provider`/`model`/`tasks`/`tags` marked as populated-from-YAML and `tasks`/`tags` marked optional

**Scenario 2: Example entry round-trips through the struct**
- **Given** the example `index.json` entry shown in the new documentation subsection (containing `provider`/`model` values that match a sample persona YAML's `provider: anthropic` / `model: claude-sonnet-4-6`)
- **When** the example entry is decoded into `PersonaIndexEntry` in a test
- **Then** `Provider` equals `"anthropic"` and `Model` equals `"claude-sonnet-4-6"`, proving the documented contract is exercisable end-to-end within this repo's test suite

## Edge Cases
**Edge Case 1: Persona YAML has no `tasks`/`tags` authored**
- **Given** the documentation's guidance that `tasks`/`tags` are optional and should be omitted (not emitted as empty arrays) when a persona's YAML does not declare them
- **When** a maintainer reads the schema subsection
- **Then** the guidance explicitly states publish tooling should omit the keys entirely rather than emit `"tasks":[]`/`"tags":[]`, consistent with the `omitempty` behavior on the Go struct

**Edge Case 2: Existing personas published before this schema change**
- **Given** community personas already published under the old four-field `index.json` shape
- **When** the documentation subsection is read
- **Then** it states that such entries remain valid but will not be model-discoverable until the community repo's index is regenerated with `provider`/`model` populated (cross-referencing the backward-compatibility guarantee from AC 02-03)

## Error Conditions
**Error Scenario 1: Contribution checklist item fails (missing provider/model in published index entry)**
- **Given** a contributor follows the extended checklist in §4
- **When** the checklist item "published `index.json` entry carries non-empty `provider`/`model`" is unchecked because the publish tooling did not populate it
- **Then** the documentation directs the contributor to verify their `index.json` regeneration step ran, rather than accepting the pull request with silently missing metadata (no formal error code — this is a docs/process control, not a runtime error)

## Performance Requirements
- **Response Time:** Not applicable — documentation change only, no runtime path affected.
- **Throughput:** Not applicable.

## Security Considerations
- **Authentication/Authorization:** Not applicable — no auth surface in documentation.
- **Input Validation:** Documentation must not instruct contributors to embed executable content, secrets, or network instructions in `provider`/`model`/`tasks`/`tags` values — these are display/search metadata only, consistent with the existing security note in `docs/personas-authoring.md` about persona prompts.

## Test Implementation Guidance
**Test Type:** UNIT (for the Go-side cross-check); documentation content is verified by manual/editorial review since there is no in-repo index-generation code path to unit test directly
**Test Data Requirements:** One example `index.json` entry matching the documentation's sample, and one sample persona YAML fragment (`provider`/`model`) it is derived from
**Mock/Stub Requirements:** None

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/personas-authoring.md` documents the full `index.json` entry schema including `provider`/`model`/`tasks`/`tags`
- [ ] Contribution checklist (§4) includes an item requiring non-empty `provider`/`model` in the published index entry
- [ ] A test decodes the documented example entry into `PersonaIndexEntry` and confirms `Provider`/`Model` match the source YAML values

**Manual Review:**
- [ ] Code reviewed and approved
