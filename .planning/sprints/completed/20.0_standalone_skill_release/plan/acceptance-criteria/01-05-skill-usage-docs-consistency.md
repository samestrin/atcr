# Acceptance Criteria: `docs/skill-usage.md` Consistency With the Dispatcher Rewrite

**Related User Story:** [01: Dispatcher Skill Rewrite](../user-stories/01-dispatcher-skill-rewrite.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation | `docs/skill-usage.md` |
| Test Framework | Manual cross-reference review (no automated doc test currently exists) | consider a lightweight grep-based check if time allows |
| Key Dependencies | `skill/SKILL.md` (source of truth for behavior), `docs/findings-format.md`, `docs/providers.md`, `docs/code-review-backend.md` | linked from skill-usage.md line 52-54 |

### Related Files (from codebase-discovery.json)

- `docs/skill-usage.md` — modify (if needed): Installation section (lines 13-22) and Usage section (lines 24-42) must accurately describe the post-rewrite dispatcher, including installing the full `skill/` directory (per AC 01-03 Edge Case 2) if the secondary-file split requires more than a single-file copy
- `skill/SKILL.md` — reference only: the rewritten dispatcher is the behavioral source of truth this doc must match
- `cmd/atcr/main.go:185-208` — reference only: canonical Cobra command tree; any command example in `docs/skill-usage.md` must match these names
- `.atcr/reviews/<id>/` artifact layout (`payload/`, `sources/pool/`, `sources/host/`, `reconciled/`) — reference only: unchanged per the story's Data Requirements; `docs/skill-usage.md` Output section (lines 44-52) must continue to match this layout exactly

## Design References

- [CLI Dispatcher Conventions](../documentation/cli-dispatcher-conventions.md) — command/subcommand conventions the Usage section must reflect
- [Agent Skill Format & Progressive Disclosure](../documentation/agent-skill-format.md) — installation implications of the secondary-file split

## Happy Path Scenarios
**Scenario 1: Installation instructions match the post-rewrite file set**
- **Given** the dispatcher rewrite adds `skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md` alongside `skill/SKILL.md`
- **When** `docs/skill-usage.md`'s Installation section is reviewed
- **Then** it instructs copying all necessary files (the full `skill/` directory or an explicit file list) into the agent's skills directory, not `SKILL.md` in isolation — otherwise a copy-only install per the current instructions silently breaks on-demand secondary-file loading

**Scenario 2: Usage section still describes the dispatcher's routed behavior accurately**
- **Given** the rewritten dispatcher supports the full `/atcr <command>` surface, with the review flow as one routable path
- **When** `docs/skill-usage.md`'s Usage section (currently framed only around the review→reconcile→report flow) is reviewed
- **Then** it is updated (if it does not already) to note that the skill is a general dispatcher and the described 6-step flow is the behavior for the review command path specifically — no stale claim that review is the skill's *only* capability

**Scenario 3: Output/artifact layout claim remains accurate**
- **Given** the story's explicit constraint that `.atcr/reviews/<id>/` layout is unchanged
- **When** `docs/skill-usage.md`'s Output section is compared against the rewritten `skill/SKILL.md`
- **Then** the documented layout (`payload/`, `sources/pool/`, `sources/host/`, `reconciled/report.md`, `findings.txt`, `findings.json`, `summary.json`, `ambiguous.json`) still matches exactly — this AC verifies, and updates if needed, per the story's Measurable criterion referencing AC2

## Edge Cases
**Edge Case 1: No update is actually needed**
- **Given** the dispatcher rewrite preserves the review flow's behavior and output layout unchanged (per AC 01-02)
- **When** `docs/skill-usage.md` is reviewed against the rewritten SKILL.md
- **Then** it is acceptable for this AC to conclude "no changes required" — the story's own wording is "verified (and updated if needed)" — but the verification step itself (an explicit read-through comparison) must still be performed and is not optional

**Edge Case 2: Doc cross-references remain valid**
- **Given** `docs/skill-usage.md` links to `findings-format.md`, `providers.md`, and `code-review-backend.md` (line 52-54)
- **When** the dispatcher rewrite is complete
- **Then** these cross-references still resolve and remain accurate — none of them describe behavior that the rewrite has changed

## Error Conditions
**Error Scenario 1: Stale installation instructions**
- **Given** `docs/skill-usage.md` still instructs `cp skill/SKILL.md .claude/skills/atcr/SKILL.md` only, after the secondary-file split lands
- **Then** this is a documentation defect: a fresh installer following the doc literally gets a broken on-demand-load skill (missing `host-review.md` etc.) — must be caught and fixed as part of this AC, not deferred

**Error Scenario 2: Output layout claim diverges from actual behavior**
- **Given** any change to the `.atcr/reviews/<id>/` layout description that no longer matches reality
- **Then** flagged as a doc-accuracy defect against the story's Data Requirements constraint ("Artifact layout ... is unchanged and must continue to match `docs/skill-usage.md`")

## Performance Requirements
- **Response Time:** N/A — documentation-only change.
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — `docs/skill-usage.md`'s Prerequisites section (gh CLI auth, registry/config setup) must remain accurate but is not itself changing.
- **Input Validation:** N/A

## Test Implementation Guidance
**Test Type:** MANUAL (documentation cross-reference review); optionally a lightweight script/grep check that every file path mentioned in `docs/skill-usage.md`'s Installation section exists under `skill/`
**Test Data Requirements:** Final rewritten `skill/SKILL.md` plus the three new secondary files (dependency on AC 01-01 through 01-04 being complete first)
**Mock/Stub Requirements:** None

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Installation section instructs copying all files needed for on-demand secondary-file loading to work
- [ ] Usage section accurately frames the review flow as one routable path of a general dispatcher
- [ ] Output/artifact layout section verified to still match the rewritten `skill/SKILL.md` and the unchanged `.atcr/reviews/<id>/` structure
- [ ] All cross-referenced doc links (`findings-format.md`, `providers.md`, `code-review-backend.md`) still resolve and remain accurate

**Manual Review:**
- [ ] Code reviewed and approved
