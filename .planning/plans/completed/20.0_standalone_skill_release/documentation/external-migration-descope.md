# External Private-Skill Migration Descope

`[REFERENCE]`

## Overview

The original epic proposed migrating the private `claude-prompts` skills (`execute-code-review`, `reconcile-code-review`) to the same monolithic `/atcr <command>` dispatcher pattern (Proposed Solution #3). Two prior `/refine-epic` passes determined that this migration cannot be automated from the `atcr` workspace because those skills live in an external repository (`~/Documents/GitHub/claude-prompts/.claude/skills/`) that is outside this workspace's write access.

This epic therefore **descopes** the actual private-skill file migration to a documented manual operator follow-up action. The local work is limited to:

1. Rewriting `skill/SKILL.md` in this repo as the dispatcher template.
2. Documenting the migration as a manual step.

## Why It Is Out of Scope

- Workspace boundary: the agent only has access to `/Users/samestrin/Documents/GitHub/atcr`.
- Epic 12.0 already validated the private-skill backward-compatibility end-to-end from the external side.
- AC3 in this epic is satisfied by a repo-local test against the documented `docs/code-review-backend.md` contract, not by re-touching the external skills.

## Migration Checklist (Manual Operator Action)

When the operator later updates the `claude-prompts` repo, they should:

1. Replace the existing fragmented skills with a single `atcr` skill.
2. Copy or adapt the dispatcher template from `skill/SKILL.md` in this repo.
3. Preserve any `.planning/` sprint workflow hooks that the private skills still need.
4. Validate against the same `docs/code-review-backend.md` contract.

## Quick Reference

| Item | Decision | Source |
|---|---|---|
| Migrate private skills automatically | Out of scope | original-requirements.md Out of Scope; codebase-discovery.json integration_points |
| Validate private-skill compatibility | Repo-local contract test only (AC3) | original-requirements.md AC3; codebase-discovery.json architecture_notes |
| Dispatcher template | Written locally as `skill/SKILL.md` | plan.md Implementation Strategy |

## Related Documentation

- [../original-requirements.md](../original-requirements.md) — Proposed Solution #3 and Out of Scope sections.
- [../plan.md](../plan.md) — Implementation Strategy and Risk Mitigation for cross-repo scope creep.
- [codebase-discovery.json](../codebase-discovery.json) — `integration_points` and `architecture_notes` describing the external `claude-prompts` migration as a manual follow-up.
