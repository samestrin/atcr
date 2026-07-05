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
| HIGH | 0 | 2 | 0 |
| MEDIUM | 0 | 4 | 0 |
| LOW | 0 | 4 | 0 |

## Disagreements

Top 10 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `cmd/atcr/audit_report.go:47` (HIGH) · score 3
- Reviewers: mira (independence 1)
- Problem: --pr 0 matches every no-PR run because omitempty serializes PR=0 records without a &#34;pr&#34; field, so the loaded records all carry PR=0 and the filter r.PR == pr (pr=0) selects all of them

### 2. solo_finding — `cmd/atcr/review.go:387` (HIGH) · score 3
- Reviewers: mira (independence 1)
- Problem: cross-directory write/read split: review writes via filepath.Join(req.Root,...) where req.Root=&#34;.&#34; (CWD-relative) while audit-report reads via repoRoot() walk-up — a review launched from a subdirectory of the git root writes records the report will never find

### 3. solo_finding — `cmd/atcr/review.go:115` (MEDIUM) · score 2
- Reviewers: otto (independence 1)
- Problem: prNumberFromFlags() uses os.Getenv() directly

### 4. solo_finding — `cmd/atcr/review.go:390` (MEDIUM) · score 2
- Reviewers: mira (independence 1)
- Problem: audit write failure is swallowed as Warn and review still exits 0, so a systematically broken compliance ledger (full disk, read-only fs, .atcr/ perm denied) leaves runs reporting success with only a log line that CI rarely surfaces

### 5. solo_finding — `internal/audit/reader.go:60` (MEDIUM) · score 2
- Reviewers: mira (independence 1)
- Problem: oversized-line recovery uses unbounded bufio.NewReader.ReadString(&#39;\n&#39;) which grows the buffer until a newline or EOF — a tampered or corrupted ledger with a multi-GB run-on line OOMs the audit-report command

### 6. solo_finding — `internal/audit/writer.go:46` (MEDIUM) · score 2
- Reviewers: mira (independence 1)
- Problem: Append does not fsync, so a crash or power loss between f.Write and Close loses the most recent record — undermining the &#34;tamper-evident compliance ledger&#34; framing in record.go and CHANGELOG

### 7. solo_finding — `cmd/atcr/audit_pr_test.go:45` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: intToStr() is a manual implementation of strconv.Itoa

### 8. solo_finding — `cmd/atcr/audit_report.go:39` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: RunE ignores error from fmt.Fprint

### 9. solo_finding — `internal/audit/capture.go:75` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: finding key uses null bytes as separators

### 10. solo_finding — `internal/audit/render.go:118` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: RenderReport uses a map for counts without ensuring key existence for canonical columns

## Findings

### HIGH

- `cmd/atcr/audit_report.go:47` — confidence MEDIUM, reviewers: mira
  - Problem: --pr 0 matches every no-PR run because omitempty serializes PR=0 records without a &#34;pr&#34; field, so the loaded records all carry PR=0 and the filter r.PR == pr (pr=0) selects all of them
  - Fix: Reject pr &lt;= 0 after GetInt via a usageError; MarkFlagRequired only checks presence not value
  - Evidence: filter &#96;if r.PR == pr { forPR = append(forPR, r) }&#96; with pr=0 returns every local (no-PR) record under a bogus &#34;PR #0&#34; report
- `cmd/atcr/review.go:387` — confidence MEDIUM, reviewers: mira
  - Problem: cross-directory write/read split: review writes via filepath.Join(req.Root,...) where req.Root=&#34;.&#34; (CWD-relative) while audit-report reads via repoRoot() walk-up — a review launched from a subdirectory of the git root writes records the report will never find
  - Fix: Resolve auditPath through repoRoot() in runReview/runResume, matching audit-report
  - Evidence: auditPath uses req.Root=&#34;.&#34;; audit_report.go:34 uses repoRoot(). On CI run from &lt;root&gt;/subdir, the two paths diverge and AC1/AC2 silently fail

### MEDIUM

- `cmd/atcr/review.go:115` — confidence MEDIUM, reviewers: otto
  - Problem: prNumberFromFlags() uses os.Getenv() directly
  - Fix: Pass the environment map or a getter to the function to make it testable without global state
  - Evidence: return prFromGitHubRef(os.Getenv(&#34;GITHUB_REF&#34;))
- `cmd/atcr/review.go:390` — confidence MEDIUM, reviewers: mira
  - Problem: audit write failure is swallowed as Warn and review still exits 0, so a systematically broken compliance ledger (full disk, read-only fs, .atcr/ perm denied) leaves runs reporting success with only a log line that CI rarely surfaces
  - Fix: Mirror the warn on stderr from the CLI so the compliance-write failure is visible regardless of log-level capture
  - Evidence: log.FromContext(ctx).Warn(&#34;failed to append audit record&#34;, &#34;error&#34;, aerr) then control falls through to the gate path and a successful exit
- `internal/audit/reader.go:60` — confidence MEDIUM, reviewers: mira
  - Problem: oversized-line recovery uses unbounded bufio.NewReader.ReadString(&#39;\n&#39;) which grows the buffer until a newline or EOF — a tampered or corrupted ledger with a multi-GB run-on line OOMs the audit-report command
  - Fix: Read fixed-size chunks and split on &#39;\n&#39; with a per-line cap (e.g. 4MiB) so any further oversized line is truncated + skipped with a stderr note
  - Evidence: for { line, rerr := r.ReadString(&#39;\n&#39;); ... } allocates without limit on tampered input
- `internal/audit/writer.go:46` — confidence MEDIUM, reviewers: mira
  - Problem: Append does not fsync, so a crash or power loss between f.Write and Close loses the most recent record — undermining the &#34;tamper-evident compliance ledger&#34; framing in record.go and CHANGELOG
  - Fix: Add an explicit f.Sync() before Close

### LOW

- `cmd/atcr/audit_pr_test.go:45` — confidence MEDIUM, reviewers: otto
  - Problem: intToStr() is a manual implementation of strconv.Itoa
  - Fix: Use strconv.Itoa
  - Evidence: manual byte-slice loop for integer conversion
- `cmd/atcr/audit_report.go:39` — confidence MEDIUM, reviewers: otto
  - Problem: RunE ignores error from fmt.Fprint
  - Fix: Use a proper logger or handle the return error if the output stream is closed
  - Evidence: _, _ = fmt.Fprint(cmd.OutOrStdout(), audit.RenderReport(forPR, pr, time.Now()))
- `internal/audit/capture.go:75` — confidence MEDIUM, reviewers: otto
  - Problem: finding key uses null bytes as separators
  - Fix: Use a structured key or a joined string with a standard delimiter like a pipe
  - Evidence: key := f.File + &#34;\x00&#34; + strconv.Itoa(f.Line) + &#34;\x00&#34; + f.Problem
- `internal/audit/render.go:118` — confidence MEDIUM, reviewers: otto
  - Problem: RenderReport uses a map for counts without ensuring key existence for canonical columns
  - Fix: The logic is correct but relies on the zero-value of int; a more explicit check or default value would improve readability
  - Evidence: n := counts[c]
