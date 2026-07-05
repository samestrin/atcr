Let me carefully review the changed code for bugs at the line/expression level.

Let me go through each file:

1. **cmd/atcr/history.go** - new file
2. **cmd/atcr/history_test.go** - new file
3. **cmd/atcr/main.go** - added newHistoryCmd
4. **cmd/atcr/main_test.go** - updated count
5. **cmd/atcr/review.go** - added history.RecordReview call
6. **internal/boundaries_test.go** - added history package entry
7. **internal/history/capture.go** - new file
8. **internal/history/capture_test.go** - new file
9. **internal/history/edge_test.go** - new file
10. **internal/history/filter.go** - new file
11. **internal/history/filter_test.go** - new file
12. **internal/history/reader.go** - new file
13. **internal/history/record.go** - new file
14. **internal/history/render.go** - new file
15. **internal/history/render_test.go** - new file
16. **internal/history/writer.go** - new file

Let me look at each carefully.

### internal/history/filter.go - ParseSince

```go
func ParseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	var d time.Duration
	switch unit := s[len(s)-1]; unit {
	case 'd', 'w':
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: want a number before %q (e.g. 30%c)", s, string(unit), unit)
		}
		per := 24 * time.Hour
		if unit == 'w' {
			per = 7 * 24 * time.Hour
		}
		d = time.Duration(n * float64(per))
	default:
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: use h/m/s or d/w (e.g. 30d, 2w, 48h)", s)
		}
		d = parsed
	}

	if d <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return d, nil
}
```

Wait — `strconv.ParseFloat(s[:len(s)-1], 64)` — if `s = "d"`, then `s[:len(s)-1] = ""` which would be parsed as an error by ParseFloat. Good, that's handled.

But what about negative values? `"-5d"` — `ParseFloat("-5", 64)` returns -5, then `d = -5 * 24h = -120h`, which is <= 0, so it's rejected. Good.

What about `0d`? `ParseFloat("0", 64)` returns 0, then `d = 0`, which is <= 0, so rejected. Good.

Hmm, but what about `0.5d`? That would be 12h, positive. Fine.

What about a single character like `"d"`? `s[:len(s)-1] = ""`, `ParseFloat("")` returns error. Good.

What about `"-d"`? `ParseFloat("-")` returns error. Good.

Let me check `s[len(s)-1]` — if `s = ""`, we already returned earlier. So `s` is non-empty here. Good.

### internal/history/filter.go - Filter

```go
func Filter(recs []Record, since time.Duration, pkg string, now time.Time) []Record {
	cutoff := now.Add(-since)
	pkg = strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(pkg), "\\", "/"), "/")
	...
}
```

`strings.TrimRight(s, "/")` — this trims ALL trailing slashes. But what if `pkg = "/"`? Then after `TrimSpace` and `ReplaceAll`, it's still "/", and `TrimRight` makes it "". So `pkg = ""` which means no filter. That seems acceptable.

But what about `pkg = "."`? `TrimRight(".", "/")` = "." (no trailing slash to trim). Then `packageMatch("a", ".")` returns `"a" == "." || HasPrefix("a", "./")` = false. So filtering by "." matches nothing. Hmm, but is that a bug? The `PackageOf` returns "." for bare filenames. So if a user wants to find findings with no package, they'd query `--package .`. That would match records with `Package == "."`. So `packageMatch(".", ".")` = `true || false` = true. OK that works.

### internal/history/capture.go - RecordReview

```go
func RecordReview(histPath, reviewDir string, ts time.Time) (int, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, poolFindingsRel))
	...
	res, err := stream.ParseSource(data)
	...
	records := make([]Record, 0, len(res.Findings))
	seen := make(map[string]bool, len(res.Findings))
	for _, f := range res.Findings {
		id := FindingID(f.File, f.Line, f.Problem)
		if seen[id] {
			continue
		}
		seen[id] = true
		records = append(records, Record{
			Timestamp: ts,
			Package:   PackageOf(f.File),
			Severity:  f.Severity,
			ID:        id,
			File:      f.File,
			Category:  f.Category,
		})
	}
	if err := Append(histPath, records); err != nil {
		return 0, err
	}
	return len(records), nil
}
```

This looks fine. The dedup is by id within the run. 

Wait — `f.Line` — what type is it? In `stream.Finding`, Line is likely an `int`. `FindingID` takes `line int`. OK.

But wait — what if `f.Line` is 0 or negative? `FindingID` would still hash it. That's fine for ID purposes.

### internal/history/record.go - FindingID

```go
func FindingID(file string, line int, problem string) string {
	h := sha256.Sum256([]byte(file + "\x00" + strconv.Itoa(line) + "\x00" + problem))
	return hex.EncodeToString(h[:8])
}
```

`hex.EncodeToString(h[:8])` — `h` is `[32]byte`, `h[:8]` is `[]byte` of length 8. `hex.EncodeToString` produces 16 hex chars. The test asserts `assert.Len(t, a, 16)`. Good.

### internal/history/record.go - PackageOf

```go
func PackageOf(file string) string {
	return filepath.ToSlash(filepath.Dir(file))
}
```

`filepath.Dir("internal/registry/load.go")` = "internal/registry". Good.
`filepath.Dir("main.go")` = ".". Good.
`filepath.Dir("")` = ".". Good.

### internal/history/reader.go - Load

```go
func Load(path string) ([]Record, error) {
	f, err := os.Open(path)
	...
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	...
}
```

This looks fine. `sc.Bytes()` returns the current line. `bytes.TrimSpace` trims whitespace. Blank lines skipped. Malformed JSON skipped.

Wait — `sc.Bytes()` returns a slice that's only valid until the next call to Scan. But `bytes.TrimSpace` returns a sub-slice, and `json.Unmarshal` copies the data into `rec`. So that's fine.

### internal/history/writer.go - Append

```go
func Append(path string, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range records {
		if err := enc.Encode(records[i]); err != nil {
			return fmt.Errorf("encoding history record: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating history dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening history ledger: %w", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing history ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing history ledger: %w", err)
	}
	return nil
}
```

This looks fine. The whole batch is written with a single Write call.

### internal/history/render.go - RenderTable

```go
func RenderTable(recs []Record) string {
	if len(recs) == 0 {
		return ""
	}

	counts := map[string]map[string]int{}
	extraSet := map[string]bool{}
	canonical := map[string]bool{}
	for _, s := range canonicalSeverities {
		canonical[s] = true
	}

	for _, r := range recs {
		sev := normalizeSeverity(r.Severity)
		if sev == "" {
			sev = "UNKNOWN"
		}
		if counts[r.Package] == nil {
			counts[r.Package] = map[string]int{}
		}
		counts[r.Package][sev]++
		if !canonical[sev] {
			extraSet[sev] = true
		}
	}

	extras := make([]string, 0, len(extraSet))
	for s := range extraSet {
		extras = append(extras, s)
	}
	sort.Strings(extras)
	columns := append(append([]string{}, canonicalSeverities...), extras...)
	...
}
```

Wait — `columns := append(append([]string{}, canonicalSeverities...), extras...)`. The inner `append([]string{}, canonicalSeverities...)` creates a new slice with the canonical severities. Then `append(..., extras...)` appends the extras. This should work fine.

But wait — `canonicalSeverities` is a package-level `[]string`. `append([]string{}, canonicalSeverities...)` creates a copy. Good, so the package-level slice isn't mutated.

Let me check the header rendering:

```go
b.WriteString("| Package |")
for _, c := range columns {
    fmt.Fprintf(&b, " %s |", c)
}
b.WriteString(" Total |\n")
```

So the header is `| Package | CRITICAL | HIGH | MEDIUM | LOW | [extras] | Total |`.

Alignment row:
```go
b.WriteString("|---------|")
for range columns {
    b.WriteString("------:|")
}
b.WriteString("------:|\n")
```

Data rows:
```go
for _, p := range packages {
    fmt.Fprintf(&b, "| %s |", p)
    rowTotal := 0
    for _, c := range columns {
        n := counts[p][c]
        fmt.Fprintf(&b, " %d |", n)
        rowTotal += n
        grand[c] += n
    }
    fmt.Fprintf(&b, " %d |\n", rowTotal)
    grandTotal += rowTotal
}
```

Grand total:
```go
b.WriteString("| **Total** |")
for _, c := range columns {
    fmt.Fprintf(&b, " %d |", grand[c])
}
fmt.Fprintf(&b, " %d |\n", grandTotal)
```

This looks correct.

### cmd/atcr/history.go - runHistory

```go
func runHistory(cmd *cobra.Command, _ []string) error {
	since := defaultHistorySince
	if raw, _ := cmd.Flags().GetString("since"); strings.TrimSpace(raw) != "" {
		d, err := history.ParseSince(raw)
		if err != nil {
			return usageError(err) // bad --since is a usage error (exit 2)
		}
		since = d
	}
	pkg, _ := cmd.Flags().GetString("package")

	histPath := filepath.Join(".", ".atcr", "findings-history.jsonl")
	recs, err := history.Load(histPath)
	if err != nil {
		return usageError(err) // corrupt/unreadable ledger (exit 2)
	}
	...
}
```

Wait — `filepath.Join(".", ".atcr", "findings-history.jsonl")` — this resolves to `.atcr/findings-history.jsonl` relative to CWD. The TD note says "atcr history resolves .atcr relative to cwd (consistent with review/status/report) so running it from a subdirectory reports no history". So this is a known issue, already tracked as TD.

But wait — in `review.go`, the history is written to `filepath.Join(req.Root, ".atcr", "findings-history.jsonl")` where `req.Root = "."`. So both use CWD-relative paths. Consistent.

### cmd/atcr/review.go - history.RecordReview call

```go
histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")
if n, herr := history.RecordReview(histPath, result.Dir, now); herr != nil {
    log.FromContext(ctx).Warn("failed to append finding history", "error", herr)
} else if n > 0 {
    log.FromContext(ctx).Debug("appended finding history", "records", n, "path", histPath)
}
```

`req.Root` is "." (set earlier in the ReviewRequest). `result.Dir` is the review directory. `now` is the review start time. This looks correct.

Wait — `now` is defined earlier as `now := time.Now()`. And it's used for `req.Date`, `req.TimeSuffix`, `req.StartedAt`. So the history timestamp matches the review start time. Good.

### Now let me look more carefully for bugs...

Let me re-examine `ParseSince` more carefully:

```go
switch unit := s[len(s)-1]; unit {
case 'd', 'w':
    n, err := strconv.ParseFloat(s[:len(s)-1], 64)
```

What if `s = "30d"` — `s[len(s)-1] = 'd'`, `s[:len(s)-1] = "30"`. `ParseFloat("30", 64)` = 30.0. `d = 30 * 24h = 720h`. Good.

What if `s = "2.5w"` — `s[:len(s)-1] = "2.5"`, `ParseFloat("2.5", 64)` = 2.5. `d = 2.5 * 7 * 24h`. Good.

What about `s = "30D"` (uppercase)? `s[len(s)-1] = 'D'`, which doesn't match `'d'` or `'w'`, so it falls through to `time.ParseDuration("30D")` which would fail. So uppercase D is not supported. Is that a bug? The help text says "h/m/s or d/w units (e.g. 30d, 2w, 48h)". So lowercase is expected. Not a bug per se, but could be surprising.

Let me look at the test cases for ParseSince:

```go
func TestParseSince_Invalid(t *testing.T) {
	for _, in := range []string{"", "  ", "abc", "d", "-5d", "0d", "0", "5x", "w"} {
		_, err := ParseSince(in)
		assert.Error(t, err, "input %q should be rejected", in)
	}
}
```

`"  "` — `TrimSpace` makes it "", which returns error. Good.
`"d"` — `s[:len(s)-1] = ""`, `ParseFloat("")` errors. Good.
`"w"` — same. Good.
`"5x"` — `s[len(s)-1] = 'x'`, falls to default, `time.ParseDuration("5x")` errors. Good.
`"0"` — `s[len(s)-1] = '0'`, falls to default, `time.ParseDuration("0")` = 0, which is <= 0, rejected. Good.
`"-5d"` — `ParseFloat("-5", 64)` = -5, `d = -5 * 24h = -120h`, <= 0, rejected. Good.
`"0d"` — `ParseFloat("0", 64)` = 0, `d = 0`, <= 0, rejected. Good.

OK, ParseSince looks solid.

### Let me look at the render test more carefully:

```go
func TestRenderTable_CountsBySeverityPerPackage(t *testing.T) {
	ts := time.Now()
	recs := []Record{
		{Timestamp: ts, Package: "internal/registry", Severity: "HIGH", ID: "1"},
		{Timestamp: ts, Package: "internal/registry", Severity: "HIGH", ID: "2"},
		{Timestamp: ts, Package: "internal/registry", Severity: "MEDIUM", ID: "3"},
		{Timestamp: ts, Package: "cmd/atcr", Severity: "low", ID: "4"}, // lowercase normalized
		{Timestamp: ts, Package: "cmd/atcr", Severity: "LOW", ID: "5"},
	}
	...
	// cmd/atcr: 2 LOW, total 2.
	assert.Regexp(t, `cmd/atcr.*\|\s*0\s*\|\s*0\s*\|\s*0\s*\|\s*2\s*\|\s*2\s*\|`, cmdRow)
	// internal/registry: 2 HIGH, 1 MEDIUM, total 3.
	assert.Regexp(t, `internal/registry.*\|\s*0\s*\|\s*2\s*\|\s*1\s*\|\s*0\s*\|\s*3\s*\|`, regRow)
	...
	assert.Regexp(t, `\|\s*0\s*\|\s*2\s*\|\s*1\s*\|\s*2\s*\|\s*5\s*\|`, totalRow)
}
```

The grand total: CRITICAL=0, HIGH=2, MEDIUM=1, LOW=2, Total=5. Let me verify:
- internal/registry: 2 HIGH, 1 MEDIUM
- cmd/atcr: 2 LOW (low normalized to LOW)
- Total: HIGH=2, MEDIUM=1, LOW=2, grand=5

The regex `\|\s*0\s*\|\s*2\s*\|\s*1\s*\|\s*2\s*\|\s*5\s*\|` matches `| 0 | 2 | 1 | 2 | 5 |`. Good.

### Now let me look at the history_test.go more carefully:

```go
func TestHistoryCmd_FiltersAndRendersTable(t *testing.T) {
	...
	assert.Regexp(t, `\*\*Total\*\*.*\|\s*0\s*\|\s*1\s*\|\s*1\s*\|\s*0\s*\|\s*2\s*\|`, out)
}
```

The test data:
- recent, internal/registry, HIGH, id=1
- recent, internal/registry, MEDIUM, id=2
- recent, internal/registry2, HIGH, id=3
- old, internal/registry, LOW, id=4

With `--since 30d --package internal/registry`:
- recent HIGH internal/registry → included
- recent MEDIUM internal/registry → included
- recent HIGH internal/registry2 → excluded (package filter)
- old LOW internal/registry → excluded (time filter)

So filtered = 1 HIGH + 1 MEDIUM for internal/registry. Grand total: CRITICAL=0, HIGH=1, MEDIUM=1, LOW=0, Total=2.

The regex: `\*\*Total\*\*.*\|\s*0\s*\|\s*1\s*\|\s*1\s*\|\s*0\s*\|\s*2\s*\|` matches `**Total** | 0 | 1 | 1 | 0 | 2 |`. Good.

### Let me look at the edge_test.go:

```go
func TestLoad_SkipsMalformedLineAndKeepsRest(t *testing.T) {
	...
	good1, _ := jsonLine(t, Record{Timestamp: ts, Package: "a", Severity: "HIGH", ID: "1"})
	good2, _ := jsonLine(t, Record{Timestamp: ts, Package: "b", Severity: "LOW", ID: "2"})
	require.NoError(t, os.WriteFile(path, []byte(good1+"{not json}\n"+good2), 0o644))
	...
}
```

`jsonLine` returns `(string, error)`. The test does `good1, _ := jsonLine(...)`. The second return value is ignored. But `jsonLine` uses `require.NoError` internally, so if `json.Marshal` fails, the test fails. The `_` is fine.

Wait — `jsonLine` returns `(string, error)` but the error is always `nil` (since `require.NoError` would have failed the test). So `good1, _ := jsonLine(...)` is fine.

### Let me look at the capture_test.go:

```go
func TestRecordReview_AppendsOneRecordPerPoolFinding(t *testing.T) {
	...
	writePoolFindings(t, reviewDir,
		"HIGH|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|greta\n"+
			"LOW|cmd/atcr/review.go:10|nit|rename|STYLE|5|ev|kai\n")
	...
}
```

The pool findings format is 8-column: `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MIN|EVIDENCE|REVIEWER`. So:
- `HIGH|internal/registry/load.go:42|unchecked error|handle it|CORRECTNESS|15|ev|greta`
- `LOW|cmd/atcr/review.go:10|nit|rename|STYLE|5|ev|kai`

This looks right for the 8-column format.

### Now let me think about potential bugs more carefully...

**Bug candidate 1: ParseSince with "d" or "w" alone**

`s = "d"` → `s[:len(s)-1] = ""` → `ParseFloat("")` → error. Handled.

**Bug candidate 2: ParseSince with negative durations like "-5d"**

`ParseFloat("-5", 64)` = -5. `d = -5 * 24h = -120h`. `d <= 0` → rejected. Good.

But wait — what about `time.ParseDuration` accepting negative durations? Like `"-5h"`. `s[len(s)-1] = 'h'`, falls to default, `time.ParseDuration("-5h")` = -5h, `d <= 0` → rejected. Good.

**Bug candidate 3: ParseSince with "0.5d"**

`ParseFloat("0.5", 64)` = 0.5. `d = 0.5 * 24h = 12h`. `d > 0` → accepted. That's fine.

**Bug candidate 4: ParseSince with just a number like "30"**

`s = "30"`, `s[len(s)-1] = '0'`, falls to default, `time.ParseDuration("30")` → error (no unit). Good, rejected.

Wait, actually `time.ParseDuration("30")` returns an error because it requires a unit. Let me verify... Yes, Go's `time.ParseDuration` requires a unit suffix. "30" without a unit returns an error. Good.

**Bug candidate 5: Filter with negative since**

If `since` is negative (which ParseSince rejects, but if someone calls Filter directly), `cutoff = now.Add(-since)` would be in the future, so `r.Timestamp.Before(cutoff)` would be true for all records, filtering everything out. But this is a direct API call issue, not a CLI issue. Not a bug in the changed code.

**Bug candidate 6: RenderTable with empty severity**

```go
sev := normalizeSeverity(r.Severity)
if sev == "" {
    sev = "UNKNOWN"
}
```

If `r.Severity` is "" or "  ", `normalizeSeverity` returns "", then `sev = "UNKNOWN"`. "UNKNOWN" is not in canonical, so it goes to extras. Good.

**Bug candidate 7: Record JSON marshaling of Timestamp**

The `Record` struct has `Timestamp time.Time `json:"ts"``. When marshaled to JSON, `time.Time` is encoded as RFC3339. When unmarshaled, it's parsed back. The test checks `ts.Equal(loaded[0].Timestamp)`. If the timestamp has timezone info, it should round-trip correctly. `time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)` would serialize as `"2026-07-04T12:00:00Z"`. Good.

But wait — what about `time.Now()`? It has monotonic clock and local timezone. When marshaled to JSON, the monotonic clock is dropped and the timezone is preserved. When unmarshaled, it's in the original timezone. `ts.Equal(loaded[0].Timestamp)` — `time.Equal` handles timezones correctly. But the monotonic clock... Actually, `time.Time.Equal` ignores the monotonic clock. So this should be fine.

Actually, wait. Let me think about this more carefully. In `TestAppend_WritesJSONLAndCreatesParentDirs`:

```go
ts := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
...
assert.True(t, ts.Equal(loaded[0].Timestamp))
```

`ts` is UTC. After JSON round-trip, `loaded[0].Timestamp` should also be UTC. `ts.Equal(loaded[0].Timestamp)` should be true. Good.

In `TestRecordReview_AppendsOneRecordPerPoolFinding`:
```go
ts := time.Date(2026, 7, 4, 9, 30, 0, 0, time.UTC)
...
assert.True(t, ts.Equal(recs[0].Timestamp))
```

Same. Good.

**Bug candidate 8: PackageOf with Windows paths**

`PackageOf("internal\\registry\\load.go")` on Windows: `filepath.Dir` returns `internal\registry`, then `filepath.ToSlash` converts to `internal/registry`. Good.

On Linux: `filepath.Dir("internal\\registry\\load.go")` returns `.` (since backslash is not a path separator on Linux). Then `filepath.ToSlash(".")` = ".". So on Linux, a Windows-style path would give ".". But this is expected — the pool findings should use OS-appropriate paths.

**Bug candidate 9: Filter package normalization**

```go
pkg = strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(pkg), "\\", "/"), "/")
```

If `pkg = "internal/registry/"`, after `TrimSpace` = "internal/registry/", `ReplaceAll` = "internal/registry/", `TrimRight` = "internal/registry". Good.

If `pkg = "internal/registry//"`, `TrimRight` removes all trailing slashes = "internal/registry". Good.

If `pkg = "/"`, `TrimRight` = "". Then `pkg == ""` means no filter. Hmm, is that a bug? If someone passes `--package /`, they'd get no filter. But that's an edge case that's unlikely in practice.

If `pkg = " internal/registry "`, `TrimSpace` = "internal/registry". Good.

**Bug candidate 10: RenderTable column order**

```go
columns := append(append([]string{}, canonicalSeverities...), extras...)
```

`canonicalSeverities` = ["CRITICAL", "HIGH", "MEDIUM", "LOW"]. `extras` is sorted. So columns = ["CRITICAL", "HIGH", "MEDIUM", "LOW", ...extras sorted...]. Good.

But wait — `append(append([]string{}, canonicalSeverities...), extras...)`. The inner `append([]string{}, canonicalSeverities...)` creates a new slice with capacity 4 and length 4. Then `append(..., extras...)` might or might not allocate a new backing array depending on capacity. If `extras` is non-empty, the capacity is 4, and we're appending more, so it allocates a new array. The original `canonicalSeverities` is not modified. Good.

Actually, wait. Let me think again. `append([]string{}, canonicalSeverities...)` — `[]string{}` has length 0 and capacity 0. `append` with `canonicalSeverities...` (4 elements) allocates a new array of capacity >= 4 and copies the 4 elements. The result has length 4, capacity >= 4. Then `append(result, extras...)` — if `extras` has, say, 1 element, and the capacity is exactly 4, then it allocates a new array. If the capacity is > 4, it reuses the array. Either way, the original `canonicalSeverities` package-level slice is never touched because the first `append` already created a copy. Good.

**Bug candidate 11: RenderTable alignment row**

```go
b.WriteString("|---------|")
for range columns {
    b.WriteString("------:|")
}
b.WriteString("------:|\n")
```

The alignment row has `|---------|` for the Package column, then `------:|` for each severity column, then `------:|` for the Total column. But the header row is:

```go
b.WriteString("| Package |")
for _, c := range columns {
    fmt.Fprintf(&b, " %s |", c)
}
b.WriteString(" Total |\n")
```

So the header has `| Package | CRITICAL | HIGH | ... | Total |`. The alignment row has `|---------|------:|------:|...|------:|`. The number of `------:|` entries is `len(columns) + 1` (one for each severity column plus one for Total). And the header has `len(columns)` severity entries plus one Total. So the alignment row has the right number of columns. Good.

**Bug candidate 12: concurrent writes to the history ledger**

The TD note mentions this: "Append relies on a single O_APPEND write being atomic across concurrent atcr review runs; POSIX does not guarantee atomicity for multi-KB writes on regular files so simultaneous runs could interleave a JSONL line". This is tracked as TD, not a new bug.

**Bug candidate 13: RecordReview reading pool findings**

`poolFindingsRel = filepath.Join("sources", "pool", "findings.txt")` — this is the relative path within the review directory. `filepath.Join(reviewDir, poolFindingsRel)` gives the full path. Good.

But wait — what if `reviewDir` is an absolute path? `filepath.Join` handles that correctly. And if it's relative? Also fine.

**Bug candidate 14: The `now` timestamp in review.go**

```go
now := time.Now()
...
result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)
...
if n, herr := history.RecordReview(histPath, result.Dir, now); herr != nil {
```

`now` is captured before `ExecuteReview`. So the history timestamp reflects when the review started, not when it finished. This is the intended behavior per the plan: "ts (RFC3339 run timestamp)".

**Bug candidate 15: history.RecordReview called even when result is nil?**

Let me check the flow:

```go
result, err := fanout.ExecuteReview(ctx, llmclient.New(), prep)

if errors.Is(ctx.Err(), context.Canceled) {
    return reportInterrupt(cmd, ctx, result, prep)
}

if result != nil {
    _, _ = fmt.Fprintf(...)
    ...
}
if err != nil {
    return err // all-agents-failed → exit 1, artifacts preserved
}

// History hook here
histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")
if n, herr := history.RecordReview(histPath, result.Dir, now); herr != nil {
```

Wait — if `err != nil`, we return early. So we only reach the history hook when `err == nil`. But what if `result` is nil even when `err` is nil? Let me check...

Actually, looking at the flow:
1. `result, err := fanout.ExecuteReview(...)` 
2. If `ctx.Err() == context.Canceled`, return early (interrupt path)
3. If `result != nil`, print summary
4. If `err != nil`, return err

So when we reach the history hook, `err == nil`. But is `result` guaranteed to be non-nil when `err == nil`? Looking at the comment: "The `if err != nil { return err }` guard above ensures this block is only reached when ExecuteReview succeeded". So the assumption is that `result != nil` when `err == nil`.

But what if `ExecuteReview` returns `(nil, nil)`? Then `result` would be nil, and `result.Dir` would panic. However, this would be a bug in `ExecuteReview`, not in the history code. And the comment explicitly says this can't happen. So this is probably fine.

Actually, let me look more carefully. The code does:

```go
if result != nil {
    _, _ = fmt.Fprintf(...)
    ...
}
if err != nil {
    return err
}

histPath := filepath.Join(req.Root, ".atcr", "findings-history.jsonl")
if n, herr := history.RecordReview(histPath, result.Dir, now); herr != nil {
```

If `result` is nil and `err` is nil, we'd panic at `result.Dir`. But the comment says this can't happen. Let me accept that.

**Bug candidate 16: ParseSince with "0.5w" or fractional weeks**

`ParseFloat("0.5", 64)` = 0.5. `d = 0.5 * 7 * 24h = 84h`. `d > 0` → accepted. Fine.

**Bug candidate 17: ParseSince with very large values**

`"999999999d"` — `ParseFloat` returns 999999999. `d = 999999999 * 24h`. This is a very large duration but `time.Duration` is int64 nanoseconds. `999999999 * 24 * 3600 * 1e9` = way more than `math.MaxInt64` = ~9.2e18. Actually, `999999999 * 24 * 3600 * 1e9` ≈ 8.64e19, which overflows int64. So `time.Duration(n * float64(per))` would overflow.

Let me check: `per = 24 * time.Hour = 24 * 3600 * 1e9 = 864e11 = 8.64e11`. `n * float64(per) = 999999999 * 8.64e11 ≈ 8.64e20`. `time.Duration(8.64e20)` — `time.Duration` is `int64`, and the max is ~9.2e18. So this would overflow to a negative or garbage value.

If the overflowed value is negative, `d <= 0` would catch it. But if it overflows to a positive value, it would be accepted as a garbage duration. However, this is an extreme edge case that's unlikely in practice. The user would have to pass a very large duration string.

Actually, let me think about this more carefully. `float64(per)` for days is `86400000000000` (8.64e13). Wait, `time.Hour = 3600 * 1e9 = 3.6e12`. `24 * time.Hour = 8.64e13`. `n * 8.64e13` for `n = 999999999` = `8.64e22`. Converting to `time.Duration` (int64): `8.64e22` overflows int64 (max ~9.2e18). The float64-to-int64 conversion would produce a garbage value.

Actually, in Go, when converting a float64 to int64 (which is what `time.Duration(n * float64(per))` does), if the value overflows, the result is implementation-defined. It could be 0, or MaxInt64, or MinInt64, or some garbage.

If it becomes 0 or negative, `d <= 0` catches it. If it becomes a large positive, it's accepted as a garbage duration. But this is a very extreme edge case.

Is this worth flagging? It's a real overflow, but the input is unrealistic. Let me think about whether a more reasonable input could cause this.

`MaxInt64 / (24 * time.Hour)` = `9223372036854775807