<think>
Let me analyze this diff carefully. The sprint plan is about consolidating the severity-rank rubric into a single canonical owner in `internal/stream`.

Let me review the changes:

1. **internal/stream/severity.go** (new) - Adds canonical `SeverityRank` map and `NormalizeSeverity` function
2. **internal/stream/severity_test.go** (new) - Tests for the canonical definitions
3. **internal/fanout/postprocess.go** - Migrates from local `severityRank` to `stream.SeverityRank` + `stream.NormalizeSeverity`
4. **internal/reconcile/merge.go** - Changes `SeverityRank` to be a re-export alias, and normalizes severity at the merge.go:104 boundary
5. **internal/reconcile/disagree.go** - Normalizes at the disagree.go:338/360 boundaries
6. **internal/reconcile/severity_consolidation_test.go** (new) - Tests for normalization at boundaries
7. **internal/report/render.go** - Migrates to `stream.SeverityRank` + `stream.NormalizeSeverity`
8. **internal/report/render_test.go** - Updates test and adds normalization test
9. **internal/verify/severity.go** - Migrates to `stream.SeverityRank` + `stream.NormalizeSeverity`
10. **internal/boundaries_test.go** - Updates allowed imports for verify to include `stream`

Let me check for issues:

**internal/stream/severity.go:**
- The `SeverityRank` is a package-level `var` map. This is mutable. The doc says "Read-only after init" but it's not enforced. This is the same as before (deferred pattern), so it's not a new issue introduced by this diff.
- `NormalizeSeverity` handles empty string: `strings.ToUpper(strings.TrimSpace(""))` = `""`. Good.
- But what about `nil`? Not relevant for strings.

**internal/fanout/postprocess.go:**
- The `strings` import is removed - correct since we use `stream.NormalizeSeverity` now.
- Uses `stream.NormalizeSeverity` for normalization - good.
- The doc comment about the local map is removed. The "Read-only after init" note was useful and is now in `stream/severity.go`.
- The `floor != ""` check: after `NormalizeSeverity`, an empty/whitespace string becomes `""`. Good.
- But wait - what if the original code's `floor != ""` check had a specific behavior? Let me check: previously `strings.ToUpper(strings.TrimSpace(minSeverity))` - same as `NormalizeSeverity`. So `floor != ""` is the same. Good.

**internal/reconcile/merge.go:**
- `SeverityRank` is now `var SeverityRank = stream.SeverityRank` - re-export alias. This is assignment of map reference - the alias points to the same map. If something mutates `stream.SeverityRank`, the alias reflects it. This is fine for read-only usage.
- The `seen[norm] = true` change: this is a behavior change. Previously `seen[f.Severity] = true` used the raw value. Now it uses the normalized form. This means mixed-case duplicates like "critical" and "CRITICAL" will be considered the same severity. This is mentioned in the test `TestMerge_MixedCaseDuplicateIsNotADisagreement` and is the intended fix per the sprint plan.
- The returned `max` and `minSev` are now normalized. The comment in the code explicitly says "the returned max/min are canonical." This is the intended fix.
- One concern: the `disagreement` calculation. The function returns `(max, disagreement string)`. The `disagreement` is built from `seen` which is now keyed by normalized values. But the returned `max` is also normalized. The caller of `mergeSeverity` would receive canonical values now. Is there any caller that depends on the raw values? Let me think... this is the intended behavior per the sprint plan.

**internal/reconcile/disagree.go:**
- Line 338: `SeverityRank[stream.NormalizeSeverity(f.Severity)]` - normalizes for lookup. But `maxSev = f.Severity` is still the raw value. Then at line 360, `SeverityRank[stream.NormalizeSeverity(maxSev)]` normalizes again for the second lookup.
- This is a double-normalization (normalize at 338, then normalize the result of maxSev at 360). It's correct but slightly wasteful. Not a bug.
- Wait - is `maxSev` set to `f.Severity` (raw) at line 339? Let me re-read:
  ```go
  if r, ok := SeverityRank[stream.NormalizeSeverity(f.Severity)]; ok {
      if r > maxRank {
          maxRank, maxSev = r, f.Severity
      }
  }
  ```
  Yes, `maxSev` is `f.Severity` (raw). Then at 360: `SeverityRank[stream.NormalizeSeverity(maxSev)]`. This is fine - it's defensive normalization.

**internal/report/render.go:**
- Uses `stream.SeverityRank[stream.NormalizeSeverity(s)]` - good.

**internal/verify/severity.go:**
- Uses `stream.SeverityRank[stream.NormalizeSeverity(...)]` - good.
- The comment about "the defensive 'skip on unknown severity' behavior (AC 02-07 EC3)" was removed in the doc. The new comment is more general but doesn't reference AC 02-07 EC3. This is a minor documentation change - not really an issue, just a slight loss of context.

**internal/boundaries_test.go:**
- Added `stream` to verify's allowed imports. Good - matches the new dependency.

**Tests:**
- The new test `TestMerge_NormalizesMixedCaseSeverity` - looks correct.
- `TestGrayZoneItem_NormalizesMixedCaseSeverity` - uses `AmbiguousCluster` and `grayZoneItem`. The score should be 4 (rank of CRITICAL) when spread is 0 and indep is true. Let me verify: `scoreFor(spread, indep, rank)`. With spread=0, indep=true (atLeastOne(2)=true), rank=4. What does scoreFor return? It's a function from the package. The test comment says "scoreFor with spread 0 returns the severity rank". I'll trust this.
- `TestMerge_MixedCaseDuplicateIsNotADisagreement` - checks that `got.Disagreement != ""` is false and `got.Severity == "CRITICAL"`. Good.

**Potential issues:**

1. **internal/stream/severity.go: Read-only enforcement**: The `SeverityRank` map is exposed as a public mutable `var`. The doc says "Read-only after init" but it's not enforced. This is a known design choice though, and the doc warns about it. Not a new issue - this is the same pattern as before.

2. **internal/reconcile/merge.go: Alias mutation risk**: `var SeverityRank = stream.SeverityRank` - this assigns the map reference. If something mutates `SeverityRank` (e.g., `delete(SeverityRank, "CRITICAL")`), it mutates `stream.SeverityRank` too. The doc warns about this. This is a known trade-off documented in `stream/severity.go`. The fact that the alias in `reconcile/merge.go` is also called `SeverityRank` means a future developer who reads `reconcile.SeverityRank` documentation might miss the warning that's in `stream/severity.go`. The comment in `merge.go` does mention "the single source of truth" but doesn't repeat the read-only warning. This is a minor maintainability concern.

3. **internal/reconcile/disagree.go: Double normalization**: The lookup at line 338 normalizes the key, but stores the raw `f.Severity` in `maxSev`. Then at line 360, it normalizes `maxSev` again. This is correct but slightly wasteful. Not a bug.

4. **internal/stream/severity.go: Test ordering assertion**: The test `TestSeverityRank_CanonicalOrdering` has a redundant check: it iterates `want` to verify values, then has a separate chain comparison. The chain comparison `CRITICAL > HIGH > MEDIUM > LOW > 0` is implied by the value comparison if all four are present. Not a bug, just slightly redundant.

5. **The `seen` map change in merge.go**: This is the most significant behavior change. The returned `max` and `minSev` are now canonical (uppercase), and the `seen` set is keyed by canonical values. This affects the `disagreement` string. Any consumer that expects the raw reviewer-provided severity in the output would see canonical values now. Is this intended? Per the sprint plan: "the returned max/min are canonical" - yes, this is the intended fix. The `Merge` function's output `Severity` field will now be canonical.

   But wait - what if a test in another file expects `got.Severity == "critical"` (lowercase)? Let me think... The test file `severity_consolidation_test.go` checks `got.Severity != "CRITICAL"`, so it's expecting canonical. But there could be other tests that depend on raw values. This is a behavior change. However, the sprint plan calls this out as the intended fix. Not a bug per the scope.

6. **internal/stream/severity.go line 19**: The `var SeverityRank` is a map literal. This is fine. The `NormalizeSeverity` function on line 26 is fine.

7. **Missing import check**: The `internal/verify/severity.go` removes the `strings` and `reconcile` imports and adds `stream`. Good. The `internal/fanout/postprocess.go` removes `strings` and keeps `stream`. Good. The `internal/reconcile/merge.go` should already import `stream` (used for `stream.Finding`). Let me check... yes, the original code uses `stream.Finding` in `mergeSeverity`, so `stream` is already imported. Good.

8. **internal/verify/severity.go**: The original `normalizeSeverity` function was package-private. Removing it - any other file in `verify` using it? I can't see the full file, but the diff removes the function. If other files in `verify` package used it, that would be a compile error. However, the diff is presented as a complete change, so presumably no other usage. Worth noting as a low-priority maintainability concern if there's a risk of stale references.

9. **The verify package no longer imports reconcile**: Wait, the `internal/verify/severity.go` previously imported `reconcile` for `reconcile.SevCritical` etc. Now it only imports `stream`. The boundaries test now shows `verify` can import `stream`. But the original `verify` package had `reconcile` in its allowed imports. Does `verify` use `reconcile` for anything else? The boundaries test still allows `verify → reconcile`, so the import isn't removed from the allowlist. Just `stream` is added. This is fine.

10. **Performance**: The `stream.NormalizeSeverity` is called on each finding in a loop. This involves two string operations (TrimSpace + ToUpper). For a large number of findings, this could allocate. But `strings.ToUpper` and `strings.TrimSpace` may not allocate if the string is already canonical. `strings.ToUpper` does allocate a new string in most cases. For a hot path processing many findings, this is a minor performance concern. However, the previous code had the same `strings.ToUpper(strings.TrimSpace(...))` pattern, so no regression. Not a new issue.

11. **The `seen` map change in merge.go - potential edge case**: If the input group has only mixed-case duplicates of one severity (e.g., "critical" and "CRITICAL"), `seen` will have one entry (the normalized "CRITICAL"). The `disagreement` string is built by joining the `seen` values. Previously it would have two entries ("critical" and "CRITICAL"), producing a spurious "critical vs CRITICAL" disagreement. Now it correctly produces no disagreement. This is the intended fix per AC3 and the test `TestMerge_MixedCaseDuplicateIsNotADisagreement`.

12. **Test coverage**: The tests cover the canonical ordering, normalization, and the two boundary fixes. Good.

13. **The `seen` map now uses normalized keys, but what if the `disagreement` string output is consumed elsewhere expecting raw values?** This is a behavior change. The sprint plan says "the returned max/min are canonical" - so this is intended.

Let me focus on the most significant issues within the sprint plan's scope:

**Findings:**

1. **internal/reconcile/merge.go:18 - Re-export alias mutation risk**: The alias `var SeverityRank = stream.SeverityRank` shares the underlying map. The doc in `stream/severity.go` warns about read-only usage, but the alias doc in `merge.go` doesn't repeat this warning. A future developer writing `delete(reconcile.SeverityRank, "CRITICAL")` would silently corrupt the canonical map across all consumers. (maintainability)

2. **internal/stream/severity.go:19 - Public mutable map**: The `SeverityRank` map is a public `var` and can be mutated by any consumer. The doc warns against this but doesn't enforce it. This is the same pattern as before (the old `reconcile.SeverityRank` was also a mutable `var`), so it's not a regression. But the consolidation makes this a more critical invariant to enforce, since now 4+ packages share the same map. (maintainability, but design choice per the sprint plan)

3. **internal/reconcile/merge.go:111 - `seen` keyed by normalized values is a behavior change**: The `disagreement` string and returned `max`/`minSev` now use canonical casing. While this is the intended fix per AC3, any downstream consumer that expected raw reviewer casing would silently receive canonical values. This should be called out in a release note or migration guide. (maintainability/correctness, but intended)

4. **internal/reconcile/disagree.go:339 - `maxSev` stores raw value but is re-normalized at line 360**: This is correct but slightly confusing. The `maxSev` variable holds the raw `f.Severity` even though the rank lookup used the normalized key. Then `maxSev` is re-normalized at line 360. A developer reading the code might wonder why `maxSev` isn't stored in normalized form directly. (maintainability)

5. **internal/verify/severity.go: Removal of `normalizeSeverity`**: The local `normalizeSeverity` function is removed. The diff shows only this one file in the `verify` package, so presumably no other file uses it. But if another file in the `verify` package referenced `normalizeSeverity` (package-private), it would now be a compile error. The diff should verify no other references exist. (correctness, but likely fine)

6. **internal/fanout/postprocess.go: Removed `strings` import**: The `strings` package is no longer imported. The diff shows only `stream` and stdlib packages remaining. Correct.

Let me think about which of these are real issues worth flagging vs. design choices...

The sprint plan explicitly says:
- "Re-export alias" for `reconcile.SeverityRank` (Option A) - so the alias is intended
- "Pure consolidation — no severity value/order change" (AC6) - but the `seen` map and returned max/min normalization is a behavior change for mixed-case input. The plan calls this out: "the intended fix" and "behavior-preserving for valid data and only fixes mixed-case/whitespace handling"
- "Read-only after init" is documented in `stream/severity.go`

So the main things to flag:

1. The re-export alias doesn't document the read-only constraint (the canonical doc in `stream/severity.go` does, but the alias doc in `merge.go` doesn't). This is a minor maintainability concern.

2. The `maxSev` variable in `disagree.go` stores the raw value but the rank lookup at line 360 re-normalizes. This is correct but slightly confusing. A minor maintainability concern.

3. The `seen` map in `merge.go` now uses normalized keys, which is a subtle behavior change for the `disagreement` string. This is the intended fix per AC3, but worth confirming no downstream test depends on the old behavior.

Actually, let me reconsider. The sprint plan is very clear about what changes are in scope. The `seen` map change and the alias are all explicitly called out. I should focus on issues that are within scope and are real problems.

Let me look more carefully:

- `internal/stream/severity.go:19` - The `SeverityRank` is a `map[string]int` literal. The `var` declaration means it can be reassigned. But since it's a map, the more common mutation is `delete()` or `m[k] = v`. The `var` keyword also means the variable itself can be reassigned (`stream.SeverityRank = nil`). This is a minor concern but documented.

- `internal/reconcile/merge.go:18` - `var SeverityRank = stream.SeverityRank` - same concern. The alias doc could mention the read-only constraint.

- The `seen` map in `merge.go` now uses normalized keys. The `disagreement` string output is now based on canonical severities. This is the intended fix per the plan.

- `internal/reconcile/disagree.go:339` - `maxSev` stores `f.Severity` (raw). At line 360, it's re-normalized. This works correctly but is slightly redundant. Not a bug.

I think the most actionable finding is:

1. The re-export alias in `merge.go` doesn't propagate the read-only warning from `stream/severity.go`. A developer using `reconcile.SeverityRank` might not see the canonical doc.

2. The `maxSev` variable in `disagree.go` stores the raw value and re-normalizes at line 360. This works but is slightly confusing and could be simplified by storing the normalized form directly.

Let me also check if there are any issues with the test:

- `TestGrayZoneItem_NormalizesMixedCaseSeverity` - The test uses `grayZoneItem` which is a package-private function. The test is in the same package (`package reconcile`), so this works. Good.

- The test checks `got.Score != 4`. This depends on `scoreFor` returning 4 for spread=0, indep=true, rank=4. The test comment says "scoreFor with spread 0 returns the severity rank". I can't verify this without seeing `scoreFor`, but the test was presumably written based on knowledge of the function.

- `TestMerge_MixedCaseDuplicateIsNotADisagreement` - checks `got.Disagreement != ""` is false and `got.Severity == "CRITICAL"`. This depends on the `Merge` function's behavior. Looks correct.

I think the findings I should report are:

1. **internal/reconcile/merge.go:18** - The re-export alias `var SeverityRank = stream.SeverityRank` doesn't document the read-only constraint that's in `stream/severity.go`. A developer using `reconcile.SeverityRank` might not see the canonical doc and could accidentally mutate the shared map. (maintainability)

2. **internal/reconcile/disagree.go:339-360** - `maxSev` stores the raw `f.Severity` but is re-normalized at line 360 for the rank lookup. This is correct but slightly confusing. Could store the normalized form directly. (maintainability)

3. **internal/stream/severity.go:19** - `SeverityRank` is a public mutable `var` map. The doc warns against mutation but doesn't enforce it. The consolidation makes this a critical invariant (4+ packages share the same map). Consider using an unexported map with accessor functions, or at minimum a package-level mutex. (maintainability, but this is a design choice per the plan)

Actually, looking at the sprint plan again: "Read-only after init: the map is written once at package load and only read thereafter. If mutation is ever needed, use a sync.Map or initialize a local copy — concurrent fan-out agents share this map, so a write would race."

This is explicitly documented. The design choice is to use a plain map with a doc warning. So this is not a finding to report - it's the intended design.

Let me reconsider what to report:

The sprint plan is very prescriptive. The design choices (mutable map with doc warning, re-export alias, normalization at boundary sites only) are all explicitly called out. So I should focus on:

1. Issues that are within the sprint plan's scope
2. Issues that are real problems (not design choices)
3. Issues that have concrete fixes

Given the prescriptive nature of the plan, the issues I see are:

1. **internal/reconcile/merge.go:18** - The alias doc doesn't mention the read-only constraint. Minor maintainability concern. The fix is to add a note in the comment.

2. **internal/reconcile/disagree.go:339** - `maxSev` stores raw value, re-normalized at 360. Minor maintainability concern. The fix is to store the normalized form directly.

3. **internal/stream/severity_test.go:19-25** - The ordering assertion is redundant with the value comparison. Minor maintainability concern. Not a bug.

Actually, I realize I should be more careful. Let me re-read the instructions:

"Be precise: Reference specific line numbers"
"Be actionable: Every issue must have a concrete fix"
"Be prioritized: HIGH = must fix, MEDIUM = should fix, LOW = nice to have"
"No false positives: Only report real issues"
"Focus on diff changes: Review what's been modified, not the entire codebase"

OK, so I should focus on real issues. Let me think about what's actually a real issue:

1. The alias doc not mentioning read-only - this is a minor doc gap, not a code issue. LOW priority.

2. The `maxSev` double-normalization - this works correctly, just slightly redundant. Not a bug. LOW priority.

3. The `seen` map using normalized keys - this is the intended behavior change per AC3.

4. The test ordering assertion being redundant - not a bug.

Hmm, I think there might not be many real issues here. The consolidation was done carefully per the sprint plan.

Let me look one more time for actual bugs or issues...

- `internal/stream/severity.go:26` - `NormalizeSeverity` uses `strings.ToUpper(strings.TrimSpace(s))`. For an empty string, `TrimSpace("")` = `""`, then `ToUpper("")` = `""`. Good. For whitespace-only, `TrimSpace("   ")` = `""`, then `ToUpper("")` = `""`. Good.

- `internal/fanout/postprocess.go:27` - `if floor := stream.NormalizeSeverity(minSeverity); floor != ""` - the empty check is preserved. Good.

- `internal/reconcile/merge.go:111` - `seen[norm] = true` - uses normalized form. The `disagreement` string is built from `seen` keys. If a group has "critical" and "CRITICAL", `seen` = {"CRITICAL"}, and the disagreement string would be just "CRITICAL" (no "vs" separator since there's only one key). But wait - the disagreement logic might check `len(seen) > 1` to decide if there's a disagreement. Let me think... the function returns `disagreement` which is set based on the `seen` map. If `seen` has one key, the function likely returns empty (no disagreement). If `seen` has multiple keys, it returns the disagreement string. With the normalized `seen`, mixed-case duplicates are correctly identified as one severity. This is the intended fix.

- `internal/reconcile/disagree.go:338-360` - The `maxSev` is stored as raw, then re-normalized. The `maxRank` is compared correctly. The final `scoreFor` call uses the normalized rank. Good.

- `internal/report/render.go:419` - `stream.SeverityRank[stream.NormalizeSeverity(s)]` - good. For an empty string, `NormalizeSeverity("")` = `""`, which is not in the map, so returns 0. Good.

- `internal/verify/severity.go:14-16` - `stream.SeverityRank[stream.NormalizeSeverity(...)]` - good. The `fr == 0` check correctly skips unknown/empty severities. Good.

- `internal/boundaries_test.go:38` - Added `stream` to verify's allowed imports. Good.

I think the most notable finding is:

The re-export alias in `merge.go:18` shares the underlying map reference with `stream.SeverityRank`. While this is the intended design (Option A per the sprint plan), the alias documentation doesn't propagate the critical read-only invariant from `stream/severity.go`. A developer who only reads the `reconcile` package docs would not know that mutating `reconcile.SeverityRank` would corrupt the canonical map across all 4+ consumers.

This is a real maintainability concern, even if the design is intentional. LOW priority.

Actually, let me reconsider. The alias comment in `merge.go` does say:
```go
// SeverityRank is a re-export of the canonical rank map owned by internal/stream
// (the single source of truth). Higher rank wins a merge and sorts earlier in
// both the reconcile radar and the report view; unknown severities sort last
// (rank 0). Kept as an exported alias so reconcile's internal lookups read it
// unqualified and external callers keep a stable symbol.
```

It mentions "the single source of truth" and "re-export of the canonical rank map owned by internal/stream". A developer following this pointer would find the read-only warning in `stream/severity.go`. So the alias doc does point to the canonical source. This is adequate.

I think the findings are very thin. Let me look one more time...

Actually, I notice that in `internal/reconcile/merge.go:107-118`, the `seen` map now uses normalized keys, and the returned `max` and `minSev` are now normalized. The `disagreement` string is built from the `seen` keys. This means:

- For a group with severities ["critical", "CRITICAL", "LOW"], `seen` = {"CRITICAL", "LOW"}, disagreement = "CRITICAL vs LOW", max = "CRITICAL", minSev = "LOW".
- Previously, `seen` = {"critical", "CRITICAL", "LOW"}, disagreement = "critical vs CRITICAL vs LOW" (spurious), max = "CRITICAL" or "critical" (whichever had higher rank, which is the same), minSev = "LOW".

Wait, previously the max was `f.Severity` (raw), and minSev was `f.Severity` (raw). The rank comparison would find the same max rank (4) for both "critical" and "CRITICAL". The first one encountered with rank 4 would win. So `max` could be "critical" or "CRITICAL" depending on iteration order. Now `max` is always "CRITICAL" (normalized). This is a deterministic behavior change.

This is the intended fix per the sprint plan. The function output is now deterministic and canonical.

OK, I think I've been thorough enough. Let me formulate the findings:

Given the sprint plan's prescriptive nature and the careful implementation, the real issues I can identify are:

1. **LOW: internal/reconcile/merge.go:18** - The re-export alias doc could more explicitly warn about the shared mutable map. (maintainability)

2. **LOW: internal/reconcile/disagree.go:339** - `maxSev` stores raw value and is re-normalized at line 360. Could store normalized form directly. (maintainability)

3. **LOW: internal/stream/severity_test.go:19-25** - The ordering chain assertion is redundant with the value comparison. (maintainability)

Actually, I realize I should be more critical. Let me look at the `seen` map change more carefully:

In `merge.go:111`, `seen[norm] = true` uses the normalized form. The `disagreement` string is built from the `seen` map's keys. But the `disagreement` variable is also tracked via the `disagreement` return value. Let me re-read the full function:

```go
func mergeSeverity(group []stream.Finding) (max, disagreement string) {
    maxRank, minRank := -1, 1<<31
    var minSev string
    for _, f := range group {
        norm := stream.NormalizeSeverity(f.Severity)
        rank, ok := SeverityRank[norm]
        if !ok {
            continue // unknown severity ignored for max/min
        }
        seen[norm] = true
        if rank > maxRank {
            maxRank, max = rank, norm
        }
        if rank < minRank {
            minRank, minSev = rank, norm
        }
    }
    ...
}
```

Wait, I need to see the full function to understand the `disagreement` return value. The diff shows the function is modified but the full function isn't shown. The diff shows lines 15-26 and 101-124. The `disagreement` return value is built somewhere after the loop. Without seeing the full function, I can't verify the `disagreement` logic.

But the test `TestMerge_MixedCaseDuplicateIsNotADisagreement` checks `got.Disagreement != ""` is false for ["critical", "CRITICAL"]. This implies the function correctly returns empty disagreement for a single normalized severity. The test would catch a bug if the `seen` map change didn't work as intended.

OK, I think the implementation is correct. The findings I can report are minor maintainability concerns.

Let me finalize:

1. **LOW: internal/reconcile/merge.go:18** - The re-export alias `var SeverityRank = stream.SeverityRank` shares the underlying map. While the alias doc points to `internal/stream` as the canonical owner, it doesn't explicitly warn that mutating the alias would corrupt the shared map across all consumers. Consider adding a one-line note. (maintainability, 5 min)

2. **LOW: internal/reconcile/disagree.go:339** - `maxSev` stores the raw `f.Severity` but is re-normalized at line 360 for the `scoreFor` call. This is correct but slightly confusing. Consider storing `norm` directly. (maintainability, 5 min)

3. **LOW: internal/stream/severity_test.go:19-25** - The ordering chain assertion (`CRITICAL > HIGH > MEDIUM > LOW > 0`) is redundant with the value comparison in the loop. (maintainability, 2 min)

Hmm, but #3 is really nitpicky. Let me drop it.

Actually, wait. Let me reconsider #1. The sprint plan says:
"re-export alias — var SeverityRank = stream.SeverityRank (Option A). The literal at internal/reconcile/merge.go:21 is deleted; the exported reconcile.SeverityRank symbol stays as a thin alias"

And the alias comment in the diff says:
```go
// SeverityRank is a re-export of the canonical rank map owned by internal/stream
// (the single source of truth). Higher rank wins a merge and sorts earlier in
// both the reconcile radar and the report view; unknown severities sort last
// (rank 0). Kept as an exported alias so reconcile's internal lookups read it
// unqualified and external callers keep a stable symbol.
```

It says "re-export of the canonical rank map owned by internal/stream (the single source of truth)". This clearly points to the canonical owner. A developer would follow this pointer. The read-only warning is in the canonical owner's doc. I think this is adequate.

Let me reconsider #2. The `maxSev` stores `f.Severity` (raw) at line 339. At line 360, it's re-normalized. This is correct but slightly redundant. However, the sprint plan explicitly says:

"the maxSev rank lookup derived from it at disagree.go:360"

This acknowledges that `maxSev` is the raw value and the lookup at 360 normalizes it. The plan says "All other reconcile lookup sites operate on provably-canonical data, so normalizing them would be a no-op and is out of scope." But `disagree.go:338/360` is in scope for the fix.

The current implementation normalizes at 338 (for the rank comparison) and at 360 (for the final scoreFor call). The `maxSev` variable itself is not normalized. This is a design choice - the plan says the fix is at the "true external-boundary lookups" only.

I think this is fine as-is. The implementation matches the plan.

OK, let me think about whether there are any actual bugs...

One thing I want to verify: the `disagree.go:339` line `maxRank, maxSev = r, f.Severity`. The `r` is the rank from the normalized lookup. The `maxSev` is the raw severity. This is correct - `maxSev` is used later for display/return, and the rank is used for comparison.

At line 360: `SeverityRank[stream.NormalizeSeverity(maxSev)]`. This re-normalizes `maxSev` for the final lookup. This is correct because `maxSev` could be in any casing.

But wait - could `maxSev` be an empty string? If no finding has a known severity, `maxSev` remains `""` (zero value). Then `NormalizeSeverity("")` = `""`, and `SeverityRank[""]` = 0 (absent). The `scoreFor` call with rank 0 would be handled by the function. This is the "unknown severity floor" behavior mentioned in the comment.

OK, I think the implementation is correct.

Let me look at one more thing: the `seen` map in `merge.go`. It's declared somewhere before the loop (not shown in the diff). After the loop, the `disagreement` string is built from `seen`. With the normalized keys, the disagreement string will use canonical severities. This is the intended fix.

But wait - what if the `disagreement` string is consumed by a caller that expects raw reviewer casing? The `Merge` function returns a `stream.Finding` with `Disagreement` and `Severity` fields. If a downstream consumer renders the disagreement string, it would now show canonical casing. This is a behavior change for mixed-case input. The sprint plan acknowledges this: "the returned max/min are canonical."

I think the only real finding worth reporting is:

**LOW: internal/reconcile/disagree.go:339-360** - The `maxSev` variable stores the raw severity and is re-normalized at line 360. While correct, this is slightly confusing. A reader might wonder why `maxSev` isn't stored in normalized form. Consider storing `norm` directly to avoid the double normalization and improve readability. (maintainability, 5 min)

Actually, I just realized there might be a subtle issue. Let me re-read disagree.go:338-360:

```go
if r, ok := SeverityRank[stream.NormalizeSeverity(f.Severity)]; ok {
    if r > maxRank {
        maxRank, maxSev = r, f.Severity
    }
}
```

At line 339, `maxSev = f.Severity` (raw). This is the severity of the finding with the highest rank so far. If two findings have the same rank (e.g., both "critical" and "CRITICAL"), the last one encountered wins. The `maxSev` would be whichever was last in the iteration order.

At line 360: `SeverityRank[stream.NormalizeSeverity(maxSev)]`. This re-normalizes whatever `maxSev` is. So even if `maxSev` is "critical" (lowercase), the lookup uses "CRITICAL". This is correct.

The double normalization is a minor performance concern (two `ToUpper` + `TrimSpace` calls instead of one) and a minor readability concern. Not a bug.

OK, I'll go with one finding. Let me also check if there's anything else...

Actually, I want to double-check the `merge.go:111` change. The comment says:
```go
// Track the normalized form so a mixed-case duplicate (e.g. "critical"
// and "CRITICAL") counts as one severity, not a spurious disagreement,
// and the returned max/min are canonical.
seen[norm] = true
```

And then:
```go
if rank > maxRank {
    maxRank, max = rank, norm
}
if rank < minRank {
    minRank, minSev = rank, norm
}
```

The `max` and `minSev` are now `norm` (canonical). This is correct per the plan.

But wait - what if a finding has severity " critical " (with spaces)? `NormalizeSeverity(" critical ")` = "CRITICAL". So `norm` = "CRITICAL", and `max` = "CRITICAL". This is the intended behavior.

OK, I think the implementation is solid. Let me finalize my findings.

Given the sprint plan's prescriptive nature, the implementation matches the plan carefully. The only minor concerns are:

1. Double normalization in `disagree.go:338/360` - LOW maintainability
2. The re-export alias sharing the map reference - this is the intended design per the plan

I'll report one finding about the double normalization as a LOW maintainability concern.

Wait, actually let me reconsider whether the double normalization is even worth flagging. The sprint plan explicitly says:
</think>
