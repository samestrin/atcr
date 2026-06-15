<think>
Let me carefully review this diff for the Epic 2.2 sprint plan. The scope is well-defined:

IN SCOPE:
- Add `scope` / `min_severity` / `max_findings` to `AgentConfig` + load-time validation
- Soft scope prompt injection into persona prompts
- Fan-out per-source enforcement of `min_severity` (drop below floor) and `max_findings` (severity-sorted truncate) with warning logs
- `docs/registry.md` field docs + example block
- Tests for all of the above

OUT OF SCOPE:
- AC1/AC6 reviewer-name stamping (already done)
- Reconcile-stage changes
- In-repo `registry.yaml`
- Hardening of external llm-tools skill files
- Hard category drop for `scope`

Let me look at the changes carefully:

1. `docs/registry.md` - Documents the new fields, includes a `bruce-backup` example with constraints. This is in scope.

2. `internal/fanout/artifacts.go` - Updates `findingsFor` to call `enforceConstraints` after stamping reviewer. In scope.

3. `internal/fanout/engine.go` - Adds `MinSeverity` and `MaxFindings` to both `Agent` and `Result` structs, threads them through. In scope.

4. `internal/fanout/loop.go` - Threads the constraints into the tool loop's `Result`. In scope.

5. `internal/fanout/postprocess.go` (new) - Implements `enforceConstraints` with severity ranking and truncation. In scope.

6. `internal/fanout/postprocess_test.go` (new) - Tests for `enforceConstraints` and `findingsFor`. In scope.

7. `internal/fanout/review.go` - Threads `MinSeverity` and `MaxFindings` from `AgentConfig` to `Agent`, applies scope to prompt. Also sets fallback agent to inherit from primary. In scope.

8. `internal/fanout/scope_inject_test.go` (new) - Tests for scope injection. In scope.

9. `internal/payload/scope.go` - New `ScopeFocus` function for soft prompt injection. In scope.

10. `internal/payload/scope_focus_test.go` (new) - Tests for `ScopeFocus`. In scope.

11. `internal/registry/config.go` - Adds `Scope`, `MinSeverity`, `MaxFindings` to `AgentConfig` with YAML tags. Adds validation and normalization. In scope.

12. `internal/registry/config_review_constraints_test.go` (new) - Tests for the new fields. In scope.

Now let me look for real issues:

**Issue 1: `enforceConstraints` reorders the underlying array even when no truncation occurs**

Looking at postprocess.go:
```go
// 1. Severity floor.
if floor := strings.ToUpper(strings.TrimSpace(minSeverity)); floor != "" {
    min := severityRank[floor]
    kept := findings[:0]
    dropped := 0
    for _, f := range findings {
        if severityRank[strings.ToUpper(f.Severity)] >= min {
            kept = append(kept, f)
        } else {
            dropped++
        }
    }
    findings = kept
```

The `findings[:0]` reuses the backing array, which is fine and preserves order. But wait — this is a known Go pattern (filter in place), but the doc says "The input slice may be reordered in place." which is okay.

**Issue 2: Variable shadowing of `min`**

```go
if floor := strings.ToUpper(strings.TrimSpace(minSeverity)); floor != "" {
    min := severityRank[floor]
    kept := findings[:0]
    dropped := 0
    for _, f := range findings {
        if severityRank[strings.ToUpper(f.Severity)] >= min {
            kept = append(kept, f)
        } else {
            dropped++
        }
    }
```

`min` is a built-in function name in Go (used for `min(a, b)` since Go 1.21). Using `min` as a variable name shadows the built-in within the scope. This is a maintainability concern.

**Issue 3: Sort stability concern**

The sort uses `sort.SliceStable` which is good for stable sorting by severity. But when multiple findings have the same severity, their emission order is preserved. The cap truncates the array. Looks correct.

**Issue 4: `findingsFor` only fires enforcement on the per-source path**

The diff shows `findingsFor` is changed to call `enforceConstraints`. But the original `findingsFor` is also called from somewhere in the fanout path. Let me re-read the diff context to ensure that's all.

Wait, looking at engine.go's `invokeSingleShot`, it copies `MinSeverity` and `MaxFindings` to `Result`. Then `findingsFor` reads from `r.Agent`, `r.MinSeverity`, `r.MaxFindings`. That's correct.

But where is `findingsFor` called? It's not in the diff. Let me check the test:
```go
r := Result{Agent: "bruce-backup", Content: content, Status: StatusOK, MinSeverity: "MEDIUM", MaxFindings: intp(1)}
got := findingsFor(r)
```

So `findingsFor` is being called and reading from the Result. Good.

**Issue 5: Lowercase severity test is misleading**

The test case `minSeverity: "high"` is testing the postprocess layer, not the registry layer. The registry already normalizes to upper-case. So the postprocess layer is being defensive about something the registry has already done. That's fine for defensive coding, but it means postprocess is doing redundant work. Not a real issue.

**Issue 6: Severity ranking for unknowns**

```go
var severityRank = map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}
```

An unknown severity ranks 0 (default for missing key). This means if the model emits an unknown severity, it will sort below LOW. This might be intentional (penalize unknown) but could be problematic. For `min_severity` filtering, a CRITICAL threshold would drop unknown-severity findings (which is probably bad). For truncation, an unknown-severity finding would be dropped first (probably good).

**Issue 7: The `enforceConstraints` function - any edge cases?**

If `maxFindings` is 0, it should be caught by registry validation (`max_findings must be > 0`). But what if it's set to 0 at runtime somehow? Then `findings = findings[:0]` would return empty. Not a real issue.

**Issue 8: Reviewer not stamped before enforcement - good, it is**

Looking at the code:
```go
for i := range findings {
    findings[i].Reviewer = r.Agent
}
return enforceConstraints(findings, r.Agent, r.MinSeverity, r.MaxFindings)
```

Reviewer is stamped first, then constraints applied. Good — per the test `TestFindingsFor_AppliesConstraints`.

**Issue 9: `r.Agent` vs the `agent` parameter**

In `enforceConstraints(findings, agent, ...)` the `agent` is only used for logging. It's passed `r.Agent` which is correct. No issue.

**Issue 10: The diff doesn't add `Scope` to `Agent` struct**

The `Scope` field is consumed in `buildAgent` to inject the prompt, but is NOT threaded through `Agent` and `Result` structs (only `MinSeverity` and `MaxFindings` are). This is fine because the scope is applied at prompt-build time and never re-evaluated later (per the design — scope is a soft prompt injection, not a hard filter).

**Issue 11: Backward compat — applyDefaults reorders `r.Agents[name] = a`**

This is pre-existing, not part of the diff.

**Issue 12: `findings[:0]` with `append`**

The `kept := findings[:0]` followed by `kept = append(kept, f)` is the standard Go filter-in-place pattern. It reuses the backing array, so the input slice is mutated. The doc says "The input slice may be reordered in place" which is correct. However, the doc should also note that the input may be mutated (not just reordered). This is minor.

Actually wait — `findings[:0]` creates a zero-length slice sharing the backing array. Then appending to `kept` writes into the backing array starting at index 0. So findings[0], findings[1], etc. are overwritten. If the caller has a reference to the original `findings` slice (or any sub-slice of it), they would see the mutation. This could be a subtle bug. But the function returns the filtered slice, so callers should use the return value.

**Issue 13: `findings[:0]` and `append` - is the returned slice the same backing array?**

Yes, `kept` shares the backing array with `findings`. The returned slice will have the kept elements. Length will be correct, but if the caller mutates the returned slice in a way that extends beyond the kept length, they would write into the dropped region of the original backing array. Standard Go gotcha but not a bug here.

**Issue 14: Stderr warning logs**

Logs are written to `os.Stderr`. This is consistent with the rest of the codebase (presumably). Fine.

**Issue 15: `findingsFor` is the only enforcement point — but what about `writeFailureSummary` and the tool-loop path?**

The diff only changes `findingsFor`, which is called for both tool-loop and single-shot. The tool-loop `res` in `loop.go` is now also stamped with `MinSeverity` and `MaxFindings`. So the constraint flows through. Good.

**Issue 16: Test `TestBuildAgent_InjectsScopeFocus` references `twoAgentConfig`**

The test uses `twoAgentConfig("http://unused")` which is presumably a test helper. This isn't in the diff but is referenced. Out of scope to flag since it's not part of the diff.

**Issue 17: `intp` helper**

`func intp(n int) *int { return &n }` — a common helper. Fine.

**Issue 18: Missing test case for emission order with min_severity + max_findings**

There's a test "min_severity then max_findings compose" which is good.

**Issue 19: No test for unknown severity**

When the model emits an unknown severity, the behavior is undefined. This might be worth flagging — but it's defensive enough (defaults to 0).

**Issue 20: `r.Content` is required for parsing**

`findingsFor` returns `nil` if `r.Content == ""`. The enforcement still runs on a nil/empty slice. Good — `len(findings) == 0` check at the top of `enforceConstraints` handles it.

**Issue 21: Registry test files use a `writeRegistry` helper**

This is a pre-existing test helper, fine.

**Issue 22: `applyDefaults` mutates after validation**

Looking at the order in `LoadRegistry`:
1. Parse YAML
2. Apply defaults
3. Validate

If applyDefaults runs first, then validate, then normalization in applyDefaults helps the validate step. Let me check the order... Actually I can't see the full file, but the code is:

```go
func (r *Registry) applyDefaults() {
    for name, a := range r.Agents {
        ...
        if a.MinSeverity != "" {
            a.MinSeverity = normalizeSeverity(a.MinSeverity)
        }
        r.Agents[name] = a
    }
}
```

And `validate()`:
```go
if a.MinSeverity != "" && !reviewSeverities[normalizeSeverity(a.MinSeverity)] {
    return agentErrf(name, "agent '%s': min_severity must be one of CRITICAL, HIGH, MEDIUM, LOW", name)
}
```

If validate runs after applyDefaults, then `normalizeSeverity` in validate is redundant. If validate runs before applyDefaults, then it would catch bad values case-insensitively. Either way, no bug. But the test "TestRegistryLoad_MinSeverityCaseInsensitive" expects the loaded value to be "HIGH" — so applyDefaults must run before validation? No, applyDefaults always normalizes, so as long as the test reads the loaded value, it'll be "HIGH" regardless of order.

But wait — if `applyDefaults` runs first, validation accepts the normalized value, which is correct. If `applyDefaults` runs after validation, validation uses `normalizeSeverity` to check (since it's case-insensitive). Both work. So order is fine.

**Issue 23: `applyDefaults` pre-existing structure - out of scope to flag**

The applyDefaults function already iterates and copies. Not part of the diff.

**Issue 24: Logging — is the format consistent?**

The log format is:
```
atcr: warning: agent %q: dropped %d finding(s) below min_severity %s
atcr: warning: agent %q: truncated %d finding(s) to max_findings %d
```

This is consistent. AC4 says "fan-out logs warnings when output is truncated or findings are dropped" — satisfied.

**Issue 25: `findingsFor` test — is it really an integration test?**

The test `TestFindingsFor_AppliesConstraints` exercises the full `findingsFor` function with parsed content. This is a good integration test. AC2 and AC3 are covered.

**Issue 26: Registry test for invalid max_findings includes `0` and likely negative numbers**

The validation is `*a.MaxFindings <= 0` which catches 0 and negatives. Good.

**Issue 27: Validation order - what if both min_severity and max_findings are invalid?**

The validation returns the first error. Both errors would be reported as the user fixes them one at a time. Slightly annoying but standard behavior.

**Issue 28: `writeRegistry` test helper is not in the diff**

This is pre-existing, not part of the diff. Out of scope.

**Issue 29: `twoAgentConfig` test helper is not in the diff**

Same as above. Out of scope.

**Issue 30: Registry test doesn't cover `min_severity: ""`**

The `validate()` checks `if a.MinSeverity != ""` so empty is fine. Backward compatible.

**Issue 31: `applyDefaults` is called per-agent, not per-scope**

The scope field is just a `[]string` and doesn't have defaults to apply. Fine.

**Issue 32: Potential issue: Sort stability and emission order for same severity**

The test "max_findings truncates keeping the most severe" uses `sort.SliceStable` which preserves emission order for equal-severity items. So if there are 3 HIGH findings and cap is 2, the first 2 HIGH in emission order are kept. This is the desired behavior.

**Issue 33: Look at `twoAgentConfig` test helper usage**

The test does:
```go
cfg := twoAgentConfig("http://unused")
scoped := cfg.Registry.Agents["greta"]
scoped.Scope = []string{"performance", "efficiency"}
cfg.Registry.Agents["greta"] = scoped
```

This suggests `greta` and `kai` are agents in the test config. The test passes if greta's prompt contains "performance" and "efficiency" and kai's prompt does NOT contain "Review Focus". The test asserts `require.NotContains(t, kaiAgent.Prompt, "Review Focus")` — this relies on the default persona prompt for kai not containing "Review Focus". If the default persona changes, this test breaks. Minor fragility.

**Issue 34: The review.go change - prompt += payload.ScopeFocus(ac.Scope)**

```go
prompt += payload.ScopeFocus(ac.Scope)
```

This is appending to `prompt` which is already constructed. If `ac.Scope` is empty, `ScopeFocus` returns "". So unscoped agents get an empty string appended (a no-op). Good.

But wait — let me look at the full picture. `prompt` is used for both `Agent.Prompt` and `Invocation.Prompt`. The scope is injected into the prompt that the model sees, so it should be in `Invocation.Prompt`. The test confirms: `require.Contains(t, gretaAgent.Invocation.Prompt, "performance")`. Good.

**Issue 35: The order of operations in `buildAgent`**

```go
prompt += payload.ScopeFocus(ac.Scope)
prov, ok := cfg.Registry.Providers[ac.Provider]
...
return Agent{
    ...
    MinSeverity:     ac.MinSeverity,
    MaxFindings:     ac.MaxFindings,
    ...
}
```

The scope is added to prompt, then the Agent is returned with MinSeverity and MaxFindings. Good.

**Issue 36: What if `Invocation.Prompt` is built from `Agent.Prompt` later?**

Let me check. In review.go the Agent.Prompt is set:
```go
Agent: llmclient.Agent{
    ...
    Prompt: prompt,
    ...
}
```

And Invocation.Prompt might be set separately. Let me check what the diff shows... actually the diff just adds `prompt += payload.ScopeFocus(ac.Scope)`. So `prompt` is the local variable that gets used. If the Agent.Prompt is set to `prompt`, then `prompt` includes the scope. If Invocation.Prompt is set to something else, then it might not include the scope. Without seeing the full code, I can't verify. But the test `TestBuildAgent_InjectsScopeFocus` checks both `gretaAgent.Prompt` and `gretaAgent.Invocation.Prompt` for "performance", which would catch this case.

**Issue 37: The fallback agent — does it inherit the scope prompt?**

Looking at the diff:
```go
func buildFallbackAgent(cfg *ReviewConfig, primary Agent, name string) (Agent, error) {
    ...
    return Agent{
        ...
        MinSeverity: primary.MinSeverity,
        MaxFindings: primary.MaxFindings,
        ...
    }, nil
}
```

The fallback inherits `MinSeverity` and `MaxFindings` from the primary, but the fallback's `Agent.Prompt` is built independently in `buildFallbackAgent`. Let me think... if the fallback's prompt is built using the primary's `ac.Scope` or a fresh `ac.Scope`...

Wait, the docstring in the `MinSeverity` field comment says:
> A fallback inherits all three from the primary it stands in for — the constraint follows the slot, like the persona prompt — so `scope`/`min_severity`/`max_findings` set on an entry that is *only* reachable as a fallback are ignored: the primary's constraints govern whoever ultimately answers that slot.

But the diff for `buildFallbackAgent` only copies `MinSeverity` and `MaxFindings`, not `Scope`. The scope is added to the prompt in `buildAgent`, not in `buildFallbackAgent`. So if the fallback's prompt is built in `buildFallbackAgent` without the scope, then the fallback's prompt won't have the scope focus. This is inconsistent with the docstring that says "the constraint follows the slot, like the persona prompt".

Wait, let me re-read. The doc says the FALLBACK's `scope`/`min_severity`/`max_findings` are ignored. So the primary's scope should apply. But how is the scope applied to the fallback's prompt? Either:
1. The primary's prompt is reused (so the scope is already in it)
2. The primary's scope is re-injected into the fallback's prompt

Looking at the diff in `buildFallbackAgent`, I see only the inheritance of MinSeverity/MaxFindings. I don't see the scope being re-applied. So this could be a bug — the fallback's prompt might not include the primary's scope focus.

But wait — the comment in the diff says:
> Scope is applied earlier (as soft prompt injection) and is not carried here.

This suggests scope is only in the prompt and not in the Agent struct. So if `buildFallbackAgent` builds the prompt from the primary's prompt (reuses it), then the scope is there. If it builds a fresh prompt for the fallback, then the scope is lost.

Without seeing the full `buildFallbackAgent`, I can't be sure. But the comment in the diff suggests scope is applied at the prompt level only, so as long as the fallback's prompt comes from the primary or includes the primary's scope, it works.

This is a potential issue — fallback scope inheritance — but I'd need to see more of the code to be sure. Let me flag it as a potential issue.

Actually, looking more carefully at the comment in the diff:
```go
// Soft per-agent scope focus (Epic 2.2): appended after the persona template
// renders so it lands in every persona regardless of its template, and feeds
// both Agent.Prompt and Invocation.Prompt below (a fallback reuses the
// primary's prompt, so it inherits the focus too). No-op when scope is unset.
prompt += payload.ScopeFocus(ac.Scope)
```

The comment says "a fallback reuses the primary's prompt, so it inherits the focus too". So the design is that the fallback's prompt = primary's prompt (which includes the scope). Good, no bug.

**Issue 38: `findingsFor` test only tests the happy path**

The test only covers one scenario. Other edge cases (empty content, no constraints, large cap) are in `TestEnforceConstraints`. Combined coverage is good.

**Issue 39: The `enforceConstraints` function is not handling nil input**

Actually, the first line is `if len(findings) == 0 { return findings }`. If `findings` is nil, `len(nil) == 0`, so it returns nil. Good.

**Issue 40: The registry test `TestRegistryLoad_EmptyScopeEntry` uses `["performance", ""]`**

The validate function checks `strings.TrimSpace(s) == ""`. A string of just spaces would also be caught. Good.

**Issue 41: `SeverityRank` lookup is case-sensitive**

```go
var severityRank = map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}
```

The lookup `severityRank[strings.ToUpper(f.Severity)]` is case-insensitive. Good.

But wait — what if `f.Severity` is an empty string? `strings.ToUpper("") == ""` and `severityRank[""]` is 0. So an empty severity ranks 0, same as unknown. This means a finding with no severity would be dropped at any min_severity. That's probably correct behavior.

**Issue 42: The variable `min` shadows Go's built-in `min` function**

This is a minor maintainability issue. Should be renamed to `minRank` or `floor`. Since Go 1.21 introduced `min(a, b)` as a built-in, using `min` as a variable name is now shadowing. Let me flag this.

**Issue 43: `applyDefaults` and `validate` order**

If `validate` runs before `applyDefaults`, then `normalizeSeverity` in `validate` is needed. If `applyDefaults` runs first, then `normalizeSeverity` in `validate` is redundant. Either way works. Not a bug.

**Issue 44: Documentation is comprehensive but maybe too verbose**

The docs/registry.md section is 35 lines for three fields. Not a problem, it's good documentation.

**Issue 45: What about negative numbers in `max_findings`?**

`max_findings: -5` would be caught by `*a.MaxFindings <= 0`. Good.

**Issue 46: What about float values in `max_findings`?**

YAML might parse `- 20.5` as a float. But the Go type is `*int`, so YAML unmarshalling would fail with a type error. This would be caught at parse time, not validate time. Still rejected, but the error message might be cryptic. Out of scope — pre-existing YAML behavior.

**Issue 47: Sorting happens after filter — is the sort done in-place?**

```go
sort.SliceStable(findings, func(i, j int) bool {
    return severityRank[strings.ToUpper(findings[i].Severity)] > severityRank[strings.ToUpper(findings[j].Severity)])
})
```

`sort.SliceStable` sorts in place. The doc says "The input slice may be reordered in place" — this is correct.

**Issue 48: What if `f.Severity` is something like "Critical " (trailing space)?**

`strings.ToUpper` doesn't trim. So `severityRank["CRITICAL "]` would be 0. The registry's `applyDefaults` normalizes the agent's `min_severity`, but the model's output severity is not normalized. So a model outputting "Critical " or "HIGH\n" would be ranked as unknown.

This could be a robustness issue. The model output should be normalized the same way the agent's `min_severity` is. Let me flag this.

Actually wait — the agent's `min_severity` is normalized at load (in `applyDefaults`). But the finding's `Severity` comes from the model's output, which is not normalized. So if a model outputs "high" or "HIGH " (with whitespace), it would not be found in the severityRank map. The fan-out's enforceConstraints would treat it as unknown (rank 0).

This is a real issue. The model's output severity should be normalized before lookup. Let me flag this.

**Issue 49: How does the parser handle model output severity?**

The parser is in `internal/stream/parser.go` (mentioned in the clarifications). The diff doesn't change the parser, so I can't verify. But the test cases in `postprocess_test.go` use clean severities ("HIGH", "LOW", "MEDIUM", "CRITICAL"), so this issue is hidden by the test.

Let me look at the sprint plan's clarifications: "ParseModelOutput enforces 7 model columns only, so a model can never self-attribute REVIEWER (internal/stream/parser.go:75-76); overflow folds into EVIDENCE not REVIEWER (internal/stream/parser.go:103-108)."

So the parser enforces 7 columns. The severity is the first column. If the model outputs "high" or "High", the parser would store it as-is. The enforceConstraints function would then look up `severityRank[strings.ToUpper("high")]` which is 3 (HIGH). So lowercase is handled.

But what about trailing whitespace? The parser might or might not trim. If it doesn't, then "HIGH\n" would be stored as "HIGH\n" and the lookup would fail. This is a potential issue.

I think I should flag this as a robustness concern.

**Issue 50: `min` built-in shadowing**

Re-flagging because it's a real issue. The Go 1.21+ built-in `min` is shadowed. This could cause issues if someone later tries to use `min(a, b)` in this scope. It's a maintainability concern.

**Issue 51: The `f` function in postprocess_test.go shadows the package-level `f` (if any)?**

No, `f` is just a local helper. Fine.

**Issue 52: The test uses `require.NoError` and `require.Contains` but the helper `eq` doesn't use testify**

The test uses both `eq` (custom) and `require.NotContains` (testify). Inconsistent. Minor.

**Issue 53: The `sevs` helper returns a slice of strings**

This is fine. The `eq` helper compares two slices. Custom helpers but no bug.

**Issue 54: No negative test for invalid `scope` value type**

If YAML is `scope: "performance"` (a string, not a list), YAML unmarshalling would fail. Caught at parse time. Good.

**Issue 55: What about a single non-list scope value?**

`scope: performance` would be a string, not a list. Unmarshalling would fail. Caught at parse time.

**Issue 56: The `internal/payload/scope.go` file's existing content**

The diff shows the file already had content (the `// Per-payload-mode scope rules` comment), and the new `ScopeFocus` function is added at the top. The file is `scope.go` and the new function is `ScopeFocus`. The existing content is about per-payload-mode scope rules. Two different concepts sharing a file. Maintainability concern, but not a bug.

Wait, actually let me re-read. The existing comment is about "Per-payload-mode scope rules" — different from the new "per-agent scope categories" (Epic 2.2). Two different scopes. The file is named `scope.go` and has both. The naming is confusing but functional. Could be a maintainability issue worth flagging.

**Issue 57: `payload.ScopeFocus` is the only export in the file that uses the new `strings` import**

The diff adds `import "strings"` to `scope.go`. The existing code might not need it. Let me check... the diff shows `+import "strings"` as a new import. So the existing code didn't use strings, and the new `ScopeFocus` does. Fine.

**Issue 58: Variable shadowing of `c` in `ScopeFocus`**

```go
for _, c := range scope {
    if c = strings.TrimSpace(c); c != "" {
        cats = append(cats, c)
    }
}
```

`c` is used as a loop variable and reassigned. This is standard Go. The variable `c` is local to the loop iteration. Fine.

**Issue 59: The `cats` slice could be `nil` initially**

`cats := make([]string, 0, len(scope))` — creates an empty (non-nil) slice. `strings.Join(nil, ...)` would panic. So this is correct.

But wait — what if all entries are blank? Then `len(cats) == 0` and we return "". Good.

**Issue 60: Order of validation and defaults in registry**

Looking at the diff:
```go
func (r *Registry) applyDefaults() {
    for name, a := range r.Agents {
        if a.MaxTurns == nil {
            mt := DefaultMaxTurns
            a.MaxTurns = &mt
        }
        // Canonicalize min_severity (Epic 2.2) so downstream enforcement compares
        // against a stable upper-case token regardless of how it was written.
        if a.MinSeverity != "" {
            a.MinSeverity = normalizeSeverity(a.MinSeverity)
        }
        r.Agents[name] = a
    }
}
```

If `applyDefaults` is called after `validate`, then validation uses `normalizeSeverity` correctly. The order in `LoadRegistry` is what matters. I can't see the full file to verify, but the design works either way.

**Issue 61: The `findingsFor` test is named `TestFindingsFor_AppliesConstraints` but actually tests truncation**

The test:
```go
r := Result{Agent: "bruce-backup", Content: content, Status: StatusOK, MinSeverity: "MEDIUM", MaxFindings: intp(1)}
got := findingsFor(r)
if len(got) != 1 {
    t.Fatalf("got %d findings, want 1 (LOW dropped, capped to 1)", len(got))
}
```

It tests both min_severity drop and max_findings cap. Good.

**Issue 62: The test doesn't verify the warning logs**

The test doesn't capture stderr to verify the warnings are logged. This is a minor gap in test coverage for AC4. Could be flagged.

**Issue 63: The `enforceConstraints` function logs even for 1 finding**

If 1 finding is dropped, the log says "dropped 1 finding(s)". This is fine.

**Issue 64: The doc string says "Dropped/truncated counts are logged to stderr" but only when `> 0`**

The code:
```go
if dropped > 0 {
    fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: dropped %d finding(s) below min_severity %s\n", agent, dropped, floor)
}
```

Only logs when there are dropped findings. For truncation, always logs (since the condition for entering the block is `len(findings) > *maxFindings`, which guarantees truncated > 0). So truncation always logs (when triggered), min_severity only logs when dropping. This is fine.

**Issue 65: The `enforceConstraints` function is documented as having `r.Agent` as input but signature is `agent`**

The function signature is `enforceConstraints(findings []stream.Finding, agent, minSeverity string, maxFindings *int)`. The `agent` is the second parameter. The call site uses `r.Agent`. No bug.

**Issue 66: `r.Agent` is the registry agent key, not the model name**

This is correct per the design (AC1/AC6 already done). The reviewer stamping is by registry key. The logging uses the registry key. Good.

**Issue 67: The fallback inherits `MinSeverity` and `MaxFindings` from primary**

```go
MinSeverity: primary.MinSeverity,
MaxFindings: primary.MaxFindings,
```

But what about `ac.MinSeverity` from the fallback's own config? The design says the primary's constraints govern. So the fallback's own `ac.MinSeverity` (from `buildFallbackAgent`'s `ac := cfg.Registry.Agents[name]`) is ignored. This is consistent with the doc.

But wait — what if the fallback is itself a primary in another context? E.g., if `bob2` falls back to `bob2-backup` AND `bob2-backup` is also a primary in another slot. Then the `bob2-backup`'s own constraints would be ignored in the fallback slot (correct per design). Good.

**Issue 68: The `applyDefaults` function is mutating `r.Agents[name]`**

Pre-existing, not part of the diff. Out of scope.

**Issue 69: The `validate` function iterates `for name, a := range r.Agents`**

The `a` is a copy (since `a` is a struct value). Mutations to `a` are not reflected back. But `validate` only reads, so this is fine. Pre-existing.

**Issue 70: The new `validate` checks access `a.MinSeverity` and `a.MaxFindings`**

Since `a` is a copy, the checks are on the loaded values. Good.

**Issue 71: The new `validate` checks access `a.Scope`**

Since `a` is a copy, the iteration over `a.Scope` is a copy of the slice header. The loop doesn't mutate, so this is fine.

**Issue 72: Does the scope `for _, s := range a.Scope` iterate over a copy?**

`a.Scope` is a `[]string` slice. The slice header is copied (pointer + length + capacity). The backing array is shared. The loop `for _, s := range a.Scope` doesn't mutate, so this is fine.

**Issue 73: The `applyDefaults` function is pre-existing in structure**

The new code is added to the existing loop. The pre-existing loop already does `r.Agents[name] = a` at the end. So the new normalization is included in the same write. Good.

**Issue 74: The test `TestRegistryLoad_MinSeverityCaseInsensitive` uses a lowercase input**

```go
min_severity: high
```

And expects:
```go
assert.Equal(t, "HIGH", reg.Agents["bruce"].MinSeverity, "min_severity normalized to canonical upper-case")
```

This works because `applyDefaults` normalizes. Good.

**Issue 75: The test `TestRegistryLoad_EmptyScopeEntry` uses `["performance", ""]`**

The validate function catches this. Good.

**Issue 76: The test `TestRegistryLoad_InvalidMaxFindings` uses `max_findings: 0`**

The validate function catches this. Good.

**Issue 77: The test `TestRegistryLoad_InvalidMinSeverity` uses `min_severity: SOMETIMES`**

The validate function catches this. Good.

**Issue 78: What about `max_findings: 0` in YAML?**

YAML parses this as int 0. The validate function catches it. Good.

**Issue 79: What about `max_findings: -1`?**

YAML parses this as int -1. The validate function catches it (`<= 0`). Good.

**Issue 80: What about `max_findings: 1.5`?**

YAML parses this as float 1.5. Go's `*int` would fail to unmarshal. YAML library returns a type error. Caught at parse time.

**Issue 81: The `Scope` field accepts any string**

The validate function only checks for non-empty. It doesn't validate against a known list of categories. This is intentional (per the design — categories are not enumerated). The "categories" are free-form. The doc says "every entry must be non-empty".

**Issue 82: The `MinSeverity` field validates against the rubric**

Good. The rubric is {CRITICAL, HIGH, MEDIUM, LOW}.

**Issue 83: The `MaxFindings` field validates as positive int**

Good.

**Issue 84: What if `scope` is a single string in YAML?**

YAML parsing would fail. Caught at parse time.

**Issue 85: What if `scope` is nested?**

YAML parsing would fail. Caught at parse time.

**Issue 86: The docstring `// soft focus categories injected into the prompt` is good**

**Issue 87: The docstring `// drop findings below this floor (CRITICAL|HIGH|MEDIUM|LOW)` is good**

**Issue 88: The docstring `// cap on findings (severity-sorted truncate); nil = unlimited` is good**

**Issue 89: The `normalizeSeverity` function is package-private (lowercase)**

Used internally by `applyDefaults` and `validate`. Fine.

**Issue 90: The `reviewSeverities` map is package-private**

Used internally by `validate`. Fine.

**Issue 91: The `severityRank` map in `postprocess.go` is package-private**

Used internally. Fine.

**Issue 92: The `severityRank` map is duplicated across `postprocess.go` and `reconcile/merge.go`**

The doc says "It is kept local so the fan-out post-processing carries no cross-package dependency on the reconciler." So this is intentional. But the duplication means a future change to the rubric must be made in both places. Maintainability concern. Could be flagged.

Actually, the doc also says: "It mirrors the canonical rubric in personas/_base.md (and reconcile's private severityRank); it is kept local so the fan-out post-processing carries no cross-package dependency on the reconciler."

So the design intentionally duplicates. The risk is drift, but the doc explicitly acknowledges this. Not a real issue per the design.

**Issue 93: The `enforceConstraints` function uses `strings.ToUpper` repeatedly**

Could be precomputed. But for small slices, this is fine. Performance is not a concern.

**Issue 94: The `enforceConstraints` function's doc says "in order: (1) min_severity drops every finding below the floor; (2) max_findings keeps only the N most severe"**

This is correct.

**Issue 95: The `enforceConstraints` function is called from `findingsFor` after REVIEWER stamping**

The test verifies this. Good.

**Issue 96: The `findingsFor` doc was updated to mention Epic 2.2**

Good.

**Issue 97: The `engine.go` `Agent` struct has new fields with a comment**

Good.

**Issue 98: The `engine.go` `Result` struct has new fields with a comment**

Good.

**Issue 99: The `loop.go` change adds the fields to the `res` initialization**

The struct literal `&Result{...}` is updated. Good.

**Issue 100: What if `invokeToolLoop` doesn't return a result via `findingsFor`?**

The `res` is set on the `loop` struct. The caller reads `loop.res`. The `findingsFor` is called by the caller (somewhere). I can't see the full call site, but the design is consistent.

**Issue 101: The test `TestBuildAgent_InjectsScopeFocus` uses `twoAgentConfig`**

This is a pre-existing test helper. Out of scope.

**Issue 102: The test `TestBuildAgent_InjectsScopeFocus` modifies the registry in-place**

```go
scoped := cfg.Registry.Agents["greta"]
scoped.Scope = []string{"performance", "efficiency"}
cfg.Registry.Agents["greta"] = scoped
```

This modifies the registry after loading. If `greta` already has a `Scope` set by the test config, this would overwrite. The helper `twoAgentConfig` likely sets up `greta` and `kai` with default configs. The test then mutates `greta` to add scope. This is a common test pattern. Fine.

**Issue 103: The test `TestBuildAgent_InjectsScopeFocus` uses `require.NotContains(t, kaiAgent.Prompt, "Review Focus")`**

This is a fragile test — it depends on kai's default persona prompt not containing "Review Focus". If a future change adds "Review Focus" to a persona template, this test breaks. Minor.

**Issue 104: The test for `ScopeFocus` doesn't check the exact format**

The test only checks `strings.Contains`. It doesn't verify the exact wording. This is a minor coverage gap but not a bug.

**Issue 105: The `ScopeFocus` function uses `strings.Join(cats, ", ")`**

Good — uses comma-space separator.

**Issue 106: The `ScopeFocus` function returns the leading blank lines**

```go
return "\n\n## Review Focus\nConcentrate this review on the following categories: " +
    strings.Join(cats, ", ") + ". Prioritize findings in these areas. This is a focus " +
    "hint, not a hard limit — still report any genuinely critical issue you find outside them."
```

The leading `\n\n` separates the block from whatever was rendered before. Good practice for prompt injection.

**Issue 107: The wording "still report any genuinely critical issue you find outside them" — is "critical" the right word here?**

"critical" is informal. The rubric is "CRITICAL|HIGH|MEDIUM|LOW". The wording could be "report any genuinely high-severity issue" or similar. Minor wording concern.

**Issue 108: The `ScopeFocus` function says "Concentrate this review on the following categories"**

This is good wording for a soft constraint.

**Issue 109: The `ScopeFocus` function says "This is a focus hint, not a hard limit"**

Good — clarifies the soft nature.

**Issue 110: The `ScopeFocus` function's docstring says "Blank entries are skipped; an empty/nil scope yields "" so an unscoped agent's prompt is unchanged"**

The test `TestScopeFocus` verifies both nil and empty slice return "". Good.

**Issue 111: The `ScopeFocus` function uses an em-dash "—" in the output**

This is fine for UTF-8 text. No issue.

**Issue 112: The registry test for `scope` with whitespace-only entry**

The test `TestRegistryLoad_EmptyScopeEntry` uses `""` (empty string). The validate function uses `strings.TrimSpace(s) == ""` which also catches whitespace-only. Good.

**Issue 113: The `applyDefaults` doesn't normalize `Scope` entries**

If a user writes `scope: ["performance", "  efficiency  "]`, the entries are stored as-is. The `ScopeFocus` function trims them. So the prompt output is clean. The validation rejects empty entries. So whitespace-only entries are rejected. Entries with leading/trailing whitespace are stored and trimmed in ScopeFocus. This is consistent.

**Issue 114: The `Scope` field in `AgentConfig` is not deduplicated or normalized**

If a user writes `scope: ["performance", "performance"]`, both entries are stored. The `ScopeFocus` would join them as "performance, performance". Not ideal but not a bug. The user can avoid this.

**Issue 115: The `severityRank` map doesn't include empty string as a valid severity**

If `f.Severity` is empty, the rank is 0. This means empty-severity findings are dropped at any min_severity. This is probably correct (the parser should have caught this).

**Issue 116: The test `TestEnforceConstraints` has a case for "min_severity accepts lower-case (defensive normalization)"**

The test uses `minSeverity: "high"` (lowercase) and expects the function to normalize and keep "HIGH". This is the defensive normalization. Good.

**Issue 117: The `enforceConstraints` function's normalization is `strings.ToUpper(strings.TrimSpace(minSeverity))`**

The registry already normalizes via `applyDefaults`. The function re-normalizes defensively. This is belt-and-suspenders. Good.

**Issue 118: The `enforceConstraints` function's normalization of `f.Severity` is `strings.ToUpper(...)` (no trim)**

If `f.Severity` has trailing whitespace, the lookup fails. This is the issue I flagged earlier (Issue 48). Let me think about this more carefully.

The model's output is parsed by `ParseModelOutput` in `internal/stream/parser.go`. The parser likely splits on `|` and trims fields. If it does, then `f.Severity` is already trimmed. If it doesn't, then `f.Severity` could have whitespace. The diff doesn't change the parser, so the existing behavior is preserved.

Looking at the test:
```go
content := "HIGH|a.go:1|bug|fix|correctness|5|ev\n" +
    "LOW|a.go:2|nit|fix|style|5|ev\n" +
    "MEDIUM|a.go:3|gap|fix|correctness|5|ev\n"
```

The severities are clean (no whitespace). The test doesn't exercise the whitespace-in-severity case.

The risk: if the parser doesn't trim, and a model outputs "HIGH " (with trailing space), then `severityRank["HIGH "]` is 0, and the finding is treated as unknown. This could be a real bug.

Let me flag this as a robustness concern.

Actually, looking at the existing code (not in the diff), I think the parser does trim or the model output is expected to be clean. But without seeing the parser, I can't be sure. This is a minor risk.

**Issue 119: The `enforceConstraints` function is in a new file `postprocess.go`**

This is a clean separation. The existing `artifacts.go` is updated to call it. Good.

**Issue 120: The test file `postprocess_test.go` is in the same package**

Standard Go testing. Good.

**Issue 121: The test `TestEnforceConstraints` uses `f` as a helper name**

The helper function `f(sev string) stream.Finding` shadows nothing but is a single-letter name. Minor readability concern.

**Issue 122: The test `TestEnforceConstraints` uses `intp` as a helper name**

The helper `intp(n int) *int` is a single-word name. Fine.

**Issue 123: The test `TestEnforceConstraints` uses `sevs` and `eq` as helper names**

Fine.

**Issue 124: The test `TestEnforceConstraints` has 8 sub-tests**

Good coverage.

**Issue 125: The test `TestFindingsFor_AppliesConstraints` is a separate test**

Good — separates unit tests from integration tests.

**Issue 126: The test `TestFindingsFor_AppliesConstraints` checks `len(got) != 1`**

This is a strict check. If the cap is 1, only 1 finding should remain. Good.

**Issue 127: The test `TestFindingsFor_AppliesConstraints` checks `got[0].Severity != "HIGH"`**

The cap should keep the most severe. With min_severity MEDIUM, the LOW is dropped. With cap 1, the most severe of {MEDIUM, HIGH} is HIGH. Good.

**Issue 128: The test `TestFindingsFor_AppliesConstraints` checks `got[0].Reviewer != "bruce-backup"`**

The reviewer is stamped before enforcement. Good.

**Issue 129: The test `TestFindingsFor_AppliesConstraints` uses `intp(1)` for cap**

`maxFindings: 1`. The test expects 1 finding. Good.

**Issue 130: The test `TestEnforceConstraints` case "max_findings no-op when under the cap (order preserved)"**

```go
in:   []stream.Finding{f("LOW"), f("HIGH")},
maxFindings: intp(5),
want: []string{"LOW", "HIGH"},
```

The cap is 5, only 2 findings. No sorting. Order preserved. Good — this is the optimization mentioned in the docstring.

**Issue 131: The `enforceConstraints` function only sorts when truncation will happen**

```go
if maxFindings != nil && len(findings) > *maxFindings {
    sort.SliceStable(findings, ...)
    ...
}
```

This is the optimization. Good.

**Issue 132: The `enforceConstraints` function's truncation log is always emitted when triggered**

```go
truncated := len(findings) - *maxFindings
findings = findings[:*maxFindings]
fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: truncated %d finding(s) to max_findings %d\n", agent, truncated, *maxFindings)
```

Good — always logs when truncation happens (which is when `len > *maxFindings`).

**Issue 133: The `enforceConstraints` function's min_severity log is only emitted when findings are dropped**

```go
if dropped > 0 {
    fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: dropped %d finding(s) below min_severity %s\n", agent, dropped, floor)
}
```

Good — no log if nothing was dropped.

**Issue 134: The `enforceConstraints` function returns the modified slice**

The function returns `findings` which is the (possibly filtered, possibly sorted) slice. The caller uses the return value. Good.

**Issue 135: The `findingsFor` function returns the result of `enforceConstraints`**

```go
return enforceConstraints(findings, r.Agent, r.MinSeverity, r.MaxFindings)
```

Good.

**Issue 136: The `findingsFor` function's doc was updated**

The new doc mentions "Epic 2.2" and the guardrails. Good.

**Issue 137: The `engine.go` `Agent` struct comment is detailed**

Good.

**Issue 138: The `engine.go` `Result` struct comment is detailed**

Good.

**Issue 139: The `loop.go` change is minimal**

Just adds the two fields to the struct literal. Good.

**Issue 140: The `review.go` `buildAgent` change is minimal**

Adds scope to prompt, adds fields to Agent. Good.

**Issue 141: The `review.go` `buildFallbackAgent` change is minimal**

Adds primary's fields to fallback. Good.

**Issue 142: The `payload/scope.go` change adds the new function**

Good.

**Issue 143: The `registry/config.go` change adds the new fields and validation**

Good.

**Issue 144: The test files are comprehensive**

Good coverage of the new functionality.

**Issue 145: The doc file is updated with the new section**

Good.

OK, I've been thorough. Let me now consolidate the real issues:

Real issues:
1. **Variable shadowing of `min` built-in** in `postprocess.go` (Issue 42/50). Go 1.21+ has `min(a, b)` as a built-in. Using `min` as a variable name shadows it. Should be renamed to `minRank` or `floor`.

2. **Robustness: model output severity not normalized** (Issue 48/118). The `enforceConstraints` function uses `strings.ToUpper` on `f.Severity` but doesn't `TrimSpace`. If the model output has trailing whitespace (or the parser doesn't trim), the lookup fails. Should be `strings.ToUpper(strings.TrimSpace(f.Severity))`.

3. **Test doesn't capture stderr to verify AC4** (Issue 62). The `enforceConstraints` function logs warnings to stderr, but the tests don't capture stderr to verify the logs. This is a minor coverage gap for AC4.

4. **Duplicated severityRank** (Issue 92). The severityRank map is duplicated between `postprocess.go` and `reconcile/merge.go`. The doc acknowledges this but it's a maintainability risk. Not a bug per design.

5. **Fragile test: `NotContains "Review Focus"`** (Issue 103/33). The test `TestBuildAgent_InjectsScopeFocus` asserts `NotContains(t, kaiAgent.Prompt, "Review Focus")`. This depends on kai's default persona not containing this string. If a future change adds it, the test breaks. Could be more specific (e.g., check the exact scope section).

Let me check what's actually in scope. The sprint plan says:
- IN scope: add `scope` / `min_severity` / `max_findings` to `AgentConfig` + load-time validation; soft scope prompt injection into persona prompts; fan-out per-source enforcement of `min_severity` (drop below floor) and `max_findings` (severity-sorted truncate) with warning logs; `docs/registry.md` field docs + example block; tests for all of the above.
- OUT of scope: AC1/AC6 reviewer-name stamping (already done); reconcile-stage changes; in-repo `registry.yaml`; hardening of the external llm-tools skill files; a hard category drop for `scope`.

The duplicated severityRank is in the fan-out package, so flagging it in postprocess.go is in scope. The reconcile version is out of scope.

Let me focus on the real issues:

1. Variable shadowing of `min` — REAL, in scope (postprocess.go)
2. Model output severity not normalized — REAL, in scope (postprocess.go)
3. Test doesn't capture stderr — minor, in scope (postprocess_test.go)
4. Duplicated severityRank — design choice, not a bug
5. Fragile test assertion — minor, in scope (scope_inject_test.go)

Let me also check:

- Is there an issue with the `enforceConstraints` function being called on a nil `findings` slice when `r.Content` is empty? No — `findingsFor` returns `nil` early if `r.Content == ""`, so `enforceConstraints` is never called with empty input from `findingsFor`. But the test `TestEnforceConstraints` tests with `nil` input and expects `nil` return. Good.

- Is there a race condition? No — all operations are single-threaded per agent.

- Is there a memory leak? No — the slice is reused in place.

- Is there an off-by-one error? Let me check the truncation: `findings = findings[:*maxFindings]`. If `len(findings) == 5` and `*maxFindings == 2`, then `findings = findings[:2]`. The truncated count is `5 - 2 = 3`. Good.

- Is there a nil dereference? `severityRank[strings.ToUpper(f.Severity)]` — if `f.Severity` is empty, the lookup is `severityRank[""]` which is 0. No nil deref. `severityRank[floor]` where `floor` is the normalized min_severity — if the agent's min_severity is "INVALID" (caught by validation), this is never called with invalid input. Good.

- Is there a logging issue? The log format uses `%q` for the agent name (quotes). Consistent. The log uses `%d` for counts. The log uses `%s` for severity. Good.

- Is the log message wording consistent with other atcr logs? I can't verify without seeing other logs, but the format is reasonable.

OK let me write up the findings. I'll focus on the most actionable issues:

1. **Variable shadowing of `min` in `postprocess.go:36`** — MEDIUM maintainability. Go 1.21+ has `min(a, b)` as a built-in. Renaming to `minRank` or `floor` would be clearer.

2. **Model severity lookup doesn't trim whitespace in `postprocess.go:39,54`** — MEDIUM correctness. The agent's min_severity is normalized via `normalizeSeverity` (TrimSpace + ToUpper), but the model's output severity in findings is only ToUpper'd, not trimmed. If the parser doesn't trim, this could cause findings to be misclassified.

3. **Test doesn't verify stderr warning logs in `postprocess_test.go`** — LOW test coverage. AC4 requires warnings when truncated/dropped. The test could capture stderr to verify the log messages.

4. **Duplicated severityRank map in `postprocess.go:14`** — LOW maintainability. The same map exists in `internal/reconcile/merge.go:17`. If the rubric changes, both must be updated. Consider exporting a shared canonical source (out of scope per design, but worth flagging).

5. **Fragile test assertion in `scope_inject_test.go:27`** — LOW test fragility. The test asserts `NotContains(t, kaiAgent.Prompt, "Review Focus")`. If a future change adds this string to a default persona, the test breaks. Could check for a more specific marker.

Wait, I should also consider:

6. **The `findings[:0]` reuse pattern** — this is a known Go pattern but mutates the input. The doc says "The input slice may be reordered in place" which is correct but doesn't mention mutation. This could surprise a caller who holds a reference to the original slice. Minor.

7. **The `enforceConstraints` function doesn't check `minSeverity` validity at runtime** — if somehow an invalid value gets through (e.g., bypassed validation), `severityRank[floor]` is 0, and every finding is dropped (since `severityRank[strings.ToUpper(f.Severity)] >= 0` is always true... wait, no, `>= 0` means all findings pass. Hmm, let me re-check.

Actually:
```go
if severityRank[strings.ToUpper(f.Severity)] >= min {
    kept = append(kept, f)
}
```

If `min == 0` (invalid severity, rank 0), then all findings pass (since all severities rank >= 0). So the filter is a no-op. Good — fail-safe behavior. Not a real issue.

8. **The `enforceConstraints` function's `sort.SliceStable` is O(n log n)** — for very large finding lists, this could be slow. But the design says `max_findings` caps the count, and the typical finding list is small. Not a real performance issue.

9. **The fallback inherits `MinSeverity` and `MaxFindings` from primary** — but what if the primary has no constraints? Then the fallback has no constraints. The fallback's own `ac.MinSeverity` is ignored. This is the design. The doc says "A fallback inherits all three from the primary it stands in for — the constraint follows the slot, like the persona prompt — so `scope`/`min_severity`/`max_findings` set on an entry that is *only* reachable as a fallback are ignored: the primary's constraints govern whoever ultimately answers that slot."

So if `bob2` falls back to `bob2-backup`, and `bob2` has `min_severity: HIGH`, and `bob2-backup` has `min_severity: LOW`, then the fallback uses `HIGH` (the primary's). This is the design. Not a bug.

10. **The scope injection happens after persona template renders** — the doc says "appended after the persona template renders so it lands in every persona regardless of its template". This is good design. The scope is always at the end, after the persona.

11. **The `ScopeFocus` function is called in `buildAgent` but not in `buildFallbackAgent`** — the doc says "a fallback reuses the primary's prompt, so it inherits the focus too". So the scope is in the primary's prompt, which is reused. Not a bug.

OK, let me finalize the findings. I'll be conservative and only flag the most actionable issues:

1. Variable shadowing of `min` built-in — MEDIUM maintainability
2. Model severity not trimmed in lookup — MEDIUM correctness
3. Test doesn't capture stderr — LOW test coverage
4. Duplicated severityRank — LOW maintainability
5. Fragile test assertion — LOW test fragility

Let me also consider:

6. **The `enforceConstraints` function's doc says "in order: (1) min_severity drops every finding below the floor; (2) max_findings keeps only the N most severe"** — but if min_severity is empty (no floor), and max_findings is set, the order is: skip step 1, do step 2. The function handles this correctly with the `if floor := ...` guard.

7. **The `enforceConstraints` function's first line `if len(findings) == 0 { return findings }`** — this handles nil and empty. Good.

8. **The `findingsFor` test's `Status: StatusOK` field** — the test sets `Status: StatusOK` but `findingsFor` doesn't check status. The status is presumably checked by the caller before calling `findingsFor`. This is fine.

9. **The `findingsFor` function's behavior on failed agents** — `r.Content == ""` returns nil. The enforcement is never called. Good.

10. **The `enforceConstraints` function's behavior on `nil` maxFindings** — the `if maxFindings != nil` check handles it. Good.

OK, I think I have a good list. Let me write them up.

Actually, let me also check one more thing: the `enforceConstraints` function's handling of `maxFindings == 0`. If somehow `maxFindings` is 0 (should be caught by validation, but defensively), then `findings = findings[:0]` returns empty. The truncated count is `len(findings) - 0 = len(findings)`. So all findings are truncated. The log says "truncated X finding(s) to max_findings 0". This is a degenerate case but handled.

OK, finalizing the findings. Let me also reconsider the severity of the model-severity-not-trimmed issue. Looking at the parser more carefully (from the clarifications): "ParseModelOutput enforces 7 model columns only". The parser splits on `|` and creates 7 fields. If the model output is `HIGH|a.go:1|...`, then the severity field is "HIGH" (clean). If the model output is `HIGH |a.go:1|...` (with trailing space), then the severity field is "HIGH " (with trailing space). The parser's behavior depends on its implementation.

Without seeing the parser, I'll assume the parser trims or the model output is expected to be clean. So this is a defensive concern, not a confirmed bug. I'll mark it as MEDIUM (potential issue, not confirmed).

Actually, let me re-read the test cases in `postprocess_test.go`:
```go
in:   []stream.Finding{f("LOW"), f("HIGH")},
minSeverity: "high",
want: []string{"HIGH"},
```

The function uses `strings.ToUpper(strings.TrimSpace(minSeverity))` for the agent's input. But it uses `strings.ToUpper(f.Severity)` for the finding's severity (no trim). This is inconsistent. The defensive normalization should be applied to both.

This is a real inconsistency. Let me flag it.

OK, final findings:

1. **MEDIUM** - `postprocess.go:36` - Variable `min` shadows Go 1.21+ built-in `min(a, b)`. Rename to `minRank` or `floor`.
2. **MEDIUM** - `postprocess.go:39,54` - Model severity lookup uses `strings.ToUpper` but not `TrimSpace`. Inconsistent with the agent's `minSeverity` normalization (which uses both). Could cause findings to be misclassified if the parser doesn't trim.
3. **LOW** - `postprocess_test.go` - Tests don't capture stderr to verify AC4 warning logs are emitted when findings are dropped/truncated.
4. **LOW** - `postprocess.go:14` - `severityRank` map is duplicated from `internal/reconcile/merge.go:17`. If the rubric changes, both must be updated. The doc acknowledges this as intentional but it's a maintenance hazard.
5. **LOW** - `scope_inject_test.go:27` - Test asserts `NotContains(t, kaiAgent.Prompt, "Review Focus")` which is fragile. A future persona change could add this string and break the test.

Let me also think about whether there's anything else...

Actually, I want to double-check the `enforceConstraints` function's handling of the `floor` variable:

```go
if floor := strings.ToUpper(strings.TrimSpace(minSeverity)); floor != "" {
    min := severityRank[floor]
```

The `floor` variable is the normalized severity string. The `min` variable is the rank. This is fine, but the shadowing of `min` is the issue.

Also, the `floor` variable is used in the log message: `... below min_severity %s\n", agent, dropped, floor)`. This is correct — it shows the normalized form.

OK, I think the findings are solid. Let me write them up in the required format.

Wait, one more thing. The `severityRank` lookup in the sort:
```go
sort.SliceStable(findings, func(i, j int) bool {
    return severityRank[strings.ToUpper(findings[i].Severity)] > severityRank[strings.ToUpper(findings[j].Severity)]
})
```

This calls `strings.ToUpper` on each comparison, which is O(n log n) calls. Could be precomputed with a `ranked` slice. But for small finding lists, this is fine. Not a real performance issue.

OK, finalizing.

Let me also check the `intp` helper:
```go
func intp(n int) *int { return &n }
```

This is a common Go helper. The function takes a value and returns a pointer to a copy. This is correct for creating a pointer to a literal. No issue.

OK, writing the findings now.

Actually, I realize I should also consider whether the test `TestEnforceConstraints` case "min_severity accepts lower-case (defensive normalization)" is testing the right thing. The test passes `"high"` (lowercase) as `minSeverity`. The function normalizes to `"HIGH"` and looks up `severityRank["HIGH"]` which is 3. The finding `"LOW"` has rank 1, which is < 3, so it's dropped. The finding `"HIGH"` has rank 3, which is >= 3, so it's kept. The result is `["HIGH"]`. Good.

But this test depends on the function's defensive normalization. If the function only accepted uppercase (as the registry ensures), the test would fail. So the test is explicitly verifying the defensive normalization. Good.

OK, final findings:

1. MEDIUM maintainability - `min` variable shadows Go built-in
2. MEDIUM correctness - Inconsistent normalization of severity in lookup
3. LOW test coverage - stderr not captured
4. LOW maintainability - duplicated severityRank
5. LOW test fragility - NotContains assertion

Let me format these properly.

Actually, I want to reconsider issue #2. The `strings.ToUpper` is applied to `f.Severity` in the lookup. The registry normalizes `minSeverity` via `normalizeSeverity` (TrimSpace + ToUpper). The function `enforceConstraints` also normalizes `minSeverity` via `strings.ToUpper(strings.TrimSpace(minSeverity))` (redundant with the registry, but defensive). But for `f.Severity`, only `strings.ToUpper` is applied.

If the parser stores `f.Severity` as `"HIGH "` (trailing space), the lookup `severityRank["HIGH "]` returns 0 (not found). The finding would be ranked as unknown, not as HIGH.

Is this a real risk? The parser's behavior matters. If the parser trims, no risk. If not, risk.

Looking at the test:
```go
content := "HIGH|a.go:1|bug|fix|correctness|5|ev\n" +
```

The test uses clean severities. So the test doesn't exercise this case.

I'll keep this as a MEDIUM robustness concern.

OK, writing the findings now.

One more consideration: the `enforceConstraints` function's `sort.SliceStable` is called only when truncation will happen. The doc says "Sort only when a truncation will actually happen, so an uncapped (or under-cap) agent keeps its emission order." Good.

But the `sort.SliceStable` sorts the entire slice, not just the top N. For a very large finding list (e.g., 1000 findings with cap of 20), the sort is O(1000 log 1000) and then we take the first 20. This is wasteful — a partial sort (heap-based) would be O(1000 log 20). But for typical finding lists (10-100), this is fine. Not a real performance issue.

OK, truly finalizing now.

Let me also check: is there a test for the case where `max_findings` is set but `min_severity` is also set, and the min_severity drops some findings, bringing the count under the cap? The test "min_severity then max_findings compose" covers this:
```go
in:          []stream.Finding{f("LOW"), f("MEDIUM"), f("HIGH"), f("CRITICAL"), f("MEDIUM")},
minSeverity: "MEDIUM",
maxFindings: intp(2),
want:        []string{"CRITICAL", "HIGH"},
```

After min_severity MEDIUM, findings are: {MEDIUM, HIGH, CRITICAL, MEDIUM}. That's 4 findings. After cap of 2, sorted by severity: {CRITICAL, HIGH}. Good.

OK, writing the findings.

Hmm, let me also think about the `enforceConstraints` function's behavior when `findings` is nil vs empty. `len(nil) == 0`, so the early return handles both. Good.

And the function's behavior when `maxFindings` points to 0 (degenerate). Caught by validation, but defensively: `findings = findings[:0]` returns empty. The truncated count is `len(findings) - 0 = len(findings)`. Log says "truncated X finding(s) to max_findings 0". This is a weird message but the behavior is correct (all findings removed).

OK, truly writing the findings now.

Let me think about whether I should also flag:

- The `payload/scope.go` file has two different "scope" concepts (per-payload-mode rules and per-agent scope). This is a naming concern. But it's pre-existing (the file already had the per-payload-mode rules). The new function is added. Not a bug per se, but the file is now mixing two concepts. Could be a maintainability concern.

Actually, the new function is `ScopeFocus` and the existing is `// Per-payload-mode scope rules`. They're related (both about "scope" in some sense) but different. The file name `scope.go` is a bit overloaded. Could be flagged as a maintainability concern.

But this is minor. I'll skip it unless I have room.

OK, final list:

1. MEDIUM maintainability - `min` variable shadows Go built-in (postprocess.go:36)
2. MEDIUM correctness/robustness - Inconsistent severity normalization (postprocess.go:39,54)
3. LOW test coverage - stderr not captured (postprocess_test.go)
4. LOW maintainability - duplicated severityRank (postprocess.go:14)
5. LOW test fragility - NotContains assertion (scope_inject_test.go:27)

Let me write these in the required format.

Wait, I should also check the `enforceConstraints` function's behavior with respect to the `agent` parameter. The function takes `agent` as a string and uses it only for logging. The caller passes `r.Agent`. If `r.Agent` contains special characters, the `%q` format will quote it. This is fine for logging.

OK, writing the findings.

Actually, I want to reconsider issue #1 (variable shadowing). The `min` variable is in a tight scope (inside the `if floor := ...; floor != ""` block). The shadowing is local. Is this really a problem?

Go 1.21+ has `min(a, b)` as a built-in. In the scope where `min` is declared, the built-in is shadowed. But the function body in that scope doesn't use the built-in `min`. So there's no actual conflict.

However, if a future developer tries to use `min(a, b)` in that scope, they'll get the variable instead of the built-in. This is a footgun.

The fix is simple: rename `min` to `minRank` or `floorRank`. This is a minor improvement.

I'll keep it as MEDIUM maintainability.

OK, truly writing the findings now.

Let me also think about whether the `enforceConstraints` function's `min` variable name is confusing. The function has a parameter `minSeverity` and a local variable `min`. This is confusing — the reader might think `min` is related to `minSeverity`. Renaming to `minRank` would be clearer.

OK, writing the findings.

Let me format them now.

For the test fragility issue, let me look at the test more carefully:

```go
kaiAgent, _, err := buildAgent(cfg, "kai", payloads, rng)
require.NoError(t, err)
require.NotContains(t, kaiAgent.Prompt, "Review Focus", "unscoped agent prompt must be unchanged")
```

The assertion checks that the unscoped agent's prompt does NOT contain "Review Focus". This is checking that the scope block is not added when scope is empty. A more robust check would be to verify the exact content of the prompt (e.g., compare to a known good prompt). Or check that the prompt is unchanged from the default.

But the current check is reasonable — it verifies the specific marker is absent. If a future change adds "Review Focus" to a default persona, the test would fail, which is actually desirable (it would catch the unintended change).

Wait, that's actually a good thing — the test would fail if "Review Focus" is added to a default persona, which is what we want. So the test is not fragile; it's actually doing the right thing.

Hmm, let me reconsider. The test is checking that the unscoped agent's prompt does NOT contain "Review Focus". If a future change adds "Review Focus" to a default persona (e.g., as a section header for some other purpose), the test would fail incorrectly. But if the only reason "Review Focus" appears in a prompt is from the scope injection, then the test is correct.

The risk is that "Review Focus" might be used for other purposes in the future. This is a low risk. I'll keep it as LOW test fragility.

OK, writing the findings.

Actually, I realize I should also consider:

- The `enforceConstraints` function's behavior when the agent's `minSeverity` is set to an unknown value (bypassing validation). `severityRank[floor]` returns 0, and the filter `severityRank[f.Severity] >= 0` is always true, so no findings are dropped. This is fail-safe (no findings lost). Good.

- The `enforceConstraints` function's behavior when `maxFindings` is negative (bypassing validation). `len(findings) > -1` is always true (since len >= 0), so truncation always happens. `findings = findings[:-1]` would panic. Wait, no — `*maxFindings` is the dereferenced value, which is -1. So `findings = findings[:-1]` — but Go doesn't support negative indices. This would panic at runtime.

Wait, let me re-check:
```go
if maxFindings != nil && len(findings) > *maxFindings {
    ...
    findings = findings[:*maxFindings]
```

If `*maxFindings == -1` and `len(findings) == 5`, then `len(findings) > *maxFindings` is `5 > -1` which is true. Then `findings = findings[:-1]` — this is a runtime panic in Go (slice bounds out of range).

But the validation catches `*a.MaxFindings <= 0`, so this shouldn't happen in practice. It's a defensive concern.

I'll add this as a LOW robustness concern.

Actually, wait. The validation is `*a.MaxFindings <= 0`, which catches 0 and negatives. So in practice, `*maxFindings` is always > 0. The defensive concern is only if validation is bypassed.

I'll skip this — it's covered by validation.

OK, writing the findings.

Final list:

1. MEDIUM maintainability - `min` variable shadows Go built-in (postprocess.go:36)
2. MEDIUM correctness/robustness - Inconsistent severity normalization (postprocess.go:39,54)
3. LOW test coverage - stderr not captured (postprocess_test.go)
4. LOW maintainability - duplicated severityRank (postprocess.go:14)
5. LOW test fragility - NotContains assertion (scope_inject_test.go:27)

Let me write these in the required format.

Actually, I want to reconsider whether issue #5 is really a problem. The test is checking that the scope block is NOT added to an unscoped agent. The assertion `NotContains "Review Focus"` is a reasonable check. If a future change adds "Review Focus" to a default persona, the test would fail, which would alert the developer. This is actually a good test.

The only concern is if "Review Focus" is used for other purposes in the future, in which case the test would need to be updated. This is a minor concern. I'll keep it as LOW.

OK, writing the findings.

Hmm, I realize I should also flag the fact that the test for `enforceConstraints` doesn't verify the stderr output. This is a real gap for AC4. Let me make this MEDIUM or LOW.

AC4: "Fan-out logs warnings when output is truncated or findings are dropped (verified by checking fan-out logs for 'dropped X findings' or 'truncated to Y findings' messages)."

The test should verify this. I'll mark it as MEDIUM test coverage.

OK, final list:

1. MEDIUM maintainability - `min` variable shadows Go built-in (postprocess.go:36)
2. MEDIUM correctness/robustness - Inconsistent severity normalization (postprocess.go:39,54)
3. MEDIUM test coverage - stderr not captured for AC4 (postprocess_test.go)
4. LOW maintainability - duplicated severityRank (postprocess.go:14)
5. LOW test fragility - NotContains assertion (scope_inject_test.go:27)

Writing now.

Wait, I should also consider: is the `enforceConstraints` function's `findings[:0]` pattern safe? The function takes a slice and may mutate the backing array. The caller (`findingsFor`) doesn't hold a reference to the original slice after calling `enforceConstraints` (it just returns the result). So the mutation is safe.

But what if a caller does:
```go
original := parse(content)
filtered := enforceConstraints(original, ...)
fmt.Println(len(original)) // might be different from expected
```

This is a potential footgun. The doc says "The input slice may be reordered in place" which is correct but doesn't fully describe the mutation. This is a minor concern.

I'll skip this — it's a standard Go pattern and the doc is clear enough.

OK, writing the findings.

Let me also think about the `payload.ScopeFocus` function's behavior. The function returns a string that's appended to the prompt. The leading `\n\n` separates the block from whatever was rendered before. This is good practice for prompt injection.

But what if the persona template already ends with a newline? Then we'd have `\n\n## Review Focus` which is fine. What if the persona template ends without a newline? Then we'd have `...persona text\n\n## Review Focus` which is also fine. Good.

OK, writing the findings.

Final findings:

1. MEDIUM maintainability - `min` variable shadows Go built-in (postprocess.go:36)
2. MEDIUM correctness/robustness - Inconsistent severity normalization (postprocess.go:39,54)
3. MEDIUM test coverage - stderr not captured for AC4 (postprocess_test.go)
4. LOW maintainability - duplicated severityRank (postprocess.go:14)
5. LOW test fragility - NotContains assertion (scope_inject_test.go:27)

Let me write them in the required format now.

Actually, I want to reconsider issue #2 one more time. The function does:
```go
if severityRank[strings.ToUpper(f.Severity)] >= min {
```

If `f.Severity` is `"HIGH "` (trailing space), the lookup is `severityRank["HIGH "]` which is 0. The comparison `0 >= min` depends on `min`. If `min` is 1 (LOW floor), then `0 >= 1` is false, and the finding is dropped. This is wrong — a HIGH finding should not be dropped at a LOW floor.

This is a real correctness issue if the parser doesn't trim. Let me flag it as MEDIUM correctness.

The fix is to use `strings.ToUpper(strings.TrimSpace(f.Severity))` in the lookup.

OK, final findings:

1. MEDIUM maintainability - `min` variable shadows Go built-in (postprocess.go:36)
2. MEDIUM correctness - Model severity lookup doesn't trim whitespace (postprocess.go:39,54)
3. MEDIUM test coverage - stderr not captured for AC4 (postprocess_test.go)
4. LOW maintainability - duplicated severityRank (postprocess.go:14)
5. LOW test fragility - NotContains assertion (scope_inject_test.go:27)

Writing now.

For the format, I need:
- SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER

Let me write each finding:

1. MEDIUM|internal/fanout/postprocess.go:36|Variable `min` shadows Go 1.21+ built-in `min(a,b)`|Rename to `minRank` to avoid shadowing and improve clarity|maintainability|2|`min := severityRank[floor]` inside `if floor := ...; floor != ""` block shadows the built-in|claude

2. MEDIUM|internal/fanout/postprocess.go:39,54|Model output severity lookup uses ToUpper but not TrimSpace, inconsistent with the function's own minSeverity normalization|Consistently apply `strings.ToUpper(strings.TrimSpace(f.Severity))` in lookups to match the minSeverity normalization|correctness|5|A finding with trailing whitespace (e.g. "HIGH ") would be ranked 0 (unknown) and dropped at any min_severity floor|claude

3. MEDIUM|internal/fanout/postprocess_test.go|Tests don't capture stderr to verify AC4 warning logs when findings are dropped or truncated|Add a test that captures os.Stderr and asserts the "dropped" and "truncated" messages are emitted|maintainability|10|AC4 requires log verification; current tests only check return values, not the warning logs|claude

4. LOW|internal/fanout/postprocess.go:14|`severityRank` map is duplicated from internal/reconcile/merge.go:17|Extract a shared canonical severityRank in a neutral package and import from both|maintainability|15|If the rubric changes (e.g. adding INFO), both copies must be updated in lockstep|claude

5. LOW|internal/fanout/scope_inject_test.go:27|Test asserts `NotContains(t, kaiAgent.Prompt, "Review Focus")` which is fragile to future persona changes|Compare against a known-good baseline prompt or check for the specific scope section marker|maintainability|5|If a default persona template ever adds "Review Focus" for another purpose, the test breaks incorrectly|claude

Wait, I should use a more concise format. Let me check the required format again:

SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER

Where:
- PROBLEM: Brief description (< 100 chars)
- FIX: How to fix it (< 100 chars)
- EVIDENCE: A short code snippet or reason (keep it under ~20 words)

Let me tighten these up.

1. MEDIUM|internal/fanout/postprocess.go:36|Variable `min` shadows Go 1.21+ built-in `min(a,b)`|Rename to `minRank` to avoid shadowing|maintainability|2|`min := severityRank[floor]` shadows the built-in inside the if block|claude

2. MEDIUM|internal/fanout/postprocess.go:39|Model severity lookup uses ToUpper but not TrimSpace, inconsistent with minSeverity normalization|Apply `strings.ToUpper(strings.TrimSpace(f.Severity))` consistently|correctness|5|Finding "HIGH " would rank 0 and be dropped at any min_severity floor|claude

3. MEDIUM|internal/fanout/postprocess_test.go|Tests don't capture stderr to verify AC4 warning logs when findings are dropped/truncated|Add stderr-capturing test asserting "dropped" and "truncated" messages|maintainability|10|AC4 requires log verification; current tests only check return values|claude

4. LOW|internal/fanout/postprocess.go:14|`severityRank` map duplicated from internal/reconcile/merge.go:17|Extract shared canonical severityRank in a neutral package|maintainability|15|Rubric changes must be made in lockstep in two places|claude

5. LOW|internal/fanout/scope_inject_test.go:27|Test asserts NotContains "Review Focus" which is fragile to future persona changes|Compare against baseline prompt or check for specific scope marker|maintainability|5|If a default persona adds "Review Focus" for another purpose, test breaks|claude

OK these are good. Let me finalize.

Actually, for the reviewer name, the instructions say "Your agent name". The system prompt says I'm "an expert code reviewer". Let me use a reasonable name. Looking at the example: "bruce". I'll use "claude" or something similar. Actually, the example uses "bruce" which seems to be a persona. Let me just use "reviewer" or "claude". The example in the prompt uses "bruce" — I'll use a similar style. Let me use "code-reviewer" or just leave it as something descriptive.

Actually, looking at the example more carefully:
```
HIGH|src/auth.go:42|Missing input validation|Add length check|security|5|user input passed directly to query|bruce
```

The reviewer is "bruce". This seems to be a persona name from the registry. I'll use "claude" since that's my model name, or I could use something more generic. Let me just use "reviewer" to be safe.

Actually, the instructions say "REVIEWER: Your agent name". So I should use my own name. I'm Claude. Let me use "claude".

OK, finalizing.

Let me also reconsider whether I should flag the `min` shadowing as MEDIUM or LOW. It's a maintainability concern, not a bug. The shadowing is local and doesn't cause a bug today. But it could cause confusion for future developers. MEDIUM seems appropriate.

For the severity normalization issue, this is a potential correctness bug. If the parser doesn't trim, findings could be misclassified. MEDIUM seems appropriate.

For the stderr capture, this is a test coverage gap for AC4. MEDIUM seems appropriate.

For the duplicated severityRank, this is a design choice acknowledged in the doc. LOW seems appropriate.

For the fragile test assertion, this is a minor test fragility. LOW seems appropriate.

OK, final findings.

One more thing — I should double-check the `enforceConstraints` function's behavior with respect to the `min` variable. Let me trace through:

```go
func enforceConstraints(findings []stream.Finding, agent, minSeverity string, maxFindings *int) []stream.Finding {
    if len(findings) == 0 {
        return findings
    }

    // 1. Severity floor.
    if floor := strings.ToUpper(strings.TrimSpace(minSeverity)); floor != "" {
        min := severityRank[floor]  // <-- shadows built-in `min`
        kept := findings[:0]
        dropped := 0
        for _, f := range findings {
            if severityRank[strings.ToUpper(f.Severity)] >= min {  // <-- uses `min` (the variable)
                kept = append(kept, f)
            } else {
                dropped++
            }
        }
        findings = kept
        if dropped > 0 {
            fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: dropped %d finding(s) below min_severity %s\n", agent, dropped, floor)
        }
    }
    ...
```

The `min` variable is used in the comparison `severityRank[...] >= min`. This is the local variable, not the built-in. The code works correctly. The shadowing is just a naming concern.

If a future developer wants to use `min(a, b)` in this scope, they'd get the variable (rank 0-4) instead of the built-in. This is a footgun.

OK, the finding is valid. Writing it up.

Let me also think about whether the `enforceConstraints` function should be exported. The name starts with lowercase, so it's package-private. The `findingsFor` function is also package-private. Both are only used within the `fanout` package. This is correct for internal helpers.

OK, final findings.

Let me also consider: should I flag the fact that the `payload/scope.go` file now has two different "scope" concepts? The existing `// Per-payload-mode scope rules` comment is about the `{{.ScopeRule}}` template variable. The new `ScopeFocus` function is about per-agent scope categories. They're related (both about focusing the review) but different mechanisms.

The file name `scope.go` is now overloaded. This could be a maintainability concern. But it's not a bug, and the code works correctly.

I'll skip this unless I have room.

OK, truly finalizing.

Actually, I realize I should also check: is the `ScopeFocus` function's output correctly handled when the scope is set but the persona template doesn't have a trailing newline? The function returns `\n\n## Review Focus\n...`. If the persona template ends with `...persona text`, the result is `...persona text\n\n## Review Focus\n...`. This is fine.

If the persona template ends with `...persona text\n`, the result is `...persona text\n\n\n## Review Focus\n...`. Three newlines. This is still fine.

OK, no issue.

Let me also check: the `payload.ScopeFocus` function is called in `buildAgent` after the persona template renders. The result is appended to the prompt. The prompt is used for both `Agent.Prompt` and `Invocation.Prompt`. The test verifies both contain the scope. Good.

OK, writing the findings now.

One more consideration: the `enforceConstraints` function is called from `findingsFor` which is called for both single-shot and tool-loop agents. The tool-loop's `res` is updated in `loop.go` to include `MinSeverity` and `MaxFindings`. The single-shot's `Result` is set in `invokeSingleShot` in `engine.go`. Both are updated. Good.

But what about other call sites of `findingsFor`? Is there only one? I can't see the full codebase, but the test only shows one call site. The design is consistent.

OK, writing the findings.

Let me format them one more time:

1. MEDIUM|internal/fanout/postprocess.go:36|Variable `min` shadows Go 1.21+ built-in `min(a,b)`|Rename to `minRank` to avoid shadowing|maintainability|2|`min := severityRank[floor]` shadows the built-in inside the if block|claude

2. MEDIUM|internal/fanout/postprocess.go:39|Model severity lookup uses ToUpper but not TrimSpace, inconsistent with minSeverity normalization|Apply `strings.ToUpper(strings.TrimSpace(f.Severity))` consistently|correctness|5|Finding "HIGH " would rank 0 and be dropped at any min_severity floor|claude

3. MEDIUM|internal/fanout/postprocess_test.go|Tests don't capture stderr to verify AC4 warning logs when findings are dropped/truncated|Add stderr-capturing test asserting "dropped" and "truncated" messages|maintainability|10|AC4 requires log verification; current tests only check return values|claude

4. LOW|internal/fanout/postprocess.go:14|`severityRank` map duplicated from internal/reconcile/merge.go:17|Extract shared canonical severityRank in a neutral package|maintainability|15|Rubric changes must be made in lockstep in two places|claude

5. LOW|internal/fanout/scope_inject_test.go:27|Test asserts NotContains "Review Focus" which is fragile to future persona changes|Compare against baseline prompt or check for specific scope marker|maintainability|5|If a default persona adds "Review Focus" for another purpose, test breaks|claude

These look good. Let me output them.
</think>

MEDIUM|internal/fanout/postprocess.go:36|Variable `min` shadows Go 1.21+ built-in `min(a,b)`|Rename to `minRank` to avoid shadowing and improve clarity|maintainability|2|`min := severityRank[floor]` shadows the built-in inside the if block|claude
MEDIUM|internal/fanout/postprocess.go:39|Model severity lookup uses ToUpper but not TrimSpace, inconsistent with minSeverity normalization|Apply `strings.ToUpper(strings.TrimSpace(f.Severity))` consistently in lookups|correctness|5|Finding "HIGH " would rank 0 (unknown) and be dropped at any min_severity floor|claude
MEDIUM|internal/fanout/postprocess_test.go|Tests don't capture stderr to verify AC4 warning logs when findings are dropped/truncated|Add stderr-capturing test asserting "dropped" and "truncated" messages are emitted|maintainability|10|AC4 requires log verification; current tests only check return values, not warning logs|claude
LOW|internal/fanout/postprocess.go:14|`severityRank` map duplicated from internal/reconcile/merge.go:17|Extract shared canonical severityRank in a neutral package and import from both|maintainability|15|Rubric changes (e.g. adding INFO) must be made in lockstep in two places|claude
LOW|internal/fanout/scope_inject_test.go:27|Test asserts NotContains "Review Focus" which is fragile to future persona changes|Compare unscoped prompt against a captured baseline or check for specific scope marker|maintainability|5|If a default persona template ever adds "Review Focus" for another purpose, test breaks|claude