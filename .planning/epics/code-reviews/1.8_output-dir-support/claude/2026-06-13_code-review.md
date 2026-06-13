# Code Review Report: 1.8_output-dir-support

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 7 / 7
- **Approval Status:** Approved
- **Review Date:** June 13, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests
- **Merge Commit:** 615d474

All seven success criteria verify against the merged implementation. The full Go test suite passes, coverage is 87.3% (above the 80% baseline), and lint/vet/fmt are clean. The `--output-dir` core (`cmd/atcr/review.go`, `internal/fanout/review.go`, `internal/fanout/reviewdir.go`) is functionally correct. Adversarial review surfaced 10 findings — all hardening / maintainability, none on the happy path, and none blocking the epic's own ACs.

## 2. Checklist Changes Applied
- **.planning/epics/completed/1.8_output-dir-support.md** – Success Criteria (7 items)
  - Before: `[ ]` → After: `[x]` (all 7)
  - Evidence: see Evidence Map below

## 3. Evidence Map
- **`atcr review --output-dir` creates the full review tree**
  - Evidence: `internal/fanout/reviewdir.go:226-247`, `internal/fanout/review.go:175-197`, `internal/fanout/review_test.go:158-176`
  - `ScaffoldOutputDir` creates the path + `payload/sources/reconciled`; `PrepareReview` dispatches to it then writes payload artifacts + manifest.
- **`.atcr/latest` is NOT updated with `--output-dir`**
  - Evidence: `internal/fanout/review.go:217-225`, `internal/fanout/review_test.go:178-180`
  - `WriteLatest` guarded by `if req.OutputDir == ""`; test asserts `ReadLatest` errors.
- **Non-empty existing dir fails with exit 2 + actionable message**
  - Evidence: `internal/fanout/reviewdir.go:232-237`, `cmd/atcr/review.go:126-129`, `internal/fanout/reviewdir_test.go:36-55`
  - `os.ReadDir` non-empty → actionable error; propagates via `usageError` → exit 2.
- **`--output-dir` and `--id` together fail with exit 2 (mutually exclusive)**
  - Evidence: `cmd/atcr/review.go:45-61`, `cmd/atcr/review_test.go:122-129`
- **`atcr reconcile` operates on an `--output-dir` review directory**
  - Evidence: `cmd/atcr/reconcile_test.go:64-78` (`TestReconcileCmd_InheritsExternalOutputDir`); reconcile consumes the path via its `[id-or-path]` positional.
- **Existing behavior (no `--output-dir`) unchanged**
  - Evidence: `internal/fanout/review.go:180-225`, `internal/fanout/review_test.go:107-152`, `internal/fanout/reviewdir_test.go:128-212`
- **Unit tests cover flag parsing, path validation, latest-skip**
  - Evidence: `cmd/atcr/review_test.go:114-149`, `internal/fanout/reviewdir_test.go:15-55`, `internal/fanout/review_test.go:155-186`

## 4. Remaining Unchecked Items
No remaining unchecked items — all 7 criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All success criteria implemented with cited evidence and dedicated tests; quality gates green. Adversarial findings are non-blocking hardening/maintenance items routed to technical debt.

## 6. Coverage Analysis
- **Coverage:** 87.3%
- **Baseline:** 80%
- **Delta:** ↑7.3%
- **Status:** PASSING
- Per-package: cmd/atcr 82.4%, internal/fanout 84.1% (both core epic packages above baseline)

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 7
- **Issues Found:** 10 (Critical: 0, High: 1, Medium: 3, Low: 6)
- **Mode:** Discovery-only (epic has no sprint-design.md risk profile)

### Issues by Severity

**HIGH (1)**
- `internal/payload/budget.go:104` (error-handling) — `Truncation.AllDropped` is producer-only: set + tested, but no production code reads it. `buildPayloads` (review.go:318-325) never checks it, so a too-small `--byte-budget` that sheds all files silently forwards an empty payload → zero findings. Re-verified by grep. Gated behind an explicit small byte budget.

**MEDIUM (3)**
- `internal/fanout/reviewdir.go:226` (security) — `ScaffoldOutputDir` has no containment/trust boundary; `--output-dir` (unlike `--id`) accepts any absolute path, so the review tree (incl. payload diffs) can be written anywhere writable. Undocumented trust boundary; relevant if driven from a less-trusted MCP/automation surface.
- `internal/fanout/reviewdir.go:232` (error-handling) — TOCTOU + symlink: `os.ReadDir` follows symlinks so a symlink-to-empty-dir bypasses the non-empty guard; check/use window before `MkdirAll`.
- `internal/payload/budget.go:20` (maintainability) — scope creep: the merge bundled unrelated TD/doc fixes (budget.go, outcome.go, ambiguous.go, registry/project.go) into a feature PR; `AllDropped` half-landed (producer only).

**LOW (6)**
- `internal/fanout/reviewdir.go:202` (maintainability) — scaffolder divergence: `ScaffoldReviewDir` is exclusive-create, `ScaffoldOutputDir` allows empty-existing and skips atomic claim → concurrent same-path runs can interleave.
- `cmd/atcr/review.go:54` (error-handling) — emptiness check trims but `filepath.Abs` gets the untrimmed value; leading-space input yields a path with space components.
- `internal/fanout/review.go:175` (error-handling) — `--output-dir` into an empty `.atcr/reviews/<id>/` is accepted but invisible to `status` (latest skipped) — confusing half-state.
- `internal/fanout/review.go:169` (maintainability) — id derived-but-unused in the OutputDir arm; three-way switch couples path + id strategy.
- `internal/fanout/reviewdir_test.go:36` (testing) — tests omit symlink-target and escaping-path cases, locking in permissive behavior.
- `internal/reconcile/ambiguous.go:165` (error-handling) — `AmbiguousHash` now panics on render error (was `""`); defensible + currently unreachable, but the panic path has no test.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/1.8_output-dir-support.md` to merge these findings into the technical-debt README.
- Prioritize the HIGH `AllDropped` wiring gap and the two MEDIUM `--output-dir` hardening items (containment, symlink/TOCTOU) when triaging TD.
- Process note: keep unrelated TD/doc fixes out of feature PRs for clean review/revert isolation.

---
*Generated by /execute-code-review on June 13, 2026 04:21:36AM*
