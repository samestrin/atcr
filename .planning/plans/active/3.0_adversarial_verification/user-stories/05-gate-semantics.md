# User Story 5: Gate Semantics

**Plan:** [3.0: Adversarial Verification](../plan.md)

## User Story

**As a** CI operator gating merge decisions with `atcr`
**I want** `--fail-on` to exclude refuted findings and `--require-verified` to count only VERIFIED findings
**So that** the CI gate blocks merges only on findings that have survived adversarial scrutiny, not on noise that skeptics have already disproved

## Story Context

- **Background:** Stories 3 and 4 deliver the verification pipeline, re-emit artifacts with v2 confidence tiers, and expose `atcr verify` via CLI and MCP. The gate counter at `internal/reconcile/gate.go:57` (`CountAtOrAbove`) currently counts findings by confidence level but does not distinguish refuted findings (v1=HIGH, verdict=refuted → confidence=LOW) from naturally-LOW findings. The MCP path uses `failingFindings` at `internal/mcp/handlers.go:339`. What is missing is the semantic update: `--fail-on` must skip refuted findings, and `--require-verified` must count only VERIFIED findings — the strictest gate, requiring findings to have been confirmed by a skeptic.
- **Assumptions:**
  - Story 3 is complete: `CountAtOrAbove` already excludes refuted findings (or this story completes the update if Story 3 only prepared the data).
  - Re-emitted `findings.json` contains `verification` blocks with `verdict` fields (confirmed/refuted/unverifiable) per finding.
  - `--fail-on` is already a CLI flag on `atcr review` / `atcr reconcile`; this story adds `--require-verified` as a new flag and updates gate logic.
- **Constraints:**
  - Refuted findings must never contribute to gate failure counts — regardless of their v1 confidence or severity.
  - `--require-verified` is an opt-in flag; without it, `--fail-on` counts all non-refuted findings at or above the threshold (including unverified and VERIFIED).
  - The MCP path (`failingFindings` in `internal/mcp/handlers.go`) must honor the same semantics as the CLI path.
  - All new logic must be unit-tested with fixture matrix tests covering confirmed/refuted/unverifiable × severity levels.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Story 3 (Confidence v2 & Re-emit) — needs re-emitted findings with verification blocks; Story 4 (CLI Command & MCP Tool) — needs `atcr verify` to have run and produced artifacts |

## Success Criteria (SMART Format)

- **Specific:** (1) `CountAtOrAbove` at `internal/reconcile/gate.go:57` filters out findings with `Verification.Verdict == "refuted"` before counting. (2) A new `--require-verified` flag (bool, default false) is added to `atcr review` and `atcr reconcile` (and `atcr verify` if it accepts gate flags). When set, the gate counts only findings with `confidence == "VERIFIED"` (i.e., verdict=confirmed) at or above the severity threshold. (3) `failingFindings` at `internal/mcp/handlers.go:339` applies the same filtering logic. (4) Fixture matrix tests validate all combinations: confirmed/refuted/unverifiable × HIGH/MEDIUM/LOW severity × `--fail-on` threshold × `--require-verified` on/off.
- **Measurable:** (1) `go test ./internal/reconcile/... ./internal/mcp/...` passes with >= 95% coverage on gate logic. (2) Matrix tests cover >= 12 scenarios (3 verdicts × 3 severities × 2 flag states, minimum). (3) A fixture with 3 findings (v1=HIGH+refuted, v1=MEDIUM+confirmed, v1=HIGH+unverifiable) and `--fail-on high` produces: count=1 (unverifiable) without `--require-verified`, count=0 with `--require-verified`. (4) `go vet` and existing CI checks remain clean.
- **Achievable:** This is a filtering update to existing gate logic. The `Verification` struct is already populated by Story 3; this story adds conditional exclusion based on verdict and confidence.
- **Relevant:** This is the CI trust layer. Without correct gate semantics, the verification stage has no operational effect — a refuted finding could still block a merge, or an unverified finding could pass a strict gate. This story ensures the gate reflects adversarial scrutiny.
- **Time-bound:** Expected to complete within week 3 of the 3–4 week epic (after Stories 3 and 4).

## Acceptance Criteria Overview

1. `--fail-on <severity>` excludes findings with `Verification.Verdict == "refuted"` from the count, regardless of their v1 confidence or severity.
2. `--require-verified` (bool flag, default false) restricts the gate to count only findings with `confidence == "VERIFIED"` at or above the threshold. When combined with `--fail-on high`, only VERIFIED findings at HIGH or above trigger failure.
3. The MCP handler `failingFindings` at `internal/mcp/handlers.go:339` applies the same filtering logic as the CLI path.
4. Fixture matrix tests validate: confirmed/refuted/unverifiable findings × HIGH/MEDIUM/LOW severity × `--fail-on` threshold × `--require-verified` on/off, covering at least 12 distinct scenarios.
5. Existing gate behavior is preserved when `--require-verified` is not set: `--fail-on` counts all non-refuted findings at or above the threshold (including unverified and VERIFIED).

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/3.0_adversarial_verification/`_

## Technical Considerations

- **Implementation Notes:**
  - **CLI flag (`internal/cmd/review.go` or `internal/cmd/gate.go`):** Add `--require-verified` (bool, default false) to the review/reconcile/verify commands. Pass it through to the gate evaluation function. The flag is mutually compatible with `--fail-on` but has no effect without it (or with any gate evaluation).
  - **Gate logic update (`internal/reconcile/gate.go`):** Modify `CountAtOrAbove(findings []Merged, threshold string, requireVerified bool) int` (or equivalent) to: (1) filter out findings where `finding.Verification != nil && finding.Verification.Verdict == "refuted"`, (2) if `requireVerified` is true, further filter to only findings with `finding.Confidence == "VERIFIED"`, (3) count findings at or above `threshold` (using the existing severity ordering: VERIFIED > HIGH > MEDIUM > LOW). The function signature may need to change to accept `requireVerified`; alternatively, a wrapper function `CountFailing(findings, threshold, requireVerified)` can be added.
  - **MCP path (`internal/mcp/handlers.go`):** Update `failingFindings` at line 339 to apply the same filtering. The MCP tool `atcr_review` (or equivalent) should accept `requireVerified` as a parameter and pass it to the gate logic.
  - **Fixture matrix tests (`internal/reconcile/gate_test.go` or `internal/verify/gate_test.go`):** Build a test matrix with findings at each combination of verdict (confirmed/refuted/unverifiable) × severity (HIGH/MEDIUM/LOW). For each combination, assert the gate count with `--fail-on high`, `--fail-on medium`, `--fail-on low`, and each with `--require-verified` on/off. The matrix should have >= 12 distinct test cases.
  - **Backward compatibility:** Without `--require-verified`, the gate behavior is: count all non-refuted findings at or above threshold. This is a change from the pre-Epic 3.0 behavior (which counted all findings), but it is the intended semantic: refuted findings should never block merges. Document this change in release notes.
- **Integration Points:**
  - `internal/reconcile/gate.go` — `CountAtOrAbove` (line 57): gate counter updated to exclude refuted and optionally require VERIFIED.
  - `internal/mcp/handlers.go` — `failingFindings` (line 339): MCP gate path updated to match CLI semantics.
  - `internal/cmd/review.go` or `internal/cmd/gate.go` — CLI flag `--require-verified` added.
  - `reconciled/findings.json` — input to gate logic; contains `verification` blocks with verdicts.
- **Data Requirements:**
  - No new schema changes. The `Verification` struct and `verdict` field are already defined and populated by Story 3.
  - `--require-verified` is a runtime flag, not a persistent configuration.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `--require-verified` has no effect without `--fail-on` | Low — user confusion | Document that `--require-verified` requires `--fail-on` to have an effect. Alternatively, make it an error to set `--require-verified` without `--fail-on`. |
| Gate logic excludes refuted findings but still counts LOW-confidence non-refuted findings | Medium — unexpected gate failures | This is correct behavior: a naturally-LOW finding (e.g., single reviewer, low severity) should still count if it meets the threshold. The change is that refuted findings (which are demoted to LOW) are excluded. Document this distinction. |
| Fixture matrix tests are incomplete (e.g., miss edge cases like empty verdict) | Medium — gate logic has untested paths | The matrix should include a test case for findings with no `Verification` block (v1-only findings). These should be counted as non-refuted and non-VERIFIED. |
| MCP path and CLI path diverge in gate semantics | High — inconsistent behavior | Both paths call the same gate logic function. Unit tests should verify both paths with the same fixture. |
| `--require-verified` causes all gates to pass (no findings are VERIFIED) | Low — false sense of security | This is correct behavior: if no findings have been verified by a skeptic, the strictest gate should not block merges. Document this semantic. |

---

**Created:** June 14, 2026 09:06:20AM
**Status:** Draft - Awaiting Acceptance Criteria
