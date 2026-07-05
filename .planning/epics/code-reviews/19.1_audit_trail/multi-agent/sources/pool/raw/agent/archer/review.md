Let me analyze the diff carefully for snippet-level bugs.

Looking at `cmd/atcr/audit_pr_test.go`:
- The test uses `intToStr` helper which has a subtle bug: when `n == 0`, it returns "0" early. But for negative numbers, it negates and processes. Let me check... Actually wait, the test only uses positive values (1234) and 0. The function seems fine for those cases.

Looking at `cmd/atcr/audit_report.go`:
- `MarkFlagRequired("pr")` only checks the flag is set, not its value. So `--pr 0` would pass the required check but match every non-PR run (PR omitted via omitempty). This is actually noted in the technical-debt README as a known issue. But wait, let me look more carefully - the TD item says "MarkFlagRequired only checks --pr is set not its value so `audit-report --pr 0` matches every non-PR run". This is a real bug but it's already documented as TD. Still, it's a real bug in the changed code.

Looking at `cmd/atcr/review.go` `prNumberFromFlags`:
```go
func prNumberFromFlags(cmd *cobra.Command) int {
	if cmd.Flags().Changed("pr") {
		if n, _ := cmd.Flags().GetInt("pr"); n > 0 {
			return n
		}
	}
	return prFromGitHubRef(os.Getenv("GITHUB_REF"))
}
```
This means `--pr 0` falls back to GITHUB_REF. That's intentional per the test "flag zero falls back to env". OK.

Looking at `prFromGitHubRef`:
```go
func prFromGitHubRef(ref string) int {
	const prefix = "refs/pull/"
	if !strings.HasPrefix(ref, prefix) {
		return 0
	}
	rest := ref[len(prefix):]
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:slash])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
```
`slash <= 0` - if slash is 0, that means `refs/pull//merge` which would have empty PR number. That's correctly rejected. If slash is -1 (not found), also rejected. Looks correct.

Looking at `internal/audit/capture.go` `summarize`:
```go
highest := make(map[string]string, len(res.Findings))
for _, f := range res.Findings {
	key := f.File + "\x00" + strconv.Itoa(f.Line) + "\x00" + f.Problem
	if prev, ok := highest[key]; ok {
		if stream.SeverityRank[stream.NormalizeSeverity(f.Severity)] > stream.SeverityRank[stream.NormalizeSeverity(prev)] {
			highest[key] = f.Severity
		}
		continue
	}
	highest[key] = f.Severity
}
```
This stores `f.Severity` (raw) in `highest[key]`, but compares using `NormalizeSeverity`. So if the first occurrence is "high" (lowercase) and the second is "HIGH", the comparison would be equal (both normalize to HIGH), so it wouldn't update. That's fine since they're equal rank. But what if SeverityRank doesn't contain a key? Let me think... if `NormalizeSeverity` returns something not in `SeverityRank`, the map lookup returns 0, so all unknown severities rank 0. That could be a subtle issue but probably not a bug per se.

Wait, there's a subtle issue: it stores the raw `f.Severity` not the normalized one. Then later:
```go
counts := make(map[string]int, 4)
for _, sev := range highest {
	counts[stream.NormalizeSeverity(sev)]++
}
```
It normalizes when counting. So the final counts are normalized. OK, that's fine.

But wait - the comparison uses `stream.SeverityRank[stream.NormalizeSeverity(f.Severity)]`. If `NormalizeSeverity` returns a value not in `SeverityRank`, this returns 0. So an unknown severity would rank 0, same as... well, what's the lowest? If LOW ranks 1 and unknown ranks 0, then unknown < LOW. That seems intentional.

Looking at `internal/audit/reader.go`:
```go
if err := sc.Err(); err != nil {
    if !errors.Is(err, bufio.ErrTooLong) {
        return nil, fmt.Errorf("reading audit ledger: %w", err)
    }
    // The scanner stopped at an oversized line. Continue with a reader...
    r := bufio.NewReader(f)
    for {
        line, rerr := r.ReadString('\n')
        ...
    }
}
```
Wait - after the scanner hits `ErrTooLong`, the file position `f` is past the point where the scanner stopped. The scanner reads in chunks. When it hits `ErrTooLong`, it has read up to its buffer capacity but not consumed the rest of the oversized line. So `f`'s position is somewhere in the middle of the oversized line. Then `bufio.NewReader(f)` starts reading from that position, NOT from the beginning of the oversized line or the next line.

Actually, let me think more carefully. `bufio.Scanner` reads from the underlying reader. When it encounters `ErrTooLong`, it has read bytes into its buffer but hasn't advanced past the full line. The file's read position is advanced by however many bytes the scanner read from the file (which could be more than the buffer size due to internal buffering).

So when we create `bufio.NewReader(f)` and call `ReadString('\n')`, we start reading from wherever `f` currently is, which is somewhere in the middle of the oversized line (or possibly past it, depending on buffering). This means we'd get a partial line (the remainder of the oversized line after the scanner's buffer), not the full next line.

Actually, the scanner's `Scan()` would have read the file in blocks. The `bufio.Reader` underlying the scanner reads chunks. When `ErrTooLong` occurs, the scanner's internal reader has buffered some data. The file position `f` is advanced by the bytes the underlying reader read, which may be ahead of what the scanner has processed.

This is a real bug: after `ErrTooLong`, continuing to read from `f` with a new `bufio.NewReader` will likely miss data or read partial lines. The records "around" the oversized line may not be correctly recovered.

But wait, the test `TestLoad_RecoversRecordsAroundOversizedLine` passes with 2 records recovered. Let me think about why...

The test writes:
```
rec + "\n"  (small line)
strings.Repeat("x", 2*1024*1024) + "\n"  (2MB line)
rec + "\n"  (small line)
```

The scanner has a 1MiB max token. It reads the first line fine (advances past it). Then it starts reading the 2MB line. The scanner's underlying `bufio.Reader` has a default buffer size... actually `sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)` sets the initial buffer to 64KB and max to 1MiB. 

The scanner reads from the file via its internal `bufio.Reader` (created by `bufio.NewScanner`). The internal reader reads from the file in chunks. When the token exceeds 1MiB, `ErrTooLong` is returned.

The file position after `ErrTooLong`: the internal `bufio.Reader` may have read ahead. The file position `f` could be anywhere - it depends on how much the internal reader buffered. For a 2MB line, the internal reader (default 4KB buffer) would have read multiple chunks. But the scanner's token buffer grows up to 1MiB. 

Actually, the key insight: `bufio.Scanner` uses an internal `bufio.Reader` with default buffer size 4KB. When scanning, it calls `r.Read()` repeatedly to fill the token. Each `r.Read()` may read from the underlying file. After `ErrTooLong`, the file position is wherever the last `Read()` left it.

For a 2MB line: the scanner would read ~2MB of data into its growing buffer (up to 1MiB max), then hit `ErrTooLong`. The file position `f` would be at approximately 2MB into the file (or wherever the last read chunk ended). 

Then `bufio.NewReader(f).ReadString('\n')` reads from that position. If the file position is at, say, 1MiB into the 2MB line, then `ReadString('\n')` reads the remaining ~1MB of x's plus the newline, returning that partial chunk. Then the next `ReadString('\n')` reads the third line (the good record). So the partial chunk (1MB of x's) would fail JSON parse and be skipped, and the third record would be recovered.

But wait - the first record was already added to `records` before the scanner hit the oversized line. So we have: record 1 (from scanner), then partial oversized chunk (skipped), then record 3 (from the reader loop). That gives 2 records. The test expects 2 records. So the test passes!

But is this actually correct behavior? The test passes, but the behavior depends on the internal buffering of `bufio.Scanner`. If the file position after `ErrTooLong` happens to be past the entire oversized line (because the internal reader read ahead), then the next `ReadString('\n')` would read the third line correctly. If it's in the middle, it reads a partial chunk (skipped) then the third line. Either way, the third record is recovered.

Actually, there's a subtle issue: what if the file position is exactly at the start of the third line? Then `ReadString('\n')` reads the third line, and there's no fourth line, so the next call returns `io.EOF`. That works.

What if the file position is in the middle of the third line? That can't happen because the third line is small and would have been read in the same chunk as the end of the oversized line.

Hmm, actually I think this might work in practice but it's fragile. The real issue is: after `bufio.Scanner` hits `ErrTooLong`, the file position is undefined relative to the logical line boundaries. The recovery code assumes it can just continue reading from `f` and pick up where it left off, but it might miss bytes or read partial lines.

Actually, let me reconsider. The more I think about it, the more I think this is a genuine bug, but it might work by luck in the test case. Let me flag it.

Actually, wait. Let me re-examine. `bufio.Scanner` wraps the reader in a `bufio.Reader` (internal). The `bufio.Reader` reads from the file in chunks of its buffer size (default 4096). When the scanner needs more data for a token, it calls `r.Read()` which reads from the file. After `ErrTooLong`, the `bufio.Reader` may have buffered some data that hasn't been consumed by the scanner.

But here's the thing: we're creating a NEW `bufio.NewReader(f)`. The old `bufio.Reader` (internal to the scanner) is discarded, and any data it had buffered is lost. So we lose whatever the scanner's internal reader had buffered but not yet returned.

For the test case: the file is `rec\n` + `2MB of x\n` + `rec\n`. The scanner reads `rec\n` (first line, success). Then it starts reading the 2MB line. The scanner's internal `bufio.Reader` reads 4KB chunks from the file. The scanner grows its token buffer up to 1MiB. After reading ~1MiB, it hits `ErrTooLong`. At this point, the internal `bufio.Reader` may have read ahead (it reads 4KB at a time, and the scanner may have requested multiple reads). The file position `f` is at wherever the last `read()` syscall left it.

If the file is small enough (2MB + 2 small lines ≈ 2MB), the OS might return all data in one or two reads. In that case, `f`'s position would be at the end of the file. Then `bufio.NewReader(f).ReadString('\n')` returns `io.EOF` immediately (nothing left to read), and we get 0 additional records. But we already have 1 from the scanner. Total: 1, not 2. The test would fail!

But the test passes (it's written to pass). So either:
1. The file is large enough that the OS doesn't return it all at once, OR
2. The `bufio.Reader` internal to the scanner doesn't read ahead that much, OR
3. Something else is going on.

Actually, `bufio.Reader.Read()` reads up to its buffer size (4KB default) per call. The scanner calls `Read()` multiple times to grow its token. After 1MiB of token, it hits `ErrTooLong`. At that point, the file position is at least 1MiB + (first line size) into the file. The file is ~2MB total. So the file position is somewhere in the middle of the 2MB line.

Then `bufio.NewReader(f).ReadString('\n')` reads from ~1MiB into the 2MB line. It reads until it finds a `\n`, which is at the end of the 2MB line. So it returns ~1MB of x's (partial oversized line). JSON parse fails, skipped. Then next `ReadString('\n')` reads the third line (good record). JSON parse succeeds. Then next call returns `io.EOF`. 

So we get: 1 (from scanner) + 1 (from reader loop, the third line) = 2. Test passes!

But this is fragile. If the internal `bufio.Reader` read ahead past the 2MB line (unlikely with 4KB chunks, but possible if the OS returns more data), the third line could be lost. Also, if there were good records between the oversized line and the end, they might be partially consumed.

Actually, I realize there's a more concrete bug here. After `ErrTooLong`, the scanner has already consumed some bytes from the file, but the scanner's internal `bufio.Reader` may have buffered more. When we create a new `bufio.NewReader(f)`, we lose that buffered data. So we might skip some records.

But for the specific test case, it works because the only data after the scanner's position is the rest of the oversized line and the third line. The rest of the oversized line is skipped (bad JSON), and the third line is recovered.

I think this is a real fragility issue but maybe not worth flagging since the test passes and the behavior is "good enough" for a tolerance reader. Let me move on.

Looking at `internal/audit/render.go`:
```go
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
```
This truncates to 12 chars. The TD item notes this as a correctness issue. But it's in the changed code, so I could flag it. However, it's already documented as TD. Let me check if there's a more immediate bug.

Looking at `RenderReport`:
```go
columns := append(append([]string{}, canonicalSeverities...), extras...)
```
This creates a new slice by copying canonicalSeverities and appending extras. But `append(append([]string{}, canonicalSeverities...), extras...)` - the first `append` creates a new slice with capacity = len(canonicalSeverities) = 4. Then the second `append` appends extras. If len(extras) > 0, this may or may not reallocate. If it doesn't reallocate (capacity is enough), it would overwrite... no, `[]string{}` has length 0 and capacity 0. `append([]string{}, canonicalSeverities...)` creates a new slice with length 4, capacity 4. Then `append(..., extras...)` appends extras, which may reallocate. This is fine - no aliasing issue.

Wait, actually `append([]string{}, canonicalSeverities...)` - `[]string{}` is a zero-length, zero-capacity slice. `append` allocates a new backing array with enough capacity. So the result is a new slice with no aliasing. Then appending extras is fine. OK.

Looking at the grand total row:
```go
b.WriteString("| **Total** | | |")
for _, c := range columns {
    fmt.Fprintf(&b, " %d |", grand[c])
}
fmt.Fprintf(&b, " %d |\n", grandTotal)
```
The header row has: `| Run (UTC) | Base | Head |` + severity columns + ` Total |`. That's 3 text columns + N severity columns + 1 total column = N+4 columns.

The total row has: `| **Total** | | |` (3 empty text columns) + severity counts + total. That's also N+4 columns. OK, matches.

But wait - the alignment row:
```go
b.WriteString("|-----------|------|------|")
for range columns {
    b.WriteString("------:|")
}
b.WriteString("------:|\n")
```
That's 3 text alignment + N severity alignments + 1 total alignment = N+4. OK.

Hmm, but the header row:
```go
b.WriteString("| Run (UTC) | Base | Head |")
for _, c := range columns {
    fmt.Fprintf(&b, " %s |", sanitizeCell(c))
}
b.WriteString(" Total |\n")
```
That's `| Run (UTC) | Base | Head |` (3 columns) + N severity columns + ` Total |` (1 column) = N+4. OK.

Data rows:
```go
fmt.Fprintf(&b, "| %s | %s | %s |",
    sanitizeCell(r.Timestamp.UTC().Format(time.RFC3339)),
    sanitizeCell(shortSHA(r.Base)),
    sanitizeCell(shortSHA(r.Head)))
```
3 columns + N + 1 = N+4. OK.

Looks consistent.

Now let me look at the `cmd/atcr/audit_report.go` more carefully:
```go
func runAuditReport(cmd *cobra.Command, _ []string) error {
	pr, _ := cmd.Flags().GetInt("pr")
	...
	forPR := make([]audit.Record, 0, len(recs))
	for _, r := range recs {
		if r.PR == pr {
			forPR = append(forPR, r)
		}
	}
```

If `pr == 0` (which passes `MarkFlagRequired` since the flag IS set, just with value 0), then `r.PR == 0` matches every record where PR was omitted (omitempty means PR is 0 in the JSON when omitted). So `--pr 0` would match ALL non-PR runs. This is the bug noted in the TD. But it's in the changed code, so I should flag it.

Actually wait, let me re-read the TD entry:
```
| 1 | [ ] | LOW | cmd/atcr/audit_report.go:23 | MarkFlagRequired only checks --pr is set not its value so `audit-report --pr 0` matches every non-PR run (PR omitted via omitempty) and renders a bogus PR-0 report of all local runs | After GetInt reject pr <= 0 with a usageError requiring a positive PR number | EDGE_CASES | 10 | execute-epic-independent |
```

So it's already documented as TD. But it's still a bug in the changed code. The instructions say "Find problems the author would prefer you didn't." So I should flag it.

Let me also look at `cmd/atcr/resume.go`:
```go
func recordResumeAudit(ctx context.Context, dir string, ts time.Time, pr int, base, head string) {
	auditPath := filepath.Join(".", ".atcr", "audit.log.jsonl")
```
This uses `"."` as the root, while `runReview` uses `req.Root` (which is also `"."`). And `audit-report` uses `repoRoot()` which walks up. So if you run `atcr review` from a subdirectory, the audit ledger is written to `./.atcr/audit.log.jsonl` (relative to CWD), but `atcr audit-report` reads from `repoRoot()/.atcr/audit.log.jsonl` (which walks up to find `.atcr/`). If you're in a subdirectory, these are different paths! This is the MEDIUM TD item.

But wait, `runReview` also uses `req.Root` which is `"."`:
```go
auditPath := filepath.Join(req.Root, ".atcr", "audit.log.jsonl")
```
And `req.Root` is set to `"."`:
```go
req := fanout.ReviewRequest{
    Repo: ".",
    Root: ".",
    ...
}
```

So both review and resume write to `./.atcr/audit.log.jsonl` (CWD-relative), while audit-report reads from `repoRoot()/.atcr/audit.log.jsonl` (walks up to find `.atcr/`). If you're in a subdirectory, the write goes to `subdir/.atcr/audit.log.jsonl` but the read looks for `.atcr/` walking up from `subdir`, finding the repo-root `.atcr/`. These are different paths!

This is a real bug. The history ledger has the same issue (also uses `filepath.Join(req.Root, ".atcr", ...)` where `req.Root = "."`). But the history command also uses `repoRoot()` to read. So this is a pre-existing pattern bug that's being replicated.

Actually, looking at the history ledger in review.go:
```go
histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")
```
And in resume.go:
```go
histPath := filepath.Join(".", ".atcr", "findings-history.jsonl")
```

Both use CWD-relative paths. And `atcr history` uses `repoRoot()` to read. So the same bug exists for history. But the audit code is new, so it's in scope.

This is the MEDIUM TD item. It's a real bug in the changed code. Let me flag it.

Now let me look at `internal/audit/capture.go` more carefully:

```go
func RecordReview(auditPath, reviewDir string, ts time.Time, pr int, base, head string) (int, error) {
	findings, err := summarize(reviewDir)
	if err != nil {
		return 0, err
	}
	rec := Record{
		Timestamp: ts,
		PR:        pr,
		Base:      base,
		Head:      head,
		Findings:  findings,
	}
	if err := Append(auditPath, []Record{rec}); err != nil {
		return 0, err
	}
	return 1, nil
}
```

If `summarize` returns an error (e.g., file exists but can't be read, or parse error), the audit record is NOT written. But the doc says "Exactly one record is written per call, unconditionally (Epic 19.1 AC1): a missing or empty pool findings file yields an empty severity summary, not a skipped write." 

A missing pool file returns `(nil, nil)` - OK, record is written. But a READ error (permissions, etc.) returns `(nil, err)`, which causes the record to be skipped. And a PARSE error also returns `(nil, err)`, skipping the record.

Wait, but `RecordReview` is called from `runReview`:
```go
if n, aerr := audit.RecordReview(auditPath, result.Dir, now, req.PRNumber, req.Range.Base, req.Range.Head); aerr != nil {
    log.FromContext(ctx).Warn("failed to append audit record", "error", aerr)
}
```

So if `summarize` returns an error, `RecordReview` returns an error, and it's logged as a warning. The review still succeeds. But the audit record is NOT written, violating AC1 ("exactly one record per run, unconditionally").

Hmm, but the doc says "a missing or empty pool findings file yields an empty severity summary, not a skipped write." A missing file is handled (returns nil). But a corrupt pool file (parse error) would cause the record to be skipped. Is that a bug?

Actually, looking at `summarize`:
```go
func summarize(reviewDir string) (map[string]int, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, poolFindingsRel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading pool findings: %w", err)
	}

	res, err := stream.ParseSource(data)
	if err != nil {
		return nil, fmt.Errorf("parsing pool findings: %w", err)
	}
```

A non-NotExist read error (e.g., permission denied) returns an error, skipping the audit record. A parse error also returns an error, skipping the record. This contradicts the doc comment that says the record is written unconditionally.

But wait, is this really a bug? The doc says "a missing or empty pool findings file yields an empty severity summary, not a skipped write." It specifically calls out "missing or empty" - a parse error is neither missing nor empty. And a permission error is a real IO problem. So maybe the intent is: missing/empty → write with empty summary; IO/parse error → skip (which is reasonable since something is fundamentally wrong).

But the doc also says "Exactly one record is written per call, unconditionally (Epic 19.1 AC1)." That's a stronger claim. The code doesn't match this claim for error cases.

I think this is worth flagging as a MEDIUM or LOW issue - the doc claims unconditional write but the code skips on parse/read errors.

Actually, let me re-read: "a missing or empty pool findings file yields an empty severity summary, not a skipped write, so the audit trail records every review run — including clean ones and runs off the findings path."

The key claim is "the audit trail records every review run." But if the pool file exists but is corrupt (parse error), the record is NOT written. This is a gap.

But is this likely? The pool file is written by `WritePool` in the same review run, so it should be well-formed. A parse error would indicate a bug in `WritePool` or `stream.ParseSource`. So it's unlikely but possible (e.g., a partial write due to disk full).

Let me flag it as LOW.

Now, let me look at the `intToStr` function in the test:
```go
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
```

This is a test helper. It works for the test cases (0 and 1234). But `byte('0' + n%10)` - if n is negative and we didn't catch it... wait, we do catch negatives. And `n%10` for positive n is 0-9, so `'0' + 0..9` = '0'..'9'. Fine.

But there's a subtle issue: `n = -n` for `n == math.MinInt` would overflow. But the test only uses 0 and 1234, so this is not a practical issue. Not worth flagging.

Let me look at the `prNumberFromFlags` function again:
```go
func prNumberFromFlags(cmd *cobra.Command) int {
	if cmd.Flags().Changed("pr") {
		if n, _ := cmd.Flags().GetInt("pr"); n > 0 {
			return n
		}
	}
	return prFromGitHubRef(os.Getenv("GITHUB_REF"))
}
```

The test case "flag zero falls back to env" has `flagSet: false`, not `flagSet: true` with `flagVal: 0`. Wait:
```go
{name: "flag zero falls back to env", flagSet: false, githubRef: "refs/pull/3/merge", want: 3},
```
`flagSet` is false, so the flag is not set. The test name says "flag zero falls back to env" but it doesn't actually test setting the flag to zero - it tests NOT setting the flag. The `prNumberFromFlags` function checks `cmd.Flags().Changed("pr")` first, and if not changed, falls through to env. So this test passes but doesn't actually test the `n > 0` guard when the flag IS set to 0.

Actually, looking more carefully at the test: when `flagSet` is false, the flag is not set, so `cmd.Flags().Changed("pr")` returns false, and we fall through to `prFromGitHubRef`. The test name "flag zero falls back to env" is misleading - it's testing "flag not set falls back to env", not "flag set to zero falls back to env."

There's no test case for `flagSet: true, flagVal: 0`. If someone passes `--pr 0`, `Changed("pr")` returns true, `GetInt("pr")` returns 0, `n > 0` is false, so we fall through to `prFromGitHubRef`. This is the intended behavior per the comment. But it's not tested. Not a bug, just a test gap.

Let me look at `cmd/atcr/audit_report.go` line 23:
```go
_ = cmd.MarkFlagRequired("pr")
```
`MarkFlagRequired` returns an error, which is ignored. This is fine since "pr" is a valid flag name. But the real issue is that `MarkFlagRequired` only checks the flag is SET, not that it has a positive value. `--pr 0` would pass the required check. Then `r.PR == pr` would match all records with `PR == 0` (i.e., all non-PR runs). This is a real bug.

Let me also look at `cmd/atcr/resume.go`:
```go
recordResumeAudit(ctx, dir, req.StartedAt, req.PRNumber, req.Range.Base, req.Range.Head)
```

Wait, in the "AllComplete" path:
```go
if info.AllComplete() {
    ...
    recordResumeHistory(ctx, dir, req.StartedAt)
    recordResumeAudit(ctx, dir, req.StartedAt, req.PRNumber, req.Range.Base, req.Range.Head)
    return nil
}
```

And in the normal completion path:
```go
recordResumeHistory(ctx, result.Dir, req.StartedAt)
recordResumeAudit(ctx, result.Dir, req.StartedAt, req.PRNumber, req.Range.Base, req.Range.Head)
```

In the AllComplete path, `dir` is used. In the normal path, `result.Dir` is used. Are these the same? `dir` is the resolved resume directory, and `result.Dir` is the directory from the execution result. They should be the same, but let me check... `ExecuteResume` returns a result with `Dir` set to the review directory. `dir` was passed to `PrepareResume` which sets up the review in that directory. So `result.Dir` should equal `dir`. OK, probably fine.

Now let me look at the `recordResumeAudit` function:
```go
func recordResumeAudit(ctx context.Context, dir string, ts time.Time, pr int, base, head string) {
	auditPath := filepath.Join(".", ".atcr", "audit.log.jsonl")
	if n, err := audit.RecordReview(auditPath, dir, ts, pr, base, head); err != nil {
		log.FromContext(ctx).Warn("failed to append audit record", "error", err)
	} else if n > 0 {
		log.FromContext(ctx).Debug("appended audit record", "records", n, "pr", pr, "path", auditPath)
	}
}
```

This uses `filepath.Join(".", ".atcr", "audit.log.jsonl")` which is CWD-relative. Meanwhile, `audit-report` uses `repoRoot()` which walks up. If run from a subdirectory, these don't match. Same issue as in `runReview`.

But wait, in `runReview`:
```go
auditPath := filepath.Join(req.Root, ".atcr", "audit.log.jsonl")
```
Where `req.Root = "."`. So both use `"."` as the root. And `audit-report` uses `repoRoot()`. This is the integration bug noted in the TD.

Let me check `repoRoot()`:
```go
// repoRoot walks up from the working directory to the directory containing
// go.mod. Robust under -trimpath, unlike runtime.Caller paths.
```
So `repoRoot()` walks up to find `go.mod`. If you're in a subdirectory, it finds the repo root. But the audit ledger is written to `./.atcr/audit.log.jsonl` (CWD), which may not be the repo root.

This is a real bug. Let me flag it.

Actually, looking more carefully at the review.go code:
```go
histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")
```
And `req.Root = "."`. So the history ledger has the same bug. But the history code is pre-existing (not in this diff). The audit code is new.

But wait, the CHANGELOG says:
```
- `internal/history`: `review --resume` now appends its findings to the history ledger, closing a gap where resumed reviews were missing from the trend data.
- `atcr history` now works correctly when run from a subdirectory instead of only from the repo root.
```

So `atcr history` was fixed to work from subdirectories. But the WRITE path still uses `"."`. Hmm, but that's a pre-existing issue for history. The audit code has the same issue.

Let me flag the audit-specific instance.

OK, let me also check if there are any issues with the `summarize` function's dedup logic:

```go
highest := make(map[string]string, len(res.Findings))
for _, f := range res.Findings {
    key := f.File + "\x00" + strconv.Itoa(f.Line) + "\x00" + f.Problem
    if prev, ok := highest[key]; ok {
        if stream.SeverityRank[stream.NormalizeSeverity(f.Severity)] > stream.SeverityRank[stream.NormalizeSeverity(prev)] {
            highest[key] = f.Severity
        }
        continue
    }
    highest[key] = f.Severity
}
```

The comparison uses `stream.SeverityRank[stream.NormalizeSeverity(f.Severity)]`. If `NormalizeSeverity` returns a value not in `SeverityRank`, the map lookup returns 0 (zero value for int). So an unknown severity would have rank 0. If the canonical LOW has rank 1, then an unknown severity (rank 0) would be LESS than LOW. So the dedup keeps the higher-ranked one. That seems correct.

But what if BOTH severities are unknown? Then both have rank 0, `0 > 0` is false, so the first one is kept. That's fine - deterministic.

What if `SeverityRank` is defined such that the lowest canonical severity has rank 0? Let me check... I don't have the `stream` package code, but the comment says "matching reconcile.SeverityRank". If LOW has rank 0, then an unknown severity (also rank 0) would tie with LOW, and the first occurrence would be kept. That could be a subtle issue if the first occurrence is the unknown one and the second is LOW - the unknown would be kept instead of LOW. But this is an edge case with non-canonical severities.

Actually, I realize I should look at what `stream.SeverityRank` is. From the boundaries comment: "stream is the canonical zero-dependency severity leaf". I don't have the code, but based on the usage pattern, it's a map from severity string to rank int.

I don't think this is worth flagging without seeing the actual `SeverityRank` definition.

Let me look at the `RenderReport` function more carefully for the grand total:

```go
grand := map[string]int{}
grandTotal := 0
for _, r := range sorted {
    counts := map[string]int{}
    for sev, n := range r.Findings {
        counts[normalizeSeverity(sev)] += n
    }
    ...
    rowTotal := 0
    for _, c := range columns {
        n := counts[c]
        fmt.Fprintf(&b, " %d |", n)
        rowTotal += n
        grand[c] += n
    }
    fmt.Fprintf(&b, " %d |\n", rowTotal)
    grandTotal += rowTotal
}
```

`grand` is keyed by column name (canonical severity). `grand[c] += n` accumulates per column. But `grand` is initialized as an empty map. For columns that have no findings in any record, `grand[c]` would be 0 (zero value). That's correct.

But wait - `grand` is used in the total row:
```go
b.WriteString("| **Total** | | |")
for _, c := range columns {
    fmt.Fprintf(&b, " %d |", grand[c])
}
```

If a column has no findings, `grand[c]` is 0