# Acceptance Criteria: Id-or-Path Resolution Table-Driven Coverage

**Related User Story:** [02: Backend Contract Backward-Compatibility Test](../user-stories/02-backend-contract-backward-compatibility-test.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI regression test | `package main` in `cmd/atcr/`; table-driven `t.Run` subtests |
| Test Framework | stdlib `testing` + `testify` (`assert`/`require`) | Matches `cmd/atcr/reconcile_test.go` convention exactly |
| Key Dependencies | `cmd/atcr` cobra command tree (`newRootCmd`/`execCmd`), `os/exec` (git, only if a fixture repo is needed for the review step) | No shelling out; in-process invocation only |

## Related Files
- `cmd/atcr/backend_contract_test.go` - create: table-driven subtest (e.g. `TestBackendContract_IdOrPathResolution`) exercising all three id-or-path resolution branches against `docs/code-review-backend.md`'s contract.
- `docs/code-review-backend.md:14-33` (Invocation/Pre-flight) - reference: the id-or-path resolution rule this AC locks (bare id → `.atcr/reviews/<id>/`, path → used as-is, omitted → `.atcr/latest`), shared by `atcr reconcile` and `atcr verify`.
- `cmd/atcr/reconcile_test.go:74-84,162-176,235-251` - reference: `fixtureReview` helper, `TestReconcileCmd_InheritsExternalOutputDir` (path branch), and `TestReconcileCmd_DefaultsToLatest` (omitted branch) — the existing per-branch tests this new table-driven test must not duplicate but must consolidate/lock together as one explicit contract test.
- `cmd/atcr/reconcile_test.go:52-70` - reference: `isolate`/`execCmd` helpers for hermetic in-process invocation.

## Happy Path Scenarios
**Scenario 1: Bare review id resolves to `.atcr/reviews/<id>/`**
- **Given** a fixture review scaffolded at `.atcr/reviews/r1/` with a valid `sources/*/findings.txt`
- **When** the test runs `atcr reconcile r1` in-process
- **Then** the command exits 0 and writes `reconciled/findings.txt` under `.atcr/reviews/r1/reconciled/`, not anywhere else

**Scenario 2: Explicit path is used as-is**
- **Given** a fixture review tree scaffolded at an arbitrary absolute path outside `.atcr/reviews/` (e.g. a `t.TempDir()`-rooted `ext-review/` directory, mirroring an `--output-dir` review)
- **When** the test runs `atcr reconcile <absolute-path>` in-process
- **Then** the command exits 0 and writes `reconciled/findings.txt` under `<absolute-path>/reconciled/`, with no interaction with `.atcr/reviews/` or `.atcr/latest`

**Scenario 3: Omitted argument resolves to `.atcr/latest`**
- **Given** a fixture review scaffolded at `.atcr/reviews/r2/` with `.atcr/latest` pointing at `r2`
- **When** the test runs `atcr reconcile` (no id-or-path argument) in-process
- **Then** the command exits 0 and writes `reconciled/findings.txt` under `.atcr/reviews/r2/reconciled/` — the review `.atcr/latest` names, not any other review present on disk

## Edge Cases
**Edge Case 1: Same three branches apply to `atcr verify`**
- **Given** the story explicitly states the id-or-path rule is "shared by `atcr reconcile` and `atcr verify`" (per `docs/code-review-backend.md` and the story's Story Context)
- **When** the table-driven subtests are structured
- **Then** the test table parameterizes the command name (`reconcile` vs `verify`) alongside the three branches where feasible, OR the AC's Story-Specific Definition of Done explicitly notes if `verify` is deferred to `reconcile`-only coverage with a comment citing the shared implementation, so the scope decision is traceable rather than silently narrowed

**Edge Case 2: A bare id and an existing `.atcr/latest` pointer to a different review do not collide**
- **Given** both `.atcr/reviews/r1/` and `.atcr/reviews/r2/` exist, with `.atcr/latest` pointing at `r2`
- **When** the test runs `atcr reconcile r1` (bare id, not omitted)
- **Then** the command operates on `r1`, proving the explicit-id branch takes precedence over the latest pointer rather than falling back to it

## Error Conditions
**Error Scenario 1: One resolution branch silently regresses to another**
- If a future change makes the bare-id branch accidentally resolve like the path branch (or vice versa), the corresponding subtest's `require.FileExists`/`require.NoFileExists` assertion on the *specific* directory the artifacts landed in fails
- Error message: standard `require` failure naming the expected vs. actual artifact path
- HTTP status / error code: N/A (filesystem-level assertion, not an HTTP boundary)

**Error Scenario 2: `.atcr/latest` missing when the argument is omitted**
- Already covered by existing `TestReconcileCmd_MissingReviewIsUsageError` in `cmd/atcr/reconcile_test.go:246-251` (exit 2); this AC's omitted-branch subtest asserts only the success path (`.atcr/latest` present and valid) and does not duplicate that existing missing-pointer coverage

## Performance Requirements
- **Response Time:** Each of the three table-driven subtests must complete in well under 1 second (no real review fan-out required — `fixtureReview`-style pre-built findings fixtures are sufficient, per `docs/code-review-backend.md`'s reconcile-only scope for this AC)
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A — no provider/network interaction in this AC; reconcile operates on pre-written fixture findings files only
- **Input Validation:** The path branch must use a `t.TempDir()`-rooted absolute path, never a path derived from untrusted input, so the test itself does not become a traversal vector; this mirrors the existing `TestReconcileCmd_TraversalIdRejected` (`cmd/atcr/reconcile_test.go:302-307`) coverage of the traversal-rejection contract, which this AC does not need to re-test

## Test Implementation Guidance
**Test Type:** UNIT/INTEGRATION (in-process cobra invocation via `execCmd`, no subprocess, no network)
**Test Data Requirements:** Three fixture setups per the `fixtureReview` helper pattern (`cmd/atcr/reconcile_test.go:74-84`): one under `.atcr/reviews/<id>/`, one at an arbitrary `t.TempDir()` path, and one under `.atcr/reviews/<id>/` with `.atcr/latest` pointing at it
**Mock/Stub Requirements:** None — reconcile operates on pre-written findings files, no LLM/provider mock needed for this AC

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Bare review id resolves to `.atcr/reviews/<id>/` and is asserted as a table-driven subtest
- [ ] Explicit path is used as-is and is asserted as a table-driven subtest
- [ ] Omitted argument resolves to `.atcr/latest` and is asserted as a table-driven subtest
- [ ] `verify` sharing the same resolution rule is either covered in the table or explicitly scoped out with a citing comment

**Manual Review:**
- [ ] Code reviewed and approved
