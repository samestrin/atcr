Let me analyze the diff carefully, focusing on the changed regions only, looking for line/expression-level bugs.

Let me go through each changed file:

1. `.planning/epics/active/15.0_documentation_audit.md` - planning doc, unlikely to have code bugs
2. `.planning/technical-debt/README.md` - technical debt tracking, unlikely to have code bugs
3. `CHANGELOG.md` - changelog, unlikely to have code bugs
4. `cmd/atcr/docs_audit_test.go` - NEW test file, 334 lines - this is where bugs would hide
5. `docs/README.md` - new docs index
6. `docs/architecture.md` - new architecture doc
7. `docs/registry.md` - modified doc

Let me focus on `cmd/atcr/docs_audit_test.go` since that's the only code file.

Let me trace through the logic carefully:

### `atcrInvocations` function (line ~71):

```go
func atcrInvocations(md string) [][]string {
	var out [][]string
	capture := func(s string) {
		s = strings.TrimSpace(s)
		s = strings.TrimSpace(strings.TrimPrefix(s, "$ "))
		if !strings.HasPrefix(s, "atcr ") {
			return
		}
		if fields := strings.Fields(s); len(fields) > 1 {
			out = append(out, fields[1:]) // drop the leading "atcr"
		}
	}
	for _, m := range regexp.MustCompile("`([^`\n]+)`").FindAllStringSubmatch(md, -1) {
		capture(m[1])
	}
	inFence := false
	for _, ln := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			capture(ln)
		}
	}
	return out
}
```

Wait, there's a subtle issue with the fence detection. The `trimmed` variable is used for fence detection, but `ln` (the untrimmed line) is passed to `capture`. Inside `capture`, it does `strings.TrimSpace(s)` first, so that's fine.

But wait - the fence detection uses `strings.HasPrefix(trimmed, "```")`. This means a line like "````text" (4 backticks) would also match. That's probably fine since 4-backtick fences are valid in some markdown flavors.

Actually, there's a more subtle issue. The fence detection checks `strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")`. But what about a fenced block that uses a different number of backticks? Like ```````` (7 backticks)? That would still match `HasPrefix(trimmed, "```")` and toggle the fence. But then the closing fence would also need 7 backticks. Since both opening and closing use `HasPrefix`, a 7-backtick fence would toggle on the opening and toggle off on the closing. That's fine.

But what about mixed fences? If a block opens with ``` and closes with ~~~? In standard markdown, these are different fence types and shouldn't close each other. But this code treats them as the same toggle. So:

```
```code
~~~
```

The ``` opens the fence, then ~~~ closes it (incorrectly in standard markdown). But this is a minor edge case and probably not a bug worth flagging since the test is about atcr invocations.

Actually wait, let me re-examine. The issue is that ``` and ~~~ are treated as interchangeable fence delimiters. In GFM, a ``` fence can only be closed by another ``` fence, and a ~~~ fence can only be closed by a ~~~ fence. So if a doc has:

```
```python
code = "~~~"
```
```

The ~~~ inside the code block would toggle the fence off, and then the closing ``` would toggle it back on, causing subsequent lines to be treated as inside a fence. This could cause false positives or false negatives in the atcr invocation detection. But this is a pretty edge-casey issue.

Let me look at more impactful bugs.

### `TestDocsReferenceOnlyRealCommands` (line ~148):

```go
func TestDocsReferenceOnlyRealCommands(t *testing.T) {
	cmds := canonicalCommands()
	groups := commandGroups()
	flags := canonicalFlags()
	reFlag := regexp.MustCompile(`^--([a-z][a-z-]+)`)
	isWord := regexp.MustCompile(`^[a-z][a-z-]+$`).MatchString
	for path, content := range auditedMarkdown(t) {
		for _, tokens := range atcrInvocations(content) {
			name0 := tokens[0]
			switch {
			case reFlag.MatchString(name0):
				// e.g. `atcr --version`: a root flag, validated in the flag loop below.
			case !cmds[name0]:
				t.Errorf("%s references `atcr %s` but %q is not a real command", path, name0, name0)
			default:
				if children, isGroup := groups[name0]; isGroup && len(tokens) > 1 {
					if next := tokens[1]; isWord(next) && !children[next] {
						t.Errorf("%s references `atcr %s %s` but %q is not a subcommand of %q", path, name0, next, next, name0)
					}
				}
			}
			for _, tok := range tokens {
				if m := reFlag.FindStringSubmatch(tok); m != nil && !flags[m[1]] {
					t.Errorf("%s references `--%s` on an `atcr %s` command, but no such flag exists", path, m[1], name0)
				}
			}
		}
	}
}
```

Let me look at the flag validation loop. `reFlag` is `^--([a-z][a-z-]+)`. This matches flags like `--help`, `--version`, etc. But what about flags with values like `--output-dir=foo`? The regex would match `--output-dir` and capture `output-dir`, then check if `output-dir` is a real flag. The `=foo` part would be in the token but the regex only captures the flag name part. Actually wait, `strings.Fields` would split `--output-dir=foo` as a single token `--output-dir=foo`. Then `reFlag.FindStringSubmatch(tok)` would match `--output-dir` and capture `output-dir`. Wait, no - the regex is `^--([a-z][a-z-]+)`. The `=` is not in `[a-z][a-z-]`, so the regex would match `--output-dir` from `--output-dir=foo` and capture `output-dir`. That seems correct.

But what about `--output-dir foo`? `strings.Fields` would split this into `--output-dir` and `foo` as separate tokens. The `--output-dir` token would match and `output-dir` would be checked. That's fine.

What about short flags like `-h` or `-o`? The regex `^--([a-z][a-z-]+)` requires `--` (two dashes), so single-dash flags wouldn't match. But cobra flags are typically `--long-name`, so this should be fine.

What about flags with numbers, like `--level2`? The regex `[a-z][a-z-]+` only matches lowercase letters and hyphens, so `--level2` wouldn't match. But this is probably intentional since CLI flags are typically all lowercase letters and hyphens.

Let me look at the `isWord` regex: `^[a-z][a-z-]+$`. This is used to check if the second token after a group command is a word (and thus should be validated as a subcommand). If the second token is `--flag`, `isWord` would return false, and the subcommand check would be skipped. That seems correct.

But wait - what if the second token is a file path or argument? Like `atcr review somefile.go`? `isWord` would match `somefile.go`? No, because `.` is not in `[a-z-]`. So `somefile.go` wouldn't match `isWord`, and the subcommand check would be skipped. But `somefile` would match `isWord` if the file had no extension. Hmm, but `review` is not a group command (it doesn't have subcommands), so `groups["review"]` wouldn't exist and the `isGroup` check would fail. So this is fine.

Actually, let me reconsider. What if someone writes `atcr benchmark run` in the docs? `benchmark` might be a group command with `run` as a subcommand. `isWord("run")` would be true, and `children["run"]` would be checked. If `run` is a real subcommand, it passes. If not, it fails. That's correct.

### `TestConfigDocsUseRealConfigFilenameAndReconcilerName` (line ~175):

```go
func TestConfigDocsUseRealConfigFilenameAndReconcilerName(t *testing.T) {
	reFilename := regexp.MustCompile(`\batcr\.yaml\b`)
	reReconciler := regexp.MustCompile(`(?i)reconciler v2`)
	for path, content := range auditedMarkdown(t) {
		if reFilename.MatchString(content) {
			t.Errorf("%s references `atcr.yaml`; the real project config is `.atcr/config.yaml`", path)
		}
		if reReconciler.MatchString(content) {
			t.Errorf("%s references \"Reconciler v2\", which is not a real feature name", path)
		}
	}
}
```

Wait, the regex `\batcr\.yaml\b` - the `\b` word boundary before `atcr` would match `.atcr/config.yaml` because... let me think. In `.atcr/config.yaml`, the `atcr` is preceded by `.` which is a word boundary (since `.` is not a word character). So `\batcr\.yaml\b` would match... wait, `atcr.yaml` in `.atcr/config.yaml`? No, `.atcr/config.yaml` doesn't contain `atcr.yaml` as a substring. It contains `atcr/config.yaml`. The regex is looking for `atcr.yaml` (with a dot between atcr and yaml), not `atcr/config.yaml` (with a slash). So this is fine.

But wait, what about the word boundary? `\b` matches between a word char and a non-word char. In `atcr.yaml`, the `\b` before `a` would match if preceded by a non-word char (like space, start of string, etc.). The `\b` after `l` would match if followed by a non-word char. So `atcr.yaml` would match. But `.atcr/config.yaml` would NOT match because it doesn't contain the literal string `atcr.yaml`. Good.

Hmm, but what about `atcr.yaml` appearing inside `.atcr.yaml`? The `\b` before `atcr` in `.atcr.yaml` - the `.` before `a` is a word boundary, so `\batcr\.yaml\b` would match `.atcr.yaml`. But this is a different filename from `.atcr/config.yaml`, so it should be flagged. Actually, `.atcr.yaml` is not the same as `.atcr/config.yaml`, so if someone wrote `.atcr.yaml` in the docs, it would be flagged. That's probably correct behavior.

### `TestReconcilerConfigSurfaceDocumented` (line ~176):

```go
func TestReconcilerConfigSurfaceDocumented(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "registry.md"))
	if err != nil {
		t.Fatalf("read registry.md: %v", err)
	}
	ref := string(b)
	for _, block := range []string{"persona:", "debate:", "verify:", "executor:"} {
		if !strings.Contains(ref, block) {
			t.Errorf("docs/registry.md does not document the `%s` config block", block)
		}
	}
}
```

This uses `strings.Contains` to check for `persona:`, `debate:`, `verify:`, `executor:`. This is a substring match, so it would match these tokens anywhere in the doc, including in prose. This is actually called out in the technical debt entry: "TestReconcilerConfigSurfaceDocumented uses strings.Contains for persona:/debate:/verify:/executor:, matching those tokens in prose as readily as in a real yaml block". So this is a known issue.

But is this a bug in the changed code? Yes, it's in the new test file. The technical debt entry itself acknowledges it. But the task says to find bugs, and this is a genuine bug in the new code - a substring match that could pass even if the config blocks aren't properly documented in YAML form.

However, the technical debt entry is about the same file and lines, and it's already been recorded. Let me check if it's in the changed regions... yes, `cmd/atcr/docs_audit_test.go:176` is in the new file. So I should flag it.

Actually wait - the technical debt README already records this as a known issue. But my job is to find bugs in the changed code, not to check if they're already known. Let me flag it.

### `TestDocsIndexCoversEveryDoc` (line ~216):

```go
func TestDocsIndexCoversEveryDoc(t *testing.T) {
	root := repoRootDir(t)
	docsDir := filepath.Join(root, "docs")

	all, err := filepath.Glob(filepath.Join(docsDir, "*.md"))
	if err != nil {
		t.Fatalf("glob docs: %v", err)
	}
	expected := map[string]bool{}
	for _, p := range all {
		base := filepath.Base(p)
		if base == "README.md" {
			continue // the index does not link itself
		}
		expected[base] = true
	}

	indexPath := filepath.Join(docsDir, "README.md")
	b, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("docs/README.md index must exist (T4): %v", err)
	}

	// Extract relative markdown link targets that point at a .md file, stripping
	// any #anchor and ./ prefix. External (http) and non-.md links are ignored.
	linked := map[string]bool{}
	re := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		target := m[1]
		if strings.Contains(target, "://") {
			continue
		}
		target = strings.SplitN(target, "#", 2)[0]
		target = strings.TrimPrefix(target, "./")
		if !strings.HasSuffix(target, ".md") || strings.Contains(target, "/") {
			continue // only same-directory doc links count toward the index
		}
		base := filepath.Base(target)
		if base == "README.md" {
			continue
		}
		// Dangling-link guard: every linked doc must exist on disk.
		if _, err := os.Stat(filepath.Join(docsDir, base)); err != nil {
			t.Errorf("docs/README.md links %q but that file does not exist", base)
			continue
		}
		linked[base] = true
	}

	for doc := range expected {
		if !linked[doc] {
			t.Errorf("docs/README.md index does not link docs/%s", doc)
		}
	}
	for doc := range linked {
		if !expected[doc] {
			t.Errorf("docs/README.md links docs/%s which is not an expected doc", doc)
		}
	}
}
```

The technical debt entry mentions: "TestDocsIndexCoversEveryDoc link regex treats a titled markdown link `](x.md "title")` as target `x.md "title"`, which fails the .md suffix check and would false-fail if a future index link adds a title". Let me verify this.

The regex is `\]\(([^)]+)\)`. For a link like `[text](x.md "title")`, the captured group would be `x.md "title"`. Then:
- `strings.Contains(target, "://")` → false
- `strings.SplitN(target, "#", 2)[0]` → `x.md "title"` (no `#`)
- `strings.TrimPrefix(target, "./")` → `x.md "title"` (no `./` prefix)
- `strings.HasSuffix(target, ".md")` → false! Because the target ends with `"` not `.md`.

So yes, a titled link would be skipped, and the doc would be reported as not linked. This is a real bug. The fix would be to strip the title before checking.

Let me also look at the regex more carefully. `\]\(([^)]+)\)` - this matches `](` followed by one or more non-`)` characters, followed by `)`. For `[text](x.md "title")`, the match would be `](x.md "title")` and the captured group would be `x.md "title"`. Yes, the title is included.

Actually, wait. `[^)]+` matches any character that is not `)`. So for `](x.md "title")`, it would match `x.md "title"` as the captured group. Then `strings.HasSuffix("x.md \"title\"", ".md")` is false. So the link would be skipped, and the doc would be reported as missing from the index. This is a real bug.

### `TestArchitectureDocDescribesReconciler` (line ~262):

```go
func TestArchitectureDocDescribesReconciler(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "docs", "architecture.md"))
	if err != nil {
		t.Fatalf("docs/architecture.md must exist (AC3): %v", err)
	}
	lower := strings.ToLower(string(b))
	for _, term := range []string{"review", "reconcile", "cluster", "dedupe", "confidence", "verify", "debate", "persona"} {
		if !strings.Contains(lower, term) {
			t.Errorf("docs/architecture.md does not describe the %q stage/concept of the reconciler", term)
		}
	}
}
```

The technical debt entry mentions: "TestArchitectureDocDescribesReconciler comment claims a fictional-architecture stub would fail, but it only checks 8 substrings exist anywhere, so a buzzword-sprinkled stub passes". This is a valid concern - the test only checks for substring presence, not structural ordering or meaningful content. But this is more of a test-strength issue than a bug.

### `TestDocsClaimedFlagsAreReal` (line ~289):

```go
func TestDocsClaimedFlagsAreReal(t *testing.T) {
	flags := canonicalFlags()
	re := regexp.MustCompile("`--([a-z][a-z-]+)`\\s+flag")
	for path, content := range auditedMarkdown(t) {
		for _, m := range re.FindAllStringSubmatch(content, -1) {
			name := m[1]
			if !flags[name] {
				t.Errorf("%s documents `--%s` as a flag but no such CLI flag exists", path, name)
			}
		}
	}
}
```

The regex `` `--([a-z][a-z-]+)`\s+flag `` matches backtick-enclosed `--flag` followed by whitespace and the word "flag". This looks correct for the stated purpose.

But wait - what about flags with numbers? Like `--level2`? The regex `[a-z][a-z-]+` wouldn't match `2`, so `--level2` wouldn't be captured. But this is probably fine since CLI flags are typically all lowercase letters and hyphens.

Let me now look at the other changed files for bugs.

### `docs/README.md`:

This is a new file with documentation links. Let me check if all the linked files exist. Actually, I can't verify that from the diff alone. But the test `TestDocsIndexCoversEveryDoc` would catch any missing links. Let me check if the README.md links match the docs/*.md files.

Looking at the links in docs/README.md:
- architecture.md
- registry.md
- providers.md
- skill-usage.md
- payload-modes.md
- execution.md
- verification.md
- cross-examination.md
- disagreement-radar.md
- findings-format.md
- personas-authoring.md
- personas-install.md
- ci-integration.md
- github-action.md
- code-review-backend.md
- benchmark.md
- scorecard.md
- metrics.md
- logging.md

That's 19 links. The test would verify these against the actual files in docs/*.md. If any of these files don't exist, the test would fail. But I can't verify this from the diff.

### `docs/architecture.md`:

This is a new file. Let me check for any issues.

Looking at the content... it mentions `ATCR_DISABLE_AST_GROUPING` environment variable. Is that a real env var? I can't verify from the diff.

It mentions `reconcile/dedupe.go` with `MergeThreshold = 0.7` and `GrayLow = 0.4`. These are referenced in the planning doc too. I can't verify if these constants exist.

It mentions `reconcile/distance.go` for the rejected NCD approach. Can't verify.

The doc seems well-structured. Let me look for any specific bugs.

Actually, looking at the architecture.md more carefully:

```
- **Deduplication is fixed internal behavior, not a configurable knob.** Within a
  cluster, findings are merged by structural similarity (AST isomorphism plus a
  token Jaccard measure). The cutoffs are hardcoded constants in
  `reconcile/dedupe.go` — findings at or above a `0.7` merge threshold are merged;
  the `0.4`–`0.7` gray zone is preserved as a soft/uncertain match rather than
  collapsed. (An earlier NCD-based distance was evaluated and rejected; see
  `reconcile/distance.go`.) There is no `.atcr/config.yaml` key to tune these.
```

This mentions `.atcr/config.yaml` which is correct per the clarifications.

### `docs/registry.md`:

The change is:
```
-1. **`--task-message` flag** — if provided it wins outright (even when empty: an explicit "no system prompt").
+1. **Programmatic task-message override** — an internal resolution seam (the `taskMessage` argument to persona resolution) that, when set, wins outright even when empty (an explicit "no system prompt"). It is **not** exposed as a CLI flag, so ordinary CLI and MCP runs pass nothing here and resolution effectively begins at level 2.
```

This looks correct - it's removing the reference to a non-existent `--task-message` flag.

Now let me go back and look more carefully at the test code for bugs.

### Re-examining `atcrInvocations`:

```go
capture := func(s string) {
    s = strings.TrimSpace(s)
    s = strings.TrimSpace(strings.TrimPrefix(s, "$ "))
    if !strings.HasPrefix(s, "atcr ") {
        return
    }
    if fields := strings.Fields(s); len(fields) > 1 {
        out = append(out, fields[1:]) // drop the leading "atcr"
    }
}
```

Wait, `strings.TrimPrefix(s, "$ ")` only removes ONE prefix. If the line is `$ $ atcr review`, it would become `$ atcr review` after the first TrimPrefix, then `TrimSpace` gives `$ atcr review`, then... wait, the `TrimSpace` is applied AFTER `TrimPrefix`. Let me re-read:

```go
s = strings.TrimSpace(s)
s = strings.TrimSpace(strings.TrimPrefix(s, "$ "))
```

So first `s` is trimmed, then `$ ` is stripped from the front, then trimmed again. If `s` is `$ atcr review`:
1. `TrimSpace("$ atcr review")` → `$ atcr review`
2. `TrimPrefix("$ atcr review", "$ ")` → `atcr review`
3. `TrimSpace("atcr review")` → `atcr review`
4. `HasPrefix("atcr review", "atcr ")` → true
5. `Fields("atcr review")` → `["atcr", "review"]`, len > 1, so `["review"]` is appended.

That's correct.

But what about `$atcr review` (no space after `$`)? 
1. `TrimSpace("$atcr review")` → `$atcr review`
2. `TrimPrefix("$atcr review", "$ ")` → `$atcr review` (no match, `$ ` != `$a`)
3. `TrimSpace("$atcr review")` → `$atcr review`
4. `HasPrefix("$atcr review", "atcr ")` → false
5. Returns, nothing appended.

So `$atcr review` wouldn't be captured. Is this a bug? In shell prompts, there's typically a space after `$`, so `$ atcr review` is the correct form. `$atcr` without a space is unusual. This seems intentional.

What about `> atcr review`? Some docs use `>` as a prompt. This wouldn't be captured because only `$ ` is stripped. But the function comment says "optionally behind a `$ ` shell prompt", so this is by design.

### Re-examining the inline code span regex:

```go
regexp.MustCompile("`([^`\n]+)`").FindAllStringSubmatch(md, -1)
```

This matches backtick-enclosed content that doesn't contain backticks or newlines. So `` `atcr review` `` would match and capture `atcr review`. But what about `` ``atcr review`` `` (double backticks)? In markdown, double backticks can be used to include single backticks in the code span. The regex `` `([^`\n]+)` `` would match the first `` ` `` and then look for the closing `` ` ``, which would be the second `` ` `` in `` `` ``. So `` ``atcr review`` `` would match as `` `atcr review` `` with the captured group being `` `atcr review` `` (including the inner backticks). Wait, no. Let me think again.

For `` ``atcr review`` ``, the string is:
`` ` `atcr review` ` ``

The regex `` `([^`\n]+)` `` would match:
- Starting at position 0: `` ` ``
- Then `[^`\n]+` matches `` `atcr review` `` (everything between the first and last backtick, which includes the inner backticks)
- Wait, no. `[^`\n]+` matches one or more characters that are NOT backtick or newline. So starting at position 0 (`` ` ``), the next character is `` ` `` which is a backtick, so `[^`\n]+` can't match. The regex would fail at position 0.

Actually, let me think about this more carefully. The string `` ``atcr review`` `` is:
Position 0: `
Position 1: `
Position 2: a
Position 3: t
...
Position 14: `
Position 15: `

The regex `` `([^`\n]+)` `` tries to match:
- `` ` `` at position 0 ✓
- `[^`\n]+` starting at position 1 - but position 1 is `` ` ``, which is excluded by `[^`...]`. So `[^`\n]+` fails (needs at least one match).
- The regex engine tries the next position.
- `` ` `` at position 1 ✓
- `[^`\n]+` starting at position 2 - `a` matches, `t` matches, ..., `e` matches, then position 14 is `` ` `` which doesn't match `[^`\n]`. So `[^`\n]+` matches `atcr review` (positions 2-13).
- `` ` `` at position 14 ✓
- Match! Captured group is `atcr review`.

So double-backtick code spans would actually work correctly! The regex would match the inner single backticks. Good.

But what about triple backticks? `` ```atcr review``` `` would be:
Position 0: `
Position 1: `
Position 2: `
Position 3: a
...

The regex would match:
- `` ` `` at position 0, then `[^`\n]+` at position 1 - but position 1 is `` ` ``, fails.
- `` ` `` at position 1, then `[^`\n]+` at position 2 - but position 2 is `` ` ``, fails.
- `` ` `` at position 2, then `[^`\n]+` at position 3 - `a` matches, ..., then position 13 is `` ` ``, stops. `[^`\n]+` matches `atcr review`.
- `` ` `` at position 13 ✓
- Match! Captured group is `atcr review`.

So triple backticks also work. Good.

But wait, what about the fenced code block detection? The fence detection happens separately:

```go
inFence := false
for _, ln := range strings.Split(md, "\n") {
    trimmed := strings.TrimSpace(ln)
    if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
        inFence = !inFence
        continue
    }
    if inFence {
        capture(ln)
    }
}
```

And the inline code span detection happens BEFORE the fence detection:

```go
for _, m := range regexp.MustCompile("`([^`\n]+)`").FindAllStringSubmatch(md, -1) {
    capture(m[1])
}
```

So inline code spans inside fenced blocks would be captured TWICE - once by the inline regex and once by the fence loop. Is this a bug?

Let me think about this. If a doc has:
```
Run `atcr review` to start.
```

The inline regex would capture `atcr review` from the inline code span. The fence loop would not capture it because it's not inside a fence.

If a doc has:
````
```
atcr review --flag
```
````

The inline regex would NOT capture `atcr review --flag` because it's not inside backticks - it's inside a fenced block. The fence loop would capture it because `inFence` is true.

But what if a doc has:
````
```
`atcr review --flag`
```
````

The inline regex would capture `atcr review --flag` from the inline code span inside the fence. The fence loop would also capture the line `` `atcr review --flag` `` and pass it to `capture`, which would TrimSpace it to `` `atcr review --flag` ``, then check `HasPrefix(..., "atcr ")` - but it starts with `` ` ``, not `atcr `, so it would return without appending. So there's no double-capture in this case.

But what if a doc has:
````
```
atcr review --flag
```
````

And also has an inline code span `` `atcr review --flag` `` somewhere else? Both would be captured, but they'd be separate entries in `out`. That's fine - the test would validate both.

Actually, I think the inline regex runs on the entire markdown string, including content inside fenced blocks. So if a fenced block contains `` `atcr review` ``, the inline regex would capture it. And the fence loop would also try to capture the line. But the fence loop's `capture(ln)` would receive the whole line including backticks, and after TrimSpace it would start with `` ` ``, not `atcr `, so it wouldn't be captured again. So there's no double-counting issue.

But wait - what if a fenced block contains a line like `atcr review` (without backticks)? The inline regex wouldn't capture it (no backticks), but the fence loop would capture it. That's correct.

And what if an inline code span contains `atcr review`? The inline regex would capture it. The fence loop would only capture it if it's inside a fence. If it's in prose (not in a fence), the fence loop wouldn't capture it. So no double-counting.

OK, I think the inline/fence interaction is fine.

### Let me look at the `canonicalFlags` function more carefully:

```go
func canonicalFlags() map[string]bool {
	flags := map[string]bool{"help": true, "version": true}
	add := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) { flags[f.Name] = true })
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		add(c.LocalFlags())
		add(c.PersistentFlags())
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
	return flags
}
```

This walks the command tree and collects all flag names. It seeds `help` and `version` manually because they're added lazily by cobra. This seems correct.

But wait - `c.LocalFlags()` and `c.PersistentFlags()` - does this capture inherited persistent flags from parent commands? In cobra, `PersistentFlags()` returns the persistent flags defined on that specific command, not inherited ones. `LocalFlags()` returns local flags plus persistent flags defined on that command. But inherited persistent flags from parent commands are not included in either.

Hmm, but the walk visits every command, so persistent flags defined on a parent would be captured when the parent is visited. So all flags should be captured. Unless a flag is defined as a persistent flag on a parent and the test checks it on a child command... but the test doesn't check which command a flag belongs to, just that the flag name exists somewhere in the tree. So this should be fine.

### Let me look at `commandGroups`:

```go
func commandGroups() map[string]map[string]bool {
	groups := map[string]map[string]bool{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		children := c.Commands()
		if len(children) > 0 {
			set := map[string]bool{}
			for _, sub := range children {
				set[sub.Name()] = true
			}
			groups[c.Name()] = set
		}
		for _, sub := range children {
			walk(sub)
		}
	}
	walk(newRootCmd())
	return groups
}
```

This maps each command that has subcommands to the set of its direct child names. The root command's name would be included. What is the root command's name? It's probably "atcr". So `groups["atcr"]` would contain the top-level commands.

In the test:
```go
if children, isGroup := groups[name0]; isGroup && len(tokens) > 1 {
    if next := tokens[1]; isWord(next) && !children[next] {
        t.Errorf(...)
    }
}
```

Here `name0` is the first token after "atcr", e.g., "review" in `atcr review`. If "review" is a group command (has subcommands), then `groups["review"]` would be its children. The test checks if the second token is a valid subcommand. This seems correct.

But what about the root command itself? If someone writes `atcr atcr review`, `name0` would be "atcr", and `groups["atcr"]` would be the set of top-level commands. `isWord("atcr")` would be true, and `children["atcr"]` would be false (assuming "atcr" is not a subcommand of itself). So the test would report an error. But `atcr atcr review` is not a valid command, so this is correct behavior.

### Let me re-examine the `TestDocsReferenceOnlyRealCommands` flag validation:

```go
for _, tok := range tokens {
    if m := reFlag.FindStringSubmatch(tok); m != nil && !flags[m[1]] {
        t.Errorf("%s references `--%s` on an `atcr %s` command, but no such flag exists", path, m[1], name0)
    }
}
```

This loop runs for every token in the invocation, including `name0` itself. If `name0` is `--version`, then `reFlag.FindStringSubmatch("--version")` would match and capture `version`. Since `flags["version"]` is true (seeded), it wouldn't report an error. Good.

But what if `name0` is a valid command like `review`, and a later token is `--output`? The regex would match `--output` and capture `output`. If `output` is not a real flag, it would report an error referencing `atcr review` (via `name0`). But `name0` is `review`, so the error message would say "on an `atcr review` command". That's correct.

But wait - what if `name0` is `--version` (a root flag)? Then the error message would say "on an `atcr --version` command", which is a bit odd but not wrong.

### Let me look at the `auditedMarkdown` function:

```go
func auditedMarkdown(t *testing.T) map[string]string {
	t.Helper()
	root := repoRootDir(t)
	out := map[string]string{}
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatalf("glob docs: %v", err)
	}
	paths := append(docs, filepath.Join(root, "