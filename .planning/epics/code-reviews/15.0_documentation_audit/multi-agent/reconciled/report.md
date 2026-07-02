# atcr Reconciled Review

## Summary

- Total findings: 6
- Sources: pool
- Clusters collapsed: 5
- Severity disagreements: 4
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 4 | 1 | 0 |
| LOW | 1 | 0 | 0 |

## Disagreements

Top 5 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. severity_split — `cmd/atcr/docs_audit_test.go:176` (MEDIUM) · score 4
- Severity disagreement: LOW vs MEDIUM
- Reviewers: brad, dax, greta, otto (independence 4)
- Problem: TestReconcilerConfigSurfaceDocumented uses strings.Contains for persona:/debate:/verify:/executor:, matching those tokens in prose as readily as in a real yaml block

### 2. severity_split — `cmd/atcr/docs_audit_test.go:216` (MEDIUM) · score 4
- Severity disagreement: LOW vs MEDIUM
- Reviewers: brad, dax, greta, otto (independence 4)
- Problem: TestDocsIndexCoversEveryDoc link regex treats a titled markdown link &#96;](x.md &#34;title&#34;)&#96; as target &#96;x.md &#34;title&#34;&#96;, which fails the .md suffix check and would false-fail if a future index link adds a title

### 3. severity_split — `cmd/atcr/docs_audit_test.go:262` (MEDIUM) · score 4
- Severity disagreement: LOW vs MEDIUM
- Reviewers: brad, dax, greta, otto (independence 4)
- Problem: TestArchitectureDocDescribesReconciler comment claims a fictional-architecture stub would fail, but it only checks 8 substrings exist anywhere, so a buzzword-sprinkled stub passes

### 4. severity_split — `cmd/atcr/docs_audit_test.go:71` (MEDIUM) · score 3
- Severity disagreement: LOW vs MEDIUM
- Reviewers: brad, dax, otto (independence 3)
- Problem: Fenced-block state toggles on any line starting with &#96;&#96;&#96; or ~~~, so a code example containing a line like echo &#34;&#96;&#96;&#96;&#34; incorrectly closes the fence and exposes subsequent prose to command parsing

### 5. solo_finding — `cmd/atcr/docs_audit_test.go:79` (MEDIUM) · score 2
- Reviewers: greta (independence 1)
- Problem: Fence toggle uses inFence=!inFence without tracking info-strings or handling unbalanced fences, leaving state true for EOF and parsing prose as commands

## Findings

### MEDIUM

- `cmd/atcr/docs_audit_test.go:71` — confidence HIGH, reviewers: brad, dax, otto
  - Severity disagreement: LOW vs MEDIUM
  - Problem: Fenced-block state toggles on any line starting with &#96;&#96;&#96; or ~~~, so a code example containing a line like echo &#34;&#96;&#96;&#96;&#34; incorrectly closes the fence and exposes subsequent prose to command parsing
  - Fix: Restrict fenced-line matching to shell-prompt ($) lines or require a following flag/argument token
  - Evidence: [brad] 20/if strings.HasPrefix(trimmed, &#34;&#96;&#96;&#96;&#34;) // strings.HasPrefix(trimmed, &#34;~~~&#34;) { inFence = !inFence } toggles on any matching prefix regardless of context / [dax] &#96;if strings.HasPrefix(s, &#34;atcr &#34;)&#96; inside &#96;capture&#96; matches any line starting with atcr inside a fence, not just commands / [otto] &#96;if strings.HasPrefix(s, &#34;atcr &#34;)&#96; inside a fence loop
- `cmd/atcr/docs_audit_test.go:79` — confidence MEDIUM, reviewers: greta
  - Problem: Fence toggle uses inFence=!inFence without tracking info-strings or handling unbalanced fences, leaving state true for EOF and parsing prose as commands
  - Fix: Track fence depth or match explicit closing fences to prevent state leakage on malformed input
  - Evidence: if strings.HasPrefix(trimmed, &#34;&#96;&#96;&#96;&#34;) // strings.HasPrefix(trimmed, &#34;~~~&#34;) { inFence = !inFence; continue }
- `cmd/atcr/docs_audit_test.go:176` — confidence HIGH, reviewers: brad, dax, greta, otto
  - Severity disagreement: LOW vs MEDIUM
  - Problem: TestReconcilerConfigSurfaceDocumented uses strings.Contains for persona:/debate:/verify:/executor:, matching those tokens in prose as readily as in a real yaml block
  - Fix: Anchor the check to fenced-yaml lines or a start-of-line block pattern rather than a bare substring
  - Evidence: [brad] strings.Contains(ref, block) matches prose occurrences of persona:, debate:, etc. / [dax] &#96;strings.Contains(ref, block)&#96; matches &#96;persona:&#96; anywhere in the doc, not just in a config block / [greta] if !strings.Contains(ref, block) { t.Errorf(&#34;docs/registry.md does not document the %s config block&#34;, block) } / [otto] &#96;for _, block := range []string{&#34;persona:&#34;, ...} { if !strings.Contains(ref, block) }&#96;
- `cmd/atcr/docs_audit_test.go:216` — confidence HIGH, reviewers: brad, dax, greta, otto
  - Severity disagreement: LOW vs MEDIUM
  - Problem: TestDocsIndexCoversEveryDoc link regex treats a titled markdown link &#96;](x.md &#34;title&#34;)&#96; as target &#96;x.md &#34;title&#34;&#96;, which fails the .md suffix check and would false-fail if a future index link adds a title
  - Fix: Strip a trailing quoted title from the captured target before the .md/anchor checks
  - Evidence: [brad] re := regexp.MustCompile(&#96;\]\(([^)]+)\)&#96;) captures x.md &#34;title&#34;, strings.HasSuffix(target, &#34;.md&#34;) rejects it / [dax] &#96;\]\(([^)]+)\)&#96; captures everything up to the closing paren, including a quoted title / [greta] re := regexp.MustCompile(&#96;\]\(([^)]+)\)&#96;) / [otto] &#96;target = strings.SplitN(target, &#34;#&#34;, 2)[0]&#96; does not handle titles
- `cmd/atcr/docs_audit_test.go:262` — confidence HIGH, reviewers: brad, dax, greta, otto
  - Severity disagreement: LOW vs MEDIUM
  - Problem: TestArchitectureDocDescribesReconciler comment claims a fictional-architecture stub would fail, but it only checks 8 substrings exist anywhere, so a buzzword-sprinkled stub passes
  - Fix: Strengthen assertion to verify stage ordering, headings, or structural markers instead of bare substring presence
  - Evidence: [brad] for _, term := range []string{&#34;review&#34;, &#34;reconcile&#34;...} { if !strings.Contains(lower, term) ... } / [dax] &#96;strings.Contains(lower, term)&#96; for 8 terms; a doc containing only those words in any order passes / [greta] for _, term := range []string{&#34;review&#34;, &#34;reconcile&#34;, &#34;cluster&#34;, &#34;dedupe&#34;, &#34;confidence&#34;, &#34;verify&#34;, &#34;debate&#34;, &#34;persona&#34;} { if !strings.Contains(lower, term) { ... } / [otto] &#96;for _, term := range []string{...} { if !strings.Contains(lower, term) }&#96;

### LOW

- `cmd/atcr/docs_audit_test.go:53` — confidence HIGH, reviewers: brad, dax, greta, otto
  - Problem: Root README.md is folded into the atcr.yaml/Reconciler-v2/command/flag audit, so an unrelated future README edit could fail CI in a docs-audit test and surprise contributors
  - Fix: Scope drift-token and command checks to docs/ only, or explicitly document the coupling in the test header
  - Evidence: [brad] paths := append(docs, filepath.Join(root, &#34;README.md&#34;)) folds root README into auditedMarkdown used by all audit tests / [dax] &#96;auditedMarkdown&#96; includes root README.md; all five audit tests iterate over it / [otto] &#96;paths := append(docs, filepath.Join(root, &#34;README.md&#34;))&#96; / [greta] for _, m := range regexp.MustCompile(&#34;&#96;([^&#96;\n]+)&#96;&#34;).FindAllStringSubmatch(md, -1) { capture(m[1]) }
