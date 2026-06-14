<think>
Let me analyze this diff carefully. The sprint plan is about wiring `SnapshotFor` result fields into the manifest review stage. Let me review the changes:

1. `internal/payload/manifest.go` - Added `SnapshotMode`, `HeadSHA`, `SnapshotWorktreePath` fields to `ReviewStage` struct
2. `internal/fanout/review.go` - Modified `ExecuteReview` to capture snapshot provenance and stamp it onto the review stage; added `snapshotManifestFields` helper
3. `internal/fanout/manifest_review_test.go` - Added tests for `snapshotManifestFields`
4. `internal/payload/manifest_review_test.go` - Added tests for manifest serialization
5. `internal/fanout/engine_e2e_test.go` - Added E2E test assertions

Let me look for issues:

**Issue 1: In `internal/fanout/review.go`**
The `snapshotManifestFields` function uses `root == repo` to determine "live" vs "worktree" mode. This is a simple string comparison, but paths could be normalized differently (e.g., trailing slashes, symlinks, `.` vs absolute paths). The `SnapshotFor` function likely returns the root in a consistent way, but this is a potential issue.

**Issue 2: In `internal/fanout/review.go`**
When `SnapshotFor` fails (error case), the code logs a warning and the snapshot fields remain zero values. The `SnapshotMode` will be `""` (empty string), which with `omitempty` will be omitted from JSON. This seems intentional per the comment. However, if `SnapshotFor` succeeds but returns a root that happens to equal `p.Repo`, the `HeadSHA` will still be recorded even though it wasn't a true "live" snapshot. Wait, the comment says "root == repo is the live fast path (head matched HEAD on a clean worktree)". But the code doesn't check if head matched HEAD - it just checks if root == repo. This is based on the assumption that `SnapshotFor` returns `repo` as root only in the live case. This is fine as long as `SnapshotFor` behavior is guaranteed.

**Issue 3: The `head` parameter**
The `snapshotManifestFields` function takes a `head` parameter. The comment says "head is already the resolved head SHA (gitrange resolves it before fan-out), recorded verbatim as head_sha." But looking at the call site, it passes `p.Head`. If `p.Head` is not yet resolved, this could be an issue. But the comment asserts it's already resolved. This is a documentation/trust issue.

**Issue 4: Error handling in `internal/fanout/review.go`**
The `cleanup()` function from `SnapshotFor` is deferred, but what if the review panics or the context is cancelled? The deferred cleanup should handle that. This seems fine.

**Issue 5: The `SnapshotMode` field uses `omitempty` but the test expects it to be present**
Looking at the test `TestManifest_ReviewStage_SnapshotLiveMode`, it checks for the presence of `snapshot_worktree_path` but not `snapshot_mode`. The `snapshot_mode` field uses `omitempty`, so when it's "live" (non-empty), it will be present. The `snapshot_worktree_path` does NOT use `omitempty`, so it will always be present (as "" in live mode). This seems correct and intentional.

**Issue 6: In `internal/fanout/engine_e2e_test.go`**
The test asserts `assert.Equal(t, "worktree", review.SnapshotMode)` and the comment says this is because the tree is dirty. But this test is environment-dependent - it assumes the test environment creates untracked files in `.atcr/reviews/`. If the test runs in a different order or the cleanup doesn't work, this could be flaky. However, this is an E2E test, so some environment dependency is expected.

**Issue 7: Path comparison in `snapshotManifestFields`**
The function compares `root == repo` as strings. If `SnapshotFor` returns a normalized path (e.g., cleaned, absolute) and `p.Repo` is in a different form, the comparison could fail. This is a potential correctness issue. However, if both come from the same source (the repo path), they should match. This is a minor concern.

**Issue 8: The `HeadSHA` field is recorded even when not a true snapshot**
Wait, looking more carefully at the code: `snapMode, snapHeadSHA, snapWorktreePath` are only set when `SnapshotFor` succeeds. If it fails, they remain zero values, and with `omitempty`, `SnapshotMode` and `HeadSHA` will be omitted from JSON. `SnapshotWorktreePath` will be `""` (not omitted). This means if no snapshot ran, the manifest will have `"snapshot_worktree_path": ""` which could be misleading - a reader might think it was a live snapshot when no snapshot actually ran. This is a potential correctness/clarity issue.

Wait, let me re-read the comment: "When no snapshot ran (snapshot failed, or no tool agent), the omitempty snapshot_mode/head_sha drop out and snapshot_worktree_path stays ""." So the intent is that when no snapshot ran, `snapshot_mode` and `head_sha` are absent, and `snapshot_worktree_path` is `""`. But this means a reader can't distinguish "live mode" from "no snapshot ran" based on `snapshot_worktree_path` alone - they need to check if `snapshot_mode` is present. The `TestManifest_ReviewStage_SnapshotLiveMode` test specifically checks that `snapshot_worktree_path` is present as `""` in live mode, which is correct. But the design choice means that in the "no snapshot ran" case, the manifest also has `snapshot_worktree_path: ""` which is the same as live mode. A reader must check for the presence/absence of `snapshot_mode` to distinguish.

This is a design choice that's documented in the comments, so it's not a bug per se, but it's a potential source of confusion.

**Issue 9: Thread safety**
The `ExecuteReview` function modifies `p.manifest.Review` after the fan-out. Since this is a single goroutine (the review is sequential), this should be fine.

**Issue 10: The `SnapshotFor` cleanup**
The `defer cleanup()` is called inside the `if anyToolAgent(p.Slots) && p.Head != ""` block. If `SnapshotFor` succeeds but the jail creation fails, the cleanup is still deferred. This seems correct.

**Issue 11: In `internal/fanout/manifest_review_test.go`**
The test `TestSnapshotManifestFields_LiveMode` calls `snapshotManifestFields("/repo", "/repo", "abc1234")`. But `snapshotManifestFields` is a package-level function in `internal/fanout/review.go`. The test is in `internal/fanout/manifest_review_test.go`, which is the same package. This should work fine.

**Issue 12: In `internal/payload/manifest_review_test.go`**
The `reviewBlock` helper function reads the manifest JSON and extracts the review block. The `readManifestJSON` function is used - I need to check if this is defined elsewhere. It's likely a test helper. This should be fine.

Let me focus on the most significant issues:

1. **Path comparison fragility**: The `root == repo` string comparison in `snapshotManifestFields` could be fragile if paths are not normalized consistently. This is a correctness concern.

2. **Ambiguous manifest state**: When no snapshot ran, `snapshot_worktree_path` is `""` which is the same as live mode. A reader must check `snapshot_mode` presence to distinguish. This is a design choice but could be confusing.

3. **The `head` parameter trust**: The code trusts that `p.Head` is already resolved. If it's not, the recorded `head_sha` could be wrong (e.g., a branch name instead of a SHA).

4. **In the E2E test**: The test assumes a dirty worktree, which is environment-dependent. The comment explains why, but this makes the test potentially flaky.

Let me look at this more carefully. The E2E test in `engine_e2e_test.go` line 173+:
- The comment says "PrepareReview has already written the untracked .atcr/reviews/ scaffolding into the repo, so git status --porcelain reports a dirty tree and SnapshotFor falls through to the slow path."
- The test then asserts `assert.Equal(t, "worktree", review.SnapshotMode)`.
- This means the test is asserting a specific behavior that depends on the state of the worktree during the test.

This is a potential issue because:
- If `PrepareReview` doesn't always write untracked files (e.g., in a clean test environment), the test would fail.
- The test is in a `// AC 03-02 Scenario 5 + AC 03-03 Scenario 4 (worktree branch)` block, but the assertion is for the worktree branch specifically.

Wait, looking at the sprint plan: "AC 03-03 Scenarios 4-5 require test assertions that verify the fields for both live and worktree modes." So the E2E test is testing the worktree mode case. The `TestSnapshotManifestFields_LiveMode` and `TestSnapshotManifestFields_WorktreeMode` tests in `manifest_review_test.go` test the helper function. The `TestManifest_ReviewStage_SnapshotWorktreeMode` and `TestManifest_ReviewStage_SnapshotLiveMode` in `payload/manifest_review_test.go` test the serialization.

So the E2E test is specifically for the worktree case. The comment explains why it's worktree (dirty tree). This seems acceptable for an E2E test.

Let me also check: in the `ExecuteReview` function, the snapshot fields are stamped onto `p.manifest.Review` after `reviewStageFor(results)` is called. But `reviewStageFor` creates a new `ReviewStage` struct. The fields are then set on that new struct. This is fine.

One more thing: the `snapshotManifestFields` function is defined at the bottom of `review.go`. The function is called from `ExecuteReview`. This is fine - Go doesn't require forward declarations.

Let me also check: the `SnapshotMode` field uses `omitempty`. When the snapshot fails, `snapMode` is `""`, so the field is omitted. When the snapshot succeeds and is live, `snapMode` is `"live"`, so it's present. When the snapshot succeeds and is worktree, `snapMode` is `"worktree"`, so it's present. This is correct.

The `HeadSHA` field uses `omitempty`. Same logic. Correct.

The `SnapshotWorktreePath` field does NOT use `omitempty`. So:
- No snapshot: `""` (present)
- Live snapshot: `""` (present)
- Worktree snapshot: the path (present)

This means a reader can distinguish "no snapshot" from "live snapshot" only by checking if `snapshot_mode` is present. The `TestManifest_ReviewStage_SnapshotLiveMode` test specifically checks that `snapshot_worktree_path` is present as `""` in live mode. But it doesn't test the "no snapshot" case. The existing test `TestManifest_ReviewStageOmittedWhenNil` tests the case where `Review` is nil entirely. But there's no test for the case where `Review` is non-nil but no snapshot ran (i.e., `SnapshotMode` is `""` and `SnapshotWorktreePath` is `""`).

Actually, wait - if `Review` is non-nil and no snapshot ran, the manifest will have `"snapshot_worktree_path": ""` but no `snapshot_mode` or `head_sha`. This is the "no snapshot" case. A reader checking for live mode would see `snapshot_mode` is absent and correctly determine no snapshot ran. So the design is actually clear if you follow the rule: "live mode requires `snapshot_mode: \"live\"` to be present."

But the design choice means that the `snapshot_worktree_path: ""` field is always present when `Review` is non-nil, which adds noise to the manifest. This is a minor maintainability concern.

Let me now look at the most significant issues to report:

1. **Path comparison in `snapshotManifestFields`**: The `root == repo` comparison is fragile. If paths are not normalized identically (e.g., one has a trailing slash, or one is a symlink), the comparison could fail and incorrectly classify a live snapshot as worktree or vice versa. This is a correctness issue.

2. **No test for the "no snapshot ran" manifest case**: The tests cover live and worktree modes, but not the case where `Review` is non-nil but no snapshot ran (e.g., snapshot failed, or no tool agent). The manifest will have `"snapshot_worktree_path": ""` but no `snapshot_mode` or `head_sha`. This should be tested to ensure the design works as documented.

3. **E2E test environment dependency**: The E2E test relies on the worktree being dirty due to `PrepareReview` writing untracked files. If the test environment changes, this could break. This is a test reliability concern.

4. **Trust in `p.Head` being resolved**: The code records `p.Head` verbatim as `head_sha` without verifying it's a resolved SHA. The comment says it's resolved by gitrange, but there's no runtime check. If `p.Head` is ever a branch name or ref, the recorded `head_sha` would be wrong.

Let me also check: in the `ExecuteReview` function, the snapshot fields are only set when `SnapshotFor` succeeds. But what if `SnapshotFor` succeeds and returns `root == repo` (live mode), but the jail creation fails? The comment says "recorded even if the jail below fails." This is correct - the fields are set before the jail check.

Actually, wait. Let me re-read the code:
```go
if root, cleanup, err := tools.NewSnapshotManager(p.Repo).SnapshotFor(p.Head); err != nil {
    fmt.Fprintf(...)
} else {
    defer cleanup()
    snapMode, snapHeadSHA, snapWorktreePath = snapshotManifestFields(root, p.Repo, p.Head)
    if jail, jerr := tools.NewJail(root); jerr != nil {
        ...
    } else {
        ...
    }
}
```

So if `SnapshotFor` succeeds, the fields are set. If the jail fails, the fields are still set. The fields are then stamped onto the manifest. This is correct.

But there's a subtle issue: if `SnapshotFor` succeeds and returns `root == repo` (live mode), the code uses `root` as the jail root. The jail is then created at the repo root. This means in live mode, the jail operates on the actual repo, not a worktree. This is the intended behavior for the fast path.

Let me now finalize my findings. The most significant issues are:

1. **Path comparison fragility** in `snapshotManifestFields` - This is a correctness concern.
2. **Missing test for "no snapshot" case** - This is a maintainability/test coverage concern.
3. **E2E test environment dependency** - This is a test reliability concern.

Let me also check if there are any issues with the `omitempty` design. The `SnapshotWorktreePath` field does NOT use `omitempty`, which means it's always present when `Review` is non-nil. This is intentional per the comment, but it means:
- No snapshot: `"snapshot_worktree_path": ""`
- Live snapshot: `"snapshot_worktree_path": ""`
- Worktree snapshot: `"snapshot_worktree_path": "/path/to/worktree"`

A reader checking for live mode must look at `snapshot_mode` presence, not `snapshot_worktree_path` value. This is documented but could be confusing.

Actually, I think the most important issue is the path comparison. Let me verify by looking at how `SnapshotFor` works. The sprint plan says "`internal/tools/snapshot.go` is complete and tested" and is out of scope. So I should trust that `SnapshotFor` returns the repo path for the live case. The `root == repo` comparison is then correct.

But wait - what if `p.Repo` has a trailing slash and `SnapshotFor` returns the cleaned path? Or vice versa? This is a real concern. However, without seeing `SnapshotFor`'s implementation, I can only flag it as a potential issue.

Let me also consider: the `snapshotManifestFields` function is a simple helper. The real question is whether `SnapshotFor` guarantees that when it returns the repo path, the comparison will be exact. If `p.Repo` is passed to `NewSnapshotManager` and then `SnapshotFor` returns a path derived from it, they should be identical. But if there's any normalization, they could differ.

Given that the sprint plan says `SnapshotFor` is complete and tested, and this is out of scope, I should be cautious about flagging issues with the path comparison. However, it's still a valid concern for the helper function that derives the mode.

Let me finalize my findings:

1. **MEDIUM**: Path comparison in `snapshotManifestFields` could be fragile if paths are normalized differently.
2. **MEDIUM**: Missing test for the "no snapshot ran" case in `internal/payload/manifest_review_test.go`.
3. **LOW**: E2E test relies on dirty worktree state which is environment-dependent.

Wait, let me also check: in the `ExecuteReview` function, the `cleanup()` is deferred inside the `else` block. This means if the function returns early before reaching this point (e.g., due to an error in the code above), the cleanup won't be deferred. But looking at the code structure, the `SnapshotFor` call is the first thing in the `if anyToolAgent(p.Slots) && p.Head != ""` block. There's no early return before it. So this is fine.

Actually, I just realized: the `defer cleanup()` is inside the `else` block of the `if err != nil` check. If `SnapshotFor` fails, `cleanup` is not deferred. But `cleanup` is the second return value of `SnapshotFor`, which is only valid when `err` is nil. So this is correct.

Let me also check: the `SnapshotMode` field uses `omitempty`. The test `TestManifest_ReviewStage_SnapshotLiveMode` checks that `snapshot_worktree_path` is present but doesn't check that `snapshot_mode` is present (it just unmarshals it). This is fine because `snapshot_mode: "live"` is non-empty, so `omitempty` doesn't omit it.

One more thing: the `SnapshotMode` field is a `string` with `omitempty`. The comment says "Omitted (via omitempty) when no snapshot ran". But `string` with `omitempty` in Go's `encoding/json` omits the field when the string is empty (`""`). So when `snapMode` is `""` (no snapshot), the field is omitted. This is correct.

I think my top findings are:
1. Path comparison fragility (MEDIUM, correctness)
2. Missing test for "no snapshot" case (LOW, maintainability)

The E2E test environment dependency is a design choice for E2E tests and is documented, so I'll flag it as LOW.

Let me also consider: the `head` parameter in `snapshotManifestFields` is used verbatim as `headSHA`. The comment says it's already resolved. But if it's not resolved, the recorded value would be wrong. This is a correctness concern, but it's about the contract between the caller and the helper. The comment documents the assumption, so it's acceptable.

Actually, I just noticed something: in the `ExecuteReview` function, the `snapMode, snapHeadSHA, snapWorktreePath` variables are declared at the top of the function (after the comment), and then assigned in the `else` block. The `p.manifest.Review` is assigned after the fan-out. The fields are then stamped onto `p.manifest.Review`. This is correct.

But wait - the variables are declared with `var snapMode, snapHeadSHA, snapWorktreePath string`. The zero value of a string is `""`. So when `SnapshotFor` fails or doesn't run, the fields are `""`. With `omitempty`, `SnapshotMode` and `HeadSHA` are omitted. `SnapshotWorktreePath` is `""` (not omitted). This matches the comment.

One more concern: the `ReviewStage` struct has `SnapshotMode`, `HeadSHA`, and `SnapshotWorktreePath` fields. But the `reviewStageFor` function creates a new `ReviewStage` with only `Agents`, `ToolsEnabled`, and `ToolsDegraded`. The new fields are then set after the function returns. This is fine because the fields are zero-valued in the struct returned by `reviewStageFor`.

Let me also check: the `p.manifest.Review` is set to the result of `reviewStageFor(results)`. Then the snapshot fields are stamped. The `WriteManifest` function is then called. This is correct.

I think I've covered the main issues. Let me write the findings.

Actually, one more thing: the `SnapshotFor` function returns `(root, cleanup, err)`. The `root` is the filesystem root the snapshot was taken at. The `cleanup` is a function to clean up the snapshot. The `err` is any error. The code checks `err` and logs a warning if it's non-nil. If it's nil, the code defers `cleanup` and proceeds.

But what if `SnapshotFor` returns `root == ""` and `err == nil`? This seems unlikely but possible. In that case, `root == p.Repo` would be `false` (assuming `p.Repo` is non-empty), and the mode would be `"worktree"` with the empty root as the worktree path. This would be incorrect. However, this is a contract issue with `SnapshotFor`, not with the code being reviewed.

Also, what if `p.Repo` is empty? Then `root == p.Repo` would be `root == ""`. If `SnapshotFor` returns `""` for the live case, the mode would be `"live"`. But if `p.Repo` is empty, the whole review is likely misconfigured. This is an edge case that's probably handled elsewhere.

I'll flag the path comparison as a MEDIUM concern with a note about normalization.

Let me also look at the test file `internal/fanout/manifest_review_test.go`. The new tests `TestSnapshotManifestFields_LiveMode` and `TestSnapshotManifestFields_WorktreeMode` test the helper function directly. They pass exact path strings. This is fine for unit tests of the helper.

The E2E test in `engine_e2e_test.go` tests the full pipeline. It asserts `"worktree"` mode because the worktree is dirty. This is documented in the comment.

The payload tests in `internal/payload/manifest_review_test.go` test the JSON serialization. They check that `snapshot_worktree_path` is present as `""` in live mode.

I think the most actionable finding is the missing test for the "no snapshot ran" case. This is a gap in test coverage that could be easily fixed.

Let me also consider: the `SnapshotMode` field uses `omitempty`. In the test `TestManifest_ReviewStage_SnapshotLiveMode`, the test checks that `snapshot_worktree_path` is present but doesn't explicitly check that `snapshot_mode` is present. However, the test unmarshals `review["snapshot_mode"]` and asserts it's `"live"`. If the field were omitted, the unmarshal would fail (or return an empty string, and the assertion would fail). So the test implicitly checks that `snapshot_mode` is present. This is fine.

Actually, let me re-read the test:
```go
require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))
```
If `review["snapshot_mode"]` is `nil` (field omitted), `json.Unmarshal(nil, &mode)` would return an error: "unexpected end of JSON input". So the `require.NoError` would fail. This means the test does check that `snapshot_mode` is present. Good.

But wait - what about the case where `Review` is non-nil but no snapshot ran? In that case, `snapshot_mode` would be `""` and omitted. The test `TestManifest_ReviewStage_SnapshotLiveMode` doesn't cover this case. The test `TestManifest_ReviewStageOmittedWhenNil` covers the case where `Review` is nil. But there's no test for the case where `Review` is non-nil with no snapshot fields. This is a gap.

OK, I think I have enough findings. Let me write them up.

Findings:
1. MEDIUM - Path comparison in `snapshotManifestFields` could be fragile
2. MEDIUM - Missing test for "no snapshot ran" case in manifest
3. LOW - E2E test depends on dirty worktree state

Let me also check: in the `internal/payload/manifest.go` change, the `SnapshotWorktreePath` field does NOT use `omitempty`. The comment explains this is intentional. But it means the field is always present when `Review` is non-nil. This is a design choice. The sprint plan says "Other AC items from sprint 2.0" are out of scope, and "Changes to the manifest schema beyond the three new fields" are out of scope. The three new fields are within scope. The `omitempty` choice is a design decision for these fields, so it's within scope to review.

I think the design choice of NOT using `omitempty` for `SnapshotWorktreePath` is correct based on the AC requirement: "AC 03-03 Scenario 5 requires \"snapshot_worktree_path\": \"\" to be present in live mode so a reader distinguishes live from a missing field." Wait, that says "distinguishes live from a missing field". But if no snapshot ran, the field is also `""`. So the reader can't distinguish "live" from "no snapshot" based on `snapshot_worktree_path` alone. They need to check `snapshot_mode` presence. The AC requirement is about distinguishing live from a missing field, not live from no-snapshot.

Actually, re-reading: "distinguishes live from a missing field" - this means in live mode, the field is present (as `""`), not missing. If the field were omitted in live mode, a reader couldn't tell if it was live or if the field was simply not recorded. By having it present as `""` in live mode, the reader knows it was explicitly recorded as live.

But the "no snapshot ran" case also has `snapshot_worktree_path: ""`. So the reader still can't distinguish "live" from "no snapshot" based on this field alone. They need to check `snapshot_mode`. This is a design limitation.

Is this a bug? The comment in `review.go` says: "When no snapshot ran (snapshot failed, or no tool agent), the omitempty snapshot_mode/head_sha drop out and snapshot_worktree_path stays ''." So the design is:
- No snapshot: `snapshot_mode` absent, `head_sha` absent, `snapshot_worktree_path: ""`
- Live: `snapshot_mode: "live"`, `head_sha: "..."`, `snapshot_worktree_path: ""`
- Worktree: `snapshot_mode: "worktree"`, `head_sha: "..."`, `snapshot_worktree_path: "..."`

A reader can distinguish these by checking the presence of `snapshot_mode`. If absent, no snapshot. If present, check the value for live vs worktree.

This is a valid design. The `snapshot_worktree_path` being `""` in both "no snapshot" and "live" cases is not a bug - it's just that the field is always present when `Review` is non-nil. The distinguishing factor is `snapshot_mode` presence.

OK, I think the design is sound. Let me finalize my findings.

One more thing: the `TestSnapshotManifestFields_LiveMode` and `TestSnapshotManifestFields_WorktreeMode` tests in `internal/fanout/manifest_review_test.go` test the helper function. But they don't test the case where `root != repo` and `root == ""` (empty worktree path). This is an edge case where `SnapshotFor` might return an empty path. The helper would classify this as "worktree" mode with an empty path. This could be a bug if `SnapshotFor` ever returns an empty path on success. But since `SnapshotFor` is out of scope and complete, I'll flag this as a LOW concern.

Actually, I think I'm overcomplicating this. Let me focus on the most important findings:

1. **Path comparison fragility** - The `root == repo` comparison is a string equality check. If `SnapshotFor` returns a normalized path that differs from `p.Repo` (e.g., trailing slash, symlink resolution), the comparison could fail. This is a correctness concern.

2. **Missing test for "no snapshot" manifest case** - There's no test for the case where `Review` is non-nil but no snapshot ran. The manifest will have `"snapshot_worktree_path": ""` but no `snapshot_mode` or `head_sha`. This should be tested.

3. **E2E test environment dependency** - The E2E test relies on the worktree being dirty. This is documented but makes the test fragile.

Let me write these up.

Wait, I should also check: the `SnapshotMode` field is a `string`. The comment says it's `"live"` or `"worktree"`. The test asserts these exact values. But what if `SnapshotFor` returns a root that doesn't match `p.Repo` but is also not a worktree? The code would classify it as "worktree" with that root as the path. This could be incorrect if the root is something else (e.g., an error recovery path). But since `SnapshotFor` is complete and tested, this is out of scope.

OK, final findings:

1. MEDIUM - `snapshotManifestFields` path comparison fragility
2. MEDIUM - Missing test for "no snapshot ran" manifest case
3. LOW - E2E test environment dependency

Let me also consider: the `head` parameter in `snapshotManifestFields` is used verbatim. The comment says it's already resolved. But if `p.Head` is a branch name, the recorded `head_sha` would be a branch name, not a SHA. This violates the field's name. This is a correctness concern, but it's about the contract with the caller. I'll flag it as LOW.

Actually, I realize I should also check: the `p.manifest.Review` could be nil (from `reviewStageFor` returning nil if no agents ran with tools). The code checks `if p.manifest.Review != nil` before stamping the fields. This is correct.

But wait - the `reviewStageFor` function returns `*payload.ReviewStage`. It could return nil. The code handles this correctly.

OK, I think I have enough. Let me write the findings in the required format.

Let me reconsider the path comparison. In the `snapshotManifestFields` function:
```go
if root == repo {
    return "live", head, ""
}
return "worktree", head, root
```

The `root` is returned by `SnapshotFor`. The `repo` is `p.Repo`. Both are passed to `NewSnapshotManager` and then to `SnapshotFor`. If `SnapshotFor` returns the same path it was given (or a cleaned version of it), the comparison should work. But if there's any normalization difference, it could fail.

For example, if `p.Repo` is `"/home/user/repo/"` (trailing slash) and `SnapshotFor` returns `"/home/user/repo"` (cleaned), the comparison would fail and incorrectly classify it as worktree mode.

This is a real concern. The fix would be to use `filepath.Clean` on both sides before comparing, or to use a more robust comparison method.

Let me finalize:

1. MEDIUM - `internal/fanout/review.go:665` - Path comparison in `snapshotManifestFields` could be fragile if paths are normalized differently
2. MEDIUM - `internal/payload/manifest_review_test.go` - Missing test for the "no snapshot ran" case
3. LOW - `internal/fanout/engine_e2e_test.go:183` - E2E test relies on dirty worktree state (environment-dependent)

Let me also double-check: the `snapshotManifestFields` function is at line 665 in the diff (added lines). The function signature is:
```go
func snapshotManifestFields(root, repo, head string) (mode, headSHA, worktreePath string) {
```

This is in `internal/fanout/review.go`.

The missing test for "no snapshot" case would go in `internal/payload/manifest_review_test.go`.

The E2E test is in `internal/fanout/engine_e2e_test.go` around line 173-192.

Let me write the final findings.

Actually, I want to reconsider the path comparison issue. The `SnapshotFor` function is described in the sprint plan as "complete and tested" and is out of scope. The `snapshotManifestFields` function is a new helper that derives the mode from the root. The comparison `root == repo` is the simplest possible check.

In Go, the convention is to use `filepath.Clean` or `filepath.Abs` to normalize paths before comparison. If `SnapshotFor` returns a cleaned path and `p.Repo` is not cleaned, the comparison could fail. This is a real concern.

However, I should also consider: the `p.Repo` is likely set once and passed to `NewSnapshotManager`. If the codebase consistently uses `filepath.Clean` or similar, the comparison would work. Without seeing the rest of the codebase, I can't be sure.

I'll flag this as a MEDIUM concern with a specific suggestion: use `filepath.Clean` on both sides, or use a more robust comparison.

Final findings:

1. MEDIUM - `internal/fanout/review.go:665` - Path string comparison `root == repo` could be fragile if paths are normalized differently (e.g., trailing slashes, symlinks)
2. MEDIUM - `internal/payload/manifest_review_test.go` - No test for the "no snapshot ran" case (Review non-nil but no snapshot fields)
3. LOW - `internal/fanout/engine_e2e_test.go:183` - E2E test asserts "worktree" mode based on dirty worktree state, which is environment-dependent

Let me write these in the required format.

Actually, I want to also flag: the `head` parameter is used verbatim as `head_sha`. The field name implies a SHA. If `p.Head` is a branch name or tag, the recorded value would be incorrect. The comment says it's resolved, but there's no runtime check. This is a LOW concern.

Also: the `SnapshotMode` field is a plain `string`. There's no validation that it's only `"live"` or `"worktree"`. The helper function only returns these two values, so it's fine. But if someone sets the field directly (e.g., in tests or future code), invalid values could be written. This is a LOW concern.

OK, I think 3-5 findings is appropriate. Let me finalize.

Final list:
1. MEDIUM - Path comparison fragility in `snapshotManifestFields`
2. MEDIUM - Missing test for "no snapshot" manifest case
3. LOW - E2E test environment dependency
4. LOW - `head` parameter used verbatim without validation

Let me write them out.
</think>

MEDIUM|internal/fanout/review.go:665|Path string comparison `root == repo` is fragile if paths are normalized differently (trailing slashes, symlinks, relative vs absolute)|Normalize both paths with `filepath.Clean` (and `filepath.EvalSymlinks` if applicable) before comparing, or compare canonicalized forms|maintainability|10|func snapshotManifestFields(root, repo, head string) — simple string equality on filesystem paths|bruce
MEDIUM|internal/payload/manifest_review_test.go:0|No test covers the "no snapshot ran" case where `Review` is non-nil but `SnapshotMode`/`HeadSHA` are empty (snapshot failed or no tool agent)|Add a test asserting `snapshot_mode` and `head_sha` are absent from JSON while `snapshot_worktree_path` is present as "" in that case|maintainability|15|Tests cover live and worktree modes but not the "no snapshot ran" edge case documented in review.go:347|bruce
LOW|internal/fanout/engine_e2e_test.go:183|E2E test asserts `"worktree"` mode because `PrepareReview` dirties the tree; this makes the test environment-dependent and brittle to test-order or repo-state changes|Add a setup step that explicitly dirties the tree (or use a separate test for live mode that ensures a clean tree) to remove the implicit dependency|maintainability|20|Comment notes "PrepareReview has already written the untracked .atcr/reviews/ scaffolding" — implicit dirty-state assumption|bruce
LOW|internal/fanout/review.go:668|`head` parameter is recorded verbatim as `head_sha` with no runtime check that it's a resolved SHA; a branch name or ref would violate the field's semantic|Add a `git.IsHash` check (or resolve via `gitrange`) before stamping, or assert in a debug build|maintainability|10|Comment says "gitrange resolves it before fan-out" but there is no runtime enforcement|bruce