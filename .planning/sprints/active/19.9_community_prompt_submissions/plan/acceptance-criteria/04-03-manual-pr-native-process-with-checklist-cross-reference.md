# Acceptance Criteria: Manual PR-Native Process with Contribution Checklist Cross-Reference

**Related User Story:** [04: Maintainer Graduation into the Vetted Library](../user-stories/04-maintainer-graduation-into-vetted-library.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (no new code) | Same "Graduating a submitted persona" section in `docs/personas-authoring.md`; this AC covers its process-boundary statement and its cross-link from `## 4. Contribution checklist` |
| Test Framework | None (docs-only) | No CLI command, API endpoint, or automated script is introduced or tested; verification is documentation accuracy |
| Key Dependencies | Epic 19.6's existing human-review PR-merge gate (reference only); `gh` PR review/merge workflow | No new GitHub App, webhook, bot, or hosted approval surface |

### Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md` (modify) — add a statement in the graduation section confirming the entire procedure (review, requested changes, approve, merge/edit) is performed through the existing GitHub PR-review workflow, with an explicit "not introduced" list (no new CLI command, no ranking/approval UI, no hosted registry surface); also add a cross-reference link/pointer from `## 4. Contribution checklist` (docs/personas-authoring.md:162-177) to the new graduation section so a maintainer reviewing a `submitted` PR against the checklist can find the graduation steps
- `cmd/atcr/personas.go` (reference only, no change) — confirms no new subcommand (e.g. `personas graduate`) is added alongside existing `newPersonasSubmitCmd()`/`newPersonasTestCmd()`/`newPersonasRemoveCmd()`, consistent with the "no new CLI command" constraint
- `.planning/plans/active/19.9_community_prompt_submissions/user-stories/03-submitted-status-distinct-from-source.md` (reference only) — cited to confirm the graduation section's cross-reference correctly points readers from the `submitted`-marker concept (Story 3) to its resolution (Story 4's graduation procedure)

## Design References
- [Personas Install & Authoring Doc Updates (AC4)](../documentation/personas-docs-updates.md) — the graduation section's process-boundary language and contribution-checklist cross-reference

## Happy Path Scenarios
**Scenario 1: Maintainer discovers graduation steps from the contribution checklist**
- **Given** a maintainer is reviewing the existing `## 4. Contribution checklist` in `docs/personas-authoring.md` while triaging a `submitted` PR
- **When** they reach the checklist item(s) relevant to a `submitted` persona (e.g. the index-entry checklist item at docs/personas-authoring.md:177)
- **Then** a cross-reference (relative link or explicit pointer) directs them to the "Graduating a submitted persona" section for the full maintainer procedure

**Scenario 2: Graduation documented as PR-native with no new tooling**
- **Given** a maintainer reads the graduation section end-to-end
- **When** they look for how graduation is actually performed
- **Then** the section states graduation happens entirely via GitHub's existing PR review, requested-changes, approve, and merge/edit actions — reusing Epic 19.6's established human-review gate — and explicitly lists what is *not* introduced: no new CLI command, no automated promotion script, no ranking/approval UI, no hosted registry surface

## Edge Cases
**Edge Case 1: Maintainer wants to edit files directly on the PR branch before merge**
- **Given** GitHub's PR UI/CLI (`gh pr checkout`) allows a maintainer to check out the PR branch and commit the persona-move + index-entry + marker-clearing edits directly
- **When** the documentation describes this option
- **Then** it is presented as an equally valid path to performing the edits as review-comment-requested-changes, since both are "ordinary git operations a maintainer performs while merging/editing the PR" (per the story's Constraints) — no distinct tooling is implied for either path

**Edge Case 2: A future contributor proposes an automated graduation script**
- **Given** the documentation is read by a contributor considering automation for the graduation step
- **When** they consult the graduation section
- **Then** the explicit "not introduced" list (no new CLI command, no automated promotion pathway, no ranking system, no hosted approval surface) is prominent enough to signal this is an intentional scope boundary, not an oversight, discouraging an out-of-scope PR

## Error Conditions
**Error Scenario 1: Cross-reference link is broken or missing**
- Error message: N/A — this is a documentation completeness check, not a runtime error; verification is a manual (or markdown-link-check tooling, if already used elsewhere in the repo) confirmation that the relative link from `## 4. Contribution checklist` to the graduation section resolves correctly within `docs/personas-authoring.md`
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — documentation artifact; no runtime path.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** None beyond the maintainer's pre-existing repo write/merge permissions used by the standard PR workflow; this AC introduces no new auth surface, consistent with "no hosted approval surface."
- **Input Validation:** N/A — no new input-parsing code path is introduced; the documentation only describes existing `git`/`gh` operations.

## Test Implementation Guidance
**Test Type:** MANUAL (documentation review)
**Test Data Requirements:** The rendered `docs/personas-authoring.md` file, reviewed end-to-end to confirm (a) the graduation section appears, (b) the "not introduced" boundary list is present and matches the story's Constraints verbatim in spirit, and (c) the cross-reference link from the contribution checklist section resolves to the graduation section.
**Mock/Stub Requirements:** None — no code, mock, or automated test harness is required; this is a documentation-accuracy verification, matching the story's Implementation Notes ("verification is that the doc accurately describes the existing schema and process").

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (no test suite changes introduced by this AC; pre-existing suite remains green)
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Graduation section explicitly states the procedure is performed entirely through the existing GitHub PR-review-and-merge workflow, reusing Epic 19.6's human-review gate
- [x] Graduation section explicitly lists what is not introduced: no new CLI command, no automated promotion script, no ranking/approval UI, no hosted registry surface
- [x] `## 4. Contribution checklist` contains a working cross-reference (link or explicit pointer) to the new graduation section

**Manual Review:**
- [ ] Code reviewed and approved
