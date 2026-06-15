# User Story 3: View Aggregated Leaderboard

**Plan:** [3.3: Per-Run Scorecard](../plan.md)

## User Story

**As a** developer running code reviews across multiple sessions
**I want** to run `atcr leaderboard [--since, --model, --persona]` to see ranked reviewer performance across runs
**So that** I can make data-driven model and persona selections based on corroborated findings and cost efficiency

## Story Context

- **Background:** Each `atcr reconcile` run emits a per-reviewer scorecard record (Story 1) stored in monthly JSONL files under `~/.config/atcr/scorecard/`. These records contain corroboration rates, cost, latency, and optionally verification outcomes. The leaderboard aggregates these records to surface which reviewer configurations consistently deliver the most value.
- **Assumptions:**
  - Scorecard JSONL files exist from prior `atcr reconcile` runs (Story 1 dependency).
  - Each record includes `reviewer`, `model`, `role`, `findings_raised`, `findings_corroborated`, `corroboration_rate`, `cost_usd`, `latency_ms`, and a timestamp (via `run_id`).
  - Records are well-formed per `schema_version: 1`; older or malformed records are skipped with a warning.
- **Constraints:**
  - All computation is local; no network calls required.
  - Output must be readable in a terminal (table format).
  - Must handle empty or missing scorecard files gracefully.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (Per-Run Scorecard Emission), Story 2 (Scorecard CLI) |

## Success Criteria (SMART Format)

- **Specific:** `atcr leaderboard` reads all JSONL files in `~/.config/atcr/scorecard/`, aggregates per-reviewer metrics, and outputs a ranked table sorted by corroboration rate descending.
- **Measurable:** Table displays reviewer, model, persona, total findings raised, total corroborated, overall corroboration rate, total cost USD, cost per corroborated finding, and average latency across filtered runs.
- **Achievable:** Aggregation is a local JSONL read and group-by operation; no new analysis or API calls required.
- **Relevant:** Enables data-driven reviewer configuration decisions; directly answers "which model finds the most real bugs at what cost for my codebase."
- **Time-bound:** Command completes within 2 seconds for up to 10,000 stored records.

## Acceptance Criteria Overview

| # | Title | AC File |
|---|-------|----------|
| 01 | Leaderboard Ranked Table Display | [03-01-leaderboard-table.md](../acceptance-criteria/03-01-leaderboard-table.md) |
| 02 | Time Range Filter (--since) | [03-02-since-filter.md](../acceptance-criteria/03-02-since-filter.md) |
| 03 | Model and Persona Filters | [03-03-model-persona-filters.md](../acceptance-criteria/03-03-model-persona-filters.md) |
| 04 | Export Versioned JSON | [03-04-export-json.md](../acceptance-criteria/03-04-export-json.md) |
| 05 | Graceful Empty and Missing Data Handling | [03-05-graceful-empty-handling.md](../acceptance-criteria/03-05-graceful-empty-handling.md) |

### Original Criteria Mapping

1. `atcr leaderboard` displays a ranked table of reviewer performance aggregated across all stored scorecard records, sorted by corroboration rate descending. → **[03-01](../acceptance-criteria/03-01-leaderboard-table.md)**
2. `--since <duration>` filters records to only those within the specified time window (e.g., `30d`, `7d`, `90d`). Default is 30 days. → **[03-02](../acceptance-criteria/03-02-since-filter.md)**
3. `--model <name>` filters to records matching the specified model (e.g., `claude-sonnet-4-6`). → **[03-03](../acceptance-criteria/03-03-model-persona-filters.md)**
4. `--persona <name>` filters to records matching the specified reviewer/persona (e.g., `bruce`). → **[03-03](../acceptance-criteria/03-03-model-persona-filters.md)**
5. Filters are composable: `atcr leaderboard --since 7d --model claude-sonnet-4-6 --persona bruce` applies all three. → **[03-03](../acceptance-criteria/03-03-model-persona-filters.md)**
6. Table columns include: reviewer, model, runs (count), findings raised, findings corroborated, corroboration rate, total cost, cost per corroborated finding, avg latency. → **[03-01](../acceptance-criteria/03-01-leaderboard-table.md)**
7. `--export` outputs the aggregated data as versioned JSON suitable for Epic 10.0 public leaderboard submission (anonymized, no provider keys or repo content). → **[03-04](../acceptance-criteria/03-04-export-json.md)**
8. Gracefully handles missing scorecard directory or empty files: prints informative message and exits with code 0. → **[03-05](../acceptance-criteria/03-05-graceful-empty-handling.md)**

## Technical Considerations

- **Implementation Notes:**
  - Read all `*.jsonl` files from `~/.config/atcr/scorecard/`, parse each line as a scorecard record.
  - Skip aggregate records (role `aggregate`) — only aggregate per-reviewer records.
  - Group by `(reviewer, model)` key; compute sums, rates, and averages per group.
  - Apply time filter by parsing `run_id` timestamp prefix or a dedicated `timestamp` field.
  - Table output via existing CLI table library (consistent with Story 2 formatting).
  - `--export` emits JSON with `schema_version`, array of aggregated records, and metadata (date range, filter applied).
- **Integration Points:**
  - Shared scorecard reader module from Story 1/2 (avoid duplicated JSONL parsing).
  - CLI flag parser consistent with existing `atcr` command patterns.
- **Data Requirements:**
  - Input: JSONL records per `schema_version: 1` from `~/.config/atcr/scorecard/`.
  - Output format for `--export`: versioned JSON schema (separate schema document or inline definition).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Malformed JSONL lines crash aggregation | Medium | Wrap per-line parsing in try/catch; log warnings for skipped lines, continue processing. |
| Large JSONL files cause memory pressure | Low | Stream-parse line-by-line rather than loading entire file; JSONL is append-only so files are naturally sequential. |
| Ambiguous duration parsing for `--since` | Low | Support explicit formats: `Nd` (days), `Nw` (weeks), `Nm` (months). Reject unknown formats with clear error. |
| `--export` schema drifts from Epic 10.0 spec | Medium | Pin export to `schema_version: 1`; include version in output. Future epics bump version explicitly. |
| Empty results after filtering confuse users | Low | Print "No records match filters" message with suggestion to widen `--since` or remove filters. |

---

**Created:** June 15, 2026 10:47:26AM
**Status:** Acceptance Criteria Generated
