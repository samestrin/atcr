# Acceptance Criteria: PersonaIndexEntry Schema Extension

**Related User Story:** [02: Structured Model Metadata Schema](../user-stories/02-structured-model-metadata-schema.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct definition | `internal/personas/search.go` |
| Test Framework | Go `testing` + `encoding/json` | Table-driven decode assertions |
| Key Dependencies | `encoding/json` (stdlib) | No new external dependencies |

## Related Files
- `internal/personas/search.go` - modify: extend `PersonaIndexEntry` with `Provider`, `Model`, `Tasks`, `Tags` fields carrying `json:"...,omitempty"` tags, without altering `Name`/`Version`/`Description`/`Path` field names or existing tags
- `internal/personas/search_test.go` - create: unit tests asserting the new fields decode correctly and existing fields are untouched
- `internal/personas/client.go` - reference only: `FetchIndex` decodes `index.json` into `[]PersonaIndexEntry` via default (permissive) `encoding/json` unmarshal — no change required here since new fields are additive

## Happy Path Scenarios
**Scenario 1: New-shape index.json entry decodes with all fields populated**
- **Given** an `index.json` entry `{"name":"security/owasp","version":"1.0.0","description":"OWASP reviewer","path":"security/owasp.yaml","provider":"anthropic","model":"claude-sonnet-4-6","tasks":["security-review"],"tags":["owasp","security"]}`
- **When** the entry is unmarshaled into `PersonaIndexEntry`
- **Then** `Provider` equals `"anthropic"`, `Model` equals `"claude-sonnet-4-6"`, `Tasks` equals `["security-review"]`, and `Tags` equals `["owasp","security"]`, alongside correctly populated `Name`/`Version`/`Description`/`Path`

**Scenario 2: Struct tags preserve existing JSON key casing**
- **Given** the extended `PersonaIndexEntry` struct definition in `search.go`
- **When** the struct is inspected via `reflect.TypeOf(PersonaIndexEntry{})` field tags in a test
- **Then** `Name`, `Version`, `Description`, `Path` retain their original `json:"name"`, `json:"version"`, `json:"description"`, `json:"path"` tags exactly, with no `omitempty` added to those four (matching existing behavior byte-for-byte)

## Edge Cases
**Edge Case 1: Entry omits `tasks`/`tags` (optional fields absent)**
- **Given** an `index.json` entry that includes `provider`/`model` but no `tasks`/`tags` keys
- **When** the entry is unmarshaled into `PersonaIndexEntry`
- **Then** `Tasks` and `Tags` decode as `nil` (zero-value slice), not an empty-but-non-nil slice, and no error occurs

**Edge Case 2: `provider`/`model` present but empty string**
- **Given** an `index.json` entry with `"provider":""` and `"model":""`
- **When** the entry is unmarshaled
- **Then** `Provider` and `Model` decode as empty strings with no error (schema-level extension does not itself enforce non-empty values — that validation is out of scope for this story)

## Error Conditions
**Error Scenario 1: Malformed JSON in an entry**
- **Given** an `index.json` payload with a syntax error (e.g., trailing comma or unterminated string)
- **When** `FetchIndex` attempts to decode the payload
- **Then** the existing decode error path in `client.go` is returned unchanged — this story does not alter error handling, only the target struct's shape

## Performance Requirements
- **Response Time:** Struct decode of a single entry adds negligible overhead (four additional scalar/slice fields); no measurable regression versus the current four-field struct for index sizes in the hundreds of entries.
- **Throughput:** No change to `FetchIndex`'s existing HTTP fetch and decode throughput characteristics.

## Security Considerations
- **Authentication/Authorization:** Not applicable — this is a pure data-shape change with no auth surface.
- **Input Validation:** New fields are decoded permissively (no `KnownFields(true)`); no additional validation is introduced at this layer per the story's constraint that strict-field validation belongs to the persona-loading path, not the index struct. Field values are treated as opaque display/search strings by downstream code, not executed or interpolated.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Inline JSON literals covering: full new-shape entry, entry missing `tasks`/`tags`, entry with empty-string `provider`/`model`.
**Mock/Stub Requirements:** None — pure `encoding/json.Unmarshal` calls against `PersonaIndexEntry`, no HTTP or filesystem mocking needed for this AC (network-level fixture tests belong to AC 02-03).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `PersonaIndexEntry` has `Provider string`, `Model string`, `Tasks []string`, `Tags []string` fields with `json:"...,omitempty"` tags
- [ ] `Name`/`Version`/`Description`/`Path` fields and tags are unchanged from the pre-story definition
- [ ] Unit test confirms new fields decode correctly when present and as zero-values when absent

**Manual Review:**
- [ ] Code reviewed and approved
