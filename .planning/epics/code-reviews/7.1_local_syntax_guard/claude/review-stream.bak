# Code Review Stream - 7.1_local_syntax_guard (Epic)

**Started:** June 22, 2026 03:43:43PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: Fixes are applied to in-memory/temp files and parsed.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/syntaxguard.go:82-146`
- **Notes:** `validateGoFixSyntax` parses the fix in-memory via `go/parser.ParseFile` (`parseGoFix`, lines 121-146) under three OR-ed strategies (whole-file, decl-prefixed, stmt-wrapped). Per the recorded clarification the guard parses the free-form Fix string directly rather than applying it to a working-tree copy (applying to the tree is explicitly OUT of scope). The "in-memory" branch of the AC is satisfied; no temp file is written.

### Criterion: Support built-in syntax checking for at least Go.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/syntaxguard.go:3-8,121-146`
- **Notes:** Pure stdlib `go/parser` + `go/token`, no shelling to `go build`/`go vet`, no temp module. Parse-only (no type check) — header comment correctly strikes "compile" (refined in #76).

### Criterion: Invalid fixes are either retried (up to N times) or flagged in the output.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/executor.go:150-155`, `internal/report/render.go:168-169`, `internal/reconcile/emit.go:135`
- **Notes:** Flag branch chosen (retry deferred per clarification). `f.FixWarning = "invalid_syntax: " + synErr.Error()` is set when a plausibly-Go fix fails to parse, cleared on valid/prose fixes (no stale warning), and surfaced in the report via the `⚠️ Fix warning:` line. Reuses the existing 7.0 `FixWarning` field — no new types.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md for epic)
**Files Reviewed:** 4 (syntaxguard.go, executor.go, emit.go, render.go + 2 test files)
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 6

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 2
- Low: 4

### Verified-clean (no defect)
- ReDoS: Go RE2 is linear — probed 200KB adversarial inputs, worst case 64ms; 256KiB cap bounds real input.
- Concurrency: worker pool mutates only `&findings[i]`; shared regexes read-only; `-race` clean.
- maxFixBytes boundary correctly exclusive; empty/whitespace/CRLF handled and tested.

### Accepted WONTFIXes (documented, not re-reported)
- First-fence-only scan; OR-ed three-strategy parse; unfenced no-block-structure fragment not flagged.
