# Sprint Design: Per-Run Scorecard

**Created:** June 15, 2026
**Plan:** [Per-Run Scorecard](.planning/plans/active/3.3_per_run_scorecard/)
**Plan Type:** ✨ Feature
**Status:** Design Complete

---

## Original User Request

> Epic Plan 3.3: Per-Run Scorecard — emit normalized per-reviewer eval records alongside each reconcile run, accumulate into local monthly JSONL store, expose via `atcr scorecard` and `atcr leaderboard` commands. Monitoring foundation for quality pipeline and data prerequisite for public Model-Eval Leaderboard (Epic 10.0).

**Referenced Resources:**
- [Epic Plan 3.3: Per-Run Scorecard](.planning/epics/active/3.3_per_run_scorecard.md)
  - **Summary:** Epic plan defining the per-run scorecard feature — emit normalized per-reviewer eval records from reconcile runs, accumulate into local monthly JSONL store, expose via CLI commands for single-run and aggregated views. Data prerequisite for Epic 10.0 public Model-Eval Leaderboard.
  - **Key Points:**
    - Per-reviewer record schema includes: schema_version, run_id, reviewer, model, role, findings_raised/corroborated/solo, corroboration_rate, cost_usd, tokens_in/out, latency_ms, plus optional verification fields
    - Storage: ~/.config/atcr/scorecard/YYYY-MM.jsonl (monthly rotation, append-only, local-only)
    - CLI commands: `atcr scorecard [id-or-path]` (single run), `atcr leaderboard [--since, --model, --persona]` (aggregated), `atcr leaderboard --export` (versioned public submission)
    - Hard prerequisite: llmclient usage parsing must resolve first — cost/token columns are always empty without it

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Scorecard Pipeline
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 10 days
**Phases:** 6
**Pattern:** Foundation → Core Items → Advanced → Integration → Testing → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
JSONL append-only storage patterns
cobra CLI command registration
text/tabwriter table formatting
anonymization PII stripping deterministic
schema versioning evolution
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - New `internal/scorecard/` package with 4 files, integration into existing reconcile pipeline, new CLI commands. Follows existing patterns but adds new module boundaries.
- **Integration:** 2/3 - Integrates with reconcile output, llmclient (hard prerequisite), CLI command structure, and JSONL storage. 3+ integration points.
- **Story/Task & Test:** 3/3 - 6 user stories with 21 acceptance criteria. Mix of unit and integration tests. Complex aggregation logic, anonymization with determinism requirements, conditional fields.
- **Risk/Unknowns:** 2/3 - Hard prerequisite (llmclient usage parsing) must resolve first. Schema versioning for Epic 10.0. Anonymization correctness critical.

**Time Formula:** COMPLEX → 8-12 days
**Calculation:** 6 stories, 21 ACs, hard prerequisite → 10 days

---

## Recommended Flags

**Adversarial:** true (complexity >= 6/12 and phases >= 3)
**Gated:** true (complexity >= 8/12)
**Recommendation strength:** false (complexity < 10/12)
**Suggested command:** `/create-sprint @.planning/plans/active/3.3_per_run_scorecard/ --gated`

---

## Phase Structure

### Phase 1: Hard Prerequisite (1 day)
**Focus:** Resolve llmclient usage parsing blocker
**Items:**
- Decode `usage` from provider responses in `internal/llmclient/client.go` (`chatResponse`)
- Decode `usage` in `internal/llmclient/chat.go` (`chatToolResponse`)
- Surface `tokens_in`, `tokens_out` via `Complete()` and `Chat()` return values
- Compute `cost_usd` from per-model rate table
- Thread values up to scorecard emitter
**Duration:** 1 day
**Success Criteria:** Cost and token fields populated in reconcile output; unit tests pass

### Phase 2: Core Emitter (2 days)
**Focus:** Story 1 — Auto-emit scorecard records
**Items:**
- Build `internal/scorecard/scorecard.go` — record schema, `Emit()` function
- Compute per-reviewer metrics from reconcile findings (raised, corroborated, solo, corroboration_rate)
- Conditional fields: when `verification.json` present, add findings_verified, findings_refuted, survived_skeptic_rate
- Write one JSONL record per reviewer plus one aggregate record per run
- Integrate into `cmd/atcr/reconcile.go` after `RunReconcile` succeeds
- Best-effort: errors logged, never returned
**Duration:** 2 days
**Success Criteria:** AC 01-01, 01-02, 01-03, 01-05 pass; JSONL file created with valid schema

### Phase 3: CLI Commands (2 days)
**Focus:** Stories 2 & 3 — Scorecard and Leaderboard CLI
**Items:**
- Build `cmd/atcr/scorecard.go` — `atcr scorecard [id-or-path]` command
- Build `internal/scorecard/store.go` — JSONL read/query functions
- Build `cmd/atcr/leaderboard.go` — `atcr leaderboard [--since, --model, --persona]` command
- Build `internal/scorecard/aggregate.go` — aggregation, ranking, filtering logic
- Register both commands in `cmd/atcr/main.go`
- Table rendering via `text/tabwriter`
**Duration:** 2 days
**Success Criteria:** AC 02-01, 02-02, 02-03, 03-01, 03-02, 03-03, 03-05 pass

### Phase 4: Export + Suppression (2 days)
**Focus:** Stories 4 & 5 — Export and --no-scorecard flag
**Items:**
- Build `internal/scorecard/export.go` — versioned public submission JSON (schema v1)
- Anonymization pass: strip PII, paths, API keys; preserve metrics, model, persona
- Deterministic output: sorted keys, stable sort order, no random elements
- Add `--export` flag to `atcr leaderboard`
- Add `--output <path>` flag for file output
- Add `--no-scorecard` flag to `atcr reconcile`
- Suppression gate: check flag as first condition in emission path, return early if true
**Duration:** 2 days
**Success Criteria:** AC 03-04, 04-01, 04-02, 04-03, 04-04, 05-01, 05-02, 05-03 pass

### Phase 5: Documentation + Integration Testing (1 day)
**Focus:** Story 6 — Documentation and end-to-end testing
**Items:**
- Create `docs/scorecard.md` — schema, storage, CLI usage, privacy model
- Document v1 record schema (all fields, required vs. conditional)
- Document monthly JSONL storage location and rotation
- Document CLI usage for all commands and flags
- Document privacy/anonymization model for `--export`
- Integration test: reconcile → emit → read back via scorecard command
- Integration test: reconcile → emit → aggregate via leaderboard command
- Integration test: reconcile with --no-scorecard → assert zero records
**Duration:** 1 day
**Success Criteria:** AC 06-01 pass; integration tests pass; docs accurate and complete

### Phase 6: Validation (1 day)
**Focus:** Final verification and quality gate
**Items:**
- Run all unit tests (`go test ./...`)
- Run integration tests
- Run linter (`golangci-lint run`)
- Run vet (`go vet ./...`)
- Verify all 21 acceptance criteria pass
- Verify hard prerequisite resolved (cost/token fields populated)
- Manual smoke test: run reconcile, check scorecard, run leaderboard, run export
**Duration:** 1 day
**Success Criteria:** All tests pass, lint clean, all ACs verified

---

## Work Decomposition

### Story 1: Auto-emit Scorecard (5 ACs)
**Testable Elements:**
- JSONL file creation and append (Integration test)
- Schema validation — all required fields present, correct types (Unit test)
- Conditional fields omitted when verification.json absent (Unit test)
- Conditional fields populated when verification.json present (Unit test)
- --no-scorecard flag suppresses all writes (Integration test)
- Aggregate record appended alongside per-reviewer records (Unit test)

**AC Links:** 01-01, 01-02, 01-03, 01-04, 01-05

### Story 2: View Single-Run Scorecard (3 ACs)
**Testable Elements:**
- Command resolution by run_id (Integration test)
- Command resolution by directory path (Integration test)
- Table rendering with all columns (Integration test)
- Conditional verification columns when data present (Integration test)
- Error handling: no records found (Integration test)
- Error handling: corrupted JSONL lines (Integration test)

**AC Links:** 02-01, 02-02, 02-03

### Story 3: View Aggregated Leaderboard (4 ACs)
**Testable Elements:**
- Ranked table display sorted by corroboration rate (Integration test)
- --since filter applies time window (Unit test)
- --model filter applies model filter (Unit test)
- --persona filter applies persona filter (Unit test)
- Filters composable (Unit test)
- Graceful empty/missing data handling (Integration test)

**AC Links:** 03-01, 03-02, 03-03, 03-05

### Story 4: Export Public Leaderboard (5 ACs)
**Testable Elements:**
- Export command produces valid JSON (Integration test)
- Public submission schema v1 conformance (Unit test)
- Anonymization strips all PII (Unit test — assert no paths, hostnames, API keys)
- Anonymization preserves all metrics (Unit test)
- Anonymization preserves model, persona, role (Unit test)
- --output flag writes to file (Integration test)
- Filters applied before anonymization (Unit test)
- Deterministic output — identical input produces byte-identical output (Unit test)
- Error handling: no records match filters (Integration test)

**AC Links:** 03-04, 04-01, 04-02, 04-03, 04-04

### Story 5: Suppress Emission (3 ACs)
**Testable Elements:**
- --no-scorecard flag registered and appears in --help (Integration test)
- Zero records written with --no-scorecard (Integration test)
- Default behavior preserved without flag (Integration test — regression guard)
- No side effects on reconcile exit code or output (Integration test)

**AC Links:** 05-01, 05-02, 05-03

### Story 6: Document Scorecard (1 AC)
**Testable Elements:**
- docs/scorecard.md exists (File existence check)
- Schema documented (all fields, types, required vs. conditional) (Manual review)
- Storage location documented (Manual review)
- CLI usage documented (all commands and flags) (Manual review)
- Privacy model documented (Manual review)

**AC Links:** 06-01

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** `internal/scorecard/*_test.go` (unit tests), `cmd/atcr/*_test.go` (integration tests)

**Test File Placement Examples:**
- `internal/scorecard/scorecard_test.go` — record schema, Emit() function
- `internal/scorecard/store_test.go` — JSONL read/query
- `internal/scorecard/aggregate_test.go` — aggregation, ranking, filtering
- `internal/scorecard/export_test.go` — anonymization, determinism, schema conformance
- `cmd/atcr/scorecard_test.go` — CLI command integration
- `cmd/atcr/leaderboard_test.go` — CLI command integration

**Unit/Integration/E2E:**
- **Unit tests:** Schema validation, conditional fields, aggregation logic, anonymization, filtering
- **Integration tests:** End-to-end reconcile → emit → read, CLI command execution, --no-scorecard suppression
- **E2E tests:** Not required (no external services or UI)

**Test Environment Status:**
- Framework: Go standard `testing` + `testify/assert` and `testify/require`
- Execution: `go test ./...`
- Coverage Tools: `go test -coverprofile=coverage.out ./...` (baseline: 80%)

---

## Architecture

**Primitives:**
- `ScorecardRecord` — per-reviewer eval record with schema_version, run_id, reviewer, model, role, findings_raised/corroborated/solo, corroboration_rate, cost_usd, tokens_in/out, latency_ms, conditional verification fields
- `PublicRecord` — anonymized aggregated record for public submission (no run_id, no PII)
- `FilterOpts` — filter options: --since, --model, --persona
- `EmitOpts` — emission options: --no-scorecard flag

**Module Boundaries:**
- `internal/scorecard/scorecard.go` — record schema definition, `Emit()` function (compute metrics from reconcile output, write JSONL)
- `internal/scorecard/store.go` — JSONL read/query functions (`ReadRecords()`, `FindByRunID()`)
- `internal/scorecard/aggregate.go` — aggregation, ranking, filtering (`Aggregate()`, `ApplyFilters()`)
- `internal/scorecard/export.go` — public submission JSON generation, anonymization (`Export()`, `AnonymizeRecord()`)
- `cmd/atcr/scorecard.go` — `atcr scorecard` CLI command
- `cmd/atcr/leaderboard.go` — `atcr leaderboard` CLI command

**External Dependencies:**
- `internal/llmclient` — must decode `usage` from provider responses and surface tokens/cost (hard prerequisite)
- `internal/reconcile` — provides findings, corroboration, verification data
- `os`, `bufio`, `encoding/json` — stdlib for JSONL append and serialization
- `text/tabwriter` — stdlib for table rendering
- `cobra` — CLI framework

**Replaceability:**
- `internal/scorecard/` is a black box — callers use `Emit()`, `ReadRecords()`, `Aggregate()`, `Export()` without knowing JSONL implementation details
- Storage backend (JSONL) can be replaced (e.g., SQLite, cloud storage) by implementing the same interface
- Anonymization logic is isolated in `export.go` — can be replaced or extended without affecting other modules

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Anonymization for public export | `internal/scorecard/export.go` | PII leakage (paths, API keys, hostnames, usernames) in public submission | Allowlist-based field selection (not denylist); unit test asserts no path-like strings, hostnames, or API key patterns in output; strip run_id entirely |
| JSONL file permissions | `~/.config/atcr/scorecard/` | Unauthorized access to local scorecard data | File created with 0600 permissions (user read/write only); document that directory should not be committed to git |
| Export determinism | `internal/scorecard/export.go` | Non-deterministic output breaks reproducibility, enables run-level de-anonymization | Sorted keys in JSON serialization; stable sort order by (model, reviewer, role); no random salts; no per-record timestamps |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| JSONL append during reconcile | 1 append per run (~500 bytes) | < 10ms overhead | Use `bufio.Writer` with `O_APPEND`; write + flush atomically; errors logged, never returned |
| Leaderboard aggregation | Up to 10,000 records | < 2 seconds | Stream-parse JSONL line-by-line (not load entire file); group-by in memory; single pass |
| Scorecard lookup by run_id | 1 lookup per command invocation | < 100ms | Derive month (YYYY-MM) from run_id timestamp; read only relevant JSONL file |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Missing verification.json | Reconcile run without `atcr verify` | Omit `findings_verified`, `findings_refuted`, `survived_skeptic_rate` fields from record |
| Empty or missing scorecard files | First run, deleted files, empty directory | Print "No records found" message; exit code 0; do not error |
| Malformed JSONL lines | Corrupted file, partial writes | Skip unparseable lines with warning; continue processing; do not fail command |
| No records match filters | Overly restrictive --since, --model, --persona | Print "No records match filters" with suggestion to widen --since or remove filters; exit code 1 |
| Concurrent reconcile runs | Multiple reconcile runs in parallel | Use atomic append (write + flush + fsync); each record is single line — partial writes recoverable |
| Cost/token fields unavailable | Provider omits usage, llmclient not yet parsing | Fields are `omitempty`; missing data shown as "—" in table; graceful degradation |

### Defensive Measures Required

- **Input Validation:** Validate run_id format, directory paths, filter values (--since duration format: Nd, Nw, Nm); reject unknown formats with clear error
- **Error Handling:** Scorecard emission errors logged but never returned (best-effort); CLI commands exit non-zero with clear error messages on failure
- **Logging/Audit:** Log warnings for skipped malformed JSONL lines; log errors for file write failures; do not log scorecard content (privacy)
- **Rate Limiting:** Not applicable (local-only, no network calls)
- **Graceful Degradation:** Missing verification.json → conditional fields omitted; missing usage → cost/token fields empty; missing scorecard directory → created on first write

---

## Risks

**Technical:**
- **Hard prerequisite not resolved** → Mitigation: Resolve llmclient usage parsing in Phase 1 before implementing scorecard emitter; block sprint if prerequisite cannot be resolved
- **Anonymization misses PII field** → Mitigation: Allowlist-based field selection (not denylist); unit test asserts no path-like strings, hostnames, or API key patterns in output
- **Schema evolves and old records become hard to query** → Mitigation: `schema_version` field on every record; leaderboard handles version negotiation; document that v1 is experimental until Epic 10.0 stabilizes

**TDD-Specific:**
- **Integration tests require reconcile output fixtures** → Mitigation: Create minimal test fixtures (mock reconcile output with findings.json, summary.json, verification.json); use testify fixtures
- **Anonymization determinism hard to test** → Mitigation: Unit test runs export twice on same input, asserts byte-identical output; use sorted keys and stable sort order
- **Conditional fields (verification-dependent) require two test paths** → Mitigation: Table-driven tests with two scenarios: verification.json present and absent; assert field presence/absence

---

**Next:** `/create-sprint @.planning/plans/active/3.3_per_run_scorecard/ --gated`
