# atcr Reconciled Review

## Summary

- Total findings: 9
- Sources: pool
- Clusters collapsed: 1
- Severity disagreements: 0
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 4 | 0 |
| LOW | 1 | 4 | 0 |

## Disagreements

Top 8 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/history/capture.go:44` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: RecordReview deduplicates findings by id within a run using first-occurrence-wins, but the pool findings.txt is a concatenation of per-reviewer rows with no guaranteed ordering; the &#34;first&#34; record&#39;s severity/category may not be the consensus severity

### 2. solo_finding — `internal/history/filter.go:25` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: ParseSince parses &#34;2w&#34; as 14 days using float64 multiplication (n * float64(per)), which can produce sub-nanosecond drift for large week counts due to IEEE 754 rounding

### 3. solo_finding — `internal/history/writer.go:21` (MEDIUM) · score 2
- Reviewers: otto (independence 1)
- Problem: Append assumes O_APPEND atomicity for multi-KB batches

### 4. solo_finding — `internal/history/writer.go:32` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: Append serializes all records to memory then writes with a single O_APPEND call, claiming POSIX atomicity; POSIX guarantees atomicity only for writes up to PIPE_BUF (typically 4096 bytes) on pipes/FIFOs, not regular files

### 5. solo_finding — `internal/history/capture_test.go:92` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: TestRecordReview_AppendsOneRecordPerPoolFinding asserts record count but does not verify that the second record&#39;s fields (Package, Severity, File, Category, ID) match the input

### 6. solo_finding — `internal/history/reader.go:28` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: Load skips malformed JSON lines silently with &#96;continue&#96;, so a ledger corrupted by a partial write silently drops records with no diagnostic; the caller (cmd/atcr/history.go) has no way to know data was lost

### 7. solo_finding — `internal/history/render.go:35` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: normalizeSeverity is a duplicate of reconcile.NormalizeSeverity

### 8. solo_finding — `internal/history/render.go:46` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: RenderTable normalizes empty severity to &#34;UNKNOWN&#34; but the canonical column set is fixed to CRITICAL/HIGH/MEDIUM/LOW; an &#34;UNKNOWN&#34; severity column is appended as an extra, but the normalizeSeverity call on line 46 is unreachable if the caller always passes a non-empty severity

## Findings

### MEDIUM

- `internal/history/capture.go:44` — confidence MEDIUM, reviewers: dax
  - Problem: RecordReview deduplicates findings by id within a run using first-occurrence-wins, but the pool findings.txt is a concatenation of per-reviewer rows with no guaranteed ordering; the &#34;first&#34; record&#39;s severity/category may not be the consensus severity
  - Fix: Document that first-occurrence-wins is intentional (the ledger stores run-time severity, not reconciled severity) or use the majority severity when multiple reviewers disagree
  - Evidence: &#96;seen[id] = true&#96; skips subsequent occurrences; no severity-resolution logic
- `internal/history/filter.go:25` — confidence MEDIUM, reviewers: dax
  - Problem: ParseSince parses &#34;2w&#34; as 14 days using float64 multiplication (n * float64(per)), which can produce sub-nanosecond drift for large week counts due to IEEE 754 rounding
  - Fix: Use integer arithmetic (n * per) after parsing n as an integer, or accept the drift as negligible and document it
  - Evidence: &#96;d = time.Duration(n * float64(per))&#96; at filter.go:35
- `internal/history/writer.go:21` — confidence MEDIUM, reviewers: otto
  - Problem: Append assumes O_APPEND atomicity for multi-KB batches
  - Fix: Use a file lock (flock) or a temporary-swap pattern to prevent interleaved JSONL lines during concurrent review runs
  - Evidence: The whole batch is serialized to memory first, then written with a single O_APPEND write() call.
- `internal/history/writer.go:32` — confidence MEDIUM, reviewers: dax
  - Problem: Append serializes all records to memory then writes with a single O_APPEND call, claiming POSIX atomicity; POSIX guarantees atomicity only for writes up to PIPE_BUF (typically 4096 bytes) on pipes/FIFOs, not regular files
  - Fix: Document the non-atomicity assumption or add advisory flock around the append, mirroring the mkdir-flock pattern used for the TD README
  - Evidence: &#96;// On a local POSIX filesystem that append is atomic&#96; comment at writer.go:32 is incorrect for regular files

### LOW

- `cmd/atcr/history.go:43` — confidence HIGH, reviewers: dax, otto
  - Problem: runHistory resolves .atcr relative to cwd (&#96;filepath.Join(&#34;.&#34;, &#34;.atcr&#34;, ...)&#96;) so running from a subdirectory silently reports no history; consistent with other commands but undocumented
  - Fix: Document the cwd requirement in the command help or resolve the repo root once for all commands
  - Evidence: [otto] histPath := filepath.Join(&#34;.&#34;, &#34;.atcr&#34;, &#34;findings-history.jsonl&#34;) / [dax] &#96;histPath := filepath.Join(&#34;.&#34;, &#34;.atcr&#34;, &#34;findings-history.jsonl&#34;)&#96; at history.go:44
- `internal/history/capture_test.go:92` — confidence MEDIUM, reviewers: dax
  - Problem: TestRecordReview_AppendsOneRecordPerPoolFinding asserts record count but does not verify that the second record&#39;s fields (Package, Severity, File, Category, ID) match the input
  - Fix: Add assertions for the second record&#39;s fields, mirroring the first-record assertions at lines 93-98
  - Evidence: Only recs[0] fields are asserted; recs[1] is only length-checked
- `internal/history/reader.go:28` — confidence MEDIUM, reviewers: dax
  - Problem: Load skips malformed JSON lines silently with &#96;continue&#96;, so a ledger corrupted by a partial write silently drops records with no diagnostic; the caller (cmd/atcr/history.go) has no way to know data was lost
  - Fix: Log a warning or return a count of skipped lines alongside the records so the CLI can surface a notice
  - Evidence: &#96;if err := json.Unmarshal(raw, &amp;rec); err != nil { continue }&#96; at reader.go:43
- `internal/history/render.go:35` — confidence MEDIUM, reviewers: otto
  - Problem: normalizeSeverity is a duplicate of reconcile.NormalizeSeverity
  - Fix: Import and use reconcile.NormalizeSeverity to ensure consistent casing logic
  - Evidence: func normalizeSeverity(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
- `internal/history/render.go:46` — confidence MEDIUM, reviewers: dax
  - Problem: RenderTable normalizes empty severity to &#34;UNKNOWN&#34; but the canonical column set is fixed to CRITICAL/HIGH/MEDIUM/LOW; an &#34;UNKNOWN&#34; severity column is appended as an extra, but the normalizeSeverity call on line 46 is unreachable if the caller always passes a non-empty severity
  - Fix: Remove the dead normalization branch or add a test proving it is reachable with an empty severity record
  - Evidence: &#96;if sev == &#34;&#34; { sev = &#34;UNKNOWN&#34; }&#96; has no test coverage
