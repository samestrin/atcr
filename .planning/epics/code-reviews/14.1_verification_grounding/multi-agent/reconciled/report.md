# atcr Reconciled Review

## Summary

- Total findings: 11
- Sources: pool
- Clusters collapsed: 0
- Severity disagreements: 0

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 1 | 0 |
| MEDIUM | 0 | 6 | 0 |
| LOW | 0 | 4 | 0 |

## Disagreements

Top 11 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/fanout/grounding.go:88` (HIGH) · score 3
- Reviewers: dax (independence 1)
- Problem: Evidence-snippet fallback uses bidirectional substring match with 4-char floor, over-retaining short fabricated evidence

### 2. solo_finding — `internal/fanout/grounding.go:47` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: groundFindings ignores Result.Tools and Result.PayloadMode, dropping tool/files agents&#39; legitimate citations outside changed range

### 3. solo_finding — `internal/fanout/grounding.go:54` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: out-of-scope blanket exemption allows fabricated finding with category=out-of-scope to bypass AC2 in diff/blocks mode

### 4. solo_finding — `internal/fanout/grounding.go:130` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: collapseSpaces uses strings.Fields which treats non-breaking spaces and other Unicode whitespace as fields; evidence with Unicode whitespace may not match

### 5. solo_finding — `internal/fanout/grounding_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test for evidence fallback with short boilerplate that should be rejected

### 6. solo_finding — `internal/fanout/grounding_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test for evidence fallback with whitespace/case normalization edge cases

### 7. solo_finding — `internal/fanout/grounding_wiring_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: TestWritePool_DropsUngrounded only asserts one grounded finding survives; does not assert the ungrounded one is absent by count

### 8. solo_finding — `internal/fanout/artifacts.go:59` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: WritePool/findingsFor/writeResumedAgents accept grounding data via variadic changed ...payload.ChangedLines, allowing silent omission

### 9. solo_finding — `internal/fanout/grounding_wiring_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: No test for WritePool with grounding data where all findings are ungrounded (empty pool)

### 10. solo_finding — `internal/payload/grounding.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: No test for BuildChangedLines error path (validateRange failure, diffChunks failure)

### 11. solo_finding — `internal/payload/grounding_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: parseFileChange tests use synthetic chunks; no test with real git diff output for edge cases

## Findings

### HIGH

- `internal/fanout/grounding.go:88` — confidence MEDIUM, reviewers: dax
  - Problem: Evidence-snippet fallback uses bidirectional substring match with 4-char floor, over-retaining short fabricated evidence
  - Fix: Use token-set Jaccard or raise evidenceMinMatch to tighten false-keep rate
  - Evidence: strings.Contains(ev, cl) // strings.Contains(cl, ev) with evidenceMinMatch=12

### MEDIUM

- `internal/fanout/grounding.go:47` — confidence MEDIUM, reviewers: dax
  - Problem: groundFindings ignores Result.Tools and Result.PayloadMode, dropping tool/files agents&#39; legitimate citations outside changed range
  - Fix: Thread Tools/PayloadMode into gate and relax range/evidence requirement for tool/files agents
  - Evidence: isGrounded only checks f.Category, f.File, f.Line, f.Evidence against changed map
- `internal/fanout/grounding.go:54` — confidence MEDIUM, reviewers: dax
  - Problem: out-of-scope blanket exemption allows fabricated finding with category=out-of-scope to bypass AC2 in diff/blocks mode
  - Fix: Restrict exemption to files mode or require FILE:LINE/EVIDENCE anchor before honoring
  - Evidence: strings.EqualFold(strings.TrimSpace(f.Category), reclib.CategoryOutOfScope) returns true unconditionally
- `internal/fanout/grounding.go:130` — confidence MEDIUM, reviewers: dax
  - Problem: collapseSpaces uses strings.Fields which treats non-breaking spaces and other Unicode whitespace as fields; evidence with Unicode whitespace may not match
  - Fix: Use regexp whitespace collapse or document Unicode whitespace behavior
  - Evidence: strings.Fields splits on unicode.IsSpace including \u00A0, \u2003, etc.
- `internal/fanout/grounding_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test for evidence fallback with short boilerplate that should be rejected
  - Fix: Add case where evidence is &#34;if err != nil {&#34; (13 chars, above floor but ubiquitous) and assert it does NOT ground a finding
  - Evidence: evidenceMinMatch=12 allows 13-char boilerplate to pass
- `internal/fanout/grounding_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test for evidence fallback with whitespace/case normalization edge cases
  - Fix: Add case with mixed indentation, extra spaces, and case differences in evidence vs changed text
  - Evidence: collapseSpaces and strings.ToLower tested only with exact match in TestGroundFindings_KeepsViaEvidence
- `internal/fanout/grounding_wiring_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: TestWritePool_DropsUngrounded only asserts one grounded finding survives; does not assert the ungrounded one is absent by count
  - Fix: Add explicit assertion that len(parsed.Findings)==1 and the dropped finding&#39;s line 999 is not present
  - Evidence: require.Len(t, parsed.Findings, 1) but no negative assertion on line 999

### LOW

- `internal/fanout/artifacts.go:59` — confidence MEDIUM, reviewers: dax
  - Problem: WritePool/findingsFor/writeResumedAgents accept grounding data via variadic changed ...payload.ChangedLines, allowing silent omission
  - Fix: Make Changed an explicit single nil-able parameter so grounding is a visible required argument
  - Evidence: func WritePool(poolDir string, results []Result, changed ...payload.ChangedLines)
- `internal/fanout/grounding_wiring_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test for WritePool with grounding data where all findings are ungrounded (empty pool)
  - Fix: Add case with all-unhallucinated findings and assert pool findings.txt is empty or has zero findings
  - Evidence: Only tests mixed grounded/ungrounded and no-grounding cases
- `internal/payload/grounding.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test for BuildChangedLines error path (validateRange failure, diffChunks failure)
  - Fix: Add test with invalid base/head refs asserting error return
  - Evidence: BuildChangedLines returns error from validateRange and diffChunks with no test coverage
- `internal/payload/grounding_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: parseFileChange tests use synthetic chunks; no test with real git diff output for edge cases
  - Fix: Add integration test with merge commit, rename, or empty diff
  - Evidence: TestParseFileChange_RangesAndText and TestParseFileChange_CommentPrefixedContent use hand-crafted strings
