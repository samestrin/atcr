# User Story 2: View Single-Run Scorecard

**Plan:** [3.3: Per-Run Scorecard](../plan.md)

## User Story

**As a** developer who has run `atcr reconcile`
**I want** to run `atcr scorecard [id-or-path]` and see a formatted table of per-reviewer metrics for that specific run
**So that** I can compare reviewer performance at a glance — corroboration rates, costs, latencies, and verification status — without manually parsing JSONL files.

## Story Context

- **Background:** Story 1 persists per-reviewer scorecard records to `~/.config/atcr/scorecard/YYYY-MM.jsonl` after each reconcile run. Without a CLI view, the data is accessible only by reading raw JSONL — impractical for quick inspection. This story adds the `atcr scorecard` command to render a single run's results as a human-readable table.
- **Assumptions:** Reconcile output directory structure is known (each run produces a `reconciled/` directory with `summary.json` and `findings.json`). The `run_id` embedded in scorecard records can be matched to a reconcile output directory. `text/tabwriter` from the Go standard library is sufficient for aligned table output.
- **Constraints:** Read-only command — no mutation of scorecard store or reconcile output. Must handle the case where no scorecard records exist for a given run (either reconcile was run before Epic 3.3, or `--no-scorecard` was used). Table output must fit in a standard 80-column terminal by default.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (Auto-emit Scorecard) — records must exist before they can be displayed |

## Success Criteria (SMART Format)

- **Specific:** `atcr scorecard [id-or-path]` accepts either a `run_id` (e.g., `2026-06-14T10:00:00Z-abc123`) or a path to a reconcile output directory. It reads the matching scorecard records from the JSONL store and prints a formatted table with one row per reviewer showing: reviewer name, model, findings raised, findings corroborated, corroboration rate, cost (USD), and latency. When verification data exists, additional columns show findings verified, findings refuted, and survived-skeptic rate.
- **Measurable:** Command produces a table for any valid `run_id` or directory path. Table columns are aligned via `text/tabwriter`. Zero non-zero exit code on success; non-zero with a clear error message when the run is not found. Output fits within 80 columns when verification columns are absent.
- **Achievable:** Reads existing JSONL records produced by Story 1. No new analysis or LLM calls. Table rendering uses `text/tabwriter` (Go stdlib), matching patterns already used in `atcr report` and `atcr status`.
- **Relevant:** Gives the developer an immediate, actionable view of reviewer quality per run. This is the primary day-to-day interaction with the scorecard system — without it, the persisted data has no consumer.
- **Time-bound:** Implemented and verified within this sprint, after Story 1 is complete.

## Acceptance Criteria Overview

| AC | Title | AC File |
|----|-------|------|
| 02-01 | Scorecard Command Resolution and Lookup | [02-01](../acceptance-criteria/02-01-scorecard-command-resolution.md) |
| 02-02 | Scorecard Table Rendering and Conditional Columns | [02-02](../acceptance-criteria/02-02-scorecard-table-rendering.md) |
| 02-03 | Scorecard Error Handling and Edge Case Resilience | [02-03](../acceptance-criteria/02-03-scorecard-error-handling.md) |

1. `atcr scorecard <run-id>` looks up scorecard records by `run_id` in the JSONL store and renders a per-reviewer table.
2. `atcr scorecard <path>` resolves a reconcile output directory path and renders the scorecard for the matching run.
3. The table includes columns: reviewer, model, findings raised, corroborated, solo, corroboration rate, cost (USD), latency (ms).
4. When verification data is present for the run, the table includes additional columns: verified, refuted, survived-skeptic rate.
5. When no scorecard records exist for the given run, the command exits with a clear error message indicating no data is available.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/3.3_per_run_scorecard/`_

## Technical Considerations

- **Implementation Notes:** New file `cmd/atcr/scorecard.go` registers the `scorecard` cobra command. Argument resolution: if the argument is an existing directory path, use it directly; otherwise treat it as a `run_id` and scan the JSONL store under `~/.config/atcr/scorecard/` for matching records. Read the relevant monthly JSONL file(s) — the `run_id` encodes a timestamp that identifies the month. Parse each line, filter by `run_id`, collect per-reviewer records. Render via `text/tabwriter` with format string `"%s\t%s\t%d\t%d\t%d\t%.2f\t$%.4f\t%dms"`. Register command in `cmd/atcr/main.go`.
- **Integration Points:** `internal/scorecard/store.go` (Story 1) for JSONL read/query. `cmd/atcr/main.go` for command registration. Existing reconcile output directory structure for path-based resolution.
- **Data Requirements:** Scorecard records from `~/.config/atcr/scorecard/YYYY-MM.jsonl` with fields: `run_id`, `reviewer`, `model`, `role`, `findings_raised`, `findings_corroborated`, `findings_solo`, `corroboration_rate`, `cost_usd`, `tokens_in`, `tokens_out`, `latency_ms`, and conditionally `findings_verified`, `findings_refuted`, `survived_skeptic_rate`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `run_id` lookup requires scanning multiple monthly JSONL files | Low | `run_id` contains an ISO timestamp — derive the month (`YYYY-MM`) from the ID to read only the relevant file. |
| Table output is too wide for terminals when verification columns are included | Medium | When verification data is present, wrap to a second section below the main table rather than widening columns. Alternatively, use a narrower format for corroboration rate (e.g., `58%` instead of `0.58`). |
| Reconcile output directory structure varies across ATCR versions | Low | Resolve by `run_id` from the JSONL store as primary path; directory path as fallback. Log a warning if directory structure is unexpected. |
| JSONL file is corrupted or contains partial records | Low | Skip unparseable lines with a warning; do not fail the entire command. Scorecard records are independent — partial data degrades gracefully. |

---

**Created:** June 15, 2026 10:47:26AM
**Status:** Acceptance Criteria Complete
