# Acceptance Criteria: Documentation Updates for the Renamed Personas

**Related User Story:** [05: Human-Names Migration for Built-in Stragglers](../user-stories/05-human-names-migration-for-built-in-stragglers.md)
**Design References:** [human-names-migration.md](../documentation/human-names-migration.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/personas-authoring.md`, `docs/personas-install.md`) | Content edits only, no code |
| Test Framework | Manual review + scoped grep verification (shared with AC 05-03) | No automated doc test framework in this repo |
| Key Dependencies | None | Pure documentation change |

### Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md` — modify: update worked examples (file path references, fixture-naming examples, performance-fixture walkthrough) from `sentinel`/`tracer`/`idiomatic` to `sasha`/`penny`/`ingrid`.
- `docs/personas-install.md` — modify: update the built-in persona summary and worked `atcr personas list`/`atcr personas test` sample output tables to reflect the new names.
- `personas/personas.go` (names slice ~line 20) — reference: source of truth for the current built-in persona names.
- `docs/personas-authoring.md` / `docs/personas-install.md` — reference: scoped-search verification shared with AC 05-03.


## Happy Path Scenarios
**Scenario 1: `personas-authoring.md` worked examples use the new slugs**
- **Given** the rename has landed
- **When** `docs/personas-authoring.md` is read end-to-end
- **Then** every worked example (file path reference, fixture-naming convention example, the performance-fixture walkthrough) refers to `sasha`, `penny`, or `ingrid` as appropriate, with no residual reference to `sentinel`, `tracer`, or `idiomatic` as a currently-active example

**Scenario 2: `personas-install.md` built-in persona list and CLI output are current**
- **Given** the rename has landed
- **When** `docs/personas-install.md` is read end-to-end
- **Then** the introductory built-in-persona summary and every worked `atcr personas list`/`atcr personas test` sample output table reflect `sasha`, `penny`, `ingrid` in place of the retired names, with `provider`/`model` columns (if introduced by Story 2/3 in the same doc) staying internally consistent with the rest of the plan's documentation updates

## Edge Cases
**Edge Case 1: Historical/narrative mentions of the old names for context**
- **Given** this plan's own supporting documentation (e.g., a `documentation/human-names-migration.md` note, if authored, or this plan's `plan.md` Theme 5) may legitimately need to *describe* the rename (mentioning `sentinel`→`sasha` as history)
- **When** `docs/personas-authoring.md` and `docs/personas-install.md` specifically are reviewed (the two files the story explicitly scopes as needing updates)
- **Then** those two user-facing docs are updated to stop presenting the old names as current/active guidance, while planning artifacts describing the migration's history are out of scope for this AC (they are process records, not user-facing product docs)

**Edge Case 2: Fixture-naming convention example stays illustrative, not slug-specific**
- **Given** `docs/personas-authoring.md` line 119 uses `sentinel_fixture.patch`/`tracer_fixture.patch` purely as *examples* of the `<slug>_fixture.patch` naming convention, not as a claim about which personas currently exist
- **When** the examples are updated
- **Then** they are replaced with `sasha_fixture.patch`/`penny_fixture.patch` (still illustrating the same naming convention pattern) rather than removed, preserving the doc's teaching value for authors creating new personas

## Error Conditions
**Error Scenario 1: Doc drift after a future rename**
- **Given** documentation references are a common source of drift after code renames
- **When** the scoped verification search from AC 05-03 is run against `docs/personas-authoring.md` and `docs/personas-install.md`
- **Then** any remaining match for `sentinel`/`tracer`/`idiomatic` as an active persona slug in these two files is treated as this AC being incomplete, blocking story completion
- HTTP status / error code: N/A (documentation review gate, not a runtime error)

## Performance Requirements
- **Response Time:** N/A — static documentation, no runtime behavior.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public documentation content.
- **Input Validation:** N/A — no user input processed.

## Test Implementation Guidance
**Test Type:** MANUAL (documentation review) + shared scoped-search verification with AC 05-03
**Test Data Requirements:** N/A — direct inspection of the two Markdown files' full content
**Mock/Stub Requirements:** None

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/personas-authoring.md` worked examples (lines 61, 119, 130, 148 per current content) reference `sasha`/`penny`/`ingrid` in place of `sentinel`/`tracer`/`idiomatic`
- [ ] `docs/personas-install.md` built-in persona summary and worked CLI output (lines 3, 76, 89 per current content) reference `sasha`/`penny`/`ingrid`
- [ ] Fixture-naming convention examples remain illustrative and use the new slugs
- [ ] Scoped search (shared with AC 05-03) confirms zero remaining active-slug references in these two files

**Manual Review:**
- [ ] Code reviewed and approved
