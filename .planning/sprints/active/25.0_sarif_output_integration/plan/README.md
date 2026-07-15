# Plan 25.0: SARIF Output Integration

## Overview

Adds SARIF 2.1.0 JSON as a fourth `atcr report --format` render target (alongside `md`, `json`, `checklist`), so ATCR's reconciled findings can feed GitHub Advanced Security's Code Scanning "Security" tab and GitLab CI's native SAST report widget — the two centralized security surfaces ATCR's existing `atcr github` direct-API PR integration does not reach. Sourced from Epic Plan 25.0, which was routed to the full `/init-plan` pipeline (rather than `/execute-epic`) because it touches 3 components (`internal/report`, `cmd/atcr`, `docs/`), exceeding `/execute-epic`'s ≤2-component scope guard.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/25.0_sarif_output_integration/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/25.0_sarif_output_integration/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/25.0_sarif_output_integration/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/25.0_sarif_output_integration/`

## Timeline & Milestones

TBD — estimated after `/design-sprint` scores complexity and phase structure. Complexity was pre-estimated as Semi-Complex (~4 user stories) during Phase 4 analysis.

## Resource Requirements

Single-engineer, single-branch effort — no new runtime dependency required (see [package-recommendations.md](package-recommendations.md)), no cross-team coordination, no infrastructure provisioning.

## Expected Outcomes

- `atcr report --format=sarif` emits schema-valid SARIF 2.1.0 JSON over reconciled findings.
- ATCR severities (CRITICAL/HIGH/MEDIUM/LOW) map correctly and consistently to SARIF `level` values, sourced from the existing canonical `reconcile.SeverityRank`/`NormalizeSeverity` rubric (no new rubric duplication — see TD-0052).
- File/line anchoring is correct, including the file-level (`Line<=0`) edge case.
- Documented, copy-pasteable CI examples exist for both the GitHub Code Scanning upload (`codeql-action/upload-sarif`) and the GitLab CI SAST-widget equivalent.

## Risk Summary

Low-to-medium risk, contained to a single new render function plus documentation. Primary risk is SARIF schema correctness (mitigated via schema validation in tests using the already-vendored `google/jsonschema-go`, plus a manual upload smoke-test) and avoiding a fourth independent severity-rubric definition (mitigated by reusing `reconcile.NormalizeSeverity`/`SeverityRank`). See `plan.md`'s Risk Mitigation section for detail.

## Documentation References

**Location:** [`documentation/`](documentation/)

- [CRITICAL] [SARIF 2.1.0 Schema Reference](documentation/sarif-schema-reference.md)
- [CRITICAL] [GitHub Code Scanning SARIF Integration Constraints](documentation/github-code-scanning-integration.md)
- [IMPORTANT] [Schema-Validating SARIF Output with jsonschema-go](documentation/schema-validation-with-jsonschema-go.md)
- [REFERENCE] [encoding/json Conventions for renderSarif](documentation/json-encoding-conventions.md)

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Package Recommendations](package-recommendations.md)
- [Documentation](documentation/)
- [User Stories](user-stories/)
- [Acceptance Criteria](acceptance-criteria/)
- [Sprint Design](sprint-design.md)
