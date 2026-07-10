# Sprint 19.7: Live Model Resolution, Lockfile & Drift Detection

**Status:** Created — awaiting `/refine-sprint` then `/execute-sprint`
**Branch:** `feature/19.7_live_model_resolution`
**Type:** ✨ Feature | **Complexity:** 10/12 (VERY COMPLEX) | **Timeline:** 15 days | **Phases:** 8
**Mode:** Strict 🔒 TDD | Adversarial 🎯 ON (inline: CRITICAL/HIGH) | Gated 🚧

---

## Overview

Layer live, auto-updating model resolution over the persona `model` bindings Epic 19.6 shipped. A persona binds to a model *family/channel*; a resolved **lock** records the concrete slug that actually runs; the model changes only on an explicit `atcr personas upgrade`, never silently mid-review. This is a resolution layer *on top of* 19.6's static slugs — 19.6's pinned `model` becomes the initial lock (zero migration).

## Timeline

| Phase | Story | Focus | Est. |
|-------|-------|-------|------|
| 1 | Catalog Routability Spike & Stable-Channel Heuristic | Authenticated spike; `@stable` heuristic; `z-ai/` prefix (manual, no code) | 1 day |
| 2 | Family/Channel Binding & Resolved Lock | Additive binding schema; `Model` field is the lock; AC7 gate unaffected | 2 days |
| 3 | Hybrid Resolver | Catalog client + alias / created-timestamp / explicit-pin + `@stable`/`@latest` | 3.5 days |
| 4 | Reproducible Upgrade | Resolver into `Upgrade()`; before→after report; resolution isolated to upgrade | 2 days |
| 5 | `atcr models check` | Net-new command; drift/deprecation/missing; `--json`; 0/1/2 exit codes | 2 days |
| 6 | Major-Bump Re-Validation Gate | `semver.Major` gate + verify flag; minor auto-locks | 1 day |
| 7 | init/quickstart Roster Reconciliation | Index-derived roster; closes 19.6 TD-011 HIGH; single reconciliation point | 1.5 days |
| 8 | Snapshot Fixture, Refresh Command & Docs | Checked-in fixture; `models refresh`; docs | 2 days |

**Dependency graph:** 1 → 2 → 3 → {4, 5} → 6 (needs 3+4); 7 independent (may parallelize); 8 needs 2+3+5.

## Expected Outcomes

- Personas ride each vendor family's capability curve without hand-editing slugs, while reviews stay reproducible by default.
- `atcr personas upgrade` advances locks explicitly with before→after reporting; no silent runtime model change.
- `atcr models check [--json]` gives a deterministic drift/deprecation/missing report (the seam Epic 19.8 wraps).
- 19.6's deferred roster/index HIGH (TD-011) is closed: online `init`/`quickstart` deliver a working, non-noisy persona set.
- CI stays zero-live-network via a checked-in catalog snapshot + maintainer-only refresh command.

## Risk Summary (top 3)

1. **`~`-alias non-routability** — the hybrid resolver's `created`-timestamp / explicit-pin fallback covers a negative AC1 result without blocking the epic (`HAS_GATED_WORK: false`).
2. **`@stable`/`expiration_date`/preview interaction ambiguity** (AC 03-04) — must be decided explicitly during Phase 3 (task 3.10), not left implicit.
3. **Two-call-site drift (TD-006/TD-007 pattern) in init/quickstart** — mitigated by a single shared roster-derivation point (Phase 7, locked Option B decision).

Full risk analysis (security-sensitive areas, performance paths, edge cases, defensive measures): [plan/sprint-design.md](plan/sprint-design.md#risk-analysis).

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable TDD phase/task plan (gated, adversarial) |
| [metadata.md](metadata.md) | Sprint tracking + execution metrics |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (created/referenced entries) |
| [plan/](plan/) | Archived planning artifacts (source of truth) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risks |
| [plan/original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [plan/user-stories/](plan/user-stories/) | 8 user stories |
| [plan/acceptance-criteria/](plan/acceptance-criteria/) | 25 acceptance criteria |
| [plan/documentation/](plan/documentation/) | OpenRouter API, existing patterns, fixture, command design, semver |

---

**Next:** `/refine-sprint @.planning/sprints/active/19.7_live_model_resolution/`
