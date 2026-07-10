# Task 05: Judgment Classification Rule Documentation

**Source:** Plan 19.8 – Objective 5
**Priority:** P2 | **Effort:** S | **Type:** Add

## Problem Statement
There is no unambiguous, repo-anchored specification of how a hermes judgment agent should classify a drift finding from `atcr models check --json` as "minor" (routine, hand to the mechanical slug-bump PR flow) vs. "major/deprecation" (needs a re-tune task with the persona's vendor prompting-guide context). `internal/personas/drift.go` defines the three condition types a finding can carry (`ConditionNewerMember`, `ConditionDeprecation`, `ConditionMissing`), but nothing maps those conditions to a classification outcome or specifies what a re-tune task must carry. Without this documented rule, a hermes-side judgment skill (brian/`glm-5.1` or cole/`kimi-k2.7-code`) cannot be built correctly, and any implementation would have to guess at the boundary between "mechanical" and "needs a re-tune task" — risking a deprecation or major-model-change being silently routed as a routine mechanical bump.

## Solution Overview
Append a `## Judgment Classification Rule` section to `docs/hermes-maintenance-agents.md` (the file created by Task 03, which already stakes out this section as a placeholder anchor: `_To be completed by Task 05._`). The new section maps each `DriftFinding.Condition` value to a minor/major classification, specifies the exact payload a "re-tune task" must carry (persona name, old model slug, new/suggested model slug, vendor prompting-guide URL), and names the judgment role's hermes agents (brian/`glm-5.1` or cole/`kimi-k2.7-code`) per the Role → Agent Configuration table Task 03 already populated. This is a documentation-only task — the classification logic itself runs hermes-side, not in this repo.

## Acceptance Criteria Coverage
This task directly contributes to the following acceptance criteria from `original-requirements.md`:
- **AC2** — defines the minor vs. major/deprecation classification rule and the vendor-guide URL payload for re-tune tasks.

## Technical Implementation
### Steps
1. Read the current state of `docs/hermes-maintenance-agents.md` (as left by Task 03) to confirm the `## Judgment Classification Rule` placeholder heading and its exact text, and to match the doc's existing heading level and table style.
2. Replace the placeholder body (`_To be completed by Task 05._`) under `## Judgment Classification Rule` with the classification mapping table, grounded in the actual `DriftFinding.Condition` constants from `internal/personas/drift.go:23-26`:

   | Condition (`DriftFinding.Condition`) | Meaning | Classification | Routing |
   |---|---|---|---|
   | `ConditionNewerMember` (`"newer-member"`) | A newer family member is available for the persona's locked slug; no deprecation involved. | Minor | Hand to the mechanical agent's slug-bump PR flow (Task 01/02) — `CurrentSlug` → `SuggestedSlug`. |
   | `ConditionDeprecation` (`"deprecation"`) | The persona's current locked model is deprecating/expiring (`ExpirationDate` set). | Major | Open a re-tune task — this is a forced move off a model going away, not a routine version bump. |
   | `ConditionMissing` (`"missing"`) | The persona's locked slug is absent from the catalog snapshot (no `SuggestedSlug`, no baseline to compare). | Minor by default | Classify minor and route to the mechanical flow UNLESS it co-occurs with a `ConditionDeprecation` finding for the same persona in the same `atcr models check --json` run, in which case treat as major (the missing slug and the deprecation are the same underlying event). |

3. Document the "same persona, same run" co-occurrence rule explicitly: the judgment agent must group findings by `DriftFinding.Persona` within a single JSON array (one `atcr models check --json` invocation) before applying the classification above, since `ConditionMissing` alone must not be escalated to major on its own.
4. Specify the re-tune task's required payload fields, sourced entirely from data already present in a `DriftFinding` plus the target persona's file:
   - **Persona** — `DriftFinding.Persona` (the persona name/slug).
   - **Old model** — `DriftFinding.CurrentSlug`.
   - **New/recommended model** — `DriftFinding.SuggestedSlug` when present (newer-member case); when absent (pure deprecation or missing with no suggestion), state the re-tune task must carry `"none suggested — requires manual selection"` rather than a fabricated slug.
   - **Vendor prompting-guide URL** — extracted from the target persona's `<!-- vendor-guidance: ... -->` preamble comment (e.g. `personas/community/anthony.md:1`), which every community persona carries exactly one of (enforced by `personas/community_test.go`'s `vendorGuidanceRe` check). Document the exact marker format the judgment agent must parse: `<!-- vendor-guidance: <description>, <url> -->`.
5. Name the judgment role's hermes agents per Task 03's Role → Agent Configuration table: brian (`glm-5.1`) or cole (`kimi-k2.7-code`) — a light classification task, not a drafting task.
6. Cross-reference `atcr models check --json`'s exact JSON finding schema as the sole input this classification logic parses: `renderDriftJSON` at `cmd/atcr/models.go:304`, invoked from `runModelsCheck` at `cmd/atcr/models.go:174` via `newModelsCheckCmd` at `cmd/atcr/models.go:153`, with documented exit codes (0 = clean, 1 = conditions found, 2 = usage failure) so the judgment agent knows exit code 1 is what triggers this classification pass at all.
7. State explicitly that this section records a rule for a hermes-side skill to implement; no atcr-repo code changes are required or expected by this task.

## Files to Create/Modify
- `docs/hermes-maintenance-agents.md` – replace the `## Judgment Classification Rule` placeholder (added by Task 03) with the full classification mapping table, co-occurrence rule, re-tune task payload spec, and judgment agent naming

## Documentation Links
- [GitHub Action Docs](../../../../docs/github-action.md) — doc structure precedent

## Related Files (from codebase-discovery.json)
- `internal/personas/drift.go`
- `cmd/atcr/models.go`

## Success Criteria
- [ ] `## Judgment Classification Rule` section maps each DriftFinding condition type (`ConditionNewerMember`, `ConditionDeprecation`, `ConditionMissing`) to minor/major
- [ ] The `ConditionMissing` + co-occurring `ConditionDeprecation` escalation rule is documented explicitly
- [ ] Re-tune task payload (persona, old model, new/suggested model, vendor prompting-guide URL) is unambiguously specified, including the exact `<!-- vendor-guidance: ... -->` marker format
- [ ] Judgment role's hermes agents (brian/`glm-5.1` or cole/`kimi-k2.7-code`) are named
- [ ] Section is implementable by a hermes-side skill with no further atcr-repo changes

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- N/A — documentation-only task

**Integration Tests:**
- N/A — verified by manual read-through against `internal/personas/drift.go`'s actual condition types and `personas/community_test.go`'s `vendorGuidanceRe` marker format

**Test Files:**
- N/A

## Risk Mitigation
- Risk: classification rule documented ambiguously, causing a hermes skill to misroute a deprecation as minor → Mitigation: explicit 1:1 mapping table (condition type → minor/major) grounded in the actual `DriftFinding` struct fields and constants, not paraphrased, plus an explicit co-occurrence rule for the `ConditionMissing` edge case.
- Risk: re-tune task payload omits the vendor prompting-guide URL or invents one → Mitigation: payload spec sources the URL directly from the persona's existing, test-enforced `<!-- vendor-guidance: ... -->` preamble comment rather than describing a new lookup mechanism.

## Dependencies
- Task 03 (Configuration Surface Documentation Skeleton) — extends the same `docs/hermes-maintenance-agents.md` file and its pre-staked `## Judgment Classification Rule` placeholder

> **Downstream note:** The re-tune task payload this task specifies is consumed by [Task 06: Drafting Agent Contract Documentation](task-06-drafting-agent-contract.md).

## Definition of Done
- [ ] Judgment classification rule section written and appended to `docs/hermes-maintenance-agents.md`, replacing the Task 03 placeholder
- [ ] Mapping table grounded in actual `DriftFinding` condition types and constants from `internal/personas/drift.go`
