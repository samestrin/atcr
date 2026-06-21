Let me analyze the diff carefully, focusing on the sprint plan scope (Epic 6.0: Cross-Examination/Debate Stage).

Key files in scope:
- cmd/atcr/debate.go, debate_test.go
- cmd/atcr/main.go, main_test.go (subcommand registration)
- cmd/atcr/report.go (contested section loading)
- cmd/atcr/review.go (--debate chaining)
- internal/boundaries_test.go (import allowlist)
- internal/debate/* (new package)
- internal/mcp/handlers.go, handlers_debate_test.go, server.go, server_test.go, tools.go, tools_test.go
- internal/reconcile/confidence.go, emit.go (ChallengeSurvived marker)
- internal/registry/config.go, debate_config_test.go (debate config)
- internal/report/contested.go, contested_test.go, render.go
- internal/verify/confidence_v2.go (refactored to use shared function)

Let me go through each file looking for issues:

1. **internal/debate/mock_test.go** - `fakeChatCompleter.Complete` method:
```go
func (f *fakeChatCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.turns) > 0 {
		return f.turns[0].content, nil
	}
	return "", nil
}
```
This always returns `f.turns[0]` and never advances the index. But `Chat` does advance. This seems like a bug in the mock - `Complete` always returns the first turn. However, looking at the code, `Complete` is the non-tool-loop completer, and the debate uses `Chat` (tool loop). So `Complete` might not be called. But if it is, it always returns the same turn. This is a test mock though, so severity is lower.

Actually wait - looking more carefully at `Chat`:
```go
func (f *fakeChatCompleter) Chat(ctx context.Context, _ llmclient.Invocation, _ []llmclient.Message, _ []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	f.mu.Lock()
	call := f.idx
	f.idx++
	var turn chatTurn
	if call < len(f.turns) {
		turn = f.turns[call]
	} else {
		turn = chatTurn{content: "default final answer"}
	}
	f.mu.Unlock()
	...
```
This looks correct - it advances through turns.

2. **internal/debate/protocol.go** - `rand.Uint32()` for sentinel:
```go
sentinel := fmt.Sprintf("%08x", rand.Uint32())
```
This uses `math/rand` which is not cryptographically secure, but for a sentinel tag in a prompt, it's probably fine. However, `rand` without seeding could produce predictable values. In Go 1.20+, the global rand is auto-seeded. But if this is pre-1.20, it would be deterministic. Let me check - the code uses `math/rand` not `crypto/rand`. For a security-sensitive sentinel (preventing prompt injection), using `math/rand` is a weakness. The verify stage likely uses the same pattern though. This is a MEDIUM security concern - the sentinel is meant to prevent early-close injection attacks, but `math/rand` is predictable.

Actually, looking at the comment: "One per-item sentinel tags every untrusted block... so reviewer- or model-authored content cannot forge a closing tag and inject instructions." If the sentinel is predictable (math/rand without proper seeding), an attacker who knows the seed could craft content to close the block early. But in practice, the attacker is the LLM output, which doesn't have access to the Go process's rand state. So this is probably LOW.

3. **internal/debate/debate.go** - In `runDebate`, when the harness fails:
```go
if herr != nil {
    log.FromContext(ctx).Warn("debate: tool harness unavailable; selected items will be unresolved")
} else {
    cc, disp = c, d
    if cleanup != nil {
        defer cleanup()
    }
}
```
Then later, `debateOne` is called with `cc` and `disp` which could be nil. Let me check if `debateOne` handles nil `cc` and `disp`:
- `CastRoles` doesn't use cc/disp
- `RunDebate` is called with cc and disp
- In `driveSeat`: `if cc == nil { return "", fanout.StatusFailed }` - handles nil cc
- But what about nil disp? `buildDebateAgent` passes disp via `fanout.WithDispatcher(disp)` only `if disp != nil`. So nil disp is handled.

Wait, but if the harness fails and cc is nil, then ALL items will be unresolved (because driveSeat returns StatusFailed for nil cc). This is logged but all items get "unresolved" with reason from CastRoles (which might succeed) or from the judge being halted. Actually, let me trace through:

If cc is nil:
- `CastRoles` might succeed (it doesn't use cc)
- `RunDebate` is called with nil cc
- `driveSeat` returns `"", fanout.StatusFailed` for all three seats
- All three seats are halted
- `judgeHalted` returns true
- Item is unresolved with reason "judge_halted"

This seems reasonable - failure isolation works.

4. **internal/debate/debate.go** - `debateOne` function:
```go
if err := os.MkdirAll(filepath.Join(debateDir, id), 0o755); err != nil {
    log.FromContext(ctx).Warn("debate: cannot create transcript dir", "err", err.Error())
}
```
The error is logged but execution continues. Then `OpenTranscript` will fail and return a disabled writer. This is fine - the transcript is best-effort.

5. **internal/debate/emit.go** - `updateManifestStage`:
```go
rawStages, _ := m["stages"].([]any)
```
The type assertion silently fails if stages is not `[]any`. If the manifest has `"stages": "review"` (a string instead of array), this would silently produce an empty stages slice, then seed it with "review" and append "debate". This could corrupt the manifest. But this mirrors verify's implementation, so it's probably an accepted pattern.

6. **internal/debate/envelope.go** - `parseRuling`:
The function scans for JSON objects with an "outcome" key. If the judge response contains multiple JSON objects, it picks the first one with an "outcome" key. This seems reasonable.

7. **internal/debate/protocol.go** - `buildDebateAgent`:
```go
Invocation: llmclient.Invocation{
    BaseURL:     seat.Provider.BaseURL,
    APIKeyEnv:   seat.Provider.APIKeyEnv,
    Model:       c.Model,
    Temperature: c.Temperature,
    Prompt:      prompt,
},
```
This looks correct - provider config is threaded through.

8. **cmd/atcr/review.go** - The gate logic:
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
Wait - when both `verifyFlag` and `debateFlag` are true, the message says "cross-examination" but the findings have gone through both verify and debate. The message could be misleading but it's a minor UX issue.

More importantly - when `debateFlag` is true but `verifyFlag` is false, the gate still runs. But the reconcile was triggered by `threshold != "" || verifyFlag || debateFlag`. So if only `--debate` is given (without `--verify`), the reconcile runs, then debate runs, then the gate runs. This seems correct.

But wait - what about `requireVerified`? If `--debate` is given without `--verify`, and `--require-verified` is set, the gate would count only VERIFIED findings. But without verify, no findings would be VERIFIED unless debate confirmed them. Let me check the validation:

Looking at the review.go code:
```go
cmd.Flags().Bool("debate", false, "one-shot: chain a cross-examination stage...")
cmd.Flags().Bool("single-model", false, "with --debate: allow the same-model persona fallback...")
```

And earlier (not in diff):
```go
// --require-verified hardens the one-shot gate to count only VERIFIED findings.
// It is meaningless without both a gate (--fail-on) and the verify stage that
// produces verdicts (--verify)
```

The validation for `--require-verified` likely checks that `--verify` is set. If `--debate` is used without `--verify`, `--require-verified` should probably also be valid since debate produces confirmed verdicts. But this validation is not in the diff, so it might be pre-existing. Let me not flag this since the validation code isn't shown.

9. **internal/debate/debate.go** - `runDebate`:
```go
df := reconcile.LoadDisagreements(reviewDir, findings)
```
`LoadDisagreements` returns a `DisagreementsFile`. If it fails to read the file, does it return an empty file or an error? Looking at the usage, it seems to return a value (not an error). So it probably returns an empty file on error. This is fine.

10. **internal/debate/cast.go** - `resolveProposer`:
```go
reviewers := append([]string{}, item.Reviewers...)
sort.Strings(reviewers)
```
This creates a copy before sorting, which is good - doesn't mutate the input.

11. **internal/debate/cast.go** - `resolveProvider`:
```go
func resolveProvider(reg *registry.Registry, cfg registry.AgentConfig) (registry.Provider, bool) {
	if reg.Providers == nil {
		return registry.Provider{}, true
	}
	p, ok := reg.Providers[cfg.Provider]
	return p, ok
}
```
When `Providers` is nil (unvalidated test registry), it returns a zero provider with `true`. This means in production, if a provider is missing, the agent won't be cast (ok=false). But in tests with nil Providers, any agent is castable. This is a deliberate test seam.

12. **internal/debate/debate.go** - Idempotency check:
```go
df.Items = filterAlreadyDebated(df.Items, findings)
```
This filters out items that already have `ChallengeSurvived`. But what about overturned findings? The comment says "Overturned findings are already excluded (refuted) by the radar". Let me verify - the radar (LoadDisagreements) should exclude refuted findings. If it does, then overturned findings won't re-enter. This seems correct.

13. **internal/debate/protocol.go** - `RunDebate` uses `rand.Uint32()` without seeding. In Go 1.20+, `math/rand` is auto-seeded. But if this codebase targets Go < 1.20, the sentinel would be deterministic (always 0x00000001 on first call). Let me check if there's a go.mod constraint... not visible in the diff. This is a potential issue but LOW severity since the threat model is LLM output, not a cryptographic attacker.

14. **internal/report/contested.go** - `writeContestedSection`:
```go
fmt.Fprintf(b, "\n### %d. %s — %s%s\n", i+1, esc(c.Outcome), codeSpan(c.File, c.Line), severityTransition(c))
```
The `esc(c.Outcome)` escapes the outcome string. But `codeSpan(c.File, c.Line)` - does this escape the file path? The comment says "file paths render verbatim inside backtick code spans (no escape, no truncation)". This is the existing behavior from the report package, so it's consistent. But if a file path contains backticks, it could break the markdown. This is a pre-existing issue, not introduced by this diff.

15. **internal/debate/envelope.go** - `truncate` function:
```go
func truncate(s string) string {
	const maxNotesLen = 2000
	if len(s) <= maxNotesLen {
		return s
	}
	cut := maxNotesLen
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…[truncated]"
}
```
This truncates at byte boundary, backing up to avoid splitting a UTF-8 character. But if `maxNotesLen` is in the middle of a multi-byte character, it backs up. However, `s[cut]` could panic if `cut >= len(s)`, but we already checked `len(s) > maxNotesLen` so `cut = maxNotesLen < len(s)`, so `s[cut]` is safe. This looks correct.

16. **internal/debate/debate.go** - `Debate` function signature:
```go
func Debate(ctx context.Context, repoRoot, reviewDir string, reg *registry.Registry, opts Options) (Result, error) {
```
If `reg` is nil, `runDebate` will call `ResolveConfig(reg.Debate)` which would panic. Let me check:
```go
func runDebate(ctx context.Context, reviewDir string, reg *registry.Registry, opts Options, newHarness harnessFunc) (Result, error) {
	start := time.Now()
	cfg := ResolveConfig(reg.Debate)
```
If `reg` is nil, this panics. The CLI path loads the registry via `fanout.LoadReviewConfig`, which should return a non-nil registry. But the MCP handler also loads it. Is there a nil check? Looking at `CastRoles`:
```go
func CastRoles(reg *registry.Registry, item reconcile.DisagreementItem, cfg Config) (Cast, bool, string) {
	if reg == nil {
		return Cast{}, false, ReasonNoProposer
	}
```
`CastRoles` checks for nil reg, but `runDebate` doesn't check before calling `ResolveConfig(reg.Debate)`. If `reg` is nil, `runDebate` panics before reaching `CastRoles`. This is a potential nil pointer dereference.

However, in practice, both CLI and MCP paths load the registry and would return an error if it's missing. So `reg` should never be nil when `runDebate` is called. But it's still a defensive coding gap.

17. **cmd/atcr/debate.go** - `runDebateCmd`:
```go
cfg, err := fanout.LoadReviewConfig(".", registry.CLIOverrides{})
if err != nil {
    return usageError(err)
}

res, err := debate.Debate(cmd.Context(), ".", reviewDir, cfg.Registry, debate.Options{
    SingleModel: boolFlag(cmd, "single-model"),
})
```
The `cfg.Registry` is passed to `debate.Debate`. If `LoadReviewConfig` returns a config with a nil Registry, this would panic in `runDebate`. But `LoadReviewConfig` should always return a non-nil Registry.

18. **internal/debate/debate.go** - Error handling in `runDebate`:
```go
if err := writeFindings(reviewDir, findings); err != nil {
    return Result{}, err
}
```
If `writeFindings` fails, the function returns an error. But `writeDebateFile` and `updateManifestStage` are called after. If `writeFindings` fails, the debate.json and manifest are not updated. This is correct - fail fast.

But if `writeDebateFile` fails after `writeFindings` succeeds, the findings are already re-emitted but the debate.json is not written. This leaves the review in an inconsistent state. However, this is a common pattern in the codebase (verify likely does the same), so it's probably accepted.

19. **internal/debate/protocol.go** - `driveSeat`:
```go
results := engine.Run(ctx, []fanout.Slot{{Primary: agent}})
if len(results) == 0 {
    return "", fanout.StatusFailed
}
r := results[0]
if r.Status != fanout.StatusOK || len(r.TrippedBudgets) > 0 {
    return r.Content, fanout.StatusFailed
}
return r.Content, fanout.StatusOK
```
This checks for tripped budgets and non-OK status. If the seat trips a budget, it returns the content (which might be partial) with StatusFailed. The caller then marks the seat as halted. This seems correct.

20. **internal/debate/select.go** - `SelectItems`:
```go
if cfg.MaxItems <= 0 || len(matched) <= cfg.MaxItems {
    return Selection{Selected: matched, Overflow: []reconcile.DisagreementItem{}}
}
```
When `MaxItems == 0` (unlimited), all items are selected. When `MaxItems < 0` (shouldn't happen due to validation, but defensively), all items are selected. This is correct.

21. **internal/debate/debate.go** - `filterAlreadyDebated`:
```go
func filterAlreadyDebated(items []reconcile.DisagreementItem, findings []reconcile.JSONFinding) []reconcile.DisagreementItem {
	debated := map[FindingKey]bool{}
	for _, f := range findings {
		if f.Verification != nil && f.Verification.ChallengeSurvived {
			debated[FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}] = true
		}
	}
```
This filters based on `ChallengeSurvived`. But what about overturned findings? The comment says "Overturned findings are already excluded (refuted) by the radar." So the radar should not include refuted findings. If it does, they would not be filtered here (since they don't have ChallengeSurvived), and they would be re-debated. This could be a bug if the radar includes refuted findings. But the comment says they're excluded, so this is probably fine.

22. **internal/debate/debate.go** - Looking at the `Debate` function more carefully:
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
The harness function creates a new `llmclient.New()` completer each time it's called. But `runDebate` only calls it once:
```go
if len(sel.Selected) > 0 {
    c, d, cleanup, herr := newHarness()
```
So it's called once per debate run, not per item. All items share the same completer and dispatcher. This is correct - the completer is stateless (or manages its own state internally).

23. **internal/debate/mock_test.go** - `fakeChatCompleter.Complete`:
```go
func (f *fakeChatCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.turns) > 0 {
		return f.turns[0].content, nil
	}
	return "", nil
}
```
This always returns the first turn and never advances. If `Complete` is called instead of `Chat`, it would always return the same response. But looking at the code, the debate uses `Chat` (tool loop), not `Complete`. So this might be dead code or required by an interface. Let me check if `fanout.ChatCompleter` requires both methods... Looking at the usage, `ChatCompleter` likely has both `Complete` and `Chat` methods. The `Complete` method is probably for non-tool-loop calls. Since debate always uses the tool loop, `Complete` is never called. So this is fine.

24. **internal/debate/debate.go** - Looking at the `applyRulings` function in emit.go:
```go
func applyRulings(findings []reconcile.JSONFinding, rulings map[FindingKey]ruleApply) {
	for i := range findings {
		key := FindingKey{File: findings[i].File, Line: findings[i].Line, Problem: findings[i].Problem}
		ra, ok := rulings[key]
		if !ok {
			continue
		}
		if ra.severity != "" {
			findings[i].Severity = ra.severity
		}
		findings[i].Verification = &reconcile.Verification{
			Verdict:           ra.verdict,
			Skeptic:           ra.judge,
			Notes:             ra.reasoning,
			ChallengeSurvived: ra.survived,
		}
		findings[i].Confidence = reconcile.ConfidenceForVerdict(findings[i].Confidence, ra.verdict)
	}
}
```
This overwrites the entire `Verification` block. If a finding already had a verification from the verify stage (e.g., `Verdict: "confirmed"`, `Skeptic: "bob"`), the debate stage overwrites it with the judge's verdict and the judge as the skeptic. This means the original skeptic's verdict is lost. Is this intentional?

Looking at the sprint plan: "uphold→satisfies --require-verified" and "overturn→refuted". So the debate verdict replaces the verify verdict. This is intentional - the debate is the final arbiter. But it does lose the original verify-stage information. The `Notes` field is overwritten with the judge's reasoning, losing the original skeptic's notes. This could be a maintainability concern but is probably intentional per the design.

Actually, wait - looking at the test `TestRunDebate_UpholdWritesConfirmedVerdict`:
```go
assert.Equal(t, "carol", f[0].Verification.Skeptic) // judge attributed
```
The judge is attributed as the skeptic. So the original skeptic info is overwritten. This is by design.

25. **internal/debate/debate.go** - `runDebate` - when there are no rulings (all items unresolved or gray-zone):
```go
if len(rulings) > 0 {
    applyRulings(findings, rulings)
    if err := writeFindings(reviewDir, findings); err != nil {
        return Result{}, err
    }
}
```
If all items are unresolved, `rulings` is empty, and `findings.json` is not re-emitted. But `debate.json` is still written, and the manifest is updated. This is correct - no findings changed, so no need to re-emit.

26. **internal/debate/protocol.go** - `buildDebateAgent`:
```go
TimeoutSecs: derefInt(c.TimeoutSecs),
```
If `TimeoutSecs` is nil, `derefInt` returns 0. Does a timeout of 0 mean "no timeout" or "immediate timeout"? This depends on the `fanout` engine's interpretation. If 0 means "no timeout", a seat with no configured timeout could run indefinitely. This is a potential issue but depends on the engine's behavior.

27. **internal/debate/debate.go** - Looking at the error from `readManifestHead`:
```go
func readManifestHead(reviewDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, manifestFile))
	if err != nil {
		return "", err
	}
	var m payload.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("parsing manifest.json: %w", err)
	}
	return m.Head, nil
}
```
If the manifest doesn't have a `head` field, `m.Head` is "". Then in `buildDispatcher`:
```go
if head == "" {
    return nil, nil, errors.New("manifest has no head SHA")
}
```
This is handled correctly.

28. **internal/debate/debate.go** - Looking at `debateOne` more carefully:
```go
rec := RunDebate(ctx, item, cast, cc, disp, tr)
```
`RunDebate` is called with `tr` which is a `*Transcript`. If `OpenTranscript` failed (disabled writer), `tr` is a disabled but non-nil `*Transcript`. `RunDebate` calls `tr.RecordTurn` and `tr.RecordRuling`, which are no-ops on a disabled writer. This is correct.

But wait - `RunDebate` is called with `tr` but `tr` could be nil if... no, `OpenTranscript` always returns a non-nil `*Transcript` (possibly disabled). So `tr` is never nil. Good.

Actually, looking at the test:
```go
rec := RunDebate(context.Background(), debateItem(), fcCast(), cc, &fakeDispatcher{}, nil)
```
The test passes `nil` for the transcript! Let me check `RunDebate`:
```go
func RunDebate(ctx context.Context, item reconcile.DisagreementItem, cast Cast, cc fanout.ChatCompleter, disp Dispatcher, tr *Transcript) Record {
```
And in `runTurn`:
```go
tr.RecordTurn(TurnEvent{...})
```
If `tr` is nil, this would panic! But in the test `TestRunDebate_DrivesThreeTurnsInOrder`, `nil` is passed:
```go
rec := RunDebate(context.Background(), debateItem(), fcCast(), cc, &fakeDispatcher{}, nil)
```

Wait, let me look at the `RecordTurn` method:
```go
func (t *Transcript) RecordTurn(ev TurnEvent) {
	ev.Event = "turn"
	ev.TS = t.ts()
	t.write(ev)
}
```
And `ts()`:
```go
func (t *Transcript) ts() string { return debateNow().UTC().Format(time.RFC3339) }
```
If `t` is nil, `t.ts()` would panic. And `t.write(ev)` checks `if t == nil`:
```go
func (t *Transcript) write(ev any) {
	if t == nil || t.disabled || t.bw == nil {
		return
	}
```
But `ts()` is called before `write()`, and `ts()` doesn't check for nil. So passing nil to `RunDebate` would panic when `RecordTurn` is called.

But wait - the test `TestRunDebate_DrivesThreeTurnsInOrder` passes nil and expects it to work:
```go
rec := RunDebate(context.Background(), debateItem(), fcCast(), cc, &fakeDispatcher{}, nil)
assert.True(t, rec.Resolved)
```

Hmm, does the test actually pass? Let me re-read... The test calls `RunDebate` with `nil` as the transcript. If `RecordTurn` panics on nil, the test would fail. But the test is written as if it should pass. 

Actually, looking more carefully at the test file:
```go
func TestRunDebate_DrivesThreeTurnsInOrder(t *testing.T) {
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"uphold","settled_severity":"HIGH","reasoning":"evidence holds"}`},
	}}
	rec := RunDebate(context.Background(), debateItem(), fcCast(), cc, &fakeDispatcher{}, nil)
```

If this test passes, then `RecordTurn` must handle nil. But looking at the code:
```go
func (t *Transcript) RecordTurn(ev TurnEvent) {
	ev.Event = "turn"
	ev.TS = t.ts()  // <-- this panics if t is nil
	t.write(ev)
}
```

Wait, in Go, calling a method on a nil pointer is valid as long as the method handles nil. But `t.ts()` dereferences `t` (calls `debateNow()` which doesn't use `t`, but the method receiver... actually `debateNow()` is a package-level function, not a method. Let me re-read:

```go
func (t *Transcript) ts() string { return debateNow().UTC().Format(time.RFC3339) }
```

This doesn't actually dereference `t`! It just calls `debateNow()` which is a package-level var. So `t.ts()` works even when `t` is nil. Then `t.write(ev)` checks for nil and returns. So the nil case is actually handled correctly!

OK so the nil transcript case works. Good.

29. **internal/debate/debate.go** - Looking at the `Debate` function:
```go
func Debate(ctx context.Context, repoRoot, reviewDir string, reg *registry.Registry, opts Options) (Result, error) {
```
If `reg` is nil, `runDebate` calls `ResolveConfig(reg.Debate)` which panics. Let me check if there's a nil guard... No, there isn't. But in practice, both CLI and MCP paths load the registry. Still, this is a potential nil panic.

Actually, looking at `CastRoles`:
```go
func CastRoles(reg *registry.Registry, item reconcile.DisagreementItem, cfg Config) (Cast, bool, string) {
	if reg == nil {
		return Cast{}, false, ReasonNoProposer
	}
```
`CastRoles` guards nil, but `runDebate` doesn't guard before `ResolveConfig(reg.Debate)`. If `reg` is nil, it panics before reaching `CastRoles`. This is a bug, but it's unlikely to be hit in practice since the registry is always loaded.

30. **internal/debate/debate.go** - Looking at the harness error handling:
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
If the harness fails, `cc` and `disp` remain nil (zero values). Then in the loop:
```go
for _, it := range sel.Selected {
    ir := debateOne(ctx, debateDir, it, cfg, reg, cc, disp)
```
`debateOne` is called with nil `cc` and `disp`. In `debateOne`, `CastRoles` is called first (doesn't use cc/disp). If casting succeeds, `RunDebate` is called with nil cc/disp. In `RunDebate`, `driveSeat` checks `if cc == nil` and returns `StatusFailed`. So all seats are halted, and the item is unresolved with reason "judge_halted". This is correct behavior.

But the reason "judge_halted" is misleading - the real reason is "harness unavailable". The item is unresolved, but the reason doesn't accurately describe why. This is a minor UX issue.

31. **cmd/atcr/report.go** - `loadContested`:
```go
func loadContested(reviewDir string) report.ContestedReport {
	df, found, err := debate.ReadDebateFile(reviewDir)
	if err != nil || !found {
		return report.ContestedReport{}
	}
```
If `ReadDebateFile` returns an error (malformed file), the error is silently swallowed and an empty report is returned. The comment says "a present-but-malformed debate.json degrades to no section rather than failing the report." This is intentional - the report doesn't fail on a malformed debate.json. But the error is completely lost - not even logged. This could hide data corruption. A log warning would be appropriate.

32. **internal/debate/emit.go** - `applyRulings`:
```go
findings[i].Verification = &reconcile.Verification{
    Verdict:           ra.verdict,
    Skeptic:           ra.judge,
    Notes:             ra.reasoning,
    ChallengeSurvived: ra.survived,
}
```
The `ra.verdict` could be empty (for unresolved items). But looking at the caller:
```go
if ir.Outcome != OutcomeUnresolved && it.Kind != reconcile.KindGrayZone {
    rulings[FindingKey{...}] = ruleApply{
        verdict:   ruleVerdict(ir),
        ...
    }
}
```
Only non-unresolved, non-gray-zone items get rulings. So `ra.verdict` is always non-empty (confirmed or refuted). This is correct.

But wait - `ruleVerdict` returns `Ruling{Outcome: ir.Outcome}.Verdict()`. For `OutcomeUphold` and `OutcomeSplit`, it returns `VerdictConfirmed`. For `OutcomeOverturn`, it returns `VerdictRefuted`. For `OutcomeUnresolved`, it returns `""`. But the check `ir.Outcome != OutcomeUnresolved` ensures unresolved items don't get rulings. So `ra.verdict` is always non-empty. Good.

33. **internal/debate/debate.go** - Looking at the overflow handling:
```go
if err := writeDebateFile(reviewDir, DebateFile{
    SchemaVersion: DebateSchemaVersion,
    Items:         items,
    Overflow:      overflowItems(sel.Overflow),
}); err != nil {
    return Result{}, err
}
```
The overflow items are recorded in the debate file. This satisfies the "never silent" requirement. Good.

34. **internal/debate/protocol.go** - Looking at the sentinel generation:
```go
sentinel := fmt.Sprintf("%08x", rand.Uint32())
```
This generates an 8-character hex string. The sentinel is used in tags like `<finding-SENTINEL>`. If two items in the same run get the same sentinel (collision), it's not a problem because they're in separate transcripts. But within one item, the same sentinel is shared across all three seats, which is intentional.

35. **internal/debate/debate.go** - Looking at the `tally` function:
```go
func tally(res *Result, ir ItemResult) {
	switch ir.Outcome {
	case OutcomeUphold:
		res.Upheld++
	case OutcomeOverturn:
		res.Overturned++
	case OutcomeSplit:
		res.Split++
	default:
		res.Unresolved++
	}
}
```
This correctly tallies all outcomes. Unresolved items (including judge_halted, unparseable_ruling, insufficient_models) all fall into the default case. Good.

36. **internal/debate/debate.go** - Looking at the `debateOne` function more carefully:
```go
ir := ItemResult{File: item.File, Line: item.Line, Kind: item.Kind, Problem: item.Problem, OriginalSeverity: item.Severity}
```
The `OriginalSeverity` is set from `item.Severity`. For a severity split, this is the severity-max value. For a gray-zone cluster, this is... what? The `DisagreementItem.Severity` for a gray-zone cluster. This might not be meaningful, but it's recorded for completeness.

37. **internal/debate/debate.go** - Looking at the `splitSeverity` function:
```go
func splitSeverity(ir ItemResult) string {
	if ir.Outcome == OutcomeSplit {
		return ir.SettledSeverity
	}
	return ""
}
```
And in `debateOne`:
```go
if ruling.Outcome == OutcomeSplit {
    sev := ruling.SettledSeverity
    if sev == "" {
        sev = item.Severity // judge gave no severity: keep the original
    }
    ir.SettledSeverity = sev
}
```
If the judge rules "split" but doesn't give a severity, the original severity is kept. Then `splitSeverity` returns the original severity. In `applyRulings`:
```go
if ra.severity != "" {
    findings[i].Severity = ra.severity
}
```
The severity is set to the original (no change). This is correct - if the judge says "split" but doesn't specify a severity, the finding keeps its original severity.

38. **internal/debate/envelope.go** - `parseRuling` - Looking at the JSON extraction:
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
    var candidate struct {
        Outcome *string `json:"outcome"`
        ...
    }
    if json.Unmarshal([]byte(obj), &candidate) == nil && candidate.Outcome != nil {
        return ruleFromCandidate(...)
    }
    idx := strings.Index(rest, obj)
    rest = rest[idx+len(obj):]
}
```
If `extractJSONObject` returns a non-empty string but `json.Unmarshal` fails or `Outcome` is nil, the code skips past that object and looks for the next one. This handles cases where there are multiple JSON objects and the first one doesn't have an "outcome" key. Good.

But there's a subtle issue: `idx := strings.Index(rest, obj)` finds the first occurrence of `obj` in `rest`. If `obj` appears earlier in