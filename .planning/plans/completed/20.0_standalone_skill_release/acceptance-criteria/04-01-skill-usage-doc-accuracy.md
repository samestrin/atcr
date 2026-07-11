# Acceptance Criteria: `docs/skill-usage.md` Accuracy Against the Dispatcher Rewrite

**Related User Story:** [4: Documentation Accuracy Pass](../user-stories/04-documentation-accuracy-pass.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation | `docs/skill-usage.md` — installation + usage guide for `skill/SKILL.md` |
| Test Framework | Manual line-by-line cross-check (no automated doc-linting harness in repo) | Ground truth is the post-Story-1 `skill/SKILL.md` frontmatter/command surface, not memory of prior doc content |
| Key Dependencies | None (documentation-only; no build tooling required to verify) | Verification reads `skill/SKILL.md` and `docs/skill-usage.md` side by side |

### Related Files (from codebase-discovery.json)

- `docs/skill-usage.md` — verify, correct only if drifted: installation steps, usage input table, orchestration step list, and the `.atcr/reviews/<id>/` output section
- `skill/SKILL.md` — read-only ground truth: post-User-Story-1 dispatcher rewrite (`/atcr <command> <flags>` routing); currently (pre-Story-1) a linear review-only script per `skill/SKILL.md:33-51` ("Orchestration Steps")
- `cmd/atcr/main.go:185-208` — read-only ground truth: canonical Cobra command tree (`newReviewCmd`, `newReconcileCmd`, `newVerifyCmd`, `newDebateCmd`, `newReportCmd`, `newGithubCmd`, `newRangeCmd`, `newStatusCmd`, `newInitCmd`, `newQuickstartCmd`, `newServeCmd`, `newDoctorCmd`, `newTrustCmd`, `newScorecardCmd`, `newLeaderboardCmd`, `newBenchmarkCmd`, `newPersonasCmd`, `newModelsCmd`, `newDebtCmd`, `newHistoryCmd`, `newAuditReportCmd`, `newVersionCmd`)

## Design References

- [CLI Dispatcher Conventions](../documentation/cli-dispatcher-conventions.md) — canonical command inventory to cross-check `docs/skill-usage.md` examples
- [Agent Skill Format & Progressive Disclosure](../documentation/agent-skill-format.md) — installation/secondary-file implications after the dispatcher rewrite

## Happy Path Scenarios
**Scenario 1: Post-rewrite command examples match the dispatcher exactly**
- **Given** User Story 1 has rewritten `skill/SKILL.md` into a `/atcr <command> <flags>` dispatcher and `docs/skill-usage.md`'s "Usage" section (currently `docs/skill-usage.md:24-33`) describes how to invoke the skill
- **When** every command name and flag referenced in `docs/skill-usage.md` is cross-checked against the rewritten `skill/SKILL.md` and `cmd/atcr/main.go:185-208`
- **Then** every reference matches exactly (no renamed, invented, or stale command/flag names), and no edit is required if the section is already accurate

**Scenario 2: `.atcr/reviews/<id>/` output description remains true**
- **Given** `docs/skill-usage.md:44-52` ("Output") states everything lands under `.atcr/reviews/<id>/` with `payload/`, `sources/pool/`, `sources/host/`, and `reconciled/report.md`
- **When** this statement is checked against the rewritten `skill/SKILL.md`'s output-path behavior
- **Then** the statement is confirmed still accurate post-rewrite (the plan's AC2 requirement), since the dispatcher rewrite changes command routing, not the artifact storage location

## Edge Cases
**Edge Case 1: Installation steps reference a skill file path that moved**
- **Given** `docs/skill-usage.md:13-22` documents installing via `cp skill/SKILL.md .claude/skills/atcr/SKILL.md`
- **When** verified against the actual post-rewrite location of `skill/SKILL.md` (unchanged by Story 1, which edits content, not file location)
- **Then** confirm the path is still `skill/SKILL.md` with no correction needed

**Edge Case 2: A drifted command name is found**
- **Given** the dispatcher rewrite renames or restructures a step (e.g., changes how `atcr range`/`atcr review`/`atcr status`/`atcr reconcile`/`atcr report` are invoked from the skill)
- **When** the corresponding step in `docs/skill-usage.md`'s "The skill then:" numbered list (`docs/skill-usage.md:35-43`) no longer matches
- **Then** only the drifted line is corrected in place — the numbered list structure and surrounding prose are not rewritten or reworded beyond the fix

## Error Conditions
**Error Scenario 1: Drift is found but the fix requires touching a file outside this story's write scope**
- **Given** a discrepancy is traced to `skill/SKILL.md` itself (e.g., the skill doc references a command the dispatcher does not actually implement)
- **When** this story's write scope is limited to `docs/skill-usage.md`, `docs/code-review-backend.md`, and `README.md` per the story's Integration Points constraint
- **Then** the discrepancy is not silently ignored or fixed by editing `skill/SKILL.md` (out of scope); it is flagged for User Story 1 rather than papered over in the doc
- Error message: N/A (not a runtime error path; this is a process/scope guard for the verification pass)
- HTTP status / error code: N/A (documentation-only story, no service boundary)

## Performance Requirements
- **Response Time:** N/A — static documentation verification, not a runtime code path
- **Throughput:** N/A — single-pass manual cross-check of one file against two ground-truth sources

## Security Considerations
- **Authentication/Authorization:** N/A — no auth surface; documentation-only change
- **Input Validation:** N/A — no user input; verification confirms doc prose accuracy, not code behavior

## Test Implementation Guidance
**Test Type:** MANUAL (documentation cross-check; no automated test asserts prose accuracy)
**Test Data Requirements:** Current contents of `skill/SKILL.md` (post-Story-1), `cmd/atcr/main.go:185-208`, and `docs/skill-usage.md`
**Mock/Stub Requirements:** None — direct file comparison, no runtime execution required

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (no test suite touches this doc; confirm `go test ./...` remains green — no regression introduced by any doc edit)
- [ ] No linting errors (repo has no markdown linter configured; N/A)
- [ ] Build succeeds (`go build -o bin/atcr ./cmd/atcr` — confirms doc edits did not touch code)

**Story-Specific:**
- [ ] Every command/flag reference in `docs/skill-usage.md` verified against post-rewrite `skill/SKILL.md` and `cmd/atcr/main.go:185-208`
- [ ] `.atcr/reviews/<id>/` output-location statement in `docs/skill-usage.md:44-52` confirmed still accurate (or corrected)
- [ ] Any correction made is limited to the specific drifted line(s); no unrelated restructuring or rewording

**Manual Review:**
- [ ] Code reviewed and approved
