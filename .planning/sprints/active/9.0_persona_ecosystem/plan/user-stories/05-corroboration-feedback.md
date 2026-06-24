# User Story 5: Corroboration Feedback via Persona Scores

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** platform lead managing a team's ATCR review configuration
**I want** to run `atcr personas list --scores` and see each installed persona's corroboration rate on my team's actual codebase
**So that** I can make data-driven decisions about which domain personas are earning their keep and which to retire or replace

## Story Context

- **Background:** Teams install domain personas (bonus built-ins like `sentinel`, `tracer`, `idiomatic`, or community personas) hoping they will surface more relevant issues than the generalist reviewers. Today there is no feedback loop — a persona either stays installed indefinitely or gets removed by gut feel. Epic 3.3 introduced a scorecard that accumulates corroboration data per reviewer across runs; this story wires that existing data into a visible `--scores` flag on `atcr personas list`, closing the ROI loop without adding new data collection.
- **Assumptions:** The scorecard JSONL file (`~/.config/atcr/scorecard.jsonl` or equivalent) is already being written by the corroboration pipeline. `scorecard.Aggregate()` returns a `[]LeaderboardRow` where each row has a `ReviewerName string` and `CorroborationRate float64`. The `internal/personas` package (T2) exposes a `List()` function that returns installed persona metadata. At least one review run with corroboration enabled has occurred before `--scores` is meaningful; zero-data personas show `n/a`.
- **Constraints:** No new data collection — only existing `scorecard.Aggregate()` output is used. The `--scores` flag is additive to the existing `atcr personas list` output; baseline behavior without the flag must not change. The corroboration rate map shape (`map[string]float64`, keyed by reviewer name) is already decided by T8's `SelectEligibleSkeptics` 4th parameter — this story reuses that same map to avoid caller churn.
- **Documentation Reference:** See [Per-Persona Corroboration Scores](../documentation/scorecard-corroboration.md) for scorecard storage, aggregation, and the shared map shape with T8.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | T2 (`atcr personas` CLI + `internal/personas` package), T6 scorecard wiring, Epic 3.3 scorecard (`scorecard.Aggregate()` + `LeaderboardRow.CorroborationRate`) |

## Success Criteria (SMART Format)

- **Specific:** `atcr personas list --scores` outputs a table with one row per installed persona showing name, source, and corroboration rate (as a percentage to one decimal place, or `n/a` when no runs exist for that reviewer).
- **Measurable:** Running `atcr personas list --scores` against a scorecard JSONL containing at least one corroborated finding for `sentinel` produces a row with a non-zero rate for `sentinel`; personas absent from the scorecard show `n/a`. A unit test covering both cases passes in CI.
- **Achievable:** Implementation requires joining the `List()` output from `internal/personas` with the `map[string]float64` built by `scorecard.Aggregate()` — no new data structures or external dependencies.
- **Relevant:** Visible corroboration rates give platform leads a concrete, evidence-based metric to justify persona retention or removal, reducing configuration noise on teams that have accumulated unused personas.
- **Time-bound:** Delivered within Sprint B alongside T2, T5, and T7-in-repo; the score display is the last consumer of the corroboration map already established by T8, so it completes the data pipeline in the same sprint.

## Acceptance Criteria Overview

This story is complete when the following acceptance criteria are met:

- **05-01**: `atcr personas list` without `--scores` produces the same output as before this change.
- **05-02**: `atcr personas list --scores` adds a `CORROBORATION` column with formatted rates or `n/a`.
- **05-03**: The scores table is sorted by corroboration rate descending, with `n/a` rows alphabetically at the bottom.
- **05-04**: The `--scores` flag is documented in `atcr personas list --help` output.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [05-01](../acceptance-criteria/05-01-baseline-list-no-regression.md) | Baseline List No Regression | Unit + Integration |
| [05-02](../acceptance-criteria/05-02-scores-column-display.md) | Scores Column Display | Unit |
| [05-03](../acceptance-criteria/05-03-sort-ordering.md) | Sort Ordering | Unit |
| [05-04](../acceptance-criteria/05-04-help-documentation.md) | Help Documentation | Unit |

## Original Criteria Overview

1. `atcr personas list` without `--scores` produces the same output as before this change — no regression to baseline behavior.
2. `atcr personas list --scores` adds a `CORROBORATION` column to the list table; each row shows the reviewer's rate as `XX.X%` when scorecard data exists, or `n/a` when the reviewer has no recorded runs.
3. The corroboration rate is sourced exclusively from `scorecard.Aggregate()` output — no new JSONL parsing code is introduced; the existing aggregation path is reused.
4. The scores table is sorted by corroboration rate descending (highest-value personas first), with `n/a` rows at the bottom sorted alphabetically by name.
5. A unit test in `internal/personas` covers the join logic: a persona present in the scorecard map shows the correct formatted rate; a persona absent from the map shows `n/a`.
6. The `--scores` flag is documented in `atcr personas list --help` output.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** Add a `--scores` boolean flag to the `list` subcommand in `internal/personas`. When set, call `scorecard.Aggregate()` to build the `map[string]float64`, then join against the `[]PersonaMeta` slice returned by `List()`. Format rates with `fmt.Sprintf("%.1f%%", rate*100)`; missing keys render as `n/a`. Sort the joined slice by rate descending, placing `n/a` entries after numeric entries. The `tabwriter` package (stdlib) handles column alignment.
- **Integration Points:** `internal/personas` (List, list subcommand) → `internal/scorecard` (Aggregate, LeaderboardRow) → `cmd/atcr` (personas list cobra binding). The corroboration map shape (`map[string]float64`) is shared with `SelectEligibleSkeptics`' 4th parameter (T8) — same key convention (reviewer name, lowercase) must be enforced on both sides.
- **Data Requirements:** `scorecard.Aggregate()` must return a type with a `ReviewerName string` and `CorroborationRate float64` per row. No schema changes needed — this is an existing Epic 3.3 output. The join key is `PersonaMeta.Name == LeaderboardRow.ReviewerName` (case-insensitive comparison to tolerate minor casing drift).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Scorecard JSONL path differs across platforms or install modes, causing `Aggregate()` to return empty results silently | Medium | `--scores` shows `n/a` for all personas with a footer note "No scorecard data found at `<path>`" when the file is absent, so the user gets an actionable message rather than a misleading all-zero table. |
| Reviewer name casing mismatch between scorecard and persona metadata causes false `n/a` for personas that do have data | Medium | Join uses `strings.ToLower` on both sides; unit test covers a mixed-case fixture. |
| `scorecard.Aggregate()` is slow on large JSONL files, blocking the CLI | Low | Aggregate is an in-memory scan already used by the existing leaderboard display; no additional I/O is introduced. If it becomes a bottleneck, a cached summary file is the upgrade path, but that is out of scope here. |
| Adding `--scores` flag breaks the existing `list` subcommand's cobra arg parsing | Low | Flag is additive (boolean, default false); no positional args are changed. Test `TestPersonasListCmd` covers both with and without the flag. |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
