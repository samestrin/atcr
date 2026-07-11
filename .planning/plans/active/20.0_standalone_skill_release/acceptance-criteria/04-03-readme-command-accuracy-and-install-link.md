# Acceptance Criteria: `README.md` Command Accuracy and `install.sh` Cross-Link

**Related User Story:** [4: Documentation Accuracy Pass](../user-stories/04-documentation-accuracy-pass.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation | `README.md` — top-level Quickstart, Commands table, and Documentation section |
| Test Framework | Manual cross-check against `cmd/atcr/main.go:185-208` and User Story 3's `install.sh` | No automated doc-linting harness in repo |
| Key Dependencies | User Story 3's `install.sh` (must exist at repo root before the cross-link can be added) | This story only links to `install.sh`; it does not author it |

## Related Files
- `README.md` - verify command accuracy and add one cross-link: Commands table (`README.md:75-93`), Quickstart section (`README.md:36-55`, read-only — no functional change permitted), Documentation section (`README.md:199-209`, add `install.sh` link here)
- `cmd/atcr/main.go:185-208` - read-only ground truth: canonical Cobra command tree (22 registered commands: `review`, `reconcile`, `verify`, `debate`, `report`, `github`, `range`, `status`, `init`, `quickstart`, `serve`, `doctor`, `trust`, `scorecard`, `leaderboard`, `benchmark`, `personas`, `models`, `debt`, `history`, `audit-report`, `version`)
- `install.sh` - read-only, produced by User Story 3: repo-root install script this story must cross-link from README.md's Documentation section

## Happy Path Scenarios
**Scenario 1: Commands table entries match the live Cobra command tree**
- **Given** `README.md:77-93` lists 15 commands with one-line purpose descriptions (`review`, `reconcile`, `verify`, `debate`, `report`, `range`, `status`, `init`, `quickstart`, `serve`, `doctor`, `history`, `trust`, `debt`, `audit-report`)
- **When** each listed command name is cross-checked against `cmd/atcr/main.go:185-208`'s registered command set
- **Then** every listed command exists in the live tree with a matching name (no stale or renamed entries); commands present in the tree but intentionally omitted from the README table (e.g., `github`, `scorecard`, `leaderboard`, `benchmark`, `personas`, `models`, `version`) are not flagged as drift unless the story determines they should be documented — this AC verifies no *false* entries, not full enumeration completeness

**Scenario 2: `install.sh` is cross-linked from the Documentation section without touching Quickstart**
- **Given** User Story 3 has landed `install.sh` at the repo root and `README.md:199-209`'s Documentation section links per-concern doc pages (`docs/providers.md`, `docs/registry.md`, etc.) following the plan's established doc-per-concern pattern
- **When** a link to `install.sh` (or an `install.sh` usage note) is added to the Documentation section, or as an alternate install option near the existing `go install` line (`README.md:42`) per the story's risk mitigation
- **Then** the link is added following the existing bullet-list pattern, and the Quickstart section's four numbered steps (`README.md:40-55`) remain functionally unchanged — no step is reordered, reworded beyond the addition, or replaced

## Edge Cases
**Edge Case 1: `go install` Quickstart path itself has drifted**
- **Given** `README.md:42` documents `go install github.com/samestrin/atcr/cmd/atcr@latest` as the install command and `README.md:41` requires "Go 1.24+"
- **When** cross-checked against the actual `go.mod` Go version directive and module path
- **Then** confirm the command and version requirement are still accurate; correct only if the module path or minimum Go version changed

**Edge Case 2: `install.sh` link is added inline to Quickstart instead of the Documentation section**
- **Given** the story's constraint explicitly warns against this exact mistake (Potential Risks: "`install.sh` cross-link gets added inline to README's Quickstart section... breaking the established doc-per-concern pattern")
- **When** the cross-link is placed
- **Then** it is added to the Documentation section (`README.md:199-209`) or immediately adjacent to the existing `go install` line as an alternate option — never inserted as a new numbered step inside the Quickstart's `bash` code block

## Error Conditions
**Error Scenario 1: `install.sh` does not exist yet when this story executes**
- **Given** this story's Dependencies field lists User Story 3 (Install Script) as a prerequisite
- **When** this story's cross-link step is attempted before `install.sh` exists at the repo root
- **Then** the cross-link addition is deferred/blocked until User Story 3 lands — no link is added to a nonexistent file (verified via `test -f install.sh` before editing README.md)
- Error message: N/A (sequencing dependency, not a runtime error; enforced by story execution order, not code)
- HTTP status / error code: N/A (documentation-only story, no service boundary)

**Error Scenario 2: A Commands table entry references a removed or renamed command**
- **Given** a command listed in `README.md:77-93` no longer appears in `cmd/atcr/main.go:185-208` (e.g., renamed or removed since the doc was last updated)
- **When** this drift is detected during the cross-check
- **Then** the stale row is corrected (renamed) or removed to match the live command tree — the table must never document a command that does not exist
- Error message: N/A (documentation-accuracy correction, not a runtime error)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation verification, not a runtime code path
- **Throughput:** N/A — single-pass manual cross-check of one file against the command tree and `install.sh`'s presence

## Security Considerations
- **Authentication/Authorization:** N/A — no auth surface; documentation-only change
- **Input Validation:** N/A — no user input; verification confirms doc prose and links, not runtime validation logic

## Test Implementation Guidance
**Test Type:** MANUAL (documentation cross-check; no automated test asserts README prose or link presence)
**Test Data Requirements:** Current `README.md`, `cmd/atcr/main.go:185-208`, and confirmation of `install.sh`'s existence/location post-User-Story-3
**Mock/Stub Requirements:** None — direct file comparison; optionally run `go build -o bin/atcr ./cmd/atcr && ./bin/atcr --help` to enumerate live commands empirically before comparing to the README table

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./...` remains green — confirms no code was touched by this doc-only story)
- [ ] No linting errors (repo has no markdown linter configured; N/A)
- [ ] Build succeeds (`go build -o bin/atcr ./cmd/atcr`)

**Story-Specific:**
- [ ] Every Commands table entry in `README.md:77-93` verified against `cmd/atcr/main.go:185-208` (no stale/renamed entries)
- [ ] `install.sh` link added to README.md's Documentation section (or as an alternate option beside the `go install` line), only after confirming `install.sh` exists post-User-Story-3
- [ ] Quickstart section (`README.md:36-55`) has zero functional changes — steps unchanged beyond the permitted cross-link addition
- [ ] No new numbered step inserted into the Quickstart `bash` code block

**Manual Review:**
- [ ] Code reviewed and approved
