Let me analyze this diff carefully. The sprint plan is for Epic 5.4: Path-Resolution Hardening, which involves:

1. Building a candidate file index from `git ls-files`
2. Tiered matching (Tier 1: exact basename elsewhere, Tier 2: Levenshtein typo, Tier 3: case-only difference)
3. PathSuggestion field threaded through the codebase
4. Symlink-safe existence check (os.Lstat/EvalSymlinks)
5. Tests

Let me go through each file in scope and look for issues.

### Files in scope:
- `internal/stream/validate.go` - existence check, case-exact check, PathSuggestion field
- `internal/stream/*` (new) - candidate index + Levenshtein helper
- `internal/reconcile/validate.go` - build index once, pass to per-finding validation
- `internal/reconcile/emit.go` - carry PathSuggestion into JSONFinding + report.md warning line
- `internal/report/render.go` - render suggestion in writePathWarning across all views
- `*_test.go` - per-tier unit tests + e2e

Let me review each file:

### 1. `internal/stream/fileindex.go`

```go
func BuildFileIndex(root string) *FileIndex {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return indexFromPaths(strings.Split(string(out), "\x00"))
}
```

This looks fine. It handles errors gracefully and returns nil for degradation.

```go
func indexFromPaths(raw []string) *FileIndex {
	idx := &FileIndex{
		tracked:  make(map[string]struct{}),
		basename: make(map[string][]string),
		dirFiles: make(map[string][]string),
		folded:   make(map[string][]string),
	}
	for _, r := range raw {
		rel := strings.TrimSpace(r)
		if rel == "" {
			continue
		}
		rel = filepath.ToSlash(rel)
		if _, seen := idx.tracked[rel]; seen {
			continue
		}
		idx.tracked[rel] = struct{}{}

		base := path.Base(rel)
		idx.basename[base] = append(idx.basename[base], rel)

		d := path.Dir(rel)
		idx.dirFiles[d] = append(idx.dirFiles[d], base)

		fold := strings.ToLower(rel)
		idx.folded[fold] = append(idx.folded[fold], rel)
	}
	return idx
}
```

This looks correct. The `git ls-files -z` output is NUL-delimited, so splitting on `\x00` is correct. The `strings.TrimSpace` handles any trailing empty string from the split.

One thing I notice: the `folded` map uses `strings.ToLower(rel)` which is Unicode-aware but not full case folding. For example, the German ß would not fold to SS. But for file paths, this is probably fine - the sprint plan says "Unicode-simple case folding (lowercase)".

### 2. `internal/stream/levenshtein.go`

```go
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < len(rb) {
		ra, rb = rb, ra
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
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
}
```

This is the standard two-row Levenshtein implementation. It looks correct. The swap ensures the inner loop is over the shorter string.

```go
func similarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	la, lb := len([]rune(a)), len([]rune(b))
	longest := la
	if lb > longest {
		longest = lb
	}
	if longest == 0 {
		return 1.0
	}
	return 1.0 - float64(levenshtein(a, b))/float64(longest)
}
```

This looks correct.

### 3. `internal/stream/suggest.go`

```go
func (x *FileIndex) CaseCorrection(citedRel string) (suggestion string, mismatch bool) {
	if x == nil {
		return "", false
	}
	rel := filepath.ToSlash(citedRel)
	if x.Has(rel) {
		return "", false
	}
	folded := x.ByFold(rel)
	switch len(folded) {
	case 0:
		return "", false
	case 1:
		return folded[0], true
	default:
		return "", true
	}
}
```

This looks correct. It checks for case-only differences.

```go
func (x *FileIndex) MissingSuggestion(citedRel string) string {
	if x == nil {
		return ""
	}
	rel := filepath.ToSlash(citedRel)
	base := path.Base(rel)
	dir := path.Dir(rel)

	if s := x.tier1(rel, base, dir); s != "" {
		return s
	}
	return x.tier2(rel, base, dir)
}
```

```go
func (x *FileIndex) tier1(rel, base, dir string) string {
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
	bestScore, best, tie := -1, "", false
	for _, c := range candidates {
		if c == rel {
			continue
		}
		score := segOverlap(dir, path.Dir(c))
		switch {
		case score > bestScore:
			bestScore, best, tie = score, c, false
		case score == bestScore:
			tie = true
		}
	}
	if tie || best == "" {
		return ""
	}
	return best
}
```

Wait, there's a subtle issue in tier1. When there are multiple candidates and some have the same score as the best, `tie` is set to true. But what if a later candidate has a HIGHER score? Let me trace through:

- Initial: bestScore=-1, best="", tie=false
- Candidate A: score=2 → 2 > -1 → bestScore=2, best=A, tie=false
- Candidate B: score=2 → 2 == 2 → tie=true
- Candidate C: score=3 → 3 > 2 → bestScore=3, best=C, tie=false

So after processing C, tie is false again. That's correct! The tie flag is reset when a new best is found. Good.

But wait, what about:
- Candidate A: score=2 → bestScore=2, best=A, tie=false
- Candidate B: score=3 → 3 > 2 → bestScore=3, best=B, tie=false
- Candidate C: score=3 → 3 == 3 → tie=true

This is correct - B and C tie, so no suggestion.

What about:
- Candidate A: score=2 → bestScore=2, best=A, tie=false
- Candidate B: score=2 → tie=true
- Candidate C: score=3 → bestScore=3, best=C, tie=false

After C, tie is false and best is C with score 3. But A and B both had score 2, which is less than 3. So C is the unique winner. This is correct.

Actually wait, let me re-read the code more carefully:

```go
case score > bestScore:
    bestScore, best, tie = score, c, false
case score == bestScore:
    tie = true
```

When score > bestScore, tie is reset to false. When score == bestScore, tie is set to true. This means:
- If we have candidates with scores [2, 2, 3], after processing all: bestScore=3, best=C, tie=false → returns C. Correct.
- If we have candidates with scores [3, 2, 3], after processing: bestScore=3, best=C, tie=true → returns "". Correct, because A and C both have score 3.
- If we have candidates with scores [3, 3, 2], after processing: bestScore=3, best=A, tie=true → returns "". Correct.

OK, this logic is correct.

```go
func (x *FileIndex) tier2(rel, base, dir string) string {
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
		switch {
		case score > bestScore:
			bestScore, best, tie = score, cand, false
		case score == bestScore:
			tie = true
		}
	}
	if tie || best == "" {
		return ""
	}
	if dir == "." {
		return best
	}
	return dir + "/" + best
}
```

Wait, there's a potential issue here. The `bestScore` is initialized to `tier2SimilarityThreshold` (0.75), and the condition is `score > bestScore`. So a score of exactly 0.75 would NOT be accepted. The sprint plan says "above the configured similarity threshold", so this is correct - it's strictly greater than.

But wait, the test says:
```go
assert.True(t, similarity("validator", "validate") >= 0.75)
```

And the similarity is approximately 0.777. So 0.777 > 0.75 is true, and the suggestion would be made. That's correct.

But what if the similarity is exactly 0.75? The condition `score > bestScore` with bestScore=0.75 would be false, so no suggestion. The sprint plan says "above the configured similarity threshold", so strictly greater is correct.

Actually, let me re-check: the initial `bestScore` is `tier2SimilarityThreshold` which is 0.75. The condition is `score > bestScore`. So:
- If score = 0.75, it's not > 0.75, so no match. Correct per "above threshold".
- If score = 0.76, it's > 0.75, so match. Correct.

This looks fine.

One issue I see: in `tier2`, the `bestScore` starts at `tier2SimilarityThreshold`, and when a candidate is found with `score > bestScore`, `bestScore` is updated to that score. So subsequent candidates need to have a score > the new bestScore to win. If a subsequent candidate has the same score, `tie` is set to true. This means if two candidates both have score 0.80, the second one would set tie=true and no suggestion is returned. This is the intended behavior per the sprint plan ("no suggestion when ambiguous").

But wait, there's a subtle issue. What if:
- Candidate A: score=0.80 → bestScore=0.80, best=A, tie=false
- Candidate B: score=0.78 → 0.78 < 0.80, neither condition matches → nothing changes
- Candidate C: score=0.80 → 0.80 == 0.80 → tie=true

After processing all, tie=true, so no suggestion. But A and C both have score 0.80, which is correct - they're ambiguous.

What about:
- Candidate A: score=0.80 → bestScore=0.80, best=A, tie=false
- Candidate B: score=0.85 → 0.85 > 0.80 → bestScore=0.85, best=B, tie=false
- Candidate C: score=0.80 → 0.80 < 0.85, neither condition matches → nothing changes

After processing all, best=B with score 0.85, tie=false → returns B. Correct.

This all looks correct.

### 4. `internal/stream/validate.go`

Let me look at the `existsContained` function:

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

This looks correct. It resolves symlinks in both root and joined, then checks containment.

Wait, there's a subtle issue. `filepath.EvalSymlinks` on a non-existent path returns an error. If the path doesn't exist, `os.IsNotExist(err)` would be true, and it returns `existsOutsideOrAbsent`. But what if the path exists but a parent directory is a symlink that points to a non-existent target? In that case, `EvalSymlinks` would also return an error, but it might not be `os.IsNotExist`. Let me think...

Actually, if a parent directory symlink points to a non-existent target, `EvalSymlinks` would return a "no such file or directory" error, which `os.IsNotExist` would catch. So it would return `existsOutsideOrAbsent`, which seems correct - the file doesn't exist.

What about the case where `EvalSymlinks` fails for other reasons (e.g., a symlink loop)? It would fall through to `default: return existsIndeterminate`, which leaves the finding unflagged. That's the safe behavior.

Now let me look at the main `ValidatePath` function:

```go
func ValidatePath(f *Finding, root string, idx *FileIndex) {
	if f == nil {
		return
	}
	if f.File == "" {
		return
	}
	if root == "" {
		root = "."
	}
	joined := filepath.Join(root, filepath.Clean(f.File))
	// ... lexical containment check ...
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
		// Indeterminate: leave unflagged
	}
}
```

Wait, I see an issue. When the path is valid (existsInside), `PathSuggestion` is not cleared. If a Finding is reused (which is unlikely but possible), a stale PathSuggestion could remain. But actually, looking at the code, Findings are created fresh each time, so this is probably not an issue in practice. But it's still a potential correctness issue if the function is called multiple times on the same Finding.

Actually, let me re-read the code. The function sets `f.PathValid = true` and `f.PathWarning = ""` but doesn't clear `f.PathSuggestion`. If the Finding was previously processed and had a PathSuggestion set, it would remain. But in practice, each Finding is validated once, so this is unlikely to be a real issue. Still, it's a correctness concern.

Wait, but actually, looking at the flow: `validateFindingPaths` iterates over findings and calls `ValidatePath` once per finding. The findings are freshly merged. So PathSuggestion would be empty initially. This is not a real issue.

But wait, there's another issue. When the case correction check fires (mismatch=true), it sets PathSuggestion and returns. But what if the path actually exists on disk (on a case-insensitive filesystem)? The function would flag it as invalid with a suggestion, even though the file exists. But that's the intended behavior per AC3: "a case-only typo is flagged invalid AND suggests the correctly-cased path, including on case-insensitive filesystems."

Actually, let me re-read the CaseCorrection logic:

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
		return "", true // ambiguous
	}
}
```

If the cited path is byte-exact tracked, it returns ("", false) - no mismatch. If the cited path is not tracked but has a case-folded match, it returns a mismatch. This is correct.

But what about a path that is NOT tracked in git but exists on disk (e.g., an untracked file)? The index wouldn't have it, so CaseCorrection would return ("", false) - no mismatch. Then the existence check would run, and if the file exists on disk, it would be marked as valid. This is correct per the sprint plan: "untracked files are not review targets" but the existence check still works.

Actually wait, let me re-read the sprint plan: "Cross-repo / non-tracked-file matching (untracked files are not review targets)" is out of scope. But the existence check still runs on the filesystem. So an untracked file that exists on disk would be marked as PathValid=true. This is the existing 5.0 behavior and is not changed by 5.4. The index is only used for suggestions, not for existence validation.

Hmm, but actually, looking at the code flow again:

1. Case correction check (if idx != nil)
2. Existence check

If a file is tracked and byte-exact, CaseCorrection returns ("", false), and the existence check runs. If the file exists on disk, it's valid. If it doesn't exist on disk (e.g., deleted but still tracked), the existence check would fail, and then MissingSuggestion would run. But MissingSuggestion checks the index, which has the file as tracked. Let me trace through:

- File "internal/auth/validate.go" is tracked but deleted from disk
- CaseCorrection: x.Has("internal/auth/validate.go") → true → returns ("", false)
- existsContained: file doesn't exist → existsOutsideOrAbsent
- MissingSuggestion("internal/auth/validate.go"):
  - tier1: ByBasename("validate.go") returns ["internal/auth/validate.go"], candidates[0] == rel → returns ""
  - tier2: HasDir("internal/auth") → true, DirBasenames includes "validate.go", but cand == base → continue. No other candidates → returns ""

So no suggestion is made for a tracked-but-deleted file. This seems correct - you wouldn't want to suggest the same file that was cited.

Actually, wait. Let me look at the test `TestMissingSuggestion_NeverSuggestsSelf`:

```go
func TestMissingSuggestion_NeverSuggestsSelf(t *testing.T) {
	idx := indexFromPaths([]string{"internal/auth/validate.go"})
	require.NotNil(t, idx)

	assert.Empty(t, idx.MissingSuggestion("internal/auth/validate.go"))
}
```

This tests that a tracked file cited exactly doesn't get a suggestion. But in practice, this case would be caught by CaseCorrection returning ("", false) and then the existence check finding the file. MissingSuggestion would only be called if the file doesn't exist on disk. But the test is still valid as a unit test of the MissingSuggestion function.

OK, let me look for actual issues now.

### Issue 1: PathSuggestion not cleared on valid path

In `ValidatePath`, when the path is valid (`existsInside`), `PathSuggestion` is not explicitly cleared:

```go
case existsInside:
    f.PathValid = true
    f.PathWarning = ""
    // PathSuggestion not cleared!
```

But as I noted, in practice Findings are fresh, so this is not a real issue. However, it's still a potential correctness concern if the function is ever called on a reused Finding. Let me check if this is actually possible...

Looking at `validateFindingPaths`:
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

Each finding is validated once. The Merged findings are freshly created during reconciliation. So PathSuggestion would be empty initially. This is not a real issue.

### Issue 2: `filepath.EvalSymlinks` on root

In `existsContained`, `filepath.EvalSymlinks(absRoot)` is called. If root itself is a symlink, this resolves it. But what if root doesn't exist? `EvalSymlinks` would return an error, and `realRoot` falls back to `absRoot`. Then `filepath.Rel(realRoot, resolved)` would use the unresolved root. This could potentially cause issues if root is a symlink to a different path. But in practice, root is always a valid directory (it's the repo root), so this is not a real issue.

### Issue 3: Performance of `existsContained`

`existsContained` calls `filepath.EvalSymlinks` twice (once for root, once for joined). `EvalSymlinks` for root is called on every finding, but root never changes. This could be optimized by resolving root once and passing it in. However, the sprint plan says "No measurable reconcile slowdown beyond the one-time `git ls-files` index build", and `EvalSymlinks` on root is a single syscall that's likely cached by the OS. This is a minor performance concern, not a real issue.

Actually wait, this IS called per-finding. For a repo with 100 findings, `filepath.EvalSymlinks(absRoot)` would be called 100 times. This is unnecessary since root doesn't change. But the OS likely caches the stat result, so the performance impact is negligible. Still, it's a maintainability/performance concern.

Let me check the severity... The sprint plan says "No measurable reconcile slowdown beyond the one-time `git ls-files` index build." The `EvalSymlinks` on root is called per-finding, which is O(n) syscalls. For a typical review with ~50-100 findings, this is 50-100 extra stat syscalls, which is negligible. I'll flag it as LOW.

### Issue 4: `git ls-files` doesn't include staged but uncommitted files

`git ls-files` lists tracked files. If a file is staged but not committed, it's included. If a file is newly added and staged, it's included. If a file is modified but not staged, it's still tracked and included. This seems correct for the use case.

But what about a file that was `git rm`'d but not committed? It would still be in `git ls-files` until the commit. This is an edge case but not a real issue.

### Issue 5: `strings.ToLower` for case folding

The `folded` map uses `strings.ToLower(rel)` which is Unicode-aware but not full case folding. For example:
- German ß (sharp s) lowercases to itself, not "ss"
- Turkish İ (dotted I) lowercases to "i̇" (i with combining dot above)

For file paths in a code repository, this is almost certainly fine. The sprint plan explicitly says "Unicode-simple case folding (lowercase)". Not an issue.

### Issue 6: `filepath.ToSlash` inconsistency

In `fileindex.go`, paths are normalized with `filepath.ToSlash`. In `suggest.go`, `filepath.ToSlash` is called on input paths. In `validate.go`, the `f.File` is used directly in `idx.CaseCorrection(f.File)` and `idx.MissingSuggestion(f.File)`, both of which call `filepath.ToSlash` internally. This is consistent.

But wait, in `validate.go`, the `joined` path is computed with `filepath.Join(root, filepath.Clean(f.File))`, which uses OS-specific separators. Then `existsContained(root, joined)` is called with OS-specific paths. Inside `existsContained`, `filepath.Abs` and `filepath.EvalSymlinks` work with OS-specific paths. This is correct.

The index uses slash-normalized paths, and the index methods (`Has`, `ByBasename`, etc.) call `filepath.ToSlash` on input. So there's no mismatch. Good.

### Issue 7: `path` vs `filepath` package usage

In `fileindex.go` and `suggest.go`, both `path` and `filepath` packages are used:
- `path.Base(rel)` - works with slash-separated paths
- `path.Dir(rel)` - works with slash-separated paths
- `filepath.ToSlash(rel)` - converts OS-specific to slash

This is correct because after `filepath.ToSlash`, the paths are slash-separated, so `path.Base` and `path.Dir` work correctly.

### Issue 8: `splitStem` for dotfiles

```go
func splitStem(base string) (stem, ext string) {
	ext = path.Ext(base)
	return strings.TrimSuffix(base, ext), ext
}
```

For a dotfile like `.gitignore`:
- `path.Ext(".gitignore")` returns "" (no extension)
- `strings.TrimSuffix(".gitignore", "")` returns ".gitignore"
- So stem=".gitignore", ext=""

For a file like `.env.local`:
- `path.Ext(".env.local")` returns ".local"
- stem=".env", ext=".local"

This seems reasonable. The sprint plan doesn't mention dotfiles specifically, so this is fine.

### Issue 9: Potential issue with `tier2` when `dir == "."`

```go
if dir == "." {
    return best
}
return dir + "/" + best
```

When the file is in the repo root, `path.Dir("file.go")` returns ".". So `dir == "."` and the suggestion is just the basename. This is correct - a root-level file's relpath is just the basename.

### Issue 10: `segOverlap` uses set intersection

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

This counts unique shared segments. If a path has duplicate segments (e.g., "foo/foo/bar"), each segment is counted once. This seems reasonable for ranking purposes.

### Issue 11: `prefixDerivation` check

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

This checks if one stem is a strict prefix of the other. For "validator" and "validate":
- Neither is a prefix of the other ("validator" starts with "validat" but "validate" doesn't start with "validator")
- So `prefixDerivation("validator", "validate")` returns false. Correct.

For "user" and "users":
- "user" is a prefix of "users"
- So `prefixDerivation("user", "users")` returns true. Correct.

For "parse" and "parser":
- "parse" is a prefix of "parser"
- So `prefixDerivation("parse", "parser")` returns true. Correct.

This looks correct.

But wait, what about "handler" and "handlers"? 
- "handler" is a prefix of "handlers"
- `prefixDerivation("handler", "handlers")` returns true. Correct.

And "route" and "router"?
- "route" is a prefix of "router"
- `prefixDerivation("route", "router")` returns true. Correct.

OK, the prefixDerivation logic is correct.

### Issue 12: `ValidatePath` doesn't clear `PathSuggestion` when path is valid

Let me look at this more carefully:

```go
switch existsContained(root, joined) {
case existsInside:
    f.PathValid = true
    f.PathWarning = ""
    // PathSuggestion not cleared
case existsOutsideOrAbsent:
    f.PathValid = false
    f.PathWarning = PathNotFoundWarning
    if idx != nil {
        f.PathSuggestion = idx.MissingSuggestion(f.File)
    }
default:
    // Indeterminate: leave unflagged
    // PathSuggestion not cleared
}
```

In the `existsInside` case, `PathSuggestion` is not cleared. In the `default` case, `PathSuggestion` is not cleared. If a Finding struct is reused (which shouldn't happen in practice), a stale PathSuggestion could remain.

But more importantly, in the `existsOutsideOrAbsent` case, `f.PathSuggestion = idx.MissingSuggestion(f.File)` is only called when `idx != nil`. If `idx == nil`, `PathSuggestion` is not cleared. But since Findings are fresh, this is not a real issue.

Actually, I realize there IS a potential issue. If `idx != nil` and the path is invalid, `MissingSuggestion` might return "" (no suggestion). In that case, `f.PathSuggestion` is set to "". This is correct.

But what about the case where `idx != nil`, the path is a case mismatch, and `CaseCorrection` returns `("", true)` (ambiguous)? In that case:

```go
if suggestion, mismatch := idx.CaseCorrection(f.File); mismatch {
    f.PathValid = false
    f.PathWarning = PathNotFoundWarning
    f.PathSuggestion = suggestion // "" when ambiguous
    return
}
```

`PathSuggestion` is set to "" (empty). This is correct - no suggestion when ambiguous.

### Issue 13: `existsContained` resolves root symlinks every call

As I noted earlier, `filepath.EvalSymlinks(absRoot)` is called in every call to `existsContained`, which is called once per finding. Root doesn't change between findings, so this is redundant work.

This is a performance issue, but it's minor. The OS caches stat results, so the actual cost is negligible. I'll flag it as LOW.

### Issue 14: Missing test for symlink-escape with suggestion

Looking at the tests, there's `TestValidatePath_SymlinkEscapeNoSuggestion` which tests that a symlink-escaping path doesn't get a suggestion. But the path "link/known" wouldn't be in the git index anyway (it's not a tracked file), so `MissingSuggestion` would return "" regardless. The test is valid but doesn't specifically test that the symlink check prevents suggestions.

Actually, wait. The test creates a git repo with "internal/real.go" tracked, and a symlink "link" pointing outside. The cited path is "link/known". Let me trace through:

1. Lexical containment: "link/known" doesn't escape lexically (no "..")
2. CaseCorrection: idx.Has("link/known") → false (not tracked). ByFold("link/known") → empty. Returns ("", false).
3. existsContained: EvalSymlinks resolves "link" to the outside dir, so "link/known" resolves to outside/root. Containment check fails → existsOutsideOrAbsent.
4. MissingSuggestion("link/known"): tier1: ByBasename("known") → empty (not tracked). tier2: HasDir("link") → false (no tracked files under "link"). Returns "".

So the test passes because "known" is not a tracked file. The test is valid but doesn't test the case where a symlink-escaping path has a basename that IS tracked elsewhere. Let me check if that's tested...

Actually, looking at AC5: "The existence check does not traverse symlinks out of the repo root (verified by a test with `link -> /tmp` and `File="link/known"` staying flagged invalid with no suggestion)."

The test `TestValidatePath_SymlinkEscapeNoSuggestion` does verify this. The fact that "known" is not tracked means no suggestion, which is the expected behavior. But what if "known" were tracked elsewhere? Let me think...

If "known" were tracked at, say, "internal/known", then:
1. existsContained would still return existsOutsideOrAbsent (symlink escape)
2. MissingSuggestion("link/known"): tier1: ByBasename("known") → ["internal/known"]. candidates[0] != "link/known" → returns "internal/known"

So a suggestion WOULD be made! Is this correct? The sprint plan says the path should be "flagged invalid with no suggestion" for the symlink case. But the code would actually suggest "internal/known" because the basename matches.

Wait, let me re-read AC5: "The existence check does not traverse symlinks out of the repo root (verified by a test with `link -> /tmp` and `File="link/known"` staying flagged invalid with no suggestion)."

The test uses "link/known" where "known" is NOT a tracked file. So the test passes. But if "known" were tracked elsewhere, the code would suggest it. Is this a bug?

Actually, I think this is fine. The symlink check prevents the file from being reported as VALID (existsInside). But the suggestion logic still runs, and if there's a confident match, it suggests it. The suggestion is advisory only - it doesn't change the fact that the cited path is invalid. The user would see:

"⚠️ File not found: link/known (did you mean internal/known?)"

This is actually helpful behavior - the user cited a wrong path, and the system suggests the right one. The symlink check prevents the file from being reported as present (which would be an existence oracle), but the suggestion is still useful.

But wait, the AC says "staying flagged invalid with no suggestion". Let me re-read...

"verified by a test with `link -> /tmp` and `File="link/known"` staying flagged invalid with no suggestion"

The test uses "link/known" where "known" is not tracked, so no suggestion. The AC is specifically about this test case, not about all symlink-escaping paths. If "known" were tracked elsewhere, a suggestion would be appropriate.

Actually, I think there might be an issue here. Let me re-read the code:

```go
case existsOutsideOrAbsent:
    f.PathValid = false
    f.PathWarning = PathNotFoundWarning
    if idx != nil {
        f.PathSuggestion = idx.MissingSuggestion(f.File)
    }
```

When the path escapes via symlink (`existsOutsideOrAbsent`), the code still runs `MissingSuggestion`. But the sprint plan says for the symlink case, there should be "no suggestion". Let me re-read the sprint plan more carefully...

"### Symlink safety (pain point #3)
Switch the existence check to `os.Lstat` (so a symlinked segment is not traversed) or `filepath.EvalSymlinks` + re-check containment before stat."

And AC5: "The existence check does not traverse symlinks out of the repo root (verified by a test with `link -> /tmp` and `File="link/known"` staying flagged invalid with no suggestion)."

The AC says "staying flagged invalid with no suggestion" - but this is specifically for the test case where "known" is not tracked. If "known" were tracked elsewhere, a suggestion would be appropriate and helpful.

Actually, wait. I think the AC is saying that the specific test case (with "link/known" where "known" is not tracked) should result in no suggestion. It's not saying that ALL symlink-escaping paths should have no suggestion. The test