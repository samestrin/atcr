# Acceptance Criteria: Registry Docs and Example YAML Updates

**Related User Story:** [06: In-Repo Documentation for Persona Installation and Authoring](../user-stories/06-in-repo-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Registry Reference | Markdown | `docs/registry.md` — existing file, addendum required |
| Example Registry Files | YAML | `examples/registry-without-executor.yaml` and `examples/registry-with-executor.yaml` — both modified |
| Test Framework | go test | Registry loader exercises example files; they must remain valid YAML after edits |
| Key Dependencies | T8 (language field + `SelectEligibleSkeptics` routing), T2 (AgentConfig.Language in schema) | Field names must match implemented Go struct field |

## Related Files
- `docs/registry.md` - modify: add `language` field reference entry covering type, canonical format, nil semantics, and routing behavior
- `examples/registry-without-executor.yaml` - modify: add at least one `language` field example on an agent definition
- `examples/registry-with-executor.yaml` - modify: add at least one `language` field example on an agent definition (skeptic agent preferred to illustrate routing)

### Related Files (from codebase-discovery.json)

- `docs/registry.md` — modify: document `language` field and routing behavior
- `examples/registry-without-executor.yaml` — modify: add `language` example
- `examples/registry-with-executor.yaml` — modify: add `language` example on skeptic agent
- `internal/registry/config.go:267` — `AgentConfig.Language` field definition
- `internal/verify/select.go:55` — `SelectEligibleSkeptics` two-partition routing implementation
- `internal/verify/select.go:84-86` — existing `n`-cap (routing behavior reference)
- `.planning/specifications/design-concepts/adversarial-verification-interface.md` — verification interface semantics

## Happy Path Scenarios

**Scenario 1: Developer reads the language field reference in docs/registry.md**
- **Given** a developer wants to configure a language-aware skeptic in their registry
- **When** they consult `docs/registry.md` and locate the `language` field entry
- **Then** the entry specifies: field type (string array), canonical format (no leading dot, lowercased), nil semantics (available to all reviews), and routing behavior (language-matched skeptics are preferred; silent fallback to generalist skeptics when no match)

**Scenario 2: Developer uses the example files as a starting point**
- **Given** a developer clones ATCR and opens `examples/registry-without-executor.yaml`
- **When** they look for how to add a language scope to an agent definition
- **Then** they find at least one agent definition with a `language` field set to a valid value, and the file remains valid YAML

**Scenario 3: Language-aware routing behavior is illustrated in docs/registry.md**
- **Given** a developer wants to understand the two-partition algorithm used by `SelectEligibleSkeptics`
- **When** they read the `language` field section in `docs/registry.md`
- **Then** the section explains: (1) if language-matched skeptics exist, only they are eligible; (2) if none match, all registered skeptics are eligible (silent fallback); this description matches the `SelectEligibleSkeptics` implementation

**Scenario 4: Example files pass go test after language field addition**
- **Given** `examples/registry-without-executor.yaml` and `examples/registry-with-executor.yaml` are edited to include `language` field examples
- **When** `go test ./...` is run
- **Then** all tests pass, confirming the example files remain valid against the registry schema

## Edge Cases

**Edge Case 1: language field with nil / omitted value**
- **Given** the `docs/registry.md` language field entry is consulted
- **When** a developer reads the nil semantics section
- **Then** the doc clearly states that omitting `language` means the agent is available to all reviews regardless of the detected repository language

**Edge Case 2: language field with multiple languages**
- **Given** a developer wants to scope an agent to both Go and TypeScript
- **When** they consult the `docs/registry.md` example
- **Then** the doc provides a multi-value example (e.g., `language: ["go", "ts"]`) consistent with the canonical format

**Edge Case 3: No language-matched skeptic available at review time**
- **Given** a registry has only language-scoped skeptics but none match the current review's detected language
- **When** `SelectEligibleSkeptics` runs
- **Then** `docs/registry.md` documents the silent fallback: all registered skeptics become eligible, so the review is never blocked

## Error Conditions

**Error Scenario 1: Invalid language value format (leading dot or uppercase)**
- **Given** a developer writes `language: [".Go"]` with a leading dot and uppercase letter
- **When** the registry is loaded
- **Then** `docs/registry.md` documents that `applyDefaults` canonicalizes the value (strips leading dot, lowercases) — or that the value is rejected with a validation error — matching the actual implementation behavior
- Error code: documented in the registry reference; implementation-determined

**Error Scenario 2: Example YAML becomes invalid after language field addition**
- **Given** the `language` field is added to an example file with incorrect YAML syntax
- **When** `go test ./...` is run
- **Then** the registry loader test fails with a YAML parse error; the DoD requires this test to pass, preventing merge of invalid YAML

**Error Scenario 3: docs/registry.md references deprecated example path**
- **Given** the deprecated path `docs/examples/registry.yaml` no longer exists
- **When** a developer reads the updated `docs/registry.md`
- **Then** no reference to `docs/examples/registry.yaml` appears anywhere in the updated document; only `examples/registry-without-executor.yaml` and `examples/registry-with-executor.yaml` are cited

## Performance Requirements
- **Response Time:** Not applicable — documentation and static YAML files
- **Throughput:** Not applicable

## Security Considerations
- **Authentication/Authorization:** No auth surface in docs or static YAML
- **Input Validation:** The `language` field canonical format rules documented in `docs/registry.md` must match the `applyDefaults` canonicalization logic exactly; mismatches would allow silently misrouted skeptics

## Test Implementation Guidance
**Test Type:** UNIT (`go test ./...` validates example YAML files against the registry loader); MANUAL (doc review)
**Test Data Requirements:** The two example YAML files with at least one `language` field each; `go test ./...` must be run after editing them
**Mock/Stub Requirements:** None — registry loader tests use the real example files
**Verification Method:**
1. Run `go test ./...` after editing both example files — must pass. This exercises `TestRegistryExamples_Valid` (or equivalent registry loader coverage) that loads both example files through `internal/registry`.
2. Grep both example files and `docs/registry.md` for `docs/examples/registry.yaml` — must return no matches.
3. Review `docs/registry.md` language field section against `SelectEligibleSkeptics` implementation to confirm routing behavior description is accurate.

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./...` — registry loader exercises both example files)
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `docs/registry.md` contains a `language` field reference entry covering type, canonical format, nil semantics, and two-partition routing behavior
- [x] `examples/registry-without-executor.yaml` includes at least one agent definition with a valid `language` field
- [x] `examples/registry-with-executor.yaml` includes at least one agent definition with a valid `language` field (skeptic agent preferred)
- [x] No file in `docs/` or `examples/` references the deprecated `docs/examples/registry.yaml` path

**Manual Review:**
- [ ] Code reviewed and approved
