# Acceptance Criteria: Publish Core Content of `docs/agentic-consumption.md`

**Related User Story:** [05: Publish the Agentic Consumption Orchestration Guide](../user-stories/05-publish-agentic-consumption-guide.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Documentation (Markdown, repo `docs/` tree) | No runtime behavior change; purely additive doc file |
| Test Framework | N/A (documentation) — verified via manual/CI doc-lint or grep-based fact-check against shipped flags/env vars/fields | |
| Key Dependencies | None | Content must trace to Stories 1-4's shipped implementation, not to plan.md draft language |

## Related Files
- `docs/agentic-consumption.md` - create: new documentation page covering `--axi` invocation on `atcr review`/`atcr report`, the reconciled exit-code contract, pagination/truncation, and the stderr-only-diagnostics/stdout-only-payload guarantee.
- `docs/ci-integration.md` - reference: source of truth for the exit-semantics table (lines 11-19); this AC's exit-code section must match it exactly and must not restate it as a competing table.
- `docs/payload-modes.md` - reference: precedent for structure/tone (Markdown, short prose, fenced examples) and for describing byte-budget/truncation behavior analogous to the pagination section this AC must write.
- `docs/logging.md` - reference: source of truth for the stderr-only-diagnostics/stdout-only-payload guarantee this AC must describe accurately (`LOG_LEVEL`, `--log-format`, redaction).
- `docs/findings-format.md` - reference: existing `atcr-findings/v1` agent-facing format precedent, cited for context on atcr's existing machine-readable surfaces.

## Happy Path Scenarios
**Scenario 1: Doc exists and covers `--axi` invocation on both subcommands**
- **Given** a reader opens `docs/agentic-consumption.md`
- **When** they read the invocation section
- **Then** they find concrete examples of running `atcr review --axi` and `atcr report --axi` (or the equivalent shipped flag/subcommand syntax from Story 1) as a subprocess, with the resulting stdout described as a clean, token-dense TOON/JSON payload

**Scenario 2: Exit-code section matches the reconciled contract exactly**
- **Given** a reader reads the doc's exit-code section
- **When** they compare it against `docs/ci-integration.md`'s exit-semantics table
- **Then** the codes match exactly: 0=clean, 1=gate-failure, 2=usage-error, 3=auth-error, with no reintroduction of the epic's original (rejected) "2=internal/syntax-error" scheme, and the section links to `docs/ci-integration.md` rather than duplicating its table verbatim

**Scenario 3: Pagination/truncation section names actual shipped values**
- **Given** a reader reads the pagination section
- **When** they look for the default cap, override mechanism, and truncation signal
- **Then** they find the actual default (500 lines), the actual env var (`ATCR_AXI_MAX_LINES`), and the actual truncation flag (`truncated`) as shipped by Story 3 — not placeholder names

**Scenario 4: Stderr/stdout separation guarantee is stated accurately**
- **Given** a reader reads the stdout/stderr section
- **When** they cross-check it against `docs/logging.md`
- **Then** the doc states that under `--axi`, stdout carries only the payload and stderr carries only diagnostics/progress logs, consistent with `docs/logging.md`'s existing sink description, without contradicting or restating outdated logging behavior

## Edge Cases
**Edge Case 1: Doc drafted before Stories 1-4 finalize names**
- **Given** this AC is executed before Stories 1-4's concrete flag/env-var/field names are finalized
- **When** the doc is drafted
- **Then** the AC is not considered complete until every named detail (flag syntax, `ATCR_AXI_MAX_LINES`, `truncated`) is verified against the actual shipped code, not plan.md's draft language — this AC is blocked on Stories 1-4 landing first

**Edge Case 2: `atcr report` coverage is Extended Scope, not optional**
- **Given** plan.md's Extended Scope Annotations state `--axi` covers `atcr report` in addition to `atcr review`
- **When** the doc's invocation section is written
- **Then** it explicitly covers both subcommands, not only `atcr review`

**Edge Case 3: Cross-reference to `docs/findings-format.md` is contextual, not a schema claim**
- **Given** the doc mentions the existing `atcr-findings/v1` format for context
- **When** it does so
- **Then** it does not claim the `--axi` payload reuses the `atcr-findings/v1` schema unless Story 1 actually implemented it that way — the doc must describe what Story 1 shipped, verified against the source, not assumed from precedent

## Error Conditions
**Error Scenario 1: Doc misstates the exit-code contract**
- **Given** the doc's exit-code section is drafted from the epic's original acceptance-criteria text instead of `docs/ci-integration.md`'s table
- **When** a reviewer cross-checks the section against `docs/ci-integration.md`
- **Then** this is a documentation defect that must block merge — flagged in review, not an automated runtime failure
- Error message: N/A (documentation content defect, not a runtime error)
- HTTP status / error code: N/A

**Error Scenario 2: Doc references a flag/env var/field name that does not match shipped code**
- **Given** a reviewer greps the codebase for `ATCR_AXI_MAX_LINES`, `truncated`, and the `--axi` flag name
- **When** any named detail in the doc does not match what Stories 1-4 actually shipped
- **Then** this is a documentation defect that must be corrected before this AC is marked done
- Error message: N/A
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** N/A — static documentation, no runtime path.
- **Throughput:** N/A.
- **Content-accuracy requirement (substitute for performance):** every concrete claim (flag name, exit code, env var, field name) must be verified against Stories 1-4's shipped implementation at doc-publish time, not against draft/placeholder language.

## Security Considerations
- **Authentication/Authorization:** N/A — no runtime behavior change.
- **Input Validation:** N/A — documentation must accurately describe existing validation/error behavior (e.g., `usageError`/`authError` classification per `docs/ci-integration.md`) without introducing new or incorrect claims about validation the code does not implement.

## Test Implementation Guidance
**Test Type:** MANUAL / documentation review (no automated test framework applies); optionally a lightweight doc-lint or grep-based CI check (e.g., asserting the doc contains the literal strings `ATCR_AXI_MAX_LINES` and `truncated`, and that its exit-code table matches `docs/ci-integration.md`'s) can be added as a drift guard, but is not required for this AC to pass.
**Test Data Requirements:** N/A.
**Mock/Stub Requirements:** N/A.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `docs/agentic-consumption.md` exists and covers `--axi` invocation for both `atcr review` and `atcr report`
- [ ] The doc's exit-code section matches `docs/ci-integration.md`'s exit-semantics table exactly (0/1/2/3) and links to it rather than duplicating it
- [ ] The pagination section names the actual shipped default (500 lines), env var (`ATCR_AXI_MAX_LINES`), and truncation flag (`truncated`)
- [ ] The stdout/stderr separation section is consistent with `docs/logging.md`

**Manual Review:**
- [ ] Code reviewed and approved
