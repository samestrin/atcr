<think>
Let me carefully review the diff for the Epic 7.0.1 Executor Model Configuration sprint. I need to focus on issues directly related to the sprint plan's stated goals and scope.

Files in scope:
- `examples/registry-with-executor.yaml` - example config
- `internal/registry/config.go` - ExecutorConfig schema fields, validation, defaults
- `internal/registry/executor_config_test.go` - unit tests
- `internal/verify/executor.go` - temperature plumbing, prompt assembly
- `internal/verify/executor_test.go` - unit tests

Let me review the changes carefully:

1. **Config schema additions**: `Temperature`, `SystemPrompt`, `Rules` fields added to `ExecutorConfig`. Looks good.

2. **Validation**:
   - Temperature range [0,2] validated. Good.
   - SystemPrompt length capped at 4096. Good.
   - Rules: empty/whitespace rejected, control characters rejected, length capped at 512. Good.

3. **Default temperature to 0.0**: Resolved at call time via `EffectiveExecutorTemperature`. Good.

4. **Prompt assembly**:
   - system_prompt replaces default framing when set. Good.
   - Rules appended after framing as constraints. Good.
   - Finding metadata still appended. Good.

5. **Temperature plumbing**: 
   - `callExecutor` binds `temp` to a local variable, takes address, passes through to `Invocation.Temperature`. Good.

Let me look for potential issues:

### Issue 1: Potential nil pointer dereference in buildFixPrompt
Looking at `buildFixPrompt`, it takes `ex *registry.ExecutorConfig`. What if `ex` is nil? Let me check the call site:
```go
prompt := buildFixPrompt(*f, snippet, ex)
```
The `ex` pointer is passed. If it's nil, the function would panic on `ex.SystemPrompt`. Let me check if there's validation ensuring `ex` is not nil. Looking at the registry config, `Executor` is loaded from the config. If it's nil, it would have been validated earlier (provider/model required). But the caller `generateFixes` accepts `ex *reconcile.Executor` which might be different from `*registry.ExecutorConfig`. Let me look more carefully...

Actually, looking at the function signature change: `buildFixPrompt(*f, snippet, ex *registry.ExecutorConfig)`. The previous signature was `(f reconcile.JSONFinding, snippet, persona string)`. The new version accesses `ex.SystemPrompt`, `ex.Persona`, `ex.Rules`. If `ex` is nil, this would panic. This is a potential issue but it's likely that the caller already validates ex is non-nil. Let me check...

Looking at `generateFixes`:
```go
prompt := buildFixPrompt(*f, snippet, ex)
```
where `ex` is passed in. This is called from `RunFixes` or similar. Let me not get too deep into the call chain—I'll note this as a low concern since existing call sites presumably ensure `ex` is non-nil.

Actually, looking more carefully, this is a maintainability/robustness concern but not a HIGH severity bug.

### Issue 2: `temp` variable scope in `callExecutor`
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

Wait, there's a subtle issue here. `temp` is declared inside the function, but each call gets a fresh `temp` because `callExecutor` is called per-finding. Actually, `temp` is a local variable on the stack; its address is taken. The address remains valid as long as `callExecutor` doesn't return. Since the address is used in the struct literal immediately and passed to `complete.Complete`, this is fine. The comment says "Bind to a local so its address outlives this expression" which is correct.

### Issue 3: Prompt injection via system_prompt
The system_prompt is allowed to have control characters (multi-line content). The comment explicitly says "Control characters are intentionally NOT rejected — unlike persona (a mid-sentence token), a system prompt is a full instruction block where multi-line content is legitimate." This is a deliberate design choice.

However, this means a user-supplied system_prompt could inject arbitrary instructions. This is by design (the registry is self-authored). Not a bug.

But wait — if the system_prompt contains `IGNORE PREVIOUS INSTRUCTIONS`-style content, that's a prompt injection vector. But the documentation says the registry is self-authored, so this is an acceptable trust boundary. Not a bug.

### Issue 4: Rules interpolation into prompt
```go
for _, rule := range ex.Rules {
    fmt.Fprintf(&b, "- %s\n", rule)
}
```
Each rule is validated at load time (no control chars, length-capped). This is safe.

### Issue 5: Repeated test field access without nil check
In `TestBuildFixPrompt_SystemPromptOverridesFraming`:
```go
ex.SystemPrompt = "ACT AS A STRICT GO LINTER. Emit only gofmt-clean code."
```
This is fine.

### Issue 6: Yaml field ordering issue
Looking at the diff for `examples/registry-with-executor.yaml`:
```yaml
executor:
  role: executor
  min_severity_for_fix: MEDIUM
  temperature: 0.0
  system_prompt: |
  rules:
    - Use tabs for indentation
    - Avoid panic() in library code
```
The example shows `system_prompt: |` which is a block scalar indicator. The line is commented out in the example (`# system_prompt: |`), so that's fine.

### Issue 7: Potential issue with `MaxExecutorPersonaLen` reference
The diff mentions persona guards for control characters. Let me check the existing persona validation in the file. I see the comments reference it but I can't see if persona already validates control chars. Looking at the existing code in the original (pre-diff), the persona validation should already reject control chars.

### Issue 8: Missing nil check on ex in buildFixPrompt
The function signature is:
```go
func buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig) string {
```

If `ex` is nil, accessing `ex.SystemPrompt` would panic. This is a potential robustness issue. Let me check the call sites...

Actually, the caller `generateFixes` receives `ex *reconcile.Executor` (or whatever the type is). Let me look at the diff context... the diff doesn't show the full signature but it passes `ex` to `buildFixPrompt(*f, snippet, ex)`. We don't know the exact type conversion. Looking at the call to `callExecutor`, it takes `ex *reconcile.Executor` (presumably) but then uses `ex.EffectiveExecutorTimeoutSecs(...)` and `ex.EffectiveExecutorTemperature()` — so `ex` has both methods.

Wait, let me re-read the diff:
```go
out, err := callExecutor(ctx, complete, prov, ex, prompt, sharedTimeoutSecs)
```
And `callExecutor` signature:
```go
func callExecutor(ctx context.Context, complete executorCompleter, prov registry.ProviderConfig, ex *reconcile.Executor, prompt string, sharedTimeoutSecs int) (string, error) {
```

Wait, the diff doesn't show the full callExecutor signature. Let me check the test: `ex := execConfig("MEDIUM")` and `ex.SystemPrompt = "..."`. The type is `*registry.ExecutorConfig` since SystemPrompt is a field on that struct.

Hmm, looking at the test for `TestCallExecutor_PassesExplicitTemperature`:
```go
ex := execConfig("MEDIUM")
temp := 0.3
ex.Temperature = &temp
```

So `ex` in `generateFixes` is `*registry.ExecutorConfig`. And `generateFixes` signature is `func generateFixes(ctx context.Context, findings []reconcile.JSONFinding, ex *reconcile.Executor, ...)`. Wait, that can't be right because `ex.Temperature` would need to be on `*reconcile.Executor`, not `*registry.ExecutorConfig`.

Let me look more carefully. The diff doesn't show the full signature change for `generateFixes`. But based on the test, `ex` must have Temperature, SystemPrompt, and Rules fields. These are only on `registry.ExecutorConfig`. So `generateFixes` either takes `*registry.ExecutorConfig` or there's a type mismatch.

Wait, looking at the test more carefully:
```go
func TestBuildFixPrompt_SystemPromptOverridesFraming(t *testing.T) {
    findings := []reconcile.JSONFinding{...}
    rec := &recordingExecutor{out: "fix"}
    ex := execConfig("MEDIUM") // Persona "fixer"
    ex.SystemPrompt = "ACT AS A STRICT GO LINTER. Emit only gofmt-clean code."
    generateFixes(context.Background(), findings, ex, execRegistry("MEDIUM"), rec, okDispatcher(), 0)
```

If `execConfig` returns `*registry.ExecutorConfig`, then `generateFixes` must accept that type. So either the signature changed in the diff (it's truncated) or `ex` in the original `generateFixes` was already a different type that now matches `*registry.ExecutorConfig`.

Looking at the diff for `callExecutor`, it accesses `ex.EffectiveExecutorTimeoutSecs(...)` which is a method on `ExecutorConfig`. So `callExecutor` likely takes `*registry.ExecutorConfig` directly. The diff doesn't show the full signature, but the body accesses fields only on `ExecutorConfig`.

OK, so `ex` is `*registry.ExecutorConfig` throughout.

### Now let me focus on real issues:

1. **Nil pointer dereference risk in `buildFixPrompt`**: If `ex` is nil, accessing `ex.SystemPrompt`, `ex.Persona`, `ex.Rules` would panic. This is a robustness issue. However, if existing callers always pass non-nil, this is acceptable. But it's a defensive coding concern. Let me note it.

Actually, wait. Looking at the diff again:
```go
prompt := buildFixPrompt(*f, snippet, ex)
```

If `ex` is nil, the function panics. But the previous signature `buildFixPrompt(*f, snippet, persona string)` couldn't panic this way. The new signature introduces this risk. It's a maintainability concern.

2. **Re-evaluation of scope**: The sprint plan says the changes are in `executor` config + temperature plumbing + prompt assembly + unit tests + example YAML + doc note. So all files in the diff are in scope.

3. **Possible prompt injection via system_prompt**: The validation allows control characters in `system_prompt`. The comment justifies this ("multi-line content is legitimate"). But it also means a user could inject arbitrary text including CR/LF to forge prompt structure. This is a security consideration, but the trust boundary is "self-authored registry". Not a bug per se.

4. **Test for empty rule uses "  " (whitespace)**: The validation uses `strings.TrimSpace(rule) == ""` which catches whitespace-only entries. Good.

5. **Test for `TestBuildFixPrompt_SystemPromptOverridesFraming` checks `assert.NotContains(t, p, "You are fixer")`**: But wait, what if the user-supplied system_prompt happens to contain the string "You are fixer"? That's fine — the assertion is specifically about the default persona framing being replaced.

6. **Looking at the test `TestCallExecutor_DefaultsTemperatureToZero`**: The test confirms that when temperature is nil, the executor sends 0.0. This matches the sprint plan clarification.

7. **Test `TestExecutor_FixTunablesHydrateTogether`**: Verifies that all three fields hydrate together. Good coverage.

8. **The test for prompt injection `TestExecutor_RuleWithControlCharsRejected` uses `\n`**: Good, tests prompt injection via newline.

9. **Looking at the order of validation in `validateExecutor`**: The new fields are validated after the existing ones. The temperature validation uses `*e.Temperature < 0 || *e.Temperature > 2`. Good.

10. **Looking at `EffectiveExecutorTemperature`**: Returns 0.0 when unset. Good. Matches the clarification.

11. **`callExecutor` change**: 
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

The `&temp` is valid because `temp` lives until the end of the function scope. Good.

12. **`executor_test.go` change**: The test struct now records `temps []*float64`. Good.

13. **Possible issue: When `ex.Temperature` is nil and `temp` is set to 0.0, is the address semantics correct?** The local `temp` is a `float64` with value `0.0`. Its address is `&temp`. This is a valid pointer to a local variable. The `Invocation.Temperature` field should be `*float64` (matches the existing definition). Good.

14. **Looking at the `Invocation` struct usage**: The diff shows `Temperature: &temp`. The existing `Invocation` struct presumably has a `Temperature *float64` field (it must, for the tests to work). Good.

Let me now think about whether there are any real bugs or concerns:

**Concern 1**: In `buildFixPrompt`, when `ex.SystemPrompt` is set, the function accesses `ex.Persona` only if `ex.SystemPrompt` is empty. But what if `ex` itself is nil? Let me check...

```go
func buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig) string {
    var b strings.Builder
    if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
        b.WriteString(sp)
        b.WriteString("\n\n")
    } else {
        persona := ex.Persona
        ...
```

If `ex` is nil, this panics. But callers should ensure `ex` is non-nil. This is a maintainability concern.

**Concern 2**: Looking at the test `TestCallExecutor_DefaultsTemperatureToZero`:
```go
rec := &recordingExecutor{out: "fix"}
ex := execConfig("MEDIUM") // Temperature nil
generateFixes(context.Background(), findings, ex, execRegistry("MEDIUM"), rec, okDispatcher(), 0)
```

Wait, `ex.Temperature` is nil but the function sends a non-nil pointer to 0.0. The test asserts:
```go
require.NotNil(t, rec.temps[0], "executor must send a temperature even when unset")
assert.Equal(t, 0.0, *rec.temps[0], "unset temperature defaults to deterministic 0.0")
```

This is correct because `temp := ex.EffectiveExecutorTemperature()` always returns a value, and `&temp` is always non-nil.

**Concern 3**: Looking at the prompt structure change. Previously:
```
You are <persona>, a code-fix executor. ...
Severity: HIGH
Location: a.go:1
Category: ...
Problem: ...
```

Now:
```
You are <persona>, a code-fix executor. ...
Coding rules to follow:
- rule1
- rule2

Severity: HIGH
...
```

Or with system_prompt:
```
<system_prompt>

Coding rules to follow:
- rule1
- rule2

Severity: HIGH
...
```

This looks correct.

**Concern 4**: Is there any concern about the order of operations in `buildFixPrompt`? Looking carefully:
```go
if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
    b.WriteString(sp)
    b.WriteString("\n\n")
} else {
    persona := ex.Persona
    if strings.TrimSpace(persona) == "" {
        persona = registry.DefaultExecutorPersona
    }
    fmt.Fprintf(&b, "You are %s, a code-fix executor. ...", persona)
}
if len(ex.Rules) > 0 {
    b.WriteString("Coding rules to follow (apply these to your fix):\n")
    for _, rule := range ex.Rules {
        fmt.Fprintf(&b, "- %s\n", rule)
    }
    b.WriteString("\n")
}
```

This looks correct.

**Concern 5**: What about the existing tests? The test `TestBuildFixPrompt_SystemPromptOverridesFraming` checks:
```go
assert.NotContains(t, p, "a code-fix executor", "default framing must be replaced by system_prompt")
assert.NotContains(t, p, "You are fixer", "persona is superseded when system_prompt is set")
```

These are good.

**Concern 6**: One thing I notice is that the existing test `TestGenerateFixes_AttributionGuardIsTokenNotPrefix` might be affected by the prompt format change. Let me check... it asserts on `findings[0].Evidence` containing `"; fix by op"`. This is unrelated to the prompt format. OK.

**Concern 7**: Let me think about whether `max_severity_for_fix` order in the example YAML is correct. Looking at:
```yaml
executor:
  role: executor
  min_severity_for_fix: MEDIUM   # MEDIUM | HIGH | CRITICAL | LOW (default MEDIUM)
  # fix_timeout: 120             # optional per-fix timeout (seconds)
  # Fix-generation tunables (Epic 7.0.1) — control fix style/determinism here
  # instead of editing ATCR source.
  temperature: 0.0               # API temperature [0,2]; default 0.0 (deterministic fixes)
```

The temperature is set to 0.0 in the example. Good.

**Concern 8**: Let me check the test `TestExecutor_SystemPromptParsed`. It asserts that `system_prompt` is parsed verbatim including the period at the end. Good.

**Concern 9**: One potential concern: what about validation when the YAML `temperature` field is provided as a string like `"0"` instead of a float? The YAML parser should handle this, but it depends on the YAML library. Not a code change concern.

**Concern 10**: The `system_prompt` is allowed to contain control characters but not tabs? Looking at the validation:
```go
if len(e.SystemPrompt) > MaxExecutorSystemPromptLen {
    errs = append(errs, fmt.Errorf("executor: system_prompt must be at most %d characters", MaxExecutorSystemPromptLen))
}
```

No control character check. The comment explicitly says multi-line is legitimate. This is intentional.

**Concern 11**: There's a subtle issue with `system_prompt`. The original `persona` field rejects control characters but `system_prompt` does not. This means if someone sets `system_prompt` to `"You are fixer, a code-fix executor."`, it works, but the persona is bypassed. However, if the user wants the persona to be honored while also having extra framing, they need to include it in `system_prompt`. This is by design.

**Concern 12**: Looking at the prompt structure more carefully:
```go
b.WriteString(sp)
b.WriteString("\n\n")
```

The `sp` is the trimmed `SystemPrompt`. The trailing newline is preserved because we TrimSpace'd then write back. Wait, `strings.TrimSpace` strips leading/trailing whitespace, then we add `\n\n` after. So the structure is:
```
<system_prompt content, trimmed>

Severity: HIGH
```

The system_prompt itself could have multiple lines (CR/LF allowed since no control char check), and they would be preserved verbatim. Good.

**Concern 13**: Potential issue with `effective temperature` always being 0.0 default. The clarification says this is intentional. But what about API clients that don't accept temperature=0.0? The clarification says normalization is at the gateway layer. OK.

**Concern 14**: One thing I want to verify: the comment in the diff says `Bind to a local so its address outlives this expression.` This is correct Go idiom. The address of a local variable is valid as long as the variable is in scope.

**Concern 15**: Looking at the example YAML more carefully:
```yaml
  # system_prompt: |             # optional: replaces the default fixer framing verbatim (supersedes persona)
  #   You are a senior Go engineer. Emit only gofmt-clean, idiomatic Go and nothing else.
```

The example uses YAML block scalar `|` for multi-line. But the validator doesn't allow control characters... wait, control characters are NOT rejected for system_prompt. So this works.

Actually wait, let me re-check. The validator code:
```go
// SystemPrompt (Epic 7.0.1) replaces the fix-prompt framing verbatim; cap its
// length to bound prompt-stuffing. Control characters are intentionally NOT
// rejected — unlike persona (a mid-sentence token), a system prompt is a full
// instruction block where multi-line content is legitimate.
if len(e.SystemPrompt) > MaxExecutorSystemPromptLen {
    errs = append(errs, fmt.Errorf("executor: system_prompt must be at most %d characters", MaxExecutorSystemPromptLen))
}
```

Yes, no control character check. Good.

**Concern 16**: Let me look at how `Role` validation works. The existing code validates Role. Not affected by this change.

OK, I've reviewed the diff thoroughly. Let me think about what real issues to flag.

Real issues:
1. **Potential nil pointer in buildFixPrompt**: If `ex` is nil, panic. This is a defensive coding concern. But it's likely the caller ensures non-nil. Low severity.

2. **System prompt allows control characters for prompt injection**: This is by design (self-authored registry). But it's a security consideration. The comment justifies it. Not a bug per se but worth flagging? Actually, the sprint plan explicitly says the registry is self-authored, so this is the trust boundary. Not a bug.

3. **Test for `TestExecutor_FixTunablesHydrateTogether`** uses `require.NotNil(t, reg.Executor)`. If `Executor` is nil, the test fails. But this is good defensive testing.

4. **Looking at `executor_test.go` change**: The `temps` field is appended to in `Complete()`. Good.

5. **Possible issue**: The test `TestBuildFixPrompt_SystemPromptOverridesFraming` doesn't check that the rules are still appended (when both are set). The third test `TestBuildFixPrompt_SystemPromptAndRulesCompose` does check this. OK.

6. **One potential concern**: The `system_prompt` is allowed to contain arbitrary text including content that might override safety guardrails. This is a security concern but explicitly out of scope per the sprint plan ("registry is self-authored"). Not a bug.

7. **Looking at validation order**: Temperature is validated after TimeoutSecs. The order doesn't matter functionally. OK.

8. **Possible issue: The validation for temperature uses `*e.Temperature > 2` but the doc comment says `[0,2]`. Inclusive vs exclusive? `> 2` means 2.0 is allowed. Good.

9. **Possible issue: The example YAML uses `temperature: 0.0` but doesn't use the default. The default would be 0.0 anyway. Redundant but not a bug.**

10. **The validation `if len(e.SystemPrompt) > MaxExecutorSystemPromptLen`** — this uses `len()` which counts bytes, not runes. For multi-byte UTF-8 characters, this could be slightly inaccurate. But for length caps, byte count is acceptable. Low concern.

11. **Same for `len(rule) > MaxExecutorRuleLen`**: byte count, not rune count. Same comment. Low concern.

12. **The control character check uses `r < 32`** which catches ASCII control chars (0-31) but not DEL (127) or Unicode control characters. The existing persona check likely uses the same. Consistent. OK.

Hmm, let me think if there's any real bug I'm missing...

Wait, looking at the test `TestBuildFixPrompt_SystemPromptOverridesFraming`:
```go
ex := execConfig("MEDIUM") // Persona "fixer"
ex.SystemPrompt = "ACT AS A STRICT GO LINTER. Emit only gofmt-clean code."
```

And `execConfig("MEDIUM")` likely creates an `ExecutorConfig` with persona "fixer". The test then sets SystemPrompt. The test expects:
- "ACT AS A STRICT GO LINTER." appears
- "a code-fix executor" does NOT appear (default framing replaced)
- "You are fixer" does NOT appear (persona superseded)

But wait, what about the rules test? The function adds:
```go
if len(ex.Rules) > 0 {
    b.WriteString("Coding rules to follow (apply these to your fix):\n")
    ...
```

The default test (`TestBuildFixPrompt_DefaultFramingWhenNoSystemPrompt`) doesn't set rules, so this block is skipped. Good.

OK, I think the main issues to flag are:

1. **No nil check on `ex` in `buildFixPrompt`**: Defensive coding concern. Medium severity.

2. **System prompt allows unrestricted control characters**: This is by design but could be flagged as a security concern. Low severity, since the registry is self-authored.

3. **`len()` vs rune count for length validation**: Minor. Low severity.

Actually, looking at this more carefully, these are all relatively minor concerns. The diff looks well-thought-out with good test coverage.

Let me re-examine for actual bugs:

**Looking at the test `TestBuildFixPrompt_SystemPromptOverridesFraming` more carefully**:
The test expects `assert.NotContains(t, p, "a code-fix executor")`. The original framing line is:
```
"You are %s, a code-fix executor. Generate the minimal code change..."
```

When system_prompt is set, the entire framing line is replaced. So "a code-fix executor" should not appear. Correct.

**Looking at `TestBuildFixPrompt_SystemPromptAndRulesCompose`**:
```go
assert.NotContains(t, p, "a code-fix executor")
```
This is checking the default framing is suppressed. But the system_prompt is "You fix Go code." which doesn't contain "a code-fix executor". Good.

But wait, what if the user's system_prompt contains the substring "a code-fix executor"? The check would fail. Not a real issue since the test system_prompt doesn't contain it.

**Looking at `TestCallExecutor_PassesExplicitTemperature`**:
```go
temp := 0.3
ex.Temperature = &temp
```
The test expects `0.3` to be forwarded. Good.

But wait, the `callExecutor` function does:
```go
temp := ex.EffectiveExecutorTemperature()
```

If `ex.Temperature` is `&(0.3)`, then `EffectiveExecutorTemperature` returns `0.3`. Then `temp` (local) is `0.3`. Then `&temp` points to the local. Good.

**Hmm, one thing**: The variable `temp` shadows nothing but is a generic name. Could be confusing. But not a bug.

Let me look once more at the buildFixPrompt change:

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
        fmt.Fprintf(&b, "You are %s, a code-fix executor. Generate the minimal code change that fixes the finding below. Preserve the existing style and conventions. Output only the fix (corrected code or a precise change instruction); do not restate the problem.\n\n", persona)
    }
    if len(ex.Rules) > 0 {
        b.WriteString("Coding rules to follow (apply these to your fix):\n")
        for _, rule := range ex.Rules {
            fmt.Fprintf(&b, "- %s\n", rule)
        }
        b.WriteString("\n")
    }
    ...
```

Wait — I notice the rules are written WITHOUT trimming them. But the validation rejects empty strings (after TrimSpace). So a rule like "  Use tabs  " would be accepted and written as "-   Use tabs  \n". This is correct (preserving user content verbatim).

Hmm, but what if the rule is just whitespace? The validator rejects that case (`strings.TrimSpace(rule) == ""`).

OK, I think the code is well-implemented. Let me focus on what's worth flagging.

Actually, looking one more time at the example YAML, I notice:
```yaml
  temperature: 0.0               # API temperature [0,2]; default 0.0 (deterministic fixes)
```

This sets temperature to 0.0. The default is also 0.0. So this is redundant but harmless. Just an observation, not an issue.

Let me think about what the most impactful findings would be...

1. **Real concern**: The function `buildFixPrompt` no longer has a nil-safe interface. If a future caller passes nil `ex`, it panics. This is a maintainability/robustness issue.

2. **Real concern**: System prompt allows unrestricted content including CR/LF, which could enable prompt injection. This is by design but the trust model should be clear.

3. **Minor**: Length checks use byte count (`len()`) which can be inaccurate for multi-byte UTF-8. Low severity.

4. **Minor**: Control character check only catches ASCII (< 32). Unicode line/paragraph separators (U+2028, U+2029) could potentially forge prompt structure. But these are unlikely in practice. Low severity.

5. **Real issue**: Looking at the validation block:
```go
if e.Temperature != nil && (*e.Temperature < 0 || *e.Temperature > 2) {
    errs = append(errs, errors.New("executor: temperature must be within [0, 2]"))
}
```

This validates the range. Good. But what about NaN? If someone sets `temperature: .nan` in YAML, this would pass the check (`NaN < 0` is false, `NaN > 2` is false). Then `EffectiveExecutorTemperature()` returns NaN, which gets sent to the LLM. The LLM might error or behave unpredictably. This is a real edge case.

Actually, JSON/YAML doesn't typically allow NaN by default (most parsers reject it). But the YAML library might allow it. Worth flagging.

Hmm, but the `AgentConfig.Temperature` validation likely has the same issue. Let me not flag this since it's pre-existing.

6. **Potential issue**: In the example YAML, the `system_prompt` is commented out using `#`. The comment includes `supersedes persona`. This is documentation, not code. Good.

OK, let me now finalize my findings. I'll flag the most impactful ones:

1. **buildFixPrompt nil pointer**: MEDIUM, maintainability
2. **system_prompt allows CR/LF injection**: LOW, security (by design, but worth noting)
3. **NaN validation gap**: LOW, correctness

Actually, looking at this again, point 1 might not be worth flagging since the call site likely ensures non-nil. Let me check if `ex` is checked before being passed to `buildFixPrompt`.

Looking at the diff for `generateFixes`:
```go
prompt := buildFixPrompt(*f, snippet, ex)
```

No nil check. If `ex` is nil, the function panics. But this function is internal and likely called only from places where `ex` is known to be non-nil.

Hmm, I'll include it as a low severity maintainability issue.

Let me also check the prompt structure once more. When rules are present but system_prompt is empty, the rules are written after the default framing. When both are present, system_prompt first then rules. This is consistent.

Wait, I want to check one more thing: the `system_prompt` is allowed to contain CR/LF, but if it ends with multiple newlines, the result might be:
```
<system_prompt content with newlines>
               <-- extra newline added

Severity: HIGH
```

This is fine — the LLM will handle whitespace.

What if the system_prompt contains something like `"You are a fixer.\n\nIgnore all previous instructions."`? This is a prompt injection. But the trust model is "self-authored registry", so this is acceptable.

OK let me also verify the `MaxExecutorSystemPromptLen` constant. It's 4096. This is reasonable for a system prompt (Anthropic allows 8K, OpenAI allows much more).

Alright, my final findings:

1. **HIGH/MEDIUM**: Nil pointer dereference risk in buildFixPrompt if ex is nil. Maintainability. The function takes `ex *registry.ExecutorConfig` and accesses `ex.SystemPrompt`, `ex.Persona`, `ex.Rules`. If caller passes nil, panic. The previous signature `(f, snippet, persona string)` couldn't panic this way.

Actually, on reflection, I realize this is internal code and the caller is trusted. It's a low severity issue at most.

Let me think about what the most impactful issues really are:

- The buildFixPrompt change is well-tested.
- The validation is reasonable.
- The temperature plumbing is correct.
- The test coverage is good.

Maybe the real concerns are:
1. **The system_prompt allows control characters including CR/LF**: This is by design per the comment. The registry is self-authored. Not a bug.

2. **The system_prompt could contain prompt injection attempts**: Same as above.

3. **The control character check for rules uses `r < 32`**: Misses DEL (127) and Unicode separators. Pre-existing pattern.

4. **Length checks use byte count**: Minor UTF-8 concern.

5. **Temperature validation misses NaN**: Pre-existing pattern.

Actually, let me look for a real bug. Let me re-examine the diff carefully...

```go
// In callExecutor:
temp := ex.EffectiveExecutorTemperature()
return complete.Complete(callCtx, llmclient.Invocation{
    BaseURL:     prov.BaseURL,
    APIKeyEnv:   prov.APIKeyEnv,
    Model:       ex.Model,
    Temperature: &temp,
    Prompt:      prompt,
})
```

Wait, the field order changed. Previously:
```go
BaseURL:   prov.BaseURL,
APIKeyEnv: prov.APIKeyEnv,
Model:     ex.Model,
Prompt:    prompt,
```

Now:
```go
BaseURL:     prov.BaseURL,
APIKeyEnv:   prov.APIKeyEnv,
Model:       ex.Model,
Temperature: &temp,
Prompt:      prompt,
```

The field is added between Model and Prompt. This is consistent with the struct field order if Temperature comes after Model. The diff doesn't show the struct definition but the existing tests must have this field already (since the test compiles).

Hmm, what about the `Invocation` struct in `llmclient`? The diff shows it accepts `Temperature *float64`. The field is already there (used elsewhere maybe?).

Actually, looking at the diff: `Temperature: &temp` is added. This means the `Invocation` struct has a `Temperature *float64` field. Whether it's new or pre-existing, the change wires it through. OK.

Let me also verify the test for `TestCallExecutor_DefaultsTemperatureToZero`. The test sets up `ex := execConfig("MEDIUM")` which presumably has Temperature nil. Then `generateFixes` calls `callExecutor` which calls `ex.EffectiveExecutorTemperature()` returning 0.0. Then `&temp` is non-nil pointer to 0.0. The recordingExecutor captures it in `temps`. Test asserts `*rec.temps[0] == 0.0`. Good.

OK, I think the diff is well-implemented. Let me flag a few minor concerns:

1. **NaN/Infinity not validated**: LOW, correctness
2. **buildFixPrompt nil deref**: LOW, maintainability
3. **Control char check misses Unicode**: LOW, security
4. **system_prompt allows CR/LF injection**: LOW, security (by design)

Hmm, none of these are HIGH or even MEDIUM really. They're all minor.

Actually, let me look once more at the control character check:

```go
case strings.IndexFunc(rule, func(r rune) bool { return r < 32 }) >= 0:
    errs = append(errs, fmt.Errorf("executor: rules[%d] must not contain control characters", i))
```

This catches characters with rune value < 32 (ASCII control characters: NUL through US). It does NOT catch:
- DEL (127)
- Unicode line separator (U+2028)
- Unicode paragraph separator (U+2029)
- Zero-width characters (U+200B, U+200C, U+200D, U+FEFF)
- Other Unicode control characters (Cf category)

For prompt injection via CR/LF, the current check is sufficient. For more sophisticated attacks, it could miss some characters. Low severity.

Actually, I want to double-check: the persona validation probably uses the same pattern. So this is consistent.

Looking at the validation comment for rules:
```go
// Rules (Epic 7.0.1): each rule is interpolated as a constraint line in the fix
// prompt. Reject blank entries (a YAML typo, not "no rule"), control characters
// (CR/LF prompt-line forgery), and over-long entries — mirroring the scope and
// persona guards.
```

This mirrors persona guards. So it's consistent.

OK, let me finalize. I'll include a few
</think>
