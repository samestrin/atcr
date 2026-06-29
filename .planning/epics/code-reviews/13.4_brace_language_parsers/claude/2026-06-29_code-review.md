# Code Review Report: 13.4_brace_language_parsers

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 3 / 3 acceptance criteria
- **Approval Status:** Approved
- **Review Date:** June 29, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial + Tests)
- **Scope reviewed:** PR #115 / merge commit `7d73422e` (the brace-parser epic implementation)

## 2. Acceptance Criteria Verified
- **AC1 — single shared brace-block parser parameterized by a per-language keyword/naming table**
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/astgroup/parsers/src/braceparser/parse_core.go:75` (one language-agnostic scanner), `configs.go:19-111` (four data-only tables), `active_ts.go:7` / `active_bash.go:7` (compile-time table selection via build tag), `build.sh:47-50` (4 builds of one source)
- **AC2 — new extensions resolve via `LanguageForExt`; findings group across line drift > 3**
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/astgroup/embed.go:37-44` (extension→language map), `embed.go:18-25` (`builtinParsers`), `benchmark_test.go:116-117`
- **AC3 — accuracy benchmark holds AST recall ≥ 0.95 and precision ≥ proximity**
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/astgroup/benchmark_test.go:113-119`, `testdata/corpus.json` (48 labeled pairs)

## 3. Evidence Map
- **Shared parser, four binaries:** `braceparser` is one Go source; differences live entirely in the `langConfig` tables (`configs.go`). The literal "single binary / no separate parser binary" wording is consciously relaxed by the epic's recorded clarification (four `.wasm` from one parameterized source, table baked in per build tag) — the binding intent (one shared parser source, config-table parameterization) is met. `build.sh:42-50` builds `ts/php/rust/bash` from `src/braceparser`; `SHA256SUMS` refreshed and guarded by `TestEmbeddedParsersMatchManifest`.
- **Extension resolution:** `LanguageForExt` maps `.ts/.tsx/.cts/.mts/.js/.jsx/.mjs→ts`, `.php→php`, `.rs→rust`, `.sh/.bash→bash` (`embed.go:37-44`); `host.go`/`grouper.go` unchanged, as the clarification required.
- **Drift grouping & benchmark (live run):** recall **1.000**, precision **1.000**, `astFP=0`, AST resolved **10/10** large-drift (>3) same-block pairs proximity missed (proximity recall 0.677 with 10 false-merges). AST signal strictly dominates proximity.

## 4. Remaining Unchecked Items
No remaining unchecked items — all 3 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** Implementation matches the epic's clarified scope and constraints. Tests, coverage, lint, types, and format all pass. Adversarial findings are heuristic-precision limitations that degrade only to line-proximity fallback (never break a reconcile), so none block acceptance.

## 6. Coverage Analysis
- **Coverage:** 89.1% (total, full suite)
- **Baseline:** 80%
- **Delta:** ↑9.1%
- **Status:** PASSING
- Package of interest: `internal/astgroup` at 90.7%.

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... (gofmt -l clean) |

## 8. Adversarial Analysis
- **Files Reviewed:** 4 (`parse_core.go`, `configs.go`, `main.go`, `embed.go`)
- **Issues Found:** 10 (Critical: 0, High: 0, Medium: 5, Low: 5)
- **Risk Profile:** Not available (epic mode — no sprint-design.md)

### Medium
1. `parse_core.go:185` — TS/JS regex literals unhandled; braces inside a regex desync the brace stack (e.g. `/^}/`, `/\d{3}/`). `correctness`
2. `parse_core.go:466` — Bash arithmetic `$((1 << n))` misread as a heredoc (`isHeredocStart` only rejects the digit form), swallowing the rest of the file. `correctness`
3. `parse_core.go:274` — any `=>` in a header hijacks classification, tagging `for`/`switch`/`while` control blocks (with inline arrows) as `func`. `correctness`
4. `parse_core.go:507` — PHP 7.3+ indented heredoc/nowdoc closer (`    EOT;`) never recognized, swallowing to EOF. `correctness`
5. `parse_core.go:334` — TS methods with any modifier (`async`/`public`/`static`/`get`…) fall to anonymous `block`, losing the naming precision the parser exists for. `maintainability`

### Low
6. `parse_core.go:285` — TS `catch (e) {` misclassified as `func` named `catch` via `funcParenName`. `correctness`
7. `parse_core.go:444` — Rust `'\u{7f}'` char escape: its `{`/`}` fabricate a balanced spurious child block; comment overstates safety. `correctness`
8. `main.go:46` — `parse(ptr,n)` bounds guard misses a negative-`n` sign check; `buf[:n]` would panic (host-unreachable today). `error-handling`
9. `parse_core.go:223` — Bash brace expansion (`{a,b}`, `file{1,2}`, `{1..10}`) fabricates spurious blocks. `correctness`
10. `parse_core.go:320` — Rust generic `impl<T> Foo<T>` loses its name (`identAfter` stops at `<`), risking false-merge across unrelated generic impls. `maintainability`

## 9. Follow-ups
- These 10 findings live in `code-review/claude/td-stream.txt`. Run `/reconcile-code-review @.planning/epics/completed/13.4_brace_language_parsers.md` to merge them into the technical-debt README with reviewer + confidence attribution.
- All are anticipated heuristic limitations (the epic's "per-language naming nuance reduces precision" risk row). Consider corpus cases for the swallow-to-EOF desyncs (#2 bash arithmetic, #4 PHP indented heredoc) since those degrade a whole file's grouping.

---
*Generated by /execute-code-review on June 29, 2026 06:32:45AM*
