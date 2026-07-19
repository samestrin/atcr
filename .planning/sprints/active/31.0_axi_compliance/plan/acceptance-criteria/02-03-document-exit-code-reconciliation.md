# Acceptance Criteria: Document the Exit-Code Reconciliation Decision

**Related User Story:** [02: Reconcile and Document the AXI Exit-Code Contract](../user-stories/02-reconcile-and-document-axi-exit-code-contract.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Documentation (Markdown) + Go source comment block | No runtime behavior change |
| Test Framework | N/A (documentation) — verified via manual/CI doc-lint or grep-based consistency check | |
| Key Dependencies | None | |

### Related Files (from codebase-discovery.json)
- `docs/ci-integration.md` - modify: exit-semantics table (lines 11-19) gets an explicit note stating `--axi` reuses this exact 0/1/2/3 contract, and that the epic's originally proposed `2`=internal/syntax-error scheme was considered and rejected in favor of the existing `usageError`/`exitFailure` classification, with reasoning.
- `cmd/atcr/main.go` - modify: extend the exit-code comment block (currently lines 122-125, immediately above the `exitFailure`/`exitUsage`/`exitAuth` constants) to state that `--axi` mode reuses this contract unchanged, with a one-line cross-reference to `docs/ci-integration.md` and to the rejected `2`=internal-error proposal.
- `.planning/plans/active/31.0_axi_compliance/documentation/exit-code-cli-mcp-precedent.md` - reference: cited inline in both updated locations as the precedent source for this reconciliation decision (already documents the `atcr verify` cross-validation and the three-contract comparison table).

## Happy Path Scenarios
**Scenario 1: `docs/ci-integration.md` states the reconciliation decision**
- **Given** a reader of `docs/ci-integration.md`'s exit-semantics table
- **When** they read the table and its surrounding notes
- **Then** they find an explicit statement that `--axi` mode uses this same table unchanged, and a brief note that an alternative "`2`=internal/syntax-error" scheme was considered (per the epic's original proposal) and deliberately not adopted, because `2` is already reserved for usage/configuration errors that CI scripts depend on

**Scenario 2: `cmd/atcr/main.go` comment block states the reconciliation decision**
- **Given** a contributor reading the exit-code comment block directly above the `exitFailure`/`exitUsage`/`exitAuth` constants (`main.go:122-130`)
- **When** they read the comment
- **Then** they find the same reconciliation statement (AXI reuses this contract, `2`≠internal-error) present at the point in the code future contributors would actually touch when adding exit-code logic

**Scenario 3: Structured-error stream decision (axi.md Principle 6) is documented**
- **Given** the story's Constraints require an explicit, non-accidental decision — for the `--axi` surface only — on whether structured errors are written to stdout (axi.md Principle 6) or remain on stderr (atcr's existing convention), including the note that errors-on-stdout payloads remain subject to Story 4's escape-free guarantee (`documentation/axi-design-principles.md`: Principle 6; `documentation/exit-code-cli-mcp-precedent.md`: three-contract comparison)
- **When** `docs/ci-integration.md` is updated per Scenario 1
- **Then** the same note (or a directly adjacent one) records which stream carries `--axi`-mode structured errors and why, so the choice is visible to orchestration engineers rather than inherited by accident

## Edge Cases
**Edge Case 1: Documentation and code comment drift**
- **Given** both `docs/ci-integration.md` and `cmd/atcr/main.go`'s comment block are updated in the same change
- **When** the two are compared
- **Then** their stated reconciliation language is consistent (not contradictory or differently scoped) — verified by a reviewer diffing both in the same PR, per the "documentation drifts from code" risk in the user story's Potential Risks table

**Edge Case 2: `atcr verify` cross-reference**
- **Given** `atcr verify`'s exit-code table already matches this contract (per `documentation/exit-code-cli-mcp-precedent.md`)
- **When** the updated `docs/ci-integration.md` is read
- **Then** it references `atcr verify` as corroborating precedent, not merely asserting the AXI decision in isolation

## Error Conditions
**Error Scenario 1: Missing cross-reference**
- **Given** a future PR adds a new AXI-relevant subcommand with its own exit-code logic
- **When** it does not cite this documented reconciliation decision
- **Then** this is flagged as a documentation gap in code review (not an automated failure — this is a process/documentation AC, not a runtime error path)
- Error message: N/A (documentation completeness, not a runtime error)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — documentation-only change.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — no runtime behavior change.
- **Input Validation:** N/A — documentation must accurately describe existing validation behavior (`usageError`/`authError`) without introducing new claims about validation that the code does not implement.

## Test Implementation Guidance
**Test Type:** MANUAL / documentation review (no automated test framework applies); optionally a lightweight grep-based CI check (e.g., asserting both files contain a shared marker string like `"AXI reuses"` or similar) can be added as a guard against future drift, but is not required for this AC to pass.
**Test Data Requirements:** N/A.
**Mock/Stub Requirements:** N/A.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/ci-integration.md`'s exit-semantics table section explicitly states `--axi` reuses the existing 0/1/2/3 contract
- [ ] `docs/ci-integration.md` explicitly states the epic's original `2`=internal/syntax-error proposal was considered and rejected, with reasoning
- [ ] `cmd/atcr/main.go`'s exit-code comment block (lines 122-130 region) contains the equivalent statement at the code site
- [ ] Both updated locations cross-reference each other and/or `documentation/exit-code-cli-mcp-precedent.md` and `atcr verify`'s exit-code table as precedent
- [ ] The structured-error stream decision for the `--axi` surface (stdout per axi.md Principle 6 vs. stderr per atcr's existing convention) is explicitly recorded in `docs/ci-integration.md`, per the story's Constraints

**Manual Review:**
- [ ] Code reviewed and approved
