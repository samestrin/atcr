# User Story 4: Export Public Leaderboard Submission

**Plan:** [3.3: Per-Run Scorecard](../plan.md)

## User Story

**As a** developer contributing to the public Model-Eval Leaderboard
**I want** to run `atcr leaderboard --export` to emit the versioned anonymized submission JSON
**So that** I can contribute my per-reviewer evaluation data to the public Model-Eval Leaderboard (Epic 10.0) without exposing private information about my codebase, providers, or organization.

## Story Context

- **Background:** Each `atcr reconcile` run produces per-reviewer quality metrics (corroboration rates, cost, latency, verification outcomes) stored locally as scorecard records (Story 1). Story 3 aggregates these records into a ranked leaderboard view. The public Model-Eval Leaderboard (Epic 10.0) needs a stable, versioned submission format that anyone running `atcr` can produce locally and submit — without leaking PII, provider API keys, repo content, or other sensitive data. This story defines and implements that submission format with a deterministic anonymization pass.
- **Assumptions:**
  - Scorecard JSONL files exist from prior `atcr reconcile` runs (Story 1 dependency).
  - Story 3 provides the aggregation pipeline; this story reuses it and layers anonymization on top.
  - The public leaderboard accepts submissions as a single JSON file per contributor, versioned by `schema_version`.
  - Anonymization is deterministic — the same input always produces the same output, enabling reproducibility.
- **Constraints:**
  - No network calls; export is purely local file generation.
  - Output must be self-contained (no external references, no relative paths).
  - Schema must be pinned to `schema_version: 1` and evolve independently from the internal record schema.
  - Must strip: provider API keys, repo paths, repo content, organization identifiers, PII (usernames, hostnames). Must preserve: model identifiers, persona names, role, all numeric metrics (counts, rates, cost, tokens, latency).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (Per-Run Scorecard Emission), Story 3 (Aggregated Leaderboard) |

## Success Criteria (SMART Format)

- **Specific:** `atcr leaderboard --export` reads aggregated scorecard records (applying the same `--since`, `--model`, `--persona` filters as the table view), runs an anonymization pass over each record, and writes a single self-contained JSON file to stdout (or to a file via `--output <path>`) conforming to the versioned public submission schema (`schema_version: 1`).
- **Measurable:** Output JSON contains zero instances of: file system paths, provider API keys, hostnames, usernames, or repository content. Output contains all required fields per the v1 submission schema. Running `atcr leaderboard --export` twice on the same input produces byte-identical output (deterministic).
- **Achievable:** Anonymization is a deterministic field-mapping and stripping pass over existing in-memory aggregated records. No new LLM calls or external services required.
- **Relevant:** Directly enables Epic 10.0 (public Model-Eval Leaderboard) by providing the submission format. Without this, scorecard data remains siloed locally and cannot contribute to cross-organization model comparison.
- **Time-bound:** Implemented and verified within this sprint.

## Acceptance Criteria Overview

| AC | Title | File |
|----|-------|------|
| 04-01 | Export Command & Public Submission Schema | [04-01-export-command-public-schema.md](../acceptance-criteria/04-01-export-command-public-schema.md) |
| 04-02 | Anonymization Pass — PII Stripping | [04-02-anonymization-pii-stripping.md](../acceptance-criteria/04-02-anonymization-pii-stripping.md) |
| 04-03 | Metric Preservation & Metadata Integrity | [04-03-metric-preservation-metadata.md](../acceptance-criteria/04-03-metric-preservation-metadata.md) |
| 04-04 | Determinism, Filtering & Error Handling | [04-04-determinism-filtering-errors.md](../acceptance-criteria/04-04-determinism-filtering-errors.md) |

1. `atcr leaderboard --export` produces a valid JSON document conforming to the v1 public submission schema, including `schema_version`, export metadata (generation timestamp, filter summary), and an array of anonymized per-reviewer aggregated records.
2. The anonymization pass strips all PII: file system paths, provider API keys, hostnames, usernames, repository identifiers, and organization names are removed or replaced with opaque identifiers.
3. The anonymization pass preserves all numeric metrics: `findings_raised`, `findings_corroborated`, `findings_solo`, `corroboration_rate`, `findings_verified`, `findings_refuted`, `survived_skeptic_rate`, `cost_usd`, `tokens_in`, `tokens_out`, `latency_ms`.
4. Model identifiers, persona/role names, and the `schema_version` field are preserved as-is (these are not considered PII).
5. `--output <path>` writes the JSON to the specified file instead of stdout. Without `--output`, JSON is written to stdout (enabling piping).
6. Filters (`--since`, `--model`, `--persona`) are applied before anonymization, so the exported dataset reflects only the filtered subset.
7. Export is deterministic: identical input records and filters always produce byte-identical output (stable sort order, no random salts, no timestamps in record data).
8. Exit code 0 on success; exit code 1 with a clear error message if no records match the filters or if the scorecard store is empty.

## Technical Considerations

- **Implementation Notes:**
  - Build `internal/scorecard/export.go` with two main functions: `Export(records []ScorecardRecord, filters FilterOpts) ([]byte, error)` for the aggregation + anonymization pipeline, and `AnonymizeRecord(raw ScorecardRecord) PublicRecord` for the per-record anonymization pass.
  - Anonymization rules: strip `run_id` (contains timestamp + hash that may correlate to specific runs); replace with opaque sequential index. Strip any `repo`, `path`, `organization`, `hostname`, `user` fields if present in the internal record. Preserve `model`, `reviewer` (persona name), `role`, and all numeric fields.
  - Public submission schema v1 shape:
    ```json
    {
      "schema_version": 1,
      "exported_at": "2026-06-15T10:00:00Z",
      "filters": { "since": "30d", "model": "", "persona": "" },
      "records": [
        {
          "index": 0,
          "reviewer": "bruce",
          "model": "claude-sonnet-4-6",
          "role": "reviewer",
          "runs": 15,
          "findings_raised": 120,
          "findings_corroborated": 78,
          "findings_solo": 42,
          "corroboration_rate": 0.65,
          "findings_verified": 50,
          "findings_refuted": 8,
          "survived_skeptic_rate": 0.86,
          "cost_usd": 0.60,
          "tokens_in": 213000,
          "tokens_out": 60000,
          "latency_ms_avg": 9100
        }
      ]
    }
    ```
  - Deterministic output: sort records by `(model, reviewer, role)` before serializing. Use `json.MarshalIndent` with 2-space indent for readability. No random elements.
  - Reuse aggregation logic from Story 3 (shared reader/group-by module); do not duplicate JSONL parsing.
- **Integration Points:**
  - Shared scorecard reader and aggregation module from Story 3.
  - CLI flag parser in `cmd/atcr/leaderboard.go` (extends existing `--export` flag with `--output`).
  - `encoding/json` for serialization (stdlib).
- **Data Requirements:**
  - Input: aggregated per-reviewer records from JSONL store (same schema as Story 1 records).
  - Output: single JSON document per v1 public submission schema. No external dependencies or network access.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Anonymization misses a PII field, leaking sensitive data in public submission | High | Define an allowlist of fields in the public schema (not a denylist). Any field not explicitly in the v1 schema is dropped by default. Unit test: assert no path-like strings, hostnames, or API key patterns in output. |
| `run_id` or other fields inadvertently enable run-level de-anonymization | Medium | Strip `run_id` entirely from public output. Replace with opaque sequential `index`. Do not include per-run granularity — only aggregated metrics per (reviewer, model) group. |
| Export schema v1 locked too early, breaks Epic 10.0 integration | Medium | Pin to `schema_version: 1`; document in `docs/scorecard.md` that v1 is experimental until Epic 10.0 stabilizes the public format. Future bumps increment version, old exports remain readable. |
| Non-deterministic output (map ordering, timestamp in record) breaks reproducibility | Low | Use sorted keys in JSON serialization. No per-record timestamps — `exported_at` is metadata only. Sort records by `(model, reviewer, role)`. |
| `--output` path is writable but user lacks read-back permissions | Low | Validate path is writable before writing. On failure, print clear error and exit 1. Do not partially write the file — write to temp file then rename. |
| Export with no matching records produces confusing empty JSON | Low | Exit code 1 with message: "No records match the specified filters. Try widening --since or removing filters." Do not produce an empty `records` array. |

---

**Created:** June 15, 2026 10:47:26AM
**Status:** AC Generated
