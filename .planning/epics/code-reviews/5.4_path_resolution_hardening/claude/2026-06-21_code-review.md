# Code Review Report: 5.4_path_resolution_hardening

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 8 / 8
- **Approval Status:** Approved
- **Review Date:** June 21, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied

- **5.4_path_resolution_hardening.md** – AC1 candidate index from `git ls-files`
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/stream/fileindex.go:30-42`, `internal/reconcile/validate.go:21-24`
- **5.4_path_resolution_hardening.md** – AC2 Tier 1 exact-basename-elsewhere
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/stream/suggest.go:80-110`
- **5.4_path_resolution_hardening.md** – AC3 Tier 3 case-only difference
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/stream/validate.go:78-85`, `internal/stream/suggest.go:31-48`
- **5.4_path_resolution_hardening.md** – AC4 Tier 2 same-dir typo above threshold
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/stream/suggest.go:114-152`
- **5.4_path_resolution_hardening.md** – AC5 symlink-safe existence check
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/stream/validate.go:115-141`, `internal/stream/validate_test.go:124,195`
- **5.4_path_resolution_hardening.md** – AC6 PathSuggestion flows through JSONFinding + all views
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/reconcile/emit.go:92,114`, `internal/report/render.go:321-330`
- **5.4_path_resolution_hardening.md** – AC7 never rewrites finding.File
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/stream/validate.go:78-96`
- **5.4_path_resolution_hardening.md** – AC8 per-tier + ambiguous + below-threshold + symlink + e2e tests
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/reconcile/pathsuggest_e2e_test.go:48-81`

## 3. Evidence Map

- **AC1 — index built once from `git ls-files`**
  - Evidence: `internal/stream/fileindex.go:30-42`, `internal/reconcile/validate.go:21-24`
  - Summary: `BuildFileIndex` runs `git ls-files -z` (not filepath.Walk); built once outside the per-finding loop and shared; nil index degrades to existence-only.
- **AC2 — Tier 1, no edit-distance threshold**
  - Evidence: `internal/stream/suggest.go:80-110`
  - Summary: single basename match returned directly; ties ranked by segment overlap, ambiguous → no suggestion.
- **AC3 — Tier 3 case-only, even on case-insensitive FS**
  - Evidence: `internal/stream/validate.go:74-85`, `internal/stream/suggest.go:31-48`
  - Summary: `CaseCorrection` consulted before the os.Stat-class existence check, so the index (not the filesystem) is authoritative for case.
- **AC4 — Tier 2 typo above threshold**
  - Evidence: `internal/stream/suggest.go:114-152`, threshold `suggest.go:17`
  - Summary: directory-exists gate, similarity ≥ 0.75 (tuned to validator→validate ≈ 0.78), tie/below → no suggestion; `prefixDerivation` guard rejects pluralizations.
- **AC5 — symlink-safe existence**
  - Evidence: `internal/stream/validate.go:115-141`
  - Summary: `existsContained` resolves both root and joined via EvalSymlinks then re-checks containment; a symlink escaping the repo is invalid with no suggestion.
- **AC6 — PathSuggestion through findings.json + all views**
  - Evidence: `internal/reconcile/emit.go:92,114`, `internal/report/render.go:321-330`, `internal/reconcile/emit.go:332-336`
  - Summary: omitempty field, copied in JSONFindings, rendered as "(did you mean …)" across markdown/checklist/refuted; original File preserved.
- **AC7 — suggestion only, never rewrite**
  - Evidence: `internal/stream/validate.go:78-96`
  - Summary: only PathValid/PathWarning/PathSuggestion set; `f.File` never reassigned; no `--correct-paths` flag (correctly deferred).
- **AC8 — full test matrix**
  - Evidence: `suggest_test.go`, `validate_test.go`, `fileindex_test.go`, `levenshtein_test.go`, `internal/reconcile/pathsuggest_e2e_test.go:48-81`
  - Summary: per-tier, ambiguous, below-threshold, symlink, and end-to-end (hallucinated finding → "did you mean" in report.md) all covered.

## 4. Remaining Unchecked Items

No remaining unchecked items - all verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All 8 acceptance criteria are implemented with code + test evidence. Tests pass, coverage 89.7% (epic packages 90.8–97.7%), lint/types/format clean. Adversarial review surfaced only non-blocking technical debt (0 critical, 0 high).

## 6. Coverage Analysis
- **Coverage:** 89.7%
- **Baseline:** 80%
- **Delta:** ↑9.7%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 8
- **Issues Found:** 10 (Critical: 0, High: 0, Medium: 2, Low: 8)

### Issues by Severity

**Medium**
- `internal/reconcile/emit.go:333` (security) — reconcile `esc()` does not escape backticks like report `esc()`; the "(did you mean …)" warning line can pass a raw backtick from a reviewer-cited `File` into report.md, opening a markdown code span. Main bullet is safe (`codeSpan`); warning line is not.
- `internal/reconcile/emit.go:332` (maintainability) — the warning-line render is duplicated between `reconcile/emit.go` and `report/render.go` with divergent backtick escaping (the drift behind the security item).

**Low**
- `internal/stream/suggest.go:93` (correctness) — tier1 multi-candidate branch may suggest a sibling for a cited path that is itself tracked but absent on disk; self-guard exists only on the single-candidate branch.
- `internal/stream/fileindex.go:60` (error-handling) — `filepath.ToSlash` is a no-op on non-Windows; backslash-cited paths are not normalized.
- `internal/stream/fileindex.go:37` (ops) — git failure collapsed to `nil` with the error discarded; feature can be silently off in CI with no log/metric.
- `internal/stream/fileindex.go:113` (docs) — comment overstates "Unicode-simple case folding"; `strings.ToLower` is ASCII-lowercase, not full Unicode fold.
- `internal/stream/suggest.go:120` (performance) — tier2 runs Levenshtein per dir file with no length-difference early-out.
- `internal/stream/validate.go:68` (security) — escape checks use `filepath.Separator`; latent cross-platform fragility (not a live break on macOS/Linux).
- `internal/reconcile/emit_test.go:175` (testing) — no test for markup injection via `PathSuggestion`/`File` on the warning line.
- `internal/reconcile/validate.go:21` (performance) — builds the git index even when `len(findings) == 0`.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/5.4_path_resolution_hardening.md` to merge these 10 findings into the technical-debt README, then `/resolve-td` for the two MEDIUM items (backtick escaping + render duplication) as the highest-value cluster.

---
*Generated by /execute-code-review on June 21, 2026 08:43:28AM*
