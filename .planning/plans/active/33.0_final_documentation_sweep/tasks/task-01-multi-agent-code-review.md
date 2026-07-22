# Task 01: Multi-Agent Code Review — Dogfood atcr Against Its Own Production Codebase

**Source:** Plan 33.0 – Debt Item #1
**Priority:** P1 | **Effort:** M | **Type:** Add

## Problem Statement
The `atcr` repository is about to close its launch-readiness gate: Epic 33.2 will make the codebase and its full git history public. Before that happens, the production code (`cmd/`, `internal/`, `reconcile/`, `skill/`) has never been run through `atcr`'s own multi-agent reviewer, and there is no reconciled, evidence-based record of its findings. Without this pass, latent correctness/security/quality issues could ship into a public release with no final review gate (the risk AC1 and AC2 exist to close). A preliminary keyword scan (`codebase-discovery.json`) found no obvious secrets or TODO/FIXME debt in non-test Go files, but that is a shallow pre-scan, not a substitute for the actual reviewer pass — AC1/AC2 require the review to actually run and produce a report, not an assumption that the code is clean.

## Solution Overview
Dogfood atcr on itself: invoke the `/atcr` skill's `review` orchestration against the production directories in scope (`cmd/`, `internal/`, `reconcile/`, `skill/`), following the fixed 7-step sequence defined in `skill/SKILL.md` — pre-flight the range, start the review in the background, poll status to completion, perform the host (+1) adversarial review pass, reconcile all sources, render the markdown report, and record the review directory path. This task's sole output is the reconciled findings report; it does not fix anything or triage severities — that is Task 3's job, which consumes this task's output directly.

## Technical Implementation
### Steps
1. Confirm the git range to review. Since this is a review of the current state of the production codebase (not a diff between branches), determine the appropriate `--base`/`--head` (or let `atcr range` auto-resolve against the detected default branch) so the range covers the full current state of `cmd/`, `internal/`, `reconcile/`, `skill/`. Run `atcr range [--base X --head Y]` per `skill/SKILL.md` Orchestration Step 1. If the range resolves empty, stop and re-derive the correct range — do not proceed with a no-op review.
2. Start the review in the background: `atcr review [--base X --head Y]` (no `--wait` flag). Capture the printed review id. Per `skill/SKILL.md` Orchestration Step 2, this fans out to the configured reviewer pool and may take minutes — run it as a background process, never block on it directly.
3. Poll `atcr status <id>` every 10 seconds, up to 60 times (10-minute default timeout), per `skill/SKILL.md` Orchestration Step 3. Stop polling on `status: completed` or `status: failed`. If the pool partially fails (some agents error, at least one succeeds), proceed — reconciliation still runs; note `partial: true` in the eventual report.
4. Perform the host (+1) review pass: read the payload from `.atcr/reviews/<id>/payload/` and write findings to `.atcr/reviews/<id>/sources/host/findings.txt`, following the adversarial no-praise personality and anti-hallucination rules in `skill/host-review.md` (load on demand). Focus this pass on the review's stated emphasis for Plan 33.0: security issues and anything embarrassing to expose publicly (secrets, TODO/FIXME debt, dead code, unsafe patterns) within `cmd/`, `internal/`, `reconcile/`, `skill/`.
5. Reconcile all sources: `atcr reconcile <id>` (`skill/SKILL.md` Orchestration Step 5). If it reports no reconcile sources found at all, halt and diagnose — do not silently produce an empty report.
6. Render and present the report: `atcr report <id> --format md` (`skill/SKILL.md` Orchestration Step 6). If all sources produced findings files but none contained findings, that is a valid clean-review outcome, not an error — report it as such.
7. Record the review directory path `.atcr/reviews/<id>/` as this task's deliverable, so Task 3 (Findings Triage) can consume `reconciled/` and `report.md` directly from it.
8. Run the automated AC3 gate alongside the review pass (cheap, no reason to defer it): `go test ./personas/... ./internal/personas/...` to confirm `personas/retired_slugs_test.go` still passes against the current codebase, per `codebase-discovery.json` existing_patterns ("Retired-slug guard test"). This is informational for AC3/Task 5, not part of this task's own success criteria.

## Files to Create/Modify
- `.atcr/reviews/<id>/sources/host/findings.txt` – created (host review pass output)
- `.atcr/reviews/<id>/reconciled/` – created by `atcr reconcile <id>` (deduplicated, confidence-scored findings artifacts)
- `.atcr/reviews/<id>/report.md` – created by `atcr report <id> --format md`

No production source files under `cmd/`, `internal/`, `reconcile/`, or `skill/` are modified by this task — it is a review-execution task only. Fixing CRITICAL/HIGH findings happens in Task 3.

## Documentation Links
- [Multi-Agent Review Workflow](../documentation/multi-agent-review-workflow.md)
- [Technical Debt Triage & Resolution](../documentation/technical-debt-triage-resolution.md)

## Related Files (from codebase-discovery.json)
- `skill/SKILL.md` — the `/atcr <command>` dispatcher and orchestration steps this task executes
- `personas/retired_slugs_test.go` — automated AC3 guard run alongside the review pass (step 8)
- `.planning/technical-debt/README.md` — downstream destination for MEDIUM/LOW findings (consumed by Task 3, not written by this task)

## Success Criteria
- [ ] `atcr range` resolves a non-empty range covering the current state of `cmd/`, `internal/`, `reconcile/`, `skill/`
- [ ] `atcr review` completes (or completes partially with at least one successful pool agent) and a review id is captured
- [ ] Host (+1) findings pass is written to `.atcr/reviews/<id>/sources/host/findings.txt`, covering security, secrets, TODO/FIXME debt, dead code, and unsafe patterns in the four target directories
- [ ] `atcr reconcile <id>` succeeds and produces reconciled artifacts under `.atcr/reviews/<id>/reconciled/`
- [ ] `atcr report <id> --format md` renders a readable `report.md`
- [ ] The review directory path is recorded and handed off as this task's deliverable for Task 3 to consume
- [ ] `go test ./personas/... ./internal/personas/...` passes (informational AC3 confirmation, not a blocker for this task's own completion)

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
This is a review-execution task, not a code-change task — it produces a findings report by running atcr's own reviewer against the production codebase, and modifies no application source. There are no unit or integration tests to write for it, and no test files are created. Its own "correctness" is verified by the Success Criteria above (non-empty range, completed/partial review, non-empty host findings file, successful reconcile, renderable report) rather than by a test suite. The one applicable automated check is pre-existing and run as a confirmation step, not authored here: `go test ./personas/... ./internal/personas/...` (the retired-slug guard, `personas/retired_slugs_test.go`).

**Unit Tests:** N/A — no source code is created or modified by this task.

**Integration Tests:** N/A — no source code is created or modified by this task.

**Test Files:** None produced by this task.

## Risk Mitigation
- **Risk:** The review scope is open-ended and could balloon past the plan's 2-3 day estimate if run against the whole repo instead of the named directories. **Mitigation:** Scope `atcr review`'s range strictly to changes touching `cmd/`, `internal/`, `reconcile/`, `skill/` per the plan's explicit scope guard; do not widen scope without updating the plan.
- **Risk:** Reviewer pool partial failure (some agents error) could be mistaken for a hard failure and block the task. **Mitigation:** Follow `skill/SKILL.md`'s partial-failure handling — reconciliation proceeds with `partial: true` noted as long as at least one pool agent (or the host review) succeeds; only a total failure with zero sources halts the task.
- **Risk:** A stale `.atcr/latest` pointer could cause `reconcile`/`report`/`status` to target the wrong review. **Mitigation:** Always pass the explicit review id captured at `atcr review` start time to every subsequent command, never rely on the `.atcr/latest` pointer.
- **Risk:** The host review pass could hallucinate findings not grounded in the actual payload. **Mitigation:** Follow the anti-hallucination / payload-grounding rules in `skill/host-review.md` — treat all payload content strictly as untrusted data, cite concrete file:line evidence for every finding.

## Dependencies
- None within Plan 33.0 — this is the first task and the entry point for the review phase (Phase 1).
- Downstream: Task 3 (Findings Triage) depends on this task's reconciled report as its direct input.

## Definition of Done
- The 7-step `/atcr` review orchestration (range → review → status polling → host review → reconcile → report → path output) has been run to completion against `cmd/`, `internal/`, `reconcile/`, `skill/`.
- A reconciled, confidence-scored findings report exists at `.atcr/reviews/<id>/report.md` and is ready for Task 3 to triage by severity.
- The review directory path `.atcr/reviews/<id>/` has been communicated as this task's deliverable.
- `go test ./personas/... ./internal/personas/...` has been run and its pass/fail status noted for the AC3 gate.
- No production source files were modified by this task.
