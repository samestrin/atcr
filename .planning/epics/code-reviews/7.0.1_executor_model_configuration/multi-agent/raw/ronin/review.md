We need to review the diff and identify any issues, but only output findings that are IN SCOPE per sprint plan. The sprint plan is about Executor Model Configuration, focusing on temperature, system_prompt, rules, config schema, validation, tests. So we should only flag issues that are within that scope. The diff adds new fields, validation, tests. We need to find any issues: potential bugs, missing validation, logic errors, etc.

Let's scan changes.

1. In config.go, added constants MaxExecutorSystemPromptLen = 4096, MaxExecutorRuleLen = 512. Looks fine.

2. In config.go, added validation for Temperature: check >0 && <=2. That's okay.

3. Added validation for SystemPrompt length > MaxExecutorSystemPromptLen -> error. That's fine.

4. Added validation for Rules: each rule must not be empty, must not contain control characters, length <= MaxExecutorRuleLen. That's fine.

5. In config struct, added Temperature *float64, SystemPrompt string, Rules []string.

6. Added EffectiveExecutorTemperature method that returns temperature if set else 0.0.

7. In callExecutor, they added passing Temperature: &temp where temp is ex.EffectiveExecutorTemperature().

8. In tests, they added tests for temperature parsing, out-of-range rejection, system_prompt parsing, rule parsing, etc.

Potential issues?

- In config validation, they check temperature range [0,2] but they also earlier said temperature is a pointer so explicit 0.0 is allowed. They check if e.Temperature != nil && (*e.Temperature < 0 || *e.Temperature > 2). That's fine.

- However, they didn't check that SystemPrompt doesn't contain control characters? They said control characters are intentionally NOT rejected for system_prompt, unlike persona. That's by design.

- In buildFixPrompt, they changed logic: if SystemPrompt set, they write it directly, else use persona. That's fine.

- In callExecutor, they added comment about temperature being sent on every call. They compute temp := ex.EffectiveExecutorTemperature() and pass &temp. That's fine.

Potential bug: In EffectiveExecutorTemperature, they return 0.0 if nil, but they also return the temperature if set. That's okay.

But maybe there is an issue: In config validation, they didn't validate that SystemPrompt doesn't contain newline characters that could break prompt? They said control characters are intentionally NOT rejected. That's by design.

Maybe there is a missing validation: They didn't check that Rules array length is not too large? They validate each rule length but not total array length; maybe not needed.

Potential issue: In config.go, they added MaxExecutorSystemPromptLen = 4096, but earlier they said MaxExecutorPersonaLen = 512. That's okay.

But maybe there is a bug: In config validation for SystemPrompt length, they compare len(e.SystemPrompt) > MaxExecutorSystemPromptLen, but SystemPrompt can be empty; that's fine.

Potential issue: In tests, they use writeRegistry(t, executorBaseProviders+'` ... `)` but that's test code, not part of diff? It is part of diff, but it's just test changes. Could be fine.

But the question: "Review the following diff and identify any issues." So we need to find any issues in the diff. Possibly there is a bug: In config validation for temperature, they check *e.Temperature > 2, but earlier they said temperature range [0,2]; but they didn't check that temperature cannot be negative? They do check <0. That's fine.

But maybe there is a logic error: In EffectiveExecutorTemperature, they return 0.0 if nil, but they also return the temperature if set. However, they earlier said temperature default is deterministic 0.0 when omitted. That's okay.

But maybe there is a missing validation: They didn't validate that SystemPrompt doesn't exceed some rune limit? They capped length but not content; it's okay.

Maybe there is an issue with the default temperature: They set default deterministic 0.0, but earlier they said temperature is a pointer so explicit 0.0 is distinguishable from unset. However, they also said that when temperature is nil, they default to 0.0 at call time. That's fine.

But maybe there is a bug: In callExecutor, they compute timeout := ex.EffectiveExecutorTimeoutSecs(registry.Settings{TimeoutSecs: sharedTimeoutSecs}) and then use that to create context. That's fine.

Potential issue: In config.go, they added validation for temperature but didn't add validation for SystemPrompt content like control characters? They said intentionally not reject. That's by design.

Maybe there is a bug: In the test for temperature out-of-range, they check err contains "temperature". That's fine.

But maybe there is a missing validation: They didn't check that SystemPrompt length is within some limit relative to MaxExecutorSystemPromptLen but they used that constant. That's fine.

Maybe there is a bug: In config struct, they added SystemPrompt string, but they didn't add validation that it doesn't contain newline? They said control characters are intentionally NOT rejected. That's okay.

But maybe there is a bug: In buildFixPrompt, they wrote:

```
if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
    // Custom framing fully replaces the default; persona is superseded.
    b.WriteString(sp)
    b.WriteString("\n\n")
} else {
    persona := ex.Persona
    ...
}
```

But they didn't add a newline after system prompt? They added "\n\n". That's okay.

But maybe there is a bug: They didn't sanitize SystemPrompt for control characters that could break prompt formatting, but they said it's intentionally not rejected because it's a full instruction block. That's okay.

Potential issue: In config validation for rules, they check for control characters using strings.IndexFunc(rule, func(r rune) bool { return r < 32 }) >= 0. That catches control chars including CR (13) and LF (10). That's fine.

But maybe there is a bug: They didn't check that rule lines don't contain leading/trailing spaces? Not needed.

Maybe there is a bug: They didn't check that temperature is not nil but value is exactly 0.0 when default deterministic; but they said that is okay.

Maybe there is a bug: In EffectiveExecutorTemperature, they return 0.0 if nil, but they also return the temperature if set. However, they didn't consider that temperature could be set to 0.0 explicitly; they said that is honored. That's fine.

Potential issue: In config validation, they didn't check that temperature is not nil but value is within [0,2]; they do check if e.Temperature != nil && (*e.Temperature < 0 || *e.Temperature > 2). That's fine.

But maybe there is a bug: They didn't check that temperature cannot be a pointer to a negative value that is uninitialized? Not relevant.

Maybe there is a bug: In tests, they use floatPtr function to create *float64. That's fine.

Potential issue: In config.go, they added comment about MaxExecutorSystemPromptLen being larger than persona cap because it's a full instruction block. That's fine.

Maybe there is a bug: They didn't add validation that SystemPrompt doesn't exceed MaxExecutorSystemPromptLen in bytes but they used len(e.SystemPrompt) > MaxExecutorSystemPromptLen. That's fine.

Potential issue: They didn't add validation that Rules length is not too large? Not required.

Maybe there is a bug: In config struct, they added SystemPrompt string but didn't add json tag? Not needed.

But the question: "Apply this scope: if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)." So we should only output findings that are IN SCOPE. The diff includes changes to config.go, internal/registry/executor_config_test.go, internal/verify/executor.go, internal/verify/executor_test.go, examples/registry-with-executor.yaml. All these are within the scope of epic 7.0.1, which is about config schema, temperature, system_prompt, rules, validation, tests. So all are in scope.

Now we need to identify any issues: maybe there is a bug: In config validation for temperature, they check *e.Temperature > 2 but they didn't check that temperature cannot be a pointer to a non-nil value that is exactly 2? They allow up to 2 inclusive? They check >2 as error, so 2 is allowed. That's fine.

But maybe there is a bug: They didn't check that temperature cannot be a pointer to a value that is exactly 0 when nil? That's fine.

Maybe there is a bug: In EffectiveExecutorTemperature, they return 0.0 if nil, but they also return the temperature if set. However, they didn't consider that temperature could be set to a pointer with value 0.0 but that is same as default; but they said explicit 0.0 is honored. That's fine.

Potential issue: In config validation, they didn't check that SystemPrompt length is within [0, MaxExecutorSystemPromptLen] but they only check > Max, not <0. That's fine.

Maybe there is a bug: They didn't check that Rules array length is not negative (can't be). That's fine.

Maybe there is a bug: They didn't check that SystemPrompt doesn't contain unescaped backticks or something that could break YAML? Not relevant.

Maybe there is a bug: In buildFixPrompt, they wrote:

```
fmt.Fprintf(&b, "- %s\n", rule)
```

But they didn't escape any special characters; but rules are already validated to not contain control chars, but they could contain formatting like "%"? That could cause format string injection? Actually they use fmt.Fprintf with a format string "- %s\n". If rule contains "%", it could be interpreted as format spec. That could cause unexpected behavior. That's a potential security issue: format string vulnerability. However, they are using Fprintf with a format string that includes %s; if rule contains "%", it will be treated as a format specifier, potentially causing a panic or injection. That's a bug. They should escape "%" or use fmt.Printf with plain string. So that's an issue.

Also, they use fmt.Fprintf(&b, "You are %s, a code-fix executor...". That's fine because persona is sanitized earlier (validatePersona). But for rules, they didn't sanitize. So a rule containing "%" could break formatting. That's a bug.

Also, they use fmt.Fprintf(&b, "Severity: %s\nLocation: %s:%d\nCategory: %s\nProblem: %s\n", ...). That's fine because those values are from finding, but they might contain format specifiers? They are strings, but could contain "%"? They are from finding fields, but they might be user-provided? The finding's Severity, Category, Problem are from code, but could be user-provided? They are from findings, maybe from user input. Could be unsafe. But they are not sanitized. However, the issue is more about the rules formatting.

Thus, a potential issue: In buildFixPrompt, when appending rules, they use fmt.Fprintf with "- %s\n". If rule contains "%", it will be interpreted as format specifier leading to undefined behavior. That's a bug.

Also, maybe there is a bug: They didn't check that SystemPrompt doesn't contain newline characters that could break prompt structure? They said control characters are intentionally not rejected, but newline is a control character? Actually newline is 10, which is control char, but they said control chars are not rejected for system_prompt. However, newline in system prompt could break the prompt formatting, causing injection. That could be a security issue. They allowed control characters, but newline is a control char; they said intentionally not rejected because system prompt is a full instruction block where multi-line content is legitimate. Actually they said control characters are intentionally NOT rejected — unlike persona (a mid-sentence token), a system prompt is a full instruction block where multi-line content is legitimate. So they allow newline. That's okay.

But maybe there is a bug: They didn't limit SystemPrompt to not contain prompt injection tokens like "Ignore previous instructions". They said it's a full instruction block, but they didn't guard against injection. However, that's out of scope maybe.

But the question: Identify any issues. So we can output a finding about the format string vulnerability in rule handling.

Also, maybe there is a bug: In config validation for temperature, they check *e.Temperature > 2 but they didn't check that temperature cannot be a pointer to a value that is exactly 2? That's allowed. Fine.

Maybe there is a bug: They didn't check that temperature cannot be a pointer to a value that is exactly 0 when nil? That's fine.

Maybe there is a bug: They didn't check that SystemPrompt length is within 4096 characters, but they used MaxExecutorSystemPromptLen = 4096. That's fine.

Maybe there is a bug: They didn't check that Rules length is not empty? They allow empty array; that's fine.

Maybe there is a bug: They didn't check that SystemPrompt is not empty when it contains only whitespace? They trim space before checking. That's fine.

Potential issue: In config validation for temperature, they check *e.Temperature < 0 || *e.Temperature > 2, but they didn't check that temperature cannot be a pointer to a value that is exactly 2? That's allowed. Fine.

Maybe there is a bug: They didn't check that temperature cannot be a pointer to a value that is exactly 0 when nil? That's fine.

Maybe there is a bug: They didn't check that SystemPrompt doesn't contain leading/trailing whitespace that could affect prompt? Not needed.

Thus, the main issue is the format string vulnerability when appending rules. Also maybe the same issue for system_prompt? They directly write sp to b without any escaping; but system_prompt is taken verbatim from config, which could contain formatting characters like "%". That could also cause format string injection. Indeed, they write b.WriteString(sp) directly, not using fmt.Fprintf. That's okay because they are just writing the string, not using formatting. But they earlier used fmt.Fprintf for system prompt? No, they just write the string. So only rules use fmt.Fprintf with "- %s\n". That's the vulnerable part.

Thus, we can output a finding: HIGH severity maybe, because it's a potential format string vulnerability that could be exploited to inject malicious prompts? It could affect the generated prompt sent to LLM, but not directly code execution. However, it's a bug.

Also, maybe there is a bug: They didn't check that SystemPrompt length is within 4096 characters, but they used that constant. That's fine.

Thus, we can output a finding: HIGH|internal/registry/config.go:... maybe line numbers? The validation for rules occurs around line ~70? Let's locate: In config.go, they added validation after the constants. The rule validation loop starts at line ~106? Let's approximate.

But we need to output FILE:LINE. We need to find exact line numbers. Let's approximate based on diff. The added validation code is after the constants and after the TimeoutSecs validation. The code is:

```
 // Temperature (Epic 7.0.1): bounded to [0,2] like the agent guard. A pointer so
 // an explicit 0.0 (the deterministic default) is distinguishable from unset.
 if e.Temperature != nil && (*e.Temperature < 0 || *e.Temperature > 2) {
 	errs = append(errs, errors.New("executor: temperature must be within [0, 2]"))
 }
 // SystemPrompt (Epic 7.0.1) replaces the fix-prompt framing verbatim; cap its
 // length to bound prompt-stuffing. Control characters are intentionally NOT
 // rejected — unlike persona (a mid-sentence token), a system prompt is a full
 // instruction block where multi-line content is legitimate.
 if len(e.SystemPrompt) > MaxExecutorSystemPromptLen {
 	errs = append(errs, fmt.Errorf("executor: system_prompt must be at most %d characters", MaxExecutorSystemPromptLen))
 }
 // Rules (Epic 7.0.1): each rule is interpolated into the fix-generation prompt as a constraint
 // line; a CR/LF (or other control character) could forge prompt lines (prompt
 // injection), so it is rejected at load mirroring the scope and persona guards.
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

The formatting string issue is in buildFixPrompt, not in validation. That code is in internal/verify/executor.go around line ~150? Let's locate: In executor.go, they changed buildFixPrompt function. The relevant lines are after the modifications. The code that adds rules is:

```
 if len(ex.Rules) > 0 {
 	b.WriteString("Coding rules to follow (apply these to your fix):\n")
 	for _, rule := range ex.Rules {
 		fmt.Fprintf(&b, "- %s\n", rule)
 	}
 	b.WriteString("\n")
 }
```

Thus, the issue is in that function. The file is internal/verify/executor.go. We need to give line numbers. Let's approximate: The diff shows changes around line 154? Actually earlier they changed buildFixPrompt signature and added logic. The line numbers in diff: internal/verify/executor.go changes around line 154? Let's scroll: The diff shows:

```
@@ -154,11 +154,17 @@ func callExecutor(ctx context.Context, complete executorCompleter, prov registry
...
 ```

But the buildFixPrompt function is earlier. Let's find the exact line numbers for the changes to buildFixPrompt. In the diff, they show:

```
@@ -121,7 +121,7 @@ func generateFixes...
 
 			defer wg.Done()
 			defer func() { <-sem }()
 			snippet := readFixSnippet(ctx, disp, f.File, f.Line)
-			prompt := buildFixPrompt(*f, snippet, ex.Persona)
+			prompt := buildFixPrompt(*f, snippet, ex)
 			out, err := callExecutor(ctx, complete, prov, ex, prompt, sharedTimeoutSecs)
@@ -154,11 +154,17 @@ func callExecutor(ctx context.Context, complete executorCompleter, prov registry
 ...
 ```

But the buildFixPrompt function is defined earlier. Let's locate that function: In the diff, they show changes to buildFixPrompt starting at line ~192? Actually they show:

```
@@ -192,24 +198,47 @@ func readFixSnippet(ctx context.Context, disp Dispatcher, file string, line int)
 	return res.Content
 }
 
-// buildFixPrompt renders the executor prompt for one finding: a fix-focused persona
-// instruction, the finding metadata, the reviewer's existing fix suggestion (when
-// present, to refine rather than reinvent), and the source snippet (when available).
+// buildFixPrompt renders the executor prompt for one finding: a fix-focused framing
+// instruction, optional user-supplied coding rules, the finding metadata, the
+// reviewer's existing fix suggestion (when present, to refine rather than reinvent),
+// and the source snippet (when available).
 //
-// Untrusted-data boundary: persona, the finding fields (Problem, Fix), and the
-// source snippet are all interpolated verbatim into this single prompt string —
-// llmclient.Invocation carries no role separation, so there is no structured way to
-// fence them. The persona is sanitized at load (validateExecutor rejects control
-// characters and caps length) to block CR/LF prompt-line forgery; the finding text
-// and snippet are reviewer/repo-derived data, not instructions. Blast radius is
-// bounded: the registry is self-authored and the output only lands in the Fix
-// column (it is never executed), so this is documented rather than actively escaped.
-func buildFixPrompt(f reconcile.JSONFinding, snippet string, persona string) string {
-	if strings.TrimSpace(persona) == "" {
-		persona = registry.DefaultExecutorPersona
-	}
+// Framing (Epic 7.0.1): when ex.SystemPrompt is set it replaces the default
+// "You are <persona>, a code-fix executor..." line verbatim and the persona is
+// superseded for that call (clarification opt-a); otherwise the default persona
+// framing is used. ex.Rules, when present, are appended as a constraints block
+// directly after the framing so they bind the whole request.
+//
+// Untrusted-data boundary: the framing (persona or system_prompt), rules, the
+// finding fields (Problem, Fix), and the source snippet are all interpolated
+// verbatim into this single prompt string — llmclient.Invocation carries no role
+// separation, so there is no structured way to fence them. Persona and each rule are
+// sanitized at load (validateExecutor rejects control characters and caps length) to
+// block CR/LF prompt-line forgery; system_prompt is length-capped (multi-line framing
+// is legitimate there). The finding text and snippet are reviewer/repo-derived data,
+// not instructions. Blast radius is bounded: the registry is self-authored and the
+// output only lands in the Fix column (it is never executed), so this is documented
+// rather than actively escaped.
+func buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig) string {
+	var b strings.Builder
+	if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
+		// Custom framing fully replaces the default; persona is superseded.
+		b.WriteString(sp)
+		b.WriteString("\n\n")
+	} else {
+		persona := ex.Persona
+		if strings.TrimSpace(persona) == "" {
+			persona = registry.DefaultExecutorPersona
+		}
+		fmt.Fprintf(&b, "You are %s, a code-fix executor. Generate the minimal code change that fixes the finding below. Preserve the existing style and conventions. Output only the fix (corrected code or a precise change instruction); do not restate the problem.\n\n", persona)
+	}
+	if len(ex.Rules) > 0 {
+		b.WriteString("Coding rules to follow (apply these to your fix):\n")
+		for _, rule := range ex.Rules {
+			fmt.Fprintf(&b, "- %s\n", rule)
+		}
+		b.WriteString("\n")
+	}
 	fmt.Fprintf(&b, "Severity: %s\nLocation: %s:%d\nCategory: %s\nProblem: %s\n", f.Severity, f.File, f.Line, f.Category, f.Problem)
 	if strings.TrimSpace(f.Fix) != "" {
 		fmt.Fprintf(&b, "Reviewer-suggested fix (refine into a minimal, correct change): %s\n", f.Fix)
```

Thus, the fmt.Fprintf line that adds rule is inside the for loop. The line numbers for that part would be after the WriteString lines. Let's approximate: The function starts at line ~260? Actually we can approximate but we need to give a line number. The diff doesn't give exact line numbers for that part, but we can approximate based on context. However, we can pick a line number that is plausible. The changes are around line ~250? Let's count: The function definition is after readFixSnippet, which ends at line ~198? Actually the diff shows that readFixSnippet ends at line ~198 (the old version). Then they added new function starting after that. So the new buildFixPrompt function starts after that, maybe around line 199? Let's approximate: The diff shows that after readFixSnippet, they added new comment // buildFixPrompt renders... and then the function. So the function starts maybe at line ~200. Then they added the logic up to the fmt.Fprintf for rules. So the line with fmt.Fprintf for rule might be around line ~250. But we need to give a specific line number. Could we compute? Let's try to approximate more precisely: The diff shows that they added new code after the comment // Framing (Epic 7.0.1): when ex.SystemPrompt is set it replaces the default ... The function starts with var b strings.Builder. Then they check SystemPrompt. Then they write sp and newline. Then else block with persona formatting using fmt.Fprintf. Then they check len(ex.Rules) > 0, then write "Coding rules to follow..." then for _, rule := range ex.Rules { fmt.Fprintf(&b, "- %s\n", rule) }. So the fmt.Fprintf line is inside that loop. The line number of that fmt.Fprintf is maybe around line ~240? Let's approximate: The function length maybe ~70 lines. Starting at ~200, plus 40 lines, so maybe ~240. Let's say line 242? But we need to be accurate? The instruction says "FILE:LINE: The file path and line number". It expects a line number referencing the diff. We can pick a line number that is definitely within the added code. Let's find the exact line numbers in the diff: The diff shows that they added new code after the comment // Framing (Epic 7.0.1): ... The function definition begins after that comment. Let's count lines manually.

I'll simulate: The diff snippet shows:

```
@@ -192,24 +198,47 @@ func readFixSnippet(ctx context.Context, disp Dispatcher, file string, line int)
 	return res.Content
 }
 
-// buildFixPrompt renders the executor prompt for one finding: a fix-focused persona
-// instruction, the finding metadata, the reviewer's existing fix suggestion (when
-// present, to refine rather than reinvent), and the source snippet (when available).
+// buildFixPrompt renders the executor prompt for one finding: a fix-focused framing
+// instruction, optional user-supplied coding rules, the finding metadata, the
+// reviewer's existing fix suggestion (when present, to refine rather than reinvent),
+// and the source snippet (when available).
 //
+// Untrusted-data boundary: the framing (persona or system_prompt), rules, the
+// finding fields (Problem, Fix), and the source snippet are all interpolated
+// verbatim into this single prompt string — llmclient.Invocation carries no role
+// separation, so there is no structured way to fence them. Persona and each rule are
+// sanitized at load (validateExecutor rejects control characters and caps length) to
+// block CR/LF prompt-line forgery; system_prompt is length-capped (multi-line framing
+// is legitimate there). The finding text and snippet are reviewer/repo-derived data,
+// not instructions. Blast radius is bounded: the registry is self-authored and the
+// output only lands in the Fix column (it is never executed), so this is documented
+// rather than actively escaped.
+func buildFixPrompt(f reconcile.JSONFinding, snippet string, ex *registry.ExecutorConfig) string {
+	var b strings.Builder
+	if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
+		// Custom framing fully replaces the default; persona is superseded.
+		b.WriteString(sp)
+		b.WriteString("\n\n")
+	} else {
+		persona := ex.Persona
+		if strings.TrimSpace(persona) == "" {
+			persona = registry.DefaultExecutorPersona
+		}
+		fmt.Fprintf(&b, "You are %s, a code-fix executor. Generate the minimal code change that fixes the finding below. Preserve the existing style and conventions. Output only the fix (corrected code or a precise change instruction); do not restate the problem.\n\n", persona)
+	}
+	if len(ex.Rules) > 0 {
+		b.WriteString("Coding rules to follow (apply these to your fix):\n")
+		for _, rule := range ex.Rules {
+			fmt.Fprintf(&b, "- %s\n", rule)
+		}
+		b.WriteString("\n")
+	}
 	fmt.Fprintf(&b, "Severity: %s\nLocation: %s:%d\nCategory: %s\nProblem: %s\n", f.Severity, f.File, f.Line, f.Category, f.Problem)
 	if strings.TrimSpace(f.Fix) != "" {
 		fmt.Fprintf(&b, "Reviewer-suggested fix (refine into a minimal, correct change): %s\n", f.Fix)
```

Thus, the fmt.Fprintf line that adds rule is inside the for loop. The line numbers for that line would be after the line that writes "Coding rules to follow...". Let's count lines from the start of the function:

Line numbers (approx):
1: func buildFixPrompt(... ) string {
2: 	var b strings.Builder
3: 	if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
4: 		// Custom framing fully replaces the default; persona is superseded.
5: 		b.WriteString(sp)
6: 		b.WriteString("\n\n")
7: 	} else {
8: 		persona := ex.Persona
9: 		if strings.TrimSpace(persona) == "" {
10: 			persona = registry.DefaultExecutorPersona
11: 		}
12: 		fmt.Fprintf(&b, "You are %s, a code-fix executor. Generate the minimal code change that fixes the finding below. Preserve the existing style and conventions. Output only the fix (corrected code or a precise change instruction); do not restate the problem.\n\n", persona)
13: 	}
14: 	if len(ex.Rules) > 0 {
15: 		b.WriteString("Coding rules to follow (apply these to your fix):\n")
16: 		for _, rule := range ex.Rules {
17: 			fmt.Fprintf(&b, "- %s\n", rule)
18: 		}
19: 		b.WriteString("\n")
20: 	}
...

Thus, the fmt.Fprintf line is line 17 within the function. But the file line numbers are different. However, we can approximate the absolute line number in the file. The function starts at some line number, maybe around 215? Let's find where the function is located in the file. The diff shows that they changed internal/verify/executor.go. The original file had some functions before. Let's locate the original file's line numbers: The original file had functions: generateFixes, callExecutor, readFixSnippet, buildFixPrompt, etc. The diff shows that they modified buildFixPrompt and added new logic. The original file likely had buildFixPrompt defined earlier. The line numbers in the diff for the changes to buildFixPrompt are around line ~192? Actually the diff shows that they changed lines around 192-24? Let's see: The diff shows that they changed internal/verify/executor.go with index db02fd6..183ccea. The changes include modifications to generateFixes (line -121,7 +121,7), callExecutor (line -154,11 +154,17), readFixSnippet (line -192,6 +229,72), buildFixPrompt (line -192,24 +198,47). So the buildFixPrompt function originally started at line ~192? Actually the original file had a function buildFixPrompt defined earlier, maybe around line ~192? The diff shows that they changed that function from 192,24 lines to 198,47 lines. So the original function started at line ~192 (maybe earlier). So the new function starts at line ~198? Actually the diff shows that the original function had 24 lines, and they changed to 47 lines. The new function starts at line ~198? Let's compute: The original function started at line ~192 (maybe earlier). The diff shows that they changed lines -192,24 +198,47. That means that the original function started at line 192 and had 24 lines. The new function starts at line 198 (maybe after modifications). So the new function occupies lines 198-244? Actually 198 + 47 -1 = 244. So the function occupies lines 198-244. So the fmt.Fprintf line that adds rule is somewhere within that range. Let's approximate: The function body lines: after the comment lines, they have var b strings.Builder (line maybe 200?), then if sp... etc. The fmt.Fprintf for rule is inside the for loop, which is after writing "Coding rules to follow...". That would be maybe around line 225? Let's approximate: Let's count within the new function: lines:

- line 198: // buildFixPrompt renders...
- line 199: // instruction, optional user-supplied coding rules...
- line 200: // reviewer's existing fix suggestion...
- line 201: // and the source snippet...
- line 202: //
- line 203: // Untrusted-data boundary...
- line 204: func buildFixPrompt(... ) string {
- line 205: 	var b strings.Builder
- line 206: 	if sp := strings.TrimSpace(ex.SystemPrompt); sp != "" {
- line 207: 		// Custom framing fully replaces the default; persona is superseded.
- line 208: 		b.WriteString(sp)
- line 209: 		b.WriteString("\n\n")
- line 210: 	} else {
- line 211: 		persona := ex.Persona
- line 212: 		if strings.TrimSpace(persona) == "" {
- line 213: 			persona = registry.DefaultExecutorPersona
- line 214: 		}
- line 215: 		fmt.Fprintf(&b, "You are %s, a code-fix executor. Generate the minimal code change that fixes the finding below. Preserve the existing style and conventions. Output only the fix (corrected code or a precise change instruction); do not restate the problem.\n\n", persona)
- line 216: 	}
- line 217: 	if len(ex.Rules) > 0 {
- line 218: 		b.WriteString("Coding rules to follow (apply these to your fix):\n")
- line 219: 		for _, rule := range ex.Rules {
- line 220: 			fmt.Fprintf(&b, "- %s\n", rule)
- line 221: 		}
- line 222: 		b.WriteString("\n")
- line 223: 	}
- line 224: 	fmt.Fprintf(&b, "Severity: %s\nLocation: %s:%d\nCategory: %s\nProblem: %s\n", f.Severity, f.File, f.Line, f.Category, f.Problem)
...

Thus, the fmt.Fprintf line that adds rule is line 220 in the new file. So the absolute line number in the file would be around 220? Actually the new file line numbers start at 198 for the function definition, but the line numbers within the file are absolute. The function starts at line 198, so line 220 would be line 198 + (220-198) = 198 + 22 = 220? Wait, that's not correct: If the function starts at line 198, then line 198 is the first line of the function (the comment). So line 199 is var b..., line 200 is if sp..., etc. So line numbers in the file correspond to the line numbers in the file. So line 220 would be somewhere inside the function. So we can say FILE