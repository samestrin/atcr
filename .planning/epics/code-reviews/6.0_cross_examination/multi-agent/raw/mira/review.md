<think>
Let me analyze this diff for Epic 6.0 (Cross-Examination/Debate Stage) carefully. I need to focus on what's in scope per the sprint plan:

IN SCOPE per sprint plan:
- T1: trigger detection + item selection
- T2: debate protocol engine
- T3: judge envelope + ruling integration
- T4: `atcr debate` command, chaining flags, MCP tool
- T5: report.md "Contested findings" section + docs
- New `debate.*` config block
- Updates to verification struct, registry, report

Let me review the diff for issues:

1. **`internal/debate/protocol.go` - `nonOKStatus` function unused parameter** - Actually wait, it IS used. Let me re-check.

2. **`cmd/atcr/review.go`** - The `runReview` function has a `verifyFlag` and `debateFlag`. Let me check the logic for the chain.

   Looking at the logic:
   - When `--verify` is set, `verify.Verify()` runs
   - When `--debate` is set, `debate.Debate()` runs
   - Both can run together
   - The gate runs at the end if threshold is set

3. **`internal/debate/debate.go`** - The `runDebate` function reads findings, builds radar, filters, selects, runs debate for each item.

4. **Reading the discussion carefully** - There's a flag handling issue. The `boolFlag(cmd, "single-model")` is used, but the flag is added in `newDebateCmd` and `newReviewCmd`. Let me check both:

   In `cmd/atcr/debate.go`:
   ```go
   cmd.Flags().Bool("single-model", false, "...")
   ```
   
   In `cmd/atcr/review.go`:
   ```go
   cmd.Flags().Bool("debate", false, "...")
   cmd.Flags().Bool("single-model", false, "with --debate: ...")
   ```
   
   The `boolFlag` function likely gets flag values from the command. This should be fine.

5. **`internal/debate/cast.go` - The `pickDistinct` function** - Let me check the `AgentsByRole` call. The function signature takes a role string and label, but I want to check the implementation. Wait, `AgentsByRole(role)` is called and returns a map keyed by agent name. Then it iterates with `candidates[name]`. This looks fine.

6. **`internal/debate/debate.go` - The `debateOne` function** - It opens a transcript, then calls `RunDebate`, which is a method on `*Record`. Wait, looking more carefully:

   ```go
   rec := RunDebate(ctx, item, cast, cc, disp, tr)
   ```
   
   This is a function call. Then:
   ```go
   if judgeHalted(rec.Halted) {
   ```
   
   That's correct, accessing struct field.

7. **`internal/debate/emit.go` - The `applyRulings` function** - It mutates findings in place. Looking at this:
   ```go
   findings[i].Verification = &reconcile.Verification{
       Verdict:           ra.verdict,
       Skeptic:           ra.judge,
       Notes:             ra.reasoning,
       ChallengeSurvived: ra.survived,
   }
   ```
   
   The judge's agent is being placed in the `Skeptic` field of the `Verification` struct. This could be misleading because the field is named "Skeptic" but the value is actually a "judge" agent. However, the field is generic (just the agent that produced the verdict), so this is reasonable.

8. **`internal/debate/protocol.go` - Looking at the sentinel generation**:
   ```go
   sentinel := fmt.Sprintf("%08x", rand.Uint32())
   ```
   
   This uses the default `math/rand` source which is not seeded with crypto-random. For non-cryptographic purposes (sentinel tags), this is fine. But worth noting.

9. **`internal/debate/cast.go` - Looking at `pickDistinct`**:
   ```go
   candidates := reg.AgentsByRole(role)
   names := make([]string, 0, len(candidates))
   for name := range candidates {
       names = append(names, name)
   }
   sort.Strings(names)
   for _, name := range names {
       cfg := candidates[name]
       if excluded[cfg.Model] {
           continue
       }
       prov, ok := resolveProvider(reg, cfg)
       if !ok {
           continue
       }
       return Caster{Label: label, Agent: name, Config: cfg, Provider: prov}, true
   }
   ```
   
   This looks fine.

10. **`internal/debate/debate.go` - The `runDebate` function** - I see something interesting:
    ```go
    if len(sel.Selected) > 0 {
        c, d, cleanup, herr := newHarness()
        if herr != nil {
            log.FromContext(ctx).Warn("debate: tool harness unavailable; selected items will be unresolved")
        } else {
            cc, disp = c, d
            if cleanup != nil {
                defer cleanup()
            }
        }
    }
    ```
    
    If `herr != nil`, then `cc` and `disp` remain nil. Then in `debateOne`:
    ```go
    if cc == nil {
        return "", fanout.StatusFailed
    }
    ```
    
    Wait, that's in `driveSeat` which is called from `runTurn`. So if cc is nil, all three seats will halt. This is correct.

11. **`internal/debate/emit.go` - `updateManifestStage` function** - I see a subtle issue:
    ```go
    if len(stages) == 0 {
        stages = []string{"review"}
    }
    m["stages"] = append(stages, debateStage)
    ```
    
    If the manifest has no stages, it seeds with "review" and adds "debate". This makes the result ["review", "debate"]. But if the manifest has "reconcile" but not "review", this could be wrong. However, the comment says "A manifest with no stages is seeded with 'review' first" - this is a design choice.

12. **`internal/debate/emit.go` - The `ItemResult` struct** - It has a `Kind` field that uses the reconcile's `Kind*` constants. That's a string, which is fine for cross-package boundaries.

13. **`cmd/atcr/review.go` - Looking at the gate logic**:
    ```go
    if threshold != "" || verifyFlag {
        rec, rerr := reconcile.RunReconcile(...)
        ...
    }
    if verifyFlag {
        vres, verr := verify.Verify(...)
        ...
    }
    if debateFlag {
        dres, derr := debate.Debate(...)
        ...
    }
    
    // Gate on the post-chain findings.json when a stage rewrote it; otherwise
    // gate the reconcile result directly.
    if verifyFlag || debateFlag {
        if threshold != "" {
            ...
        }
        return nil
    }
    ```
    
    Wait, this has a subtle issue. If `--debate` is set without `--verify`, then `verifyFlag` is false, so the `if threshold != "" || verifyFlag` block won't run, which means reconcile won't run! But the comment in the code says:
    > "debate runs after reconcile"
    
    And the original code was `if threshold != "" || verifyFlag`. So this is a bug - if `--debate` is set alone (without threshold and without --verify), the reconcile stage won't run.
    
    Let me re-check the diff:
    ```go
    -	if threshold != "" || verifyFlag {
    +	if threshold != "" || verifyFlag || debateFlag {
    ```
    
    OK, so it does include `debateFlag` in the condition. Good, that's a fix.

14. **Looking at the gate logic in the post-chain block**:
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
    
    This is correct - it only enters this block if either verify or debate ran. Good.

15. **`cmd/atcr/review.go` - `debateFlag` retrieval**:
    ```go
    debateFlag, _ := cmd.Flags().GetBool("debate")
    ```
    
    This is fine.

16. **`internal/mcp/handlers.go` - `handleDebate`**:
    ```go
    res, err := debate.Debate(ctx, e.root, dir, reg, debate.Options{SingleModel: in.SingleModel})
    if err != nil {
        if errors.Is(err, debate.ErrNoReconciledFindings) {
            return nil, DebateResult{}, fmt.Errorf("no reconciled findings found in %s — run 'atcr reconcile' first", dir)
        }
        return nil, DebateResult{}, err
    }
    ```
    
    This is correct.

17. **`internal/debate/debate.go` - `filterAlreadyDebated` function** - Looks correct. It uses the `File+Line+Problem` triple to identify findings.

18. **`internal/debate/protocol.go` - `buildJudgePrompt`** - The function uses `item.Kind` to decide whether to include `cluster_decision`. But `Kind` is a string from `reconcile`. Let me check:
    ```go
    if item.Kind == reconcile.KindGrayZone {
        b.WriteString(`"cluster_decision": "merge|separate", `)
    }
    ```
    
    This is fine.

19. **`internal/debate/protocol.go` - The `block` function**:
    ```go
    func block(name, sentinel, content string) string {
        tag := name + "-" + sentinel
        return "<" + tag + ">\n" + content + "\n</" + tag + ">"
    }
    ```
    
    This wraps untrusted content in sentinel-tagged blocks. Good defense against prompt injection.

20. **`internal/debate/cast.go` - The `resolveProposer` function** - Looking at the fallback:
    ```go
    // Fallback: any reviewer-role agent, deterministically.
    if seat, ok := pickDistinct(reg, registry.RoleReviewer, LabelProposer, nil); ok {
        return seat, true
    }
    return Caster{}, false
    ```
    
    Wait, the `pickDistinct` function uses `excludeModels` to filter. The fallback passes `nil` for excludeModels, which means no filtering. So if the crediting reviewer doesn't resolve, the fallback picks any reviewer-role agent. But this means the proposer might be the SAME model as another reviewer. That's OK because the challenger and judge are still distinct from the proposer.

    Actually wait, there's a potential issue. If `resolveProposer` returns a proposer that's NOT from the crediting reviewers list, but is a fallback reviewer, then `pickDistinct` for challenger will exclude that proposer's model. This might prevent using the actual crediting reviewers as challengers. Let me re-check.

    Actually no, `pickDistinct` looks at the registry's AgentsByRole(role), not the finding's reviewers. So this is fine.

21. **`internal/debate/debate.go` - The `Debate` function** - I see something interesting:
    ```go
    func Debate(ctx context.Context, repoRoot, reviewDir string, reg *registry.Registry, opts Options) (Result, error) {
        harness := func() (fanout.ChatCompleter, Dispatcher, func(), error) {
            disp, cleanup, err := buildDispatcher(repoRoot, reviewDir)
            if err != nil {
                return nil, nil, nil, err
            }
            return llmclient.New(), disp, cleanup, nil
        }
        return runDebate(ctx, reviewDir, reg, opts, harness)
    }
    ```
    
    The `repoRoot` parameter is captured in the closure, which is fine for tests because they use a different harness.

22. **`internal/debate/cast.go` - The `resolveProvider` function**:
    ```go
    func resolveProvider(reg *registry.Registry, cfg registry.AgentConfig) (registry.Provider, bool) {
        if reg.Providers == nil {
            return registry.Provider{}, true
        }
        p, ok := reg.Providers[cfg.Provider]
        return p, ok
    }
    ```
    
    The first check returns `(registry.Provider{}, true)` if `Providers` is nil. This is the "tolerant" behavior for unvalidated test registries. The second check returns `(p, ok)` based on lookup. If `cfg.Provider` is not in the map, `ok` is false. Good.

23. **`internal/debate/protocol.go` - `driveSeat`**:
    ```go
    func driveSeat(ctx context.Context, seat Caster, prompt string, cc fanout.ChatCompleter, disp Dispatcher) (string, string) {
        if cc == nil {
            return "", fanout.StatusFailed
        }
        ...
    }
    ```
    
    This is the check for nil completer.

24. **Looking at the `--debate` flag without `--verify`** - The current logic in `review.go` chains `--debate` after `--verify`. But what if the user wants to chain `--debate` without `--verify`? The diff does allow that:
    ```go
    if debateFlag {
        dres, derr := debate.Debate(ctx, ".", result.Dir, cfg.Registry, debate.Options{
            SingleModel: boolFlag(cmd, "single-model"),
        })
    ```
    
    But the comment in the long help says:
    > "Optional args: id_or_path (review id only; defaults to the latest review), singleModel, failOn (CRITICAL|HIGH|MEDIUM|LOW), requireVerified."
    
    And the help text in `newDebateCmd` says:
    > "Runs after `atcr reconcile` (and, for verification disagreements, after `atcr verify`)."
    
    So debate alone is supported.

25. **`cmd/atcr/review.go` - the message changes** - The error message changes from "verification" to "cross-examination" when --debate runs. This is good but might break existing CI scripts. The comment in the code says "Keep the verify-only message stable (operators/CI may match on it); name cross-examination only when --debate actually ran." This is a thoughtful backward-compat choice.

26. **`internal/debate/select.go` - `sortByPriority`**:
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
    
    This uses `stream.NormalizeSeverity` to canonicalize. The ranking then uses `reconcile.SeverityRank`. This is fine.

27. **Looking at `driveSeat`** - It always invokes a new engine with one slot. This is correct for the debate protocol.

28. **`internal/debate/emit.go` - `DebateFile` struct** - The `Overflow` field is `[]OverflowItem`. The JSON tag is `overflow`. If overflow is nil, it will be `null` in JSON. The `writeDebateFile` function ensures it's not nil:
    ```go
    if df.Overflow == nil {
        df.Overflow = []OverflowItem{}
    }
    ```
    
    Good.

29. **`internal/debate/emit.go` - `ItemResult`** - Some fields use `omitempty`:
    - `problem`, `reason`, `original_severity`, `settled_severity`, `cluster_decision`, `challenge_survived`, `single_model`, `proposer`, `challenger`, `judge`, `reasoning`, `transcript`
    
    But the `file`, `line`, `kind`, `outcome` fields don't have omitempty. This is fine.

30. **`cmd/atcr/review.go` - Flag retrieval** - `boolFlag(cmd, "single-model")` is called multiple times. The flag exists in both `newDebateCmd` and `newReviewCmd`. If the user runs `atcr review --debate --single-model`, it works. If the user runs `atcr review --single-model` (without --debate), the flag is still set but unused. That's harmless.

31. **`cmd/atcr/debate.go` - Error handling**:
    ```go
    if err != nil {
        if errors.Is(err, debate.ErrNoReconciledFindings) {
            return fmt.Errorf("no reconciled findings found in %s — run 'atcr reconcile' first", reviewDir)
        }
        return usageError(err)
    }
    ```
    
    Wait, the function `runDebateCmd` first calls `resolveReviewDir(arg)`. If that errors, it wraps with `debateFailureError`. Then it calls `fanout.LoadReviewConfig` and wraps with `usageError`. Then it calls `debate.Debate`. The error from `Debate` is checked for `ErrNoReconciledFindings` first. If it's not that, it returns `usageError(err)`.
    
    But wait, `debateFailureError` is only called for `resolveReviewDir` errors. What about the case where `Debate` returns an error that's NOT `ErrNoReconciledFindings`? It returns `usageError(err)`. That seems inconsistent.
    
    Let me re-check. Looking at the code:
    ```go
    res, err := debate.Debate(cmd.Context(), ".", reviewDir, cfg.Registry, debate.Options{
        SingleModel: boolFlag(cmd, "single-model"),
    })
    if err != nil {
        if errors.Is(err, debate.ErrNoReconciledFindings) {
            return fmt.Errorf("no reconciled findings found in %s — run 'atcr reconcile' first", reviewDir)
        }
        return usageError(err)
    }
    ```
    
    The "no reconciled findings" error returns a plain error, while other errors get wrapped with `usageError`. The comment in `debateFailureError` says:
    > "wraps a non-ErrNoReconciledFindings error from debate.Debate with a consistent "debate failed:" prefix so `atcr debate` and `atcr review --debate` produce identical stderr shapes. Both map to exit 2."
    
    But the actual code only wraps `resolveReviewDir` errors with `debateFailureError`, not the `Debate` errors. This looks like a bug! The code should use `debateFailureError` for the `Debate` error path too, to produce consistent stderr shape with `atcr review --debate`.

    Let me re-check `runDebateCmd`:
    ```go
    reviewDir, err := resolveReviewDir(arg)
    if err != nil {
        return debateFailureError(err) // missing/incomplete review → exit 2
    }
    
    cfg, err := fanout.LoadReviewConfig(".", registry.CLIOverrides{})
    if err != nil {
        return usageError(err) // missing/invalid registry → exit 2
    }
    
    res, err := debate.Debate(cmd.Context(), ".", reviewDir, cfg.Registry, debate.Options{
        SingleModel: boolFlag(cmd, "single-model"),
    })
    if err != nil {
        if errors.Is(err, debate.ErrNoReconciledFindings) {
            return fmt.Errorf("no reconciled findings found in %s — run 'atcr reconcile' first", reviewDir)
        }
        return usageError(err)
    }
    ```
    
    Hmm, so if `Debate` returns an error other than `ErrNoReconciledFindings`, it returns `usageError(err)`, not `debateFailureError(err)`. This seems like an inconsistency with the design intent.

    Looking at `cmd/atcr/review.go`:
    ```go
    if debateFlag {
        dres, derr := debate.Debate(ctx, ".", result.Dir, cfg.Registry, debate.Options{
            SingleModel: boolFlag(cmd, "single-model"),
        })
        if derr != nil {
            return debateFailureError(derr)
        }
    ```
    
    So `atcr review --debate` wraps Debate errors with `debateFailureError`, but `atcr debate` returns them as plain `usageError`. The intent comment says they should produce identical stderr shapes, but the code doesn't match.

    This is a real issue! Let me flag it.

    Wait, let me re-read the comment:
    > "debateFailureError wraps a non-ErrNoReconciledFindings error from debate.Debate with a consistent "debate failed:" prefix so `atcr debate` and `atcr review --debate` produce identical stderr shapes. Both map to exit 2."
    
    So the intent is that both should produce identical stderr shapes. The current code in `runDebateCmd` returns `usageError(err)` for non-ErrNoReconciledFindings errors, but `usageError` likely just returns the error as-is. Let me check what `usageError` does.

    I don't have the full code, but typically `usageError` formats the error with a "usage:" prefix or similar. The `debateFailureError` wraps with "debate failed:" prefix.

    So there's a mismatch:
    - `atcr debate` produces "usage: <error>" for Debate failures
    - `atcr review --debate` produces "debate failed: <error>" for Debate failures

    This is a real inconsistency. The code comment says they should be identical.

32. Let me look for more issues. Let me look at the `--debate` flag handling more carefully.

    In `cmd/atcr/review.go`:
    ```go
    cmd.Flags().Bool("debate", false, "one-shot: chain a cross-examination stage (proposer/challenger/judge) after reconcile/verify, settling disputed findings")
    cmd.Flags().Bool("single-model", false, "with --debate: allow the same-model persona fallback when fewer than 3 distinct models are available")
    ```
    
    The `--single-model` flag is in the `newReviewCmd` (review command), not specifically tied to debate. But the help text says "with --debate: ...". So if the user runs `atcr review --single-model` (without --debate), the flag is set but unused. That's harmless.

33. Looking at the `internal/debate/select.go` - The `ResolveConfig` function has a subtle behavior:
    ```go
    src := dc.Triggers
    if len(src) == 0 {
        src = []string{
            registry.DebateTriggerSeveritySplit,
            registry.DebateTriggerGrayZone,
            registry.DebateTriggerVerificationDisagreement,
        }
    }
    for _, t := range src {
        triggers[t] = true
    }
    ```
    
    If `dc.Triggers` is empty, all three are enabled. If `dc.Triggers` has one or more, only those are enabled. This is the "opt-out" model. But the registry's `applyDefaults` also sets defaults:
    ```go
    if len(r.Debate.Triggers) == 0 {
        r.Debate.Triggers = []string{
            DebateTriggerSeveritySplit,
            DebateTriggerGrayZone,
            DebateTriggerVerificationDisagreement,
        }
    }
    ```
    
    So the defaults are applied twice (once in `applyDefaults`, once in `ResolveConfig`). This is redundant but not incorrect.

34. Let me check the `runDebate` function's harness call more carefully:
    ```go
    var cc fanout.ChatCompleter
    var disp Dispatcher
    if len(sel.Selected) > 0 {
        c, d, cleanup, herr := newHarness()
        if herr != nil {
            log.FromContext(ctx).Warn("debate: tool harness unavailable; selected items will be unresolved")
        } else {
            cc, disp = c, d
            if cleanup != nil {
                defer cleanup()
            }
        }
    }
    ```
    
    If `herr != nil`, then `cc` and `disp` remain nil. Then in `debateOne`:
    ```go
    cast, ok, reason := CastRoles(reg, item, cfg)
    if !ok {
        ir.Outcome = OutcomeUnresolved
        ir.Reason = reason
        return ir
    }
    ```
    
    The cast might succeed, but then `RunDebate` is called with nil `cc`. In `driveSeat`:
    ```go
    if cc == nil {
        return "", fanout.StatusFailed
    }
    ```
    
    So all three seats will halt, and `judgeHalted` will be true, so the outcome is `unresolved`. Good.

35. Let me check the `ItemResult` JSON serialization. The `problem` field has `omitempty`:
    ```go
    Problem string `json:"problem,omitempty"`
    ```
    
    But the `File` and `Line` fields don't. If `File` is empty, it will be serialized as `"file":""`. That's a minor inconsistency but not a bug.

36. Looking at `internal/debate/emit.go` - `writeDebateFile`:
    ```go
    if df.Items == nil {
        df.Items = []ItemResult{}
    }
    if df.Overflow == nil {
        df.Overflow = []OverflowItem{}
    }
    ```
    
    Good, ensures non-nil slices for JSON serialization.

37. Let me check the `Debate` function's handling of `reg` being nil:
    ```go
    func Debate(ctx context.Context, repoRoot, reviewDir string, reg *registry.Registry, opts Options) (Result, error) {
    ```
    
    The `reg` is not checked for nil here. It would be passed to `runDebate`, which would call `reg.Debate` to get the config. If `reg` is nil, this would panic. Let me check the callers.

    In `cmd/atcr/debate.go`:
    ```go
    cfg, err := fanout.LoadReviewConfig(".", registry.CLIOverrides{})
    if err != nil {
        return usageError(err) // missing/invalid registry → exit 2
    }
    
    res, err := debate.Debate(cmd.Context(), ".", reviewDir, cfg.Registry, debate.Options{
        SingleModel: boolFlag(cmd, "single-model"),
    })
    ```
    
    `cfg.Registry` is likely never nil after `LoadReviewConfig` succeeds. But in the MCP handler:
    ```go
    reg, err := e.loadVerifyRegistry(in.RegistryPath)
    if err != nil {
        return nil, DebateResult{}, err
    }
    
    res, err := debate.Debate(ctx, e.root, dir, reg, debate.Options{SingleModel: in.SingleModel})
    ```
    
    `reg` could potentially be nil if `loadVerifyRegistry` returns nil on error. But the error check before prevents that.

    In `cmd/atcr/review.go`:
    ```go
    dres, derr := debate.Debate(ctx, ".", result.Dir, cfg.Registry, debate.Options{
        SingleModel: boolFlag(cmd, "single-model"),
    })
    ```
    
    Same as above.

    So in practice, `reg` is never nil. But there's no defensive check in `Debate` itself. This is a minor robustness concern.

38. Let me check the `nonOKStatus` function:
    ```go
    func nonOKStatus(status string) string {
        if status == fanout.StatusOK {
            return ""
        }
        return status
    }
    ```
    
    Wait, what's the constant `fanout.StatusOK`? Let me check if it's defined. The code uses it, so it must be defined in the fanout package. This is fine.

39. Let me look at the `RunDebate` function more carefully:
    ```go
    func RunDebate(ctx context.Context, item reconcile.DisagreementItem, cast Cast, cc fanout.ChatCompleter, disp Dispatcher, tr *Transcript) Record {
        rec := Record{Item: item, Cast: cast, Resolved: true, SingleModel: cast.SingleModel}
    ```
    
    `tr` is passed as a pointer, but the function signature doesn't check for nil. Let me check the `RecordTurn` and `RecordRuling` methods:
    ```go
    func (t *Transcript) write(ev any) {
        if t == nil || t.disabled || t.bw == nil {
            return
        }
    ```
    
    So `nil` is handled. Good.

40. Let me check the `defer` order in `debateOne`:
    ```go
    tr := OpenTranscript(filepath.Join(debateDir, id, "transcript.jsonl"))
    defer func() { _ = tr.Close() }()
    ```
    
    The transcript is opened, then `RunDebate` is called, which uses the transcript. The defer ensures it's closed. Good.

41. Let me check the `applyRulings` function's interaction with the gate:
    ```go
    findings[i].Verification = &reconcile.Verification{
        Verdict:           ra.verdict,
        Skeptic:           ra.judge,
        Notes:             ra.reasoning,
        ChallengeSurvived: ra.survived,
    }
    findings[i].Confidence = reconcile.ConfidenceForVerdict(findings[i].Confidence, ra.verdict)
    ```
    
    For an `uphold` ruling:
    - `verdict` = `VerdictConfirmed` ("confirmed")
    - `survived` = true
    - `ConfidenceForVerdict` returns `VERIFIED`
    
    For an `overturn` ruling:
    - `verdict` = `VerdictRefuted` ("refuted")
    - `survived` = false
    - `ConfidenceForVerdict` returns `ConfLow` ("LOW")
    
    For a `split` ruling:
    - `verdict` = `VerdictConfirmed` ("confirmed")
    - `survived` = true
    - `severity` is the settled severity
    - `ConfidenceForVerdict` returns `VERIFIED`
    
    This matches the sprint plan's gate semantics.

42. Let me check the `ItemResult.Reason` field. For an `unresolved` outcome, the `Reason` is set. For other outcomes, it's not. The JSON tag is `reason,omitempty`, so it will be omitted when empty. Good.

43. Let me check the `single_model` field in `ItemResult`:
    ```go
    SingleModel       bool   `json:"single_model,omitempty"`
    ```
    
    It's a bool with omitempty. So when false, it's omitted. When true, it appears. Good.

44. Let me check the `Overflow` field in `DebateFile`:
    ```go
    Overflow      []OverflowItem `json:"overflow"`
    ```
    
    No omitempty. The `writeDebateFile` function ensures it's not nil, so it will always be an empty array `[]` in JSON, not `null`. Good.

45. Let me check the `loadContested` function in `cmd/atcr/report.go`:
    ```go
    func loadContested(reviewDir string) report.ContestedReport {
        df, found, err := debate.ReadDebateFile(reviewDir)
        if err != nil || !found {
            return report.ContestedReport{}
        }
        items := make([]report.Contested, 0, len(df.Items))
        for _, it := range df.Items {
            items = append(items, report.Contested{
                File:              it.File,
                Line:              it.Line,
                Outcome:           it.Outcome,
                OriginalSeverity:  it.OriginalSeverity,
                SettledSeverity:   it.SettledSeverity,
                Judge:             it.Judge,
                Reasoning:         it.Reasoning,
                Reason:            it.Reason,
                ChallengeSurvived: it.ChallengeSurvived,
                SingleModel:       it.SingleModel,
                ClusterDecision:   it.ClusterDecision,
            })
        }
        return report.ContestedReport{Items: items, Overflow: len(df.Overflow)}
    }
    ```
    
    The `Overflow` is set to `len(df.Overflow)`. But the `ContestedReport.Overflow` field is an `int`:
    ```go
    type ContestedReport struct {
        Items    []Contested
        Overflow int
    }
    ```
    
    So it just counts the overflow items. Good.

46. Let me check the `writeContestedSection` function in `internal/report/contested.go`:
    ```go
    func writeContestedSection(b *bytes.Buffer, cr ContestedReport) {
        if !cr.HasContent() {
            return
        }
        fmt.Fprintf(b, "\n## Contested findings\n\nDebated %d finding(s) through cross-examination (proposer/challenger/judge).\n", len(cr.Items))
    ```
    
    The section header is preceded by a newline. This is good for separating from the previous section.

47. Let me check the `severityTransition` function:
    ```go
    func severityTransition(c Contested) string {
        if c.Outcome == "split" && c.SettledSeverity != "" && c.SettledSeverity != c.OriginalSeverity {
            return fmt.Sprintf(" (%s → %s)", esc(c.OriginalSeverity), esc(c.SettledSeverity))
        }
        if c.OriginalSeverity != "" {
            return fmt.Sprintf(" (%s)", esc(c.OriginalSeverity))
        }
        return ""
    }
    ```
    
    The "→" character is a Unicode arrow. The `esc` function should handle this. But let me check if `esc` exists. It's used in the report package, so it must be defined.

48. Let me look for potential issues with the `runDebate` function's handling of `reg` being nil:
    ```go
    func runDebate(ctx context.Context, reviewDir string, reg *registry.Registry, opts Options, newHarness harnessFunc) (Result, error) {
        start := time.Now()
        cfg := ResolveConfig(reg.Debate)
    ```
    
    If `reg` is nil, `reg.Debate` would panic. This is a defensive coding concern. But as I noted, the callers always pass a non-nil reg.

49. Let me check the `DebateFile` struct's `SchemaVersion` field:
    ```go
    SchemaVersion string         `json:"schemaVersion"`
    ```
    
    It's exported as `schemaVersion` in JSON. The writeDebateFile function sets it:
    ```go
    writeDebateFile(reviewDir, DebateFile{
        SchemaVersion: DebateSchemaVersion,
        Items:         items,
        Overflow:      overflowItems(sel.Overflow),
    })
    ```
    
    Good.

50. Let me check the `transcript.go` file:
    ```go
    var transcriptLog = func(format string, a ...any) {
        fmt.Fprintf(os.Stderr, "atcr: warning: "+format+"\n", a...)
    }
    ```
    
    This is a package-level variable that logs to stderr. It's used for transcript I/O failures. Good.

51. Let me look at the `OpenTranscript` function:
    ```go
    func OpenTranscript(path string) *Transcript {
        f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
        if err != nil {
            transcriptLog("debate: open transcript %s: %v", path, err)
            return &Transcript{disabled: true}
        }
        return &Transcript{bw: bufio.NewWriter(f), closer: f}
    }
    ```
    
    Good - it returns a disabled writer on error.

52. Let me look at the `extractJSONObject` function in `envelope.go`:
    ```go
    func extractJSONObject(s string) string {
        start := strings.IndexByte(s, '{')
        if start < 0 {
            return ""
        }
        depth := 0
        inStr := false
        escaped := false
        for i := start; i < len(s); i++ {
            c := s[i]
            if inStr {
                switch {
                case escaped:
                    escaped = false
                case c == '\\':
                    escaped = true
                case c == '"':
                    inStr = false
                }
                continue
            }
            switch c {
            case '"':
                inStr = true
            case '{':
                depth++
            case '}':
                depth--
                if depth == 0 {
                    return s[start : i+1]
                }
            }
        }
        return ""
    }
    ```
    
    This is a string-aware JSON object extractor. It handles escaped quotes inside strings. Good.

53. Let me look at the `parseRuling` function:
    ```go
    func parseRuling(response string) Ruling {
        if strings.TrimSpace(response) == "" {
            return Ruling{Outcome: OutcomeUnresolved, Reasoning: "empty_response"}
        }
</think>
