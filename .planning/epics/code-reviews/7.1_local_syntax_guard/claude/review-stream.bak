# Code Review Stream - 7.1_local_syntax_guard (Epic)

**Started:** June 22, 2026 11:50:52AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: Fixes are applied to in-memory/temp files and parsed
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/syntaxguard.go:94-119` (parseGoFix), `internal/verify/executor.go:140-149`
- **Notes:** Generated fix is parsed in-memory via `go/parser.ParseFile` against a `token.FileSet` (no on-disk temp file — the AC permits in-memory). Three parse strategies (whole-file, decl-wrapped, stmt-wrapped) cover the free-form Fix shape. Guard invoked inside `generateFixes` right after the fix string is produced.

### Criterion: Support built-in syntax checking for at least Go
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/syntaxguard.go:1-151` (pure stdlib `go/parser`, `go/token`)
- **Notes:** Go syntax validation via stdlib only — no shelling to `go build`/`go vet`, no temp module, no new runtime deps. Non-Go fenced blocks (python/js/etc.) are explicitly excluded so the Go guard does not false-flag other languages.

### Criterion: Invalid fixes are either retried (up to N times) or flagged in the output
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/executor.go:145-149`, `internal/reconcile/emit.go:126-133`, `internal/report/render.go:164-168`
- **Notes:** Flag branch chosen (AC permits either). Syntactically-invalid code fixes set `f.FixWarning = "invalid_syntax: <parser error>"` via the existing field (zero new types), the attempted fix stays visible, and the markdown report surfaces it as "⚠️ Fix warning". A pipeline warning is also logged. Valid/prose fixes clear any stale warning.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (no sprint-design.md — discovery-only)
**Files Reviewed:** 7
**Issues Found:** 11 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 11

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 5
- Low: 6

### Notes
Core guard logic is sound; lint/vet/format clean, 100% coverage on new guard functions. The 11 findings are follow-up TD, not blockers. Two MEDIUM correctness items are real regex-fence edge cases (first-fence-only validation → silent false negative on multi-block responses; nested triple-backtick string literal → false positive on valid code). Three MEDIUM testing items are coverage gaps around the new FixWarning path (golden file unpinned, escaping/truncation untested, omitempty JSON contract unguarded). Documented design trade-offs (conservative recall, OR-parse semantics, looksLikeGoCode ignoring inline `:=`) were intentionally NOT flagged as defects.
