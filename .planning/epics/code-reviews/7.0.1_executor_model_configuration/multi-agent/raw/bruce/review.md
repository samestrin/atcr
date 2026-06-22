<think>
Let me carefully analyze the diff for issues within the scope of the sprint plan (Epic 7.0.1: Executor Model Configuration).

The sprint plan covers:
1. `ExecutorConfig` schema fields (`temperature`, `system_prompt`, `rules`) + validation + defaults
2. Temperature plumbing through `llmclient.Invocation`
3. Prompt assembly (`system_prompt` override + `rules` block)
4. Unit tests
5. Example YAML + doc note

Let me trace through the changes:

### `examples/registry-with-executor.yaml`
- Adds `temperature`, `system_prompt`, and `rules` fields under `executor:`. Looks good.

### `internal/registry/config.go`
- Adds `MaxExecutorSystemPromptLen = 4096` and `MaxExecutorRuleLen = 512` constants.
- Adds `Temperature *float64`, `SystemPrompt string`, `Rules []string` to `ExecutorConfig`.
- Adds `EffectiveExecutorTemperature()` method.
- Adds validation for temperature range, system_prompt length, and rules (empty, control chars, length).

Let me check the validation logic carefully:

```go
for i, rule := range e.Rules {
    switch {
    case strings.TrimSpace(rule) == "":
        errs = append(errs, fmt.Errorf("executor: rules[%d] must not be empty", i))
    case strings.IndexFunc(rule, func(r rune) bool { return r < 32 }) >= 0:
        errs = append(errs, fmt.Errorf("executor: rules[%d] must not contain control characters", i))
    case len(rule) > MaxExecutorRuleLen:
        errs = append(errs, fmt.Errorf("executor: rules[%d] must be at most %d characters", i, MaxExecutorRuleLen))
    }
}
```

Wait - `r < 32` would also match the null character `\0` (rune 0) and the tab `\t` (rune 9). But more importantly, the `switch` statement is wrong here. In Go, a `switch` without a fall-through will only execute the first matching case. But with `case` statements that each have conditions, it would only execute the first matching case. So if a rule is both empty AND has control characters, only the empty error would be reported. That's actually fine behavior.

But wait - there's a subtle issue. The `switch` in Go with conditions on each case works like a series of if/else. So if `strings.TrimSpace(rule) == ""` is true, it executes that case and breaks out. If not, it checks the next case. So this is fine.

Actually wait, I need to re-read the control character check. `r < 32` would catch all control characters (0-31), but not DEL (127). But the persona guard might use a different range. Let me check the diff for the persona guard... The diff doesn't show it, but the comment says "mirroring the scope and persona guards". This might be consistent with existing code.

But here's a potential issue: `r < 32` would also catch `\t` (tab, 9), `\n` (newline, 10), and `\r` (carriage return, 13), which are the main ones that could forge prompt lines. But it would also reject tabs and newlines in rules, which is probably the intent (the comment says "CR/LF prompt-line forgery"). The system_prompt explicitly allows these (the comment says "Control characters are intentionally NOT rejected — unlike persona (a mid-sentence token), a system prompt is a full instruction block where multi-line content is legitimate").

Wait, actually `r < 32` catches r=0 through r=31. The common control chars are:
- 0: NUL
- 9: TAB
- 10: LF
- 13: CR

So this would reject tabs in rules. But the system_prompt allows them. Is this a problem? The sprint says "rules are coding guidelines appended to the fix prompt as constraints" - and the validation comment says "Control characters are intentionally NOT rejected — unlike persona (a mid-sentence token), a system prompt is a full instruction block where multi-line content is legitimate." This implies that rules DO reject control characters.

But there's an inconsistency: the system_prompt comment says control characters are NOT rejected, but a system_prompt with embedded control characters could still do prompt injection. The reasoning given (multi-line content) is weak - multi-line content is also legitimate for rules (though perhaps less so). This is a design decision that might be inconsistent but is within scope.

Actually, wait - there's a more serious issue. The system_prompt is NOT checked for control characters, which means someone could inject prompt-line forgery through system_prompt. The comment justifies this as "multi-line content is legitimate" but the persona guard DOES reject control characters. This is potentially a security gap, but it might be an intentional design decision per the sprint.

### `internal/verify/executor.go`
The `callExecutor` function changes:

```go
temp := ex.EffectiveExecutorTemperature()
return complete.Complete(callCtx, llmclient.Invocation{
    BaseURL:     prov.BaseURL,
    APIKeyEnv:   prov.APIKeyEnv,
    Model:       ex.Model,
    Temperature: &temp,
    Prompt:      prompt,
})
```

The comment says "Bind to a local so its address outlives this expression." This is correct - you can't take the address of a function return value directly. The pointer to the local variable is correct.

The `buildFixPrompt` function changes:

```go
func buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig) string {
    var b strings.Builder
    if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
        b.WriteString(sp)
        b.WriteString("\n\n")
    } else {
        persona := ex.Persona
        if strings.TrimSpace(persona) == "" {
            persona = registry.DefaultExecutorPersona
        }
        fmt.Fprintf(&b, "You are %s, a code-fix executor. ...\n\n", persona)
    }
    if len(ex.Rules) > 0 {
        b.WriteString("Coding rules to follow (apply these to your fix):\n")
        for _, rule := range ex.Rules {
            fmt.Fprintf(&b, "- %s\n", rule)
        }
        b.WriteString("\n")
    }
    ...
}
```

Issues to check:
1. When system_prompt is set, the trimmed system_prompt is written (not the original). If the user has a system_prompt with leading/trailing whitespace, the trimmed version is used. This is probably fine.

2. The system_prompt could contain control characters (not checked at load). If a system_prompt contains a CR/LF, it could forge prompt lines. This is a security concern.

3. Rules are appended after the framing and before the finding metadata. The sprint says "appended to the executor's context prompt prior to fix generation" - this is implemented correctly.

4. The `generateFixes` call site changes from `buildFixPrompt(*f, snippet, ex.Persona)` to `buildFixPrompt(*f, snippet, ex)` - correct signature change.

5. The `Temperature: &temp` line takes the address of a local variable `temp` in `callExecutor`. This is fine as long as `temp` is not modified after the call.

Wait, let me re-check the call to `buildFixPrompt`. The signature changed from `buildFixPrompt(f reconcile.JSONFinding, snippet, persona string)` to `buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig)`. The call site was updated correctly.

Let me also check the test files for correctness.

### `internal/registry/executor_config_test.go`
The tests look correct. Let me check a few:

- `TestExecutor_FixTunablesHydrateTogether` - checks all fields hydrate together.
- `TestExecutor_TemperatureParsed` - checks explicit 0.0 temperature survives.
- `TestExecutor_TemperatureOutOfRangeRejected` - checks 3.5 is rejected.
- `TestExecutorConfig_EffectiveExecutorTemperature` - checks the resolver.
- `TestExecutor_SystemPromptParsed` - checks system_prompt is parsed.
- `TestExecutor_SystemPromptOverLengthRejected` - checks length cap.
- `TestExecutor_RulesParsed` - checks rules are parsed.
- `TestExecutor_RuleEmptyRejected` - checks blank rules are rejected.
- `TestExecutor_RuleWithControlCharsRejected` - checks control chars are rejected.
- `TestExecutor_RuleOverLengthRejected` - checks rule length cap.

### `internal/verify/executor_test.go`
- `TestCallExecutor_PassesExplicitTemperature` - checks explicit temperature is forwarded.
- `TestCallExecutor_DefaultsTemperatureToZero` - checks unset temperature defaults to 0.0.
- `TestBuildFixPrompt_SystemPromptOverridesFraming` - checks system_prompt replaces default.
- `TestBuildFixPrompt_RulesAppended` - checks rules are appended.
- `TestBuildFixPrompt_SystemPromptAndRulesCompose` - checks both compose.
- `TestBuildFixPrompt_DefaultFramingWhenNoSystemPrompt` - checks default is retained.

These look correct.

Now let me think about potential issues:

1. **System prompt control character check is missing**: The validation only checks length for system_prompt, not control characters. The comment says "Control characters are intentionally NOT rejected" but this could be a security concern since system_prompt is interpolated verbatim into the prompt. However, the sprint clarification says "system_prompt fully replaces the default framing line verbatim and persona is superseded for that call" - so the design intent is to allow multi-line content. This is within scope as a design decision, but is a potential security concern.

2. **Temperature validation uses `*e.Temperature < 0 || *e.Temperature > 2`**: This is correct for the [0,2] range.

3. **The `temp` variable in `callExecutor`**: The comment says "Bind to a local so its address outlives this expression." This is correct Go practice.

4. **System prompt with leading/trailing whitespace**: The code uses `strings.TrimSpace(ex.SystemPrompt)` to check if it's non-empty, but then writes the ORIGINAL `ex.SystemPrompt` to the builder. This means a system_prompt of `"   hello   "` would be written as `"   hello   "` (with whitespace). The trimming is only used for the emptiness check. This is probably fine.

Wait, let me re-read:
```go
if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
    b.WriteString(sp)
    b.WriteString("\n\n")
}
```

`sp` is the trimmed version, and `sp` is what's written. So the trimmed version IS written, not the original. This is fine.

5. **Rules validation uses `switch` with conditions**: This works correctly in Go - it's like a chain of if/elseif/elseif. Only the first matching case executes.

6. **The `r < 32` check for control characters**: This catches all C0 control characters (0-31) but not DEL (127). The persona guard might use a different range. Let me check if there's a potential issue with this. The comment says "mirroring the scope/persona guard" so presumably this is consistent with existing code.

7. **The `MaxExecutorRuleLen = 512`**: The comment says "mirroring the persona cap" (which is 512). This is consistent.

8. **The `MaxExecutorSystemPromptLen = 4096`**: The comment says "It is larger than the persona cap because it is a full instruction block." This makes sense.

9. **The `EffectiveExecutorTemperature()` method**: This is a value receiver, not a pointer receiver. The other effective methods (`EffectiveFixMinSeverity`, `EffectiveExecutorTimeoutSecs`) are also value receivers, so this is consistent.

10. **The `callExecutor` function**: The `temp` variable is created with `ex.EffectiveExecutorTemperature()` which returns a `float64`. The address of `temp` is then taken. This is correct.

Actually, wait. Let me look at this more carefully:

```go
temp := ex.EffectiveExecutorTemperature()
return complete.Complete(callCtx, llmclient.Invocation{
    ...
    Temperature: &temp,
    ...
})
```

`temp` is a `float64` local variable. `&temp` is a `*float64`. The `llmclient.Invocation.Temperature` field is presumably `*float64` (based on the comment in the test "nil = inherit shared timeout" and the existing `omitempty` marshaling). This should work.

But wait - `temp` goes out of scope when the function returns. If the `Invocation` struct holds a reference to `temp` and uses it after the function returns, that's a use-after-free. But since `Invocation` is passed by value to `Complete()`, the pointer is copied, but the value it points to is on the stack of `callExecutor`. If `Complete()` is synchronous (which it appears to be based on the context handling), the value is read before `callExecutor` returns, so this is safe.

Actually, looking at the test:
```go
r.temps = append(r.temps, inv.Temperature)
```

The test stores the pointer in `r.temps`. The `recordingExecutor.Complete` is called synchronously within `callExecutor`, so the pointer is valid during the call. After `Complete` returns, the pointer is stored in `r.temps` for later assertion. But by that time, `callExecutor` has returned and `temp` is out of scope.

Wait, this is actually a potential issue! The test does:
```go
r.temps = append(r.temps, inv.Temperature)
...
assert.Equal(t, 0.3, *rec.temps[0], ...)
```

The test reads `*rec.temps[0]` after `callExecutor` has returned. If `temp` was on the stack of `callExecutor`, it could be overwritten or invalid by the time the test reads it.

But wait - Go's escape analysis would see that `&temp` is passed to a function that might store it, so `temp` would be allocated on the heap (escape analysis). So this is actually safe in Go because the compiler would allocate `temp` on the heap.

Let me verify: the comment says "Bind to a local so its address outlives this expression." This suggests the author was aware of this issue. The Go compiler's escape analysis would detect that `&temp` escapes (because it's stored in a slice in the test), so `temp` would be heap-allocated. This is correct.

11. **The `buildFixPrompt` function signature change**: The function now takes `*registry.ExecutorConfig` instead of a `string` persona. The call site in `generateFixes` is updated correctly.

12. **The `callExecutor` function**: The signature is unchanged. The `ex` parameter is `*registry.ExecutorConfig`, so it has access to `Temperature`.

Let me now think about what issues to report:

1. **System prompt control character check is missing**: This is a potential security concern. The system_prompt is interpolated verbatim into the prompt, and could contain CR/LF for prompt-line forgery. The comment justifies this as "multi-line content is legitimate" but the persona guard DOES reject control characters. This inconsistency could be a finding.

However, looking at the sprint plan more carefully:
- "system_prompt, when set, fully replaces the default framing line"
- "system_prompt is length-capped (multi-line framing is legitimate there)"

This is an intentional design decision per the sprint. So flagging it as a security issue would be within scope but might be a design choice.

2. **The `r < 32` check for control characters in rules**: This is correct and consistent with the sprint's security requirements.

3. **The `switch` statement in rules validation**: This works correctly in Go.

4. **The `temp` variable binding**: This is correct Go practice.

5. **The `EffectiveExecutorTemperature` method**: This is a value receiver, consistent with other effective methods.

Let me think about what else could be an issue...

Actually, I notice that in the `buildFixPrompt` function, the system_prompt is written to the builder with `b.WriteString(sp)`, where `sp` is the trimmed system_prompt. But then `b.WriteString("\n\n")` is added. This means the system_prompt is followed by two newlines. This is correct for prompt formatting.

The rules section writes:
```go
b.WriteString("Coding rules to follow (apply these to your fix):\n")
for _, rule := range ex.Rules {
    fmt.Fprintf(&b, "- %s\n", rule)
}
b.WriteString("\n")
```

This adds a blank line after the rules. This is correct for prompt formatting.

Let me check if there are any issues with the test for `TestBuildFixPrompt_SystemPromptAndRulesCompose`:
- The test sets `ex.SystemPrompt = "You fix Go code."` and `ex.Rules = []string{"No global state"}`.
- The test asserts that "You fix Go code." appears, "No global state" appears, and "a code-fix executor" does NOT appear.
- The system_prompt replaces the default framing, and the rules are appended. This should work.

Wait, there's a subtle issue. The test `TestBuildFixPrompt_SystemPromptAndRulesCompose` does:
```go
assert.NotContains(t, p, "a code-fix executor")
```

But the system_prompt is "You fix Go code." which doesn't contain "a code-fix executor". The default framing is "You are fixer, a code-fix executor. Generate the minimal code change..." which DOES contain "a code-fix executor". Since the system_prompt replaces the default, the default framing is not present, so "a code-fix executor" should not appear. This is correct.

Let me also check the test `TestBuildFixPrompt_SystemPromptOverridesFraming`:
```go
assert.NotContains(t, p, "a code-fix executor", "default framing must be replaced by system_prompt")
assert.NotContains(t, p, "You are fixer", "persona is superseded when system_prompt is set")
```

The system_prompt is "ACT AS A STRICT GO LINTER. Emit only gofmt-clean code." which doesn't contain "a code-fix executor" or "You are fixer". The default framing would contain both. Since the system_prompt replaces the default, neither should appear. This is correct.

Now, let me think about potential issues more carefully:

1. **The `buildFixPrompt` function no longer trims the system_prompt before writing**: Actually, it does - `sp := strings.TrimSpace(ex.SystemPrompt)` and then `b.WriteString(sp)`. So the trimmed version is written.

2. **The `rules` validation uses `strings.TrimSpace(rule) == ""` for the empty check**: This correctly handles whitespace-only rules.

3. **The `rules` validation uses `strings.IndexFunc(rule, func(r rune) bool { return r < 32 })` for control characters**: This catches all C0 control characters but not DEL (127). The persona guard might use a different range. If the persona guard uses `r < 32 || r == 127`, then the rules guard is inconsistent. But without seeing the persona guard code, I can't confirm this.

4. **The temperature validation `*e.Temperature < 0 || *e.Temperature > 2`**: This is correct for the [0,2] range. It uses `*e.Temperature` to dereference the pointer, which is safe because the check is inside `if e.Temperature != nil`.

5. **The `EffectiveExecutorTemperature` method**: This is a value receiver. The comment says "It mirrors EffectiveFixMinSeverity / EffectiveExecutorTimeoutSecs: the single resolver so the nil → 0.0 fallback lives in one place." This is consistent with the existing pattern.

Let me look for any other issues...

Actually, I want to double-check the `switch` statement in the rules validation:

```go
for i, rule := range e.Rules {
    switch {
    case strings.TrimSpace(rule) == "":
        errs = append(errs, fmt.Errorf("executor: rules[%d] must not be empty", i))
    case strings.IndexFunc(rule, func(r rune) bool { return r < 32 }) >= 0:
        errs = append(errs, fmt.Errorf("executor: rules[%d] must not contain control characters", i))
    case len(rule) > MaxExecutorRuleLen:
        errs = append(errs, fmt.Errorf("executor: rules[%d] must be at most %d characters", i, MaxExecutorRuleLen))
    }
}
```

In Go, a `switch` without an expression and with conditions on each case works like a chain of if/elseif/elseif. Only the first matching case executes. So:
- If a rule is empty, only the "must not be empty" error is reported.
- If a rule has control characters but is not empty, only the "must not contain control characters" error is reported.
- If a rule is over-length but is not empty and has no control characters, only the "must be at most N characters" error is reported.

This is correct behavior. However, it means a rule could have multiple issues but only one error is reported. This is a minor UX issue but not a correctness issue.

Actually, wait. I want to reconsider the system_prompt control character issue. The sprint plan says:

> "system_prompt fully replaces the default framing line verbatim and persona is superseded for that call"

And the validation comment says:
> "Control characters are intentionally NOT rejected — unlike persona (a mid-sentence token), a system prompt is a full instruction block where multi-line content is legitimate."

This is an explicit design decision. The system_prompt is meant to be a full instruction block that can contain multi-line content. However, this means a malicious system_prompt could contain CR/LF to forge prompt lines. But the registry is "self-authored" (as noted in the untrusted-data boundary comment), so the blast radius is bounded.

Given that this is an explicit design decision documented in the sprint plan, I should probably not flag it as a security issue. But I could flag it as a maintainability concern (inconsistency with persona guard).

Let me also check if there are any issues with the `callExecutor` function:

```go
temp := ex.EffectiveExecutorTemperature()
return complete.Complete(callCtx, llmclient.Invocation{
    ...
    Temperature: &temp,
    ...
})
```

The `temp` variable is bound to the address of a local variable. The comment says "Bind to a local so its address outlives this expression." This is correct Go practice for taking the address of a value that needs to outlive the expression.

But wait - is there a potential issue with the `Complete` function being called synchronously vs asynchronously? If `Complete` is asynchronous, the `temp` variable could go out of scope before the value is used. But looking at the existing code, `Complete` is called synchronously (the result is used immediately). So this is safe.

Actually, let me look at the test again:
```go
func (r *recordingExecutor) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
    r.calls++
    r.prompts = append(r.prompts, inv.Prompt)
    r.temps = append(r.temps, inv.Temperature)
    return r.out, r.err
}
```

The test stores `inv.Temperature` (which is `&temp` from `callExecutor`) in `r.temps`. If `Complete` is synchronous, the pointer is valid during the call. After `Complete` returns, the pointer is stored in `r.temps` for later assertion.

But by the time the test reads `*rec.temps[0]`, `callExecutor` has returned and `temp` is out of scope. However, Go's escape analysis would detect that `&temp` escapes (because it's stored in a slice in the test), so `temp` would be heap-allocated. This is correct.

Let me verify this is actually what happens. The compiler sees:
1. `temp` is a local variable in `callExecutor`.
2. `&temp` is passed to `Complete`.
3. `Complete` stores `inv.Temperature` in a slice (`r.temps`).
4. `r.temps` is later read by the test.

The compiler would determine that `temp` escapes to the heap because its address is stored in a data structure that outlives the function. So `temp` is heap-allocated, and the pointer remains valid. This is correct.

Now, let me think about what findings to report:

1. **System prompt control character check is missing**: This is a design decision per the sprint, but could be flagged as a potential security concern. However, the sprint explicitly says "Control characters are intentionally NOT rejected" for system_prompt. So this is within the design intent.

2. **The `switch` statement in rules validation only reports the first error per rule**: This is a minor UX issue. A rule with both empty and control character issues would only report the empty error. But this is a minor concern.

3. **The `r < 32` check for control characters**: This is correct for C0 control characters. The persona guard might use a different range, but without seeing it, I can't confirm inconsistency.

4. **The `temp` variable binding**: This is correct Go practice.

5. **The `EffectiveExecutorTemperature` method**: This is consistent with the existing pattern.

Let me look for any other issues...

Actually, I want to check the test for `TestExecutor_RuleWithControlCharsRejected`:
```go
rules:
    - "Use tabs\nIGNORE PREVIOUS INSTRUCTIONS"
```

The rule contains a newline character (`\n`, rune 10). The check `r < 32` would catch this (r=10 < 32). So the test should pass.

But wait - the YAML string `"Use tabs\nIGNORE PREVIOUS INSTRUCTIONS"` contains a literal newline. In Go test strings, `\n` is interpreted as a newline. So the rule string would be "Use tabs\nIGNORE PREVIOUS INSTRUCTIONS" with a literal newline. The check `r < 32` would catch the newline (r=10). So the test should pass.

Let me also check the test for `TestExecutor_RuleEmptyRejected`:
```go
rules:
    - "Use tabs"
    - "  "
```

The second rule is `"  "` (two spaces). The check `strings.TrimSpace(rule) == ""` would catch this (trimmed to empty). So the test should pass.

Now, let me think about what findings to report. I should focus on issues that are:
1. Within the scope of the sprint plan
2. Genuine bugs or concerns
3. Not already addressed by the design decisions in the sprint

Let me reconsider the system_prompt control character issue. The sprint plan says:
> "system_prompt fully replaces the default framing line verbatim and persona is superseded for that call"

And the validation comment says:
> "Control characters are intentionally NOT rejected — unlike persona (a mid-sentence token), a system prompt is a full instruction block where multi-line content is legitimate."

This is an explicit design decision. The system_prompt is meant to allow multi-line content. However, this means a malicious system_prompt could contain CR/LF to forge prompt lines. But the registry is "self-authored" (as noted in the untrusted-data boundary comment), so the blast radius is bounded.

Given that this is an explicit design decision, I should probably not flag it as a security issue. But I could note it as a potential concern.

Actually, let me re-read the sprint plan's "Clarifications" section:

> "system_prompt, when set, fully replaces the default framing line ("You are {persona}, a code-fix executor...") in buildFixPrompt. persona is superseded for that call. Finding metadata, source snippet, and rules are still appended after the custom framing."

This confirms that system_prompt is meant to be a full framing block. The validation comment about not rejecting control characters is consistent with this design.

However, there's a subtle issue: the system_prompt could contain CR/LF to forge prompt lines, and this is not checked. But the sprint says the registry is "self-authored" and the output only lands in the Fix column. So the blast radius is bounded.

I think the main issues to report are:

1. **The `switch` statement in rules validation only reports the first error per rule**: This is a minor UX issue. If a rule has both empty and control character issues, only the empty error is reported. But this is a minor concern.

Actually, I realize I should also check if there are any issues with the temperature validation. The sprint says:
> "Temperature handling mirrors the existing AgentConfig.Temperature pattern: *float64, validated to the range 0.0..2.0, passed through unchanged."

The validation `*e.Temperature < 0 || *e.Temperature > 2` is correct for the [0,2] range. But wait - should it be `*e.Temperature < 0.0 || *e.Temperature > 2.0`? In Go, `0` and `2` are untyped constants that can be compared with `float64`. So `*e.Temperature < 0` is equivalent to `*e.Temperature < 0.0`. This is correct.

Let me also check if there's an issue with the `EffectiveExecutorTemperature` method. The method returns `0.0` when `e.Temperature` is nil. But the comment says "the executor's own temperature when set, otherwise the deterministic 0.0 default". This is correct.

Now, let me think about what findings to report. I want to be thorough but not pedantic.

Actually, I just noticed something. In the `buildFixPrompt` function, the system_prompt is written with `b.WriteString(sp)`, where `sp` is the trimmed system_prompt. But then `b.WriteString("\n\n")` is added. This means the system_prompt is followed by two newlines.

But what if the system_prompt already ends with a newline? Then there would be three newlines. This is a minor formatting issue but not a correctness issue.

Let me also check if there's an issue with the rules section. The rules are written with `fmt.Fprintf(&b, "- %s\n", rule)`. This adds a "- " prefix and a newline after each rule. Then `b.WriteString("\n")` adds a blank line after the rules section. This is correct.

Actually, I want to check one more thing. The `buildFixPrompt` function signature changed from:
```go
func buildFixPrompt(f reconcile.JSONFinding, snippet, persona string) string
```
to:
```go
func buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig) string
```

The call site in `generateFixes` was updated from:
```go
prompt := buildFixPrompt(*f, snippet, ex.Persona)
```
to:
```go
prompt := buildFixPrompt(*f, snippet, ex)
```

This is correct. But I should check if there are any other callers of `buildFixPrompt`. Let me search...

Looking at the diff, there's only one call site in `generateFixes`. The tests in `executor_test.go` call `generateFixes`, not `buildFixPrompt` directly. So there's no other caller to update.

Wait, but the test `TestGenerateFixes_SnippetEmbeddedInPrompt` and the new tests call `generateFixes`, not `buildFixPrompt`. So the signature change is only reflected in the `generateFixes` call site. This is correct.

Let me now think about what findings to report. I'll focus on:

1. **The `switch` statement in rules validation only reports the first error per rule**: This is a minor maintainability concern. A rule with multiple issues would only have one error reported.

Actually, I realize I should also check if the `buildFixPrompt` function handles the case where `ex` is nil. The function signature is `buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig)`. If `ex` is nil, accessing `ex.SystemPrompt` would panic. But looking at the call site, `ex` is always non-nil (it's the executor config from the registry). So this is safe.

Let me also check if there's an issue with the `callExecutor` function. The function takes `ex *registry.ExecutorConfig`. If `ex` is nil, accessing `ex.EffectiveExecutorTemperature()` would panic. But looking at the call site, `ex` is always non-nil. So this is safe.

OK, I think I've analyzed this thoroughly. Let me now write up my findings.

Actually, wait. I want to check one more thing. The `EffectiveExecutorTemperature` method is a value receiver:
```go
func (e ExecutorConfig) EffectiveExecutorTemperature() float64 {
```

This means it operates on a copy of the `ExecutorConfig`. Since `ExecutorConfig` contains a `*float64` (pointer), the copy shares the same pointer. This is fine for reading the temperature value.

But wait - the method is called on `ex` which is `*registry.ExecutorConfig` (pointer to ExecutorConfig). In Go, you can call a value receiver method on a pointer, and the compiler will automatically dereference. So `ex.EffectiveExecutorTemperature()` is equivalent to `(*ex).EffectiveExecutorTemperature()`. This is correct.

Now, let me think about what findings to report. I want to be thorough but not pedantic.

Actually, I just realized something. The `callExecutor` function does:
```go
temp := ex.EffectiveExecutorTemperature()
return complete.Complete(callCtx, llmclient.Invocation{
    ...
    Temperature: &temp,
    ...
})
```

The `temp` variable is a `float64`. `&temp` is a `*float64`. The `llmclient.Invocation.Temperature` field is presumably `*float64`. This is correct.

But wait - the comment says "Bind to a local so its address outlives this expression." This suggests the author was aware that you can't take the address of a function return value. Let me verify: in Go, you can't do `&ex.EffectiveExecutorTemperature()` because the return value is not addressable. So you need to bind it to a local variable first. This is correct.

OK, I think I've analyzed this thoroughly. Let me now write up my findings.

Let me think about what the most important issues are:

1. **The `switch` statement in rules validation only reports the first error per rule**: This is a minor maintainability concern. A rule with multiple issues would only have one error reported. However, this is a common pattern in Go and not really a bug.

2. **The system_prompt does not validate control characters**: This is an explicit design decision per the sprint, but could be a security concern. However, the registry is self-authored, so the blast radius is bounded.

3. **The `r < 32` check for control characters**: This is correct for C0 control characters. The persona guard might use a different range, but without seeing it, I can't confirm inconsistency.

Actually, I want to reconsider issue #1. The `switch` statement in Go with conditions on each case works like a chain of if/elseif/elseif. So:
- If a rule is empty, only the "must not be empty" error is reported.
- If a rule has control characters but is not empty, only the "must not contain control characters" error is reported.
- If a rule is over-length but is not empty and has no control characters, only the "must be at most N characters" error is reported.

This is actually fine behavior. It's not a bug, just a design choice. The first error is the most important one to report.

Let me think about what other issues there might be...

Actually, I want to check if there's an issue with the `buildFixPrompt` function when both system_prompt and rules are set. The system_prompt replaces the default framing, and the rules are appended after. This is correct per the sprint.

But wait - the sprint says "system_prompt, when set, fully replaces the default framing line ('You are {persona}, a code-fix executor...') in buildFixPrompt. persona is superseded for that call. Finding metadata, source snippet, and rules are still appended after the custom framing."

So the order should be:
1. System prompt (or default framing with persona)
2. Rules (if any)
3. Finding metadata
4. Source snippet

Looking at the code:
```go
if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
    b.WriteString(sp)
    b.WriteString("\n\n")
} else {
    persona := ex.Persona
    if strings.TrimSpace(persona) == "" {
        persona = registry.DefaultExecutorPersona
    }
    fmt.Fprintf(&b, "You are %s, a code-fix executor. ...\n\n", persona)
}
if len(ex.Rules) > 0 {
    b.WriteString("Coding rules to follow (apply these to your fix):\n")
    for _, rule := range ex.Rules {
        fmt.Fprintf(&b, "- %s\n", rule)
    }
    b.WriteString("\n")
}
fmt.Fprintf(&b, "Severity: %s\nLocation: %s:%d\nCategory: %s\nProblem: %s\n", f.Severity, f.File, f.Line, f.Category, f.Problem)
...
```

This is correct. The order is: framing (system_prompt or default), rules, finding metadata, source snippet.

OK, I think I've analyzed this thoroughly. Let me now write up my findings.

Actually, I want to check one more thing. The `callExecutor` function creates a new `Invocation` struct with `Temperature: &temp`. But what if `llmclient.Invocation.Temperature` is not `*float64`? Let me check the diff...

The diff
</think>
