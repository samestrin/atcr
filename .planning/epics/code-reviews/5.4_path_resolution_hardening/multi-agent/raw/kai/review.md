The user wants me to review a diff and identify issues in a specific format: pipe-delimited findings.

First, let me understand the scope from the sprint plan:
- Epic 5.4: Path-Resolution Hardening
- Files affected: `internal/stream/validate.go`, `internal/stream/*` (new), `internal/reconcile/validate.go`, `internal/reconcile/emit.go`, `internal/report/render.go`, and test files.

The diff shows changes to:
- `internal/reconcile/emit.go`
- `internal/reconcile/emit_test.go`
- `internal/reconcile/pathsuggest_e2e_test.go`
- `internal/reconcile/validate.go`
- `internal/report/render.go`
- `internal/report/validate_test.go`
- `internal/stream/fileindex.go`
- `internal/stream/fileindex_test.go`
- `internal/stream/levenshtein.go`
- `internal/stream/levenshtein_test.go`
- `internal/stream/parser.go`
- `internal/stream/suggest.go`
- `internal/stream/suggest_test.go`
- `internal/stream/validate.go`
- `internal/stream/validate_test.go`

Now I need to carefully review the diff for issues.

Let me go through each file:

1. `internal/reconcile/emit.go`:
   - Adds `PathSuggestion` field to `JSONFinding` struct. Good.
   - Updates `JSONFindings()` to include `PathSuggestion`. Good.
   - Updates `writeFindingsList` to render suggestion. Good.
   - No obvious issues here.

2. `internal/reconcile/emit_test.go`:
   - Tests for `PathSuggestion` omission when empty, carrying through JSON, and rendering in markdown. Good.

3. `internal/reconcile/pathsuggest_e2e_test.go`:
   - Uses `gitRepoWithFiles` helper.
   - Uses `stream.PathNotFoundWarning` - need to check if this constant is exported. Looking at `internal/stream/validate.go`, `PathNotFoundWarning` is declared as `const PathNotFoundWarning = "file not found"` - yes, it's exported (capital P).
   - Wait, in the diff for `internal/stream/validate.go`, the const is `const PathNotFoundWarning = "file not found"` - yes, exported.
   - Calls `RunReconcile` with `Options{Root: root}`.
   - Checks `hall.PathSuggestion`. Good.
   - No obvious issues.

4. `internal/reconcile/validate.go`:
   - `validateFindingPaths` now calls `stream.BuildFileIndex(root)` once, then passes it to `stream.ValidatePath`. Good.
   - But wait - the function signature of `ValidatePath` changed from `ValidatePath(&findings[i].Finding, root)` to `ValidatePath(&findings[i].Finding, root, idx)`. Need to check all callers. The diff shows `internal/stream/validate_test.go` updated all callers to pass `nil`. Are there any other callers not in the diff? The diff doesn't show any other callers, but the scope is limited to the diff. Since the diff only shows test callers updated, and the function is in the same package... Actually `ValidatePath` is exported (capital V), so it could be called from other packages. The diff shows `internal/reconcile/validate.go` calling it with `idx`. If there are other callers outside the diff, they would break, but since we're only reviewing the diff, and the diff doesn't show them, I shouldn't flag them unless they're in the diff. Actually, wait - the user said "Review the following diff". So I should only consider what's in the diff. The diff updates the signature and updates callers in tests and reconcile. That's fine.

5. `internal/report/render.go`:
   - `writePathWarning` updated to show suggestion. Good.

6. `internal/report/validate_test.go`:
   - Tests for suggestions in markdown, checklist, refuted views. Good.

7. `internal/stream/fileindex.go`:
   - `BuildFileIndex` runs `git -C root ls-files -z`.
   - `indexFromPaths` builds maps.
   - Wait, `indexFromPaths` uses `path.Base(rel)` and `path.Dir(rel)`. Since `rel` is slash-normalized, `path` package is appropriate.
   - But `filepath.ToSlash(rel)` is used on input.
   - In `Has`, it uses `filepath.ToSlash(relpath)` - good.
   - In `DirBasenames`, it uses `filepath.ToSlash(dir)` - good.
   - In `ByFold`, it uses `filepath.ToSlash(relpath)` - good.
   - One potential issue: `BuildFileIndex` uses `strings.TrimSpace(root) == ""` check, but `indexFromPaths` doesn't handle the trailing NUL from `git ls-files -z` properly? Let's see: `strings.Split(string(out), "\x00")` - if the output ends with NUL, the last element will be empty string. `indexFromPaths` checks `if rel == "" { continue }`, so that's fine.
   - However, `BuildFileIndex` uses `exec.Command("git", "-C", root, "ls-files", "-z")`. If `root` has spaces, it's passed as a single arg, so that's fine.
   - Wait, `git -C <path> ls-files -z` - the `-C` flag takes the path as the next argument. This is correct.
   - Issue: `cmd.Output()` can hang if the command produces a lot of output and the buffer fills? Actually `Output()` captures stdout into a buffer. For large repos, `git ls-files` output could be large but manageable. Not really an issue.
   - Issue: `os/exec` with `Output()` doesn't set a timeout. The sprint plan mentions "no measurable reconcile slowdown". A hung `git ls-files` could hang reconcile indefinitely. But the sprint plan doesn't mention timeouts, and this is a local command. Not sure if this is an issue.
   - Actually, looking at `gitRepoWithFiles` in test files, they use `exec.Command`. No timeout there either. This seems acceptable for a CLI tool.
   - Wait, I see `indexFromPaths` has a bug: `if _, seen := idx.tracked[rel]; seen { continue }`. This deduplicates tracked files. But what about `basename`, `dirFiles`, and `folded` maps? If there are duplicates (which there shouldn't be from git, but synthetic tests might have them), the slices could get duplicates. But since git doesn't output duplicates, this is fine.
   - Actually, if there are duplicates, the second occurrence would be skipped entirely, so `basename` etc. wouldn't get duplicates either. Good.

8. `internal/stream/fileindex_test.go`:
   - `gitRepo` helper similar to `gitRepoWithFiles`.
   - Tests look good.

9. `internal/stream/levenshtein.go`:
   - `levenshtein` function works on runes. Good for Unicode.
   - `similarity` uses `len([]rune(a))` and `len([]rune(b))`. Then calls `levenshtein(a, b)` which also converts to runes. So it's consistent.
   - `similarity` normalizes by the longer string. For "validator" (9 runes) vs "validate" (8 runes), distance is 2, longer is 9, similarity = 1 - 2/9 = 0.777... This matches the test.
   - The `min3` helper is straightforward.

10. `internal/stream/levenshtein_test.go`:
    - Tests look correct.

11. `internal/stream/parser.go`:
    - Adds `PathSuggestion` field to `Finding` struct. Good.

12. `internal/stream/suggest.go`:
    - `tier2SimilarityThreshold = 0.75`.
    - `CaseCorrection`: checks case-folded match. If `x.Has(rel)` returns true, it's byte-exact. Then `ByFold` returns matches. If 1, return it. If multiple, return `true` for mismatch but empty suggestion. Wait, the comment says:
      - multiple case-folded matches -> ("", true) (mismatch, ambiguous)
      - no case-folded match -> ("", false) (genuine miss)
      But looking at the code for `CaseCorrection`:
      ```go
      folded := x.ByFold(rel)
      switch len(folded) {
      case 0:
          return "", false // not a case issue
      case 1:
          return folded[0], true
      default:
          return "", true // ambiguous: mismatch but no single winner
      }
      ```
      Wait, this is a problem. If there are multiple case-folded matches (e.g., both `Parser.go` and `parser.go` exist in the index - which can happen on a case-sensitive filesystem), it returns `mismatch=true` but empty suggestion. However, the caller in `validate.go` does:
      ```go
      if suggestion, mismatch := idx.CaseCorrection(f.File); mismatch {
          f.PathValid = false
          f.PathWarning = PathNotFoundWarning
          f.PathSuggestion = suggestion // "" when ambiguous (multiple cases)
          return
      }
      ```
      So if there are multiple case-folded matches, it sets `PathValid=false`, `PathWarning="file not found"`, and `PathSuggestion=""`. But wait - if there are multiple files differing only by case, and the user cites a third variation (e.g., `PARSER.go`), it's ambiguous. But what if the user cites `parser.go` and there are two files: `Parser.go` and `parser.go`? Then `x.Has(rel)` would be true for `parser.go`, so it returns `false, false` and falls through. Good.
      But what if the user cites `Parser.go` (with capital P) and there are two files: `Parser.go` and `parser.go`? Then `x.Has("Parser.go")` is true, so it returns `false, false`. But wait - if the filesystem is case-insensitive, `os.Stat` would find `Parser.go` even if only `parser.go` exists. But here, `x.Has("Parser.go")` would only be true if `Parser.go` is tracked. If only `parser.go` is tracked, `x.Has("Parser.go")` is false, `ByFold` returns 1 match (`parser.go`), so it suggests `parser.go`. Good.
      What if both `Parser.go` and `parser.go` are tracked (possible on Linux)? If user cites `parser.go`, `x.Has` is true -> valid. If user cites `Parser.go`, `x.Has` is true -> valid. If user cites `PARSER.go`, `ByFold` returns 2 matches -> ambiguous, flagged as invalid but no suggestion. That seems reasonable.

    - `MissingSuggestion`:
      Calls `tier1` then `tier2`.
    - `tier1`:
      Gets candidates by basename. If 1 candidate and not the cited path, return it. If multiple, rank by segment overlap.
      Wait, `tier1` has this code:
      ```go
      candidates := x.ByBasename(base)
      if len(candidates) == 0 {
          return ""
      }
      if len(candidates) == 1 {
          if candidates[0] != rel {
              return candidates[0]
          }
          return ""
      }
      ```
      This seems wrong. If `len(candidates) == 1`, and the cited path is absent from the tracked set (which it must be, since this is called from `MissingSuggestion` only when the path is missing), then `candidates[0] != rel` is always true. Wait, is it possible that the cited path is in the tracked set but we still call `MissingSuggestion`? No, because `MissingSuggestion` is only called when the path is absent from the index. But wait - `tier1` is called from `MissingSuggestion`, which is called when the file doesn't exist on disk. However, a file could be tracked by git but deleted from disk. In that case, `os.Stat` says it doesn't exist, but `x.Has(rel)` might be true. Let's trace through `ValidatePath`:
      ```go
      // First: case check
      if idx != nil {
          if suggestion, mismatch := idx.CaseCorrection(f.File); mismatch {
              ...
              return
          }
      }
      switch existsContained(root, joined) {
      case existsInside:
          f.PathValid = true
      case existsOutsideOrAbsent:
          f.PathValid = false
          f.PathWarning = PathNotFoundWarning
          if idx != nil {
              f.PathSuggestion = idx.MissingSuggestion(f.File)
          }
      ...
      }
      ```
      So `MissingSuggestion` is called when the file is absent from disk. If the file is tracked but absent from disk (deleted), `existsContained` returns `existsOutsideOrAbsent`, and then `MissingSuggestion` is called. If the cited path is exactly a tracked path but missing from disk, `MissingSuggestion` calls `tier1`. `tier1` gets candidates by basename. If the basename exists only as the cited path (which is tracked but missing from disk), `candidates[0] == rel` would be true, so `tier1` returns "". Then `tier2` is called. `tier2` checks `x.HasDir(dir)`. If the directory exists, it compares the stem. If the stem is exactly the same (which it is, since it's the exact file), `cand == base` so it continues. So `tier2` returns "". Thus a tracked-but-deleted file gets no suggestion, which is correct (it's not a hallucination, just missing).

      But wait, what about the `candidates == 1` case in `tier1`? The comment says: "Only meaningful when it is not the cited path itself; an absent path is never in the tracked set, so a lone match is always elsewhere." This comment is wrong if the path is tracked but missing from disk. However, the code checks `candidates[0] != rel`, so it handles that case correctly by returning "". So the code is safe, though the comment is slightly inaccurate.

    - `tier2`:
      ```go
      if !x.HasDir(dir) {
          return ""
      }
      citedStem, citedExt := splitStem(base)
      bestScore, best, tie := tier2SimilarityThreshold, "", false
      for _, cand := range x.DirBasenames(dir) {
          if cand == base {
              continue
          }
          candStem, candExt := splitStem(cand)
          if candExt != citedExt {
              continue
          }
          if prefixDerivation(citedStem, candStem) {
              continue
          }
          score := similarity(citedStem, candStem)
          ...
      }
      ```
      Wait, `splitStem` uses `path.Ext(base)`. For a file like `.gitignore`, `path.Ext(".gitignore")` returns `""` (empty string) because `Ext` returns the extension starting from the last dot, but if there's no dot or the dot is the first character, it returns empty? Let's check: Go's `path.Ext` returns the extension starting from the last dot. For `.gitignore`, there is no dot before the last character? Actually `.gitignore` - the last dot is at index 0. According to Go docs: "If there is no dot, Ext returns an empty string." Wait, the docs say: "Ext returns the file name extension used by path. The extension is the suffix beginning at the final dot in the final element of path; it is empty if there is no dot."
      Actually for `.gitignore`, the final dot is at index 0. The suffix from the final dot would be `.gitignore`? Let's check Go behavior. In Go's `path.Ext`, if the filename starts with a dot, the extension is empty string. Actually no: `path.Ext(".gitignore")` returns `""` because the dot is the first character. Let me recall: Go's `path.Ext` specifically checks: if dot is at position 0 or if the last slash is after the last dot, it returns empty. Wait no, let me think. The actual implementation:
      ```go
      func Ext(path string) string {
          for i := len(path) - 1; i >= 0 && path[i] != '/'; i-- {
              if path[i] == '.' {
                  return path[i:]
              }
          }
          return ""
      }
      ```
      So for `.gitignore`, it would return `.gitignore` because it finds the dot at index 0 and returns from there. Wait, but there might be a special case. Let me check... Actually in Go 1.17+, `path.Ext(".gitignore")` returns `""`? No, I think it returns `.gitignore`. But then `strings.TrimSuffix(base, ext)` would return empty string for the stem. That might be weird for dotfiles, but dotfiles are unlikely to be review targets in this context. Not a critical issue.

      Actually, looking at `splitStem`:
      ```go
      func splitStem(base string) (stem, ext string) {
          ext = path.Ext(base)
          return strings.TrimSuffix(base, ext), ext
      }
      ```
      For `.gitignore`, `path.Ext` returns `.gitignore` (I need to verify). Actually, no - Go's `path.Ext` documentation: "The extension is the suffix beginning at the final dot in the final element of path; it is empty if there is no dot." So for `.gitignore`, the final dot is at the beginning. It doesn't say it skips leading dots. So it would return `.gitignore`. Then `strings.TrimSuffix(".gitignore", ".gitignore")` returns `""`. Stem is empty. This is a bit odd but dotfiles are edge cases.

      Wait, I recall that Go changed `path.Ext` behavior for dotfiles. Let me think... In Go 1.17, there was a change: `path.Ext` no longer includes the dot for hidden files? No, that was for something else. Actually, looking at the Go source: `path.Ext` iterates backwards from the end, and if it finds a dot, it returns the substring from that dot to the end. So for `.gitignore`, it returns `.gitignore`. This means the stem is empty. This is fine for the purpose of code review - nobody reviews `.gitignore` with line numbers typically.

      Let's look at `prefixDerivation`:
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
      This checks if one is a strict prefix of the other. But `len(b)` is byte length, not rune length! The `levenshtein` function works on runes, but `prefixDerivation` uses `len()` on strings (bytes) and `strings.HasPrefix` (bytes). This is inconsistent and could cause issues with Unicode filenames. However, filenames are typically ASCII, and the sprint plan doesn't mention Unicode concerns. This is a minor maintainability issue at best.

      But wait - there's a more serious issue in `prefixDerivation`. Consider `a="validat"`, `b="validate"`. `short="validat"`, `long="validate"`. `strings.HasPrefix("validate", "validat")` is true. So `prefixDerivation` returns true. But the comment says: "It deliberately does NOT fire for validator/validate, which share a prefix but where neither contains the other." Let's check `validator` vs `validate`: `short="validate"` (8 bytes), `long="validator"` (9 bytes). `strings.HasPrefix("validator", "validate")` is false. `strings.HasPrefix("validate", "validator")` is false. Good.
      What about `user` vs `users`: `short="user"`, `long="users"`. `strings.HasPrefix("users", "user")` is true. Good.
      What about `handler` vs `handlers`: `strings.HasPrefix("handlers", "handler")` is true. Good.
      What about `parse` vs `parser`: `short="parse"`, `long="parser"`. `strings.HasPrefix("parser", "parse")` is true. Good.
      What about `route` vs `router`: `short="route"`, `long="router"`. `strings.HasPrefix("router", "route")` is true. Good.
      What about `test` vs `testing`: `strings.HasPrefix("testing", "test")` is true. But the comment says "pluralization or derivation (user/users, handler/handlers, parse/parser)". `testing` is a derivation of `test`. Should it be filtered? The implementation would filter it. Is that intended? Probably yes.

      However, there's a subtle bug in `segOverlap`:
      ```go
      func segOverlap(a, b string) int {
          set := make(map[string]struct{})
          for _, s := range strings.Split(a, "/") {
              if s != "" && s != "." {
                  set[s] = struct{}{}
              }
          }
          n := 0
          for _, s := range strings.Split(b, "/") {
              if s == "" || s == "." {
                  continue
              }
              if _, ok := set[s]; ok {
                  n++
              }
          }
          return n
      }
      ```
      This counts shared path segments as a set (order-independent, duplicates not counted). For `internal/auth/sub` vs `internal/auth`, segments are `["internal", "auth", "sub"]` and `["internal", "auth"]`. Overlap is 2. For `internal/auth` vs `web/auth`, overlap is 1. This seems reasonable for ranking.

    - Now let's look at `ValidatePath` in `internal/stream/validate.go`:
      ```go
      func ValidatePath(f *Finding, root string, idx *FileIndex) {
          ...
          // lexical containment check
          if rel, err := filepath.Rel(root, joined); err != nil ||
              rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
              f.PathValid = false
              f.PathWarning = PathNotFoundWarning
              return
          }
          // Tier 3 (case-only) check
          if idx != nil {
              if suggestion, mismatch := idx.CaseCorrection(f.File); mismatch {
                  f.PathValid = false
                  f.PathWarning = PathNotFoundWarning
                  f.PathSuggestion = suggestion
                  return
              }
          }
          switch existsContained(root, joined) {
      ...
      }
      ```
      Wait, I see an issue here. The lexical containment check happens BEFORE the case correction check. But what if `joined` is a case-typo that also happens to pass the lexical check? It will pass. Then case correction is checked. That's fine.
      But what about the `existsContained` function:
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
              realRoot = absRoot
          }
          resolved, err := filepath.EvalSymlinks(absJoined)
          switch {
          case err == nil:
              rel, rerr := filepath.Rel(realRoot, resolved)
              if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
                  return existsOutsideOrAbsent
              }
              return existsInside
          case os.IsNotExist(err):
              return existsOutsideOrAbsent
          default:
              return existsIndeterminate
          }
      }
      ```
      Potential issues:
      1. `filepath.Abs(root)` might fail, falls back to root. But if root is relative, `EvalSymlinks` might behave differently. Not a big issue.
      2. `filepath.EvalSymlinks(absRoot)` - if root contains a symlink, this resolves it. Then `filepath.Rel(realRoot, resolved)` checks if resolved is under realRoot. But what if `absJoined` resolves to exactly `realRoot`? `filepath.Rel` returns `.`, which is fine. What if it returns `..`? Caught.
      3. `os.IsNotExist(err)` - but `filepath.EvalSymlinks` returns an error if the path doesn't exist OR if a symlink in the path is broken. Actually, `EvalSymlinks` returns an error if the final path component doesn't exist or if any intermediate symlink doesn't exist. So `os.IsNotExist` catches missing files. Good.
      4. However, there is a subtle issue: if `joined` is a symlink to a file outside the repo, `EvalSymlinks` resolves to the outside path, and `filepath.Rel` checks containment. Good.
      5. But what if `joined` contains a symlink segment that points outside, but then continues with `..` to come back inside? `EvalSymlinks` resolves all symlinks and `..` segments, so it should catch that.
      6. Wait, `filepath.EvalSymlinks` follows symlinks but also cleans the path. If a path like `link/../internal/auth/validate.go` where `link` points outside, `EvalSymlinks` resolves `link` first, then the `..` goes back... actually `EvalSymlinks` should resolve the full path. If `link` points to `/tmp`, then `link/../internal/auth/validate.go` becomes `/tmp/../internal/auth/validate.go` which becomes `/internal/auth/validate.go` (on Unix). Then `filepath.Rel` would show it as outside. Good.

      But there is a potential security/functional issue: `existsContained` does NOT actually check if the resolved path is a regular file! It only checks if it exists. But `os.Stat` in the original code checked if it was a file (well, `os.Stat` doesn't distinguish between file and directory - actually `os.Stat` works on directories too). The original code used `os.Stat(joined)` and checked `err == nil`. If `joined` was a directory, it would also return nil. So this behavior is preserved.

      Wait, actually I see something. In `existsContained`, when `err == nil` after `EvalSymlinks`, it returns `existsInside` without checking if the final path is actually a file or directory. But the original `os.Stat` would also succeed for directories. The function is named `existsContained`, implying it checks existence and containment. So directories would pass. Is this a problem? The original code allowed directories too, since `os.Stat` works on directories. So not a new issue.

      However, there is an issue with the lexical check before `existsContained`. The lexical check uses `filepath.Rel(root, joined)`. But `joined` is `filepath.Join(root, f.File)`. If `f.File` is `foo/../../../../etc/passwd`, then `joined` becomes `/root/foo/../../../../etc/passwd` which `filepath.Join` cleans to `/etc/passwd`. Then `filepath.Rel(root, "/etc/passwd")` on Unix would return `../../etc/passwd` or similar, which starts with `..`, so it would be caught. Good.
      But what if `root` itself contains symlinks? The lexical check doesn't resolve them. However, the `existsContained` function resolves symlinks and re-checks. So a path like `symlink-to-parent/known` would pass lexical check (no `..`), but fail the `existsContained` check because after resolving `symlink-to-parent`, it goes outside. Good.

      Now, looking back at `ValidatePath` - there's a logic issue:
      ```go
      if rel, err := filepath.Rel(root, joined); err != nil ||
          rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
          f.PathValid = false
          f.PathWarning = PathNotFoundWarning
          return
      }
      if idx != nil {
          if suggestion, mismatch := idx.CaseCorrection(f.File); mismatch {
              f.PathValid = false
              f.PathWarning = PathNotFoundWarning
              f.PathSuggestion = suggestion
              return
          }
      }
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
      default:
      }
      ```

      Wait, I see a bug! If `f.File` is a valid path (byte-exact tracked file, correct case), then `idx.CaseCorrection` returns `mismatch=false`. Then we call `existsContained`. If it exists, we set `PathValid=true`. Good.
      But what if the file exists on disk but is NOT tracked by git? `idx.CaseCorrection` returns false (no case mismatch, because `x.Has(rel)` is true? Wait! If the file exists on disk but is UNTRACKED, `x.Has(rel)` is FALSE. Then `ByFold` might return nothing (if no case variant exists). So `CaseCorrection` returns `false, false`. Then `existsContained` is called. If the file exists on disk (untracked), `existsContained` returns `existsInside`. Then `PathValid = true`. So untracked files are considered valid. But the sprint plan says the candidate source is `git ls-files`, and the scope says "untracked files are not review targets". However, the acceptance criteria says: "AC1: A candidate file index is built once per reconcile run from `git ls-files`". It doesn't say that only tracked files are valid. The original 5.0 behavior was to check filesystem existence. So untracked files that exist on disk are still considered valid. This is probably intended - the index is used for suggestion, not for restricting validity to tracked files. The sprint plan says: "untracked files are not review targets" under Out of Scope, meaning the index doesn't suggest untracked files, but it doesn't say existing untracked files should be flagged invalid. So this is fine.

      However, I see a potential issue: what if `f.File` is a tracked file but `existsContained` returns `existsIndeterminate` (permission error)? Then `PathValid` remains false (as initialized), `PathWarning` remains empty (as initialized). Wait, `Finding` is initialized with `PathValid=false` and `PathWarning=""`. In the default case (`existsIndeterminate`), the code does nothing. So the finding remains `PathValid=false` and `PathWarning=""`. This is the same as the original code. The comment says "Indeterminate (permission, I/O): leave the finding unflagged rather than assert a 'not found' we cannot prove." Wait, but `PathValid` is already false by zero value. The original code had:
      ```go
      default:
          // Indeterminate ... leave unflagged
      ```
      And the struct field comment says: "PathValid defaults to false". So if it's indeterminate, `PathValid` stays false and `PathWarning` stays empty. Is that correct? The comment says "unflagged" but `PathValid=false` means it's not validated? Or does `PathValid=false` with empty `PathWarning` mean "not checked"? The original code preserved this. Not a new issue.

      Let's look for real issues.

      **Issue 1: `existsContained` does not check if the path is a symlink itself**
      The function resolves symlinks in the path, but if the final component is a symlink to a file inside the repo, it follows it and checks the target. The sprint plan says: "Switch the existence stat to `os.Lstat` (so a symlinked segment is not traversed) or `filepath.EvalSymlinks` + re-check containment before stat." They chose `EvalSymlinks`. If the final component is a symlink to a file inside the repo, it will be resolved and considered existing. But what if the final component is a symlink to a file outside? It will be caught by the containment check. So symlink safety seems handled.

      **Issue 2: `existsContained` doesn't actually check the file type or if it's readable**
      But the original `os.Stat` also didn't. Not a regression.

      **Issue 3: Potential TOCTOU in `existsContained`**
      Between `EvalSymlinks` and `filepath.Rel`, the filesystem could change. But this is a local CLI tool, not a long-running service. Not a serious issue.

      **Issue 4: `strings.HasPrefix(rel, ".."+string(filepath.Separator))` might not work on all platforms**
      On Windows, `filepath.Separator` is `\`. The `Rel` function returns paths with platform-specific separators. So `..\\` on Windows. This is correct.

      **Issue 5: In `validate.go`, `idx` is built once per call to `validateFindingPaths`**
      ```go
      func validateFindingPaths(findings []Merged, root string) {
          if root == "" {
              return
          }
          idx := stream.BuildFileIndex(root)
          for i := range findings {
              stream.ValidatePath(&findings[i].Finding, root, idx)
          }
      }
      ```
      This is correct per AC1.

      **Issue 6: Test in `validate_test.go` called `TestValidatePath_EmptyRootDefaultsToCwd`**
      ```go
      func TestValidatePath_EmptyRootDefaultsToCwd(t *testing.T) {
          f := Finding{File: "parser.go"}
          ValidatePath(&f, "", nil)
          assert.True(t, f.PathValid)
          assert.Empty(t, f.PathWarning)
      }
      ```
      Wait, this test passes `nil` for idx, and root is empty. `ValidatePath` checks `if root == "" { root = "." }` (I assume - let me check the original code). Looking at the diff for `validate.go`:
      ```go
      func ValidatePath(f *Finding, root string, idx *FileIndex) {
          if f == nil {
              return
          }
          if root == "" {
              root = "."
          }
          joined := filepath.Join(root, f.File)
      ```
      Yes, root defaults to ".". So `ValidatePath` uses cwd. But then in `existsContained`, `root` is ".". `filepath.Abs(".")` returns cwd. `filepath.EvalSymlinks(".")` returns cwd. Then `joined` is `./parser.go`. `filepath.Abs("./parser.go")` returns cwd/parser.go. If that exists, it returns existsInside. This test assumes `parser.go` exists in the package dir (the test file's directory). This is a bit fragile but was there before.

      **Issue 7: `filepath.Join(root, f.File)` with absolute `f.File`**
      If `f.File` is absolute like `/etc/passwd`, `filepath.Join(root, "/etc/passwd")` returns `/etc/passwd` (on Unix). Then the lexical check `filepath.Rel(root, "/etc/passwd")` returns an error or a path starting with `..`? Actually on Unix, `filepath.Rel("/tmp/root", "/etc/passwd")` would return `../../etc/passwd` or similar, which starts with `..`. So it's caught. Good.

      **Issue 8: What if `git ls-files` returns paths with spaces or special characters?**
      `git ls-files -z` returns NUL-delimited paths. `strings.Split(string(out), "\x00")` splits on NUL. If the output ends with NUL, there will be a trailing empty string, which is skipped by the `rel == ""` check. Good.

      **Issue 9: In `fileindex.go`, `BuildFileIndex` returns nil on error**
      This causes graceful degradation. Good.

      **Issue 10: `existsContained` calls `filepath.EvalSymlinks(absJoined)`**
      If `absJoined` is a path to a symlink that points to a non-existent target, `EvalSymlinks` returns an error. `os.IsNotExist(err)` would be true (for the target), returning `existsOutsideOrAbsent`. But what if the symlink itself exists? The sprint plan says `os.Lstat` should be used. Wait, the current implementation uses `EvalSymlinks`. If the path is a broken symlink, `EvalSymlinks` returns an error indicating the target doesn't exist. So it returns `existsOutsideOrAbsent`. This is arguably correct - a broken symlink is not a valid file. But what if the path is a symlink to a directory outside the repo? `EvalSymlinks` resolves to the outside directory, caught by containment. Good.

      However, there is a concern: the sprint plan specifically mentions `os.Lstat` as an alternative to avoid traversing symlinks. The implementation uses `EvalSymlinks` instead. But the plan also says "`filepath.EvalSymlinks` + re-check containment before stat" as an option. So this is acceptable.

      **Issue 11: The `existsContained` function only uses `EvalSymlinks`, not an actual stat/lstat**
      Wait, `EvalSymlinks` returns the canonical path but doesn't check if the final path exists as a regular file? Actually `EvalSymlinks` requires the final component to exist (unlike `Abs`). From Go docs: "EvalSymlinks returns the path name after the evaluation of any symbolic links. If path is relative the result will be relative to the current directory, unless the result would be under the current working directory, in which case it will be an absolute path. On Windows, EvalSymlinks will not allow a volume name to be substituted for its drive letter. If the result would be a Windows UNC path, it will be returned as an absolute path." And importantly: "If path is a symbolic link, the returned path will be the path name of the linked-to file, unless