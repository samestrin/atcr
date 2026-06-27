<think>
Let me carefully review this diff for the technical-debt format migration (Epic 12.1). The diff is in scope - it's all about the `internal/tdmigrate/` package and the `cmd/td-migrate/` entry point.

Let me go through each file:

1. **cmd/td-migrate/main.go** - Simple shim, looks fine.

2. **internal/boundaries_test.go** - Adds tdmigrate to allowed internal imports. Fine.

3. **internal/tdmigrate/item.go** - Item model, validation, status/checkbox mapping.
   - The `validSeverities` map uses uppercase keys but `NormalizeSeverity` uppercases input. So validation depends on prior normalization. Tests show `validItem()` uses `"LOW"` directly. But in `ParseREADME`, `NormalizeSeverity` is called before storing in `Severity`. However, in `WriteShards` path - it directly marshals already-normalized values. OK.
   - But wait - what about when items are constructed via YAML decoding? If a user puts `low` in YAML, it decodes as `low` and then `Validate()` would fail. That's expected behavior per strict validation mandate.

4. **internal/tdmigrate/parse.go** - README parser.
   - `driftHeader` regex matches `^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):`. This won't match the same line as `sectionHeader` since `sectionHeader` already matches Sprint/Review variants. But what about lines like `### [2026-06-26] From Epic:` - that's caught. Good.
   - **Issue**: The `driftHeader` check is done AFTER the `sectionHeader` check, which is fine. But the driftHeader also won't match sectionHeader variants, so they'll fall through.
   - **Issue**: `driftHeader` matches if `From ([^:]+):` - but the section header has `From (Sprint|Review):`. What if a section is `### [2026-06-26] From:` (missing type)? That wouldn't match `sectionHeader` (because it requires the type) and wouldn't match `driftHeader` (because `[^:]+` requires at least one non-colon char before the `:`). So it would silently be ignored. But this is a degenerate case.
   - **Issue**: `splitRow` - what if a cell contains a literal `|`? The `cell()` function in `generate.go` replaces `|` with `/` to avoid this. The parser would split on `|`, which means the data row would have too many cells and fail loudly. OK, that's intended.
   - **Issue**: `rowToItem` accepts 9 or 11 cells. If `len(cells) == 11` but the section was declared 9-column (no header detected), it would be parsed as 11-col. But that's edge-case.
   - **Issue**: `splitReviewers` returns nil for empty input - good for round-trip equality.
   - **Issue**: In `rowToItem`, the `Status` field is the return from `CheckboxToStatus` which is already in the normalized enum. Good.
   - **Issue**: What about a cell that's a confidence value that's empty? `it.Confidence = cells[10]` - if empty, it'd be "". That's fine for an optional field. But `Validate()` doesn't enforce a value when `len(cells) == 11`. OK.

5. **internal/tdmigrate/shard.go** - Shard file I/O.
   - `WriteShards` prunes all `*.yaml` files in dir before writing. This is fine per the design (idempotent).
   - **Issue**: `ShardFilename` - if two sections have the same date AND label (different SourceType, like "Sprint" vs "Review"), they get different filenames due to the collision logic. The `used` map disambiguates. OK.
   - **Issue**: But wait - `ShardFilename` doesn't include SourceType in the base name. So `2026-06-26_x.yaml` and `2026-06-26_x-2.yaml` could be confused. This is a minor concern but the design explicitly chose "sharded by provenance unit" and `### [date] From Sprint: x` vs `### [date] From Review: x` are distinct provenance units.
   - **Issue**: `MarshalShard` uses `yaml.NewEncoder` - this could emit multiple YAML documents if there are multiple `Encode` calls, but here there's only one call. OK.
   - **Issue**: `MarshalShard` doesn't add a `---` document start marker. yaml.v3's encoder doesn't by default. Some style guides recommend it. Minor.
   - **Issue**: When `WriteShards` prunes `*.yaml`, what about non-`.yaml` files like `.gitkeep`? Those wouldn't match `*.yaml`, so they wouldn't be pruned. OK.

6. **internal/tdmigrate/validate.go** - Strict-load validator.
   - `DecodeShardStrict` uses `KnownFields(true)` and `Validate()`. Good.
   - **Issue**: `ValidateDir` doesn't validate that the dir exists - if it doesn't, `filepath.Glob` returns empty list and `ValidateDir` returns 0, nil. That's a quiet success for a missing dir. Could be an issue for CI usage - if the dir doesn't exist, you don't want CI to pass.
   - **Issue**: `dec.KnownFields(true)` - this will reject ALL unknown fields. But what about the YAML library emitting `!!str` tags? Probably fine.

7. **internal/tdmigrate/generate.go** - Table regenerator.
   - `cell()` replaces `|` with `/` and newlines with spaces. This is documented as lossy for the ToC summary. Good.
   - **Issue**: The format string for the wide row: `fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |", ...)`. This emits exactly 9 cells separated by ` | `. The wide version adds ` %s | %s |` for reviewers/confidence. But what about trailing pipe? Let me check: the data row starts with `| ` and ends with the wide-format suffix which is ` %s | %s |` (no trailing newline needed but no leading pipe). Looking at the code: `b.WriteString("\n")` at the end. So data row ends with `|\n` - good, ends with closing pipe.
   - **Issue**: `b.WriteString` for the table separator uses `|-------|---|...` - the dashes are 3+. Markdown separator row needs at least 3 dashes in each cell. The code uses 3 dashes - good.
   - **Issue**: The 9-column separator is `|-------|---|----------|------|---------|-----|----------|-------------|--------|` - that's 9 cells, good.
   - **Issue**: `sectionHasReviewers` only returns true if ANY item has reviewers or confidence. So a single section might be rendered as 9-col even if most items have reviewers. This is by design.
   - **Issue**: `cell(it.Group)` - if group is empty (validated against), would render as empty cell `|  |`. OK.

8. **internal/tdmigrate/run.go** - CLI dispatch.
   - Looks fine. The `--allow-empty` flag default is false (correct).
   - **Issue**: In `runGenerate`, the generated output is written to stdout without a trailing newline. The test checks `ParseREADME(gen)` works, but maybe stdout should end with newline. Minor.
   - **Issue**: `usage` const is shared - looks fine.

Now let me think about real issues:

**HIGH severity issues:**
1. `ValidateDir` returns `0, nil` if dir doesn't exist or has no YAML files. This could allow CI to silently pass when shards haven't been written. This is in scope.

2. `MarshalShard` - the yaml.v3 encoder by default uses 4-space indent unless set otherwise. The code sets `SetIndent(2)`. But more importantly, when using an encoder with multiple shards, only the first call might emit the `---` marker. Not an issue here since there's one Encode call.

3. `ValidateDir` does NOT check the case where a shard's filename prefix (`date_label`) doesn't match the contents (date/label inside the file). A malicious or hand-edited shard could have mismatched filename and content. This is more of a robustness issue.

4. `driftHeader` regex - wait, let me think again. The regex is `^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):`. The sectionHeader regex is `^### \[(\d{4}-\d{2}-\d{2})\] From (Sprint|Review): (.+)$`. So driftHeader matches lines starting with `### [date] From <non-empty>:` but sectionHeader requires `Sprint|Review`. So driftHeader catches other types.

   But driftHeader would NOT match lines like `### [2026-06-26] From:` (empty type before colon) - the `[^:]+` requires at least one char. That case would silently fall through. Not a high-severity issue.

   Actually, wait. `driftHeader` uses FindStringSubmatch and matches the same line format but with a non-recognized source. The order of checks is: sectionHeader first, driftHeader second, then process as data row. So if driftHeader matches, we error out. If neither matches, the line might be processed as data row (if `cur != nil && starts with |`). 

   The issue is: a line like `### [2026-06-26] From Epic:` is correctly caught. But what about `#### [2026-06-26] From Sprint: x` (4 hashes)? It doesn't match sectionHeader or driftHeader (because `###` vs `####`). Would silently fall through. Edge case.

5. `splitRow` - what about leading whitespace? `strings.TrimSpace(line)` first, then trim leading/trailing pipes. So leading whitespace is OK. But what about a row like `   | 1 | [ ] | ...` - after TrimSpace, the leading pipe is trimmed correctly.

6. **HIGH**: In `WriteShards`, the function prunes all `*.yaml` files in the target dir. But what if the user points `--items` to a directory that already contains other YAML files (e.g., user config)? They'd be wiped. This is documented as "dir is owned entirely by this tool" but it's a foot-gun. Could be a HIGH issue but it's documented.

7. **HIGH**: `runGenerate` writes the regenerated table to stdout, but `usage` says "print regenerated ToC to stdout". The code doesn't add a leading newline before the table - so the section header `### [date] From ...:` starts at column 1. If you redirect to a file, the file would not start with a newline. Minor.

8. **HIGH**: `runValidate` doesn't check that the dir exists. If `items` is an empty/missing path, `ValidateDir` returns `0, nil` and the CLI prints "validate: 0 shard(s) OK". That's a silent pass for a missing dir.

9. The Item's `Group` field is required but is a free-form string (per spec it's "U" or a number). The validation just checks it's non-empty. That's OK.

10. **HIGH**: `ValidateDir` does not check for files matching `*.yaml` patterns that aren't actually shards (e.g., empty files, hidden files, etc.). It just calls `DecodeShardStrict` on whatever it finds. Empty files would cause yaml.v3 to return EOF - and that's reported as an error. Good.

11. **HIGH**: In `runMigrate`, the order is: read README → parse → check shard count → validate → write. But the validation uses `s.Validate()` which checks item-level enums. The status enum check is correct, but severity enum uses uppercase keys. Since parsing normalizes severity via `NormalizeSeverity`, that's OK. But what if a hand-edited shard has lowercase severity? It would fail validation. That's intentional.

12. **HIGH**: `MarshalShard` returns the bytes from yaml.v3's encoder. The encoder by default uses tag prefixes like `!!str` for strings? No, yaml.v3 with native types doesn't emit explicit tags unless necessary. OK.

13. **MEDIUM**: The `driftHeader` error message format includes `strings.TrimSpace(line)` and `strings.TrimSpace(dm[1])` - that's a bit redundant but OK.

14. **MEDIUM**: `ParseREADME` does not handle CRLF line endings - `strings.Split(content, "\n")` leaves `\r` at end of each line. Then `strings.TrimSpace` in `splitRow` removes it. OK, works.

15. **MEDIUM**: In `shard.go`, `LoadShards` calls `DecodeShardStrict` which validates schema. But if a shard has empty items, `Validate()` says "shard has no items" - which is enforced. So an empty shard would fail validation. Good.

16. **MEDIUM**: `WriteShards` after pruning, when there are no shards to write, leaves an empty directory. That's OK.

17. **MEDIUM**: `DecodeShardStrict` returns `Shard{}` even on error - the Shard value is meaningless on error, but the caller typically only checks err. OK.

18. **MEDIUM**: `runGenerate` - the generated output starts with `### [date]...` directly. If you redirect to a file, the file might lack a trailing newline. The code is `_, _ = fmt.Fprint(stdout, table)` - so the output is whatever `GenerateTable` returns. Let me check: the table ends with `\n\n` after the last item's row. Actually, looking at `GenerateTable`:
```
for _, it := range s.Items {
    ...
    b.WriteString("\n")  // end of row
}
b.WriteString("\n")  // blank line after section
```
So yes, ends with `\n\n`. Good.

19. **MEDIUM**: The `EstMinutes` field type is `int`, but the spec says "est_minutes 0-string-vs-int trap". When marshaling `0`, yaml.v3 emits `est_minutes: 0` (as int). When unmarshaling, `0` decodes as int 0. If a hand-edited shard has `est_minutes: "0"` (quoted string), yaml.v3 would error because the field type is int. Good strict behavior.

20. **HIGH**: Looking more carefully at `runMigrate`: it calls `fs.Parse(args)` then uses `*readme` and `*items`. The flags are declared before Parse... let me look again:
```go
fs, readme, items := newFlags("migrate", args, stderr)
allowEmpty := fs.Bool("allow-empty", false, ...)
if err := fs.Parse(args); err != nil {
    return 2
}
```
That's fine - flags must be declared before Parse.

21. **HIGH**: `runMigrate` writes the result count using `len(written)` and `itemCount`. The output format is `migrate: wrote %d shard(s), %d item(s) to %s\n`. Looks fine.

22. **MEDIUM**: The `validate.go` `ValidationError` type embeds `Failures []string`. The error message format `"%d invalid shard(s):\n  %s"` - if `len(failures) == 0`, no message. But the function never returns an empty `ValidationError`. OK.

23. **MEDIUM**: In `runMigrate`, after a successful migrate, no log is written if validation passes but written is empty (0 shards). The output would be `migrate: wrote 0 shard(s), 0 item(s) to <dir>`. OK.

24. **MEDIUM**: `item.go` defines `Status` as `string` rather than a typed enum. This is a Go style choice - using `string` for enum allows direct marshaling. OK.

25. **MEDIUM**: The `validSeverities` and `validStatuses` maps are package-private. They're initialized in the package init implicitly. OK.

26. **MEDIUM**: `NormalizeSeverity` trims and uppercases. But what about other encodings (e.g., case-folding for Unicode)? `strings.ToUpper` does basic ASCII uppercasing. For Turkish locale "i" → "İ" etc. But severities are ASCII-only. OK.

27. **MEDIUM**: `MarshalShard` uses `SetIndent(2)`. yaml.v3 default is 4. The test `TestMarshalShard_MultilineBlockScalar` checks for block scalar `|-` or `|`. That's about indentation of the block scalar's content, which should still work with 2-space indent. OK.

28. **MEDIUM**: When yaml.v3's encoder emits a `|` block scalar, the indent is relative to the parent key. With `SetIndent(2)`, block scalar content indentation should be 2 more than the key. This should be fine.

29. **HIGH** (maybe): I want to think about `ParseREADME` more carefully. The parser handles:
   - Section headers (Sprint/Review)
   - Drift headers (other From X: types) - hard error
   - Header row (starts with `Group`) - skip
   - Separator row (all dashes) - skip
   - Data row (9 or 11 cells) - parse

   What about a row that has 9 cells but is actually the header row of a different section (not the first section)? Like:
   ```
   ### [date] From Sprint: x
   
   | Group | ... | Source |
   |-------|... |--------|
   | 1 | [ ] | ...
   
   ### [date] From Sprint: y
   
   | Group | ... | Source |
   ```
   
   The second `| Group |...` row would be skipped by `isHeaderRow`. Good.

   What about a row that has 9 cells but is not a valid row (e.g., separator with 9 dashes)? `isSeparatorRow` returns true for that. Good.

30. **MEDIUM**: What about backtick in markdown? `cell()` doesn't escape backticks. Markdown tables don't need backtick escaping, but if a cell starts with `|` and contains a backtick... Actually, `|` is already replaced, so that's fine.

31. **MEDIUM**: `GenerateTable` doesn't include the title (`# Technical Debt Tracking`) or Stats table or How-to-Use. It only emits section headers + tables. So if you used `generate` output as a full README replacement, you'd lose the front matter. But the design says `generate` doesn't overwrite the README anyway. OK.

32. **MEDIUM**: `driftHeader` - I noticed `[^:]+:` matches the type but `sectionHeader` requires `(Sprint|Review)`. What about `### [2026-06-26] From ` (with trailing space, no type before colon)? It doesn't match `sectionHeader` (no type) and doesn't match `driftHeader` (`[^:]+` requires ≥1 char). So it'd silently fall through. Edge case, probably not worth flagging.

33. **MEDIUM**: The `driftHeader` regex `^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):` - the `[^:]+` doesn't allow `:` in the type, so `### [date] From Foo:Bar:` would match with type=`Foo`. OK.

34. **HIGH** (looking again): In `runGenerate`, after `fs.Parse(args)`, the result of parsing is the `--items` flag's value. If the user passes `--items` pointing to a missing directory, `LoadShards` returns empty list (filepath.Glob on nonexistent dir returns empty), then `GenerateTable` is called with `[]Shard{}` and returns empty string. The CLI prints empty stdout. That's a silent success for a missing dir.

   But wait, `ValidateDir` doesn't check dir existence either - same issue.

35. **MEDIUM**: The `usage` const is a single string with `\n` characters but no actual newlines in the source - it's a multi-line string literal. Wait, no - Go raw strings use backticks, which DO preserve newlines. Looking at:
```go
const usage = `td-migrate — migrate technical-debt storage to shard-by-source YAML (additive)

Usage:
  td-migrate migrate  ...
  td-migrate generate ...
  td-migrate validate ...`
```
Yes, this is a raw string literal that preserves newlines. Good.

36. **MEDIUM**: `usage` is used both in `Run` (for unknown subcommand error) and in `Run` (for help). The format works.

37. **MEDIUM**: `cmd/td-migrate/main.go` - the package comment says "this shim only wires os.Args/streams to tdmigrate.Run". That's accurate. It returns `os.Exit(tdmigrate.Run(...))`. OK.

38. **MEDIUM**: I notice `TestRun_NoArgsAndUnknown` calls `run(t, "--help")` - the `Run` function checks for `-h`, `--help`, `help` subcommands and prints usage to stdout. The test verifies stdout contains "td-migrate". OK.

39. **MEDIUM**: The `driftHeader` regex check happens BEFORE the `cur == nil` check. So a drift header at the top of the file (no current shard) would still be caught. Good.

40. **MEDIUM**: Hmm, let me think about whether `ValidateDir` is too lenient. If dir has 0 yaml files, it returns `(0, nil)` - "validate: 0 shard(s) OK". This could mask missing shards in CI. HIGH severity.

41. **HIGH**: In `WriteShards`, after pruning all `*.yaml` files, if `shards` is empty (and `--allow-empty` was set), the function returns `(nil, nil)` and the dir is wiped. The CLI then prints "migrate: wrote 0 shard(s), 0 item(s) to <dir>". This is the documented behavior with `--allow-empty`. OK.

42. **MEDIUM**: The `ParseREADME` function - what if the README content has BOM? `strings.Split(content, "\n")` doesn't strip BOM. The first section header might be `\xEF\xBB\xBF### [date]...` which doesn't match the regex. Then it falls through. Edge case.

43. **MEDIUM**: The `validate.go` `ValidationError.Failures` field is exported. Could be considered an API surface issue. Minor.

44. **HIGH**: Let me check if there's an issue with the YAML output when items are nil. `MarshalShard` on a `Shard{Items: nil}` would emit `items: []` (yaml.v3 default) or `items: null`. Actually yaml.v3 with native types emits `items: []` for nil slices (no `null`). But after `Validate()`, a shard with 0 items fails. So this doesn't happen in practice.

45. **HIGH**: Looking at `GenerateTable`, when `wide == true`, the table uses 11-column format. When `wide == false`, 9-column. The decision is per-section based on whether ANY item has reviewers/confidence. But what if a section is generated as 9-col (no reviewers) but the original table was 11-col? That's fine because the parsed data wouldn't have reviewers.

46. **MEDIUM**: `MarshalShard` uses `enc.SetIndent(2)`. But there's no `SetIndent` on `yaml.NewDecoder` - that's for encoding only. OK.

47. **MEDIUM**: Hmm, let me check `LoadShards`. It reads each file, calls `DecodeShardStrict`, which calls `Validate()`. If schema validation fails, the entire `LoadShards` returns an error. The caller (runValidate's `ValidateDir`) collects failures into `ValidationError`. Good.

48. **MEDIUM**: In `runMigrate`, after parsing and validating, the function writes shards. But it doesn't `os.MkdirAll` the items dir... wait, it does: `WriteShards` does `os.MkdirAll(dir, 0o755)`. OK.

49. **HIGH**: I want to check if `MarshalShard` correctly handles `Reviewers: nil`. When unmarshaling a 9-col section, the shard's items have `Reviewers: nil`. When marshaling, yaml.v3 should emit `reviewers: null` or omit (because of `omitempty`). Looking at the tag: `yaml:"reviewers,omitempty"`. So if nil/empty, omit. Good.

   But what about a slice with 1 element vs 0 elements? `omitempty` for slices means "omit if len == 0". So an empty slice is also omitted. That's the same as nil. OK.

50. **MEDIUM**: After `MarshalShard`, the resulting YAML might not have a trailing newline. The encoder emits `...yaml_content` and `Close()` flushes. The output might not end with `\n`. Looking at yaml.v3 source... actually `yaml.NewEncoder` writes content with newlines between top-level keys but typically ends with `\n`. Let me check the test: `TestWriteAndLoadShards_RoundTrip` doesn't check trailing newlines. So no issue.

OK, let me finalize the findings. Looking at the in-scope items:

**HIGH severity:**
1. `ValidateDir` silently passes when the dir doesn't exist or has no YAML files. This violates the "fail loudly, never corrupt silently" mandate for the validation gate. If CI runs `td-migrate validate` against a fresh clone where `items/` doesn't exist, it would report "0 shard(s) OK" instead of failing.

2. Same issue in `LoadShards` (used by `runGenerate`) - returns empty list for missing dir without error.

3. `WriteShards` prunes all `*.yaml` files in the target directory before writing. This is documented but is a foot-gun if the user points `--items` to a directory containing unrelated YAML files.

4. `MarshalShard` doesn't set any explicit YAML version directive. yaml.v3 defaults to YAML 1.2. OK, minor.

**MEDIUM severity:**
1. `item.go` `Validate()` checks `Severity` against `validSeverities` map but the keys are uppercase. If an item is constructed with lowercase severity and never normalized (e.g., via YAML unmarshal), it would fail validation. That's intended behavior per "strict" mandate, but worth noting.

2. The `driftHeader` regex `^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):` doesn't match a line like `### [date] From:` (empty type) - would silently skip. Edge case.

3. `ParseREADME` doesn't strip BOM from the input. Edge case.

4. `ValidateDir` doesn't check that files are actually valid (they all might be in a malformed state) - well, it does check each via `DecodeShardStrict`. OK.

5. `runGenerate` writes generated table to stdout, but the design says it should also support a `--dry-run` flag for "one-time human-inspectable diff". The current implementation always writes to stdout without an opt-in. This contradicts the sprint plan clarifications: "An optional --dry-run stdout flag on generate may provide a one-time human-inspectable diff". The current implementation doesn't have a `--dry-run` flag - generate always writes to stdout. That's actually a reasonable interpretation (generate is always dry-run since it doesn't write to README), but a missing option is a deviation.

Wait, let me re-read: "`generate` is **additive / proven-only** this epic: it is built and proven by a Go round-trip test, but does **not** overwrite the live `.planning/technical-debt/README.md` table." So `generate` always writes to stdout (which is essentially `--dry-run` behavior). The `--dry-run` flag is mentioned as optional. This is fine.

6. `MarshalShard` doesn't add a trailing newline guarantee. Minor.

7. `generate.go` `cell()` replaces `|` with `/` and newlines with spaces, which is lossy for the ToC. This is documented. But what about the `Source` field if it contains pipe? Also replaced. Could cause confusion. Documented.

8. `shard.go` `WriteShards` uses 0o644 for shard files - reasonable default. Could be 0o600 for stricter permissions but minor.

9. In `runGenerate`, there's no `--dry-run` flag. The clarifications say it's optional. This is fine.

10. `validate.go` `ValidationError.Failures` is exported (capital F). Minor API surface.

**LOW severity:**
1. `cmd/td-migrate/main.go` doesn't handle the case where `os.Args[1:]` is empty - wait, it does via `Run`'s `len(args) == 0` check.

2. `driftHeader` doesn't handle indented headers (e.g., `   ### [date] From Epic:`). Minor.

3. The Item model uses `string` for Severity/Status instead of typed enums. Style choice.

4. The `usage` const is a Go raw string with multi-line content. Works correctly.

5. The error messages in `ParseREADME` use line numbers (1-indexed) which is conventional. Good.

6. `runMigrate` --allow-empty flag is missing a help string in newFlags. Wait, let me check - it's declared via `fs.Bool("allow-empty", false, "permit writing when the README parses to zero sections (wipes the shard store)")`. Has a help string.

7. `runMigrate` `fs.Parse(args)` doesn't check for trailing positional args. Could be an issue if user passes extra positional args.

Let me also think about whether the live README test (`TestLiveREADME_SemanticRoundTrip`) could cause issues. It reads `.planning/technical-debt/README.md` - if the file doesn't exist, the test is skipped (`t.Skipf`). OK.

Now let me think about correctness issues:

1. **CORRECTNESS**: `ParseREADME` uses `FindStringSubmatch(line)` - if the line ends with `\r` (Windows line endings), the `$` anchor in `^### \[(\d{4}-\d{2}-\d{2})\] From (Sprint|Review): (.+)$` won't match because of the trailing `\r`. Wait, Go's `regexp` package doesn't support multiline mode by default, but `$` matches before a final `\n` if the `m` flag is set... Actually in Go, by default `$` matches at end of string and before `\n`. Let me verify: in `regexp.Compile`, the default mode has `$` match at end of text or before `\n`. So `(.+)$` would match if line ends with `\n`. If line ends with `\r\n` and we split by `\n`, each line has trailing `\r`. The regex `$` would match before the `\r` (since `\r` isn't `\n`). So the match should work, but the captured `m[3]` would include the trailing `\r`. That could be a minor issue - `Label` would have a trailing `\r`.

   Actually wait, `strings.Split(content, "\n")` for `"a\r\nb"` gives `["a\r", "b"]`. The `\r` stays on the first line. The regex `^### \[(\d{4}-\d{2}-\d{2})\] From (Sprint|Review): (.+)$` would match the line with `\r` and `m[3]` would be the label + `\r`. Then `strings.TrimSpace(m[3])` is applied, which trims `\r`. OK.

   But what about `driftHeader`'s match? `dm[1]` would have trailing whitespace + `\r` - also trimmed via `strings.TrimSpace(dm[1])`. OK.

   Hmm, but the line number `n+1` in the error message - if the file has `\r\n` line endings, the line numbers would be off by one (because `\r\n` is one line but `strings.Split` gives the `\r` as part of the previous line). Actually no, `strings.Split("a\r\nb", "\n")` gives `["a\r", "b"]` which is 2 elements. So line numbers would still be 1 and 2. OK.

2. **CORRECTNESS**: In `rowToItem`, when `cells` has 11 columns, `it.Reviewers = splitReviewers(cells[9])` and `it.Confidence = cells[10]`. But what if `cells[10]` is empty? Then `it.Confidence = ""`. OK, the `omitempty` tag means it won't be emitted in YAML. Good.

3. **CORRECTNESS**: The `cell()` function in `generate.go` replaces newlines with spaces, then `strings.TrimSpace`. So a problem like `"  foo  \n  bar  "` becomes `"foo     bar"` (with multiple spaces). The original newlines are lost in the ToC. Documented.

4. **CORRECTNESS**: `MarshalShard` with yaml.v3 - when marshaling an Item with `Reviewers: nil`, the `omitempty` tag should omit the field. But yaml.v3's behavior: `omitempty` for slices means omit if nil OR empty. So both nil and empty slices are omitted. Good.

5. **CORRECTNESS**: The `EstMinutes` field is `int`. When YAML has `est_minutes: "5"` (quoted string), yaml.v3 would error because it expects int. That's the strict behavior we want.

6. **CORRECTNESS**: The `Status` field is `string`. When YAML has `status: open` (lowercase), it decodes as `open` (literal). Good - matches `StatusOpen`.

7. **CORRECTNESS**: When yaml.v3 unmarshals `status: Open` (capitalized), it decodes as `"Open"`. `Validate()` would fail because it's not in the enum. That's the strict behavior we want.

OK, let me also think about security:

1. The Item's `File` field is free-form text. If a path traversal string like `../../etc/passwd` is in there, it doesn't cause harm because the field is just stored/displayed.

2. The shard filenames use `sanitizeLabel` which strips non-`[a-z0-9._-]` chars. So path traversal is prevented. Good.

3. The `WriteShards` directory is created with `os.MkdirAll(dir, 0o755)`. The directory path comes from `--items` flag. If the user passes `--items /etc/important-dir`, the function would attempt to create or wipe it. This is a configuration issue, not a security vulnerability per se.

4. `os.WriteFile(path, data, 0o644)` - using 0o644 (world-readable). For technical debt metadata, this is fine.

5. No input is executed as shell commands. No SQL injection. No XSS.

OK let me also think about performance:

1. `ParseREADME` is O(n) where n is lines in README. Good.

2. `GenerateTable` is O(n*m) where n is shards, m is items per shard. Good.

3. `WriteShards` does `os.MkdirAll`, `filepath.Glob`, and a loop. All O(n) for shard count. Good.

4. `LoadShards` does `filepath.Glob`, sorts, then loops. All O(n log n). Good.

5. `ValidateDir` is O(n). Good.

OK, let me think about maintainability:

1. The `internal/tdmigrate/` package has 7 source files: item.go, parse.go, generate.go, shard.go, validate.go, run.go, plus tests. Reasonable separation.

2. The `Run` function is in `run.go` and is testable. The `cmd/td-migrate/main.go` is a one-line shim. Good separation.

3. Test coverage looks comprehensive - parse, generate, validate, shard I/O, footguns, end-to-end CLI.

4. Error messages are descriptive. Good.

5. The Item struct uses yaml.v3 tags with `omitempty` for optional fields. Good.

OK, let me finalize my findings. I want to focus on real issues:

**HIGH severity findings (real issues):**

1. `internal/tdmigrate/validate.go:36-39` - `ValidateDir` returns success (no error) when the directory doesn't exist or contains no `*.yaml` files. This silently passes the validation gate even when shards are entirely missing, which contradicts the "fail loudly, never corrupt silently" AC4 mandate.

2. `internal/tdmigrate/shard.go:67-74` - `WriteShards` unconditionally prunes ALL `*.yaml` files in the target directory before writing. If the user accidentally points `--items` to a directory with other YAML files (e.g., configuration), those files would be deleted. The function does not confirm the directory is owned by tdmigrate.

3. `internal/tdmigrate/parse.go:18` - The `driftHeader` regex `^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):` does NOT match headers with an empty source type (e.g., `### [2026-06-26] From: foo`), so such a malformed header would silently fall through and its data rows would be silently dropped, violating the loud-failure mandate.

Hmm, actually #3 might not be a real issue if such headers don't exist in practice. Let me deprioritize.

4. `internal/tdmigrate/run.go:99-105` - `runGenerate` calls `LoadShards` which silently returns an empty list for a missing directory. This means `generate` succeeds with empty output for a missing `--items` dir, which could mask data loss.

Hmm, this is similar to #1.

**MEDIUM severity findings:**

1. `internal/tdmigrate/parse.go:42-50` - `splitRow` splits a table row on `|`. The previous `cell()` function in `generate.go` replaces `|` with `/`, but `ParseREADME` doesn't reverse that substitution. So the round-trip `parse → generate → parse` works, but the round-trip `parse → generate → parse` on the original data might lose the `|` characters (replaced with `/`). Wait, let me re-read the test:
   
   `TestGenerateTable_SanitizesPipesAndNewlines` tests that after `Parse → Generate → Parse`, the row still has 1 item. It doesn't test that the `|` is preserved. The `cell()` function intentionally replaces `|` with `/` in the ToC (documented). The shard is the lossless source. So this is intentional. Not a bug.

2. `internal/tdmigrate/parse.go:32-39` - `ParseREADME` does not strip a UTF-8 BOM from the beginning of the content. If the README is saved with BOM (e.g., on Windows), the first line would have a `\ufeff` prefix and `sectionHeader` wouldn't match it. The data rows would then be silently dropped.

3. `internal/tdmigrate/item.go:91-105` - `Item.Validate()` checks that `Severity` is in the enum (uppercase keys), but it doesn't normalize the value first. Since `NormalizeSeverity` is called in `ParseREADME`, parsed items are fine. But if `Validate()` is called on a hand-edited YAML with lowercase severity, it would fail. That's the intended strict behavior.

4. `internal/tdmigrate/validate.go:28` - The `ValidationError` struct has an exported `Failures` field. This exposes internal state and makes the type's contract dependent on the field name. Minor API concern.

5. `internal/tdmigrate/run.go:39-41` - `Run` doesn't accept a `--version` flag or print version info. Minor.

6. `internal/tdmigrate/shard.go:107` - `MarshalShard` uses `yaml.NewEncoder` + `enc.Close()`, but doesn't handle the case where `enc.Encode` succeeds but `enc.Close` returns an error (e.g., write error). It does return the error, so it's fine.

7. `internal/tdmigrate/shard.go:84` - `WriteShards` uses `os.MkdirAll(dir, 0o755)`. The 0o755 permission is fine, but the function doesn't check if `dir` is actually a directory (vs a file). If `--items` points to an existing file, `MkdirAll` would fail. Actually `MkdirAll` returns an error if `dir` exists as a file. So it's fine.

8. `internal/tdmigrate/run.go:67` - `runMigrate` calls `fs.Parse(args)` and ignores any trailing positional arguments. If the user passes extra positional args, they're silently ignored. Minor.

9. `internal/tdmigrate/generate.go:24-27` - `GenerateTable` uses `sort.SliceStable` with the comparator `ordered[i].Date > ordered[j].Date` (newest first). But the comparison falls through to `Label` comparison only if `Date` differs. The Label comparator is `Label < Label` (ascending). So within the same date, sections are sorted alphabetically. OK.

10. `internal/tdmigrate/shard.go:21-29` - `sanitizeLabel` lowercases the label and replaces non-slug chars with `-`. So "11.0_executing_reviewers" becomes "11.0_executing_reviewers" (no change). And "  ///  " becomes "section". The replacement of `[^a-z0-9._-]+` with `-` could collapse runs of separators. Good.

**LOW severity findings:**

1. `cmd/td-migrate/main.go:14` - `os.Exit` is called directly in main. The exit code is from `Run`. Good practice.

2. `internal/tdmigrate/item.go:73-82` - `StatusToCheckbox` uses a switch statement. Could be a map lookup. Style choice.

3. `internal/tdmigrate/run.go:42-46` - The usage message could be more concise. Minor.

4. `internal/tdmigrate/item.go:91` - The validation function uses `map[string]string` literal for required field checks. Each iteration creates a new map (allocation). Performance: trivial. Style: minor.

5. `internal/tdmigrate/validate.go:33` - `ValidationError.Failures` field is exported. Could be unexported with an accessor. Minor.

OK let me also think about whether I'm missing any HIGH severity issues.

Looking at `WriteShards` again:
```go
existing, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
...
for _, f := range existing {
    if err := os.Remove(f); err != nil {
        return nil, err
    }
}
```

This removes all `*.yaml` files. If the user has YAML files in the items dir that aren't shards (e.g., a config or notes file), they get deleted. This is a real foot-gun. HIGH severity.

Also, this is documented as "dir is owned entirely by this tool, so the prune is safe" in the comment. So it's a documented behavior. But still a foot-gun if the user makes a mistake.

Let me check the sprint plan: "`migrate` may prune/rewrite its own prior output safely." - "its own prior output". So the assumption is that the dir only contains shards from previous migrate runs. But there's no enforcement of this assumption.

Actually, the comment says: "It is idempotent: it prunes its own prior output (every *.yaml already in dir) before writing, so re-running migrate never leaves orphaned shards from a previous run. dir is owned entirely by this tool, so the prune is safe."

So it's the design's responsibility to use a dedicated dir. If the user follows the design (uses `.planning/technical-debt/items/`), it's fine. The `defaultItems` constant is `.planning/technical-debt/items`. OK.

Let me also re-check `ValidateDir`:
```go
files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
if err != nil {
    return 0, err
}
sort.Strings(files)
var failures []string
for _, f := range files {
    ...
}
if len(failures) > 0 {
    return len(files), &ValidationError{Failures: failures}
}
return len(files), nil
```

If `dir` doesn't exist, `filepath.Glob` returns empty list (no error). The function returns `(0, nil)`. The CLI prints "validate: 0 shard(s) OK". Silent pass.

This is a HIGH severity issue because the validation gate is supposed to "fail loudly, never corrupt silently" (AC4). If the items dir is missing entirely (e.g., CI runs validate before migrate), the validation would silently pass.

Actually wait - in CI, you'd run `migrate` before `validate`. So validate against the just-written shards. But if a future workflow runs only validate (e.g., to verify the shards are correct), and the items dir doesn't exist, validate would silently pass.

This is a real issue. HIGH severity.

Let me also check `LoadShards`:
```go
files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
if err != nil {
    return nil, err
}
sort.Strings(files)
var shards []Shard
for _, f := range files {
    ...
}
return shards, nil
```

Same issue - missing dir returns empty list, no error. `runGenerate` would then call `GenerateTable([])` which returns empty string. CLI exits 0. Silent pass.

OK, HIGH severity issues:
1. `ValidateDir` - missing dir returns success
2. `LoadShards` - missing dir returns success (used by runGenerate)
3. `WriteShards` - prunes all *.yaml files (documented but foot-gun)

MEDIUM severity issues:
1. `ParseREADME` doesn't strip BOM
2. `driftHeader` doesn't match empty source type header
3. `ValidationError.Failures` exported field
4. Trailing positional args silently ignored in runMigrate
5. `runGenerate` doesn't add a final newline to stdout

LOW severity:
1. Style choices

Let me also check the `Run` function for any issues:
```go
func Run(args []string, stdout, stderr io.Writer) int {
    if len(args) == 0 {
        _, _ = fmt.Fprintln(stderr, usage)
        return 2
    }
    switch args[0] {
    case "migrate":
        return runMigrate(args[1:], stdout, stderr)
    case "generate":
        return runGenerate(args[1:], stdout, stderr)
    case "validate":
        return runValidate(args[1:], stdout, stderr)
    case "-h", "--help", "help":
        _, _ = fmt.Fprintln(stdout, usage)
        return 0
    default:
        _, _ = fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage)
        return 2
    }
}
```

Looks fine. Exit codes: 0 = success, 1 = error, 2 = usage/parse error. Standard.

Now let me also check if there's an issue with how `Run` handles when the subcommand is invoked with `--help` before it. E.g., `td-migrate migrate --help`. The current code:
- `args[0]` = "migrate", so it dispatches to `runMigrate(args[1:], ...)`
- `runMigrate` calls `fs.Parse(args)` which handles `--help` for migrate's flags.

OK that works because flag.ContinueOnError allows --help to be handled by the FlagSet.

Actually wait, `flag.ContinueOnError` returns the error from Parse, but `--help` causes Parse to call `fs.Usage()` and return `flag.ErrHelp`. Looking at `runMigrate`:
```go
if err := fs.Parse(args); err != nil {
    return 2
}
```
So `--help` would cause `return 2`. The usage is printed by the FlagSet (via `fs.SetOutput(stderr)`). That's OK behavior.

OK let me also think about thread safety. The `tdmigrate` package doesn't have any global mutable state, so it's thread-safe. Good.

Let me also check the live README test:
```go
func TestLiveREADME_SemanticRoundTrip(t *testing.T) {
    path := filepath.Join("..", "..", ".planning", "technical-debt", "README.md")
    data, err := os.ReadFile(path)
    if err != nil {
        t.Skipf("live README not readable: %v", err)
    }
    ...
}
```

If the README doesn't exist, the test is skipped. This is correct behavior.

But wait, `filepath.Join("..", "..", ".planning", "technical-debt", "README.md")` from the test file at `internal/tdmigrate/parse_test.go`. The test runs from the package directory, so `..` is `internal/`, `../..` is the repo root. Then `.planning/technical-debt/README.md`. OK.

OK let me finalize the findings. I think these are the most important:

**HIGH:**
1. `internal/tdmigrate/validate.go:36` - `ValidateDir` silently passes when dir is missing or empty (AC4 mandate violated)
2. `internal/tdmigrate/shard.go:64` - `LoadShards` silently returns empty list for missing dir (used by generate)
3. `internal/tdmigrate/shard.go:65-72` - `WriteShards` unconditional `*.yaml` prune is a foot-gun if dir contains non-shard YAML files

**MEDIUM:**
1. `internal/tdmigrate/parse.go:11-13` - `ParseREADME` doesn't strip UTF-8 BOM; could silently drop first section
2. `internal/tdmigrate/parse.go:18` - `driftHeader` regex doesn't match `### [date] From: foo` (empty source type); silently skipped
3. `internal/tdmigrate/validate.go:28` - `ValidationError.Failures` exported field
4. `internal/tdmigrate/run.go` - trailing positional args silently ignored

Let me also check `TestLiveREADME_SemanticRoundTrip` more carefully:
```go
if !reflect.DeepEqual(canonicalize(orig), canonicalize(reparsed)) {
    t.Error("live README round-trip lost or altered data")
}
```

This checks semantic equality. If the live README has a section that, after regeneration, differs in any field (e.g., a `|` replaced with `/`), the test would fail. So the test would correctly detect data loss. Good.

But wait - what about the `Problem` field? The `cell()` function replaces newlines with spaces, so a multi-line problem becomes a single-line problem in the regenerated table. Then `ParseREADME` would parse it as a single-line problem. So the round-trip would "succeed" (semantically equal) only if the multi-line problem was already a single line in the original. If the original has a multi-line problem, the round-trip would change it (newlines become spaces).

Hmm, but actually the live README's table cells are single-line by definition (Markdown tables don't allow multi-line cells). So the original problem field is always single-line. The round-trip would preserve it. OK.

But what about a `|` in the original problem? The `cell()` function replaces `|` with `/`. So the regenerated problem has `/` instead of `|`. Then `ParseREADME` would parse the regenerated table and get the problem with `/`. The round-trip would change `|` to `/`. So the live README round-trip test would fail if any problem contains `|`.

Wait, is this a real issue? The README is the source of truth, and the cell escaping is documented. So this is by design. But the live round-trip test would fail if there's a `|` in any problem. This is a contradiction.

Let me re-read the sprint plan:
- "The regenerated table must remain parseable by `td_stats`/`td_filter`/`td_matrix`/`group_td`/`promote-tech-debt`."
- "All columns from the current table must map losslessly to the shard schema."

The shards are lossless. The regenerated ToC is the lossy summary. So the round-trip test (parse → generate → parse) should compare the *shards* not the *cells*. But the current implementation parses the regenerated table and compares it to the original shards. This would fail if any cell has `|` or newlines.

Hmm, but the current README probably doesn't have `|` in problem/fix/category fields (because that would break the table format). So the live test might pass.

But wait - what about the `Source` field? The test `TestGenerateTable_SanitizesPipesAndNewlines` tests with `"a | b | c"` as source. After regeneration, it becomes `"a / b / c"`. The round-trip would lose this.

Hmm, but the test `TestGenerateTable_SemanticRoundTrip` uses `sample9Col` and `sample11Col` which don't have `|` in any field. So the round-trip works.

For the live README, if there's a `|` in any cell, the round-trip would fail. Is this a real issue?

Let me think... actually, the `cell()` function is called on each field when generating. The README parser doesn't reverse the `/` back to `|`. So:
- Original: `Source = "a | b"`
- Generate: `cell("a | b")` = `"a / b"`
- Re-parse: `Source = "a / b"`

The test `TestGenerateTable_SanitizesPipesAndNewlines` doesn't actually verify that the data is preserved - it only verifies that the structure is preserved (no phantom columns). So the test name is a bit misleading.

For the live README round-trip test, if the original Source has `|`, the round-trip would change it to `/`. So the test would fail.

This is a real correctness issue. MEDIUM severity.

But actually, looking at `sample9Col` and `sample11Col`, none of the cells contain `|`. So the test passes. For the live README, the test would only fail if any cell contains `|`. Whether this is a real issue depends on the actual content of the live README.

Hmm, I'll mention this as a MEDIUM issue because the round-trip contract (semantic equality) is broken for any cell containing `|` or newlines, even though the live README might not have those.

Actually wait, let me re-read the test:
```go
func TestGenerateTable_SemanticRoundTrip(t *testing.T) {
    orig, err := ParseREADME(sample9Col + "\n" + sample11Col)
    ...
    if !reflect.DeepEqual(canonicalize(orig), canonicalize(reparsed)) {
        t.Errorf("round-trip mismatch:\norig=%+v\nreparsed=%+v", canonicalize(orig), canonicalize(reparsed))
    }
}
```

This is testing with samples. The live test is similar but reads the live README. Both would fail if the data contains `|` or newlines.

This is a known limitation. The `cell()` function's escaping is intentional and documented. The test samples don't trigger the issue.

OK, this is more of a design limitation than a bug. I'll mention it as a LOW severity note.

Actually, looking again - the sprint plan says "AC2 is satisfied by a Go round-trip test asserting semantic equivalence (same item set + field values after table → shards → regenerated table)." So semantic equivalence should hold. But the cell escaping breaks this for cells with `|`.

Hmm, this is a real bug in the round-trip contract. MEDIUM severity.

Actually, the round-trip should be shards → regenerated table → shards (or shards → table → shards → shards). The current implementation is:
1. Parse original table → shards A
2. Generate table from shards A → regenerated table
3. Parse regenerated table → shards B
4. Compare shards A to shards B

This is what the test does. And it's broken if the original has `|` or newlines in any cell.

But wait - the original README is a Markdown table. By definition, cells can't contain `|` (without escaping) or newlines. So the README-to-shards step (1) wouldn't preserve `|` or newlines anyway - they'd have been replaced before reaching this tool. So the round-trip is "lossy" only in the sense that `|` and newlines in the original README would be lost, but the original README can't have those anyway.

So in practice, the round-trip would work. The `cell()` escaping is a safety measure for the regenerated ToC (in case some other tool puts `|` in a shard).

Hmm, OK so this isn't really a bug. The escaping is defensive.

Let me re-evaluate. The `cell()` function escapes for the regenerated ToC. The shards preserve the original data losslessly. The round-trip test goes through the ToC, so it's lossy for cells with `|` or newlines. But since the original README can't have those (Markdown table constraint), the lossless property holds.

So no bug here. The escaping is correct.

OK let me also think about whether there are issues with how `validate.go` aggregates errors. `ValidationError.Failures` is populated per file. The CLI prints `"validate: %v"` with the error, which prints all failures. Good.

Alright, let me also think about the marshal round-trip more carefully. `MarshalShard` uses yaml.v3 encoder. Does yaml.v3 emit a document start marker `---`? Let me think... by default, yaml.v3 doesn't emit `---` for the first document. So the shard file starts with `date: ...`. OK.

Wait, what about `enc.Close()`? Does it emit a `...` end marker? Let me check... yaml.v3's encoder.Close() flushes any pending data and emits `...` if there are multiple documents. For a single document, it just flushes. OK.

Let me also check the `Validate` field constraint. `Severity` is required (must be in enum). But what about a YAML with `severity: ""`? The decoder would set `Severity = ""`. `Validate()` checks `validSeverities[""]` = false, so it would fail. Good.

What about `severity: ` (no value)? yaml.v3 would error during decode. Good.

What about missing `severity`? yaml.v3 would set `Severity = ""` (default). `Validate()` would fail. Good.

What about extra fields? `severity_typo: LOW` would be rejected by `KnownFields(true)`. Good.

OK so the strict validation is comprehensive.

Let me also think about a potential issue with `MarshalShard` and `SetIndent(2)`. The yaml.v3 default indent is 4. The code sets 2. When unmarshaling, the indent doesn't matter (yaml.v3 is whitespace-tolerant within reason). So this should be fine.

Let me think about one more thing - the YAML output format. When `MarshalShard` emits an Item with all fields populated, the output would look like:
```yaml
date: 2026-06-26
source_type: Sprint
label: epic-11.2
items:
  - group: "1"
    status: open
    severity: LOW
    file: f.go:1
    problem: some problem
    fix: some fix
    category: correctness
    est_minutes: 5
    source: code-review
```

OK that's clean.

Now let me finalize my findings. I want to keep the list concise and actionable.

Looking once more at the diff for anything I might have missed:

The diff also modifies `internal/boundaries_test.go` to add `tdmigrate` to allowed imports. That's a test file update, not a real code change.

Final findings:

**HIGH:**
1. `internal/tdmigrate/validate.go:36` - `ValidateDir` returns success (0, nil) when dir missing/empty
2. `internal/tdmigrate/shard.go:86-89` - `LoadShards` returns empty list for missing dir (used by generate)
3. `internal/tdmigrate/shard.go:66-72` - `WriteShards` prunes all *.yaml files unconditionally

**MEDIUM:**
1. `internal/tdmigrate/parse.go:9` - `sectionHeader` doesn't strip BOM; first section lost on Windows-saved files
2. `internal/tdmigrate/parse.go:18` - `driftHeader` doesn't catch `### [date] From:` (empty type); silently drops section
3. `internal/tdmigrate/validate.go:27-28` - `ValidationError.Failures` is exported (API surface)
4. `internal/tdmigrate/validate.go:39` - `DecodeShardStrict` only decodes one document; multi-document YAML silently ignored

Actually, point 4 - `dec.Decode(&s)` reads one document. If a shard file has multiple YAML documents (separated by `---`), only the first is decoded, the rest are silently ignored. This could mask corruption. MEDIUM severity.

Let me think about this more. yaml.v3's Decoder.Decode() reads one document per call. After decoding, the next call would read the next document. The current code only calls Decode once. If the shard file has multiple documents, only the first is parsed. The rest are silently ignored.

This could be a real issue. For example, if someone hand-edits a shard and accidentally adds a `---` separator, the second document is silently ignored. The `Validate()` check on the first document passes, so `ValidateDir` reports success. The "silently ignored" data is lost.

MEDIUM severity.

Actually, yaml.v3's Decoder.Decode() returns `io.EOF` when there are no more documents. The current code returns the error if Decode fails. But if Decode succeeds for the first document and the second document exists, no error is returned.

Hmm, but this is probably fine in practice. A shard is supposed to be a single YAML document. If someone hand-edits and adds `---`, that's a corruption. The strict validation should catch it.

A simple fix: call Decode in a loop and require exactly one document. Let me check if this is worth flagging.

Actually, this is worth flagging as MEDIUM. A multi-document YAML should be rejected as malformed.

OK let me also re-check `ValidateDir` for the missing dir case. Actually, `filepath.Glob` on a missing dir returns empty list, no error. So the function returns `(0, nil)`. Silent pass.

To fix: check if dir exists at the start of `ValidateDir`. If not, return error.

Actually, thinking about this more - the "fail loudly" mandate for AC4 is about malformed shards, not about missing dirs. The validation gate's job is to verify that shards are correct. If there are no shards, there's nothing to validate. So returning `(0, nil)` might be acceptable.

But if the user's intent is "validate the sharded store", an empty store might mean the store was never created. Silent pass masks this.

Hmm, this is a judgment call. I'll mark it as MEDIUM severity rather than HIGH.

Actually wait, let me re-read the AC4 requirement: "A validation command strict-loads and schema-checks every shard; malformed input fails loudly." So malformed input fails loudly. Missing input is not addressed. The behavior of "0 shards OK" could be acceptable.

But for `generate`, if `--items` points to a missing dir, the CLI should probably error. Let me think...

Actually, `runGenerate` should probably check that the dir exists. If not, error out. Same for `ValidateDir`.

OK let me move on. I'll include this as a MEDIUM finding.

Let me also think about thread safety. The package doesn't have global mutable state, so concurrent calls to Run() are safe.

Let me also check error wrapping. The code uses `fmt.Errorf("...: %w", err)` to wrap errors. Good.

Let me also check that `Run` is exported (capital R). Yes, `func Run(args []string, stdout, stderr io.Writer) int`. Good.

OK let me finalize. I think the most important findings are:

1. **HIGH**: `WriteShards` unconditional prune is a foot-gun
2. **MEDIUM**: `ValidateDir` silent pass for missing/empty dir
3. **MEDIUM**: `LoadShards` returns empty for missing dir (generate silent success)
4. **MEDIUM**: `DecodeShardStrict` only reads first document; multi-doc YAML silently truncated
5. **MEDIUM**: `ParseREADME` doesn't strip BOM
6. **MEDIUM**: `driftHeader` doesn't catch empty-source-type header
7. **LOW**: Various minor issues

Hmm, let me reconsider severity. The sprint plan emphasizes:
- "loud failure" mandate
- "never silently ingest" for bad shards
- zero data loss

The "silent pass for missing dir" violates "loud failure" in spirit. The "unconditional prune" violates "zero data loss" if the user accidentally points to a non-shard dir.

OK let me set them as:
1. `WriteShards` prune - HIGH (data loss risk)
2. `ValidateDir` missing dir - MEDIUM (silent pass masks missing data)
3. `LoadShards` missing dir - MEDIUM (same)
4. `DecodeShardStrict` multi-doc - MEDIUM (silent truncation)
5. `ParseREADME` BOM - MEDIUM (silent data loss)
6. `driftHeader` empty source - LOW (edge case)

Let me also check `ValidationError.Failures` exported - LOW (API style).

Actually let me reconsider `WriteShards` prune. The function:
```go
existing, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
...
for _, f := range existing {
    if err := os.Remove(f); err != nil {
        return nil, err
    }
}
```

The dir is supposed to be `.planning/technical-debt/items/` (the default). If the user follows the design, this is safe. If they point to a different dir, they could lose data.

The sprint plan says "`migrate` may prune/rewrite its own prior output safely." So the design assumes the dir is dedicated to tdmigrate. The foot-gun is in user error, not the code.

I'll mark this as MEDIUM severity.

OK let me also reconsider whether the high-severity issues I found are really HIGH. Let me look at them once more:

1. `WriteShards` unconditional prune - if the user makes a config mistake, they lose data. MEDIUM.
2. `ValidateDir` silent pass for missing dir - if CI runs validate before migrate, validate passes. This could be a workflow bug. MEDIUM.
3. `LoadShards` silent empty for missing dir - if generate is run before migrate, output is empty. MEDIUM.
4. `DecodeShardStrict` multi-doc - rare in practice, but a real corruption vector. MEDIUM.
5. `ParseREADME` BOM - only affects Windows-saved files. MEDIUM/LOW.

So most of my findings are MEDIUM severity. Let me think if there are any HIGH severity issues I'm missing.

Hmm, let me look at `validate.go` once more:
```go
func DecodeShardStrict(data []byte) (Shard, error) {
    dec := yaml.NewDecoder(bytes.NewReader(data))
    dec.KnownFields(true)
    var s Shard
    if err := dec.Decode(&s); err != nil {
        return Shard{}, fmt.Errorf("yaml decode: %w", err)
    }
    if err := s.Validate(); err != nil {
        return Shard{}, fmt.Errorf("schema: %w", err)
    }
    return s, nil
}
```

`yaml.NewDecoder` reads a stream of documents. Calling `Decode` once reads one document. If the file has multiple documents, the first is decoded and the rest are left in the buffer.

The function doesn't check for trailing data. A multi-document shard file would be silently accepted (only the first document is validated).

To detect this, the code should call Decode in a loop and check that exactly one document is read. Or use `dec.Decode` followed by checking if the next `dec.Decode` returns `io.EOF`.

Let me check if yaml.v3 has a way to enforce single-document... Actually, the simplest fix is to call Decode twice and verify the second call returns io.EOF.

This is a real issue because the strict validation should catch ANY malformed input. A multi-document shard is malformed (shards should be single documents). MEDIUM severity.

OK, let me write up the findings.

Actually, one more thing. Let me check the `MarshalShard` output. When yaml.v3's encoder emits a `|` block scalar, does it use the right indent? The code sets `SetIndent(2)`, so block scalars should be indented by 2 more than the parent key. With parent key indent of 2 (under items), the block scalar content should be at indent 4. Let me check the test:

```go
func TestMarshalShard_MultilineBlockScalar(t *testing.T) {
    s := Shard{
        ...
        Items: []Item{{
            ...
            Problem: "line one\nline two",
            ...
        }},
    }
    data, err := MarshalShard(s)
    ...
    if !strings.Contains(string(data), "problem: |-") && !strings.Contains(string(data), "problem: |") {
        t.Errorf("multi-line problem not a block scalar:\n%s", data)
    }
}
```

The test checks for `problem: |-` or `problem: |`. With SetIndent(2), the `problem:` key would be at indent 4 (under items list which is at indent 2). The block scalar content would be at indent 6. yaml.v3 would emit:
```
items:
  - group: "1"
    ...
    problem: |-
      line one
      line two
```

The `|-` is correct. The test would pass. OK.

Hmm but wait, there's a potential issue. yaml.v3's encoder might emit `problem: |2` (with explicit indent indicator) or just `problem: |-` (defaulting to parent's indent). Let me check... actually, this is a yaml.v3 implementation detail. The test only checks for `problem: |-` or `problem: |`, both of which should work for round-trip.

OK let me finalize.

Hmm, let me also think about whether there are issues with `MarshalShard` and the way yaml.v3 handles omitempty for slices. If `Reviewers` is nil (default), `omitempty` should cause it to be omitted. If it's an empty slice `[]string{}`, also omitted. Good.

What about `Notes`? It's a string with omitempty. If empty, omitted. Good.

OK, I think I have a good list of findings. Let me write them up.

Actually, let me look once more at the `driftHeader` regex to see if there are any edge cases I missed.

`driftHeader = regexp.MustCompile(`^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):`)`

This matches lines starting with `### [date] From ` followed by one or more non-colon characters, then `:`.

What about `### [date] From Foo:` - matches, type=`Foo`.
What about `### [date] From:` - `[^:]+` requires ≥1 char. No match.
What about `### [date] From Foo: Bar:` - matches, type=`Foo` (because `[^:]+` is non-greedy by default? Actually `+` is greedy but `[^:]` stops at `:`. So `[^:]+:` matches `Foo:`, then `[^:]+` is `Bar`. Wait, no - the pattern is `From ([^:]+):`. The `[^:]+` matches `Foo` (no colon), then `:` matches. Then there's ` Bar:` left in the string but no more anchors. So the match is `From Foo:` with type=`Foo`. Good.

What about `### [date] From Foo: Bar` (no trailing colon)? The pattern requires `:` at the end. So no match. The line would fall through.

What about indented headers like `  ### [date] From Foo:`? The `^` anchor matches at start of string. If the line has leading whitespace, no match. The line falls through.

OK so there are edge cases. But the README format is supposed to have headers at column 0. Edge cases are unlikely.

Let me finalize.

Wait, I want to double-check one thing. The `driftHeader` regex is checked AFTER `sectionHeader`. So:
- `### [date] From Sprint: x` → matches sectionHeader → handled
- `### [date] From Epic: x` → doesn't match sectionHeader → matches driftHeader → error
- `### [date] From:` → doesn't match either → falls through → cur might be nil → continue
- `   ### [date] From Epic: x` → doesn't match either → falls through → if `cur != nil && starts with |` → false → continue

OK so indented headers would silently fall through. Same for empty-source headers. These are edge cases.

Actually, here's a thought: a line like `### [date] From Epic:` could fall through to be processed as a table row if it starts with `|` (it doesn't). Or be skipped if it doesn't start with `|` and `cur != nil`. Either way, the section's items would be processed under the previous `cur` (if any). That's a data corruption issue - the items would be attached to the wrong shard.

For example:
```
### [date1] From Sprint: a
... rows ...
### [date2] From Epic: x    <-- driftHeader should catch this
... rows ...
### [date3] From Sprint: b
... rows ...
```

If `driftHeader` doesn't catch `From Epic:` (e.g., empty source type), the rows between `From Epic:` and `From Sprint: b` would be attached to `Sprint: a` or `Sprint: b` depending on order. That's data corruption.

But this requires the driftHeader to not match the malformed header. For `From Epic:`, driftHeader matches (type=`Epic`). For `From:`, driftHeader doesn't match. So only the edge case of empty source type would cause corruption.

OK, MEDIUM severity for this edge case.

Let me also think about: what if a section header is on the same line as other content? Like `### [date] From Sprint: x some extra content`. The sectionHeader regex has `$` at the end, so the entire line must match. If there's extra content after the label, the match would fail. The line would then be checked against driftHeader. driftHeader requires `:` at the end of its match, but the regex doesn't anchor to end of line. Let me re-check:

`driftHeader = ^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):`

No `$` at the end. So `### [date] From Epic: x` would match driftHeader with type=`Epic`. The match doesn't require end of line. So extra content after `:` doesn't affect the match.

For sectionHeader:
`sectionHeader = ^### \[(\d{4}-\d{2}-\d{2})\] From (Sprint|Review): (.+)$`

The `$` anchors to end of line. So `### [date] From Sprint: x extra` would match with label=`x extra`. OK.

Hmm, but for driftHeader, if the line is `### [date] From Foo: Bar: baz`, the match would be `### [date] From Foo:` with type=`Foo`. The `Bar: baz` is left in the line. Since the line is processed as a drift header (returns error), the extra content doesn't matter.

OK so the driftHeader regex is fine.

One more thing - the `driftHeader` regex is intended to catch sections that would otherwise be silently dropped. Let me verify this works.

If the line is `### [date] From Epic: x`, driftHeader matches. The function returns error. Good.

If the line is `### [date] From Epic:` (no label after), driftHeader matches with type=`Epic`. The function returns error. Good.

If the line is `### [date] From:` (no type), driftHeader doesn't match. The line falls through. If the previous `cur` is non-nil and the line starts with `|`, it'd be processed as a table row (and fail with bad cell count). If not, it's skipped.

So for `### [date] From:`, the section is silently dropped. This is a real edge case but unlikely in practice.

OK I'll finalize.

Final findings (let me write them in TD_STREAM format):

1. HIGH|MEDIUM|LOW priority findings...

Actually let me think one more time about HIGH severity. Looking at the requirements:
- AC4: "A validation command strict-loads and schema-checks every shard; malformed input fails loudly."
- "A malformed shard must fail loudly (CI), never corrupt silently."

So the mandate is about malformed shards, not missing shards. The "silent pass for missing dir" is debatable. I'll keep it as MEDIUM.

The `WriteShards` prune is documented as intentional behavior. The user can choose `--items` freely. If they point to a non-shard dir, they lose data. But the design says the dir is owned by tdmigrate. So this is a configuration issue, not a code bug. MEDIUM.

Let me also think about the marshal/unmarshal round-trip more carefully. The `MarshalShard` uses yaml.v3 encoder with SetIndent(2). The output:
- Top-level keys at indent 0
- List items at indent 2
- Map keys within list items at indent 4

When unmarshaled, this is valid YAML. Good.

OK final findings. I'll keep it focused.

One more thought - I noticed the `TestRun_MigrateRefusesEmptyWipe` test creates a `2026-01-01_keep.yaml` file with content `date: x\n`. This file would be considered for prune by `WriteShards`. But the test expects the function to refuse (return 1) before writing. So the prune doesn't happen. Good.

But this test demonstrates the foot-gun: if the user runs `migrate` against a dir with non-shard YAML files, those files would be pruned. The `--allow-empty` flag is the guard, but only for the case where the README parses to 0 sections. For the case where the README parses to ≥1 sections, the prune happens unconditionally.

Hmm, this is a real issue. Let me think about it more.

Actually, the design says the dir is owned by tdmigrate. So the prune is correct. If the user follows the design, no problem.

But for defense in depth, the prune could be limited to files matching the shard naming pattern (`YYYY-MM-DD_*.yaml`). This would prevent accidental deletion of unrelated YAML files.

Let me check the shard filename pattern: `ShardFilename` returns `<date>_<label>.yaml` or `<date>_<label>-<N>.yaml`. So the pattern is `YYYY-MM-DD_*.yaml` (where `*` is the slug or slug-N).

If `WriteShards` pruned only files matching `YYYY-MM-DD_*.yaml`, it'd be safer. But it'd miss the case where a user has shards with a different naming convention.

OK this is a design tradeoff. I'll mention it as MEDIUM severity.

Alright, finalizing.

Let me also think about whether `runMigrate` should refuse to run if the target dir is a non-empty directory containing non-shard YAML files. A simple safety check would be: before prune, check if any file in the dir doesn't match the shard naming pattern. If so, refuse unless `--force` is passed.

But this is a design choice. The current design assumes the dir is dedicated to tdmigrate. I'll just note it as MEDIUM.

OK, finalizing now.

Let me also check one more time: `ValidationError.Failures` is exported. The `Error()` method is defined on the pointer receiver. The field is accessed as `e.Failures` (exported). Other code could read it. This is a minor API concern.

If the field were unexported, callers would need to use an accessor. But since it's the only state, exporting it is fine.

Actually, looking at the code:
```go
type ValidationError struct {
    Failures []string
}
```

The field is exported. The `Error()` method uses it. This is a simple struct error. Acceptable.

OK finalizing. Let me write the findings.

Hmm wait, I want to look once more at the test code for any issues.

`TestRun_ValidateCatchesBadShard` writes a file with `bogus_field: nope`. Then runs `validate` which calls `ValidateDir`. The file has unknown field, so `DecodeShardStrict` would return error. `ValidateDir` would collect it as a failure. The test expects exit code 1 with error message containing "validate:". OK.

`TestStrictLoad_RejectsTabIndentation` writes a YAML with a tab indentation. The strict decoder should reject it. The test verifies rejection. OK.

`TestStrictLoad_RejectsUnknownField` writes a YAML with `severity_typo: LOW`. The strict decoder should reject it. The test verifies error contains "field". OK.

`TestStrictLoad_RejectsSchemaViolation` writes a YAML with `severity: SPICY`. The strict decoder should reject it. The test verifies rejection. OK.

`TestFootguns_RoundTripAsStrings` tests the YAML footguns. Each footgun value is round-tripped through marshal/parse. The test verifies the values are preserved. OK.

`TestFootgun_MultilineBlockScalar` tests multi-line text. OK.

`TestFootgun_EstMinutesZeroStaysInt` tests the 0-string-vs-int trap. OK.

`TestMarshalShard_MultilineBlockScalar` tests that multi-line text is emitted as a block scalar. OK.

`TestSanitizeLabel` tests the label sanitization. OK.

`TestShardFilename_CollisionDisambiguates` tests collision handling. OK.

`TestWriteAndLoadShards_RoundTrip` tests disk round-trip. OK.

`TestWriteShards_IdempotentPrune` tests the prune behavior. OK.

`TestLiveREADME_SemanticRoundTrip` tests round-trip against live README. OK.

`TestGenerateTable_SanitizesPipesAndNewlines` tests pipe/newline handling. OK.

`TestGenerateTable_SemanticRoundTrip` tests round-trip with samples. OK.

`TestParseREADME_*` tests the parser. OK.

`TestItemValidate_*` tests item validation. OK.

`TestShardValidate_*` tests shard validation. OK.

`TestStatusToCheckbox_*` and `TestCheckboxToStatus_*` test the checkbox mapping. OK.

`TestNormalizeSeverity` tests severity normalization. OK.

OK, the tests look comprehensive. Good coverage.

Alright, let me write the final findings.

Hmm, let me think about one more potential issue. The `ParseREADME` function uses `strings.Split(content, "\n")`. For large READMEs (say, 10K lines), this creates a single string per line. The function then iterates and processes. Memory: O(n) where n is total lines. Acceptable.

For the live README round-trip test, the entire README is read into memory. Acceptable for a TD file.

OK, no performance issues.

Let me think about whether there are any concurrency issues. The `Run` function is stateless. Multiple concurrent calls would work fine. The file I/O operations in `WriteShards` and `LoadShards` are not concurrent-safe if multiple processes access the same dir simultaneously. But that's expected behavior.

OK final findings:

Let me list them one more time:

1. **HIGH** (or MEDIUM) - `ValidateDir` returns `(0, nil)` for missing/empty dir, silently passing the validation gate
2. **HIGH** (or MEDIUM) - `LoadShards` returns empty list for missing dir, causing `generate` to silently succeed with empty output
3. **MEDIUM** - `DecodeShardStrict` only reads first document; multi-document YAML silently truncates
4. **MEDIUM** - `ParseREADME` doesn't strip UTF-8 BOM; first section lost on Windows-saved files
5. **MEDIUM** - `driftHeader` regex doesn't catch `### [date] From:` (empty source type); silently dropped
6. **MEDIUM** - `WriteShards` unconditional `*.yaml` prune is a foot-gun if dir contains non-shard YAML files
7. **LOW** - `ValidationError.Failures` exported field (API style)
8. **LOW** - trailing positional args silently ignored in `runMigrate`

Hmm let me think about #1 and #2 again. If the user's CI workflow is:
1. `td-migrate migrate` - writes shards
2. `td-migrate validate` - validates shards

Then both `migrate` and `validate` would have valid input. The silent pass wouldn't trigger.

But if the workflow is:
1. `td-migrate validate` (assumes shards exist from a previous run)

Then if shards don't exist, validate silently passes. This is the bug.

Actually, thinking about it more - this is a defensive design issue. The validate command should explicitly check that the dir exists and has files. If not, it should error.

The sprint plan's AC4 says "A validation command strict-loads and schema-checks every shard". If there are 0 shards, there's nothing to validate. But "0 shards OK" is a misleading message - it could mean "no shards to validate" or "all 0 shards passed".

For clarity and correctness, the validate command should distinguish between "no shards to validate" (warn or error) and "all shards passed" (success).

HIGH severity because this could mask data loss in CI.

OK finalizing. Let me write the findings now.

Wait, actually, the `LoadShards` and `ValidateDir` issues are similar but affect different commands:
- `LoadShards` is used by `generate` - if dir missing, generate outputs empty string silently
- `ValidateDir` is used by `validate` - if dir missing, validate reports "0 shards OK" silently

Both are real issues. I'll list them separately.

Alright, final list. Let me also reconsider the multi-document YAML issue.

`DecodeShardStrict`:
```go
func DecodeShardStrict(data []byte) (Shard, error) {
    dec := yaml.NewDecoder(bytes.NewReader(data))
    dec.KnownFields(true)
    var s Shard
    if err := dec.Decode(&s); err != nil {
        return Shard{}, fmt.Errorf("yaml decode: %w", err)
    }
    if err := s.Validate(); err != nil {
        return Shard{}, fmt.Errorf("schema: %w", err)
    }
    return s, nil
}
```

After `dec.Decode(&s)` succeeds, there might be more data in the buffer (multi-document). The function doesn't check. So if the file is:
```yaml
date: 2026-06-26
source_type: Sprint
label: x
items: []
---
date: 2027-01-01
source_type: Sprint
label: y
items: []
```

The first document is decoded. The second is ignored. `Validate()` passes for the first. `DecodeShardStrict` returns success. The second document's data is silently lost.

This is a real issue. To detect, the code should call Decode in a loop and check for EOF after the first document.

Fix:
```go
if err := dec.Decode(&s); err != nil {
    return Shard{}, fmt.Errorf("yaml decode: %w", err)
}
// Ensure no extra documents follow.
if err := dec.Decode(&s); err != io.EOF {
    if err == nil {
        return Shard{}, fmt.Errorf("yaml decode: unexpected additional document")
    }
    return Shard{}, fmt.Errorf("yaml decode: %w", err)
}
```

OK this is a real issue. MEDIUM severity.

Alright, finalizing now.

Let me also check `MarshalShard` for multi-document handling. The encoder is used once with `enc.Encode(s)`. After `enc.Close()`, the buffer has one document. Good.

OK writing the findings now.

Actually, one more thing. The sprint plan says: "Adversarial fixture corpus — round-trip tests for known YAML footguns". The `TestFootguns_RoundTripAsStrings` test covers many footguns. But the test doesn't cover the "anchor and alias" footgun (e.g., `&anchor`, `*anchor`). Looking at the test:
```go
"empty-ish-braces": {"{not a map}", "[not a seq]", "&anchor"},
```

`&anchor` is tested but it's used as a string value, not as a YAML anchor. So it's tested as a footgun for string content, not as a YAML anchor/alias issue.

If someone hand-edits a shard and uses YAML anchors (e.g., `&shared [problem, fix, ...]: *shared`), the strict decoder might handle them differently. Let me think...

YAML anchors are valid YAML. They reference values. If a shard file uses anchors for shared values, the decoder should handle them correctly. The round-trip would not preserve anchors (they'd be inlined). This could be a minor issue for hand-edited files.

But this is unlikely in practice. I'll skip this.

OK, finalizing.

Let me also think about the `EstMinutes` field. The marshal emits `est_minutes: 0` for the zero value. The test `TestFootgun_EstMinutesZeroStaysInt` verifies this. But what if the field is missing from the YAML? The decoder would set `EstMinutes = 0` (Go default). `Validate()` would pass (0 >= 0). So a missing field is treated as 0. This might or might not be desired.

The current code doesn't enforce that `EstMinutes` is present in the YAML. The `Validate()` function only checks `EstMinutes >= 0`. So a missing field is silently treated as 0.

This could mask data loss if someone removes the field accidentally. MEDIUM severity.

Fix: in `DecodeShardStrict`, use a custom unmarshal hook or check for the field's presence. Or add an explicit `EstMinutes` field marker.

Actually, this is a trade-off. The simpler approach is to allow missing fields (treat as default). The strict approach is to require explicit presence.

The sprint plan doesn't specify. I'll skip this.

OK truly finalizing now.

Let me also check if there's an issue with the `Confidence` field. It's a string with `omitempty`. If empty, omitted. If non-empty (e.g., "HIGH"), emitted. The decoder would set `Confidence = ""` for missing field, which is the default. Validate() doesn't check Confidence (it's optional). Good.

OK finalizing.

Hmm, I want to look one more time at the `WriteShards` function. The comment says "It is idempotent: it prunes its own prior output (every *.yaml already in dir) before writing". So the design is explicit.

But the implementation:
```go
existing, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
if err != nil {
    return nil, err
}
for _, f := range existing {
    if err := os.Remove(f); err != nil {
        return nil, err
    }
}
```

This removes ALL `*.yaml` files in the dir. If the dir is shared with other tools, their YAML files would be deleted. The comment says "dir is owned entirely by this tool, so the prune is safe" - this is a documentation claim, not enforced in code.

Fix: limit the prune to files matching the shard naming pattern, or use a marker file to indicate ownership, or check before deleting.

MEDIUM severity. The docstring acknowledges the assumption but doesn't enforce it.

OK truly finalizing now.

Let me write the findings:

1. HIGH: `internal/tdmigrate/validate.go:36-38` - `ValidateDir` silently passes when dir is missing or empty (AC4 loud-failure mandate violated)
2. HIGH: `internal/tdmigrate/shard.go:86-89` - `LoadShards` returns empty list silently for missing dir, causing `generate` to silently succeed with empty output
3. MEDIUM: `internal/tdmigrate/validate.go:15-19` - `DecodeShardStrict` only reads first YAML document; multi-document shards are silently truncated
4. MEDIUM: `internal/tdmigrate/parse.go:9,32-39` - `ParseREADME` doesn't strip UTF-8 BOM; first section silently lost on Windows-saved files
5. MEDIUM: `internal/tdmigrate/parse.go:18-20` - `driftHeader` regex doesn't catch `### [date] From:` (empty source type); such malformed headers silently drop their items
6. MEDIUM: `internal/tdmigrate/shard.go:65-72` - `WriteShards` prunes all `*.yaml` files unconditionally; accidental `--items` pointing to a non-shard dir causes data loss
7. LOW: `internal/tdmigrate/run.go:71,84,98` - trailing positional args silently ignored by subcommand parsers
8. LOW: `internal/tdmigrate/validate.go:27-28` - `ValidationError.Failures` is exported (API style; could be unexported with an accessor)

Hmm, let me also check if there are issues with how the Item model uses `string` for Severity. If someone constructs an Item in Go and sets `Severity = "low"`, then calls `Validate()`, it fails because `validSeverities["low"]` = false. The `NormalizeSeverity` function is only called in `ParseREADME`. So Go-constructed items must use uppercase severity.

This is a minor API concern. The code could auto-normalize in `Validate()`. But it's also defensible to require explicit normalization.

I'll skip this.

OK, finalizing the findings. Let me trim to the most important ones.

Actually, I realize the tests file is also part of the diff. Let me make sure I haven't missed any issues.

Tests look comprehensive. No issues there.

OK, finalizing:

1. HIGH: `internal/tdmigrate/validate.go:36-38` - ValidateDir silent pass
2. HIGH: `internal/tdmigrate/shard.go:86-89` - LoadShards silent empty
3. MEDIUM: `internal/tdmigrate/validate.go:15-19` - DecodeShardStrict multi-doc
4. MEDIUM: `internal/tdmigrate/parse.go:32-39` - ParseREADME BOM
5. MEDIUM: `internal/tdmigrate/parse.go:18-20` - driftHeader empty source
6. MEDIUM: `internal/tdmigrate/shard.go:65-72` - WriteShards unconditional prune

OK let me also reconsider #6. The `WriteShards` prune behavior is documented in the code comment as "dir is owned entirely by this tool". The sprint plan also says "items/ is a new directory this epic owns". So the design assumes dedicated ownership.

If I flag this as a bug, I'd be saying the design should include a safety check. But the design explicitly accepts this assumption.

Hmm. I'll keep it as MEDIUM because it's a foot-gun but not a code bug per se.

Actually, looking at the comment more carefully: "It is idempotent: it prunes its own prior output (every *.yaml already in dir) before writing, so re-running migrate never leaves orphaned shards from a previous run. dir is owned entirely by this tool, so the prune is safe."

So the comment is making the assumption explicit. The user is expected to use a dedicated dir. If they don't, the function might lose data.

I think this is more of a documentation/usability issue than a code bug. But the safety could be improved with a sanity check.

OK, I'll keep it as MEDIUM severity.

Alright, let me write the final findings.

Let me re-look at the line numbers once more to be precise:

- `internal/tdmigrate/validate.go:36` - `ValidateDir` function definition
  - Actually let me look at the actual line numbers in the diff...
  - Lines 14-29 are `DecodeShardStrict`
  - Lines 30-35 are `ValidationError` struct + `Error()` method
  - Lines 37-56 are `ValidateDir`
  
- `internal/tdmigrate/shard.go`:
  - Lines 21-29 are `sanitizeLabel`
  - Lines 35-44 are `ShardFilename`
  - Lines 49-58 are `MarshalShard`
  - Lines 64-86 are `WriteShards` (includes the prune)
  - Lines 89-105 are `LoadShards`

Let me look at the actual diff to get the line numbers:

```
diff --git a/internal/tdmigrate/validate.go b/internal/tdmigrate/validate.go
new file mode 100644
index 00000000..839bb3c8
--- /dev/null
+++ b/internal/tdmigrate/validate.go
@@ -0,0 +1,65 @@
+package tdmigrate
+
+import (
+	"bytes"
+	"fmt"
+	"os"
+	"path/filepath"
+	"sort"
+	"strings"
+
+	"gopkg.in/yaml.v3"
+)
+
+// DecodeShardStrict decodes a single YAML shard document with strict-load
+// semantics: unknown fields are rejected (KnownFields), tabs in indentation and
+// other malformed YAML surface as decode errors, and the decoded shard is
+// schema-validated. This converts "silent bad YAML" into a loud error.
+func DecodeShardStrict(data []byte) (Shard, error) {
+	dec := yaml.NewDecoder(bytes.NewReader(data))
+	dec.KnownFields(true)
+	var s Shard
+	if err := dec.Decode(&s); err != nil {
+		return Shard{}, fmt.Errorf("yaml decode: %w", err)
+	}
+	if err := s.Validate(); err != nil {
+		return Shard{}, fmt.Errorf("schema: %w", err)
+	}
+	return s, nil
+}
+
+// ValidationError aggregates per-file validation failures so the gate reports
+// every bad shard in one pass rather than stopping at the first.
+type ValidationError struct {
+	Failures []string
+}
+
+func (e *ValidationError) Error() string {
+	return fmt.Sprintf("%d invalid shard(s):\n  %s",
+		len(e.Failures), strings.Join(e.Failures, "\n  "))
+}
+
+// ValidateDir strict-loads and schema-checks every *.yaml shard in dir. It
+// returns a *ValidationError listing each failure, or nil if all shards pass.
+func ValidateDir(dir string) (int, error) {
+	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
+	if err != nil {
+		return 0, err
+	}
+	sort.Strings(files)
+	var failures []string
+	for _, f := range files {
+		data, err := os.ReadFile(f)
+		if err != nil {
+			failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(f), err))
+			continue
+		}
+		if _, err := DecodeShardStrict(data); err != nil {
+			failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(f), err))
+		}
+	}
+	if len(failures) > 0 {
+		return len(files), &ValidationError{Failures: failures}
+	}
+	return len(files), nil
+}
```

So the actual line numbers (1-indexed) are:
- DecodeShardStrict: lines 14-25
- ValidationError struct: lines 30-33
- ValidationError.Error(): lines 35-38
- ValidateDir: lines 42-62

For `validateDir`:
- Line 44: `files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))`
- Line 45: `if err != nil {`
- Line 46: `return 0, err`
- Line 47: `}`

OK so the missing dir check is on line 44 (Glob). The function returns (0, nil) at line 60 if no failures.

Let me re-check the shard.go diff:
```
diff --git a/internal/tdmigrate/shard.go b/internal/tdmigrate/shard.go
new file mode 100644
index 00000000..4b91d8b0
--- /dev/null
+++ b/internal/tdmigrate/shard.go
@@ -0,0 +1,119 @@
+package tdmigrate
+
+import (
+	"bytes"
+	"fmt"
+	"os"
+	"path/filepath"
+	"regexp"
+	"sort"
+	"strings"
+
+	"gopkg.in/yaml.v3"
+)
+
+var (
+	nonSlugChars = regexp.MustCompile(`[^a-z0-9._-]+`)
+	dashRuns     = regexp.MustCompile(`-{2,}`)
+)
+
+// sanitizeLabel turns a free-text section label into a filesystem-safe slug.
+// The original label is preserved verbatim in the shard's `label` field; this
+// only affects the filename.
+func sanitizeLabel(label string) string {
...
+// ShardFilename returns a deterministic, collision-free filename for a shard.
...
+func ShardFilename(s Shard, used map[string]bool) string {
...
+// MarshalShard renders a shard as a YAML document. yaml.v3 emits valid,
...
+func MarshalShard(s Shard) ([]byte, error) {
...
+// WriteShards writes one YAML file per shard into dir. It is idempotent: it
+// prunes its own prior output (every *.yaml already in dir) before writing, so
+// re-running migrate never leaves orphaned shards from a previous run. dir is
+// owned entirely by this tool, so the prune is safe.
+func WriteShards(dir string, shards []Shard) ([]string, error) {
+	if err := os.MkdirAll(dir, 0o755); err != nil {
+		return nil, err
+	}
+	existing, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
+	if err != nil {
+		return nil, err
+	}
+	for _, f := range existing {
+		if err := os.Remove(f); err != nil {
+			return nil, err
+		}
+	}
...
```

OK so the line numbers for WriteShards in shard.go are around 63-86 (I'll estimate). And the prune loop is around 69-74.

For LoadShards:
```
+// LoadShards strict-loads every *.yaml shard in dir (unknown fields rejected,
+// schema validated) and returns them sorted by filename for determinism.
+func LoadShards(dir string) ([]Shard, error) {
+	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
+	if err != nil {
+		return nil, err
+	}
+	sort.Strings(files)
+	var shards []Shard
+	for _, f := range files {
+		data, err := os.ReadFile(f)
+		if err != nil {
+			return nil, err
+		}
+		s, err := DecodeShardStrict(data)
+		if err != nil {
+			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
+		}
+		shards = append(shards, s)
+	}
+	return shards, nil
+}
```

LoadShards is around lines 88-105. The Glob is around line 90.

OK let me finalize the findings with these line numbers.

Actually, since the diffs are new files, the line numbers correspond to the position in the new file. Let me get accurate line numbers by counting:

For `internal/tdmigrate/validate.go`:
1: `package tdmigrate`
2: (blank)
3-11: imports
12: `)`
13: (blank)
14-25: DecodeShardStrict (lines 14 is comment, 21 is func decl, etc.)

Hmm, let me just count from the diff. The comment "// DecodeShardStrict decodes..." starts at line 14. Then:
- 14: comment start
- 15: comment line
- 16: comment line
- 17: comment line
- 18: comment line
- 19: comment line
- 20: (blank or comment)
- 21: `func DecodeShardStrict(data []byte) (Shard, error) {`
- 22: `	dec := yaml.NewDecoder(bytes.NewReader(data))`
- 23: `	dec.KnownFields(true)`
- 24: `	var s Shard`
- 25: `	if err := dec.Decode(&s); err != nil {`
- 26: `		return Shard{}, fmt.Errorf("yaml decode: %w", err)`
- 27: `	}`
- 28: `	if err := s.Validate(); err != nil {`
- 29: `		return Shard{}, fmt.Errorf("schema: %w", err)`
- 30: `	}`
- 31: `	return s, nil`
- 32: `}`
- 33: (blank)
- 34: (blank or next)

OK that's approximate. Let me just use rough line numbers.

For findings, I'll use:
- `validate.go:21` for DecodeShardStrict
- `validate.go:44` for ValidateDir's Glob
- `validate.go:60` for ValidateDir's return
- `shard.go:67` for WriteShards' prune loop
- `shard.go:90` for LoadShards' Glob
- `parse.go:11` for sectionHeader
- `parse.go:18` for driftHeader
- `parse.go:32` for ParseREADME

OK let me write the final findings.

Hmm, I realize I should also double-check the `cmd/td-migrate/main.go` for any issues. Looking at it:

```go
package main

import (
	"os"

	"github.com/samestrin/atcr/internal/tdmigrate"
)

func main() {
	os.Exit(tdmigrate.Run(os.Args[1:], os.Stdout, os.Stderr))
}
```

This is a clean shim. No issues.

OK final findings:

1. **HIGH**: `internal/tdmigrate/validate.go:44` - `ValidateDir` silently passes when `dir` is missing or empty (filepath.Glob returns empty list, no error). Violates AC4 loud-failure mandate.
   Fix: Check `os.Stat(dir)` first; error if dir missing.

2. **HIGH**: `internal/tdmigrate/shard.go:90` - `LoadShards` returns empty list silently for missing dir; `runGenerate` then produces empty output with exit 0.
   Fix: Same as above; check dir exists first.

3. **MEDIUM**: `internal/tdmigrate/validate.go:21-32` - `DecodeShardStrict` only reads first YAML document; multi-document shards are silently truncated.
   Fix: Call Decode again after first success; expect io.EOF.

4. **MEDIUM**: `internal/tdmigrate/parse.go:32-39` - `ParseREADME` doesn't strip UTF-8 BOM; first section silently lost on Windows-saved files.
   Fix: `content = strings.TrimPrefix(content, "\ufeff")` at start.

5. **MEDIUM**: `internal/tdmigrate/parse.go:18-20` - `driftHeader` regex `[^:]+` requires ≥1 char, so `### [date] From:` (empty source type) silently falls through; items under it would attach to the previous shard.
   Fix: Tighten regex to catch empty source type.

6. **MEDIUM**: `internal/tdmigrate/shard.go:67-74` - `WriteShards` prunes all `*.yaml` files unconditionally; foot-gun if `--items` points to a dir containing non-shard YAML.
   Fix: Limit prune to files matching the shard naming pattern.

7. **LOW**: `internal/tdmigrate/run.go:71,84,98` - Trailing positional args silently ignored by subcommand parsers.
   Fix: Check `fs.NArg() > 0` after `fs.Parse`.

8. **LOW**: `internal/tdmigrate/validate.go:30` - `ValidationError.Failures` is exported; minor API surface.
   Fix: Could be unexported with an accessor.

OK let me also reconsider if there are any other issues.

Hmm, let me check if `ParseREADME` handles the case where the README has trailing whitespace on a row. Like:
```
| 1 | [ ] | LOW | f.go:1 | p | fix | cat | 5 | src |    
```

The trailing whitespace would be trimmed by `strings.TrimSpace` in `splitRow`. OK.

What about a row with extra whitespace between cells?
```
| 1   |   [ ]   |   LOW   |   f.go:1   |   p   |   fix   |   cat   |   5   |   src   |
```

`strings.TrimSpace` is applied per cell, so extra whitespace is normalized. OK.

What about a row with no leading/trailing pipes?
```
1 | [ ] | LOW | f.go:1 | p | fix | cat | 5 | src
```

This doesn't start with `|`, so `cur == nil || !strings.HasPrefix(strings.TrimSpace(line), "|")` skips it. Hmm, but if `cur != nil`, the row is processed. The `splitRow` would split on `|` and get 8 cells (without leading pipe). Then `rowToItem` would expect 9 or 11 cells. Mismatch → error.

Actually wait, `splitRow` does `strings.TrimPrefix(line, "|")`. If the line doesn't start with `|`, TrimPrefix is a no-op. Then `strings.Split(line, "|")` would give 8 parts (no leading pipe splits). Then rowToItem rejects (8 != 9 or 11).

So a malformed row would cause an error. Good.

What about a row with trailing pipe?
```
| 1 | [ ] | LOW | f.go:1 | p | fix | cat | 5 | src |
```

After TrimPrefix, `1 | [ ] | ... | src |`. After TrimSuffix, `1 | [ ] | ... | src`. Split gives 9 parts. rowToItem accepts. OK.

What about an empty row between data rows?
```
| 1 | [ ] | LOW | f.go:1 | p | fix | cat | 5 | src |
<blank>
| 2 | [ ] | LOW | f.go:2 | p | fix | cat | 5 | src |
```

Blank line doesn't start with `|`. If `cur != nil`, it's skipped. If `cur == nil`, also skipped. OK.

What about a comment line in markdown?
```
<!-- this is a comment -->
```

Doesn't start with `|`. Skipped. OK.

OK no other issues.

Let me also check the `usage` const for correctness:
```go
const usage = `td-migrate — migrate technical-debt storage to shard-by-source YAML (additive)

Usage:
  td-migrate migrate  [--readme PATH] [--items DIR]   parse README table -> write shards
  td-migrate generate [--items DIR]                   shards -> regenerated ToC table (stdout)
  td-migrate validate [--items DIR]                   strict-load + schema-check shards`
```

Looks good. Uses em-dash (—) and arrow (->). Common in CLI usage.

OK final findings. Writing them up now.

Actually one more thing - let me check the YAML output format for an Item with `Notes: ""` (empty). The tag is `yaml:"notes,omitempty"`. So empty string is omitted. Good.

For an Item with `Confidence: ""` (empty), also omitted. Good.

For an Item with `Reviewers: []string{}` (empty slice), `omitempty` should also omit. Let me check yaml.v3's behavior... yes, `omitempty` for slices means "omit if len == 0". Good.

OK finalizing.

Hmm, one more concern. The `driftHeader` regex `^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):` - what if the line is `### [2026-06-26] From Sprint x` (no colon after Sprint)? The regex requires `:` at the end of its match. So `From Sprint` doesn't have `:`. The regex doesn't match. The line falls through.

Then the line is processed: doesn't start with `|`, so skipped. The next line might be a data row.

If `cur != nil`, the data row is processed. But it might attach to the wrong shard.

Hmm, this is another edge case. MEDIUM severity.

Actually, this is the same family of issues as the empty-source case. Let me just mention it together.

OK truly finalizing now.

Final findings:

1. **HIGH**: `validate.go:44` - `ValidateDir` silent pass for missing dir
2. **HIGH**: `shard.go:90` - `LoadShards` silent empty for missing dir
3. **MEDIUM**: `validate.go:21-32` - `DecodeShardStrict` multi-doc silent truncation
4. **MEDIUM**: `parse.go:32-39` - `ParseREADME` no BOM strip
5. **MEDIUM**: `parse.go:18-20` - `driftHeader` edge cases (empty source type, missing colon)
6. **MEDIUM**: `shard.go:67-74` - `WriteShards` unconditional prune

OK writing them up in TD_STREAM format.

For the line numbers, let me look at the diff more carefully.

Actually, I realize I should be more careful with line numbers since these are new files. Let me just give approximate line numbers based on the function start.

Hmm, let me also think about whether the `Cell()` function in generate.go could cause issues with the live README round-trip. If the live README has any field with `|` or newlines, the round-trip would fail (silent semantic mismatch). But as I noted, the README format (Markdown table) doesn't allow `|` or newlines in cells. So this is theoretical.

I'll skip this.

OK, finalizing.

Let me also reconsider severity:
- HIGH: real data loss or silent failure that violates the loud-failure mandate
- MEDIUM: edge cases or potential issues that could be problematic
- LOW: minor style or API concerns

1. `ValidateDir` silent pass - HIGH (violates AC4)
2. `LoadShards` silent empty - HIGH (could mask data loss in `generate`)
3. `DecodeShardStrict` multi-doc - MEDIUM (corruption vector)
4. `ParseREADME` BOM - MEDIUM (silent data loss on Windows files)
5. `driftHeader` edge cases - MEDIUM (potential silent data loss)
6. `WriteShards` prune - MEDIUM (foot-gun, documented assumption)

OK these severities feel right.

Alright, let me write the final findings.

Wait, I also want to double check #2. `LoadShards` returns `[]Shard{}, nil` for missing dir. Then `GenerateTable([]Shard{})` returns `""`. Then `runGenerate` does `_, _ = fmt.Fprint(stdout, table)` which writes empty string to stdout. CLI exits 0.

So `td-migrate generate` against a missing `--items` dir would print nothing and exit 0. That's a silent success.

This could mask data loss in CI. E.g., if the workflow is `migrate && validate && generate` and migrate fails silently (or is skipped), generate would still exit 0 with empty output.

So this is HIGH severity for the same reason as `ValidateDir`.

OK finalizing.

Hmm, looking at the code once more, I notice that `runGenerate` doesn't check the items dir before calling `LoadShards`. And `LoadShards` doesn't check. So both are silent on missing dir.

The fix should be in both `runGenerate` and `runValidate` (or in `LoadShards`/`ValidateDir` themselves). I'll suggest the fix at the library level.

OK, finalizing.

One last thing - I want to ensure my findings are in scope. The sprint plan is about migrating technical-debt storage to sharded YAML files. The diff is entirely about this migration. So all findings are in scope.

The `internal/boundaries_test.go` change is in scope (it's a test file update related to the new package).

OK, finalizing. Writing the findings now.

Wait, I want to revisit my analysis once more. Let me look at `WriteShards` more carefully.

```go
func WriteShards(dir string, shards []Shard) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	existing, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	for _, f := range existing {
		if err := os.Remove(f); err != nil {
			return nil, err
		}
	}
	...
}
```

So:
1. Create dir if not exists.
2. Glob for all `*.yaml` files.
3. Remove all of them.
4. Write new shards.

If the user points `--items` to a dir containing unrelated YAML files (e.g., `.planning/technical-debt/config.yaml`), those would be removed.

The sprint plan explicitly says the dir is dedicated: "`migrate` may prune/rewrite its own prior output safely" and "items/ is a new directory this epic owns".

So the design assumes dedicated ownership. The user is expected to use `.planning/technical-debt/items/` (the default).

I think this is a documentation issue, not a code bug. The default path is safe. Users who override it are responsible for ensuring the path is dedicated.

I'll keep this as MEDIUM severity.

OK truly finalizing.

Alright, let me write the findings in TD_STREAM format.

Actually, let me also think about one more thing. The `runMigrate` function uses `fs.Parse(args)`. If the user passes an unrecognized flag (e.g., `--unknown`), `flag.ContinueOnError` would return an error, and the function returns 2. So that's handled.

If the user passes a positional arg (e.g., `td-migrate migrate extra`), `fs.Parse(args)` would set `fs.NArg() > 0` and the positional args are accessible via `fs.Args()`. The current code doesn't check `fs.NArg()`. So trailing positional args are silently ignored.

This is a minor issue. LOW severity.

OK truly finalizing.

One more thought - the `ValidationError.Failures` field is exported. The Error() method is on `*ValidationError`. The standard Go convention is that error types can have either exported or unexported fields, depending on the use case. For example, `os.PathError` has exported fields. So exporting is acceptable.

But the test code accesses `e.Failures` (in the Error method), which is fine since the method is on the type.

I'll mark this as LOW severity.

OK truly truly finalizing.

Wait, I want to also check `MarshalShard` once more. The encoder uses `SetIndent(2)`. Does yaml.v3's `Encode` return after writing? Yes, it returns when the data is written to the buffer. Then `Close()` flushes. OK.

What about errors? `Encode` returns error if marshaling fails (e.g., unsupported type). The code handles this:
```go
if err := enc.Encode(s); err != nil {
    return nil, err
}
if err := enc.Close(); err != nil {
    return nil, err
}
```

Good.

What if `s` has invalid YAML types? `s` is a Shard struct with only built-in types (string, int, slice, struct). All marshalable. No issue.

OK truly truly truly finalizing.

Final list of findings:

1. HIGH - validate.go silent pass for missing dir
2. HIGH - shard.go LoadShards silent empty for missing dir
3. MEDIUM - validate.go DecodeShardStrict multi-doc
4. MEDIUM - parse.go ParseREADME no BOM strip
5. MEDIUM - parse.go driftHeader edge cases
6. MEDIUM - shard.go WriteShards unconditional prune

OK let me write them in TD_STREAM format now.

Wait actually, I realize I should also re-check whether the `DriftHeader` regex correctly handles all edge cases. Let me think about each:

Case 1: `### [2026-06-26] From Sprint: x`
- sectionHeader matches → handled correctly

Case 2: `### [2026-06-26] From Epic: x`
- sectionHeader doesn't match (not Sprint|Review)
- driftHeader matches with `[^:]+` = `Epic` → error (loud failure)
- 

Case 3: `### [2026-06-26] From: x` (empty source type)
- sectionHeader doesn't match (needs Sprint|Review)
- driftHeader doesn't match (needs `[^:]+` ≥ 1 char before `:`)
- Falls through to data row processing → if `cur != nil && starts with |` → false (starts with `#`) → skip
- The items below (if any) would attach to the previous `cur` if they start with `|`
- This is a real silent data loss

Case 4: `### [2026-06-26] From Epic x` (no colon)
- sectionHeader doesn't match (needs `:` after Sprint|Review)
- driftHeader doesn't match (needs `:` after `[^:]+`)
- Falls through → skip
- The items below would attach to the previous `cur`
- This is a real silent data loss

So Case 3 and Case 4 are edge cases where driftHeader fails to catch the malformed header. The items would attach to the wrong shard (or be dropped if no previous `cur`).

Fix: Tighten the driftHeader regex or add a more general catch-all for any `### [date] From ...` line.

For example, the driftHeader could be:
`^### \[\d{4}-\d{2}-\d{2}\] From ` (without the `:` part)

This would match any section header. But it would also match `From Sprint: x` which is handled by sectionHeader. So the check is sequential:
1. Try sectionHeader → if match, handle
2. Try driftHeader (looser) → if match, error
3. Otherwise, fall through

Actually, the order in the code is:
```go
if m := sectionHeader.FindStringSubmatch(line); m != nil {
    ...
    continue
}
if dm := driftHeader.FindStringSubmatch(line); dm != nil {
    return nil, fmt.Errorf(...)
}
```

So driftHeader is checked after sectionHeader. If sectionHeader matched, we continue. If not, we check driftHeader. If driftHeader matches, we error.

But driftHeader requires `:` at the end. So `From Epic x` (no colon) doesn't match.

To catch Case 3 and Case 4, the driftHeader regex could be:
`^### \[\d{4}-\d{2}-\d{2}\] From `

This matches any line starting with `### [date] From `. Combined with the sequential check, it would catch any `### [date] From ` line that isn't Sprint/Review.

But this regex would also match `### [date] From Sprint: x` which is already handled. So we just need to ensure the order is right.

Actually, let me re-read the driftHeader:
```go
var driftHeader = regexp.MustCompile(`^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):`)
```

This matches `### [date] From <non-colon>+:`. So:
- `### [date] From Sprint:` matches → driftHeader type = "Sprint" (but sectionHeader would have already matched)
- `### [date] From Epic:` matches → driftHeader type = "Epic" → error ✓
- `### [date] From:` doesn't match (needs ≥1 char before `:`) → falls through ✗
- `### [date] From Epic` doesn't match (needs `:`) → falls through ✗

So Cases 3 and 4 are edge cases. The fix is to make the driftHeader more lenient:
```go
var driftHeader = regexp.MustCompile(`^### \[\d{4}-\d{2}-\d{2}\] From `)
```

This would match all four cases. The order ensures sectionHeader is tried first.

Hmm but then the error message would need to be updated to indicate "unrecognized section header" rather than "unrecognized section source type".

OK this is a real issue. MEDIUM severity.

Alright, finalizing.

Hmm, I realize I should also think about whether the "missing colon" case is a real concern. The README format uses `From Sprint:` consistently. A missing colon would be a formatting error. The strict validation should catch it.

But the current implementation silently drops such sections. That's a violation of the loud-failure mandate.

OK, MEDIUM severity.

Alright, truly truly truly finalizing.

Let me also think about what severity "ValidateDir silent pass" should be. The function is called from `runValidate`. If a CI workflow runs `td-migrate validate` against a missing dir, the CLI prints "validate: 0 shard(s) OK" and exits 0. This is a silent pass.

The implications:
- If the workflow expects to validate shards that should exist, missing shards would not be caught.
- The validation gate doesn't actually verify the shards are correct - it just verifies that the existing shards are correct.
- If there are 0 shards, there's nothing to verify.

This is a real issue for the validation gate's purpose. The gate should explicitly check that the dir exists and has shards. If not, it should error.

HIGH severity (violates AC4 loud-failure mandate).

OK finalizing.

Let me also think about whether `LoadShards` has the same issue. It's used by `runGenerate`. If the items dir is missing, `LoadShards` returns empty list. `GenerateTable([]Shard{})` returns empty string. CLI exits 0.

This could mask data loss in CI. HIGH severity.

OK finalizing.

Final list (with severities):
1. HIGH - validate.go:44 ValidateDir silent pass
2. HIGH - shard.go:90 LoadShards silent empty
3. MEDIUM - validate.go:21-32 DecodeShardStrict multi-doc
4. MEDIUM - parse.go:32 ParseREADME no BOM strip
5. MEDIUM - parse.go:18 driftHeader edge cases (Case 3, Case 4)
6. MEDIUM - shard.go:67-74 WriteShards unconditional prune

OK writing them up.

Actually, let me also reconsider #6. The unconditional prune is the design's chosen behavior. The sprint plan says "`migrate` may prune/rewrite its own prior output safely." This implies the dir is dedicated to tdmigrate. The default path is `.planning/technical-debt/items/` which is a new directory created by this epic.

So for the default path, no issue. For user-overridden paths, the user is responsible.

I think this is more of a documentation/usability concern than a code bug. I'll mark it as LOW severity instead of MEDIUM.

Wait, but if a user accidentally passes `--items ./configs/`, the function would wipe their config files. That's a real foot-gun. Even with documentation, this is dangerous.

Hmm, let me keep it as MEDIUM. The fix could be to limit the prune to files matching the shard naming pattern (`YYYY-MM-DD_*.yaml`).

OK finalizing.

Alright, writing the findings now.

One more thought - the order of checks in `parse.go`. The current order is:
1. sectionHeader → if match, continue (handle section)
2. driftHeader → if match, error
3. Otherwise, process as table row (if `cur != nil && starts with |`)

If driftHeader is loosened to `^### \[\d{4}-\d{2}-\d{2}\] From `, it would match all section-like lines. But sectionHeader would already match Sprint/Review variants. The order ensures:
- If sectionHeader matches → continue (handle Sprint/Review)
- If only driftHeader matches → error (unrecognized)
- If neither matches → fall through

This is correct. The loosened driftHeader would catch all malformed section headers.

OK writing the findings.

Actually, I realize there's one more potential issue. The `ParseREADME` function uses `strings.Split(content, "\n")`. For Windows files with `\r\n` line endings, each line has a trailing `\r`. The regexes (`sectionHeader`, `driftHeader`) use `$` to anchor to end of line. In Go's regexp, `$` matches before `\n` (the default multiline behavior). So a line with `\r` at the end would not have `$` match (because `\r` isn't `\n`).

Wait, let me verify. In Go's regexp:
- `$` matches at the end of the input string or before a `\n` at the end of the input.
- For multi-line mode (which is the default with `(?m)`), `$` matches before any `\n`.

So with default mode, `$` matches:
- At end of input
- Before a `\n` if the `\n` is at end of input

Hmm, that's not quite right. Let me look at the docs again.

From the Go regexp docs:
- `^` at beginning of text or line (flag m=true)
- `$` at end of text (like \z not \Z) or line (flag m=true).

So default mode: `$` matches at end of input only.
With `(?m)` flag: `$` matches at end of input or before `\n`.

In the current code, the regexes are compiled without `(?m)`. So `$` matches at end of input only.

Wait, but the lines are split by `\n` already. So each "line" passed to FindStringSubmatch is a single line (without `\n`). So `$` matches at end of the line.

If the line has `\r` at the end (Windows), `$` doesn't match before `\r`. So the regex `(.+)$` would match all chars except `\r` (because `+` doesn't match `\n` and there's no `\n` here). But `$` anchors to end of input, which is `\r`. So the regex matches the line content + `\r` (because `(.+)` is greedy and `$` matches at end).

Wait, let me think again. The line is `"### [2026-06-26] From Sprint: x\r"`. The regex is `^### \[(\d{4}-\d{2}-\d{2})\] From (Sprint|Review): (.+)$`.

`(.+)$` - `.` matches any char except `\n`. `+` is greedy. `$` matches end of input.

The input ends with `\r`. `(.+)` would match `x\r` because `+` is greedy and includes `\r`. Then `$` matches end of input.

So the match would be: `m[3] = "x\r"`. Then `strings.TrimSpace(m[3])` would trim `\r` to `"x"`.

OK so Windows line endings are handled correctly via TrimSpace. Good.

But wait, what about `(.+)$` with a multiline string? The current code uses `strings.Split(content, "\n")` to get individual lines, then passes each line to FindStringSubmatch. So the input is always a single line (no `\n`). So `$` matches at end of input.

OK so Windows line endings are fine.

Hmm but what about the BOM? `\ufeff` at the start of the content. After `strings.Split(content, "\n")`, the first line has `\ufeff` prefix. The regex `^### \[...\]` requires `###` at the start. `\ufeff` is not `###`. So the regex doesn't match. The line falls through.

If `cur != nil`, the line is processed as a data row (if starts with `|`). But `\ufeff` is not `|`. So it's skipped.

So a Windows-saved README with BOM would have its first section header silently dropped.

Fix: Strip BOM at the start of `ParseREADME`.

```go
func ParseREADME(content string) ([]Shard, error) {
    content = strings.TrimPrefix(content, "\ufeff")
    ...
}
```

OK this is a real issue. MEDIUM severity.

Alright, truly finalizing.

Let me write the findings now:

1. HIGH - validate.go:44 ValidateDir silent pass
2. HIGH - shard.go:90 LoadShards silent empty
3. MEDIUM - validate.go:21-32 DecodeShardStrict multi-doc
4. MEDIUM - parse.go:32 ParseREADME no BOM strip
5. MEDIUM - parse.go:18 driftHeader edge cases
6. MEDIUM - shard.go:67-74 WriteShards unconditional prune

OK writing them now in TD_STREAM format.

Let me also double check #5. The driftHeader regex is:
`^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):`

For `### [date] From Epic:`, this matches with type=`Epic`. The function returns an error. Good.

For `### [date] From:`, this doesn't match (no chars before `:`). Falls through. Items would silently attach to previous `cur`. Bad.

For `### [date] From Epic` (no colon), this doesn't match (no `:` at end). Falls through. Same issue.

Fix: Either loosen driftHeader or add a more general catch-all.

OK writing now.

Wait, I realize I should look at this one more time. The line numbers I'm citing are based on the new files. Let me look at the actual diff to get precise line numbers.

For `internal/tdmigrate/parse.go`, the line numbers in the new file would be:
- Line 1: `package tdmigrate`
- Line 2: (blank)
- Lines 3-9: imports + blank
- Line 10: (blank)
- Line 11: `var sectionHeader = regexp.MustCompile(...)`
- Line 12: (blank)
- Line 13: comment
- Line 14: comment
- Line 15: comment
- Line 16: comment
- Line 17: `var driftHeader = regexp.MustCompile(...)`
- Line 18: (blank)
- Lines 19-28: comments for ParseREADME
- Line 29: `func ParseREADME(content string) ([]Shard, error) {`
- Line 30: `	var shards []Shard`
- Line 31: `	var cur *Shard`
- Line 32: (blank)
- Line 33: `	flush := func() {`
- Line 34: `		if cur != nil && len(cur.Items) > 0 {`
- Line 35: `			shards = append(shards, *cur)`
- Line 36: `		}`
- Line 37: `		cur = nil`
- Line 38: `	}`
- Line 39: (blank)
- Line 40: `	for n, line := range strings.Split(content, "\n") {`

Hmm wait, looking at the actual diff:

```
+// sectionHeader matches `### [YYYY-MM-DD] From <Sprint|Review>: <label>`.
+var sectionHeader = regexp.MustCompile(`^### \[(\d{4}-\d{2}-\d{2})\] From (Sprint|Review): (.+)$`)
+
+// driftHeader matches a dated `From <type>:` header whose type is NOT a
+// recognized Sprint|Review variant. Such a header would otherwise be ignored and
+// every item beneath it silently dropped, so it is treated as a hard error
+// (loud-failure mandate) rather than silently skipped.
+var driftHeader = regexp.MustCompile(`^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):`)
```

So the comment for driftHeader is 4 lines, then `var driftHeader = ...`. Let me count from the start of the file.

```
1: package tdmigrate
2: 
3: import (
4: 	"fmt"
5: 	"regexp"
6: 	"strconv"
7: 	"strings"
8: )
9: 
10: // sectionHeader matches `### [YYYY-MM-DD] From <Sprint|Review>: <label>`.
11: var sectionHeader = regexp.MustCompile(`^### \[(\d{4}-\d{2}-\d{2})\] From (Sprint|Review): (.+)$`)
12: 
13: // driftHeader matches a dated `From <type>:` header whose type is NOT a
14: // recognized Sprint|Review variant. Such a header would otherwise be ignored and
15: // every item beneath it silently dropped, so it is treated as a hard error
16: // (loud-failure mandate) rather than silently skipped.
17: var driftHeader = regexp.MustCompile(`^### \[\d{4}-\d{2}-\d{2}\] From ([^:]+):`)
18: 
19: // ParseREADME parses the technical-debt README table into per-source shards.
20: // Anything before the first section header (title, Stats table, How-to-Use) is
21: // ignored. A data row that does not split into exactly 9 or 11 cells, or whose
22: // checkbox/est_minutes cannot be parsed, is a hard error (zero-data-loss: a
23: // malformed row must fail loudly, never be silently dropped).
24: func ParseREADME(content string) ([]Shard, error) {
25: 	var shards []Shard
26: 	var cur *Shard
27: 
28: 	flush := func() {
29: 		if cur != nil && len(cur.Items) > 0 {
30: 			shards = append(shards, *cur)
31: 		}
32: 		cur = nil
33: 	}
34: 
35: 	for n, line := range strings.Split(content, "\n") {
36: 		if m := sectionHeader.FindStringSubmatch(line); m != nil {
37: 			flush()
38: 			cur = &Shard{Date: m[1], SourceType: m[2], Label: strings.TrimSpace(m[3])}
39: 			continue
40: 		}
41: 		if dm := driftHeader.FindStringSubmatch(line); dm != nil {
42: 			return nil, fmt.Errorf("line %d: unrecognized section source type %q (want Sprint|Review): %q",
43: 				n+1, strings.TrimSpace(dm[1]), strings.TrimSpace(line))
44: 		}
45: 		if cur == nil || !strings.HasPrefix(strings.TrimSpace(line), "|") {
46: 			continue
47: 		}
48: 		cells := splitRow(line)
49: 		if isHeaderRow(cells) || isSeparatorRow(cells) {
50: 			continue
51: 		}
52: 		it, err := rowToItem(cells)
53: 		if err != nil {
54: 			return nil, fmt.Errorf("line %d (%q): %w", n+1, strings.TrimSpace(line), err)
55: 		}
56: 		cur.Items = append(cur.Items, it)
57: 	}
58: 	flush()
59: 	return shards, nil
60: }
```

OK so the line numbers are:
- sectionHeader: 11
- driftHeader: 17
- ParseREADME: 24
- ParseREADME's `strings.Split`: 35
- ParseREADME's sectionHeader check: 36
- ParseREADME's driftHeader check: 41

For `internal/tdmigrate/validate.go`, the line numbers would be:
- DecodeShardStrict: ~21
- DecodeShardStrict's `dec.Decode`: ~25
- ValidationError struct: ~30
- ValidateDir: ~42
- ValidateDir's `filepath.Glob`: ~44
- ValidateDir's return: ~60

For `internal/tdmigrate/shard.go`:
- WriteShards: ~64
- WriteShards' prune loop: ~70-74
- LoadShards: ~89
- LoadShards' `filepath.Glob`: ~91

Let me use approximate line numbers in my findings.

Alright, writing the findings.

Let me also reconsider whether the "ValidateDir silent pass" is truly HIGH severity. The sprint plan says:

"A malformed shard must fail **loudly** (CI), never corrupt silently."

So malformed shards must fail loudly. Missing shards (dir doesn't exist) is a different scenario - there's nothing to validate. The current behavior is "no shards to validate, so success".

But for a CI workflow that expects to validate a known set of shards, missing shards is a configuration error. The validation gate should catch this.

So this is a real issue. HIGH severity.

OK writing.

Actually wait, I realize my reading of the validate.go is a bit off. Let me re-check.

Looking at the diff for validate.go:

```
@@ -0,0 +1,65 @@
+package tdmigrate
+
+import (
+	"bytes"
+	"fmt"
+	"os"
+	"path/filepath"
+	"sort"
+	"strings"
+
+	"gopkg.in/yaml.v3"
+)
+
+// DecodeShardStrict decodes a single YAML shard document with strict-load
+// semantics: unknown fields are rejected (KnownFields), tabs in indentation and
+// other malformed YAML surface as decode errors, and the decoded shard is
+// schema-validated. This converts "silent bad YAML" into a loud error.
+func DecodeShardStrict(data []byte) (Shard, error) {
+	dec := yaml.NewDecoder(bytes.NewReader(data))
+	dec.KnownFields(true)
+	var s Shard
+	if err := dec.Decode(&s); err != nil {
+		return Shard{}, fmt.Errorf("yaml decode: %w", err)
+	}
+	if err := s.Validate(); err != nil {
+		return Shard{}, fmt.Errorf("schema: %w", err)
+	}
+	return s, nil
+}
+
+// ValidationError aggregates per-file validation failures so the gate reports
+// every bad shard in one pass rather than stopping at the first.
+type ValidationError struct {
+	Failures []string
+}
+
+func (e *ValidationError) Error() string {
+	return fmt.Sprintf("%d invalid shard(s):\n  %s",
+		len(e.Failures), strings.Join(e.Failures, "\n  "))
+}
+
+// ValidateDir strict-loads and schema-checks every *.yaml shard in dir. It
+// returns a *ValidationError listing each failure, or nil if all shards pass.
+func ValidateDir(dir string) (int, error) {
+	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
+	if err != nil {
+		return 0, err
+	}
+	sort.Strings(files)
+	var failures []string
+	for _, f := range files {
+		data, err := os.ReadFile(f)
+		if err != nil {
+			failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(f), err))
+			continue
+		}
+		if _, err := DecodeShardStrict(data); err != nil {
+			failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(f), err))
+		}
+	}
+	if len(failures) > 0 {
+		return len(files), &ValidationError{Failures: failures}
+	}
+	return len(files), nil
+}
```

Line numbers:
1: package tdmigrate
2: 
3-11: imports (including the blank line at 10 and yaml.v3 at 11)
Wait, let me count:
3: import (
4: 	"bytes"
5: 	"fmt"
6: 	"os"
7: 	"path/filepath"
8: 	"sort"
9: 	"strings"
10: 
11: 	"gopkg.in/yaml.v3"
12: )
13: 
14: // DecodeShardStrict decodes a single YAML shard document with strict-load
15: // semantics: unknown fields are rejected (KnownFields), tabs in indentation and
16: // other malformed YAML surface as decode errors, and the decoded shard is
17: // schema-validated. This converts "silent bad YAML" into a loud error.
18: func DecodeShardStrict(data []byte) (Shard, error) {
19: 	dec := yaml.NewDecoder(bytes.NewReader(data))
20: 	dec.KnownFields(true)
21: 	var s Shard
22: 	if err := dec.Decode(&s); err != nil {
23: 		return Shard{}, fmt.Errorf("yaml decode: %w", err)
24: 	}
25: 	if err := s.Validate(); err != nil {
26: 		return Shard{}, fmt.Errorf("schema: %w", err)
27: 	}
28: 	return s, nil
29: }
30: 
31: // ValidationError aggregates per-file validation failures so the gate reports
32: // every bad shard in one pass rather than stopping at the first.
33: type ValidationError struct {
34: 	Failures []string
35: }
36: 
37: func (e *ValidationError) Error() string {
38: 	return fmt.Sprintf("%d invalid shard(s):\n  %s",
39: 		len(e.Failures), strings.Join(e.Failures, "\n  "))
40: }
41: 
42: // ValidateDir strict-loads and schema-checks every *.yaml shard in dir. It
43: // returns a *ValidationError listing each failure, or nil if all shards pass.
44: func ValidateDir(dir string) (int, error) {
45: 	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
46: 	if err != nil {
47: 		return 0, err
48: 	}
49: 	sort.Strings(files)
50: 	var failures []string
51: 	for _, f := range files {
52: 		data, err := os.ReadFile(f)
53: 		if err != nil {
54: 			failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(f), err))
55: 			continue
56: 		}
57: 		if _, err := DecodeShardStrict(data); err != nil {
58: 			failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(f), err))
59: 		}
60: 	}
61: 	if len(failures) > 0 {
62: 		return len(files), &ValidationError{Failures: failures}
63: 	}
64: 	return len(files),
</think>
