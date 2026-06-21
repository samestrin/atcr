The user wants me to review the diff provided and identify issues, outputting findings in a specific pipe-delimited format. I need to apply the scope constraint from the sprint plan: only flag issues in files/areas directly related to the work items (Epic 6.0: Cross-Examination). The diff adds the debate stage, CLI command, MCP tool, report contested section, debate protocol, casting, selection, envelope parsing, transcript, emit, and config changes.

I need to find real issues in the diff, focusing on:
- Security vulnerabilities
- Error handling
- Performance
- Correctness
- Maintainability

Let's carefully examine the diff.

**1. `internal/debate/debate.go`**
- `Debate` function has `harness` as a closure capturing variables. It returns `buildDispatcher` error.
- `runDebate`: 
  - `findings, err := reconcile.ReadReconciledFindings(reviewDir)` - if not exist, wraps with `ErrNoReconciledFindings`. Good.
  - `df := reconcile.LoadDisagreements(reviewDir, findings)` - uses disagreements.
  - `df.Items = filterAlreadyDebated(df.Items, findings)` - modifies the loaded disagreements file.
  - `sel := SelectItems(df, cfg)` - selects items.
  - Harness building: `c, d, cleanup, herr := newHarness()`. If `herr != nil`, it logs a warning but does NOT return. `cc` and `disp` remain nil if there was an error. Then later `debateOne` is called with nil `cc` and `disp`. In `protocol.go`, `driveSeat` checks `if cc == nil { return "", fanout.StatusFailed }`. So it halts. This seems like failure isolation is handled.
  - However, `defer cleanup()` is inside the `else` block. But if `newHarness()` returns an error, `cleanup` is nil or undefined? Actually if `herr != nil`, the code doesn't enter else, so no defer. That's fine.
  - Wait, the code:
    ```go
    if len(sel.Selected) > 0 {
        c, d, cleanup, herr := newHarness()
        if herr != nil {
            log...Warn(...)
        } else {
            cc, disp = c, d
            if cleanup != nil {
                defer cleanup()
            }
        }
    }
    ```
    This is okay, but if `herr != nil`, the loop runs with nil `cc`/`disp`, causing all items to be halted/unresolved. That's intentional failure isolation. But is it correct to not return an error? The comment says "A harness failure is non-fatal — the seats degrade and the affected items are recorded unresolved." So that's by design.
  - `for _, it := range sel.Selected { ir := debateOne(...); items = append(items, ir); tally(&res, ir); ... }`
  - After loop, if `len(rulings) > 0`, applies rulings and writes findings.
  - `writeDebateFile`, `updateManifestStage`.
  - `res.Selected = len(sel.Selected)` is set after the loop. But `res` was used in `tally` during the loop. `tally` increments `Upheld`, `Overturned`, etc. Then `Selected` is set. This seems fine.
  - `applyRulings` is called on `findings` which is a slice. It mutates findings in place.
  - `updateManifestStage` appends "debate" to manifest stages.

**2. `internal/debate/protocol.go`**
- `sentinel := fmt.Sprintf("%08x", rand.Uint32())` - uses `math/rand` without a source. In Go, `math/rand` default source is deterministic. But it's used as a sentinel to prevent prompt injection. If the source is deterministic, an attacker knowing the seed could predict the sentinel? The default source is seeded to 1 at program start. However, `math/rand`'s global functions are safe for concurrent use by multiple goroutines since Go 1.20? Actually since Go 1.20, the global random number generator is seeded randomly. Wait, let me recall: Go 1.20 automatically seeds the global generator with a random value. So it should be unpredictable. But is it cryptographically secure? No, `math/rand` is not cryptographically secure. For a prompt injection defense, using a predictable PRNG might be a concern if the attacker can infer the sequence. However, the attacker would need to know the state. Since it's global and many calls happen, it's probably fine, but ideally `crypto/rand` should be used for security-sensitive randomness. But is this HIGH severity? The sentinel is just a defense-in-depth mechanism against a very specific prompt injection (closing a tag). If the sentinel is guessable, the defense is weaker. But given the context (code review tool, local execution), this might be LOW or MEDIUM. Let's check the sprint scope. This is definitely in scope. I'll flag it as LOW or MEDIUM security. Actually, using `math/rand` for security-sensitive operations is generally discouraged. Let's mark it MEDIUM|security.
- `driveSeat`: 
  ```go
  if cc == nil {
      return "", fanout.StatusFailed
  }
  ```
  This handles nil cc.
- `engine := fanout.NewEngine(cc, opts...)` - okay.
- `results := engine.Run(ctx, []fanout.Slot{{Primary: agent}})` - okay.
- `if r.Status != fanout.StatusOK || len(r.TrippedBudgets) > 0 { return r.Content, fanout.StatusFailed }` - returns content even when failed? Yes, it returns partial content. Then `runTurn` records it and appends halted. That's okay.
- `buildDebateAgent`: 
  ```go
  Invocation: llmclient.Invocation{
      BaseURL:     seat.Provider.BaseURL,
      APIKeyEnv:   seat.Provider.APIKeyEnv,
      ...
      Prompt:      prompt,
  },
  ```
  The `Prompt` is included in the `llmclient.Invocation`. Is that correct? The `fanout.Agent` also has `Prompt`. The `llmclient.Invocation` might use `Prompt` as the actual prompt sent. But is there a risk of double-prompting or the engine ignoring `Agent.Prompt`? This seems like an integration detail. Not necessarily an issue.

**3. `internal/debate/cast.go`**
- `resolveProposer`: 
  ```go
  reviewers := append([]string{}, item.Reviewers...)
  sort.Strings(reviewers)
  ```
  This is fine.
- `pickDistinct`: `candidates := reg.AgentsByRole(role)`. The registry method returns a map. Then `names := make([]string, 0, len(candidates))`. Then iterates and sorts. Fine.
- `resolveProvider`: 
  ```go
  if reg.Providers == nil {
      return registry.Provider{}, true
  }
  ```
  This returns true for an unvalidated test registry. In production, `reg.Providers` being nil might cause a panic later? But it's a documented test tolerance. In `cast_test.go`, `rosterReg` sets Providers. But in `resolveProposer` fallback, if `reg.Providers` is nil, it returns a zero Provider and true, meaning the seat is cast with an empty Provider. Then `buildDebateAgent` will set `Provider: seat.Provider` into the Agent, and `llmclient.Invocation` will have empty `BaseURL` and `APIKeyEnv`. This might cause a runtime error later when the LLM client tries to use it. But `resolveProvider` says "a nil Providers map (an unvalidated test registry) yields a zero provider that the caller tolerates". The caller is `CastRoles`. Then `Debate` uses it. If the provider is empty, the LLM call will likely fail. But this is only for test registries. Is there a risk in production? If the registry is validated, Providers should not be nil. But what if an agent references a provider not in the map? `resolveProvider` with non-nil map and absent key returns `ok=false`, which correctly fails the cast. So this is safe for production.

**4. `internal/debate/emit.go`**
- `applyRulings`: 
  ```go
  for i := range findings {
      key := FindingKey{File: findings[i].File, Line: findings[i].Line, Problem: findings[i].Problem}
      ra, ok := rulings[key]
      if !ok {
          continue
      }
      if ra.severity != "" {
          findings[i].Severity = ra.severity
      }
      findings[i].Verification = &reconcile.Verification{...}
      findings[i].Confidence = reconcile.ConfidenceForVerdict(findings[i].Confidence, ra.verdict)
  }
  ```
  Wait: if `ra.verdict` is empty (OutcomeUnresolved), `ConfidenceForVerdict` returns prior. That's fine. But what about `ChallengeSurvived`? It's set based on `ra.survived`. For `OutcomeUphold` or `OutcomeSplit`, `Verdict()` returns `VerdictConfirmed`, `ChallengeSurvived()` returns true. For `OutcomeOverturn`, `Verdict()` returns `VerdictRefuted`, `ChallengeSurvived` is false. Good.
- `updateManifestStage`: 
  ```go
  rawStages, _ := m["stages"].([]any)
  stages := make([]string, 0, len(rawStages))
  for _, s := range rawStages {
      if str, ok := s.(string); ok {
          stages = append(stages, str)
      }
  }
  for _, s := range stages {
      if s == debateStage {
          return nil // already recorded
      }
  }
  if len(stages) == 0 {
      stages = []string{"review"}
  }
  m["stages"] = append(stages, debateStage)
  ```
  This has a subtle bug: if the manifest has stages as `[]interface{}` containing non-string elements (e.g., numbers), they are silently skipped. But more importantly, if `stages` is empty, it seeds with `["review"]`. But if the review actually had no stages? Unlikely. However, this mirrors `verify.UpdateManifestStage`.
  - The use of `append(stages, debateStage)` where `stages` is a newly allocated slice. But `m["stages"]` is assigned the result. This is fine.

**5. `internal/debate/envelope.go`**
- `parseRuling`: 
  ```go
  for {
      obj := extractJSONObject(rest)
      if obj == "" {
          next := strings.IndexByte(rest, '{')
          if next < 0 {
              break
          }
          rest = rest[next+1:]
          continue
      }
      var candidate struct{...}
      if json.Unmarshal([]byte(obj), &candidate) == nil && candidate.Outcome != nil {
          return ruleFromCandidate(...)
      }
      idx := strings.Index(rest, obj)
      rest = rest[idx+len(obj):]
  }
  ```
  Bug: `idx := strings.Index(rest, obj)`. If `rest` contains the object string, it finds it. But what if the extracted object appears multiple times? Unlikely. However, `rest = rest[idx+len(obj):]` could panic if `idx == -1` because the object wasn't found in `rest`. But `obj` was extracted from `rest` by `extractJSONObject(rest)`, so it must be a substring of `rest` starting at some index. However, if `rest` has changed? `rest` is the current iteration's string. `extractJSONObject` returns the first balanced object in `rest`. So `strings.Index(rest, obj)` should find it at the start (or later if there are duplicates). But what if `obj` contains special characters that `strings.Index` handles? It's fine. But `idx` could be -1 if `obj` is empty? No, `obj == ""` is handled earlier.
  Wait, there's a correctness issue: after extracting the object, `idx` is found using `strings.Index(rest, obj)`. If `rest` contains multiple occurrences, it finds the first one. But `extractJSONObject` already advanced past it. If there are identical JSON objects back-to-back, this might skip incorrectly. This is a bit fragile but probably not a real bug.
  Actually, looking closer: `extractJSONObject` returns `s[start : i+1]`. So it starts at the first `{`. `strings.Index(rest, obj)` will find it at `start`. Then `rest = rest[start+len(obj):]`. This is correct.
  However, there is a subtle bug in `extractJSONObject`: if there is a `{` inside a string that is not the start of an object? No, `extractJSONObject` starts at the first `{` outside a string. It handles string literals. Seems fine.

- `truncate`: 
  ```go
  cut := maxNotesLen
  for cut > 0 && (s[cut]&0xC0) == 0x80 {
      cut--
  }
  ```
  If `len(s) > maxNotesLen`, it sets `cut = maxNotesLen`. Then it checks `s[cut]`. But if `maxNotesLen` is exactly at a continuation byte, it backs up. This is correct UTF-8 truncation. But if `s` is ASCII, it's fine. One issue: if `maxNotesLen` is 2000, and the string is longer, but `s[2000]` happens to be a continuation byte, it might back up to 0 if every byte from 2000 down to 0 is a continuation byte? That would mean the string starts in the middle of a rune, which is invalid UTF-8. But the loop condition `cut > 0` prevents infinite loop and prevents cut from going below 0. But what if `s` is not valid UTF-8? Then `(s[cut]&0xC0) == 0x80` might be true for non-continuation bytes too? No, `0xC0` is `11000000`. `0x80` is `10000000`. The condition checks if the top two bits are `10`, which is exactly the continuation byte pattern. So it's correct for valid UTF-8. If the string is invalid UTF-8 and starts with a continuation byte at index 0? Then `s[0]&0xC0 == 0x80` is true, but `cut > 0` prevents going to 0? Actually if `cut` becomes 0, the loop condition `cut > 0` is false, so it stops. Then it returns `s[:0] + "…[truncated]"`. That's fine.

- `reviewSeverity`: checks canonical severities. Good.

**6. `cmd/atcr/debate.go`**
- `runDebateCmd`:
  ```go
  res, err := debate.Debate(cmd.Context(), ".", reviewDir, cfg.Registry, debate.Options{SingleModel: boolFlag(cmd, "single-model")})
  if err != nil {
      if errors.Is(err, debate.ErrNoReconciledFindings) {
          return fmt.Errorf("no reconciled findings found in %s — run 'atcr reconcile' first", reviewDir)
      }
      return usageError(err)
  }
  ```
  Wait: `debateFailureError` is defined but not used here! In `runDebateCmd`, when `err != nil` and it's not `ErrNoReconciledFindings`, it returns `usageError(err)`. But the comment on `debateFailureError` says it wraps a non-ErrNoReconciledFindings error with a consistent prefix. The CLI should probably use `debateFailureError(err)` instead of `usageError(err)` for non-reconcile errors to match the comment and the `--debate` chain path. In `review.go`, it uses `debateFailureError(derr)`. In `debate.go`, it does not. This is an inconsistency. Is it a bug? The comment says "Both map to exit 2." `usageError` also maps to exit 2. But the prefix "debate failed:" is missing. This is a maintainability/consistency issue. Let's flag it.
  Actually, looking at the code:
  ```go
  func runDebateCmd(...) error {
      ...
      res, err := debate.Debate(...)
      if err != nil {
          if errors.Is(err, debate.ErrNoReconciledFindings) {
              return fmt.Errorf(...)
          }
          return usageError(err)
      }
      ...
  }
  ```
  And `debateFailureError` is defined:
  ```go
  func debateFailureError(err error) error {
      return usageError(fmt.Errorf("debate failed: %w", err))
  }
  ```
  This function is unused in `debate.go`! It's only used in `review.go`. This is dead code in `debate.go` from the perspective of `debate.go` itself. Actually no, it's used in `review.go` which is the same package (`main`). So it's not dead code globally, but locally in `debate.go` it could be used for consistency. The fact that `runDebateCmd` doesn't use it means the error shape is inconsistent between standalone `atcr debate` and `atcr review --debate`. This is a correctness/maintainability issue.

**7. `cmd/atcr/review.go`**
- Gate logic:
  ```go
  if verifyFlag || debateFlag {
      if threshold != "" {
          findings, ferr := reconcile.ReadReconciledFindings(result.Dir)
          if ferr != nil {
              return usageError(ferr)
          }
          if n := reconcile.CountFailingJSON(findings, threshold, requireVerified); n > 0 {
              stage := "verification"
              if debateFlag {
                  stage = "cross-examination"
              }
              return fmt.Errorf("%d finding(s) at or above %s survived %s", n, threshold, stage)
          }
      }
      return nil
  }
  ```
  Wait: if `threshold != ""` but `n == 0`, it returns nil (success). That's fine. But what if `threshold == ""`? Then it returns nil immediately. That's fine.
  But there is a logic issue: the gate only runs when `verifyFlag || debateFlag`. If `debateFlag` is true but `verifyFlag` is false, and `threshold == ""`, it returns nil. That's correct because there's no gate threshold. But if `threshold != ""`, it counts failing findings. Good.
  However, note the comment: "Gate on the post-chain findings.json when a stage rewrote it; otherwise gate the reconcile result directly." But the code below this block is inside `if verifyFlag || debateFlag`. What if `threshold != ""` but neither verify nor debate ran? Then the original code before the diff handled it. Let's look at the original structure. Before the diff:
  ```go
  if threshold != "" || verifyFlag {
      rec, rerr := reconcile.RunReconcile(...)
      ...
      if verifyFlag {
          ...
          if threshold != "" {
              findings, ferr := reconcile.ReadReconciledFindings(result.Dir)
              ...
              if n > 0 { return fmt.Errorf(...) }
          }
          return nil
      }
  }
  ```
  After the diff:
  ```go
  if threshold != "" || verifyFlag || debateFlag {
      rec, rerr := reconcile.RunReconcile(...)
      ...
      if verifyFlag { ... }
      if debateFlag { ... }
      
      if verifyFlag || debateFlag {
          if threshold != "" {
              ...
          }
          return nil
      }
  }
  ```
  Wait, there is a subtle bug: if `debateFlag` is true but `verifyFlag` is false, and `threshold == ""`, the code enters the outer `if` because `debateFlag` is true. Then it runs reconcile. Then it runs debate. Then it enters `if verifyFlag || debateFlag` (true because debateFlag). Then `if threshold != ""` is false, so it does nothing and returns nil. That seems okay.
  But what if `threshold != ""` and `debateFlag` is true? It enters outer if, runs reconcile, runs debate, then reads findings and counts failing. Good.
  However, what if `threshold != ""`, `verifyFlag` is false, `debateFlag` is false? It enters the outer if (because threshold != ""). It runs reconcile. Then it does NOT enter `if verifyFlag || debateFlag`. So it falls through to the code below. Before the diff, it would have gated after reconcile. Let's check the code after this block.
  Looking at the diff, the code after the `if verifyFlag || debateFlag` block is... actually, the diff ends there. But in the original code, after the `if threshold != "" || verifyFlag` block, there might be more code. The diff shows:
  ```go
      if verifyFlag || debateFlag {
          if threshold != "" {
              ...
          }
          return nil
      }
  }
  ```
  Wait, the closing brace at line 299? Let's look at the indentation. Actually, the outer `if` starting at line 271 (in the diff) encloses the reconcile run and the verify/debate chains. The `if verifyFlag || debateFlag` block starting at line 302 is inside that outer `if`. But if `verifyFlag` is true and `threshold == ""`, it returns nil. If `debateFlag` is true and `threshold == ""`, it returns nil.
  But what if `threshold != ""` and neither verify nor debate ran? Then the outer `if` is entered. Reconcile runs. Then `if verifyFlag || debateFlag` is false. So it falls through. Before the diff, the gating logic was inside the `if verifyFlag` block, but there was also gate logic for reconcile-only? Let's check the original code. The diff shows:
  ```go
  -	if threshold != "" || verifyFlag {
  +	if threshold != "" || verifyFlag || debateFlag {
  ```
  And then later the verify block had gate logic. But now the gate logic is moved to `if verifyFlag || debateFlag`. If `threshold != ""` but no stage flag is set, the gate logic is lost! Actually no, the original code had:
  ```go
  if threshold != "" || verifyFlag {
      // run reconcile
      if verifyFlag {
          // run verify
          // gate
          return nil
      }
      // If no verify but threshold != "", what happened before? The diff doesn't show the original code after this.
  }
  ```
  Actually, looking at the diff more carefully:
  The original `if threshold != "" || verifyFlag {` block contained reconcile and then the verify chain. The verify chain ended with `return nil`. If `verifyFlag` was false but `threshold != ""`, the original code would run reconcile and then... what? The diff shows the block ending with `}` at line 299? No, let's re-read.
  The diff hunk for `review.go`:
  ```diff
  @@ -263,7 +271,7 @@ func runReview(cmd *cobra.Command, _ []string) error {
   	// error and short-circuits before this line. The FailureMarker correction in
   	// ReadManifestPartial is only needed by the out-of-process `atcr reconcile`
   	// path that runs after the fact against the on-disk summary.json.
  -	if threshold != "" || verifyFlag {
  +	if threshold != "" || verifyFlag || debateFlag {
   		rec, rerr := reconcile.RunReconcile(ctx, result.Dir, nil, reconcile.Options{
   			ReconciledAt: time.Now(),
   			Partial:      result.Summary.Partial,
  @@ -274,8 +282,10 @@ func runReview(cmd *cobra.Command, _ []string) error {
   		}
   		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reconciled %d finding(s)\n", rec.Summary.TotalFindings)
   
  -		// --verify implies the reconcile stage (run exactly once, above) and then
  -		// chains the adversarial verify stage in the same process (AC 04-02).
  +		// --verify chains the adversarial verify stage after the single reconcile
  +		// (AC 04-02). --debate then chains the cross-examination stage. Both mutate
  +		// the on-disk findings, so the one-shot gate runs LAST, on the post-chain
  +		// findings — a refuted or overturned finding never blocks the gate.
   		if verifyFlag {
   			vres, verr := verify.Verify(ctx, ".", result.Dir, cfg.Registry, verify.Options{
   				Fresh:       boolFlag(cmd, "fresh"),
  @@ -289,15 +299,35 @@ func runReview(cmd *cobra.Command, _ []string) error {
   				"verified %d finding(s): %d confirmed, %d refuted, %d unverifiable\n",
   				vres.FindingsProcessed, vres.VerdictCounts.Confirmed, vres.VerdictCounts.Refuted,
   				vres.VerdictCounts.Unverifiable)
  -			// Gate on the post-verify findings so a refuted finding never blocks the
  -			// one-shot gate (the whole point of the verify stage).
  +		}
  +		if debateFlag {
  +			dres, derr := debate.Debate(ctx, ".", result.Dir, cfg.Registry, debate.Options{
  +				SingleModel: boolFlag(cmd, "single-model"),
  +			})
  +			if derr != nil {
  +				return debateFailureError(derr)
  +			}
  +			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
  +				"debated %d item(s): %d upheld, %d overturned, %d split, %d unresolved (%d overflow)\n",
  +				dres.Selected, dres.Upheld, dres.Overturned, dres.Split, dres.Unresolved, dres.Overflow)
  +		}
  +
  +		// Gate on the post-chain findings.json when a stage rewrote it; otherwise
  +		// gate the reconcile result directly.
  +		if verifyFlag || debateFlag {
   			if threshold != "" {
   				findings, ferr := reconcile.ReadReconciledFindings(result.Dir)
   				if ferr != nil {
   					return usageError(ferr)
   				}
   				if n := reconcile.CountFailingJSON(findings, threshold, requireVerified); n > 0 {
  -					return fmt.Errorf("%d finding(s) at or above %s survived verification", n, threshold)
  +					// Keep the verify-only message stable (operators/CI may match on it);
  +					// name cross-examination only when --debate actually ran.
  +					stage := "verification"
  +					if debateFlag {
  +						stage = "cross-examination"
  +					}
  +					return fmt.Errorf("%d finding(s) at or above %s survived %s", n, threshold, stage)
   				}
   			}
   			return nil
  ```
  So previously, the gate logic was inside `if verifyFlag { ... }`. If `verifyFlag` was false but `threshold != ""`, the original code ran reconcile and then... did it gate? Actually the diff context doesn't show the original code after the verify block. It seems the original `if verifyFlag { ... }` block contained the gate and returned nil. If `verifyFlag` was false, the code after the `if verifyFlag` block (still inside `if threshold != "" || verifyFlag`) would continue. What was there? We can't see from the diff. But it's likely there was gate logic for the reconcile-only case too. Now, by wrapping the gate in `if verifyFlag || debateFlag`, if `threshold != ""` and neither flag is set, the gate is skipped entirely. But wait, the outer `if` condition includes `threshold != ""`. So if `threshold != ""` is true and both flags are false, the outer block runs, reconcile runs, but then the inner `if verifyFlag || debateFlag` is false, so it falls through. But what comes after? We can't see from the diff. The diff only shows up to the closing brace of the outer `if`. It's possible the original code after the verify block had `if threshold != "" { gate }`. The diff shows that the gate was moved. But if `threshold != ""` and no flags, the gate might be lost.
  Actually, looking at the diff lines: The original code had:
  ```go
  if threshold != "" || verifyFlag {
      // reconcile
      if verifyFlag {
          // verify
          // gate + return nil
      }
      // missing: reconcile-only gate?
  }
  ```
  I don't have the full file, but it's very likely that before this diff, there was gate logic after the `if verifyFlag` block for the reconcile-only case. The diff shows that the gate logic is now inside `if verifyFlag || debateFlag`. If `threshold != ""` but no stage flags, the gate is now skipped. This is a potential correctness issue. However, without seeing the original code, I can't be 100% sure. But given the structure, it's suspicious. Wait, the diff hunk ends at the closing brace of `if threshold != "" || verifyFlag || debateFlag`. The original hunk had 15 lines after the verify block that were replaced. The replacement includes the new gate block. So the original code likely had something like:
  ```go
  if threshold != "" || verifyFlag {
      rec, rerr := reconcile.RunReconcile(...)
      ...
      if verifyFlag {
          ...
          if threshold != "" {
              findings, ferr := reconcile.ReadReconciledFindings(result.Dir)
              ...
              if n > 0 { return fmt.Errorf(...) }
          }
          return nil
      }
      // reconcile-only gate?
      if threshold != "" {
          findings, ferr := reconcile.ReadReconciledFindings(result.Dir)
          ...
          if n > 0 { return fmt.Errorf(...) }
      }
  }
  ```
  But the diff shows the replacement entirely removes the gate from after the verify block and puts it inside `if verifyFlag || debateFlag`. So if `threshold != ""` and no flags, the gate might indeed be missing now. But wait, the replacement gate block says: "Gate on the post-chain findings.json when a stage rewrote it; otherwise gate the reconcile result directly." This comment implies that when no stage rewrote it, it should gate the reconcile result directly. But the code says `if verifyFlag || debateFlag { ... } else { // gate reconcile result? }`. The `else` is missing. So the comment contradicts the code? Actually, if neither flag is set, the outer `if` runs because of `threshold != ""`. Then reconcile runs. Then the inner `if verifyFlag || debateFlag` is false. So it falls through. Then what? The code after the outer `if` might handle it. But we can't see it. Let's assume the outer `if` ends and then there's code that handles the reconcile-only gate. But the diff doesn't show it because it's outside the changed hunk. However, it's also possible that the original code had the gate only inside the verify block, and for reconcile-only the gate was handled elsewhere. Without seeing the full file, it's hard to say. I should be careful not to flag a false positive.
  Let's look at the line numbers. The diff shows:
  ```diff
  @@ -263,7 +271,7 @@ func runReview(cmd *cobra.Command, _ []string) error {
  ```
  And ends at:
  ```diff
  @@ -289,15 +299,35 @@ func runReview(cmd *cobra.Command, _ []string) error {
  ```
  So the hunk replaces lines 289-303 (15 lines) with 35 lines. The original 15 lines were:
  ```
  289:			// Gate on the post-verify findings so a refuted finding never blocks the
  290:			// one-shot gate (the whole point of the verify stage).
  291:			if threshold != "" {
  292:				findings, ferr := reconcile.ReadReconciledFindings(result.Dir)
  293:				if ferr != nil {
  294:					return usageError(ferr)
  295:				}
  296:				if n := reconcile.CountFailingJSON(findings, threshold, requireVerified); n > 0 {
  297:					return fmt.Errorf("%d finding(s) at or above %s survived verification", n, threshold)
  298:				}
  299:			}
  300:			return nil
  301:		}
  ```
  Wait, lines 289-303 in the original? The diff marker `@@ -289,15 +299,35 @@` means the original hunk starts at line 289 and has 15 lines. The new hunk starts at 299 and has 35 lines. But the context before shows the `if verifyFlag {` block. The original 15 lines are the gate logic and return inside the `if verifyFlag` block. So the original code was:
  ```go
  if verifyFlag {
      // verify run
      // Gate on the post-verify findings...
      if threshold != "" {
          ...
      }
      return nil
  }
  ```
  And after the `if verifyFlag` block (still inside `if threshold != "" || verifyFlag`), there was nothing else shown. So if `threshold != ""` and `verifyFlag` was false, the outer block ran reconcile, then the inner `if verifyFlag` was skipped, and then it reached the end of the outer block. What happened after? The diff doesn't show lines after 303. But the original function likely continued and had gate logic elsewhere, or the outer `if` was the only thing. Actually, looking at the diff context, after line 303 there is no more code shown in the original. But the new code has more lines. It's possible the original code had:
  ```go
  }
  // end of outer if
  // more code below
  ```
  But the diff only shows the changed part. I think it's safer not to flag this as it's unclear.

**8. `internal/debate/select.go`**
- `sortByPriority`: 
  ```go
  sort.SliceStable(items, func(i, j int) bool {
      a, b := items[i], items[j]
      ra := reconcile.SeverityRank[stream.NormalizeSeverity(a.Severity)]
      rb := reconcile.SeverityRank[stream.NormalizeSeverity(b.Severity)]
      if ra != rb {
          return ra > rb
      }
      if a.Score != b.Score {
          return a.Score > b.Score
      }
      if a.File != b.File {
          return a.File < b.File
      }
      if a.Line != b.Line {
          return a.Line < b.Line
      }
      return a.Kind < b.Kind
  })
  ```
  This sorts in place. But `items` is a slice of structs. The `SeverityRank` map lookup: what if `NormalizeSeverity` returns something not in the map? Then `ra` or `rb` would be 0. This is okay, but low-severity items might get rank 0. The map probably contains all standard severities. If a severity is unknown, both get 0, and it falls through to score/file/line/kind. That's acceptable.
  But wait, `items` is passed by value to `sortByPriority`? No, it's a slice header passed by value, but the underlying array is modified. That's fine.

**9. `internal/debate/protocol.go` prompt building**
- `buildJudgePrompt`: constructs a prompt with `cluster_decision` only for gray zone. It appends `cluster_decision` to the JSON schema in the prompt. But in the JSON example string in the prompt:
  ```go
  b.WriteString(`{"outcome": "uphold|overturn|split", "settled_severity": "CRITICAL|HIGH|MEDIUM|LOW", `)
  if item.Kind == reconcile.KindGrayZone {
      b.WriteString(`"cluster_decision": "merge|separate", `)
  }
  b.WriteString(`"reasoning": "..."}`)
  ```
  The resulting JSON example might have a trailing comma before `reasoning` if `cluster_decision` is included. JSON does not allow trailing commas. The LLM might output a trailing comma, which would be invalid JSON and could cause parsing issues? But `parseRuling` uses `json.Unmarshal` on the extracted object, and `json.Unmarshal` does NOT allow trailing commas. However, the prompt is just an example; the LLM might copy the pattern. If the LLM outputs:
  ```json
  {"outcome": "uphold", "settled_severity": "MEDIUM", "cluster_decision": "merge", "reasoning": "..."}
  ```
  That's valid. But the example in the prompt is:
  ```
  {"outcome": "uphold|overturn|split", "settled_severity": "CRITICAL|HIGH|MEDIUM|LOW", "cluster_decision": "merge|separate", "reasoning": "..."}
  ```
  Wait, the code writes:
  ```go
  b.WriteString(`{"outcome": "uphold|overturn|split", "settled_severity": "CRITICAL|HIGH|MEDIUM|LOW", `)
  if item.Kind == reconcile.KindGrayZone {
      b.WriteString(`"cluster_decision": "merge|separate", `)
  }
  b.WriteString(`"reasoning": "..."}`)
  ```
  If cluster_decision is written, the string becomes:
  `{"outcome": "uphold|overturn|