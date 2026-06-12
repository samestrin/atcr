# Sprint 1.0: atcr-core-review

## Metadata

**Plan:** 1.0_atcr_core
**Type:** Feature (✨)
**Complexity:** 9/12 (VERY COMPLEX)
**TDD Mode:** Moderate 🔄
**Adversarial Review:** ENABLED 🎯
**Execution Mode:** Gated 🚧
**Timeline:** 11 days
**Phases:** 5
**Created:** June 10, 2026

---

## Overview

Build v1 of atcr (Agent Team Code Review): a standalone Go binary (CLI + MCP server) that fans a code change out to a panel of heterogeneous LLM reviewer personas, then deterministically reconciles their findings into a single deduplicated, confidence-scored deliverable.

**Original Request:** [plan/original-requirements.md](plan/original-requirements.md)
**Sprint Design:** [plan/sprint-design.md](plan/sprint-design.md)
**Sprint Plan:** [sprint-plan.md](sprint-plan.md)

---

## Timeline

| Phase | Days | Focus |
|-------|------|-------|
| 1. Foundation | 1-2 | Go module scaffold, cobra CLI, config loading |
| 2. Core Systems | 3-5 | Git range resolution, payload engine, LLM client |
| 3. Engines | 6-8 | Fan-out concurrency, reconciler, report rendering |
| 4. Integration | 9-10 | MCP server, Skill, end-to-end validation |
| 5. Validation & Docs | 11 | Documentation, CI examples, final validation |

---

## Expected Outcomes

### Deliverables

- Go binary with 6 subcommands: `review`, `reconcile`, `report`, `range`, `init`, `serve`
- Deterministic reconciler pipeline with Jaccard-based deduplication
- Three payload modes: diff, blocks, files
- MCP server with 5 tools
- Agent Skill for host-model review and orchestration
- Two-tier config system with precedence resolution

### Success Criteria

- All 24 acceptance criteria passing (14 unit + 10 integration)
- Coverage ≥70%
- `go vet ./...` and `golangci-lint run` clean
- End-to-end pipeline works with mock provider

---

## Risk Summary (Top 3)

1. **Deterministic dedupe accuracy** - Jaccard token-set similarity may under/over-merge vs. prompt-based judgment
   - Mitigation: Conservative threshold (≥0.7), ambiguous.json sidecar for gray zone (0.4-0.7), fixture corpus testing

2. **Payload builder edge cases** - `blocks` mode struggles with languages without braces, binary files, renames
   - Mitigation: Fallback to plain `-U<n>` context diff, explicit tests per edge case

3. **Token cost surprises** - `blocks`/`files` modes on large ranges may exceed budgets
   - Mitigation: Byte budgets with recorded truncation, documentation steers large ranges to `diff` mode

**Full Risk Analysis:** [plan/sprint-design.md#risk-analysis](plan/sprint-design.md#risk-analysis)

---

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Task checklist and execution guide |
| [metadata.md](metadata.md) | Sprint tracking and metrics |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge manifest |
| [plan/](plan/) | Copied plan artifacts |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture and decomposition |
| [plan/original-requirements.md](plan/original-requirements.md) | User's actual request |
| [plan/user-stories/](plan/user-stories/) | 6 user stories |
| [plan/acceptance-criteria/](plan/acceptance-criteria/) | 24 acceptance criteria |
| [plan/documentation/](plan/documentation/) | Technical documentation |

---

## Execution

**Start:** `/execute-sprint @.planning/sprints/active/1.0_atcr_core/`

**Gated Mode:** Sprint stops at each phase boundary for human review before proceeding.

**Adversarial Review:** Each AC includes a subagent review step. CRITICAL/HIGH findings fixed inline, MEDIUM/LOW deferred to tech debt.

---

**Next:** `/validate-sprint @.planning/sprints/active/1.0_atcr_core/`
