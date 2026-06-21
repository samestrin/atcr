# Code Review Stream - 5.4_path_resolution_hardening (Epic)

**Started:** June 21, 2026 08:43:28AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — candidate file index built once per reconcile run from `git ls-files`
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/fileindex.go:30-42` (BuildFileIndex runs `git ls-files -z`), `internal/reconcile/validate.go:21-24` (index built once, shared across the per-finding loop)
- **Notes:** Uses `git ls-files -z`, not filepath.Walk; nil index degrades to existence-only.

### Criterion: AC2 — Tier 1 exact-basename-elsewhere suggestion, no edit-distance threshold
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/suggest.go:80-110` (tier1: single basename match returned directly; ties ranked by segOverlap, ambiguous → "")
- **Notes:** No similarity threshold applied to Tier 1. Test `TestMissingSuggestion_Tier1WrongDir` (suggest_test.go:67).

### Criterion: AC3 — Tier 3 case-only difference flagged invalid AND suggests correct case (case-insensitive FS too)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/validate.go:78-85` (CaseCorrection consulted before existence stat), `internal/stream/suggest.go:31-48` (CaseCorrection)
- **Notes:** Index — not os.Stat — is authoritative for case, so a case-typo present on APFS is still flagged. Test `TestValidatePath_SuggestsCaseTypo` (validate_test.go:167).

### Criterion: AC4 — Tier 2 same-dir typo suggestion above threshold; none when ambiguous/below
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/suggest.go:114-152` (tier2: HasDir gate, similarity ≥ tier2SimilarityThreshold, tie/below → ""), threshold `suggest.go:17`
- **Notes:** Threshold 0.75 tuned to admit validator→validate (0.78). Tests Tier2Typo/Tier2BelowThreshold (suggest_test.go:99,109).

### Criterion: AC5 — existence check does not traverse symlinks out of repo root
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/validate.go:115-141` (existsContained: EvalSymlinks then containment re-check), tests `TestValidatePath_SymlinkEscapeFlagged` (validate_test.go:124), `TestValidatePath_SymlinkEscapeNoSuggestion` (validate_test.go:195)
- **Notes:** link→outside resolves and is re-checked; stays invalid with no suggestion.

### Criterion: AC6 — PathSuggestion flows through JSONFinding (omitempty) and renders "(did you mean…)" in all views; original preserved
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/emit.go:92` (omitempty field), `emit.go:114` (copied in JSONFindings), `internal/report/render.go:321-330` (writePathWarning backs markdown/checklist/refuted), `internal/reconcile/emit.go:332-336`
- **Notes:** File is never reassigned; suggestion is additive. Tests across emit_test.go:348-389, report/validate_test.go:65-106.

### Criterion: AC7 — default never rewrites finding.File (suggestion only)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/validate.go:78-96` — only PathValid/PathWarning/PathSuggestion are set; f.File is never assigned anywhere in the change.
- **Notes:** No `--correct-paths` flag introduced (correctly deferred per epic scope).

### Criterion: AC8 — unit tests per tier + ambiguous + below-threshold + symlink + one e2e to report.md
- **Verdict:** VERIFIED ✅
- **Evidence:** suggest_test.go (Tier1/2/3 + ambiguous + below-threshold), validate_test.go (symlink), `internal/reconcile/pathsuggest_e2e_test.go:48-81` (hallucinated finding → "did you mean" in report.md)
- **Notes:** e2e asserts `md` contains "did you mean".

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (Full hostile review, no sprint-design.md risk profile)
**Files Reviewed:** 8 source files
**Issues Found:** 10 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 2
- Low: 8

### Notable findings
- **MEDIUM (security):** reconcile `esc()` (emit.go:411) does not escape backticks, unlike report `esc()` — the "(did you mean …)" warning line in `writeFindingsList` can pass a raw backtick from a reviewer-cited `File` into report.md, opening a markdown code span. Confirmed by the reviewer agent with a throwaway test. The main bullet is safe (uses `codeSpan`); the warning line is not.
- **MEDIUM (maintainability):** the warning-line render is duplicated between `reconcile/emit.go` and `report/render.go` with divergent backtick escaping — the drift that produced the security item.

### Areas probed and found solid
- AC5 symlink-escape oracle: `existsContained` re-resolves both root and joined with EvalSymlinks and re-checks containment; parent-symlink-on-missing-leaf maps to invalid. Genuinely well done.
- Path traversal / absolute paths neutralized by lexical guard + resolved containment.
- Levenshtein two-row DP correct; threshold honestly tuned to the real distance-2 validator→validate example.
- Tier 2 `prefixDerivation` guard correctly rejects user/users, parse/parser while keeping validator/validate.
- omitempty byte-identity for `path_suggestion` confirmed; `File` never rewritten (AC7); nil-safety throughout.

All 10 findings are non-blocking technical debt; none affect the AC verdicts. The epic is already merged (#61) and archived.
