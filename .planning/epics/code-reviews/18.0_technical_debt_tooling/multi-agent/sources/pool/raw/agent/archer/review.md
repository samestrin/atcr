Let me analyze the changed code for snippet-level bugs.

Let me go through the files carefully:

1. **cmd/atcr/debt.go** - New file with `newDebtCmd`, `loadRecords`, `runDebtList`, `renderDebtTable`, `truncate`

2. **cmd/atcr/debt_add.go** - New file with `newDebtAddCmd`, `runDebtAdd`, `promptEntry`

3. **cmd/atcr/debt_dashboard.go** - New file with `newDebtDashboardCmd`, `runDebtDashboard`, `checkDashboard`

4. **internal/debt/add.go** - New file with `Section`, `insertRow`, `AppendItem`, `RefreshStats`, `renderStatsBlock`

5. **internal/debt/aggregate.go** - New file with `Component`, `Summarize`, `ageDays`, `bandLabel`

6. **internal/debt/dashboard.go** - New file with `RenderDashboard`, `monthHistogram`

7. **internal/debt/debt.go** - New file with `Record`, `Load`, `Flatten`, `Filter`, `Apply`, `Sort`, `SyncShards`

Let me look for bugs:

### 1. `cmd/atcr/debt.go` - `truncate` function

```go
func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
```

This looks okay. If `len(r) <= n`, return s. Otherwise, take `n-1` runes and add ellipsis. But wait - if `n == 0`, then `len(r) <= 0` would be false for any non-empty string, and then `r[:n-1]` = `r[:-1]` which would panic. But `n` is hardcoded to 60 in the call site, so this isn't reachable in practice. Still, it's a latent bug.

Actually, let me check - `truncate(r.Problem, 60)` - n is always 60. So this is not a practical issue.

### 2. `cmd/atcr/debt_add.go` - `promptEntry`

Looking at the `ask` function:
```go
ask := func(label, dflt string, required bool) string {
    if perr != nil {
        return ""
    }
    for {
        if dflt != "" {
            _, _ = fmt.Fprintf(out, "%s [%s]: ", label, dflt)
        } else {
            _, _ = fmt.Fprintf(out, "%s: ", label)
        }
        if !sc.Scan() {
            if dflt != "" {
                return dflt
            }
            if !required {
                return ""
            }
            perr = fmt.Errorf("input ended before required field %q was provided", label)
            return ""
        }
        v := strings.TrimSpace(sc.Text())
        if v == "" {
            v = dflt
        }
        if v == "" && required {
            _, _ = fmt.Fprintf(out, "  %s is required; please enter a value.\n", label)
            continue
        }
        return v
    }
}
```

This looks okay. If `sc.Scan()` returns false (EOF), and there's a default, return default. If no default and not required, return "". If no default and required, set perr and return "".

### 3. `internal/debt/add.go` - `RefreshStats`

```go
func RefreshStats(readmePath, modDate string) error {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read README: %w", err)
	}
	content := string(data)

	statsIdx := strings.Index(content, "## Stats")
	lmIdx := strings.Index(content, "**Last Modified:**")
	if statsIdx < 0 || lmIdx < 0 || lmIdx < statsIdx {
		return nil // no recognizable stats block to refresh
	}
	// Extend the replaced span to the end of the Last Modified line.
	lmLineEnd := len(content)
	if nl := strings.IndexByte(content[lmIdx:], '\n'); nl >= 0 {
		lmLineEnd = lmIdx + nl
	}

	shards, err := tdmigrate.ParseREADME(content)
	if err != nil {
		return fmt.Errorf("parse README for stats: %w", err)
	}
	sum := Summarize(Flatten(shards), time.Time{}, 0)

	block := renderStatsBlock(sum, modDate)
	updated := content[:statsIdx] + block + content[lmLineEnd:]
	if err := os.WriteFile(readmePath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write README stats: %w", err)
	}
	return nil
}
```

Wait - there's a potential issue here. `AppendItem` calls `RefreshStats` AFTER writing the README and syncing shards:

```go
func AppendItem(readmePath, itemsDir string, sec Section, it tdmigrate.Item, stderr io.Writer) error {
	// ... read README, insert row, validate, write README ...
	if err := SyncShards(readmePath, itemsDir, stderr); err != nil {
		return fmt.Errorf("regenerate shards after add: %w", err)
	}
	// Keep the authoritative README self-consistent: refresh its Stats table and
	// Last Modified summary so they agree with the row just appended.
	if err := RefreshStats(readmePath, sec.Date); err != nil {
		return fmt.Errorf("refresh README stats after add: %w", err)
	}
	return nil
}
```

So `RefreshStats` reads the README that was just written (with the new row), parses it, and recomputes stats. That seems correct.

But wait - `RefreshStats` replaces from `## Stats` to the end of the `**Last Modified:**` line. Let me check what `renderStatsBlock` produces vs what gets replaced.

The replacement is: `content[:statsIdx] + block + content[lmLineEnd:]`

So everything from `## Stats` to the end of the Last Modified line is replaced with the new block. The new block is:
```
## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| CRITICAL | ... |
...

**Last Modified:** ... | **Open Items:** ... | ...
```

And `lmLineEnd` is the position of the newline after the Last Modified line. So `content[lmLineEnd:]` starts with that newline. The block doesn't end with a newline (the Fprintf for Last Modified doesn't add one). So the result would be:

```
...preamble...\n## Stats\n\n| Severity |...|\n...| LOW | ... |\n\n**Last Modified:** ... | **Total Items:** N**Last Modified:** ... 
```

Wait, no. Let me re-read. `lmLineEnd` is `lmIdx + nl` where `nl` is the index of the newline after `**Last Modified:**`. So `content[lmLineEnd:]` starts with `\n` (the newline character itself). And `block` ends with the Last Modified summary line WITHOUT a trailing newline.

So the result is:
```
content[:statsIdx] + block + content[lmLineEnd:]
```

Where `block` ends with `...**Total Items:** N` (no newline) and `content[lmLineEnd:]` starts with `\n...`.

So the join is: `...**Total Items:** N` + `\n...` = `...**Total Items:** N\n...`

That looks correct. The old content after the Last Modified line (starting with the newline) is preserved.

But wait - what about the content between `## Stats` and `**Last Modified:**`? The old content has:
```
## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 0 | 0 |
| MEDIUM | 0 | 0 | 0 |
| LOW | 0 | 0 | 0 |

**Last Modified:** ...
```

And `statsIdx` points to `## Stats` and `lmLineEnd` points to the newline after `**Last Modified:**`. So everything from `## Stats` through the end of the Last Modified line is replaced. That's correct.

But there's a subtle issue: `strings.Index(content, "## Stats")` could match `## Statistics` or some other heading that starts with `## Stats`. This is a substring match, not a line match. But in practice, the TD README has `## Stats` as a heading, so this is unlikely to be a problem. Still, it's fragile.

Actually, let me look more carefully. The existing README has:
```
## Stats

| Severity | Open | Deferred | Resolved |
```

And `strings.Index(content, "## Stats")` would match `## Stats` at the start of that line. That's fine.

But what if there's a `## Statistics` section? Then `## Stats` would match inside `## Statistics`. This is a theoretical concern but unlikely in this specific README.

### 4. `internal/debt/add.go` - `insertRow` - section boundary detection

```go
for i := secIdx + 1; i < len(lines); i++ {
    if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
        break
    }
    if strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
        lastPipe = i
    }
}
```

Wait - `strings.HasPrefix(strings.TrimSpace(lines[i]), "#")` - this would match any line starting with `#`, including `# Technical Debt` (h1), `## Stats` (h2), `### [date]` (h3), etc. The comment says "Using any `#`-prefixed line as the boundary (not just `### `)" which is intentional. But this also means that a table row that starts with `|` but has a `#` after trimming would not be matched as a pipe line. That's fine since `|` and `#` are different characters.

But wait - what about a line that starts with `#` but is actually part of a code block or comment? In Markdown, `#` at the start of a line is a heading. But inside a code block, it's literal. The parser doesn't distinguish code blocks here. This could be a problem if a section's table is followed by a code block that contains `#`-prefixed lines before the next heading. But this is unlikely in the TD README format.

### 5. `internal/debt/aggregate.go` - `Summarize` - Top priority sorting

```go
// Top priority: severity then age (oldest first), deterministic on ties.
_ = Sort(top, SortSeverity) // severity rank, then Date asc, then File
if topN >= 0 && len(top) > topN {
    top = top[:topN]
}
s.Top = top
```

Wait - `Sort` sorts by `SortSeverity` which is: severity rank, then Date ascending (older first), then File. But the comment says "severity then age (oldest first)". That's correct.

But `topN >= 0` - what if `topN` is 0? Then `len(top) > 0` would be true if there are items, and `top[:0]` would give an empty slice. So `s.Top` would be empty. That's the behavior described in the TD item: "`atcr debt dashboard --top 0` prints '_No unresolved items._' under Top Priority even when unresolved items exist". This is the known issue.

But wait - there's also a negative `topN` case. If `topN < 0`, the condition `topN >= 0` is false, so no truncation happens. All unresolved items are included. Is that intended? Looking at the dashboard command:

```go
top, _ := cmd.Flags().GetInt("top")
content := debt.RenderDashboard(recs, top)
```

The default is 10. A user could pass `--top -1` and get all items. That might be intentional (negative means "all"). But it's not documented. This is a minor issue.

### 6. `internal/debt/debt.go` - `filePath` function

```go
func filePath(file string) string {
	i := strings.LastIndex(file, ":")
	if i < 0 {
		return file
	}
	tail := file[i+1:]
	if tail == "" {
		return file
	}
	for _, r := range tail {
		if (r < '0' || r > '9') && r != '-' {
			return file // not a line/range suffix; keep verbatim
		}
	}
	return file[:i]
}
```

This strips a trailing `:line` or `:line-range` suffix. But what about a Windows path like `C:\path\to\file.go:123`? The `LastIndex(file, ":")` would find the last colon, which is the one before `123`. That's correct. But what about `C:\path` (no line number)? `LastIndex` finds the colon after `C`, and `tail` is `\path`, which contains non-digit characters, so it returns the original. That's correct.

What about a file path with no line number but a colon in the path on Windows? Like `C:\Users\test\file.go`? `LastIndex` finds `:`, tail is `\Users\test\file.go`, which has non-digits, so returns original. Correct.

### 7. `internal/debt/dashboard.go` - `monthHistogram`

```go
func monthHistogram(recs []Record) []monthCount {
	counts := map[string]int{}
	for _, r := range recs {
		if !unresolved(r) {
			continue
		}
		m := "unknown"
		if len(r.Date) >= 7 {
			m = r.Date[:7]
		}
		counts[m]++
	}
	// ...
}
```

This takes the first 7 characters of the date as the month. If the date is `2026-07-03`, it takes `2026-07`. That's correct. But if the date is `2026-7-3` (no zero-padding), it takes `2026-7-` which is wrong. However, the shard format uses `YYYY-MM-DD` with zero-padding, so this should be fine.

### 8. `internal/debt/add.go` - `AppendItem` - double write issue

```go
func AppendItem(readmePath, itemsDir string, sec Section, it tdmigrate.Item, stderr io.Writer) error {
	data, err := os.ReadFile(readmePath)
	// ...
	updated, err := insertRow(string(data), sec, it)
	// ...
	if _, err := tdmigrate.ParseREADME(updated); err != nil {
		return fmt.Errorf("refusing to write README: ...", err)
	}
	if err := os.WriteFile(readmePath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write README: %w", err)
	}
	if err := SyncShards(readmePath, itemsDir, stderr); err != nil {
		return fmt.Errorf("regenerate shards after add: %w", err)
	}
	if err := RefreshStats(readmePath, sec.Date); err != nil {
		return fmt.Errorf("refresh README stats after add: %w", err)
	}
	return nil
}
```

After writing the README with the new row, `SyncShards` regenerates shards from the README. Then `RefreshStats` reads the README again, recomputes stats, and writes it again. But `RefreshStats` calls `tdmigrate.ParseREADME(content)` to parse the README. This should work because the README was just written with the new row.

But there's a subtle issue: `RefreshStats` replaces the `## Stats` block. After `RefreshStats` writes the updated README, the shards are now stale (they were generated from the README before the stats refresh). But the stats block doesn't contain any items - it's just summary counts. So the shards should still be correct because the item rows haven't changed. The `ParseREADME` in `SyncShards` would have parsed the items, and `RefreshStats` only changes the stats summary, not the item rows. So this should be fine.

Actually wait - let me re-read `RefreshStats` more carefully:

```go
shards, err := tdmigrate.ParseREADME(content)
if err != nil {
    return fmt.Errorf("parse README for stats: %w", err)
}
sum := Summarize(Flatten(shards), time.Time{}, 0)

block := renderStatsBlock(sum, modDate)
updated := content[:statsIdx] + block + content[lmLineEnd:]
```

So it reads the current README content, parses it to get all items, computes summary stats, and then replaces the Stats block. The items in the README are not touched. So the shards generated by `SyncShards` are still valid. Good.

But there's another issue: `RefreshStats` writes the README, but the shards were already generated from the pre-stats-refresh version. Since the stats block doesn't affect the parsed items, the shards are still correct. But if someone later runs `SyncShards` again, it would parse the same items. So no issue.

### 9. `cmd/atcr/debt_add.go` - `runDebtAdd` - flag vs interactive mode

```go
switch {
case sev != "" && file != "" && problem != "" && fix != "" && category != "":
    // Flag mode
    sec = debt.Section{Date: def.Date, SourceType: def.SourceType, Label: def.Label}
    it = tdmigrate.Item{
        Group: def.Group, Status: def.Status, Severity: strings.ToUpper(sev),
        File: file, Problem: problem, Fix: fix, Category: category,
        EstMinutes: est, Source: def.Source,
    }
case debtStdinIsTTY(cmd.InOrStdin()):
    // Interactive wizard
    var err error
    sec, it, err = promptEntry(cmd.InOrStdin(), cmd.OutOrStdout(), def)
    if err != nil {
        return err
    }
default:
    return usageError(fmt.Errorf("missing required flags ..."))
}
```

The TD item says: "On a TTY, partially-supplied required flags are ignored and the wizard re-prompts every field from scratch because wizardDefaults is not seeded from the flags already passed". This is a known issue.

But there's another issue: if the user provides SOME but not ALL required flags on a TTY, the wizard runs and re-prompts for everything. The flags that were provided are ignored (except for the defaults like date, group, status, etc.). This means if the user provides `--severity HIGH --file x.go:1` but forgets `--problem`, they go into the wizard and have to re-enter severity and file. This is the known TD item.

### 10. `internal/debt/add.go` - `renderRow` - `sanitizeCell` on Group

```go
cells := []string{
    sanitizeCell(it.Group),
    box,
    it.Severity,
    sanitizeCell(it.File),
    sanitizeCell(it.Problem),
    sanitizeCell(it.Fix),
    sanitizeCell(it.Category),
    strconv.Itoa(it.EstMinutes),
    sanitizeCell(it.Source),
}
```

`it.Group` is sanitized, but `it.Severity` is not. If severity contains a pipe, it would break the table. But severity is validated by `it.Validate()` which checks it's one of CRITICAL|HIGH|MEDIUM|LOW, so it can't contain a pipe. OK.

`strconv.Itoa(it.EstMinutes)` - if `EstMinutes` is negative, this would produce a negative number in the table cell. That's fine for Markdown.

### 11. `internal/debt/aggregate.go` - `Summarize` - severity matching

```go
if sc, ok := sevIdx[strings.ToUpper(r.Severity)]; ok {
    sc.Total++
    // ...
}
```

If `r.Severity` is not one of the four canonical values (e.g., empty or unknown), it won't be counted in any severity bucket. But it will still be counted in the total and in the status counts. This means the sum of `BySeverity` totals might not equal `s.Total`. This is by design (unknown severities sort last and aren't in the canonical four).

### 12. `internal/debt/debt.go` - `Filter.Match` - Component prefix

```go
if f.Component != "" && !strings.HasPrefix(filePath(r.File), f.Component) {
    return false
}
```

This uses `strings.HasPrefix` for path prefix matching. But this means `--component internal/autofix` would match `internal/autofix/apply.go:108` (after stripping `:108`), which gives `internal/autofix/apply.go`. And `strings.HasPrefix("internal/autofix/apply.go", "internal/autofix")` is true. Good.

But `--component internal/aut` would also match `internal/authentication/...`. This is a prefix match, not a path-segment match. This might be intentional (the help says "path prefix"), but it could be surprising. Not a bug per se.

### 13. `cmd/atcr/debt.go` - `loadRecords` - sync error handling

```go
func loadRecords(cmd *cobra.Command) ([]debt.Record, error) {
	items, _ := cmd.Flags().GetString("items")
	readme, _ := cmd.Flags().GetString("readme")
	sync, _ := cmd.Flags().GetBool("sync")

	if sync {
		if err := debt.SyncShards(readme, items, cmd.ErrOrStderr()); err != nil {
			return nil, err
		}
	}
	return debt.Load(items)
}
```

If `--sync` fails, the error is returned. But the shards might be in a partially-written state (from `tdmigrate.WriteShards` which removes all existing *.yaml before writing new ones). If sync fails mid-write, the existing shards are gone and the load would fail or return partial results. This is the known TD item about `WriteShards` removing all existing files first.

### 14. Let me look at `internal/debt/add.go` more carefully - `insertRow` section finding

```go
secIdx := -1
for i, ln := range lines {
    if strings.TrimSpace(ln) == hdr {
        secIdx = i
        break
    }
}
```

This does an exact match on the trimmed line. The header is `### [2026-07-03] From Sprint: manual`. If the README has `### [2026-07-03] From Sprint: manual ` (trailing space), it would still match because of `TrimSpace`. Good.

But what if there are multiple sections with the same header? It finds the first one. That's probably fine - there should be only one section per date+source+label.

### 15. `internal/debt/add.go` - `appendNewSection` - trailing newline handling

```go
func appendNewSection(content, hdr, row string) string {
	trimmed := strings.TrimRight(content, "\n")
	var b strings.Builder
	b.WriteString(trimmed)
	b.WriteString("\n\n")
	b.WriteString(hdr)
	b.WriteString("\n\n")
	b.WriteString(newSectionHeaderRow)
	b.WriteString("\n")
	b.WriteString(newSectionSepRow)
	b.WriteString("\n")
	b.WriteString(row)
	b.WriteString("\n")
	return b.String()
}
```

This trims trailing newlines from content, adds two newlines, then the header, etc. The result ends with `\n` after the row. This looks correct.

But what if `content` is empty? `strings.TrimRight("", "\n")` returns `""`. Then the builder writes `"" + "\n\n" + hdr + ...`. So the result starts with `\n\n### [date]...`. That means there are two blank lines at the start. If the README was empty, this would produce a file starting with two newlines. Not ideal but not a bug.

### 16. `internal/debt/add.go` - `RefreshStats` - `Summarize` with `time.Time{}` and `topN=0`

```go
sum := Summarize(Flatten(shards), time.Time{}, 0)
```

`topN=0` means `s.Top` will be empty (since `topN >= 0 && len(top) > 0` is true, so `top = top[:0]`). But `RefreshStats` only uses `sum.BySeverity`, `sum.Open`, `sum.Deferred`, `sum.Resolved`, `sum.Total`. It doesn't use `sum.Top`. So this is fine.

### 17. Let me look at the `checkDashboard` function more carefully:

```go
func checkDashboard(cmd *cobra.Command, out, content string) error {
	existing, err := os.ReadFile(out)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("dashboard %s does not exist; run `atcr debt dashboard` to generate it", out)
		}
		return fmt.Errorf("read dashboard: %w", err)
	}
	if string(existing) != content {
		return fmt.Errorf("dashboard %s is out of date; regenerate with `atcr debt dashboard`", out)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Dashboard %s is up to date.\n", out)
	return err
}
```

The comparison `string(existing) != content` is a byte-level comparison. If the on-disk file has a different newline at the end (e.g., `\r\n` on Windows), this would fail. But the dashboard is generated on the same platform, so this should be consistent. Not a practical issue.

### 18. Let me look at `internal/debt/dashboard.go` - `RenderDashboard` - the `--top 0` issue

```go
sum := Summarize(recs, time.Time{}, topN)
```

If `topN` is 0, `sum.Top` is empty. Then:

```go
if len(sum.Top) == 0 {
    b.WriteString("_No unresolved items._\n")
}
```

This prints "_No unresolved items._" even when there ARE unresolved items but `--top 0` was passed. This is the known TD item. The fix would be to distinguish between "no unresolved items" and "top list suppressed because --top 0".

### 19. Let me look at `internal/debt/aggregate.go` - `bandLabel`

```go
func bandLabel(days int) string {
	for _, b := range ageBands {
		if b.max < 0 || days <= b.max {
			return b.label
		}
	}
	return ageBands[len(ageBands)-1].label
}
```

The `ageBands` are:
```go
{"0-7d", 7},
{"8-30d", 30},
{"31-90d", 90},
{">90d", -1},
```

For `days = 0`: `b.max = 7`, `0 <= 7` → "0-7d". Correct.
For `days = 8`: `b.max = 7`, `8 <= 7` is false. Next: `b.max = 30`, `8 <= 30` → "8-30d". Correct.
For `days = 100`: `7` no, `30` no, `90` no, `b.max = -1`, `-1 < 0` → ">90d". Correct.

But this function is used in `Summarize` which is called by `RenderDashboard` with `time.Time{}`. Let me check what happens with `time.Time{}`:

```go
func ageDays(r Record, now time.Time) (int, bool) {
	d, err := time.Parse("2006-01-02", strings.TrimSpace(r.Date))
	if err != nil {
		return 0, false
	}
	days := int(now.Sub(d).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days, true
}
```

With `now = time.Time{}` (January 1, year 1), `now.Sub(d)` would be negative for any real date, so `days` would be 0. So all items would be in the "0-7d" bucket. But `RenderDashboard` doesn't use `ByAge` - it uses `monthHistogram` instead. So this is fine.

But `RefreshStats` calls `Summarize` with `time.Time{}` and `topN=0`, and it doesn't use `ByAge` either. So the zero-time age computation is harmless.

### 20. Let me look at `cmd/atcr/debt_add.go` - the `wizardDefaults` struct

```go
type wizardDefaults struct {
	Date, SourceType, Label string
	Group, Status, Source   string
	Est                     int
}
```

And in `runDebtAdd`:
```go
def := wizardDefaults{
    Date: date, SourceType: mustFlag(cmd, "source-type"), Label: mustFlag(cmd, "label"),
    Group: mustFlag(cmd, "group"), Status: mustFlag(cmd, "status"), Source: mustFlag(cmd, "source"),
    Est: est,
}
```

The `Source` field in `wizardDefaults` is the capture source (e.g., "manual"), and the `SourceType` is "Sprint" or "Review". These are different fields. Let me make sure there's no confusion.

In `promptEntry`:
```go
date := ask("Date (YYYY-MM-DD)", def.Date, false)
stype := ask("Source type (Sprint|Review)", def.SourceType, false)
label := ask("Label", def.Label, true)
group := ask("Group", def.Group, false)
sev := ask("Severity (CRITICAL|HIGH|MEDIUM|LOW)", "", true)
file := ask("File (file:line)", "", true)
problem := ask("Problem", "", true)
fix := ask("Fix", "", true)
category := ask("Category", "", true)
estStr := ask("Est minutes", strconv.Itoa(def.Est), false)
status := ask("Status (open|deferred|resolved)", def.Status, false)
source := ask("Source", def.Source, false)
```

And then:
```go
sec := debt.Section{Date: date, SourceType: stype, Label: label}
it := tdmigrate.Item{
    Group: group, Status: status, Severity: strings.ToUpper(sev),
    File: file, Problem: problem, Fix: fix, Category: category,
    EstMinutes: est, Source: source,
}
```

This looks correct. The `Source` field in the item is the capture source, and `SourceType` in the section is Sprint|Review.

### 21. Let me look at the `truncate` function again more carefully:

```go
func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
```

If `n == 1` and `len(r) > 1`, then `r[:0]` is empty, and the result is `"…"`. That's fine.
If `n == 0` and `len(r) > 0`, then `len(r) <= 0` is false, and `r[:n-1]` = `r[:-1]` which panics. But `n` is always 60 in the call site.

### 22. Let me look at `internal/debt/add.go` - `insertRow` - the `lastPipe < 0` case

```go
if lastPipe < 0 {
    // Section header with no table beneath it (unusual, e.g. a stub). Rebuild
    // the block: header row + separator + the new row, inserted right after
    // the section header line.
    block := []string{newSectionHeaderRow, newSectionSepRow, row}
    lines = insertAt(lines, secIdx+1, block)
    return strings.Join(lines, "\n"), nil
}
```

If the section header exists but has no table beneath it, this inserts a new table right after the header. But it doesn't add a blank line between the header and the table. The existing format has:
```
### [date] From Sprint: label

| Group | | Severity | ...
```

With a blank line between the header and the table. But this code inserts:
```
### [date] From Sprint: label
| Group | | Severity | ...
|-------|---|...
| 1 | [ ] | HIGH | ...
```

Without a blank line. Would `ParseREADME` still parse this? It might, since Markdown tables don't require a blank line before them. But it's inconsistent with the existing format. Let me check if this is a real issue...

Actually, looking at `appendNewSection`:
```go
b.WriteString(hdr)
b.WriteString("\n\n")
b.WriteString(newSectionHeaderRow)
```

This adds a blank line. But the `lastPipe < 0` case doesn't. This is a minor inconsistency, but it might cause `ParseREADME` to fail if it requires a blank line after the heading. Let me check what `ParseREADME` does...

I don't have the `ParseREADME` code, but the test `TestInsertRow_AppendsToExistingSection` has:
```
"### [2026-07-03] From Sprint: manual\n\n" +
"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
```

With a `\n\n` between the header and the table. So the parser likely expects a blank line. The `lastPipe < 0` case doesn't add one, which could cause the row to not be parsed. But this case is described as "unusual, e.g. a stub" and might not be hit in practice.

Actually, wait. Let me re-read the code. When `lastPipe < 0`, the section header exists but has no pipe lines beneath it. The loop scans from `secIdx + 1` until it hits a `#`-prefixed line or EOF, looking for pipe lines. If it finds none, `lastPipe` stays -1.

In this case, the code inserts the table right after `secIdx + 1`. But if there's content between the header and the next heading (e.g., prose), the table would be inserted before that prose, without a blank line separator. This could be problematic.

But this is an edge case (section header with no table), and the code handles it by creating a new table. The lack of a blank line is a minor formatting issue that might or might not affect parsing. Let me flag this if it's a real concern.

Actually, let me think about this more. If the section is:
```
### [2026-07-03] From Sprint: manual

Some prose here.

## Next Section
```

Then `secIdx` points to the header line. The loop starts at `secIdx + 1`:
- Line `secIdx + 1` is `""` (blank) - not `#`, not `|`, skip.
- Line `secIdx + 2` is `"Some prose here."` - not `#`, not `|`, skip.
- Line `secIdx + 3` is `""` (blank) - skip.
- Line `secIdx + 4` is `"## Next Section"` - starts with `#`, break.

So `lastPipe` is -1. The code inserts the table at `secIdx + 1`:
```
### [2026-07-03] From Sprint: manual
| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [ ] | HIGH | ...

Some prose here.

## Next Section
```

This puts the table immediately after the header with no blank line, and the prose follows. This is malformed Markdown - the table should have a blank line before the prose. And `ParseREADME` might not parse this correctly.

But this is an edge case. Let me check if there's a test for it... I don't see one. This could be a real bug if someone has a section header with prose but no table. But the TD format always has a table after a section header, so this might never happen in practice.

Let me move on to more impactful findings.

### 23. `internal/debt/debt.go` - `SyncShards` - error handling

```go
func SyncShards(readme, itemsDir string, stderr io.Writer) error {
	code := tdmigrate.Run([]string{"migrate", "--readme", readme, "--items", itemsDir}, io.Discard, stderr)
	if code != 0 {
		return fmt.Errorf("shard sync failed: td-migrate migrate exited %d", code)
	}
	return nil
}
```

This calls `tdmigrate.Run` with `io.Discard` for stdout. The stderr is forwarded. If the migrate fails, the error message says "shard sync failed: td-migrate migrate exited N". But the actual error details are in stderr, which was already written. So the caller gets a generic error. This is fine for CLI usage but could be confusing in tests.

### 24. Let me look at `cmd/atcr/debt.go` - `renderDebtTable