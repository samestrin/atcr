# Acceptance Criteria: Concurrency Guarantee and Package Documentation

**Related User Story:** [01: Local TD Store Persistence](../user-stories/01-local-td-store-persistence.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package doc comments (`internal/localdebt`) | No runtime behavior change — documentation-only AC, verified by static inspection and a concurrent-write test |
| Test Framework | `go test` (concurrency regression test) + manual doc-comment review | Mirrors `internal/scorecard/store_test.go`'s `TestStore_ConcurrentAppend_SameMonthFile` |
| Key Dependencies | None beyond stdlib `sync` for the test | |

### Related Files (from codebase-discovery.json)
- `internal/localdebt/store.go` — modify: package-level and `Append`-level doc comments stating the concurrency guarantee (one `Append` call = one `os.Write`, no cross-record batching) and referencing the accepted TD-004 won't-fix stance, mirroring `internal/scorecard/store.go:76-88`
- `internal/localdebt/doc.go` (or package comment atop `record.go`/`store.go`) — create/modify: package-level doc explaining why `.atcr/` (not `.planning/`) is the correct root for this store, distinguishing it from `internal/history`'s Epic 19.4 migration
- `internal/localdebt/store_test.go` — create: `TestStore_ConcurrentAppend_SameMonthFile`-equivalent test locking the no-tearing guarantee under concurrent goroutines
- `internal/scorecard/store.go` — reference (read-only): lines 76-88, the concurrency-guarantee comment style to mirror
- `internal/scorecard/store_test.go` — reference (read-only): `TestStore_ConcurrentAppend_SameMonthFile` as the concurrency regression test pattern to replicate
- `internal/history/paths.go` — reference (read-only): `LegacyLedgerPath` (`.atcr/findings-history.jsonl`) and `ShardDir` (`.planning/history/`) documenting the Epic 19.4 migration that `internal/localdebt` must distinguish itself from
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/local-td-store-schema.md` — reference: "Concurrency and Persistence Guarantees" and "Relationship to Other Stores" sections that this AC's doc comments must reflect
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/append-only-store-pattern.md` — reference: explicit concurrency-guarantee and TD-004 won't-fix requirements

## Happy Path Scenarios
**Scenario 1: Concurrent Append calls to the same month shard never tear or interleave**
- **Given** an empty store directory and 50 goroutines, each holding a `Record` with a unique sentinel value in an otherwise-identical `run_id` month
- **When** all 50 goroutines call `Append(dir, rec)` concurrently and the test waits for all to complete
- **Then** the resulting shard file contains exactly 50 well-formed JSON lines (no torn/merged lines), each independently parseable, and every sentinel value 0..49 appears exactly once (no lost or duplicated writes)

**Scenario 2: Package doc comment states the concurrency guarantee explicitly**
- **Given** the `internal/localdebt` package source
- **When** the `Append` function's doc comment is read
- **Then** it explicitly states: one `Append` call issues exactly one `os.Write` per record to a file opened `O_APPEND`; no `bufio.Writer` or batching is shared across records; and it references the accepted TD-004 won't-fix tradeoff on cross-process `O_APPEND` locking already applied to the other five append-only ledgers (audit, debate, scorecard, tools, history)

## Edge Cases
**Edge Case 1: Package doc explains `.atcr/` vs `.planning/` without contradicting Epic 19.4**
- **Given** the `internal/localdebt` package-level doc comment
- **When** read alongside `internal/history`'s package doc (which documents its Epic 19.4 migration to `.planning/history/`)
- **Then** the `internal/localdebt` doc explicitly states it targets a different audience (standalone/public users with zero `.planning/` directory) and does not import or extend `internal/history`'s storage logic — only reuses `FindingID` — so a reader does not conclude the two stores contradict each other

**Edge Case 2: Dedup strategy decision is documented as write-time dedup**
- **Given** the design sprint resolved the dedup strategy as write-time dedup by `history.FindingID(file, line, problem)` using a full-history `ReadAll` scan before each append
- **When** the `internal/localdebt` package doc comment (or `record.go`/`store.go` comment) is read
- **Then** it explicitly documents this strategy and its rationale, so Story 2 (the `atcr reconcile` persistence hook) has a settled contract to write against

## Error Conditions
**Error Scenario 1: A doc-only AC has no runtime error path**
- **Given** this AC is documentation- and guarantee-verification-focused
- **When** reviewed for error conditions
- Error message: N/A — no new error paths are introduced by this AC; the concurrency guarantee is verified via the concurrent-append test (Happy Path Scenario 1), not via error injection
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A beyond Append's existing single-write-per-record cost (covered by AC 01-01).
- **Throughput:** The concurrency test (50 goroutines) must complete without flaking under `go test -race`; no torn or lost writes at that concurrency level.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** N/A — this AC is about documentation accuracy and concurrent-write integrity, not input handling.

## Test Implementation Guidance
**Test Type:** UNIT (concurrency regression) + manual doc-comment review
**Test Data Requirements:** 50 `Record` values sharing a `run_id` month but each carrying a unique sentinel field (e.g. `est_minutes` or a dedicated test field) so post-hoc parsing can verify no sentinel was lost or duplicated.
**Mock/Stub Requirements:** None — real `t.TempDir()` and real goroutines with `sync.WaitGroup`, run under `go test -race` to catch data races in addition to file-level tearing.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Concurrent-append test (50 goroutines, same month shard) passes with zero torn/lost/duplicated lines under `go test -race`
- [x] `Append`'s doc comment explicitly states the one-Append-equals-one-os.Write guarantee and cites TD-004
- [x] Package-level doc comment explains why `.atcr/` (not `.planning/`) is correct for this store and distinguishes it from `internal/history`'s Epic 19.4 migration
- [x] The dedup strategy is documented as write-time dedup by `id` (`history.FindingID`) using a full-history `ReadAll` scan before each append, not left as an open question

**Manual Review:**
- [ ] Code reviewed and approved
