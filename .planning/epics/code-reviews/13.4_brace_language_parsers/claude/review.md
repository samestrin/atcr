# Code Review Stream - 13.4_brace_language_parsers (Epic)

**Started:** June 29, 2026 06:32:45AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — single shared brace-block parser parameterized by a per-language keyword/naming table (no separate parser binary per language)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/astgroup/parsers/src/braceparser/parse_core.go:75` (one language-agnostic scanner), `configs.go:19-111` (four data-only tables), `active_ts.go:7`/`active_bash.go:7` (compile-time table selection), `build.sh:47-50` (4 builds of one source)
- **Notes:** One shared `braceparser` source; all language differences live in the `langConfig` data tables, not scanner control flow. The literal "no separate parser binary" clause is consciously relaxed by the epic's recorded clarification (FOUR `.wasm` from ONE parameterized source, table baked in per build tag) — the binding intent (single shared parser source, config-table parameterization) is met.

### Criterion: AC2 — new extensions resolve via LanguageForExt and findings group across line drift > 3
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/astgroup/embed.go:37-44` (`.ts/.tsx/.cts/.mts/.js/.jsx/.mjs→ts`, `.php→php`, `.rs→rust`, `.sh/.bash→bash`), `embed.go:18-25` (`builtinParsers` entries), `benchmark_test.go:116-117` (asserts AST resolves every >3-drift positive proximity misses)
- **Notes:** Drift-grouping verified by structural Merkle hash in the host (unchanged from 13.1). Confirmed green by test run in Phase 4.

### Criterion: AC3 — accuracy benchmark holds AST recall ≥ 0.95 and precision ≥ proximity on the new languages
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/astgroup/benchmark_test.go:113-119` (recall ≥ 0.95, `astFP == 0`, dominance over proximity), `testdata/corpus.json` (48 labeled pairs: 8 ts / 6 php / 6 rs / 5 sh + 1 bash, plus go/py)
- **Notes:** Corpus extended with drift/no-drift pairs per new language. Confirmed green by test run in Phase 4.

## Adversarial Analysis (Discovery Mode — no sprint-design risk profile)

**Mode:** Full hostile review (epic — no embedded adversarial)
**Files Reviewed:** 4 (parse_core.go, configs.go, main.go, embed.go)
**Issues Found:** 10 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic mode)

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 10

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 5
- Low: 5

All 10 are heuristic-parser precision limitations (regex/heredoc/arrow/modifier/brace-expansion edge cases). By design, each degrades only to ±3 line-proximity grouping for the affected finding and can never break a reconcile — consistent with the epic's stated blast radius and its "per-language naming nuance reduces precision" risk row. None block epic acceptance.
