# Acceptance Criteria: skill/SKILL.md Prerequisites Section Rewritten to Point to CONVENTIONS.md

**Related User Story:** [04: Shared Skill Conventions Extraction](../user-stories/04-shared-skill-conventions-extraction.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation edit (Agent Skill dispatcher file) | Edits `skill/SKILL.md`'s existing "## Prerequisites" section in place |
| Test Framework | Go `testing` + `stretchr/testify` | `skill/skill_test.go`'s `TestSkill_RequiredSections`, `TestSkill_NoAbsoluteOrClaudePaths`, and a new pointer-presence assertion mirroring `TestSkill_SecondaryFilePointers` |
| Key Dependencies | None | Depends on AC 04-01 (`skill/CONVENTIONS.md` must exist as the pointer target) |

### Related Files (from codebase-discovery.json)
- `skill/SKILL.md` — modify: "## Prerequisites" section (skill/SKILL.md:18-23) rewritten from inline checks to a short pointer sentence referencing `skill/CONVENTIONS.md`
- `skill/CONVENTIONS.md` — reference: the pointer target created in AC 04-01
- `skill/skill_test.go` — reference: `TestSkill_RequiredSections` (skill/skill_test.go:26) asserts the `## Prerequisites` heading still exists; `TestSkill_SecondaryFilePointers` (skill/skill_test.go:172) is the existing pattern for pointer-presence assertions this AC's new test mirrors
- `skill/host-review.md` — reference (read-only): on-demand pointer pattern already used in SKILL.md
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/agent-skills-format.md` — reference: progressive-disclosure and on-demand-reference conventions

## Happy Path Scenarios
**Scenario 1: Prerequisites heading remains, body becomes a pointer**
- **Given** `skill/SKILL.md`'s "## Prerequisites" section currently inlines the binary-on-PATH check, git-worktree check, and `gh` CLI note
- **When** the section is rewritten
- **Then** the `## Prerequisites` heading is unchanged (so `TestSkill_RequiredSections` continues to find it) and the body is replaced with a short pointer sentence directing the reader to load `CONVENTIONS.md`

**Scenario 2: Pointer style mirrors "Host Review Instructions"**
- **Given** "## Host Review Instructions" (skill/SKILL.md:89-91) already demonstrates the on-demand-reference pattern: a short paragraph plus "load `host-review.md` on demand" rather than inlining host-review content
- **When** the Prerequisites section is rewritten
- **Then** it follows the identical phrasing pattern — a short paragraph plus a backtick-quoted `` `CONVENTIONS.md` `` reference, loaded on demand rather than inlined

**Scenario 3: No coverage lost**
- **Given** the original three checks (binary-on-PATH, git-worktree, `gh` CLI note) were enforced by `skill/SKILL.md` directly
- **When** the rewrite lands
- **Then** all three checks remain discoverable and enforceable — now via `skill/CONVENTIONS.md` referenced from the pointer — with no net loss of behavior for an agent following the skill

## Edge Cases
**Edge Case 1: Required-sections test continues passing unmodified**
- **Given** `TestSkill_RequiredSections` (skill/skill_test.go:26-37) checks for the literal string `## Prerequisites` in `SkillMD`
- **When** the Prerequisites body is rewritten
- **Then** the `## Prerequisites` heading string is preserved exactly, so the existing test passes without modification

**Edge Case 2: Dispatcher routing table and command list unaffected**
- **Given** `dispatcherCommands` (skill/skill_test.go:133) and `TestSkill_DispatcherRoutingTable` (skill/skill_test.go:144) assert routing-table coverage over CLI commands, not the Prerequisites section
- **When** the Prerequisites section is rewritten
- **Then** no entry is added to `dispatcherCommands` (per the story's explicit note: this is a shared doc file, not a CLI command) and `TestSkill_DispatcherRoutingTable` continues passing unmodified

**Edge Case 3: Body line budget preserved**
- **Given** `TestSkill_BodyLineBudget` (skill/skill_test.go:236) caps `SkillMD` at ~500 lines
- **When** the Prerequisites section shrinks from three inline bullets to a short pointer sentence
- **Then** `SkillMD`'s total line count decreases (net-negative), keeping ample margin under the 500-line budget

## Error Conditions
**Error Scenario 1: Prerequisites text duplicated instead of pointed-to**
- Condition: a maintainer leaves the original inline checks in `SKILL.md` in addition to adding a `CONVENTIONS.md` reference (duplication rather than extraction)
- Detection: a new assertion (added in AC 04-03's test coverage) that `SkillMD` references `` `CONVENTIONS.md` `` but does not contain the full original halt-message text verbatim (e.g. asserting `SkillMD` does NOT contain `Not a git repository. Run the skill from within a git working tree.` once that text has moved to `ConventionsMD`)
- Error message (test failure): assertion failure indicating the Prerequisites section still contains duplicated text instead of a pointer

**Error Scenario 2: Pointer references a non-existent or misnamed file**
- Condition: `SKILL.md` references `CONVENTIONS.md` by a different filename or path than the one actually created/embedded
- Detection: `TestSkill_SecondaryFilesVerbatim`-style test fails because `ConventionsMD` (from AC 04-03) is empty or the embed path in `skill.go` does not match; alternatively a new `TestSkill_SecondaryFilePointers`-style assertion fails because the exact backtick-quoted string `` `CONVENTIONS.md` `` is absent from `SkillMD`
- Error message (test failure): `SKILL.md must point to secondary file` `CONVENTIONS.md``

## Performance Requirements
- **Response Time:** N/A — static Markdown edit, no runtime execution path
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — documentation only
- **Input Validation:** N/A — no user input processed by this change

## Test Implementation Guidance
**Test Type:** UNIT (Go `testing` over the embedded `SkillMD` string; string-presence and string-absence assertions)
**Test Data Requirements:** The rewritten `skill/SKILL.md` Prerequisites section text; the exact halt-message substrings previously inline, to assert their absence post-extraction
**Mock/Stub Requirements:** None — pure string-content assertions over embedded constants, no I/O

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./skill/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./skill/...`)

**Story-Specific:**
- [ ] `skill/SKILL.md`'s `## Prerequisites` heading is unchanged; `TestSkill_RequiredSections` passes without modification
- [ ] The Prerequisites body is a short pointer sentence referencing `` `CONVENTIONS.md` ``, not the original inline checks
- [ ] `SkillMD` no longer contains the full duplicated original halt-message text (moved to `ConventionsMD`)
- [ ] `dispatcherCommands` and `TestSkill_DispatcherRoutingTable` remain unmodified and passing

**Manual Review:**
- [ ] Code reviewed and approved
