The user wants me to review a diff based on a sprint plan. I need to check for issues in the modified code that fall within the scope of the sprint plan.

Sprint plan summary:
- Epic 7.0.1: Executor Model Configuration
- Extend `executor` configuration schema to allow developers to control fix generation behavior
- Add `temperature` (float, default 0.0), `system_prompt` (string), `rules` (array of strings)
- AC1: Schema supports these fields
- AC2: If temperature specified, passed correctly to LLM client
- AC3: If rules provided, appended to executor's context prompt prior to fix generation
- AC4: Unit tests verify configuration values correctly hydrate request payloads
- Temperature defaults to deterministic low value (0.0) if omitted
- `system_prompt` fully replaces default framing line when set; persona superseded; metadata/snippet/rules still appended after
- `temperature` plumbs through `llmclient.Invocation` which already has `Temperature` field marshaled `omitempty`
- `EffectiveExecutorTemperature` helper resolves default 0.0 when nil
- Control-char guards + length caps for `system_prompt` and each `rules` entry mirroring existing `persona` guards
- Temperature validated to [0,2]
- Must map temperature abstractly across providers

Files in diff:
- examples/registry-with-executor.yaml
- internal/registry/config.go
- internal/registry/executor_config_test.go
- internal/verify/executor.go
- internal/verify/executor_test.go

Let me review each file for issues.

**1. examples/registry-with-executor.yaml**
- Added temperature, system_prompt, rules. Looks fine.

**2. internal/registry/config.go**
- Added constants MaxExecutorSystemPromptLen = 4096, MaxExecutorRuleLen = 512. Good.
- Added fields to ExecutorConfig: Temperature *float64, SystemPrompt string, Rules []string. Good.
- Added EffectiveExecutorTemperature() method. Returns *e.Temperature if not nil, else 0.0. Good.
- Added validation in validateExecutor:
  - Temperature: checks if not nil and < 0 or > 2. Good.
  - SystemPrompt: checks len > MaxExecutorSystemPromptLen. Good. But wait - the comment says "Control characters are intentionally NOT rejected — unlike persona (a mid-sentence token), a system prompt is a full instruction block where multi-line content is legitimate." This is intentional per the sprint plan clarification: "control-char guards + length caps for system_prompt and each rules entry, mirroring the existing persona guards". Wait, the sprint plan says "mirroring the existing persona guards" which include control-char guards? Let me re-read the sprint plan.
  
  Sprint plan Technical Approach: "New fields follow the AgentConfig precedent (pointer for temperature so unset is distinguishable from 0.0; control-char guards + length caps for system_prompt and each rules entry, mirroring the existing persona guards)."
  
  However, in the clarification it says: "system_prompt, when set, fully replaces the default framing line". The code in config.go comment says: "Control characters are intentionally NOT rejected — unlike persona (a mid-sentence token), a system prompt is a full instruction block where multi-line content is legitimate."
  
  But the sprint plan Technical Approach explicitly says "control-char guards + length caps for system_prompt". This seems like a contradiction. However, the sprint plan also has a risk table and clarifications. The comment in the code explains the reasoning: multi-line is legitimate for system_prompt. But the sprint plan technical approach says control-char guards. Let me think if this is an issue.
  
  Wait, the sprint plan says: "control-char guards + length caps for system_prompt and each rules entry, mirroring the existing persona guards". But the code intentionally does NOT reject control characters for system_prompt. Is this a deviation? The comment says it's intentional. But the sprint plan says to mirror persona guards. However, the sprint plan is a plan, and the clarification section might have overridden it? The clarification section doesn't explicitly mention control chars for system_prompt. It only discusses length. Hmm. Actually, looking at the risk/clarification: there's no explicit mention of control chars. But the technical approach in the sprint plan says control-char guards. This might be an inconsistency, but is it a code issue? Probably not worth flagging as a bug since the comment explains the rationale and it's a design decision. But wait - could control characters in system_prompt be a security issue? The prompt is built with system_prompt verbatim. If someone injects newlines, they could potentially mess with the prompt structure. However, the code accepts multi-line content intentionally. I don't think I should flag this as an issue unless there's a clear bug.

  - Rules validation: checks empty (after trim), control characters (r < 32), and length > 512. Good.

Wait, is there an issue with the `EffectiveExecutorTemperature` method? It returns 0.0 if nil. The sprint plan says "Temperature defaults to a deterministic low value if omitted" and "The executor's effective temperature defaults to 0.0 (deterministic) when omitted." So that's correct.

Is there an issue with the pointer and the local variable binding in executor.go?
```go
temp := ex.EffectiveExecutorTemperature()
return complete.Complete(callCtx, llmclient.Invocation{
    ...
    Temperature: &temp,
    ...
})
```
This binds the temperature to a local variable and takes its address. This is correct and necessary because if we did `Temperature: &ex.EffectiveExecutorTemperature()` it wouldn't compile (can't take address of function return). So that's fine.

Wait, what about `omitempty` on `llmclient.Invocation.Temperature`? The sprint plan says "Temperature is sent on every executor call... the resolver supplies the deterministic 0.0 default when temperature is unset". But if `llmclient.Invocation.Temperature` is marshaled with `omitempty`, and the temperature is 0.0, will it be omitted? The sprint plan says: "temperature plumbs through the existing Invocation.Temperature field (already marshaled omitempty); only callExecutor needs to pass the executor's effective temperature."

If the field has `omitempty` and the value is 0.0, then when marshaling to JSON, a pointer to 0.0 might be omitted if the JSON tag has `omitempty`. Let me think: in Go's `encoding/json`, `omitempty` on a pointer field means omit if nil. Wait, no - for pointers, `omitempty` omits if the pointer is nil. It does NOT omit if the pointer is non-nil, even if the pointed-to value is the zero value. Let me verify: yes, for pointers, `omitempty` only omits nil pointers. A pointer to 0.0 is not nil, so it will be marshaled as `0.0`. So that's fine.

But wait - in `callExecutor`, the code always sends `&temp`. The temp is always non-nil (either explicit value or 0.0). So `omitempty` will always include it. That matches the sprint plan requirement: "Temperature is sent on every executor call... the deterministic 0.0 default when temperature is unset". Good.

Let me look at `buildFixPrompt` changes:
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
        fmt.Fprintf(&b, "You are %s, a code-fix executor....", persona)
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
Wait, there's a potential nil pointer dereference here! The function signature changed from `buildFixPrompt(f reconcile.JSONFinding, snippet, persona string)` to `buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig)`. It now takes a pointer to ExecutorConfig. In the caller:
```go
prompt := buildFixPrompt(*f, snippet, ex)
```
Where `ex` is `*registry.ExecutorConfig` from the function parameter. But is `ex` ever nil? Let's look at `generateFixes`:
```go
func generateFixes(ctx context.Context, findings []reconcile.JSONFinding, ex *registry.ExecutorConfig, reg *registry.Registry, complete executorCompleter, disp Dispatcher, sharedTimeoutSecs int) {
```
`ex` is passed as a pointer. If it's nil, then inside `buildFixPrompt`, accessing `ex.SystemPrompt` will panic. But this is probably guarded elsewhere. Let's check if there's any nil check. In the original code, `ex.Persona` was accessed directly too, so maybe `ex` is never nil in practice. But with the new code, if `ex` is nil, it will panic on `ex.SystemPrompt`. However, this isn't a new issue introduced by the diff per se, since the old code also used `ex` pointer. Wait, the old `buildFixPrompt` didn't take `ex`, it took `persona string`. The caller was:
```go
prompt := buildFixPrompt(*f, snippet, ex.Persona)
```
If `ex` was nil, `ex.Persona` would panic in the old code too. So this isn't a new regression. Not worth flagging.

Wait, another issue: in `buildFixPrompt`, when `ex.SystemPrompt` is set, it uses `strings.TrimSpace(ex.SystemPrompt)` and if not empty, writes it followed by `\n\n`. What if `ex.SystemPrompt` is just whitespace? Then it falls through to the persona branch. That's probably fine.

But what about the rules? `fmt.Fprintf(&b, "- %s\n", rule)` - if a rule contains format verbs like `%s`, it would be interpreted by Fprintf. Wait, `rule` is a string passed as an argument to Fprintf with format `"- %s\n"`. Since the format string is constant and `%s` is the only verb, and `rule` is a string, this is safe. It won't interpret verbs inside `rule` because `%s` prints the string verbatim without interpreting further verbs. Actually, that's correct: `%s` prints the string as-is. It doesn't recursively format. So no format string vulnerability there.

However, what about `fmt.Fprintf(&b, "Reviewer-suggested fix (refine into a minimal, correct change): %s\n", f.Fix)`? Same thing, safe.

Is there any issue with `ex.SystemPrompt` not ending with a newline and the code adding `\n\n`? That's fine.

Let me look at the tests.

In `executor_test.go`:
```go
func TestCallExecutor_PassesExplicitTemperature(t *testing.T) {
    findings := []reconcile.JSONFinding{
        {Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: ConfidenceVerified},
    }
    rec := &recordingExecutor{out: "fix"}
    ex := execConfig("MEDIUM")
    temp := 0.3
    ex.Temperature = &temp
    generateFixes(context.Background(), findings, ex, execRegistry("MEDIUM"), rec, okDispatcher(), 0)
    require.Len(t, rec.temps, 1)
    require.NotNil(t, rec.temps[0], "executor temperature must reach the payload")
    assert.Equal(t, 0.3, *rec.temps[0], "the configured temperature must be forwarded verbatim")
}
```
Wait, `generateFixes` takes a parameter `sharedTimeoutSecs int`. The last argument is `0`. But in the original code, `generateFixes` is called with `sharedTimeoutSecs`. Passing `0` means `EffectiveExecutorTimeoutSecs` might return some default or 0? Let's check `execRegistry` and `execConfig` definitions which aren't shown, but presumably they work.

In `TestCallExecutor_DefaultsTemperatureToZero`:
```go
ex := execConfig("MEDIUM") // Temperature nil
generateFixes(..., rec, okDispatcher(), 0)
```
Checks that `rec.temps[0]` is not nil and equals 0.0. Good.

Wait, I see a potential issue in `internal/registry/config.go` in the `validateExecutor` method:
```go
if e.Temperature != nil && (*e.Temperature < 0 || *e.Temperature > 2) {
    errs = append(errs, errors.New("executor: temperature must be within [0, 2]"))
}
```
The sprint plan says: "Temperature handling mirrors the existing AgentConfig.Temperature pattern: *float64, validated to the range 0.0..2.0". Looking at the diff, does it validate against AgentConfig? There's no cross-reference to AgentConfig validation, but the code validates [0,2]. The sprint plan says validated to 0.0..2.0. The code uses `*e.Temperature < 0 || *e.Temperature > 2`. This allows exactly 2.0. Is that correct? The range is [0,2]. Yes.

Is there any issue with the constant names? `MaxExecutorSystemPromptLen = 4096`. No problem.

Let me look at `executor.go` line where `buildFixPrompt` is called:
```go
prompt := buildFixPrompt(*f, snippet, ex)
```
Wait, `ex` is of type `*registry.ExecutorConfig`. This is fine.

Actually, I need to look carefully at the `buildFixPrompt` function signature change. It now accepts `ex *registry.ExecutorConfig`. But inside the function, it does:
```go
if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
```
What if `ex` is nil? As I noted, if `ex` is nil, this will panic. In the old code, the caller did `buildFixPrompt(*f, snippet, ex.Persona)`, which would also panic if `ex` is nil. So the nil-safety hasn't changed. But wait - is `ex` guaranteed to be non-nil? The function `generateFixes` takes `ex *registry.ExecutorConfig`, and inside it calls `buildFixPrompt(*f, snippet, ex)`. If `ex` were nil, the old code would have panicked at `ex.Persona`. So this is not a new issue.

Wait, I found something. In `callExecutor`:
```go
temp := ex.EffectiveExecutorTemperature()
```
If `ex` is nil, this panics. But again, same as before.

Let me check if there are any off-by-one errors in length validation.
```go
if len(e.SystemPrompt) > MaxExecutorSystemPromptLen {
```
So if length == 4096, it passes. If 4097, rejected. This is a "must be at most" check. The error message says "at most %d characters". So `>` is correct.

For rules:
```go
case len(rule) > MaxExecutorRuleLen:
```
Same thing, correct.

One thing: the `validateExecutor` function doesn't check for nil `ExecutorConfig`. But that was already the case.

Wait, looking at the diff for `internal/registry/config.go`, I see:
```go
+	// Rules (Epic 7.0.1): each rule is interpolated as a constraint line in the fix
+	// prompt. Reject blank entries (a YAML typo, not "no rule"), control characters
+	// (CR/LF prompt-line forgery), and over-long entries — mirroring the scope and
+	// persona guards.
+	for i, rule := range e.Rules {
+		switch {
+		case strings.TrimSpace(rule) == "":
+			errs = append(errs, fmt.Errorf("executor: rules[%d] must not be empty", i))
+		case strings.IndexFunc(rule, func(r rune) bool { return r < 32 }) >= 0:
+			errs = append(errs, fmt.Errorf("executor: rules[%d] must not contain control characters", i))
+		case len(rule) > MaxExecutorRuleLen:
+			errs = append(errs, fmt.Errorf("executor: rules[%d] must be at most %d characters", i, MaxExecutorRuleLen))
+		}
+	}
```
Is `len(rule)` counting bytes or runes? It counts bytes. The sprint plan cap is likely in characters (runes). However, the existing persona guard probably also uses `len()` on bytes. So it mirrors existing behavior. Not an issue.

Wait, `strings.IndexFunc(rule, func(r rune) bool { return r < 32 })` - this checks for runes less than 32. That includes tab (9), newline (10), carriage return (13), etc. The comment says CR/LF. This is correct.

But what about the `system_prompt`? It intentionally allows control characters. Could a system_prompt with newlines be used for prompt injection? The comment explains it's intentional because multi-line content is legitimate. The sprint plan says it's bounded by length. I think this is acceptable per the design.

Now, is there any correctness issue with `EffectiveExecutorTemperature`?
```go
func (e ExecutorConfig) EffectiveExecutorTemperature() float64 {
	if e.Temperature != nil {
		return *e.Temperature
	}
	return 0.0
}
```
This uses a value receiver. If called on a nil pointer `(*ExecutorConfig)(nil).EffectiveExecutorTemperature()`, it would panic before entering because the method is on the value, but if called via a pointer and the pointer is nil... wait, in Go, if you have a value receiver, you can call it on a pointer, but if the pointer is nil, it dereferences and panics. But `ex` is already dereferenced earlier if it were nil. Not an issue.

Wait, I need to look at the test for `executor.go`. Is there a bug in the test setup?
In `TestBuildFixPrompt_SystemPromptOverridesFraming`:
```go
ex := execConfig("MEDIUM") // Persona "fixer"
ex.SystemPrompt = "ACT AS A STRICT GO LINTER. Emit only gofmt-clean code."
generateFixes(...)
```
And then checks that the prompt doesn't contain "a code-fix executor" and "You are fixer". But what if `execConfig("MEDIUM")` sets other fields? Hard to know without the helper source, but presumably fine.

Let me look for concurrency issues. In `generateFixes`:
```go
for i := range findings {
    ...
    go func(f *reconcile.JSONFinding) {
        ...
        prompt := buildFixPrompt(*f, snippet, ex)
        out, err := callExecutor(ctx, complete, prov, ex, prompt, sharedTimeoutSecs)
```
`ex` is shared across goroutines but is not mutated. `buildFixPrompt` reads from `ex`. That's fine.

Wait, in `callExecutor`:
```go
temp := ex.EffectiveExecutorTemperature()
return complete.Complete(callCtx, llmclient.Invocation{
    ...
    Temperature: &temp,
    ...
})
```
`&temp` takes the address of a local variable. That's safe.

Is there any issue with the prompt building when both system_prompt and persona are empty? The code defaults to `registry.DefaultExecutorPersona` if persona is empty. But what if `ex.SystemPrompt` is whitespace-only? `strings.TrimSpace(ex.SystemPrompt)` returns empty string, so it falls through to persona. That's fine.

What about the `rules` section:
```go
if len(ex.Rules) > 0 {
    b.WriteString("Coding rules to follow (apply these to your fix):\n")
    for _, rule := range ex.Rules {
        fmt.Fprintf(&b, "- %s\n", rule)
    }
    b.WriteString("\n")
}
```
This is fine. But wait - the sprint plan AC3 says: "If rules are provided, they are appended to the executor's context prompt prior to fix generation." The code appends them after the framing and before the finding metadata. That matches the requirement.

Now, let me look very carefully for any subtle bug.

In `internal/registry/config.go`, the comment for `MaxExecutorSystemPromptLen` says "It is larger than the persona cap because it is a full instruction block". Good.

In `validateExecutor`, the temperature validation uses `errors.New` for the message, but other validations use `fmt.Errorf`. That's just inconsistency, not a bug.

Wait! I see something. In `validateExecutor`, the temperature error uses `errors.New("executor: temperature must be within [0, 2]")`. But all other error messages in the function use `fmt.Errorf`. And the test `TestExecutor_TemperatureOutOfRangeRejected` checks:
```go
assert.Contains(t, err.Error(), "temperature")
```
This will pass.

Is there any missing validation? The sprint plan says temperature is `*float64`, validated to [0,2]. Done.

What about the `ExecutorConfig` struct tags? `Temperature *float64` has `yaml:"temperature,omitempty"`. Good.

One thing: in `executor.go`, `buildFixPrompt` now takes `ex *registry.ExecutorConfig`. But in the old code, the persona string was passed directly. The new code accesses `ex.Persona` and `ex.SystemPrompt` and `ex.Rules`. I already considered nil.

Wait, is there a bug where `buildFixPrompt` uses `fmt.Fprintf(&b, "You are %s, a code-fix executor....", persona)` and `persona` could contain format specifiers? No, because persona is validated to not contain `%`? Actually, no validation prevents `%` in persona. But `Fprintf` interprets format verbs in the format string, not in the arguments. Since the format string is a literal constant and persona is passed as an argument to `%s`, it's safe. If persona contained `%s`, it would just be printed as the string content because `%s` prints verbatim. Wait, no - `%s` prints the string value. It doesn't interpret formatting inside the string. So it's safe.

Let me check if there's an issue with the `recordingExecutor` in tests. It now records `temps []*float64`. In `Complete`, it does `r.temps = append(r.temps, inv.Temperature)`. `inv.Temperature` is a `*float64`. This just copies the pointer. Good.

Wait, is there a potential issue with `TestCallExecutor_DefaultsTemperatureToZero`? The test expects `rec.temps[0]` to be non-nil and equal to 0.0. But what if `generateFixes` has multiple findings? The test has only 1 finding. So `rec.temps` will have length 1. Good.

Let me think about the scope constraint. The sprint plan mentions:
- IN scope: ExecutorConfig schema fields + validation + defaults; temperature plumbing through llmclient.Invocation; prompt assembly; unit tests; example YAML + doc note.
- The files changed are exactly in scope.

Is there any issue with the nil pointer dereference of `ex` in `buildFixPrompt`? As discussed, it's not a new issue, but should I flag it? The instructions say "Focus on diff changes: Review what's been modified, not the entire codebase". Since `buildFixPrompt` signature was changed to accept `ex` directly, and previously it received a string (the dereferenced persona), there might be a subtle change: if `ex` is nil, the old code would panic at the call site `ex.Persona`, while the new code panics inside `buildFixPrompt`. But since both panic, it's not a new issue. However, the function now dereferences `ex` in multiple places. But again, same effective behavior.

Wait, I should check if `ex` could be nil in `generateFixes`. Looking at the diff, `generateFixes` signature takes `ex *registry.ExecutorConfig`. The caller isn't shown in the diff. But the fact that it was already accepting a pointer and accessing `ex.Persona` before means it was already assumed non-nil.

Actually, I want to re-examine `buildFixPrompt` signature change. Old signature: `buildFixPrompt(f reconcile.JSONFinding, snippet, persona string) string`. New signature: `buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig) string`. The old caller was `buildFixPrompt(*f, snippet, ex.Persona)`. If `ex` is nil, that would panic. The new caller is `buildFixPrompt(*f, snippet, ex)`. If `ex` is nil, the panic happens inside. Not a new issue.

Wait, I found a potential correctness issue! In `buildFixPrompt`, the code does:
```go
if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
    b.WriteString(sp)
    b.WriteString("\n\n")
} else {
    persona := ex.Persona
    if strings.TrimSpace(persona) == "" {
        persona = registry.DefaultExecutorPersona
    }
    fmt.Fprintf(&b, "You are %s, a code-fix executor...", persona)
}
```
If `ex.SystemPrompt` is non-empty after trim, it writes the trimmed version `sp`, not the raw `ex.SystemPrompt`. But wait - the prompt assembly writes exactly what the user configured (after trim). The sprint plan says "system_prompt, when set, fully replaces the default framing line". Trimming whitespace is probably fine, but it modifies the user's configured prompt. Is this an issue? It means if a user intentionally sets a system_prompt with leading/trailing whitespace (maybe for formatting), it gets trimmed. However, the old code also did `strings.TrimSpace(persona)` before checking. For system_prompt, the sprint plan says "replaces the default framing line verbatim". "Verbatim" suggests no modification. But the code trims it. Is that a bug?

Actually, looking carefully: `if sp := strings.TrimSpace(ex.SystemPrompt); sp != ""` - if the system_prompt is all whitespace, it's treated as empty and falls through to persona. But if it has content, `sp` (the trimmed version) is written, not the original `ex.SystemPrompt`. So leading/trailing whitespace is lost. Is this a problem? The sprint plan says "replaces the default fix-prompt framing verbatim (supersedes persona)". "Verbatim" might imply exact text. But practically, trimming is defensive. However, the test `TestBuildFixPrompt_SystemPromptOverridesFraming` uses a prompt without extra whitespace, so it doesn't catch this.

Wait, the sprint plan says: "system_prompt, when set, fully replaces the default framing line". The code comment says: "Custom framing fully replaces the default; persona is superseded." But it doesn't say "trimmed". If I want to be strict, this could be seen as a minor deviation. But is it worth flagging? Probably LOW at best. Actually, it might not matter.

Let me look for another issue. In `examples/registry-with-executor.yaml`:
```yaml
+  # system_prompt: |             # optional: replaces the default fixer framing verbatim (supersedes persona)
+  #   You are a senior Go engineer. Emit only gofmt-clean, idiomatic Go and nothing else.
```
The literal block scalar `|` preserves newlines. That's fine.

In `internal/registry/executor_config_test.go`, the test `TestExecutor_SystemPromptOverLengthRejected` uses a raw string. Fine.

Wait, I see a potential issue in `executor.go` `buildFixPrompt`. When `ex.SystemPrompt` is used, it writes:
```go
b.WriteString(sp)
b.WriteString("\n\n")
```
Then later, it writes the finding metadata. But what if the system_prompt already ends with newlines? Then there might be extra newlines. But that's cosmetic.

Let me look at `callExecutor` again.
```go
func callExecutor(ctx context.Context, complete executorCompleter, prov registry.Provider, ex *registry.ExecutorConfig, prompt string, sharedTimeoutSecs int) (string, error) {
    timeout := ex.EffectiveExecutorTimeoutSecs(registry.Settings{TimeoutSecs: sharedTimeoutSecs})
    callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
    defer cancel()
    temp := ex.EffectiveExecutorTemperature()
    return complete.Complete(callCtx, llmclient.Invocation{
        BaseURL:     prov.BaseURL,
        APIKeyEnv:   prov.APIKeyEnv,
        Model:       ex.Model,
        Temperature: &temp,
        Prompt:      prompt,
    })
}
```
Is `ex` nil checked anywhere before calling `ex.EffectiveExecutorTimeoutSecs` or `ex.EffectiveExecutorTemperature`? No. But as I said, this isn't new.

Actually, I just realized there might be an issue with the temperature pointer in `llmclient.Invocation`. The struct field `Temperature` is a `*float64`. In `callExecutor`, we do:
```go
temp := ex.EffectiveExecutorTemperature()
...
Temperature: &temp,
```
But what is the zero value of `llmclient.Invocation.Temperature`? It's `nil`. With `omitempty`, `nil` means omit. But we're always setting it to a non-nil pointer now. That's the intended behavior change.

However, there's a subtle issue: `EffectiveExecutorTemperature()` is defined with a value receiver: `func (e ExecutorConfig) EffectiveExecutorTemperature() float64`. But `ex` is a `*registry.ExecutorConfig`. Calling `ex.EffectiveExecutorTemperature()` works fine in Go (automatic dereferencing). If `ex` is nil, it panics. But again, not new.

Wait, I want to look at `internal/registry/config.go` again. The `ExecutorConfig` struct now has `Rules []string`. In the `validateExecutor` function, the rules are validated. But what about duplicate rules? Not mentioned in sprint plan. No need to flag.

Is there any issue with the test file? `executor_config_test.go` uses `floatPtr(f float64)` helper. That's fine.

Wait, I found something! In `executor.go`, the function `buildFixPrompt` now takes `ex *registry.ExecutorConfig`. But the old tests for `buildFixPrompt` (not shown in the diff) might be broken because the signature changed. However, the diff shows new tests and modified tests. Let me check if there are any compile issues. The diff only shows additions, not modifications to existing tests except the `recordingExecutor` struct. The old tests that called `buildFixPrompt` directly with a persona string would break, but the diff doesn't show them being updated. Are there any? The diff for `executor_test.go` only shows additions at the end. But maybe the old tests weren't shown because they weren't changed? Actually, git diff only shows changes. If a line wasn't changed, it won't appear. But if there were existing tests that call `buildFixPrompt`, they wouldn't compile with the new signature unless they were updated. Since the diff doesn't show updates to existing tests that call `buildFixPrompt`, maybe there weren't any, or maybe they were updated but the diff context hid them? Looking at the diff:
```diff
@@ -197,6 +229,72 @@ func TestGenerateFixes_SnippetEmbeddedInPrompt(t *testing.T) {
```
This shows that between line 197 and the new additions, there are existing tests. But the diff doesn't show modifications to them. If they called the old `buildFixPrompt`, they would need to be updated. But since they're not in the diff, perhaps they don't call `buildFixPrompt` directly. Actually, `TestGenerateFixes_SnippetEmbeddedInPrompt` uses `generateFixes`, which internally calls `buildFixPrompt`. So it doesn't call it directly. There might not be any direct callers in the test file. That's fine.

Let me look for performance issues. None.

Let me look for error handling issues. In `generateFixes`, errors from `callExecutor` are logged but not returned. That's existing behavior.

Wait, I need to check the `validateExecutor` in `config.go` more carefully.
```go
if e.Temperature != nil && (*e.Temperature < 0 || *e.Temperature > 2) {
    errs = append(errs, errors.New("executor: temperature must be within [0, 2]"))
}
```
It validates that temperature is within [0,2]. But what about NaN or Inf? If `*e.Temperature` is `math.NaN()`, then `*e.Temperature < 0` is false and `*e.Temperature > 2` is false, so NaN passes validation. Then `EffectiveExecutorTemperature` returns NaN, which gets passed to the LLM client. Is this an issue? The sprint plan says validated to range 0.0..2.0. NaN is not in that range, but the check doesn't catch it. Similarly, +Inf or -Inf: `-Inf < 0` is true, so it would be rejected. `+Inf > 2` is true, rejected. But NaN is not caught. Is this worth flagging? It's a corner case. But the existing agent temperature validation probably has the same issue. I could flag it as LOW. However, YAML parsing of `temperature: .NaN` might not even produce NaN. Actually, YAML float parsing might produce NaN for `.nan`. It's possible but unlikely. Given the strict format and no false positives, I need to decide if this is a real issue. It's a minor validation gap. I'll skip it unless it's clearly a bug.

What about `MaxExecutorSystemPromptLen`? The validation uses `len(e.SystemPrompt) > MaxExecutorSystemPromptLen`. `len` counts bytes, not runes. If the system prompt contains multi-byte UTF-8 characters, the byte length could exceed 4096 while the rune count is less. But again, this mirrors existing persona validation which also uses `len` on bytes. The sprint plan doesn't specify rune vs byte. So this is consistent.

Wait! I see something. In `executor.go` `buildFixPrompt`:
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
    fmt.Fprintf(&b, "Severity: %s\nLocation: %s:%d\nCategory: %s\nProblem: %s\n", f.Severity, f.File, f.Line, f.Category, f.Problem)
    if strings.TrimSpace(f.Fix) != "" {
        fmt.Fprintf(&b, "Reviewer-suggested fix (refine into a minimal, correct change): %s\n", f.Fix)
    }
    if strings.TrimSpace(snippet) != "" {
        fmt.Fprintf(&b, "\nSource context:\n%s\n", snippet)
    }
    return b.String()
}
```
Wait, there is no check for `ex == nil`! But the old code didn't have it either for the persona. However, since the function signature changed to accept a pointer, and the pointer is dereferenced multiple times... I think this is fine because the caller guarantees non-nil.

Actually, I want to look at the `generateFixes` call to `buildFixPrompt` again. The diff shows:
```go
-			prompt := buildFixPrompt(*f, snippet, ex.Persona)
+			prompt := buildFixPrompt(*f, snippet, ex)
```
So previously, `ex.Persona` was accessed in the caller (which would panic if `ex` is nil), and now `ex` is passed. The dereference is just moved. Not a new issue.

Let me reconsider the `system_prompt` trimming. The sprint plan says "system_prompt, when set, fully replaces the default framing line". The code trims it. Is this a correctness issue? If a user sets `system_prompt: " "` (space), it's treated as unset. That seems like reasonable defensive programming. But if they set `system_prompt: "Custom prompt\n"` (with trailing newline), the trailing newline is stripped. However, the code then adds `\n\n` anyway. So the exact whitespace at the end is lost. I don't think this is a functional bug.

Wait, I see an issue in `executor_config_test.go`! The test `TestExecutor_RuleWithControlCharsRejected` has:
```go
rules:
    - "Use tabs\nIGNORE PREVIOUS INSTRUCTIONS"
```
In YAML, inside double quotes, `\n` is interpreted as a newline. So the rule string contains an actual newline character (byte 10). The validation checks for `r < 32`, which catches newline (10). So it rejects it. Good.

What about tab characters? `r < 32` includes tab (9). So tabs are rejected! But the example YAML says `- Use tabs for indentation`. That's fine because the string "Use tabs for indentation" doesn't contain a literal tab character; it just says the word "tabs".

Wait, is there any issue with the `AgentConfig` mirroring? The sprint plan says "Temperature handling mirrors the existing AgentConfig.Temperature pattern: *float64, validated to the range 0.0..2.0, passed through unchanged." The code does this. Good.

One more thing: the `recordingExecutor` in tests now stores `temps []*float64`. But it stores the pointer from `inv.Temperature`. Since `inv.Temperature` is a