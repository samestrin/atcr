# Code Review Stream - 2.1_manifest_review_stage_recording (Epic)

**Started:** June 13, 2026 06:22:41PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: `manifest.json` review stage contains `snapshot_mode`, `head_sha`, `snapshot_worktree_path` after a review run
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/manifest.go:63-77` (three new ReviewStage fields), `internal/fanout/review.go:284`, `internal/fanout/review.go:296-298`, `internal/fanout/review.go:343-349` (stamped after fan-out), `internal/fanout/engine_e2e_test.go:176-196` (asserts on disk end-to-end)
- **Notes:** Fields land in the serialized `review` block. Epic prose says "stages.review", but `ReviewStage` serializes as a top-level `review` key (sibling of `stages`, per `TestManifest_ReviewStageOmittedWhenNil`). Wording imprecision in the epic, not a code defect — the intent (record provenance on the review stage) is satisfied.

### Criterion: AC 03-02 Scenario 5 assertion test passes
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/manifest_review_test.go:70-86` (`TestManifest_ReviewStage_SnapshotWorktreeMode`), `:88-110` (`TestManifest_ReviewStage_SnapshotLiveMode`); both PASS. Live mode explicitly asserts `snapshot_worktree_path` is present-as-"" (not omitted).

### Criterion: AC 03-03 Scenarios 4-5 assertion tests pass (live + worktree mode)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/manifest_review_test.go:81-89` (`TestSnapshotManifestFields_LiveMode`, S5), `:92-98` (`TestSnapshotManifestFields_WorktreeMode`, S4); both PASS. e2e `TestExecuteReview_ToolAgentEndToEnd` exercises the real worktree slow path on disk (dirty tree via `.atcr/` scaffolding).
- **Notes:** `head_sha` consistency confirmed sound — `gitrange.resolveRef` returns a fully-resolved SHA, matching the worktree leaf (`resolvedHead` in `snapshot.go:88`), so the e2e dual assertion `head_sha == worktree-leaf` holds in production, not just the test.

### Criterion: Existing snapshot and manifest tests unaffected
- **Verdict:** VERIFIED ✅
- **Evidence:** `go test ./internal/payload/... ./internal/fanout/... ./internal/tools/...` all pass. Coverage: payload 90.0%, fanout 87.1%, tools 86.5% (all > 80% baseline). `SnapshotMode`/`HeadSHA` use `omitempty`, so a pure 1.x roster manifest is byte-unchanged.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (full hostile review, epic had no embedded adversarial tasks)
**Files Reviewed:** 2 source (review.go, manifest.go) + 3 test files
**Issues Found:** 1 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic, no sprint-design.md)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 1

**LOW — `snapshotManifestFields` couples live-detection to SnapshotFor's fast-path return.** `review.go:660` infers `"live"` purely from `root == repo` string equality. This is correct today only because `NewSnapshotManager` stores `repoRoot` verbatim and the fast path returns that exact string. If `SnapshotFor` is ever changed to canonicalize `repoRoot` (e.g. `EvalSymlinks`/`Abs` — which `snapshotCleanupGuard` already does for the temp guard), a clean-HEAD live snapshot would silently misclassify as `"worktree"` and leak the repo root into `snapshot_worktree_path`. No integration test drives the real SnapshotFor *fast* path to manifest (the e2e only hits the worktree slow path; the unit tests pass literal strings), so this coupling is unguarded.

