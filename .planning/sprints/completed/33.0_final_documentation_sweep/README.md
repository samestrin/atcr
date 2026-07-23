# Sprint 33.0: Final Code Review + Documentation Sweep

**Type:** Technical Debt 🔧
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 8 days
**Phases:** 5
**Execution Mode:** Gated 🚧 (adversarial ENABLED, inline-fix CRITICAL/HIGH)
**Branch:** `feature/33.0_final_documentation_sweep`

---

## Overview

Following the completion of the core engine documentation updates (Epic 22.0) and the built-in reviewer persona renaming/generalization (Epic 23.0), the `atcr` repository needs a final hardening pass before launch. This sprint is the first in the launch cluster (`33.0` review + docs → `33.1` launch content → `33.2` go-public + install + atcr.dev launch) and runs before the repo is made public, so the code and history are reviewed and hardened before public exposure.

Two risks are closed, in order: (1) latent correctness/security/quality issues shipping into a public release with no final review gate, and (2) documentation discrepancies or stale references (lingering mentions of `sentinel`, `tracer`, or `idiomatic`) misleading users or the website. Both are resolved against the *final* production code — review and fix first, then document.

See [metadata.md](metadata.md) for full plan and tracking details, and [sprint-plan.md](sprint-plan.md) for the executable phase-by-phase plan.

---

## Timeline

| Phase | Focus | Duration | Items |
|-------|-------|----------|-------|
| 1. Foundation | Code Review Execution | 1 day | Task 1, Task 2 (parallel) |
| 2. Core Items | Findings Triage & Remediation | 3 days | Task 3 |
| 3. Advanced | Documentation & Persona Audit | 2 days | Task 4, Task 5, Task 7 |
| 4. Integration | Website Compatibility Check | 1 day | Task 6 |
| 5. Validation | Final Verification Pass | 1 day | Task 8 |

**Total:** 8 days (7-day task-effort baseline + 1-day risk buffer for Task 3's unknown CRITICAL/HIGH finding volume)

---

## Expected Outcomes

- A reconciled, evidence-grounded code-review findings report (atcr's own multi-agent reviewer + manual adversarial pass) over `cmd/`, `internal/`, `reconcile/`, `skill/`.
- Every CRITICAL/HIGH finding fixed directly in the codebase via RED → GREEN → ADVERSARIAL → REFACTOR; MEDIUM/LOW findings sharded into `.planning/technical-debt/README.md`.
- `README.md`, `docs/` (29 files), `skill/SKILL.md`, and CLI help text verified accurate against the finalized code.
- Zero legacy persona-slug references (`sentinel`, `tracer`, `idiomatic`) remaining in documentation or command help screens; `sasha`/`penny`/`ingrid` used consistently.
- All 29 `docs/` files validated as clean, self-contained, and ready for `atcr.dev` import.
- A fresh, end-to-end guard run (persona/AC3 suite, `go vet`, `golangci-lint`, `go test -race ./...`, `reconcile/` submodule tests) confirming AC1-AC5 with cited evidence.

---

## Risk Summary (Top 3)

1. **Unknown finding volume:** The multi-agent review (Task 1) or adversarial pass (Task 2) could surface far more CRITICAL/HIGH findings than the original 2-3 day estimate assumed. Phase 2's 3-day duration already carries a 1-day risk buffer; if insufficient, the correct response is re-scoping (defer more to TD) rather than silently extending Phase 2.
2. **Secret discovered in git history:** If Task 2's history scan finds a committed secret, remediation (`git filter-repo`/BFG) is a destructive, high-blast-radius operation out of scope for this sprint's tasks — any such finding is escalated to the user for explicit approval before any action.
3. **Cross-package regression from Task 3 fixes:** A CRITICAL/HIGH fix in one package could break adjacent code no single task's own test run would catch. Task 8's fresh, full-repo `go test -race ./...` plus the `reconcile/` submodule's separate suite is the closing gate designed specifically to catch this.

---

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — executable phase-by-phase task plan (this sprint's primary working document)
- [metadata.md](metadata.md) — plan/sprint metadata and execution tracking
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — knowledge manifest (created/referenced entries)
- [plan/](plan/) — copied source plan (original-requirements.md, sprint-design.md, tasks/, documentation/, codebase-discovery.json)
