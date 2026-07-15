# Code Review Report: 26.0_atcrignore_token_protection

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 3 / 3
- **Approval Status:** Approved
- **Review Date:** July 15, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied
- **.planning/epics/completed/26.0_atcrignore_token_protection.md** – Files matching `.gitignore` are excluded from the review payload
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/payload/ignore.go:51-61`, `internal/payload/diff.go:210-248`
- **.planning/epics/completed/26.0_atcrignore_token_protection.md** – Files matching `.atcrignore` are excluded from the review payload
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/payload/ignore.go:66-108`
- **.planning/epics/completed/26.0_atcrignore_token_protection.md** – `atcr review` logs a debug message noting which files were skipped
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/payload/diff.go:234`

## 3. Evidence Map
- **`.gitignore` exclusion**
  - Evidence: `internal/payload/ignore.go:51-61`, `internal/payload/diff.go:223-248`, `internal/payload/ignore.go:103-105`
  - Summary: Repo-root `.gitignore` compiled with full gitignore semantics and OR'd into the matcher; changed files matching it are partitioned out of the file list at the `changedFilesMemo` chokepoint (ahead of `ApplyByteBudget`) and excluded from all downstream diff variants via `:(exclude,literal)` pathspecs.
- **`.atcrignore` exclusion**
  - Evidence: `internal/payload/ignore.go:66-88`, `internal/payload/ignore.go:32-35`
  - Summary: Repo-root-only, purely additive to `.gitignore`; `!` negation lines stripped before compile (no re-inclusion), escaped `\!` preserved. Separate matchers OR'd so neither source can un-exclude the other.
- **Debug logging of skipped files**
  - Evidence: `internal/payload/diff.go:234`, `internal/payload/diff.go:279-284`, `internal/payload/rangebuilder.go:51`
  - Summary: Each excluded file logged at slog debug via the existing `gitRunner` logger, wired from the review context logger. The `--no-ignore` opt-out is fully threaded on the fresh path (CLI → `ReviewRequest.NoIgnore` → `buildPayloads` → `WithoutIgnoreFilter`).

## 4. Remaining Unchecked Items
No remaining unchecked items - all 3 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All acceptance criteria are met with concrete code evidence; the full test suite passes with coverage above baseline; lint, vet, and format are all clean. Adversarial analysis surfaced only follow-up hardening items (0 critical, 0 high), none of which block the epic's deliverables.

## 6. Coverage Analysis
- **Coverage:** 89.0%
- **Baseline:** 80%
- **Delta:** ↑9.0%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | gofmt -l (git-tracked .go files) |

## 8. Adversarial Analysis
- **Files Reviewed:** 7
- **Issues Found:** 7 (Critical: 0, High: 0, Medium: 3, Low: 4)

### Issues by Severity

**MEDIUM**
- `internal/payload/diff.go:223` — All-files-ignored range (e.g. lockfile-only PR) yields the misleading error "no changed files (only merge or empty commits?)" with no hint that `--no-ignore` would surface the filtered files. (error-handling)
- `cmd/atcr/resume.go:104` — `runResume` never sets `NoIgnore` and it is not persisted in the manifest, so resuming a `--no-ignore` review silently re-filters ignored files — inconsistent payload across the same review, undetectable by SHA-based resume validation. (correctness)
- `internal/fanout/review.go:244` — No end-to-end test asserts `NoIgnore` threads through `PrepareReview`/`PrepareResume` to the built payload; the resume regression above is invisible behind a green suite. (testing)

**LOW**
- `internal/payload/diff.go:75` — Subdir-CWD pathspec asymmetry: `--name-status` lists the whole repo but the `.`-scoped exclude diff can silently empty an out-of-subdir file's body when run from a subdirectory with an active ignore file. (correctness)
- `internal/payload/diff.go:243` — 'C' (copy) status collapsed into `kindRenamed` also excludes the copy source; dead code today (`-M` never emits 'C') but becomes silent data loss if copy detection is enabled. (correctness)
- `internal/payload/grounding.go:37` — Standalone package-level `BuildChangedLines`/`BuildEntries` cannot honor `--no-ignore`; the never-taken grounding fallback would drop findings on ignored files under `--no-ignore`. (maintainability)
- `internal/payload/builder.go:136` — The new "ChangedFileCount must agree with BuildEntries" comment/invariant is false under `--no-ignore` (ChangedFileCount hardcodes filtering); unreachable today (MCP caller never sets NoIgnore). (maintainability)

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/26.0_atcrignore_token_protection.md` to merge these 7 findings into the technical-debt README with reviewer + confidence attribution.
- Theme for a follow-up hardening pass: make `--no-ignore` symmetric across resume, grounding, and `ChangedFileCount` paths, and add the missing end-to-end wiring test.

---
*Generated by /execute-code-review on July 15, 2026 05:29:05AM*
