# User Story 3: CI Integration

**Plan:** [1.0: atcr Core - Review Engine, Reconciler, and Skill](../plan.md)

## User Story

**As a** CI pipeline enforcing code quality gates
**I want** to block PR merges when critical or high-severity findings survive reconciliation
**So that** I can prevent problematic code from being merged without manual intervention

## Story Context

- **Background:** CI pipelines need automated gates to catch bugs, security issues, and design problems before merge. atcr provides exit-code semantics that map finding severity to process exit codes, enabling PR gates with no glue code.
- **Assumptions:** CI environment has git, atcr binary, and access to LLM API keys via environment variables. CI runs `atcr review && atcr reconcile --fail-on <severity>` as a pipeline stage.
- **Constraints:** Exit code must be deterministic based on reconciled findings. Severity threshold is configurable via --fail-on flag. Must work in GitHub Actions, GitLab CI, and other CI systems.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | CLI Review Workflow (US-01), Reconciler |

## Success Criteria (SMART Format)

- **Specific:** `atcr reconcile --fail-on HIGH` returns exit code 0 when no HIGH or CRITICAL findings survive, nonzero (1) when any HIGH or CRITICAL finding survives
- **Measurable:** Exit code maps correctly to severity threshold in all cases (no findings, only LOW/MEDIUM, HIGH present, CRITICAL present)
- **Achievable:** Centralized exit-code logic in main() checks reconciled findings.json against threshold
- **Relevant:** Enables atcr as a PR gate — core integration point for CI pipelines
- **Time-bound:** Implemented alongside reconciler (task 9) and CLI (task 1)

## Acceptance Criteria Overview

1. `atcr reconcile --fail-on CRITICAL` returns nonzero only when CRITICAL findings survive
2. `atcr reconcile --fail-on HIGH` returns nonzero when HIGH or CRITICAL findings survive
3. `atcr reconcile --fail-on MEDIUM` returns nonzero when MEDIUM, HIGH, or CRITICAL findings survive
4. `atcr reconcile --fail-on LOW` returns nonzero when any finding survives
5. Exit code 0 when no findings at/above threshold survive reconciliation
6. Exit code 1 when any finding at/above threshold survives
7. `atcr review --fail-on <severity>` works as one-shot (review + reconcile + exit-code check)
8. Exit-code logic centralized in main(), not scattered across commands
9. CI example provided in examples/ci-gate.sh
10. Documentation in README.md explains CI integration

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/1.0_atcr_core/`_

## Technical Considerations

- **Implementation Notes:** 
  - Exit-code logic: cmd/atcr/main.go — centralized after command execution
  - Severity ordering: CRITICAL > HIGH > MEDIUM > LOW
  - Threshold check: iterate reconciled findings.json, check if any finding.Severity >= threshold
  - Exit codes: 0 = pass, 1 = fail (finding at/above threshold), 2 = error (usage, config, etc.)
  - One-shot mode: `atcr review --fail-on` combines review + reconcile + exit-code check

- **Integration Points:** 
  - Reconciler: produces reconciled/findings.json with severity and confidence
  - CLI: cobra commands with --fail-on flag
  - CI systems: GitHub Actions, GitLab CI, etc. consume exit codes

- **Data Requirements:** 
  - reconciled/findings.json: array of finding objects with severity field
  - Severity enum: CRITICAL, HIGH, MEDIUM, LOW (string values)
  - Threshold mapping: CRITICAL=4, HIGH=3, MEDIUM=2, LOW=1

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Exit code not deterministic | High | Centralized logic in main(), single code path |
| Severity comparison case-sensitive | Medium | Normalize to uppercase before comparison |
| CI timeout during review | Medium | Global timeout via context; CI should set appropriate timeout |
| False positives block merge | Medium | Confidence scoring helps: HIGH confidence = 2+ reviewers agree; teams can adjust --fail-on threshold |
| API key not available in CI | Medium | Clear error message: "API key env var not set"; document CI setup |

---

**Created:** June 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
