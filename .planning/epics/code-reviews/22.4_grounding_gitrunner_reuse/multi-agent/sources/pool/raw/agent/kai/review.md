 The user wants me to act as "kai", an architecture and design reviewer. I need to review the changed regions in the provided diff, focusing on the sprint plan: "Reuse Payload Builder's Diff in Grounding Data" (Epic 22.4).

Let me analyze the changed files and lines:

1. `.planning/epics/active/25.0_sarif_output_integration.md` - Out of scope for Epic 22.4. This is SARIF output integration. Not related to the sprint plan. Should ignore unless critical.
2. `.planning/epics/completed/22.4_grounding_gitrunner_reuse.md` - Epic move to completed. Not code.
3. `.planning/epics/superseded/23.0_human_persona_renaming.md` - Out of scope. Not related.
4. `.planning/product/presentation/index.html` - Out of scope. Presentation deck.
5. `.planning/product/presentation/style.css` - Out of scope.
6. `CHANGELOG.md` - Changelog entry for 22.4.0. Not code.
7. `internal/fanout/resume.go` - Changed lines around 260-310. This is in scope (internal/fanout). It changes `buildPayloads` to return `rb` (RangeBuilder), and passes `rb` to `computeGroundingData`.
8. `internal/payload/builder.go` - Changed lines around 93-120. Adds `buildEntriesValidated` method. In scope (internal/payload).
9. `internal/payload/diff.go` - Changed lines around 45-65 and 456-500. Adds `zeroCtx` field to `rangeState`, adds `zeroCtxChunks` method, modifies `rangeChunks` to use `zeroCtxChunks`. In scope.
10. `internal/payload/grounding.go` - Changed lines around 28-55. Modifies `BuildChangedLines` to use `g.changedLines`, adds `changedLines` method. In scope.
11. `internal/payload/rangebuilder.go` - New file. In scope.
12. `internal/payload/rangebuilder_test.go` - New file. In scope.

Now I need to look for architecture/design issues in the in-scope code changes.

Let me review carefully:

**internal/payload/rangebuilder.go:**
- `RangeBuilder` struct has `g *gitRunner`, `base, head string`, `validated bool`.
- `NewRangeBuilder` creates a new `gitRunner`.
- `validate()` runs `validateRange` once.
- `BuildEntries` calls `validate()` then `b.g.buildEntriesValidated(mode, b.base, b.head)`.
- `BuildChangedLines` calls `validate()` then `b.g.changedLines(b.base, b.head)`.

Issues I see:
1. `RangeBuilder` is NOT safe for concurrent use (documented), but `BuildEntries` and `BuildChangedLines` both mutate `g`'s cache. The comment says "Callers use it sequentially (payload modes, then grounding)." This seems fine.

2. In `BuildEntries`, the unknown mode check mirrors package-level `BuildEntries`. But `buildEntriesValidated` doesn't check mode. Wait, actually `BuildEntries` checks mode. But `buildEntriesValidated` in builder.go doesn't check mode. Let me check `builder.go`:
```go
func (g *gitRunner) buildEntriesValidated(mode PayloadMode, base, head string) ([]FileEntry, error) {
```
It doesn't check mode. But `BuildEntries` in rangebuilder.go does check mode. Good.

3. **Coupling issue**: `internal/fanout/resume.go` now calls `buildPayloads` which returns `(payloads, rb, err)`. Let me check the diff for `internal/fanout/review.go` - it's not shown in the payload! The payload only shows `resume.go`. Wait, the epic says `internal/fanout/review.go:470` is the target. But `review.go` is NOT in the diff payload. Only `resume.go` is shown from `internal/fanout`.

Wait, looking at the diff:
- `internal/fanout/resume.go` changes `buildPayloads` call to accept and return `rb`.

But where is `buildPayloads` defined? It's likely in `internal/fanout/review.go`. Since `review.go` is not in the diff payload, I cannot see its changes. However, the scope constraint says I should review only changed regions. If `review.go` is not in the payload, I cannot review it. But wait - the payload shows 12 changed files. Let me recount:
1. `.planning/epics/active/25.0_sarif_output_integration.md`
2. `.planning/epics/completed/22.4_grounding_gitrunner_reuse.md`
3. `.planning/epics/superseded/23.0_human_persona_renaming.md`
4. `.planning/product/presentation/index.html`
5. `.planning/product/presentation/style.css`
6. `CHANGELOG.md`
7. `internal/fanout/resume.go`
8. `internal/payload/builder.go`
9. `internal/payload/diff.go`
10. `internal/payload/grounding.go`
11. `internal/payload/rangebuilder.go`
12. `internal/payload/rangebuilder_test.go`

That's 12 files. So `internal/fanout/review.go` is NOT in the diff. But `resume.go` calls `buildPayloads` and gets a third return value `rb`. This means `buildPayloads` signature changed. Since `review.go` is not visible, I can only comment on what I see in `resume.go`.

Wait, but `buildPayloads` might be defined in `review.go`. The diff doesn't show it. I cannot verify the implementation. However, I can flag issues based on what I see in `resume.go`.

In `resume.go`:
```go
	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
```
and later:
```go
	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
```

So `computeGroundingData` now takes `rb`. But I don't see `computeGroundingData`'s definition in the diff either. It's likely in `review.go` or another file. Since it's not in the diff, I should not flag issues in its implementation unless they are visible from the call site.

Wait, let me look more carefully at `internal/fanout/resume.go`. The function `PrepareResume` rebuilds payloads:
```go
	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
```
Then it builds slots using `buildSlots(cfg, payloads, req.Range, "", scopeConstraint, false)`.
Then it calls `computeGroundingData(ctx, req, rb)`.

This seems correct architecturally: reuse the builder.

But wait - there's a subtle issue. `PrepareResume` reads the manifest, validates range and roster. Then it rebuilds payloads. Then it calls `computeGroundingData` with `rb`. The `RangeBuilder` is constructed inside `buildPayloads` (presumably). 

Actually, I see a potential issue: `buildPayloads` now returns a `*payload.RangeBuilder`. This creates a coupling where `internal/fanout` knows about `payload.RangeBuilder`. Was it already importing `internal/payload`? Probably yes, since it was calling `payload.BuildEntries` or similar before.

But there's a design issue in `RangeBuilder` itself:

**Issue 1: `RangeBuilder` exposes internal `gitRunner` via return value?**
Looking at `rangebuilder.go`, `RangeBuilder` struct fields are unexported except... wait, in `resume.go`, the return value is `rb`. The type would be `*payload.RangeBuilder`. That's fine, it's an exported type.

But looking at `TestRangeBuilder_GroundingReusesPayloadGitProcesses`:
```go
	afterPayload := rb.g.execCount
```
It accesses `rb.g.execCount`. `g` is lowercase `g` in `RangeBuilder` struct:
```go
type RangeBuilder struct {
	g          *gitRunner
```
Wait, `g` is unexported. How does the test access it? The test is in `package payload` (same package). So that's fine.

But wait, `RangeBuilder` is exported (`type RangeBuilder struct`), yet its field `g` is unexported. That's fine.

**Issue 2: `BuildEntries` mode validation duplicates logic?**
In `rangebuilder.go`:
```go
	if mode != ModeDiff && mode != ModeBlocks && mode != ModeFiles {
		return nil, fmt.Errorf("unknown payload mode '%s': must be one of diff, blocks, files", mode)
	}
```
This duplicates the validation in `BuildEntries` (package-level). Actually, looking at `builder.go`, the package-level `BuildEntries` doesn't validate mode? Let me check the original code... The diff doesn't show the original `BuildEntries` signature or validation. The new `buildEntriesValidated` doesn't validate mode. The `RangeBuilder.BuildEntries` validates mode. This is fine, but it means if someone calls `buildEntriesValidated` directly, they skip validation.

Actually, looking at original `builder.go` in the diff:
```go
func BuildEntries(ctx context.Context, mode PayloadMode, repo, base, head string) ([]FileEntry, error) {
```
The body is not shown (only `buildEntries` method is shown). So I can't see if mode validation existed there.

**Issue 3: Error message format inconsistency?**
The error message says `unknown payload mode '%s': must be one of diff, blocks, files`. Note the mode names. But `PayloadMode` is likely a string type. If `mode` is empty string, it prints empty quotes. Minor.

**Issue 4: `zeroCtx` map added to `rangeState` but no mutex?**
`rangeState` is a struct inside `gitRunner`. Looking at `diff.go`:
```go
type rangeState struct {
	key        string
	files      []changedFile
	binary     map[string]bool
	fc         map[string]string
	plain      map[string]string
	raw        map[string]string
	zeroCtx    map[string]string
	lineRanges map[string][]lineRange
}
```
This is stored in a map on `gitRunner`:
```go
func (g *gitRunner) forRange(base, head string) *rangeState {
```
The `gitRunner` itself might not be safe for concurrent use (documented for `RangeBuilder`), but if multiple `RangeBuilder`s or goroutines share a `gitRunner`, they could race. However, the `RangeBuilder` doc says it's not safe for concurrent use. But `gitRunner` might be used elsewhere concurrently? The original code likely had the same assumption.

Wait, looking at `BuildChangedLines` (package-level):
```go
func BuildChangedLines(ctx context.Context, repo, base, head string) (ChangedLines, error) {
	g := &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)}
```
It creates a throwaway `gitRunner`. So the old path was safe. The new `RangeBuilder` creates one `gitRunner` and uses it sequentially. The doc comment explicitly warns against concurrent use. This seems acceptable.

**Issue 5: `changedLines` method vs `BuildChangedLines` function contract**
In `grounding.go`:
```go
func BuildChangedLines(ctx context.Context, repo, base, head string) (ChangedLines, error) {
	g := &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)}
	if err := validateRange(g, base, head); err != nil {
		return nil, err
	}
	return g.changedLines(base, head)
}

func (g *gitRunner) changedLines(base, head string) (ChangedLines, error) {
	chunks, err := g.zeroCtxChunks(base, head)
```
The package-level `BuildChangedLines` now uses `g.changedLines`. The comment says "It is the runner-bound core shared by the standalone BuildChangedLines and RangeBuilder.BuildChangedLines".

Wait - there is a subtle contract issue. `BuildChangedLines` validates range, then calls `g.changedLines`. `RangeBuilder.BuildChangedLines` calls `b.validate()`, then `b.g.changedLines()`. Both validate before calling `changedLines`. Good.

But wait: `changedLines` calls `g.zeroCtxChunks(base, head)`, which calls `g.forRange(base, head)`. If the range hasn't been validated, `forRange` might still work (it just looks up/creates cache entry). But `zeroCtxChunks` calls `g.diffChunks(base, head, "--unified=0")` if cache miss. `diffChunks` might behave badly with invalid refs? But callers validate first.

**Issue 6: `rangeChunks` now uses `zeroCtxChunks`**
In `diff.go`:
```go
func (g *gitRunner) rangeChunks(base, head string) (map[string][]lineRange, error) {
	s := g.forRange(base, head)
	if s.lineRanges != nil {
		return s.lineRanges, nil
	}
	chunks, err := g.zeroCtxChunks(base, head)
```
Previously, `rangeChunks` called `g.diffChunks(base, head, "--unified=0")` directly. Now it calls `g.zeroCtxChunks(base, head)`. If `zeroCtxChunks` returns `s.zeroCtx` from cache, and `s.zeroCtx` was populated by a prior call, it works. But what if `zeroCtx` is populated but `lineRanges` is nil? Then it proceeds to parse. Good.

But what if `lineRanges` is nil and `zeroCtx` is also nil? `zeroCtxChunks` will call `diffChunks` and populate both. Good.

Wait, there's a potential issue: `zeroCtxChunks` returns `s.zeroCtx`, which is the raw chunks. `rangeChunks` then parses these into `lineRanges` and stores them. So subsequent calls to `zeroCtxChunks` return the raw chunks, and `rangeChunks` returns parsed ranges. This is fine.

**Issue 7: `zeroCtx` field is never cleaned up / memory**
Not an architecture issue per se.

**Issue 8: `internal/fanout/resume.go` - `computeGroundingData` signature change**
The diff shows:
```go
-	changed, groundingDisabledReason := computeGroundingData(ctx, req)
+	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
```
This is fine. But I don't see `computeGroundingData` definition in the diff. Since it's not in the changed regions (maybe in `review.go` which isn't in the diff?), I can't review it. Wait, maybe it IS in the diff but in a file not shown? The payload says 12 files. `review.go` is not among them. So I can't see the implementation of `computeGroundingData`. However, the call site in `resume.go` is changed.

But wait - in `PrepareResume`, `rb` is returned by `buildPayloads`. `buildPayloads` is presumably modified in `review.go`. Since `review.go` isn't in the diff, I cannot verify that `buildPayloads` actually returns a `RangeBuilder`. But the call site in `resume.go` compiles, so it must.

Wait, looking at the diff for `resume.go`, `buildPayloads` is called but its signature change is not visible. This is a problem: I can only review what I see. I see that `resume.go` now calls `buildPayloads` expecting 3 return values. This is a changed line in `resume.go`, so I can comment on it.

**Issue 9: `buildPayloads` now returns `*payload.RangeBuilder` - but is this returned value used correctly in the fresh path?**
The diff shows `PrepareResume` using it. But what about the fresh review path? That's in `review.go`, not shown. I cannot see if `review.go` properly disposes of or uses the `RangeBuilder`. But since `review.go` is not in the diff, I shouldn't flag it unless it's critical.

Wait, looking at the payload mode: "blocks". The payload shows `resume.go` but not `review.go`. The line numbers in `resume.go` are around 260-310.

**Issue 10: `RangeBuilder.BuildChangedLines` error handling contract**
The comment says: "the fail-open contract (a git error disables the grounding gate) lives at the fan-out caller."

But wait, looking at `RangeBuilder.BuildChangedLines`:
```go
func (b *RangeBuilder) BuildChangedLines() (ChangedLines, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b.g.changedLines(b.base, b.head)
}
```
If validation fails, it returns an error. The caller (`computeGroundingData`) presumably treats any error as "disable grounding". But what about the case where `b.validate()` fails? That means the range is invalid. But `PrepareResume` already validated the range earlier with `ValidateResumeRange(m, req.Range)`. And `buildPayloads` presumably validated it again. Then `rb.BuildChangedLines()` validates again? Wait, `b.validate()` is idempotent and cached. If `buildPayloads` already called it, it's latched. So no extra cost. But if `buildPayloads` didn't validate (maybe it constructs `RangeBuilder` but doesn't validate until `BuildEntries`), then `BuildChangedLines` validates. This is fine.

**Issue 11: `PrepareResume` rebuilds payloads from scratch instead of reusing persisted payloads**
This is pre-existing behavior: `PrepareResume` calls `buildPayloads` to rebuild payloads, even though completed agents already ran. The comment says "Payloads are rebuilt from the (validated-identical) recorded range so pending agents see exactly what the completed agents reviewed." This is unchanged. The change is that now it gets `rb` back and reuses it for grounding. This is fine.

**Issue 12: `RangeBuilder` is constructed in `buildPayloads` but also could be constructed independently**
The design introduces `RangeBuilder` as the primary API for reusing git processes. But `buildPayloads` (fanout level) wraps it. Is there a risk that `buildPayloads` doesn't expose `rb` in all call sites? In `resume.go` it does. But I can't see `review.go`.

Wait, I notice something in `internal/payload/rangebuilder.go`:
```go
// BuildEntries returns the per-file payload contributions for mode, reusing the
// builder's memoized range caches. Mirrors the package-level BuildEntries.
func (b *RangeBuilder) BuildEntries(mode PayloadMode) ([]FileEntry, error) {
	if mode != ModeDiff && mode != ModeBlocks && mode != ModeFiles {
		return nil, fmt.Errorf("unknown payload mode '%s': must be one of diff, blocks, files", mode)
	}
```
The error message mode list is hardcoded. If new modes are added, this string won't match. But that's minor.

**Issue 13: `zeroCtx` in `rangeState` is populated by `zeroCtxChunks` but what about `rangeChunks`?**
Wait, `rangeChunks` was previously calling `diffChunks(base, head, "--unified=0")`. Now it calls `zeroCtxChunks(base, head)`. `zeroCtxChunks` calls `diffChunks(base, head, "--unified=0")` and stores in `s.zeroCtx`. This is fine.

**Issue 14: `internal/payload/grounding.go` - `BuildChangedLines` standalone path now uses `changedLines` which uses `zeroCtxChunks`.**
The standalone `BuildChangedLines` creates a new `gitRunner`, validates, then calls `g.changedLines`. This means it now benefits from the `zeroCtxChunks` memoization within that single `gitRunner` instance. But since it's a throwaway `gitRunner`, the memoization only helps if `changedLines` is called multiple times on the same runner (which it isn't in the standalone path). However, the `zeroCtxChunks` method is still useful for the `RangeBuilder` reuse path. The standalone path behavior is unchanged (still creates a runner, validates, runs diff). Good.

**Issue 15: `changedLines` comment says "Callers must validate the range before the first cache-populating call"**
The comment is:
```go
// changedLines builds the grounding data from the memoized whole-range
// zero-context diff. It is the runner-bound core shared by the standalone
// BuildChangedLines and RangeBuilder.BuildChangedLines: when the range's payload
// was already built on the same gitRunner, the --unified=0 chunks (and the
// --name-status underlying them) are served from cache, so grounding adds no git
// subprocess. Callers must validate the range before the first cache-populating
// call (BuildChangedLines and RangeBuilder both do).
```
This is a weak contract. It relies on callers to validate. But `changedLines` is a method on `gitRunner`, which is unexported. Only internal package callers use it. Both `BuildChangedLines` and `RangeBuilder.BuildChangedLines` validate first. This is acceptable for internal API, but it's a "callee assumes caller validated" contract. If someone adds a new caller and forgets to validate, `diffChunks` might give a confusing error. However, this is pre-existing in the codebase's style.

**Issue 16: `buildEntriesValidated` comment**
In `builder.go`:
```go
// buildEntriesValidated is buildEntries without the range validation, for callers
// (RangeBuilder) that validate the range once and reuse the runner across
// multiple builds so the two rev-parse processes are not re-spent per mode.
```
This splits the validation from the work. Fine.

**Issue 17: `RangeBuilder` doesn't close / release resources?**
The `gitRunner` doesn't hold open file descriptors (it spawns subprocesses). No cleanup needed. Fine.

**Issue 18: `internal/fanout/resume.go` - `buildPayloads` returns `rb`, but what if `buildPayloads` encounters an error after partially constructing `rb`?**
In `resume.go`:
```go
	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
	if err != nil {
		return nil, nil, err
	}
```
If `buildPayloads` returns an error, `rb` might be nil or partially constructed. But the error check returns immediately, so `rb` is not used. Good.

**Issue 19: `computeGroundingData(ctx, req, rb)` - `rb` might be nil if `buildPayloads` returned nil on error, but error is checked. Fine.**

**Issue 20: `PrepareResume` passes `rb` to `computeGroundingData`, but `computeGroundingData` might also be called from the fresh path in `review.go` with a different signature?**
Not visible. But I can infer that `computeGroundingData` signature changed to accept `rb`. This is fine.

**Issue 21: `NewRangeBuilder` takes `ctx context.Context` but stores it in `gitRunner`**
Looking at `rangebuilder.go`:
```go
func NewRangeBuilder(ctx context.Context, repo, base, head string) *RangeBuilder {
	return &RangeBuilder{
		g:    &gitRunner{ctx: ctx, dir: repo, logger: log.FromContext(ctx)},
		base: base,
		head: head,
	}
}
```
The `ctx` is stored in `gitRunner.ctx`. This means the `RangeBuilder` is bound to a context. If the context is cancelled, subsequent git operations might fail. But `RangeBuilder` is short-lived (one review). Fine.

**Issue 22: `payload.BuildEntries` (package-level) still exists and still creates a new `gitRunner`**
Yes, the diff shows `BuildEntries` still exists in `builder.go`. It still calls `buildEntries` which validates and builds. So the old API is preserved. Good.

**Issue 23: Duplication of `validateRange` logic?**
`RangeBuilder` has its own `validate()` method. `BuildEntries` (package-level) calls `buildEntries` which calls `validateRange`. `BuildChangedLines` (package-level) calls `validateRange`. So validation is duplicated across entry points, but that's the cost of having both single-shot and reusable APIs. Acceptable.

**Issue 24: `TestRangeBuilder_ZeroContextDiffRunsOnce` tests `gitRunner` directly, not `RangeBuilder`**
The test creates a raw `gitRunner`, calls `rangeChunks`, then `changedLines`. This tests the underlying reuse, not the `RangeBuilder` wrapper. The test name says `TestRangeBuilder_ZeroContextDiffRunsOnce` but it doesn't use `RangeBuilder`. That's a naming/structural issue, but it's in tests.

Actually, let me re-read the test:
```go
func TestRangeBuilder_ZeroContextDiffRunsOnce(t *testing.T) {
	dir := initRepo(t)
	...
	g := &gitRunner{ctx: context.Background(), dir: dir}
	// First consumer: the range-split parse (files-mode line ranges).
	_, err := g.rangeChunks(base, head)
	...
	// Second consumer: grounding changed-text parse. Must reuse the memoized
	// zero-context chunks, adding no git process.
	_, err = g.changedLines(base, head)
	...
}
```
This test is named `TestRangeBuilder_*` but tests `gitRunner` methods directly. The comment says it tests that grounding reuses memoized chunks. While the behavior is important, the test doesn't actually exercise `RangeBuilder`. This is a test design issue: the test is coupled to `gitRunner` internals rather than the exported `RangeBuilder` API. If `RangeBuilder` logic changes but `gitRunner` stays the same, this test might pass even if `RangeBuilder` breaks the reuse. Conversely, it tests the wrong abstraction layer.

Wait, but `RangeBuilder` is just a thin wrapper. The real reuse logic is in `gitRunner`. Still, the test name implies it tests `RangeBuilder`. This is a LOW finding at best, but it's in tests (out of scope? No, tests are changed regions).

Actually, looking more carefully, is there any real architecture issue?

**Potential CRITICAL/HIGH issues:**

1. **Boundary violation / Coupling**: `internal/fanout/resume.go` now depends on `*payload.RangeBuilder`. But what is `buildPayloads`? If `buildPayloads` is in `internal/fanout`, it now returns a payload type. Was it already returning payload types (`[]payload.FileEntry`)? Probably yes. So adding `*payload.RangeBuilder` extends existing coupling. Not a new boundary violation.

2. **Contract issue in `RangeBuilder`**: The `BuildEntries` method checks for unknown mode, but the error message is hardcoded. If a new mode is added, this check and the `buildEntriesValidated` might get out of sync. However, `buildEntriesValidated` doesn't check mode. So `RangeBuilder.BuildEntries` is the only gate. But wait, what if someone calls `BuildEntries` (package-level) with a bogus mode? I can't see the original `BuildEntries` code. Not visible in diff.

3. **Race condition in `gitRunner` cache?** `rangeState` fields are maps. `gitRunner.forRange` returns a pointer to `rangeState`. Multiple goroutines could technically call methods on the same `gitRunner` if shared. But `RangeBuilder` doc says not concurrent-safe. `PrepareResume` uses it sequentially. However, in the fresh review path (not visible), if `review.go` launches multiple payload mode builds in parallel using the same `RangeBuilder`, that would be a race. But `buildPayloads` likely builds sequentially (or `RangeBuilder` is not used concurrently).

Actually, I recall that the payload builder likely builds entries for one mode at a time. The `RangeBuilder` comment says: "Callers use it sequentially (payload modes, then grounding)." So it's designed for sequential use.

4. **Memory leak / unbounded cache**: `gitRunner` caches `rangeState` in a map `g.cache` (presumably). The `zeroCtx` map adds more data per range. But this is pre-existing pattern.

5. **`changedLines` vs `rangeChunks` parse the same `zeroCtx` differently**:
- `rangeChunks` calls `parseHeadRanges(chunk)` which parses hunk headers.
- `changedLines` calls `parseFileChange(chunk)` which "parses one zero-context per-file diff chunk into its changed head ranges (from the @@ hunk headers, reusing parseHeadRanges) and the trimmed text of every added ('+') and removed ('-') line."

Wait, `parseFileChange` reuses `parseHeadRanges`. Both consume the same `chunk` string. `rangeChunks` stores parsed `lineRange` in cache. `changedLines` stores `FileChange` (which includes ranges and text). They both parse from raw chunk. But `rangeChunks` doesn't store the raw chunk text for line content; it only stores ranges. `changedLines` stores both. This is fine; they serve different purposes.

6. **`zeroCtxChunks` method name vs `rangeChunks`**: `zeroCtxChunks` is a new exported method (wait, it's lowercase `zeroCtxChunks`, so unexported). Fine.

**Let me look for hidden dependencies:**

In `internal/fanout/resume.go`, the `PrepareResume` function now has:
```go
	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
```
Then later:
```go
	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
```
But `computeGroundingData` is presumably modified to accept `*payload.RangeBuilder`. What if `computeGroundingData` is also called from `internal/fanout/review.go` in the fresh path? If so, the signature must be consistent. Since `resume.go` is changed and compiles, it's fine.

Wait - I see something. The function `buildPayloads` returns `rb`. But in `PrepareResume`, `payloads` is used to build slots. Then `rb` is passed to `computeGroundingData`. However, what about the case where `scopeConstraint` is read and applied to slots? That's unchanged.

**Is there any issue with the fact that `buildPayloads` is called in `PrepareResume` but the payloads are not persisted?** Pre-existing.

**Real issue: `computeGroundingData` signature change might break other callers?**
If there are other callers of `computeGroundingData` in unchanged files, they would break. But since the code compiles (presumably), they must all be updated. The diff only shows `resume.go`. If `review.go` was updated but not shown in diff, that's fine. But wait - the diff shows 12 files. `review.go` is not among them. How can the codebase compile if `computeGroundingData` signature changed but `review.go` (which likely calls it) wasn't updated?

Possibility 1: `review.go` was updated but is not in the diff because of payload mode? No, payload mode is "blocks", which shows all changed hunks. If `review.go` had changes, it would appear.

Possibility 2: `computeGroundingData` is defined in `resume.go` and only called there? Let me check... No, `computeGroundingData` is likely in `review.go`.

Possibility 3: The diff is from `292c5bb..748f0f6`. Maybe `review.go` changes are in the diff but I missed them? Let me re-read the file list.

Files in diff:
1. `.planning/epics/active/25.0_sarif_output_integration.md`
2. `.planning/epics/completed/22.4_grounding_gitrunner_reuse.md`
3. `.planning/epics/superseded/23.0_human_persona_renaming.md`
4. `.planning/product/presentation/index.html`
5. `.planning/product/presentation/style.css`
6. `CHANGELOG.md`
7. `internal/fanout/resume.go`
8. `internal/payload/builder.go`
9. `internal/payload/diff.go`
10. `internal/payload/grounding.go`
11. `internal/payload/rangebuilder.go`
12. `internal/payload/rangebuilder_test.go`

`internal/fanout/review.go` is definitely NOT in the diff. How is this possible?

Wait, let me look at `internal/fanout/resume.go` again. The function `PrepareResume` calls `buildPayloads` and `computeGroundingData`. These are likely defined in the same package (`internal/fanout`). If `review.go` is not changed, then `computeGroundingData` might not be defined in `review.go`. Let me check if it could be in `resume.go`... No, it's not visible in the diff of `resume.go`.

Actually, maybe `buildPayloads` is a function that was already returning `(payloads, rb, err)` in `review.go`, and `resume.go` is just updated to use the new return value. And `computeGroundingData` might have a new overload or the old one is still there? Or maybe `review.go` was changed in a previous commit not in this range? No, the diff range is `292c5bb..748f0f6`.

Wait, could `computeGroundingData` be in `grounding.go`? That's in `internal/payload`. No, the call is in `internal/fanout/resume.go` and doesn't have a package prefix, so it's same package.

This is confusing. But I must work with what I see. The fact that `review.go` isn't in the diff means either:
- `review.go` wasn't changed (unlikely if `computeGroundingData` signature changed)
- Or `computeGroundingData` is defined in a file that WAS changed but isn't named `review.go`
- Or there are overloads

Actually, looking at the epic plan, it says:
- `computeGroundingData` reuses the payload builder's memoized `rangeChunks`/`gitRunner`.

If `computeGroundingData` is in `internal/fanout/review.go`, and that file isn't in the diff, perhaps the user is showing me a selective diff? But the payload says "Reviewing 12 changed file(s)". So only these 12 changed.

Maybe `buildPayloads` was already returning a third value? Let me look at the `resume.go` diff more carefully:

```diff
-	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
+	payloads, rb, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
```
And:
```go
-	changed, groundingDisabledReason := computeGroundingData(ctx, req)
+	changed, groundingDisabledReason := computeGroundingData(ctx, req, rb)
```

So `buildPayloads` signature changed from 2 returns to 3. And `computeGroundingData` from 2 args to 3. If `review.go` contains these definitions and wasn't changed, the code couldn't compile unless there was some interface trickery. But maybe `review.go` was changed in the base commit and this diff is on top? No, the range is explicit.

Actually, I think the most likely explanation is that `review.go` IS changed but the diff generator omitted it for some reason? No, 12 files are listed.

Wait - could `buildPayloads` and `computeGroundingData` be in `internal/fanout/resume.go` itself? Let me check the diff of `resume.go`. It only shows `PrepareResume`, `filterPendingSlots`, and `readScopeConstraintArtifact`. So no.

Could they be in `internal/fanout/review.go` which was renamed? No.

I think I have to assume that `review.go` wasn't changed because the PR author only updated `resume.go` and the payload package, and somehow `buildPayloads` already had the signature or is variadic? Or maybe `buildPayloads` is in `review.go` and already returned `rb` but `resume.go` wasn't updated until now? That's possible: in a previous commit, `review.go` was updated to return `rb`, but `resume.go` still used the old signature and wouldn't compile? No, git diff wouldn't show a non-compiling intermediate state in a PR.

Actually, looking at the commit range `292c5bb2..748f0f60`, this is a range of commits. It's possible that `review.go` was changed in an earlier commit within this range, and then changed back or the changes are squashed in a way that net-zero? No, git diff shows net changes.

Given the constraint "Stay on the diff", I should only review lines that are in the changed regions of the files shown. I cannot see `review.go`, so I cannot verify the `buildPayloads` definition or `computeGroundingData` definition. However, I can see the call sites in `resume.go`.

But wait—if `review.go` is unchanged, then `buildPayloads` must be defined in a changed file or be unchanged. It's not in any changed file I can see. Therefore, it must be in an unchanged file. That means its signature is unchanged? But the call site in `resume.go` clearly shows `payloads, rb, err := buildPayloads(...)`. This would be a compile error unless `buildPayloads` returns 3 values.

This is a paradox. The only resolution is that `review.go` is changed but not listed in the 12 files? Let me count the diff headers again.

Diff headers:
1. `diff --git a/.planning/epics/active/25.0_sarif_output_integration.md b/.planning/epics/active/25.0_sarif_output_integration.md`
2. `diff