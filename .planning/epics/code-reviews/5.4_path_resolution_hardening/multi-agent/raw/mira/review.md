<think>
Let me analyze this diff carefully for the Epic 5.4 Path-Resolution Hardening sprint plan. I need to focus on issues in the files mentioned in the sprint plan and address concerns within the sprint's stated goals.

Files in scope:
- internal/stream/validate.go
- internal/stream/* (new) - candidate index + Levenshtein helper
- internal/reconcile/validate.go
- internal/reconcile/emit.go
- internal/report/render.go
- *_test.go for these

Let me review the changes:

1. **internal/reconcile/emit.go** - Adds PathSuggestion to JSONFinding, updates RenderMarkdown to show "(did you mean ...)". Looks fine.

2. **internal/stream/parser.go** - Adds PathSuggestion field to Finding struct.

3. **internal/stream/validate.go** - Major refactor:
   - Changed signature: `ValidatePath(f *Finding, root string, idx *FileIndex)`
   - Added symlink-safe existence check via EvalSymlinks
   - Added Tier 3 (case-only) check
   - Uses tier 1/2 for missing suggestions

4. **internal/stream/fileindex.go** - New file with BuildFileIndex function
5. **internal/stream/levenshtein.go** - New file with levenshtein and similarity functions
6. **internal/stream/suggest.go** - New file with tier1, tier2, CaseCorrection logic
7. **internal/reconcile/validate.go** - Updates to build index once per run
8. **internal/report/render.go** - Updates writePathWarning

Let me look for actual issues:

### Issue 1: Case-folded lookup uses simple lowercase which is incorrect for non-ASCII
In `fileindex.go`:
```go
fold := strings.ToLower(rel)
```
Using `strings.ToLower` (simple Unicode lowering) is acceptable for ASCII but doesn't handle Unicode case folding properly. For Turkish "İ" -> "i̇" or German ß -> SS, the case fold may not be consistent. This is a minor correctness issue but might not be a real problem for Go file paths which are typically ASCII.

Actually, the bigger issue is that `strings.ToLower` doesn't handle Unicode properly for case folding. For example, the German ß (sharp s) uppercases to "SS" but lowercases to itself. But this is really a minor edge case for file paths.

### Issue 2: Levenshtein implementation - the swap of `prev, curr = curr, prev` could have a bug
Looking at the Levenshtein code more carefully:
```go
for i := 1; i <= len(ra); i++ {
    curr[0] = i
    for j := 1; j <= len(rb); j++ {
        cost := 1
        if ra[i-1] == rb[j-1] {
            cost = 0
        }
        curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
    }
    prev, curr = curr, prev
}
return prev[len(rb)]
```

Wait, there's a potential issue: when we swap `prev, curr = curr, prev`, on the next iteration, `curr[0] = i` overwrites what was previously stored. But we should be writing the new row's first element. After swap, `prev` is the row we just computed (the "current" computation result), and `curr` is what was previously `prev` (the old "prev"). The next iteration sets `curr[0] = i+1` which is the new deletion count for the first row. But wait - the swap makes prev point to the newly computed row, and curr points to the old prev row. In the next iteration, we set `curr[0] = i+1` which sets the first element of the OLD prev row (which is now `curr`). That's fine because we're going to overwrite it anyway. 

Actually wait, this is correct. After swap:
- prev now holds the just-computed row (row i)
- curr now holds the old row i-1 (which we don't need anymore)

Then for row i+1:
- curr[0] = i+1 (correct, the new "previous" row starts at i+1 because we have i+1 deletions from ra to "")
- prev[j] correctly refers to the row i values
- prev[j-1] correctly refers to the row i values (diagonal)
- curr[j-1] correctly refers to row i+1 values computed so far

Yes, this is correct.

### Issue 3: Tier 2 extension guard may be wrong
In `suggest.go`:
```go
citedStem, citedExt := splitStem(base)
...
candStem, candExt := splitStem(cand)
if candExt != citedExt {
    continue
}
```

This requires the extension to match. But what if the cited path has no extension (just a filename) and the candidate has one? Or vice versa? Edge case but probably fine.

### Issue 4: tier1 ignores case-different candidates that are unique
In `suggest.go` tier1:
```go
if len(candidates) == 1 {
    // Only meaningful when it is not the cited path itself; an absent path
    // is never in the tracked set, so a lone match is always elsewhere.
    if candidates[0] != rel {
        return candidates[0]
    }
    return ""
}
```

This works for Tier 1 (exact basename), but it doesn't apply Tier 3 logic. However, Tier 3 is checked first in `ValidatePath` so this is fine.

### Issue 5: prefixDerivation may incorrectly block valid typos
In `suggest.go`:
```go
func prefixDerivation(a, b string) bool {
    if a == b {
        return false
    }
    short, long := a, b
    if len(b) < len(a) {
        short, long = b, a
    }
    return strings.HasPrefix(long, short)
}
```

This says: if one is a prefix of the other, it's a derivation (not a typo). But what about cases like "parse" vs "parsex" where "parse" is a prefix? Well, that would still be a derivation/related, so maybe correct.

But what about "config" vs "configx"? Those would never be valid candidates anyway because of the similarity threshold.

The concern is: what about something like "validat" vs "validate"? "validat" is a prefix of "validate", so this would block. But "validat" alone is unlikely to be a real filename.

### Issue 6: existsContained may fail on non-existent file
Looking at `existsContained`:
```go
func existsContained(root, joined string) existence {
    absRoot, err := filepath.Abs(root)
    if err != nil {
        absRoot = root
    }
    absJoined, err := filepath.Abs(joined)
    if err != nil {
        absJoined = joined
    }
    realRoot, err := filepath.EvalSymlinks(absRoot)
    if err != nil {
        realRoot = absRoot // best effort; containment still re-checked below
    }
    resolved, err := filepath.EvalSymlinks(absJoined)
    switch {
    case err == nil:
        rel, rerr := filepath.Rel(realRoot, resolved)
        if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
            return existsOutsideOrAbsent // escaped the repo via a symlink
        }
        return existsInside
    case os.IsNotExist(err):
        return existsOutsideOrAbsent
    default:
        return existsIndeterminate
    }
}
```

Note: `EvalSymlinks` returns an error if ANY segment of the path doesn't exist. So for a missing file, `resolved` will have `os.IsNotExist(err) == true` and we return `existsOutsideOrAbsent`. Good.

But what if the path partially exists - like `internal/auth/validator.go` where `internal/auth/` exists but `validator.go` doesn't? `EvalSymlinks` will return ENOENT, so `os.IsNotExist(err)` will catch it.

### Issue 7: buildFileIndex leaks "existsContained" but not all of ValidatePath errors

Actually one concern: when `os.Stat` returns an indeterminate error (permission denied, I/O error), ValidatePath leaves the finding unflagged. The new code with `existsContained` returns `existsIndeterminate` which is also unflagged. But there's a subtle issue: `filepath.EvalSymlinks` for an existing file with permission issues could fail differently than `os.Stat`. Let me check.

`os.Stat` returns the file's metadata. `filepath.EvalSymlinks` resolves each symlink segment. If a file is unreadable but exists, EvalSymlinks may still succeed (it only resolves symlinks, not file access). So if you have permission issues, EvalSymlinks might succeed and return a valid path. But `os.Stat` would fail. This means the new code might report `existsInside` for a file where `os.Stat` would have returned `existsIndeterminate`. Actually, this is fine — if EvalSymlinks succeeded and the path is inside root, then the file exists.

But wait, what if the directory containing the file has a permission issue? EvalSymlinks would fail to read the directory, returning EACCES or similar. Let me check Go docs... Actually EvalSymlinks uses os.Lstat + os.Readlink, so it only reads the symlinks themselves, not the file's content. So permission issues on the file itself would still pass EvalSymlinks but os.Stat would fail with EACCES.

Actually looking more carefully: `os.Stat` on a file you don't have permission to read will still succeed if you have execute permission on the directory (you can stat but not read). So this difference is minor.

### Issue 8: ValidatePath lexical guard breaks for non-relative paths
```go
if rel, err := filepath.Rel(root, joined); err != nil ||
    rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
```

If `joined` is `/etc/hosts` and root is `/tmp/abc`, `filepath.Rel("/tmp/abc", "/etc/hosts")` returns "../../etc/hosts" which starts with "..". Good.

What if `joined` is exactly `root`? Then `rel = "."`, which is fine.

### Issue 9: Tier 3 may suggest itself
In `CaseCorrection`:
```go
func (x *FileIndex) CaseCorrection(citedRel string) (suggestion string, mismatch bool) {
    if x == nil {
        return "", false
    }
    rel := filepath.ToSlash(citedRel)
    if x.Has(rel) {
        return "", false // correctly cited, byte-exact
    }
    folded := x.ByFold(rel)
    ...
```

If citedRel is the correctly-cased tracked file, `Has` returns true and we return no mismatch. Good.

But wait - the test in `validate_test.go`:
```go
func TestValidatePath_EmptyRootDefaultsToCwd(t *testing.T) {
    f := Finding{File: "parser.go"}
    ValidatePath(&f, "", nil)
    assert.True(t, f.PathValid)
    ...
}
```

This passes the parser.go in the current directory. When root is empty, it's defaulted to ".". The file is `internal/stream/parser.go` in the test working directory? Wait, the test would need to be running from `internal/stream/` for this to pass. Or maybe it's using a different directory. Let me look at the original test...

Actually it was there before the changes - this is preserved behavior. Just noting.

### Issue 10: New line comment in validate.go claims symlink is gone
Actually let me re-read the changes to validate.go:

The old code used `os.Stat(joined)` which:
- Follows symlinks
- Is case-insensitive on macOS/Windows

The new code uses `filepath.EvalSymlinks(absJoined)` and `filepath.EvalSymlinks(absRoot)`:
- Follows symlinks but for both paths so containment can be re-checked
- Still case-insensitive on macOS/Windows for the symlink resolution

This addresses AC5 (symlink safety). 

### Issue 11: Tier 2 stem comparison - missing cap on similarity range
The `similarity` function computes `1.0 - distance/longest`. For two completely different strings (e.g., "abcdef" and "ghijkl"), distance is 6, longest is 6, so similarity is 0.0. Good.

But there's an edge case: if `longest == 0` (both empty), similarity is 1.0. Handled.

### Issue 12: tier1 may suggest ambiguous case-only paths
When tier1 has two candidates with case differences (e.g., "Parser.go" and "parser.go"), both would have the same `segOverlap` with the cited path. The `segOverlap` comparison would tie. So no suggestion. Good - falls back to Tier 3 or no suggestion.

But wait - what about when the tier1 candidates differ only in case? `ByBasename(base)` returns all relpaths with that basename, regardless of case. So "Parser.go" and "parser.go" would both match "Parser.go" lookup. This could cause issues if the cited path is "parser.go" (lowercase) and there are two files "Parser.go" and "parser.go".

But this is a case-sensitivity issue and Tier 3 handles it. So it should be OK.

### Issue 13: Levenshtein test has wrong assumption about validator/validate
The test comment correctly notes:
```
NOTE: the 5.0 plan (and this epic's Open Question 1) asserted that
validator->validate is edit distance 1; it is actually 2...
```

This is a documentation/test fix.

### Issue 14: No Suggestion for tier1 cases where the basename matches in cited directory but with different case

Looking at Tier 1 vs Tier 3 interaction:
- Tier 3 (case-only) is checked first in ValidatePath
- If case mismatch, returns immediately with case suggestion
- If case exact, falls through to existence check
- If file doesn't exist, tries Tier 1 (wrong dir), then Tier 2 (typo)

So for a path like "internal/auth/Validate.go" (capital V) where real file is "internal/auth/validate.go":
- Tier 3 catches it - returns lowercase suggestion
- No Tier 1/Tier 2 needed. Good.

For a path like "internal/auth/Validate.go" where the real file is "pkg/auth/Validate.go" (capital V, wrong dir):
- Tier 3: `internal/auth/Validate.go` folded = `internal/auth/validate.go`, no exact match in `internal/auth/`. Mismatch = false.
- Falls through to existence check: file doesn't exist.
- Tier 1: candidates for "Validate.go" = ["pkg/auth/Validate.go"]. One match. Returns it.
- Result: PathSuggestion = "pkg/auth/Validate.go" with no PathWarning? Wait, the case check returns no mismatch, so we don't set PathWarning here. Then existence check fails, sets PathWarning, then tries Tier 1.

Actually wait, the case check:
```go
if idx != nil {
    if suggestion, mismatch := idx.CaseCorrection(f.File); mismatch {
        f.PathValid = false
        f.PathWarning = PathNotFoundWarning
        f.PathSuggestion = suggestion
        return
    }
}
```

If `mismatch == false` (the file either has no case-folded match or is exact match), we fall through. Good.

But what if the cited path has no case-folded match? `mismatch = false`. Falls through. Good.

### Issue 15: Potential issue - paths in `idx.CaseCorrection` and `idx.MissingSuggestion` use different normalization

Looking at:
- `CaseCorrection`: `rel := filepath.ToSlash(citedRel)` then `x.Has(rel)` (Has also normalizes).
- `MissingSuggestion`: `rel := filepath.ToSlash(citedRel)` then various operations.

Both normalize. Good.

### Issue 16: race condition when symlink target is created during EvalSymlinks
Not really a code issue.

### Issue 17: TestValidatePath_NilSafe passes nil for idx now
The test was updated to pass `nil` for idx. With the new code, when idx is nil, the case check is skipped and Tier 1/2 don't run. So passing nil means no suggestions, which is the "graceful degradation" path. Good.

### Issue 18: For paths with backslashes (Windows-style), what happens?
On Windows, filepath.Separator is '\\'. The lexical guard uses `string(filepath.Separator)`. The `ToSlash` converts to forward slash. The tracked paths from git are always forward slash. The cited paths might be backslash on Windows. This could cause issues.

Looking at `rel = filepath.ToSlash(rel)` - good for the index.

But the existence check uses native `filepath.Abs(joined)` and `filepath.EvalSymlinks(joined)` which uses OS-native paths. Then compares with `filepath.Rel(realRoot, resolved)` which uses OS-native separators.

If the OS is Windows:
- `joined = "internal\\auth\\validate.go"` (Windows-style)
- `filepath.Abs(joined)` works (Windows uses \)
- `filepath.EvalSymlinks(joined)` works
- `realRoot = "C:\\Users\\...\\repo"`
- `resolved = "C:\\Users\\...\\repo\\internal\\auth\\validate.go"`
- `filepath.Rel(realRoot, resolved)` returns `"internal\\auth\\validate.go"`
- `rel != ".."` and doesn't start with `"..\\"` → existsInside. 

The original lexical guard:
```go
if rel, err := filepath.Rel(root, joined); err != nil ||
    rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
```

On Windows, `string(filepath.Separator)` is `\` and `rel` uses `\` too. So this is consistent.

### Issue 19: ValidatePath's lexical guard runs BEFORE case check
```go
if rel, err := filepath.Rel(root, joined); err != nil ||
    rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
    f.PathValid = false
    f.PathWarning = PathNotFoundWarning
    return
}
// Tier 3 (case-only) is checked before existence
if idx != nil {
    if suggestion, mismatch := idx.CaseCorrection(f.File); mismatch {
        ...
```

So a path that escapes (e.g., `../foo`) gets flagged without going through Tier 3. That's fine - escaping paths aren't typos.

But wait, what about a path that escapes? It has `..` segments in `f.File`. After joining with root, `filepath.Rel(root, joined)` returns `"../foo"`. The check returns `true` for the rel check. PathWarning is set, no suggestion. Good - this is what we want.

### Issue 20: Tier 2 file lookup issue - similar names across multiple tracked files

Wait, looking at tier2 more carefully:
```go
citedStem, citedExt := splitStem(base)
...
for _, cand := range x.DirBasenames(dir) {
    if cand == base {
        continue
    }
    candStem, candExt := splitStem(cand)
    if candExt != citedExt {
        continue
    }
```

But wait - tier2 runs even when `idx.tracked` doesn't contain `rel`. That's fine because rel is the missing path. But the check `if cand == base` - this ensures we don't suggest the cited path back. Since the cited path is missing from `idx.tracked`, this is just defensive. Actually, since the cited basename matches `base` (which is the basename of `rel`), and `rel` is not tracked, then `base` won't appear in `DirBasenames(dir)` for tracked files with same basename. Unless there's an exact match (e.g., case-folded match), but case-folded check happens in Tier 3 first.

### Issue 21: tier2 - what about citedStem being a prefix of multiple candidates?

```go
func prefixDerivation(a, b string) bool {
    if a == b {
        return false
    }
    short, long := a, b
    if len(b) < len(a) {
        short, long = b, a
    }
    return strings.HasPrefix(long, short)
}
```

"user" vs "users": short = "user", long = "users". "users" has prefix "user". Returns true.
"users" vs "user": short = "user", long = "users". Same. Returns true.

So pluralization cases are correctly blocked.

But what about "validate" vs "validatex"? That's a weird edge case. blocked.
"validate" vs "valid"? "valid" is shorter, "validate" is longer. "validate" has prefix "valid". blocked.

But "validate" vs "validator"? Neither is a prefix of the other. Not blocked. They have similarity of about 0.78, which is above 0.75, so validator -> validate would be suggested. Good.

### Issue 22: Levenshtein implementation may have bug with rune handling

```go
ra, rb := []rune(a), []rune(b)
if len(ra) < len(rb) {
    ra, rb = rb, ra
}
```

Wait, if we swap so ra is the longer one... then the comparison `ra[i-1] == rb[j-1]` is comparing runes from different strings. That should still work correctly because we're computing distance, which is symmetric.

But the swap: after swap, ra is the longer string. We use `len(ra)` as the row count and `len(rb)` as the column count. The inner loop uses `ra[i-1]` and `rb[j-1]`. This should still give correct edit distance because we're just iterating differently.

Let me verify: standard Levenshtein uses dp[i][j] = min(dp[i-1][j]+1, dp[i][j-1]+1, dp[i-1][j-1]+cost). With our row-swapping:
- `prev[j]` = dp[i-1][j]
- `curr[j-1]` = dp[i][j-1]
- `prev[j-1]` = dp[i-1][j-1]

`curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)` = `min(dp[i-1][j]+1, dp[i][j-1]+1, dp[i-1][j-1]+cost)` = dp[i][j]. Correct.

### Issue 23: GetDirs - tier2 may have stem = "" with extension
What if `cited.go` is cited? `splitStem("cited.go")` returns `("cited", ".go")`. 
What if `cited` is cited (no extension)? `splitStem("cited")` returns `("cited", "")`. 

For Tier 2, all candidates would have `candExt == ""`. So if all files in the directory have extensions and the cited path doesn't, no candidates match. This might miss some valid cases, but it's defensive.

Actually this could be an issue - if the cited file is "Makefile" (no extension) and the real file is "Makefile.bak" or something? Well, that's a weird case.

### Issue 24: An important issue - the case-folding in `indexFromPaths` 

```go
fold := strings.ToLower(rel)
idx.folded[fold] = append(idx.folded[fold], rel)
```

This builds a case-folded lookup. But the case fold is done once at index build time. The lookup uses `strings.ToLower(filepath.ToSlash(relpath))`. So:
- Index build: `ToLower(rel) -> [realPath1, realPath2, ...]`
- Lookup: `ToLower(cited) -> ...` should match if they're case-insensitive equal.

But what about Unicode case folding? `strings.ToLower` is simple lowercasing, not full Unicode case folding. For most ASCII file names, this is fine. For non-ASCII, edge cases exist.

The docstring says "Unicode-simple case folding (lowercase)". This is documented, so it's intentional.

### Issue 25: Tier 2 doesn't consider case when comparing stems

When tier2 compares `citedStem` to `candStem` for similarity, it does so on the raw stems (case-sensitive). So `validate` vs `Validate` would have edit distance 1 (case difference) but tier2 wouldn't suggest because... wait, actually tier2 would suggest it! Because tier2 only compares strings, not considering case. The similarity would be 8/9 = 0.89, above 0.75. But this would skip Tier 3, which would correctly identify the case-only difference.

Wait, but Tier 3 is checked FIRST in `ValidatePath`. If the case-only check passes (i.e., not a case mismatch), we fall through to Tier 2. So by the time Tier 2 runs, the case-only check has confirmed the citation isn't a case-only mismatch.

Hmm but Tier 3 only confirms "is this case-only mismatch", not "does this case-fold to a real file in a different directory". Let me re-read:

```go
func (x *FileIndex) CaseCorrection(citedRel string) (suggestion string, mismatch bool) {
    if x == nil {
        return "", false
    }
    rel := filepath.ToSlash(citedRel)
    if x.Has(rel) {
        return "", false // correctly cited, byte-exact
    }
    folded := x.ByFold(rel)
    switch len(folded) {
    case 0:
        return "", false // not a case issue
    case 1:
        return folded[0], true
    default:
        return "", true // ambiguous: mismatch but no single winner
    }
}
```

So `CaseCorrection` checks if `citedRel` case-folds to any tracked file. If yes and there's one match, it's a case mismatch. If multiple case-fold matches (rare), ambiguous.

So by the time Tier 2 runs, we know the cited path doesn't case-fold to any tracked file. So tier2 comparing with case-sensitive strings is fine.

But there's a subtle issue: tier2 might compare `validate` to `Validate` (both tracked as separate files? No, can't both exist on case-insensitive FS). OK this is fine.

### Issue 26: tier2 may suggest wrong file when multiple extensions match but no exact match

Looking at test:
```go
func TestMissingSuggestion_Tier2ExtensionGuard(t *testing.T) {
    root := gitRepo(t, "internal/auth/validate.md")
    idx := BuildFileIndex(root)

    assert.Empty(t, idx.MissingSuggestion("internal/auth/validator.go"))
}
```

This tests that when the cited extension (.go) doesn't match the candidate's extension (.md), no suggestion. Good.

### Issue 27: prefixDerivation issue with case sensitivity

```go
func prefixDerivation(a, b string) bool {
    ...
    return strings.HasPrefix(long, short)
}
```

This is case-sensitive. So "User" vs "users" doesn't trigger prefixDerivation (since "User" != "user" prefix-wise). The similarity would be ~0.75 or so, just at the threshold. Not a big deal.

### Issue 28: A potential edge case in tier2 with no-extension files

What about a Makefile scenario:
- Tracked: "Makefile" (no extension)
- Cited: "makefile" (lowercase, case mismatch)

Tier 3 catches it (single case-fold match).

What about:
- Tracked: "Makefile" 
- Cited: "makefil" (typo, missing 'e')

Tier 3: no case-fold match (no "makefil" tracked). No mismatch.
Tier 2: directory exists. Cited stem = "makefil", ext = "". Cand stem = "Makefile", ext = "". Same ext. Not equal (so not `cand == base`). 
- `prefixDerivation("makefil", "Makefile")`: case-sensitive. "makefil" is not a prefix of "Makefile" (case-sensitive). Returns false. Good.
- similarity("makefil", "Makefile") = ? 

Let's compute: "makefil" vs "Makefile" - 8 chars vs 8 chars. 
- "makefil" -> "Makefile": substitute 'm'->'M', substitute 'i'->'i' (same), substitute 'l'->'l' (same)... wait let me redo.

"makefil" length 7, "Makefile" length 8. To go from "makefil" to "Makefile":
- 'm' -> 'M' (substitute, 1)
- insert 'e' after 'a' (2)

So distance = 2, longest = 8. Similarity = 1 - 2/8 = 0.75.

That's exactly at the threshold (bestScore = tier2SimilarityThreshold = 0.75, must be > 0.75). So no suggestion. Edge case at threshold.

Actually wait: "makefil" is the cited stem, "Makefile" is the cand stem. Distance from "makefil" to "Makefile" is 2 (1 substitute + 1 insert). But this is case-sensitive distance. The case differences count.

Hmm, this is a design choice. If we're already past Tier 3, case differences are not the issue. But tier2 counts case differences as edit distance. Could be a minor false negative.

This might be intentional - if you cited "makefil" instead of "Makefile", the system might be looking for a typo (not case difference), and tier2 may not suggest because of the case penalty. But Tier 3 only catches exact-case-fold matches.

This is a design tradeoff. Not necessarily a bug.

### Issue 29: real_root when EvalSymlinks fails
```go
realRoot, err := filepath.EvalSymlinks(absRoot)
if err != nil {
    realRoot = absRoot // best effort; containment still re-checked below
}
```

If EvalSymlinks fails on root, we use absRoot. Then filepath.Rel(absRoot, resolved) should still work. But if root doesn't exist or has symlinks we can't resolve, this could be wrong. Edge case.

### Issue 30: ValidatePath lexical guard doesn't catch all symlink escapes
```go
if rel, err := filepath.Rel(root, joined); err != nil ||
    rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
```

The `joined` is built by:
```go
joined := filepath.Join(root, f.File)
```

If `f.File = "../foo"`, joined = root + "../foo". After Rel, it's "../foo". Caught.

If `f.File = "link/../foo"` where link is a symlink to outside, joined = root + "link/../foo" = root + "foo" (after ..). Rel(root, joined) = "foo". Not caught by lexical guard.

This case is caught by `existsContained` because `EvalSymlinks("root/link/../foo")` would resolve the symlink first then resolve `..`. So it would return the external path. Good.

### Issue 31: Test - `TestRunReconcile_SuggestsHallucinatedPathEndToEnd`

This uses `writeFindings` helper. Let me check it doesn't have issues... It's calling existing helper. Good.

The e2e test creates a real git repo, runs reconcile, and checks the output. Looks correct.

### Issue 32: `tier2SimilarityThreshold` const
Set to 0.75. Matches test expectations. Good.

### Issue 33: A potential issue with concurrency in BuildFileIndex

If `BuildFileIndex` is called concurrently for the same root, no issue (it's read-only after construction). But if multiple goroutines call it for different roots, each builds its own index. Fine.

### Issue 34: Tier 2 only uses stem, not filename

For tier2:
```go
citedStem, citedExt := splitStem(base)
...
candStem, candExt := splitStem(cand)
if candExt != citedExt {
    continue
}
...
score := similarity(citedStem, candStem)
```

But the suggestion is the full basename (with extension), not just stem:
```go
if dir == "." {
    return best
}
return dir + "/" + best
```

Where `best` is set to `cand` (full filename). Good.

### Issue 35: `idx.Has` and `idx.ByBasename` may have edge cases on Windows

`filepath.ToSlash` converts to forward slash. `path.Base` works on forward slash. On Windows:
- tracked path: "internal/auth/validate.go" (from git)
- citedRel: "internal\\auth\\validate.go" (Windows native)

`Has(citedRel)` does `filepath.ToSlash(citedRel)` → "internal/auth/validate.go". Lookup in tracked. Works.

But what about case-insensitive Windows? Has is byte-exact (lowercase comparison in Go maps). So "Validate.go" wouldn't match "validate.go" in `Has`. This is correct behavior - we want byte-exact match.

But wait - what if the case-folded lookup uses case-fold? Yes, `ByFold` does case-fold. So Tier 3 catches it.

### Issue 36: Potential issue with how BuildFileIndex handles path with trailing slash

What if git returns an empty string in the ls-files output (last entry after final NUL)? `strings.Split(string(out), "\x00")` would have a trailing empty string. The loop:
```go
for _, r := range raw {
    rel := strings.TrimSpace(r)
    if rel == "" {
        continue
    }
```

Empty entries are skipped. Good.

### Issue 37: Subtle issue - EvalSymlinks requires the path to exist

`filepath.EvalSymlinks(path)` returns an error if `path` doesn't exist. So for a missing file, the error is `os.IsNotExist(err) == true`, handled by returning `existsOutsideOrAbsent`.

What if the file exists but the user has no permission to traverse the directory? EvalSymlinks would still succeed because it operates on each segment. But if the directory itself has a symlink to something outside, EvalSymlinks would follow it.

### Issue 38: Tier 2 - what about subdirectory matches?

Tier 2 only looks at `dir` (the immediate parent directory). It doesn't look at subdirectories. So a typo in "internal/auth/validator.go" where the real file is "internal/auth/sub/validate.go" wouldn't be found by tier2. Tier 1 might find it if "validate.go" is uniquely basename'd in another dir. But if there are multiple "validate.go" files, tier1 would be ambiguous.

This is a design choice - tier2 is intentionally narrow.

### Issue 39: Index map memory usage

For large repos with thousands of files, the index could use significant memory. But it's built once per run, so OK.

### Issue 40: Tests look comprehensive

The tests cover:
- Tier 1, 2, 3 happy paths
- Ambiguous cases (no suggestion)
- Below threshold cases (no suggestion)
- Symlink escape (no suggestion)
- Case-only typos (suggestion)
- Plural derivation rejection (no suggestion)

Good test coverage.

### Issue 41: One real issue - ValidatePath's lexical guard uses filepath.Rel which is platform-dependent

```go
if rel, err := filepath.Rel(root, joined); err != nil ||
    rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
```

`filepath.Rel` returns OS-native separators. On Linux, separator is `/`. The check uses `string(filepath.Separator)`. Consistent.

But wait, what if `joined == root`? Then `rel == "."`. Not "..", doesn't start with "../". So passes. Then existence check would say existsInside (assuming file exists). Good.

What if `joined` is `root + "subdir"` and root is `/tmp/abc`? Then `rel = "subdir"`. Passes. Good.

### Issue 42: In `existsContained`, when EvalSymlinks fails on absJoined due to file not existing, absRoot's symlinks might still be resolved

```go
realRoot, err := filepath.EvalSymlinks(absRoot)
if err != nil {
    realRoot = absRoot
}
```

If root is a normal dir, EvalSymlinks succeeds. Good.

If root doesn't exist (shouldn't happen but...), EvalSymlinks fails. We use absRoot. Then comparing absRoot to resolved... well, absJoined won't exist either, so we'd get `existsOutsideOrAbsent` from `os.IsNotExist(err)`.

### Issue 43: PathSuggestion in error format - "did you mean" might collide with existing text

The format is `"⚠️ File not found: %s (did you mean %s?)"`. If a reviewer-controlled path includes "did you mean", the rendering could be confusing. But esc() neutralizes markup but not text content. Edge case.

### Issue 44: Important - the PathSuggestion field might not always be preserved through a path that doesn't go through reconcile

If a Finding is created directly (not via the stream parser), PathSuggestion wouldn't be set. But this is by design - suggestions come from the validation pipeline.

### Issue 45: A subtle bug - the tier1 function might over-suggest

Actually let me re-read tier1:

```go
func (x *FileIndex) tier1(rel, base, dir string) string {
    candidates := x.ByBasename(base)
    if len(candidates) == 0 {
        return ""
    }
    if len(candidates) == 1 {
        if candidates[0] != rel {
            return candidates
</think>
