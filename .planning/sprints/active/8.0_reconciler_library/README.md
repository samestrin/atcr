# Sprint 8.0: Reconciler Library Extraction

**Status:** Created (not yet executed)
**Branch:** `feature/8.0_reconciler_library`
**Plan Type:** Feature ✨
**Execution:** `/execute-sprint` (gated 🚧 — stops at each phase boundary)

---

## Overview

Extract ATCR's deterministic reconciler from `internal/reconcile` into a standalone, embeddable Go module at `./reconcile/` (`github.com/samestrin/atcr/reconcile`), consumed by ATCR via a root `replace` directive. The public API is lifted **as-is** to guarantee byte-identical behavior. Ships a `reconcile-json/v1` JSON adapter, README + runnable godoc example, dual licensing (Apache 2.0 + commercial placeholder), and independent module CI. Turns the core architectural moat into a separable asset that other tools can embed.

---

## Configuration

| Field | Value |
|-------|-------|
| Complexity | 9/12 (COMPLEX) |
| Timeline | 10 days |
| Phases | 5 |
| Pattern | Foundation → Core Extraction → Consumer Flip → Adapter & Docs → CI & Validation |
| TDD Mode | Moderate 🔄 (RED → GREEN → REFACTOR) |
| Adversarial | ENABLED 🎯 (inline-fix CRITICAL/HIGH; defer MEDIUM/LOW) |
| Execution Mode | Gated 🚧 |
| User Stories | 6 |
| Acceptance Criteria | 25 |

---

## Timeline

| Phase | Name | Duration | Stories |
|-------|------|----------|---------|
| 1 | Foundation & Scaffold | 2 days | 2 (partial) |
| 2 | Core Extraction | 3 days | 1, 2 (completion) |
| 3 | Consumer Import-Flip | 2 days | 1 (completion) |
| 4 | Adapter, Docs & Licensing | 2 days | 3, 4, 5 |
| 5 | CI, Leaderboard & Validation | 1 day | 6 + final |

---

## Expected Outcomes

- `./reconcile/` nested module with stdlib-only `go.mod` and root `replace` directive.
- All 9 ATCR consumer packages re-import the library; byte-identical fixtures preserved; zero behavioral change.
- `reconcile-json/v1` JSON adapter (decode single/array → `[]Source`; encode `Result` → versioned envelope).
- README with godoc + runnable `ExampleReconcile()`, install + quickstart, licensing section.
- Apache 2.0 `LICENSE` + `LICENSE-COMMERCIAL.md` placeholder; no enforcement code.
- Independent module CI (tag-push + PR-time job) and `docs/scorecard.md` reference-implementation citation.

---

## Risk Summary (Top 3)

1. **`emit.go`/`discover.go` type/IO split is error-prone** — public types entangled with file I/O. Mitigation: split mechanically, types first, compile-check both packages, then relocate I/O; never move I/O and types in the same commit.
2. **Root `go test ./...` does not cross the nested `go.mod` boundary** — library regressions hide silently. Mitigation: dual CI jobs are mandatory; PR-time `./reconcile` job + boundary-gap proof.
3. **Consumer scope broader than the original epic list (9 packages)** + **golangci-lint version drift (`ci.yml` v2.6.2 vs target v2.12.2)**. Mitigation: use the 2026-06-23 audit as canonical source; grep-verify no residual re-declarations; pin a single golangci-lint version before Phase 5.

---

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Phase-by-phase TDD task plan (executable) |
| [metadata.md](metadata.md) | Plan + sprint tracking |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (created/referenced) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth |
| [plan/user-stories/](plan/user-stories/) | 6 user stories |
| [plan/acceptance-criteria/](plan/acceptance-criteria/) | 25 acceptance criteria |
| [plan/documentation/](plan/documentation/) | Go module, API, JSON adapter, testing, CI references |

---

**Next:** `/refine-sprint @.planning/sprints/active/8.0_reconciler_library/`
