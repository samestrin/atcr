# Acceptance Criteria: skill/CONVENTIONS.md Creation

**Related User Story:** [04: Shared Skill Conventions Extraction](../user-stories/04-shared-skill-conventions-extraction.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (Agent Skill sibling file) | Follows the existing `host-review.md` / `ambiguity-adjudication.md` / `findings-format.md` on-demand-sibling-file pattern in `skill/` |
| Test Framework | Go `testing` + `stretchr/testify` (`assert`/`require`) | Verified at build time via `skill/skill_test.go`, mirroring `TestSkill_SecondaryFilesVerbatim` |
| Key Dependencies | None (pure Markdown; no new packages) | Content is relocated/authored text only |

### Related Files (from codebase-discovery.json)
- `skill/CONVENTIONS.md` — create: new shared-conventions file containing the binary-on-PATH check, git-worktree check, `gh` CLI note, and new `.atcr/` path-safety rules
- `skill/SKILL.md` — reference: source text to relocate lives in the existing "## Prerequisites" section (skill/SKILL.md:19-22)
- `skill/host-review.md` — reference: structural pattern to mirror (standalone H1-headed sibling file loaded on demand from SKILL.md)
- `cmd/atcr/reconcile.go` — reference: `Root: "."` convention (cmd/atcr/reconcile.go:93) that grounds the new `.atcr/` path-safety rules
- `skill/skill_test.go` — reference (read-only): `TestSkill_NoAbsoluteOrClaudePaths` (line 113) will include `ConventionsMD` once embedded in AC 04-03
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/agent-skills-format.md` — reference: Level 3 resource / on-demand sibling-file pattern
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/skill-dispatcher-conventions.md` — reference: shared Prerequisites extraction requirements per Epic 20.0 addendum

## Happy Path Scenarios
**Scenario 1: File created with all three relocated checks**
- **Given** `skill/SKILL.md`'s "## Prerequisites" section currently inlines the binary-on-PATH halt message, the git-worktree halt message, and the `gh` CLI note (skill/SKILL.md:19-22)
- **When** `skill/CONVENTIONS.md` is authored
- **Then** it contains the binary-on-PATH halt message text (`atcr binary not found. Install atcr or add it to PATH before using the skill.`), the git-worktree halt message text (`Not a git repository. Run the skill from within a git working tree.`), and the `gh` CLI note for PR resolution, each preserved without semantic weakening

**Scenario 2: New `.atcr/` path-safety rules section added**
- **Given** no existing shared document states the convention that public-skill file operations stay rooted at `.atcr/`
- **When** `skill/CONVENTIONS.md` is authored
- **Then** it includes a `.atcr/` path-safety rules section stating that all public-skill file operations are rooted at the repo's `.atcr/` directory (per `cmd/atcr/reconcile.go`'s `Root: "."` convention) and must never read or write outside it or under `.planning/`

**Scenario 3: File structure mirrors existing secondary files**
- **Given** `host-review.md`, `ambiguity-adjudication.md`, and `findings-format.md` each open with a single H1 heading and are self-contained
- **When** `skill/CONVENTIONS.md` is authored
- **Then** it opens with a single H1 heading (e.g. `# Shared Skill Conventions`) and is a self-contained Markdown file, not a fragment requiring external context to parse

## Edge Cases
**Edge Case 1: No `.claude`-specific or absolute filesystem paths**
- **Given** `TestSkill_NoAbsoluteOrClaudePaths` (skill/skill_test.go:113) enforces no `.claude` substrings and no absolute paths (`/Users/`, `/home/`, `/opt/`, `C:\`) across embedded skill bodies
- **When** `skill/CONVENTIONS.md`'s content is written
- **Then** it contains neither `.claude` nor any absolute filesystem path, so it can be added to that test's iteration list without failing

**Edge Case 2: `gh` CLI note is not dropped**
- **Given** the original Prerequisites section's third bullet covers `gh` CLI requirements for PR resolution (skill/SKILL.md:22)
- **When** the extraction happens
- **Then** the `gh` CLI note is present verbatim (or with equivalent meaning) in `skill/CONVENTIONS.md` — the original Prerequisites section's third bullet covers this note, and the risk table in the story (skill/../user-stories/04-shared-skill-conventions-extraction.md Potential Risks) explicitly calls this out

**Edge Case 3: Path-safety rules do not contradict `internal/localdebt`'s scoping**
- **Given** Story 1 (01-local-td-store-persistence.md) scopes its local TD store at `.atcr/debt/` using the same `Root: "."` convention
- **When** the `.atcr/` path-safety rules are written
- **Then** the rules are consistent with (not contradictory to) `.atcr/debt/`'s existing/planned scoping — no invented restriction that would make Story 1's store non-compliant

## Error Conditions
**Error Scenario 1: Extraction drops enforced coverage**
- Condition: a maintainer removes a check (e.g. the git-worktree halt) instead of relocating it
- Detection: `skill/skill_test.go`'s `TestSkill_SecondaryFilesVerbatim`-style anchor assertions fail because the expected halt-message substring is absent from `ConventionsMD`
- Error message (test failure): `secondary file CONVENTIONS.md must contain relocated content anchor "Not a git repository. Run the skill from within a git working tree."`

**Error Scenario 2: `.atcr/` path-safety rules invented ad hoc**
- Condition: rules text is written without grounding in `Root: "."` or contradicts `.atcr/debt/` scoping
- Detection: manual review (Definition of Done, Manual section) — no automated test can assert semantic groundedness, only presence of the section and key terms (`.atcr/`, `.planning/`)

## Performance Requirements
- **Response Time:** N/A — this is a static Markdown file; no runtime execution path
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — documentation only, no code path
- **Input Validation:** The `.atcr/` path-safety rules themselves are a security-relevant convention: they must explicitly state that file operations never read/write outside `.atcr/` and never under `.planning/`, preventing a future skill author from scoping a public skill's file writes into private-pipeline directories

## Test Implementation Guidance
**Test Type:** UNIT (build-time Go test over embedded string content; no runtime behavior to integration-test)
**Test Data Requirements:** The literal text of `skill/CONVENTIONS.md` once embedded as `ConventionsMD` (see AC 04-03 for the embed itself); anchor substrings for the three relocated checks and the new path-safety rules section
**Mock/Stub Requirements:** None — pure string-content assertions, no I/O or external dependencies to mock

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./skill/...`)
- [x] No linting errors
- [x] Build succeeds (`go build ./skill/...`)

**Story-Specific:**
- [x] `skill/CONVENTIONS.md` exists and contains the binary-on-PATH halt message, git-worktree halt message, and `gh` CLI note, each preserved without loss of coverage
- [x] `skill/CONVENTIONS.md` contains a new `.atcr/` path-safety rules section grounded in `cmd/atcr/reconcile.go`'s `Root: "."` convention
- [x] `skill/CONVENTIONS.md` contains no `.claude`-specific paths and no absolute filesystem paths
- [x] `skill/CONVENTIONS.md`'s path-safety rules are consistent with Story 1's `.atcr/debt/` scoping

**Manual Review:**
- [ ] Code reviewed and approved
