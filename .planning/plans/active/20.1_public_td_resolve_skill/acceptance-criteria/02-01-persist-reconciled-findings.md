# Acceptance Criteria: Persist Reconciled Findings Into the Local TD Store

**Related User Story:** [02: Reconcile-Time Persistence Hook](../user-stories/02-reconcile-time-persistence-hook.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (CLI command handler) | `cmd/atcr/reconcile.go`, `runReconcile` |
| Test Framework | `go test` + `testify/require` | Matches existing `cmd/atcr/reconcile_test.go` conventions |
| Key Dependencies | `internal/localdebt` (Story 1), `internal/reconcile` (`Result.JSONFindings()`), stdlib only | No new external dependency |

## Related Files
- `cmd/atcr/reconcile.go` - modify: add the persistence call in `runReconcile`, immediately after the `scorecard.EmitForReconcile` call (~line 111), passing `res.JSONFindings()` (not `res.Findings`) plus `Root: "."`
- `internal/reconcile/emit.go` - reference (no change): `Result.JSONFindings()` (line 197) and `JSONFinding` (line 62) are the source of `Justification`/`SourceReport` — `Result.Findings []Merged` does NOT carry these fields, only the cached, path-validated `jsonFindings` does
- `internal/reconcile/justification.go` - reference (no change): `stampJustifications` (called from `internal/reconcile/gate.go:255`) is what populates `Justification`/`SourceReport` on `JSONFinding` before `runReconcile` ever sees the `Result`
- `internal/localdebt/store.go` - consume (Story 1 dependency): calls `localdebt.Append(dir, rec)` once per finding
- `cmd/atcr/reconcile_test.go` - modify: add coverage for the new persistence call, mirroring the existing `countScorecardLines`-style helper pattern (see `cmd/atcr/reconcile_test.go:23`)

## Happy Path Scenarios
**Scenario 1: A completed reconcile run persists every finding to the local store**
- **Given** a review directory with 3 reconciled findings and no `.atcr/debt/` directory yet
- **When** `atcr reconcile <review-dir>` runs to completion
- **Then** `.atcr/debt/YYYY-MM.jsonl` is created (directory `0700`, file `0600`) and contains exactly 3 JSONL lines, one per finding, each with `schema_version: 1`, a non-empty `id` (from `history.FindingID(file, line, problem)`), `run_id` equal to `res.Summary.ReconciledAt + "-" + filepath.Base(reviewDir)` (matching `scorecard.EmitForReconcile`'s `runID` construction), and the required fields from `documentation/local-td-store-schema.md` (`ts`, `severity`, `file`, `line`, `problem`, `fix`, `category`, `est_minutes`, `evidence`, `reviewers`, `confidence`)

**Scenario 2: Justification and SourceReport carry through when present**
- **Given** a review directory whose `sources/<reviewer>/review.md` narrative matches a finding's `file:line` (so `stampJustifications` stamps `Justification` and `SourceReport` on that finding before `runReconcile` receives the `Result`)
- **When** `atcr reconcile <review-dir>` runs
- **Then** the persisted record for that finding includes non-empty `justification` and a `source_report` object with `path`, `line`, and `section`, sourced from `res.JSONFindings()[i].Justification` / `.SourceReport`, not re-derived

**Scenario 3: Findings with no justification match persist without the optional fields**
- **Given** a finding with no matching `review.md` narrative (Justification/SourceReport left empty by `stampJustifications`)
- **When** `atcr reconcile <review-dir>` runs
- **Then** the persisted record omits `justification` and `source_report` (`omitempty`), matching the v1 schema's optional-field contract, and all required fields are still present

## Edge Cases
**Edge Case 1: Reconcile finds zero findings**
- **Given** a review directory that reconciles to zero findings
- **When** `atcr reconcile <review-dir>` runs
- **Then** no `.atcr/debt/` directory is created (or, if already present from a prior run, no new lines are appended) — persistence is a no-op on an empty result, not a zero-length write

**Edge Case 2: `.atcr/debt/` directory does not yet exist**
- **Given** a fresh repo with no `.atcr/` directory at all
- **When** `atcr reconcile <review-dir>` runs and produces at least one finding
- **Then** `.atcr/debt/` is created lazily (`MkdirAll`, `0700`) exactly as `internal/localdebt.Append` documents, with no manual pre-creation required by the caller

**Edge Case 3: Multiple findings in one run share the same month shard**
- **Given** a reconcile run with 5 findings, all reconciled in the same calendar month
- **When** `atcr reconcile <review-dir>` runs
- **Then** all 5 records land in the same `YYYY-MM.jsonl` file via 5 separate `Append` calls (one `os.Write` per record — no batching), and no line is torn or interleaved

## Error Conditions
**Error Scenario 1: `localdebt.Append` fails for one or more findings (e.g., disk full, `.atcr/debt/` unwritable)**
- Error message: failure is logged via `cmd.ErrOrStderr()` (the same diagnostics channel `scorecard.EmitForReconcile` uses), not returned as a command error
- HTTP status / error code: N/A (CLI) — `runReconcile`'s own return value and exit code are unaffected; the reconcile gate's exit-1/exit-2 semantics are unchanged by a persistence failure

**Error Scenario 2: `res.JSONFindings()` is empty despite `res.Findings` being non-empty (an internal invariant violation)**
- Error message: N/A — this is a defensive case; the hook must not panic on a length mismatch or nil slice, and should simply persist whatever `JSONFindings()` returns (including zero records) rather than falling back to `res.Findings` and silently losing `Justification`/`SourceReport`
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** Persistence adds no more than one `os.Write` per finding to `runReconcile`'s total wall time; for a typical review (10-50 findings) this must not add a perceptible (>100ms) delay to command completion
- **Throughput:** Must handle a review with hundreds of findings (e.g., a large multi-source fan-out) without materializing the whole store file in memory — only appends are performed, never a full-file read

## Security Considerations
- **Authentication/Authorization:** N/A — local filesystem operation under the CLI-invoking user's existing permissions; no new privilege boundary
- **Input Validation:** File/line/problem/fix/evidence text is persisted verbatim (already validated upstream by the reconcile pipeline's path-existence checks and severity enum); the persistence call must not re-interpret or execute any of these string fields. Diagnostics written to `cmd.ErrOrStderr()` on failure must use `basePathErr`-style path scrubbing (mirroring `internal/scorecard/store.go`'s `basePathErr`) so an absolute store path is not echoed with a username component into logs

## Test Implementation Guidance
**Test Type:** INTEGRATION (exercises the full `atcr reconcile` CLI command against a real temp review directory and real `.atcr/debt/` files on disk, matching the existing `cmd/atcr/reconcile_test.go` style)
**Test Data Requirements:** A fixture review directory with 2-3 source findings (some with matching `review.md` narratives, some without) sufficient to exercise Scenarios 1-3; a pre-populated `.atcr/debt/` directory for Edge Case 3 (same-month accumulation)
**Mock/Stub Requirements:** None — `internal/localdebt` (Story 1) is a real stdlib-only package operating on a temp directory; no filesystem or clock mocking beyond `t.TempDir()` and `t.Chdir()` (or equivalent CWD redirection matching existing test helpers)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `runReconcile` calls the Story 1 store's append API once per finding in `res.JSONFindings()`, after the `scorecard.EmitForReconcile` call
- [ ] Persisted records carry `Justification`/`SourceReport` when present and omit them when absent, per the v1 schema
- [ ] A zero-finding reconcile performs no persistence I/O
- [ ] A persistence failure is logged and never changes `runReconcile`'s return value or exit code

**Manual Review:**
- [ ] Code reviewed and approved
