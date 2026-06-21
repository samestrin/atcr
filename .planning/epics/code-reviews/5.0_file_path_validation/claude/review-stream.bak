# Code Review Stream - 5.0_file_path_validation (Epic)

**Started:** June 20, 2026 01:17:47PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — All findings validated for file existence after parsing
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/gate.go:222` (validateFindingPaths call), `internal/reconcile/validate.go:11-17`
- **Notes:** Validation runs in the reconcile pipeline (on merged findings, after Reconcile / before Emit), not in the pure parser — the deliberate design adaptation recorded in the epic Clarifications (parser has no repo root). Satisfies the intent of AC1.

### Criterion: AC2 — Non-existent paths flagged PathValid=false, PathWarning="file not found"
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/validate.go:13` (const PathNotFoundWarning = "file not found"), `internal/stream/validate.go:34-70`
- **Notes:** ValidatePath sets PathValid=false + PathWarning=PathNotFoundWarning on os.IsNotExist; leaves indeterminate (permission/IO) errors unflagged to avoid false "not found".

### Criterion: AC3 — Reports display warning for invalid paths
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/emit.go:319-324` (report.md), `internal/report/render.go:314-320` (writePathWarning, used in markdown/checklist/refuted views)
- **Notes:** Both renderers covered (report.md via reconcile.emit, `atcr report` via report.render). Path is HTML-escaped (esc) — no markup injection from reviewer-controlled path.

### Criterion: AC4 — Findings with invalid paths preserved, not discarded
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/validate.go` (only stamps flags, never removes), tests assert problem text retained
- **Notes:** Flag-only; finding is emitted with the warning line appended.

### Criterion: AC5 — Unit tests cover valid/invalid/typo/wrong-dir
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/validate_test.go:13-117` (ExistingFile, MissingFile, Typo, WrongDirectory, EmptyFile, EmptyRoot, EscapesRoot, AbsolutePath, NilSafe)
- **Notes:** Exceeds AC5 — also covers traversal/absolute-path containment and nil-safety adversarial cases.

### Criterion: AC6 — Integration test with hallucinated path shows warning in report
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/validate_test.go:99-154` (TestRunReconcile_FlagsHallucinatedPathEndToEnd)
- **Notes:** End-to-end through RunReconcile → findings.json → report.md; asserts hallucinated path flagged AND clean path unflagged AND both preserved.

### Criterion: AC7 — (Optional) Fuzzy matching corrects typos >90% similarity
- **Verdict:** NOT_FOUND ❌ (intentionally deferred)
- **Evidence:** Epic Clarifications (line 200), commit "chore(td): capture 5 TD item(s) from epic 5.0"
- **Notes:** Explicitly OUT of scope per the epic's own Clarifications and Open Questions (safe default = correction behind a flag). Deferred to technical debt — NOT a defect.

---

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (Full hostile review; no sprint-design risk profile — epic)
**Files Reviewed:** 11 source files changed by the epic
**Issues Found:** 10 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 10

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 2
- Low: 8

**Notes:** Zero critical/high. The epic met AC1–AC6 cleanly with strong tests. The 10 findings are hardening/quality items, all latent (production callers pass Root="." so no active defect): the standout is `internal/mcp/handlers.go:320` hardcoding `Root: "."` instead of `e.root` (correct only by coincidence). Adversarial reviewers explicitly confirmed NO regressions in: the findings.json round-trip (PathValid/PathWarning survive re-read), the verify re-emit (fields preserved), Reconcile staying I/O-free, and markdown injection defenses (esc/codeSpan).
