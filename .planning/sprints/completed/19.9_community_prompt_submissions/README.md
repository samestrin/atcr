# Sprint 19.9: Community Prompt Submissions (Intake & Curation)

**Type:** ✨ Feature | **Complexity:** 9/12 (COMPLEX) | **Timeline:** 8 days | **Phases:** 5
**TDD Mode:** Moderate 🔄 | **Adversarial:** ENABLED 🎯 (inline CRITICAL/HIGH) | **Execution:** Gated 🚧
**Branch:** `feature/19.9_community_prompt_submissions`

---

## Overview

Establish a GitHub-native intake and curation flow so users contribute refined, battle-tested reviewer prompts back to the canonical library — turning "I improved my local copy" into "I opened a PR" in one command, without a marketplace, website, or hosted registry. Adds `atcr personas submit <name>` (local fixture gate + fork+PR via `gh`) and a two-tier curation model via a `submitted` status that stays orthogonal to the existing `Source` provenance field until a maintainer graduates the persona into the vetted `personas/community/` library.

## Timeline

| Phase | Focus | Story | Est. |
|-------|-------|-------|------|
| 1 — Foundation | Local fixture-gate reuse & submission blocking (`newPersonasSubmitCmd`) | Story 1 | 1.5d |
| 2 — Core | Fork + PR automation via `gh` (injectable seam) | Story 2 | 2.5d |
| 3 — Core | `submitted` status distinct from `Source` (atomic marker) | Story 3 | 2d |
| 4 — Integration | Documentation: graduation + submit flow & two-tier model (docs-only) | Stories 4 & 5 | 1d |
| 5 — Validation | Full-suite verification, risk-profile review, drift analysis | All | 1d |

Each phase runs RED → GREEN → ADVERSARIAL (fresh subagent) → REFACTOR, followed by a Phase-Boundary Gate. In gated mode, `/execute-sprint` **stops at each phase boundary** for a go-ahead.

## Expected Outcomes

- `atcr personas submit <name>` — seventh `personas` subcommand, fixture-gated, opens a fork+PR under the user's own `gh` auth
- Injectable `gh` fork/branch-push/PR-create seam (`Fork`/`PushBranch`/`CreatePR`) — fully stubbable, zero live calls in tests
- `submitted` status marker: attribution + fixture-pass + timestamp, atomically persisted outside the vetted tree, distinct from `Source`
- Docs: maintainer graduation checklist + `submit` subcommand + `submitted` → graduated two-tier model

## Risk Summary (Top 3)

1. **`gh` argument injection / auth exposure** — mitigated: array-argument `gh.ExecContext` only (never shell-concatenated), persona name validated before reaching any `gh` argument, raw `gh auth` output never logged.
2. **`submitted` implemented as a fourth `Source` value** — mitigated: separate struct/marker; explicit test asserts `Source` ∈ {built-in, community, project}; `List*` signatures untouched.
3. **Marker-without-PR inconsistent state / marker TOCTOU** — mitigated: marker written only after a successful PR; `writeFileAtomic`/`atomicfs.WriteFileAtomic` exclusively (symlink-refusing, sibling-temp-then-rename).

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable TDD phases & tasks |
| [metadata.md](metadata.md) | Plan + sprint tracking (source of truth) |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (created/referenced) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [plan/original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [plan/user-stories/](plan/user-stories/) | 5 user stories |
| [plan/acceptance-criteria/](plan/acceptance-criteria/) | 15 acceptance criteria |
| [plan/test-planning-matrix.md](plan/test-planning-matrix.md) | AC → test-level mapping |

---

**Next:** `/refine-sprint @.planning/sprints/active/19.9_community_prompt_submissions/`
