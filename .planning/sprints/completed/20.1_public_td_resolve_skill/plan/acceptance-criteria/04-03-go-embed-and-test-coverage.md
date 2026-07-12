# Acceptance Criteria: skill/CONVENTIONS.md Embedded in skill.go as ConventionsMD with Test Coverage

**Related User Story:** [04: Shared Skill Conventions Extraction](../user-stories/04-shared-skill-conventions-extraction.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go `//go:embed` build-time embed constant | `skill/skill.go`, mirroring the existing `HostReviewMD`/`AmbiguityAdjudicationMD`/`FindingsFormatMD` pattern |
| Test Framework | Go `testing` + `stretchr/testify` (`assert`/`require`) | `skill/skill_test.go` |
| Key Dependencies | Go standard library `embed` package (already imported as `_ "embed"` in skill.go) | No new dependency |

### Related Files (from codebase-discovery.json)
- `skill/skill.go` — modify: add `//go:embed CONVENTIONS.md` directive and `var ConventionsMD string`, alongside the existing three embedded secondary files (skill/skill.go:20-36); update the package doc comment's list of secondary files (skill/skill.go:6-10)
- `skill/skill_test.go` — modify: add `ConventionsMD` to the `TestSkill_NoAbsoluteOrClaudePaths` iteration list (skill/skill_test.go:114); add new assertions for non-empty content, pointer-not-duplication, and (optionally) a `TestSkill_SecondaryFilesVerbatim`-style anchor check for `ConventionsMD`
- `skill/CONVENTIONS.md` — reference: the file being embedded (created in AC 04-01)
- `skill/SKILL.md` — reference (read-only): Prerequisites pointer source that must reference `CONVENTIONS.md`
- `skill/host-review.md` — reference (read-only): existing secondary embed pattern to mirror
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/agent-skills-format.md` — reference: Go embed harness conventions for secondary skill files

## Happy Path Scenarios
**Scenario 1: ConventionsMD embed constant added**
- **Given** `skill/skill.go` currently embeds `SkillMD`, `HostReviewMD`, `AmbiguityAdjudicationMD`, and `FindingsFormatMD` via `//go:embed` directives (skill/skill.go:14-36)
- **When** `skill/CONVENTIONS.md` is finalized (AC 04-01)
- **Then** `skill/skill.go` gains a `//go:embed CONVENTIONS.md` directive and `var ConventionsMD string`, following the exact same doc-comment-plus-directive-plus-var pattern as the other three secondary files

**Scenario 2: ConventionsMD is non-empty and covered by tests**
- **Given** `TestSkill_FileExistsAndNonEmpty`-style assertions exist for `SkillMD` (skill/skill_test.go:12) and `TestSkill_SecondaryFilesVerbatim` (skill/skill_test.go:182) requires non-empty content for the other three secondary files
- **When** the new embed lands
- **Then** a new or extended test asserts `require.NotEmpty(t, ConventionsMD, ...)`, matching the existing pattern for the other embedded files

**Scenario 3: SKILL.md references, does not duplicate, Prerequisites text**
- **Given** AC 04-02 rewrites `SKILL.md`'s Prerequisites section into a pointer
- **When** the new test coverage is added
- **Then** a test asserts `SkillMD` contains the backtick-quoted reference `` `CONVENTIONS.md` `` (mirroring `TestSkill_SecondaryFilePointers`, skill/skill_test.go:172) and does not contain the full duplicated Prerequisites text that now lives solely in `ConventionsMD`

**Scenario 4: ConventionsMD included in the no-`.claude`/no-absolute-path check**
- **Given** `TestSkill_NoAbsoluteOrClaudePaths` (skill/skill_test.go:113-120) iterates a fixed list `[]string{SkillMD, HostReviewMD, AmbiguityAdjudicationMD, FindingsFormatMD}`
- **When** the new embed lands
- **Then** the list is updated to `[]string{SkillMD, HostReviewMD, AmbiguityAdjudicationMD, FindingsFormatMD, ConventionsMD}` so `ConventionsMD` is held to the identical no-`.claude`/no-absolute-path bar as the existing three files

## Edge Cases
**Edge Case 1: Package doc comment stays accurate**
- **Given** `skill/skill.go`'s package doc comment (skill/skill.go:6-10) explicitly lists the secondary files (`host-review.md, ambiguity-adjudication.md, findings-format.md`)
- **When** `CONVENTIONS.md` is added as a fourth secondary file
- **Then** the doc comment's list is updated to include `CONVENTIONS.md`, so the comment does not silently drift out of sync with the actual embeds (this is documentation only — no test enforces the comment text, so it is a manual-review item)

**Edge Case 2: `dispatcherCommands` list untouched**
- **Given** the story explicitly states no new `dispatcherCommands` entry is needed since `CONVENTIONS.md` is a shared doc file, not a CLI command
- **When** the embed and tests are added
- **Then** `dispatcherCommands` (skill/skill_test.go:133-138) is not modified, and `TestSkill_DispatcherRoutingTable` continues to pass unmodified

**Edge Case 3: Existing three-file tests remain green**
- **Given** `TestSkill_SecondaryFilesVerbatim` (skill/skill_test.go:182-214) currently iterates a `cases` slice covering exactly `host-review.md`, `ambiguity-adjudication.md`, `findings-format.md`
- **When** `ConventionsMD` coverage is added
- **Then** it is added as a new case entry (or a separate new test function) without altering the existing three cases' anchors or assertions — additive only, per the story's Constraints section

## Error Conditions
**Error Scenario 1: `go:embed` directive path mismatch**
- Condition: the `//go:embed` directive references a filename that does not match the actual file (e.g. case mismatch, wrong extension, or the file not yet created)
- Detection: `go build ./skill/...` fails at compile time with a Go embed error (e.g. `pattern CONVENTIONS.md: no matching files found`)
- Error message: Go toolchain embed error, surfaced by `go build`/`go vet`

**Error Scenario 2: `ConventionsMD` empty at runtime**
- Condition: `skill/CONVENTIONS.md` exists but is empty, or the embed captured the wrong file
- Detection: `require.NotEmpty(t, ConventionsMD, "CONVENTIONS.md must be embedded and non-empty")` fails
- Error message (test failure): `CONVENTIONS.md must be embedded and non-empty`

**Error Scenario 3: `TestSkill_NoAbsoluteOrClaudePaths` list not updated**
- Condition: `ConventionsMD` is embedded but never added to the iteration list in `TestSkill_NoAbsoluteOrClaudePaths`
- Detection: this is a silent coverage gap, not a test failure — `ConventionsMD` would go unchecked for `.claude`/absolute-path violations. Caught only by manual review or a dedicated new assertion that explicitly checks the list length/membership
- Mitigation: reviewer must confirm the list literal includes `ConventionsMD` as part of Definition of Done (Story-Specific)

## Performance Requirements
- **Response Time:** N/A — build-time embed, zero runtime cost beyond the existing `//go:embed` pattern already used for three other files
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — no runtime code path, build-time content embed only
- **Input Validation:** The no-`.claude`/no-absolute-path test (`TestSkill_NoAbsoluteOrClaudePaths`) is itself a security/portability guard preventing environment-specific or user-specific paths from leaking into a distributable skill artifact; extending it to `ConventionsMD` preserves that guarantee for the new file

## Test Implementation Guidance
**Test Type:** UNIT (Go `testing` package, build-time-embedded string assertions; no I/O, no network, no subprocess)
**Test Data Requirements:** Final text of `skill/CONVENTIONS.md` (from AC 04-01) and the rewritten `skill/SKILL.md` Prerequisites pointer (from AC 04-02) — both must be finalized before this AC's tests can pass, confirming the dependency ordering noted in the story (this story should land alongside or before Story 3, and AC 04-01/04-02 land before or alongside 04-03)
**Mock/Stub Requirements:** None — `go:embed` resolves at compile time from the real filesystem; no mocking needed or possible for embed directives

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./skill/...`)
- [x] No linting errors
- [x] Build succeeds (`go build ./skill/...`)

**Story-Specific:**
- [x] `skill/skill.go` has `//go:embed CONVENTIONS.md` and `var ConventionsMD string`, matching the existing three-file pattern; package doc comment lists `CONVENTIONS.md`
- [x] `ConventionsMD` is asserted non-empty in `skill/skill_test.go`
- [x] `SkillMD` is asserted to reference `` `CONVENTIONS.md` `` and not contain the full duplicated original Prerequisites text
- [x] `TestSkill_NoAbsoluteOrClaudePaths`'s iteration list includes `ConventionsMD`
- [x] `dispatcherCommands` list and `TestSkill_DispatcherRoutingTable` are unmodified and still passing

**Manual Review:**
- [ ] Code reviewed and approved
