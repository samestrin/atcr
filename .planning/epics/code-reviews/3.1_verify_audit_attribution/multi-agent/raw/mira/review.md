<think>
Let me carefully review this diff for the TD-011 sprint plan focused on audit attribution in the verify pipeline.

The sprint plan focuses on:
1. `internal/verify/invoke.go` — surface tripped budgets from `invokeSkeptic`
2. `internal/verify/pipeline.go` — winning-skeptic model attribution
3. `internal/verify/pipeline.go` — skip-already-verified path metadata preservation
4. `internal/verify/pipeline.go` — early-return paths set `base.Model = ""`
5. New `ReadVerificationResults` reader in `emit_verification.go`

Let me analyze each part:

### `invoke.go` changes
- Signature change: `(*reconcile.Verification, []string, error)` - good
- `len(results) == 0` returns `nil` for tripped - correct
- `res.Status != fanout.StatusOK || len(res.TrippedBudgets) > 0` returns `res.TrippedBudgets` - correct
- Clean verdict returns `nil` for tripped - correct

### `pipeline.go` changes
- Loads `priorByKey` from on-disk verification.json
- Skip path now checks verdict match before carrying forward
- Early returns set `base.Model = ""` explicitly
- `winningAttribution` function - new

Let me look at `winningAttribution` more carefully:

```go
func winningAttribution(skeptics []Skeptic, perSkeptic []*reconcile.Verification, perTripped [][]string, winner string) (string, []string) {
    counts := map[string]int{}
    for _, v := range perSkeptic {
        if v != nil {
            counts[v.Verdict]++
        }
    }
    decisive := true
    for verdict, n := range counts {
        if verdict != winner && n >= counts[winner] {
            decisive = false
            break
        }
    }
    ...
}
```

Wait, this is interesting. If `winner` is `unverifiable` (e.g., tie), and there are multiple unverifiable voters, then `counts[winner]` could equal `n` for `unverifiable` entries that aren't winner. But `verdict != winner` skips that case. Hmm, but the question is: can `aggregateVerdicts` return `unverifiable` as winner when `unverifiable` was the majority? Let me think...

Looking at the AC2 description: "Winning" skeptics = those whose per-skeptic verdict equals the aggregated (majority) verdict

If 2 unverifiable and 1 confirmed: aggregateVerdicts probably returns unverifiable (majority). In that case winner=unverifiable, and counts={"unverifiable":2, "confirmed":1}. Decisive check: confirmed (1) >= unverifiable (2)? No. So decisive=true. We'd record only unverifiable voters. Is that right? Per the AC, "winning" skeptics = those whose per-skeptic verdict equals the aggregated (majority) verdict, so yes, that seems right.

If 2 unverifiable and 1 refuted: similar - winner=unverifiable, decisive=true, record only unverifiable voters.

Now, what if there's a tie at unverifiable - say 1 unverifiable, 1 confirmed, 1 refuted? aggregateVerdicts probably emits unverifiable (tie). counts={u:1, c:1, r:1}. Decisive check: c(1) >= u(1)? Yes. So decisive=false. We record all voters. This is the tie case described in the comment.

What if there's a tie at winner=2 confirmed, 2 unverifiable? Then aggregateVerdicts... I'd need to see that function. But this is a hypothetical 4-skeptic case.

Hmm, what about: 1 confirmed, 1 unverifiable? counts={c:1, u:1}. If aggregateVerdicts picks unverifiable as the winner on tie, then winner=unverifiable, counts[winner]=1, counts["confirmed"]=1 >= 1, so decisive=false. We record all. Good.

What if aggregateVerdicts picks the first voter or has its own tiebreak? I need to look at it but it's not in the diff. Let me assume it follows the documented behavior in the comment.

### Bug check: tie detection edge case

If `winner == "unverifiable"` and there's a single unverifiable verdict with 1 confirmed/1 refuted (3-skeptic tie to unverifiable):
- counts = {u:1, c:1, r:1}
- For c (1 >= 1): decisive=false ✓
- Records all voters. ✓

If there's a 2-1 outcome where winner=confirmed (2 confirmed, 1 refuted):
- counts = {c:2, r:1}
- For r (1 >= 2): no, decisive=true
- Records only c voters. ✓

If there's a 3-0 outcome with unverifiable winner (2 unverifiable, 1 confirmed):
- counts = {u:2, c:1}
- For c (1 >= 2): no, decisive=true
- Records only u voters. ✓ - this seems correct, the unverifiable voters "won" because they were the majority.

OK, this seems fine. Now let me think about edge cases that could be issues:

### Issue 1: `nil` handling in `perSkeptic` for tie detection

In the loop:
```go
counts := map[string]int{}
for _, v := range perSkeptic {
    if v != nil {
        counts[v.Verdict]++
    }
}
```

This handles nil. But then in the main loop:
```go
for i, v := range perSkeptic {
    if v == nil {
        continue
    }
    ...
}
```

OK, nil is handled consistently.

### Issue 2: Index alignment

Comment says "verifyFinding builds the three slices together, so indices align." Looking at verifyFinding:

```go
perSkeptic := make([]*reconcile.Verification, 0, len(skeptics))
perTripped := make([][]string, 0, len(skeptics))
for _, sk := range skeptics {
    v, tripped, ierr := invokeSkeptic(ctx, sk, prompt, cc, disp)
    if ierr != nil {
        v = &reconcile.Verification{Verdict: verdictUnverifiable, Notes: ierr.Error(), Skeptic: sk.Name}
        tripped = nil
    }
    perSkeptic = append(perSkeptic, v)
    perTripped = append(perTripped, tripped)
}
```

Indices align. Good.

### Issue 3: Selection order

The test says "Alphabetical selection: s2(m-s2), s3(m-s3), skep(m-skep). s3 refutes; the other two confirm → winner=confirmed, winners={m-s2, m-skep}."

Then asserts: `assert.Contains(t, vf.Findings[0].Model, "m-s2")`, `assert.Contains(t, vf.Findings[0].Model, "m-skep")`, `assert.NotContains(t, vf.Findings[0].Model, "m-s3")`.

The order in `winningAttribution` is based on the order in `perSkeptic` which is based on `skeptics` slice order. So this should work.

But wait, the deduplication. What if two skeptics share a model? The `seenModel` map dedupes. That's fine.

### Issue 4: `aggregateVerdicts` output

I'm assuming `aggregateVerdicts` returns `ver.Verdict` that could be empty string for nil/empty. Let me check the calling code:

```go
ver := aggregateVerdicts(perSkeptic)

base.Verdict = ver.Verdict
base.Skeptic = ver.Skeptic
base.Reasoning = ver.Notes
base.Model, base.TrippedBudgets = winningAttribution(skeptics, perSkeptic, perTripped, ver.Verdict)
```

The `winner` parameter to `winningAttribution` is `ver.Verdict`. If `ver.Verdict` is empty, then `counts[winner]` is 0 (zero value in Go map). Then in the decisive check, any verdict with n >= 0 makes it non-decisive. So all voters are recorded (with no tripped budgets since winner doesn't match). The empty verdict would still appear in the list... 

Wait, but `ver.Verdict` empty would mean `v.Verdict != winner` is always true (since empty != anything), so all voters would be included. That's probably the right fallback.

### Issue 5: `winner == ""` and counts

If `ver.Verdict` is empty:
- `counts[winner]` would lookup counts[""], which is 0
- For every other verdict: `n >= 0` is always true → decisive=false
- Records all voters

OK, this is acceptable behavior.

### Issue 6: Test for early returns

The test `TestVerifyFinding_EarlyReturnsRecordEmptyModel` calls `verifyFinding` with a nil cc on the no_eligible_skeptic path:

```go
_, vr := verifyFinding(context.Background(), f, nil, finalChat("{}"), okDispatcher())
```

This should be fine since `len(skeptics) == 0` is checked first. But the cc param... let me check the signature.

Actually I can't see the function signature in the diff, but the test passes `finalChat("{}")` and `okDispatcher()`. The early return checks `len(skeptics) == 0` and `disp == nil`. The second test passes `nil` for disp:
```go
_, vr = verifyFinding(context.Background(), f, []Skeptic{testSkeptic()}, finalChat("{}"), nil)
```

This should hit the `disp == nil` check. OK.

### Issue 7: `aggregateVerdicts` and empty perSkeptic

What if `len(skeptics) > 0` but all skeptic invocations return `nil` perSkeptic entries? Could `aggregateVerdicts` handle this? Probably yes, but the `winningAttribution` would have all nil entries - it would return "", []. That's the "no winner" case.

### Issue 8: `base.Skeptic` not updated

In the verifyFinding changes:
```go
base.Skeptic = ver.Skeptic
```

But in the early-return paths:
```go
v := &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "no_eligible_skeptic"}
base.Verdict, base.Reasoning = v.Verdict, v.Notes
base.Model = ""
return v, base
```

`base.Skeptic` is not set on early returns. This is the pre-existing behavior (not changed by this diff). I shouldn't flag this as it's not in scope.

### Issue 9: ReadVerificationResults

```go
func ReadVerificationResults(reviewDir string) ([]VerificationResult, error) {
    path := filepath.Join(reviewDir, reconciledSubdir, "verification.json")
    data, err := os.ReadFile(path)
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return nil, nil
        }
        return nil, err
    }
    var vf VerificationFile
    if err := json.Unmarshal(data, &vf); err != nil {
        return nil, fmt.Errorf("parsing verification.json: %w", err)
    }
    return vf.Findings, nil
}
```

This is reasonable. Missing file → (nil, nil). Parse error → error. The caller handles both.

### Issue 10: Carry-forward on stale verdict

The check is:
```go
if strings.EqualFold(strings.TrimSpace(prior.Verdict), strings.TrimSpace(f.Verification.Verdict)) {
    rec.Model = prior.Model
    rec.DurationMs = prior.DurationMs
    rec.TrippedBudgets = prior.TrippedBudgets
}
```

This compares verdict case-insensitively with trim. This is good for robustness. But what about a finding with no prior verdict? `priorByKey[key]` returns zero-value `VerificationResult{Verdict: ""}`. Then the equal-fold check would compare "" with the actual verdict, which would fail. So no carry-forward. Good.

But what about a finding with `f.Verification == nil`? Then `f.Verification.Verdict` would panic with nil dereference. Let me check the calling code.

Looking at the diff:
```go
for _, f := range findings {
    key := FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}
    if r, ok := rich[key]; ok {
        results = append(results, r)
        continue
    }
    rec := VerificationResult{
        File: f.File, Line: f.Line, Problem: f.Problem,
        Verdict:   f.Verification.Verdict,
        Skeptic:   f.Verification.Skeptic,
        Reasoning: f.Verification.Notes,
    }
    prior := priorByKey[key]
    if strings.EqualFold(strings.TrimSpace(prior.Verdict), strings.TrimSpace(f.Verification.Verdict)) {
        ...
    }
}
```

This is in the "skipped this run" path. For a finding to be skipped (in `rich` map missing), it must NOT have been re-verified this run. Looking at the skip check earlier (not in diff, but referenced):

```go
// hasTrustedVerdict reports whether a finding's existing verification block is a
// cached result safe to skip (AC 04-05). Only the three canonical verdicts are
// trusted; nil, empty, or an unknown verdict is treated as unverified so the
```

So if `f.Verification` is nil or has empty verdict, the finding is NOT trusted, and is re-verified this run. So in the skipped path, `f.Verification` should be non-nil. But this is outside the diff change.

Wait, but the diff is changing the skipped path synthesis. Let me look more carefully. The pre-existing code in the skipped path was:
```go
results = append(results, VerificationResult{
    File: f.File, Line: f.Line, Problem: f.Problem,
    Verdict:   f.Verification.Verdict,
    Skeptic:   f.Verification.Skeptic,
    Reasoning: f.Verification.Notes,
})
```

This already dereferences `f.Verification`. So this assumption was already made. The diff doesn't change this. OK.

### Issue 11: priorByKey could include records for findings not in current run

The `priorByKey` map includes ALL records from prior verification.json, even findings that no longer exist. But the code only looks up `priorByKey[key]` for keys in the current `findings` slice. So extra entries are harmless. The TD-007 check is in a different place (after the loop). Good.

### Issue 12: Concurrency?

I don't see any concurrency considerations. The priorByKey is read-only after construction. The Verdict string comparison is safe. OK.

### Issue 13: Test for skip-preserves-prior

```go
res, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
    t.Fatal("already-verified finding must not invoke the harness")
    return nil, nil, nil, nil
})
```

This test relies on the finding being skipped. The `Options{}` is empty (no Thorough). The finding has `Confidence: "VERIFIED"` and `Verification: &reconcile.Verification{Verdict: "confirmed", Skeptic: "s1"}`. The `hasTrustedVerdict` should return true. Then it's skipped.

Wait, but the skeptic registry is `skepticRegistry()`. The diff references this helper. Let me look at the test file to understand the helper.

Actually I should focus on the diff. The test looks reasonable.

### Issue 14: `aggregateVerdicts` tie behavior

In the 1/1/1 split test, the test expects `vf.Findings[0].Verdict == "unverifiable"`. This depends on `aggregateVerdicts` resolving ties to unverifiable. The comment says so, and the test passes (assuming the test runs). The test is asserting behavior, not validating it.

### Issue 15: `winningAttribution` when `winner` has count 0

If `ver.Verdict` is some verdict that no skeptic produced... actually, aggregateVerdicts probably always returns one of the produced verdicts. But hypothetically:
- counts = {c:2, r:1}
- winner = "somethingelse" (impossible?)
- counts[winner] = 0
- For c: 2 >= 0? Yes. decisive=false.
- Records all.

OK.

### Issue 16: `seenBudget` map per tied verdict

When decisive=false (tie), all skeptics' tripped budgets are accumulated. The deduplication is per-call. This is fine. If two skeptics trip the same budget, only one is recorded. That's reasonable.

But wait, the comment says: "Tripped budgets attach only to unverifiable verdicts (a budget trip collapses a skeptic to unverifiable), so a confirmed/refuted winner correctly carries an empty TrippedBudgets."

So if a tied verdict includes an unverifiable voter (which is always the case for a tie to unverifiable), and that voter tripped a budget, the budget appears in TrippedBudgets. That's correct.

What if a tied verdict is "confirmed" (impossible since aggregateVerdicts resolves ties to unverifiable)? Then no tripped budgets. That's also correct.

### Issue 17: `winner` parameter in `winningAttribution` for tied unverifiable

Tied unverifiable: counts={u:1, c:1, r:1}, winner="unverifiable".
- counts[winner] = 1
- For c: 1 >= 1? Yes. decisive=false.
- Records all.
- For each voter, check budget trip:
  - u voter: probably tripped (so has tripped budgets)
  - c voter: no budget (clean verdict)
  - r voter: no budget (clean verdict)
- Tripped budgets: from u voter only.
- Models: all three, deduplicated.

In the test:
```go
"m-s2":   `{"verdict":"confirmed","reasoning":"yes"}`,
"m-s3":   `{"verdict":"refuted","reasoning":"no"}`,
"m-skep": `{"verdict":"unverifiable","reasoning":"dunno"}`,
```

The unverifiable vote from "m-skep" was reached via malformed output, not a budget trip. So `perTripped` for that skeptic would be `nil`. So the resulting TrippedBudgets would be empty. The test doesn't check this, but the comment in the function says this is correct behavior.

### Issue 18: Edge case - `perSkeptic` is empty

If all skeptics somehow return nil and the slices are empty, the function would return "", []. The test doesn't cover this.

### Issue 19: Potential issue with mixed-case verdict comparison

The compare uses `strings.EqualFold` with `TrimSpace`. This handles case and whitespace. Good. But the prior's verdict could be in a different format. Let me check.

Actually, `prior.Verdict` is read from verification.json which was written by `WriteVerification`. So it should be in the canonical format. But the comparison is defensive. Good.

### Issue 20: pre-existing `hasTrustedVerdict` in skip path

The diff doesn't change `hasTrustedVerdict`, but the carry-forward assumes the finding was skipped. If `hasTrustedVerdict` returns false, the finding is re-verified and goes through the rich map path. The skip path synthesis (now with carry-forward) is only for actually-skipped findings.

But wait, the skip-path code synthesizes a record from the on-disk block. The `f.Verification` should be the trusted one. So `f.Verification.Verdict` is the verdict to compare against. Good.

### Issue 21: Type assertion in tests

```go
func (b byModelChat) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
    return b.byModel[inv.Model], nil
}
```

This returns a string but `fanout.ChatCompleter` interface may have a different method. Let me check.

Looking at `alwaysChat`:
```go
type alwaysChat struct{}
func (a alwaysChat) Chat(_ context.Context, _ llmclient.Invocation, _ []llmclient.Message, _ []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
```

And:
```go
type finalChat string
func (f finalChat) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
```

OK so `Chat` and `Complete` are both required for `fanout.ChatCompleter`. `byModelChat` implements both. Good.

### Issue 22: Missing import

`emit_verification.go` adds `errors` import. `pipeline.go` was already importing `strings`. Let me check if any imports are missing.

Actually the diff doesn't show context for all imports. Let me focus on the visible changes.

### Issue 23: Test for `SkipDropsMismatchedPriorMetadata`

```go
dir := pipelineReview(t, []reconcile.JSONFinding{{
    Severity: "HIGH", File: "a.go", Line: 1, Problem: "boom", Confidence: "VERIFIED",
    Reviewers: []string{"rev"}, Verification: &reconcile.Verification{Verdict: "confirmed", Skeptic: "s1"},
}})
```

The Confidence is "VERIFIED" and Verification has verdict "confirmed". Should be trusted and skipped.

```go
require.NoError(t, WriteVerification(dir, []VerificationResult{{
    File: "a.go", Line: 1, Problem: "boom", Verdict: "refuted", Skeptic: "s1",
    Model: "m-stale", DurationMs: 9999, TrippedBudgets: []string{"timeout_secs"},
}}))
```

Prior verdict is "refuted" but current is "confirmed" - mismatch.

```go
_, err := runVerify(context.Background(), dir, skepticRegistry(), Options{}, func() (fanout.ChatCompleter, Dispatcher, func(), error) {
    t.Fatal("already-verified finding must not invoke the harness")
    return nil, nil, nil, nil
})
```

`Options{}` is non-Thorough. So only one skeptic runs. The finding is trusted (Confidence: VERIFIED), so it's skipped. Harness should not be invoked.

After re-run:
- The finding is in `rich` map? No, because the finding was not re-verified (skipped). 
- Wait, `rich` map is populated by what? Let me look at the code path before the change.

Actually I don't have full context, but typically `rich` is the in-memory map of findings that WERE re-verified this run. Skipped findings are not in `rich`.

So the finding falls into the "skipped this run" path:
- `key` is the FindingKey
- `r, ok := rich[key]` - false (skipped)
- Synthesize record with verdict="confirmed", skeptic="s1", reasoning=""
- Look up prior: `priorByKey[key]` returns the prior record
- Verdict match check: "refuted" vs "confirmed" - no match
- Don't carry forward
- Result: `rec` has Verdict="confirmed", Model="", DurationMs=0, TrippedBudgets=nil

Test asserts:
- `vf.Findings[0].Verdict == "confirmed"` ✓
- `vf.Findings[0].Model == ""` ✓
- `vf.Findings[0].DurationMs == 0` ✓
- `vf.Findings[0].TrippedBudgets == nil/empty` ✓

Good.

### Issue 24: `tripped` nil vs empty slice

In `invokeSkeptic`:
- Clean verdict path: `return v, nil, nil` - nil slice
- Unverifiable (engine no result): `return ..., nil, nil` - nil slice
- Unverifiable (budget trip): `return ..., res.TrippedBudgets, nil` - whatever res.TrippedBudgets is

In `winningAttribution`:
```go
for _, b := range perTripped[i] {
    if !seenBudget[b] {
        ...
    }
}
```

`range` over nil slice is a no-op. OK.

But the test asserts `assert.Empty(t, tripped, "a clean verdict trips no budgets")` - this works for both nil and empty slice.

### Issue 25: `base.Model = ""` already on `verdictUnverifiable` in the early returns

In `verifyFinding`:
```go
type VerificationResult struct {
    ...
    Model string `json:"model"`
    ...
}
```

The zero value of `Model` is `""`. So `base.Model = ""` is redundant but explicit. The comment justifies this. Good.

### Issue 26: When `aggregateVerdicts` returns empty Verdict

`ver.Verdict` could be empty if `aggregateVerdicts` returns an empty Verdict for some reason. In `winningAttribution`:
- `winner = ""`
- `counts[""] = 0` (zero value)
- For all other verdicts: `n >= 0` is true → decisive=false
- Records all voters

If a voter is `nil`, skipped. Otherwise:
```go
if m := skeptics[i].Config.Model; m != "" && !seenModel[m] {
    seenModel[m] = true
    models = append(models, m)
}
```

Adds models. Final Model: join of all voters' models. Hmm, this could be a problem if winner is empty but not because of nil entries.

Wait, this only happens if `ver.Verdict` is "". I'm not sure if `aggregateVerdicts` can return "". Looking at the test cases, it always returns "confirmed", "refuted", or "unverifiable". Probably fine.

### Issue 27: `perSkeptic` entries with empty `Verdict`

If `parseVerdict` returns a `Verification` with empty Verdict (shouldn't happen, but defensive), then:
- `counts[""]++` for that skeptic
- This would add to the empty verdict count
- If `winner` is "", `counts[winner]` increases
- If `winner` is "confirmed", `counts[""]` would be 0, and `n=0 >= counts[winner]?` could be true

This is a defensive concern. But `parseVerdict` probably normalizes. Let's not flag this.

### Issue 28: Test for TwoConfirmOneRefute - runVerify may not return the actual finding

The test reads `vf.Findings[0]`. If multiple findings are produced, this could be wrong. But the test only seeds one finding. OK.

### Issue 29: priorByKey construction efficiency

`priorByKey` is built unconditionally even if no findings are skipped. Minor inefficiency, not a bug.

### Issue 30: `fmt.Fprintf` to stderr for prior unreadable

```go
if prior, perr := ReadVerificationResults(reviewDir); perr != nil {
    fmt.Fprintf(os.Stderr, "atcr: verify: prior verification.json unreadable, skip-path metadata not carried forward: %v\n", perr)
} else {
    ...
}
```

If `perr != nil`, `prior` is nil (from ReadVerificationResults). The `else` branch is correct. But the `prior` variable is scoped to the `if` initializer. The `for _, r := range prior` in the else branch is fine.

This is using `fmt.Fprintf` to stderr. The codebase probably has a logger. Let me check.

Looking at the existing code in the diff, it uses `logSkepticFailure` (a function) for logging. Using direct `fmt.Fprintf` to stderr is inconsistent. But this is minor.

Actually, looking at the diff in pipeline.go, the existing code already uses `fmt.Fprintf(os.Stderr, ...)` style in some places. So this is consistent with existing patterns. OK.

### Issue 31: `String` verdict comparison

What if the prior has a verdict in different format like "Confirmed" vs "confirmed"? The `strings.EqualFold` handles this. Good.

But what if the prior verdict has trailing whitespace? `TrimSpace` handles this. Good.

What if the prior verdict is "" (empty)? Then `TrimSpace("")` is "", and `f.Verification.Verdict` is "confirmed" or similar. Mismatch. No carry-forward. Good.

### Issue 32: Carry-forward with non-canonical prior verdicts

If the prior is a non-canonical verdict like "skipped" or "unknown", the current `f.Verification.Verdict` (a canonical verdict from findings.json) won't match. No carry-forward. Good.

But what if both are non-canonical? Then they could match and carry forward stale metadata. This is an edge case not likely in practice.

### Issue 33: Test order dependency

`TestRunVerify_SkipDropsMismatchedPriorMetadata` and `TestRunVerify_SkipPreservesPriorMetadata` use `t.TempDir()` via `pipelineReview`. They should be independent.

### Issue 34: `pipelineReview` helper

The test uses `pipelineReview` which I can't see in the diff. But it's an existing helper. OK.

### Issue 35: `skepticRegistry` helper

The test uses `skepticRegistry()`. Also an existing helper. OK.

### Issue 36: Test for TwoConfirmOneRefute

The test uses `Options{Thorough: true}`. The `skepticRegistry` has `s1, skep` (probably). After adding s2, s3, the registry has 4 skeptics. The selection logic picks the relevant ones based on role. The test expects 3 to be selected: s2, s3, skep (alphabetical). OK.

But what if the selection logic includes s1 too? The test would fail. The comment says "Alphabetical selection" so the test author assumes s1 is not selected. Hopefully that's correct.

### Issue 37: Final review of completeness

Let me also check:
- The comment in `VerificationResult` says "Model names only the skeptics whose verdict produced the recorded outcome" - good documentation
- The comment in `winningAttribution` describes both decisive and tie cases
- Tests cover AC1, AC2, AC3, AC4
- ReadVerificationResults is tested

### Issue 38: Scope of `f.Verification == nil` in skip path

The diff assumes `f.Verification` is non-nil in the skip path. The pre-existing code dereferences it. So this isn't a new issue. But if the pre-existing code had a nil check that the diff removed, that would be a regression. Let me check... no, the diff adds code after the dereference but doesn't remove any nil check. OK.

### Issue 39: `len(f.Verification.Notes)` vs `f.Verification.Notes`

In the new code:
```go
Reasoning: f.Verification.Notes,
```

This was in the pre-existing code. Not changed. OK.

### Issue 40: `perTripped` alignment with `perSkeptic`

In verifyFinding:
```go
perSkeptic := make([]*reconcile.Verification, 0, len(skeptics))
perTripped := make([][]string, 0, len(skeptics))
for _, sk := range skeptics {
    v, tripped, ierr := invokeSkeptic(ctx, sk, prompt, cc, disp)
    if ierr != nil {
        v = &reconcile.Verification{Verdict: verdictUnverifiable, Notes: ierr.Error(), Skeptic: sk.Name}
        tripped = nil
    }
    perSkeptic = append(perSkeptic, v)
    perTripped = append(perTripped, tripped)
}
```

The `tripped = nil` reset on ierr is important to keep the slices aligned. Good.

### Issue 41: Skip-path review of model attribution

For skipped findings, `winningAttribution` is not called. The model is just carried forward from the prior. This makes sense because the finding was not re-verified this run.

### Issue 42: Rich records losing Model

If a finding IS re-verified this run (in the rich map), the rich record is used. The rich record's Model is set by `winningAttribution` in verifyFinding. So it should be correct.

### Issue 43: The `rich[key]` check

The diff says:
```go
for _, f := range findings {
    key := FindingKey{...}
    if r, ok := rich[key]; ok {
        results = append(results, r)
        continue
    }
    // Skipped this run: synthesize from on-disk block
    ...
}
```

So `rich` is the map of findings verified this run. Skipped findings go to the synthesis path. Good.

### Issue 44: Stderr logging level

`fmt.Fprintf(os.Stderr, "atcr: verify: ...")` - this is just informational. The user might want to suppress it on first run. Minor concern.

### Issue 45: What if prior is empty array?

`ReadVerificationResults` returns `(nil, nil)` for missing file and the parsed findings for present file. If the file has `{"findings": []}`, it returns `[]VerificationResult{}` (empty, non-nil). Then `priorByKey` is empty. The skip path doesn't find a prior, so no carry-forward. The harness may be invoked if the finding is not trusted. OK.

### Issue 46: Test for missing prior

The `ReadVerificationResults` test covers missing file → (nil, nil). Good.

### Issue 47: Test for corrupt prior

The test covers corrupt file → error. The caller logs to stderr. But the test doesn't directly check that the pipeline handles a corrupt prior gracefully. That's tested implicitly. The "unreadable" case is also a concern - what about a permission error? `errors.Is(err, os.ErrNotExist)` only handles the not-exist case. Other errors are returned. The caller logs and continues without carry-forward. Good defensive design.

### Issue 48: `ver` in verifyFinding - is it possible for `aggregateVerdicts` to return nil?

If all perSkeptic are nil, `aggregateVerdicts` could return nil. Then `ver.Verdict` would panic.

Looking at the call:
```go
ver := aggregateVerdicts(perSkeptic)

base.Verdict = ver.Verdict
```

If `ver` is nil, this panics. But `aggregateVerdicts` probably returns at least an empty struct for non-empty input. This is a defensive concern not introduced by this diff.

Actually, let me look at the call site. If all `invokeSkeptic` calls return `nil` for `*reconcile.Verification`, then perSkeptic has nil entries. But the code in verifyFinding always sets `v` to a non-nil struct on error:
```go
v = &reconcile.Verification{Verdict: verdictUnverifiable, Notes: ierr.Error(), Skeptic: sk.Name}
```

But on success, `invokeSkeptic` could return nil for `*reconcile.Verification`? Let me check.

Looking at invokeSkeptic:
- On nil ctx/cc/disp: `return nil, nil, errors.New(...)` - returns nil Verification
- On engine no result: `return &reconcile.Verification{...}, nil, nil` - non-nil
- On status/budget: `return &reconcile.Verification{...}, res.TrippedBudgets, nil` - non-nil
- On parseVerdict: `parseVerdict` returns a value (`v, _ := parseVerdict(...)`). v is non-nil unless parseVerdict returns nil.

The first return (nil ctx) is a programming error that shouldn't happen in normal flow. But if it did, perSkeptic would have a nil entry, and `aggregateVerdicts` might fail.

In `winningAttribution`:
```go
for _, v := range perSkeptic {
    if v != nil {
        counts[v.Verdict]++
    }
}
```

This handles nil entries. So if `aggregateVerdicts` also handles nil entries (probably does), the code is safe.

But the line `ver := aggregateVerdicts(perSkeptic)` then `base.Verdict = ver.Verdict` could panic if `ver` is nil. This is a pre-existing concern, not introduced by the diff.

### Issue 49: Concurrency / race in stderr write

The stderr write is single-threaded in this function. No race.

### Issue 50: `ReadVerificationResults` race?

Just file read, no shared state. OK.

### Issue 51: Test for "VERIFIED" confidence

The `hasTrustedVerdict` function checks verdict, not confidence. The diff doesn't change this. OK.

Actually, looking at the test for skip path:
```go
Confidence: "VERIFIED",
Reviewers: []string{"rev"}, Verification: &reconcile.Verification{Verdict: "confirmed", Skeptic: "s1"},
```

The confidence is "VERIFIED" but that's not what `hasTrustedVerdict` checks. The verification has verdict "confirmed" which IS trusted. So the test is correct.

### Issue 52: `os.WriteFile` permission

The corrupt test uses `0o644` for the corrupt file. Good.

### Issue 53: `os.MkdirAll` for subdir

The test uses `os.MkdirAll(filepath.Join(bad, reconciledSubdir), 0o755)`. Good.

### Issue 54: Potential nil dereference in `byModelChat`

```go
func (b byModelChat) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
    return b.byModel[inv.Model], nil
}
```

If `inv.Model` is not in `b.byModel`, the map returns the zero value (empty string). The chat completer returns empty string. The parser would see empty content, mark it as `empty_response` or `malformed_output`, and set `verdictUnverifiable`. OK.

But wait, for the tie test, `m-skep` returns `{"verdict":"unverifiable","reasoning":"dunno"}` which is a valid JSON. So parseVerdict would succeed. The verdict is "unverifiable" with reasoning "dunno". This is a valid path.

For the budget trip test, the model is not in `b.byModel` so the chat returns "". But this test doesn't use byModelChat. OK.

### Issue 55: `winningAttribution` - what if all entries have empty Verdict?

If `parseVerdict` returns `Verification{Verdict: "", ...}`, then:
- counts = {"":n}
- winner might be ""
- If winner is "": counts[winner] = n
- For other verdicts (none if all are empty): decisive=true
- Records all voters with empty verdicts

But this is a weird edge case. Let me not flag this.

### Issue 56: Decisive check in `winningAttribution` - off-by-one?

```go
decisive := true
for verdict, n := range counts {
    if verdict != winner && n >= counts[winner] {
        decisive = false
        break
    }
}
```

If another verdict equals or exceeds the winner, it's a tie. Correct.

Edge case: if `winner` is not in `perSkeptic` (impossible if aggregateVerdicts returns from input), then `counts[winner] = 0`. Any verdict with n>=0 makes it non-decisive. So decisive=false. Records all.

This is the right behavior for "winner is not in the actual results".

### Issue 57: `winningAttribution` not symmetric with `aggregateVerdicts`

If `aggregateVerdicts` uses different tie-breaking logic than `winningAttribution`, they could disagree. For example, if aggregateVerdicts resolves 2-1 to confirmed (decisive), and winningAttribution also sees 2-1 to confirmed (decisive). Same logic. OK.

But what if aggregateVerdicts uses some other tie-break? E.g., 2 confirmed, 1 refuted, 1 unverifiable - which wins? Probably confirmed (plurality). winningAttribution: counts={c:2, r:1, u:1}. For r: 1>=2? No. For u: 1>=2? No. decisive=true. Records c voters. OK.

### Issue 58: `ver.Skeptic` for tie case

For a tie (1+1+1), `ver.Skeptic` names every voter (per the comment in `joinSkeptics`). The `winningAttribution` records all voters' models. So `ver.Skeptic` and `base.Model` are consistent. Good.

### Issue 59: `ver.Skeptic` for decisive case

For a decisive verdict, `ver.Skeptic` probably names the majority. `winningAttribution` records only the winners' models. Consistent.

But wait, what if `ver.Skeptic` includes all voters (including losers) for a decisive case? Then `base.Skeptic` would include losers but `base.Model` would not. Inconsistent.

Looking at the test for TwoConfirmOneRefute:
```go
assert.Contains(t, vf.Findings[0].Model, "m-s2", "winning skeptic model must be recorded")
assert.Contains(t, vf.Findings[0].Model, "m-skep", "winning skeptic model must be recorded")
assert.NotContains(t, vf.Findings[0].Model, "m-s3", "losing (refuting) skeptic model must NOT be attributed")
```

The test only checks Model, not Skeptic. If `Skeptic` includes the losing skeptic, that would be inconsistent. But the test doesn't catch this.

Hmm, the comment says "Skeptic names every participating voter; Model names only the skeptics whose verdict produced the recorded outcome". So Skeptic is all voters, Model is winners. This is documented as a design choice. OK.

But wait, that's a potential issue. If Skeptic includes losers, the audit trail is inconsistent: "Skeptic: s1, s2, s3" but "Model: m-s2, m-skep" - which model corresponds to which skeptic? Hard to tell. But this is a design choice, not a bug. Not flagging.

### Issue 60: `ver.Skeptic` could be a single name even for multi-vote

This is a possibility. If aggregateVerdicts names only the "lead" skeptic. The test doesn't check Skeptic for the multi-vote case, so this isn't tested. The comment in the diff says "Skeptic names every participating voter" - this is documented as a design choice, but only for ties. For decisive cases, it could be a single name. The comment is ambiguous.

Let me look at the comment more carefully:
```
// Skeptic names every participating voter; Model names only the skeptics whose
// verdict produced the recorded outcome — a winners-only subset on a decisive vote,
// all participants on a tie, and "" when no skeptic executed. So for a multi-vote
// run Model may list fewer entries than Skeptic by design (see winningAttribution).
```

This says "Skeptic names every participating voter" universally. But this might not be true if aggregateVerdicts doesn't do this. Without seeing aggregateVerdicts, I'll trust the comment.

OK, I think I've been thorough enough. Let me summarize the issues I've found.

### Summary of real issues:

1. **`winningAttribution` doesn't use `ver.Skeptic` to determine winners**: The function uses verdict matching against `winner`, but `ver.Skeptic` might already list the winners. The comment in the sprint plan says "Winning skeptics = those whose per-skeptic verdict equals the aggregated (majority) verdict". The implementation matches this. But the sprint plan also says "identify the winning skeptic by matching the aggregated verdict against per-skeptic results (mirroring `joinSkeptics`)". This is what the code does. OK, no issue.

2. **Stderr write is inconsistent with codebase logging**: The codebase uses `logSkepticFailure` (a function). Direct `fmt.Fprintf` to stderr is inconsistent. Minor.

3. **Pre-existing concern: `ver := aggregateVerdicts(...)` could panic on nil**: Not introduced by this diff.

4. **`priorByKey` constructed even if not needed**: Minor efficiency.

5. **`base.Skeptic` not set on early-return paths**: Pre-existing, not in scope.

6. **`winningAttribution` aggregates tripped budgets across all skeptics even on decisive unverifiable verdicts**: Wait, let me think. If winner=unverifiable (decisive, e.g., 2 unverifiable, 1 confirmed), then all unverifiable voters are recorded. Their tripped budgets are aggregated. That's correct - the unverifiable voters' tripped budgets should be reported. OK.

7. **Race in `perTripped` for nil error path**: In verifyFinding, on ierr, `tripped = nil` is set BEFORE append. Good. No race.

8. **`perSkeptic` is built but `len(skeptics) > 0` and all return nil**: `aggregateVerdicts` would need to handle this. Defensive concern.

Hmm, I don't see major issues. Let me look once more for issues in the sprint plan scope.

Actually, let me re-read the diff for the `pipeline.go` more carefully:

```go
priorByKey := map[FindingKey]VerificationResult{}
if prior, perr := ReadVerificationResults(reviewDir); perr != nil {
    fmt.Fprintf(os.Stderr, "atcr: verify: prior verification.json unreadable, skip-path metadata not carried forward: %v\n", perr)
} else {
    for _, r := range prior {
        priorByKey[FindingKey{File: r.File, Line: r.Line, Problem: r.Problem}] = r
    }
}
```

Wait, what if `perr != nil` because the file was corrupt, but it had some parseable records? `ReadVerificationResults` returns an error for any parse error, even partial. But typically, JSON parse fails entirely on first error. So if it's corrupt, all is lost. The skip path just continues with empty priorByKey. The test for corrupt file expects an error. OK.

But there's a subtle issue: the user might want to know that their prior verification.json is corrupt. The stderr message does say so. Good.

9. **`ver` nil panic**: I already noted this. Pre-existing.

10. **Look at the test `TestRunVerify_ThoroughMultiSkepticRecordsAllModels`** - this is pre-existing. Not changed. OK.

11. **`ver.Skeptic` for the multi-skeptic test**: The pre-existing test only checks that the Model contains "m-skep". It doesn't check Skeptic. If Skeptic is all voters (per the new comment), the test would still pass. If Skeptic is a single name, the test would still pass. OK.

12. **Documentation comment for `VerificationResult`**: The new comment is detailed and accurate. Good.

13. **Test for `TestRunVerify_ThreeWayTieRecordsAllParticipantModels`**: This test only checks Model, not Skeptic. The comment says Skeptic should be all participants. The test doesn't verify this. Minor: test could be more thorough.

14. **`aggregateVerdicts` empty input**: If `perSkeptic` is empty, what does `aggregateVerdicts` return? If it panics or returns nil, the subsequent code would fail. But this is pre-existing.

OK, I think the main issues to report are:

A. **stderr logging is inconsistent with codebase patterns** (minor, maintainability)
B. **Pre-existing nil deref in `ver := aggregateVerdicts(...)` if it returns nil** (not in diff, out of scope)

Actually, let me re-examine for things specifically in scope of the sprint plan:

The sprint plan is in scope for:
- `internal/verify/invoke.go` - signature change
- `internal/verify/pipeline.go` - winning attribution, skip path, early returns
- `internal/verify/emit_verification.go` - new reader
- Tests for these

Let me look at the tests once more:

`TestRunVerify_SkipDropsMismatchedPriorMetadata` - tests carry-forward guard
`TestRunVerify_SkipPreservesPriorMetadata` - tests carry-forward
`TestVerifyFinding_EarlyReturnsRecordEmptyModel` - tests early returns
`TestRunVerify_WinningModelAttribution_TwoConfirmOneRefute` - tests winning attribution
`TestRunVerify_ThreeWayTieRecordsAllParticipantModels` - tests tie case
`TestInvokeSkeptic_SurfacesTrippedBudgets` - tests tripped budget surfacing
`TestInvokeSkeptic_NoTrippedBudgetsOnCleanVerdict` - tests clean case
`TestReadVerificationResults` - tests reader

All ACs have tests. Good.

But wait, the sprint plan AC5 says: "new tests cover multi-skeptic model attribution (2-confirm/1-refute) and skip-already-verified metadata preservation". Both are covered.

Let me also check the `pipeline_test.go` modification for any issues:

```go
type byModelChat struct{ byModel map[string]string }

func (b byModelChat) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
    return b.byModel[inv.Model], nil
}
```

If `b.byModel` is nil and `inv.Model` is in it, the map access would panic. But the tests initialize it. OK.

```go
func (b byModelChat) Chat(_ context.Context, inv llmclient.Invocation, _ []llmclient.Message, _ []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
    c := b.byModel[inv.Model]
    return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", Content: &c}, FinishReason: "stop"}, nil
}
```

`&c` takes address of local variable. This is fine in Go. The content is escaped.

Wait, but `c` is a local variable. Taking its address in a return value... Go's escape analysis handles this. The `&c` would be promoted to heap. OK.

Actually, looking at the `alwaysChat` for comparison:
```go
func (a alwaysChat) Chat(_ context.Context, _ llmclient.Invocation, _ []llmclient.Message, _ []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
    c := ""
    return &llmclient.ChatResponse{Message: llmclient.Message{Role: "assistant", Content: &c}, FinishReason: "stop"}, nil
}
```

Same pattern. OK.

### Final review of real issues:

I think the main legitimate issues are:

1. **`fmt.Fprintf(os.Stderr, ...)` is inconsistent with codebase logging patterns**: The codebase has `logSkepticFailure` (a function). Direct stderr write doesn't follow the established pattern. This is a maintainability concern.

2. **Tests don't cover the `Skeptic` field on multi-vote results**: The comment in the diff says "Skeptic names every participating voter; Model names only the skeptics whose verdict produced the recorded outcome". The tests check Model but not Skeptic. If the implementation doesn't match the documentation, the tests wouldn't catch it.

3. **In `winningAttribution`, the `decisive` check could be incorrect for cases where the winner has 0 count**: If `counts[winner] == 0` (e.g., winner="" or winner is a verdict no skeptic produced), then any verdict with n>=0 makes it non-decisive. So decisive=false, records all. This is the correct behavior (records all on no-winner case).

4. **Possible nil deref in `ver := aggregateVerdicts(perSkeptic); base.Verdict = ver.Verdict`**: If `aggregateVerdicts` returns nil, this panics. But the function probably doesn't. Pre-existing.

5. **Comment vs implementation mismatch for `decisive`**: The comment says "Decisive (a strict plurality, e.g. 2 confirmed > 1 refuted → confirmed)". The implementation uses `n >= counts[winner]` which means a tie at the top is NOT decisive. So "2 confirmed, 2 refuted" is NOT decisive (decisive=false, all recorded). This matches "strict plurality" - 2>2 is not a strict plurality. OK, comment matches.

6. **`perTripped` for budget-tripped skeptic** - the diff says "Tripped budgets attach only to unverifiable verdicts (a budget trip collapses a skeptic to unverifiable), so a confirmed/refuted winner correctly carries an empty TrippedBudgets." But what if a confirmed skeptic ALSO tripped a budget (the model returns content with verdict:confirmed even though it hit max_turns)? Actually no - looking at invoke.go, if `len(res.TrippedBudgets) > 0`, the verdict is forced to `verdictUnverifiable`. So a budget-tripped skeptic always has `verdictUnverifiable`. The comment is correct.

7. **What if `perTripped` has non-nil entries for clean verdicts?** Looking at invoke.go, clean verdict returns `nil, nil`. So clean verdicts have nil perTripped. OK.

Hmm, I think the issues I've found are minor. Let me re-examine the diff for any clear bugs.

Actually, I want to look at this line again:

```go
prior := priorByKey[key]
if strings.EqualFold(strings.TrimSpace(prior.Verdict), strings.TrimSpace(f.Verification.Verdict)) {
    rec.Model = prior.Model
    rec.DurationMs = prior.DurationMs
    rec.TrippedBudgets = prior.TrippedBudgets
}
```

The `prior` variable shadows the outer scope. But the outer scope doesn't use `prior` (it's only used here in the new code). OK.

But wait, the outer `prior` was the local variable from `if prior, perr := ReadVerificationResults(reviewDir); perr != nil`. The shadowing is intentional and fine.

8. **`priorByKey` is constructed even when not needed**: Always constructed at the start of runVerify. If no findings are skipped, this is wasted work. Minor efficiency concern.

9. **JSON unmarshal of corrupt file**: If the file is JSON but has unexpected structure (e.g., `{"findings": null}`), `vf.Findings` would be nil. Then `priorByKey` would be empty. OK.

10. **JSON unmarshal of corrupt file with partial data**: If `verification.json` is `{"findings": [...incomplete]}`, json.Unmarshal would fail. ReadVerificationResults returns an error. The caller logs and continues. OK.

Let me look for issues specifically with the verification of the AC:

AC1: `invokeSkeptic` returns `trippedBudgets []string` separately. ✓ (implemented)
AC2: In multi-skeptic run, `VerificationResult.Model` reflects winning skeptic. ✓ (winningAttribution)
AC3: `VerificationResult.Model` is non-empty on `no_eligible_skeptic` and `tool_harness_unavailable`. ✓ (set to "" explicitly)
AC4: skip-already-verified path carries forward. ✓ (carry-forward logic)
AC5: All existing pipeline tests pass. (Need to verify - I can't run them)

All ACs are addressed in the diff. The implementation looks reasonable.

Let me check once more for any subtle bugs:

11. **`winningAttribution` with all nil perSkeptic**: If all are nil, the loop `for _, v := range perSkeptic { if v == nil { continue } ... }` skips all. Returns "", nil. OK.

12. **`winningAttribution` with empty `ver.Verdict`**: Discussed above. Records all.

13. **`winningAttribution` with `winner` not in counts**: Discussed above. Records all.

14. **Test for TwoConfirmOneRefute**: This test uses `Options{Thorough: true}`. The `skepticRegistry()` returns a registry. After adding s2, s3, the registry has 4 skeptics: s1, s2, s3, skep. The selection for "skeptic" role is alphabetical: s1, s2, s3, skep (or some subset). Hmm, wait - the test relies on s1 NOT being selected. Let me re-read.

Looking at the test:
```go
reg := skepticRegistry() // skep=m-skep
reg.Agents["s2"] = registry.AgentConfig{Provider: "p", Model: "m-s2", Role: registry.RoleSkeptic, SupportsFC: true}
reg.Agents["s3"] = registry.AgentConfig{Provider: "p", Model: "m-s3", Role: registry.RoleSkeptic, SupportsFC: true}
// Alphabetical selection: s2(m-s2), s3(m-s3), skep(m-skep). s3 refutes; the
// other two confirm → winner=confirmed, winners={m-s2, m-skep}.
```

If s1 is also selected, the test would fail. The comment says "Alphabetical selection: s2, s3, skep" - implying s1 is NOT in the registry. Let me check `skepticRegistry()` - it's an existing helper that I don't have full visibility into. The comment in the diff suggests it returns only "skep" and possibly "s1". The test author assumes s1 is not in the registry.

This is a test concern, not a code concern. If the test assumption is wrong, the test would fail. But the test author seems to know the helper. OK.

15. **`runVerify` reading prior**: This is done unconditionally. If the file doesn't exist (first run), `ReadVerificationResults` returns (nil, nil), and `priorByKey` is empty. No issue.

16. **TOCTOU on prior verification.json**: If the file is deleted between the check and the read, the read would fail. But the check is for "unreadable", not "exists", so a deletion after the function returns would just mean the carry-forward doesn't happen. Not a real issue.

17. **`fmt.Fprintf` is a method, not a logger**: The codebase has `logSkepticFailure` as a function. Using `fmt.Fprintf` directly to stderr bypasses any structured logging. This is a maintainability concern.

18. **The stderr message format**: `"atcr: verify: prior verification.json unreadable, skip-path metadata not carried forward: %v\n"` - this has a typo: "unreabable" should be "unreadable". Wait, let me re-read: "unreadable". OK, no typo. But the wording is awkward.

Actually, the diff says: `"atcr: verify: prior verification.json unreadable, skip-path metadata not carried forward: %v\n"`. That's "unreadable". Good.

19. **The `fmt.Fprintf` call uses `os.Stderr`**: This requires `os` import. Let me check if `os` is imported in pipeline.go. The diff doesn't show imports, but `os` was likely already imported (since the file is `pipeline.go` and has lots of file ops).

20. **`fmt` import in pipeline.go**: The `fmt.Fprintf` call requires `fmt`. If `fmt` was not already imported, this would be a compile error. The diff doesn't show imports. I'll assume `fmt` is imported.

21. **What if the prior verification.json has findings not in the current `findings`?**: The `priorByKey` includes them, but they're never looked up. The TD-007 check (after the loop) surfaces key-construction mismatches. OK.

22. **What if the current `findings` has a key not in `priorByKey`?**: The lookup returns zero-value `VerificationResult{Verdict: ""}`. The match check fails. No carry-forward. OK.

23. **`aggregateVerdicts` returning empty Verdict**: If `ver.Verdict` is "", then `winningAttribution` would behave as "no winner", recording all voters. Hmm, this might be a bug if `aggregateVerdicts` ever returns "" for non-empty input. But I doubt it does.

24. **The function `winningAttribution` has a comment that's verbose**: Some might say too verbose, but documentation is generally good. Not a bug.

25. **Decisive check uses `>=`**: This is correct for "strict plurality" (strict >). If another verdict equals, it's a tie. If `n > counts[winner]`, decisive would still be true (which would be a bug - the other verdict won). Wait, let me re-check.

If counts = {c:2, r:3}, winner=c (assuming aggregateVerdicts picks c... wait, if r has 3 and c has 2, then r would be the winner, not c). OK, that's fine.

If counts = {c:2, r:2}, winner=c (tie resolved to c somehow?): for r, n=2 >= counts[c]=2, so decisive=false. OK.

If counts = {c:3, r:2, u:1}, winner=c: for r, n=2 >= 3? No. For u, n=1 >= 3? No. decisive=true. Records only c voters. OK.

If counts = {c:3, r:3, u:1}, winner=c: for r, n=3 >= 3? Yes. decisive=false. OK.

If counts = {c:1, r:1, u:1}, winner=u (tie resolved to u): for c, n=1 >= 1? Yes. decisive=false. OK.

If counts = {c:2, r:1}, winner=c: for r, n=1 >= 2? No. decisive=true. OK.

If counts = {c:2, r:1}, winner=r (aggregateVerdicts picked r?): for c, n=2 >= 1? Yes. decisive=false. OK, but this is weird because r has fewer votes.

Wait, this last case is weird. If aggregateVerdicts picks r as the winner (despite c having more votes), then `winningAttribution` would record all voters. But that's not what we want - we want to record only the winners.

But this scenario (aggregateVerdicts picking a non-plurality winner) shouldn't happen with a correct `aggregateVerdicts`. So this is not a real concern.

26. **What if `aggregateVerdicts` picks `unverifiable` as the winner when unverifiable is the plurality?**: E.g., 2 unverifiable, 1 confirmed. counts = {u:2, c:1}, winner=u. For c, n=1 >= 2? No. decisive=true. Records only u voters. OK.

27. **What if a confirmed skeptic has tripped budgets?**: As discussed, invokeSkeptic forces `verdictUnverifiable` on budget trip. So a confirmed verdict has no tripped budgets. OK.

28. **The tie case includes tripped budgets from all voters**: If a confirmed and a refuted and an unverifiable (with tripped budget) are tied, the TrippedBudgets would include the unverifiable's tripped budget. This is correct.

OK, I'm running out of issues. Let me consolidate.

### Issues to report:

1. **Stderr logging inconsistency** (LOW, maintainability)
2. **Test gap: Multi-vote Skeptic field not verified** (LOW, but this is more of a test concern)

Hmm, I don't think these are strong enough to report. Let me look once more for HIGH or MEDIUM issues.

Actually, wait. Let me look at this:

```go
priorByKey := map[FindingKey]VerificationResult{}
if prior, perr := ReadVerificationResults(reviewDir); perr != nil {
    fmt.Fprintf(os.Stderr, "atcr: verify: prior verification.json unreadable, skip-path metadata not carried forward: %v\n", perr)
} else {
    for _, r := range prior {
        priorByKey[FindingKey{File: r.File, Line: r.Line, Problem: r.Problem}] = r
    }
}
```

If `perr != nil` because the file is corrupt, the user gets a stderr message. But the run continues. The downstream consumers (3.3 scorecard, 10.x leaderboard) might still read the prior correctly... actually no, the prior is corrupt, so they can't. But the current run would still produce a new verification.json.

The issue is: if the prior is corrupt, the run proceeds with a fresh verification.json, but no historical data is preserved. This might be unexpected. The user might want to know that the prior is gone. The stderr message does inform them, but they might miss it.

This is a minor concern.

29. **Silent fallback on corrupt prior**: The user might not notice the stderr message and lose history. LOW severity, maintainability.

OK, I think these are the main issues. Let me look once more for any correctness bugs.

30. **`strings.TrimSpace` on Verdict could mask issues**: If the prior has whitespace in the verdict (e.g., from a malformed write), the trim normalizes it. This is defensive and probably good. Not a bug.

31. **`strings.EqualFold` is case-insensitive**: This is defensive and good. But what if the prior uses a different case (e.g., "Confirmed" vs "confirmed")? The match would succeed. This is intentional for robustness.

32. **`base.Skeptic` for the carry-forward path**: When the finding is skipped and we carry forward the prior, we keep `rec.Skeptic` from the on-disk block (`f.Verification.Skeptic`). But the prior might have a different Skeptic value. Should we use the prior's Skeptic? The current code uses the current's Skeptic, not the prior's. This is reasonable - the on-disk block is the "current truth" and the prior's rich metadata supplements it.

But wait, the prior's Skeptic might be a multi-skeptic list (all voters), while the on-disk block might be a single name (the lead). In that case, `rec.Skeptic` from the on-disk block might lose information. But this is the pre-existing behavior; the diff doesn't change it.

Actually, let me re-read. The code:
```go
rec := VerificationResult{
    File: f.File, Line: f.Line, Problem: f.Problem,
    Verdict:   f.Verification.Verdict,
    Skeptic:   f.Verification.Skeptic,
    Reasoning: f.Verification.Notes,
}
```

Uses `f.Verification.Skeptic`, not `prior.Skeptic`. The carry-forward only includes `Model`, `DurationMs`, `TrippedBudgets`. So `Skeptic` is from the on-disk findings.json block, not the prior. This is the design.

33. **Carry-forward of `Skeptic` from prior?**: The current code doesn't carry forward `Skeptic` from the prior. The comment in the diff says "Skeptic names every participating voter". If the on-disk block is just the lead, and the prior has all voters, we lose the all-voters info. But the diff doesn't address this. Is it a bug? The sprint plan AC4 says "carries `Model`/`DurationMs`/`TrippedBudgets` from the on-disk `VerificationResult` record". It doesn't mention `Skeptic`. So the diff matches the AC.

But the documentation comment in the diff says "Skeptic names every participating voter" - which is true for the rich record from `verifyFinding` but NOT necessarily for the carry-forward (which uses the on-disk block).

This is a documentation/code mismatch. The docstring might be misleading. LOW severity, maintainability.

34. **`ver.Skeptic` for ties**: The comment says "Tie (aggregateVerdicts emits unverifiable with Skeptic naming every voter)". I need to check `aggregateVerdicts` to see if this is true. But the test for ties only checks Model, not Skeptic. If `ver.Skeptic` is not all voters for a tie, the comment is wrong.

Hmm, this is a potential issue. Let me check if I can find `aggregateVerdicts` behavior.

Actually, I don't have the full code. I'll note this as a potential concern.

35. **Final thoughts**: I think the code is generally well-implemented. The main issues I see are:
   - Stderr logging inconsistency (LOW)
   - Documentation/code alignment for Skeptic field (LOW)
   - Test gap for Skeptic field on multi-vote (LOW)

None of these are HIGH or MEDIUM. Let me re-examine the diff one more time to make sure I'm not missing anything.

OK, looking at the changes once more:

`invoke.go`:
- Signature change: ✓
- Return tripped budgets: ✓
- Clean verdict returns nil for tripped: ✓
- Tests updated: ✓
- New tests for AC1: ✓

`pipeline.go`:
- Reads prior verification.json: ✓
- Skip path carries forward rich metadata with verdict match: ✓
- Early returns set Model="": ✓
- winningAttribution function: ✓
- Tests added: ✓

`emit_verification.go`:
- ReadVerificationResults: ✓
- Test added: ✓
- Comment updated: ✓

`pipeline_test.go`:
- byModelChat helper: ✓
- Tests for early returns, multi-vote, tie, skip carry-forward: ✓

`emit_test.go`:
- TestReadVerificationResults: ✓

`invoke_test.go`:
- Updated existing tests for new signature: ✓
- New tests for tripped budgets: ✓

`verify_e2e_test.go`:
- Updated for new signature: ✓

OK, I think I've covered everything. Let me write up the findings.

Actually, let me look at one more thing: the comment in the diff says "for a finding skipped on a later re-run it is carried forward unchanged, not recomputed." This is true for Model/DurationMs/TrippedBudgets. But what about Reasoning? The skip path uses `f.Verification.Notes` as Reasoning, which is the current's reasoning, not the prior's. The prior might have a different reasoning. But again, the design is to use the current's verdict/skeptic/reasoning from the on-disk block, and supplement with prior's rich metadata.

OK, the design is consistent. The carry-forward is for Model/DurationMs/TrippedBudgets only.

36. **Documentation says "Skeptic names every participating voter" but for skipped findings, Skeptic comes from on-disk block which might not list all voters**: This is the documentation/code mismatch I noted. LOW.

37. **Documentation comment is overly long but useful**: Maintainability, but not a bug.

Alright, I think the legitimate issues to report are:

A. `fmt.Fprintf(os.Stderr, ...)` in pipeline.go is inconsistent with the codebase's logger function pattern (e.g., `logSkepticFailure`). Should use the established logger for consistency.

B. The `Skeptic` field claim "names every participating voter" in the `VerificationResult` docstring is not always true: for skipped findings carried forward, `Skeptic` is taken from the on-disk findings.json block (which historically carried the lead skeptic only), not from the prior verification.json. This creates an inconsistency between the rich records and the skip-path records.

Actually wait, let me re-examine. The docstring says:
```
// Skeptic names every participating voter; Model names only the skeptics whose
// verdict produced the recorded outcome — a winners-only subset on a decisive vote,
// all participants on a tie, and "" when no skeptic executed.
```

This is the new docstring. The implementation:
- For verified-this-run findings (rich record): `ver.Skeptic` from aggregateVerdicts
- For skipped findings: `f.Verification.Skeptic` from on-disk block

If `aggregateVerdicts` returns all voters for ties and the lead for decisive, then:
- Tie: `ver.Skeptic` = all voters
- Decisive: `ver.Skeptic` = lead (single name)
- Skipped: `f.Verification.Skeptic` = single name (the lead from when it was verified)

So the docstring "Skeptic names every participating voter" is wrong for decisive cases (single name) and skipped cases (single name from on-disk block).

This is a documentation bug. The docstring is misleading.

38. **Documentation for `Skeptic` field is misleading**: The docstring says Skeptic names every participating voter, but for decisive votes and skip-path records, Skeptic is a single name. The actual behavior depends on the verdict path.

Hmm, this is a maintainability/correctness issue with documentation. MEDIUM maybe.

39. **For the carry-forward path, the prior's `Skeptic` is not used**: If the prior has multi-skeptic info and the current on-disk block doesn't, we lose information. This is by design per the AC, but inconsistent with the docstring.

OK let me just report the documentation issue and the logging issue.

Actually, let me also check this: in the test for `TestRunVerify_SkipPreservesPriorMetadata`, the assertion is:
```go
assert.Equal(t, "m-prior", vf.Findings[0].Model, "skip path must carry prior model")
```

The Model is "m-prior" from the prior. The Skeptic in the prior is "s1". The on-disk block has Skeptic "s1". So they match. Good.

But what if the prior Skeptic was "s1, s2, s3" (multi) and the on-disk block was "s1" (lead)? The carry-forward would use "s1" (from on-disk). The model would be "m-prior" (from prior). The user would see inconsistent Skeptic (s1) and Model (m-prior which is the joined models). This is confusing.

But this is an edge case that doesn't seem to be tested or addressed. Let me not flag it.

40. **OK I think I've spent enough time. Let me finalize my findings.**

Let me look one more time for any obvious issues.

Looking at the test for TwoConfirmOneRefute:
```go
harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
    return byModelChat{byModel: map[string]string{...}}, okDispatcher(), nil, nil
}
_, err := runVerify(context.Background(), dir, reg, Options{Thorough: true}, harness)
```

The harness factory returns a 4-tuple: (ChatCompleter, Dispatcher, cleanup, error). The 3rd return (cleanup func) is nil. The 4th return (error) is nil. This matches the signature of the harness factory.

OK.

41. **One potential issue: In the `byModelChat` helper, if `inv.Model` is not in the map, the returned string is "". The chat completer returns an empty content. The parser would mark this as `empty_response` or `malformed_output`. This is fine for tests that have all models in the map.**

42. **`harness` return values - cleanup is nil**: If `runVerify` calls cleanup, it would panic on nil. But the test doesn't use cleanup. OK.

43. **Final check on `ReadVerificationResults`**: The function reads `reviewDir/reconciled/verification.json`. The `reconciledSubdir` constant is "reconciled". The caller passes `reviewDir` (the review directory). The path is `<reviewDir>/reconciled/verification.json`. This is consistent with `WriteVerification` writing to the same path. Good.

44. **`json.Unmarshal` into `VerificationFile`**: I need to know the structure of `VerificationFile`. The diff shows:
```go
var vf VerificationFile
if err := json.Unmarshal(data, &vf); err != nil {
    return nil, fmt.Errorf("parsing verification.json: %w", err)
}
return vf.Findings, nil
```

So `VerificationFile` has a `Findings []VerificationResult` field. OK.

45. **What if `VerificationFile.Findings` is `nil` (not present in JSON)?**: `json.Unmarshal` would leave it as nil. The function returns nil. The caller treats it as "no prior". OK.

46. **What if `VerificationFile.Findings` is `[]` (empty array)?**: `json.Unmarshal` would set it to empty slice (non-nil). The function returns empty slice. The caller iterates and finds no entries. The `priorByKey` is empty. OK.

47. **What if the file is `null`?**: `json.Unmarshal` would set `vf` to zero value. `vf.Findings` is nil. The function returns nil. OK.

OK, I think the implementation is solid. Let me write up the findings.

Actually, I realized I should also look at potential issues in the test for the `byModelChat` Completer. The test uses `Complete` method. Let me check if this is correct.

Looking at `finalChat`:
```go
type finalChat string
func (f finalChat) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
```

And `byModelChat`:
```go
func (b byModelChat) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
    return b.byModel[inv.Model], nil
}
```

Both implement `Complete`. The `Chat` method is also implemented on `byModelChat`. The `fanout.ChatCompleter` interface requires both `Complete` and `Chat`. OK.

48. **Wait, what if `fanout.ChatCompleter` is the same as `llmclient.ChatCompleter`?**: The test uses `byModelChat` as the chat completer. The `Chat` method signature is `(context.Context, llmclient.Invocation, []llmclient.Message, []llmclient.ToolDef) (*llmclient.ChatResponse, error)`. This matches `llmclient.ChatCompleter.Chat`. OK.

49. **`byModelChat` is a value receiver**: It receives a copy of the struct. The `byModel` map is a reference, so modifications would be visible. But the methods don't modify the map. OK.

50. **OK I'm done analyzing. Let me write the findings.**

Final findings:

1. **LOW**: Stderr logging inconsistency - `fmt.Fprintf(os.Stderr, ...)` in pipeline.go is inconsistent with the codebase's `logSkepticFailure` pattern. Should use a structured logger.

2. **LOW**: Documentation/code mismatch - the new docstring for `VerificationResult.Skeptic` says it names every participating voter, but for decisive votes and skip-path records, Skeptic is a single name (the lead or from the on-disk block). The docstring is misleading.

3. **LOW/MEDIUM**: Possible issue with `winningAttribution` when all voters' verdicts are equal in count but not all the same. Actually I think this is handled correctly.

4. **MEDIUM?**: There's a potential issue with the `Skeptic` field for the carry-forward path. The prior's `Skeptic` is not used; the on-disk block's `Skeptic` is used. If the on-disk block was a lead-only representation, the rich metadata (Model) from the prior would be joined from multiple voters, but Skeptic would be single. This could be confusing for downstream consumers. But this is the design per the AC.

Actually, let me look at the docstring comment again:
```
// Skeptic names every participating voter; Model names only the skeptics whose
// verdict produced the recorded outcome — a winners-only subset on a decisive vote,
// all participants on a tie, and "" when no skeptic executed. So for a multi-vote
// run Model may list fewer entries than Skeptic by design (see winningAttribution).
```

The "all participants on a tie" refers to the Model field. The Skeptic is described as "every participating voter". For ties, this should be all voters. For decisive, this could be all voters (per joinSkeptics) or the lead (per aggregateVerdicts). The comment doesn't distinguish.

Let me check the pre-existing behavior. The diff removes:
```go
models := make([]string, 0, len(skeptics))
for _, sk := range skeptics {
    models = append(models, sk.Config.Model)
}
base.Model = strings.Join(models, ", ")
```

But doesn't change the `base.Skeptic = ver.Skeptic` line. So `ver.Skeptic` is whatever `aggregateVerdicts` returns. I don't know what that is.

The pre-existing test `TestRunVerify_ThoroughMultiSkepticRecordsAllModels` checks that Model contains "m-skep" but doesn't check Skeptic. So the original test doesn't constrain Skeptic behavior.

OK, the docstring is making a claim about Skeptic that might not be accurate (depends on aggregateVerdicts). This is a documentation concern.

Alright, let me finalize.

Actually, I realize I might be missing something. Let me re-examine the sprint plan:

The sprint plan says "Skeptic names every participating voter" is the design (per joinSkeptics). So the docstring reflects the design. The implementation relies on `aggregateVerdicts` to do this. If `aggregateVerdicts` doesn't, the implementation diverges from the design.

I can't verify `aggregateVerdicts` behavior from the diff. So I can only flag this as a potential issue if I'm uncertain.

OK let me just report the logging inconsistency and the documentation ambiguity, both as LOW.

Wait, let me also think about edge cases in `winningAttribution` once more.

`winningAttribution` is called with `ver.Verdict` as `winner`. If `ver.Verdict` is "unverifiable" and the unverifiable votes are the majority (e.g., 2 unverifiable, 1 confirmed), then:
- counts = {u:2, c:1}
- For c: 1 >= 2? No. decisive=true.
- Records only u voters (including their tripped budgets).

This means a 2-1 majority of unverifiable (e.g., both tripped a budget) would record those two models and their budgets. The confirmed voter is excluded. Is this correct?

Per the AC: "Winning" skeptics = those whose per-skeptic verdict equals the aggregated (majority) verdict. So yes, only the unverifiable voters are recorded. The confirmed voter is a loser. OK.

But wait, the diff comment says "Tie (aggregateVerdicts emits unverifiable with Skeptic naming every voter, e.g. 1 confirmed + 1 refuted, or 1+1+1): no skeptic "won", so record every participant's model". So the tie case is specifically 1+1 or 1+1+1, not 2+1.

If 2 unverifiable + 1 confirmed: aggregateVerdicts emits unverifiable (2>1). This is decisive. winningAttribution records only u voters. OK.

If 2 unverifiable + 2 confirmed (4 skeptics): aggregateVerdicts emits unverifiable (tie resolved to unverifiable). counts = {u:2, c:2}. For c: 2 >= 2? Yes. decisive=false. Records all voters. OK.

If 3 unverifiable + 1 confirmed: aggregateVerdicts emits unverifiable (3>1). counts = {u:3, c:1}. For c: 1 >= 3? No. decisive=true. Records only u voters. OK.

If 2 unverifiable + 1 confirmed + 1 refuted: aggregateVerdicts emits unverifiable (2>1). counts = {u:2, c:1, r:1}. For c: 1 >= 2? No. For r: 1 >= 2? No. decisive=true. Records only u voters. OK.

OK, the logic is correct.

51. **One more potential issue: the test for ThreeWayTie uses `m-s2, m-s3, m-skep` and the assertion is that all three are in the Model field. But the test doesn't verify that the Skeptic field also lists all three. If Skeptic is only the unverifiable voter (m-skep), then the inconsistency would be: Skeptic=skep, Model=m-s2, m-s3, m-skep. Confusing.

This is a test gap, not a code bug. The test should also assert the Skeptic field. But the sprint plan didn't specify what Skeptic should be in the tie case (it just says Model lists all participants).

52. **OK I think the diff is mostly fine. Let me write up the findings now.**

Findings to report:
1. Stderr logging inconsistency (LOW, maintainability)
2. Documentation ambiguity for Skeptic field (LOW, maintainability)

Actually, I want to also note: the test for `byModelChat` doesn't handle the case where `inv.Model` is not in the map. It returns "", which the parser would mark as empty/malformed. If a skeptic's model isn't in the byModel map, the test would fail unexpectedly. But the test author is aware of this (the test maps all participating models).

53. **One more thing: `modelMap` should be initialized in `byModelChat` to avoid nil map panics**: If `b.byModel` is nil, then `b.byModel[inv.Model]` would return the zero value (empty string), not panic. Reading from a nil map is safe in Go. OK.

Alright, finalizing.

Let me also re-look at:

```go
priorByKey := map[FindingKey]VerificationResult{}
if prior, perr := ReadVerificationResults(reviewDir); perr != nil {
    fmt.Fprintf(os.Stderr, "atcr: verify: prior verification.json unreadable, skip-path metadata not carried forward: %v\n", perr)
} else {
    for _, r := range prior {
        priorByKey[FindingKey{File: r.File, Line: r.Line, Problem: r.Problem}] = r
    }
}
```

Wait, I want to verify the scope. The sprint plan says AC4: "the skip-already-verified rebuild path (pipeline.go:199) carries Model/DurationMs/TrippedBudgets from the on-disk VerificationResult record rather than synthesizing a lossy record". The diff implements this. The use of `ReadVerificationResults` is appropriate.

But the use of `FindingKey` requires that the prior's `File`, `Line`, `Problem` match the current finding's. What if the prior has different `File`/`Line`/`Problem` (e.g., due to refactoring)? Then the lookup would fail. The finding would not get carry-forward. This is a TOCTOU concern, but not a real bug.

54. **The skip-path carry-forward is keyed on `FindingKey{File, Line, Problem}`**: If the prior's keys don't match the current's, the carry-forward is lost. This is a potential issue if the codebase is unstable (refactoring changes file paths). But this is by design per the AC.

55. **OK, finalizing the report.**

Wait, let me also check this: the test for `TestRunVerify_SkipPreservesPriorMetadata` expects:
```go
assert.Equal(t, []string{"max_turns"}, vf.Findings[0].TrippedBudgets, "skip path must carry prior tripped budgets")
```

The test compares `[]string{"max_turns"}` (a non-nil slice with one element) against `vf.Findings[0].TrippedBudgets`. If the carry-forward preserves the nil-vs-empty distinction, the test might fail. But the prior was created with `TrippedBudgets: []string{"max_turns"}` (non-nil). After JSON marshal/unmarshal via WriteVerification, the slice is preserved. Then the carry-forward copies it as-is. The assertion should pass.

But what if the JSON round-trip changes nil to empty or vice versa? `json.Marshal` on `[]string{"x"}` produces `["x"]`. `json.Unmarshal` of `["x"]` into `[]string` produces `[]string{"x"}`. So the round-trip preserves non-nil. OK.

56. **JSON round-trip preserves nil-vs-empty for `[]string`**: The test should pass. OK.

Alright, I'm finalizing. The issues I'll report are:

1. **LOW** - Stderr logging inconsistency (maintainability)
2. **LOW** - Documentation ambiguity for Skeptic field (maintainability)

These are minor. Let me see if I can find anything stronger.

Hmm, I keep going back to the same minor issues. Let me commit to a final report.

Actually, one more thought: the comment for `winningAttribution` mentions the tie case "1 confirmed + 1 refuted, or 1+1+1". This implies that 1+1+1 (3-way tie) is the same as 1+1 (2-way tie). The test for ThreeWayTie tests 1+1+1. But what about 1+1? Is there a test for it? The tests cover:
- 2 confirmed + 1 refuted (decisive)
- 1+1+1 tie (unverifiable)

But 1+1 tie (confirmed + refuted) is not tested. This is a minor test gap. LOW.

OK, I'll include this as a test gap.

Final findings:

1. **LOW** - Stderr logging inconsistency in `pipeline.go` (maintainability)
2. **LOW** - Documentation ambiguity for `Skeptic` field in `VerificationResult` docstring (maintainability)
3. **LOW** - Test gap: 1+1 tie (confirmed + refuted) not tested (test coverage)

Hmm, these are all LOW. Let me see if there are any stronger issues.

Let me look at the diff one more time, focusing on the most complex part: `winningAttribution`.

```go
func winningAttribution(skeptics []Skeptic, perSkeptic []*reconcile.Verification, perTripped [][]string, winner string) (string, []string) {
    counts := map[string]int{}
    for _, v := range perSkeptic {
        if v != nil {
            counts[v.Verdict]++
        }
    }
    decisive := true
    for verdict, n := range counts {
        if verdict != winner && n >= counts[winner] {
            decisive = false
            break
        }
    }

    var models, budgets []string
    seenModel := map[string]bool{}
    seenBudget := map[string]bool{}
    for i, v := range perSkeptic {
        if v == nil {
            continue
        }
        if decisive && v.Verdict != winner {
            continue
        }
        if m := skeptics[i].Config.Model; m != "" && !seenModel[m] {
            seenModel[m] = true
            models = append(models, m)
        }
        for _, b := range perTripped[i] {
            if !seenBudget[b] {
                seenBudget[b] = true
                budgets = append(budgets, b)
            }
        }
    }
    return strings.Join(models, ", "), budgets
}
```

Wait, I see a potential issue. When `decisive=false` (tie), the function records all voters' models. But the AC says "joined models when multiple voters agree". On a tie (no agreement), the joined models might be misleading.

Actually, the docstring says "no skeptic "won", so record every participant's model so Model stays consistent with Skeptic rather than misattributing the outcome to the lone unverifiable voter". OK, this is intentional. On a tie to unverifiable, all voters' models are recorded.

But what if Skeptic in the tie case is just the unverifiable voter (e.g., "skep")? Then Model=m-s2, m-s3, m-skep but Skeptic=skep. Inconsistent.

Hmm, this is the documentation issue I noted. The code does what the docstring says, but the docstring might not match `aggregateVerdicts` behavior.

57. **Potential inconsistency between `winningAttribution` Model and `ver.Skeptic`**: The docstring says Model should match Skeptic (both all participants on tie), but if `aggregateVerdicts` returns Skeptic as a single name on tie, the Model and Skeptic would diverge.

I think this is worth flagging as MEDIUM if `aggregateVerdicts` doesn't do this. But I can't verify without seeing `aggregateVerdicts`. So I'll note it as a concern.

OK, finalizing.

Let me also check: when `decisive=true` and a winner has only 1 vote (and others have 0 of the same verdict), the function records only the winner. The dedup of `seenModel` ensures the same model isn't added twice. But if the winner has 1 vote, the function adds the winner's model once. OK.

What if two voters have the same model and both are winners? E.g., 2 confirmed both using "m-x". `seenModel["m-x"]` is set on the first iteration, the second iteration skips. So models = ["m-x"]. OK.

What if two voters have the same model and only one is the winner? E.g., 2 confirmed ("m-x", "m-y"), 1 refuted ("m-x"). Decisive=true. Records only confirmed voters. seenModel["m-x"]=true (first iteration), seenModel["m-y"]=true (second). Third is refuted, skipped. Models = ["m-x", "m-y"]. OK.

What if a confirmed voter has tripped budgets? As discussed, invokeSkeptic forces unverifiable on budget trip. So this doesn't happen.

OK, I'm done analyzing. Let me write the findings.

Final findings:

1. **LOW** - `fmt.Fprintf(os.Stderr, ...)` in `pipeline.go` for the corrupt-prior case is inconsistent with the codebase's logger pattern (`logSkepticFailure` function). Should use the established logger.

2. **LOW** - The new docstring for `VerificationResult.Skeptic` claims it "names every participating voter" universally, but for decisive votes and skip-path records, the Skeptic field is a single name (from `ver.Skeptic` for decisive, or `f.Verification.Skeptic` for skipped). The Model field is documented correctly (winners-only on decisive, all on tie, "" if no execution), but Skeptic's behavior is more nuanced.

3. **LOW** - Test gap: 1+1 tie (1 confirmed + 1 refuted) is not covered by tests, only 1+1+1 tie is. The docstring mentions both as tie cases.

These are all LOW. Let me see if there are any I missed.

Actually, let me re-examine one more potential issue:

58. **The test `TestRunVerify_SkipDropsMismatchedPriorMetadata` uses `Options{}` (no Thorough) but `skepticRegistry()`**: If the registry has multiple skeptics and `Options{}` only selects one, the harness would only be called for that one skeptic. But the finding is skipped, so the harness isn't called at all. OK.

59. **The test `TestRunVerify_ThreeWayTieRecordsAllParticipantModels` checks Model but not Skeptic**: The test gap I mentioned. LOW.

60. **The test `TestRunVerify_WinningModelAttribution_TwoConfirmOneRefute` checks Model but not Skeptic**: Same gap. LOW.

OK, these are all minor. Let me write the final report.

Wait, I should also consider: the diff modifies the public-facing `VerificationResult` struct's comment. The comment is documentation. If the comment is wrong, future maintainers might be confused. This is a real concern.

Let me also check: does the comment match the implementation for the rich record path (verified this run)?

For rich record path:
- `base.Skeptic = ver.Skeptic` (from aggregateVerdicts)
- `base.Model = ...` (from winningAttribution, which derives from skeptic models)

If `ver.Skeptic` is all voters, then the comment "Skeptic names every participating voter" is correct.
If `ver.Skeptic` is the lead only, then the comment is wrong for decisive cases.

I don't know what `aggregateVerdicts` does. I'll flag this as a documentation concern.

For skip path:
- `rec.Skeptic = f.Verification.Skeptic` (from on-disk block)
- `rec.Model = prior.Model` (from prior, joined winner models)

The on-disk block's `f.Verification.Skeptic` is whatever was written when the finding was verified. If the on-disk block has a single Skeptic name (which is typical), then Skeptic is single. The prior's Model might be joined from multiple voters. Inconsistency.

The comment "Skeptic names every participating voter" is wrong for skip-path records.

This is a real documentation issue. MEDIUM.

OK, finalizing.

Actually, let me also note one more issue:

61. **`base.Skeptic` on the `ver` return value**: In `verifyFinding`, the function returns `ver, base`. `ver` is from `aggregateVerdicts` and has its own `Skeptic` field. `base.Skeptic = ver.Skeptic`. So the function's return value and the base record have the same Skeptic. OK.

But on early-return paths, `ver` is constructed with `Skeptic: ""` (zero value). `base.Skeptic` is not set (zero value). Both are "". OK.

62. **The `ver` returned to the caller might have a different Skeptic than the one written to verification.json**: The caller receives `ver` (the in-memory reconcile.Verification) and uses it to update findings.json. The verification.json is built from `base`. So `ver.Skeptic` and `base.Skeptic` should match. They do (both set to `ver.Skeptic` in the verified path). OK.

63. **Skip path doesn't set `base.Skeptic` separately**: Wait, the skip path is in `runVerify`, not `verifyFinding`. The skip path doesn't call `verifyFinding`; it synthesizes the record directly. So `base.Skeptic` doesn't apply here. The synthesized record is `rec.Skeptic = f.Verification.Skeptic`. OK.

64. **Inconsistency between `ver` and `base` on early returns**: 
```go
v := &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "no_eligible_skeptic"}
base.Verdict, base.Reasoning = v.Verdict, v.Notes
base.Model = ""
return v, base
```

`v.Skeptic` is "" (zero value). `base.Skeptic` is "" (zero value, not set). OK, consistent.

65. **`ver` on early return doesn't have Skeptic set**: Could this be an issue? The caller might use `ver.Skeptic` to update findings.json. If the caller writes "" as Skeptic to findings.json, that could overwrite a previously set Skeptic. But this is a pre-existing concern.

66. **Final findings**:
   1. Stderr logging inconsistency (LOW, maintainability)
   2. Documentation ambiguity for Skeptic field (MEDIUM, maintainability - the docstring claims behavior that may not hold for all paths)
   3. Test gaps for Skeptic field (LOW, test coverage)
   4. Test gap for 1+1 tie (LOW, test coverage)

OK, let me write the final report.

Hmm, let me also consider if there are any race conditions or concurrency issues.

67. **`runVerify` is called sequentially**: The harness factory is called per-finding. The prior verification.json is read once. No concurrency issues. OK.

68. **`winningAttribution` is called per-finding**: Sequential. No concurrency issues. OK.

69. **`ReadVerificationResults` is called once per run**: OK.

70. **No new shared mutable state introduced**: OK.

Alright, I'm done. Let me write the final report.

Actually, one more thing. Let me look at the test for ThreeWayTie:

```go
harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
    return byModelChat{byModel: map[string]string{
        "m-s2":   `{"verdict":"confirmed","reasoning":"yes"}`,
        "m-s3":   `{"verdict":"refuted","reasoning":"no"}`,
        "m-skep": `{"verdict":"unverifiable","reasoning":"dunno"}`,
    }}, okDispatcher(), nil, nil
}
```

The verdict for m-skep is "unverifiable". The parseVerdict would set `v.Verdict = "unverifiable"`, `v.Notes = "dunno"`. This is a CLEAN unverifiable (not a budget trip). So perTripped for m-skep is nil. OK.

The test asserts all three models are in the Model field. The test doesn't assert TrippedBudgets. The docstring says "a confirmed/refuted winner correctly carries an empty TrippedBudgets" but doesn't explicitly say what the tie case carries. In the test, no budgets are tripped, so TrippedBudgets would be empty. The test doesn't check this.

If a tie case had a budget-tripped unverifiable voter, TrippedBudgets would include the budget. The test doesn't cover this. Minor gap.

71. **Test gap: tie case with budget-tripped unverifiable voter not tested**: LOW.

OK, I'm done. Let me write the final report.

Alright, finalizing with these findings:

1. **LOW** - `fmt.Fprintf(os.Stderr, ...)` in `pipeline.go` for the corrupt-prior case is inconsistent with the codebase's logger pattern. The codebase has `logSkepticFailure` as a function-based logger. Direct stderr write bypasses structured logging.

2. **LOW** - Documentation ambiguity in `VerificationResult` docstring: The docstring claims "Skeptic names every participating voter", but for the carry-forward (skip) path, the Skeptic is taken from `f.Verification.Skeptic` (the on-disk block), which may be a single name (e.g., the lead skeptic) rather than all participants. The Model field is documented correctly (winners-only on decisive, all on tie, "" on no execution), but the Skeptic field's behavior is not as uniformly "all participants".

3. **LOW** - Test gap: The 1+1 tie case (1 confirmed + 1 refuted) is not covered by tests, only the 1+1+1 tie (3-way) is tested. Both are mentioned in the docstring as tie cases.

Hmm, I think I'll consolidate to just the most impactful issues.

Let me also look at one more potential issue with how the `decisive` calculation works:

If `ver.Verdict` (winner) is "confirmed" and there are 2 confirmed and 1 refuted, then:
- counts = {c:2, r:1}
- For r: 1 >= 2? No. decisive=true.
- Records only c voters. ✓

If `ver.Verdict` is "unverifiable" and there are 1 unverifiable, 1 confirmed, 1 refuted (3-way tie to unverifiable):
- counts = {u:1, c:1, r:1}
- For c: 1 >= 1? Yes. decisive=false.
- Records all. ✓

If `ver.Verdict` is "unverifiable" and there are 2 unverifiable, 1 confirmed (decisive unverifiable):
- counts = {u:2, c:1}
- For c: 1 >= 2? No. decisive=true.
- Records only u voters. ✓

OK, the logic is correct.

Wait, one more thing. In the diff:

```go
for verdict, n := range counts {
    if verdict != winner && n >= counts[winner] {
        decisive = false
        break
    }
}
```

What if `counts[winner]` is 0? E.g., winner is "" or winner is a verdict not in counts. Then `n >= 0` is always true (for n > 0). decisive=false. Records all.

What if counts is empty (all perSkeptic are nil)? Then the loop doesn't execute, decisive=true. The main loop then has all nil entries, returns "", nil. OK.

What if `counts[winner]` is 0 and `counts` is non-empty? E.g., counts = {c:2, r:1} and winner="". Then for c: 2 >= 0? Yes. decisive=false. Records all.

This is the right behavior for "winner is not in the actual results".

OK, the logic is robust. Let me write the final report.

Actually, I want to also flag one more issue:

72. **The `decisive` check doesn't handle the case where `winner` is the empty string correctly**: If `ver.Verdict` is "", the function falls into the "no winner" case, recording all voters. But the AC implies that `ver.Verdict` should be one of the canonical verdicts (confirmed, refuted, unverifiable). If `aggregateVerdicts` ever returns "", the function would record all voters, which might not be the intended behavior. Defensive concern.

OK, this is a minor defensive concern. I'll note it.

Alright, FINAL report:

1. **LOW** - Stderr logging inconsistency
2. **LOW** - Documentation ambiguity for Skeptic field
3. **LOW** - Test gap for 1+1 tie case
4. **LOW** - Defensive concern: empty winner would record all voters (not a bug, but worth noting)

Let me prioritize the most impactful:

The most important one is #2 (documentation), since it could mislead future maintainers. The rest are minor.

OK let me write the final report.

Actually, you know what, let me try one more pass looking for HIGH or MEDIUM issues.

Looking at the test for TwoConfirmOneRefute:
```go
harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
    return byModelChat{byModel: map[string]string{...}}, okDispatcher(), nil, nil
}
_, err := runVerify(context.Background(), dir, reg, Options{Thorough: true}, harness)
```

The `reg` is `skepticRegistry()` with s2 and s3 added. The harness returns a `byModelChat` with three entries: m-s2, m-s3, m-skep. So the test relies on s1 NOT being a skeptic in the registry.

If `skepticRegistry()` returns a registry where s1 is a skeptic, then s1 would be selected for the run, and the harness would be called with `inv.Model` being s1's model. If s1's model is not in `byModelChat`, the chat returns "". The parseVerdict would mark it as `empty_response` or `malformed_output`, and the verdict would be `unverifiable`. The test result would be unpredictable.

This is a test design concern. The test author needs to know the registry contents. If they assumed s1 is not a skeptic, the test could be flaky.

But this is a test concern, not a code concern. LOW.

OK, I think I've been thorough. Let me write the final report.

Final findings (consolidated):

1. **LOW** - Documentation ambiguity for `Skeptic` field in `VerificationResult` docstring (maintainability)
2. **LOW** - Stderr logging inconsistency in `pipeline.go` (maintainability)
3. **LOW** - Test gap: 1+1 tie case not covered (test coverage)

Hmm, these are all LOW. The diff is generally well-implemented. The main concerns are documentation and test coverage.

Let me write the final report.

Actually, one more thing to consider. The test for `TestReadVerificationResults` does:
```go
got, err := ReadVerificationResults(t.TempDir())
require.NoError(t, err)
assert.Nil(t, got)
```

Then later:
```go
got, err = ReadVerificationResults(dir)
require.NoError(t, err)
require.Len(t, got, 1)
assert.Equal(t, want[0], got[0])
```

The `assert.Equal(t, want[0], got[0])` uses `Equal` for deep comparison. If `want[0]` has `TrippedBudgets: []string{"timeout_secs"}` and `got[0]` has the same (after round-trip), the comparison should pass.

But wait, the `WriteVerification` call in between might not write the same structure. Let me check.

The `want[0]`:
```go
VerificationResult{
    File: "a.go", Line: 7, Problem: "boom", Verdict: "confirmed", Skeptic: "s1",
    Model: "m-x", Reasoning: "ok", DurationMs: 99, TrippedBudgets: []string{"timeout_secs"},
}
```

`WriteVerification` writes this to JSON via `json.Marshal`. `ReadVerificationResults` reads it back via `json.Unmarshal`. The round-trip should preserve all fields. The test should pass.

But wait, what about the `Notes` field? The struct has a `Notes` field (per the diff). The test uses `Reasoning: "ok"`. After round-trip, `Notes` would be... hmm, the test doesn't check Notes. But the struct might have multiple text fields (Reasoning, Notes). Let me check.

Looking at the `VerificationResult` struct in the diff:
```go
type VerificationResult struct {
    File           string   `json:"file"`
    Line           int      `json:"line"`
    Problem        string   `json:"problem"`
    Verdict        string   `json:"verdict"`
    Skeptic        string   `json:"skeptic"`
    Model          string   `json:"model"`
    Reasoning      string   `json:"reasoning"`
    Notes          string   `json:"notes,omitempty"`
    DurationMs     int      `json:"duration_ms"`
    TrippedBudgets []string `json:"tripped_budgets,omitempty"`
}
```

The test sets `Reasoning: "ok"` and `Notes: ""` (zero). After round-trip, `Notes` would be "". The test compares the whole struct, so Notes should be "" on both sides. OK.

What about the field `Notes` vs `Reasoning`? The diff has both. The test sets only `Reasoning`. The `assert.Equal` would compare both, and if both are "", it passes.

OK, the test should work. Good.

73. **Round-trip test for `ReadVerificationResults`**: The test uses `assert.Equal` for full struct comparison. This requires all fields to round-trip correctly. The test should pass for a well-implemented JSON round-trip. OK.

74. **OK final report time.**

Alright, the final findings are:

1. **LOW** - Stderr logging inconsistency in `pipeline.go` 
2. **LOW** - Documentation ambiguity for `Skeptic` field
3. **LOW** - Test gap for 1+1 tie case (and Skeptic field assertions on multi-vote)

Let me write the report.

Actually let me look at one more thing - what about the `Notes` field in the early-return paths?

```go
if len(skeptics) == 0 {
    v := &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "no_eligible_skeptic"}
    base.Verdict, base.Reasoning = v.Verdict, v.Notes
    base.Model = ""
    return v, base
}
```

`v.Notes = "no_eligible_skeptic"`. `base.Reasoning = "no_eligible_skeptic"`. OK.

The reconcile.Verification has `Notes` field. The VerificationResult has `Reasoning` field. The diff maps Notes → Reasoning. OK.

75. **Inconsistent field name `Notes` vs `Reasoning`**: The diff uses `Reasoning` in `VerificationResult` and `Notes` in `reconcile.Verification`. The mapping is `base.Reasoning = v.Notes`. This is intentional. Not a bug.

OK, truly final report time.

Let me also think about the inconsistency between the test for `TestVerifyFinding_EarlyReturnsRecordEmptyModel` and the test for `TestRunVerify_WinningModelAttribution_TwoConfirmOneRefute`. The first test uses `finalChat("{}")` (returns empty content), the second uses `byModelChat` (per-model). Both are valid test setups. OK.

76. **Test consistency**: Tests use different chat completers as needed. OK.

Alright, FINAL report:

1. **LOW** - Stderr logging inconsistency
2. **LOW** - Documentation ambiguity for `Skeptic` field
3. **LOW** - Test gap for 1+1 tie case

Done.

Hmm, let me also double-check one more time that the diff is consistent with the ACs:

AC1: `invokeSkeptic` returns `trippedBudgets []string` separately. ✓
AC2: Multi-skeptic `VerificationResult.Model` reflects winning skeptic's model. ✓
AC3: `VerificationResult.Model` is empty on `no_eligible_skeptic` and `tool_harness_unavailable`. ✓ (set to "")
AC4: Skip-already-verified path carries forward. ✓
AC5: All existing pipeline tests pass; new tests cover the cases. ✓

All ACs are met. The implementation is correct.

OK, my final findings are the LOW issues I've identified. Let me write the report.

Wait, I want to look at one more subtle thing. The comment for the `VerificationResult` struct says:

```
// Skeptic names every participating voter; Model names only the skeptics whose
// verdict produced the recorded outcome — a winners-only subset on a decisive vote,
// all participants on a tie, and "" when no skeptic executed.
```

For the `no_eligible_skeptic` and `tool_harness_unavailable` paths, the code sets `base.Model = ""` but does NOT explicitly set `base.Skeptic`. So `base.Skeptic` is the zero value ("").

The comment says "Skeptic names every participating voter" - but for early-return paths, there are no participating voters. So Skeptic is "". OK, that matches the comment's intent.

For Model: "" when no skeptic executed. Matches.

For tie case: Model names all participants. But Skeptic - what does Skeptic name in a tie case? Per the comment, "every participating voter". So Skeptic is all voters. Model is all voters. OK.

For decisive case: Model names only winners. But Skeptic - "every participating voter" or just the winner(s)? The comment says "every participating voter" universally. So Skeptic is all voters (including losers). Model is winners only. This is the design (per joinSkeptics naming).

OK, the documentation is internally consistent (assuming joinSkeptics names all voters). The implementation matches if `aggregateVerdicts` and `joinSkeptics` do the right thing.

77. **Documentation comment interpretation**: The comment "Skeptic names every participating voter" assumes that `aggregateVerdicts` returns Skeptic as all voters (per `joinSkeptics`). This is the design, and the implementation should match. If the implementation diverges, the documentation is wrong.

78. **The `aggregateVerdicts` function is the key**: Without seeing it, I can't verify the documentation matches the implementation. This is a verification concern.

OK, I think the documentation is the only real issue. The implementation is otherwise solid.

Final findings:

1. **LOW** - Stderr logging inconsistency in `pipeline.go` (use codebase logger instead of direct stderr)
2. **LOW** - Documentation comment for `VerificationResult.Skeptic` claims it "names every participating voter" universally, but this depends on `aggregateVerdicts` behavior. The docstring could be more precise about when Skeptic names all voters vs. the lead.

I'll just include the most impactful ones.

Alright, let me write the final report.
</think>

LOW|internal/verify/pipeline.go:228|Direct fmt.Fprintf to stderr bypasses codebase logger|Use logSkepticFailure-style logger or a structured log helper for consistency|maintainability|5|fmt.Fprintf(os.Stderr, ...) inconsistent with logSkepticFailure pattern used elsewhere|claude
LOW|internal/verify/emit_verification.go:25|Docstring claim "Skeptic names every participating voter" is ambiguous|Clarify Skeptic: all voters on tie, lead on decisive, single name from on-disk block on carry-forward|maintainability|5|Comment says Skeptic=every voter but for decisive/skip paths it may be a single name; only Model's behavior is fully documented|claude
LOW|internal/verify/pipeline.go:227|priorByKey always built even when no findings will be skipped|Guard with a defer-until-needed check or document as intentional|performance|3|Reads verification.json on every run even when no skip-path metadata is needed|claude
LOW|internal/verify/pipeline.go:435|winningAttribution with empty winner records all voters silently|Guard against empty winner or document as intentional fallback|error-handling|5|ver.Verdict=="" would cause decisive=false and dump every model with no attribution signal|claude
LOW|internal/verify/pipeline_test.go:362|1+1 tie case (1 confirmed + 1 refuted) not covered by tests|Add a test mirroring ThreeWayTie but with 2 skeptics, not 3|maintainability|10|TestRunVerify_ThreeWayTieRecordsAllParticipantModels covers 1+1+1 but not the 1+1 case the docstring also cites|claude
LOW|internal/verify/pipeline_test.go:362|Multi-vote tests assert Model but not Skeptic field|Add Skeptic assertions to lock the joinSkeptics contract the docstring promises|maintainability|10|Tests verify winning-attribution Model but never check Skeptic, so a regression in Skeptic emission would pass tests|claude