# Acceptance Criteria: Skill Structure and Installation

**Related User Story:** [05: Host Review via Skill](../user-stories/05-host-review-via-skill.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Agent Skill | Markdown (`skill/SKILL.md`) | Instructions for AI agent (e.g., Claude Code) |
| Installation | File copy to `.claude/skills/atcr/` | Agent skill directory convention |
| Input Parsing | Shell/Bash in skill instructions | Accepts git range, branch name, or PR URL |
| Test Framework | `testify` (assert) | Validate SKILL.md structure and content |

## Related Files
- `skill/SKILL.md` - create: Agent skill definition with host review instructions, orchestration loop, and input handling
- `skill/SKILL_test.go` - create: Structural tests for SKILL.md content (required sections, format examples)
- `internal/skill/install.go` - create: Skill installation helper (copies skill/SKILL.md to target project)
- `docs/skill-usage.md` - create: User-facing documentation for skill installation and usage

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [CLI Architecture](../documentation/cli-architecture.md) — Skill's orchestration calls into the same cobra commands; nothing custom.
- [Range Resolution](../documentation/range-resolution.md) — Skill uses `atcr range` as the pre-flight step; result JSON shape is documented in the spec.
- [Reconciler & Findings Stream](../documentation/reconciler.md) — Skill's reconciliation step uses the same `atcr reconcile` CLI; the Skill reads `reconciled/report.md` to present results.

### Spec alignment notes

- **Skill input contract** (per `user-stories/05-host-review-via-skill.md` original criterion #11): a git range, branch, or PR reference. **No sprint paths, no `.planning/` coupling, no DoD checks.** Output contract is the review directory path. Keeps the skill portable to any repo without atcr-specific project state.
- **Skill installation** is by file copy: `skill/SKILL.md` lives in this repo and is installable into `.claude/skills/atcr/`. The agent's standard skill-resolution rules apply (local project copy wins over shipped copy).
- **Skill file is plain Markdown** — no frontmatter required by the v1 spec, but frontmatter is permitted. Required sections per the AC: overview, input format, orchestration steps, host review instructions, and findings format reference.
- **PR URL extraction** uses the standard format `https://github.com/<owner>/<repo>/pull/<n>` and resolves `base`/`head` via `gh pr view <n> --json baseRefName,headRefName`. If `gh` is not available, the Skill halts with installation guidance.

## Happy Path Scenarios

**Scenario 1: Skill file exists at expected location**
- **Given** the atcr repository is checked out
- **When** the skill installation path is resolved
- **Then** `skill/SKILL.md` exists and contains the skill definition
- **And** the file includes all required sections: overview, input format, orchestration steps, host review instructions, and findings format reference

**Scenario 2: Skill accepts git range as input**
- **Given** the skill is invoked by an AI agent
- **When** the user provides a git range (e.g., `main..feature-branch`)
- **Then** the skill parses the range and passes it to `atcr range`
- **And** the skill proceeds with the orchestration loop

**Scenario 3: Skill accepts branch name as input**
- **Given** the skill is invoked by an AI agent
- **When** the user provides a branch name (e.g., `feature-branch`)
- **Then** the skill resolves the branch to a range against the default branch
- **And** passes the resolved range to `atcr range`

**Scenario 4: Skill accepts PR URL as input**
- **Given** the skill is invoked by an AI agent
- **When** the user provides a PR URL (e.g., `https://github.com/owner/repo/pull/42`)
- **Then** the skill extracts the base and head refs from the PR
- **And** passes the resolved range to `atcr range`

## Edge Cases

**Edge Case 1: No input provided (use current branch)**
- **Given** the skill is invoked with no arguments
- **When** the agent is on a feature branch
- **Then** the skill defaults to reviewing the current branch against the detected default branch
- **And** proceeds with the orchestration loop

**Edge Case 2: Skill installed in non-standard location**
- **Given** the user places `SKILL.md` in a project-specific skill directory (e.g., `.claude/skills/atcr/`)
- **When** the agent loads the skill
- **Then** the skill functions identically regardless of installation path
- **And** no hardcoded paths are used in skill instructions

**Edge Case 3: Multiple skill versions on disk**
- **Given** a user has both the shipped `skill/SKILL.md` and a local copy in `.claude/skills/atcr/`
- **When** the agent resolves the skill
- **Then** the agent uses the local project copy (standard skill resolution)
- **And** the shipped copy serves as the canonical reference

## Error Conditions

**Error Scenario 1: Invalid git range provided**
- Error message: "Invalid range: <input>. Provide a git range (base..head), branch name, or PR URL."
- Skill behavior: Halt orchestration and display usage guidance

**Error Scenario 2: atcr binary not found in PATH**
- Error message: "atcr binary not found. Install atcr or add it to PATH before using the skill."
- Skill behavior: Halt orchestration and display installation instructions

**Error Scenario 3: Not inside a git repository**
- Error message: "Not a git repository. Run the skill from within a git working tree."
- Skill behavior: Halt orchestration immediately

## Performance Requirements
- **Skill Loading:** Skill file parses and is ready for agent consumption in < 100ms
- **Input Parsing:** Range/branch/PR input resolves in < 2 seconds (git operations only)
- **Installation:** Skill file copy completes in < 500ms

## Security Considerations
- **Input Validation:** All user-provided input (range, branch, PR URL) is validated before passing to `atcr` commands; no shell injection
- **No arbitrary code execution:** Skill instructions do not include eval/exec of user input
- **Path traversal prevention:** Skill does not write files outside the review directory (`.atcr/reviews/<id>/`)

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:**
- Sample SKILL.md content with all required sections
- Valid and invalid git ranges, branch names, PR URLs
- Test fixture: mock git repository with branches
**Mock/Stub Requirements:**
- Mock `atcr` binary calls (range, review, reconcile, report)
- Mock git operations (branch detection, range resolution)

**Test Cases:**
1. `TestSkill_FileExists` — verify skill/SKILL.md exists and is non-empty
2. `TestSkill_RequiredSections` — verify SKILL.md contains all required sections
3. `TestSkill_ParseGitRange` — verify range input parsing
4. `TestSkill_ParseBranchName` — verify branch input resolution
5. `TestSkill_ParsePRUrl` — verify PR URL extraction
6. `TestSkill_DefaultToCurrentBranch` — verify default behavior with no input

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (unit + integration)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./cmd/atcr`)
- [ ] `skill/SKILL.md` exists and contains all required sections

**Story-Specific:**
- [ ] Skill accepts git range, branch name, and PR URL as input
- [ ] Skill defaults to current branch when no input provided
- [ ] Skill installation path is documented
- [ ] No hardcoded paths in skill instructions

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Skill instructions are clear and actionable for an AI agent
