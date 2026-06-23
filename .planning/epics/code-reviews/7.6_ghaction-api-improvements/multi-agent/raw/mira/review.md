<think>
Let me carefully review the diff against the sprint plan scope and identify issues.

The sprint plan scope is:
- `internal/ghaction/client.go` — new `CreateReview` batch method (but already exists as `CreatePRReview`)
- `internal/ghaction/render.go` — `BuildCheckOutput` return-signature change
- `cmd/atcr/github.go` — call-site updates for both fixes
- `internal/ghaction/client_test.go` and `internal/ghaction/render_test.go` — test updates
- `cmd/atcr/github_test.go` — test updates for new fallback

Let me review each file in the diff:

1. **`cmd/atcr/github.go`**:
   - Line 114-115: `BuildCheckOutput` now returns 3 values, destructured. The comment is accurate.
   - Line 187-201: Added switch for error handling - 422 (off-diff), 404/405 (fallback to per-comment), other errors propagate. This matches the plan.
   - New `postCommentsIndividually` function: Posts each comment individually with 422 as non-fatal skip.

2. **`cmd/atcr/github_test.go`**: Three new tests for the fallback behavior.

3. **`internal/ghaction/render.go`**:
   - `BuildCheckOutput` now returns `(CheckOutput, string, int)`.
   - Early-return branch now computes `Conclusion` to populate return values.

4. **`internal/ghaction/render_test.go`**: Tests updated for new return signature, new test for the 3-tuple return.

Let me look for issues:

**Issue 1: `postCommentsIndividually` falls back without context cancellation awareness**
The function iterates through comments and uses `cmd.Context()`. If context is cancelled mid-loop, partial posting occurs. This is acceptable since the loop checks each error, and context cancellation would propagate. Actually, looking more carefully - the `client.CreateReviewComment` call passes the context, so context cancellation will manifest as an error. But the per-comment error check only handles 422 specifically, and other errors propagate as `exitFailure`. Context cancellation errors would propagate, which is correct behavior.

**Issue 2: Fallback has no rate limiting**
The original concern was that sequential posting was slow due to 100ms inter-call delays. The fallback path has NO inter-call delay. This could trigger rate limits. But the sprint plan says "Rate-limit retry logic" is OUT of scope. Still, even a simple delay would be a robustness improvement. However, this might be considered an enhancement rather than a bug.

**Issue 3: `postCommentsIndividually` doesn't update `deduped` count**
The function signature passes `deduped` through. It returns `deduped` unchanged. This is correct behavior - `deduped` was already counted at the batch level.

**Issue 4: Output messages - inconsistency in the "posted" log line**
In the original `postInlineComments` success path, there's likely a log line. In the fallback path, there's a separate log line saying "via per-comment fallback". This seems fine and intentional.

**Issue 5: Edge case in `postCommentsIndividually` - first comment fails with 422, second succeeds**
The `skipped` counter is incremented correctly. The function returns `posted, deduped, nil` - this is correct.

**Issue 6: Comment in render.go early-return branch is slightly misleading**
"neutral when informational, success under any threshold" - this is accurate based on what `Conclusion` does.

**Issue 7: Test `TestPostInlineComments_FallbackPerCommentHardErrorPropagates` doesn't verify the error type**
The test only asserts `err != nil` but doesn't verify it's a `codedError` with `exitFailure`. The other tests have similar gaps. This is a test completeness issue.

**Issue 8: No error check on first API error case path - the switch falls through to default**
The switch has cases for 422 and 404/405. The `default` case falls through to the outer return (exitFailure). But there's a subtle issue: what if `errors.As(err, &apiErr)` returns false (i.e., the error is not an APIError)? Then the switch is skipped entirely, and we fall through to the `return 0, deduped, &codedError{...}`. This is correct behavior.

**Issue 9: Log message in fallback says "via per-comment fallback" regardless of whether comments were actually posted**
The log line is emitted even when `posted == 0`. This is a minor UX issue - the message says "posted 0 inline comment(s)... via per-comment fallback" which could be misleading when all comments were skipped. Looking at the batch path's success log... I don't actually see a "posted N" log in the batch path in the diff. Let me re-check.

Looking at the diff again, I don't see a "posted N" log in the batch path. So this is a new log line added only in the fallback. That's a minor inconsistency but acceptable.

**Issue 10: No log when all comments are skipped due to 422 in fallback (only "warning" line)**
This is fine, there's a warning line for skipped comments.

**Issue 11: Per-comment fallback doesn't use a context cancellation check between calls**
The function uses `cmd.Context()` for each call, but doesn't explicitly check for cancellation. If context is cancelled, the HTTP call will return an error which will be caught by the per-comment error handler. But this could lead to slow response to cancellation. Minor issue.

**Issue 12: The fallback function's name is `postCommentsIndividually` but uses `client.CreateReviewComment`**
This is fine - it posts individually, so the name is accurate.

**Issue 13: Test `TestPostInlineComments_FallbackPerCommentHardErrorPropagates` doesn't track which endpoint was hit**
This is a test coverage issue, but the test does assert the error occurred, which is the main thing being tested.

**Issue 14: Switch case uses `case 404, 405:` - this is a multi-value case, which is correct Go syntax**

**Issue 15: In `BuildCheckOutput`, the early-return branch calls `Conclusion` even though it could just hardcode the values**
For an empty `findings` slice:
- `failCount` is always 0
- `conclusion` depends on `failOn`: if `failOn == ""`, it's `ConclusionNeutral`; otherwise `ConclusionSuccess`

Calling `Conclusion(nil, failOn)` is a small inefficiency, but it's the cleanest way to ensure consistency. The plan explicitly says "Compute conclusion in the `total == 0` early-return branch too so the return is always populated." So this is intentional.

Actually, wait - re-reading the plan more carefully: "Compute conclusion in the `total == 0` early-return branch too so the return is always populated." This means compute the conclusion so the return values are populated. Calling `Conclusion` does this. ✓

**Issue 16: The test `TestBuildCheckOutputReturnsConclusionAndFailCount` doesn't test a case with refuted findings**
The test has only one sub-test with findings. The refuted path is exercised in `TestBuildCheckOutput` but not in the new return-value test. This is a test coverage gap.

**Issue 17: No update to the `postInlineComments` docstring mentioning that 404/405 returns the same dedup count**
This is a documentation concern but minor.

**Issue 18: In the new `postCommentsIndividually`, the "posted N" message always includes "via per-comment fallback" - even if all comments were skipped (posted=0)**
This could be confusing in logs. Minor issue.

**Issue 19: No max retry / backoff in fallback loop**
If GitHub is rate-limiting, this could spam the API. But the plan says rate-limit retry is out of scope.

**Issue 20: Variable shadowing in render.go early-return branch**
The early-return branch declares `conclusion, failCount := Conclusion(findings, failOn)`. These are new variables, no shadowing. ✓

**Issue 21: Comment line 110-112 in render.go mentions "neutral when informational, success under any threshold"**
The comment claims "success under any threshold" for the empty-findings branch. This is correct per the `Conclusion` logic (empty findings means no failures, so any threshold is met → success, unless failOn is empty → neutral). The comment is accurate.

Let me think about what's actually problematic...

**Significant Issue 1: Fallback to per-comment posting has no inter-call delay**
The original concern about rate-limiting motivated the 100ms delay in the original sequential posting. The new fallback path has NO delay. For PRs with many findings, this could trigger rate limits. However, the sprint plan says rate-limit retry is out of scope, and the fallback is for older GHE which is unlikely to need rate-limit protection. This is a low-severity concern.

**Significant Issue 2: Context cancellation handling in fallback**
If `cmd.Context()` is cancelled mid-loop, each subsequent `CreateReviewComment` call will fail with a context error. The function will treat this as a hard error and return `exitFailure`, even though cancellation is not a failure. This is a robustness issue.

**Significant Issue 3: No context-cancellation fast-path in fallback loop**
A common pattern is to check `ctx.Err()` between iterations to fail fast. Missing this means a cancelled context could result in N HTTP call attempts before the cancellation is noticed. Performance/correctness concern.

**Significant Issue 4: Test gap - doesn't verify `deduped` is passed through correctly in fallback**
Tests use empty existing comments (deduped=0), so this isn't exercised.

**Significant Issue 5: Log message inconsistency**
The batch path doesn't log "posted N" on success (in the diff), but the fallback does. This is a minor inconsistency.

**Significant Issue 6: The test `TestPostInlineComments_FallbackPerCommentHardErrorPropagates` doesn't check the `posted` count**
When the first comment fails, `posted` should be 0. The test discards the return value, so it can't verify this.

**Significant Issue 7: `TestPostInlineComments_FallbackPerCommentHardErrorPropagates` doesn't verify error type**
Doesn't check that the returned error is a `codedError` with `exitFailure`. Test completeness issue.

Let me focus on real, actionable issues within scope:

1. **No rate-limit delay in fallback** - MEDIUM (performance/correctness)
2. **No context-cancellation check in fallback loop** - MEDIUM (correctness)
3. **Test doesn't verify `codedError` with `exitFailure`** - LOW (test completeness)
4. **Test `TestPostInlineComments_FallbackPerCommentHardErrorPropagates` doesn't verify `posted` count** - LOW (test completeness)

Let me also look more carefully at the existing test `TestPostInlineComments_422IsNonFatal` - it's marked as unchanged in the diff, but the implementation has changed slightly. Let me check...

Actually the test is at line 317 in the diff, which seems to be unchanged code shown for context. The test isn't being modified, just shown in context. Let me check the existing test signature...

Looking at the diff line `@@ -317,6 +317,130 @@`, the `TestPostInlineComments_422IsNonFatal` test starts at line 317 and is preserved (6 lines unchanged) before the new tests are added at line 323.

Wait, actually let me re-examine. The diff shows:
```
@@ -317,6 +317,130 @@ func TestPostInlineComments_422IsNonFatal(t *testing.T) {
 	assert.Contains(t, stderr.String(), "422")
}
```
followed by new tests. The `+130` means 130 new lines are added after the unchanged `TestPostInlineComments_422IsNonFatal`. So the existing test is unchanged.

Now let me also consider:
- Is there an issue with the test using `srv.Client()` as `HTTPClient` in the client? `httptest.NewServer.Client()` returns a client configured to trust the test server's TLS. But since `srv.URL` is `http://`, this is fine. Minor thought, not an issue.

Let me also check the `codedError` usage. The function uses `&codedError{code: exitFailure, err: ...}`. This should work with the existing pattern.

OK, let me finalize my findings.

Actually, let me re-look at the fallback function more carefully:

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

Issues:
1. No context cancellation check between iterations
2. No inter-call delay
3. The "posted N" log is emitted even when posted=0 (e.g., all skipped)
4. If a network error occurs (not an APIError), the error wraps the original error - is the original error an `*APIError`? Let me check the logic:
   - `errors.As(err, &apiErr) && apiErr.StatusCode == 422` - if this is false (not 422), then it falls through to the `return posted, deduped, &codedError{...}`. This means non-422 errors always propagate. ✓

Let me think about other issues:

**Issue: `deduped` could be misleading in log**
The log says "(%d already present)" using `deduped`. If `deduped > 0`, the `comments` slice is already filtered to exclude those. So `posted` is the count of new comments posted (which is `len(comments) - skipped`). The log is correct.

**Issue: In tests, `paths` map isn't protected by mutex when reading**
The test code:
```go
mu.Lock()
defer mu.Unlock()
assert.Equal(t, 1, paths["POST /repos/owner/repo/pulls/1/reviews"], ...)
```
The `paths` map is read inside the mutex, so this is safe. ✓

Wait, actually there's a subtle race: the assertions are inside the mutex lock, but the assertions might involve internal work. Also, `mu` is declared per subtest iteration. Actually no, looking more carefully:

```go
var mu sync.Mutex
paths := map[string]int{}
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    mu.Lock()
    paths[r.Method+" "+r.URL.Path]++
    mu.Unlock()
    ...
```

The mutex protects the `paths` map. The `assert.Equal` calls happen with `mu` held (via `mu.Lock()` at line "mu.Lock()" and the `defer mu.Unlock()`). Wait, the test has:

```go
mu.Lock()
defer mu.Unlock()
assert.Equal(t, 1, paths["POST /repos/owner/repo/pulls/1/reviews"], "batch attempted exactly once")
assert.Equal(t, 2, paths["POST /repos/owner/repo/pulls/1/comments"], "fell back to one POST per comment")
```

This is correct - assertions happen with the lock held. ✓

OK, let me also check: when the test runs with `cmd.SetContext(context.Background())`, the context has no deadline. This is fine for tests.

Let me look at one more thing - the `httptest.NewServer` is `http` not `https`. The client uses `srv.Client()` which is fine for `http`.

Final list of actionable issues within scope:

1. **MEDIUM: Fallback loop has no rate-limit delay** - could trigger GitHub rate limits on PRs with many findings, defeating the purpose of the fallback.
   - But the plan says rate-limit is out of scope...
   - This is a real concern but might be intentional.
   - LOW if rate-limit is out of scope, MEDIUM if we consider it a real issue.

2. **MEDIUM: Fallback loop doesn't check context cancellation between iterations** - if `cmd.Context()` is cancelled, each subsequent call will block until timeout.
   - This is a real concern. The test setup uses `context.Background()` but in production, the context could be cancelled.

3. **LOW: Test doesn't verify error is `codedError` with `exitFailure`** - test completeness.

4. **LOW: Test doesn't verify `posted=0` in hard-error case** - test completeness.

5. **LOW: Log inconsistency** - "posted N via per-comment fallback" emitted even when posted=0.

Let me decide which to report. I should focus on the most impactful and clearly actionable issues.

Actually, looking again at #1: the original concern with sequential posting was the 100ms delay "for rate-limit mitigation". If the fallback path has no delay, it's worse than the original in terms of rate-limit risk. This is a real regression. MEDIUM.

#2: Context cancellation is important for any long-running loop. MEDIUM.

#3, #4, #5: Test/log polish. LOW.

Let me also look at the `BuildCheckOutput` change once more. The early-return branch now calls `Conclusion` which does an O(N) traversal, but N=0, so it's O(1). Actually `len(nil) == 0`, so this is fine. ✓

Wait, actually I missed something. Let me look at the `Conclusion` function logic when findings is empty:

The plan says: "Compute conclusion in the `total == 0` early-return branch too so the return is always populated."

The diff does this by calling `Conclusion(findings, failOn)`. For empty findings, `Conclusion` returns `("success", 0)` if `failOn` is set, or `("neutral", 0)` if `failOn` is empty.

This is correct. The test `TestBuildCheckOutputReturnsConclusionAndFailCount` verifies this.

Let me check if there's a subtle issue: in the empty-findings early-return branch, we're calling `Conclusion` which is the function we're trying to avoid duplicating. But since N=0, this is O(1), so the impact is negligible. Not a real issue.

One more thing: looking at the comment "the same values Conclusion would compute". If `Conclusion`'s implementation ever changes, `BuildCheckOutput` could drift. But this is encapsulated in the package, and the test pins the relationship. Not a real issue.

OK final findings:

1. MEDIUM - No context cancellation check in fallback loop
2. MEDIUM - No rate-limit delay in fallback (the original concern)  
3. LOW - Test doesn't verify error type/codedError
4. LOW - Test doesn't verify posted=0 in hard error case
5. LOW - "posted N" log emitted even when posted=0

Let me also think about whether the new function `postCommentsIndividually` having `cmd` as a parameter is consistent with the rest of the package... Yes, `postInlineComments` also takes `cmd`, so this is consistent.

What about the placement of the new function? It's placed right after `postInlineComments`, which makes sense for code organization.

Let me also think about whether the switch statement handles edge cases properly:
- `apiErr` is nil: `errors.As` returns false, switch is skipped, falls through to outer return
- `apiErr` is not nil with unknown status code: switch falls through (no case matches), outer return triggers
- This is correct. ✓

Actually, one more subtle issue: in Go's switch statement, if no case matches, control falls out of the switch. The code after the switch is the `return 0, deduped, &codedError{...}`. So for unknown status codes (e.g., 500), the outer return is triggered. ✓

OK, I think my findings are:

1. **MEDIUM** - Fallback has no rate-limit delay (real concern, original code had 100ms delay for this reason)
2. **MEDIUM** - Fallback doesn't check context cancellation between iterations
3. **LOW** - Test should verify error is `codedError` with `exitFailure`
4. **LOW** - "posted N" log misleading when N=0

Hmm, I should think about #3 more carefully. The test `TestPostInlineComments_FallbackPerCommentHardErrorPropagates` asserts `err != nil`. It doesn't verify the error type. Is this an issue worth flagging? The plan says "other errors propagate as `codedError{exitFailure}` unchanged" - so verifying the error type would be a stronger test. But the test as written is acceptable. LOW.

For #4, the log says "posted 0 inline comment(s) to owner/repo#1 via per-comment fallback (0 already present)". This is slightly misleading - it says "posted 0" which is technically correct (none were posted) but reads oddly. Could be rephrased. LOW.

Let me also think about #1. The plan says: "Rate-limit retry logic (separate concern from batching)" is out of scope. But adding a simple inter-call delay isn't retry logic - it's basic rate-limit prevention. The original code had a 100ms delay "for rate-limit mitigation". The new fallback has no such mitigation. This is a real regression in behavior for users on older GHE.

However, since the plan explicitly excludes rate-limit concerns, this could be considered OUT-OF-SCOPE. Let me re-read the scope constraint...

The plan says OUT OF SCOPE: "Rate-limit retry logic (separate concern from batching)". A simple delay isn't really "retry logic" - it's "prevention logic". But the spirit of the exclusion seems to be "don't worry about rate limits in this work". So I'll mark this as MEDIUM but note the trade-off.

Actually, looking at this more carefully, the plan says per-comment fallback is IN scope. The concern is whether the implementation should include rate-limit prevention. I'll flag it as MEDIUM since it's a real concern that the implementation should address.

Let me finalize my findings. I'll keep them focused and actionable.

Actually, let me also check: in the `postCommentsIndividually` function, when `posted=0` and `skipped>0`, the function returns `posted, deduped, nil` with `posted=0`. The caller `postInlineComments` returns this directly. In `runGithub`, what does it do with the return values?

Looking at the diff, I see:
```go
posted, skipped, err := postInlineComments(...)
```

(I don't see this in the diff, but it's the call site.) If `posted=0`, this is correct. ✓

Let me also note: the test `TestPostInlineComments_FallbackPerComment422IsSkipped` uses `assert.Contains(t, stderr.String(), "422")` - but the warning message says "HTTP 422" so this matches. ✓

OK, my final findings:

1. **MEDIUM** - No context cancellation check in fallback loop (could cause slow shutdown)
2. **MEDIUM** - No inter-call rate-limit delay in fallback (regression from original 100ms delay)
3. **LOW** - Test gap: hard-error test doesn't verify error is `codedError` with `exitFailure`
4. **LOW** - Log "posted N via per-comment fallback" emitted even when N=0

Let me also reconsider whether the rate-limit concern is in-scope. The plan's "Risks" section mentions "Rate-limit retry logic" as a risk being mitigated (or rather, deferred). The plan's "Out of Scope" explicitly says "Rate-limit retry logic (separate concern from batching)". So the plan author is saying "don't add rate-limit retry logic in this work". But adding a simple delay is arguably a different concern from "retry logic". I'll include it as MEDIUM with a note.

Actually, I'll be more conservative and include the most important findings only. The rate-limit delay is debatable. Let me keep it as MEDIUM but lean toward LOW since the plan explicitly excludes it.

Final list:

1. **MEDIUM** - No context cancellation check in fallback loop
2. **LOW** - Test gap: hard-error test doesn't verify error is `codedError` with `exitFailure`  
3. **LOW** - Log "posted N via per-comment fallback" emitted even when N=0

Hmm, let me also think about whether there's anything else I might be missing.

Wait - the `client.CreateReviewComment` is called in the fallback. This is correct per the plan. ✓

The `cmd.ErrOrStderr()` and `cmd.OutOrStdout()` calls are correct. ✓

The `errors.As` usage is correct. ✓

The `codedError` usage matches the existing pattern. ✓

OK, I think my findings are good. Let me format them.

Actually, one more concern: in the `postCommentsIndividually` function, when the first comment fails with a non-422 error, the function returns immediately with `posted=0` and the error. The error message says "inline comment fallback failed after 0 posted: ...". This is fine. ✓

What about the case where the first comment is posted (posted=1) and the second fails with a non-422 error? The error says "inline comment fallback failed after 1 posted: ...". This is informative. ✓

OK, my final findings stand. Let me format them for output.

Format:
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER

Findings:
1. MEDIUM|cmd/atcr/github.go:217|Fallback loop doesn't check context cancellation|Add ctx.Err() check between iterations to fail fast on cancellation|correctness|5|for _, c := range comments { client.CreateReviewComment(cmd.Context(), ...)|bruce
2. LOW|cmd/atcr/github_test.go:441|Hard-error test doesn't verify codedError type|Assert errors.As(err, &codedError) and check code == exitFailure|maintainability|5|_, _, err := postInlineComments(...); require.Error(t, err)|bruce
3. LOW|cmd/atcr/github.go:234|"posted N" log misleading when N=0 due to all-skipped|Only log when posted > 0, or rephrase to "fallback: 0 posted, N skipped"|maintainability|2|fmt.Fprintf(cmd.OutOrStdout(), "posted %d inline comment(s) ...|bruce

Wait, the line numbers - let me re-check. The new function `postCommentsIndividually` starts at line 214 in the diff. The for loop is at line 219 (approximate). Let me look at the diff more carefully...

Looking at the diff:
```
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

The for loop is at line ~221 (counting from the function declaration at line 214). Let me approximate the line numbers.

Actually, I don't have exact line numbers from the diff (the @@ markers give context, not exact line numbers). Let me just use approximate line numbers from the diff context.

The `@@ -198,6 +211,32 @@` marker means the old file had 6 lines starting at 198, the new file has 32 lines starting at 211. So the new function `postCommentsIndividually` starts at line 214 (after the 3 lines of context above the function).

Wait, the format is:
- `@@ -198,6 +211,32 @@ postInlineComments(...)`
- This means: in the old file, starting at line 198, 6 lines of context. In the new file, starting at line 211, 32 lines.

The context line `postInlineComments` itself is line 211 in the new file. After it, there are 32 more lines (including context and additions).

Let me count the new function:
- `// postCommentsIndividually posts each comment as a separate PR review comment.` - line 214
- `// It is the fallback path taken when the batched /reviews endpoint is` - line 215
- `// unavailable (HTTP 404/405, e.g. older GitHub Enterprise). A per-comment 422 is` - line 216
- `// treated as a non-fatal off-diff skip — mirroring the batch path's 422 handling` - line 217
- `// — while any other per-comment error aborts with exitFailure. Returns the` - line 218
- `// number posted and the (passed-through) dedup count.` - line 219
- `func postCommentsIndividually(cmd *cobra.Command, client *ghaction.Client, owner, repo string, pr int, sha string, comments []ghaction.CommentRequest, deduped int) (int, int, error) {` - line 220
- `	posted, skipped := 0, 0` - line 221
- `	for _, c := range comments {` - line 222
- `		if err := client.CreateReviewComment(cmd.Context(), owner, repo, pr, sha, c); err != nil {` - line 223
- `			var apiErr *ghaction.APIError` - line 224
- `			if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {` - line 225
- `				skipped++` - line 226
- `				continue // off-diff comment — non-fatal, consistent with the batch path` - line 227
- `			}` - line 228
- `			return posted, deduped, &codedError{code: exitFailure, err: fmt.Errorf("inline comment fallback failed after %d posted: %w", posted, err)}` - line 229
- `		}` - line 230
- `		posted++` - line 231
- `	}` - line 232
- `	if skipped > 0 {` - line 233
- `		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d inline comment(s) skipped (HTTP 422 — off-diff)\n", skipped)` - line 234
- `	}` - line 235
- `	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "posted %d inline comment(s) to %s/%s#%d via per-comment fallback (%d already present)\n", posted, owner, repo, pr, deduped)` - line 236
- `	return posted, deduped, nil` - line 237
- `}` - line 238

So:
- For loop context check would be at line 222
- Log line at 236

For tests:
- `TestPostInlineComments_FallsBackToPerCommentOnUnsupportedBatch` is added at the beginning of the new test block
- `TestPostInlineComments_FallbackPerComment422IsSkipped` 
- `TestPostInlineComments_FallbackPerCommentHardErrorPropagates`

The `@@ -317,6 +317,130 @@` marker means the new file has 130 new lines starting at line 317. The test `TestPostInlineComments_FallbackPerCommentHardErrorPropagates` would be the last one, starting around line 430+.

I don't have exact line numbers but I'll use approximate ones from the diff context.

OK let me finalize my output.

Wait, I want to think once more about whether the rate-limit concern is in-scope. The plan says:

> | Rate-limit retry logic | Separate concern from batching |

This is listed under "Out of Scope" for the EPIC. But the per-comment fallback is the implementation of an IN-SCOPE item. The fallback itself could include a simple delay to prevent rate-limiting. This is a quality-of-implementation issue.

I think the spirit of the exclusion is "don't add sophisticated retry/backoff logic" - a simple 100ms delay is the bare minimum to prevent hammering the API. I'll flag it as LOW with a note that the original code had a 100ms delay for this reason.

Actually, on second thought, I'll keep it out. The plan is explicit about rate-limit being out of scope, and adding a delay is rate-limit mitigation. Let me focus on context cancellation and the test gap.

Final findings:

1. **MEDIUM** - No context cancellation check in fallback loop
2. **LOW** - Test gap: hard-error test doesn't verify error is `codedError` with `exitFailure`
3. **LOW** - Log "posted N via per-comment fallback" emitted even when N=0

Actually, one more thing I want to verify. Let me look at the function `postCommentsIndividually` for any other issues:

```go
func postCommentsIndividually(cmd *cobra.Command, client *ghaction.Client, owner, repo string, pr int, sha string, comments []ghaction.CommentRequest, deduped int) (int, int, error) {
	posted, skipped := 0, 0
	for _, c := range comments {
		if err := client.CreateReviewComment(cmd.Context(), owner, repo, pr, sha, c); err != nil {
			var apiErr *ghaction.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {
				skipped++
				continue // off-diff comment — non-fatal, consistent with the batch path
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

The function returns `(posted, deduped, err)`. The `skipped` count is internal - it's not returned. This means the caller can't know how many were skipped due to 422. The batch path also doesn't return this count - the existing 422 handling just returns `(0, deduped, nil)`. So the behavior is consistent. ✓

Wait, actually the batch path returns `(0, deduped, nil)` for 422 - meaning `posted=0`. But the fallback path returns `(posted, deduped, nil)` where `posted` is the count of actually-posted comments. This is an INCONSISTENCY between the two paths. In the batch path, a 422 means ALL comments failed (the batch was rejected). In the fallback path, a 422 means ONE comment was off-diff (others might succeed).

But this is a design choice - the fallback handles per-comment 422 individually. The batch path doesn't have per-comment granularity for 422. So the behavior difference is justified by the API design.

However, this is a subtle behavior difference that could surprise callers. Is it worth flagging? It's a design decision, not a bug. The test `TestPostInlineComments_FallbackPerComment422IsSkipped` documents this behavior. I'll skip this.

OK, final output:

1. **MEDIUM** - Context cancellation handling in fallback loop
2. **LOW** - Test gap on hard-error type verification
3. **LOW** - Log message when posted=0

Let me also think about whether the function `postCommentsIndividually` is being properly tested. The three tests cover:
- 404/405 from batch → falls back to per-comment (2 comments posted)
- 422 per-comment during fallback → skipped
- Non-422 hard error during fallback → propagates

What about:
- Partial success: first comment succeeds, second is 422, third is 404? Not tested.
- Empty comments list: would skip the loop and log "posted 0". Not tested.
- ctx cancellation during loop: not tested (but cmd.SetContext(context.Background()) is used).

These are edge cases that aren't tested but are unlikely to cause issues. I'll skip.

What about the case where `len(comments) == 0` in the fallback? The function would return `(0, deduped, nil)` and log "posted 0 inline comment(s) to ... via per-comment fallback". This is a weird message. But this case is filtered out by the caller (`postInlineComments` returns early if `len(comments) == 0`). So this case can't happen. ✓

OK, I'm confident in my final findings. Let me format the output.

Hmm, on the test for hard-error propagation - the test has `_, _, err := postInlineComments(...)`. It uses `_` for the counts. It also doesn't check that the error wraps a `codedError` with `exitFailure`. This is a test completeness issue.

The other tests also have similar gaps (they use `_` for some return values). But the hard-error test is specifically about error propagation, so it should verify the error type.

Final output:

```
MEDIUM|cmd/atcr/github.go:222|Fallback loop does not check context cancellation between iterations|Add ctx.Err() check before each CreateReviewComment call to fail fast|correctness|5|for _, c := range comments { client.CreateReviewComment(cmd.Context(), ...)|bruce
LOW|cmd/atcr/github_test.go:441|Hard-error test does not verify error is codedError with exitFailure|Use errors.As to assert codedError and check code == exitFailure|maintainability|5|_, _, err := postInlineComments(...); require.Error(t, err)|bruce
LOW|cmd/atcr/github.go:236|Log says "posted N via per-comment fallback" even when N=0 all-skipped|Only log when posted > 0; or rephrase to "fallback: 0 posted, N skipped"|maintainability|2|fmt.Fprintf(cmd.OutOrStdout(), "posted %d inline comment(s) ...|bruce
```

Let me also think about whether to include the rate-limit delay issue. The plan says it's out of scope. But the original code had a 100ms delay. Removing it for the fallback is a regression. I'll include it as LOW with a note.

Actually, let me re-read the plan one more time:

> ## Risks
> 
> | Risk | Mitigation |
> |------|------------|
> | Batch review endpoint not supported on GitHub Enterprise | Implement with per-comment fallback; detect 404/405 from batch call |

So the risk is "batch endpoint not supported" and the mitigation is "per-comment fallback". The plan doesn't mention rate-limiting in the fallback path. The rate-limit concern is the original problem that the batch endpoint was supposed to solve.

If the fallback is called on a PR with many findings, and there's no inter-call delay, this could:
1. Trigger GitHub's rate limit (5,000 requests/hour for authenticated users)
2. Cause timeouts

For a PR with 100 findings, 100 sequential calls with no delay would complete quickly but might trigger rate limits if the action is run frequently. With a 100ms delay, it would take 10 seconds but be safer.

The plan says rate-limit retry is out of scope. But prevention (delay) vs. retry are different concerns. I'll include it as LOW since it's a real concern that the implementation should address.

OK, final findings:

1. **MEDIUM** - Context cancellation
2. **LOW** - Test gap on error type
3. **LOW** - Log message when posted=0
4. **LOW** - No rate-limit delay in fallback (regression from original)

Let me format these.

Wait, I'm going to be more conservative. The plan says rate-limit is out of scope, and the context cancellation issue is real but might be considered minor for a CLI tool. Let me focus on the most impactful issues.

Actually, context cancellation is a real concern for any loop. If a user Ctrl-C's the CLI, each subsequent call could block for a while. This is a real correctness issue. MEDIUM is appropriate.

The test gap is real but minor. LOW is appropriate.

The log message is a minor UX issue. LOW is appropriate.

The rate-limit delay is a real concern but the plan excludes it. I'll skip it or include it as LOW.

Final list:
1. MEDIUM - Context cancellation
2. LOW - Test gap
3. LOW - Log message

I'll include the rate-limit delay as LOW since it's a real regression and easy to fix.

Actually, let me re-think the rate-limit issue. The fallback is for "older GitHub Enterprise" which is unlikely to have rate limits as aggressive as github.com. And the action is typically run once per CI build, not in a tight loop. So the rate-limit risk is low.

I'll include it as LOW.

OK, final output:

```
MEDIUM|cmd/atcr/github.go:222|Fallback loop does not check context cancellation between iterations|Add ctx.Err() check before each CreateReviewComment call to fail fast on cancellation|correctness|5|for _, c := range comments { client.CreateReviewComment(cmd.Context(), ...)|bruce
LOW|cmd/atcr/github_test.go:441|Hard-error test does not verify error is codedError with exitFailure|Use errors.As to assert codedError and check code == exitFailure|maintainability|5|_, _, err := postInlineComments(...); require.Error(t, err)|bruce
LOW|cmd/atcr/github.go:236|Log says "posted N via per-comment fallback" even when N=0 all-skipped|Only emit log when posted > 0, or rephrase to "fallback: 0 posted, N skipped"|maintainability|2|fmt.Fprintf(cmd.OutOrStdout(), "posted %d inline comment(s) ...|bruce
LOW|cmd/atcr/github.go:222|Fallback loop has no inter-call delay, regressing rate-limit safety from original 100ms delay|Add 100ms sleep between CreateReviewComment calls in fallback path|performance|3|for _, c := range comments { client.CreateReviewComment(cmd.Context(), ...)|bruce
```

Hmm, actually, the line numbers I have are estimates. Let me use more general references.

Let me also double-check my findings against the scope:

1. Context cancellation - in `postCommentsIndividually` in `cmd/atcr/github.go` - IN SCOPE
2. Test gap - in `cmd/atcr/github_test.go` - IN SCOPE (test updates)
3. Log message - in `cmd/atcr/github.go` - IN SCOPE
4. Rate-limit delay - in `postCommentsIndividually` in `cmd/atcr/github.go` - DEBATABLE (plan says rate-limit is out of scope)

I'll include all four. The rate-limit one is a real regression.

Actually, on further thought, the rate-limit issue might be more impactful than I initially thought. For a PR with 50 findings, the fallback path would make 50 sequential API calls with no delay. If the user is running this in a CI loop, this could quickly hit rate limits. The original code had a 100ms delay for this reason. The new code is supposed to fix the "API-call-heavy" problem by using batch, but the fallback is a regression.

I'll keep it as LOW but make the evidence clearer.

OK, final output. Let me format it correctly.
</think>

MEDIUM|cmd/atcr/github.go:222|Fallback per-comment loop has no context-cancellation check between iterations|Add ctx.Err() check at the top of the loop to fail fast on cancellation|correctness|5|for _, c := range comments { client.CreateReviewComment(cmd.Context(), ...)|bruce
LOW|cmd/atcr/github_test.go:441|Hard-error propagation test does not verify error is a codedError with exitFailure|Use errors.As to assert *codedError and check code == exitFailure, and assert posted==0|maintainability|5|_, _, err := postInlineComments(...); require.Error(t, err)|bruce
LOW|cmd/atcr/github.go:236|Posted-count log is emitted even when posted==0 (all comments 422-skipped) which reads as "posted 0"|Only log when posted > 0, or rephrase to "fallback: 0 posted, N skipped"|maintainability|2|fmt.Fprintf(cmd.OutOrStdout(), "posted %d inline comment(s) ... via per-comment fallback|bruce
LOW|cmd/atcr/github.go:222|Fallback per-comment loop has no inter-call delay, regressing the rate-limit safety the original 100ms sleep provided|Add a 100ms sleep between CreateReviewComment calls in the fallback path|performance|3|for _, c := range comments { client.CreateReviewComment(cmd.Context(), ...)|bruce