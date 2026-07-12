# Migrating the private `claude-prompts` skills to the `/atcr` dispatcher

atcr's standalone skill ([`skill/SKILL.md`](../skill/SKILL.md)) is a single `/atcr <command>` dispatcher. The private `claude-prompts` skills (`execute-code-review`, `reconcile-code-review`) that drive the internal `.planning/` sprint workflow can adopt the same dispatcher pattern — but that migration is a **manual operator action**, documented here, not something this repository's tooling performs.

## Why this is a manual step

The private skills live in a **separate repository** (`~/Documents/GitHub/claude-prompts/.claude/skills/`), outside this repository's workspace. Automated tooling operating in the `atcr` repo has write access only to `/Users/samestrin/Documents/GitHub/atcr`, so it cannot edit, stage, or commit files in `claude-prompts`. Migrating those skills is therefore left to a manual operator step.

This is a deliberate descope, **not** an open compatibility question. Epic 12.0 (Skill Integration) already validated the private-skill backward-compatibility end-to-end from the external (`claude-prompts`) side, and this repository additionally locks the documented `atcr review --output-dir` + `atcr reconcile` backend contract with a repo-local regression test (see [`docs/code-review-backend.md`](code-review-backend.md)). The private skills remain fully supported.

## Manual migration checklist

When you next update the `claude-prompts` repository, migrate its atcr-related skills to the dispatcher pattern:

1. **Replace the fragmented private skills** (`execute-code-review`, `reconcile-code-review`, and related single-capability skills) with a single `atcr` dispatcher skill.
2. **Copy or adapt the dispatcher template** from this repo's [`skill/SKILL.md`](../skill/SKILL.md) (and its on-demand siblings `host-review.md`, `ambiguity-adjudication.md`, `findings-format.md`).
3. **Preserve any `.planning/` sprint-workflow hooks** the private skills still rely on — the dispatcher changes the skill's entrypoint UX, not its integration with the private sprint workflow.
4. **Validate against the backend contract** in [`docs/code-review-backend.md`](code-review-backend.md) — the `--output-dir` + `reconcile` output tree and findings formats — the same contract the repo-local regression test locks.

## Scope boundary

This document is the deliverable; the migration itself is performed manually in the `claude-prompts` repository. This repository's tooling makes no writes to `~/Documents/GitHub/claude-prompts/`.

## Related

- [`skill/SKILL.md`](../skill/SKILL.md) — the `/atcr <command>` dispatcher template.
- [`docs/skill-usage.md`](skill-usage.md) — standalone skill installation and usage.
- [`docs/code-review-backend.md`](code-review-backend.md) — the `--output-dir` backend contract to validate against.
