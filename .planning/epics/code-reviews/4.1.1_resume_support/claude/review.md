# Code Review Stream - 4.1.1_resume_support (Epic)

**Started:** June 18, 2026 10:34:19AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — `atcr review --resume latest` resolves latest review dir from `.atcr/latest`
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/resume.go:26-31` (resolveResumeDir → resolveReviewDir("")), `cmd/atcr/anchor.go:20-27` (ReadLatest → ReviewsRoot join); test `cmd/atcr/resume_test.go:66` TestResolveResumeDir_LatestAndEmpty
- **Notes:** "latest" and empty both map to the `.atcr/latest` pointer; reserved word is safe (ReviewID always date-prefixed).

### Criterion: AC2 — all agents complete → clean exit + reconcile
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/resume.go:119-126` (info.AllComplete() prints "All configured agents already completed. Re-running reconciliation..." then resumeReconcile), `internal/fanout/resume.go:215` (AllComplete); test `cmd/atcr/resume_test.go:197` TestResume_AllCompleteReconcilesAndExitsZero
- **Notes:** Message string matches the AC verbatim.

### Criterion: AC3 — changed git base/head aborts exit 2
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/resume.go:34-40` (ValidateResumeRange/ErrRangeChanged), `cmd/atcr/resume.go:103-108` (PrepareResume err → usageError = exit 2); test `cmd/atcr/resume_test.go:171` TestResume_RangeMismatchIsExit2
- **Notes:** Base/head compared as resolved SHAs from manifest vs current tree.

### Criterion: AC4 — fanout skips completed agents, runs only pending/failed
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/resume.go:218-227` (filterPendingSlots), `:97-145` (CompletedAgents via per-agent status.json status==ok); tests `internal/fanout/resume_test.go:158` TestFilterPendingSlots, `:63` TestCompletedAgents_OnlyOKAgentsAreComplete
- **Notes:** Completion signal is per-agent `status.json status=="ok"` (per recorded Clarifications), NOT the literal "non-empty findings.txt" in the AC body — the clarification explicitly rejected that heuristic because a zero-findings OK agent and a failed agent both write empty findings.txt. Implemented per the superior clarified design.

### Criterion: AC5 — resumed findings written + status.json updated incrementally
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/resume.go:296-318` (writeResumedAgents → writeAgentArtifacts), `:321-372` (RebuildPool merges union); tests `internal/fanout/resume_test.go:271` TestExecuteResume_MergesCompletedAndPending, `:239` TestWriteResumedAgents_PreservesFailedStatusOnNeverRun
- **Notes:** Real on-disk layout is `sources/pool/raw/agent/<agent>/{review.md,findings.txt,status.json}` + rebuilt `sources/pool/{findings.txt,summary.json}` (per Clarifications), not the illustrative `sources/<agent>/findings.txt`. Completed agents' artifacts untouched.

### Criterion: AC6 — all resumed agents finish → status "completed"
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/resume.go:248-289` (ExecuteResume finalizes manifest Interrupted=false/Partial from union), `:175-186` (ClearInterrupted clears stale marker); test `cmd/atcr/resume_test.go:252` TestResume_AllCompleteClearsStaleInterrupted
- **Notes:** "completed" is derived (manifest Interrupted=false + all-OK union). Stale interrupt marker (signal after last agent ok) cleared on all-complete resume.

### Criterion: AC7 — re-interruption preserves partial findings, keeps "interrupted"
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/resume.go:250-285` (interrupted = ctx.Canceled, writeResumedAgents persists before manifest, manifest.Interrupted=interrupted), `cmd/atcr/resume.go:148-153` (ctx.Canceled → interruptMessage + exit 1)
- **Notes:** Synthesized context-cancel results skipped so a prior real failure's status is not clobbered (writeResumedAgents guard).

### Criterion: AC8 — unit tests cover roster filtering
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/resume_test.go:158` TestFilterPendingSlots, `:63/:82/:93` TestCompletedAgents_*, `:137` TestValidateResumeRoster_SetEquality
- **Notes:** Roster drift fail-closed (set equality) also covered by `cmd/atcr/resume_test.go:183` TestResume_RosterMismatchIsExit2.

### Criterion: AC9 — integration: interrupt after agents, resume, all complete
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/resume_test.go:396` TestResume_InterruptThenResumeCompletesAllAgents, `cmd/atcr/resume_test.go:214` TestResume_RunsPendingAgentThenReconciles
- **Notes:** End-to-end interrupt→resume→reconcile path covered.

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (no sprint-design.md risk profile — epic)
**Files Reviewed:** 5 (cmd/atcr/{resume,review,anchor}.go, internal/fanout/{resume,review}.go)
**Issues Found:** 9 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 2
- Low: 7

### Notable (verified against code)
- MED: RebuildPool silently drops a completed agent's findings on a findings.txt parse error while still counting it Succeeded (no log) — resume-only aggregate divergence.
- MED: resume interrupt path lacks the structured `log.Warn("review interrupted by signal")` the fresh path emits — breaks AC9/AC10 greppability parity.
- LOW: live lint failure (errcheck) at resume_test.go:258 fails the golangci-lint CI gate on this branch.

### Reviewer confirmed correct (no finding)
- Synthesized-timeout skip in writeResumedAgents correctly preserves a prior real failure's status.
- agentDirName rejects path traversal (., .., separators).
- All-agents-failed gate + Partial recomputation judged over the union, consistent with fresh path.
- Snapshot-provenance carry-over branching is sound; local-copy manifest pattern correctly applied.
- Exit-code mapping (usage=2, failure=1, interrupt=1) consistent with the fresh review path.
