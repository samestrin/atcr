# Sprint 2.0: Tool-Using Reviewers

**Sprint Number:** 2.0
**Plan Type:** ✨ Feature
**Feature Category:** Agent Engine
**Created:** 2026-06-13
**Status:** Ready
**Branch:** `feature/2.0_tool_using_reviewers`

---

## Overview

Turn the pool reviewers from single-shot prompted calls into bounded multi-turn agents. Each reviewer can explore the repository through read-only, path-jailed tools (`read_file`, `grep`, `list_files`) exposed via OpenAI-compatible function calling, with the Go engine owning the entire tool harness.

The payload becomes the starting point of a review, not the universe. Reviewers can look up what they need — callers, adjacent files, config values — and produce findings backed by evidence actually read.

---

## Timeline

**Total Duration:** 13 days

| Phase | Name | Days | Stories |
|-------|------|------|---------|
| 1 | Research & Spike | Day 1 | Spike (wire format, path jail, worktree) |
| 2 | Foundation | Days 2-3 | Story 7 (Tool Definitions & Dispatcher) + Story 3 (Path Jail & Snapshot) |
| 3 | Core Items | Days 4-6 | Story 1 (Agent Loop Execution) + Story 2 (Budget Enforcement) |
| 4 | Advanced | Days 7-8 | Story 4 (Graceful Degradation) |
| 5 | Integration | Days 9-10 | Story 5 (Transcript & Accounting) + Story 6 (Persona Guidance & Docs) |
| 6 | Testing & Validation | Days 11-13 | End-to-end, final regression, docs |

**Complexity:** 10/12 (VERY COMPLEX)
**TDD Mode:** Strict 🔒
**Execution Mode:** Gated 🚧 (stops at each phase boundary)
**Adversarial Review:** ENABLED 🎯 (CRITICAL/HIGH inline, MEDIUM/LOW deferred)

---

## Expected Outcomes

- **Multi-turn agent loop** driving `tool_calls` exchanges in `internal/fanout/engine.go`
- **Tool harness** (`read_file`, `grep`, `list_files`) in `internal/tools/` — read-only, path-jailed
- **Snapshot manager** with live-worktree fast path and `git worktree` slow path
- **Budget enforcement** per agent: `max_turns`, `tool_budget_bytes`, `timeout_secs`
- **Graceful degradation** for non-tool-capable models (`tools_degraded: true`)
- **Transcript writer** (`raw/<agent>/transcript.jsonl`) + live status.json counters
- **Persona guidance** for tool-enabled agents with evidence-citation rule
- **Documentation**: `docs/registry.md`, `docs/payload-modes.md`, README cost guidance

**No new third-party dependencies.**

---

## Risk Summary (Top 3)

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Path jail escape (TOCTOU between EvalSymlinks and Open) | Medium | Critical | `O_NOFOLLOW` where supported; documented threat model; spike validation in Phase 1 |
| Provider function-calling dialect variance | Medium | Medium | Strict lowest-common-denominator wire format; litellm normalizes most providers; degrade path for the rest |
| Token cost explosion from multi-turn loops | Medium | High | Hard per-agent budgets; counters in status.json; 3-10× cost guidance in README |

---

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Full sprint implementation plan with all tasks |
| [metadata.md](metadata.md) | Sprint tracking, metrics, execution status |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge entries referenced by this sprint |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, phase structure, test strategy |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth — original user request |
| [plan/user-stories/](plan/user-stories/) | 7 user stories |
| [plan/acceptance-criteria/](plan/acceptance-criteria/) | 30 acceptance criteria |
| [plan/plan.md](plan/plan.md) | Full plan document |

---

**Next:** `/execute-sprint @.planning/sprints/active/2.0_tool_using_reviewers/`
