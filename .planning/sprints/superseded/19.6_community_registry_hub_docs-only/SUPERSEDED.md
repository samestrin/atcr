# SUPERSEDED — Docs-Only Scope (retired 2026-07-07)

This sprint implemented the **retired** docs-only framing of Epic 19.6: "add a recommendation to install a default majors persona pack to `docs/personas-install.md` and the README Quickstart" (complexity 2/12, no code).

It was **never executed** (all task checkboxes were unchecked, no commits landed against it) and is preserved here for traceability only.

## Why superseded

Epic 19.6 was rescoped on 2026-07-07 from a docs-only recommendation into a real in-repo feature: **community-canonical persona distribution** (fetched from `samestrin/atcr`, not embedded), **structured model metadata + model-aware discovery**, a **model-indexed, human-named persona library**, and **onboarding-hierarchy docs** that lead with `atcr quickstart` (Synthetic). See the current epic and its `Rescope & Pivot (2026-07-07)` section:

- `.planning/epics/active/19.6_community_registry_hub.md`

The matching superseded plan bundle is at:

- `.planning/plans/superseded/19.6_community_registry_hub_docs-only/`

## Do not

- Do not run `/execute-sprint` against this directory.
- Do not treat its `sprint-plan.md` / `plan/` as current requirements.

The regenerated sprint will be produced by re-running the pipeline (`/init-plan` → … → `/create-sprint`) on the rewritten epic.
