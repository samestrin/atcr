# User Story 4: Maintainer-Facing Prompt Quality Report

**Plan:** [30.0: Community Prompt Quality Signal](../plan.md)

## User Story

**As the** atcr maintainer (Sam Estrin), the sole consumer of the aggregated quality signal
**I want** a CLI report that ranks per-persona+model over/under-reporting from the aggregated dismissed/confirmed counters
**So that** I know exactly which reviewer prompts need tuning next, closing the loop from Epic 24.0's dismissal signal into the 19.7 drift-detection arc, 19.8's drafting agent, and 19.9's community submissions

## Story Context

- **Background:** `cmd/atcr/report.go` (`newReportCmd`/`runReport`) is the established pattern for a read-only CLI subcommand that renders a stored artifact in multiple formats (`--format md|json|checklist|sarif`), resolves an anchor/review directory, and fails closed (usage error) when the source data is absent. This story adds a sibling maintainer-facing subcommand that renders Theme 1's per-persona+model dismissed/confirmed aggregation instead of reconciled findings, following `internal/scorecard/aggregate.go`'s `Aggregate()` grouping idiom (group by `(reviewer, model)`, sum counters, sort deterministically) rather than inventing a new aggregation shape. The persona+model keying dimension is the same one used across the epic: `personas/community/*.yaml` (`name`, `model`, `provider`).
- **Assumptions:** Theme 1 of this plan (Story 1) delivers the per-persona+model dismissed/confirmed counters this story renders — this story is a pure consumer of that aggregation output and does not compute dismissal counts itself. The underlying data is opt-in per Epic AC1: on a machine where quality-signal collection has never been enabled, the aggregation is empty and the report must render a clear "no data" state rather than erroring or panicking.
- **Constraints:** Must not modify, alias, or change the output of the existing `atcr report` command (`cmd/atcr/report.go`) — this is a net-new, separately-named subcommand. The report may render only aggregate counters and persona/model identifiers; it must never read or display underlying finding text, file paths, or code from `reconcile`/`findings.json` — sourcing exclusively from the new aggregation struct enforces this at the type level. Ranking/flagging of "over-reporting" vs "under-reporting" must use a simple, deterministic metric (e.g. dismissal rate, sorted descending) rather than an invented, unexplained threshold.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (per-persona+model dismissed/confirmed aggregation) — this report renders that aggregation's output; benefits from, but does not structurally require, Story 2 (opt-in gate) to be complete first |

## Success Criteria (SMART Format)

- **Specific:** A new maintainer-facing CLI subcommand (e.g. `cmd/atcr/telemetry_report.go`, following the `newReportCmd`/`runReport` shape) renders the per-persona+model dismissed/confirmed counters from Story 1's aggregation, ranked so the maintainer can immediately identify the highest-dismissal-rate (over-reporting) and lowest-confirmation (under-reporting) persona+model pairs.
- **Measurable:** Given a fixture aggregation with known per-persona+model dismissed/confirmed counts, the rendered report's ranking order and displayed counters exactly match hand-computed expected values in a table-driven test; given an empty aggregation, the command exits cleanly with a "no data" message (no panic, no non-zero unexpected exit code).
- **Achievable:** Reuses the existing `cmd/atcr/report.go` subcommand skeleton and `internal/scorecard/aggregate.go` `Aggregate()` grouping/sorting idiom — no new CLI framework or aggregation algorithm is introduced.
- **Relevant:** Directly satisfies epic AC2 ("surfaced to the maintainer in a form that identifies over/under-reporting prompts, closing the loop to 19.8's drafting and 19.9's submissions").
- **Time-bound:** Deliverable within the current sprint alongside the other four plan.md user story themes, gated only on Story 1's aggregation output existing.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [04-01](../acceptance-criteria/04-01-ranked-quality-report-rendering.md) | Ranked Per-Persona+Model Quality Report Rendering | Unit |
| [04-02](../acceptance-criteria/04-02-content-free-privacy-guarantee.md) | Content-Free Privacy Guarantee on the Report Render Path | Unit |
| [04-03](../acceptance-criteria/04-03-empty-aggregation-no-data-state.md) | Empty-Aggregation "No Data" State | Unit |
| [04-04](../acceptance-criteria/04-04-distinct-subcommand-registration.md) | Distinct Subcommand Registration Alongside Existing `atcr report` | Unit |

## Original Criteria Overview

1. A new CLI subcommand renders the per-persona+model dismissed/confirmed counters (from Story 1's aggregation) ranked by dismissal rate, clearly surfacing the highest over-reporting and lowest under-reporting persona+model pairs.
2. The report's output is limited to aggregate counters and persona+model identifiers only — never finding text, file paths, or code — verified by a test asserting the rendering path has no access to `reconcile`/`findings.json` content.
3. An empty aggregation (no opted-in data collected yet) renders a clear "no data" state rather than erroring, panicking, or displaying a misleading empty table.

## Technical Considerations

- **Implementation Notes:** Add a new subcommand file (e.g. `cmd/atcr/telemetry_report.go`) mirroring `newReportCmd`/`runReport` in `cmd/atcr/report.go`: a Cobra command with a `--format` flag (at minimum `md` and `json`, matching existing report conventions) that reads Story 1's aggregated counters, ranks rows by dismissal rate descending, and renders a table (md) or structured payload (json). Reuse `internal/scorecard/aggregate.go`'s `Aggregate()` grouping-by-`(reviewer, model)` idiom as the pattern for the new per-persona+model grouping rather than writing a new algorithm from scratch.
- **Integration Points:** Consumes Story 1's aggregation struct/function directly (in-process call, not a file round-trip if avoidable); does not touch `cmd/atcr/report.go`, `internal/reconcile`, or `internal/report`'s existing findings-rendering path. Registers alongside existing subcommands in the root Cobra command tree the same way `newReportCmd` does today.
- **Data Requirements:** Input is Story 1's per-persona+model `{persona, model, dismissed_count, confirmed_count}`-shaped aggregation (exact field names TBD by Story 1's AC). Output fields are limited to persona identifier, model, dismissed count, confirmed count, and a derived dismissal rate — no other fields are added to the render path.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Report implementation reuses `reconcile`/findings data structures directly instead of Story 1's aggregation, accidentally exposing finding text or file paths | High | Source the render path exclusively from Story 1's aggregation type; add a test asserting the report package has no import of `internal/reconcile` or raw findings types |
| "Over-reporting" vs "under-reporting" is left ambiguous, leading to an arbitrary or unexplained threshold in the implementation | Medium | Use a single deterministic metric (dismissal rate, descending sort) with no invented cutoff; document the ranking basis alongside the report so the maintainer's read of "which prompts need tuning" is transparent |
| New subcommand name collides with or is confused for the existing `atcr report` (findings report) command | Low | Give the new subcommand a distinct, clearly-scoped name (e.g. `telemetry report` or `quality-report`) and cross-reference the two commands in `--help` text |

---

**Created:** July 17, 2026
**Status:** Draft - Awaiting Acceptance Criteria
