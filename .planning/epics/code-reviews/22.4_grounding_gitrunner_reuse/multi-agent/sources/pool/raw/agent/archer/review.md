The user wants me to find bugs in the provided diff.
I will act as "archer", focusing on snippet-level bugs in the changed regions.
I need to output findings in a specific format: `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE`

Let's analyze the diff carefully.

Files changed:
1. `.planning/epics/active/25.0_sarif_output_integration.md` - documentation changes, mostly text.
2. `.planning/epics/completed/22.4_grounding_gitrunner_reuse.md` - moved to completed.
3. `.planning/epics/superseded/23.0_human_persona_renaming.md` - merge conflict markers left in! `<<<<<<< HEAD... ======= >>>>>>> ...`
4. `.planning/product/presentation/index.html` - new file.
5. `.planning/product/presentation/style.css` - new file.
6. `CHANGELOG.md` - updated.
7. `internal/fanout/resume.go` - changed `buildPayloads` return value, passed to `computeGroundingData`.
8. `internal/payload/builder.go` - refactored `buildEntries`, added `buildEntriesValidated`.
9. `internal/payload/diff.go` - added `zeroCtxChunks`, modified `rangeChunks` to call it.
10. `internal/payload/grounding.go` - added `changedLines`, modified `BuildChangedLines`.
11. `internal/payload/rangebuilder.go` - new file.
12. `internal/payload/rangebuilder_test.go` - new test file.

Let's look closely at Go code changes.

`internal/fanout/resume.go`:
```go
	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
	// ...
	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
```
Wait, `computeGroundingData` signature changed to take `rb`. What is `rb`? It's the return value from `buildPayloads`. Let's check `internal/payload/builder.go` or similar to see if `buildPayloads` returns a `*RangeBuilder` or something. The diff doesn't show `buildPayloads`, but it shows `NewRangeBuilder` in `rangebuilder.go`. Wait, `rb` is likely the builder. But in `resume.go`, it's passed as `rb` to `computeGroundingData`. Is `rb` defined? Yes, from `buildPayloads`.

Let's check `internal/payload/rangebuilder.go`:
```go
func NewRangeBuilder(ctx context.Context, repo, base, head string) *RangeBuilder {
	return &RangeBuilder{
		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
		base: b.base, // wait, typo? `b` is not defined. It should be `repo`.
```
Ah! Line 28 in `rangebuilder.go`:
`base: repo,` -> The diff shows:
```go
	return &RangeBuilder{
		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
		base: b.base,
		head: head,
	}
```
Wait, let's look at the exact text in the prompt for `rangebuilder.go`:
```go
func NewRangeBuilder(ctx context.Context, repo, base, head string) *RangeBuilder {
	return &RangeBuilder{
		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
		base: b.base,
		head: head,
	}
}
```
Yes! `b.base` is a typo. It should be `base`. This will cause a compile error or runtime panic if it somehow compiled (but Go won't compile it). Actually, `b` is undefined. So this is a CRITICAL/HIGH compile error or bug. Let's check the exact line in the prompt:
```go
+	return &RangeBuilder{
+		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
+		base: b.base,
+		head: head,
+	}
```
This is definitely a bug. `b` is not in scope. It should be `base`.

Let's check other parts of `rangebuilder.go`:
```go
func (b *RangeBuilder) validate() error {
	if b.validated {
		return nil
	}
	if err := validateRange(b.g, b.base, b.head); err != nil {
		return err
	}
	b.validated = true
	return nil
}
```
This looks correct.

Check `internal/payload/diff.go`:
```go
+// rangeChunks memoizes the whole-range zero-context diff, split per head path
+// and parsed into changed line ranges, so the N per-file range queries collapse
-// to a single git process.
+// to a single git process. It reuses the memoized raw zero-context chunks so the
+// grounding builder shares the same --unified=0 process.
 func (g *gitRunner) rangeChunks(base, head string) (map[string][]lineRange, error) {
 	s := g.forRange(base, head)
 	if s.lineRanges != nil {
 		return s.lineRanges, nil
 	}
-	chunks, err := g.diffChunks(base, head, "--unified=0")
+	chunks, err := g.zeroCtxChunks(base, head)
```
Looks fine.

Check `internal/payload/grounding.go`:
```go
 func BuildChangedLines(ctx context.Context, repo, base, head string) (ChangedLines, error) {
 	g := &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)}
 	if err := validateRange(g, base, head); err != nil {
 		return nil, err
 	}
-	chunks, err := g.diffChunks(base, head, "--unified=0")
+	return g.changedLines(base, head)
```
Wait, `changedLines` calls `g.zeroCtxChunks`. `zeroCtxChunks` does:
```go
func (g *gitRunner) zeroCtxChunks(base, head string) (map[string]string, error) {
	s := g.forRange(base, head)
	if s.zeroCtx != nil {
		return s.zeroCtx, nil
	}
	m, err := g.diffChunks(base, head, "--unified=0")
```
This looks consistent.

Check `internal/payload/rangebuilder_test.go`:
```go
+func TestRangeBuilder_GroundingReusesPayloadGitProcesses(t *testing.T) {
...
+	rb := NewRangeBuilder(context.Background(), dir, base, head)
```
Wait, in the test, they call `NewRangeBuilder(context.Background(), dir, base, head)`. But the function signature is `NewRangeBuilder(ctx context.Context, repo, base, head string)`. That's fine.

Let's look at `internal/fanout/resume.go` again:
```go
-	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
+	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
...
-	changed, groundingDisabledReason := computeGroundingData(ctx, req)
+	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
```
Does `computeGroundingData` exist? The diff doesn't show its definition, but it's in `review.go`. It's plausible.

Wait, look at `internal/payload/rangebuilder.go` line 27-30:
```go
+func NewRangeBuilder(ctx context.Context, repo, base, head string) *RangeBuilder {
+	return &RangeBuilder{
+		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
+		base: b.base,
+		head: head,
+	}
+}
```
`b` is definitely undefined. It should be `base`. This is a compile error. CRITICAL.

Are there any other bugs?
Let's check `internal/payload/diff.go`:
```go
+// zeroCtxChunks memoizes the whole-range zero-context (--unified=0) diff split
+// per head path (raw chunks). Both rangeChunks (files-mode line-range parse) and
+// changedLines (grounding changed-text parse) consume this one process, so the
+// zero-context diff runs once per range across payload building and grounding.
 func (g *gitRunner) zeroCtxChunks(base, head string) (map[string]string, error) {
 	s := g.forRange(base, head)
 	if s.zeroCtx != nil {
 		return s.zeroCtx, nil
 	}
 	m, err := g.diffChunks(base, head, "--unified=0")
 	if err != nil {
 		return nil, err
 	}
 	s.zeroCtx = m
 	return m, nil
 }
```
Looks correct.

Check `internal/payload/grounding.go`:
```go
+// changedLines builds the grounding data from the memoized whole-range
+// zero-context diff. It is the runner-bound core shared by the standalone
+// BuildChangedLines and RangeBuilder.BuildChangedLines: when the range's payload
+// was already built on the same gitRunner, the --unified=0 chunks (and the
+// --name-status underlying them) are served from cache, so grounding adds no git
+// subprocess. Callers must validate the range before the first cache-populating
+// call (BuildChangedLines and RangeBuilder both do).
 func (g *gitRunner) changedLines(base, head string) (ChangedLines, error) {
 	chunks, err := g.zeroCtxChunks(base, head)
```
Looks correct.

Check `internal/payload/rangebuilder.go` again:
```go
+// BuildEntries returns the per-file payload contributions for mode, reusing the
+// builder's memoized range caches. Mirrors the package-level BuildEntries.
 func (b *RangeBuilder) BuildEntries(mode PayloadMode) ([]FileEntry, error) {
 	if mode != ModeDiff && mode != ModeBlocks && mode != ModeFiles {
 		return nil, fmt.Errorf("unknown payload mode '%s': must be one of diff, blocks, files", mode)
 	}
 	if err := b.validate(); err != nil {
 		return nil, err
 	}
 	return b.g.buildEntriesValidated(mode, b.base, b.head)
 }
```
Looks correct.

Check `internal/payload/rangebuilder_test.go`:
```go
+func TestRangeBuilder_ChangedLinesMatchesStandalone(t *testing.T) {
...
+	rb := NewRangeBuilder(ctx, dir, base, head)
```
All good.

What about the merge conflict markers in `.planning/epics/superseded/23.0_human_persona_renaming.md`?
The prompt says: "Review only the changed regions." and "If nothing is wrong, emit nothing." But this file has `<<<<<<< HEAD... ======= >>>>>>> ...`. That's a clear bug/artifact in the diff. It should be categorized as `correctness` or `style` or `out-of-scope`? The prompt says: "If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope". This is a changed file with merge conflict markers. I'll flag it as HIGH|...|CATEGORY: correctness or style. Actually, it's a clear bug in the commit/diff. I will report it.

Wait, the prompt says: "You see bugs that hide in plain sight at the line and expression level — the ones a telescope misses."
The `b.base` typo is definitely a snippet-level bug.

Let's verify line numbers for `rangebuilder.go`.
The diff shows:
```go
+func NewRangeBuilder(ctx context.Context, repo, base, head string) *RangeBuilder {
+	return &RangeBuilder{
+		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
+		base: b.base,
+		head: head,
+	}
+}
```
Line numbers in the diff start around line 28 (relative to file). I'll use `internal/payload/rangebuilder.go:29` or just `internal/payload/rangebuilder.go:28`. The diff shows it's the first few lines of the new file. I'll cite `internal/payload/rangebuilder.go:29`.

Another thing: `internal/fanout/resume.go`
```go
-	changed, groundingDisabledReason := computeGroundingData(ctx, req)
+	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
```
Does `computeGroundingData` accept a third argument? The diff doesn't show its signature, but it's plausible. I won't flag it unless obvious.

Check `internal/payload/diff.go`:
```go
+// rangeChunks memoizes the whole-range zero-context diff, split per head path
+// and parsed into changed line ranges, so the N per-file range queries collapse
-// to a single git process.
+// to a single git process. It reuses the memoized raw zero-context chunks so the
+// grounding builder shares the same --unified=0 process.
 func (g *gitRunner) rangeChunks(base, head string) (map[string][]lineRange, error) {
 	s := g.forRange(base, head)
 	if s.lineRanges != nil {
 		return s.lineRanges, nil
 	}
-	chunks, err := g.diffChunks(base, head, "--unified=0")
+	chunks, err := g.zeroCtxChunks(base, head)
```
This is fine.

Check `internal/payload/grounding.go`:
```go
 func BuildChangedLines(ctx context.Context, repo, base, head string) (ChangedLines, error) {
 	g := &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)}
 	if err := validateRange(g, base, head); err != nil {
 		return nil, err
 	}
-	chunks, err := g.diffChunks(base, head, "--unified=0")
+	return g.changedLines(base, head)
```
Wait! `changedLines` calls `g.zeroCtxChunks`. `zeroCtxChunks` does:
```go
func (g *gitRunner) zeroCtxChunks(base, head string) (map[string]string, error) {
	s := g.forRange(base, head)
	if s.zeroCtx != nil {
		return s.zeroCtx, nil
	}
	m, err := g.diffChunks(base, head, "--unified=0")
```
This is fine.

Let's look at `internal/payload/rangebuilder.go` line 29 again.
`base: b.base,` -> `b` is the receiver of `NewRangeBuilder`? No, `NewRangeBuilder` takes `(ctx context.Context, repo, base, head string)`. It does not have a receiver. So `b` is undefined. This is a compile error.

Is there any other bug?
Check `internal/payload/rangebuilder_test.go`:
```go
+func TestRangeBuilder_GroundingReusesPayloadGitProcesses(t *testing.T) {
...
+	rb := NewRangeBuilder(context.Background(), dir, base, head)
```
All good.

Check `internal/fanout/resume.go` line 273:
```go
-	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
+	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
```
Wait, `buildPayloads` is likely in `internal/payload/builder.go` or similar. The diff doesn't show it changing, but it implies `buildPayloads` now returns a second value `rb`. This is fine if the caller matches.

Check `internal/payload/builder.go`:
```go
 func (g *gitRunner) buildEntriesValidated(mode PayloadMode, base, head string) ([]FileEntry, error) {
 	files, err := g.changedFilesMemo(base, head)
 	if err != nil {
 		return nil, err
 	}
 	entries := make([]FileEntry, 0, len(files))
 	for _, f := range files {
 		body, err := g.fileBody(mode, base, head, f)
```
Looks fine.

Check `.planning/epics/superseded/23.0_human_persona_renaming.md`:
It contains merge conflict markers:
`<<<<<<<< HEAD:...`
`========`
`>>>>>>>> ...`
This is a clear bug in the diff. I will report it.

Let's verify line numbers for `rangebuilder.go`. The prompt shows:
```go
+func NewRangeBuilder(ctx context.Context, repo, base, head string) *RangeBuilder {
+	return &RangeBuilder{
+		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
+		base: b.base,
+		head: head,
+	}
+}
```
Line 28 is `func NewRangeBuilder...`
Line 29 is `	return &RangeBuilder{`
Line 30 is `		g:    &gitRunner{...}`
Line 31 is `		base: b.base,`
So `internal/payload/rangebuilder.go:31`.

I will format the finding as:
CRITICAL|internal/payload/rangebuilder.go:31|Undefined variable `b` used for struct field initialization; should be `base`|Change `b.base` to `base`|correctness|2|base: b.base,

And for the merge conflict marker:
HIGH|.planning/epics/superseded/23.0_human_persona_renaming.md:51|Unresolved git merge conflict markers present in committed file|Remove `<<<<<<< HEAD`, `========`, and `>>>>>>>` blocks|out-of-scope|2|<<<<<<< HEAD:.planning/epics/completed/23.0_human_persona_renaming.md...

Wait, the prompt says "If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope". This file is changed (the diff shows changes to it). The merge conflict markers are in the changed region. I'll use `correctness` or `style`. I'll stick with `correctness`.

Let's double check `internal/payload/rangebuilder.go` line 31.
Is there any chance `b` is defined elsewhere? No, it's a function without a receiver. `NewRangeBuilder` takes `(ctx context.Context, repo, base, head string)`. So `b` is definitely a typo for `base`.

What about `internal/fanout/resume.go` line 263:
```go
-	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
+	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
```
And line 286:
```go
-	changed, groundingDisabledReason := computeGroundingData(ctx, req)
+	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
```
This assumes `buildPayloads` returns two values now. The diff doesn't show `buildPayloads`, but it's plausible. I won't flag it.

Check `internal/payload/diff.go` line 470:
```go
+// zeroCtxChunks memoizes the whole-range zero-context (--unified=0) diff split
+// per head path (raw chunks). Both rangeChunks (files-mode line-range parse) and
+// changedLines (grounding changed-text parse) consume this one process, so the
+// zero-context diff runs once per range across payload building and grounding.
 func (g *gitRunner) zeroCtxChunks(base, head string) (map[string]string, error) {
```
Looks fine.

Check `internal/payload/grounding.go` line 31:
```go
+// changedLines builds the grounding data from the memoized whole-range
+// zero-context diff. It is the runner-bound core shared by the standalone
+// BuildChangedLines and RangeBuilder.BuildChangedLines: when the range's payload
+// was already built on the same gitRunner, the --unified=0 chunks (and the
+// --name-status underlying them) are served from cache, so grounding adds no git
+// subprocess. Callers must validate the range before the first cache-populating
+// call (BuildChangedLines and RangeBuilder both do).
 func (g *gitRunner) changedLines(base, head string) (ChangedLines, error) {
```
Looks fine.

One more thing: `internal/payload/rangebuilder_test.go` line 24:
```go
+	rb := NewRangeBuilder(context.Background(), dir, base, head)
```
Wait, the function signature is `NewRangeBuilder(ctx context.Context, repo, base, head string)`. The test passes `dir` as `repo`, `base` as `base`, `head` as `head`. That's correct.

Let's verify line numbers for `internal/payload/rangebuilder.go` carefully from the prompt:
```go
+// RangeBuilder computes every whole-range artifact a fan-out review needs — the
...
+type RangeBuilder struct {
...
+func NewRangeBuilder(ctx context.Context, repo, base, head string) *RangeBuilder {
+	return &RangeBuilder{
+		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
+		base: b.base,
+		head: head,
+	}
+}
```
Counting lines from the start of the file in the diff:
1-27: comments and imports
28: `func NewRangeBuilder...`
29: `	return &RangeBuilder{`
30: `		g:    &gitRunner{...}`
31: `		base: b.base,`
So `internal/payload/rangebuilder.go:31` is accurate.

I will formulate the findings now.
Format: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE

Finding 1:
CRITICAL|internal/payload/rangebuilder.go:31|Undefined variable `b` used for struct field initialization; should be `base`|Change `b.base` to `base`|correctness|2|base: b.base,

Finding 2:
HIGH|.planning/epics/superseded/23.0_human_persona_renaming.md:51|Unresolved git merge conflict markers present in committed file|Remove `<<<<<<< HEAD`, `========`, and `>>>>>>>` blocks|out-of-scope|2|<<<<<<< HEAD:.planning/epics/completed/23.0_human_persona_renaming.md...

Wait, the prompt says "If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope". The file is changed in the diff (it has additions and deletions). So `out-of-scope` might not be strictly correct if it's part of the change. I'll use `correctness` or `style`. Actually, merge conflict markers are a clear bug. I'll stick with `correctness`. Or I can just flag it as `HIGH|.planning/epics/superseded/23.0_human_persona_renaming.md:51|Unresolved git merge conflict markers remain in the file|Remove the `<<<<<<<`, `========`, and `>>>>>>>` blocks|style|1|<<<<<<< HEAD:...`

Let's check if there are any other subtle bugs.
In `internal/payload/rangebuilder.go`:
```go
 func (b *RangeBuilder) validate() error {
 	if b.validated {
 		return nil
 	}
 	if err := validateRange(b.g, b.base, b.head); err != nil {
 		return err
 	}
 	b.validated = true
 	return nil
 }
```
This is fine.

In `internal/payload/diff.go`:
```go
 func (g *gitRunner) rangeChunks(base, head string) (map[string][]lineRange, error) {
 	s := g.forRange(base, head)
 	if s.lineRanges != nil {
 		return s.lineRanges, nil
 	}
 	chunks, err := g.zeroCtxChunks(base, head)
```
Wait, `zeroCtxChunks` does:
```go
 func (g *gitRunner) zeroCtxChunks(base, head string) (map[string]string, error) {
 	s := g.forRange(base, head)
 	if s.zeroCtx != nil {
 		return s.zeroCtx, nil
 	}
 	m, err := g.diffChunks(base, head, "--unified=0")
```
It uses `g.forRange(base, head)` on both. That's fine.

In `internal/payload/grounding.go`:
```go
 func BuildChangedLines(ctx context.Context, repo, base, head string) (ChangedLines, error) {
 	g := &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)}
 	if err := validateRange(g, base, head); err != nil {
 		return nil, err
 	}
 	return g.changedLines(base, head)
 }
```
`changedLines` calls `g.zeroCtxChunks`. That's fine.

Check `internal/fanout/resume.go`:
```go
-	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
+	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
...
-	changed, groundingDisabledReason := computeGroundingData(ctx, req)
+	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
```
This implies `buildPayloads` now returns a second value, and `computeGroundingData` takes a third. This is part of the epic's refactoring. It's plausible.

One minor thing: `internal/payload/rangebuilder.go` line 31 typo is definitely a compile error. I'll report it as CRITICAL.

Are there any off-by-one or null dereferences?
`internal/payload/diff.go`:
```go
 func parseHeadRanges(chunk string) []lineRange {
 	var ranges []lineRange
 	for _, line := range strings.Split(chunk, "\n") {
 		m := hunkHeaderRe.FindStringSubmatch(line)
 		if m == nil {
 			continue
 		}
 		start, _ := strconv.Atoi(m[1])
 		length := 1
 		if m[2] != "" {
 			length, _ = strconv.Atoi(m[2])
 		}
```
This is standard. No obvious bugs.

Check `internal/payload/rangebuilder_test.go`:
```go
+func TestRangeBuilder_InvalidRefError(t *testing.T) {
...
+	rb := NewRangeBuilder(context.Background(), "deadbeefdeadbeef", head)
```
Wait! `NewRangeBuilder` signature is `NewRangeBuilder(ctx context.Context, repo, base, head string)`.
The test calls it with 4 arguments: `context.Background()`, `"deadbeefdeadbeef"`, `head`. That's only 3 arguments after ctx? Wait, the function takes `(ctx, repo, base, head string)`. The call has 4 args total. That matches. But `"deadbeefdeadbeef"` is passed as `repo`? No, it's passed as `base`. The test meant to pass `dir` as repo, but wrote `"deadbeefdeadbeef"` as the second arg. Actually, looking at the test:
```go
+	rb := NewRangeBuilder(context.Background(), "deadbeefdeadbeef", head)
```
It's missing an argument! `NewRangeBuilder` takes 4 args. This call only provides 3. This will be a compile error in the test file.
Let's count: `context.Background()` (1), `"deadbeefdeadbeef"` (2), `head` (3). It needs 4. So it's missing `repo`.
Wait, maybe the function signature in the test is different? No, it's the same package.
So `internal/payload/rangebuilder_test.go:98` (approx) has a wrong number of arguments for `NewRangeBuilder`.
Let's check the exact line in the diff:
```go
+	rb := NewRangeBuilder(context.Background(), "deadbeefdeadbeef", head)
```
Yes, missing one argument. I'll flag it as CRITICAL or HIGH. Actually, it's a compile error in tests. HIGH.

Let's verify line numbers for the test file.
The diff shows:
```go
+// An unresolvable ref surfaces the validation error from both build entry points,
+// and the range is not marked validated (so a later valid call would re-run it).
 func TestRangeBuilder_InvalidRefError(t *testing.T) {
 	dir := initRepo(t)
 	write(t, dir, "a.go", goFileV1)
 	head := commitAll(t, dir, "v1")

 	rb := NewRangeBuilder(context.Background(), "deadbeefdeadbeef", head)
```
Line number is around 98. I'll use `internal/payload/rangebuilder_test.go:98`.

Wait, `NewRangeBuilder` signature: `(ctx context.Context, repo, base, head string)`
Call in test: `NewRangeBuilder(context.Background(), "deadbeefdeadbeef", head)` -> 3 args. Missing `repo`.
This is a compile error. I'll report it.

Another check: `internal/payload/rangebuilder.go` line 31 `b.base` typo.
Both are compile errors. I will list them.

Format:
CRITICAL|internal/payload/rangebuilder.go:31|Undefined variable `b` used for struct field initialization; should be `base`|Change `b.base` to `base`|correctness|2|base: b.base,
HIGH|internal/payload/rangebuilder_test.go:98|Call to `NewRangeBuilder` passes 3 arguments but function expects 4 (missing `repo`)|Add the repository path as the second argument|correctness|2	rb := NewRangeBuilder(context.Background(), "deadbeefdeadbeef", head)

Check line numbers carefully from the prompt:
`rangebuilder.go` starts at line 1 in the diff block. The function is at line 28. `base: b.base,` is line 31. Correct.
`rangebuilder_test.go` starts at line 1. `TestRangeBuilder_InvalidRefError` starts around line 93. The call is on line 98. Correct.

What about the merge conflict markers? I'll include it as HIGH with category `style` or `correctness`. The prompt says "If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope". This file is changed, so I'll use `correctness`. Actually, it's just a leftover from a merge. I'll put `out-of-scope` as instructed for messy diffs, or just `style`. I'll stick to `correctness`.

Wait, the prompt says: "If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope". This implies if it's in changed code, use the appropriate category. I'll use `correctness`.

Let's verify one more thing in `internal/payload/rangebuilder.go`:
```go
 func (b *RangeBuilder) BuildEntries(mode PayloadMode) ([]FileEntry, error) {
 	if mode != ModeDiff && mode != ModeBlocks && mode != ModeFiles {
 		return nil, fmt.Errorf("unknown payload mode '%s': must be one of diff, blocks, files", mode)
 	}
```
This handles unknown modes. Fine.

Check `internal/payload/diff.go`:
```go
 func (g *gitRunner) rangeChunks(base, head string) (map[string][]lineRange, error) {
 	s := g.forRange(base, head)
 	if s.lineRanges != nil {
 		return s.lineRanges, nil
 	}
 	chunks, err := g.zeroCtxChunks(base, head)
```
Fine.

Check `internal/payload/grounding.go`:
```go
 func BuildChangedLines(ctx context.Context, repo, base, head string) (ChangedLines, error) {
 	g := &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)}
 	if err := validateRange(g, base, head); err != nil {
 		return nil, err
 	}
 	return g.changedLines(base, head)
 }
```
Fine.

All good. I will generate the output now.
One minor point: `internal/payload/rangebuilder_test.go` line 98 might be slightly off in line number depending on how the diff is counted, but it's clearly in that function. I'll use `internal/payload/rangebuilder_test.go:98`.

Let's double check the exact line for the test call:
```go
+	rb := NewRangeBuilder(context.Background(), "deadbeefdeadbeef", head)
```
It's definitely missing an argument.

I will produce the findings exactly as requested.
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE
CRITICAL|internal/payload/rangebuilder.go:31|Undefined variable `b` used for struct field initialization; should be `base`|Change `b.base` to `base`|correctness|2|base: b.base,
HIGH|internal/payload/rangebuilder_test.go:98|Call to `NewRangeBuilder` passes 3 arguments but function expects 4 (missing `repo`)|Add the repository path as the second argument|correctness|2|rb := NewRangeBuilder(context.Background(), "deadbeefdeadbeef", head)
HIGH|.planning/epics/superseded/23.0_human_persona_renaming.md:51|Unresolved git merge conflict markers remain in the file|Remove `<<<<<<<`, `========`, and `>>>>>>>` blocks|out-of-scope|1|<<<<<<< HEAD:.planning/epics/completed/23.0_human_persona_renaming.md...

Wait, the prompt says "If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope". The file is changed, but the conflict markers are artifacts. I'll keep `out-of-scope` to be safe, or change to `style`. I'll stick with `out-of-scope` as instructed for non-code artifacts or use `correctness`. I'll use `correctness` since it's a direct diff artifact. Actually, the prompt says "If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope". This file is changed, so `out-of-scope` might not apply. I'll use `style`. But it's fine either way. I'll just use `correctness`.

Let's verify line numbers one last time.
`rangebuilder.go` starts at line 1 in the diff block.
Line 28: `func NewRangeBuilder...`
Line 31: `		base: b.base,` -> Correct.
`rangebuilder_test.go` starts at line 1 in the diff block.
The function `TestRangeBuilder_InvalidRefError` starts around line 93.
Line 98: `	rb := NewRangeBuilder(context.Background(), "deadbeefdeadbeef", head)` -> Correct.

All set. Output matches format.
Proceed. 
One detail: `internal/payload/rangebuilder.go:31` evidence should be exactly as in the diff.
`base: b.base,`
Correct.

Final check of the prompt: "Emit ONLY findings, one per line, exactly 7 pipe-delimited columns".
I will output exactly that.
No extra text.
Ready. 
Output matches requirements.✅
Proceeds. 
[Self-Correction/Refinement during thought]
Wait, `internal/payload/rangebuilder_test.go` line 98 might be slightly different if I count the `+`