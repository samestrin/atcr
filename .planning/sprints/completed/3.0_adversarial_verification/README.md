# Sprint 3.0: Adversarial Verification Sprint

**Status:** Active  
**Created:** June 14, 2026  
**Timeline:** 10 days (5 phases)  
**Complexity:** 8/12 — COMPLEX  
**Execution Mode:** Gated 🚧 | Adversarial Review: ENABLED 🎯

---

## Overview

Adds an adversarial verification stage to the `atcr` review pipeline. After `atcr reconcile` produces unique, deduped findings, `atcr verify` runs skeptic agents — a different model from any reviewer credited on the finding — who attempt to disprove each finding using the Epic 2.0 tool loop (real code access). Verdicts feed a second confidence axis: `VERIFIED > HIGH > MEDIUM > LOW`. Refuted findings are demoted to LOW and retained for audit (never deleted). The `--fail-on` gate counts only non-refuted findings, making the CI gate trustworthy enough to block merges.

---

## Timeline

| Phase | Name | Days | Key Deliverable |
|-------|------|------|----------------|
| Phase 1 | Foundation | 2 | `internal/verify/select.go`, `AgentsByRole`, `SelectEligibleSkeptics` |
| Phase 2 | Core — Skeptic Invocation | 3 | `skeptic.go`, `verdict.go`, `invoke.go`, `votes.go` (7 verdict cases) |
| Phase 3 | Advanced — Confidence v2 & Re-emit | 2 | `confidence_v2.go`, `emit_*.go`, gate counter update |
| Phase 4 | Integration — CLI, MCP, Gate | 2 | `atcr verify`, `atcr_verify` MCP, `--require-verified`, gate matrix |
| Phase 5 | Validation — Report, Docs, Fixtures | 1 | Report v2 rendering, `docs/verification.md`, fixture corpus |

---

## Expected Outcomes

- `atcr verify` CLI subcommand with `--fresh`, `--thorough`, `--min-severity` flags
- `atcr_verify` MCP tool registered with identical behavior to CLI
- `atcr review --verify` chains review → reconcile → verify
- `--fail-on <severity> --require-verified` gate semantics
- Confidence v2: VERIFIED (confirmed), HIGH (2+ reviewers unverified), MEDIUM (single reviewer), LOW (refuted)
- Report rendering: VERIFIED tier, skeptic panel, collapsed Refuted section
- `docs/verification.md` (mechanics, confidence v2, gate semantics)
- `docs/registry.md` updated with `role: skeptic`
- `docs/findings-format.md` updated with verification block schema

---

## Risk Summary (Top 3)

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Over-eager skeptics refute true findings | Medium | High | Conservative prompt ("refute only with concrete evidence"); `--thorough` majority rule; fixture corpus tracks refutation accuracy |
| Cost doubling on large reviews | Medium | Medium | `min_severity` floor (default MEDIUM), dedup-first placement, skip-already-verified, per-finding budgets |
| Import cycle `verify` ↔ `reconcile` | Low | High | `verify` imports `reconcile`, NOT vice-versa; gate counter stays in `reconcile/gate.go`; verify with `go build ./...` after Phase 1 scaffolding |

---

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable task checklist (35 tasks across 5 phases + 5 gates) |
| [metadata.md](metadata.md) | Tracking metrics, populated by `/execute-sprint` |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (created/referenced entries) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, phase structure, test strategy |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth — original request verbatim |
| [plan/user-stories/](plan/user-stories/) | 6 user stories |
| [plan/acceptance-criteria/](plan/acceptance-criteria/) | 28 acceptance criteria |
| [plan/documentation/](plan/documentation/) | Verification pipeline, CLI/MCP, tool loop, testing fixture docs |

---

**Run:** `/execute-sprint @.planning/sprints/active/3.0_adversarial_verification/`
