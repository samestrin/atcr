# atcr Reconciled Review

## Summary

- Total findings: 7
- Sources: pool
- Clusters collapsed: 1
- Severity disagreements: 0
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 3 | 0 |
| LOW | 1 | 3 | 0 |

## Disagreements

Top 6 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/astgroup/parsers/src/pyparser/main.go:193` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: (scanLine) Missing test for triple-quote inside single-line string of the same quote type

### 2. solo_finding — `internal/astgroup/parsers/src/pyparser/main.go:198` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: (scanLine) Escape handling inside single-line string (&#96;case &#39;\\&#39;: i += 2&#96;) has no test coverage

### 3. solo_finding — `internal/astgroup/parsers/src/pyparser/main.go:228` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: (scanLine) No test for &#96;#&#96; inside a triple-quoted (multi-line) string

### 4. solo_finding — `internal/astgroup/parsers/src/pyparser/main.go:199` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: (scanLine) Backslash escape &#96;i += 2&#96; may skip past end of line without bounds check; boundary untested

### 5. solo_finding — `internal/astgroup/parsers/src/pyparser/main.go:221` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: (scanLine) Comment restates the a-priori objective of the epic

### 6. solo_finding — `internal/astgroup/parsers/src/pyparser/main.go:234` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: Comment restates the a-priori objective of the epic

## Findings

### MEDIUM

- `internal/astgroup/parsers/src/pyparser/main.go:193` — confidence MEDIUM, reviewers: dax
  - Problem: (scanLine) Missing test for triple-quote inside single-line string of the same quote type
  - Fix: Add test with &#96;&#34;text &#34;&#34;&#34;&#96; or &#96;&#39;text &#39;&#39;&#39;&#96; to ensure same-quote triple-quote does not flip state
  - Evidence: &#96;if q != 0&#96; at line 193 guards against triple-quote detection; &#96;TestHost_PyParseTripleQuoteInsideString&#96; only tests &#96;&#34;&#96; with &#96;&#39;&#39;&#39;&#96;, not &#96;&#34;&#96; with &#96;&#34;&#34;&#34;&#96;
- `internal/astgroup/parsers/src/pyparser/main.go:198` — confidence MEDIUM, reviewers: dax
  - Problem: (scanLine) Escape handling inside single-line string (&#96;case &#39;\\&#39;: i += 2&#96;) has no test coverage
  - Fix: Add test with escaped quote inside string containing &#96;#&#96; to exercise escape logic
  - Evidence: &#96;case &#39;\\&#39;: i += 2&#96; at line 198 is never executed; &#96;TestHost_PyParseHashInsideString&#96; uses unescaped string
- `internal/astgroup/parsers/src/pyparser/main.go:228` — confidence MEDIUM, reviewers: dax
  - Problem: (scanLine) No test for &#96;#&#96; inside a triple-quoted (multi-line) string
  - Fix: Add test with a triple-quoted string containing &#96;#&#96; to verify it is not treated as comment
  - Evidence: &#96;func scanTripleQuotes&#96; at line 228 calls &#96;scanLine&#96; which ignores &#96;#&#96; inside &#96;delim != &#34;&#34;&#96;; no test exercises this

### LOW

- `internal/astgroup/parsers/src/pyparser/main.go:171` — confidence HIGH, reviewers: dax, otto
  - Problem: &#96;stripComment&#96; now uses &#96;scanLine&#96; but no test for comment after a closed string
  - Fix: Add test like &#96;x = &#34;a&#34; # comment&#96; to verify comment stripping still works
  - Evidence: [otto] &#96;...is quote- and escape-aware (epic 22.3)&#96; / [dax] &#96;stripComment&#96; at line 333 calls &#96;scanLine&#96;; no test verifies comment after closed string is stripped
- `internal/astgroup/parsers/src/pyparser/main.go:199` — confidence MEDIUM, reviewers: dax
  - Problem: (scanLine) Backslash escape &#96;i += 2&#96; may skip past end of line without bounds check; boundary untested
  - Fix: Add test with backslash at end of line inside string to verify safe handling
  - Evidence: &#96;i += 2&#96; at line 199 after &#96;case &#39;\\&#39;:&#96;; no test for line ending with backslash inside string
- `internal/astgroup/parsers/src/pyparser/main.go:221` — confidence MEDIUM, reviewers: otto
  - Problem: (scanLine) Comment restates the a-priori objective of the epic
  - Fix: Remove &#34;(epic 22.3)&#34; from the comment
  - Evidence: &#96;...appearing inside a # comment (epic 22.3)&#96;
- `internal/astgroup/parsers/src/pyparser/main.go:234` — confidence MEDIUM, reviewers: otto
  - Problem: Comment restates the a-priori objective of the epic
  - Fix: Remove &#34;(epic 22.3)&#34; from the comment
  - Evidence: &#96;...string span on this line is treated as string content, not a comment start (epic 22.3)&#96;
