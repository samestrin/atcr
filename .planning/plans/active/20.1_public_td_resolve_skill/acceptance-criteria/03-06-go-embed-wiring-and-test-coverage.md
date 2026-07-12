# Acceptance Criteria: Go Embed Wiring and Test Coverage for `skill/debt-resolve/SKILL.md`

**Related User Story:** [03: `/atcr debt resolve` Skill Route](../user-stories/03-atcr-debt-resolve-skill-route.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go build-time embed harness | `skill/skill.go` (`//go:embed` + exported `string` constants) |
| Test Framework | go test | `skill/skill_test.go` |
| Key Dependencies | Go stdlib `embed` package only — no new dependency | |

### Related Files (from codebase-discovery.json)
- `skill/skill.go` — modify: add `//go:embed debt-resolve/SKILL.md` and `var DebtResolveMD string`, alongside the existing `HostReviewMD`, `AmbiguityAdjudicationMD`, `FindingsFormatMD` constants, and update the package doc comment's list of secondary files
- `skill/skill_test.go` — modify: extend `TestSkill_NoAbsoluteOrClaudePaths`'s iterated list (currently `SkillMD, HostReviewMD, AmbiguityAdjudicationMD, FindingsFormatMD`) to include `DebtResolveMD`; add new assertions that `DebtResolveMD` is non-empty and documents the RED/GREEN/ADVERSARIAL/REFACTOR stage names and a reference to `CONVENTIONS.md` (Story 4) rather than duplicating its Prerequisites text
- `skill/debt-resolve/SKILL.md` — reference: the file being embedded and tested (created by AC 03-01/03-03/03-04)
- `skill/CONVENTIONS.md` — reference (Story 4 dependency): the shared file `debt-resolve/SKILL.md` must point to, not duplicate
- `skill/SKILL.md` — reference (read-only): dispatcher file that points to `debt-resolve/SKILL.md` on demand
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/agent-skills-format.md` — reference: Go embed harness and secondary-file conventions

## Happy Path Scenarios
**Scenario 1: `DebtResolveMD` embeds and loads successfully**
- **Given** `skill/debt-resolve/SKILL.md` exists on disk with valid content
- **When** `go build ./skill/...` runs
- **Then** the `//go:embed debt-resolve/SKILL.md` directive resolves without error and `skill.DebtResolveMD` is populated with the file's full contents at build time

**Scenario 2: non-empty and non-duplication assertions pass**
- **Given** `skill/skill_test.go` is extended per this AC
- **When** `go test ./skill/...` runs
- **Then** a new `TestSkill_DebtResolve...` test (or additions to existing tests) asserts `DebtResolveMD` is non-empty, contains the four cycle-stage markers (`RED`, `GREEN`, `ADVERSARIAL`, `REFACTOR`), and references `CONVENTIONS.md` by name rather than re-stating the binary-on-PATH/git-worktree checks verbatim

**Scenario 3: existing no-`.claude`/no-absolute-path bar extends to the new file**
- **Given** `TestSkill_NoAbsoluteOrClaudePaths` iterates a fixed slice of embedded-MD variables
- **When** `DebtResolveMD` is added to that slice
- **Then** the test fails loudly if `skill/debt-resolve/SKILL.md` ever contains `.claude`, `/Users/`, `/home/`, `/opt/`, or `C:\` — the same bar every other public skill file is held to

## Edge Cases
**Edge Case 1: `skill/debt-resolve/` directory missing at embed time**
- **Given** the `debt-resolve/` subdirectory or its `SKILL.md` file is absent (e.g. a partial checkout, or this story lands before its own file is created)
- **When** `go build ./skill/...` runs
- **Then** the build fails deterministically at compile time with a clear `go:embed` "no matching files" error rather than silently embedding an empty string — this is the desired fail-fast behavior, not a bug to work around

**Edge Case 2: `DebtResolveMD` referenced before `CONVENTIONS.md` (Story 4) lands**
- **Given** Story 4 may sequence in parallel with this story per the plan's dependency notes
- **When** `skill/debt-resolve/SKILL.md` is authored ahead of `skill/CONVENTIONS.md` landing
- **Then** the reference is stubbed with a clear placeholder (or the two stories are coordinated to land together) so the embed test does not assert against a broken or self-contradictory pointer — per Story Context's "if sequenced in parallel, this story stubs the reference"

## Error Conditions
**Error Scenario 1: test asserts duplication instead of reference**
- Error message (test failure output): `debt-resolve/SKILL.md must reference CONVENTIONS.md, not restate its checks` (or equivalent `assert.NotContains`/`assert.Contains` failure message)
- HTTP status / error code: N/A — `go test` non-zero exit on assertion failure

**Error Scenario 2: missing cycle-stage documentation**
- Error message: `DebtResolveMD must document stage %q` (parameterized per missing `RED`/`GREEN`/`ADVERSARIAL`/`REFACTOR` marker)
- HTTP status / error code: N/A — `go test` non-zero exit on assertion failure

## Performance Requirements
- **Response Time:** N/A — compile-time embed, zero runtime cost
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** N/A — static content embedding; the no-absolute-path test is itself the relevant safety check (prevents leaking a contributor's local machine paths into the shipped public skill)

## Test Implementation Guidance
**Test Type:** UNIT (`go test ./skill/...`, pure string-content assertions against the embedded constants — no filesystem I/O at test time beyond what `go:embed` already performed at build time)
**Test Data Requirements:** None beyond the actual `skill/debt-resolve/SKILL.md` and `skill/CONVENTIONS.md` file contents
**Mock/Stub Requirements:** None

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./skill/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `skill/skill.go` embeds `skill/debt-resolve/SKILL.md` as `DebtResolveMD`, matching the existing three-file embed pattern exactly
- [ ] `TestSkill_NoAbsoluteOrClaudePaths` covers `DebtResolveMD` alongside the existing four embedded strings
- [ ] New assertions confirm `DebtResolveMD` documents all four cycle stages and references `CONVENTIONS.md` rather than duplicating it

**Manual Review:**
- [ ] Code reviewed and approved
