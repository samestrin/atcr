# Acceptance Criteria: Frontmatter Validity and SKILL.md Line-Budget Constraints

**Related User Story:** [01: Dispatcher Skill Rewrite](../user-stories/01-dispatcher-skill-rewrite.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | YAML frontmatter validation + Markdown line-count constraint | `skill/SKILL.md` |
| Test Framework | Go `testing` + `testify` (regex assertions) | `skill/skill_test.go` |
| Key Dependencies | Agent Skill format spec (name/description constraints) | no external package |

## Related Files
- `skill/SKILL.md` - modify: frontmatter `name` and `description` fields, and overall body length after adding the command-routing table (AC 01-01) and secondary-file pointers (AC 01-03)
- `skill/skill_test.go` - modify: `TestSkill_Frontmatter` (lines 16-24) currently only checks `name` starts with `atcr` and `description` is non-empty — extend with explicit length/charset assertions (`name` ≤64 chars, lowercase/numbers/hyphens only, no "anthropic"/"claude" substring; `description` ≤1024 chars) and add a new line-count assertion (SKILL.md body under ~500 lines) so the constraint is enforced automatically rather than only checked manually

## Happy Path Scenarios
**Scenario 1: Frontmatter passes all format constraints**
- **Given** the rewritten `skill/SKILL.md` frontmatter
- **When** `name` and `description` are validated
- **Then** `name` is `atcr` (lowercase, hyphens/numbers only, ≤64 chars, does not contain "anthropic" or "claude"), and `description` is ≤1024 chars and describes the dispatcher pattern (per AC 01-01, Scenario 3)

**Scenario 2: Body stays within budget after the routing-table addition**
- **Given** the rewritten `skill/SKILL.md` with the command-routing table for ~20 top-level commands and pointers to three secondary files
- **When** the file's line count is measured
- **Then** it is under ~500 lines total (the story's Measurable success criterion), achieved by keeping per-command entries to one line each in Level 2 and deferring detail to Level 3

## Edge Cases
**Edge Case 1: Description length pressure from enumerating all commands**
- **Given** the temptation to list all 21 command names/descriptions in the frontmatter `description` field for discoverability
- **When** the description is drafted
- **Then** it stays under 1024 chars by summarizing the dispatcher capability at a high level (e.g. "review, reconcile, verify, debate, report, and the rest of the atcr CLI surface") rather than exhaustively enumerating every command with its full description

**Edge Case 2: Body creeps toward the 500-line budget as commands are added**
- **Given** a future command is added to `newRootCmd` and the routing table grows
- **Then** the per-command entry convention (one line each, Level 2) must be followed for the new entry too, rather than proportionally growing SKILL.md — this is a documented convention, not an automated gate, but should be noted inline (e.g. a comment or convention note) so future edits don't silently regress the budget

## Error Conditions
**Error Scenario 1: Invalid `name` field**
- **Given** `name` contains uppercase letters, underscores, or the substring "claude"/"anthropic", or exceeds 64 chars
- **Then** this fails Agent Skill format validation — `TestSkill_Frontmatter` must fail with a clear assertion message identifying which constraint was violated

**Error Scenario 2: `description` exceeds 1024 chars**
- **Given** `description` length > 1024 characters
- **Then** `TestSkill_Frontmatter` fails with an assertion on `len(description) <= 1024`

**Error Scenario 3: Body exceeds ~500 lines**
- **Given** `skill/SKILL.md` line count exceeds ~500 after the rewrite
- **Then** this fails the story's Measurable success criterion; the new line-count test in `skill_test.go` fails with a message reporting the actual vs. budgeted line count

## Performance Requirements
- **Response Time:** N/A — static validation only.
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** Frontmatter charset/length constraints are themselves the "input validation" surface here — enforced by regex assertions in `skill_test.go`, matching Claude Code's own Agent Skill loader constraints so the skill is guaranteed loadable.

## Test Implementation Guidance
**Test Type:** UNIT (Go `testing`, regex + length assertions over the embedded `SkillMD` string)
**Test Data Requirements:** None beyond the embedded constant.
**Mock/Stub Requirements:** None.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./skill/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `name` is ≤64 chars, lowercase/numbers/hyphens only, and excludes "anthropic"/"claude"
- [ ] `description` is ≤1024 chars
- [ ] `skill/SKILL.md` body is under ~500 lines
- [ ] `skill_test.go` gained automated assertions for all three constraints above

**Manual Review:**
- [ ] Code reviewed and approved
