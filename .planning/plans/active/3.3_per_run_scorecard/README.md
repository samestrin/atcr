## Overview
Per-Run Scorecard (Plan 3.3) adds a persistence and projection layer over the existing `atcr reconcile` pipeline. Every reconcile run already computes the quality signal — per-reviewer attribution, corroboration counts, verification verdicts — but that signal is currently discarded. This plan captures it as versioned JSONL records in `~/.config/atcr/scorecard/YYYY-MM.jsonl` and exposes it via two new CLI commands: `atcr scorecard` (single-run view) and `atcr leaderboard` (aggregated, filterable view). The same schema becomes the submission format for Epic 10.0's public Model-Eval Leaderboard.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/3.3_per_run_scorecard/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/3.3_per_run_scorecard/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/3.3_per_run_scorecard/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/3.3_per_run_scorecard/`

## Timeline & Milestones
| Milestone | Target |
|-----------|--------|
| Plan created | 2026-06-15 |
| User stories + AC | TBD |
| Sprint design | TBD |
| Implementation (llmclient prerequisite + emitter + CLI + export) | ~1 week |
| Docs + tests | concurrent with implementation |

## Resource Requirements
- **Runtime:** Go 1.x toolchain
- **Storage:** `~/.config/atcr/scorecard/` (user-local, never committed)
- **Dependencies:** Epic 1.0 reconcile output (complete), Epic 3.0 verification.json (optional), llmclient usage parsing (hard prerequisite — must resolve first)

## Expected Outcomes
1. Per-reviewer scorecard records written automatically after each `atcr reconcile` run.
2. `atcr scorecard [id-or-path]` displays a formatted table of per-reviewer metrics for a single run.
3. `atcr leaderboard [--since, --model, --persona]` ranks reviewer performance across runs.
4. `atcr leaderboard --export` emits the versioned, anonymized public submission JSON.
5. `--no-scorecard` flag suppresses emission for test/dry runs.
6. `docs/scorecard.md` documents schema, storage, CLI usage, and privacy model.

## Risk Summary
| Risk | Severity | Mitigation |
|------|----------|------------|
| JSONL unbounded growth | Low | Monthly rotation; user can delete old months |
| Export format locked too early | Medium | `schema_version` from day one; v1 marked experimental |
| Cost/token columns always empty | High (prerequisite) | llmclient usage parsing must resolve first; without it, columns are empty, not degraded |

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
