# Acceptance Criteria: Verification Documentation

**Related User Story:** [[06]: Report Updates & Documentation](../user-stories/06-report-updates-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Documentation | Markdown files | No build-time generation; static `.md` files |
| Verification Reference | `docs/verification.md` | New file covering full verification mechanics |
| Registry Reference | `docs/registry.md` | Modify: add `role: skeptic` subsection |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/registry/config.go:37` - reference: `RoleSkeptic` constant
- `internal/reconcile/emit.go:36` - reference: `Verification` struct shape
- `personas/_base.md` - reference: base persona template for skeptic_base.md
- `docs/registry.md` - modify: activate `role` field documentation
- `docs/findings-format.md` - modify: document verification block and v2 confidence

- `docs/verification.md` - create: new documentation file with sections: Overview, Skeptic Selection, Verdict Envelope, Confidence v2, Gate Semantics, Cost Controls, Artifacts
- `docs/registry.md` - modify: add `role: skeptic` configuration subsection with example YAML
- `personas/_base.md` - read: reference for skeptic persona structure
- `internal/reconcile/emit.go` - read: `Verification` struct definition for accurate documentation

## Happy Path Scenarios
**Scenario 1: verification.md exists with required sections**
- **Given** the documentation set after story completion
- **When** `docs/verification.md` is inspected
- **Then** it contains the following sections: Overview, Skeptic Selection, Verdict Envelope, Confidence v2, Gate Semantics, Cost Controls, and Artifacts

**Scenario 2: verification.md explains skeptic selection and different-model rule**
- **Given** a reader of `docs/verification.md`
- **When** they read the Skeptic Selection section
- **Then** it explains: role-based filtering (`role: skeptic` in registry), the different-model rule (skeptic cannot share a model with any reviewer credited on the finding), and the `no_eligible_skeptic` fallback behavior

**Scenario 3: verification.md documents confidence v2 tier model**
- **Given** a reader of `docs/verification.md`
- **When** they read the Confidence v2 section
- **Then** it includes a tier table (VERIFIED > HIGH > MEDIUM > LOW), transition rules (confirmed + HIGH → VERIFIED, refuted → demoted, unverifiable → unchanged), and comparison to v1 model

**Scenario 4: verification.md documents gate semantics**
- **Given** a reader of `docs/verification.md`
- **When** they read the Gate Semantics section
- **Then** it explains: `--fail-on` excludes refuted findings from the gate check, `--require-verified` counts only VERIFIED-tier findings as passing, and interaction between v2 tiers and gate thresholds

**Scenario 5: registry.md contains role: skeptic subsection**
- **Given** the updated `docs/registry.md`
- **When** a reader searches for "skeptic"
- **Then** they find a subsection with: (1) example YAML showing `role: skeptic` with `model: claude-sonnet-4-6`, (2) explanation of the different-model rule, (3) note that empty `role` defaults to `reviewer` for backward compatibility, (4) reference link to `docs/verification.md`

**Scenario 6: registry.md role field is no longer listed as "reserved/inert"**
- **Given** the updated `docs/registry.md`
- **When** the "Still reserved (inert until a later stage)" table is inspected
- **Then** the `role` field has been moved from the reserved table to an active section documenting its current use for skeptic agents

## Edge Cases
**Edge Case 1: verification.md Cost Controls section covers all knobs**
- **Given** a reader of `docs/verification.md`
- **When** they read the Cost Controls section
- **Then** it documents: `verify.min_severity` (minimum severity for verification), per-finding budgets, `--fresh` flag (bypass cached verdicts), and `--thorough` majority voting semantics

**Edge Case 2: verification.md Artifacts section lists all outputs**
- **Given** a reader of `docs/verification.md`
- **When** they read the Artifacts section
- **Then** it documents: `verification.json` schema, `findings.json` verification block structure, `manifest.json` stages field, and `summary.json` verdictCounts

## Error Conditions
**Error Scenario 1: Documentation references non-existent code**
- Behavior: All code references (struct names, field names, constants) in documentation must match actual codebase definitions — verified by manual review against `internal/reconcile/emit.go` and `internal/registry/`

## Performance Requirements
- **N/A:** Documentation files are static markdown; no runtime performance requirements

## Security Considerations
- **No secrets in documentation:** Example YAML in `registry.md` must not contain real API keys or credentials — use placeholder variable names (`api_key_env: OPENROUTER_API_KEY`)
- **No executable code:** Documentation examples are illustrative only; no copy-paste security risk

## Test Implementation Guidance
**Test Type:** UNIT (documentation validation)
**Test Data Requirements:** N/A — markdown files are validated structurally
**Mock/Stub Requirements:** N/A

Validation approach:
- Test that `docs/verification.md` exists and contains required section headers (grep for `## Overview`, `## Skeptic Selection`, etc.)
- Test that `docs/registry.md` contains `role: skeptic` subsection with YAML example
- Optionally: a documentation lint test that checks all internal cross-references resolve

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./...` if any doc-validation tests exist)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)
- [ ] Markdown files have no broken headings, unmatched code fences, or broken internal links

**Story-Specific:**
- [ ] `docs/verification.md` exists and contains all 7 required sections: Overview, Skeptic Selection, Verdict Envelope, Confidence v2, Gate Semantics, Cost Controls, Artifacts
- [ ] `docs/verification.md` explains the different-model rule clearly
- [ ] `docs/verification.md` includes a confidence v2 tier table
- [ ] `docs/registry.md` contains `role: skeptic` subsection with example YAML and cross-reference to `docs/verification.md`
- [ ] `role` field moved from "reserved/inert" table to active documentation in `registry.md`

**Manual Review:**
- [ ] Documentation reviewed for accuracy against actual codebase implementation
- [ ] Examples are correct and copy-pasteable (with placeholder values)
- [ ] No "we" / plural voice in documentation (per user profile rules)
