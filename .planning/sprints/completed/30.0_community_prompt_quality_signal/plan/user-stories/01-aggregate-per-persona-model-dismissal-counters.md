# User Story 1: Aggregate Per-Persona+Model Dismissed/Confirmed Counters

**Plan:** [30.0: Community Prompt Quality Signal](../plan.md)

## User Story

**As a** the atcr maintainer (Sam Estrin), preparing to gate and surface a content-free quality signal
**I want** Epic 24.0's per-finding `wontfix`/`resolved` dismissal outcomes aggregated into per-persona+model dismissed/confirmed counters
**So that** there is a single, well-defined, content-free counter set for the opt-in gate (Story 2), local preview (Story 3), and maintainer report (Story 4) to consume, instead of each of those stories re-deriving counts from raw debt records independently

## Story Context

- **Background:** Epic 24.0 already records dismissal outcomes as terminal `status` values (`wontfix`/`resolved`) appended to `.atcr/debt/` JSONL records via `atcr debt resolve` (`cmd/atcr/debt_resolve.go` `markDebtResolved`). Each `localdebt.Record` (`internal/localdebt/record.go:24-44`) carries `Reviewers []string` (persona attribution, unioned across merged findings by `internal/reconcile/merge.go`'s `unionReviewers`) but has no `Model` field. `internal/scorecard/aggregate.go`'s `Aggregate()` is the established per-(reviewer, model) grouping idiom (group, sum, sort deterministically) this story's aggregation must follow structurally, and `internal/scorecard/telemetry.go`'s `HashPersonaID`/`TelemetryPersonaRecord` is the reusable pseudonymization primitive for the persona identifier once aggregation produces raw persona names.
- **Assumptions:**
  - Codebase discovery during this story (confirmed by repo research) found that **Model cannot be reliably derived from the existing `RunID` field**: the original open record's `RunID` is set by `persistLocalDebt` (`cmd/atcr/reconcile.go:254,273`) to `res.Summary.ReconciledAt + "-" + filepath.Base(reviewDir)` — a composite string with no delimiter-safe reverse mapping back to a fan-out review directory — and `markDebtResolved` (`cmd/atcr/debt_resolve.go:325`) additionally overwrites `RunID` on the resolution record itself with `now + "-" + status`, destroying even that composite on the very record this aggregation reads. There is no existing helper that reconstructs a `fanout` review directory from a `localdebt.Record.RunID`.
  - Given the above, the aggregation needs `Model` attached directly to the record at write time rather than derived after the fact. The approach (confirmed at design-sprint and pinned in the sprint plan, Phase 1.1–1.3) is a `localdebt.SchemaVersion` bump (1 → 2) adding an optional `Model` field to `Record`, populated by `persistLocalDebt` from the same `fanout.AgentStatus.Model` data it already has in scope when it writes the open record. Records written under schema v1 (no `Model`) are read as before and excluded from per-model aggregation (bucketed as attribution-incomplete), never causing a read error.
  - `Reviewers` can legitimately hold more than one persona name on a single record (multi-agent merged findings, per `unionReviewers`) — this story's aggregation must define and document a single, deterministic attribution rule for that case (e.g., attribute the outcome to every listed persona) rather than silently picking `Reviewers[0]` and dropping the rest.
- **Constraints:** The aggregation reads only fields already present on `localdebt.Record` (or the new `Model` field once added) — no code, file path, problem/fix text, or justification is ever copied into the aggregated shape. Output must be a fixed, allowlisted struct (mirroring `internal/telemetry/event.go`'s zero-`omitempty`, exactly-fixed-fields pattern) so downstream stories (2/3/4) and the future `internal/telemetry/quality_signal.go` payload type have one unambiguous shape to depend on. This story does not implement the opt-in gate, the `--preview` render, the maintainer report, or the actual network send — it only produces the aggregated counters and the allowlisted payload type.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** A new aggregation function (mirroring `internal/scorecard/aggregate.go`'s `Aggregate()` grouping/sorting idiom) reads `.atcr/debt/` local debt records and produces one row per `(persona, model)` pair with `dismissed_count` (status `wontfix`) and `confirmed_count` (status `resolved`) totals; a new allowlisted payload type in `internal/telemetry/quality_signal.go` defines the exact, fixed field set of one aggregated row, following `Event`'s no-`omitempty` pattern.
- **Measurable:** A table-driven test with a fixture set of debt records (including multi-reviewer merged records, mixed schema-v1/v2 records, and both terminal statuses) produces hand-verified exact counts per `(persona, model)` pair; a dedicated allowlist regression test (mirroring `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys`) asserts the new payload struct's field set never silently grows to include code, file paths, or finding text.
- **Achievable:** Reuses the existing `Aggregate()` grouping/sort idiom, `HashPersonaID` pseudonymization primitive, and `localdebt.ReadAll` read path verbatim — the only new mechanism is the `Model`-attachment schema change, which follows the codebase's existing `SchemaVersion` forward/backward-compatibility precedent.
- **Relevant:** This aggregation is the sole data source every other story in this plan (opt-in gate wiring, preview, maintainer report) depends on; without a correct, deterministic per-persona+model counter set, none of those surfaces has anything trustworthy to gate, preview, or report.
- **Time-bound:** Deliverable first within the sprint, ahead of Stories 3 and 4 which structurally depend on this story's aggregation output and payload type existing.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-per-persona-model-aggregation.md) | Per-(Persona, Model) Dismissed/Confirmed Aggregation | Unit |
| [01-02](../acceptance-criteria/01-02-model-field-schema-bump-and-exclusion.md) | `Model` Field Schema Bump and Attribution-Incomplete Exclusion | Unit/Integration |
| [01-03](../acceptance-criteria/01-03-multi-persona-reviewers-attribution.md) | Multi-Persona `Reviewers` Attribution Rule | Unit |
| [01-04](../acceptance-criteria/01-04-append-only-record-fold-by-id.md) | Append-Only Record Fold by ID Before Aggregation | Unit |
| [01-05](../acceptance-criteria/01-05-allowlisted-quality-signal-payload-type.md) | Allowlisted `quality_signal.go` Payload Type with Locking Regression Test | Unit |

## Original Criteria Overview

1. Given a set of local debt records with `wontfix`/`resolved` terminal statuses across multiple personas and models, the aggregation produces exactly one summed row per `(persona, model)` pair with correct dismissed and confirmed counts, sorted deterministically.
2. Records lacking a resolvable model (schema v1, or any record where model attribution cannot be established) are excluded from per-model rows rather than corrupting counts with an empty/guessed model value, and a multi-persona `Reviewers` record attributes its outcome to every listed persona per a single documented rule.
3. The new `internal/telemetry/quality_signal.go` payload type carries only an allowlisted, fixed field set (persona identifier, model, dismissed count, confirmed count) with a locking regression test — no code, file path, or finding-content field can be added without failing that test.


## Technical Considerations

- **Implementation Notes:** Add a `Model` field to `internal/localdebt.Record` behind a `SchemaVersion` bump (1 → 2, following the existing forward/backward-compat contract documented on `SchemaVersion`), populated by `persistLocalDebt` (`cmd/atcr/reconcile.go`) from the same `fanout.AgentStatus.Model` data already available at that write site — this avoids the RunID-reconstruction dead end found during discovery. Add a new aggregation function (e.g. `internal/localdebt/qualitysignal.go` or a sibling of `internal/scorecard/aggregate.go` — confirm exact placement at design-sprint) that reads records via `localdebt.ReadAll`, folds the append-only stream by `ID` the same way `cmd/atcr/debt_resolve.go`'s `selectOpenDebt` does (a later terminal record wins), then groups by `(persona, model)` and sums outcomes. Reuse `scorecard.HashPersonaID` when producing the eventual hashed-identifier payload row (the raw persona name is fine inside the aggregation step itself; hashing happens at the payload-construction boundary, matching `NewTelemetryPersonaRecord`'s existing split between internal aggregation and outbound-payload construction).
- **Integration Points:** `internal/localdebt/record.go` (schema bump), `cmd/atcr/reconcile.go` (`persistLocalDebt`, the only write site that has `fanout.AgentStatus.Model` in scope), `cmd/atcr/debt_resolve.go` (`isClosedStatus`/`selectOpenDebt` fold logic, reused read-side pattern), `internal/scorecard/aggregate.go` (grouping/sort idiom to mirror), `internal/scorecard/telemetry.go` (`HashPersonaID` reuse), new `internal/telemetry/quality_signal.go` (payload type, consumed by Stories 3 and 4).
- **Data Requirements:** `localdebt.Record` gains an optional `Model string` field (`omitempty`, schema v2); the aggregation's internal shape is `{Persona, Model string; DismissedCount, ConfirmedCount int}` per group; the outbound payload type in `quality_signal.go` allowlists exactly the pseudonymized/identifier fields needed for transmission (persona identifier — hashed or raw per design-sprint decision — model, dismissed count, confirmed count), with no `omitempty` on any allowlisted field, matching `Event`'s pattern.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Model cannot be derived from existing records (RunID is non-reversible, no field carries it today) — confirmed by codebase discovery in this story | High | Add `Model` directly to `localdebt.Record` via a `SchemaVersion` bump populated at write time (`persistLocalDebt`) instead of attempting a fragile after-the-fact RunID→reviewDir reconstruction; document schema v1 records as attribution-incomplete and excluded from per-model rows |
| Multi-persona `Reviewers` records (merged findings) get attributed to only `Reviewers[0]`, silently under-counting or mis-attributing dismissal outcomes for the other listed personas | Medium | Define and test a single explicit attribution rule (attribute the outcome to every listed persona) rather than picking one; cover with a fixture record carrying 2+ reviewers in the aggregation test |
| New payload/aggregation struct accidentally grows a field that leaks finding content (problem/fix text, file path, justification) as the epic evolves | High | Mirror `internal/telemetry/event.go`'s locked-allowlist pattern exactly: a dedicated regression test asserts the payload struct's exact field set, failing any future addition until the test itself is updated |
| Aggregation logic quietly diverges from `internal/scorecard/aggregate.go`'s established grouping/sort conventions (e.g., non-deterministic ordering), making downstream report/preview output flaky across runs | Low | Copy the `Aggregate()` sort-then-group idiom (stable sort, explicit tie-break) verbatim rather than reimplementing grouping from scratch |

---

**Created:** July 17, 2026
**Status:** Draft - Awaiting Acceptance Criteria
