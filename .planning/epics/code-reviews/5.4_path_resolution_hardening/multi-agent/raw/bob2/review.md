We are reviewing the diff for the sprint plan "Epic 5.4: Path-Resolution Hardening".
 The scope is limited to the files mentioned in the sprint plan under "Files Affected".

 Let's list the files that are in scope according to the sprint plan:

   internal/stream/validate.go
   internal/stream/* (new)   -> meaning new files in internal/stream/
   internal/reconcile/validate.go
   internal/reconcile/emit.go
   internal/report/render.go
   *_test.go (for the above)

 Now, let's look at the diff provided and check for issues only in these files.

 We are to output only the findings in the TD_STREAM format.

 We'll go through each file change in the diff and look for potential issues.

 Note: We must not flag issues in files not in the scope.

 Let's break down the diff by file:

 1. internal/reconcile/emit.go
 2. internal/reconcile/emit_test.go
 3. internal/reconcile/pathsuggest_e2e_test.go (new file)
 4. internal/reconcile/validate.go
 5. internal/report/render.go
 6. internal/report/validate_test.go
 7. internal/stream/fileindex.go (new file)
 8. internal/stream/fileindex_test.go (new file)
 9. internal/stream/levenshtein.go (new file)
10. internal/stream/levenshtein_test.go (new file)
11. internal/stream/parser.go
12. internal/stream/suggest.go (new file)
13. internal/stream/suggest_test.go (new file)
14. internal/stream/validate.go
15. internal/stream/validate_test.go

 All of these are in the scope because:
   - internal/stream/* (new) covers the new files in internal/stream/
   - internal/reconcile/validate.go, internal/reconcile/emit.go are explicitly listed.
   - internal/report/render.go is explicitly listed.
   - The test files are for the above and are allowed.

 Now, we look for issues in these files.

 We are to be precise and only report real issues.

 Let's examine each file change:

 internal/reconcile/emit.go:
   - Added a new field `PathSuggestion` to `JSONFinding`.
   - In `JSONFindings`, we are setting `PathSuggestion: m.PathSuggestion`.
   - In `writeFindingsList`, we are conditionally printing the suggestion.

   I don't see any obvious issue here.

 internal/reconcile/emit_test.go:
   - Added tests for the new field.

   Looks good.

 internal/reconcile/pathsuggest_e2e_test.go:
   - New end-to-end test.

   Looks good.

 internal/reconcile/validate.go:
   - Changed the function signature of `validateFindingPaths` to take an index and pass it to `stream.ValidatePath`.
   - Added a call to `stream.BuildFileIndex(root)` once per reconcile run.

   This seems correct.

 internal/report/render.go:
   - Changed `writePathWarning` to conditionally print the suggestion.

   Looks good.

 internal/report/validate_test.go:
   - Added tests for the new suggestion rendering.

   Looks good.

 internal/stream/fileindex.go:
   - New file implementing the candidate index.

   We should check for potential issues:

   - The function `BuildFileIndex` returns nil on error (non-git repo, etc.) which is intended for graceful degradation.
   - The index is built from `git ls-files -z` and then split by null.

   However, note that the function `indexFromPaths` uses `strings.Split(string(out), "\x00")` which is correct for null-delimited.

   But note: the output of `git ls-files -z` is null-delimited, but if there are no files, we get an empty string and then split by null gives [""]?
   Actually, if there are no files, `out` is empty, then `string(out)` is "", and `strings.Split("", "\x00")` returns [""] (one element which is empty string).

   Then in the loop, we have `rel := strings.TrimSpace(r)` which for an empty string becomes empty string and we skip.

   So it's safe.

   However, there is a potential issue: the function `Has` uses `filepath.ToSlash(relpath)` but note that the index stores the paths already converted to slash by `filepath.ToSlash` in `indexFromPaths`.
   So when we check, we convert the input to slash again. This is safe because if the input is already slash, it remains slash; if it's OS-specific, we convert to slash.

   But note: the index keys are stored in slash form (because we did `filepath.ToSlash(rel)` when building). So when we check, we convert the input to slash and then look up.

   This is correct.

   However, note that the function `HasDir` and `DirBasenames` also use `filepath.ToSlash` on the input.

   This is consistent.

   I don't see an obvious bug.

 internal/stream/fileindex_test.go:
   - Tests for the index.

   Looks good.

 internal/stream/levenshtein.go and internal/stream/levenshtein_test.go:
   - Standard Levenshtein implementation and tests.

   Looks good.

 internal/stream/parser.go:
   - Added `PathSuggestion` field to `Finding`.

   Looks good.

 internal/stream/suggest.go:
   - New file implementing the suggestion logic.

   We should check for potential issues:

   - The function `CaseCorrection`:
        It returns (suggestion, mismatch). If there's a case-only mismatch and exactly one match, it returns the real path and true.
        If there are multiple matches (ambiguous), it returns ("", true) -> meaning mismatch but no suggestion.

   - The function `MissingSuggestion`:
        It tries Tier 1 (exact basename elsewhere) then Tier 2 (same-dir typo).

   - In `tier1`:
        It returns the basename match in another directory if there's exactly one (and it's not the same as the cited path, which it can't be because the cited path is missing).
        If there are multiple, it ranks by path-segment overlap and returns the best if unique, else empty.

   - In `tier2`:
        It requires that the directory exists (has at least one tracked file directly under it).
        Then it compares the stem (without extension) of the cited file with each candidate in the same directory (same extension only).
        It skips if the candidate stem is a strict prefix of the cited stem or vice versa (to avoid suggesting plurals/derviations).
        Then it uses similarity (from levenshtein) and picks the highest above threshold (0.75) if unique.

   - There's a function `prefixDerivation` that checks if one stem is a strict prefix of the other.

   - The function `segOverlap` counts shared path segments.

   - The function `splitStem` splits the basename into stem and extension.

   I notice a potential issue in `tier2`:

        We have:
          citedStem, citedExt := splitStem(base)
          ...
          for _, cand := range x.DirBasenames(dir) {
              if cand == base { ... }
              candStem, candExt := splitStem(cand)
              if candExt != citedExt { ... }
              if prefixDerivation(citedStem, candStem) { ... }
              score := similarity(citedStem, candStem)
              ...

        The problem: we are comparing the stems (without extension) but we are requiring the extensions to be the same.

        This is correct for Tier 2: we are only suggesting files with the same extension.

   However, note that the function `splitStem` returns the extension including the dot.

   Example: "validate.go" -> stem="validate", ext=".go"

   This is correct.

   But note: what if the file has no extension? Then ext is empty string.

   Example: "file" -> stem="file", ext=""

   Then we compare extensions: if both are empty, they match.

   This is correct.

   Another issue: the function `prefixDerivation` is used to avoid suggesting plurals/derviations. However, note that the condition:

        if prefixDerivation(citedStem, candStem) { continue }

   This skips if one stem is a strict prefix of the other.

   But note: the function `prefixDerivation` returns true if one is a strict prefix of the other.

   Example: citedStem = "user", candStem = "users" -> "user" is a prefix of "users", so we skip.

   This is intended.

   However, note that the canonical example "validator" and "validate" are not in a prefix relation? 
        "validator" and "validate": 
          Neither is a prefix of the other? 
          "validat" is common, but then "or" vs "e".

   So it passes.

   But note: the function `prefixDerivation` is case-sensitive? 
        The stems are compared as they are (from the basename, which is the actual string).

   However, note that we are in the context of a directory that we know has the files (from the index) and we are comparing the stems as stored.

   The index stores the actual tracked filenames (with their case).

   The citedRel is the reviewer's input (which we have converted to slash).

   We split the basename of the citedRel to get the stem and extension.

   Then we compare the stem (as given by the reviewer) with the stem of the candidate (from the index).

   This is case-sensitive.

   However, note that we have already done a case-exact check in `CaseCorrection` (Tier 3) and we are now in `MissingSuggestion` only if there was no case-only mismatch.

   So the case of the citedRel might be different from the real file, but we are not doing case folding here.

   This is by design: Tier 2 is for typos in the same directory, and we are comparing the stems as given (case-sensitive) because we have already ruled out case-only differences.

   But wait: what if the reviewer typed the stem with wrong case and also a typo? 
        Example: real file is "validate.go", reviewer wrote "ValidaTOR.go" (wrong case and typo).

   Then:
        - Tier 3 (case-only) would not trigger because the stem "ValidaTOR" (when folded) would match the folded real file "validate" (because we fold to lower) but note: 
          the function `CaseCorrection` folds the entire path? Actually, it folds the entire relpath.

        Let me see: 
          `CaseCorrection` does:
            rel := filepath.ToSlash(citedRel)
            if x.Has(rel) { ... } // false
            folded := x.ByFold(rel)   // returns all tracked files that fold to the same lower string.

          For the example: 
            citedRel = "internal/auth/ValidaTOR.go"
            folded = x.ByFold("internal/auth/validator.go")   [because we lower the entire path]

          If there is only one tracked file that folds to that (i.e., "internal/auth/validate.go"), then we return that as the suggestion and set mismatch=true.

        So the case-only typo would be caught by Tier 3 and we would not get to Tier 2.

   Therefore, in Tier 2 we are only seeing stems that are not case-only different (so the case of the stem in the citedRel might be arbitrary, but we are comparing to the real stem as stored in the index, which is the actual case).

   This means that if the reviewer typed the stem with wrong case and also a typo, we would have caught it in Tier 3 and suggested the correctly cased file (without fixing the typo). 

   But note: the Tier 3 suggestion is the real tracked file (with its original case). So if the reviewer wrote "ValidaTOR.go", we would suggest "validate.go" (the real file) and that would fix both the case and the typo? 

   Actually, no: because the Tier 3 function only checks for case-only differences. It does not correct typos. 

   How does Tier 3 work?
        It returns the real tracked file if there is exactly one tracked file that matches when folded (case-folded) and the cited path is not exactly tracked.

        So for "ValidaTOR.go", we fold to "validator.go" and if there is exactly one tracked file that folds to that (which is "validate.go"), then we return "validate.go".

        This suggests the real file, which has the correct case and the correct spelling.

        So it fixes both.

   Therefore, Tier 2 is only reached when the case of the entire path (when folded) does not match any tracked file, or when there are multiple tracked files that fold to the same string (so ambiguous for case).

   In the case of multiple case-folded matches, we return mismatch=true but no suggestion (so we fall through to Tier 1 and Tier 2).

   Then in Tier 2, we are comparing the stems as given (with the reviewer's case) to the real stems (as stored in the index, which is the actual case).

   This means that if the reviewer typed the stem with wrong case, we are comparing the wrong-case stem to the real-case stem.

   Example: 
        Real file: "validate.go" (stem: "validate")
        Reviewer: "ValidaTOR.go" -> stem: "ValidaTOR" (if we take the basename and split, we get stem="ValidaTOR", ext=".go")

        Now, we are in Tier 2 because the case-folded check found multiple matches? Actually, no: if there is only one real file that folds to "validator.go", then the case-folded check would have returned that one file and we would have suggested it in Tier 3.

        So we only get to Tier 2 if the case-folded check either:
          (a) found no match (so the stem, when folded, doesn't match any tracked file's stem when folded) -> then we know it's not a case-only issue, but it might be a typo in the stem (and we don't care about case because we are going to compare the stems as given? but note: the real stem is in a specific case, and the reviewer's stem is in arbitrary case) OR
          (b) found multiple matches (so ambiguous for case) -> then we don't suggest via case, and we fall back to typo matching.

   In case (a): we have no case match at all, so we want to do a typo match that is case-insensitive? 
        But note: the real file's stem is in a specific case (say "validate"), and the reviewer wrote "ValidaTOR" (which when folded is "validator", same as the real file's stem folded). 
        Wait, that would have been caught in (b) if there was only one? 

        Actually, if there is only one real file that folds to a given string, then the case-folded check returns that one file and we suggest it (and we don't get to Tier 2).

        So we only get to Tier 2 for case-folded when:
          - There is no tracked file that folds to the cited path's folded string -> then we know it's not a case-only issue (so we can do case-sensitive typo matching? but note: the real file's stem in its original case might be close to the reviewer's stem in arbitrary case? We should do case-insensitive stem comparison for the typo?).

        However, the current code in Tier 2 does case-sensitive stem comparison.

        Example: 
          Real file: "validate.go" (stem: "validate")
          Reviewer: "Validator.go" -> stem: "Validator" (note: capital V)

          Case-folded check: 
            citedRel folded: "internal/auth/validator.go"
            real file folded: "internal/auth/validator.go" -> match, and if it's the only one, we would have suggested it in Tier 3.

          So we don't get to Tier 2.

        Now, what if there are two real files: 
          "internal/auth/validate.go" and "internal/auth/Validator.go" (which is impossible on a case-sensitive filesystem, but note: the index is built from `git ls-files` which is case-sensitive? 
          Actually, the index stores the paths as returned by `git ls-files`, which preserves case. 
          But note: on a case-insensitive filesystem, you cannot have two files that differ only in case. 
          However, the index is built from the git index, which is case-sensitive. 
          But the problem states: we are to build the index from `git ls-files`, which returns the paths as stored in the index (which is case-sensitive).

          So if the repo has two files that differ only in case, then `git ls-files` would list both.

          Then, for a cited path that is, say, "internal/auth/validate.go" (lowercase v), the case-folded check would return both files (because both fold to the same string) -> ambiguous -> no suggestion from Tier 3.

          Then we go to Tier 2: 
            We are in the directory "internal/auth", and we look for files with basename matching the cited basename? 
            Actually, no: we are in MissingSuggestion because the cited path is not in the index (exact match). 
            But note: the cited path "internal/auth/validate.go" is in the index? 
                If the repo has "internal/auth/validate.go" (lowercase v) then it is in the index -> so we wouldn't be in MissingSuggestion.

          So let's assume the reviewer typed "internal/auth/Validate.go" (capital V) and the repo has:
                "internal/auth/validate.go" (lowercase v)
                "internal/auth/Validator.go" (capital V)   [if the repo allowed it, but note: on case-insensitive FS you can't have both, but the index is case-sensitive so if the repo has both, then the FS must be case-sensitive]

          Actually, if the repo has two files that differ only in case, then the filesystem must be case-sensitive (like Linux ext4). 
          Then, the reviewer's path "internal/auth/Validate.go" (capital V) is not in the index (if the index has the lowercase one and the uppercase one as two separate files) because:
                The index has:
                  "internal/auth/validate.go"
                  "internal/auth/Validator.go"

          So the cited path "internal/auth/Validate.go" matches the second one exactly? 
                If the reviewer typed exactly "internal/auth/Validator.go", then it would be in the index.

          Therefore, if the reviewer typed a case that matches one of the files exactly, we would have an exact match and not be in the missing path logic.

          So the only way we get to Tier 2 for a path that is not in the index is if the reviewer's path (with whatever case) does not exactly match any tracked file.

          And if there are multiple tracked files that fold to the same string (so case-insensitive duplicate), then we get ambiguous in Tier 3 and fall to Tier 2.

          In Tier 2, we are comparing the stem of the cited path (with the reviewer's case) to the stem of each candidate (with the candidate's case as stored in the index).

          This is case-sensitive.

          Example: 
            Repo has two files: 
                "internal/auth/foo.go"   (stem: "foo")
                "internal/auth/FOO.go"   (stem: "FOO")
            Reviewer types: "internal/auth/Foo.go"   (stem: "Foo")

          Then:
            Tier 3: 
                citedRel folded: "internal/auth/foo.go"
                matches both files -> ambiguous -> no suggestion from Tier 3.

            Tier 2:
                We look in directory "internal/auth" for files with extension ".go".
                Candidates: ["foo.go", "FOO.go"]
                For candidate "foo.go": 
                    stem = "foo", citedStem = "Foo"
                    similarity("Foo", "foo") = ? 
                      levenshtein("Foo", "foo") = 1 (change 'F' to 'f') -> similarity = 1 - 1/3 = 0.666... -> below 0.75 -> skip.
                For candidate "FOO.go":
                    stem = "FOO", citedStem = "Foo"
                    levenshtein("Foo", "FOO") = 1 (change 'o' to 'O' and 'o' to 'O'? Actually: 
                         Foo -> FOO: 
                            F->F, o->O, o->O -> two substitutions? 
                         Actually: 
                            Foo: F, o, o
                            FOO: F, O, O
                         So two substitutions -> distance=2 -> similarity = 1 - 2/3 = 0.333...

                So no suggestion.

          But note: the reviewer meant one of the two, and we didn't suggest.

          This is acceptable because we don't want to guess when there's ambiguity.

   However, note that the Open Question 2 says: "Multiple equal candidates. Report ambiguity (no suggestion) vs list top-N. Recommendation: no suggestion when ambiguous".

   So we are following that.

   Therefore, the code in `suggest.go` seems to be in line with the requirements.

   But note: there is a comment in the Open Question 1 about the threshold. The threshold is set to 0.75.

   The test in `levenshtein_test.go` shows:
        assert.InDelta(t, 0.777, similarity("validator", "validate"), 0.01)
        assert.True(t, similarity("validator", "validate") >= 0.75)

   So the example passes.

   However, note that the similarity function is defined as:
        1.0 - (edit_distance / max(len(a), len(b)))

   For "validator" (9 letters) and "validate" (8 letters):
        edit_distance = 2 (as per the test: they say it's actually 2)
        max(len) = 9
        similarity = 1 - 2/9 = 7/9 ≈ 0.777...

   So the threshold 0.75 is set to accept this.

   Now, note that the function `tier2` uses:
        bestScore, best, tie := tier2SimilarityThreshold, "", false

   and then updates if score > bestScore.

   So it requires strictly greater than the threshold to update the best.

   But note: we want to accept the example which is 0.777... which is > 0.75.

   However, what if we have a score exactly 0.75? 
        We would not update because we require >.

   The threshold is set to 0.75, and we want to accept scores >= 0.75? 
        The comment says: "the threshold is set to admit that example with margin"

   But the example is 0.777, so we have margin.

   However, the Open Question 1 says: "do not apply a distance threshold to Tier 1 ... for Tier 2 use ~85% on the stem"

   And 85% is 0.85, but we set it to 0.75 to admit the example.

   This is a deliberate choice to tune against the 5.0 examples.

   So it's acceptable.

   But note: the function `similarity` returns a float64, and we are comparing with >.

   There is a potential for floating point inaccuracies, but the numbers are simple.

   I don't see an issue.

   However, note that the function `tier2` initializes `bestScore` to the threshold, and then we look for a score strictly greater than the current best.

   This means that if we have a candidate with score exactly equal to the threshold, we do not take it.

   But we want to take candidates that are at least the threshold? 

   The Open Question 1 says: "use ~85% on the stem", meaning at least 85%? 

   But note: the example is 77.7% and we are using 75% to accept it.

   The intention is to accept the example and reject clearly different ones.

   The threshold is set to 0.75, and we want to accept scores >= 0.75? 

   However, the code uses:
        if score > bestScore { ... }

   and we start bestScore at the threshold.

   So the first candidate that has a score > threshold will become the best.

   But note: we want to accept a candidate that is exactly at the threshold? 
        The Open Question doesn't specify, but the example is above.

   However, to be safe, we might want to change to >=? 

   But note: the problem says "above the configured similarity threshold" in AC4.

   AC4: "a basename typo in an existing directory (e.g. validator.go → validate.go) yields the closest tracked file as PathSuggestion when above the configured similarity threshold"

   So "above" means strictly greater.

   Therefore, the code is correct.

   However, note that the threshold is set to 0.75, and the example is 0.777... which is above.

   So we are good.

   But wait: what if there are two candidates with the same score above the threshold? 
        Then we set `tie = true` and return no suggestion.

   This is correct.

   I don't see an issue in `suggest.go`.

 internal/stream/validate.go:
   - Changed the function signature to take an index.
   - Added the case-only check (Tier 3) before the existence check.
   - Added the symlink-safe existence check via `existsContained`.
   - After the existence check, if the file is not found (existsOutsideOrAbsent) and we have an index, we set the PathSuggestion from `idx.MissingSuggestion`.

   Let's check the flow:

        if idx != nil {
            if suggestion, mismatch := idx.CaseCorrection(f.File); mismatch {
                f.PathValid = false
                f.PathWarning = PathNotFoundWarning
                f.PathSuggestion = suggestion   // note: suggestion might be empty if ambiguous
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
        default: // existsIndeterminate
            // leave unflagged
        }

   This seems correct.

   However, note that in the case of a case-only mismatch that is ambiguous (multiple case-folded matches), we set:
        f.PathSuggestion = suggestion   // which is "" (from CaseCorrection: when ambiguous, returns ("", true))

   So we set PathSuggestion to empty string.

   Then we return.

   This is correct: we flag the path as invalid (PathValid=false, PathWarning set) but we do not set a suggestion (because it's ambiguous).

   Now, what about the symlink safety?

        We replaced the simple `os.Stat` with `existsContained` which uses `filepath.EvalSymlinks` and then checks containment.

        This should prevent following symlinks out of the repo.

   However, note the function `existsContained`:

        absRoot, err := filepath.Abs(root)
        if err != nil { absRoot = root }
        absJoined, err := filepath.Abs(joined)
        if err != nil { absJoined = joined }
        realRoot, err := filepath.EvalSymlinks(absRoot)
        if err != nil { realRoot = absRoot }
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

   This function returns:
        existsInside: if the resolved path is inside the resolved root (and we can compute the relative path and it doesn't escape)
        existsOutsideOrAbsent: if the resolved path is outside the resolved root (by symlink) or if the file does not exist.
        existsIndeterminate: if there was an error (other than not exist) in evaluating the symlinks.

   Note: if the file does not exist, we get os.IsNotExist(err) and return existsOutsideOrAbsent.

   This is correct.

   However, note that we are using `filepath.EvalSymlinks` on both the root and the joined path.

   This is the correct way to check if the resolved path is under the resolved root.

   But note: what if the root itself is a symlink? 
        We resolve the root to its real path, and then resolve the joined path to its real path, and then check if the resolved joined path is under the resolved root.

   This is correct.

   However, there is a potential issue: 
        The function `filepath.Abs` on Windows might return a path with a drive letter and then we do `EvalSymlinks` which might not be defined? 
        But note: `filepath.EvalSymlinks` on Windows does nothing if there are no symlinks, and if there are, it follows them.

   The standard library handles it.

   I don't see an issue.

   But note: the function `existsContained` does not handle the case where the root is not an absolute path? 
        We convert to absolute at the beginning.

   This is correct.

   However, note that the root passed in is the repo root (which is absolute?).

   The reconcile process sets Root="." (the current directory) and then we convert to absolute.

   This is safe.

   One more thing: in the case of a symlink that points to a non-existent target, what happens?
        Example: 
          root = "/repo"
          We have a symlink: "/repo/link" -> "/nonexistent"
          Then we check for file: "link/target"

        Steps:
          absRoot = "/repo"
          absJoined = "/repo/link/target"
          realRoot = EvalSymlinks("/repo") -> if /repo is not a symlink, then "/repo"
          resolved = EvalSymlinks("/repo/link/target") 
                = EvalSymlinks("/nonexistent/target") 
                -> if /nonexistent does not exist, then EvalSymlinks returns an error? 
                Actually, `filepath.EvalSymlinks` returns an error if any symlink in the path cannot be resolved or points to a non-existent file? 
                From the docs: EvalSymlinks returns the path after following any symbolic links, using GetFullPathname on Windows. 
                If the path does not exist, EvalSymlinks returns an error.

        So in this case, `err` from EvalSymlinks on the joined path would be non-nil and not os.IsNotExist? 
                Actually, if the symlink points to a non-existent file, then EvalSymlinks returns an error.

        Then we go to the default case: existsIndeterminate.

        Then we leave the finding unflagged (PathValid remains false? actually, we don't set it to true, but note: we start with PathValid=false? 
                No: the Finding starts with PathValid=false (zero value) and PathWarning empty.

                Then we do:
                  if idx != nil { ... CaseCorrection ... }   // might set PathValid=false and PathWarning and return? 
                  but if we don't return, then we do the existence check.

                In the existence check, we get existsIndeterminate -> we do nothing (so PathValid remains false and PathWarning remains empty?).

                But note: we want to flag it as not found? 

        However, the problem says: 
          "Indeterminate (permission, I/O): leave the finding unflagged rather than assert a 'not found' we cannot prove."

        So we leave it unflagged.

        But note: the finding already has PathValid=false (because we never set it to true) and PathWarning empty.

        This is consistent with unflagged.

        However, the Epic 5.0 behavior was to flag as not found only when we know it's not found (os.IsNotExist). 
        For indeterminate, we leave it as not flagged (which means PathValid=false and PathWarning empty) -> but note: 
          the report only shows a warning if PathWarning is non-empty.

        So in the indeterminate case, we do not show a warning.

        This is correct.

   Therefore, the changes in `internal/stream/validate.go` look correct.

   However, note that we are setting `f.PathValid = false` and `f.PathWarning = PathNotFoundWarning` in the case-only mismatch branch and in the existsOutsideOrAbsent branch.

   In the case-only mismatch branch, we set:
        f.PathValid = false
        f.PathWarning = PathNotFoundWarning
        f.PathSuggestion = suggestion   // which might be empty if ambiguous

   In the existsOutsideOrAbsent branch, we set:
        f.PathValid = false
        f.PathWarning = PathNotFoundWarning
        f.PathSuggestion = idx.MissingSuggestion(f.File)   // which might be empty

   This is correct.

   But note: what if the index is nil? 
        Then we skip the CaseCorrection block (because idx==nil) and then in the existsOutsideOrAbsent branch, we skip setting PathSuggestion.

   This is correct: we degrade to 5.0 behavior.

   I don't see an issue.

   However, note that the function `ValidatePath` now has three parameters: (f *Finding, root string, idx *FileIndex)

   And we are updating the callers: 
        internal/reconcile/validate.go: now passes the index.
        internal/stream/validate_test.go: updated to pass nil for the index in the tests that don't want suggestion.

   The tests that do want suggestion pass a non-nil index.

   This is correct.

   Now, let's look at the test file `internal/stream/validate_test.go`:

        We see that the existing tests were updated to pass `nil` for the index.

        And we added new tests for the suggestion cases.

   The new tests look correct.

   However, note one test: `TestValidatePath_SuggestsWrongDirectory`

        It sets up a repo with "pkg/auth/validator.go"
        Then it creates a finding for "internal/auth/validator.go"
        Then it calls ValidatePath with the index.

        It expects:
            PathValid = false
            PathWarning = "file not found"
            PathSuggestion = "pkg/auth/validator.go"

   This is correct.

   Similarly, the other tests.

   I don't see an issue in the test file.

   But note: the test `TestValidatePath_SuggestsCaseTypo`:

        It sets up a repo with "internal/auth/parser.go"
        Then it creates a finding for "internal/auth/Parser.go"
        Then it calls ValidatePath with the index.

        It expects:
            PathValid = false
            PathWarning = "file not found"
            PathSuggestion = "internal/auth/parser.go"

        And it also checks: 
            assert.False(t, f.PathValid, "a case-only typo must be flagged even on case-insensitive FS")

   This is correct.

   However, note that on a case-insensitive filesystem, the os.Stat/EvalSymlinks would report the file as present, 
        but we catch it in the CaseCorrection block (because we do that before the existence check) and set the suggestion.

   This is correct.

   Therefore, I do not see any obvious issue in the diff.

   But wait: the sprint plan says that we should build the index once per reconcile run.

   In `internal/reconcile/validate.go`, we have:

        func validateFindingPaths(findings []Merged, root string) {
            if root == "" {
                return
            }
            idx := stream.BuildFileIndex(root)
            for i := range findings {
                stream.ValidatePath(&findings[i].Finding, root, idx)
            }
        }

   This builds the index once and then uses it for every finding.

   This is correct.

   However, note that the function `BuildFileIndex` might return nil (if not a git repo, etc.), and then we pass nil to `ValidatePath`.

   This is correct.

   Now, let's check the `internal/reconcile/emit.go` change:

        We added the field `PathSuggestion` to `JSONFinding`.

        And in `JSONFindings`, we set it from `m.PathSuggestion