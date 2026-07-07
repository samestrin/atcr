# Acceptance Criteria: Quick Walkthrough Recommends Default Persona Pack

**Related User Story:** [03: Recommend Default Persona Pack in Documentation](../user-stories/03-recommend-default-persona-pack-in-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation | `docs/personas-install.md` |
| Test Framework | Manual review / `git diff` | No automated test runner for docs-only changes |
| Key Dependencies | Existing "Quick walkthrough" section (`docs/personas-install.md:156-176`); `bundle/<name>` install syntax already documented at `docs/personas-install.md:51-61` | Real bundle name sourced from Stories 1-2's published output once available; a clearly-marked placeholder (e.g. `bundle/default-reviewers`) is acceptable if this story ships first per the story's Dependencies note |

## Related Files
- `docs/personas-install.md` - modify: insert a new early step into the "Quick walkthrough" section (currently numbered 1-6, search → install → list → test → upgrade → remove) recommending the default 3-persona pack, with a concrete `atcr personas install bundle/<name>` example
- `.planning/plans/active/19.6_community_registry_hub/user-stories/03-recommend-default-persona-pack-in-documentation.md` - reference only: source story defining scope and constraints (no edits from this AC)

## Happy Path Scenarios
**Scenario 1: New reader follows the walkthrough and installs the default pack first**
- **Given** a reader opens `docs/personas-install.md` and reaches the "Quick walkthrough" section
- **When** they read the numbered steps top to bottom
- **Then** a step near the top of the walkthrough (before or alongside the existing "Discover a persona" / "Install it" steps) explicitly recommends installing the default model-tuned persona pack, showing a runnable command in the form `atcr personas install bundle/<name>`

**Scenario 2: Recommendation uses the existing bundle install syntax**
- **Given** the new walkthrough step's example command
- **When** a reader copy-pastes it into a shell
- **Then** the command matches the already-documented `atcr personas install bundle/<name>` syntax described earlier in the same file (`docs/personas-install.md:51-61`), so it is runnable without modification (given the bundle exists in the registry)

## Edge Cases
**Edge Case 1: Stories 1-2 have not yet published a concrete bundle name at doc-edit time**
- **Given** this story is implemented before Stories 1-2 publish real persona/bundle names
- **When** the new walkthrough step is written
- **Then** the step uses clearly-generic placeholder language (e.g. "the recommended starter pack — see [releases] for the current bundle name") rather than a fabricated command that would 404 against the live registry, per the story's documented risk mitigation

**Edge Case 2: Existing numbered steps must not be renumbered out of their documented order**
- **Given** the "Quick walkthrough" section's existing 6 steps (search, install, list, test, upgrade, remove)
- **When** the new recommendation step is inserted
- **Then** the existing 6 steps retain their relative order and their commands are unchanged (only step numbers shift to accommodate the insertion, per the story's Constraint to be additive)

## Error Conditions
**Error Scenario 1: N/A — documentation-only change**
- Error message: N/A for docs-only change
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — documentation only.
- **Input Validation:** N/A.

## Test Implementation Guidance
**Test Type:** MANUAL
**Test Data Requirements:** Rendered `docs/personas-install.md` (GitHub markdown preview or local renderer) to visually confirm the new step reads naturally within the existing numbered walkthrough and the example command is syntactically valid per the file's own documented `install` syntax
**Mock/Stub Requirements:** None

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] "Quick walkthrough" section in `docs/personas-install.md` contains a new step recommending the default 3-persona pack
- [ ] The new step includes a concrete `atcr personas install bundle/<name>` example (or clearly-marked placeholder if Stories 1-2 haven't published names yet)
- [ ] The existing 6 walkthrough steps are preserved in their original order with unchanged commands
- [ ] `git diff docs/personas-install.md` shows only an additive insertion, no unrelated restructuring

**Manual Review:**
- [ ] Code reviewed and approved
