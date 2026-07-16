# Acceptance Criteria: `docs_audit_test.go` Flag/Index Coverage Passes with Finalized Docs

**Related User Story:** [05: Telemetry Privacy Documentation](../user-stories/05-telemetry-privacy-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go test verification (no new test code) | Existing gate: `cmd/atcr/docs_audit_test.go` |
| Test Framework | `go test` | `TestDocsIndexCoversEveryDoc` (~line 468), `TestDocsClaimedFlagsAreReal` (~line 587) |
| Key Dependencies | `github.com/spf13/cobra`, `github.com/spf13/pflag` (already in `go.mod`, used by the audit test to walk the live command tree) | No new dependency introduced by this AC |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/docs_audit_test.go` - reference only (no code change expected): the existing test file whose `TestDocsIndexCoversEveryDoc` (`cmd/atcr/docs_audit_test.go:468`) and `TestDocsClaimedFlagsAreReal` (`cmd/atcr/docs_audit_test.go:587`) functions gate this AC
- `docs/telemetry.md` - reference (created by AC 05-01): input to both audited tests (must be indexed and its flag idiom must resolve)
- `docs/README.md` - reference (modified by AC 05-01): input to `TestDocsIndexCoversEveryDoc`
- `docs/scorecard.md` - reference (modified by AC 05-02): re-audited as part of the full `docs/*.md` sweep even though it is not the primary target of either test's new-content concern

## Happy Path Scenarios

**Scenario 1: Full docs test target passes after all Story 5 doc changes land**
- **Given** AC 05-01 has created `docs/telemetry.md` and linked it from `docs/README.md`, and AC 05-02 has updated `docs/scorecard.md`'s Privacy Model section
- **When** `go test ./cmd/atcr/... -run TestDocs` is run
- **Then** the command exits 0 and all matching tests (including `TestDocsIndexCoversEveryDoc`, `TestDocsClaimedFlagsAreReal`, and any other `TestDocs*`-prefixed test in the file, e.g. `TestDocsIndexCoversEveryDoc`, `TestAtcrInvocationsSkipsFencedProse` if matched by the prefix) report PASS

**Scenario 2: `TestDocsIndexCoversEveryDoc` recognizes the new file**
- **Given** `docs/telemetry.md` exists on disk and `docs/README.md` links `telemetry.md` under "Benchmarking & observability"
- **When** `TestDocsIndexCoversEveryDoc` globs `docs/*.md` and cross-checks against `docs/README.md`'s extracted link targets
- **Then** `telemetry.md` appears in both the `expected` set (from the glob) and the `linked` set (from the extracted links), so no `t.Errorf` fires for it

**Scenario 3: `TestDocsClaimedFlagsAreReal` validates every flag idiom across all audited markdown**
- **Given** `docs/telemetry.md` uses the `` `--sync-cloud` flag `` idiom, and `--sync-cloud` is a real registered flag returned by `canonicalFlags()` once Story 4 lands
- **When** `TestDocsClaimedFlagsAreReal` runs its regex over every file returned by `auditedMarkdown()` (all of `docs/*.md` plus root `README.md`)
- **Then** the `--sync-cloud` match resolves against `flags["sync-cloud"] == true` and produces no error

## Edge Cases

**Edge Case 1: Sequencing before Stories 1-4 are complete**
- **Given** this story is explicitly sequenced last (Dependencies: Stories 1-4) because `TestDocsClaimedFlagsAreReal` validates against the *compiled* command tree
- **When** `docs/telemetry.md` is drafted before `--sync-cloud`/`ATCR_TELEMETRY`/`ATCR_API_KEY` are merged
- **Then** the AC is not considered complete until run against a build where Stories 1-4's flags/commands are actually registered — running the test against an incomplete tree is expected to fail and does not satisfy this AC

**Edge Case 2: Root `README.md` is also in the audited set**
- **Given** `auditedMarkdown()` includes the repository root `README.md` alongside `docs/*.md`
- **When** this story's changes are scoped only to `docs/telemetry.md` and `docs/scorecard.md`
- **Then** no unrelated edit to the root `README.md` is required for this AC, but if the root `README.md` happens to reference `--sync-cloud` or other new flags elsewhere, that reference is equally subject to `TestDocsClaimedFlagsAreReal` and must resolve

## Error Conditions

**Error Scenario 1: `docs/telemetry.md` created but not linked**
- Error message: `docs/README.md index does not link docs/telemetry.md`
- Source: `t.Errorf` in `TestDocsIndexCoversEveryDoc` (`cmd/atcr/docs_audit_test.go:518`)
- Resolution: this AC requires AC 05-01's `docs/README.md` link addition to be present before this test suite is re-run

**Error Scenario 2: A documented flag does not exist on the compiled command tree**
- Error message: `%s documents `--%s` as a flag but no such CLI flag exists` (e.g. `docs/telemetry.md documents \`--sync-cloud\` as a flag but no such CLI flag exists`)
- Source: `t.Errorf` in `TestDocsClaimedFlagsAreReal` (`cmd/atcr/docs_audit_test.go:594`)
- Resolution: verify the exact flag name against `cmd/atcr/flags.go:addSyncCloudFlags` (Story 4) rather than the plan's proposed name, and correct the doc's idiom to match

## Performance Requirements
- **Response Time:** `go test ./cmd/atcr/... -run TestDocs` completes in well under 5 seconds (in-process file reads and regex scans over a small `docs/` tree — no network, no external process).
- **Throughput:** Not applicable (single test invocation, not a load path).

## Security Considerations
- **Authentication/Authorization:** N/A — test-only verification, no runtime auth surface.
- **Input Validation:** N/A — the audit test reads static files from the repository checkout; no external/untrusted input is processed.

## Test Implementation Guidance
**Test Type:** UNIT (pre-existing Go tests; this AC is pure verification, not new test authorship)
**Test Data Requirements:** A working tree with Stories 1-4 merged (or their exact flag/env/exit-code contracts finalized) plus this story's `docs/telemetry.md` and `docs/scorecard.md` changes in place.
**Mock/Stub Requirements:** None — `docs_audit_test.go` walks the real `newRootCmd()` tree built from the actual compiled binary's command registration, with no mocking.

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./cmd/atcr/... -run TestDocs` exits 0
- [ ] `go vet ./cmd/atcr/...` reports no issues
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `TestDocsIndexCoversEveryDoc` passes with `docs/telemetry.md` present and linked
- [ ] `TestDocsClaimedFlagsAreReal` passes with `--sync-cloud` (and any other newly documented flag) resolving against the real compiled command tree
- [ ] Test run performed against a build that includes Stories 1-4's merged flag/env-var/exit-code contracts, not speculative names

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Test output captured/confirmed locally (`go test ./cmd/atcr/... -run TestDocs -v`) as part of this story's Definition of Done, per the story's Potential Risks mitigation
