# atcr Reconciled Review

## Summary

- Total findings: 10
- Sources: pool
- Clusters collapsed: 0
- Severity disagreements: 0
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 8 | 0 |
| LOW | 0 | 2 | 0 |

## Disagreements

Top 10 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/fanout/response_truncation_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test covers invokeSingleShot fallback to UsageCompleter when MetaCompleter is absent

### 2. solo_finding — `internal/fanout/response_truncation_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test covers invokeSingleShot fallback to plain Complete when neither MetaCompleter nor UsageCompleter is present

### 3. solo_finding — `internal/fanout/response_truncation_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test covers invokeSlot truncation failover when the primary is truncated-empty AND the fallback also truncates empty

### 4. solo_finding — `internal/fanout/response_truncation_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test covers the MetaCompleter error path in invokeSingleShot

### 5. solo_finding — `internal/fanout/response_truncation_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test covers the tool-loop path where requestFinalAnswer returns a truncated response

### 6. solo_finding — `internal/llmclient/truncation_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test covers CompleteWithMeta when JSON unmarshal fails

### 7. solo_finding — `internal/llmclient/truncation_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test covers CompleteWithMeta when the response has an empty choices array

### 8. solo_finding — `internal/verify/executor_truncation_test.go:1` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: No test covers the snippet-path truncation when the completer does NOT implement metaCompleter

### 9. solo_finding — `internal/fanout/response_truncation_e2e_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: E2E test for scenario (a) does not assert the primary&#39;s ResponseTruncated marker is preserved in status.json

### 10. solo_finding — `internal/fanout/response_truncation_test.go:1` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: TestWritePool_CountsTruncatedZeroFindings passes a nil changed to WritePool but the grounding gate is never tested

## Findings

### MEDIUM

- `internal/fanout/response_truncation_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test covers invokeSingleShot fallback to UsageCompleter when MetaCompleter is absent
  - Fix: Add test with a UsageCompleter-only mock and assert the result carries content and usage but ResponseTruncated=false
  - Evidence: the else-if branch at engine.go invokeSingleShot is exercised only by TestSingleShot_UsageOnlyCompleterLeavesTruncationFalse which uses a usageCompleter that returns empty content — the content+usage path is untested
- `internal/fanout/response_truncation_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test covers invokeSingleShot fallback to plain Complete when neither MetaCompleter nor UsageCompleter is present
  - Fix: Add test with a Completer-only mock and assert StatusOK with ResponseTruncated=false
  - Evidence: the final else branch at engine.go invokeSingleShot (content, err = e.completer.Complete(...)) has zero coverage
- `internal/fanout/response_truncation_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test covers invokeSlot truncation failover when the primary is truncated-empty AND the fallback also truncates empty
  - Fix: Add test with both primary and fallback returning truncated-empty and assert StatusFailed
  - Evidence: TestInvokeSlot_AllTruncatedEmpty_SlotFails covers this but does not assert the fallback&#39;s ResponseTruncated is true — only the last result&#39;s marker is checked
- `internal/fanout/response_truncation_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test covers the MetaCompleter error path in invokeSingleShot
  - Fix: Add test where CompleteWithMeta returns an error and assert Result.ResponseTruncated is false
  - Evidence: invokeSingleShot sets ResponseTruncated from comp.Truncated only on the success path; the error branch (line ~700) returns r without ever setting ResponseTruncated — it stays false by zero value, which is correct but untested
- `internal/fanout/response_truncation_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test covers the tool-loop path where requestFinalAnswer returns a truncated response
  - Fix: Add test where the loop trips a budget, requestFinalAnswer fires, and the Chat response has Truncated=true
  - Evidence: requestFinalAnswer sets l.res.ResponseTruncated = resp.Truncated but no test drives a budget trip that reaches this assignment
- `internal/llmclient/truncation_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test covers CompleteWithMeta when JSON unmarshal fails
  - Fix: Add test with a malformed JSON body and assert error + CallRecords populated
  - Evidence: the json.Unmarshal error path in CompleteWithMeta returns Completion{CallRecords: records} — untested
- `internal/llmclient/truncation_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test covers CompleteWithMeta when the response has an empty choices array
  - Fix: Add test with choices:[] and assert an error is returned with Truncated=false
  - Evidence: the len(parsed.Choices)==0 error path in CompleteWithMeta returns Completion{CallRecords: records} with Truncated=false — untested
- `internal/verify/executor_truncation_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: No test covers the snippet-path truncation when the completer does NOT implement metaCompleter
  - Fix: Add test with an executorCompleter-only mock and assert the fix lands normally with no truncation flag
  - Evidence: callExecutor&#39;s else branch (content, err := complete.Complete(...)) returns truncated=false — untested

### LOW

- `internal/fanout/response_truncation_e2e_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: E2E test for scenario (a) does not assert the primary&#39;s ResponseTruncated marker is preserved in status.json
  - Fix: Add assertion that the primary agent&#39;s ResponseTruncated=true even though the fallback rescued the slot
  - Evidence: the test reads the fallback agent&#39;s status.json but never checks whether the primary&#39;s truncated marker was recorded
- `internal/fanout/response_truncation_test.go:1` — confidence MEDIUM, reviewers: dax
  - Problem: TestWritePool_CountsTruncatedZeroFindings passes a nil changed to WritePool but the grounding gate is never tested
  - Fix: Add a variant with non-empty changed to assert GroundingEnabled is true in the summary
  - Evidence: WritePool with nil changed sets groundingEnabled=false; the test never asserts the GroundingEnabled field
