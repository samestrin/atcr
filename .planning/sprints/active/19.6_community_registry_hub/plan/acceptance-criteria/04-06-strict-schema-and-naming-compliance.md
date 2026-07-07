# Acceptance Criteria: Strict Schema Validation and Human-Name Convention Compliance

**Related User Story:** [04: Model-Indexed Persona Library Authoring](../user-stories/04-model-indexed-persona-library-authoring.md)
**Design References:** [persona-yaml-schema.md](../documentation/persona-yaml-schema.md), [human-names-migration.md](../documentation/human-names-migration.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Registry agent schema validation (Go), naming-convention lint | `internal/registry`'s strict-field validation applied to each community persona YAML |
| Test Framework | Go `testing` package, table-driven negative-case assertions | Extends the community persona test file with reject-on-invalid cases |
| Key Dependencies | `internal/registry` schema validator; no new dependency | Reuses existing strict/non-strict validation split documented in `docs/personas-authoring.md` |

### Related Files (from codebase-discovery.json)
- `personas/community/*.yaml` — reference: all 10+ authored persona YAML files, the subject of strict validation and human-name convention checks.
- `personas/community_test.go` — create: positive tests asserting every community persona YAML passes strict registry-schema validation, plus negative tests for unknown agent fields, out-of-range values, and role-based naming.
- `docs/personas-authoring.md` — reference: strict-vs-non-strict validation contract and the all-human-names convention.
- `internal/registry` — reference: registry agent schema validator.


## Happy Path Scenarios
**Scenario 1: Every persona YAML validates cleanly with no unknown agent fields**
- **Given** the full set of 10 community persona YAML files
- **When** each is parsed and validated against the registry agent schema
- **Then** validation succeeds for all 10, with only recognized agent fields (`provider`, `model`, `persona`, `role`, `language`) plus catalog-only keys (`name`, `version`, `description`) present

**Scenario 2: No persona ships with a placeholder or unvalidated binding**
- **Given** any community persona YAML's `model` field
- **When** its value is inspected
- **Then** it is a concrete model id string (e.g. `claude-opus-4-1`, `deepseek-chat`) — never a placeholder like `TODO`, `<model>`, or an empty string

**Scenario 3: Every persona slug/name follows the all-human-names convention**
- **Given** the `name`/slug of each of the 10 authored community personas
- **When** compared against the Epic 23.0 all-human-names convention (no role-based names such as `security-reviewer` or `perf-checker`)
- **Then** every persona uses a human first name (or human-name-based slug), consistent with the convention already applied to `bruce`, `greta`, `kai`, `mira`, `dax`, `otto`

## Edge Cases
**Edge Case 1: An out-of-range `role` value is rejected**
- **Given** a persona YAML with `role: "auditor"` (not one of `reviewer`/`skeptic`/`judge`)
- **When** the registry schema validates it
- **Then** validation fails with an out-of-range error, and the offending persona is caught before merge

**Edge Case 2: A catalog-only field typo does not fail strict validation**
- **Given** a persona YAML with an extra non-agent key like `notes: "internal reminder"`
- **When** validated
- **Then** the non-strict catalog-field handling does not reject the file (consistent with `docs/personas-authoring.md`'s documented non-strict-for-catalog-keys behavior), distinguishing this from a genuinely unknown *agent* field

## Error Conditions
**Error Scenario 1: Unknown agent field present**
- **Given** a persona YAML with an unrecognized agent-level key (e.g. `temperature: 0.7` if not a supported agent field)
- **When** the registry schema validates it
- **Then** validation fails with an unknown-field error identifying the offending key and persona file

**Error Scenario 2: Persona name uses a role-based slug**
- **Given** a persona authored with a slug like `security-reviewer` instead of a human name
- **When** the naming-convention check runs
- **Then** the check fails, listing the non-compliant persona and requiring a rename before the AC is satisfied

## Performance Requirements
- **Response Time:** Schema validation of 10 persona YAML files completes in well under 1 second in the test suite.
- **Throughput:** N/A (test-time only).

## Security Considerations
- **Authentication/Authorization:** N/A — schema validation is a local, static check with no auth surface.
- **Input Validation:** Strict validation on all agent-level fields (`provider`, `model`, `persona`, `role`, `language`) rejects unknown or out-of-range values before a persona is ever installable, closing the smuggled-behavior vector `docs/personas-authoring.md` calls out explicitly.

## Test Implementation Guidance
**Test Type:** UNIT (positive validation across all 10 personas + negative cases for unknown field / out-of-range value / role-based naming)
**Test Data Requirements:** The 10 committed persona YAML files, plus 2-3 synthetic invalid YAML fixtures (unknown field, out-of-range `role`, role-based slug) used only in the negative-case tests, not committed to `personas/community/`
**Mock/Stub Requirements:** None — pure schema/string validation, no network or LLM call required

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] All 10 community persona YAML files pass strict registry-schema validation
- [ ] No persona ships with a placeholder or empty `model`/`provider` value
- [ ] Every persona slug/name follows the all-human-names convention (no role-based names)
- [ ] Negative-case tests confirm an unknown agent field or out-of-range value is rejected

**Manual Review:**
- [ ] Code reviewed and approved
