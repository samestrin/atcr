<think>
Let me analyze the diff carefully to identify real issues within the scope of the sprint plan (Epic 3.2: Disagreement Radar).

The sprint plan covers:
1. Disagreement scoring
2. `atcr report --disagreements` mode
3. Radar section in `report.md`
4. Verification disagreement intake
5. Cross-exam handoff schema (`reconciled/disagreements.json`)
6. Docs and tests

Let me look at the diff for issues:

1. **In `cmd/atcr/report.go`**: The `disagreements` flag case has a check for `format == report.FormatMarkdown` - but the `disagreements` case already handles everything. Wait, the structure is:
   ```go
   switch {
   case disagreements:
       // read ambiguous, build, render disagreements
   case format == report.FormatMarkdown:
       // read ambiguous (silently), build, render markdown with disagreements
   default:
       // render plain
   }
   ```
   This is fine.

2. **In `internal/reconcile/emit.go`**: In `RenderMarkdown`, the radar section is now injected. But this is called from `Emit` which writes report.md. The test `TestRenderMarkdown_NoDisagreementsOmitsRadar` verifies this. But wait - there's a question of whether `RenderMarkdown` in `internal/reconcile` and `renderMarkdown` in `internal/report` are the same. Looking at the diff, `internal/reconcile/emit.go`'s `RenderMarkdown` also calls `writeRadarSection` from `disagree.go`. So there are two implementations of the radar logic - one in `reconcile` and one in `report`. This is duplication.

3. **In `internal/report/disagree.go`**: The `writeRadarSection` function is duplicated between `internal/reconcile/disagree.go` and `internal/report/disagree.go`. Both have a `writeRadarSection` function and a `formatScore` function. This is a maintenance issue but maybe not a bug.

4. **In `internal/report/render.go`**: The `renderMarkdown` function now takes a `df` parameter. The comment says `// The plain markdown report carries no radar section (empty DisagreementsFile); callers that want the radar use RenderMarkdownWithDisagreements.` But this contradicts the actual behavior - the function does write the radar section via `writeRadarSection(&b, df)`. The comment in `RenderMarkdown` says "This keeps Render's md output byte-identical to its pre-3.2 form for every existing caller." But the function is now being called with an empty `DisagreementsFile` and still passes through to `renderMarkdown`. Since the radar section is omitted when empty, the output is still byte-identical. OK this is correct.

5. **In `cmd/atcr/report.go`**: There's a problem in the markdown case - it reads ambiguous.json but if ambiguous.json is missing/corrupt, it silently ignores the error. This is intentional per the comment.

6. **In `internal/reconcile/disagree.go`**: 
   - `grayZoneItem` uses `minRank = 1<<31` as initial value. If the cluster has no findings with valid severity, this would lead to weird spread calculation. But `maxRank` starts at 0 and only updates if `r, ok := severityRank[f.Severity]; ok` is true. So if all severities are unknown, both maxRank and minRank stay at their initial values. The spread check `if maxRank > 0 && minRank <= maxRank` would be `0 > 0` = false, so spread=0. Good.
   
   - But there's a potential issue: when computing `minRank`, it uses `1<<31` which is `2147483648`. If `minRank` stays at this value and a valid severity is found (rank 1-4), the spread would be calculated as `maxRank - minRank` which could be negative. Let me re-check: `if r > maxRank { maxRank = r }` and `if r < minRank { minRank = r }`. So for the first valid finding, `r=3 (HIGH)`, maxRank=3, minRank=3. For the second, r=1 (LOW), maxRank stays 3, minRank=1. So minRank is bounded by valid values found. OK, this is fine.

7. **In `internal/mcp/handlers.go`**: The MCP handler now also reads ambiguous.json silently. This is consistent with the CLI behavior. OK.

8. **Potential issue in `internal/reconcile/disagree.go`**: The `grayZoneItem` function accesses `c.Findings[0].Severity` if `maxSev == ""`. But `c.Findings` is iterated in the loop already. The code does:
   ```go
   for _, f := range c.Findings {
       if r, ok := severityRank[f.Severity]; ok {
           if r > maxRank {
               maxRank, maxSev = r, f.Severity
           }
       }
   }
   ```
   If none of the severities are in `severityRank`, `maxSev` stays `""` and `maxRank` stays 0. Then:
   ```go
   if maxSev == "" && len(c.Findings) > 0 {
       maxSev = c.Findings[0].Severity
   }
   ```
   This works. But then `scoreFor(0, indep, severityRank[maxSev])` - if `maxSev` is unknown, `severityRank[maxSev]` returns 0, so score=0. OK.

9. **In `cmd/atcr/report_test.go`**: The test `TestReportCmd_DisagreementsModeReadsAmbiguousSidecar` calls `execCmd` with arguments but doesn't pass `--disagreements`. Wait, it does: `execCmd(t, "report", "--disagreements", "2026-06-10_g")`. OK.

10. **In `cmd/atcr/report_test.go`**: The test `TestReportCmd_DisagreementsMode` doesn't check the exit code assertion properly. Let me re-read:
    ```go
    require.Equal(t, 0, execCmd(t, "report", "--disagreements", "--output", out, "2026-06-10_d"))
    ```
    OK, this checks exit code 0.

11. **In `internal/reconcile/disagree.go`**: Looking at the `soloItem` function:
    ```go
    case len(f.Reviewers) <= 1:
    ```
    The sprint plan says "Solo findings = merged findings with exactly one distinct reviewer". But the code uses `<= 1` - this catches both empty reviewer lists and single-reviewer. An empty reviewer list shouldn't normally occur but it's a defensive choice. Actually, looking at the planning doc: "Solo findings = merged findings with exactly one distinct reviewer (Confidence MEDIUM)." So `<=1` is slightly off - it should be `== 1` to match the spec, or at minimum the test for empty reviewers. But this is a minor issue.

12. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function in `internal/reconcile/emit.go` calls `BuildDisagreements(r.JSONFindings(), r.Ambiguous)` inside the function. But this is only called from `Emit` to write the report.md file. The function is also exported and could be called from elsewhere. Let me check the test `TestRenderMarkdown_NoDisagreementsOmitsRadar` - it calls `RenderMarkdown(&buf, res)` and checks no radar section. If `res.Ambiguous` is empty, `BuildDisagreements` returns empty items, and `writeRadarSection` writes nothing. OK.

    But wait - there's a potential issue: the `RenderMarkdown` function in `internal/reconcile/emit.go` is a different function from `renderMarkdown` in `internal/report/render.go`. They have similar names but live in different packages. This is confusing but not a bug per se.

13. **In `internal/reconcile/disagree.go`**: The `writeRadarSection` in `reconcile` package writes to a `*bytes.Buffer` and is called from `internal/reconcile/emit.go`'s `RenderMarkdown`. But there's also a `writeRadarSection` in `internal/report/disagree.go` that does the same thing. This is code duplication.

14. **In `cmd/atcr/report.go`**: There's a subtle issue with the switch. When `disagreements` is true and `format` is also set (e.g., `--disagreements --format json`), the `--disagreements` flag wins and renders only the radar, ignoring the format. This is documented in the comment but could be confusing. Not really a bug.

15. **In `internal/reconcile/disagree.go`**: The `grayZoneItem` function:
    ```go
    spread := 0
    if maxRank > 0 && minRank <= maxRank {
        spread = maxRank - minRank
    }
    ```
    Since `minRank` is initialized to `1<<31` and only updated when a valid rank is found, this is fine. But `if maxRank > 0 && minRank <= maxRank` - the second condition is always true when maxRank is valid (since minRank <= maxRank by construction). So the `minRank <= maxRank` check is redundant but not a bug.

16. **In `cmd/atcr/report.go`**: Looking at the code flow:
    ```go
    disagreements, _ := cmd.Flags().GetBool("disagreements")
    switch {
    case disagreements:
        clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
        if err != nil {
            return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
        }
        ...
    ```
    Good - the `--disagreements` mode properly handles ambiguous.json errors.

17. **In `internal/reconcile/disagree.go`**: The `splitNames` function is used. Let me check if it's used in a way that could cause issues. The `Skeptic` field is comma-joined. If a skeptic name contains a comma, this would split incorrectly. But this is the same representation the verify stage uses, so not a new issue.

18. **In `cmd/atcr/report.go`**: The `disagreements` case uses `report.RenderDisagreements` but the markdown case uses `report.RenderMarkdownWithDisagreements`. Both are in the `report` package. This is fine.

19. **Looking at `internal/reconcile/disagree.go` more carefully**:
    - The function `BuildDisagreements` takes `findings []JSONFinding, clusters []AmbiguousCluster`. Good.
    - The `grayKeys` map is built from clusters. This prevents double-surfacing. Good.
    - The `for _, c := range clusters` loop adds gray-zone items after processing findings. Good.

20. **In `cmd/atcr/report.go`**: The `format` variable is not used in the `disagreements` case. The switch case ignores the format flag. This is intentional but could be flagged as dead code. Minor.

21. **In `internal/reconcile/disagree.go`**: Looking at the `atLeastOne` function - it's used to ensure independence is at least 1. This is correct because `len(f.Reviewers)` could be 0 for some edge cases.

22. **Potential issue in `internal/reconcile/disagree.go`**: The `longestProblem` function picks the longest problem text as a representative. This is content-based and could be unstable if problem texts are similar in length but different in content. But the planning doc says "content-based representative of the contested location" so this is intentional.

23. **In `cmd/atcr/report.go`**: The `disagreements` flag is checked with `_, _ := cmd.Flags().GetBool("disagreements")` - the error is ignored. If the flag is not registered, this would silently return false. But the flag is registered above, so this is fine.

24. **In `cmd/atcr/report_test.go`**: The test `TestReportCmd_DisagreementsModeReadsAmbiguousSidecar` doesn't check the output content, just the exit code. This is a weak test but not a bug.

25. **In `cmd/atcr/report_test.go`**: The test `TestReportCmd_DisagreementsModeEmptyIsClean` also only checks exit code 0. Again, weak test.

26. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function in the reconcile package calls `BuildDisagreements` which reads `r.Ambiguous`. But the `Emit` function also writes `disagreements.json` separately. The `BuildDisagreements` is called twice in the `Emit` flow - once for the file and once for the report.md. This is a minor inefficiency but not a bug.

27. **In `cmd/atcr/report.go`**: The `disagreements` case doesn't handle the case where `findings` is nil/empty. But `BuildDisagreements` handles that fine.

28. **In `internal/report/disagree.go`**: The `RenderDisagreements` function doesn't call `writeRadarSection` - it calls `writeRadarItems` directly with `"## "` prefix. This is different from the `reconcile` package's `writeRadarSection` which uses `### `. So the headings are different between the focused view (##) and the radar section (###). This is intentional for nesting.

29. **In `internal/reconcile/disagree.go`**: The `writeRadarSection` uses `### ` prefix (3 hashes) which means it's used inside the report.md. But the function is defined in `reconcile` package. The `internal/report/disagree.go` version uses the items with `### ` prefix too. OK consistent.

30. **In `internal/report/disagree.go`**: The `RenderDisagreements` function calls `writeRadarItems` with `"## "` prefix. The items are top-level (##). But the function `writeRadarSection` in the same file uses `### ` prefix for items. This is correct because the section is at `## Disagreements` and items are at `### N. kind`.

31. **Looking at `cmd/atcr/report.go` again**:
    ```go
    case format == report.FormatMarkdown:
        clusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
        df := reconcile.BuildDisagreements(findings, clusters)
    ```
    The error from `ReadAmbiguousClusters` is intentionally ignored. The comment says: "A corrupt ambiguous.json must not break the main report, so a read error degrades to a findings-only radar rather than failing." This is consistent with the planning doc.

32. **In `internal/mcp/handlers.go`**: Same pattern - silently ignores ambiguous.json read error. Consistent.

33. **Potential issue in `internal/reconcile/disagree.go`**: The `grayZoneItem` function uses `maxSev` which could be `""` if no findings have valid severities. Then `severityRank[maxSev]` returns 0, and the score would be 0. This would be a valid item with score 0. But it would be sorted to the bottom. OK.

34. **In `internal/reconcile/disagree.go`**: The `splitNames` function is defined but not exported. It's only used internally. Fine.

35. **In `cmd/atcr/report.go`**: The `disagreements` flag, when combined with `--output`, writes the radar to the file. The test verifies this. Good.

36. **Looking at the `--disagreements` mode behavior**: When `--disagreements` is set, the output is the focused radar view, not the standard report. The format flag is ignored. This is documented in the comment but could be confusing for users. The planning doc says "The flag is listed in `atcr report --help`."

37. **In `internal/reconcile/disagree.go`**: The `writeRadarSection` function is defined in the reconcile package but also in the report package. This is code duplication. The reconcile package version is used for the report.md written by `Emit`, and the report package version is used for the CLI/MCP report commands. This duplication is a maintainability concern.

38. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function is called from `Emit` to write the report.md. It also calls `BuildDisagreements` to get the radar data. But the `disagreements.json` file is also written by `Emit` via a separate call to `BuildDisagreements`. So `BuildDisagreements` is called twice per `Emit` invocation. Minor inefficiency.

39. **In `internal/report/render.go`**: The `renderMarkdown` function now takes a `df` parameter. The `Render` function passes an empty `DisagreementsFile{}` to `renderMarkdown` when format is markdown. This is correct - the plain markdown report has no radar section.

40. **In `cmd/atcr/report.go`**: The `disagreements` case doesn't check if `findings` is loaded successfully. But the code above already checks for `readErr`. OK.

41. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function in the reconcile package is called from `Emit` with `r` (the reconcile Result). It also reads `r.Ambiguous` via `r.JSONFindings()` and `r.Ambiguous`. Wait, `r.JSONFindings()` returns the JSON findings, and `r.Ambiguous` returns the ambiguous clusters. Both are used. Good.

42. **Looking at the test `TestReportCmd_DisagreementsMode`**: The fixture `splitFinding` has a finding with `disagreement: "LOW vs CRITICAL"`. The test checks for `"LOW vs CRITICAL"` in the output. Good.

43. **In `internal/reconcile/disagree.go`**: The `grayZoneItem` function:
    ```go
    if maxSev == "" && len(c.Findings) > 0 {
        maxSev = c.Findings[0].Severity
    }
    ```
    If `c.Findings[0].Severity` is also not in `severityRank`, the score would be 0. But the item would still be created. This is a minor edge case.

44. **In `internal/reconcile/disagree.go`**: The `longestProblem` function:
    ```go
    func longestProblem(findings []stream.Finding) string {
        best := ""
        for _, f := range findings {
            if len(f.Problem) > len(best) {
                best = f.Problem
            }
        }
        return best
    }
    ```
    If all findings have empty problems, returns "". OK.

45. **In `internal/reconcile/disagree.go`**: The `sortedKeys` function sorts the reviewer names. This ensures deterministic output. Good.

46. **In `cmd/atcr/report.go`**: The `disagreements` case reads ambiguous clusters with error handling, but the markdown case silently ignores errors. This asymmetry is intentional but could be confusing. The planning doc mentions this: "A corrupt ambiguous.json must not break the main report."

47. **In `internal/report/disagree.go`**: The `RenderDisagreements` function:
    ```go
    b.WriteString("# atcr Disagreement Radar\n\n")
    if len(df.Items) == 0 {
        b.WriteString("No disagreements detected.\n")
        _, err := w.Write(b.Bytes())
        return err
    }
    ```
    The empty case writes a message. Good. But the error from `w.Write` is returned, not the message write. Actually both are returned. OK.

48. **In `internal/reconcile/disagree.go`**: The `formatScore` function is duplicated in `internal/report/disagree.go`. Minor duplication.

49. **In `cmd/atcr/report.go`**: The `disagreements` flag is a boolean. There's no way to control the threshold for inclusion. The planning doc says: "The inclusion threshold is a fixed default in v1: every severity split (spread ≥ 1), every solo finding, every gray-zone pair, and every verification tie is surfaced. It is not configurable yet." So this is intentional.

50. **In `internal/reconcile/disagree.go`**: The `BuildDisagreements` function:
    ```go
    if isRefutedJSON(f) {
        continue
    }
    ```
    Refuted findings are excluded. Good - this matches the planning doc.

51. **In `cmd/atcr/report.go`**: The `disagreements` case passes `df` to `RenderDisagreements`. The `RenderDisagreements` function renders the focused view. Good.

52. **In `internal/reconcile/disagree.go`**: The `severityRank` map is used but not defined in the diff. It must be defined elsewhere in the package. Let me check if it's a global map. The code uses `severityRank[f.Severity]` and `severityRank[strings.TrimSpace(parts[0])]`. This map must be defined in the reconcile package. Assuming it exists, this is fine.

53. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function in the reconcile package:
    ```go
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ```
    This calls `BuildDisagreements` which is defined in `disagree.go`. Good.

54. **Potential issue**: The `RenderMarkdown` in `internal/reconcile/emit.go` is called from `Emit` to write the report.md. But there's also a `RenderMarkdown` in `internal/report/render.go` (as `renderMarkdown`). These are two different functions in two different packages. The reconcile package's `RenderMarkdown` is used for the persisted report.md, and the report package's `renderMarkdown` is used for the CLI/MCP report output. Both need the radar section.

55. **In `cmd/atcr/report.go`**: The `disagreements` case calls `report.RenderDisagreements`. The markdown case calls `report.RenderMarkdownWithDisagreements`. Both are in the report package. Good.

56. **In `internal/mcp/handlers.go`**: The MCP handler calls `report.RenderMarkdownWithDisagreements`. Good.

57. **Looking at the test `TestEmit_WritesAllArtifacts`**: The test now checks for 6 files including `DisagreementsJSON`. Good.

58. **Looking at the test `TestEmit_DeterministicOutput`**: The test now includes `DisagreementsJSON` in the comparison. Good.

59. **In `cmd/atcr/report.go`**: The `disagreements` case:
    ```go
    case disagreements:
        clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
        if err != nil {
            return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
        }
    ```
    Wait, `reviewDir` is used here. Let me check where `reviewDir` comes from. Looking at the code above:
    ```go
    reviewDir, readErr := resolveReviewDir(cmd)
    if readErr != nil {
        return usageError(readErr)
    }
    findings, loadErr := reconcile.ReadReconciledFindings(reviewDir)
    if loadErr != nil {
        return usageError(loadErr)
    }
    ```
    OK, `reviewDir` is resolved before the switch. Good.

60. **In `cmd/atcr/report.go`**: The `format` variable is checked in the markdown case but not in the disagreements case. The disagreements case ignores the format. This is intentional.

61. **Potential issue in `internal/reconcile/disagree.go`**: The `grayZoneItem` function uses `c.Findings[0].Severity` as a fallback for `maxSev`. But `c.Findings[0].Severity` could be empty. If all findings have empty severities, `maxSev` would be "". The score would be 0. This is a minor edge case.

62. **In `internal/reconcile/disagree.go`**: The `isRefutedJSON` function:
    ```go
    func isRefutedJSON(f JSONFinding) bool {
        return f.Verification != nil && canonVerdict(f.Verification.Verdict) == VerdictRefuted
    }
    ```
    This checks if the verification verdict is "refuted". Good.

63. **In `internal/reconcile/disagree.go`**: The `isVerificationTie` function:
    ```go
    func isVerificationTie(v *Verification) bool {
        if v == nil || canonVerdict(v.Verdict) != VerdictUnverifiable {
            return false
        }
        return len(splitNames(v.Skeptic)) >= 2
    }
    ```
    This checks if the verdict is "unverifiable" with 2+ skeptics. Good - matches the planning doc.

64. **In `cmd/atcr/report_test.go`**: The test `TestReportCmd_DisagreementsModeMalformedAmbiguousIsUsageError` checks that a malformed ambiguous.json returns exit code 2. Good.

65. **In `internal/reconcile/disagree.go`**: The `splitNames` function splits on comma. If a skeptic name contains a comma, this would split incorrectly. But the planning doc doesn't address this. Minor concern.

66. **In `internal/reconcile/disagree.go`**: The `severityRank` map is accessed with unknown keys returning 0. This is the zero value. So unknown severities get rank 0. Good.

67. **In `cmd/atcr/report.go`**: The `disagreements` case:
    ```go
    case disagreements:
        clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
        if err != nil {
            return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
        }
        df := reconcile.BuildDisagreements(findings, clusters)
        if err := report.RenderDisagreements(&buf, df); err != nil {
            return usageError(err)
        }
    ```
    The `df` is built and passed to `RenderDisagreements`. Good.

68. **In `internal/reconcile/disagree.go`**: The `BuildDisagreements` function returns a `DisagreementsFile` with `SchemaVersion` and `IndependenceModel` set. Good.

69. **In `internal/reconcile/emit.go`**: The `Emit` function writes `disagreements.json` with the result of `BuildDisagreements`. Good.

70. **In `internal/report/disagree.go`**: The `RenderDisagreements` function writes the focused view. Good.

71. **Looking at the overall structure**: The code is well-organized with clear separation between the reconcile package (data model and scoring) and the report package (rendering). The duplication of `writeRadarSection` and `formatScore` between the two packages is a minor concern.

72. **Potential issue**: The `RenderMarkdown` function in `internal/reconcile/emit.go` is used by `Emit` to write the persisted report.md. But the `Emit` function also writes `disagreements.json` separately. The `BuildDisagreements` is called twice - once for the report.md and once for the disagreements.json. This is a minor inefficiency.

73. **In `cmd/atcr/report.go`**: The `disagreements` case:
    ```go
    case disagreements:
        ...
    case format == report.FormatMarkdown:
        ...
    default:
        if err := report.Render(&buf, findings, format); err != nil {
            return usageError(err)
        }
    }
    ```
    The `default` case handles non-markdown formats (json, checklist). Good.

74. **In `cmd/atcr/report_test.go`**: The test `TestReportCmd_DisagreementsMode` uses `fixtureReconciled` (not `fixtureReconciledWithAmbiguous`). This means ambiguous.json is not present. The test should still work because `ReadAmbiguousClusters` returns `(nil, nil)` for missing files. Good.

75. **In `internal/reconcile/disagree.go`**: The `writeRadarSection` function is defined in the reconcile package. It's called from `RenderMarkdown` in the same package. But there's also a `writeRadarSection` in the report package. This duplication means changes to the radar rendering need to be made in two places.

76. **Looking at the `formatScore` function**: It's defined in both `internal/reconcile/disagree.go` and `internal/report/disagree.go`. This is duplication.

77. **In `internal/report/render.go`**: The `renderMarkdown` function is called from `Render` (with empty df) and from `RenderMarkdownWithDisagreements` (with non-empty df). The function signature changed to accept a `df` parameter. This is a breaking change for any external callers, but since it's an unexported function, this is fine.

78. **In `internal/report/render.go`**: The `Render` function now passes an empty `DisagreementsFile{}` to `renderMarkdown` when format is markdown. This ensures backward compatibility for the plain markdown report. Good.

79. **Potential issue in `internal/reconcile/emit.go`**: The `RenderMarkdown` function in the reconcile package:
    ```go
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ```
    This is called for every report.md write, even if there are no disagreements. The `BuildDisagreements` function processes all findings, which could be slow for large reviews. But the planning doc doesn't mention performance concerns. Minor.

80. **In `cmd/atcr/report.go`**: The `disagreements` case is checked first in the switch. This means `--disagreements` takes precedence over `--format`. This is intentional.

81. **In `internal/mcp/handlers.go`**: The MCP handler also reads ambiguous.json silently. This is consistent with the CLI behavior.

82. **Looking at the test `TestReportCmd_DisagreementsModeEmptyIsClean`**: The test uses a consensus finding (no disagreement). The expected output is exit code 0. The test doesn't check the output content. This is a weak test - it should also check that the output says "No disagreements detected." or similar.

83. **In `cmd/atcr/report_test.go`**: The test `TestReportCmd_DisagreementsMode` checks for "Disagreement Radar" in the output. Good.

84. **In `internal/reconcile/disagree.go`**: The `BuildDisagreements` function:
    ```go
    items := make([]DisagreementItem, 0, len(findings)+len(clusters))
    ```
    This pre-allocates capacity. Good for performance.

85. **In `internal/reconcile/disagree.go`**: The `sortDisagreements` function uses `sort.SliceStable`. This ensures stable sorting. Good for determinism.

86. **In `cmd/atcr/report.go`**: The `disagreements` flag is defined as a `Bool` flag. The default is `false`. Good.

87. **In `internal/reconcile/disagree.go`**: The `grayZoneItem` function:
    ```go
    positions := make([]Position, 0, len(c.Findings))
    ```
    This pre-allocates capacity. Good.

88. **In `internal/reconcile/disagree.go`**: The `splitNames` function:
    ```go
    func splitNames(s string) []string {
        parts := strings.Split(s, ",")
        out := make([]string, 0, len(parts))
        for _, p := range parts {
            if t := strings.TrimSpace(p); t != "" {
                out = append(out, t)
            }
        }
        return out
    }
    ```
    This trims whitespace and filters empty strings. Good.

89. **In `cmd/atcr/report.go`**: The `disagreements` case:
    ```go
    case disagreements:
        clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
        if err != nil {
            return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
        }
    ```
    Wait, if `reviewDir` is the reconciled dir or the review dir? Let me check. The `resolveReviewDir` function likely returns the review dir. And `ReadAmbiguousClusters` does `filepath.Join(reviewDir, reconciledSubdir, AmbiguousJSON)`. So it expects the review dir. Good.

90. **In `cmd/atcr/report.go`**: The `findings` variable is loaded from `reconcile.ReadReconciledFindings(reviewDir)`. This reads `findings.json` from the reconciled subdir. Good.

91. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function in the reconcile package:
    ```go
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ```
    This is called inside `RenderMarkdown`. The `r.JSONFindings()` method is called. Let me check if this method exists. It should return the findings as `[]JSONFinding`. Assuming it exists, this is fine.

92. **Looking at the test `TestRenderMarkdown_RadarSectionAboveFindings`**: The test creates two sources with different severities for the same location. The reconciler merges them and adds a `disagreement` annotation. The test checks that the radar section is above the findings section. Good.

93. **Looking at the test `TestRenderMarkdown_NoDisagreementsOmitsRadar`**: The test creates two sources with the same severity. The reconciler merges them without a disagreement annotation. The test checks that the radar section is omitted. Good.

94. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function in the reconcile package:
    ```go
    writeSeverityConfidenceTable(&b, inScope)
    
    // Disagreement radar above the consensus findings (Epic 3.2). Nothing is
    // written when there is no tension, so report.md is byte-identical to the
    // pre-3.2 output for a review with no disagreements.
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ```
    The radar is written after the severity/confidence table and before the findings. Good.

95. **In `internal/reconcile/disagree.go`**: The `BuildDisagreements` function:
    ```go
    for _, f := range findings {
        if isRefutedJSON(f) {
            continue
        }
        if f.Category == CategoryOutOfScope {
            continue
        }
        ...
    }
    ```
    Refuted and out-of-scope findings are excluded. Good.

96. **In `internal/reconcile/disagree.go`**: The `grayKeys` map is built from clusters. This prevents double-surfacing of gray-zone members. Good.

97. **In `internal/reconcile/disagree.go`**: The `BuildDisagreements` function:
    ```go
    for _, c := range clusters {
        if allOutOfScope(c.Findings) {
            continue
        }
        items = append(items, grayZoneItem(c))
    }
    ```
    Gray-zone clusters with all out-of-scope members are excluded. Good.

98. **Potential issue in `internal/reconcile/disagree.go`**: The `allOutOfScope` function:
    ```go
    func allOutOfScope(findings []stream.Finding) bool {
        if len(findings) == 0 {
            return false
        }
        for _, f := range findings {
            if strings.ToLower(strings.TrimSpace(f.Category)) != CategoryOutOfScope {
                return false
            }
        }
        return true
    }
    ```
    This checks if all findings in the cluster are out-of-scope. If the cluster has no findings, it returns false (not all out-of-scope). This means empty clusters would be surfaced. But empty clusters shouldn't exist in practice. Minor.

99. **In `cmd/atcr/report.go`**: The `disagreements` case:
    ```go
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
    ```
    The `df` is built and rendered. The `RenderDisagreements` function handles empty `df` by writing "No disagreements detected." Good.

100. **In `cmd/atcr/report.go`**: The markdown case:
    ```go
    clusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return usageError(err)
    }
    ```
    The `clusters` error is ignored. The `df` is built and rendered. Good.

Now, let me think about what real issues exist:

1. **Code duplication**: `writeRadarSection` and `formatScore` are defined in both `internal/reconcile/disagree.go` and `internal/report/disagree.go`. This is a maintainability concern.

2. **The `RenderMarkdown` function in `internal/reconcile/emit.go` calls `BuildDisagreements`** which is also called when writing `disagreements.json`. This means `BuildDisagreements` is called twice per `Emit` invocation. Minor inefficiency.

3. **The `RenderMarkdown` function in `internal/reconcile/emit.go`** has the radar section comment but the function is also called from `Emit`. The function signature didn't change - it still takes a `Result`. The radar section is added inside the function. This is fine.

4. **Looking at the `--disagreements` mode in `cmd/atcr/report.go`**: The `format` variable is not used in this case. The `disagreements` flag takes precedence over `--format`. This is documented in the comment but could be confusing for users who expect `--format` to work with `--disagreements`. Not a bug per se.

5. **In `internal/reconcile/disagree.go`**: The `grayZoneItem` function uses `c.Findings[0].Severity` as a fallback. But if all findings have empty severities, `maxSev` would be "". The `severityRank[""]` returns 0, so the score would be 0. This is a minor edge case.

6. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function in the reconcile package:
    ```go
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ```
    This is called for every `RenderMarkdown` invocation. The `BuildDisagreements` function iterates over all findings and clusters. For large reviews, this could be slow. But the planning doc doesn't mention performance concerns.

7. **In `cmd/atcr/report.go`**: The `disagreements` case:
    ```go
    case disagreements:
        clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
        if err != nil {
            return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
        }
        df := reconcile.BuildDisagreements(findings, clusters)
        if err := report.RenderDisagreements(&buf, df); err != nil {
            return usageError(err)
        }
    ```
    The `df` is built but if `BuildDisagreements` panics or has a bug, it would crash. But `BuildDisagreements` is a pure function and shouldn't panic. Good.

8. **In `internal/reconcile/disagree.go`**: The `BuildDisagreements` function:
    ```go
    grayKeys := map[string]bool{}
    for _, c := range clusters {
        for _, f := range c.Findings {
            grayKeys[findingKey(f.File, f.Line, f.Problem)] = true
        }
    }
    ```
    This builds a set of finding keys from clusters. The `findingKey` function uses `file + "\x00" + line + "\x00" + problem`. This is a stable key. Good.

9. **In `internal/reconcile/disagree.go`**: The `BuildDisagreements` function:
    ```go
    for _, f := range findings {
        if isRefutedJSON(f) {
            continue
        }
        if f.Category == CategoryOutOfScope {
            continue
        }
        if grayKeys[findingKey(f.File, f.Line, f.Problem)] {
            continue
        }
        switch {
        case isVerificationTie(f.Verification):
            items = append(items, verificationItem(f))
        case f.Disagreement != "":
            items = append(items, severitySplitItem(f))
        case len(f.Reviewers) <= 1:
            items = append(items, soloItem(f))
        }
    }
    ```
    The order of checks is: refuted → out-of-scope → gray-zone member → verification tie → severity split → solo. The first match wins. This means a finding that is both a verification tie and a severity split is labeled a verification disagreement (keeps the spread-based score). The planning doc says: "A tie finding may also be a severity split; keep the stronger spread-based score when present so labeling it a verification disagreement never demotes a high-spread split." This is implemented in `verificationItem`:
    ```go
    spread := spreadFromDisagreement(f.Disagreement)
    ...
    Score: scoreFor(spread, indep, severityRank[f.Severity]),
    ```
    Good.

10. **In `cmd/atcr/report.go`**: The `disagreements` case doesn't check if `reviewDir` is valid. But the code above already checks `readErr`. Good.

11. **In `internal/reconcile/disagree.go`**: The `writeRadarSection` function:
    ```go
    fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))
    ```
    The header says "reviewer splits, solo findings, and gray-zone clusters" but doesn't mention "verification disagreements". This is a minor inconsistency with the planning doc which lists all four classes. But the planning doc says the section is a "focused, ranked view" - the header could be more inclusive. Minor documentation issue.

12. **In `internal/report/disagree.go`**: The `writeRadarSection` function:
    ```go
    fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))
    ```
    Same issue as above. Minor.

13. **In `cmd/atcr/report.go`**: The `disagreements` case:
    ```go
    case disagreements:
        clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
    ```
    If `reviewDir` doesn't have a `reconciled/ambiguous.json`, `ReadAmbiguousClusters` returns `(nil, nil)`. The `clusters` variable would be nil. `BuildDisagreements` handles nil clusters. Good.

14. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function:
    ```go
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ```
    The `r.Ambiguous` is the ambiguous clusters. If nil, `BuildDisagreements` handles it. Good.

15. **Potential issue in `internal/reconcile/disagree.go`**: The `grayZoneItem` function:
    ```go
    if r, ok := severityRank[f.Severity]; ok {
        if r > maxRank {
            maxRank, maxSev = r, f.Severity
        }
        if r < minRank {
            minRank = r
        }
    }
    ```
    If `f.Severity` is not in `severityRank`, it's skipped. But the position is still added:
    ```go
    positions = append(positions, Position{Reviewer: f.Reviewer, Severity: f.Severity, Problem: f.Problem})
    ```
    So the position list includes all findings, even those with unknown severities. This is correct behavior.

16. **In `internal/reconcile/disagree.go`**: The `BuildDisagreements` function:
    ```go
    for _, c := range clusters {
        if allOutOfScope(c.Findings) {
            continue
        }
        items = append(items, grayZoneItem(c))
    }
    ```
    The `grayZoneItem` is called for each cluster. If the cluster has no findings, `allOutOfScope` returns false, and the item is created with empty fields. The `longestProblem` returns "". The score is 0. This is a minor edge case.

17. **In `cmd/atcr/report.go`**: The `disagreements` case:
    ```go
    case disagreements:
        ...
    case format == report.FormatMarkdown:
        ...
    ```
    The `disagreements` case is checked first. If `disagreements` is true, the markdown case is not reached. Good.

18. **In `internal/mcp/handlers.go`**: The MCP handler:
    ```go
    if format == report.FormatMarkdown {
        clusters, _ := reconcile.ReadAmbiguousClusters(dir)
        df := reconcile.BuildDisagreements(findings, clusters)
        if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
            return nil, ReportResult{}, err
        }
    } else if err := report.Render(&buf, findings, format); err != nil {
    ```
    The MCP handler doesn't have a `--disagreements` equivalent. The radar is always included in the markdown report. This is consistent with the CLI behavior for the standard report.

19. **In `cmd/atcr/report.go`**: The `disagreements` case:
    ```go
    df := reconcile.BuildDisagreements(findings, clusters)
    ```
    The `df` is built from `findings` and `clusters`. But `findings` is loaded from `findings.json` which already includes verification blocks. So verification disagreements are surfaced. Good.

20. **In `internal/reconcile/emit.go`**: The `RenderMarkdown` function:
    ```go
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ```
    The `r.JSONFindings()` returns the findings as `[]JSONFinding`. The `r.Ambiguous` returns the clusters. The `BuildDisagreements` function builds the radar. Good.

Now, let me focus on real issues that are within the scope of the sprint plan:

1. **Code duplication**: `writeRadarSection` and `formatScore` are defined in both `internal/reconcile/disagree.go` and `internal/report/disagree.go`. This is a maintainability concern. However, this might be intentional separation of concerns - the reconcile package writes the persisted report.md, and the report package writes the CLI/MCP report. The duplication is real but might be acceptable.

2. **The `RenderMarkdown` function in `internal/reconcile/emit.go` calls `BuildDisagreements`** which is also called when writing `disagreements.json`. This means `BuildDisagreements` is called twice per `Emit` invocation. Minor inefficiency.

3. **The header text in the radar section** doesn't mention "verification disagreements". This is a minor documentation issue but could be confusing for users who see verification disagreements in the radar.

4. **The `soloItem` condition `len(f.Reviewers) <= 1`** catches both empty and single reviewer lists. The planning doc says "exactly one distinct reviewer". This is a minor deviation from the spec.

5. **In `cmd/atcr/report.go`**: The `format` variable is not used in the `disagreements` case. This is intentional but could be flagged as a minor issue.

6. **In `cmd/atcr/report.go`**: The `disagreements` case doesn't handle the case where `format` is set to a non-markdown value. The `--disagreements` flag always renders the focused view, ignoring `--format`. This could be confusing.

Let me look more carefully for actual bugs:

Looking at `internal/reconcile/emit.go` `RenderMarkdown`:
```go
writeSeverityConfidenceTable(&b, inScope)

// Disagreement radar above the consensus findings (Epic 3.2). Nothing is
// written when there is no tension, so report.md is byte-identical to the
// pre-3.2 output for a review with no disagreements.
writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
```

And in `internal/report/render.go` `renderMarkdown`:
```go
// Disagreement radar above the consensus findings (Epic 3.2). Empty df →
// nothing written → output identical to the plain report.
writeRadarSection(&b, df)
```

Both call `writeRadarSection` with a `*bytes.Buffer` and a `DisagreementsFile`. But the functions are defined in different packages. The reconcile package's `writeRadarSection` is used for the persisted report.md, and the report package's `writeRadarSection` is used for the CLI/MCP report.

Looking at the test `TestRenderMarkdown_RadarSectionAboveFindings`:
```go
res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
var buf bytes.Buffer
require.NoError(t, RenderMarkdown(&buf, res))
```

This calls `RenderMarkdown` in the reconcile package. The test checks for `## Disagreements` in the output. Good.

Looking at the test `TestRenderMarkdownWithDisagreements_RadarAboveFindings`:
```go
df := reconcile.BuildDisagreements(findings, nil)
var buf bytes.Buffer
require.NoError(t, RenderMarkdownWithDisagreements(&buf, findings, df))
```

This calls `RenderMarkdownWithDisagreements` in the report package. The test checks for `## Disagreements` in the output. Good.

Now, let me look at the `internal/reconcile/emit.go` more carefully:

The `RenderMarkdown` function in the reconcile package is:
```go
func RenderMarkdown(w io.Writer, r Result) error {
    var b bytes.Buffer
    ...
    writeSeverityConfidenceTable(&b, inScope)

    // Disagreement radar above the consensus findings (Epic 3.2). Nothing is
    // written when there is no tension, so report.md is byte-identical to the
    // pre-3.2 output for a review with no disagreements.
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ...
}
```

The `r.JSONFindings()` method is called. Let me check if this method exists. It should return `[]JSONFinding`. The `r.Ambiguous` field is `[]AmbiguousCluster`. Good.

Now, the key question: is there a bug where `BuildDisagreements` is called and the result is used in two places (report.md and disagreements.json), but the `BuildDisagreements` function is not deterministic? Let me check:

The `BuildDisagreements` function:
1. Builds `grayKeys` from clusters (deterministic if clusters are in a stable order).
2. Iterates over findings (order is preserved from input).
3. Iterates over clusters (order is preserved from input).
4. Sorts items using `sortDisagreements` (deterministic).

The function is pure and deterministic. Good.

But wait - the `grayKeys` map iteration order is not deterministic in Go! However, the map is only used for lookup, not iteration. So this is fine.

Now, let me look at the `sortDisagreements` function:
```go
func sortDisagreements(items []DisagreementItem) {
    sort.SliceStable(items, func(i, j int) bool {
        a, b := items[i], items[j]
        if a.Score != b.Score {
            return a.Score > b.Score
        }
        if ra, rb := severityRank[a.Severity], severityRank[b.Severity]; ra != rb {
            return ra > rb
        }
        if a.File != b.File {
            return a.File < b.File
        }
        if a.Line != b.Line {
            return a.Line < b.Line
        }
        if a.Kind != b.Kind {
            return a.Kind < b.Kind
        }
        return a.Problem < b.Problem
    })
}
```

This is a total order: score desc, severity rank desc, file asc, line asc, kind asc, problem asc. Deterministic. Good.

Now, let me think about edge cases:

1. **Empty findings and empty clusters**: `BuildDisagreements` returns empty items. `writeRadarSection` writes nothing. Good.

2. **All refuted findings**: `BuildDisagreements` returns empty items. Good.

3. **All out-of-scope findings**: `BuildDisagreements` returns empty items. Good.

4. **Single finding with verification tie**: `BuildDisagreements` returns one item. Good.

5. **Multiple findings with same score**: `sortDisagreements` breaks ties by file, line, kind, problem. Good.

Now, let me look for actual bugs:

**Bug 1**: In `internal/reconcile/emit.go`, the `RenderMarkdown` function in the reconcile package calls `BuildDisagreements(r.JSONFindings(), r.Ambiguous)`. But the `Emit` function also writes `disagreements.json` via a separate call to `BuildDisagreements`. This means `BuildDisagreements` is called twice per `Emit` invocation. This is a minor inefficiency but not a bug.

**Bug 2**: In `cmd/atcr/report.go`, the `disagreements` case:
```go
case disagreements:
    clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
    if err != nil {
        return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
    }
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
```

The `format` variable is not used. If a user passes `--disagreements --format json`, the format is ignored. This is documented in the comment but could be confusing.

**Bug 3**: In `internal/reconcile/disagree.go`, the `grayZoneItem` function:
```go
if r, ok := severityRank[f.Severity]; ok {
    if r > maxRank {
        maxRank, maxSev = r, f.Severity
    }
    if r < minRank {
        minRank = r
    }
}
```

If a cluster has findings with unknown severities, those findings are skipped for the spread calculation but still added to positions. This is correct behavior.

**Bug 4**: In `internal/reconcile/disagree.go`, the `BuildDisagreements` function:
```go
for _, f := range findings {
    if isRefutedJSON(f) {
        continue
    }
    if f.Category == CategoryOutOfScope {
        continue
    }
    if grayKeys[findingKey(f.File, f.Line, f.Problem)] {
        continue
    }
    switch {
    case isVerificationTie(f.Verification):
        items = append(items, verificationItem(f))
    case f.Disagreement != "":
        items = append(items, severitySplitItem(f))
    case len(f.Reviewers) <= 1:
        items = append(items, soloItem(f))
    }
}
```

The `len(f.Reviewers) <= 1` condition is a deviation from the planning doc which says "exactly one distinct reviewer". This is a minor issue.

**Bug 5**: In `internal/reconcile/disagree.go`, the `writeRadarSection` function header text:
```go
fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))
```

The header says "reviewer splits, solo findings, and gray-zone clusters" but doesn't mention "verification disagreements". This is a minor documentation issue.

**Bug 6**: In `internal/report/disagree.go`, the same header text issue.

**Bug 7**: In `cmd/atcr/report.go`, the `disagreements` case doesn't pass the `format` variable to `RenderDisagreements`. This is intentional.

**Bug 8**: In `cmd/atcr/report_test.go`, the test `TestReportCmd_DisagreementsModeEmptyIsClean` doesn't check the output content. This is a weak test.

**Bug 9**: In `cmd/atcr/report_test.go`, the test `TestReportCmd_DisagreementsModeReadsAmbiguousSidecar` doesn't check the output content. This is a weak test.

**Bug 10**: In `internal/reconcile/disagree.go`, the `splitNames` function splits on comma. If a skeptic name contains a comma, this would split incorrectly. Minor concern.

Now, let me think about which of these are real issues that should be flagged:

1. **Code duplication** - This is a real maintainability concern. The `writeRadarSection` and `formatScore` functions are defined in both `internal/reconcile/disagree.go` and `internal/report/disagree.go`. Any change to the radar rendering would need to be made in two places.

2. **The `BuildDisagreements` is called twice in `Emit`** - This is a minor inefficiency.

3. **The `soloItem` condition `<= 1` vs `== 1`** - The planning doc says "exactly one distinct reviewer". The code uses `<= 1`. This is a minor deviation from the spec.

4. **The header text doesn't mention verification disagreements** - Minor documentation issue.

5. **The `disagreements` flag ignores `--format`** - This is documented in the comment but could be confusing.

Now, let me look for actual bugs that would cause incorrect behavior:

**Looking at `cmd/atcr/report.go` more carefully**:

The switch structure:
```go
switch {
case disagreements:
    clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
    if err != nil {
        return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
    }
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
case format == report.FormatMarkdown:
    clusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return usageError(err)
    }
default:
    if err := report.Render(&buf, findings, format); err != nil {
        return usageError(err)
    }
}
```

This is correct. The `disagreements` case is checked first. If true, the markdown case is not reached.

**Looking at `internal/mcp/handlers.go`**:

The handler:
```go
if format == report.FormatMarkdown {
    clusters, _ := reconcile.ReadAmbiguousClusters(dir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return nil, ReportResult{}, err
    }
} else if err := report.Render(&buf, findings, format); err != nil {
    return nil, ReportResult{}, err
}
```

This is correct. The MCP handler doesn't have a `--disagreements` equivalent, so the radar is always included in the markdown report.

**Looking at `internal/reconcile/emit.go`**:

The `RenderMarkdown` function:
```go
writeSeverityConfidenceTable(&b, inScope)

// Disagreement radar above the consensus findings (Epic 3.2). Nothing is
// written when there is no tension, so report.md is byte-identical to the
// pre-3.2 output for a review with no disagreements.
writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
```

The `BuildDisagreements` is called with `r.JSONFindings()` and `r.Ambiguous`. The `r.JSONFindings()` method should return `[]JSONFinding`. The `r.Ambiguous` is `[]AmbiguousCluster`. Good.

**Looking at `internal/reconcile/disagree.go`**:

The `BuildDisagreements` function:
```go
grayKeys := map[string]bool{}
for _, c := range clusters {
    for _, f := range c.Findings {
        grayKeys[findingKey(f.File, f.Line, f.Problem)] = true
    }
}
```

The `f` here is of type `stream.Finding`. The `findingKey` function takes `file string, line int, problem string`. Good.

The `findingKey` function:
```go
func findingKey(file string, line int, problem string) string {
    return file + "\x00" + strconv.Itoa(line) + "\x00" + problem
}
```

The `\x00` separator prevents collisions. Good.

Now, let me look at the `cmd/atcr/report.go` `format` variable usage:

In the `disagreements` case, `format` is not used. This means if a user passes `--disagreements --format json`, the format is ignored. The `--disagreements` flag always renders the focused view.

Wait, let me re-read the `disagreements` case:
```go
case disagreements:
    clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
    if err != nil {
        return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
    }
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
```

The `format` variable is not used. The `RenderDisagreements` function always renders markdown. If a user passes `--disagreements --format json`, the format is ignored. This is documented in the comment: "The disagreement radar is a focused, ranked view; it replaces the standard report rather than layering onto a chosen --format."

This is intentional but could be confusing for users.

Now, let me look at the `internal/reconcile/emit.go` `RenderMarkdown` function again:

The function is called from `Emit` to write the report.md. The radar section is injected. But the function is also exported and could be called from elsewhere. If a test or external code calls `RenderMarkdown` with a `Result` that has ambiguous clusters, the radar would be included. This is consistent with the planning doc.

Now, let me think about the overall structure:

The code has two implementations of the radar rendering:
1. `internal/reconcile/disagree.go` - `writeRadarSection` - used by `RenderMarkdown` in the reconcile package
2. `internal/report/disagree.go` - `writeRadarSection` - used by `renderMarkdown` in the report package

This is duplication. Any change to the radar rendering would need to be made in two places. This is a maintainability concern.

Also, the `formatScore` function is duplicated:
1. `internal/reconcile/disagree.go` - `formatScore`
2. `internal/report/disagree.go` - `formatScore`

Now, let me look at the test coverage:

The tests cover:
- `TestBuildDisagreements_SeveritySplitScoredBySpreadTimesIndependence`
- `TestBuildDisagreements_SoloScoredBySeverityRank`
- `TestBuildDisagreements_CriticalSoloOutranksLowMediumSplit`
- `TestBuildDisagreements_MultiReviewerConsensusIsNotSurfaced`
- `TestBuildDisagreements_GrayZoneClusterSurfacedWithPositions`
- `TestBuildDisagreements_GrayZoneMembersNotDoubleSurfacedAsSolo`
- `TestBuildDisagreements_ExcludesOutOfScope`
- `TestBuildDisagreements_DeterministicOrdering`
- `TestBuildDisagreements_VerificationTieSurfaced`
- `TestBuildDisagreements_SingleSkepticUnverifiableIsNotTie`
- `TestBuildDisagreements_RefutedNeverSurfaced`
- `TestBuildDisagreements_VerificationTieAlsoSplitKeepsSpreadScore`
- `TestRenderMarkdown_RadarSectionAboveFindings`
- `TestRenderMarkdown_NoDisagreementsOmitsRadar`
- `TestEmit_WritesDisagreementsJSON`
- `TestReadDisagreements_MissingReturnsEmpty`
- `TestReadDisagreements_MalformedIsError`
- `TestBuildDisagreements_SchemaMetadata`
- `TestDisagreementsSchema_StableContract`
- `TestRenderDisagreements_EmptyIsExplicit`
- `TestRenderDisagreements_RanksHighestTensionFirst`
- `TestRenderDisagreements_GrayZoneShowsPositionsSideBySide`
- `TestRenderMarkdownWithDisagreements_RadarAboveFindings`
- `TestRenderMarkdownWithDisagreements_EmptyMatchesPlainRender`
- `TestRenderDisagreements_ShowsVerificationSkepticSplit`
- `TestRenderDisagreements_EscapesFreeText`

This is good test coverage.

Now, let me look at the `cmd/atcr/report_test.go` tests:

- `TestReportCmd_DisagreementsMode` - checks for "Disagreement Radar", "LOW vs CRITICAL", "`a.go:1`"
- `TestReportCmd_DisagreementsModeReadsAmbiguousSidecar` - only checks exit code
- `TestReportCmd_DisagreementsModeEmptyIsClean` - only checks exit code
- `TestReportCmd_DisagreementsModeMalformedAmbiguousIsUsageError` - checks exit code 2

The last three tests are weak - they only check exit codes. They should also check output content. But this is a test quality issue, not a bug.

Now, let me look at the `internal/reconcile/emit_test.go` tests:

- `TestEmit_WritesAllArtifacts` - checks for 6 files including `DisagreementsJSON`
- `TestEmit_DeterministicOutput` - includes `DisagreementsJSON` in comparison

Good.

Now, let me look for actual bugs:

**Potential Bug 1**: In `cmd/atcr/report.go`, the `disagreements` case:
```go
case disagreements:
    clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
    if err != nil {
        return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
    }
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
```

If `findings` is empty (no findings.json), `findings` would be nil or empty. `BuildDisagreements` handles nil findings. Good.

**Potential Bug 2**: In `cmd/atcr/report.go`, the markdown case:
```go
case format == report.FormatMarkdown:
    clusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return usageError(err)
    }
```

If `ReadAmbiguousClusters` fails (corrupt ambiguous.json), the error is silently ignored. The `clusters` would be nil. `BuildDisagreements` handles nil clusters. The radar would be findings-only. This is intentional.

**Potential Bug 3**: In `internal/reconcile/disagree.go`, the `BuildDisagreements` function:
```go
for _, f := range findings {
    if isRefutedJSON(f) {
        continue
    }
    if f.Category == CategoryOutOfScope {
        continue
    }
    if grayKeys[findingKey(f.File, f.Line, f.Problem)] {
        continue
    }
    switch {
    case isVerificationTie(f.Verification):
        items = append(items, verificationItem(f))
    case f.Disagreement != "":
        items = append(items, severitySplitItem(f))
    case len(f.Reviewers) <= 1:
        items = append(items, soloItem(f))
    }
}
```

The `len(f.Reviewers) <= 1` condition is a deviation from the planning doc which says "exactly one distinct reviewer". This means a finding with no reviewers would also be classified as a solo finding. This is a minor issue.

**Potential Bug 4**: In `internal/reconcile/disagree.go`, the `writeRadarSection` function header text doesn't mention "verification disagreements". This is a minor documentation issue.

**Potential Bug 5**: In `internal/report/disagree.go`, the same header text issue.

**Potential Bug 6**: Code duplication between `internal/reconcile/disagree.go` and `internal/report/disagree.go`.

Now, let me think about which of these are real issues that should be flagged in the TD_STREAM format:

1. **Code duplication** - This is a real maintainability concern. The `writeRadarSection` and `formatScore` functions are defined in both packages. This is a LOW severity issue.

2. **The `soloItem` condition `<= 1` vs `== 1`** - The planning doc says "exactly one distinct reviewer". The code uses `<= 1`. This is a LOW severity issue.

3. **The header text doesn't mention verification disagreements** - This is a LOW severity documentation issue.

4. **The `disagreements` flag ignores `--format`** - This is documented in the comment but could be confusing. LOW severity.

5. **The `BuildDisagreements` is called twice in `Emit`** - This is a minor inefficiency. LOW severity.

6. **Weak test coverage in `cmd/atcr/report_test.go`** - The tests for empty and reads-ambiguous-sidecar only check exit codes. LOW severity.

Now, let me look more carefully for actual bugs that would cause incorrect behavior:

**Looking at `internal/reconcile/emit.go` `RenderMarkdown`**:

The function is:
```go
func RenderMarkdown(w io.Writer, r Result) error {
    var b bytes.Buffer
    ...
    writeSeverityConfidenceTable(&b, inScope)

    // Disagreement radar above the consensus findings (Epic 3.2). Nothing is
    // written when there is no tension, so report.md is byte-identical to the
    // pre-3.2 output for a review with no disagreements.
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ...
}
```

Wait, I need to check if `r.JSONFindings()` is a method or a field. Let me look at the test:
```go
res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
var buf bytes.Buffer
require.NoError(t, RenderMarkdown(&buf, res))
```

The test passes `res` to `RenderMarkdown`. The `res` is a `Result`. The `RenderMarkdown` function takes a `Result`. Inside, it calls `r.JSONFindings()` and `r.Ambiguous`. The `r.JSONFindings()` should be a method that returns `[]JSONFinding`. The `r.Ambiguous` is a field of type `[]AmbiguousCluster`. Good.

But wait - in the `Emit` function:
```go
{DisagreementsJSON, func(w io.Writer) error {
    return renderIndentedJSON(w, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
}},
```

The `r.JSONFindings()` is called again. This is the second call to `BuildDisagreements` for the same `r`. The `r.JSONFindings()` method might be expensive if it constructs a new slice each time. But this is a minor inefficiency.

**Looking at `internal/reconcile/disagree.go` `BuildDisagreements`**:

The function takes `findings []JSONFinding, clusters []AmbiguousCluster`. The `findings` parameter is a slice, not a pointer. The function is pure and doesn't modify the input. Good.

The function:
1. Builds `grayKeys` from clusters.
2. Iterates over findings, filtering out refuted, out-of-scope, and gray-zone members.
3. Iterates over clusters, filtering out all-out-of-scope clusters.
4. Sorts items.
5. Returns `DisagreementsFile`.

Good.

**Looking at `internal/reconcile/disagree.go` `writeRadarSection`**:

The function:
```go
func writeRadarSection(b *bytes.Buffer, df DisagreementsFile) {
    if len(df.Items) == 0 {
        return
    }
    fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))
    for i, it := range df.Items {
        fmt.Fprintf(b, "\n### %d. %s — %s (%s) · score %s\n",
            i+1, esc(it.Kind), codeSpan(it.File, it.Line), esc(it.Severity), formatScore(it.Score))
        if it.Disagreement != "" {
            fmt.Fprintf(b, "- Severity disagreement: %s\n", esc(it.Disagreement))
        }
        if it.Skeptics != "" {
            fmt.Fprintf(b, "- Skeptics split: %s\n", esc(it.Skeptics))
        }
        if len(it.Reviewers) > 0 {
            fmt.Fprintf(b, "- Reviewers: %s (independence %d)\n", esc(joinOrNone(it.Reviewers)), it.Independence)
        }
        if it.Problem != "" {
            fmt.Fprintf(b, "- Problem: %s\n", esc(it.Problem))
        }
        if it.Detail != "" {
            fmt.Fprintf(b, "- Detail: %s\n", esc(it.Detail))
        }
        if len(it.Positions) > 0 {
            b.WriteString("- Positions:\n")
            for _, p := range it.Positions {
                name := p.Reviewer
                if name == "" {
                    name = "(unknown)"
                }
                fmt.Fprintf(b, "  - %s — %s: %s\n", esc(name), esc(p.Severity), esc(p.Problem))
            }
        }
    }
}
```

This function uses `esc`, `codeSpan`, `joinOrNone`, and `formatScore`. These must be defined elsewhere in the reconcile package. The `formatScore` is defined in the same file. The `esc`, `codeSpan`, and `joinOrNone` must be defined in another file. Assuming they exist, this is fine.

**Looking at `internal/report/disagree.go` `writeRadarSection`**:

The function:
```go
func writeRadarSection(b *bytes.Buffer, df reconcile.DisagreementsFile) {
    if len(df.Items) == 0 {
        return
    }
    fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))
    writeRadarItems(b, df.Items, "### ")
}
```

This function calls `writeRadarItems` with `"### "` prefix. The `writeRadarItems` function:
```go
func writeRadarItems(b *bytes.Buffer, items []reconcile.DisagreementItem, heading string) {
    for i, it := range items {
        fmt.Fprintf(b, "\n%s%d. %s — %s (%s) · score %s\n",
            heading, i+1, esc(it.Kind), codeSpan(it.File, it.Line), esc(it.Severity), formatScore(it.Score))
        ...
    }
}
```

This uses `esc`, `codeSpan`, `escTrunc`, `formatScore`, and `reviewerOrUnknown`. These must be defined in the report package. The `formatScore` is defined in the same file. The others must be defined in another file. Assuming they exist, this is fine.

Now, let me look for actual bugs:

**Bug**: The `internal/reconcile/disagree.go` `writeRadarSection` function uses `joinOrNone` which is not defined in the visible code. But it's likely defined in another file in the reconcile package. Same for `esc` and `codeSpan`. These are not bugs.

**Bug**: The `internal/report/disagree.go` `writeRadarItems` function uses `escTrunc` which is not defined in the visible code. But it's likely defined in another file in the report package. Not a bug.

Now, let me think about the overall quality of the code:

The code is well-structured with clear separation of concerns. The tests are comprehensive. The documentation is thorough.

Real issues I can identify:

1. **Code duplication**: `writeRadarSection` and `formatScore` are defined in both `internal/reconcile/disagree.go` and `internal/report/disagree.go`. This is a maintainability concern.

2. **The `soloItem` condition `len(f.Reviewers) <= 1`** is a deviation from the planning doc which says "exactly one distinct reviewer". This means a finding with no reviewers would also be classified as a solo finding.

3. **The header text in the radar section** doesn't mention "verification disagreements". This is a minor documentation issue.

4. **The `disagreements` flag ignores `--format`** in the CLI. This is documented but could be confusing.

5. **The `BuildDisagreements` is called twice in `Emit`** - once for the report.md and once for the disagreements.json. Minor inefficiency.

6. **Weak test coverage in `cmd/atcr/report_test.go`** - some tests only check exit codes.

Now, let me format these as TD_STREAM findings:

1. **Code duplication** - LOW severity, maintainability
2. **Solo finding condition deviation** - LOW severity, correctness
3. **Header text missing verification disagreements** - LOW severity, maintainability
4. **Disagreements flag ignores format** - LOW severity, maintainability
5. **BuildDisagreements called twice in Emit** - LOW severity, performance
6. **Weak test coverage** - LOW severity, maintainability

Let me re-examine the diff for more issues:

In `cmd/atcr/report.go`:
```go
disagreements, _ := cmd.Flags().GetBool("disagreements")
// The disagreement radar is a focused, ranked view; it replaces the standard
// report rather than layering onto a chosen --format.
switch {
case disagreements:
    clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
    if err != nil {
        return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
    }
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
case format == report.FormatMarkdown:
    // The standard markdown report carries the radar above its findings. A
    // corrupt ambiguous.json must not break the main report, so a read error
    // degrades to a findings-only radar rather than failing (the dedicated
    // --disagreements view above surfaces such errors explicitly instead).
    clusters, _ := reconcile.ReadAmbiguousClusters(reviewDir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return usageError(err)
    }
default:
    if err := report.Render(&buf, findings, format); err != nil {
        return usageError(err)
    }
}
```

This is correct. The `disagreements` case is checked first. The `format` variable is used in the second case and the default case.

In `internal/reconcile/disagree.go`:
```go
func BuildDisagreements(findings []JSONFinding, clusters []AmbiguousCluster) DisagreementsFile {
    items := make([]DisagreementItem, 0, len(findings)+len(clusters))

    // Locations covered by a gray-zone cluster: their member findings surface as
    // the cluster item, never again as solo/split items.
    grayKeys := map[string]bool{}
    for _, c := range clusters {
        for _, f := range c.Findings {
            grayKeys[findingKey(f.File, f.Line, f.Problem)] = true
        }
    }

    for _, f := range findings {
        if isRefutedJSON(f) {
            continue
        }
        // Out-of-scope findings are pre-existing issues outside the reviewed
        // change; they are annotated in their own report section and excluded from
        // the gate, so they are not change-tension and never enter the radar.
        if f.Category == CategoryOutOfScope {
            continue
        }
        if grayKeys[findingKey(f.File, f.Line, f.Problem)] {
            continue
        }
        switch {
        case isVerificationTie(f.Verification):
            items = append(items, verificationItem(f))
        case f.Disagreement != "":
            items = append(items, severitySplitItem(f))
        case len(f.Reviewers) <= 1:
            items = append(items, soloItem(f))
        }
    }
    for _, c := range clusters {
        // Skip a gray-zone pair only when every member is out-of-scope (fail-
        // closed, matching modalCategory): an in-scope member keeps the pair as
        // real change-tension.
        if allOutOfScope(c.Findings) {
            continue
        }
        items = append(items, grayZoneItem(c))
    }

    sortDisagreements(items)
    return DisagreementsFile{
        SchemaVersion:     DisagreementsSchemaVersion,
        IndependenceModel: IndependenceModelReviewerCount,
        Items:             items,
    }
}
```

This is correct. The function builds items, sorts them, and returns a `DisagreementsFile`.

Now, let me look at the `grayZoneItem` function:
```go
func grayZoneItem(c AmbiguousCluster) DisagreementItem {
    maxRank, minRank := 0, 1<<31
    var maxSev string
    revSet := map[string]bool{}
    positions := make([]Position, 0, len(c.Findings))
    for _, f := range c.Findings {
        if r, ok := severityRank[f.Severity]; ok {
            if r > maxRank {
                maxRank, maxSev = r, f.Severity
            }
            if r < minRank {
                minRank = r
            }
        }
        if f.Reviewer != "" {
            revSet[f.Reviewer] = true
        }
        positions = append(positions, Position{Reviewer: f.Reviewer, Severity: f.Severity, Problem: f.Problem})
    }
    spread := 0
    if maxRank > 0 && minRank <= maxRank {
        spread = maxRank - minRank
    }
    if maxSev == "" && len(c.Findings) > 0 {
        maxSev = c.Findings[0].Severity
    }
    reviewers := sortedKeys(revSet)
    indep := atLeastOne(len(reviewers))
    return DisagreementItem{
        Kind:         KindGrayZone,
        File:         c.File,
        Line:         c.Line,
        Severity:     maxSev,
        Problem:      longestProblem(c.Findings),
        Score:        scoreFor(spread, indep, severityRank[maxSev]),
        Spread:       spread,
        Independence: indep,
        Reviewers:    reviewers,
        Detail:       "similarity " + strconv.FormatFloat(c.Similarity, 'f', 2, 64),
        Positions:    positions,
    }
}
```

This is correct. The function builds a `DisagreementItem` for a gray-zone cluster.

The `minRank = 1<<31` initialization is a large value. If no valid severity is found, `minRank` stays at this value. The spread calculation `if maxRank > 0 && minRank <= maxRank` would be `0 > 0` = false, so spread=0. Good.

But wait - `if maxRank > 0 && minRank <= maxRank` - the `minRank <= maxRank` is always true when `maxRank > 0` (since `minRank` is either initialized to `1<<31` or updated to a value <= `maxRank` by construction). So the `minRank <= maxRank` check is redundant. But this is not a bug.

Actually, let me re-check: if `maxRank` is updated to 3 (for HIGH), then `minRank` would be updated to 3 (for the first finding) or to 1 (for the second finding). So `minRank <= maxRank` is always true. The check is redundant but not incorrect.

Now, let me look at the `scoreFor` function:
```go
func scoreFor(spread, independence, sevRank int) float64 {
    if spread > 0 {
        return float64(spread * independence)
    }
    return float64(sevRank)
}
```

This is correct. If there's a severity spread, score = spread × independence. Otherwise, score = severity rank.

The `grayZoneItem` calls `scoreFor(spread, indep, severityRank[maxSev])`. If `maxSev` is empty (no valid severity found), `severityRank[maxSev]` returns 0, so score=0. This is a minor edge case.

Now, let me look at the `verificationItem` function:
```go
func verificationItem(f JSONFinding) DisagreementItem {
    spread := spreadFromDisagreement(f.Disagreement)
    indep := atLeastOne(len(f.Reviewers))
    return DisagreementItem{
        Kind:         KindVerificationDisagreement,
        File:         f.File,
        Line:         f.Line,
        Severity:     f.Severity,
        Problem:      f.Problem,
        Score:        scoreFor(spread, indep, severityRank[f.Severity]),
        Spread:       spread,
        Independence: indep,
        Reviewers:    f.Reviewers,
        Disagreement: f.Disagreement,
        Skeptics:     f.Verification.Skeptic,
        Detail:       f.Verification.Notes,
    }
}
```

This is correct. The function builds a `DisagreementItem` for a verification disagreement.

The `f.Verification.Skeptic` and `f.Verification.Notes` are used. If `f.Verification` is nil, this would panic. But the caller checks `isVerificationTie(f.Verification)` first, which returns false if `f.Verification` is nil. Good.

Now, let me look at the `severitySplitItem` function:
```go
func severitySplitItem(f JSONFinding) DisagreementItem {
    spread := spreadFromDisagreement(f.Disagreement)
    indep := atLeastOne(len(f.Reviewers))
    return DisagreementItem{
        Kind:         KindSeveritySplit,
        File:         f.File,
        Line:         f.Line,
        Severity:     f.Severity,
        Problem:      f.Problem,
        Score:        scoreFor(spread, indep, severityRank[f.Severity]),
        Spread:       spread,
        Independence: indep,
        Reviewers:    f.Reviewers,
        Disagreement: f.Disagreement,
    }
}
```

This is correct. The function builds a `DisagreementItem` for a severity split.

Now, let me look at the `soloItem` function:
```go
func soloItem(f JSONFinding) DisagreementItem {
    indep := atLeastOne(len(f.Reviewers))
    return DisagreementItem{
        Kind:         KindSoloFinding,
        File:         f.File,
        Line:         f.Line,
        Severity:     f.Severity,
        Problem:      f.Problem,
        Score:        scoreFor(0, indep, severityRank[f.Severity]),
        Spread:       0,
        Independence: indep,
        Reviewers:    f.Reviewers,
    }
}
```

This is correct. The function builds a `DisagreementItem` for a solo finding.

The `scoreFor(0, indep, severityRank[f.Severity])` - since spread=0, score=severityRank[f.Severity]. Good.

Now, let me look at the `spreadFromDisagreement` function:
```go
func spreadFromDisagreement(d string) int {
    parts := strings.SplitN(d, " vs ", 2)
    if len(parts) != 2 {
        return 0
    }
    lo := severityRank[strings.TrimSpace(parts[0])]
    hi := severityRank[strings.TrimSpace(parts[1])]
    if hi < lo {
        return 0
    }
    return hi - lo
}
```

This is correct. The function parses a "lo vs hi" string and returns the tier distance.

The `if hi < lo` check handles the case where the parsed order is wrong. But the planning doc says "mergeSeverity guarantees lo is the minimum and hi the maximum", so this should never happen. The check is defensive.

Now, let me look at the `isVerificationTie` function:
```go
func isVerificationTie(v *Verification) bool {
    if v == nil || canonVerdict(v.Verdict) != VerdictUnverifiable {
        return false
    }
    return len(splitNames(v.Skeptic)) >= 2
}
```

This is correct. The function checks if the verdict is "unverifiable" with 2+ skeptics.

The `canonVerdict` function normalizes the verdict (lowercase, trim). Good.

Now, let me look at the `isRefutedJSON` function:
```go
func isRefutedJSON(f JSONFinding) bool {
    return f.Verification != nil && canonVerdict(f.Verification.Verdict) == VerdictRefuted
}
```

This is correct. The function checks if the verdict is "refuted".

Now, let me look at the `atLeastOne` function:
```go
func atLeastOne(n int) int {
    if n < 1 {
        return 1
    }
    return n
}
```

This is correct. The function ensures the value is at least 1.

Now, let me look at the `longestProblem` function:
```go
func longestProblem(findings []stream.Finding) string {
    best := ""
    for _, f := range findings {
        if len(f.Problem) > len(best) {
            best = f.Problem
        }
    }
    return best
}
```

This is correct. The function returns the longest problem text.

Now, let me look at the `sortedKeys` function:
```go
func sortedKeys(set map[string]bool) []string {
    out := make([]string, 0, len(set))
    for k := range set {
        out = append(out, k)
    }
    sort.Strings(out)
    return out
}
```

This is correct. The function returns the keys of a map as a sorted slice.

Now, let me look at the `splitNames` function:
```go
func splitNames(s string) []string {
    parts := strings.Split(s, ",")
    out := make([]string, 0, len(parts))
    for _, p := range parts {
        if t := strings.TrimSpace(p); t != "" {
            out = append(out, t)
        }
    }
    return out
}
```

This is correct. The function splits a comma-joined string and trims whitespace.

Now, let me look at the `findingKey` function:
```go
func findingKey(file string, line int, problem string) string {
    return file + "\x00" + strconv.Itoa(line) + "\x00" + problem
}
```

This is correct. The function creates a unique key from file, line, and problem.

Now, let me look at the `canonVerdict` function:
```go
func canonVerdict(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
```

This is correct. The function normalizes a verdict string.

Now, let me look at the `allOutOfScope` function:
```go
func allOutOfScope(findings []stream.Finding) bool {
    if len(findings) == 0 {
        return false
    }
    for _, f := range findings {
        if strings.ToLower(strings.TrimSpace(f.Category)) != CategoryOutOfScope {
            return false
        }
    }
    return true
}
```

This is correct. The function checks if all findings in a group are out-of-scope.

Now, let me look at the `formatScore` function:
```go
func formatScore(s float64) string {
    if s == float64(int64(s)) {
        return strconv.FormatInt(int64(s), 10)
    }
    return strconv.FormatFloat(s, 'f', 2, 64)
}
```

This is correct. The function formats a score as a compact string.

Now, let me look at the `sortDisagreements` function:
```go
func sortDisagreements(items []DisagreementItem) {
    sort.SliceStable(items, func(i, j int) bool {
        a, b := items[i], items[j]
        if a.Score != b.Score {
            return a.Score > b.Score
        }
        if ra, rb := severityRank[a.Severity], severityRank[b.Severity]; ra != rb {
            return ra > rb
        }
        if a.File != b.File {
            return a.File < b.File
        }
        if a.Line != b.Line {
            return a.Line < b.Line
        }
        if a.Kind != b.Kind {
            return a.Kind < b.Kind
        }
        return a.Problem < b.Problem
    })
}
```

This is correct. The function sorts items with a total order.

Now, let me look at the `writeRadarSection` function in `internal/reconcile/disagree.go`:
```go
func writeRadarSection(b *bytes.Buffer, df DisagreementsFile) {
    if len(df.Items) == 0 {
        return
    }
    fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))
    for i, it := range df.Items {
        fmt.Fprintf(b, "\n### %d. %s — %s (%s) · score %s\n",
            i+1, esc(it.Kind), codeSpan(it.File, it.Line), esc(it.Severity), formatScore(it.Score))
        if it.Disagreement != "" {
            fmt.Fprintf(b, "- Severity disagreement: %s\n", esc(it.Disagreement))
        }
        if it.Skeptics != "" {
            fmt.Fprintf(b, "- Skeptics split: %s\n", esc(it.Skeptics))
        }
        if len(it.Reviewers) > 0 {
            fmt.Fprintf(b, "- Reviewers: %s (independence %d)\n", esc(joinOrNone(it.Reviewers)), it.Independence)
        }
        if it.Problem != "" {
            fmt.Fprintf(b, "- Problem: %s\n", esc(it.Problem))
        }
        if it.Detail != "" {
            fmt.Fprintf(b, "- Detail: %s\n", esc(it.Detail))
        }
        if len(it.Positions) > 0 {
            b.WriteString("- Positions:\n")
            for _, p := range it.Positions {
                name := p.Reviewer
                if name == "" {
                    name = "(unknown)"
                }
                fmt.Fprintf(b, "  - %s — %s: %s\n", esc(name), esc(p.Severity), esc(p.Problem))
            }
        }
    }
}
```

This is correct. The function writes the radar section to a buffer.

The header text says "reviewer splits, solo findings, and gray-zone clusters" but doesn't mention "verification disagreements". This is a minor documentation issue.

Now, let me look at the `writeRadarSection` function in `internal/report/disagree.go`:
```go
func writeRadarSection(b *bytes.Buffer, df reconcile.DisagreementsFile) {
    if len(df.Items) == 0 {
        return
    }
    fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))
    writeRadarItems(b, df.Items, "### ")
}
```

Same header text issue.

Now, let me think about the overall quality:

The code is well-structured and well-tested. The main issues are:
1. Code duplication between reconcile and report packages
2. Minor deviations from the planning doc (solo finding condition, header text)
3. Minor inefficiencies (BuildDisagreements called twice in Emit)
4. Weak test coverage in some CLI tests

These are all LOW severity issues.

Let me also check the `internal/mcp/handlers.go` more carefully:

```go
if format == report.FormatMarkdown {
    // The markdown report carries the disagreement radar above its findings
    // (Epic 3.2). A corrupt ambiguous.json degrades to a findings-only radar
    // rather than failing the report.
    clusters, _ := reconcile.ReadAmbiguousClusters(dir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return nil, ReportResult{}, err
    }
} else if err := report.Render(&buf, findings, format); err != nil {
```

This is correct. The MCP handler includes the radar in the markdown report.

Now, let me look at the `dir` variable in the MCP handler. It should be the review directory. The `ReadAmbiguousClusters` expects the review directory. Good.

Now, let me think about potential issues with the MCP handler:

1. The MCP handler doesn't have a way to request the focused `--disagreements` view. This could be a missing feature, but it's not in the sprint plan's scope.

2. The MCP handler always includes the radar in the markdown report. This is consistent with the CLI behavior for the standard report.

Now, let me look at the `internal/report/disagree.go` more carefully:

```go
func RenderDisagreements(w io.Writer, df reconcile.DisagreementsFile) error {
    var b bytes.Buffer
    b.WriteString("# atcr Disagreement Radar\n\n")
    if len(df.Items) == 0 {
        b.WriteString("No disagreements detected.\n")
        _, err := w.Write(b.Bytes())
        return err
    }
    fmt.Fprintf(&b, "%d tension spot(s), highest first.\n", len(df.Items))
    writeRadarItems(&b, df.Items, "## ")
    _, err := w.Write(b.Bytes())
    return err
}
```

This is correct. The function renders the focused view.

The empty case writes "No disagreements detected." Good.

Now, let me look at the `writeRadarItems` function:
```go
func writeRadarItems(b *bytes.Buffer, items []reconcile.DisagreementItem, heading string) {
    for i, it := range items {
        fmt.Fprintf(b, "\n%s%d. %s — %s (%s) · score %s\n",
            heading, i+1, esc(it.Kind), codeSpan(it.File, it.Line), esc(it.Severity), formatScore(it.Score))
        if it.Disagreement != "" {
            fmt.Fprintf(b, "- Severity disagreement: %s\n", esc(it.Disagreement))
        }
        if it.Skeptics != "" {
            fmt.Fprintf(b, "- Skeptics split: %s\n", esc(it.Skeptics))
        }
        if len(it.Reviewers) > 0 {
            fmt.Fprintf(b, "- Reviewers: %s (independence %d)\n", esc(joinReviewers(it.Reviewers)), it.Independence)
        }
        if it.Problem != "" {
            fmt.Fprintf(b, "- Problem: %s\n", escTrunc(it.Problem))
        }
        if it.Detail != "" {
            fmt.Fprintf(b, "- Detail: %s\n", escTrunc(it.Detail))
        }
        if len(it.Positions) > 0 {
            b.WriteString("- Positions:\n")
            for _, p := range it.Positions {
                fmt.Fprintf(b, "  - %s — %s: %s\n", esc(reviewerOrUnknown(p.Reviewer)), esc(p.Severity), escTrunc(p.Problem))
            }
        }
    }
}
```

This is correct. The function renders each ranked item.

Note: The `writeRadarItems` in `internal/report/disagree.go` uses `escTrunc` for problem and detail, while the `writeRadarSection` in `internal/reconcile/disagree.go` uses `esc` (no truncation). This is a minor inconsistency.

Wait, let me check this more carefully:

In `internal/reconcile/disagree.go`:
```go
if it.Problem != "" {
    fmt.Fprintf(b, "- Problem: %s\n", esc(it.Problem))
}
if it.Detail != "" {
    fmt.Fprintf(b, "- Detail: %s\n", esc(it.Detail))
}
```

In `internal/report/disagree.go`:
```go
if it.Problem != "" {
    fmt.Fprintf(b, "- Problem: %s\n", escTrunc(it.Problem))
}
if it.Detail != "" {
    fmt.Fprintf(b, "- Detail: %s\n", escTrunc(it.Detail))
}
```

The reconcile package uses `esc` (no truncation), while the report package uses `escTrunc` (truncation). This is an inconsistency. The two implementations of the radar section produce different output for the same input.

Wait, but the reconcile package's `writeRadarSection` is used for the persisted report.md (via `Emit` → `RenderMarkdown` → `writeRadarSection`), while the report package's `writeRadarSection` is used for the CLI/MCP report (via `RenderMarkdownWithDisagreements` → `renderMarkdown` → `writeRadarSection`).

So the persisted report.md and the CLI/MCP report would have different output for the same findings. This is a bug!

Let me verify this:

1. `Emit` calls `RenderMarkdown` in the reconcile package, which calls `writeRadarSection` in the reconcile package. This uses `esc` (no truncation).

2. CLI `atcr report` calls `RenderMarkdownWithDisagreements` in the report package, which calls `renderMarkdown` in the report package, which calls `writeRadarSection` in the report package. This uses `escTrunc` (truncation).

So the persisted report.md and the CLI report.md would have different output for long problem texts. This is a bug.

Actually, wait. Let me re-read the code:

In `internal/report/render.go`:
```go
func renderMarkdown(w io.Writer, findings []reconcile.JSONFinding, df reconcile.DisagreementsFile) error {
    var b bytes.Buffer
    b.WriteString("# atcr Review Report\n\n")
    verified := anyVerification(findings)
    writeSummaryGrid(&b, findings, verified)

    // Disagreement radar above the consensus findings (Epic 3.2). Empty df →
    // nothing written → output identical to the plain report.
    writeRadarSection(&b, df)
    ...
}
```

The `writeRadarSection` here is the one in the report package. It uses `writeRadarItems` which uses `escTrunc`.

In `internal/reconcile/emit.go`:
```go
func RenderMarkdown(w io.Writer, r Result) error {
    var b bytes.Buffer
    ...
    writeSeverityConfidenceTable(&b, inScope)

    // Disagreement radar above the consensus findings (Epic 3.2). Nothing is
    // written when there is no tension, so report.md is byte-identical to the
    // pre-3.2 output for a review with no disagreements.
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ...
}
```

The `writeRadarSection` here is the one in the reconcile package. It uses `esc` (no truncation).

So yes, the two implementations of `writeRadarSection` produce different output for long problem texts. This is a bug.

This is a real issue. The persisted report.md and the CLI report.md should be consistent.

Let me also check the positions rendering:

In `internal/reconcile/disagree.go`:
```go
if len(it.Positions) > 0 {
    b.WriteString("- Positions:\n")
    for _, p := range it.Positions {
        name := p.Reviewer
        if name == "" {
            name = "(unknown)"
        }
        fmt.Fprintf(b, "  - %s — %s: %s\n", esc(name), esc(p.Severity), esc(p.Problem))
    }
}
```

In `internal/report/disagree.go`:
```go
if len(it.Positions) > 0 {
    b.WriteString("- Positions:\n")
    for _, p := range it.Positions {
        fmt.Fprintf(b, "  - %s — %s: %s\n", esc(reviewerOrUnknown(p.Reviewer)), esc(p.Severity), escTrunc(p.Problem))
    }
}
```

The reconcile package uses `esc` for problem text in positions, while the report package uses `escTrunc`. This is another inconsistency.

So there are multiple inconsistencies between the two implementations:
1. Problem text: `esc` vs `escTrunc`
2. Detail text: `esc` vs `escTrunc`
3. Position problem text: `esc` vs `escTrunc`

This is a real bug. The persisted report.md and the CLI report.md would have different output.

This is a MEDIUM severity issue because it causes inconsistent output between the persisted and CLI reports.

Let me also check the `reviewerOrUnknown` vs inline `(unknown)` logic:

In `internal/reconcile/disagree.go`:
```go
name := p.Reviewer
if name == "" {
    name = "(unknown)"
}
```

In `internal/report/disagree.go`:
```go
esc(reviewerOrUnknown(p.Reviewer))
```

Where `reviewerOrUnknown` is:
```go
func reviewerOrUnknown(name string) string {
    if name == "" {
        return "(unknown)"
    }
    return name
}
```

These are equivalent. Good.

Now, let me also check the `joinOrNone` vs `joinReviewers` functions:

In `internal/reconcile/disagree.go`:
```go
fmt.Fprintf(b, "- Reviewers: %s (independence %d)\n", esc(joinOrNone(it.Reviewers)), it.Independence)
```

In `internal/report/disagree.go`:
```go
fmt.Fprintf(b, "- Reviewers: %s (independence %d)\n", esc(joinReviewers(it.Reviewers)), it.Independence)
```

These are different function names but likely do the same thing. The `joinOrNone` and `joinReviewers` are not defined in the visible code but are likely defined in other files. Assuming they do the same thing, this is fine.

Now, let me think about the severity of the inconsistencies:

1. The persisted report.md (via `Emit`) uses `esc` for problem/detail text.
2. The CLI report.md (via `atcr report`) uses `escTrunc` for problem/detail text.

If `escTrunc` truncates long text, the CLI report would have shorter problem text than the persisted report. This is an inconsistency that could confuse users.

This is a MEDIUM severity issue.

Now, let me also look at the `formatScore` duplication:

The `formatScore` function is defined in both packages with the same implementation. This is code duplication. LOW severity.

Now, let me look at the `writeRadarSection` duplication:

The `writeRadarSection` function is defined in both packages with similar but inconsistent implementations. This is code duplication with inconsistencies. MEDIUM severity.

Let me also check if there are any other issues:

Looking at `internal/reconcile/emit.go`:
```go
{DisagreementsJSON, func(w io.Writer) error {
    return renderIndentedJSON(w, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
}},
```

The `BuildDisagreements` is called here. The `r.JSONFindings()` method is called. If `r.JSONFindings()` is expensive (e.g., constructs a new slice), this could be inefficient. But this is a minor concern.

Now, let me look at the test `TestEmit_WritesAllArtifacts`:
```go
for _, name := range []string{FindingsTxt, FindingsJSON, ReportMD, SummaryJSON, AmbiguousJSON, DisagreementsJSON} {
    assert.FileExists(t, filepath.Join(dir, name))
}
```

This checks that all 6 files are written. Good.

Now, let me look at the test `TestEmit_DeterministicOutput`:
```go
for _, name := range []string{FindingsTxt, FindingsJSON, ReportMD, SummaryJSON, DisagreementsJSON} {
    a, _ := os.ReadFile(filepath.Join(d1, name))
    b, _ := os.ReadFile(filepath.Join(d2, name))
    assert.Equal(t, string(a), string(b), "%s must be byte-identical across runs", name)
}
```

This checks that the output is deterministic. Good.

Now, let me think about the overall quality:

The code is well-structured and well-tested. The main issues are:
1. Inconsistencies between the two implementations of `writeRadarSection` (MEDIUM)
2. Code duplication (LOW)
3. Minor deviations from the planning doc (LOW)
4. Minor inefficiencies (LOW)

The MEDIUM severity issue is the inconsistency between the persisted report.md and the CLI report.md. This could confuse users.

Let me also look at the `cmd/atcr/report.go` more carefully:

```go
disagreements, _ := cmd.Flags().GetBool("disagreements")
```

The error from `GetBool` is ignored. This is fine because the flag is registered above.

```go
case disagreements:
    clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
    if err != nil {
        return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
    }
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
```

The `format` variable is not used. If a user passes `--disagreements --format json`, the format is ignored. This is documented in the comment.

Now, let me look at the `internal/mcp/handlers.go` more carefully:

```go
if format == report.FormatMarkdown {
    clusters, _ := reconcile.ReadAmbiguousClusters(dir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return nil, ReportResult{}, err
    }
} else if err := report.Render(&buf, findings, format); err != nil {
```

The MCP handler always includes the radar in the markdown report. This is consistent with the CLI behavior for the standard report.

Now, let me think about the final list of issues:

1. **Inconsistency between persisted and CLI report.md radar sections** (MEDIUM, correctness)
2. **Code duplication of `writeRadarSection` and `formatScore`** (LOW, maintainability)
3. **Solo finding condition `<= 1` vs `== 1`** (LOW, correctness)
4. **Header text missing verification disagreements** (LOW, maintainability)
5. **`disagreements` flag ignores `--format`** (LOW, maintainability)
6. **`BuildDisagreements` called twice in `Emit`** (LOW, performance)

Let me format these as TD_STREAM findings.

Actually, let me re-examine the inconsistency more carefully. The `writeRadarSection` in `internal/reconcile/disagree.go` is used by `RenderMarkdown` in the reconcile package, which is called by `Emit` to write the persisted report.md. The `writeRadarSection` in `internal/report/disagree.go` is used by `renderMarkdown` in the report package, which is called by `RenderMarkdownWithDisagreements` in the report package, which is called by the CLI `atcr report` command and the MCP handler.

So the persisted report.md and the CLI report.md would have different output for long problem texts. This is a real inconsistency.

Wait, let me check if the `Emit` function's `RenderMarkdown` is the only way the persisted report.md is written. Looking at the code:

```go
{ReportMD, func(w io.Writer) error { return RenderMarkdown(w, r) }},
```

Yes, `Emit` calls `RenderMarkdown` in the reconcile package to write the persisted report.md. The `RenderMarkdown` in the reconcile package calls `writeRadarSection` in the reconcile package.

The CLI `atcr report` command calls `RenderMarkdownWithDisagreements` in the report package, which calls `renderMarkdown` in the report package, which calls `writeRadarSection` in the report package.

So the two paths produce different output. This is a bug.

The fix would be to either:
1. Make both implementations use the same functions (e.g., move `writeRadarSection` to a shared location)
2. Make the reconcile package's `RenderMarkdown` call the report package's `renderMarkdown`

This is a MEDIUM severity issue.

Now, let me also check if there are any other inconsistencies:

Looking at the `writeRadarSection` in `internal/reconcile/disagree.go`:
- Uses `esc` for problem, detail, and position problem
- Uses `joinOrNone` for reviewers

Looking at the `writeRadarItems` in `internal/report/disagree.go`:
- Uses `escTrunc` for problem, detail, and position problem
- Uses `joinReviewers` for reviewers

The `esc` vs `escTrunc` difference is the main inconsistency. The `joinOrNone` vs `joinReviewers` is likely equivalent.

Now, let me think about the severity:

The inconsistency could cause:
1. Users see different output from the persisted report.md and the CLI report.md
2. Tests might pass with one implementation but fail with the other
3. The `TestRenderMarkdownWithDisagreements_EmptyMatchesPlainRender` test would fail if the implementations were unified

This is a MEDIUM severity issue.

Let me also look at the test `TestRenderMarkdownWithDisagreements_EmptyMatchesPlainRender`:
```go
func TestRenderMarkdownWithDisagreements_EmptyMatchesPlainRender(t *testing.T) {
    findings := []reconcile.JSONFinding{
        {Severity: "HIGH", File: "a.go", Line: 1, Problem: "p",
            Reviewers: []string{"greta", "host"}, Confidence: "HIGH"},
    }
    df := reconcile.BuildDisagreements(findings, nil) // no tension → empty
    var withRadar, plain bytes.Buffer
    require.NoError(t, RenderMarkdownWithDisagreements(&withRadar, findings, df))
    require.NoError(t, Render(&plain, findings, FormatMarkdown))
    assert.Equal(t, plain.String(), withRadar.String(),
        "no disagreements → byte-identical to the plain markdown report")
}
```

This test checks that the report with an empty radar is byte-identical to the plain report. But this only tests the report package's implementation, not the reconcile package's implementation.

There's no equivalent test for the reconcile package's `RenderMarkdown`. The test `TestRenderMarkdown_NoDisagreementsOmitsRadar` only checks that the radar section is not present, not that the output is byte-identical to the pre-3.2 output.

So the inconsistency might not be caught by tests.

This is a MEDIUM severity issue.

Let me also look at the `internal/reconcile/emit.go` more carefully:

```go
{ReportMD, func(w io.Writer) error { return RenderMarkdown(w, r) }},
```

The `ReportMD` is written by calling `RenderMarkdown` in the reconcile package. This uses the reconcile package's `writeRadarSection`.

```go
{DisagreementsJSON, func(w io.Writer) error {
    return renderIndentedJSON(w, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
}},
```

The `DisagreementsJSON` is written by calling `BuildDisagreements` in the reconcile package. This is the same data that would be used by the report package's `writeRadarSection`.

So the persisted `report.md` and `disagreements.json` use the reconcile package's implementation, while the CLI report uses the report package's implementation. This is the source of the inconsistency.

Now, let me think about the fix:

The fix would be to have a single implementation of the radar section rendering. This could be done by:
1. Moving the radar section rendering to the reconcile package and having the report package call it
2. Moving the radar section rendering to a new shared package
3. Having the reconcile package's `RenderMarkdown` call the report package's `renderMarkdown`

Option 1 seems cleanest. The reconcile package would own the data model and the rendering for the persisted report.md, and the report package would call into the reconcile package for the radar section.

But this might create a circular dependency. The report package already imports the reconcile package. If the reconcile package's `RenderMarkdown` called the report package's `renderMarkdown`, that would be a circular dependency.

So option 2 or 3 is better. Option 2 is to move the radar section rendering to a shared location. Option 3 is to have the reconcile package's `RenderMarkdown` not include the radar section, and have the CLI add it separately.

Actually, looking at the code more carefully, the reconcile package's `RenderMarkdown` is called by `Emit` to write the persisted report.md. The report package's `renderMarkdown` is called by the CLI to write the CLI report. These are two different use cases.

The cleanest fix would be to have a single implementation of the radar section rendering. This could be done by:
1. Having the reconcile package's `RenderMarkdown` call a shared function
2. Having the report package's `renderMarkdown` call the same shared function

The shared function could be in the reconcile package (since the report package already imports it).

This is a refactoring issue, not a simple bug fix. It's a MEDIUM severity issue.

Now, let me think about the final list of issues to report:

1. **Inconsistency between persisted and CLI report.md radar sections** (MEDIUM, correctness)
   - The reconcile package's `writeRadarSection` uses `esc` for problem/detail text
   - The report package's `writeRadarItems` uses `escTrunc` for problem/detail text
   - This causes the persisted report.md and the CLI report.md to have different output for long problem texts

2. **Code duplication of `writeRadarSection` and `formatScore`** (LOW, maintainability)

3. **Solo finding condition `<= 1` vs `== 1`** (LOW, correctness)
   - The planning doc says "exactly one distinct reviewer"
   - The code uses `len(f.Reviewers) <= 1`, which also catches empty reviewer lists

4. **Header text missing verification disagreements** (LOW, maintainability)
   - The header says "reviewer splits, solo findings, and gray-zone clusters"
   - It should also mention "verification disagreements"

5. **`disagreements` flag ignores `--format`** (LOW, maintainability)
   - The `--disagreements` flag always renders the focused view, ignoring `--format`
   - This is documented in the comment but could be confusing

6. **`BuildDisagreements` called twice in `Emit`** (LOW, performance)
   - Once for the report.md and once for the disagreements.json
   - Minor inefficiency

Let me format these as TD_STREAM findings.

Actually, let me reconsider the solo finding condition. The planning doc says:

> Solo findings = merged findings with exactly one distinct reviewer (Confidence MEDIUM).

But the code uses `len(f.Reviewers) <= 1`. This means:
- If a finding has 0 reviewers, it's classified as a solo finding
- If a finding has 1 reviewer, it's classified as a solo finding
- If a finding has 2+ reviewers, it's not classified as a solo finding

The planning doc says "exactly one distinct reviewer", which would be `== 1`. The code uses `<= 1`, which is slightly different.

However, looking at the `jf` test helper:
```go
func jf(sev, file string, line int, problem string, reviewers []string, disagreement string) JSONFinding {
    conf := ConfMedium
    if len(reviewers) >= 2 {
        conf = ConfHigh
    }
    ...
}
```

The test helper sets confidence to MEDIUM for 0-1 reviewers and HIGH for 2+ reviewers. This matches the planning doc's "Confidence MEDIUM" for solo findings.

The `len(f.Reviewers) <= 1` condition is a defensive choice - it catches both empty and single reviewer lists. The planning doc says "exactly one distinct reviewer", which is `== 1`. This is a minor deviation.

This is a LOW severity issue.

Now, let me also look at the `soloItem` function:
```go
func soloItem(f JSONFinding) DisagreementItem {
    indep := atLeastOne(len(f.Reviewers))
    ...
}
```

The `atLeastOne(len(f.Reviewers))` ensures independence is at least 1, even if `f.Reviewers` is empty. This is correct.

Now, let me think about the final list of issues:

1. **Inconsistency between persisted and CLI report.md radar sections** (MEDIUM, correctness)
2. **Solo finding condition `<= 1` vs `== 1`** (LOW, correctness)
3. **Code duplication of `writeRadarSection` and `formatScore`** (LOW, maintainability)
4. **Header text missing verification disagreements** (LOW, maintainability)
5. **`disagreements` flag ignores `--format`** (LOW, maintainability)

Let me also check if there are any other issues I missed:

Looking at the `internal/reconcile/disagree.go` `BuildDisagreements` function:
```go
grayKeys := map[string]bool{}
for _, c := range clusters {
    for _, f := range c.Findings {
        grayKeys[findingKey(f.File, f.Line, f.Problem)] = true
    }
}
```

The `f` here is of type `stream.Finding`. The `findingKey` function takes `file string, line int, problem string`. Good.

But wait - the `stream.Finding` type has a `Reviewer` field (singular), not `Reviewers` (plural). The `JSONFinding` type has `Reviewers` (plural). So the cluster findings use `Reviewer` (singular) and the JSON findings use `Reviewers` (plural). This is consistent with the existing data model.

The `findingKey` function uses `f.File, f.Line, f.Problem` which are common fields. Good.

Now, let me look at the `allOutOfScope` function:
```go
func allOutOfScope(findings []stream.Finding) bool {
    if len(findings) == 0 {
        return false
    }
    for _, f := range findings {
        if strings.ToLower(strings.TrimSpace(f.Category)) != CategoryOutOfScope {
            return false
        }
    }
    return true
}
```

This checks if all findings in a group are out-of-scope. The `f.Category` is compared to `CategoryOutOfScope`. The `CategoryOutOfScope` constant is likely defined elsewhere. Assuming it exists, this is fine.

Now, let me look at the `grayZoneItem` function:
```go
func grayZoneItem(c AmbiguousCluster) DisagreementItem {
    maxRank, minRank := 0, 1<<31
    var maxSev string
    revSet := map[string]bool{}
    positions := make([]Position, 0, len(c.Findings))
    for _, f := range c.Findings {
        if r, ok := severityRank[f.Severity]; ok {
            if r > maxRank {
                maxRank, maxSev = r, f.Severity
            }
            if r < minRank {
                minRank = r
            }
        }
        if f.Reviewer != "" {
            revSet[f.Reviewer] = true
        }
        positions = append(positions, Position{Reviewer: f.Reviewer, Severity: f.Severity, Problem: f.Problem})
    }
    spread := 0
    if maxRank > 0 && minRank <= maxRank {
        spread = maxRank - minRank
    }
    if maxSev == "" && len(c.Findings) > 0 {
        maxSev = c.Findings[0].Severity
    }
    reviewers := sortedKeys(revSet)
    indep := atLeastOne(len(reviewers))
    return DisagreementItem{
        Kind:         KindGrayZone,
        File:         c.File,
        Line:         c.Line,
        Severity:     maxSev,
        Problem:      longestProblem(c.Findings),
        Score:        scoreFor(spread, indep, severityRank[maxSev]),
        Spread:       spread,
        Independence: indep,
        Reviewers:    reviewers,
        Detail:       "similarity " + strconv.FormatFloat(c.Similarity, 'f', 2, 64),
        Positions:    positions,
    }
}
```

This is correct. The function builds a `DisagreementItem` for a gray-zone cluster.

The `longestProblem` function returns the longest problem text. This is used as the representative problem for the cluster.

Now, let me also look at the `verificationItem` function:
```go
func verificationItem(f JSONFinding) DisagreementItem {
    spread := spreadFromDisagreement(f.Disagreement)
    indep := atLeastOne(len(f.Reviewers))
    return DisagreementItem{
        Kind:         KindVerificationDisagreement,
        File:         f.File,
        Line:         f.Line,
        Severity:     f.Severity,
        Problem:      f.Problem,
        Score:        scoreFor(spread, indep, severityRank[f.Severity]),
        Spread:       spread,
        Independence: indep,
        Reviewers:    f.Reviewers,
        Disagreement: f.Disagreement,
        Skeptics:     f.Verification.Skeptic,
        Detail:       f.Verification.Notes,
    }
}
```

This is correct. The function builds a `DisagreementItem` for a verification disagreement.

The `f.Verification.Skeptic` and `f.Verification.Notes` are used. If `f.Verification` is nil, this would panic. But the caller checks `isVerificationTie(f.Verification)` first, which returns false if `f.Verification` is nil. Good.

Now, let me think about the final list of issues:

1. **Inconsistency between persisted and CLI report.md radar sections** (MEDIUM, correctness)
2. **Solo finding condition `<= 1` vs `== 1`** (LOW, correctness)
3. **Code duplication of `writeRadarSection` and `formatScore`** (LOW, maintainability)
4. **Header text missing verification disagreements** (LOW, maintainability)
5. **`disagreements` flag ignores `--format`** (LOW, maintainability)

Let me format these as TD_STREAM findings.

Wait, I should also check the `internal/mcp/handlers.go` to see if it has the same inconsistency. Looking at the code:

```go
if format == report.FormatMarkdown {
    clusters, _ := reconcile.ReadAmbiguousClusters(dir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return nil, ReportResult{}, err
    }
}
```

The MCP handler calls `RenderMarkdownWithDisagreements` in the report package, which uses the report package's `writeRadarSection`. So the MCP report would have the same output as the CLI report, not the persisted report.md.

So the persisted report.md (via `Emit`) and the CLI/MCP report would have different output for long problem texts. This is the inconsistency.

Now, let me also check the `cmd/atcr/report.go` `disagreements` case:
```go
case disagreements:
    ...
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
```

The CLI `--disagreements` mode calls `RenderDisagreements` in the report package, which uses the report package's `writeRadarItems`. So the CLI `--disagreements` mode would have the same output as the CLI report, not the persisted report.md.

So there are three different paths:
1. Persisted report.md (via `Emit` → `RenderMarkdown` in reconcile package → `writeRadarSection` in reconcile package)
2. CLI report (via `atcr report` → `RenderMarkdownWithDisagreements` in report package → `renderMarkdown` in report package → `writeRadarSection` in report package)
3. CLI --disagreements (via `atcr report --disagreements` → `RenderDisagreements` in report package → `writeRadarItems` in report package)

Paths 2 and 3 use the report package's implementation. Path 1 uses the reconcile package's implementation. This is the inconsistency.

Now, let me think about the severity:

The inconsistency could cause:
1. Users see different output from the persisted report.md and the CLI report.md
2. The CLI report might truncate long problem texts, but the persisted report.md would not
3. This could be confusing for users who expect consistent output

This is a MEDIUM severity issue.

Now, let me format the findings:

1. **Inconsistency between persisted and CLI report.md radar sections**
   - File: `internal/reconcile/disagree.go` and `internal/report/disagree.go`
   - Problem: The two implementations of `writeRadarSection` produce different output for long problem texts (one uses `esc`, the other uses `escTrunc`)
   - Fix: Unify the implementations to use the same escaping/truncation logic
   - Category: correctness
   - Severity: MEDIUM

2. **Solo finding condition `<= 1` vs `== 1`**
   - File: `internal/reconcile/disagree.go` (line ~119)
   - Problem: The code uses `len(f.Reviewers) <= 1` but the planning doc says "exactly one distinct reviewer"
   - Fix: Change to `len(f.Reviewers) == 1` or document the deviation
   - Category: correctness
   - Severity: LOW

3. **Code duplication of `writeRadarSection` and `formatScore`**
   - File: `internal/reconcile/disagree.go` and `internal/report/disagree.go`
   - Problem: The same functions are defined in both packages
   - Fix: Move to a shared location or have one package call the other
   - Category: maintainability
   - Severity: LOW

4. **Header text missing verification disagreements**
   - File: `internal/reconcile/disagree.go` (line ~358) and `internal/report/disagree.go` (line ~41)
   - Problem: The header says "reviewer splits, solo findings, and gray-zone clusters" but doesn't mention "verification disagreements"
   - Fix: Update the header to include "verification disagreements"
   - Category: maintainability
   - Severity: LOW

5. **`disagreements` flag ignores `--format`**
   - File: `cmd/atcr/report.go` (line ~52)
   - Problem: The `--disagreements` flag always renders the focused view, ignoring `--format`
   - Fix: Document this behavior or add validation
   - Category: maintainability
   - Severity: LOW

Let me also think about whether there are any HIGH severity issues I've missed.

Looking at the `cmd/atcr/report.go` `disagreements` case:
```go
case disagreements:
    clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
    if err != nil {
        return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
    }
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
```

If `ReadAmbiguousClusters` fails, the error is returned. Good.

If `BuildDisagreements` panics, the program would crash. But `BuildDisagreements` is a pure function and shouldn't panic. Good.

If `RenderDisagreements` fails, the error is returned. Good.

No HIGH severity issues here.

Looking at the `internal/mcp/handlers.go`:
```go
if format == report.FormatMarkdown {
    clusters, _ := reconcile.ReadAmbiguousClusters(dir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return nil, ReportResult{}, err
    }
}
```

If `ReadAmbiguousClusters` fails, the error is silently ignored. This is intentional.

If `BuildDisagreements` panics, the program would crash. But `BuildDisagreements` is a pure function and shouldn't panic. Good.

If `RenderMarkdownWithDisagreements` fails, the error is returned. Good.

No HIGH severity issues here.

Looking at the `internal/reconcile/emit.go`:
```go
{DisagreementsJSON, func(w io.Writer) error {
    return renderIndentedJSON(w, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
}},
```

If `BuildDisagreements` panics, the program would crash. But `BuildDisagreements` is a pure function and shouldn't panic. Good.

If `renderIndentedJSON` fails, the error is returned. Good.

No HIGH severity issues here.

Looking at the `internal/reconcile/disagree.go` `BuildDisagreements` function:
```go
for _, f := range findings {
    if isRefutedJSON(f) {
        continue
    }
    if f.Category == CategoryOutOfScope {
        continue
    }
    if grayKeys[findingKey(f.File, f.Line, f.Problem)] {
        continue
    }
    switch {
    case isVerificationTie(f.Verification):
        items = append(items, verificationItem(f))
    case f.Disagreement != "":
        items = append(items, severitySplitItem(f))
    case len(f.Reviewers) <= 1:
        items = append(items, soloItem(f))
    }
}
```

The `verificationItem` function accesses `f.Verification.Skeptic` and `f.Verification.Notes`. If `f.Verification` is nil, this would panic. But the caller checks `isVerificationTie(f.Verification)` first, which returns false if `f.Verification` is nil. Good.

No HIGH severity issues here.

Now, let me finalize the list of issues:

1. **Inconsistency between persisted and CLI report.md radar sections** (MEDIUM, correctness)
2. **Solo finding condition `<= 1` vs `== 1`** (LOW, correctness)
3. **Code duplication of `writeRadarSection` and `formatScore`** (LOW, maintainability)
4. **Header text missing verification disagreements** (LOW, maintainability)
5. **`disagreements` flag ignores `--format`** (LOW, maintainability)

Let me format these as TD_STREAM findings.

Actually, I realize I should also check the `cmd/atcr/report.go` for the `format` variable usage in the `disagreements` case. Looking at the code:

```go
disagreements, _ := cmd.Flags().GetBool("disagreements")
// The disagreement radar is a focused, ranked view; it replaces the standard
// report rather than layering onto a chosen --format.
switch {
case disagreements:
    clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
    if err != nil {
        return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
    }
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderDisagreements(&buf, df); err != nil {
        return usageError(err)
    }
case format == report.FormatMarkdown:
    ...
default:
    ...
}
```

The `format` variable is not used in the `disagreements` case. This is intentional but could be confusing. The `disagreements` flag takes precedence over `--format`.

This is a LOW severity issue.

Now, let me also check the `internal/mcp/handlers.go` for any issues:

```go
if format == report.FormatMarkdown {
    clusters, _ := reconcile.ReadAmbiguousClusters(dir)
    df := reconcile.BuildDisagreements(findings, clusters)
    if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
        return nil, ReportResult{}, err
    }
} else if err := report.Render(&buf, findings, format); err != nil {
```

The MCP handler always includes the radar in the markdown report. This is consistent with the CLI behavior for the standard report.

No issues here.

Now, let me also check the `cmd/atcr/report_test.go` for any issues:

```go
func TestReportCmd_DisagreementsModeEmptyIsClean(t *testing.T) {
    isolate(t)
    consensus := `[{"severity":"HIGH","file":"a.go","line":1,"problem":"p","reviewers":["greta","host"],"confidence":"HIGH"}]`
    fixtureReconciled(t, "2026-06-10_c", consensus)
    require.Equal(t, 0, execCmd(t, "report", "--disagreements", "2026-06-10_c"))
}
```

This test only checks the exit code. It should also check the output content (e.g., "No disagreements detected."). This is a weak test.

```go
func TestReportCmd_DisagreementsModeReadsAmbiguousSidecar(t *testing.T) {
    isolate(t)
    ambiguous := `[{"id":"amb-1","file":"g.go","line":7,"similarity":0.55,...}]`
    fixtureReconciledWithAmbiguous(t, "2026-06-10_g", oneFinding, ambiguous)
    require.Equal(t, 0, execCmd(t, "report", "--disagreements", "2026-06-10_g"))
}
```

This test only checks the exit code. It should also check the output content (e.g., the gray-zone cluster is surfaced). This is a weak test.

These are LOW severity issues.

Now, let me also check the `internal/reconcile/emit.go` for any issues:

```go
{DisagreementsJSON, func(w io.Writer) error {
    return renderIndentedJSON(w, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
}},
```

The `BuildDisagreements` is called here. The `r.JSONFindings()` method is called. If `r.JSONFindings()` is expensive, this could be inefficient. But this is a minor concern.

The `BuildDisagreements` is also called in `RenderMarkdown`:
```go
writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
```

So `BuildDisagreements` is called twice per `Emit` invocation. This is a minor inefficiency.

This is a LOW severity issue.

Now, let me finalize the list of issues:

1. **Inconsistency between persisted and CLI report.md radar sections** (MEDIUM, correctness)
2. **Solo finding condition `<= 1` vs `== 1`** (LOW, correctness)
3. **Code duplication of `writeRadarSection` and `formatScore`** (LOW, maintainability)
4. **Header text missing verification disagreements** (LOW, maintainability)
5. **`disagreements` flag ignores `--format`** (LOW, maintainability)
6. **`BuildDisagreements` called twice in `Emit`** (LOW, performance)
7. **Weak test coverage in `cmd/atcr/report_test.go`** (LOW, maintainability)

Let me format these as TD_STREAM findings.

Actually, I want to double-check the inconsistency issue. Let me re-read the code:

In `internal/reconcile/emit.go`:
```go
func RenderMarkdown(w io.Writer, r Result) error {
    var b bytes.Buffer
    ...
    writeSeverityConfidenceTable(&b, inScope)

    // Disagreement radar above the consensus findings (Epic 3.2). Nothing is
    // written when there is no tension, so report.md is byte-identical to the
    // pre-3.2 output for a review with no disagreements.
    writeRadarSection(&b, BuildDisagreements(r.JSONFindings(), r.Ambiguous))
    ...
}
```

The `writeRadarSection` here is the one in the reconcile package (same file). It uses `esc` for problem/detail text.

In `internal/report/render.go`:
```go
func renderMarkdown(w io.Writer, findings []reconcile.JSONFinding, df reconcile.DisagreementsFile) error {
    var b bytes.Buffer
    b.WriteString("# atcr Review Report\n\n")
    verified := anyVerification(findings)
    writeSummaryGrid(&b, findings, verified)

    // Disagreement radar above the consensus findings (Epic 3.2). Empty df →
    // nothing written → output identical to the plain report.
    writeRadarSection(&b, df)
    ...
}
```

The `writeRadarSection` here is the one in the report package. It uses `writeRadarItems` which uses `escTrunc` for problem/detail text.

So yes, the two implementations produce different output for long problem texts. This is a real inconsistency.

Let me also check the `internal/report/disagree.go` `writeRadarItems` function:
```go
func writeRadarItems(b *bytes.Buffer, items []reconcile.DisagreementItem, heading string) {
    for i, it := range items {
        fmt.Fprintf(b, "\n%s%d. %s — %s (%s) · score %s\n",
            heading, i+1, esc(it.Kind), codeSpan(it.File, it.Line), esc(it.Severity), formatScore(it.Score))
        if it.Disagreement != "" {
            fmt.Fprintf(b, "- Severity disagreement: %s\n", esc(it.Disagreement))
        }
        if it.Skeptics != "" {
            fmt.Fprintf(b, "- Skeptics split: %s\n", esc(it.Skeptics))
        }
        if len(it.Reviewers) > 0 {
            fmt.Fprintf(b, "- Reviewers: %s (independence %d)\n", esc(joinReviewers(it.Reviewers)), it.Independence)
        }
        if it.Problem != "" {
            fmt.Fprintf(b, "- Problem: %s\n", escTrunc(it.Problem))
        }
        if it.Detail != "" {
            fmt.Fprintf(b, "- Detail: %s\n", escTrunc(it.Detail))
        }
        if len(it.Positions) > 0 {
            b.WriteString("- Positions:\n")
            for _, p := range it.Positions {
                fmt.Fprintf(b, "  - %s — %s: %s\n", esc(reviewerOrUnknown(p.Reviewer)), esc(p.Severity), escTrunc(p.Problem))
            }
        }
    }
}
```

Yes, this uses `escTrunc` for problem, detail, and position problem. The reconcile package's `writeRadarSection` uses `esc` (no truncation) for the same fields.

This is a real inconsistency.

Now, let me format the findings:

1. **Inconsistency between persisted and CLI report.md radar sections**
   - File: `internal/reconcile/disagree.go` (line ~358) and `internal/report/disagree.go` (line ~41)
   - Problem: The reconcile package's `writeRadarSection` uses `esc` for problem/detail text, while the report package's `writeRadarItems` uses `escTrunc`. This causes the persisted report.md and the CLI report.md to have different output for long problem texts.
   - Fix: Unify the implementations to use the same escaping/truncation logic
   - Category: correctness
   - Severity: MEDIUM
   - Evidence: reconcile/disagree.go uses `esc(it.Problem)` while report/disagree.go uses `escTrunc(it.Problem)`

2. **Solo finding condition `<= 1` vs `== 1`**
   - File: `internal/reconcile/disagree.go` (line ~119)
   - Problem: The code uses `len(f.Reviewers) <= 1` but the planning doc says "exactly one distinct reviewer"
   - Fix: Change to `len(f.Reviewers) == 1` or document the deviation
   - Category: correctness
   - Severity: LOW
   - Evidence: `case len(f.Reviewers) <= 1:` in BuildDisagreements

3. **Code duplication of `writeRadarSection` and `formatScore`**
   - File: `internal/reconcile/disagree.go` and `internal/report/disagree.go`
   - Problem: The same functions are defined in both packages
   - Fix: Move to a shared location or have one package call the other
   - Category: maintainability
   - Severity: LOW
   - Evidence: `writeRadarSection` and `formatScore` are defined in both packages

4. **Header text missing verification disagreements**
   - File: `internal/reconcile/disagree.go` (line ~358) and `internal/report/disagree.go` (line ~41)
   - Problem: The header says "reviewer splits, solo findings, and gray-zone clusters" but doesn't mention "verification disagreements"
   - Fix: Update the header to include "verification disagreements"
   - Category: maintainability
   - Severity: LOW
   - Evidence: `fmt.Fprintf(b, "\n## Disagreements\n\nTop %d tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.\n", len(df.Items))`

5. **`disagreements` flag ignores `--format`**
   - File: `cmd/atcr/report.go` (line ~52)
   - Problem: The `--disagreements` flag always renders the focused view, ignoring `--format`
   - Fix: Document this behavior or add validation
   - Category: maintainability
   - Severity: LOW
   - Evidence: `case disagreements:` doesn't use the `format` variable

6. **`BuildDisagreements` called twice in `Emit`**
   - File: `internal/reconcile/emit.go` (line ~113 and ~261)
   - Problem: `BuildDisagreements` is called once for the report.md and once for the disagreements.json
   - Fix: Compute once and pass to both
   - Category: performance
   - Severity: LOW
   - Evidence: `BuildDisagreements(r.JSONFindings(), r.Ambiguous)` is called in both `RenderMarkdown` and the `DisagreementsJSON` writer

7. **Weak test coverage in `cmd/atcr/report_test.go`**
   - File: `cmd/atcr/report_test.go` (line ~57 and ~64)
   - Problem: Some tests only check exit codes, not output content
   - Fix: Add assertions for output content
   - Category: maintainability
   - Severity: LOW
   - Evidence: `TestReportCmd_Disagre
</think>
