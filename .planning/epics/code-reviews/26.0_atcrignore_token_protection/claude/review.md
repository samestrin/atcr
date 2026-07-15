# Code Review Stream - 26.0_atcrignore_token_protection (Epic)

**Started:** July 15, 2026 05:29:05AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: Files matching `.gitignore` are excluded from the review payload
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/ignore.go:51-61` (loadGitignore, full gitignore semantics), `internal/payload/diff.go:210` (changedFilesMemo calls applyIgnore), `internal/payload/diff.go:223-248` (applyIgnore partitions kept vs excluded), `internal/payload/ignore.go:103-105` (git.MatchesPath)
- **Notes:** Repo-root `.gitignore` loaded once per runner via lazy `matcher()`; matched files partitioned out of the changed-file list at the `changedFilesMemo` chokepoint ahead of `ApplyByteBudget`. Excluded paths also passed to git as `:(exclude,literal)` pathspecs so diff chunks stay in lockstep with the filtered head-path set.

### Criterion: Files matching `.atcrignore` are excluded from the review payload
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/ignore.go:66-88` (loadAtcrignore strips `!` negation lines, additive-only), `internal/payload/ignore.go:106-108` (atcr.MatchesPath OR'd with git), `internal/payload/ignore.go:32-35` (separate matchers OR'd → additive contract)
- **Notes:** `.atcrignore` is repo-root-only, purely additive to `.gitignore`. Negation lines dropped before compile so an entry can only add exclusions, matching the epic's "no `!` negation" spec. Escaped literal `\!` preserved.

### Criterion: `atcr review` logs a trace/debug message noting which files were skipped due to ignore rules
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/diff.go:234` (`g.log().Debug("payload: skipping ignored file", "file", f.path, "kind", f.kind)`), `internal/payload/diff.go:279-284` (log() nil-safe via injected context logger), `internal/payload/rangebuilder.go:51` (NewRangeBuilder seeds runner with log.FromContext(ctx))
- **Notes:** Each excluded file logged at slog debug via the existing gitRunner logger, wired from the review context logger. Also `--no-ignore` opt-out fully threaded on the fresh path: `cmd/atcr/review.go:76,318` (flag→ReviewRequest.NoIgnore) → `internal/fanout/review.go:244,777-782` (buildPayloads→WithoutIgnoreFilter) → `internal/payload/diff.go:113-122` (matcher() returns nil when noIgnore).

**AC Summary:** 3 verified, 0 partial, 0 incomplete.

---

## Adversarial Analysis (Discovery Mode)

**Mode:** Full hostile review (no sprint-design.md → discovery-only, no pre-identified risk profile)
**Files Reviewed:** 7 (internal/payload/ignore.go, diff.go, builder.go, rangebuilder.go, grounding.go; cmd/atcr/review.go, resume.go; internal/fanout/review.go, resume.go)
**Issues Found:** 7 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 3
- Low: 4

### Summary of findings
Core ignore-matcher logic (`ignore.go`) and the filter chokepoint (`applyIgnore`/`changedFilesMemo`) are correct: CRLF/BOM handling, escaped `\!`, `#` comments, mid-pattern `!`, `:(exclude,literal)` injection safety, once-per-runner matcher load, and all diff variants (name-status, numstat, function-context, plain, raw, zero-context) routing through the filter were all checked and are clean. The independent-review "literal pathspec magic" fix (commit fb71d381) is present at diff.go:240,244.

The 7 findings cluster around one theme — **the `--no-ignore` opt-out is honored only on the fresh RangeBuilder path**, not on the resume path (MEDIUM, resume.go:104 — silent payload divergence across a resumed review), the standalone package-level grounding/BuildEntries/ChangedFileCount paths (LOW ×2, currently unreachable but the stated invariant is false under --no-ignore), and there is no end-to-end test covering the wiring (MEDIUM, review.go:244). Plus: all-files-ignored yields a misleading "no changed files" error with no --no-ignore hint (MEDIUM, diff.go:223), a subdir-CWD pathspec asymmetry that can silently empty an out-of-subdir file's body (LOW, diff.go:75), and dead 'C'-copy defensive code that becomes silent data loss if copy detection is ever enabled (LOW, diff.go:243).

None block the epic's acceptance criteria — all three ACs are fully met and tests/coverage/lint/types/format all pass. The findings are follow-up hardening routed to technical debt.
