<think>
Let me carefully review this diff against the sprint plan scope. The sprint plan is about Epic 12.2: Sprint Plan Scoping in ATCR. I need to focus on:

1. CLI flag (`--sprint-plan`)
2. `internal/payload/sprintplan.go` reader/formatter
3. SCOPE CONSTRAINT block construction
4. 16 KiB size-cap
5. Engine wiring through `PrepareReview`/`PrepareReviewFromDiff` → `buildSlots` → `buildAgent`
6. Unit tests

Files in scope:
- README.md (flag documentation)
- cmd/atcr/review.go (flag wiring)
- cmd/atcr/review_test.go (flag tests)
- cmd/atcr/flags.go (mentioned in sprint plan)
- internal/fanout/resume.go (buildSlots call update)
- internal/fanout/retry_wiring_test.go (buildAgent signature update)
- internal/fanout/review.go (core wiring)
- internal/fanout/review_scope_inject_test.go (buildAgent tests)
- internal/fanout/review_sprintplan_test.go (new tests)
- internal/fanout/review_test.go (buildAgent signature update)
- internal/fanout/review_tools_test.go (buildAgent signature update)
- internal/payload/sprintplan.go (new)
- internal/payload/sprintplan_test.go (new)

Let me look at the actual diff carefully for correctness issues:

1. **`resolveScopeConstraint` in review.go** — there's a subtle issue. The `readSprintPlan` semantics from the clarifications say:
   - Missing → "", no error
   - Unreadable → error (caller warns on stderr, review proceeds)
   - Empty or whitespace-only → "", no error
   - Valid → formatted SCOPE CONSTRAINT block

   But `ReadSprintPlan` returns the raw content (not the formatted block). The empty/whitespace check happens in `ScopeConstraint`. Let me trace:
   - `ReadSprintPlan("")` → `("", nil)` — correct
   - `ReadSprintPlan(missing)` → `("", nil)` — correct (missing)
   - `ReadSprintPlan(emptyFile)` → `("", nil)` — correct (empty file)
   - `ReadSprintPlan(whitespaceOnly)` → `("   \n", nil)` — returns whitespace content
   - `ScopeConstraint("   \n")` → `("", false)` — trims to empty, returns no block

   So whitespace-only handling works but is split across two functions. That's fine.

2. **`resolveScopeConstraint` doesn't handle the whitespace-only case as "missing"** — It will call `ScopeConstraint` with whitespace, which returns `("", false)`. So the constraint is empty, no warning. This is fine.

3. **A concern: `resolveScopeConstraint` returns a warning for truncation but the warning doesn't mention the truncation is at a UTF-8 boundary** — not a bug, just a note.

4. **Let me check: `ReadSprintPlan` returns an error for "is a directory" case** — Yes, `os.ReadFile` of a directory will return an error on most systems. This is correct.

5. **Permission-denied case** — `os.ReadFile` returns the error, `os.IsNotExist(err)` is false, so the error propagates. Good.

6. **The race condition with `os.IsNotExist`** — If the file exists but is not readable due to permissions, `os.IsNotExist(err)` is false. Good.

7. **Check: in `resolveScopeConstraint`, what if `req.SprintPlanPath` is whitespace-only?** 
   - `ReadSprintPlan` checks `strings.TrimSpace(path) == ""` → returns ("", nil)
   - Then `ScopeConstraint("")` → ("", false)
   - Result: ("", "")
   - This is correct.

8. **Check: the warning format** — `fmt.Sprintf("sprint plan %q is unreadable; proceeding with a diff-wide review: %v", req.SprintPlanPath, err)` — uses `%q` which quotes the path. Looks fine.

9. **Check: in `review.go` line ~240**, `resolveScopeConstraint` is called in `PrepareReview` and `PrepareReviewFromDiff`. The warning is printed to stderr. But `PrepareReview` and `PrepareReviewFromDiff` are library functions — adding stderr output to library functions is a side effect that might be unexpected. However, the sprint plan AC3 requires the warning to stderr, so this is by design.

10. **Potential issue: `resolveScopeConstraint` in `PrepareReview` at review.go:240** — it's called but `err` from `ReadSprintPlan` is the error, then the function returns the warning. What about the `scopeConstraint` being `""` when there's an error? Let me check:
    ```go
    raw, err := payload.ReadSprintPlan(req.SprintPlanPath)
    if err != nil {
        return "", fmt.Sprintf("sprint plan %q is unreadable; proceeding with a diff-wide review: %v", req.SprintPlanPath, err)
    }
    block, truncated := payload.ScopeConstraint(raw)
    if truncated {
        warning = fmt.Sprintf("sprint plan %q exceeded %d bytes and was truncated before injection", req.SprintPlanPath, payload.MaxSprintPlanBytes)
    }
    return block, warning
    ```
    When err is non-nil, returns `("", warning)` — correct, no constraint.
    When err is nil and raw is whitespace-only or empty, `ScopeConstraint` returns `("", false)`. So `block = ""`, `truncated = false`, warning = "" — correct, silent.

11. **Potential issue: The warning for truncation doesn't include the byte counts in a way that's machine-parseable** — not a correctness issue.

12. **Potential issue: `MaxSprintPlanBytes` is `int64` but the type used in `ScopeConstraint` is `int`**:
    ```go
    plan, truncated = capUTF8(plan, int(MaxSprintPlanBytes))
    ```
    `capUTF8` takes `int`, so this is a conversion. On 32-bit systems, `int` could be 32-bit (4 bytes), so converting 16384 to int is fine. No bug.

13. **Potential issue: `capUTF8` returns `(s[:cut], true)` but `cut` might be 0** — if `cut` reaches 0, the loop exits and returns `("", true)`. This is fine.

14. **Let me check the `ScopeConstraint` function for the truncation case** — It says "block" includes "SENTINEL SCOPE CONSTRAINT" in test, but the actual function uses "SCOPE CONSTRAINT". The test in `review_scope_inject_test.go` uses:
    ```go
    const constraint = "## SENTINEL SCOPE CONSTRAINT\nplan body\n\n"
    ```
    This is a custom string passed to `buildAgent`, not the result of `ScopeConstraint`. So this is fine.

15. **Important: `cmd/atcr/flags.go` is mentioned in the sprint plan but doesn't appear in the diff** — The sprint plan says "Map the `--sprint-plan` flag in `cmd/atcr/review.go` and `cmd/atcr/flags.go`". But the diff only modifies `cmd/atcr/review.go`. Let me check if `cmd/atcr/flags.go` exists or has related changes. Looking at the diff, `flags.go` is not modified. The flag is registered directly in `newReviewCmd()` in `review.go`. This could be a scope concern but might be intentional if `flags.go` is for shared flags. The sprint plan mentions it, so this might be OUT-OF-SCOPE or it's just that the plan mentioned it but the implementation went a different way (registered directly in the command).

16. **Let me check `cmd/atcr/flags.go` to see what it contains** — It's not in the diff, so I can't tell. But the sprint plan explicitly says "Register `--sprint-plan` flag in `cmd/atcr/flags.go`". This is a discrepancy. However, since `flags.go` isn't in the diff, I can't comment on it. I should note this as out-of-scope concern.

17. **Let me look at the test `TestResolveScopeConstraint`** — It creates a plan with `bytes.Repeat([]byte("x"), int(payload.MaxSprintPlanBytes)+1000)`. `MaxSprintPlanBytes` is `int64`, and `int()` conversion could overflow on 32-bit systems if `MaxSprintPlanBytes > MaxInt32`, but 16384 is fine.

18. **Edge case: `os.WriteFile(big, bytes.Repeat([]byte("x"), int(payload.MaxSprintPlanBytes)+1000), 0o644)`** — This creates a file of ~17 KiB. The truncation logic should kick in. Let me trace: `ScopeConstraint(17384 bytes of "x")` → `capUTF8(plan, 16384)` → `len(s) > max` so cuts at 16384. Since "x" is ASCII (1 byte each), `cut = 16384` is a rune boundary. Returns `(16384 x's, true)`. Then wraps it. Good.

19. **Issue: In `TestScopeConstraint_TruncatesOnRuneBoundary`**:
    ```go
    runes := strings.Repeat("世", int(MaxSprintPlanBytes)/3+100)
    ```
    "世" is 3 bytes in UTF-8. `16384/3 = 5461` runes = 16383 bytes + 100 runes = 5461 runes × 3 + 100 × 3 = 16383 + 300 = 16683 bytes total. Let me re-check:
    - `strings.Repeat("世", int(MaxSprintPlanBytes)/3+100)` = 5461 + 100 = 5561 runes
    - 5561 × 3 = 16683 bytes
    - This exceeds `MaxSprintPlanBytes` (16384)
    - `capUTF8` will truncate at 16384 bytes
    - Since rune boundaries are every 3 bytes, 16384 = 5461 * 3 + 1, so `cut = 16384` is NOT a rune start (16384 % 3 = 1)
    - The loop backs off to 16383 (16383 % 3 = 0, rune start) — wait, actually `s[cut]` being checked as rune start. `s[16384]` is the byte at position 16384, which would be the 2nd byte of a rune. The loop backs off to find a rune start. 16383 is the first byte of a rune (since 16383 = 5461 * 3). So `cut = 16383`, returns `s[:16383]`.
    - The result is 16383 bytes of "世" runes = 5461 complete runes. Valid UTF-8. Good.

20. **Issue: The comment in `ScopeConstraint` says "block is a SOFT scope"** — It does include a critical-issue escape hatch. Good.

21. **Potential issue with `resolveScopeConstraint`'s truncation warning not mentioning UTF-8 boundary** — Not really a bug.

22. **`PrepareReview` and `PrepareReviewFromDiff` both call `resolveScopeConstraint` and write to stderr** — This is a duplicate code path. Could be DRY'd up but not a bug. Both call sites use the same pattern:
    ```go
    scopeConstraint, scopeWarn := resolveScopeConstraint(req)
    if scopeWarn != "" {
        fmt.Fprintln(os.Stderr, "warn: "+scopeWarn)
    }
    ```
    Acceptable.

23. **Issue: in `PrepareResume` (resume.go), `buildSlots` is called with `""` for the scope constraint** — Resume doesn't read the sprint plan. Is this intentional? The sprint plan says: "Read the sprint plan once during `PrepareReview` / `PrepareReviewFromDiff`" — Resume is a separate path. If a user resumes a review with `--sprint-plan`, the constraint would be lost. But this is a design choice. Looking at the diff for resume.go:
    ```go
    slots, _, err := buildSlots(cfg, payloads, req.Range, "", "")
    ```
    The `req` is passed to `PrepareResume`. The `req.SprintPlanPath` is in the struct. But the resume path doesn't use it. This could be a HIGH issue — sprint plan scoping is silently dropped during resume. But the sprint plan doesn't explicitly say resume needs to support it. Looking at the implementation components:
    - "Read the sprint plan once during `PrepareReview` / `PrepareReviewFromDiff`" — Resume is not mentioned.
    - The sprint plan doesn't list resume.go as in scope.
    
    However, this could be a real correctness issue: a user who starts a review with `--sprint-plan` and then resumes will lose the constraint. But this is borderline out of scope since resume.go is not in the listed in-scope files... wait, resume.go IS in the diff (modified). Let me re-check the scope:
    
    "IN scope: `--sprint-plan` CLI flag, `internal/payload/sprintplan.go` reader/formatter, SCOPE CONSTRAINT block construction, 16 KiB size-cap, engine wiring through `PrepareReview`/`PrepareReviewFromDiff` → `buildSlots` → `buildAgent`, unit tests."
    
    Resume is not explicitly in or out. The diff modifies resume.go to update the `buildSlots` signature. This suggests the implementer recognized that resume must be updated for the signature change but didn't pass the constraint through. This is a potential bug.

    Wait, let me re-check. The resume path doesn't load the plan and doesn't pass it. So during a resume, the constraint is silently lost. This could lead to:
    - Inconsistent review: first run had constraint, resume doesn't.
    - Cache key mismatch: cache was computed with constraint, resume produces prompts without constraint → cache miss but at least correct.
    
    Actually, this is a genuine correctness issue. Let me flag it as MEDIUM.

24. **Let me look at the `flags.go` mention again** — The sprint plan says "Register `--sprint-plan` flag in `cmd/atcr/flags.go`" and "Map the `--sprint-plan` flag in `cmd/atcr/review.go` and `cmd/atcr/flags.go`". The actual diff only modifies `cmd/atcr/review.go`. If `cmd/atcr/flags.go` exists and has a pattern for registering flags, the flag should be there. But since `flags.go` is not in the diff, and the diff is what's being reviewed, this might be out of scope of the review (the file isn't being changed in this diff).

25. **Another issue: in `review.go`, `resolveScopeConstraint` is a private function** — That's fine, it's an implementation detail.

26. **Issue: `resolveScopeConstraint` takes `ReviewRequest` by value** — This is fine since `ReviewRequest` is a small struct.

27. **Edge case: what if `req.SprintPlanPath` contains only whitespace?** — `ReadSprintPlan` checks `strings.TrimSpace(path) == ""` and returns `("", nil)`. Good.

28. **Looking at `TestResolveScopeConstraint` more carefully** — It tests with `bytes.Repeat([]byte("x"), int(payload.MaxSprintPlanBytes)+1000)`. `payload.MaxSprintPlanBytes` is `int64` = 16384. `int(16384) + 1000 = 17384` bytes. The file is 17384 bytes. Then `ScopeConstraint` caps at 16384. Good.

29. **Issue: in `resolveScopeConstraint`, the warning for truncation doesn't distinguish between a plan that was exactly at the cap and one that was over** — Not a bug.

30. **Issue: what if `req.SprintPlanPath` is set but the file is empty?**
    - `ReadSprintPlan("path/to/empty.md")` → file exists, empty → returns `("", nil)`
    - `ScopeConstraint("")` → `("", false)`
    - Result: `("", "")`
    - Good, silent (AC2).

31. **Issue: `resolveScopeConstraint` returns the warning to the caller for printing. What if the caller doesn't print it?** — The caller (PrepareReview, PrepareReviewFromDiff) does print. Good.

32. **Issue: there's no logger abstraction** — The function uses `fmt.Fprintln(os.Stderr, ...)` directly. This is the established pattern in the codebase (per the sprint plan, "consistent with the AC3 stderr pattern").

33. **Test issue: `TestPrepareReviewFromDiff_InjectsSprintPlanConstraint` checks for `"func total"` in the prompt, expecting it to appear after the constraint**. Let me look at this:
    ```go
    di := strings.Index(p, "func total") // appears in looseDiff
    ```
    This relies on `looseDiff` containing "func total". If looseDiff doesn't have this, the test fails. But since this is a test that the author presumably wrote correctly with their test data, this is fine.

34. **Potential bug: the `twoAgentConfig` and `diffReq` helpers** — I don't have visibility into these but they're presumably set up correctly.

35. **Issue: `sprintPlanPath` function** — Let me check:
    ```go
    func sprintPlanPath(cmd *cobra.Command) string {
        v, _ := cmd.Flags().GetString("sprint-plan")
        return strings.TrimSpace(v)
    }
    ```
    Ignores the error from `GetString`. The test `TestSprintPlanPath_MapsFlag` only tests that the value is read. The error from `GetString` would only happen if the flag doesn't exist (returns ""), but we've verified the flag exists via the `Lookup` check in `TestReviewCmd_SprintPlanFlagRegistered`. Good.

36. **Looking at `TestSprintPlanPath_MapsFlag`**:
    ```go
    require.NoError(t, cmd.ParseFlags([]string{"--sprint-plan", "  plans/sprint.md  "}))
    require.Equal(t, "plans/sprint.md", sprintPlanPath(cmd), "value is read and trimmed")
    ```
    This tests trimming. Good.

37. **Issue: `sprintPlanPath` returns empty when flag is unset, but doesn't validate the path exists** — This is intentional per the sprint plan (handling is in the engine).

38. **Bug check: The `resolveScopeConstraint` is called inside both `PrepareReview` and `PrepareReviewFromDiff`. But what if `req.SprintPlanPath` is set but the user doesn't have the file?** The engine handles it silently (AC2). Good.

39. **Looking at `prepareReview` for handling warning if it's nil warning** — The check `if scopeWarn != ""` handles nil (nil string is ""); both nil and "" are handled.

40. **Race condition: The sprint plan file is read once per `PrepareReview` call. If the file changes between PrepareReview and the actual agent invocation, no problem since it's already in the rendered prompt.**

41. **Cache key concern**: The constraint is part of the rendered prompt, so the cache key (which hashes the rendered prompt) changes when the plan changes. Good (AC5).

42. **Performance: reading the plan file must be < 5ms** — Not testable in this diff, but it's a file read which is fast.

43. **Issue: in `review.go` line 240ish**, the function `resolveScopeConstraint` is called every time `PrepareReview` is called. If called multiple times (e.g., retry), the file is re-read. Could cache the result, but the sprint plan says "Read the sprint plan once during `PrepareReview` / `PrepareReviewFromDiff`" — so once per prepare call is fine.

44. **Test concern: `TestScopeConstraint_TruncatesOnRuneBoundary`** — Let me check the rune math:
    - `MaxSprintPlanBytes = 16384`
    - `16384 / 3 = 5461` (integer division)
    - `5461 + 100 = 5561` runes
    - `5561 * 3 = 16683` bytes
    - Truncation at 16384 bytes, backs off to 16383 (a rune start since 16383 = 5461 * 3)
    - Result: 5461 complete "世" runes = 16383 bytes, valid UTF-8
    - Good.

45. **Looking for more issues**: Let me look at the sprint plan vs. implementation once more.
    
    - AC1: CLI flag exists ✓
    - AC2: Missing/empty file is silent ✓ (covered by tests)
    - AC3: Unreadable file warns on stderr ✓ (covered by tests)
    - AC4: Valid file injects constraint before diff ✓ (covered by tests)
    - AC5: Cache key recalculates when plan changes — implicit (constraint is in rendered prompt)
    - AC6: Oversized plan capped at 16 KiB ✓ (covered by tests)
    
    Wait, let me re-check AC3: "An unreadable sprint plan file (e.g. permission error) prints a warning to stderr but allows the review to proceed." The current implementation does print to stderr. But what about the `resolveScopeConstraint` function — it returns a warning, and the caller prints it. What if there are multiple prepare paths that should print but don't? Let me check `PrepareResume`:
    
    ```go
    slots, _, err := buildSlots(cfg, payloads, req.Range, "", "")
    ```
    
    Resume doesn't call `resolveScopeConstraint`! So during a resume with an unreadable plan, there's no warning. But wait, during resume, the user already saw the warning from the initial run. The new `buildSlots` call doesn't have a constraint. This could lead to:
    - First run with `--sprint-plan plans/foo.md`: Plan is unreadable, warning printed, review proceeds without constraint.
    - User resumes the review with the same `--sprint-plan plans/foo.md`: No warning, no constraint (the constraint was empty in the first run too).
    
    This is consistent. But what if:
    - First run with `--sprint-plan plans/foo.md`: Plan is readable, constraint injected.
    - User resumes with the same `--sprint-plan plans/foo.md`: During resume, `buildSlots` is called with empty constraint. So the resumed agents run WITHOUT the constraint.
    
    This is a real bug! Resume drops the sprint plan constraint. The cached prompts from the first run had the constraint, so resume would re-render without it, creating prompts that don't match the cache.

    Actually, wait. Let me look at this more carefully. Resume is for completing pending agents after an interruption. If the first run completed some agents, those are cached. The remaining agents would be re-rendered during resume. If the rendering during resume doesn't include the constraint but the original (cached) prompts did, then:
    - First run: Agent A prompt rendered with constraint → sent to model, result cached.
    - Resume: Agent B prompt rendered WITHOUT constraint → sent to model.
    
    This is inconsistent. The resumed agents run unconstrained while the originally-completed agents ran constrained. This is a real correctness issue.

    Actually, the bigger issue is: during a resume, if the user passed `--sprint-plan`, should the plan be re-read and re-applied? The sprint plan doesn't explicitly address this. But the implementation drops the constraint entirely during resume. This is a bug.

46. **Severity**: This is a MEDIUM issue — it doesn't cause a crash, but it does silently change behavior between the first run and resume.

47. **Let me look at one more thing**: the `flags.go` file. The sprint plan mentions it but it's not in the diff. Should I flag this?

The sprint plan says:
- "Register `--sprint-plan` flag in `cmd/atcr/flags.go`."
- "Map the `--sprint-plan` flag in `cmd/atcr/review.go` and `cmd/atcr/flags.go` to `ReviewRequest.SprintPlanPath`."

The actual diff registers the flag in `cmd/atcr/review.go` directly. If `cmd/atcr/flags.go` exists and has shared flag registration, this is a deviation from the plan. But since the diff doesn't touch `flags.go`, I can't comment on it. This is more of an "is the plan being followed" concern, not a code correctness issue. I'll skip this.

48. **Another issue to check**: Is the `resolveScopeConstraint` function correctly handling the case where `req.SprintPlanPath` is set but `ReadSprintPlan` returns an error? Let me trace:
    
    - `req.SprintPlanPath = "/bad/path"`
    - `ReadSprintPlan("/bad/path")` → if the file doesn't exist, returns `("", nil)` (silent)
    - If the path exists but is unreadable (permission), returns `("", err)`
    - `resolveScopeConstraint` checks err != nil → returns `("", warning)`
    - Caller prints warning.
    
    Good.

49. **What if the file is a symlink to a non-existent target?** `os.ReadFile` follows symlinks. If the target doesn't exist, `os.IsNotExist(err)` returns true, so it's treated as "no plan". Good.

50. **What if the path is `~` or `~/plan.md`?** The function doesn't expand `~`. The CLI passes the path as-is. The sprint plan says "The path is passed through verbatim (relative paths resolve against CWD when the engine reads the file)". So this is intentional. The shell would expand `~` if the user typed `--sprint-plan ~/plan.md` (without quotes). OK.

51. **Looking at `TestScopeConstraint`** more carefully:
    ```go
    huge := strings.Repeat("x", int(MaxSprintPlanBytes)+5000)
    capped, truncated := ScopeConstraint(huge)
    if !truncated {
        t.Fatalf("ScopeConstraint(oversized) truncated=false, want true")
    }
    if n := int64(len(embeddedPlan(t, capped))); n > MaxSprintPlanBytes {
        t.Fatalf("ScopeConstraint embedded %d plan bytes, want <= %d", n, MaxSprintPlanBytes)
    }
    ```
    
    Wait, `embeddedPlan` returns the plan content between BEGIN/END markers. Let me check the marker: `"\n----- END SPRINT PLAN -----"`. The wrapped content in `ScopeConstraint` is:
    ```
    plan
    \n----- END SPRINT PLAN -----
    ```
    
    So after the BEGIN marker is `plan\n`, then `----- END SPRINT PLAN -----`. The `embeddedPlan` function looks for `----- END SPRINT PLAN -----` and returns everything before it. The returned content is `plan` (without trailing newline because the END marker consumes `\n----- END SPRINT PLAN -----`).
    
    Wait, let me re-read:
    ```go
    const begin = "----- BEGIN SPRINT PLAN -----\n"
    const end = "\n----- END SPRINT PLAN -----"
    ```
    
    `ScopeConstraint` writes:
    ```
    ----- BEGIN SPRINT PLAN -----\n
    plan
    \n----- END SPRINT PLAN -----\n\n
    ```
    
    So after the BEGIN marker `\n`, the content is `plan\n----- END SPRINT PLAN -----\n\n`.
    
    The `embeddedPlan` function finds `begin` at position i, takes everything after it: `plan\n----- END SPRINT PLAN -----\n\n`. Then finds `end` (`\n----- END SPRINT PLAN -----`) in that: it would find it at position `len(plan)+1`. So returns `plan` (the content between BEGIN and END markers, not including the surrounding newlines).
    
    Wait, but if `plan` ends with `\n` (e.g., when truncated), then `plan + \n + ----- END SPRINT PLAN ----- + \n\n`. The `end` is `\n----- END SPRINT PLAN -----`. So `rest = plan\n----- END SPRINT PLAN -----\n\n`. `j` finds `\n----- END SPRINT PLAN -----` at position `len(plan)`. Returns `rest[:len(plan)]` = `plan`. Good.
    
    But `ScopeConstraint` adds a trailing `\n` to the plan:
    ```go
    b.WriteString(plan)
    b.WriteString("\n----- END SPRINT PLAN -----\n\n")
    ```
    
    So the content between BEGIN and END markers is `plan\n`. Wait no — let me re-trace:
    - `b.WriteString("----- BEGIN SPRINT PLAN -----\n")` — writes up to and including the newline.
    - `b.WriteString(plan)` — writes the plan content (which may or may not end with newline).
    - `b.WriteString("\n----- END SPRINT PLAN -----\n\n")` — writes `\n----- END SPRINT PLAN -----\n\n`.
    
    After the BEGIN marker, the content is `plan\n----- END SPRINT PLAN -----\n\n`.
    
    The `embeddedPlan` looks for `\n----- END SPRINT PLAN -----` in that. It finds it at position `len(plan)`. Returns `rest[:len(plan)]` = `plan`.
    
    So the embedded plan size is `len(plan)` exactly, regardless of whether the plan ends with a newline. Good.
    
    For an oversized plan of `16384 + 5000 = 21384` "x"s:
    - `capUTF8` truncates to 16384 bytes (all ASCII, no rune boundary issues).
    - `plan` is now 16384 "x"s.
    - The block has 16384 "x"s embedded.
    - `len(embeddedPlan) = 16384 = MaxSprintPlanBytes`.
    - Test checks `n > MaxSprintPlanBytes` → false → test passes.
    
    Wait, actually `len(embeddedPlan) = 16384` and `MaxSprintPlanBytes = 16384`. So `n > MaxSprintPlanBytes` is `16384 > 16384` = false. Test passes. Good.

52. **Looking at the `len(embeddedPlan(t, capped)) > MaxSprintPlanBytes`** check — `MaxSprintPlanBytes` is int64. `len()` returns int. `int64(int)` is fine. The comparison `int64 > int64` works. Good.

53. **One more issue to check: `cmd/atcr/flags.go`** — The diff doesn't include any change to `flags.go`. The sprint plan says flags should be registered in `flags.go`. Since `flags.go` isn't in the diff, I won't flag this — it's out of scope of this review.

54. **Hmm, let me check if there's a `cmd/atcr/flags.go` at all by examining the diff for references** — The diff doesn't reference `flags.go`. So either it doesn't exist or wasn't changed. I'll skip this concern.

55. **Wait — let me check the `resolveScopeConstraint` function again. There's a subtle issue**: It only handles `err != nil` from `ReadSprintPlan`. But what about the case where `req.SprintPlanPath` is whitespace-only?
    
    - `req.SprintPlanPath = "   "`
    - `ReadSprintPlan("   ")`: `strings.TrimSpace("   ") == ""` → returns `("", nil)`
    - `err == nil`, so we proceed.
    - `ScopeConstraint("")` → `("", false)`
    - Returns `("", "")`. Correct.

56. **Let me look at the `flags.go` file mention one more time** — Actually, looking at the diff carefully, the flag is registered in `cmd/atcr/review.go` via `cmd.Flags().String("sprint-plan", ...)`. This is a common pattern for command-specific flags. The sprint plan mentions `flags.go` but the implementation chose to register it directly. This is a minor design choice. Since `flags.go` isn't in the diff, this is out of scope.

OK, I think the main issues I've found:

1. **HIGH/MEDIUM: Resume path drops the sprint plan constraint** — `PrepareResume` doesn't load or pass the constraint, so resumed agents run unconstrained. This is a correctness issue.

2. **LOW: `resolveScopeConstraint` doesn't validate the path is absolute or canonical** — Not a bug, intentional.

3. **LOW: The duplication between `PrepareReview` and `PrepareReviewFromDiff` for the constraint loading** — Could be refactored.

4. **MEDIUM: `flags.go` not updated per the sprint plan** — Out of scope of the diff (file not in diff), skip.

Actually, looking more carefully at the resume issue. The sprint plan says:
"IN scope: ... engine wiring through `PrepareReview`/`PrepareReviewFromDiff` → `buildSlots` → `buildAgent`, unit tests."

Resume is not in the listed in-scope files. But the diff DOES modify `internal/fanout/resume.go`. The sprint plan says "Map the `--sprint-plan` flag in `cmd/atcr/review.go` and `cmd/atcr/flags.go`" — these are CLI flag mappings, not engine wiring. Resume is engine wiring.

Actually, the issue is that the diff modifies `resume.go` to add the new parameter to `buildSlots`, but doesn't thread the constraint through. This is in the diff and it's a correctness bug. So I should flag it.

Let me also check if there's any issue with `cmd/atcr/flags.go`. The diff doesn't modify it. So nothing to flag there.

Let me also consider:
- **Truncation warning doesn't mention how much was truncated** — Minor.
- **No way to tell if a plan was loaded but truncated** — Actually the warning does say "exceeded N bytes and was truncated before injection", which conveys it was over the cap.

Let me look at one more potential issue:

57. **`buildSlots` change adds a new parameter `scopeConstraint string`** — This is a breaking change to the function signature. All callers must be updated. The diff updates `PrepareReview`, `PrepareReviewFromDiff`, `PrepareResume`, and all tests. Good.

58. **`buildAgent` change adds `scopeConstraint string`** — Same as above. All callers updated.

59. **Test `TestBuildAgent_PrependsScopeConstraint` uses `strings.Index`** — Checks that constraint is at position less than diff position. Good test design.

60. **Test `TestResolveScopeConstraint`** — Creates a directory and passes it as `SprintPlanPath`. `os.ReadFile(dir)` returns an error (on most systems), so this tests the "unreadable" path. Good.

61. **One concern: `os.ReadFile` of a directory on Linux returns `EISDIR`, on Windows it may return something else.** The test uses `t.TempDir()` which is a real directory. `ReadSprintPlan(dir)` should return an error. Let me check:
    
    On Linux: `os.ReadFile("/tmp/test-dir")` returns `read /tmp/test-dir: is a directory`.
    On macOS: similar.
    On Windows: `os.ReadFile` of a directory may return a different error but still an error.
    
    So the test should pass on common platforms. Good.

62. **Let me check the `prepareReview`'s stderr write — what if `os.Stderr` is unavailable?** Unlikely in practice. Not a bug.

63. **Issue with `TestReviewCmd_SprintPlanFlagRegistered`**: It only checks the flag exists and defaults to empty. Doesn't check the help text. Good enough.

64. **Issue: The error handling in `resolveScopeConstraint` returns a warning that includes the error message via `%v`**. This could expose path information or system details. Since the path is the user's own input, this is fine.

65. **Let me look for issues with the `prepareReview` function flow**: After `resolveScopeConstraint`, `buildSlots` is called with the constraint. The constraint is empty if no plan or plan is unreadable. Good.

66. **Issue: In `PrepareReview`, the constraint is loaded AFTER the early-return checks**. Let me check:
    
    Looking at the diff:
    ```go
    // Existing code...
    if empty {
        return nil, fmt.Errorf(...)
    }
    // Sprint-plan scope (Epic 12.2): ...
    scopeConstraint, scopeWarn := resolveScopeConstraint(req)
    if scopeWarn != "" {
        fmt.Fprintln(os.Stderr, "warn: "+scopeWarn)
    }
    slots, perAgentMode, err := buildSlots(cfg, payloads, req.Range, "", scopeConstraint)
    ```
    
    So the constraint is loaded only if the review has content. If the review has no content (empty range), the constraint is never loaded, no warning is printed. This is fine — if there's no review, why warn about the plan?

67. **Issue: `PrepareResume` doesn't call `resolveScopeConstraint`**. This is the main issue. Should it?

    The sprint plan doesn't explicitly say resume must support sprint plans. But practically, if a user starts a review with `--sprint-plan` and the review is interrupted, on resume they'd expect the constraint to be reapplied. The current implementation drops it.

    Actually wait, let me re-think. The resume path doesn't re-read files from disk; it reads cached state. If the original review had a constraint in the prompt, that constraint is in the cache. The cache key is based on the rendered prompt. On resume, if `buildSlots` is called with empty constraint, the cache key would differ from the original, causing a cache miss.

    But wait — on resume, `PrepareResume` reads the review state from disk. The state includes... let me think. Does it include the rendered prompts or the request?

    Looking at the diff, `PrepareResume` takes `req ReviewRequest` — so the request is available. The `req.SprintPlanPath` is in the request. The implementation could:
    1. Re-read the plan file during resume (but the path might not be in the original request if resume is from CLI without the flag).
    2. Read the constraint from the persisted review state.

    The current implementation just passes `""` for the constraint, which means resumed agents run without the constraint.

    This is a real bug. The constraint should either be persisted in the review state and re-loaded, or the resume should re-read the plan file. Since the current code passes `""`, the resumed agents run unconstrained. The cache key would differ from the original (which had the constraint), causing cache misses (which is "safe" in the sense that the review proceeds, but inconsistent).

68. **Severity**: This is MEDIUM. The resumed agents run with different prompts than the original run, leading to inconsistency. The fix is to either persist the constraint or re-read the plan file.

OK, let me also check if there's anything I'm missing about how the review state is persisted. I don't have visibility into `PrepareResume`'s full implementation from the diff. But the diff shows that `buildSlots` is called with `""` for the constraint, which is the bug.

Now let me think about what other issues to flag. Let me review once more:

69. **`buildAgent` signature change**: The function now takes `scopeConstraint` as a parameter. This is a breaking change. The diff updates all callers. Good.

70. **`buildSlots` signature change**: Same as above. Good.

71. **Test isolation**: Each test uses `t.TempDir()` for filesystem isolation. Good.

72. **Test for whitespace-only plan content**: `TestScopeConstraint` tests `ScopeConstraint("   \n\t  ")` → `("", false)`. Good.

73. **Issue: `ReadSprintPlan` could potentially be called concurrently from multiple prepare paths** — `os.ReadFile` is safe for concurrent use. The result `string` is immutable. Good.

74. **Issue: `resolveScopeConstraint` doesn't validate the plan content for malicious content** — The sprint plan says "The sprint plan file is trusted local operator input. No additional sandbox validation is required." Good.

75. **Issue: What about race conditions between PrepareReview and the agent invocation?** The constraint is in the rendered prompt at build time. If the plan file changes between build and invocation, no effect because the constraint is already in the prompt. Good.

OK, let me finalize my findings:

**Finding 1**: Resume path drops the sprint plan constraint (MEDIUM, correctness)
**Finding 2**: Maybe also note that the truncation warning could be more informative (LOW)
**Finding 3**: The `flags.go` mention in the sprint plan isn't reflected in the diff — but this is out of scope since flags.go isn't in the diff.

Actually wait, let me check the issue more carefully. The sprint plan says resume isn't in scope. The diff modifies resume.go to update the buildSlots signature. The fact that the constraint is dropped is a consequence of the signature change. Is this in scope?

Looking at the SCOPE CONSTRAINT in the system prompt:
"The following sprint plan defines what is IN SCOPE for this review. Only flag issues in files/areas directly related to the work items below."

The sprint plan in-scope items:
- `--sprint-plan` CLI flag
- `internal/payload/sprintplan.go` reader/formatter
- SCOPE CONSTRAINT block construction
- 16 KiB size-cap
- engine wiring through `PrepareReview`/`PrepareReviewFromDiff` → `buildSlots` → `buildAgent`
- unit tests

Resume is "engine wiring" in a sense. The diff modifies resume.go to support the new `buildSlots` signature. The fact that resume drops the constraint is a bug in the engine wiring. So it's in scope.

Let me also think about whether this is HIGH or MEDIUM. The bug:
- User starts review with `--sprint-plan plans/auth.md`
- Review is interrupted
- User resumes
- Resume drops the constraint
- Resumed agents run without the constraint, producing potentially out-of-scope findings
- The cached results from the original run had the constraint

This is a correctness bug. Severity: MEDIUM (doesn't crash, but produces inconsistent results).

Actually, on further thought, this could be HIGH because the resumed review will silently produce different (unconstrained) results than the original review. But the AC says "Allow the review to proceed" for missing/empty plans, so proceeding without a constraint is valid behavior. The issue is more about consistency between the original and resumed runs.

Let me settle on MEDIUM.

Let me also check one more potential issue:

76. **`TestResolveScopeConstraint` uses `dir` as `SprintPlanPath`** — This relies on `os.ReadFile` of a directory returning an error. On some platforms (e.g., some BSDs), this might behave differently. But the test would fail on those platforms, indicating a real issue. Not a bug in the code, just a test portability concern.

77. **`scopeConstraint` could be very long if the plan is huge** — But it's capped at 16 KiB. Good.

78. **What if the rendered prompt with constraint + diff exceeds the model's context window?** Not handled here, but the existing `ApplyByteBudget` handles the diff portion. The constraint is uncounted. The sprint plan acknowledges this: "The constraint is uncounted extra bytes added to every agent prompt". So the model might see truncated prompts due to byte budget, but the constraint is added on top. This could cause issues with very tight byte budgets. The sprint plan acknowledges this and the size cap mitigates it.

79. **Test `TestScopeConstraint` checks for "critical" in the block** — Good test of the escape hatch.

OK, I think my main finding is the resume issue. Let me also see if there are any other issues I missed.

Looking once more at the `resolveScopeConstraint` function:
```go
func resolveScopeConstraint(req ReviewRequest) (constraint, warning string) {
    raw, err := payload.ReadSprintPlan(req.SprintPlanPath)
    if err != nil {
        return "", fmt.Sprintf("sprint plan %q is unreadable; proceeding with a diff-wide review: %v", req.SprintPlanPath, err)
    }
    block, truncated := payload.ScopeConstraint(raw)
    if truncated {
        warning = fmt.Sprintf("sprint plan %q exceeded %d bytes and was truncated before injection", req.SprintPlanPath, payload.MaxSprintPlanBytes)
    }
    return block, warning
}
```

The error message says "is unreadable". But this includes the case where the file doesn't exist (since `ReadSprintPlan` returns nil for missing files). Wait, no — `ReadSprintPlan` returns nil for missing files (AC2), so `err` would be nil for missing files. So the error message only fires for actual read errors (permission, directory, etc.). Good.

But wait — the error message could be misleading. If the user typed `--sprint-plan /bad/path` and the file doesn't exist, `ReadSprintPlan` returns `("", nil)` (silent per AC2). The user gets no feedback. This is per spec (AC2). Good.

80. **Issue: `ResolveScopeConstraint` doesn't trim `req.SprintPlanPath` itself**. It passes the path directly to `ReadSprintPlan` which does the trim. Good.

81. **Issue: The warning message for unreadable plan uses `%q` which adds quotes**. Cosmetic, fine.

82. **One more potential issue: `cmd/atcr/flags.go` not updated**. The sprint plan says flags should be mapped there. Since `flags.go` isn't in the diff, I can't verify. But the flag is registered directly in `cmd/atcr/review.go`. This might be a deviation from the plan. Since `flags.go` isn't in the diff, this is OUT-OF-SCOPE per the scope constraint.

Actually, the scope constraint says "if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE". Since `flags.go` isn't in the diff and isn't directly mentioned in the in-scope tasks (the in-scope list mentions it but the diff doesn't modify it), I should skip this.

Wait, actually the sprint plan does mention `cmd/atcr/flags.go` explicitly in the Technical Approach:
"Map the `--sprint-plan` flag in `cmd/atcr/review.go` and `cmd/atcr/flags.go` to `ReviewRequest.SprintPlanPath`."

But the diff doesn't modify `cmd/atcr/flags.go`. Is this a bug or out of scope?

The scope constraint says to focus on files/areas directly related to work items. The sprint plan lists `cmd/atcr/flags.go` as a file to modify. If the diff doesn't modify it, that's a deviation. But since `flags.go` isn't in the diff, I can't flag an issue in that file.

Hmm, this is a meta-issue. Let me skip it since I can't review code that's not in the diff.

OK let me finalize my findings.

Actually, one more thing I want to verify. Let me re-read the sprint plan's clarification:

"The formatted constraint is threaded as a parameter through `buildSlots` → `buildAgent` and prepended to `PayloadContext.Payload` at `review.go:766`."

The diff modifies `buildSlots` and `buildAgent` to take the constraint. It prepends at the `RenderPrompt` call. Looking at the diff:
```go
prompt, err := payload.RenderPrompt(persona.Text, payload.PayloadContext{
    ...
    Payload: scopeConstraint + mp.Text,
    ...
})
```

The Payload is `scopeConstraint + mp.Text`. So the constraint is prepended. Good.

Let me also check: the sprint plan says `review.go:766`. The actual line in the diff is around line 829 (based on the diff context). This is because the diff adds new lines before the modification. Let me count:

Before the change, the line `Payload: mp.Text` is at line 766. After adding ~20 lines for the comment block and the new `scopeConstraint` parameter, the line is now around 829. This is fine — the line number changes due to additions.

OK, I think my main finding is:

**Finding: `PrepareResume` doesn't thread the sprint plan constraint through `buildSlots`, so resumed agents run without the constraint even if `--sprint-plan` was specified.**

Let me also think about whether there are other issues:

83. **`buildSlots` parameter `scopeConstraint` is always passed, even when empty**. This is fine — empty string just means no constraint.

84. **Test for `TestPrepareReviewFromDiff_InjectsSprintPlanConstraint`** — This test creates a real file and calls `PrepareReviewFromDiff`. It verifies the constraint appears in every slot's prompt. Good test.

85. **Test `TestResolveScopeConstraint`** — Tests all three dispositions. Good test.

86. **Test for the truncation case** — Tests that the embedded plan is capped at `MaxSprintPlanBytes`. Good.

87. **Test for UTF-8 truncation** — Tests that the cap doesn't split a rune. Good.

88. **Test for `TestBuildAgent_PrependsScopeConstraint`** — Tests that the constraint is prepended. Good.

OK, I think I have my main finding. Let me also consider:

89. **Issue: `scopeWarn` is never read after being checked for empty** — Actually, it IS checked: `if scopeWarn != ""`. That's the check. Good.

90. **Issue: `fmt.Fprintln(os.Stderr, "warn: "+scopeWarn)`** — The "warn: " prefix is hardcoded. This matches the AC3 "prints a warning to stderr" requirement. Good.

91. **Issue: What if the plan file has Windows line endings?** `os.ReadFile` returns the content as-is, including `\r\n`. `ScopeConstraint` doesn't strip `\r`. The content is just trimmed at the edges. The plan content with `\r\n` line endings would be embedded in the block. The model's prompt would have `\r\n` line endings. This might cause minor formatting issues but no correctness bugs.

OK, I'm confident in my finding. Let me write it up.

Actually, wait. Let me also check one more thing about the resume issue. The diff shows:
```go
slots, _, err := buildSlots(cfg, payloads, req.Range, "", "")
```

The `req` is available in `PrepareResume`. The `req.SprintPlanPath` could be checked. But the current code passes `""` for the constraint. This means during resume:
- The plan is not re-read (good — avoids re-reading the file).
- The constraint is not applied.

The fix would be to either:
1. Re-read the plan and apply the constraint (could fail if file is now missing).
2. Skip the constraint entirely (current behavior).
3. Persist the constraint in the review state and re-load it.

Option 3 would be the cleanest but requires more changes. The current code does option 2. This is a design choice, but it does mean resumed reviews are unconstrained.

Is this a bug? Looking at the sprint plan's AC1: "`atcr review --sprint-plan <path>` is a valid CLI flag." AC1 only requires the flag to exist on `atcr review`. It doesn't say resume must support it.

AC2-AC4 are about behavior with various plan files. AC5 is about cache invalidation. AC6 is about size capping.

So technically, the sprint plan doesn't require resume to support sprint plans. The current behavior is "diff-wide review on resume". This could be considered a known limitation rather than a bug.

But it's still inconsistent. If a user runs `atcr review --sprint-plan plans/auth.md` and the review is interrupted, on resume they'd expect the constraint to still apply. The current behavior silently drops it.

I'll flag this as MEDIUM (correctness issue, design concern).

Actually, let me also think about whether this is in scope for the review. The diff modifies `internal/fanout/resume.go` to update the `buildSlots` signature. The change is minimal (just adding `""` for the new parameter). The fact that the constraint isn't applied during resume is a consequence of this minimal change.

If the implementer had wanted to support sprint plans during resume, they would have:
1. Called `resolveScopeConstraint(req)` in `PrepareResume`.
2. Passed the result to `buildSlots`.

They chose not to. This could be intentional (out of scope per the plan) or an oversight.

Either way, the current implementation drops the constraint during resume. This is a real behavior change. I'll flag it as MEDIUM.

Let me also think about whether to flag the duplication between `PrepareReview` and `PrepareReviewFromDiff`. Both call:
```go
scopeConstraint, scopeWarn := resolveScopeConstraint(req)
if scopeWarn != "" {
    fmt.Fprintln(os.Stderr, "warn: "+scopeWarn)
}
```

This is 4 lines duplicated. Could be extracted into a helper. But it's not a bug, just code duplication. I'll skip this.

OK, finalizing my findings:

**Finding 1 (MEDIUM)**: Resume path drops sprint plan constraint. `PrepareResume` passes `""` for the scope constraint, so resumed agents run without the constraint even if `--sprint-plan` was specified.

**Finding 2 (LOW)**: Truncation warning could include original size for better diagnostics.

Actually, let me think about Finding 2 more carefully. The current warning is:
```
sprint plan "plans/auth.md" exceeded 16384 bytes and was truncated before injection
```

This tells the user the cap and that truncation happened. The original size isn't shown. This is a minor UX issue. LOW.

Hmm, I'm going to keep just the main finding. Let me also check if there's anything else.

92. **Issue: `prepareReview`'s stderr write** — The function writes to stderr directly. This is a side effect in a "Prepare" function which is unusual. Library functions typically return errors and let the caller handle output. But the sprint plan AC3 requires stderr output, and the implementer chose to handle it in the Prepare functions. This is a minor design concern but not a bug.

93. **Issue: `cmd/atcr/flags.go` not in diff** — Out of scope per the scope constraint.

94. **One more potential issue: The `sprintPlanPath` function in `cmd/atcr/review.go` doesn't handle the case where the flag is registered with a different name**. Not a bug.

OK, I'm going with my main finding. Let me write it up in the required format.

Actually, let me re-examine the diff for one more issue. The `cmd/atcr/review.go` diff shows:

```go
// sprintPlanPath returns the --sprint-plan flag value (trimmed). Empty when the
// flag is unset, which leaves the review diff-wide (Epic 12.2). The path is
// passed through verbatim (relative paths resolve against CWD when the engine
// reads the file); a missing/unreadable plan is handled in the engine, not here,
// so a bad path never blocks flag parsing.
func sprintPlanPath(cmd *cobra.Command) string {
    v, _ := cmd.Flags().GetString("sprint-plan")
    return strings.TrimSpace(v)
}
```

This is a helper function. It's fine. The error from `GetString` is intentionally ignored (the flag exists, so no error). Good.

95. **Issue: `sprintPlanPath` ignores the error from `GetString`** — This is intentional per the comment. The flag is registered in `newReviewCmd`, so `GetString` will succeed. If someone refactors and removes the flag registration, this would silently return empty. Not a bug, but a brittleness concern.

OK, I'm done. My main finding is the resume issue.

Wait, let me also think about one more thing: the `flags.go` mention. The sprint plan says to register the flag in `flags.go`. The diff registers it in `review.go` directly. This is a deviation from the plan, but since `flags.go` isn't in the diff, I can't flag it as a code issue.

If `flags.go` exists and has a pattern like:
```go
func AddSharedFlags(cmd *cobra.Command) {
    cmd.Flags().String("sprint-plan", "", "...")
}
```

Then `review.go` should call this function instead of registering the flag directly. But I can't see `flags.go`, so I can't flag this.

Actually, the sprint plan explicitly says:
"Register `--sprint-plan` flag in `cmd/atcr/flags.go`."

This is a task. If `flags.go` isn't being modified, then the task isn't being completed. But again, this is out of scope of the diff review.

OK, final answer: just the resume finding.

Wait, one more thing. Let me re-read the scope constraint:
"if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)."

The resume issue is in `internal/fanout/resume.go`. Is this file mentioned in the sprint plan's tasks/stories?

Looking at the sprint plan:
- "IN scope: `--sprint-plan` CLI flag, `internal/payload/sprintplan.go` reader/formatter, SCOPE CONSTRAINT block construction, 16 KiB size-cap, engine wiring through `PrepareReview`/`PrepareReviewFromDiff` → `buildSlots` → `buildAgent`, unit tests."

Resume is not explicitly listed. But the diff modifies resume.go to support the new `buildSlots` signature. This means the diff is touching resume.go as part of the engine wiring change. The bug is in the engine wiring for resume.

I think this is borderline in-scope. The engine wiring change naturally extends to resume (because resume calls `buildSlots`). The fact that the constraint isn't applied during resume is a consequence of the engine wiring change.

I'll include this finding. It's a real correctness issue in code that's being modified by this diff.

Let me also think about whether to flag the `flags.go` deviation. The sprint plan says flags should be in `flags.go`. The diff puts them in `review.go`. This is a deviation from the plan, but since `flags.go` isn't in the diff, I can't review it.

Actually, looking at this more carefully: the sprint plan says to register the flag in `flags.go`. The diff registers it in `review.go`. If `flags.go` exists and has a shared flags pattern, this is a code organization issue. But since `flags.go` isn't in the diff, I can't see if this is a deviation from an established pattern.

I'll skip this. It's too speculative without seeing `flags.go`.

OK, my final finding is the resume issue. Let me write it up.

Actually, I realize I should also check the buildAgent signature change more carefully. The function now takes `scopeConstraint` as a parameter. Let me check if there are any other callers that might not be in the diff.

Looking at the diff, `buildAgent` is called from:
1. `buildSlots` (line ~675 in new code) — updated.
2. Tests in `internal/fanout/retry_wiring_test.go` — updated.
3. Tests in `internal/fanout/review_scope_inject_test.go` — updated.
4. Tests in `internal/fanout/review_test.go` — updated.
5. Tests in `internal/fanout/review_tools_test.go` — updated.

Are there any other callers? Without seeing the full file, I can't be 100% sure. But the diff seems comprehensive.

OK, I'm done. My main finding is the resume issue.

Let me also consider if there's a HIGH severity issue I'm missing. Let me think:

- The constraint is prepended to the payload. If `mp.Text` (the diff) is empty, the constraint is the entire payload. This is fine.
- The constraint is not added to `ScopeRule` or `ScopeFocus`. The sprint plan says: "Compose — do not conflict — with the existing scope mechanisms: `ScopeRule` (per-payload-mode) and `ac.Scope`/`ScopeFocus` (per-agent, Epic 2.2)." So they're separate. Good.
- The constraint is uncounted by `ApplyByteBudget`. This is acknowledged in the sprint plan.

I don't see any HIGH severity issues.

One more potential issue: 

96. **Issue: `cmd/atcr/flags.go` not updated** — The sprint plan says flags should be in `flags.go`. The diff doesn't modify `flags.go`. This could be a missing change. But since `flags.go` isn't in the diff, I can't flag it.

OK, I'm done. Final answer: one MEDIUM finding about resume dropping the constraint.

Hmm, let me think one more time. The sprint plan in-scope items:
- `--sprint-plan` CLI flag ✓ (in review.go)
- `internal/payload/sprintplan.go` reader/formatter ✓ (new file)
- SCOPE CONSTRAINT block construction ✓ (in sprintplan.go)
- 16 KiB size-cap ✓ (MaxSprintPlanBytes)
- engine wiring through `PrepareReview`/`PrepareReviewFromDiff` → `buildSlots` → `buildAgent` ✓
- unit tests ✓

Resume is not in the in-scope list. But the diff modifies resume.go to support the new `buildSlots` signature. This is technically a "consequence" of the engine wiring change (since `buildSlots` signature changed).

The bug is: during resume, the sprint plan constraint is silently dropped.

This is borderline in-scope. I'll include it.

Actually, let me also check if there's a way to test this. The test `TestPrepareReviewFromDiff_InjectsSprintPlanConstraint` doesn't test the resume path. So the bug isn't caught by tests.

OK, finalizing. I'll output one MEDIUM finding.

Actually, wait. Let me re-read the diff for resume.go:
```go
-	slots, _, err := buildSlots(cfg, payloads, req.Range, "")
+	slots, _, err := buildSlots(cfg, payloads, req.Range, "", "")
```

The diff changes from `buildSlots(..., "")` to `buildSlots(..., "", "")`. The first empty string was `forceMode`. The second empty string is the new `scopeConstraint`. So during resume, the constraint is always empty.

The fix would be to:
1. Add `scopeConstraint, scopeWarn := resolveScopeConstraint(req)` in `PrepareResume`.
2. Print the warning if non-empty.
3. Pass `scopeConstraint` to `buildSlots`.

This is a real fix needed.

OK, my final finding:

**MEDIUM**: `internal/fanout/resume.go:287` — `PrepareResume` passes `""` for the scope constraint, so resumed agents run without the sprint plan constraint even if `--sprint-plan` was specified in the original review request. This silently produces different (unconstrained) results than the original run.

Fix: Call `resolveScopeConstraint(req)` in `PrepareResume` and pass the result to `buildSlots`.

OK let me write this up in the required format.

Actually, one more sanity check. Let me verify the resume.go line number. The diff shows:
```
@@ -284,7 +284,7 @@ func PrepareResume(ctx context.Context, cfg *ReviewConfig, reviewDir string, req
 	if err != nil {
 		return nil, nil, err
 	}
-	slots, _, err := buildSlots(cfg, payloads, req.Range, "")
+	slots, _, err := buildSlots(cfg, payloads, req.Range, "", "")
```

So the change is at line 287 (after the `+1` offset). The line number in the new file is 287.

OK, final answer:
</think>

MEDIUM|internal/fanout/resume.go:287|Resume path silently drops sprint plan constraint|Call resolveScopeConstraint(req) in PrepareResume and pass result to buildSlots so resumed agents honor --sprint-plan|correctness|15|buildSlots called with "" constraint, req.SprintPlanPath ignored on resume|bruce