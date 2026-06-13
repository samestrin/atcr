
### Criterion: `atcr review --output-dir /tmp/test-review` creates the full review tree (manifest.json, payload/, sources/)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/reviewdir.go:226-247`, `internal/fanout/review.go:175-197`, `internal/fanout/review_test.go:158-176`
- **Notes:** `ScaffoldOutputDir` creates the path (MkdirAll, parents included) plus the `payload/sources/reconciled` trio; `PrepareReview` dispatches to it when `req.OutputDir != ""`, then `writePayloadArtifacts` + `WriteManifest` populate the tree. `TestScaffoldOutputDir_CreatesTreeWhenAbsent` and `TestRunReview_OutputDirSkipsLatest` (asserts `res.Dir == out`) confirm.

### Criterion: `.atcr/latest` is NOT updated when `--output-dir` is used
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/review.go:217-225`, `internal/fanout/review_test.go:178-180`
- **Notes:** `WriteLatest` is guarded by `if req.OutputDir == ""`. `TestRunReview_OutputDirSkipsLatest` asserts `ReadLatest` errors (no pointer written) under the repo root after an output-dir run.

### Criterion: `--output-dir` with a non-empty existing directory fails with exit 2 and an actionable message
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/reviewdir.go:232-237`, `internal/fanout/review.go:126-129`, `internal/fanout/reviewdir_test.go:36-55`
- **Notes:** `ScaffoldOutputDir` uses `os.ReadDir`; any entry → error "output directory %q is not empty: refusing to overwrite (point --output-dir at a new or empty path)". A file at the path also rejects. The error propagates from `PrepareReview` and `runReview` wraps it in `usageError` → exit 2. `TestScaffoldOutputDir_RejectsNonEmpty` + `RejectsFileAtPath` confirm.

### Criterion: `--output-dir` and `--id` together fail with exit 2 (mutually exclusive)
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/review.go:45-61`, `cmd/atcr/review_test.go:122-129`
- **Notes:** `outputDirFromFlags` returns `usageError("--output-dir and --id are mutually exclusive")` when both flags are Changed(), before any review work. `TestOutputDirFromFlags_MutuallyExclusiveWithID` confirms.

### Criterion: `atcr reconcile` operates correctly on a review directory created with `--output-dir`
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/reconcile_test.go:64-78`
- **Notes:** Per the recorded clarification, reconcile takes the explicit path via its `[id-or-path]` positional argument — no new flag. `TestReconcileCmd_InheritsExternalOutputDir` proves a review at an external output-dir path reconciles correctly.

### Criterion: Existing behavior (no `--output-dir`) is unchanged — `.atcr/reviews/<id>/` and `.atcr/latest` work as before
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/review.go:180-225`, `internal/fanout/review_test.go:107-152`, `internal/fanout/reviewdir_test.go:128-212`
- **Notes:** When `OutputDir == ""`, the switch falls through to `claimReviewDir` (derived id) / `ScaffoldReviewDir` (explicit id), and `WriteLatest` runs. `TestRunReview_EndToEnd` asserts the latest pointer equals the review id; `TestScaffoldReviewDir_CreatesLayout` and `TestClaimReviewDir_*` cover the default path unchanged.

### Criterion: Unit tests cover flag parsing, path validation, and the `.atcr/latest` skip logic
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/review_test.go:114-149`, `internal/fanout/reviewdir_test.go:15-55`, `internal/fanout/review_test.go:155-186`
- **Notes:** Flag parsing: `TestOutputDirFromFlags_{Unset,MutuallyExclusiveWithID,RelativeResolvedToAbs,EmptyValueIsUsageError}`. Path validation: `TestScaffoldOutputDir_{CreatesTreeWhenAbsent,AllowsEmptyExisting,RejectsNonEmpty,RejectsFileAtPath}`. Latest skip: `TestRunReview_OutputDirSkipsLatest`.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — epic has no sprint-design.md risk profile)
**Files Reviewed:** 7 (cmd/atcr/review.go, internal/fanout/review.go, internal/fanout/reviewdir.go, internal/fanout/outcome.go, internal/payload/budget.go, internal/reconcile/ambiguous.go, internal/registry/project.go)
**Issues Found:** 10 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 10

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 3
- Low: 6

### Notes
- The HIGH (budget.go AllDropped producer-only) was independently re-verified by grep: only test code reads the field; buildPayloads never checks it. Real but gated behind an explicit too-small --byte-budget.
- The `--output-dir` core (review.go / reviewdir.go) is functionally correct; findings are hardening (containment, symlink/TOCTOU) and maintainability, not happy-path bugs.
- Notable process finding: the merge bundled unrelated TD/doc fixes (budget.go, outcome.go, ambiguous.go, registry/project.go) into a feature PR — flagged as MED scope-creep.
