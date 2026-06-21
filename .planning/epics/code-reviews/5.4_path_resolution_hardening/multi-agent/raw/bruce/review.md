<think>
Let me carefully analyze this diff for Epic 5.4: Path-Resolution Hardening. I need to review the changes in the scope of the sprint plan, looking at:

1. `internal/reconcile/emit.go` - PathSuggestion field + report rendering
2. `internal/reconcile/emit_test.go` - tests for emit
3. `internal/reconcile/pathsuggest_e2e_test.go` - e2e test
4. `internal/reconcile/validate.go` - building index once
5. `internal/report/render.go` - rendering suggestion
6. `internal/report/validate_test.go` - tests for report
7. `internal/stream/fileindex.go` - new file: FileIndex
8. `internal/stream/fileindex_test.go` - tests for file index
9. `internal/stream/levenshtein.go` - new file: edit distance
10. `internal/stream/levenshtein_test.go` - tests for levenshtein
11. `internal/stream/parser.go` - new PathSuggestion field
12. `internal/stream/suggest.go` - new file: tiered suggestion
13. `internal/stream/suggest_test.go` - tests for suggest
14. `internal/stream/validate.go` - ValidatePath changes
15. `internal/stream/validate_test.go` - tests for validate

Let me look at the code in detail.

### `internal/stream/fileindex.go`

The `BuildFileIndex` function:
- Uses `git -C root ls-files -z` to get NUL-delimited paths
- Calls `indexFromPaths(strings.Split(string(out), "\x00"))`

But wait - `git ls-files -z` outputs paths separated by NUL bytes, and the last entry will have a trailing NUL (or not, depending on git version). The `strings.Split` will produce an empty string at the end. The `indexFromPaths` does check `if rel == "" { continue }` so that's handled. Good.

However, there might be a subtle issue. With NUL-delimited output, `strings.Split(string(out), "\x00")` on a string like `"a.go\0b.go\0"` would give `["a.go", "b.go", ""]`. The trailing empty is filtered. Good.

But what about `git ls-files` without `-z` that might have paths with newlines? The `-z` flag is used. Good.

What about quoted/escaped paths? Git's `-z` mode avoids quoting (uses raw bytes). This should be fine.

Looking at `indexFromPaths`:
- It uses `filepath.ToSlash(rel)` to normalize. Good.
- It checks for duplicates: `if _, seen := idx.tracked[rel]; seen { continue }`. Good.
- The basename, dir, and folded maps all use the same normalized path. Good.

But wait - what about case-sensitivity? The `basename` index uses exact basename, and `folded` uses lowercase. If a tracked file is `Parser.go` and the citation is `parser.go`, the basename index would have two entries (one for `Parser.go`, one for... well, only `Parser.go` since `parser.go` is the citation, not tracked). And the folded index would have one entry for `parser.go` pointing to `Parser.go`. This seems correct.

### `internal/stream/levenshtein.go`

The `levenshtein` function:
- Standard two-row DP. Looks correct.
- Edge case: when both strings are empty: `len(rb) == 0` returns `len(ra)` = 0. Good.
- When one is empty and the other isn't: returns the length. Good.

The `similarity` function:
- `1.0 - float64(levenshtein)/float64(longest)`. Looks correct.
- Two empty strings: `1.0` returned. Good.

### `internal/stream/suggest.go`

Tier 1 (`tier1`):
- Gets candidates by basename
- If exactly 1 candidate and it's not the cited path itself, returns it
- If multiple candidates, ranks by segOverlap
- On tie, returns "" (ambiguous)
- Good.

Tier 2 (`tier2`):
- Returns "" if directory doesn't exist
- Gets same-dir basenames
- Skips candidates with different extension
- Skips prefix derivations (to avoid user.go -> users.go)
- Finds closest by similarity
- Tie -> "" (ambiguous)
- Good.

But wait - I see a potential issue. The `prefixDerivation` check uses `strings.HasPrefix(long, short)`. This is fine for the cases mentioned (user->users), but what about `parse.go` vs `parser.go`? "parser" has prefix "parse", so it would be skipped. The test `TestMissingSuggestion_Tier2RejectsPluralDerivation` covers this case. Good.

But there's still a subtle issue. The `prefixDerivation` test case: `{"internal/parse/parse.go", "internal/parse/parser.go"}` - cited is "parser.go", tracked is "parse.go". So `short = "parse"`, `long = "parser"`, and `strings.HasPrefix("parser", "parse")` = true. Correctly returns true. Good.

Now what about a case where the cited name is longer? E.g., `{"internal/store/user.go", "internal/store/users.go"}` - cited is "users.go", tracked is "user.go". `short = "user"`, `long = "users"`, `strings.HasPrefix("users", "user")` = true. Correctly returns true. Good.

The `CaseCorrection` function:
- Returns ("", false) if byte-exact tracked
- Returns (single_match, true) if exactly one case-folded match
- Returns ("", true) if multiple case-folded matches (ambiguous)
- Returns ("", false) if no case-folded match
- Good.

But wait - if a tracked file is `parser.go` and the cited path is `Parser.go` (case-typo), the byte-exact check (`Has`) would return false, then `ByFold` would return `["parser.go"]` (one match). So `mismatch = true`, `suggestion = "parser.go"`. Good.

But what if the cited path doesn't exist as a tracked file AND has no case-folded match? Then `mismatch = false` and we fall through to existence check. Good.

Hmm, but `ValidatePath` checks `CaseCorrection` BEFORE the existence check. This means that even on a case-sensitive filesystem, the case-typo would be caught by the index before `os.Stat` is called. But what if a user has an untracked file with the wrong case? The `CaseCorrection` would return `("", false)` if there's no folded match, falling through to the existence check. But what if there IS a folded match but the untracked file exists? The case-typo would be flagged as invalid. This seems correct because we're checking against tracked files.

Wait, but what about an untracked file at the exact wrong case? E.g., repo has `parser.go` tracked, but the user also has `Parser.go` untracked. The cited `Parser.go` would have a folded match (`parser.go`), and the case-typo check would flag it as invalid. But the user might have intentionally created `Parser.go` and it does exist on disk. Hmm, this is a philosophical question - does the index check override the filesystem check?

Looking at the code: yes, the index check returns early without calling `existsContained`. So the case-typo is flagged as invalid. This might be intentional - if it's tracked, it should be the canonical version. But it could be a minor issue. Let me note this as a potential concern.

### `internal/stream/validate.go`

The `ValidatePath` function:
- Now takes a `*FileIndex` parameter
- Checks lexical containment (no `..` in relative path)
- If `idx != nil`, checks `CaseCorrection` first
- Then calls `existsContained`
- If `existsOutsideOrAbsent` and idx != nil, gets `MissingSuggestion`

The `existsContained` function:
- Gets abs paths
- Resolves symlinks for both root and joined
- Checks containment after symlink resolution
- Handles `IsNotExist` as `existsOutsideOrAbsent`
- Other errors are `existsIndeterminate`

This looks good. The symlink safety check is properly implemented.

But wait, there's an issue. The `realRoot` resolution uses `filepath.EvalSymlinks(absRoot)`. If root is `/tmp/repo` and it contains a symlink `link -> /etc`, then `EvalSymlinks` on root itself just resolves root (no symlinks in root). So `realRoot` is still `/tmp/repo`. Then for `joined = /tmp/repo/link/passwd`, `EvalSymlinks` resolves to `/etc/passwd`. Then `filepath.Rel("/tmp/repo", "/etc/passwd")` gives `../etc/passwd`, which starts with `..`. So it's correctly flagged as `existsOutsideOrAbsent`. Good.

But what if the root itself is a symlink? E.g., `/tmp/repo` is a symlink to `/var/repo`. Then `EvalSymlinks(absRoot)` gives `/var/repo`. Then for a normal file `/tmp/repo/internal/auth/validate.go`, `EvalSymlinks(absJoined)` gives `/var/repo/internal/auth/validate.go`. `filepath.Rel("/var/repo", "/var/repo/internal/auth/validate.go")` gives `internal/auth/validate.go`. Good.

But what if the file is `/tmp/repo/link/target` and `link` symlinks to `/etc`? `EvalSymlinks(absJoined)` gives `/etc/target`. `filepath.Rel("/var/repo", "/etc/target")` gives `../etc/target`, which starts with `..`. Correctly flagged. Good.

Now, there's a potential issue with the lexical containment check. The current code:
```go
if rel, err := filepath.Rel(root, joined); err != nil ||
    rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
```

This is a lexical check. If the root doesn't have a trailing separator, the rel could include root in the path. E.g., if `root = "/tmp/repo"` and `joined = "/tmp/repo/../etc/passwd"`, then `filepath.Rel("/tmp/repo", "/tmp/repo/../etc/passwd")` gives `../etc/passwd`, which starts with `..`. Good.

But what if `joined` is the same as `root`? E.g., `joined = "/tmp/repo"`. Then `rel` would be `.`, which doesn't start with `..`. This is correct (the root is within the repo).

But what if `joined = "/tmp/repo-subdir/file"` and `root = "/tmp/repo"`? Then `rel` would be `../repo-subdir/file`, which starts with `..`. Good.

OK so the lexical check is fine. And then the symlink check is the real defense.

### `internal/reconcile/emit.go`

The `PathSuggestion` field is added to `JSONFinding` with `omitempty`. Good.

The `writeFindingsList` function:
- If `m.PathWarning != ""`:
  - If `m.PathSuggestion != ""`, render with "(did you mean %s?)"
  - Else, render the bare warning
- Good.

### `internal/report/render.go`

The `writePathWarning` function is similarly updated. Good.

### Tests

Let me check if the tests properly cover the requirements:

- AC1: Build index once per run - `validate.go` builds it once. Good.
- AC2: Tier 1 - `TestMissingSuggestion_Tier1WrongDir` covers this. Good.
- AC3: Tier 3 - `TestCaseCorrection_CaseTypo` covers this. Good.
- AC4: Tier 2 - `TestMissingSuggestion_Tier2Typo` covers this. Good.
- AC5: Symlink safety - `TestValidatePath_SymlinkEscapeFlagged` and `TestValidatePath_SymlinkEscapeNoSuggestion` cover this. Good.
- AC6: `PathSuggestion` flows through - `TestJSONFindings_CarriesPathSuggestion`, `TestRenderMarkdown_ShowsPathSuggestion`, and the e2e test cover this. Good.
- AC7: Default never rewrites - the e2e test asserts the original path is preserved. Good.
- AC8: Unit tests per tier - all tiers are covered. Good.

### Issues I've identified

Let me look more carefully for issues:

1. **Potential issue with `CaseCorrection` and untracked files**: As mentioned, if a file exists on disk at the wrong case but is not tracked, the case-typo check would flag it as invalid. This could be a false positive, but it's probably an acceptable trade-off since we're reviewing tracked files.

2. **Potential issue with the `prefixDerivation` check**: The check uses `strings.HasPrefix` which is character-based. For Unicode strings, this might not behave as expected. But for the use case (English file names), it should be fine.

3. **Potential issue with `similarity` and the `longest` calculation**: The `longest` uses `len([]rune(a))` and `len([]rune(b))`. But the `levenshtein` also uses runes. So the units are consistent. Good.

4. **The `e2e test` uses `context.Background()`**: The test is synchronous, so this is fine. But there might be a timeout concern if `git` is slow. The test doesn't set a timeout on the context. This is a minor issue.

5. **The `gitRepo` function in the test uses `t.TempDir()`**: This is correct for creating temporary directories. Good.

6. **The `gitRepo` function doesn't handle the case where `git` is not installed**: If `git` is not available, `exec.Command("git", "init", "-q")` will fail. The test would then fail. This is probably acceptable for a test that requires `git`.

7. **Potential issue with `MissingSuggestion` and the `tier1` check for `len(candidates) == 1`**: The comment says "an absent path is never in the tracked set, so a lone match is always elsewhere." But what if the cited path is the same as the tracked path? Then the file would exist, and we'd never reach `MissingSuggestion`. So this is correct.

8. **Potential issue with the e2e test**: The test uses `writeFindings` and `mf` helpers that are presumably defined elsewhere in the `reconcile` package. Let me check if these are in the diff... The test file uses `writeFindings(t, sources, "greta/findings.txt", ...)` and `mf(...)`. These are presumably existing helpers in the test package. The diff doesn't show them, so they should exist already.

9. **Performance concern**: The `BuildFileIndex` is called once per reconcile run. The `git ls-files` command should be fast. The index is built in memory. For large repos, this could be memory-intensive, but the sprint plan acknowledges this risk.

10. **The `CaseCorrection` is called before `existsContained`**: This means that even on a case-sensitive filesystem (where `os.Stat` would fail for a case-typo), the case check is done first. This is correct because the case check is more authoritative than the filesystem check.

11. **Potential issue: what if a cited path has a case-typo AND the file doesn't exist on disk?**: The `CaseCorrection` would catch it first and return a suggestion. The existence check wouldn't be reached. Good.

12. **Potential issue: what if a cited path is a case-typo of an untracked file?**: The `CaseCorrection` would check the index (tracked files). If the untracked file's name case-folds to a tracked file, it would be flagged. If not, it would fall through to the existence check. This seems reasonable.

13. **Potential issue with `prefixDerivation` and the `parse.go` vs `parser.go` case**: The test `TestMissingSuggestion_Tier2RejectsPluralDerivation` covers this. But what about `parse.go` vs `parser.go` where both exist? E.g., tracked files are `parse.go` and `parser.go`, cited is `parseer.go`. The `prefixDerivation` would skip both candidates (both are prefix derivations). So no suggestion would be made. But a `parseer.go` typo is clearly a typo, not a derivation. Hmm, this might be a false negative. But the test only has one candidate per case, so this scenario isn't tested.

14. **Potential issue: the `prefixDerivation` check is too aggressive**: The check skips ANY case where one stem is a strict prefix of the other. This means if a user cites `config.go` and the tracked file is `configuration.go`, no suggestion would be made. This might be too conservative. But the epic's philosophy is "a wrong guess is worse than none", so this is probably acceptable.

15. **Thread safety**: The `FileIndex` is built once and shared across all findings. The maps are read-only after construction. So it's safe for concurrent reads. Good.

16. **Error handling in `BuildFileIndex`**: If `git ls-files` fails, it returns nil. This is the graceful degradation path. Good.

17. **The `git ls-files -z` output**: The output is split by `\x00`. The last element is typically empty (if the output ends with `\x00`). The `indexFromPaths` filters empty strings. Good.

18. **Potential issue with `git ls-files` and binary files**: `git ls-files` lists all tracked files, including binary ones. The index would include them. But the suggestion logic only operates on basenames and extensions, so binary files shouldn't cause issues. Good.

19. **The `segOverlap` function**: It counts shared directory segments. For `internal/auth` vs `pkg/auth`, the shared segment is `auth` (1). For `internal/auth` vs `internal/parser`, the shared segment is `internal` (1). This is a simple heuristic. The epic acknowledges this is a tie-breaker, not a primary ranking mechanism.

20. **Potential issue with `MissingSuggestion` and the `tier1` `len(candidates) == 1` case**: The comment says "an absent path is never in the tracked set, so a lone match is always elsewhere." But what if the cited path is byte-exact to a tracked path but the file doesn't exist on disk? Then the cited path IS in the tracked set. The `MissingSuggestion` is only called when the file doesn't exist (`existsOutsideOrAbsent`). So if the path is in the tracked set, the file should exist (git tracks files that exist). Unless the file was deleted after being committed. In that case, `os.Stat` would fail, and `MissingSuggestion` would be called. The `tier1` check would see that the cited basename has one candidate (itself) and would skip it (the `candidates[0] != rel` check). So no false suggestion. Good.

Wait, actually, if the file is deleted but still tracked, the `tier1` check would see `candidates[0] == rel` and return "". But this is a special case. The file is tracked but doesn't exist. The suggestion should probably be "this file was tracked but is now missing". But the current code returns "". This is a minor issue.

Actually, looking more carefully: if the file is tracked but doesn't exist on disk, the `MissingSuggestion` is called. The `tier1` check sees one candidate (the cited path itself) and returns "" because `candidates[0] == rel`. So no suggestion. This is a minor edge case.

21. **The `validate.go` change in `internal/reconcile/validate.go`**: The function now builds the index once and passes it to `ValidatePath`. This is correct per AC1. Good.

22. **The `emit.go` change**: The `PathSuggestion` field is added with `omitempty`. The `writeFindingsList` function is updated. Good.

23. **The `render.go` change**: The `writePathWarning` function is updated. Good.

24. **Potential issue: the `esc` function**: The `esc` function is used to escape markup. The suggestion is also escaped. This is correct.

Let me look at the test more carefully:

`TestMissingSuggestion_Tier1Ranked`:
- Tracked: `internal/auth/handler.go`, `web/ui/handler.go`
- Cited: `internal/auth/sub/handler.go`
- Expected: `internal/auth/handler.go`

The `tier1` logic:
- `candidates = ["internal/auth/handler.go", "web/ui/handler.go"]`
- `len(candidates) == 2`, so go to ranking
- For `internal/auth/handler.go`: `segOverlap("internal/auth/sub", "internal/auth")` = 2 (shared: internal, auth)
- For `web/ui/handler.go`: `segOverlap("internal/auth/sub", "web/ui")` = 0
- Best score = 2, best = `internal/auth/handler.go`, no tie
- Returns `internal/auth/handler.go`. Good.

`TestMissingSuggestion_Tier1AmbiguousNoSuggestion`:
- Tracked: `alpha/handler.go`, `beta/handler.go`
- Cited: `gamma/handler.go`
- Expected: ""

The `tier1` logic:
- `candidates = ["alpha/handler.go", "beta/handler.go"]`
- For `alpha/handler.go`: `segOverlap("gamma", "alpha")` = 0
- For `beta/handler.go`: `segOverlap("gamma", "beta")` = 0
- Best score = 0, best = `alpha/handler.go`, tie = true (second candidate also scores 0)
- Returns "". Good.

`TestMissingSuggestion_Tier2RejectsPluralDerivation`:
- Tracked: `internal/store/user.go`
- Cited: `internal/store/users.go`
- Expected: ""

The `tier2` logic:
- `HasDir("internal/store")` = true
- `citedStem = "users"`, `citedExt = ".go"`
- For `user.go`: `candStem = "user"`, `candExt = ".go"` (match)
- `prefixDerivation("users", "user")`: `short = "user"`, `long = "users"`, `strings.HasPrefix("users", "user")` = true
- Returns true, so candidate is skipped
- No other candidates, so returns "". Good.

`TestMissingSuggestion_Tier2KeepsTrueTypo`:
- Tracked: `internal/auth/validate.go`
- Cited: `internal/auth/validator.go`
- Expected: `internal/auth/validate.go`

The `tier2` logic:
- `HasDir("internal/auth")` = true
- `citedStem = "validator"`, `citedExt = ".go"`
- For `validate.go`: `candStem = "validate"`, `candExt = ".go"` (match)
- `prefixDerivation("validator", "validate")`: `short = "validate"`, `long = "validator"`, `strings.HasPrefix("validator", "validate")` = false
- `similarity("validator", "validate")` = 1 - 2/9 = 0.778
- 0.778 > 0.75 (threshold), so best = `validate.go`
- Returns `internal/auth/validate.go`. Good.

Everything looks correct. Let me look for any other issues.

### Potential issues I want to flag

1. **The `prefixDerivation` check might be too aggressive**: As mentioned, it would skip suggestions for `config.go` -> `configuration.go`. But this is a philosophical choice, not a bug. I'll mention it as a LOW concern.

2. **The `MissingSuggestion` doesn't handle the case where the file is tracked but deleted**: As mentioned, this is a minor edge case.

3. **The `tier2SimilarityThreshold` of 0.75 is hardcoded**: The sprint plan suggests this should be configurable or at least documented. The constant is documented in the code, so this is acceptable.

4. **The `CaseCorrection` is called even when the file is valid**: Wait, no. The `CaseCorrection` is called before the existence check. If the file is valid (byte-exact tracked), `CaseCorrection` returns `("", false)`, and the code falls through to the existence check. If the file is a case-typo, `CaseCorrection` returns `(suggestion, true)`, and the code returns early. So the `CaseCorrection` is only "called even when valid" in the sense that it's always called first, but it correctly handles the valid case. Good.

5. **The `MissingSuggestion` might suggest a file that doesn't exist on disk**: If a tracked file is deleted (e.g., after a refactor), the index still has it, and `MissingSuggestion` might suggest it. This is a minor edge case. The user would see the suggestion, check the file, and find it doesn't exist. This is acceptable.

6. **The `git ls-files` command might fail silently**: If the command fails (e.g., not a git repo), the index is nil. This is the graceful degradation path. Good.

7. **The `git ls-files` output might contain paths with special characters**: The `-z` flag handles this. Good.

8. **The `BuildFileIndex` doesn't handle the case where `root` contains a symlink**: The `filepath.Abs` and `filepath.EvalSymlinks` calls in `existsContained` handle this. Good.

9. **The `PathSuggestion` field is exported in `JSONFinding`**: This means it's part of the public API. The `omitempty` tag ensures it's not in the output when empty. Good.

10. **The `Finding.PathSuggestion` field in `stream/parser.go`**: This is also exported. Good.

11. **Thread safety of `FileIndex`**: The maps are built once and only read. Safe for concurrent reads. Good.

12. **The `validate.go` change in `internal/reconcile/validate.go`**: The function signature is unchanged (`validateFindingPaths(findings []Merged, root string)`). The index is built inside. Good.

13. **The `emit.go` change**: The `JSONFinding` struct is updated. The `JSONFindings` method is updated. The `writeFindingsList` function is updated. Good.

14. **The `render.go` change**: The `writePathWarning` function is updated. Good.

15. **The e2e test**: The test creates a git repo, writes a finding, runs reconcile, and checks the output. The test uses `writeFindings` and `mf` helpers that are presumably defined elsewhere. The test also uses `ReadReconciledFindings` and `ReportMD` which are presumably defined elsewhere. Good.

16. **Potential issue: the e2e test doesn't test the symlink case end-to-end**: The unit tests cover it, but the e2e test doesn't. This is a minor gap, but the sprint plan says "Unit tests per tier + an end-to-end test (hallucinated path → suggestion in report)." So the e2e test is for the hallucinated path, not the symlink case. The symlink case is covered by unit tests. Good.

17. **Potential issue: the `tier2SimilarityThreshold` might be too low**: The comment says "The 5.0 plan and this epic's Open Question 1 recommended ~0.85... it is actually distance 2... giving stem similarity 1 - 2/9 ≈ 0.78. The epic's binding directive is to tune 'against the 5.0 examples rather than a round number', so the threshold is set to admit that example with margin while still rejecting clearly-different names (e.g. config/cfg ≈ 0.50)."

The threshold is set to 0.75. The canonical example is 0.78. The margin is 0.03. This is a tight margin. If the user has a different example that's slightly below 0.78, it would be rejected. This is a philosophical choice. I'll mention it as a LOW concern.

Actually, looking at the test:
```go
assert.True(t, similarity("validator", "validate") >= 0.75)
assert.True(t, similarity("config", "cfg") < 0.75)
```

The `config/cfg` similarity is 1 - 5/6 = 0.167. Wait, let me recalculate. `config` has 6 chars, `cfg` has 3 chars. Levenshtein distance: `config` -> `cfg` = delete 'o', 'n', 'f' = 3 deletions? Or substitute? Let me think...

`config` = c-o-n-f-i-g
`cfg` = c-f-g

To transform `config` to `cfg`:
- c -> c (match)
- o -> (delete)
- n -> (delete)
- f -> f (match)
- i -> (delete)
- g -> g (match)

So 3 deletions. Levenshtein distance = 3. Longest = 6. Similarity = 1 - 3/6 = 0.5. The test says `config/cfg ≈ 0.50`, which matches. Good.

18. **Potential issue: the `MissingSuggestion` doesn't handle the case where the cited path is a directory**: E.g., cited `internal/auth/` (with trailing slash). The `path.Base` would return `auth`, and `path.Dir` would return `internal/auth/`. The tier1 check would look for files with basename `auth`, which might not match. The tier2 check would look in directory `internal/auth/`, which might not have files. This is a minor edge case.

Actually, `filepath.ToSlash` doesn't remove trailing slashes. So `path.Base("internal/auth/")` returns `auth`, and `path.Dir("internal/auth/")` returns `internal/auth`. So the trailing slash is handled. But the basename would be `auth` (a directory name), and the tier1 check would look for files named `auth`. This might not match. Minor edge case.

19. **Potential issue: the `MissingSuggestion` doesn't handle the case where the cited path is empty**: `path.Base("")` returns `.`, and `path.Dir("")` returns `.`. The tier1 check would look for files with basename `.`, which is unusual. The tier2 check would look in directory `.`, which is the root. This is a minor edge case.

20. **The `validate.go` lexical containment check**: The check uses `filepath.Rel(root, joined)`. If `root` is a relative path (not absolute), the `Rel` might not work as expected. But the `existsContained` function uses `filepath.Abs` first, so this is handled. The lexical check is a quick pre-filter.

Wait, actually, the lexical check is done BEFORE `existsContained`. So if the lexical check fails, the path is flagged invalid without going to `existsContained`. This is correct (it's a quick check).

But the lexical check uses `root` directly (not `filepath.Abs(root)`). If `root` is `.` (the default), then `filepath.Rel(".", "joined")` might not work as expected. Let me think...

If `root = "."` and `joined = "internal/auth/validator.go"`, then `filepath.Rel(".", "internal/auth/validator.go")` gives `internal/auth/validator.go`. This doesn't start with `..`, so it passes. Good.

If `root = "."` and `joined = "../external/file"`, then `filepath.Rel(".", "../external/file")` gives `../external/file`. This starts with `..`, so it's flagged. Good.

OK, the lexical check seems fine.

21. **The `existsContained` function handles `filepath.Abs` errors**: If `Abs` fails, it falls back to the original path. This is a minor robustness concern.

22. **The `existsContained` function handles `EvalSymlinks` errors**: If `EvalSymlinks` fails for root, it falls back to `absRoot`. This is a minor robustness concern.

23. **The `MissingSuggestion` is called even when the cited path is a case-typo**: Wait, no. The `CaseCorrection` is called first, and if it's a case-typo, the function returns early. So `MissingSuggestion` is only called when the path is not a case-typo. Good.

24. **The `MissingSuggestion` is called even when the cited path escapes the root**: Wait, no. The lexical check is done first, and if it escapes, the function returns early. So `MissingSuggestion` is only called when the path is within the root (lexically). But the symlink check might still flag it as outside. In that case, `MissingSuggestion` would be called. This is correct (we want to suggest a real file even if the cited path escapes via symlink).

But wait, the test `TestValidatePath_SymlinkEscapeNoSuggestion` asserts that no suggestion is made for a symlink-escaping path. Let me re-read the code:

```go
if rel, err := filepath.Rel(root, joined); err != nil ||
    rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
    f.PathValid = false
    f.PathWarning = PathNotFoundWarning
    return
}
// ... CaseCorrection ...
switch existsContained(root, joined) {
case existsInside:
    f.PathValid = true
    f.PathWarning = ""
case existsOutsideOrAbsent:
    f.PathValid = false
    f.PathWarning = PathNotFoundWarning
    if idx != nil {
        f.PathSuggestion = idx.MissingSuggestion(f.File)
    }
```

So if the path escapes via symlink (but not lexically), `existsContained` returns `existsOutsideOrAbsent`, and `MissingSuggestion` IS called. But the test asserts no suggestion is made. Let me check the test:

```go
func TestValidatePath_SymlinkEscapeNoSuggestion(t *testing.T) {
    root := gitRepo(t, "internal/real.go")
    outside := t.TempDir()
    require.NoError(t, os.WriteFile(filepath.Join(outside, "known"), []byte("x\n"), 0o644))
    require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))
    idx := BuildFileIndex(root)

    f := Finding{File: "link/known"}
    ValidatePath(&f, root, idx)

    assert.False(t, f.PathValid)
    assert.Equal(t, PathNotFoundWarning, f.PathWarning)
    assert.Empty(t, f.PathSuggestion)
}
```

The cited path is `link/known`. The index has `internal/real.go` tracked. The `MissingSuggestion` is called with `link/known`. The basename is `known`, which is not in the index. So `tier1` returns "". The `tier2` checks if the directory `link` exists in the index. It doesn't (the index has `internal/real.go`, not `link/known`). So `tier2` returns "". So `MissingSuggestion` returns "". The test passes. Good.

But what if the symlink target is a file that IS tracked? E.g., the symlink points to a file in the repo. Then `MissingSuggestion` might suggest a real file. But this is a different scenario (the cited path is valid via symlink, but the symlink target is outside the repo). The test doesn't cover this. Minor gap.

Actually, the test `TestValidatePath_SymlinkEscapeFlagged` uses `t.TempDir()` for the outside directory, which is outside the repo. The tracked file in the repo is `internal/real.go`. The symlink `link` points to the outside directory. The cited `link/known` resolves to `<outside>/known`, which is outside the repo. The `MissingSuggestion` is called but finds no match. So no suggestion. Good.

25. **The `MissingSuggestion` might suggest a file that's the symlink target itself**: If the symlink target is a tracked file, the `MissingSuggestion` might suggest it. But the symlink target is outside the repo (in the test), so it's not tracked. Minor gap.

26. **The `prefixDerivation` check uses character-based prefix**: For ASCII file names, this is fine. For Unicode, it might not behave as expected. But the use case is primarily English file names, so this is acceptable.

27. **The `similarity` function uses rune-based length**: This is correct for Unicode. Good.

28. **The `levenshtein` function uses rune-based DP**: This is correct for Unicode. Good.

29. **The `tier2SimilarityThreshold` is 0.75**: This is a constant. The epic says it should be tuned against the 5.0 examples. The threshold admits the canonical example (0.78) with a small margin. This is a philosophical choice. I'll mention it as a LOW concern.

30. **The `MissingSuggestion` doesn't handle the case where the cited path is a tracked file but doesn't exist on disk**: As mentioned, this is a minor edge case.

31. **The `MissingSuggestion` might suggest a file in a different directory that's a better match**: The tier1 check uses `segOverlap` to rank candidates. But the tier2 check only looks in the same directory. If the cited directory is wrong and the cited file doesn't exist anywhere, tier2 returns "" (because the directory doesn't exist in the index). This
</think>
