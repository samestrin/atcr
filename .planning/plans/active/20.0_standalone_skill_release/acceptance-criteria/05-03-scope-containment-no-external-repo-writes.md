# Acceptance Criteria: Scope Containment — No External-Repo Writes

**Related User Story:** [05: External Migration Descope Note](../user-stories/05-external-migration-descope-note.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Change-scope verification (git diff / file-tree audit) | No new runtime component; verifies the *process* of authoring this story stayed in-bounds |
| Test Framework | `git status` / `git diff --name-only` inspection | No code test framework applies |
| Key Dependencies | None | Verification uses only local git tooling |

## Related Files
- `docs/external-migration.md` - create: the only new file this story is permitted to introduce outside the plan folder
- `.planning/plans/active/20.0_standalone_skill_release/user-stories/05-external-migration-descope-note.md` - modify: AC backlink update only (this file), no content drift beyond the `## Acceptance Criteria` section
- `.planning/plans/active/20.0_standalone_skill_release/documentation/external-migration-descope.md` - read-only source: must not be deleted or moved, only read from
- `~/Documents/GitHub/claude-prompts/.claude/skills/` (external repo, out of workspace) - explicitly must NOT appear in any diff, stage, or commit produced by this story

## Happy Path Scenarios
**Scenario 1: Only in-scope paths are touched**
- **Given** the story's Constraints ("no code, no shell scripts, no changes outside this repository's `docs/` tree, and this plan's `user-stories/`/`documentation/` folders")
- **When** this story's work is complete
- **Then** `git diff --name-only` against the base branch shows only `docs/external-migration.md` (new), optionally `docs/README.md` and/or `docs/skill-usage.md` (cross-link edit), and this plan's `user-stories/05-external-migration-descope-note.md` (AC backlink update) plus the `acceptance-criteria/05-*.md` files

**Scenario 2: No external-repo path appears anywhere in the change set**
- **Given** the workspace boundary is `/Users/samestrin/Documents/GitHub/atcr`
- **When** the diff and any generated content is reviewed
- **Then** no file path under `~/Documents/GitHub/claude-prompts/` (or any path outside this repo) is written, staged, or referenced as a target of a write operation — read-only mentions of that path in prose (e.g. explaining the workspace boundary) are permitted, but no tool call attempts to create/edit/delete a file there

## Edge Cases
**Edge Case 1: Story mistakenly modifies the private skills' local mirror or a cached copy**
- **Given** no such mirror is expected to exist in this workspace
- **When** verifying the file list touched
- **Then** confirm no `.claude/skills/` path outside this repo's own `skill/` directory was touched; if any such path exists in the diff, it is flagged as an error condition (see below)

**Edge Case 2: Cross-link edit in `docs/README.md` accidentally rewrites unrelated index entries**
- **Given** `docs/README.md` is a shared index file used by other docs (`architecture.md`, `code-review-backend.md`, etc.)
- **When** the AC-2 cross-link is added
- **Then** the diff to `docs/README.md` is limited to the single new list-item addition — no reordering, rewording, or deletion of existing entries

## Error Conditions
**Error Scenario 1: A write attempt targets the external `claude-prompts` repository**
- Condition: any tool call in this story's execution attempts to create, edit, or delete a file under `~/Documents/GitHub/claude-prompts/`
- Detection: tool permission system denies filesystem writes outside the workspace root, or a pre-commit/manual diff review catches an out-of-repo path
- Required fix: abort the write, remove any staged reference, and confirm the operation is documented as manual/out-of-scope guidance only

**Error Scenario 2: Unrelated files are modified beyond the story's declared scope**
- Condition: `git diff --name-only` includes files outside `docs/`, this plan's `user-stories/`/`documentation/`/`acceptance-criteria/` folders
- Detection: manual diff review against the Constraints section
- Required fix: revert the out-of-scope change before this AC is marked done

## Performance Requirements
- **Change-Scope Bar:** 100% of touched files must fall within the declared scope (`docs/`, this plan's `user-stories/`, `documentation/`, `acceptance-criteria/`) — zero tolerance for out-of-scope writes, since this is the story's core descope guarantee
- **Reversibility:** All changes are plain-text markdown edits, trivially revertible via `git revert` if scope creep is detected post-merge

## Security Considerations
- **No Cross-Repo Write Capability Exercised:** Confirms the workspace write-access boundary asserted throughout this story (and the 2026-07-05 `/refine-epic` audit) is actually respected in practice, not just documented
- **No Secret/Path Leakage:** The one permitted reference to the external repo's path (`~/Documents/GitHub/claude-prompts/.claude/skills/`) is already public within this plan's artifacts and contains no credentials

## Test Implementation Guidance
**Test Type:** MANUAL (git diff audit)
**Test Data Requirements:** `git diff --name-only <base>...<story-branch>` output compared against the declared allow-list of paths
**Mock/Stub Requirements:** None

## Definition of Done
**Auto-Verified:**
- [ ] No linting errors
- [ ] `git status` shows no untracked/staged changes outside the declared scope
- [ ] Build succeeds (docs-only change; no build artifacts affected)

**Story-Specific:**
- [ ] `git diff --name-only` contains only `docs/external-migration.md`, optional `docs/README.md`/`docs/skill-usage.md` cross-link, and this plan's `user-stories/`, `documentation/`, `acceptance-criteria/` files
- [ ] No path under `~/Documents/GitHub/claude-prompts/` appears as a write target in the diff
- [ ] `docs/README.md`'s existing entries remain unchanged aside from the single new cross-link addition (if that file is the one modified)

**Manual Review:**
- [ ] Code reviewed and approved
