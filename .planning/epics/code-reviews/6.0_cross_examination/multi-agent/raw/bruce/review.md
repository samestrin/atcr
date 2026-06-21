<think>
Let me carefully analyze this diff to find issues. The diff is for Epic 6.0 (Cross-Examination / Debate Stage) and adds a new `debate` package, CLI command, MCP tool, and supporting infrastructure.

Let me look for issues systematically:

1. **Filter logic in `filterAlreadyDebated`** (debate.go around line 142-160): The function only filters items where `f.Verification != nil && f.Verification.ChallengeSurvived`. But looking at the code, it doesn't filter overturned items - the comment says "Overturned findings are already excluded (refuted) by the radar", but `LoadDisagreements` may or may not filter refuted items. Let me check this assumption. Actually the comment explicitly says it's handled by the radar, so this is fine if the radar indeed filters refuted items.

2. **The `select.go` sortByPriority** uses `reconcile.SeverityRank` and `stream.NormalizeSeverity` - need to verify these exist and work correctly.

3. **`parseRuling` function** (envelope.go): Looking at the extraction logic, there's a potential issue. The function:
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

Wait, there's a potential infinite loop or issue here. When `extractJSONObject` returns empty, we skip past the next `{`. But when it returns a non-empty object that doesn't match (no outcome key), we use `strings.Index(rest, obj)` to find where to continue from. This seems okay.

4. **`reconcile.ConfidenceForVerdict`**: This is in `reconcile/confidence.go`. Let me check if `VerdictConfirmed` and `VerdictRefuted` constants exist in the reconcile package. Looking at the existing `internal/reconcile/emit.go` it shows `Verdict string `json:"verdict"` // confirmed | refuted | unverifiable`` so these are expected to exist. The new code references `reconcile.VerdictConfirmed` and `reconcile.VerdictRefuted` in `confidence.go`. These are used by verify too based on `confidentV2` previously using local constants `verdictConfirmed`/`verdictRefuted`. 

Wait, looking at the diff for `confidence_v2.go`:
```go
func confidenceV2(v1Confidence, verdict string) string {
	return reconcile.ConfidenceForVerdict(v1Confidence, verdict)
}
```

It now delegates. And the `ConfidenceForVerdict` in `reconcile/confidence.go` uses `reconcile.VerdictConfirmed` and `reconcile.VerdictRefuted`. Are these defined? Looking at `internal/reconcile/emit.go` it only shows a comment with the values. Let me check if these constants exist elsewhere.

Actually, the new `reconcile/confidence.go` uses:
```go
switch strings.ToLower(strings.TrimSpace(verdict)) {
case VerdictConfirmed:
    return ConfidenceVerified
case VerdictRefuted:
    return ConfLow
```

So `VerdictConfirmed`, `VerdictRefuted`, and `ConfLow` must be defined in the reconcile package. We need to verify these exist. The diff for `emit.go` doesn't add them, so they must exist already (from Epic 3.0). Looking at the comment, the values are "confirmed | refuted | unverifiable" so the constants likely exist as "confirmed" and "refuted".

5. **`reconcile.DebateConfig`** in registry - it has a `MaxItems *int` field. The `validate()` function checks `*r.Debate.MaxItems < 0`, so if `MaxItems` is nil, this is skipped. Good.

6. **Manifest update in `updateManifestStage`** (emit.go): The code reads manifest as `map[string]any`, but the actual `payload.Manifest` is a struct. The code does:
```go
var m map[string]any
if err := json.Unmarshal(raw, &m); err != nil {
    return fmt.Errorf("parsing manifest.json: %w", err)
}
```

This should work, but the manifest might have additional fields that are typed. Let me check if there's a concern... The `payload.Manifest` has `Head` and `Base` and `Stages []string` and probably `Roster` and `Partial`. Reading as `map[string]any` and re-marshaling might preserve them. The "debate" stage string is added. Should work.

7. **`RunDebate` and turn ordering** (protocol.go): The `RunDebate` function uses a random sentinel. This is fine for security purposes but the test for sentinel defense could be stronger.

8. **`--single-model` flag**: Looking at the review.go and debate.go - the flag is defined on both `debate` command and `review` command. The review command uses it to pass to `debate.Options`.

9. **Test for nil completer**: In `TestRunDebate_NilCompleterHaltsAllSeats`:
```go
rec := RunDebate(context.Background(), debateItem(), fcCast(), nil, &fakeDispatcher{}, nil)
```
Looking at the `RunDebate` code, if `cc` is nil, `driveSeat` returns `("", fanout.StatusFailed)`. So all three seats are halted. Good.

10. **`fakeChatCompleter.Complete`** (mock_test.go): It always returns `turns[0].content` regardless of which turn. This is for the complete method, not Chat. Looking at how `driveSeat` uses it - it calls `engine.Run` which probably uses `Chat` for FC-supported agents. Looking at `mock_test.go` `Chat` method increments idx. So the completer has two methods: `Complete` (used by non-FC) and `Chat` (used by FC). For debate, agents are forced to have `Tools: true` and `SupportsFC` from agent config. So `Chat` is what matters. OK.

But wait - looking at `mock_test.go`:
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

`Complete` always returns the first turn. But `Chat` advances through turns. This might be fine if the engine only calls Chat for FC agents.

11. **`ReadDebateFile` returns error on malformed file**: But the report handler degrades to empty on error - so a malformed debate.json doesn't break the report. Good.

12. **`loadContested` in report.go**: It uses `debate.ReadDebateFile` and maps items. The `debate` package is imported in `cmd/atcr/report.go`. The report package itself doesn't import debate, which is good (separation).

13. **MCP `handleDebate`**: Returns `DebateResult` with `GateStatus *GateStatus` which exists in the mcp package.

14. **`reconcile/findings.json` writing in `writeFindings`**: The `applyRulings` function mutates findings in place. The `writeFindings` uses `atomicfs.WriteFileAtomic` for safety. Good.

15. **Idempotency - the `filterAlreadyDebated`**: This handles the ChallengeSurvived case. But what about a split outcome? A split also gets `ChallengeSurvived: true`, so it's also filtered. Good.

16. **Overflow ordering**: The `SelectItems` test shows the order is severity desc. In `TestRunDebate_OverflowRecorded`:
```go
// The CRITICAL item is debated first (severity priority); the HIGH overflows.
assert.Equal(t, "HIGH", df.Overflow[0].Severity)
```

17. **Looking at `cast.go` `resolveProvider`**: 
```go
if reg.Providers == nil {
    return registry.Provider{}, true
}
```
A nil `Providers` map returns `Provider{}, true` (zero provider). This is documented as a test tolerance. But in production, this would mean using an unconfigured agent. Could this be a real production issue? Looking at the comment "A nil Providers map (an unvalidated test registry) yields a zero provider that the caller tolerates". This is documented behavior.

18. **Boundary import check**: `internal/boundaries_test.go` - the new "debate" entry allows imports from `reconcile, stream, registry, fanout, payload, tools, llmclient, atomicfs, log`. The diff shows `internal/debate` imports these. Good.

19. **Looking at `TestDriftGuard_TriggerConstantsMatchReconcileKinds`**: This tests that the constants are byte-equal. If they ever drift, the test fails. Good.

20. **`Contested` struct has `Line int` not `omitempty`**: This is consistent.

21. **`RenderMarkdownWithContested`**: When the contested report is empty, the output should be byte-identical to `RenderMarkdownWithDisagreements`. Test `TestWriteContestedSection_EmptyIsByteIdentical` verifies this.

22. **`writeContestedSection` uses `c.Outcome == "uphold"` etc. as string literals**: This is a code smell - using string literals instead of the constants from the debate package. The constants `OutcomeUphold`, `OutcomeOverturn`, etc. are in `internal/debate/envelope.go` but the report package can't import debate (separation). So they need to use literals. This is a maintainability concern but not a bug.

23. **Looking at `select.go`**: `SelectItems` uses `reconcile.DisagreementsFile` with `Items` field. The function takes the entire `DisagreementsFile` but only uses `Items`. This is fine for API consistency.

24. **Manifest stage update idempotency**: In `updateManifestStage`, the code:
```go
for _, s := range stages {
    if s == debateStage {
        return nil // already recorded
    }
}
```
This is correct - re-runs won't add "debate" twice.

25. **`parseRuling` for empty/whitespace input**:
```go
if strings.TrimSpace(response) == "" {
    return Ruling{Outcome: OutcomeUnresolved, Reasoning: "empty_response"}
}
```
This is handled correctly.

26. **`TestRunDebate_IdempotentReRun`**: Sets `Verification.ChallengeSurvived: true` on the finding, expects 0 selected. This verifies the idempotency filter.

27. **`loadContested` calls `debate.ReadDebateFile`**: Looking at `ReadDebateFile`:
```go
func ReadDebateFile(reviewDir string) (df DebateFile, found bool, err error) {
    data, rerr := os.ReadFile(filepath.Join(reviewDir, reconciledSubdir, DebateJSON))
    if rerr != nil {
        if os.IsNotExist(rerr) {
            return DebateFile{}, false, nil
        }
        return DebateFile{}, false, rerr
    }
    if err := json.Unmarshal(data, &df); err != nil {
        return DebateFile{}, false, fmt.Errorf("parsing %s: %w", DebateJSON, err)
    }
    return df, true, nil
}
```

And `loadContested`:
```go
func loadContested(reviewDir string) report.ContestedReport {
    df, found, err := debate.ReadDebateFile(reviewDir)
    if err != nil || !found {
        return report.ContestedReport{}
    }
    ...
}
```

Wait - if the file is malformed, `err` is non-nil AND `found` is true. In that case, `loadContested` returns empty. The error is silently swallowed. The comment in `report.go` says: "the report package stays decoupled from the debate package" and "matching the radar's tolerant-read contract". This is intentional behavior.

28. **Test for malformed debate.json in `ReadDebateFile`**: 
```go
// Malformed file → error.
require.NoError(t, os.WriteFile(filepath.Join(dir, reconciledSubdir, DebateJSON), []byte("{not json"), 0o644))
_, _, err = ReadDebateFile(dir)
assert.Error(t, err)
```
Good, this confirms the error path.

29. **`writeContestedSection` uses `c.Outcome` as string literals**: This is a maintainability concern but not a correctness issue. The report package is decoupled from debate.

30. **Looking at the `RunDebate` overflow in `runDebate`**:
```go
res.Overflow = len(sel.Overflow)
```
This sets the overflow count but the debate.json's `Overflow` field is set in `writeDebateFile` separately. So both are populated. Good.

31. **`applyRulings` for `Severity`**: The split severity overwrites the finding's severity, which is the intended behavior.

32. **`reconcile.FindingsJSON` constant**: The `writeFindings` function uses `reconcile.FindingsJSON`. This should exist in the reconcile package.

33. **The `mcpsdk` and `DebateResult` JSON field `gateStatus`**: The MCP output uses camelCase. Looking at the struct:
```go
type DebateResult struct {
    ...
    GateStatus *GateStatus `json:"gateStatus,omitempty"`
}
```
Good.

34. **`requireVerified` in `handleDebate`**: The check `if in.RequireVerified && threshold == ""` is correctly placed before any work.

35. **`CountFailingJSON`**: The MCP handler uses this. The function name suggests it counts findings that fail the gate. The threshold + requireVerified are passed correctly.

36. **`tools_test.go` for `TestToolRegistration_Count`**: Updated from 7 to 8 to account for the new debate tool. Good.

37. **`server_test.go` `TestServe_InitializeHandshake`**: Updated from 7 to 8 tools. Good.

38. **The `WriteFileAtomic` for `writeFindings` and `writeDebateFile`**: Both use atomic write for safety. Good.

39. **`buildDebateAgent` uses `derefInt(c.TimeoutSecs)` etc.**: These are `*int` pointers on `registry.AgentConfig`. The derefInt returns 0 for nil. This means if a config doesn't have these set, the values are 0. Whether this is a bug depends on the engine's handling of 0. Looking at `fanout.Agent`, zero values might mean "use default" or "no timeout". This is consistent with verify.

40. **`RunDebate` and `resolveProvider`**: The `prov, pok := resolveProvider(reg, cfg)` returns `Provider{}, false` if the provider key is absent. The `pickDistinct` function then continues to the next candidate. This is good.

41. **Looking at `debatelooking at `debatelooking at the `disagreement` kind handling in `LoadDisagreements`**: The debate code uses `reconcile.LoadDisagreements` to rebuild the radar. This includes verification_disagreement items that wouldn't be in the on-disk disagreements.json snapshot. Good.

42. **The `splitSeverity` function**: Returns the settled severity for split, "" for anything else. Used in `applyRulings` to set the finding's severity. Good.

43. **Potential issue - the `splitSeverity` returns empty for non-split, but `applyRulings` only applies severity if `ra.severity != ""`**. Wait, let me re-check:
```go
type ruleApply struct {
    verdict   string
    survived  bool
    severity  string
    judge     string
    reasoning string
}

// In debate.go:
rulings[FindingKey{...}] = ruleApply{
    verdict:   ruleVerdict(ir),
    survived:  ir.ChallengeSurvived,
    severity:  splitSeverity(ir),
    ...
}
```

And `splitSeverity`:
```go
func splitSeverity(ir ItemResult) string {
    if ir.Outcome == OutcomeSplit {
        return ir.SettledSeverity
    }
    return ""
}
```

So for non-split, severity is "". For split, it's the SettledSeverity (which defaults to original severity if judge didn't set it). This is fine.

44. **The `ir.SettledSeverity` is set in `debateOne`**:
```go
if ruling.Outcome == OutcomeSplit {
    sev := ruling.SettledSeverity
    if sev == "" {
        sev = item.Severity // judge gave no severity: keep the original
    }
    ir.SettledSeverity = sev
}
```

So if the judge says split with no severity, we keep the original. Then `splitSeverity` returns this. Then `applyRulings` writes it back as the finding's severity. So a split with no explicit severity is a no-op for the finding's severity. Good.

45. **Looking at `manifestFile` constant in debate/emit.go**: It uses `manifestFile = "manifest.json"`. The verify package likely has a similar constant. This is a duplication of the path.

46. **`reconciledSubdir = "reconciled"`** constant: Duplicated from reconcile. Could be a maintenance issue.

47. **`reconcile.AmbiguousJSON`**: Used in `writeAmbiguous` test helper. This constant must exist in the reconcile package.

48. **The `parseRuling` function - the second loop iteration**: When `extractJSONObject` returns an object that doesn't have an "outcome" key, we move past it. But `strings.Index(rest, obj)` returns the index of `obj` in `rest`, and then we set `rest = rest[idx+len(obj):]`. This should skip past the object correctly.

But wait - what if `obj` appears multiple times in `rest`? For example, if `rest` contains the same JSON object twice (unlikely but possible), `Index` returns the first match. The first match is the one we just processed. So `idx` is the position of the object we just processed, and `rest[idx+len(obj):]` skips past it. Good.

49. **A subtle issue in `parseRuling`**: The function uses `extractJSONObject` to find a balanced {...} pair. But `extractJSONObject` finds the FIRST `{` and matches braces. If there's a `{` inside a string that doesn't close properly, it could fail. Looking at `extractJSONObject`, it handles strings correctly with `inStr` and `escaped` tracking. Good.

50. **The `extractJSONObject` returns the FIRST balanced object**: If the first balanced object doesn't have an "outcome" key, we skip past it. The function then tries to find another balanced object in the remaining text. This handles the case where a model writes a "thinking" object first and the actual ruling second.

51. **Looking at the `TestParseRuling_SkipsDecoyBrace` test**: It uses `{"note":"thinking"}` as a decoy and then `{"outcome":"overturn","reasoning":"nope"}` as the real ruling. The test expects the real ruling to win. This is good.

52. **`extractJSONObject` for empty `rest`**: If `rest` is empty, `IndexByte(s, '{')` returns -1, and we return "". Good.

53. **The `sentinel` in `RunDebate`**: Generated via `fmt.Sprintf("%08x", rand.Uint32())`. This is per-item, generated once. Each item gets a different sentinel. Good.

54. **The `prompt` functions all use the same `sentinel`**: So the same item's blocks all have the same tag. Good.

55. **The `block` function**: 
```go
func block(name, sentinel, content string) string {
    tag := name + "-" + sentinel
    return "<" + tag + ">\n" + content + "\n</" + tag + ">"
}
```

The opening tag is `<name-sentinel>` and closing is `</name-sentinel>`. The test `TestPrompts_SentinelDefeatsEarlyClose` verifies that `</finding>` in the content doesn't close the block. Good.

56. **Looking at the `applyRulings` for empty Verification**: The function creates a new `Verification` block. If the finding already had a `Verification` (e.g., from a prior verify), it's overwritten. This is correct because debate supersedes verify's verdict on a debated item.

57. **`reconcile.ConfidenceForVerdict`**: The function uses `strings.ToLower(strings.TrimSpace(verdict))`. This is good for case-insensitive comparison.

58. **Looking at `TestParseRuling` test case "invalid severity dropped, outcome kept"**: 
```go
{
    name:        "invalid severity dropped, outcome kept",
    raw:         `{"outcome":"uphold","settled_severity":"BLOCKER"}`,
    wantOutcome: OutcomeUphold, wantSev: "", wantSurvive: true, wantVerdict: reconcile.VerdictConfirmed,
}
```

The expected `wantSev` is "". But the test expectation says `wantSurvive: true` and `wantVerdict: reconcile.VerdictConfirmed`. The `Verdict` is set regardless of whether the severity is valid. Good.

59. **`r.Outcome` to test is `OutcomeUphold`** but the function `ruleFromCandidate` does:
```go
switch strings.ToLower(strings.TrimSpace(outcome)) {
case OutcomeUphold:
    outcome = OutcomeUphold
```

So if the parsed outcome is "uphold" (lowercase), it becomes `OutcomeUphold` ("uphold"). The constant is "uphold" lowercase. Good.

60. **`TestResolveConfig_ExplicitZeroMeansUnlimited`**: Sets `MaxItems: &zero` (0), expects `cfg.MaxItems == 0`. The function:
```go
maxItems := registry.DefaultDebateMaxItems
if dc.MaxItems != nil {
    maxItems = *dc.MaxItems
}
```

So 0 is preserved. Good.

61. **Looking at the `mcp/handlers.go` `loadVerifyRegistry` comment**: It says "the user/project merged registry the other handlers use". The new `handleDebate` uses this. Good.

62. **The `engine.root` field**: Used in `handleDebate` for the repoRoot parameter. This must be the git repo root. Good.

63. **`mcpsdk.CallToolResult` return value**: The first return is `*mcpsdk.CallToolResult` which is the MCP protocol-level result. The second is the typed result. The handler returns `nil, out, nil`. Good.

64. **Looking at `fanout.NewEngine`** usage: In `driveSeat`, it creates an engine per turn. This is wasteful but correct. Each turn gets a fresh engine. Good.

65. **Per-call budgets in `buildDebateAgent`**: `MaxTurns`, `ToolBudgetBytes`, `MaxRetries`, `InitialBackoffMs` are forwarded. The `ToolBudgetBytes` is the per-call budget, not the per-debate budget. This might be a concern - a debate has 3 calls but each call has its own budget. A seat that exhausts its budget mid-debate would halt, but that's per-call.

66. **The `MaxRetries` is 0 from `derefInt` if nil**: This means no retries. This matches the existing behavior.

67. **Looking at the `TestRunDebate_JudgeHaltedIsUnresolved` test**:
```go
cc := &fakeChatCompleter{turns: []chatTurn{
    {content: "p"}, {content: "c"}, {err: errContext()},
}}
```

The third turn errors out. The test expects `res.Unresolved == 1`. In `driveSeat`, if Chat returns an error, the engine returns `r.Status != fanout.StatusOK`, so the seat is halted. The judge is halted, so `judgeHalted` returns true, and the item is recorded as unresolved. Good.

68. **`reconcile.Kind` constants**: `KindSeveritySplit`, `KindGrayZone`, `KindVerificationDisagreement`, `KindSoloFinding`. The debate test `TestDriftGuard_TriggerConstantsMatchReconcileKinds` verifies the latter three are the right values. The KindSoloFinding is used in test "non-trigger kinds are filtered out".

69. **`reconcile.SeverityRank`**: Used in `sortByPriority`. The test doesn't explicitly test this but it should exist.

70. **`stream.NormalizeSeverity`**: Used in `sortByPriority` and `ruleFromCandidate`. The test "split lowers severity" tests `parseRuling` with `"low"` and expects `"LOW"`. This verifies the normalization.

71. **`TestRunDebate_OverflowRecorded` test**: Sets `MaxItems: &one` (1), two findings, one debated (CRITICAL) and one overflow (HIGH). Test expects 1 selected, 1 overflow. Good.

72. **`TestRunDebate_GrayZoneRecordedNotApplied`**: Tests that gray-zone items don't have verdicts written to findings. The test asserts `f.Verification == nil` for all findings. Good.

73. **`TestRunDebate_UpholdWritesConfirmedVerdict`**: 
```go
assert.Equal(t, "carol", f[0].Verification.Skeptic) // judge attributed
```

The skeptic field gets the judge's agent name. This is correct because the judge is the "skeptic" in the verification block.

74. **`TestRunDebate_SplitOverwritesSeverity`**: Tests that a split ruling changes the severity. The finding originally has `Severity: "HIGH"`, ruling sets `settled_severity: "MEDIUM"`, test expects `f[0].Severity == "MEDIUM"`. Good.

75. **Looking at the `TestRunDebate_UnresolvedWhenNoDistinctModelsAndNoOptIn` test**: 
```go
reg := rosterReg(map[string][2]string{"alice": {"model-a", registry.RoleReviewer}})
cc := &fakeChatCompleter{}
```

Only a reviewer role, no skeptic/judge. Distinct path fails. No opt-in. So all items are unresolved. The test expects `f[0].Verification == nil`. Good.

76. **Looking at the `TestRunDebate_MissingFindingsErrors` test**: 
```go
dir := t.TempDir()
cc := &fakeChatCompleter{}
_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
assert.ErrorIs(t, err, ErrNoReconciledFindings)
```

The temp dir has no findings.json. The function returns `ErrNoReconciledFindings`. Good.

77. **`--debate` flag on `review` command**: The flag is on the review command, not just the debate command. The review command's `RunE` function processes the flag. Good.

78. **Looking at the chained review command logic**:
```go
if threshold != "" || verifyFlag || debateFlag {
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

if verifyFlag || debateFlag {
    if threshold != "" {
        ...
    }
    return nil
}
```

The logic is structured so:
- If any of threshold/verify/debate is set, run reconcile
- If verify, run verify
- If debate, run debate
- If verify or debate, run gate and return

This looks correct.

79. **The `runReview` `if verifyFlag || debateFlag` gate check**: This re-reads the findings to count failing ones. This is correct.

80. **Looking at the `review.go` error wrapping**:
```go
if derr != nil {
    return debateFailureError(derr)
}
```

This wraps the error. Good.

81. **`debateFailureError` function**: Returns a wrapped error via `usageError`. The CLI returns the error with exit 2. Good.

82. **Looking at the `subCmd` list in main_test.go**: Updated to include "debate" and count to 13. Good.

83. **The `allowedInternalImports` map for `verify`**: The original allowed `mcp` to import `verify`. The new allowed `mcp` to import `debate`. Good.

84. **Looking at the `boudaries_test.go` `verify` entry**: 
```
"verify":         {"reconcile", "stream", "registry", "fanout", "payload", "tools", "llmclient", "atomicfs", "log"},
```

The new `debate` entry has the same dependencies plus... let me check. The debate imports:
- fanout
- llmclient
- log
- payload
- reconcile
- registry
- tools

But the debate package also uses:
- `stream` (in `select.go` and `envelope.go`)
- `atomicfs` (in `emit.go`)

So the allowed imports for debate should be: `reconcile, stream, registry, fanout, payload, tools, llmclient, atomicfs, log`. The diff shows:
```
"debate":         {"reconcile", "stream", "registry", "fanout", "payload", "tools", "llmclient", "atomicfs", "log"},
```

This matches. Good.

85. **Looking at the `cmd/atcr/main.go` `newRootCmd`**: The debate command is registered between verify and report. Good.

86. **Looking at the `mcp/handlers.go` `descDebate` constant**: It's used in `descDebate` in tools.go. The registration uses `descDebate` for the debate tool. Good.

87. **Looking at the `descDebate` description**: It says "Runs after atcr_reconcile (and atcr_verify, for verification disagreements)". This is accurate.

88. **Looking at the test for `TestHandleDebate_RequireVerifiedWithoutFailOn`**: Tests the validation that requireVerified requires failOn. Good.

89. **`TestHandleDebate_UnresolvedWithoutRoles`**: Similar to the CLI test, tests that no roles yields unresolved. Good.

90. **Looking at the `mcp/handlers.go` `loadVerifyRegistry`**: This function is shared between verify and debate handlers. Good reuse.

91. **`internal/registry/debate_config_test.go`**: New tests for the debate config. Good coverage.

92. **`TestDebateConfig_DefaultsWhenAbsent`**: Verifies that absent config yields the defaults. Good.

93. **`TestDebateConfig_InvalidTrigger`**: Tests that an invalid trigger is rejected. Good.

94. **`TestDebateConfig_NegativeMaxItems`**: Tests that negative max_items is rejected. Good.

95. **`TestDebateConfig_ExplicitMaxItemsPreserved`**: Verifies that an explicit max_items is preserved. Good.

96. **Looking at the `registry.RoleSkeptic` and `registry.RoleJudge` constants**: These are used in `cast.go`. They must exist (referenced in the cast_test.go).

97. **Looking at the `registry.AgentsByRole` function**: Used in `pickDistinct`. This must exist in the registry.

98. **Looking at the `pickDistinct` logic**: It uses `reg.AgentsByRole(role)` to get agents whose effective role matches `role`. Then it filters by excluded models and provider resolution. Good.

99. **Looking at the `pickDistinct` for label parameter**: The label is set to the role (e.g., LabelChallenger) but the agent name is the registry key. Good.

100. **The `registerTool` for `ToolDebate`**: Added to `server.go`. The tool count went from 7 to 8. Good.

Now let me look for actual issues:

**Issue 1: `reconcile.KindSoloFinding` is not a debate trigger**
The `reconcile.KindSoloFinding` exists in the test as a non-debated kind. This is fine - solo findings are not debated.

**Issue 2: Manifest read in `updateManifestStage`**
The function reads the manifest as `map[string]any`, not as `payload.Manifest`. This means custom fields not in the struct are preserved. The re-marshaling uses `json.MarshalIndent` which formats it nicely. This might not preserve original formatting. But this is a minor concern.

**Issue 3: The `r.Outcome` literal comparisons in `writeContestedSection`**
The report package uses string literals "uphold", "split", "overturn" instead of constants from the debate package. This is a maintainability concern because if the debate package changes the constants, the report breaks silently. However, the report package is intentionally decoupled from debate. This is a known trade-off.

**Issue 4: The `splitSeverity` returns original severity when judge gives no severity**
If the judge says split but doesn't provide a severity, the function uses the original. This is a reasonable default. But it means a "split" ruling can be a no-op for severity. The intent of split is to change the severity, so this is a minor issue.

Wait, looking at the logic more carefully:
```go
if ruling.Outcome == OutcomeSplit {
    sev := ruling.SettledSeverity
    if sev == "" {
        sev = item.Severity // judge gave no severity: keep the original
    }
    ir.SettledSeverity = sev
}
```

This sets `ir.SettledSeverity` to the original. Then in `applyRulings`:
```go
if ra.severity != "" {
    findings[i].Severity = ra.severity
}
```

This writes the original back. So it's a no-op. But it's not incorrect.

**Issue 5: The `applyRulings` could fail to update the confidence**
Looking at `ConfidenceForVerdict`:
```go
case VerdictConfirmed:
    return ConfidenceVerified
case VerdictRefuted:
    return ConfLow
```

For uphold, the verdict is `VerdictConfirmed`, so confidence becomes `VERIFIED`. Good.
For overturn, the verdict is `VerdictRefuted`, so confidence becomes `ConfLow`. Good.
For split, the verdict is `VerdictConfirmed` (since split survives), so confidence becomes `VERIFIED`. Good.

But wait - the prior confidence might already be `VERIFIED` (from a prior verify). The function returns `ConfidenceVerified` regardless. This is correct.

**Issue 6: The `applyRulings` writes the `Skeptic` field as the judge**
The judge name goes into the `Skeptic` field. This is a bit semantically odd (the judge is not a "skeptic") but the verify package uses the same field. This is the design.

**Issue 7: The `split` ruling writes `verdict: "confirmed"` but doesn't change `confidence`**
The `ConfidenceForVerdict` is called for split, returning `ConfidenceVerified`. The prior confidence might be `MEDIUM` or `HIGH`. The split ruling confirms the finding, so promoting to `VERIFIED` is correct.

**Issue 8: The `Notes` field is set to `ra.reasoning`**
The reasoning is stored in `Notes`. This is the same as verify.

**Issue 9: The `transcriptLog` is a global function**
It's used for logging transcript errors. It's a global, but it's `var transcriptLog = func(...)`. Tests could override it.

**Issue 10: The `debateNow` is a global function**
It's used for timestamps. Tests could override it. Currently no tests do, but the seam exists.

**Issue 11: The `extractJSONObject` doesn't handle the case where `{` appears inside a JSON string within the previous object**
The function tracks `inStr` correctly. If a `{` appears inside a string, `inStr` is true and it's not counted. Good.

**Issue 12: The `parseRuling` loop could be infinite if `obj == ""` and there's no `{`**
Looking at:
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
    ...
}
```

If `extractJSONObject` returns "" (no balanced object), we look for the next `{`. If there's no `{`, we break. If there is, we move past it. The next iteration of the loop calls `extractJSONObject` on the new `rest`. This should terminate.

But wait - what if `rest` is just `{` (single brace with no match)? `extractJSONObject` would return "" (no balanced pair). Then `IndexByte(rest, '{')` returns 0. We set `rest = rest[1:]` = "". Next iteration: `extractJSONObject("")` returns "". `IndexByte("", '{')` returns -1. Break. Good.

**Issue 13: The `reconcile.FindingsJSON` constant usage**
The `writeFindings` function uses `reconcile.FindingsJSON`. This constant must be defined in the reconcile package. The diff doesn't add it, so it must exist (from prior work).

**Issue 14: The `fanout.Slot` usage in `driveSeat`**
The function uses `engine.Run(ctx, []fanout.Slot{{Primary: agent}})`. This runs one agent. The `engine.Run`
</think>
