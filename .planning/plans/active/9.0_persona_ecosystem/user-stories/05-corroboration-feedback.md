# User Story 5: Per-Persona Corroboration Feedback

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** platform lead managing team code-review configuration
**I want** to run `atcr personas list --scores` and see each persona's corroboration rate alongside its name and version
**So that** I can make data-driven decisions about which personas are delivering ROI on my team's actual codebase and retire or replace underperforming ones

## Story Context

- **Background:** Sprint 8.0 introduced per-run scorecard tracking in `internal/scorecard/`. Every reconciled finding records which reviewers credited it and whether a skeptic confirmed it, producing monthly JSONL records under `~/.config/atcr/scorecard/`. `scorecard.Aggregate()` (`internal/scorecard/aggregate.go:122`) groups those records by `(reviewer, model)` and surfaces a `CorroborationRate` per `LeaderboardRow`. The `atcr leaderboard` command already renders this data in a ranked table. T6 re-uses the same aggregation pipeline to annotate `atcr personas list` output with per-persona corroboration rates, making persona quality visible without requiring teams to cross-reference the leaderboard separately.
- **Assumptions:** The `atcr personas list` command (delivered by T2, Story 2) exists and can list installed personas from `~/.config/atcr/personas/`. Scorecard data may be absent for a given persona (new install, no runs yet), in which case the score column renders as `n/a`. The scorecard directory is always resolved via `scorecard.DefaultDir()` (`internal/scorecard/paths.go:23`).
- **Constraints:** No new external dependencies — `scorecard.ReadAll` and `scorecard.Aggregate` are the only data sources. The `--scores` flag is additive; omitting it preserves the existing `list` output format unchanged. The implementation lives in `internal/personas/list.go` and follows the `cmd/atcr/leaderboard.go` pattern exactly (same read → filter → aggregate pipeline). The `map[string]float64` built by T6 (reviewer name → corroboration rate) is the same shape passed to `SelectEligibleSkeptics` as its fourth parameter (T8), so the two tasks share one map construction site.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 2 (T2 — `atcr personas list` base command); Story 3 (T8 — `SelectEligibleSkeptics` fourth-parameter shape agreed); `internal/scorecard` package (Sprint 8.0, already shipped) |

## Success Criteria (SMART Format)

- **Specific:** `atcr personas list --scores` prints a corroboration-rate column (as a percentage, e.g. `72%`, or `n/a` when no data) for every persona returned by `list`, sourced exclusively from `scorecard.Aggregate()` output keyed by reviewer name.
- **Measurable:** Unit tests in `internal/personas/list_test.go` cover: (a) `--scores` renders the rate column with correct values for personas that have scorecard data, (b) personas with no scorecard data render `n/a`, (c) omitting `--scores` produces output identical to the pre-flag baseline (no regression). All three cases pass `go test ./internal/personas/...`.
- **Achievable:** The data pipeline (`scorecard.ReadAll` → `scorecard.Aggregate` → `map[string]float64`) is already proven in `cmd/atcr/leaderboard.go`; T6 adds only a flag and a table column, not new aggregation logic.
- **Relevant:** Persona ROI is currently invisible — teams must manually correlate `atcr leaderboard` output against installed persona names. Surfacing the rate in `personas list` closes that gap and provides the primary adoption signal for vertical-market persona bundles (T5).
- **Time-bound:** Delivered within Sprint B (9.0) before the sprint's cumulative adversarial review.

## Acceptance Criteria Overview

1. `atcr personas list --scores` displays a corroboration-rate column; personas with scorecard data show a percentage and personas without show `n/a`.
2. Omitting `--scores` produces unchanged output — no regression to the base `list` format.
3. The `map[string]float64` built from `scorecard.Aggregate()` output is the same structure passed to `SelectEligibleSkeptics` (nil-safe; shared with T8).

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** Add a `--scores` boolean flag to `newPersonasListCmd()` in `cmd/atcr/personas.go`. When set, call `scorecard.DefaultDir()`, then `scorecard.ReadAll(dir, scorecard.ReadOpts{Writer: cmd.ErrOrStderr()})`, then `scorecard.Aggregate(records)` (no filter — all-time rates give the most signal), then build `map[string]float64{row.Reviewer: row.CorroborationRate}`. Pass the map into `internal/personas/list.go`'s render function to annotate output. Format the rate via `formatPercent` (already in `cmd/atcr/leaderboard.go`) or an equivalent helper; `n/a` when the reviewer key is absent from the map.
- **Integration Points:** `internal/scorecard` (`aggregate.go`, `paths.go`, `store.go`) — read-only consumers; `internal/personas/list.go` — new `scores map[string]float64` parameter on the render function; `cmd/atcr/personas.go` — flag registration and map construction; `SelectEligibleSkeptics` fourth parameter (T8, `internal/verify/select.go`) — same `map[string]float64` shape, no new import needed in the verify package.
- **Data Requirements:** Monthly JSONL scorecard records at `~/.config/atcr/scorecard/YYYY-MM.jsonl`. Records must have `RecordType == RecordTypeReviewer` to be included by `Aggregate`. An empty or missing scorecard directory is a graceful no-data state (all personas render `n/a`); it is not an error.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `scorecard.Aggregate` groups by `(reviewer, model)` — a persona used with two models produces two rows, making the key lookup by reviewer name ambiguous when rates differ | Medium | Average the rates across models for the same reviewer name when building the display map, or show the highest rate; document the choice in a code comment. The T8 tie-break map can use the same averaged value. |
| Scorecard directory unreadable (permissions error) | Low | Mirror `leaderboard.go` error handling: return a descriptive `fmt.Errorf` wrapping the OS error; do not fall back silently to `n/a` for all personas (that would hide the permission problem). |
| `--scores` flag name collides with a future sub-subcommand flag on `list` | Low | Register the flag only on the `list` subcommand (not on the parent `personas` command); scoping prevents collision. |
| Render output width grows with a new column and breaks existing snapshot tests | Low | Snapshot tests (if any) are updated in the same commit as the flag addition; the TDD RED step identifies breakage before GREEN. |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
