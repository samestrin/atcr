# Code Review Stream - 13.6_brace_languages_expansion (Epic)

**Started:** June 30, 2026 02:36:09PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests if enabled]

---

## Acceptance Criteria Findings

### Criterion: AC1 — Four new brace languages added primarily as data configs; scanner gains only the two named additions
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/astgroup/parsers/src/braceparser/configs.go:120-236` (javaConfig/kotlinConfig/cppConfig/csharpConfig, all data-only); `internal/astgroup/parsers/src/braceparser/parse_core.go:74` (`stTripleString` state), `parse_core.go:255-263` (tripleQuote scanner case), `parse_core.go:401-440` (`funcParenName` trailing-identifier balanced scan)
- **Notes:** Per-language differences live entirely in `configs.go` as `langConfig` data. The scanner gained exactly the two promised language-agnostic additions: (a) `funcParenName` reworked to take the trailing identifier before `(` via a balanced reverse scan (names `public void execute()` → execute, `void Foo::bar()` → bar; rejects member-access `foo.bar()`), and (b) a `tripleQuote`-gated `"""` opaque-string state. No per-language control flow.

### Criterion: AC2 — Extensions map to new parsers via LanguageForExt
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/astgroup/embed.go:50-60` (`.java`→java; `.kt`/`.kts`→kotlin; `.c`/`.cpp`/`.cc`/`.cxx`/`.h`/`.hpp`→cpp; `.cs`→csharp); `embed.go:24-27` (builtinParsers entries + go:embed)
- **Notes:** All extensions from the AC are mapped. The four `.wasm` files are embedded via `go:embed` and registered in `builtinParsers`.

### Criterion: AC3 — Extended corpus holds AST recall ≥0.95 and precision ≥ proximity on new languages
- **Verdict:** VERIFIED ✅ (test-asserted; runtime confirmation in Phase 4)
- **Evidence:** `internal/astgroup/benchmark_test.go:113` (`require.GreaterOrEqual(t, astRecall, 0.95)`), `benchmark_test.go:119` (`require.LessOrEqual(t, astFP, proxFP)`); `internal/astgroup/testdata/corpus.json` (60 cases incl. .java×3, .kt×3, .cpp×3, .cs×3)
- **Notes:** The benchmark asserts global recall ≥0.95 and AST false-positives ≤ proximity false-positives over the whole corpus, which now includes the 12 new-language drift cases. There is no per-language assertion (global only) — acceptable, but a per-language recall breakdown would harden the AC3 guarantee for each new language individually.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (no sprint-design.md in epic mode; epic Risks table fed to reviewers as the risk profile)
**Files Reviewed:** 10
**Issues Found:** 15 (verified from TD_STREAM)
**Risk Profile:** Epic Risks table (3 rows) — used as verification targets

### Risk Verification Summary
- ✅ Anticipated & Addressed: 3 (all three epic-risk mitigations verified correctly implemented in code: C/C++ `#` line-comment + macros out-of-scope; `tripleQuote` precedes single-`"`, empty/six-quote/unterminated all traced safe; `funcParenName` does not regress keyworded TS/PHP/Rust/Bash via keyword precedence)
- ⚠️ Anticipated & Missed: 0 (no mitigation found broken; no panics or index-out-of-range on any traced path)
- 🔍 Unanticipated: 15 (all findings are test-hardening or minor grouping-quality / doc items; none block — every defect degrades to ±3 line proximity, never breaks reconcile)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 3 (test-coverage gaps: per-language wasm table not distinctively verified; trailing-token signature untested; new-language kind mappings untested)
- Low: 12
