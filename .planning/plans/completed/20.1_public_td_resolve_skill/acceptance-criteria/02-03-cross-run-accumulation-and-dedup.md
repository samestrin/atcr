# Acceptance Criteria: Cross-Run Accumulation With Write-Time Dedup

**Related User Story:** [02: Reconcile-Time Persistence Hook](../user-stories/02-reconcile-time-persistence-hook.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (CLI command handler + store consumer) | `cmd/atcr/reconcile.go`, calling into `internal/localdebt` |
| Test Framework | `go test` + `testify/require` | Multi-invocation integration tests against a shared temp `.atcr/debt/` |
| Key Dependencies | `internal/localdebt` (Story 1 `Append`/`ReadRecords`), `internal/history.FindingID` | Dedup key reused verbatim, not reimplemented |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/reconcile.go` — modify: the persistence call must check existing store records for a matching `id` (`history.FindingID(file, line, problem)`) before appending, per the write-time dedup decision below
- `internal/localdebt/store.go` — consume (Story 1 dependency): `ReadAll` used to check for an existing `id` across all shards before calling `Append`; this AC's dedup behavior depends on Story 1 exposing a read path, not just `Append`
- `internal/history/record.go` — reference (read-only): `FindingID` at line 48 is the dedup key reused verbatim, not reimplemented
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/local-td-store-schema.md` — reference (no change): documents the resolved dedup strategy (write-time dedup by `FindingID`) in the "Identity and Deduplication" section; this AC is the implementation contract for that decision
- `cmd/atcr/reconcile_test.go` — modify: add `TestRunReconcile_LocalDebtAccumulatesAcrossRuns` and `TestRunReconcile_LocalDebtDedupsSameFinding`, running `runReconcile`/`execCmd` twice against the same repo CWD with a shared `.atcr/debt/` directory

## Decision: Write-Time Dedup by FindingID

Per the plan's explicit design instruction and `documentation/local-td-store-schema.md`'s dedup decision, this story resolves the dedup question as **write-time dedup**: before calling `localdebt.Append` for a given finding, the hook reads all existing records (`ReadAll`) and checks whether a record with the same `id` (`history.FindingID(file, line, problem)`) already exists anywhere in the store; if so, the finding is skipped (not re-appended). The full-history scope prevents a finding from silently re-duplicating across a month boundary. This is the safer default given the store is append-only with no read-time compaction yet (per the story's risk table). Read-before-write is O(total records) per reconcile run but is accepted at the documented scale (append-only ledgers, hundreds of records/month) — no index or database is introduced.

## Happy Path Scenarios
**Scenario 1: Two runs against different review directories both accumulate**
- **Given** `atcr reconcile <review-dir-A>` has already run and persisted 2 findings
- **When** `atcr reconcile <review-dir-B>` runs against a different review directory with 3 distinct findings (different `file:line:problem` triples)
- **Then** the local store contains 5 records total (2 + 3) — the second run's persistence does not overwrite or truncate the first run's records

**Scenario 2: Re-running reconcile on the same review directory does not duplicate unchanged findings**
- **Given** `atcr reconcile <review-dir>` has already run and persisted a finding with `id = X`
- **When** `atcr reconcile <review-dir>` runs again against the same review directory, producing the identical finding (same `file`, `line`, `problem` → same `id = X`)
- **Then** the store still contains exactly one record with `id = X` after the second run — no duplicate line is appended

**Scenario 3: A finding whose problem text changed after a partial fix persists as a new record**
- **Given** a prior run persisted a finding with `id = X` for `problem = "leaks a file handle"`
- **When** a later `atcr reconcile` run on the same file:line reconciles a different `problem` text (e.g., the issue was partially fixed and the finding now reads differently), producing `id = Y`
- **Then** the store gains a new record for `id = Y` alongside the still-present `id = X` record — this is accepted behavior per the schema doc's documented line-drift/problem-text tradeoff (Related Documentation `local-td-store-schema.md` "Identity and Deduplication"), not a bug to work around in this story

## Edge Cases
**Edge Case 1: Dedup check against a missing or empty `.atcr/debt/` directory**
- **Given** no prior `.atcr/debt/` directory exists
- **When** the first `atcr reconcile` run persists findings
- **Then** the dedup read (`ReadAll`/`ReadRecords` on a missing directory) returns no existing records without error (per Story 1's documented `(nil, nil)` contract on a missing directory), and every finding is appended as new

**Edge Case 2: Dedup check spans only the current month shard, not all history**
- **Given** a record with `id = X` was persisted in a prior calendar month's shard (`2026-06.jsonl`)
- **When** a reconcile run in the current month (`2026-07`) reconciles the same finding (`id = X` recurs)
- **Then** the documented behavior is stated explicitly by this AC: dedup scope is per-month-shard only (matching `ReadRecords`'s single-file read shape) OR full-history via `ReadAll` — this AC requires the implementation to use `ReadAll` (full-history scan) for the dedup check specifically so a finding does not silently re-duplicate across a month boundary, since the story's stated goal is "accumulate... rather than overwriting or losing prior runs' records" across arbitrarily-spaced re-runs

**Edge Case 3: Two findings in the same run share the same `id` (a reconcile-internal duplicate)**
- **Given** the reconciled `Result` itself contains two `JSONFinding` entries that collapse to the same `id` (a defensive/unexpected case — reconcile's own clustering should prevent this, but the persistence hook must not assume it)
- **When** persisting the run's findings
- **Then** only one record for that `id` is written to the store, even within a single run (in-run dedup, not just cross-run)

## Error Conditions
**Error Scenario 1: The dedup read itself fails (e.g., a malformed shard file)**
- Error message: logged via `cmd.ErrOrStderr()`, matching the best-effort contract from AC 02-01
- HTTP status / error code: N/A — on a dedup-read failure, the hook must fail open toward "append anyway" rather than skip persistence entirely for the whole run; a corrupt read must not silently drop all of this run's findings from the backlog. This must not fail `runReconcile`'s own return value or exit code

## Performance Requirements
- **Response Time:** The dedup read-before-write must not scale worse than O(existing records in scope) per reconcile run; for the documented scale (hundreds of records/month) this stays well under 100ms even with `ReadAll` across several months
- **Throughput:** Must remain correct (no torn/duplicate lines) under the same single-`Append`-per-record, no-batching guarantee documented in AC 02-01 and Story 1

## Security Considerations
- **Authentication/Authorization:** N/A — local filesystem, same trust boundary as AC 02-01
- **Input Validation:** The `id` comparison is a plain string equality check over content-hash values; no untrusted input is interpreted as a path or command

## Test Implementation Guidance
**Test Type:** INTEGRATION (multi-invocation: run `runReconcile`/`execCmd` two or more times against the same temp repo CWD and inspect `.atcr/debt/*.jsonl` contents between runs)
**Test Data Requirements:** Two distinct fixture review directories (for Scenario 1); a single fixture review directory re-invoked twice with identical findings (for Scenario 2); a fixture where the second run's `problem` text is deliberately altered (for Scenario 3); a pre-seeded prior-month shard file (for Edge Case 2)
**Mock/Stub Requirements:** None — real files on disk under `t.TempDir()`; if month-boundary behavior (Edge Case 2) is impractical to test via wall-clock time, seed the prior-month shard file directly with a known `run_id` prefix rather than mocking `time.Now`

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Re-running `atcr reconcile` on the same review directory with unchanged findings does not duplicate records (write-time dedup by `FindingID`, checked via full-history `ReadAll`)
- [ ] Runs against different review directories accumulate additively in the same store
- [ ] A finding whose `problem` text changes across runs is treated as a distinct record (documented, not a defect)
- [ ] In-run duplicate `id`s (two findings in one `Result` collapsing to the same id) are written at most once
- [ ] A dedup-read failure fails open (appends) rather than silently dropping the run's findings, and never fails `runReconcile`'s exit code

**Manual Review:**
- [ ] Code reviewed and approved
