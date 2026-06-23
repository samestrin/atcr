# Code Review Report: 7.1_local_syntax_guard

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 3 / 3
- **Approval Status:** Approved
- **Review Date:** June 22, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied
All three acceptance criteria were already `[x]` in the archived epic and are confirmed by code evidence (no checkbox changes required):
- **.planning/epics/completed/7.1_local_syntax_guard.md** – Fixes applied to in-memory/temp files and parsed
  - State: `[x]` (verified)
  - Evidence: `internal/verify/syntaxguard.go:82-146`
- **.planning/epics/completed/7.1_local_syntax_guard.md** – Built-in syntax checking for at least Go
  - State: `[x]` (verified)
  - Evidence: `internal/verify/syntaxguard.go:3-8,121-146`
- **.planning/epics/completed/7.1_local_syntax_guard.md** – Invalid fixes retried or flagged
  - State: `[x]` (verified)
  - Evidence: `internal/verify/executor.go:150-155`

## 3. Evidence Map
- **Fixes are applied to in-memory/temp files and parsed**
  - Evidence: `internal/verify/syntaxguard.go:82-146`
  - Summary: `validateGoFixSyntax` parses the free-form Fix string in-memory via `go/parser.ParseFile` under three OR-ed strategies (whole-file, decl-prefixed, stmt-wrapped). Per the recorded clarification the guard parses the Fix content directly; applying to a working-tree copy is explicitly out of scope. The in-memory branch of the AC is satisfied.
- **Support built-in syntax checking for at least Go**
  - Evidence: `internal/verify/syntaxguard.go:3-8,121-146`
  - Summary: Pure stdlib `go/parser` + `go/token`. No shelling to `go build`/`go vet`, no temp module. Parse-only (no type check); header comment correctly strikes "compile" (refined in #76).
- **Invalid fixes are either retried (up to N times) or flagged in the output**
  - Evidence: `internal/verify/executor.go:150-155`, `internal/report/render.go:168-169`, `internal/reconcile/emit.go:135`
  - Summary: Flag branch chosen (retry deferred per clarification). `FixWarning = "invalid_syntax: <parser error>"` is set when plausibly-Go code fails to parse, cleared on valid/prose fixes (no stale warning), and rendered via the `⚠️ Fix warning:` line. Reuses the existing 7.0 `FixWarning` field — zero new types.

## 4. Remaining Unchecked Items
No remaining unchecked items — all 3 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All ACs satisfied with direct code evidence. Implementation is minimal-surface (stdlib-only, one net-new file, reuses existing FixWarning field) and matches every recorded clarification. Adversarial pass found no critical/high defects; concurrency is race-clean and the regexes are RE2 (ReDoS-safe).

## 6. Coverage Analysis
- **Coverage:** 89.7% (total); `internal/verify` package 95.3%; `syntaxguard.go` 100% per-function
- **Baseline:** 80%
- **Delta:** ↑9.7%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run (0 issues) |
| Types | PASSING | go vet ./... |
| Format | PASSING | gofmt (clean) |

## 8. Adversarial Analysis
- **Files Reviewed:** 4 (syntaxguard.go, executor.go, emit.go, render.go) + 2 test files
- **Issues Found:** 6 (Critical: 0, High: 0, Medium: 2, Low: 4)
- **Mode:** Discovery-only (no sprint-design.md risk profile for an epic)

### Issues by Severity

**Medium**
- `internal/verify/syntaxguard.go:49` — `blockOpenRe` flags a prose change-instruction whose line ends in a lone `{` as `invalid_syntax` (false positive not covered by the documented WONTFIXes). [correctness]
- `internal/verify/syntaxguard.go:35` — `fenceRe` rejects a CommonMark info string with a trailing word (```` ```go note ````), so the raw backticked block is parsed as Go and a non-Go fence can be flagged. [correctness]

**Low**
- `internal/verify/syntaxguard.go:83` — size cap measured on raw `fix` before extraction; benign but comment overstates precision. [maintainability]
- `internal/verify/syntaxguard_test.go:180` — no test for the prose-line-ending-in-`{` false-positive branch. [testing]
- `internal/verify/syntaxguard_test.go:166` — no test for the fence-with-trailing-word info string. [testing]
- `internal/verify/executor.go:154` — FixWarning ownership correct on the live path but the "not copied in reconcile" invariant is implicit/unenforced. [maintainability]

### Verified clean (no defect)
- ReDoS: Go RE2 linear; 200KB adversarial probe worst-case 64ms; 256KiB cap bounds input.
- Concurrency: worker pool mutates only its own `findings[i]`; shared regexes read-only; `-race` clean.
- `maxFixBytes` boundary correctly exclusive; empty/whitespace/CRLF handled and tested.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/7.1_local_syntax_guard.md` to merge these 6 findings into the TD README. The two medium correctness items align with the existing `epic 7.5_syntax-guard-refinements` scope and can be routed there.

---
*Generated by /execute-code-review on June 22, 2026 03:43:43PM*
