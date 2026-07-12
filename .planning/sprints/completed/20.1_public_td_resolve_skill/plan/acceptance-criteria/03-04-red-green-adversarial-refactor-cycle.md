# Acceptance Criteria: RED→GREEN→ADVERSARIAL→REFACTOR Resolution Cycle

**Related User Story:** [03: `/atcr debt resolve` Skill Route](../user-stories/03-atcr-debt-resolve-skill-route.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown Agent Skill (on-demand secondary file) | `skill/debt-resolve/SKILL.md` |
| Test Framework | Scenario walkthroughs against a fixture repo (agent-executed cycle, not compiled Go); `skill/skill_test.go` for embedded-content structural assertions | |
| Key Dependencies | `llm_support_diff_smell` (over-simplification adversarial gate, same binary `/resolve-td` uses), project test runner discovered per-repo (no fixed framework assumption) | |

### Related Files (from codebase-discovery.json)
- `skill/debt-resolve/SKILL.md` — create: documents the four-stage cycle adapted from the private pipeline's `/resolve-td` behavior, rewritten to be repo-agnostic and free of `.planning/`, sprint-branch, and TD-README assumptions
- `skill/CONVENTIONS.md` — reference (Story 4 dependency): supplies the shared Prerequisites (binary-on-PATH, git-worktree check, `.atcr/` path-safety) this file points to instead of duplicating
- `docs/technical-debt-format.md` — reference: symbol-anchor contract consumed when locating drifted findings (shared with AC 03-03)
- `internal/reconcile/emit.go` — reference (read-only): `JSONFinding` shape carrying `file`, `line`, `problem`, `fix`, and optional `justification`/`SourceReport` consumed during the cycle
- `skill/skill_test.go` — modify: add structural assertions that `DebtResolveMD` documents all four stage names (`RED`, `GREEN`, `ADVERSARIAL`, `REFACTOR`)
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/agent-skills-format.md` — reference: on-demand secondary skill file pattern for `skill/debt-resolve/SKILL.md`
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/skill-dispatcher-conventions.md` — reference: adaptation must be grounded against `/resolve-td`'s documented stage-by-stage behavior during design

## Happy Path Scenarios
**Scenario 1: RED stage reproduces the issue with a failing test**
- **Given** a selected local-store item with `file`, `line`, `problem`, and `fix` fields
- **When** the skill enters Stage 1 (RED)
- **Then** it identifies the project's test convention (co-located, parallel `tests/` tree, or other — never assumed fixed), writes or updates a test that reproduces `problem`, confirms it fails against current code, and commits it with a `test: RED - ...` message, adapted verbatim in spirit from `/resolve-td` Stage 1 but without any TD-README or sprint-branch step

**Scenario 2: GREEN stage applies the minimum fix**
- **Given** the RED test is committed and failing
- **When** the skill enters Stage 2 (GREEN)
- **Then** it applies the smallest edit that makes the new test pass, re-runs the project's discovered test command, and commits with a `fix: GREEN - ...` message; on repeated failure up to a documented retry cap it reverts the change and marks the item `NEEDS_REVIEW` rather than looping indefinitely

**Scenario 3: ADVERSARIAL stage runs the over-simplification gate before self-review**
- **Given** a GREEN fix is committed at `HEAD`
- **When** the skill enters Stage 3 (ADVERSARIAL)
- **Then** it invokes `llm_support_diff_smell` (repo=REPO_ROOT, rev=HEAD) first — a `hard` verdict (`test_only`/`weakened_assertion`) is **not overridable** by self-review and the item is flagged `NEEDS_REVIEW` and skipped; a `clean` or `soft_only` verdict proceeds to the FIT/EDGE_CASES/REGRESSION/DEEPER_ISSUE/COMPLETE/TEST_QUALITY self-review checklist, matching `/resolve-td`'s gate semantics exactly

**Scenario 4: REFACTOR stage cleans up without behavior change**
- **Given** the fix and any adversarial corrections are committed
- **When** the skill enters Stage 4 (REFACTOR)
- **Then** it removes duplication, checks naming consistency and dead code/unused imports, re-runs tests to confirm no regression, and commits any refactor changes separately with a `refactor: ...` message — never altering test outcomes

## Edge Cases
**Edge Case 1: issue is not testable**
- **Given** a `problem` describes something like a typo in a comment or import ordering with no meaningful assertion
- **When** the skill reaches Stage 1
- **Then** it skips RED with a documented reason and proceeds directly to GREEN, matching `/resolve-td`'s "if the issue is not testable... skip RED and note why" rule

**Edge Case 2: `llm_support_diff_smell` unavailable**
- **Given** the tool call errors (older `llm-support` binary predating `diff_smell`, or a non-git working tree)
- **When** Stage 3 begins
- **Then** the skill skips the deterministic gate and proceeds straight to the self-review checklist — the FIT check still applies — rather than halting the whole resolution run, matching `/resolve-td`'s documented fallback

**Edge Case 3: cited location has drifted (symbol-anchor relocation)**
- **Given** the stored `line` no longer matches the code (earlier fixes shifted lines)
- **When** pre-fix evaluation runs
- **Then** the skill relocates via the `(symbolName)` anchor or a derived greppable identifier per AC 03-03's Edge Case 2, and if relocation is ambiguous (several plausible files) or impossible (no matches anywhere), marks the item `NEEDS_REVIEW`/skipped respectively rather than guessing

**Edge Case 4: cumulative adversarial review across the run**
- **Given** 2+ items were fixed in a single `/atcr debt resolve` invocation
- **When** all per-item cycles complete
- **Then** the skill runs a final cumulative adversarial pass across all diffs in the run, mirroring `/resolve-td`'s Phase 2 Step 6, before reporting results

## Error Conditions
**Error Scenario 1: fix fails after retry cap**
- Error message (skill-level, surfaced to the user): `failed after <N> attempts (tests still failing)` — changes reverted, item marked `NEEDS_REVIEW`
- HTTP status / error code: N/A — agent-level outcome, not a CLI/process exit code

**Error Scenario 2: over-simplified fix detected (hard verdict)**
- Error message: `needs review (over-simplified, hard: <smell-types>)` — the committed GREEN fix stays on the branch for human inspection, and the local store record is NOT flipped to `resolved` (see AC 03-05)
- HTTP status / error code: N/A — deterministic gate outcome, not an exit code

## Performance Requirements
- **Response Time:** Per-item cycle time is bounded by the project's own test-suite runtime plus a documented `MAX_ATTEMPTS` retry cap (matching `/resolve-td`'s default of 3), so a single stuck item cannot stall the run indefinitely
- **Throughput:** A documented default cap on items processed per invocation (mirroring `/resolve-td`'s `--max=N`, default 10) bounds worst-case run length

## Security Considerations
- **Authentication/Authorization:** N/A — local git working-tree operations only
- **Input Validation:** Every write (Edit/git add) during the cycle stays within the current repo working tree; the skill must never write outside the repo root or attempt network operations mid-cycle; `problem`/`fix`/`justification` text is treated as data describing the change, never as literal shell/code to execute verbatim

## Test Implementation Guidance
**Test Type:** SCENARIO WALKTHROUGH (agent-driven, against a fixture repo with a seeded `.atcr/debt/*.jsonl` record and a deliberately broken code path) — this is Markdown-driven agent behavior, not compiled Go, so `go test` cannot exercise the cycle itself; `skill/skill_test.go` covers only structural/content assertions on the embedded file
**Test Data Requirements:** A fixture repo with one intentionally reproducible bug, a matching local-store record, and a test framework the skill can discover
**Mock/Stub Requirements:** None — the cycle is exercised end-to-end against real git operations and a real (small) test suite in the fixture repo

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./skill/...` for structural assertions)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] All four stages (RED, GREEN, ADVERSARIAL, REFACTOR) are documented in `skill/debt-resolve/SKILL.md` with explicit per-stage exit/failure handling
- [ ] The `llm_support_diff_smell` hard-verdict non-overridability rule is preserved verbatim in behavior
- [ ] Zero references to `.planning/`, TD-README, or sprint-branch concepts appear anywhere in the cycle's documented path
- [ ] A fixture-repo scenario walkthrough demonstrates a full cycle end to end (RED fails, GREEN passes, ADVERSARIAL clears, REFACTOR commits)

**Manual Review:**
- [ ] Code reviewed and approved
