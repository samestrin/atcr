# Task 08: Final Verification Pass — Re-run Automated Guards End-to-End (Plan Definition-of-Done Gate)

**Source:** Plan 33.0 – Debt Item #8
**Priority:** P1 | **Effort:** S | **Type:** Fix

## Problem Statement
Tasks 1–7 of this plan run a multi-agent code review, a manual adversarial/security pass, findings triage (CRITICAL/HIGH fixes in the codebase), a code-to-docs accuracy audit, persona reference cleanup, a website-compatibility check, and technical-debt capture — each touching different files across `cmd/`, `internal/`, `reconcile/`, `skill/`, `personas/`, `docs/`, and `README.md`. No single one of those tasks re-validates the codebase as a whole after all the others have landed their changes. Without a closing regression pass, a fix in one task (e.g., a CRITICAL finding remediation in Task 3) could silently break a guard another task depends on (e.g., `TestNoRetiredSlugs` in Task 5, or `go vet`/lint cleanliness expected by the coding standards referenced in `plan.md`'s Technical Planning Notes), and the plan could be marked complete with a false sense of safety. This task is the plan's Definition-of-Done gate: it must confirm, with fresh command output — not by trusting each task's individual self-report — that the full automated guard suite passes against the final state of the repository, and that all 5 acceptance criteria (AC1–AC5) hold.

## Solution Overview
Re-run every automated guard used across this plan, end-to-end, against the final repository state produced by Tasks 1–7: the persona retired-slug test suite (AC3's authoritative gate), `go vet ./...`, `golangci-lint run` (the repo's actual pre-push/CI lint invocation), and the full `go test ./...` regression suite (plus the `reconcile` submodule's own `go test ./...`, since it is a separate Go module not covered by the root `go test ./...` per `.githooks/pre-push`). This task does not introduce new code changes or re-run the multi-agent reviewer — it is a verification-only closing gate. If any guard fails, the failure is itself a new finding: capture it precisely (command, output, file:line) and report it as a blocking regression rather than attempting an ad-hoc fix or silently reclassifying scope. After the guards pass, do a final manual walkthrough confirming each of the 5 plan-level acceptance criteria (AC1–AC5) holds against the current state of the repo and `.planning/technical-debt/README.md`.

## Technical Implementation
### Steps
1. Confirm the working tree reflects the final state of Tasks 1–7 (no uncommitted changes pending from earlier tasks that would make this a partial verification) before running any guard.
2. Run the persona/AC3 guard: `go test ./personas/... ./internal/personas/...` — confirm `TestNoRetiredSlugs` (`personas/retired_slugs_test.go`) and the related coverage in `internal/personas/community_schema_test.go` and `internal/personas/list_test.go` all pass with zero failures.
3. Run `go vet ./...` from the repo root — confirm zero suspicious-construct/type-issue reports, matching the fast gate already enforced in `.githooks/pre-commit`.
4. Run `golangci-lint run` from the repo root — confirm zero lint findings, matching the gate enforced in `.githooks/pre-push` and `.github/workflows/ci.yml`. If `golangci-lint` is not installed locally, install it or fall back to the CI-equivalent invocation rather than skipping the check silently.
5. Run the full root-module regression suite: `go test -race ./...` — matching the exact invocation in `.githooks/pre-push` (race-enabled, matches CI's push-time job in `.github/workflows/ci.yml`). Confirm zero failing packages across all directories touched by Tasks 1–7 (`cmd/`, `internal/`, `reconcile/`, `skill/`, `personas/`, plus any others).
6. Run the `reconcile` submodule's own test suite separately: `(cd reconcile && go test ./...)` — this module has its own `go.mod` and is NOT covered by the root `go test ./...`, per `.githooks/pre-push` and the `reconcile-module.yml` CI workflow. Confirm zero failures.
7. If every guard in steps 2–6 passes, proceed to the AC verification pass. If any guard fails, stop, record the exact command, failing package/test name, and error output as a blocking finding, and report FAIL status for this task rather than attempting to fix the regression inline (fixing belongs to a follow-up cycle through Task 3's triage discipline, not this closing gate).
8. Perform the final AC1–AC5 walkthrough against the current repo state:
   - AC1: Confirm Task 1's multi-agent review and Task 2's adversarial pass ran, Task 3's triage fixed all CRITICAL/HIGH findings (verify via the task's own success-criteria checklist and/or a fresh look at the findings list), and remaining MEDIUM/LOW items are present in `.planning/technical-debt/README.md` (Task 7).
   - AC2: Confirm no secrets/credentials/embarrassing artifacts remain — spot-check Task 2's adversarial-pass output for this specific claim rather than re-running a full secrets scan from scratch.
   - AC3: Satisfied by step 2's passing `TestNoRetiredSlugs` guard plus Task 5's prose sweep record.
   - AC4: Confirm Task 4's code-to-docs audit record shows all features up to Epic 23.0 covered.
   - AC5: Confirm Task 6's website-compatibility check record shows all `docs/` markdown files validated for `atcr.dev` import.
9. Write a brief pass/fail summary of steps 2–8 (guard-by-guard status plus AC-by-AC status) so the plan's completion record has verifiable evidence rather than an unqualified "done."

## Files to Create/Modify
- None — this is a verification-only task. No source or documentation files are created or modified by this task itself (any regression found in step 7 is reported as a finding for a separate fix cycle, not patched here).

## Documentation Links
- [Technical Debt Triage & Resolution](../documentation/technical-debt-triage-resolution.md)
- [Multi-Agent Review Workflow](../documentation/multi-agent-review-workflow.md)
- [Persona Naming & Documentation Accuracy](../documentation/persona-naming-doc-accuracy.md)

## Related Files (from codebase-discovery.json)
- `personas/retired_slugs_test.go` — authoritative automated AC3 guard (`TestNoRetiredSlugs`)
- `internal/personas/community_schema_test.go` — related retired-slug coverage
- `internal/personas/list_test.go` — related retired-slug coverage
- `.planning/technical-debt/README.md` — destination for MEDIUM/LOW findings (AC1), checked in the final AC walkthrough

## Success Criteria
- [ ] `go test ./personas/... ./internal/personas/...` passes with zero failures (AC3 automated gate)
- [ ] `go vet ./...` reports zero issues
- [ ] `golangci-lint run` reports zero findings
- [ ] `go test -race ./...` passes with zero failing packages across the full repo
- [ ] `(cd reconcile && go test ./...)` passes with zero failures
- [ ] AC1–AC5 each explicitly confirmed against the final repo/docs/TD-README state, with the evidence source for each (which prior task's record it was verified against)
- [ ] A pass/fail summary of all guards and all 5 ACs is recorded (in the task's completion notes or equivalent), giving the plan a verifiable, non-assumed Definition-of-Done

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `go test ./personas/... ./internal/personas/...` (AC3 guard)

**Integration Tests:**
- `go test -race ./...` (full root-module regression suite, matches `.githooks/pre-push` and CI push-time job)
- `(cd reconcile && go test ./...)` (separate Go module, not covered by root `go test ./...`)
- `go vet ./...` and `golangci-lint run` (lint/vet gates, matching `.githooks/pre-commit` and `.githooks/pre-push`)

**Test Files:**
- `personas/retired_slugs_test.go`
- `internal/personas/community_schema_test.go`
- `internal/personas/list_test.go`

## Risk Mitigation
- **Risk:** Treating each prior task's self-reported "tests passed" as sufficient without a fresh end-to-end run could miss cross-task regressions (e.g., Task 3's CRITICAL/HIGH fix in one package breaking a test in another package Task 3 never touched directly). **Mitigation:** This task always executes the guards fresh against the final combined state — it never substitutes a prior task's individual report for its own run.
- **Risk:** `golangci-lint` may not be installed in the local environment, tempting a skip. **Mitigation:** Step 4 explicitly requires installing it or using the CI-equivalent invocation rather than silently omitting the lint gate — a skipped lint gate is a false "pass," not a real one.
- **Risk:** The `reconcile/` submodule has its own `go.mod` and is invisible to root `go test ./...`, so a verification pass that only runs the root command would silently miss regressions in that module. **Mitigation:** Step 6 explicitly runs `(cd reconcile && go test ./...)` as a separate command, matching `.githooks/pre-push` and the `reconcile-module.yml` CI workflow.
- **Risk:** If a guard fails at this final stage, there is a temptation to patch it inline to "just get it green," bypassing the triage discipline (severity classification, root-cause fix vs. workaround) that Task 3 applied to every other finding in this plan. **Mitigation:** Step 7 explicitly stops and reports FAIL with full evidence rather than attempting an inline fix — any regression found here re-enters the triage process rather than getting a shortcut.

## Dependencies
- Task-01 through Task-07 — all must be complete before this final gate runs; this task consumes their combined output as its input and produces no new findings of its own scope beyond confirming the guards and ACs

## Definition of Done
- All automated guards (persona/AC3 test suite, `go vet`, `golangci-lint`, full `go test -race ./...`, and the `reconcile` submodule's `go test ./...`) have been re-run fresh against the final repository state and all pass with zero failures.
- Any guard failure discovered during this pass has been recorded as an explicit blocking finding (command, output, location) rather than silently fixed or ignored.
- AC1 (code review executed, CRITICAL/HIGH fixed, MEDIUM/LOW captured as TD), AC2 (no secrets/embarrassing artifacts), AC3 (no legacy persona names), AC4 (features up to Epic 23.0 documented), and AC5 (`docs/` validated for `atcr.dev` import) have each been explicitly confirmed against the final state, with the specific evidence source cited for each.
- A guard-by-guard and AC-by-AC pass/fail summary exists as the plan's closing verification record.
