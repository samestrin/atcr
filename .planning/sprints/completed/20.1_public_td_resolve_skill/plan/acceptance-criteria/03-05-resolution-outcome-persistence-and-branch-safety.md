# Acceptance Criteria: Resolution Outcome Persistence and Branch Safety

**Related User Story:** [03: `/atcr debt resolve` Skill Route](../user-stories/03-atcr-debt-resolve-skill-route.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI subcommand (Cobra) + Markdown Agent Skill | `cmd/atcr/debt_resolve.go` (mark-resolved write path) and `skill/debt-resolve/SKILL.md` (branch/commit convention documentation) |
| Test Framework | go test for the CLI write path; scenario walkthrough for the branch-creation behavior | |
| Key Dependencies | `internal/localdebt` (Story 1's `Record.Status`/`ResolvedAt` fields, append-only store semantics) | |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/debt_resolve.go` — modify (extends AC 03-02's file): add a mark-resolved write path that appends an updated record (`status: resolved`, `resolved_at: <RFC3339>`) for each item the cycle in AC 03-04 completes successfully
- `skill/debt-resolve/SKILL.md` — modify (extends AC 03-03/03-04's file): documents the git branch/commit convention for autonomous fixes and the store-update contract
- `internal/localdebt/store.go` — reference (Story 1 dependency): the store is append-only (no in-place row edit), so "marking resolved" means appending a new record sharing the same `id` with an updated `status`, not mutating the original line
- `documentation/local-td-store-schema.md` — reference: defines `status` (`open`/`in_progress`/`resolved`/`wont_fix`) and `resolved_at` as the optional fields this AC populates
- `internal/scorecard/store.go` — reference (read-only): append-only ledger pattern to mirror for the mark-resolved write path
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/local-td-store-schema.md` — reference: "Identity and Deduplication" latest-status-wins semantics for resolved records

## Happy Path Scenarios
**Scenario 1: successfully resolved item is marked in the store**
- **Given** an item completes RED→GREEN→ADVERSARIAL→REFACTOR without being flagged `NEEDS_REVIEW`
- **When** the per-item cycle finishes
- **Then** the skill (via `atcr debt resolve --mark-resolved <id>` or equivalent CLI call) appends a new record sharing the original `id` with `status: resolved` and `resolved_at` set to the current RFC3339 timestamp — consistent with the store's append-only, reader-dedups-by-`id`-and-latest-wins semantics

**Scenario 2: autonomous fixes land on a dedicated branch, never directly on the default branch**
- **Given** `/atcr debt resolve` runs on a repo whose current branch is the detected default branch
- **When** the skill is about to make its first commit
- **Then** it creates and checks out a dedicated branch (e.g. `debt-resolve/<date>`, analogous to `/resolve-td`'s `td/<date>` convention) before any RED/GREEN commit lands, so unreviewed autonomous changes never land directly on the user's default branch

**Scenario 3: run summary reports outcomes per item**
- **Given** a run processes N items with a mix of outcomes
- **When** the cycle completes for all selected items
- **Then** the skill reports a summary (fixed count, needs-review count, skipped count) and names the branch the fixes landed on, so the user knows what to review before merging

## Edge Cases
**Edge Case 1: already on a non-default branch**
- **Given** the user invokes `/atcr debt resolve` while already on a feature/working branch (not the default branch)
- **When** the skill checks branch state
- **Then** it resolves in place on the current branch rather than creating a new one, mirroring `/resolve-td`'s "resolving on $CURRENT_BRANCH" behavior for non-default, non-`td/*` branches

**Edge Case 2: `NEEDS_REVIEW` items are not marked resolved**
- **Given** an item is flagged `NEEDS_REVIEW` by the ADVERSARIAL over-simplification gate or a failed-after-retries GREEN stage
- **When** the run's summary and store updates are produced
- **Then** no `status: resolved` record is appended for that item — it remains `open` (or unset) in the store so a subsequent run can pick it up again, and any partial commits stay on the branch for human inspection

**Edge Case 3: re-running against an item already marked resolved**
- **Given** the store already has a `status: resolved` record for a given `id`
- **When** a later `/atcr debt resolve` run selects items
- **Then** the selection step (AC 03-03) excludes already-resolved items by default (reading the latest-status-wins record per `id`), so the same item is not re-attempted

## Error Conditions
**Error Scenario 1: branch creation fails (e.g. dirty working tree, existing conflicting branch name)**
- Error message: `atcr debt resolve: failed to create branch <name>: <git error>` — the skill halts before making any commits rather than proceeding on the wrong branch
- HTTP status / error code: N/A — agent-level halt; underlying `git` command exits non-zero

**Error Scenario 2: mark-resolved write fails (e.g. `.atcr/debt/` unwritable)**
- Error message: `atcr debt resolve: failed to persist resolution status: <underlying os error>`
- HTTP status / error code: process exit code 1; the code fix itself (already committed) is NOT rolled back — only the store-status update failed, and the skill reports this discrepancy explicitly to the user rather than silently losing the resolution record

## Performance Requirements
- **Response Time:** Marking an item resolved is a single `Append` call (one `os.Write`), consistent with Story 1's documented atomic-append performance characteristics — no measurable overhead beyond the fix itself
- **Throughput:** N/A — single-run, single-user flow

## Security Considerations
- **Authentication/Authorization:** N/A — local git/filesystem operations only
- **Input Validation:** Branch names are derived from a fixed template (`debt-resolve/<date>`) plus validated date formatting — never built from unsanitized `problem`/`fix` text — to avoid constructing an invalid or attacker-influenced branch/ref name

## Test Implementation Guidance
**Test Type:** UNIT for the CLI mark-resolved write path (`go test`); SCENARIO WALKTHROUGH for branch-creation behavior across default-branch vs. feature-branch starting states
**Test Data Requirements:** A fixture store with an `open` record; two starting-branch fixtures (default branch, existing feature branch)
**Mock/Stub Requirements:** `git` operations exercised against a real temp git repo (matching the rest of the codebase's no-mocking-git convention); no network calls involved

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Successfully resolved items are marked `status: resolved` with `resolved_at` in the local store via an appended record, not an in-place edit
- [ ] Autonomous fixes never commit directly to the default branch — a dedicated branch is created when starting from the default branch
- [ ] `NEEDS_REVIEW`/failed items are never marked resolved and remain selectable in a later run

**Manual Review:**
- [ ] Code reviewed and approved
