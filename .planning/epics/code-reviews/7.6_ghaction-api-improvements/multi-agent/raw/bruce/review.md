<think>
Let me analyze this diff carefully against the sprint plan scope.

The sprint plan focuses on:
1. Item 1: Per-comment fallback for batch review POST (in `cmd/atcr/github.go` and tests)
2. Item 2: `BuildCheckOutput` 3-tuple return (in `internal/ghaction/render.go` and tests)

Let me trace through the changes:

**File: `cmd/atcr/github.go`**
1. `runGithub`: Changed from calling `Conclusion` then `BuildCheckOutput` to calling `BuildCheckOutput` which now returns the 3-tuple. ✓ This matches Item 2.
2. `postInlineComments`: Added 404/405 fallback to `postCommentsIndividually`. ✓ This matches Item 1.
3. New `postCommentsIndividually` function: Posts comments individually with 422 skip. ✓ This matches Item 1.

**File: `cmd/atcr/github_test.go`**
- Three new tests covering the fallback behavior. ✓ Matches Item 1.

**File: `internal/ghaction/render.go`**
- `BuildCheckOutput` now returns `(CheckOutput, string, int)`. ✓ Matches Item 2.

**File: `internal/ghaction/render_test.go`**
- Tests updated for new return signature. ✓ Matches Item 2.

Now let me look for issues in the in-scope changes:

### Issue 1: `postCommentsIndividually` - No rate-limit delay between calls

The original sequential posting (described in the sprint plan as the "100ms delay between calls") had a deliberate 100ms delay for rate-limit mitigation. The new `postCommentsIndividually` fallback has NO delay between calls. This is a regression for GitHub Enterprise users who hit this fallback path.

Actually, looking more carefully, the sprint plan says:
> "Out of Scope: Rate-limit retry logic (separate concern from batching)"

But the 100ms delay wasn't retry logic — it was rate-limit mitigation. Hmm. However, the sprint plan explicitly says rate-limit retry is out of scope. So this might be acceptable. But it's worth flagging as a correctness concern.

Actually, looking at the plan more carefully:
> "100ms inter-call delay was added for rate-limit mitigation but doesn't address the fundamental O(N) API call count"

And:
> "OUT of scope: ... rate-limit retry logic"

The rate-limit delay is implicitly out of scope by the explicit exclusion. But it's still a potential issue worth noting since the fallback could hammer the API. I'll flag this.

### Issue 2: `postCommentsIndividually` - Missing context check

The function uses `cmd.Context()` which is fine, but there's no consideration for context cancellation between calls. Not a major issue.

### Issue 3: `postCommentsIndividually` - 422 in fallback increments `skipped` but the return semantics

Looking at the function:
- It returns `posted, deduped, nil` — but `skipped` is NOT in the return. The "skipped" count from per-comment 422s is only logged, not surfaced to the caller.
- The caller (`postInlineComments`) returns `0, deduped, nil` on batch 422.
- After fallback, the caller gets `(posted, deduped, nil)` but the per-comment 422 count is lost.
- This is an inconsistency: the batch 422 path returns `0` for `posted` and logs a warning; the fallback 422 path logs a warning and returns the actual `posted` count (excluding skipped).

Wait, actually looking more carefully:
- Batch 422: `return 0, deduped, nil` — returns 0 for posted
- Fallback path: returns `posted, deduped, nil` where `posted` is the count of successfully posted comments (not counting skipped)

So the semantics differ: in the batch path, if there were 5 comments and all 422'd, `posted = 0`. In the fallback path, if 3 succeeded and 2 422'd, `posted = 3`. This is actually correct behavior, but the test `TestPostInlineComments_FallbackPerComment422IsSkipped` expects `posted = 0` when all 422, which matches.

This seems consistent. Let me move on.

### Issue 4: `BuildCheckOutput` empty findings with `failOn = ""` 

Looking at the early-return branch:
```go
if total == 0 {
    conclusion, failCount := Conclusion(findings, failOn)
    return CheckOutput{...}, conclusion, failCount
}
```

And then later:
```go
conclusion, failCount := Conclusion(findings, failOn)
```

When `failOn = ""`, `Conclusion` likely returns `(neutral, 0)`. The test covers this. But wait — looking at the code, the original `BuildCheckOutput` didn't compute `Conclusion` in the empty-findings branch at all. Now it does. This means we're calling `Conclusion` for empty findings. Is that wasteful?

Looking at the original code, the empty-findings early return was a fast path. Now we're calling `Conclusion(nil, failOn)` which presumably is O(1) for empty input. This is probably fine — it's the same complexity as the non-empty path's first computation.

Actually wait — I need to verify that `Conclusion(nil, failOn)` is well-defined. If `failOn` is some invalid threshold, does `Conclusion` handle it gracefully? This depends on `Conclusion`'s implementation, which isn't shown in the diff. The test `TestBuildCheckOutputReturnsConclusionAndFailCount/empty_findings_without_threshold_is_neutral` confirms it works for empty string.

### Issue 5: Missing context cancellation in fallback loop

`postCommentsIndividually` loops over comments without checking context cancellation between iterations. If there are many comments and the context is cancelled, the loop will continue making HTTP calls until completion. This is a minor issue.

Actually, this is a common pattern in Go and isn't necessarily wrong. Let me not flag it.

### Issue 6: The `BuildCheckOutput` function now calls `Conclusion` twice in the empty-findings branch? No, just once.

Looking at the diff again:
```go
if total == 0 {
    conclusion, failCount := Conclusion(findings, failOn)  // called once
    return CheckOutput{...}, conclusion, failCount
}
conclusion, failCount := Conclusion(findings, failOn)  // called once (non-empty path)
```

So `Conclusion` is still called once per `BuildCheckOutput` call. But the original code didn't call `Conclusion` in the empty branch at all! Now we do. This is correct (we need to return the values), but it adds a call.

### Issue 7: Are all callers of `BuildCheckOutput` updated?

Let me search the diff for `BuildCheckOutput` callers... The diff only shows updates to:
- `cmd/atcr/github.go` — `runGithub`
- `internal/ghaction/render_test.go` — tests

Are there other callers? The sprint plan says: "`go build ./...` will catch missing return-value assignments" as mitigation. So this is presumably handled by the compiler. If there were other callers, they would fail to compile.

This isn't a bug in the diff per se, but the AC3 says "All existing `internal/ghaction` tests pass" and AC4 says "All existing `cmd/atcr` integration tests pass". The tests in the diff look updated, so this seems fine.

### Issue 8: Dedup count loss in 404/405 fallback

In the fallback path:
```go
case 404, 405:
    return postCommentsIndividually(cmd, client, owner, repo, pr, sha, comments, deduped)
```

`postCommentsIndividually` returns `(posted, deduped, nil)`. The `deduped` is passed through. ✓

### Issue 9: What if the batch endpoint returns a different error like 500?

Looking at the switch:
```go
switch apiErr.StatusCode {
case 422: ... return 0, deduped, nil
case 404, 405: ... fallback
}
// falls through to:
return 0, deduped, &codedError{code: exitFailure, err: ...}
```

For 500, it falls through to exitFailure. This is correct per the plan.

### Issue 10: What if `errors.As` fails (not an APIError)?

```go
var apiErr *ghaction.APIError
if errors.As(err, &apiErr) {
    switch apiErr.StatusCode {
    case 422: ...
    case 404, 405: ...
    }
}
return 0, deduped, &codedError{...}
```

If `errors.As` returns false (not an APIError), the code falls through to the exitFailure path. ✓ Correct.

### Issue 11: `postCommentsIndividually` doesn't handle the case where `client.CreateReviewComment` returns a non-APIError error

```go
for _, c := range comments {
    if err := client.CreateReviewComment(...); err != nil {
        var apiErr *ghaction.APIError
        if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {
            skipped++
            continue
        }
        return posted, deduped, &codedError{...}
    }
    posted++
}
```

If `err` is a network error (not APIError), it falls through to the error return. ✓ Correct.

### Issue 12: `deduped` parameter naming in `postCommentsIndividually`

The parameter is called `deduped` but it represents "comments already present from previous runs". Looking at the original code:
```go
deduped := len(comments) - len(filtered)
```

So `deduped` is the count of comments skipped because they were already on the PR. The variable name is fine.

### Issue 13: Test isolation — `srv` has no mutex for the GET /comments request

In `TestPostInlineComments_FallsBackToPerCommentOnUnsupportedBatch`:
```go
var mu sync.Mutex
paths := map[string]int{}
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    mu.Lock()
    paths[r.Method+" "+r.URL.Path]++
    mu.Unlock()
    ...
```

The mutex is properly used. ✓

But wait — the test asserts `paths["POST /repos/owner/repo/pulls/1/reviews"]` equals 1. With a single batch attempt and 2 individual posts, that's correct.

### Issue 14: The `deduped` variable might be wrong in `postCommentsIndividually`

Let me trace through more carefully. `postInlineComments` calls `deduplicateComments` to get the filtered comments and the `deduped` count. Then it calls `CreatePRReview` with the filtered comments. If that fails with 404/405, it falls back with the FILTERED comments. But `CreateReviewComment` is called per-comment on each of the filtered comments. The `deduped` count represents how many comments were already present (i.e., NOT in the filtered slice). So when we fall back, the `deduped` is preserved through the fallback path. ✓ Correct.

### Issue 15: Potential issue — the warning message in the fallback path is printed to stderr, but the original code also prints warnings. Let me verify consistency.

Original 422 warning:
```go
_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d inline comment(s) could not be posted (HTTP 422 — comments may be off-diff): %v\n", len(comments), apiErr)
```

New 404/405 warning:
```go
_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: batched review endpoint unavailable (HTTP %d); falling back to per-comment posting\n", apiErr.StatusCode)
```

These are consistent in style. ✓

### Issue 16: Test `TestPostInlineComments_FallbackPerComment422IsSkipped` asserts `out` doesn't contain "posted X" message

Wait, let me look at this test again:
```go
posted, deduped, err := postInlineComments(cmd, client, "owner", "repo", 1, "sha", findings)
require.NoError(t, err)
assert.Equal(t, 0, posted)
assert.Equal(t, 0, deduped)
assert.Contains(t, stderr.String(), "422")
```

The stderr contains "422" from either:
1. The "warning: batched review endpoint unavailable (HTTP 404)" message (contains "404", not "422")
2. The "warning: %d inline comment(s) skipped (HTTP 422 — off-diff)" message (contains "422")

So the "422" check passes because of the second message. ✓

### Issue 17: Error wrapping in `postCommentsIndividually`

```go
return posted, deduped, &codedError{code: exitFailure, err: fmt.Errorf("inline comment fallback failed after %d posted: %w", posted, err)}
```

The error message includes `posted` count, which is useful. ✓

### Issue 18: Looking at the empty-findings branch in `BuildCheckOutput`

The original code:
```go
if total == 0 {
    return CheckOutput{
        Title:   "atcr — no findings",
        Summary: "atcr review found no findings.",
        Text:    "ATCR review completed with no findings.",
    }
}
```

The new code calls `Conclusion(findings, failOn)` in the empty branch. But `Conclusion(nil, "")` returns `("neutral", 0)`. Is calling `Conclusion` for an empty slice expensive? Looking at the function (not in diff), it's likely O(1) for empty input. This is fine.

### Issue 19: A subtle issue — the `BuildCheckOutput` summary for empty findings

Original:
- Title: "atcr — no findings"
- Summary: "atcr review found no findings."

If `failOn = "HIGH"` and no findings, `Conclusion` returns `("success", 0)`. The new code uses this. But the Summary still says "atcr review found no findings." regardless. So a reader sees "no findings" but the conclusion might be "neutral" (if failOn is empty). This is consistent with the original behavior. ✓

### Issue 20: Let me look for any race conditions or concurrency issues.

The `paths` map in the test uses a mutex. ✓

### Issue 21: `postCommentsIndividually` — what if `len(comments) == 0`?

The caller (`postInlineComments`) already checks `if len(comments) == 0 { return 0, 0, nil }` before calling `CreatePRReview`. So if we reach the fallback, there's at least one comment. The fallback loop would iterate 0 times if called with empty comments. The success message would print "posted 0 inline comment(s)... (0 already present)". Not great UX but not a bug.

Actually wait — could `len(comments) == 0` reach the fallback? No, because the caller returns early before `CreatePRReview`. ✓

### Issue 22: Could `postCommentsIndividually` be called from elsewhere?

It's a new function, so only one caller. But it's exported via lowercase (package-private). If it should be exported for testing, the test would need to be in the same package. Looking at `cmd/atcr/github_test.go`, it's the same package. ✓

### Issue 23: Test variable `out` is unused in some assertions

In `TestPostInlineComments_FallbackPerComment422IsSkipped`:
```go
var out, stderr bytes.Buffer
cmd.SetOut(&out)
```

`out` is assigned but not asserted on. This is a minor issue — dead variable. Not a bug.

### Issue 24: Looking for subtle issues in the test code

The tests look well-structured. Let me check for any edge cases that might fail:

In `TestPostInlineComments_FallsBackToPerCommentOnUnsupportedBatch`:
- The test sets up a mock server that:
  - GETs /comments returns `[]`
  - POST /reviews returns the specified status code
  - Default (POST /comments) returns 201

- This should work. The fallback calls `CreateReviewComment` which posts to `/comments`. ✓

Wait, what does `CreateReviewComment` do? It probably posts to `POST /repos/{owner}/{repo}/pulls/{pull_number}/comments`. The mock server's default case returns 201 for any non-handled path. ✓

### Issue 25: Looking at the test for hard error propagation

In `TestPostInlineComments_FallbackPerCommentHardErrorPropagates`:
- GET /comments returns `[]`
- POST /reviews returns 404
- Default (POST /comments) returns 403

The fallback will call `CreateReviewComment` which gets a 403. `errors.As(err, &apiErr)` returns true, `apiErr.StatusCode == 422` is false. Falls through to the error return. ✓

### Issue 26: Performance concern in fallback path

The fallback path makes N sequential HTTP calls with no delay. For a PR with 100 findings, this could be slow and might hit rate limits. The original code had a 100ms delay. The sprint plan says rate-limit is out of scope, but this is still a concern for users on GitHub Enterprise hitting this fallback.

I'll flag this as MEDIUM.

### Issue 27: What about the `deduped` being added to the printed message in fallback?

```go
_, _ = fmt.Fprintf(cmd.OutOrStdout(), "posted %d inline comment(s) to %s/%s#%d via per-comment fallback (%d already present)\n", posted, owner, repo, pr, deduped)
```

This is fine — provides useful info to the user.

### Summary of potential issues:

1. **MEDIUM**: No rate-limit delay in `postCommentsIndividually` fallback — could hammer the API on GitHub Enterprise. The original 100ms delay is lost.

2. **LOW**: Variable `out` is unused in `TestPostInlineComments_FallbackPerComment422IsSkipped` — minor dead code.

3. **LOW**: `deduped` parameter shadowing — no, it's used. ✓

4. **POTENTIAL**: `BuildCheckOutput` now calls `Conclusion` in the empty-findings branch, which previously didn't. If `Conclusion` has side effects or is expensive for empty input, this could be an issue. But it's a function call, not a side effect. ✓

5. **POTENTIAL**: The `postInlineComments` function's logic for handling `errors.As` not matching — if the error is wrapped deeply, `errors.As` should still find the `*ghaction.APIError`. ✓

Let me also check if there's a specific concern about the fallback's error wrapping. If the batch fails with 404, the fallback might also fail (e.g., because the GHE doesn't support ANY of these endpoints). The fallback wraps the error correctly. ✓

### Issue 28: Let me look at whether `Conclusion` is exported and if `BuildCheckOutput` callers need to update other places.

The sprint plan mentions:
> Update `BuildCheckOutput`'s callers and tests accordingly

In the diff:
- `cmd/atcr/github.go` is updated ✓
- `internal/ghaction/render_test.go` is updated ✓

What about `cmd/atcr/github_test.go`? Are there callers there? Let me search the diff... No, github_test.go doesn't call `BuildCheckOutput`. ✓

What about other callers? The diff doesn't show changes to any other files, so either:
1. There are no other callers (compiler-checked by `go build ./...`)
2. There are other callers that weren't updated

The sprint plan says "`go build ./...` will catch missing return-value assignments" as mitigation. This suggests the team relies on the compiler. If there were other callers, the build would fail. So this is fine.

### Issue 29: The `BuildCheckOutput` function signature change is breaking

This is a public API change. The function is in `internal/ghaction` (internal package), so external callers can't access it. Within the module, all callers are updated. ✓

### Issue 30: Does the test file `cmd/atcr/github_test.go` need to be updated for the `runGithub` change?

The diff doesn't show changes to `runGithub` tests in `cmd/atcr/github_test.go`. Were there any tests for `runGithub` that check `conclusion` or `failCount`? If so, they might need updates.

But since the diff doesn't include such updates, either:
1. There are no such tests
2. The tests don't check those specific values

This is acceptable.

### Issue 31: Let me check for any issues with the `BuildCheckOutput` summary text in the empty branch.

The empty branch summary is "atcr review found no findings." This is fine.

### Issue 32: The `BuildCheckOutput` summary is computed in the non-empty branch. If there are many findings, the summary might change. This isn't a regression — it was the same before. ✓

### Issue 33: Looking at the order of returns in `BuildCheckOutput`

The return type is `(CheckOutput, string, int)`. The caller unpacks:
```go
output, conclusion, failCount := ghaction.BuildCheckOutput(findings, failOn)
```

✓ Correct.

### Issue 34: Let me verify the test for empty findings with threshold

```go
t.Run("empty findings with threshold reports success and zero", func(t *testing.T) {
    _, conclusion, failCount := BuildCheckOutput(nil, "HIGH")
    wantC, wantN := Conclusion(nil, "HIGH")
    assert.Equal(t, wantC, conclusion)
    assert.Equal(t, wantN, failCount)
    assert.Equal(t, ConclusionSuccess, conclusion)
    assert.Equal(t, 0, failCount)
})
```

This tests that `BuildCheckOutput` returns the same values as `Conclusion` for empty findings with a threshold. ✓

### Issue 35: Is there a risk that `Conclusion` changes its behavior for empty findings in the future?

If `Conclusion` is updated to return different values for empty findings, `BuildCheckOutput` would automatically reflect that change. This is the intended behavior — they're now consistent. ✓

### Issue 36: Looking at the summary text in the empty branch

The summary says "atcr review found no findings." regardless of failOn. If failOn is empty, the conclusion is neutral. Should the summary mention "no findings to evaluate"? No, "no findings" is clear enough. ✓

OK, I think the main issues are:
1. **MEDIUM**: No rate-limit delay in fallback path
2. **LOW**: Unused variable in test

Let me also think about whether the `deduped` parameter being named the same in both `postInlineComments` and `postCommentsIndividually` could cause confusion. The naming is consistent. ✓

One more thing — the `skipped` count in `postCommentsIndividually` is only logged, not returned. The original batch 422 path doesn't return this either. So the behavior is consistent. ✓

Actually wait — in the batch 422 path, ALL comments are skipped (the entire batch). So `posted = 0` and there's nothing more to report. In the fallback path, comments are skipped individually. The total count of skipped comments is logged but not returned. This is consistent with the batch path (where all skipped = no count returned). ✓

Let me also verify the fallback handles the edge case where the batch endpoint returns 200 but the response indicates an error. Looking at the code:
```go
if err != nil {
    var apiErr *ghaction.APIError
    if errors.As(err, &apiErr) { ... }
    return 0, deduped, &codedError{...}
}
```

If the batch endpoint returns 200 with an error in the body, `CreatePRReview` might or might not return an error. This depends on the implementation of `CreatePRReview`. If it returns nil for 200 responses, the code assumes success. If it returns an error for invalid response bodies, the code handles it.

This is out of scope for this review (we're not looking at `CreatePRReview` itself).

### Final review

Let me compile my findings:

1. **MEDIUM**: `postCommentsIndividually` has no rate-limit delay between sequential calls, which could trigger GitHub API rate limits for users on older GitHub Enterprise. The original 100ms delay was removed but is needed for the fallback path.

2. **LOW**: Unused `out` variable in `TestPostInlineComments_FallbackPerComment422IsSkipped`.

Wait, actually I should double-check if this is truly a regression or just a design decision. Looking at the sprint plan:

> "Item 1's batch review POST is ALREADY implemented"
> "The one remaining piece of Item 1 is the **per-comment fallback**"

So the original "sequential loop with 100ms delay" was REPLACED by the batch POST. The fallback is NEW code, not a reversion. So the original 100ms delay isn't relevant to the fallback — the fallback is a NEW path.

But the fallback DOES need rate-limit mitigation because it's posting N comments sequentially. This is a real concern.

However, the sprint plan says:
> "Out of Scope: Rate-limit retry logic (separate concern from batching)"

So this might be intentionally out of scope. I should still flag it because it's a correctness/reliability issue that could affect production behavior.

Let me think about whether to flag this. The sprint plan explicitly says rate-limit is out of scope. So flagging it might be considered out-of-scope review. But the issue is real and could cause user-facing problems.

I'll flag it as MEDIUM with a note that it's a potential concern.

Actually, let me reconsider. The sprint plan's scope constraint says:

> "SCOPE CONSTRAINT (from sprint-plan.md):"
> "The following sprint plan defines what is IN SCOPE for this review."

The fallback IS in scope. The rate-limit concern IS related to the fallback implementation. So flagging it is within scope, even if the FIX would be out of scope for this sprint.

I'll flag it.

Actually, looking at this more carefully — the sprint plan says:
> "Retain per-comment fallback if batch fails (some GitHub Enterprise versions may not support the endpoint)"

And:
> "Mitigation: Implement with per-comment fallback; detect 404/405 from batch call"

The fallback is a key part of the implementation. Rate-limit mitigation is a known concern but explicitly out of scope. So flagging it as a finding is appropriate — it raises awareness without trying to fix it in this sprint.

Let me finalize my findings.

Actually, one more issue — let me look at the `BuildCheckOutput` early-return branch:

```go
if total == 0 {
    conclusion, failCount := Conclusion(findings, failOn)
    return CheckOutput{
        Title:   "atcr — no findings",
        Summary: "atcr review found no findings.",
        Text:    "ATCR review completed with no findings.",
    }, conclusion, failCount
}
```

The comment says: "Derive the verdict from Conclusion so this early-return branch reports the same conclusion (neutral when informational, success under any threshold) as the non-empty path."

This is correct logic. ✓

But wait — what about the `Summary` text? It says "atcr review found no findings." regardless of the conclusion. If the conclusion is `neutral` (no threshold), the summary should probably indicate that no gate was applied. If the conclusion is `success` (threshold set), the summary makes sense.

This is a minor UX concern but not a bug. Not flagging.

OK, let me also check one more thing — is the `BuildCheckOutput` function thread-safe? It doesn't modify any global state, so yes. ✓

Let me also check: does `Conclusion` have side effects? Probably not — it's a pure function. ✓

### Final findings:

1. **MEDIUM**: No rate-limit delay in `postCommentsIndividually` — could trigger API rate limits on GitHub Enterprise for PRs with many findings.

2. **LOW**: Unused `out` variable in `TestPostInlineComments_FallbackPerComment422IsSkipped`.

Hmm, let me look more carefully for issues. I want to make sure I'm not missing anything.

Looking at the `postInlineComments` function flow:
1. Build comments from findings
2. If no comments, return early
3. Dedup against existing comments
4. If no comments left after dedup, return early
5. Call `CreatePRReview` with filtered comments
6. If error and 422, warn and return 0
7. If error and 404/405, fall back to individual posting
8. Otherwise, propagate error
9. Return success

Wait — there's an issue! In step 3, after dedup, we might have 0 comments to post. The code says:
```go
if len(comments) == 0 {
    return 0, deduped, nil
}
```

But looking at the diff, I don't see this check explicitly. Let me re-read:

```go
func postInlineComments(cmd *cobra.Command, client *ghaction.Client, owner, repo string, pr int, sha string, findings []reconcile.JSONFinding) (int, int, error) {
    comments := ghaction.BuildInlineComments(findings)
    if len(comments) == 0 {
        return 0, 0, nil
    }
    // ...
    deduped := len(comments) - len(filtered)
    if len(filtered) == 0 {
        return 0, deduped, nil  // all already present
    }
    // ...
}
```

I don't have the full context. Let me check the diff again... The diff only shows the parts that changed. The full function structure is:
- `if len(comments) == 0 { return 0, 0, nil }` (early return if no comments to post)
- ... (dedup logic, not shown in diff)
- `err := client.CreatePRReview(...)` (changed)
- error handling (changed)
- return success (changed)

OK so the structure is intact. ✓

Let me look at one more potential issue. In `postCommentsIndividually`, the function calls `client.CreateReviewComment(cmd.Context(), ...)`. This is fine, but let me verify that `cmd.Context()` is the right way to get the context. Looking at cobra documentation, `cmd.Context()` is the standard way. ✓

OK I think my findings are:
1. MEDIUM: Rate-limit concern in fallback path
2. LOW: Unused variable in test

Let me also look at whether the `deduped` parameter passed to `postCommentsIndividually` is correct.

In `postInlineComments`:
```go
return postCommentsIndividually(cmd, client, owner, repo, pr, sha, comments, deduped)
```

`comments` is the filtered slice (already deduped), and `deduped` is the count of comments that were removed during dedup.

In `postCommentsIndividually`:
```go
return posted, deduped, nil
```

The caller gets back `(posted, deduped, nil)`. The `deduped` is passed through unchanged. ✓

One more thing — in `postCommentsIndividually`, the variable `skipped` is used but not returned. Is this an issue? Looking at the code, `skipped` represents the count of comments that were 422'd (off-diff). This is logged but not returned. The caller already logs this via the warning message. So it's consistent.

Actually wait — the caller in `postInlineComments` doesn't know how many were skipped. But that's OK because:
1. The fallback function logs the skipped count
2. The caller doesn't use the skipped count for anything

So this is fine.

Let me think about whether there's a subtle bug in the `errors.As` check for the fallback's per-comment 422 handling.

```go
if err := client.CreateReviewComment(...); err != nil {
    var apiErr *ghaction.APIError
    if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {
        skipped++
        continue
    }
    return posted, deduped, &codedError{...}
}
```

If `err` is not an `APIError`, it falls through to the error return. ✓

If `err` is an `APIError` with a different status code, it falls through to the error return. ✓

If `err` is an `APIError` with 422, it's skipped. ✓

This is correct.

### One more check: thread safety of the test setup

The test creates a mock server with a mutex. The mutex protects the `paths` map. The `srv.Client()` is the standard HTTP client. This is thread-safe. ✓

### Final list of findings:

I'll output:
1. MEDIUM: No rate-limit delay in `postCommentsIndividually` fallback
2. LOW: Unused `out` variable in `TestPostInlineComments_FallbackPerComment422IsSkipped`

Wait, actually, I want to look at the diff one more time to see if there's anything I missed.

Looking at `internal/ghaction/render.go`:
```go
func BuildCheckOutput(findings []reconcile.JSONFinding, failOn string) (CheckOutput, string, int) {
    total := len(findings)
    if total == 0 {
        conclusion, failCount := Conclusion(findings, failOn)
        return CheckOutput{
            Title:   "atcr — no findings",
            Summary: "atcr review found no findings.",
            Text:    "ATCR review completed with no findings.",
        }, conclusion, failCount
    }

    conclusion, failCount := Conclusion(findings, failOn)
    // ...
}
```

This is a breaking change. The function signature is now `(CheckOutput, string, int)` instead of `CheckOutput`. All callers must be updated.

The diff shows updates to:
- `cmd/atcr/github.go` (the main caller)
- `internal/ghaction/render_test.go` (tests)

Are there any other callers? The sprint plan mentions "Update `BuildCheckOutput`'s callers and tests accordingly" as part of Item 2. The mitigation is "`go build ./...` will catch missing return-value assignments". So the team relies on the compiler to catch any missed callers. If there are missed callers, the build would fail.

This is an acceptable risk given the team's mitigation. Not flagging.

### Looking at the comment in the empty branch

```go
if total == 0 {
    // No findings: the gate trivially passes. Derive the verdict from
    // Conclusion so this early-return branch reports the same conclusion
    // (neutral when informational, success under any threshold) as the
    // non-empty path.
    conclusion, failCount := Conclusion(findings, failOn)
    ...
}
```

The comment says "neutral when informational" — this implies that when failOn is empty (informational mode), the conclusion is neutral. Let me verify with the test:

```go
t.Run("empty findings without threshold is neutral", func(t *testing.T) {
    _, conclusion, failCount := BuildCheckOutput(nil, "")
    assert.Equal(t, ConclusionNeutral, conclusion)
    assert.Equal(t, 0, failCount)
})
```

✓ Matches the comment.

And "success under any threshold" — tested by:
```go
t.Run("empty findings with threshold reports success and zero", func(t *testing.T) {
    _, conclusion, failCount := BuildCheckOutput(nil, "HIGH")
    assert.Equal(t, ConclusionSuccess, conclusion)
    assert.Equal(t, 0, failCount)
})
```

✓ Matches.

OK, I think my findings are solid. Let me write them up.

Actually, let me reconsider the rate-limit issue. The fallback path is called only when the batch endpoint is unavailable (404/405). This is a relatively rare case (older GHE). For a typical PR with a handful of findings (say 5-20), the fallback would make 5-20 API calls without delay. GitHub's rate limit is typically 5000 requests per hour. So unless there are many findings, this shouldn't be an issue.

However, for a PR with 100+ findings (large refactor or sweep), this could trigger secondary rate limits. The original 100ms delay was specifically added for this reason.

I'll flag this as MEDIUM because it's a real concern but not catastrophic for typical use cases.

Let me also think about whether the `deduped` is correctly threaded through the fallback. In `postInlineComments`:
```go
return postCommentsIndividually(cmd, client, owner, repo, pr, sha, comments, deduped)
```

`comments` is the FILTERED slice (after dedup). `deduped` is the count of removed comments.

In `postCommentsIndividually`:
```go
posted, deduped int
```

The function receives `deduped` and passes it through to the return value. ✓

So the total returned count is `posted + deduped`, which represents "successfully posted this run + already present before this run". The caller can use this for reporting. ✓

Actually, let me verify by looking at how `postInlineComments` is called in `runGithub`:

I don't have that context from the diff. But based on the function signature, it returns `(int, int, error)` = `(posted, deduped, err)`. The caller likely logs both counts. ✓

### One final check — `postCommentsIndividually` doesn't pass `commitID` correctly

Looking at the function signature:
```go
func postCommentsIndividually(cmd *cobra.Command, client *ghaction.Client, owner, repo string, pr int, sha string, comments []ghaction.CommentRequest, deduped int) (int, int, error) {
```

And the call:
```go
if err := client.CreateReviewComment(cmd.Context(), owner, repo, pr, sha, c); err != nil {
```

`sha` is passed correctly. ✓

### OK, finalizing

My findings:
1. **MEDIUM**: No rate-limit delay in `postCommentsIndividually` fallback — could trigger GitHub API rate limits for users on older GHE hitting this path with many findings.
2. **LOW**: Unused `out` variable in `TestPostInlineComments_FallbackPerComment422IsSkipped`.

Wait, let me look once more at the unused variable. The test has:
```go
var out, stderr bytes.Buffer
cmd.SetOut(&out)
cmd.SetErr(&stderr)
```

`out` is assigned to `cmd.SetOut(&out)` but never asserted on. This is dead code that could be removed. However, it's not a bug — just unused. The variable is needed to construct the cobra command's output destination. But the test doesn't read it. So it's truly unused.

Hmm, this is a minor issue. Let me decide whether to flag it.

Actually, `cmd.SetOut(&out)` is needed to set up the command's output. Without it, `cmd.OutOrStdout()` would panic or return os.Stdout. So the variable is needed for setup, but the test doesn't verify its contents. This is a minor cleanliness issue.

I'll flag it as LOW.

Actually, I realize I should be more careful. Let me re-read the test:

```go
func TestPostInlineComments_FallbackPerComment422IsSkipped(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ...
    }))
    defer srv.Close()

    cmd := &cobra.Command{}
    var out, stderr bytes.Buffer
    cmd.SetOut(&out)
    cmd.SetErr(&stderr)
    cmd.SetContext(context.Background())

    client := &ghaction.Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
    findings := []reconcile.JSONFinding{
        {File: "a.go", Line: 1, Problem: "p1", Fix: "f1"},
        {File: "b.go", Line: 2, Problem: "p2", Fix: "f2"},
    }

    posted, deduped, err := postInlineComments(cmd, client, "owner", "repo", 1, "sha", findings)
    require.NoError(t, err, "per-comment 422 during fallback is a non-fatal off-diff skip")
    assert.Equal(t, 0, posted, "both off-diff comments skipped")
    assert.Equal(t, 0, deduped)
    assert.Contains(t, stderr.String(), "422")
}
```

`out` is set but never read. The test asserts on `stderr.String()` (which checks for the "422" warning). The `out` buffer would contain the "posted 0 inline comment(s)..." message but it's not checked.

This is a minor test quality issue. The test could verify the stdout message too. But it's not a bug — just incomplete coverage.

Hmm, I'll flag it as LOW for test completeness.

Actually, on reflection, this is really minor and might be intentional (e.g., the stdout message is implementation detail). Let me not flag it as it doesn't really impact correctness.

Wait, let me check if there's any issue I might be missing related to the `errors.As` check in `postInlineComments`. If `CreatePRReview` returns an error that wraps `*ghaction.APIError`, then `errors.As(err, &apiErr)` should find it. ✓

But what if the error chain is broken (e.g., the error is converted to a string)? Then `errors.As` would fail and the code falls through to the error return. This is the correct behavior. ✓

OK, I'm done. Let me write up the findings.

Actually, let me look at one more thing. In the test `TestPostInlineComments_FallsBackToPerCommentOnUnsupportedBatch`, the mock server returns `w.WriteHeader(status)` for the batch endpoint. But it doesn't set the Content-Type. Is that an issue?

Looking at the `CreatePRReview` implementation (not in diff), it likely checks the status code first. If status >= 400, it returns an `*APIError`. The response body might be parsed for the error message.

Without knowing `CreatePRReview`'s implementation, I can't verify this. But the test was apparently written to work, so it must be OK.

OK final answer:

1. MEDIUM: No rate-limit delay in `postCommentsIndividually` fallback
2. Maybe LOW: Unused `out` variable in test

Actually I want to reconsider point 1. The sprint plan is very explicit:

> "Out of Scope: Rate-limit retry logic (separate concern from batching)"

This explicitly says rate-limit logic is out of scope. Flagging it might be seen as going outside the sprint scope. But it's still a real concern that affects production behavior of the in-scope code.

The scope constraint says:
> "Apply this scope: if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)."

The finding IS in a file mentioned in the sprint plan (`cmd/atcr/github.go`). And it addresses a concern about the fallback path which IS in the sprint's stated goals (per-comment fallback for Item 1). So it's not out-of-scope.

The "fix" being out of scope for this sprint doesn't make the finding out of scope — it just means the fix might be deferred.

I'll flag it as MEDIUM.

Hmm, but actually the issue is that the 100ms delay was part of the ORIGINAL code that was REMOVED. The current batch implementation doesn't need it. The fallback reverts to sequential posting but without the delay. So the fallback is missing a feature that the original had.

This is a valid finding because:
1. The fallback path exists in the in-scope code
2. The original sequential code had rate-limit mitigation
3. The fallback omits this mitigation
4. Users hitting the fallback could experience rate-limit issues

I'll flag it.

Let me also check the output more carefully:

Actually, I realize there might be one more issue. The `postCommentsIndividually` function's success message:
```go
_, _ = fmt.Fprintf(cmd.OutOrStdout(), "posted %d inline comment(s) to %s/%s#%d via per-comment fallback (%d already present)\n", posted, owner, repo, pr, deduped)
```

This is printed to stdout. But the original `postInlineComments` success path doesn't print a similar message. Let me check...

Looking at the original code (before the diff), the success path was:
```go
return len(comments), deduped, nil
```

No print message. So the fallback adds a print message that the batch path doesn't have. This is inconsistent.

But actually, looking at the diff, I see that the batch path ALSO has a print message? Let me check... No, the batch path doesn't print anything on success. Only the fallback prints.

This is an inconsistency: the fallback prints a message on success, but the batch path doesn't. This could be intentional (the fallback is a "special case" so it logs more), or it could be an oversight.

I won't flag this as it's a minor inconsistency.

### OK, final findings:

1. MEDIUM: No rate-limit delay in `postCommentsIndividually` fallback
2. (Maybe) LOW: Unused `out` variable in test — skipping this as it's too minor

Let me also double-check the fallback function for correctness once more:

```go
func postCommentsIndividually(cmd *cobra.Command, client *ghaction.Client, owner, repo string, pr int, sha string, comments []ghaction.CommentRequest, deduped int) (int, int, error) {
    posted, skipped := 0, 0
    for _, c := range comments {
        if err := client.CreateReviewComment(cmd.Context(), owner, repo, pr, sha, c); err != nil {
            var apiErr *ghaction.APIError
            if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {
                skipped++
                continue
            }
            return posted, deduped, &codedError{code: exitFailure, err: fmt.Errorf("inline comment fallback failed after %d posted: %w", posted, err)}
        }
        posted++
    }
    if skipped > 0 {
        _, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d inline comment(s) skipped (HTTP 422 — off-diff)\n", skipped)
    }
    _, _ = fmt.Fprintf(cmd.OutOrStdout(), "posted %d inline comment(s) to %s/%s#%d via per-comment fallback (%d already present)\n", posted, owner, repo, pr, deduped)
    return posted, deduped, nil
}
```

This looks correct. ✓

One subtle issue: if all comments are 422'd, `posted = 0` and `skipped > 0`. The function logs the skipped warning AND the success message (with posted=0). Then returns `(0, deduped, nil)`.

The caller in `postInlineComments` returns this as-is. So the caller would see `posted=0, deduped=0, err=nil`. This matches the test expectation. ✓

If one comment succeeds and another 422's, `posted=1, skipped=1`. The function logs the skipped warning AND the success message. Returns `(1, deduped, nil)`. ✓

If the first comment succeeds and the second fails with 403, `posted=1, err != nil`. The function returns `(1, deduped, &codedError{...})`. ✓

If the first comment fails with 403, `posted=0, err != nil`. Returns `(0, deduped, &codedError{...})`. ✓

All paths look correct.

OK, finalizing.

Actually, one more potential issue — looking at the test `TestPostInlineComments_FallbackPerCommentHardErrorPropagates`:

```go
_, _, err := postInlineComments(cmd, client, "owner", "repo", 1, "sha", findings)
require.Error(t, err, "a non-422 per-comment failure during fallback must propagate as exitFailure")
```

This test doesn't check the specific error type (exitFailure vs other). It just checks that an error is returned. This is a minor test weakness — the test could verify the codedError's exit code. But it's not a bug.

Won't flag.

### Truly final findings:

1. MEDIUM: No rate-limit delay in `postCommentsIndividually` fallback
2. (Possibly) LOW: Unused `out` variable in test

I'll output the MEDIUM finding only, as the unused variable is too minor.

Actually, wait. Let me also look at this:

```go
case 404, 405:
    // The batched /reviews endpoint is unavailable — older GitHub
    // Enterprise versions return 404/405 for it. Fall back to posting
    // each comment individually so the action still works there.
    _, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: batched review endpoint unavailable (HTTP %d); falling back to per-comment posting\n", apiErr.StatusCode)
    return postCommentsIndividually(cmd, client, owner, repo, pr, sha, comments, deduped)
```

What if the `*ghaction.APIError` pointer is nil? The code does `apiErr.StatusCode` which would panic on nil pointer dereference. But `errors.As` would have set `apiErr` to the matched error, so it shouldn't be nil. Unless `errors.As` returns true but sets apiErr to nil — which shouldn't happen per Go's docs.

This is a non-issue. ✓

OK, truly done. Let me write up the final findings.

One more thing — I want to verify that the `*ghaction.APIError` type is correctly defined. The code uses `errors.As(err, &apiErr)` where `apiErr` is `*ghaction.APIError`. This requires that `*ghaction.APIError` implements the `error` interface. We can assume this is the case based on the existing code using it.

OK, truly final findings:

1. MEDIUM: No rate-limit delay in `postCommentsIndividually` fallback

Let me write this up.

Actually, I realize there's another subtle issue. Looking at the test:

```go
t.Run(http.StatusText(status), func(t *testing.T) {
    var mu sync.Mutex
    paths := map[string]int{}
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        mu.Lock()
        paths[r.Method+" "+r.URL.Path]++
        mu.Unlock()
        ...
    }))
    defer srv.Close()
    ...
})
```

This is a subtest that creates a server. The `defer srv.Close()` is inside the subtest function, so it will be called when the subtest exits. This is correct. ✓

But wait — the `mu.Lock()` and `paths[r.Method+" "+r.URL.Path]++` — this is reading and writing to a map under a mutex. The map is not initialized with a size, but Go handles this dynamically. ✓

The test assertion:
```go
assert.Equal(t, 1, paths["POST /repos/owner/repo/pulls/1/reviews"], "batch attempted exactly once")
```

With 2 findings and 1 batch attempt that fails, the batch should be attempted exactly once. Then the fallback posts each comment individually, so 2 POST /comments requests. ✓

OK, the test is correct.

Final answer:

1. MEDIUM: `postCommentsIndividually` lacks rate-limit delay between sequential calls (cmd/atcr/github.go:226)

Hmm, what line number is that exactly? Let me count from the diff...

The diff for `cmd/atcr/github.go`:
- Line 111-114: `runGithub` change
- Line 163-168: comment update
- Line 187-204: error handling change
- Line 211-238: new `postCommentsIndividually` function

The function starts around line 211 (the `+// postCommentsIndividually posts...` line). The for loop is around line 224.

I'll use line 224 for the finding.

Wait, let me re-count. The diff context shows:
```
@@ -187,9 +191,18 @@ func postInlineComments(...)
 		Comments: comments,
 	}); err != nil {
 		var apiErr *ghaction.APIError
-		if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {
-			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d inline comment(s) could not be posted (HTTP 422 — comments may be off-diff): %v\n", len(comments), apiErr)
-			return 0, deduped, nil
+		if errors.As(err, &apiErr) {
+			switch apiErr.StatusCode {
+			case 422:
+				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d inline comment(s) could not be posted (HTTP 422 — comments may be off-diff): %v\n", len(comments), apiErr)
+				return 0, deduped, nil
+			case 404, 405:
+				// The batched /reviews endpoint is unavailable — older GitHub
+				// Enterprise versions return 404/405 for it. Fall back to posting
+				// each comment individually so the action still works there.
+				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: batched review endpoint unavailable (HTTP %d); falling back to per-comment posting\n", apiErr.StatusCode)
+				return postCommentsIndividually(cmd, client, owner, repo, pr, sha, comments, deduped)
+			}
 		}
 		return 0, deduped, &codedError{code: exitFailure, err: fmt.Errorf("%d inline comment(s) failed to post: %w", len(comments), err)}
 	}
@@ -198,6 +211,32 @@ func postInlineComments(cmd *cobra.Command, client *ghaction.Client, owner, repo
 	return len(comments), deduped, nil
 }
 
+// postCommentsIndividually posts each comment as a separate PR review comment.
+// It is the fallback path taken when the batched /reviews endpoint is
+// unavailable (HTTP 404/405, e.g. older GitHub Enterprise). A per-comment 422 is
+// treated as a non-fatal off-diff skip — mirroring the batch path's 422 handling
+// — while any other per-comment error aborts with exitFailure. Returns the
+// number posted and the (passed-through) dedup count.
+func postCommentsIndividually(cmd *cobra.Command, client *ghaction.Client, owner, repo string, pr int, sha string, comments []ghaction.CommentRequest, deduped int) (int, int, error) {
+	posted, skipped := 0, 0
+	for _, c := range comments {
+		if err := client.CreateReviewComment(cmd.Context(), owner, repo, pr, sha, c); err != nil {
```

So the for loop starts around line 222 (in the new file). The `client.CreateReviewComment` call is around line 223.

I'll use line 222 for the finding.

Actually, since I don't have exact line numbers, I'll use a reasonable approximation. Let me say "cmd/atcr/github.go:222" for the for loop.

OK, finalizing the output.

Wait, one more issue I want to check. The `postInlineComments` function comment was updated:

```
// postInlineComments posts anchorable findings as a single batched PR review.
// It first lists existing PR review comments to skip any that atcr already
// posted (dedup across re-runs). It returns the number of new comments posted
// and the number skipped because they were already present.
```

Updated to:
```
// postInlineComments posts anchorable findings as a single batched PR review.
// It first lists existing PR review comments to skip any that atcr already
// posted (dedup across re-runs). When the batched /reviews endpoint is
// unavailable (HTTP 404/405, e.g. older GitHub Enterprise) it falls back to
// posting each comment individually via postCommentsIndividually. It returns the
// number of new comments posted and the number skipped because they were already
// present.
```

✓ This matches the implementation.

OK truly done now. Final output:

MEDIUM|cmd/atcr/github.go:222|Fallback path posts N comments sequentially without delay, could hit GitHub API rate limits for PRs with many findings|Add a small delay (e.g., 100ms) between per-comment POSTs to mirror original rate-limit mitigation|correctness|10|postCommentsIndividually loops CreateReviewComment with no backoff; original 100ms delay removed|bruce

Hmm, but is this really MEDIUM? The original delay was 100ms = 0.1s. For 100 comments, that's 10 seconds total. For 10 comments, 1 second. So the delay adds latency. The trade-off is:
- Without delay: faster but risk of rate limit
- With delay: slower but safer

For typical use cases (10-20 comments), the delay would be 1-2 seconds. The rate limit risk depends on how often the fallback is hit.

I'd say MEDIUM is appropriate.

Actually, let me reconsider. The sprint plan says:
> "100ms inter-call delay was added for rate-limit mitigation but doesn't address the fundamental O(N) API call count"

And:
> "OUT of scope: ... rate-limit retry logic"

So the original delay was a rate-limit mitigation that was specific to the old sequential code. The new batch code doesn't need it (one call). The fallback reverts to sequential, but the delay wasn't re-added.

This is a known gap that the team may want to address in a future sprint. Flagging it raises awareness.

I'll keep it as MEDIUM.

OK, final output coming up.

Actually, I realize I should also look at this more carefully. The sprint plan says:

> "Retain per-comment fallback if batch fails (some GitHub Enterprise versions may not support the endpoint)"

This says "per-comment fallback" but doesn't specify rate-limit handling. The team's choice to omit the delay is a design decision.

But the original code had it. Removing it for the fallback is a regression in robustness.

OK, MEDIUM it is.

Let me also check if there are any test issues I should flag.

Looking at `TestPostInlineComments_FallsBackToPerCommentOnUnsupportedBatch`:
- It iterates over `[]int{http.StatusNotFound, http.StatusMethodNotAllowed}` — both 404 and 405
- For each status, it creates a fresh server and tests the fallback
- The server returns the batch status, then falls back to per-comment

This is comprehensive. ✓

But wait — the test uses `defer srv.Close()` inside the subtest function. This is correct because `srv` is defined inside the subtest.

One subtle issue: the subtest creates a new `srv` for each iteration, but the outer `srv` variable... wait, no, `srv` is declared inside the subtest function. ✓

OK, all good.

### Final output:

MEDIUM|cmd/atcr/github.go:222|Fallback path posts N comments sequentially without delay, could hit GitHub API rate limits|Add a small inter-call delay (e.g., 100ms) to mirror original rate-limit mitigation|correctness|10|postCommentsIndividually loops CreateReviewComment with no backoff between calls|bruce

Hmm, I realize I should also consider whether this is a real concern or theoretical. GitHub's secondary rate limit is typically 900 requests per minute for the REST API. For a PR with 100 findings, the fallback would make 100 requests in ~1 second (no delay). This would NOT exceed the primary rate limit (5000/hour) but might exceed the secondary rate limit (900/minute).

For a PR with 500 findings, the fallback would make 500 requests. This could trigger secondary rate limiting.

So the concern is real for large PRs. MEDIUM is appropriate.

OK final output.

Wait, let me look one more time at the `postCommentsIndividually` function to see if there's a context cancellation check.

```go
func postCommentsIndividually(cmd *cobra.Command, client *ghaction.Client, owner, repo string, pr int, sha string, comments []ghaction.CommentRequest, deduped int) (int, int, error) {
    posted, skipped := 0, 0
    for _, c := range comments {
        if err := client.CreateReviewComment(cmd.Context(), owner, repo, pr, sha, c); err != nil {
            ...
        }
        posted++
    }
    ...
}
```

No context cancellation check. If the context is cancelled mid-loop, the loop continues until all comments are processed. The HTTP client should respect context cancellation, so each `CreateReviewComment` call should fail quickly with a context error. But the loop will still iterate through all comments, attempting each one and failing.

This is a minor inefficiency. Not a major issue.

Won't flag.

### TRULY final output:

MEDIUM|cmd/atcr/github.go:222|Fallback path posts N comments sequentially without delay, could hit GitHub API rate limits|Add a small inter-call delay (e.g., 100ms) to mirror original rate-limit mitigation|correctness|10|postCommentsIndividually loops CreateReviewComment with no backoff between calls|bruce
</think>

MEDIUM|cmd/atcr/github.go:222|Fallback path posts N comments sequentially without delay, risking GitHub secondary rate limits|Add a small inter-call delay (e.g., 100ms) to mirror original rate-limit mitigation|correctness|10|postCommentsIndividually loops CreateReviewComment with no backoff between calls|bruce