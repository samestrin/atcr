# Task 06: Drafting Agent Contract Documentation

**Source:** Plan 19.8 – Objective 6
**Priority:** P2 | **Effort:** S | **Type:** Add

## Problem Statement
There is no unambiguous, repo-anchored specification of how the drafting agent should turn a re-tune task into a prompt-edit PR. `docs/hermes-maintenance-agents.md` (Task 03) already stakes out a `## Drafting Agent Contract` placeholder heading (`_To be completed by Task 06._`), but its body is unwritten. Without it, a cross-vendor LLM (marcus/`openai/qwen-3.7-plus`) drafting a re-tune of another vendor's prompting guidance risks restructuring the mandatory persona contract, mixing a prompt edit into a mechanical slug-bump PR, or producing a change with no explicit human-approval gate.

## Solution Overview
Replace the Task 03 placeholder under `## Drafting Agent Contract` in `docs/hermes-maintenance-agents.md` with the full contract: the input (the re-tune task payload from Task 05), the edit procedure (read the persona's `<!-- vendor-guidance: ... -->` preamble → fetch the cited guide → edit only the body below the preamble, preserving the mandatory persona section structure byte-for-byte, updating the preamble's URL/description if the guide moved), the model assignment (marcus/`openai/qwen-3.7-plus` default, nolan/`glm-5.2` fallback, per Task 03's Role → Agent Configuration table), and the hard output contract (separate PR from any mechanical PR, never auto-merge-eligible, human approval required, must pass the reused C3 fixture gate before it is even reviewable).

## Acceptance Criteria Coverage
This task directly contributes to the following acceptance criteria from `original-requirements.md`:
- **AC3** — specifies how the drafting agent turns a re-tune task into a separate, LLM-drafted prompt-edit PR.
- **AC4** — mandates separate PRs, explicit human approval, and no auto-merge for prompt changes.
- **AC5** — requires the agent-authored prompt to pass the reused C3 fixture gate before review.

## Technical Implementation
### Steps
1. Read the current state of `docs/hermes-maintenance-agents.md` (as left by Tasks 03 and 05) to confirm the `## Drafting Agent Contract` placeholder heading and its exact text, and to match the doc's existing heading level, table style, and cross-reference conventions.
2. Replace the placeholder body (`_To be completed by Task 06._`) with a subsection documenting the contract's **input**: the re-tune task payload Task 05 specifies — persona slug (`DriftFinding.Persona`), old model (`DriftFinding.CurrentSlug`), new/recommended model (`DriftFinding.SuggestedSlug`, or `"none suggested — requires manual selection"` when absent), and the vendor prompting-guide URL extracted from the persona's `<!-- vendor-guidance: ... -->` preamble.
3. Document the **edit procedure** as an ordered sequence:
   a. Read the target persona's `.md` file and parse its `<!-- vendor-guidance: <description>, <url> -->` preamble comment (the same marker Task 05 documents, test-enforced by `personas/community_test.go`'s `vendorGuidanceRe` at lines 96-98 — a non-empty value is required, but the convention is free-text vendor/guide description followed by a URL, as seen in `personas/community/anthony.md:1`, `gene.md:1`, `celeste.md:1`).
   b. Fetch the current guide at the cited URL.
   c. Edit only the persona body **below** the preamble comment — never the preamble's HTML-comment line itself except as described in (d).
   d. If the guide's location or title changed, update the preamble's description/URL to match; otherwise leave the preamble untouched.
4. Document the **invariant**: the mandatory persona structure from `docs/personas-authoring.md` (`## Role` / `## Focus` / `## Scope` / `## Severity Rubric` / `## Output Format` (the 7-column reviewer-finding contract) / `## Payload`) must never be restructured, reordered, renamed, or have headings added/removed — the drafting agent may only change prose content *within* these existing sections.
5. Document that the drafting agent must never touch the paired `.yaml` (provider/model binding) file unless the re-tune task payload explicitly calls for a model change (i.e., carries a concrete `SuggestedSlug`, not the "none suggested" placeholder) — a pure prompt re-tune touches the `.md` only.
6. Document the **model assignment**: marcus (`openai/qwen-3.7-plus`) is the default drafting agent — prompt re-tuning is prose/instruction work, not code, and marcus's 1M context window can ingest a full vendor guide, the current persona file, and its fixtures in one pass; nolan (`glm-5.2`) is the fallback when strict schema/template precision matters more than prose quality. Cross-reference Task 03's Role → Agent Configuration table and Drafting Model Default & Fallback section rather than restating the assignment as a new decision.
7. Document the **hard output contract**:
   - Opens a PR **separate** from any mechanical slug-bump PR — never mixed into the same PR or commit.
   - The PR touches `personas/community/*.md` only (and, when explicitly authorized per Step 5, the paired `.yaml`) — a path Task 02's auto-merge structural filter explicitly excludes, so this PR is structurally incapable of matching the mechanical auto-merge path.
   - The PR is explicitly a **draft** and requires explicit human approval before merge; it never auto-merges under any configuration.
   - Before the PR is even reviewable, it must pass the reused C3 guardrail chain unmodified: schema validation (`internal/registry/validate.go`), length caps (`internal/tools/limits.go`), and the fixture gate (`internal/personas/community_fixture_test.go`, `internal/personas/community_schema_test.go`).
8. State explicitly that this section records a contract for a hermes-side skill to implement; no atcr-repo code changes are required or expected by this task.

## Files to Create/Modify
- `docs/hermes-maintenance-agents.md` – replace the `## Drafting Agent Contract` placeholder (added by Task 03) with the full input spec, edit procedure, structural invariant, model assignment, and hard output contract

## Documentation Links
- [Persona Authoring Contract](../../../../docs/personas-authoring.md) — the 7-column Output Format contract the drafting agent must preserve
- [GitHub Action Docs](../../../../docs/github-action.md) — doc structure precedent

## Related Files (from codebase-discovery.json)
- `docs/personas-authoring.md`
- `personas/community/anthony.md`
- `personas/community/gene.md`
- `personas/community/celeste.md`
- `personas/community_test.go`
- `internal/registry/validate.go`
- `internal/tools/limits.go`
- `internal/personas/community_fixture_test.go`
- `internal/personas/community_schema_test.go`

## Success Criteria
- [x] `## Drafting Agent Contract` placeholder replaced with content specifying the re-tune task input payload
- [x] Edit procedure (read vendor-guidance preamble → fetch guide → edit body only → conditionally update preamble) is unambiguous and ordered
- [x] Invariant preserving the mandatory 7-section persona structure byte-for-byte (content-only edits) is stated
- [x] `.yaml` binding file is documented as off-limits unless the re-tune task explicitly calls for a model change
- [x] Model assignment (marcus/`openai/qwen-3.7-plus` default, nolan/`glm-5.2` fallback) documented, cross-referencing Task 03 rather than restating it
- [x] Output contract states: separate PR from mechanical, structurally excluded from Task 02's auto-merge path filter, always a draft, human approval required, never auto-merges, must pass the reused C3 fixture gate before review

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- N/A — documentation-only task

**Integration Tests:**
- N/A — verified by manual read-through against `docs/personas-authoring.md`'s section contract and an actual community persona `.md` file's vendor-guidance preamble (e.g. `personas/community/anthony.md:1`)

**Test Files:**
- N/A

## Risk Mitigation
- Risk (Low): vendor prompting-guide fetch fails or returns stale content, producing a low-quality draft → Mitigation (already documented at the epic level): output is explicitly a draft requiring human approval before merge; a bad draft costs review time, not correctness.
- Risk: drafting agent restructures the mandatory persona contract → Mitigation: contract explicitly forbids touching anything but content within existing sections, preserving the 7-section structure (including the 7-column Output Format) byte-for-byte.
- Risk: a prompt-edit PR is accidentally mixed with a mechanical slug-bump PR, or slips through the mechanical auto-merge path → Mitigation: contract mandates a separate PR touching only `.md` (and conditionally `.yaml`) paths, which Task 02's structural path filter already excludes by design — never gated on authorship.

## Dependencies
- Task 03 (Configuration Surface Documentation Skeleton) — replaces the `## Drafting Agent Contract` placeholder it staked out in the same `docs/hermes-maintenance-agents.md` file, and its Role → Agent Configuration / Drafting Model Default & Fallback sections are cross-referenced rather than restated
- Task 05 (Judgment Classification Rule) — this task's input is the re-tune task payload Task 05 specifies

## Definition of Done
- [x] Drafting agent contract section written, replacing the Task 03 placeholder in `docs/hermes-maintenance-agents.md`
- [x] Contract is implementable by a hermes-side skill (marcus) with no further atcr-repo changes
