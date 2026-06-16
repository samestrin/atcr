# Sprint 3.3: Scorecard Pipeline

**Status:** Ready for Execution
**Created:** June 15, 2026
**Branch:** `feature/3.3_per_run_scorecard`
**Duration:** 10 days
**Complexity:** 9/12 (COMPLEX)
**Execution Mode:** Gated 🚧
**Adversarial Review:** ENABLED 🎯 (inline: CRITICAL/HIGH; defer: MEDIUM/LOW)

---

## Overview

Emit a normalized per-reviewer eval record alongside each `atcr reconcile` run, accumulate records into a local monthly JSONL store (`~/.config/atcr/scorecard/YYYY-MM.jsonl`), and expose them via `atcr scorecard` (single run) and `atcr leaderboard` (aggregated view). This is the monitoring foundation for the quality pipeline and the data prerequisite for Epic 10.0's public Model-Eval Leaderboard.

---

## Timeline

| Phase | Focus | Duration |
|-------|-------|----------|
| 1 | Hard Prerequisite — llmclient usage parsing | 1 day |
| 2 | Core Emitter — Story 1: Auto-emit Scorecard | 2 days |
| 3 | CLI Commands — Stories 2 & 3 | 2 days |
| 4 | Export + Suppression — Stories 4 & 5 | 2 days |
| 5 | Documentation + Integration Testing — Story 6 | 1 day |
| Final | Validation | 1 day |
| **Total** | | **10 days** |

---

## Expected Outcomes

- `internal/llmclient/` — usage decoding: `tokens_in`, `tokens_out`, `cost_usd` populated
- `internal/scorecard/` — new package: `scorecard.go`, `store.go`, `aggregate.go`, `export.go`
- `cmd/atcr/scorecard.go` — `atcr scorecard [id-or-path]` command
- `cmd/atcr/leaderboard.go` — `atcr leaderboard [--since, --model, --persona, --export]` command
- `--no-scorecard` flag on `atcr reconcile`
- `docs/scorecard.md` — schema, storage, CLI usage, privacy model
- All 21 acceptance criteria verified

---

## Risk Summary

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Hard prerequisite (llmclient) blocks cost/token fields | Medium | High | Phase 1 resolves it first; sprint blocked if Phase 1 cannot complete |
| Anonymization misses PII field in public export | Low | High | Allowlist-based field selection; unit tests assert no paths/hostnames/API keys in output |
| Export schema locked too early as public API surface | Medium | Medium | `schema_version: 1` from day one; documented as experimental until Epic 10.0 stabilizes |

---

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable sprint plan — all tasks, TDD phases, adversarial reviews, gates |
| [metadata.md](metadata.md) | Sprint metadata and execution tracking |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest (created/referenced entries) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, risk analysis |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth — original request |
| [plan/user-stories/](plan/user-stories/) | 6 user stories |
| [plan/acceptance-criteria/](plan/acceptance-criteria/) | 21 acceptance criteria |

---

## Next Step

```
/execute-sprint @.planning/sprints/active/3.3_per_run_scorecard/
```
