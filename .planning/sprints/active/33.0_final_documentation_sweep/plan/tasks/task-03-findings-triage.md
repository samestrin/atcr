# Task 03: Findings Triage — Classify, Fix CRITICAL/HIGH, Route MEDIUM/LOW to Technical Debt

**Source:** Plan 33.0 – Debt Item #3
**Priority:** P1 | **Effort:** L | **Type:** Fix

## Problem Statement
Task 1 (atcr's dogfooded multi-agent reviewer over `cmd/`, `internal/`, `reconcile/`, `skill/`) and Task 2 (manual adversarial pass for secrets, dead code, unsafe patterns, TODO/FIXME debt) produce raw, unclassified findings streams. Per AC1 ("A comprehensive code review ... has been run over the production codebase; all CRITICAL/HIGH findings are fixed and MEDIUM/LOW are captured as technical debt"), nothing is done until every finding is (a) triaged to a verified severity, (b) fixed in the codebase if CRITICAL/HIGH, or (c) captured in a handoff artifact for Task 7 to shard into `.planning/technical-debt/README.md` if MEDIUM/LOW. Without this step, findings from Tasks 1–2 remain inert reports with no closure — and the documentation sweep (Tasks 4–8) cannot proceed, since it must describe the finalized, hardened codebase per the plan's architecture note that Phase 1 (review) must precede Phase 2 (docs).

## Solution Overview
Merge the two findings streams from Task 1 (`atcr reconcile` / `atcr report <id> --format md` reconciled output, 9-column `atcr-findings/v1` format) and Task 2 (manual adversarial findings, recorded in the same pipe-delimited format for consistency). Re-verify each finding's severity against actual impact rather than trusting the reviewer's raw label. For every CRITICAL/HIGH finding, apply the RED → GREEN → ADVERSARIAL → REFACTOR discipline from `skill/debt-resolve/SKILL.md` (pre-fix evaluation, failing repro/test, minimal fix, non-overridable adversarial self-check for test-only changes/weakened assertions/lint suppressions/stubbed bodies, then cleanup) directly against the production code in `cmd/`, `internal/`, `reconcile/`, `skill/`. For every MEDIUM/LOW finding, do not fix inline — write it into a structured triage handoff artifact so Task 7 can shard it into `.planning/technical-debt/README.md` using the project's existing severity/group/status format. Produce a triage summary as evidence that AC1 is closed.

## Technical Implementation
### Steps
1. Collect Task 1's output (the reconciled `atcr-findings/v1` 9-column stream produced by `atcr reconcile <id>` / rendered via `atcr report <id> --format md`, per `documentation/multi-agent-review-workflow.md`) and Task 2's output (manual adversarial findings at `.planning/plans/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt`, written in the same reconciled 9-column `atcr-findings/v1` format with `REVIEWERS=adversarial-pass`/`CONFIDENCE=HIGH` per Task 2 Step 7 — so both streams share one column shape and merge by direct concatenation, no format conversion needed). Merge both into one combined findings list, deduplicating any finding both passes independently caught (same `FILE:LINE` ±3 lines and same `PROBLEM` theme).
2. For each finding, verify/re-classify severity against actual impact rather than accepting the reviewer's raw label at face value: CRITICAL = exploitable security flaw, secret/credential exposure, or data-loss/correctness bug; HIGH = correctness or security bug with narrower blast radius; MEDIUM/LOW = style, minor performance, or non-blocking issues. Record any re-classification and its reasoning in the triage summary (Step 6).
3. For every CRITICAL/HIGH finding, run the per-item resolution cycle documented in `documentation/technical-debt-triage-resolution.md` and `skill/debt-resolve/SKILL.md`: (0) pre-fix evaluation — confirm the finding still exists and has a clear, safely-scoped fix; (1) RED — write or confirm a failing test (or concrete reproduction) demonstrating the defect in the relevant co-located `*_test.go` file; (2) GREEN — apply the minimal fix in the cited file under `cmd/`, `internal/`, `reconcile/`, or `skill/`; (3) ADVERSARIAL — self-check the diff for test-only changes, weakened/deleted assertions, lint/type suppressions, or stubbed/empty bodies (non-overridable — if flagged, do not mark resolved, escalate for manual review instead); (4) REFACTOR — clean up names/dead scaffolding, re-run the test to confirm still green.
4. Apply `.planning/specifications/coding-standards.md` to every fix: Go naming conventions, `error` as last return value, no ignored errors, `fmt.Errorf("doing action: %w", err)` wrapping. After each fix (or small batch), run `golangci-lint run` and `go vet ./...` to confirm no new lint/vet violations were introduced.
5. For every MEDIUM/LOW finding, write it to `.planning/plans/active/33.0_final_documentation_sweep/code-review/triaged-findings-medium-low.md` using the 9-column reconciled `atcr-findings/v1` format (`SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWERS|CONFIDENCE`) plus a suggested `GROUP` label per finding (e.g. by directory/category) — this is the handoff artifact Task 7 consumes to shard entries into `.planning/technical-debt/README.md`. Do not write directly into the TD README in this task; that write is Task 7's responsibility.
6. Write `.planning/plans/active/33.0_final_documentation_sweep/code-review/triage-summary.md` documenting: total findings ingested, counts by final severity, any severity re-classifications with reasoning, the full list of CRITICAL/HIGH findings with the file/commit that fixed each, and the count/location of MEDIUM/LOW findings routed to the Step 5 artifact — this is the evidence trail closing AC1.
7. Run the full gate — `go test ./...`, `golangci-lint run`, `go vet ./...` — across the repository as the final check that all CRITICAL/HIGH fixes are complete and introduce no regressions.

## Files to Create/Modify
- `cmd/**`, `internal/**`, `reconcile/**`, `skill/**` – fix files cited by CRITICAL/HIGH findings (exact paths determined by Task 1/Task 2 output; not enumerable ahead of the review run)
- co-located `*_test.go` files for any CRITICAL/HIGH fix requiring a RED reproduction test
- `.planning/plans/active/33.0_final_documentation_sweep/code-review/triaged-findings-medium-low.md` – create (MEDIUM/LOW handoff artifact for Task 7)
- `.planning/plans/active/33.0_final_documentation_sweep/code-review/triage-summary.md` – create (triage evidence trail for AC1)

## Documentation Links
- [Technical Debt Triage & Resolution](../documentation/technical-debt-triage-resolution.md)
- [Multi-Agent Review Workflow](../documentation/multi-agent-review-workflow.md)

## Related Files (from codebase-discovery.json)
- `.planning/technical-debt/README.md`
- `personas/retired_slugs_test.go`

## Success Criteria
- [ ] All findings from Task 1 (multi-agent review) and Task 2 (adversarial pass) are merged, deduplicated, and classified by severity, with reviewer-assigned severity independently re-verified against actual impact.
- [ ] Every CRITICAL and HIGH finding is fixed directly in the codebase (`cmd/`, `internal/`, `reconcile/`, `skill/`) following the RED → GREEN → ADVERSARIAL → REFACTOR cycle, with no item left NEEDS_REVIEW-flagged and unresolved.
- [ ] Every MEDIUM and LOW finding is recorded in `code-review/triaged-findings-medium-low.md` with file/line, problem, fix, category, severity, and confidence — ready for Task 7 to shard into the TD README.
- [ ] `go test ./...`, `golangci-lint run`, and `go vet ./...` all pass cleanly after all CRITICAL/HIGH fixes are applied.
- [ ] `code-review/triage-summary.md` documents the full findings count, severity breakdown, and fix evidence, satisfying AC1's "all CRITICAL/HIGH findings are fixed and MEDIUM/LOW are captured as technical debt" clause.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- For each CRITICAL/HIGH fix that changes behavior, a RED test added to (or an existing test extended in) the co-located `*_test.go` file, reproducing the defect before the fix and passing (GREEN) after.

**Integration Tests:**
- Full-suite `go test ./...` run after all CRITICAL/HIGH fixes are applied, confirming no regressions were introduced into adjacent code across `cmd/`, `internal/`, `reconcile/`, `skill/`.

**Test Files:**
- Dynamic — co-located `*_test.go` files matching whichever production files receive CRITICAL/HIGH fixes (e.g. `internal/<package>/<file>_test.go`), determined by Task 1/Task 2 findings.

## Risk Mitigation
- **Risk:** Reviewer-assigned severity may be miscalibrated (over- or under-stated). **Mitigation:** Step 2 requires independent judgment against actual impact before fixing or routing, per the triage discipline in `documentation/technical-debt-triage-resolution.md`; re-classifications are logged with reasoning.
- **Risk:** Fixing CRITICAL/HIGH findings could introduce regressions in adjacent code. **Mitigation:** Per-item RED → GREEN → ADVERSARIAL → REFACTOR cycle plus a final full-suite `go test ./...` gate (Step 7) before considering the task done.
- **Risk:** Scope creep — fixing MEDIUM/LOW findings inline instead of routing to TD, ballooning the plan's 2-3 day estimate. **Mitigation:** Strict CRITICAL/HIGH-only fix bar per `plan.md` Risk Mitigation; all MEDIUM/LOW findings route to the Step 5 artifact for Task 7, never fixed in this task.
- **Risk:** A CRITICAL/HIGH fix touches security-sensitive code (secrets, auth, path handling). **Mitigation:** The non-overridable ADVERSARIAL stage (Step 3) explicitly flags stubbed bodies, weakened assertions, or lint suppressions — any such fix is escalated for manual review rather than auto-marked resolved.

## Dependencies
- Task-01 (Multi-agent code review) — provides findings input
- Task-02 (Adversarial/security pass) — provides findings input

## Definition of Done
- All findings from Tasks 1–2 classified by verified severity.
- Every CRITICAL/HIGH finding fixed in the codebase and passing its RED test, the ADVERSARIAL gate, and the full test/lint/vet suite.
- Every MEDIUM/LOW finding captured in `code-review/triaged-findings-medium-low.md`, ready for Task 7.
- `code-review/triage-summary.md` written as AC1 evidence.
- `go test ./...`, `golangci-lint run`, `go vet ./...` all pass with zero new failures.
