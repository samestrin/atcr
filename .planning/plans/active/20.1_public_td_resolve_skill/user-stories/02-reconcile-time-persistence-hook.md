# User Story 2: Reconcile-Time Persistence Hook

**Plan:** [20.1: Public TD Resolve Skill](../plan.md)

## User Story

**As a** standalone/public atcr user with no `.planning/` directory
**I want** `atcr reconcile` to automatically append reconciled findings into the local `.atcr/`-scoped technical-debt store, with an opt-out flag
**So that** findings from repeated review runs accumulate into a durable backlog instead of being lost the moment a review's own directory is cleaned up or overwritten

## Story Context

- **Background:** `atcr reconcile` (`cmd/atcr/reconcile.go`) already has a precedent for a post-reconcile, opt-out-able side effect: scorecard emission runs after `internal/reconcile.RunReconcile` returns, guarded by a `--no-scorecard` flag (`cmd/atcr/reconcile.go:43`, emit call at `cmd/atcr/reconcile.go:110-111`). Story 1 builds the local TD store package (recommended: `internal/localdebt`) that this story writes into. Without this hook, the store from Story 1 is inert — nothing ever populates it from a live `atcr reconcile` run, so standalone users would have no path from "review found issues" to "issues are tracked."
- **Assumptions:**
  - Story 1 has produced a store package with an `Append`-style API (mirroring `internal/scorecard/store.go`'s atomic-append JSONL pattern) that this story calls directly — no HTTP/IPC boundary.
  - `internal/reconcile/gate.go:255`'s `stampJustifications` has already run by the time `runReconcile` in `cmd/atcr/reconcile.go` receives its `Result`, so `Justification`/`SourceReport` fields are present on findings without any change to the gate pipeline.
  - Repo root for the store is CWD (`Root: "."`), matching the existing convention already used for finding-path validation in the same function (`cmd/atcr/reconcile.go:99`).
- **Constraints:**
  - Must not change the exit-code/gate semantics of `atcr reconcile` — persistence is a side effect, not a gate input, exactly like scorecard emission (best-effort, logged on failure, never fails the command).
  - Must follow the `--no-scorecard` flag's naming and behavior convention (a new `--no-local-debt` flag, boolean, defaults to persistence-on).
  - Must not introduce a `.planning/` dependency anywhere in the hook or the store it calls — this is the standalone/public code path.
  - Placement in `cmd/atcr/reconcile.go` must run after the scorecard emit block (~line 111) so the persisted record can carry the same already-enriched `Result` scorecard consumes, keeping the two side effects visibly parallel.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 1 (Local TD store) |

## Success Criteria (SMART Format)

- **Specific:** `atcr reconcile` calls the Story 1 store's append API once per run with the current run's reconciled findings (including `Justification`/`SourceReport` when present), gated by a new `--no-local-debt` opt-out flag that mirrors `--no-scorecard`'s behavior and defaults to off (persistence enabled).
- **Measurable:** Running `atcr reconcile` twice against two different review directories in the same repo results in a local store that contains both runs' findings, queryable/verifiable by reading the store's records after each run; running with `--no-local-debt` produces zero new records.
- **Achievable:** The integration point, flag pattern, and post-`RunReconcile` insertion point are already established precedent in the same file (`cmd/atcr/reconcile.go:110-111`); this story adds one function call and one flag, not new architecture.
- **Relevant:** Directly satisfies AC2 — without this hook, Story 1's store is unreachable from any live command, and the plan's core promise (accumulating backlog across runs) is unmet.
- **Time-bound:** Completed within the current sprint, sequenced immediately after Story 1 lands.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-persist-reconciled-findings.md) | Persist Reconciled Findings Into the Local TD Store | Integration |
| [02-02](../acceptance-criteria/02-02-no-local-debt-opt-out-flag.md) | `--no-local-debt` Opt-Out Flag | Integration |
| [02-03](../acceptance-criteria/02-03-cross-run-accumulation-and-dedup.md) | Cross-Run Accumulation With Write-Time Dedup | Integration |

## Original Criteria Overview

1. `atcr reconcile` persists the current run's reconciled findings into the local `.atcr/`-scoped store (from Story 1) after the reconcile `Result` is available, carrying `Justification`/`SourceReport` fields when present.
2. A new `--no-local-debt` flag opts out of persistence for a single run, following the same flag shape, help text style, and best-effort/non-fatal failure behavior as the existing `--no-scorecard` flag.
3. Re-running `atcr reconcile` against the same or a different review directory accumulates findings in the store rather than overwriting or losing prior runs' records, with an explicit, documented decision on duplicate handling (write-time dedup by `FindingID`, read-time dedup, or accepted at-least-once semantics) rather than leaving the question open.

## Technical Considerations

- **Implementation Notes:** Add the persistence call in `cmd/atcr/reconcile.go`'s `runReconcile`, immediately after the `scorecard.EmitForReconcile` call (~line 111), passing the same `res *reconcile.Result` scorecard receives plus `Root: "."` for repo-root resolution — matching the convention already set for finding-path validation earlier in the same function. Read the new `--no-local-debt` flag via `cmd.Flags().GetBool("no-local-debt")` in the same style as the existing `noScorecard` read, and register the flag in `newReconcileCmd()` alongside `--no-scorecard` (`cmd/atcr/reconcile.go:43`).
- **Integration Points:** `cmd/atcr/reconcile.go:runReconcile` (hook site); Story 1's store package (append API consumer); `internal/reconcile/justification.go`'s `stampJustifications` output (data source, already enriched by the time `runReconcile` runs — no change needed to `internal/reconcile/gate.go`).
- **Data Requirements:** Each persisted record must carry, at minimum, the fields the Story 3 skill route needs to autonomously resolve items later: finding identity/anchor, file/line, severity, problem/justification text, and `SourceReport` back-reference. Exact schema is Story 1's responsibility; this story is a consumer of that schema, not its designer — resolve any mismatch by coordinating with Story 1's interface rather than inventing a second shape.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Re-running `atcr reconcile` on the same review directory (e.g., after a partial fix) silently duplicates records in the append-only store, inflating the backlog with stale/duplicate entries | Medium | Decide and document duplicate handling explicitly during design (write-time dedup by `FindingID` is the safer default given the store is append-only and has no read-time compaction yet); do not leave it undecided per the open design question already flagged in the plan's discovery notes |
| Persistence failure (e.g., disk full, `.atcr/` unwritable) is treated as fatal and breaks `atcr reconcile`'s primary gate function for standalone users | High | Mirror scorecard's best-effort contract exactly: log the failure via the existing diagnostics channel (`cmd.ErrOrStderr()`) and never return an error from the persistence call path, consistent with `scorecard.EmitForReconcile`'s documented behavior |
| `--no-local-debt` flag naming or defaults drift from the `--no-scorecard` precedent, creating an inconsistent CLI surface for standalone users learning both flags | Low | Directly copy the `--no-scorecard` flag's declaration shape (`cmd.Flags().Bool(...)`), help-text tone, and default-false semantics rather than designing a new convention |

---

**Created:** July 11, 2026
**Status:** Draft - Awaiting Acceptance Criteria
