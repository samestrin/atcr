# atcr Reconciled Review

## Summary

- Total findings: 8
- Sources: pool
- Clusters collapsed: 0
- Severity disagreements: 0

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 5 | 0 |
| LOW | 0 | 3 | 0 |

## Disagreements

Top 8 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/astgroup/host_test.go:80` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: TestHost_BraceParsersLoadAndParse only asserts a func child exists; does not validate block structure for edge cases (nested blocks, empty bodies, annotations)

### 2. solo_finding — `internal/astgroup/parsers/src/braceparser/configs_test.go:122` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: New language config tests cover happy paths only; no test for malformed input (unclosed triple-quote, unterminated block comment, empty source)

### 3. solo_finding — `internal/astgroup/parsers/src/braceparser/parse_core.go:43` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: New tripleQuote config field has no test proving it interacts correctly with charLiterals/strChars when all are enabled simultaneously

### 4. solo_finding — `internal/astgroup/parsers/src/braceparser/parse_core.go:248` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: C# 11 raw strings with 4+ quotes desync state machine; documented limitation but no test proving graceful degradation

### 5. solo_finding — `internal/astgroup/parsers/src/braceparser/parse_core.go:515` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: funcParenName reserved-word guard omits try/synchronized/using/lock/fixed; Java/C# control headers misclassify as named funcs

### 6. solo_finding — `internal/astgroup/embed.go:4` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: Four new .wasm files added to embed.FS but no test verifies they are actually embedded and loadable

### 7. solo_finding — `internal/astgroup/parsers/src/braceparser/configs_test.go:122` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: No test for C# verbatim strings (@&#34;...&#34;) which are documented as out-of-scope; regression could silently break

### 8. solo_finding — `internal/astgroup/parsers/src/braceparser/parse_core.go:476` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: funcParenName modifier allowlist removed; keyword-less statement-prefixed calls like &#96;return foo() {&#96; now name func &#34;foo&#34;

## Findings

### MEDIUM

- `internal/astgroup/host_test.go:80` — confidence MEDIUM, reviewers: dax
  - Problem: TestHost_BraceParsersLoadAndParse only asserts a func child exists; does not validate block structure for edge cases (nested blocks, empty bodies, annotations)
  - Fix: Add corpus-driven test using testdata/corpus.json entries for new languages
  - Evidence: Test only checks &#96;hasFunc&#96; boolean; no assertion on child count, block nesting, or name correctness
- `internal/astgroup/parsers/src/braceparser/configs_test.go:122` — confidence MEDIUM, reviewers: dax
  - Problem: New language config tests cover happy paths only; no test for malformed input (unclosed triple-quote, unterminated block comment, empty source)
  - Fix: Add error-path tests: empty source, unclosed &#34;&#34;&#34; string, unclosed /* comment, file with only braces
  - Evidence: All new tests use syntactically valid source; no negative cases
- `internal/astgroup/parsers/src/braceparser/parse_core.go:43` — confidence MEDIUM, reviewers: dax
  - Problem: New tripleQuote config field has no test proving it interacts correctly with charLiterals/strChars when all are enabled simultaneously
  - Fix: Add test with tripleQuote+charLiterals+strChars enabled and source containing &#34;&#34;&#34;, &#39;, and &#34; in close proximity
  - Evidence: &#96;tripleQuote&#96; added to langConfig; no combinatorial test for feature interaction
- `internal/astgroup/parsers/src/braceparser/parse_core.go:248` — confidence MEDIUM, reviewers: dax
  - Problem: C# 11 raw strings with 4+ quotes desync state machine; documented limitation but no test proving graceful degradation
  - Fix: Add test with 4-quote raw string asserting parser does not panic and produces a valid (degraded) tree
  - Evidence: &#96;case cfg.tripleQuote &amp;&amp; matchAt(src, i, &#34;\&#34;\&#34;\&#34;&#34;)&#96; matches exactly 3 quotes; comment at line 248-253 documents the gap
- `internal/astgroup/parsers/src/braceparser/parse_core.go:515` — confidence MEDIUM, reviewers: dax
  - Problem: funcParenName reserved-word guard omits try/synchronized/using/lock/fixed; Java/C# control headers misclassify as named funcs
  - Fix: Extend reserved-word set with try/synchronized/using/lock/fixed and add test for each
  - Evidence: &#96;switch name { case &#34;catch&#34;, &#34;with&#34;, &#34;switch&#34;:&#96; at line 515-517; no guard for try/synchronized/using/lock/fixed

### LOW

- `internal/astgroup/embed.go:4` — confidence MEDIUM, reviewers: dax
  - Problem: Four new .wasm files added to embed.FS but no test verifies they are actually embedded and loadable
  - Fix: Add TestEmbeddedParsersMatchManifest entry for new parsers or extend existing test
  - Evidence: &#96;parserFS&#96; embed directive lists new files; TestEmbeddedParsersMatchManifest exists but may not cover new entries
- `internal/astgroup/parsers/src/braceparser/configs_test.go:122` — confidence MEDIUM, reviewers: dax
  - Problem: No test for C# verbatim strings (@&#34;...&#34;) which are documented as out-of-scope; regression could silently break
  - Fix: Add test proving verbatim string braces degrade to proximity rather than breaking parser
  - Evidence: &#96;csharpConfig&#96; comment documents verbatim strings as out of scope; no test guarding against regression
- `internal/astgroup/parsers/src/braceparser/parse_core.go:476` — confidence MEDIUM, reviewers: dax
  - Problem: funcParenName modifier allowlist removed; keyword-less statement-prefixed calls like &#96;return foo() {&#96; now name func &#34;foo&#34;
  - Fix: Add test proving &#96;return foo() {&#96; degrades to anonymous block (or document accepted behavior)
  - Evidence: Modifier allowlist removed at line 476-490; only &#96;.&#96; member-access guard remains
