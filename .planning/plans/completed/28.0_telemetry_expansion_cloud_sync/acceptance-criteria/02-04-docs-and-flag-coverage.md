# Acceptance Criteria: `docs/telemetry.md` Documents the Opt-Out and `docs_audit_test.go` Coverage Passes

**Related User Story:** [02: Telemetry Opt-Out](../user-stories/02-telemetry-opt-out.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation + Go test (doc-fact cross-check) | `docs/telemetry.md`, `cmd/atcr/docs_audit_test.go` |
| Test Framework | `go test` | Existing `docs_audit_test.go` pattern: `TestArchitectureDocDescribesReconciler`-style fact cross-check, `TestDocsClaimedFlagsAreReal`-style flag cross-check |
| Key Dependencies | None new | Reuses `canonicalFlags()`, `auditedMarkdown(t)` helpers already in `docs_audit_test.go` |

### Related Files (from codebase-discovery.json)
- `docs/telemetry.md` - create: documents `ATCR_TELEMETRY` (exact accepted values, inverse boolean direction vs. `ATCR_DISABLE_AST_GROUPING`) and `atcr config set telemetry <true|false>` (exact command, key restriction, persistence target).
- `cmd/atcr/docs_audit_test.go` - modify: add a fact-check (following `TestArchitectureDocDescribesReconciler`'s `facts` table pattern, `cmd/atcr/docs_audit_test.go:555-559`) asserting `docs/telemetry.md` contains the literal string `ATCR_TELEMETRY`, and extend/add coverage so `atcr config set` is recognized as a real, documented command (following `TestDocsClaimedFlagsAreReal`'s `canonicalFlags()` cross-check pattern, `cmd/atcr/docs_audit_test.go:587-598`, or an equivalent command-existence check).
- `cmd/atcr/config.go` - modify: `Long` help text on `newConfigSetCmd()` states the exact accepted boolean values and that `telemetry` is currently the only supported key, per the story's third Potential Risk (confusable inverse-direction env vars).
- `docs/README.md` - modify: add `telemetry.md` to the linked-docs index (the audit test's `linked`/`expected` map, per the pattern visible around `cmd/atcr/docs_audit_test.go:520-526`, requires every doc under `docs/` to be linked from `docs/README.md`).

## Happy Path Scenarios
**Scenario 1: doc exists and states the env var**
- **Given** `docs/telemetry.md` has been created
- **When** `TestDocsClaimedFlagsAreReal`-adjacent or a new dedicated test reads the file
- **Then** it contains the literal string `ATCR_TELEMETRY` and documents both the enabling values (unset, `1`, `true`, `t`, `True`, `TRUE`) and disabling values (`0`, `false`, `f`, `F`, `False`, `FALSE`)

**Scenario 2: doc states the config command exactly**
- **Given** `docs/telemetry.md` documents the persistence route
- **When** the doc is read
- **Then** it contains the literal command `atcr config set telemetry false` (and the `true` re-enable form), matching the real cobra command surface added in AC 02-02

**Scenario 3: `docs/README.md` links the new doc**
- **Given** `docs/telemetry.md` is added under `docs/`
- **When** the existing `docs/README.md` link-coverage test (docs_audit_test.go around line 520) runs
- **Then** `docs/telemetry.md` is linked from `docs/README.md` and the test does not flag it as an orphaned/unlinked doc

## Edge Cases
**Edge Case 1: doc must state the inverse-direction warning explicitly**
- **Given** the codebase already has `ATCR_DISABLE_AST_GROUPING` (disable-when-truthy) alongside the new `ATCR_TELEMETRY` (enable-when-truthy — inverse direction)
- **When** `docs/telemetry.md` is written
- **Then** it explicitly calls out that `ATCR_TELEMETRY` names the enabled state directly (unlike `ATCR_DISABLE_AST_GROUPING`), so a reader does not set `ATCR_TELEMETRY=true` intending to disable telemetry

**Edge Case 2: doc-fact cross-check catches a stale rewrite**
- **Given** a future edit accidentally renames or removes `ATCR_TELEMETRY` from `docs/telemetry.md` while leaving generic telemetry vocabulary intact
- **When** the new fact-check test in `docs_audit_test.go` runs
- **Then** it fails (mirroring how `TestArchitectureDocDescribesReconciler` fails on a drifted `ATCR_DISABLE_AST_GROUPING` reference), not merely passing on keyword presence

**Edge Case 3: `atcr config set` help text is self-documenting**
- **Given** a user runs `atcr config set --help` or `atcr config set` with no args
- **Then** the cobra `Long` text states the single supported key (`telemetry`) and the accepted boolean values, so the CLI itself is a secondary source of truth consistent with `docs/telemetry.md`

## Error Conditions
**Error Scenario 1: doc missing entirely**
- **Given** `docs/telemetry.md` does not exist
- **When** the new docs-audit test attempts to read it
- **Then** the test fails with a clear message
- Error message: `docs/telemetry.md must exist (per Story 02 AC4): <underlying os error>` (mirrors `TestArchitectureDocDescribesReconciler`'s `t.Fatalf` pattern at docs_audit_test.go:540)
- HTTP status / error code: N/A (test failure, not a runtime error)

**Error Scenario 2: doc omits the env var literal**
- **Given** `docs/telemetry.md` exists but never mentions `ATCR_TELEMETRY` verbatim (e.g. only says "the telemetry environment variable")
- **When** the fact-check test runs
- **Then** it fails
- Error message: `docs/telemetry.md omits the ATCR_TELEMETRY env var name; the doc must state this load-bearing fact, not just telemetry vocabulary` (mirrors docs_audit_test.go:562)
- HTTP status / error code: N/A (test failure)

**Error Scenario 3: `docs/README.md` link coverage fails**
- **Given** `docs/telemetry.md` is added but not linked from `docs/README.md`
- **When** the existing link-coverage test runs
- **Then** it fails, flagging the new doc as unreachable from the docs index
- Error message: `docs/README.md does not link docs/telemetry.md` (mirrors the existing `expected[doc]` check style at docs_audit_test.go:522-524)
- HTTP status / error code: N/A (test failure)

## Performance Requirements
- **Response Time:** N/A — documentation and a compile-time-adjacent test; no runtime performance surface.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** N/A — this AC is documentation and static doc-fact verification, no runtime input.
- **Privacy transparency:** The doc must not merely mention telemetry exists but must give privacy-conscious readers a complete, actionable disable procedure (both surfaces), since this doc is the trust artifact epic AC3 depends on — an incomplete or misleading doc undermines the opt-out's credibility even if the code is correct.

## Test Implementation Guidance
**Test Type:** UNIT (Go test reading and asserting on `docs/telemetry.md` content, following the existing `docs_audit_test.go` conventions exactly)
**Test Data Requirements:** The real `docs/telemetry.md` file content (read via `os.ReadFile`, not a fixture — this test guards the actual shipped doc, matching how `TestArchitectureDocDescribesReconciler` reads the real `docs/architecture.md`).
**Mock/Stub Requirements:** None — pure file-content assertions, no mocking needed.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/telemetry.md` created, documents `ATCR_TELEMETRY` exact accepted values and `atcr config set telemetry <true|false>` exact command
- [ ] Doc explicitly states the inverse boolean direction vs. `ATCR_DISABLE_AST_GROUPING`
- [ ] `docs/README.md` links `docs/telemetry.md`
- [ ] New/extended `docs_audit_test.go` fact-check asserts `ATCR_TELEMETRY` and the `atcr config set telemetry` command are present in the doc, following the `ATCR_DISABLE_AST_GROUPING` precedent

**Manual Review:**
- [ ] Code reviewed and approved
