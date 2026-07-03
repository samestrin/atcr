# Sprint 17.0: Auto-Merged Fixes Execution

**Status:** Active — Ready for Execution
**Branch:** `feature/17.0_auto_merged_fixes`
**Type:** ✨ Feature | **Complexity:** 11/12 (VERY COMPLEX)
**Execution:** Gated 🚧 + Adversarial 🎯 | **TDD:** Strict 🔒

---

## Overview

Enable ATCR to actively apply the fixes it generates. An opt-in `--auto-fix` flow parses a model-generated diff, applies the patch to the local working tree safely, runs a configurable local validation command, auto-reverts on failure, and — only after validation passes — orchestrates a GitHub branch, commit, and pull request. The core safety guarantee: **no GitHub-mutating call is reachable before local validation has passed.**

## Timeline

**14 days across 7 phases** (gated — `/execute-sprint` stops at each phase boundary).

| Phase | Name | Duration | Work |
|-------|------|----------|------|
| 1 | Research & Spike | 1 day | Spike `go-gitdiff` + GitHub Git Data API 4-call sequence |
| 2 | Foundation | 4 days | Story 1 (apply) + Story 2 (validation) |
| 3 | Core Items | 2 days | Story 3 (auto-revert) |
| 4 | Advanced | 4 days | Story 4 (branch/commit) + Story 5 (PR) |
| 5 | Integration | 2 days | Story 6 (`--auto-fix` gate wiring) |
| 6 | Testing | 1 day | Cross-story integration + zero-HTTP-on-failure regression |
| 7 | Validation | (folded) | 23-AC DoD + quality gates + drift analysis |

## Expected Outcomes

- `internal/autofix` — `ApplyPatch` / `Revert` over `atomicfs`, wrapping `go-gitdiff`.
- `internal/verify` — configurable local validation runner with a conservative exit-code-only gate.
- `internal/ghaction.Client` — `CreateBranch`, `CreateCommit`, `CreatePullRequest`, `UpdatePullRequest` reusing existing `postDo`/`get` plumbing.
- `cmd/atcr` — `--auto-fix` opt-in flag (off by default) with an all-or-nothing refuse-without-backend gate.

**Success criteria:** auto-fix ≥ 70% of simple technical-debt items; zero broken builds introduced in the test corpus; default behavior byte-identical when the flag is absent.

## Risk Summary (top 3)

1. **Cross-system rollback gap** — a pushed branch or opened PR cannot be undone by the local revert. Mitigation: sequence so no GitHub-mutating call happens until local validation passes; branch/PR creation is the last step (encoded in Story 4/5's dependency on Story 2). Verified by Phase 6's zero-HTTP-on-failure regression test.
2. **New `go-gitdiff` dependency, fuzzy hunk matching** — could mis-apply a drifted-context hunk instead of failing cleanly. Mitigation: Phase 1 spike targets drifted fixtures; any non-nil `gitdiff.Apply` error is a hard per-file failure, no custom leniency.
3. **Git Data API 4-call partial failure** — blob → tree → commit → ref can leave orphaned objects or a stale ref. Mitigation: surface a clear `APIError` naming the failed step; orphaned blobs/trees are inert (GitHub GCs them); explicit method+path stub routing plus the Phase 6 cross-check guard against false-green tests.

Full risk analysis: [plan/sprint-design.md](plan/sprint-design.md#risk-analysis).

## Sprint Assets

| File | Purpose |
|------|---------|
| [sprint-plan.md](sprint-plan.md) | Executable phase/task plan (gated, strict + adversarial) |
| [metadata.md](metadata.md) | Plan + sprint tracking, complexity, execution mode |
| [sprint-knowledge.yaml](sprint-knowledge.yaml) | Knowledge entries created/referenced by this sprint |
| [plan/](plan/) | Archived source plan (requirements, stories, ACs, design, docs) |
| [plan/sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy, risk analysis |
| [plan/original-requirements.md](plan/original-requirements.md) | Source of truth |

## Execution

```
/execute-sprint @.planning/sprints/active/17.0_auto_merged_fixes/
```

Gated mode: execution stops after each phase's GATE task. Resume to advance to the next phase.
