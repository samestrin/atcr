# Acceptance Criteria: Personas Authoring Guide

**Related User Story:** [06: In-Repo Documentation for Persona Installation and Authoring](../user-stories/06-in-repo-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Documentation | Markdown | `docs/personas-authoring.md` — new file |
| Persona Format | YAML | Authoring template is a fill-in-the-blank YAML block embedded in Markdown |
| Fixture Format | `.patch` / `.diff` | Fixture files live in `personas/testdata/` |
| Test Framework | go test / testify | `TestPersonaFixture` exercises fixture files; authoring guide must match its expectations |
| Key Dependencies | T1 (bonus personas as reference implementations), T8 (language field) | Guide must reflect implemented field names exactly |

## Related Files
- `docs/personas-authoring.md` - create: complete persona authoring and contribution guide
- `personas/testdata/` - reference: fixture file location and format that the guide must accurately document
- `internal/` or relevant Go package containing `TestPersonaFixture` - reference: cross-check fixture requirements against test logic

## Happy Path Scenarios

**Scenario 1: Contributor creates a valid persona YAML using the template**
- **Given** a contributor has read `docs/personas-authoring.md` and has no prior knowledge of ATCR internals
- **When** they copy the fill-in-the-blank template from the guide and fill in all required fields
- **Then** the resulting YAML validates against the registry schema and passes `go test ./...` without modification

**Scenario 2: Contributor creates a passing fixture file**
- **Given** a contributor follows the fixture requirements section of `docs/personas-authoring.md`
- **When** they create a `.patch` / `.diff` fixture file in `personas/testdata/` with the documented structure
- **Then** `TestPersonaFixture` passes for their new persona without consulting source code

**Scenario 3: Contributor specifies a language scope using the canonical format**
- **Given** a contributor authors a Go-specific persona
- **When** they set `language: ["go"]` in their persona YAML following the guide's canonical format rules (no leading dot, lowercased)
- **Then** the field is accepted by the registry schema and the persona is routed correctly as a language-aware skeptic

**Scenario 4: Contributor follows the step-by-step contribution checklist**
- **Given** a contributor has a complete persona YAML and fixture file
- **When** they follow the contribution checklist at the end of `docs/personas-authoring.md`
- **Then** they can submit a correct PR without needing additional guidance from maintainers

## Edge Cases

**Edge Case 1: Language field with multiple values**
- **Given** a persona applies to both Go and TypeScript
- **When** the contributor sets `language: ["go", "ts"]` per the guide's multi-language example
- **Then** the guide's format example matches the canonical format enforced by `applyDefaults` canonicalization logic

**Edge Case 2: Language field omitted (generalist persona)**
- **Given** a contributor authors a domain-agnostic persona
- **When** they leave `language` unset (nil) per the guide's "nil semantics" explanation
- **Then** the guide accurately documents that the persona is available to all reviews regardless of detected language

**Edge Case 3: Prompt structure with required sections**
- **Given** the guide specifies mandatory prompt structure sections (e.g., role declaration, output format)
- **When** a contributor omits a required section
- **Then** the guide's checklist item for prompt structure makes the omission detectable before submission

## Error Conditions

**Error Scenario 1: Invalid YAML in persona file**
- **Given** a contributor introduces a YAML syntax error in their persona file
- **When** `go test ./...` is run
- **Then** the guide documents that the registry loader will reject the file with a YAML parse error and instructs the contributor to validate their YAML before submitting
- Error code: non-zero exit from `go test`

**Error Scenario 2: Missing required field in persona YAML**
- **Given** a contributor omits a required field (e.g., `name` or `prompt`)
- **When** the registry schema validation runs
- **Then** the guide's template clearly marks required vs optional fields so the contributor can identify the missing field
- Error message format documented in the guide (e.g., `persona validation failed: missing required field "prompt"`)

**Error Scenario 3: Fixture file not found by TestPersonaFixture**
- **Given** a contributor names the fixture file incorrectly
- **When** `TestPersonaFixture` runs
- **Then** the guide documents the expected filename convention (e.g., `<slug>.patch` in `personas/testdata/`) so the contributor can correct the path

## Performance Requirements
- **Response Time:** Not applicable — documentation file
- **Throughput:** Not applicable

## Security Considerations
- **Authentication/Authorization:** The authoring guide must include a note that personas are executed as part of the review pipeline and contributors must not embed credentials, secrets, or external network calls in persona prompts
- **Input Validation:** The guide must state that persona YAML is validated against the registry schema; unrecognized fields are rejected to prevent injection of unsupported behavior

## Test Implementation Guidance
**Test Type:** MANUAL (authoring walkthrough) backed by UNIT (`TestPersonaFixture`)
**Test Data Requirements:** A reference persona YAML and fixture file created solely from the guide's template, without consulting existing persona source files
**Mock/Stub Requirements:** None — `TestPersonaFixture` runs against real fixture files
**Verification Method:** A contributor unfamiliar with ATCR internals follows `docs/personas-authoring.md` from start to finish, produces a persona YAML + fixture, and achieves a passing `go test ./...` run. Any step requiring source-code lookup is a failure. Cross-reference the template's required fields against `TestPersonaFixture` logic to confirm completeness.

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/personas-authoring.md` exists and includes a complete fill-in-the-blank persona YAML template with all required fields marked
- [ ] Canonical `language` format rules are documented (no leading dot, lowercased, e.g. `["go", "ts"]`) and nil semantics are explained
- [ ] Fixture file requirements (format, location `personas/testdata/`, naming convention) are documented and match `TestPersonaFixture` expectations
- [ ] A step-by-step contribution checklist is present at the end of the guide

**Manual Review:**
- [ ] Code reviewed and approved
