# Acceptance Criteria: README Quickstart Documentation for install.sh

**Related User Story:** [3: Install Script](../user-stories/03-install-script.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation | Edit to `README.md` Quickstart section (`README.md:36-57`) |
| Test Framework | Manual/markdown-lint review | No automated test framework for prose; verified via link-check and manual read-through |
| Key Dependencies | None | Pure documentation change, no build/runtime dependency |

### Related Files (from codebase-discovery.json)

- `README.md:40-57` — modify: Quickstart section (step "1. Install" at `README.md:40-42`) — add `install.sh` as a documented alternative/companion alongside the existing `go install` command
- `install.sh` — reference only: the script being documented (created/modified by AC 03-01 and AC 03-02)
- `.planning/plans/active/20.0_standalone_skill_release/documentation/install-script-conventions.md` — reference only: Quick Reference table confirms `README.md Quickstart` as the source of truth for the existing `go install` instruction that must not be removed or contradicted

## Design References

- [Install Script Conventions](../documentation/install-script-conventions.md) — scope guard and Quick Reference for how `install.sh` is presented in README.md

## Happy Path Scenarios
**Scenario 1: Quickstart documents both install paths**
- **Given** the current README.md Quickstart section shows only `go install github.com/samestrin/atcr/cmd/atcr@latest` as step 1 (`README.md:41-42`)
- **When** the documentation update is applied
- **Then** step 1 of the Quickstart section documents `install.sh` (e.g., `curl -fsSL <raw-file-url> | bash` or `./install.sh` after cloning) as an alternative/companion command, presented alongside — not replacing — the existing `go install` line

**Scenario 2: Existing go install instruction remains intact**
- **Given** the updated README.md Quickstart section
- **When** a reader reviews step 1
- **Then** the original `go install github.com/samestrin/atcr/cmd/atcr@latest` command is still present verbatim and functional, so users who prefer the manual path are unaffected

## Edge Cases
**Edge Case 1: Reader has no Go toolchain and reads only the install.sh instruction**
- **Given** a reader who follows only the new `install.sh` documentation without reading the surrounding prose
- **When** they run the script without Go installed
- **Then** the README text (or the script's own error output, per AC 03-02) makes clear that a Go toolchain is a prerequisite — the README should not imply `install.sh` bootstraps Go itself

**Edge Case 2: Numbered step sequence in Quickstart**
- **Given** the Quickstart section is a numbered 5-step list (`README.md:40-55`: Install, onboarding, doctor, review, report)
- **When** the `install.sh` mention is added to step 1
- **Then** the numbering and structure of subsequent steps (2-5) remain unchanged — the edit is additive within step 1, not a renumbering of the list

## Error Conditions
**Error Scenario 1: Documentation references a non-existent or incorrect path to install.sh**
- **Given** the README links to or names `install.sh`
- **When** the documentation is reviewed against the actual repo state
- **Then** the referenced path/command must resolve to the real `install.sh` at the repo root (e.g., a raw-content URL or relative `./install.sh` reference) — a broken or placeholder link is a documentation defect, not a runtime error, and must be caught in review rather than shipped
- Error message: N/A (documentation defect, not a runtime error condition)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation, no runtime performance characteristics
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** None
- **Input Validation:** If the README documents a `curl | bash` invocation pattern, the documentation should point at a specific, stable, verifiable source (the repo's own `install.sh` via a pinned/raw GitHub URL) rather than an unqualified or redirect-prone URL, so readers are not steered toward piping an untrusted or mutable script into `bash`

## Test Implementation Guidance
**Test Type:** E2E
**Test Data Requirements:** The final README.md content; no external test data needed
**Mock/Stub Requirements:** None — verify by rendering/reading the README and confirming (a) the new `install.sh` reference is present, (b) the link/URL cited resolves to the repo-root `install.sh` file, (c) the original `go install` line is unchanged

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors (markdown-lint, if configured, passes on README.md)
- [ ] Build succeeds (N/A for docs-only change; repo build unaffected)

**Story-Specific:**
- [ ] README.md Quickstart step 1 documents `install.sh` alongside the existing `go install` command
- [ ] Existing `go install github.com/samestrin/atcr/cmd/atcr@latest` instruction is preserved verbatim
- [ ] No renumbering or restructuring of the surrounding 5-step Quickstart list
- [ ] Any URL/path referencing `install.sh` resolves to the actual repo-root script

**Manual Review:**
- [ ] Code reviewed and approved
