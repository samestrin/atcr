<think>
Let me analyze the diff carefully. The changes are all related to Epic 7.3 (GitHub Action / PR Integration), which is in scope. Let me review each file:

1. **action.yml** - Composite GitHub Action
2. **cmd/atcr/github.go** - New `atcr github` subcommand
3. **cmd/atcr/github_integration_test.go** - Integration test
4. **cmd/atcr/github_test.go** - Unit tests
5. **cmd/atcr/main.go** - Registers the new subcommand
6. **cmd/atcr/main_test.go** - Updates test to expect 14 subcommands
7. **docs/ci-integration.md** - Documentation update
8. **docs/github-action.md** - New documentation
9. **internal/boundaries_test.go** - Adds ghaction to allowed imports
10. **internal/ghaction/client.go** - HTTP client
11. **internal/ghaction/client_test.go** - Client tests
12. **internal/ghaction/comments.go** - Inline comment logic
13. **internal/ghaction/comments_test.go** - Comment tests
14. **internal/ghaction/render.go** - Check output rendering
15. **internal/ghaction/render_test.go** - Render tests

Let me look for issues:

### action.yml

Looking at the `atcr github` step:
```yaml
ARGS=(github --repo "$REPO" --sha "$HEAD_SHA" --check-name "$CHECK_NAME")
if [ -n "$FAIL_ON" ]; then
  ARGS+=(--fail-on "$FAIL_ON")
fi
if [ "$INLINE" = "true" ]; then
  ARGS+=(--inline-comments --pr "$PR_NUMBER")
fi
"$ATCR_BIN" "${ARGS[@]}"
```

The default for `fail-on` is "high" in the action.yml inputs. The command line flag will always have a value (empty or "high"). The subcommand treats empty as "informational, non-blocking". So the default is "high", which is fine.

Wait - looking at this more carefully: `--fail-on "$FAIL_ON"` is only added `if [ -n "$FAIL_ON" ]`, so if the user explicitly sets it to empty, the flag isn't passed. But the default is "high", so `FAIL_ON` will almost always be non-empty.

The `--inline-comments` flag is added if `INLINE=true`. The Go code's `--inline-comments` is a bool flag. Cobra bool flags don't take values; they just need `--inline-comments` to set them to true. This looks fine.

### cmd/atcr/github.go

```go
if len(args) == 1 {
    arg = args[0]
}
reviewDir, err := anchorDir(arg)
```

`anchorDir` is called with the positional arg. If `arg` is empty (no positional arg), this would use the current dir or default. Looks fine.

```go
conclusion, failCount := ghaction.Conclusion(findings, failOn)
output := ghaction.BuildCheckOutput(findings, failOn)

client := &ghaction.Client{APIURL: apiURL, Token: token}
if err := client.CreateCheckRun(cmd.Context(), owner, repo, ghaction.CheckRunRequest{
    Name:       checkName,
    HeadSHA:    sha,
    Conclusion: conclusion,
    Output:     output,
}); err != nil {
    return &codedError{code: exitFailure, err: err}
}
```

This looks okay.

```go
if conclusion == "failure" {
    return &codedError{code: exitFailure, err: fmt.Errorf("%d finding(s) at or above %s", failCount, failOn)}
}
return nil
```

This handles the gate logic. The check run has already been posted with "failure" conclusion. The exit code returns 1, which is consistent with `--fail-on` semantics.

But wait - if `failOn` is empty (informational), conclusion is "neutral", and the function returns nil. Good.

If `failOn` is set but no findings block, conclusion is "success", and returns nil. Good.

If `failOn` is set and findings block, conclusion is "failure", check is posted as failure, and we exit 1. Good.

### Race condition in tests

In `github_integration_test.go`:
```go
var mu sync.Mutex
var checkBody map[string]any
var comments []map[string]any
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    raw, _ := io.ReadAll(r.Body)
    mu.Lock()
    switch r.URL.Path {
    case "/repos/samestrin/atcr/check-runs":
        _ = json.Unmarshal(raw, &checkBody)
    case "/repos/samestrin/atcr/pulls/12/comments":
        var c map[string]any
        _ = json.Unmarshal(raw, &c)
        comments = append(comments, c)
    }
    mu.Unlock()
    w.WriteHeader(http.StatusCreated)
    _, _ = w.Write([]byte(`{"id":1}`))
}))
```

The mutex protects reads and writes. OK.

But in `github_test.go`:
```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    mu.Lock()
    paths[r.URL.Path]++
    mu.Unlock()
    ...
}))
```

The map is protected by the mutex in the handler. But then outside:
```go
mu.Lock()
defer mu.Unlock()
assert.Equal(t, 1, paths["/repos/samestrin/atcr/check-runs"], "check run still posts")
```

The map access outside is also locked. OK.

### Hmm, let me look at the BuildCheckOutput logic

In render.go:
```go
for i, f := range findings {
    row := fmt.Sprintf("| %s | %s | %s | %s |\n",
        cell(f.Severity), cell(location(f)), cell(f.Problem), cell(f.Confidence))
    // Stop before crossing GitHub's output.text limit, leaving room for the
    // truncation notice. The remaining findings still live in the artifacts.
    if b.Len()+len(row)+128 > maxCheckTextBytes {
        fmt.Fprintf(&b, "\n_…table truncated: %d of %d findings shown. See the uploaded artifacts for the full set._\n", i, total)
        break
    }
    b.WriteString(row)
}
```

The loop variable `i` is used for the truncation count. Since `i` starts at 0 and is incremented per iteration, when we break, `i` is the number of findings that have been shown. Wait, but if we break, we haven't shown finding `i` yet, just the first `i` findings. So "i of total" means "i shown, total findings". That's correct - the user sees the i shown + the rest in artifacts.

Actually wait, let me re-read. If i=0, we show finding 0. If i=1, we show finding 1. So when we break, we have shown findings 0..i-1, which is i findings. The truncation message says "i of total findings shown", which is correct.

Hmm, but the check is `b.Len()+len(row)+128 > maxCheckTextBytes` before writing. If we hit the limit when checking finding at index i, we break and show "i of total". Correct.

### FixAttribution tokenization

In render.go:
```go
func FixAttribution(evidence string) string {
    for _, seg := range strings.Split(evidence, "; ") {
        seg = strings.TrimSpace(seg)
        if rest, ok := strings.CutPrefix(seg, fixAttributionPrefix); ok {
            if name := strings.TrimSpace(rest); name != "" {
                return name
            }
        }
    }
    return ""
}
```

The token is "; fix by <name>". If the executor stage writes "snippet; fix by opus", then splitting by "; " gives ["snippet", "fix by opus"]. Cutting the prefix "fix by " gives "opus". Good.

But what if the snippet itself contains "; "? E.g., "snippet with ; semicolon; fix by opus". Then split gives ["snippet with ", " semicolon", "fix by opus"]. The "; semicolon" segment is trimmed to "; semicolon", which doesn't start with "fix by ". The "fix by opus" segment matches. OK, this is fine.

### Comments: missing file/line

The comment builder skips findings without file/line. Good.

### Action.yml script injection

The action.yml uses env vars to pass values to shell, which is the right approach. Good.

### Default `fail-on` in action.yml vs in subcommand

The action.yml has `default: 'high'` for `fail-on`. The subcommand's `--fail-on` flag has no default, so if not passed, it's empty. The action.yml's `if [ -n "$FAIL_ON" ]` correctly only adds the flag when non-empty. So if a user wants informational, they set it to empty. Good.

But wait - the action.yml docs say "Leave empty for an informational, non-blocking check" but the default is "high". So the user has to explicitly clear it. That's a minor doc thing, not a code issue.

### `--inline-comments` flag

This is a `Bool` flag in cobra. In the action.yml:
```bash
ARGS+=(--inline-comments --pr "$PR_NUMBER")
```

The `--inline-comments` is added without a value, which is correct for a bool flag.

But wait - the subcommand also has `cmd.Flags().Bool("inline-comments", false, ...)`. The flag is named `inline-comments` with a hyphen. When cobra parses flags, `--inline-comments` becomes `inline-comments` key. OK.

### Are there any real issues?

Let me look more carefully...

1. **Unused variable `out` in `github_test.go`:** `_ = out` is used to suppress unused error. Not an issue.

2. **The `failCount` in `github.go` is unused after computing `conclusion`:** Actually, `failCount` is used in the final error message. OK.

3. **Token never validated:** The token is required and validated as non-empty. Good.

4. **The check run is posted as "completed" always, never "queued" or "in_progress":** This is documented. OK.

5. **`internal/ghaction/render.go` line ~30:** The `isRefuted` function checks `f.Verification != nil`. The `Verification` field is a pointer in `reconcile.JSONFinding`. OK.

6. **`Conclusion` returns "neutral" for empty threshold:** This is fine.

7. **`--inline-comments` without `--pr` returns usage error:** Validated. Good.

8. **Edge case: `--pr` is required when inline comments is set, but the check below tests this only for the case where `--pr` is missing:** Actually the test `TestGithubCmd_InlineCommentsRequirePR` covers this. Good.

9. **Possible issue: `INLINE` env var passed to action.yml is a string "true" or "false"**, the bash check is `if [ "$INLINE" = "true" ]`. This is correct.

10. **`fail-on` default of "high" in action.yml vs default of "high" via `ParseSeverity`:** Both work. The action.yml default is "high" by string, and the subcommand parses it. Good.

11. **The build step uses `working-directory: ${{ github.action_path }}`** - This means building happens in the action's repo checkout, not the consumer's. Good.

12. **Cache dependency path uses `${{ github.action_path }}/go.sum`:** This is the action's go.sum, not the consumer's. Reasonable.

13. **Potential security issue: `OPENROUTER_API_KEY` is exposed as env var in the consumer's step.** This is normal for CI actions, but the env var could be logged. The action.yml comment mentions script injection mitigation, which is good.

14. **Missing input validation for `go-version`:** It's passed to `setup-go@v5`. If a user sets a malicious value... but this is just a Go version string. Not a major concern.

15. **No retries on API failure:** The client makes a single POST. If GitHub is briefly unavailable, the comment fails. The postInlineComments function logs the failure and continues. Good for best-effort.

16. **`output.Text` truncation uses `b.Len()+len(row)+128 > maxCheckTextBytes`:** This leaves 128 bytes for the truncation notice. Reasonable. But the calculation is approximate because `fmt.Fprintf` produces more than just the format string. Let me think... The notice is "_\n_…table truncated: %d of %d findings shown. See the uploaded artifacts for the full set._\n". This can be 80-100 chars. The 128-byte buffer should be enough.

17. **Action.yml's `--check-name` default of "atcr"** matches the subcommand's default. Good.

18. **The `repo` flag is required to be set or `GITHUB_REPOSITORY` env to be set.** This is validated.

19. **Token absence is a usage error:** `errors.New("a GitHub token is required (pass --token or set GITHUB_TOKEN)")`. Good.

20. **Race condition in the test handler's `defer mu.Unlock()` at the end:** I see `mu.Lock()` at start and `mu.Unlock()` at end of the handler. The mu is held during `io.ReadAll` and `json.Unmarshal`. Good.

21. **Test assertion issues:** The integration test asserts the inline comment body contains "ATCR found: JWT signature not verified before claims are read". The body is built as "ATCR found: <problem>. Fix: <fix>. Suggested by: <executor>." with the period after each clause. Let me check:

In comments.go:
```go
fmt.Fprintf(&b, "ATCR found: %s.", strings.TrimSpace(f.Problem))
```

The test JSON has `"problem":"JWT signature not verified before claims are read"`. So the body would be "ATCR found: JWT signature not verified before claims are read." The test asserts `assert.Contains(t, body, "ATCR found: JWT signature not verified before claims are read")`. This matches. Good.

The test also asserts `assert.Contains(t, body, "Fix: Call jwt.Verify before decoding claims")`. The body would have "Fix: Call jwt.Verify before decoding claims." which contains the test assertion. Good.

Test asserts `assert.Contains(t, body, "Suggested by: opus")`. The body would have "Suggested by: opus." which contains the test assertion. Good.

22. **Cell function for markdown tables:** The `cell` function replaces `|` with `/` to avoid breaking the table. This is good but the test asserts:
```go
assert.NotContains(t, out.Text, "Unbounded map | grows")
assert.Contains(t, out.Text, "Unbounded map / grows")
```

This works because `|` is replaced with `/`. Good.

23. **`fixAttributionPrefix` constant:** Mirrors the executor stage's token. If the executor changes its format, this needs to change too. The constant is well-documented.

24. **The action uses `go-version: ${{ inputs.go-version }}` for setup-go:** If a user sets `go-version: '1.21'`, the build will fail if the codebase requires Go 1.25. Not a critical issue.

25. **The action doesn't pin actions/setup-go@v5 to a SHA:** It uses `@v5` (the major version tag). This is a common pattern but not the most secure. The GitHub Security Hardening guide recommends pinning to a SHA. This could be flagged as a security concern.

Actually wait, this is a soft recommendation. Most users accept the risk. Let me think about whether this is within scope for review...

It's related to the action's design, so it's in scope. But the question is whether to flag it as a real issue or a nit.

26. **Composite actions expose `OPENROUTER_API_KEY` to the consumer's step:** The action.yml uses `env:` to pass the key, which is standard practice. The key is masked in GitHub Actions logs. Good.

27. **`GITHUB_TOKEN` is exposed to all steps:** This is standard. The action needs it for the `atcr github` step.

28. **What if `atcr reconcile` doesn't produce `reconciled/findings.txt`?:** The subcommand calls `readReconciledFindings` which would error, returning a usage error. Good.

29. **What if the comment path is `internal/auth/token.go:42` but GitHub returns 422?:** The postInlineComments function logs a warning and continues. The check run is still posted. Good.

30. **What about the `comments_test.go` test for `CreateReviewComment`?** The fake server records the body, and assertions are made. Good.

31. **Looking at `TestBuildInlineComments` in comments_test.go:**

```go
{Severity: "LOW", File: "b.go", Line: 3, Problem: "minor", Fix: "", Evidence: "snippet"},
```

`Fix` is empty, so the body would be "ATCR found: minor." (no Fix clause, no attribution). The test asserts:
```go
assert.Contains(t, comments[1].Body, "ATCR found: minor")
assert.NotContains(t, comments[1].Body, "Fix:")
assert.NotContains(t, comments[1].Body, "Suggested by:")
```

The body is "ATCR found: minor." - no "Fix:" and no "Suggested by:". Good.

32. **The `cell` function replaces `|`, `\n`, `\r` in problem text.** The `Problem` field could contain markdown like `**`, `` ` ``, etc. The `cell` function doesn't escape these. A finding with a problem like "Use `flag.Parse()` correctly" would render as a markdown code span in the GitHub table. Probably fine.

33. **Markdown injection in the cell:** If a problem contains `|column|`, it's replaced with `/column/`. But what about `[link](url)`? That's fine in a table cell.

34. **No rate limit handling in client.go:** If the API returns 429 or 403 with rate limit, the client returns an error. The comment posting handles this per-comment. The check run creation would fail and the subcommand exits 1. Reasonable.

35. **Action.yml's `permissions:` example in docs is correct.** The docs/github-action.md has:
```yaml
permissions:
  contents: read
  checks: write
  pull-requests: write
```

This is the right set of permissions.

36. **The integration test's `fixtureReconciled` helper is not shown in the diff.** This is an existing helper. The test uses it. Not a concern.

37. **`execCmdCapture` helper is not shown in the diff.** Existing helper. Used by tests.

38. **`isolate` helper is not shown in the diff.** Existing helper.

39. **`anchorDir` helper is not shown in the diff.** Used to anchor the review dir. Existing or new? Let me check... The diff doesn't include it, so it's existing. OK.

40. **`readReconciledFindings` helper is not shown in the diff.** Existing. Used by the new github command.

41. **`usageError` and `codedError` helpers are not shown.** Existing.

42. **`exitFailure` constant is not shown.** Existing.

43. **The `--check-name` flag's default is "atcr" in both action.yml and the subcommand.** Good.

44. **The `--api-url` flag is for testing/local GitHub Enterprise.** Not in action.yml. So the consumer's `atcr github` call doesn't set `--api-url`. That means it uses `GITHUB_API_URL` env var, which GitHub sets automatically. Good.

But wait, the action.yml doesn't pass `GITHUB_API_URL` env var to the `atcr github` step. Let me check:

```yaml
- name: atcr github
  shell: bash
  working-directory: ${{ github.workspace }}
  env:
    GITHUB_TOKEN: ${{ inputs.github-token }}
    ATCR_BIN: ${{ github.action_path }}/bin/atcr
    REPO: ${{ github.repository }}
    HEAD_SHA: ${{ github.event.pull_request.head.sha || github.sha }}
    CHECK_NAME: ${{ inputs.check-name }}
    FAIL_ON: ${{ inputs.fail-on }}
    INLINE: ${{ inputs.inline-comments }}
    PR_NUMBER: ${{ github.event.pull_request.number }}
  run: |
    ...
```

The `env:` block sets specific variables. By default, GitHub Actions does NOT inherit the runner's environment variables unless you use `$GITHUB_ENV` or similar. Wait, that's not quite right.

Actually, GitHub Actions steps do have a base environment (like `GITHUB_SHA`, `GITHUB_REPOSITORY`, `GITHUB_API_URL`, etc.) that's always set by the runner. These are available to all steps regardless of whether you specify them in `env:`.

But the `env:` block in the step will OVERRIDE the runner-provided values if they have the same name. So if a user sets `GITHUB_TOKEN` in `env:`, it overrides the runner's default `GITHUB_TOKEN`.

For `GITHUB_API_URL`, the runner provides it (e.g., `https://api.github.com` for github.com, or the Enterprise URL for GHE). The step's `env:` block doesn't set `GITHUB_API_URL`, so the runner's value is used.

The subcommand uses `envOr(apiURLFlag, "GITHUB_API_URL")`, which checks the flag first, then the env var. If the env var is empty, `apiURL` would be empty, and the client uses `DefaultAPIURL = "https://api.github.com"`. So if GHE is used, `GITHUB_API_URL` would be set, and the client would use it. Good.

45. **Let me check the `--inline-comments` parsing.** The `cmd.Flags().GetBool("inline-comments")` returns a bool. The flag is set when `--inline-comments` is in the args. If the bash script adds `--inline-comments --pr "$PR_NUMBER"`, both are parsed. Good.

46. **Edge case: What if the user passes `--inline-comments=true`?** Cobra would parse this as `inline-comments=true`. The bash script always passes `--inline-comments` (no value), which sets it to `true`. The Go code checks `if inline && pr <= 0`. Good.

47. **`failCount` in `BuildCheckOutput`:** Wait, let me re-read:

```go
conclusion, failCount := Conclusion(findings, failOn)
output := ghaction.BuildCheckOutput(findings, failOn)
```

`Conclusion` is called once, and `BuildCheckOutput` is called once. `BuildCheckOutput` internally calls `Conclusion` again:

```go
conclusion, failCount := Conclusion(findings, failOn)
```

And in `BuildCheckOutput`:
```go
var title string
if strings.TrimSpace(failOn) == "" {
    title = fmt.Sprintf("atcr — %d finding(s)", total)
} else {
    threshold, _ := reconcile.ParseSeverity(failOn)
    title = fmt.Sprintf("atcr — %d finding(s), %d at/above %s", total, failCount, threshold)
}
```

`failCount` is the number of blocking findings. The title shows "X finding(s), Y at/above Z". This is correct.

But there's a minor inefficiency: `Conclusion` is called twice. Not a performance issue at this scale.

48. **Race in the integration test's `srv` handler:**

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    raw, _ := io.ReadAll(r.Body)
    mu.Lock()
    switch r.URL.Path {
    case "/repos/samestrin/atcr/check-runs":
        _ = json.Unmarshal(raw, &checkBody)
    case "/repos/samestrin/atcr/pulls/12/comments":
        var c map[string]any
        _ = json.Unmarshal(raw, &c)
        comments = append(comments, c)
    }
    mu.Unlock()
    w.WriteHeader(http.StatusCreated)
    _, _ = w.Write([]byte(`{"id":1}`))
}))
```

If two comments are posted concurrently (which they might be), the handler is called twice, and each call holds the mutex. The map append and checkBody assignment are atomic under the lock. Good.

But wait, the check-run is created first (synchronously), then the two comments are posted. Are they posted in parallel? Let me look at postInlineComments:

```go
for _, c := range comments {
    if err := client.CreateReviewComment(cmd.Context(), owner, repo, pr, sha, c); err != nil {
        ...
    }
    posted++
}
```

This is a sequential loop. So the comments are posted one at a time. No race in this test.

49. **`httptest.NewServer` with `srv.Client()`:** This is a real concern. In `client_test.go`:

```go
c := &Client{APIURL: srv.URL, Token: "tok", HTTPClient: srv.Client()}
```

The HTTP client is set to `srv.Client()`. But in the actual `runGithub` function:

```go
client := &ghaction.Client{APIURL: apiURL, Token: token}
```

The HTTP client is not set, so the default is used (30-second timeout). The tests use `srv.Client()` which has its own configuration. Both work. The integration test's fake server is used differently.

50. **The action's default of "high" for `fail-on`:** This means out of the box, the action blocks PRs on HIGH findings. This is a strong default. Some teams might want a different default. But it's documented.

51. **The test `TestGithubCmd_InvalidFailOnIsUsageError`:** Tests that `--fail-on bogus` returns exit 2 (usage error). Good.

52. **What if `OPENROUTER_API_KEY` is empty?** The action.yml declares it as required: `required: true`. GitHub Actions will refuse to run the step if it's empty. So the `atcr review` step would receive an empty env var. The Go code might fail when calling the API. This is a config issue, not a code issue.

53. **What if `GITHUB_TOKEN` is empty?** The action.yml's `github-token` has `default: ${{ github.token }}`. GitHub always provides a token. So this is always set. Good.

54. **`head_sha` in the check run body:** The check run uses `head_sha` (snake_case), which is the GitHub API field name. Good.

55. **The test `TestCreateCheckRun` checks `head_sha`:** The server records the body and the test asserts the snake_case field. Good.

56. **The integration test's `--repo` arg is passed to `execCmdCapture`:** This is a test helper. The actual command-line would be `atcr github --repo samestrin/atcr --sha headsha42 --token ghtoken --api-url <url> --fail-on high --inline-comments --pr 12 2026-06-22_e2e`. This is parsed by cobra. The positional arg is `2026-06-22_e2e`, which is the review dir. Good.

57. **The test `TestGithubCmd_InlineCommentsRequirePR` only tests the case where `--pr` is missing.** What if `--pr 0` is passed? The subcommand checks `if inline && pr <= 0`, so `--pr 0` would also fail. Not tested.

58. **Comment post with a non-anchorable finding:** The test `TestBuildInlineComments` covers this: findings with no file or no line are skipped. Good.

59. **Test for `BuildCheckOutput` with empty findings:** Returns a "no findings" output. Good.

60. **Test for `BuildCheckOutput` with oversized text:** Creates 4000 findings, asserts the output is below the limit and contains "truncated". Good.

61. **Test for `Conclusion`:** Tests the neutral, failure, success, and refuted cases. Good.

62. **Test for `FixAttribution`:** Tests the present, present_simple, absent, empty_name, prose_mention, and empty_evidence cases. Good.

63. **Documentation for `fail-on` empty:** "Leave empty for an informational, non-blocking check." Good.

64. **Documentation for `inline-comments`:** "default false (check + artifacts only)". Good.

65. **Example workflow in docs uses `inline-comments: true`:** But the default is false. The example is correct as an opt-in example.

66. **The `openrouter-api-key` input is required.** This is good - prevents accidental runs without the key.

67. **What about the `atcr reconcile` step?** It runs in the consumer's workspace. The reconciled findings should be in `reconciled/findings.txt` relative to the workspace. The `atcr github` step also runs in the consumer's workspace. Good.

But wait - what's the "workspace" for the action.yml? It's `${{ github.workspace }}`, which is the consumer's checkout. The `atcr review` and `atcr reconcile` steps use the consumer's workspace, so they write to the consumer's `.atcr/` dir or similar. The `atcr github` step also uses the consumer's workspace, so it reads from there. Good.

68. **The `atcr review` step doesn't specify `--output-dir`:** It uses the default. The default is presumably `.atcr/reviews/<date>/` or similar. The `atcr reconcile` step reads from the review dir(s) and writes to `reconciled/findings.txt`. The `atcr github` step reads from `reconciled/findings.txt`.

Hmm, but the action.yml doesn't pass any review dir to `atcr github`. The positional arg is omitted. The subcommand's `anchorDir("")` would use the default (current dir?). This needs to be the consumer's workspace where the reconcile happened. Since `atcr github` runs in the same workspace, this should work.

69. **The `atcr github` subcommand's positional arg is `[id-or-path]`:** If omitted, `anchorDir` is called with empty string. The behavior depends on `anchorDir`. Not visible in the diff. Assuming it defaults to the current dir or `.atcr/reviews/latest/`. The tests use a positional arg (e.g., "2026-06-22_e2e"). The action.yml doesn't pass a positional arg. This might be a problem if `anchorDir("")` doesn't find the reconciled findings.

But this is existing behavior, not introduced by this diff. Not a new issue.

70. **Let me check the `anchorDir` behavior again.** The diff doesn't include `anchorDir`. It's a pre-existing function. The new code uses it, but the function itself isn't new. If the action.yml doesn't pass a positional arg, `anchorDir("")` is called. The behavior is determined by the existing function.

71. **I don't see any obvious bugs in the diff itself.** The code is well-tested, well-documented, and follows good practices.

72. **Let me check the boundary test for ghaction:**

```go
"ghaction":       {"reconcile"},
```

The boundary test ensures that `ghaction` only imports `reconcile` from `internal/`. Looking at the new files:
- `client.go` imports: `bytes, context, encoding/json, fmt, io, net/http, strings, time` (stdlib only)
- `comments.go` imports: `context, fmt, strings, github.com/samestrin/atcr/internal/reconcile` ✓
- `render.go` imports: `fmt, strings, github.com/samestrin/atcr/internal/reconcile` ✓

OK, the boundary test correctly lists `reconcile` as the only allowed internal import. Good.

73. **Let me also check the imports in `cmd/atcr/github.go`:**

```go
import (
    "errors"
    "fmt"
    "os"
    "strings"

    "github.com/samestrin/atcr/internal/ghaction"
    "github.com/samestrin/atcr/internal/reconcile"
    "github.com/spf13/cobra"
)
```

These are the only imports. `cmd/atcr` is the binary entry point, so it can import anything. The boundary test doesn't apply to `cmd/`. Good.

74. **The docs/ci-integration.md update adds a section about the maintained PR Action.** This is a good cross-reference.

75. **The docs/github-action.md is a new file with comprehensive documentation.** It includes usage, inputs, permissions, configuration, rendering details, and a manual smoke test. Good.

OK let me think about real issues now:

**Potential issue 1: The default `fail-on` value in action.yml is "high", but the subcommand's --fail-on flag has no default. If the user sets `fail-on: ''` in their workflow, the bash `if [ -n "$FAIL_ON" ]` skips the flag, and the subcommand uses the empty default (informational). Good.

**Potential issue 2: The default for `inline-comments` is the string "false" in action.yml (boolean input). The bash checks `if [ "$INLINE" = "true" ]`. If a user sets `inline-comments: yes` (non-standard boolean), the check fails and inline comments are not posted. This is technically a bug, but it's a user config issue.

Actually wait, GitHub Actions boolean inputs accept: true, false, True, False, TRUE, FALSE. "yes" is not valid. If a user passes "yes", GitHub Actions might convert it to "false" or treat it as invalid. Either way, the bash check `if [ "$INLINE" = "true" ]` is case-sensitive and only matches lowercase "true". If a user passes "True", the check fails. This could be a minor issue.

But this is a common pattern in composite actions. Not a major concern.

**Potential issue 3: The `--api-url` flag is not passed in the action.yml.** The subcommand uses `GITHUB_API_URL` env var. The runner provides this. For GitHub Enterprise, the runner sets `GITHUB_API_URL` to the GHE URL. So this works automatically. Good.

**Potential issue 4: The `actions/setup-go@v5` action is pinned to a major version, not a SHA.** This is a common security best practice concern. Pinning to a SHA prevents supply chain attacks. The action uses `@v5` which is a floating tag. If the upstream `actions/setup-go` is compromised, the action would pull the malicious code. This is a known security concern in the GitHub Actions ecosystem.

This could be flagged as a medium security issue. But it's a common pattern and the official GitHub docs recommend `@v5` for major version pinning.

**Potential issue 5: The `atcr github` subcommand doesn't handle the case where the `atcr reconcile` step didn't produce any reconciled findings.** The subcommand calls `readReconciledFindings` which would error. The subcommand returns a usage error. The check run is not posted. The job fails. This is a hard fail for missing data, which is reasonable.

But the user might expect "no findings" to be a passing check, not a failure. Hmm, let me think... The `readReconciledFindings` would return an error if the file is missing or malformed. The subcommand returns usage error (exit 2). The job fails with exit 2, which is different from exit 1 (gate failure).

This is a config issue, not a code issue. If the user wants to handle "no reconciled findings" gracefully, they'd need a different command. Not a real bug.

**Potential issue 6: The check run output's `summary` field is the same as `title`:** `Summary: title`. This might not be ideal. GitHub's check run API allows separate title and summary. Using the same for both is wasteful. But it works.

Actually wait, looking at the code:
```go
return CheckOutput{Title: title, Summary: title, Text: b.String()}
```

Both `Title` and `Summary` are the same. This is intentional based on the design? Or a bug? The summary is supposed to be a one-line summary shown below the title. Using the same string for both means the summary duplicates the title. This might be intentional (no need for separate summary if the title is informative) or a minor issue.

**Potential issue 7: The check run output's `summary` is used in `CreateCheckRun`:** Yes, the client sets the summary field. So GitHub would show the title twice on the check run page. Minor visual issue.

**Potential issue 8: The `github_integration_test.go` uses a fixed timestamp `2026-06-22_e2e` as the review dir:** This is fine, just a fixture name.

OK, I've reviewed quite thoroughly. Let me summarize the real issues I see:

1. **Actions/setup-go pinned to `@v5` (floating tag) instead of a SHA** - Security best practice. Could be flagged as medium security concern.

2. **`Summary: title` duplication** - The check run's summary duplicates the title. This is a minor visual concern.

3. **The `--fail-on` default is "high" in action.yml but unspecified in the subcommand.** If the action.yml's bash adds the flag only when `FAIL_ON` is non-empty, then when the user explicitly sets `fail-on: ''`, the subcommand sees no flag and uses the empty default. The subcommand's default behavior with empty `failOn` is "neutral" (informational). So this is consistent.

Wait, looking at the action.yml more carefully:

```yaml
fail-on:
  description: 'Severity threshold for the merge gate (CRITICAL, HIGH, MEDIUM, LOW). Leave empty for an informational, non-blocking check.'
  required: false
  default: 'high'
```

The default is "high". So out of the box, the action blocks on HIGH findings. The user has to explicitly set `fail-on: ''` to make it informational. This is a reasonable default for a code review action.

4. **What about the `permissions:` block in the example workflow?** The docs/github-action.md has:
```yaml
permissions:
  contents: read
  checks: write
  pull-requests: write
```

This sets the job-level permissions. The `actions/checkout@v4` and `samestrin/atcr@v1` steps inherit these. The `pull-requests: write` is only needed for inline comments. If the user doesn't enable inline comments, they could narrow permissions. This is a minor docs issue, not a code issue.

5. **What about the `failure` conclusion and the gate logic?**

In `runGithub`:
```go
if conclusion == "failure" {
    return &codedError{code: exitFailure, err: fmt.Errorf("%d finding(s) at or above %s", failCount, failOn)}
}
```

`failOn` here is the validated, canonical severity (e.g., "HIGH"). The error message says "%d finding(s) at or above %s". This is informative. Good.

6. **What if `failOn` is empty and the conclusion is "neutral"?** The function returns nil. Good.

7. **What if `failOn` is set and `failCount` is 0?** The conclusion is "success" (not "failure"), so the function returns nil. Good.

8. **What if the API call fails?** The function returns a coded error (exit 1). The check run was not posted. The user sees an error. Good.

9. **What if some inline comments fail and some succeed?** The `postInlineComments` function returns nil (success) and logs a warning for each failure. The summary line says "posted N, M skipped". Good.

10. **What if all inline comments fail?** The function returns a coded error (exit 1). Good.

11. **What if there are no findings at all?** `BuildInlineComments` returns an empty slice. `postInlineComments` returns nil immediately. The check run is posted with "success" (or "neutral" if no threshold). Good.

12. **What if a finding has `Line: -1` or `Line: 99999`?** The `BuildInlineComments` function only checks `f.Line <= 0`. So negative lines are skipped. Large positive lines (like 99999) are not validated, but GitHub would return 422 if the line is out of range. The handler logs a warning. The check run is still posted. Good.

13. **What if a finding's `File` contains `..` or other path traversal?** The `BuildInlineComments` function doesn't sanitize the file path. GitHub's API would likely reject it, but the code doesn't validate. Minor concern, not a real bug.

14. **The `cell` function doesn't escape markdown in `Problem` field:** A finding with a problem containing `|` would be replaced with `/`. But a problem containing `*` or `_` would still be interpreted as markdown emphasis. This is a minor rendering concern.

15. **The check run's `output.text` includes a markdown table:** The `text` field in GitHub check runs supports markdown. So the table is rendered. Good.

16. **`fixAttributionPrefix` is a constant:** If the executor stage changes its format, this needs to be updated. The constant is well-documented as mirroring the executor's output. The tests cover the common cases. Good.

Hmm, I don't see many real issues. Let me think about correctness more carefully.

**Correctness issue: The `Conclusion` function returns "neutral" when `failOn` is empty.** This is intentional per the design. But what if the user wants "success" with no findings? The `BuildCheckOutput` function uses the conclusion to render the gate message:

```go
switch conclusion {
case "failure":
    fmt.Fprintf(&b, "**Gate failed:** %d finding(s) at or above the threshold.\n\n", failCount)
case "success":
    b.WriteString("**Gate passed:** no findings at or above the threshold.\n\n")
default:
    b.WriteString("Informational review — no merge gate configured.\n\n")
}
```

For "neutral" (empty threshold), the message is "Informational review — no merge gate configured." Good.

**Correctness issue: The check run's `head_sha` is set to the PR head SHA.** The action.yml uses `${{ github.event.pull_request.head.sha || github.sha }}`. For a `pull_request` event, `github.event.pull_request.head.sha` is the PR head SHA. For other events (e.g., `push`), `github.sha` is used. The subcommand's `envOr(shaFlag, "GITHUB_SHA")` falls back to `GITHUB_SHA` if `--sha` is not set. `GITHUB_SHA` is the runner-provided SHA.

For a `pull_request` event, `GITHUB_SHA` is the merge commit SHA, not the PR head SHA. So if the user runs the action on a `pull_request` event and the env var `GITHUB_SHA` is used, the check run would be reported against the merge commit, not the PR head. This is wrong because check runs on a PR should be against the PR head SHA.

But the action.yml explicitly sets `HEAD_SHA: ${{ github.event.pull_request.head.sha || github.sha }}` and passes it to `atcr github --sha "$HEAD_SHA"`. So `--sha` is always set explicitly. The subcommand uses `--sha` first, then `GITHUB_SHA`. So the merge commit issue is avoided.

Good. No bug here.

**Correctness issue: For a `pull_request` event, the check run should be created against the PR head SHA.** GitHub might reject a check run if the SHA is not part of the PR's commits. The action.yml uses `github.event.pull_request.head.sha`, which is the PR head. Good.

**Correctness issue: For a `pull_request` event with inline comments, the comment's `commit_id` must be a commit in the PR.** The action.yml passes `HEAD_SHA` as the commit_id. The check run uses the same SHA. The comment's `commit_id` is set to `sha` in the postInlineComments call:

```go
if err := client.CreateReviewComment(cmd.Context(), owner, repo, pr, sha, c); err != nil {
```

Where `sha` is the validated head SHA. Good.

**Correctness issue: The check run's `status` is "completed" with a terminal conclusion.** This is intentional per the comment. The check run is posted after the review completes, so there's no in-progress phase. Good.

**Correctness issue: The comment's `side` is "RIGHT"** (post-change side of the diff). The action.yml uses `pull_request` event, so the diff is the PR's diff. The comments are anchored to the post-change side. Good.

OK, I've done a thorough review. Let me list the issues I want to flag:

1. **Security: actions/setup-go@v5 pinned to floating tag instead of SHA.** This is a supply chain concern. Recommend pinning to a commit SHA.

2. **Correctness/Minor: Check run Summary duplicates Title.** The `Summary: title` in `BuildCheckOutput` causes the same string to appear twice on the check run page. Could be a more informative summary.

3. **Maintainability: Markdown injection in cell() function.** The `cell` function only escapes `|`, `\n`, `\r`. Other markdown characters (e.g., `*`, `_`, `[`, `]`, backtick) are not escaped. A malicious or poorly-formatted finding could inject markdown in the check run output.

Wait, this is a real concern. The check run output is rendered as markdown. A finding with a problem like "[click here](http://evil.com)" would render as a link. This is a phishing risk in the check run output.

Hmm, but the findings come from the LLM-based review. A prompt injection in the code could cause the LLM to produce malicious markdown. This is a real risk.

Let me flag this as a security concern.

4. **The `--check-name` and other inputs are not validated:** The user could pass any string. The check name "atcr" is short, but a user could pass a 1000-character string. Not a security concern, but a maintainability concern.

5. **Documentation mentions "leave empty" for `fail-on` but the default is "high":** This is clear in the docs, but the default of "high" is opinionated. Some users might prefer the default to be "neutral" (informational) and require explicit threshold for blocking. Minor docs/design concern.

OK, let me finalize my findings. I'll focus on the most important issues:

1. **HIGH/MEDIUM: Markdown injection in check run output.** Findings with markdown in the problem field could inject links or other content in the check run. Should escape markdown in the `cell` function.

Actually, let me think about this more carefully. The findings come from a multi-model review. The LLM could be prompt-injected via the code being reviewed. If the code contains a string that tricks the LLM into producing a finding with `[evil](http://phishing.com)`, the check run would render a phishing link.

This is a real concern. The `cell` function should escape or strip markdown.

2. **MEDIUM: actions/setup-go@v5 not pinned to SHA.** Supply chain concern. Common best practice is to pin to SHA.

3. **LOW: Check run Summary duplicates Title.** Minor UX issue.

4. **LOW: The `Summary` field in `BuildCheckOutput` is set to the same value as `Title`.** Same as #3.

5. **LOW: Comment body's "ATCR found: <problem>." puts a period after the problem.** If the problem ends with a period, there are two periods. Not a bug, but could be cleaner.

Actually, the `cell` function is used for the check run table, but the comment body in `commentBody` doesn't escape the problem. Let me check:

```go
func commentBody(f reconcile.JSONFinding) string {
    var b strings.Builder
    fmt.Fprintf(&b, "ATCR found: %s.", strings.TrimSpace(f.Problem))
    if fix := strings.TrimSpace(f.Fix); fix != "" {
        fmt.Fprintf(&b, " Fix: %s.", fix)
    }
    if who := FixAttribution(f.Evidence); who != "" {
        fmt.Fprintf(&b, " Suggested by: %s.", who)
    }
    return b.String()
}
```

No escaping. The problem is inserted as-is. If the problem is "[evil](http://phishing.com)", the comment body would be "ATCR found: [evil](http://phishing.com).", which renders as a clickable link.

This is the same markdown injection risk as in the check run. Worse, comments are typically more visible than check runs.

So the comment body also has a markdown injection risk.

Let me also check the `Fix` and `Suggested by` fields. Same issue.

OK so the main issue is markdown injection in user-controlled fields. Let me flag this as a security concern.

Let me also think about other concerns:

- **Token exposure in logs:** The `GITHUB_TOKEN` is set via `env:` in the action.yml. GitHub Actions masks secrets in logs. The `OPENROUTER_API_KEY` is also masked. Good.

- **Script injection in shell commands:** The action.yml uses `env:` to pass values, not direct interpolation. Good. (The comment in the file mentions this explicitly.)

- **Path traversal in file paths:** The findings have file paths from the diff. A malicious finding could have a file path with `..`. The code doesn't validate. The `CreateReviewComment` API would likely reject invalid paths, but the code doesn't pre-validate. Minor concern.

- **Rate limiting:** The client doesn't implement rate limit handling. If the API returns 429, the comment fails. The postInlineComments function logs and continues. The check run is still posted. The merge gate logic doesn't depend on comment posting. So rate limiting is handled gracefully.

- **The `head_sha` field for the check run:** If a finding is on a deleted file, the comment's `commit_id` is valid (it's the head SHA), but the line might not be in the diff. GitHub would return 422. The handler logs a warning. The check run is still posted. Good.

OK let me finalize my list of issues. I want to be conservative and only flag real issues.

**Real issues to flag:**

1. **Markdown injection in comment body (`internal/ghaction/comments.go`):** The `commentBody` function doesn't escape the `Problem`, `Fix`, or `Suggested by` fields. An attacker could inject markdown (e.g., links, images, mentions) into the comment. This is a security concern because comments are user-visible and could be used for phishing or social engineering.

   The fix is to escape or strip markdown from the user-controlled fields before inserting them into the comment body.

2. **Markdown injection in check run output (`internal/ghaction/render.go`):** The `cell` function only escapes `|`, `\n`, `\r`. Other markdown characters are not escaped. A finding with `[text](url)` in the `Problem` field would render as a link in the check run output.

   The fix is to escape or strip markdown in the `cell` function, or use HTML entity encoding.

3. **Check run Summary duplicates Title (`internal/ghaction/render.go`):** The `BuildCheckOutput` function sets `Summary: title`. This causes the same string to appear twice on the check run page. The summary should be a different, more informative string (e.g., "1 finding blocks the merge" or "0 findings, all below threshold").

   This is a minor UX issue.

4. **Actions/setup-go pinned to floating tag (`action.yml`):** The `actions/setup-go@v5` is pinned to a major version tag, not a commit SHA. This is a supply chain risk. A compromise of the `actions/setup-go` repository would allow malicious code to run in the action.

   The fix is to pin to a specific commit SHA.

Hmm, on reflection, I'm not sure #4 is a real issue in this context. Most users accept floating major versions. The official GitHub documentation recommends `@vN` for major version pinning. Pinning to SHA is a hardening step. I'll flag it as low.

For #1 and #2, markdown injection is a real concern. Let me think about the severity.

The findings come from an LLM-based review. A prompt injection in the code being reviewed could trick the LLM into producing a finding with malicious markdown. The malicious markdown would then be rendered in the comment or check run.

This is a real security concern. The attacker would need to:
1. Submit a PR with prompt injection in the code.
2. The LLM is tricked into producing a finding with malicious markdown.
3. The comment/check run is posted with the malicious markdown.

The attack is plausible and the impact is phishing/social engineering. The severity is medium.

OK, let me write up my findings.

Wait, one more thing. Let me check the test for `TestBuildInlineComments`:

```go
{Severity: "LOW", File: "b.go", Line: 3, Problem: "minor", Fix: "", Evidence: "snippet"},
...
// No fix → no Fix clause, no attribution.
assert.Contains(t, comments[1].Body, "ATCR found: minor")
assert.NotContains(t, comments[1].Body, "Fix:")
assert.NotContains(t, comments[1].Body, "Suggested by:")
```

The body is "ATCR found: minor." - the test checks "ATCR found: minor" (without period). `Contains` matches. Good.

But wait, the test `assert.NotContains(t, comments[1].Body, "Fix:")` checks that "Fix:" is not in the body. The body is "ATCR found: minor." - no "Fix:". Good.

OK, I think I've covered everything. Let me write up the findings.

Actually, let me also check if there's any issue with how the action handles the case where the PR has no changes (e.g., a no-op PR). The `atcr review` would find no findings, `atcr reconcile` would produce an empty reconciled stream, and `atcr github` would post a "no findings" check. The check would pass. Good.

What if the PR has no commits yet? The `atcr review` would fail to resolve the range. The `atcr github` step might not be reached. The user would see an error from `atcr review`. Good.

What if the consumer's workflow has `permissions:` not set? The default `GITHUB_TOKEN` has read-only permissions. The check run creation would fail with 403. The job would fail. The user would see an error. This is expected behavior.

OK, I think I've covered the main issues. Let me write up the findings.

Hmm, on the markdown injection concern - is this really a security issue? Let me think about it more carefully.

The findings are produced by the LLM review. The LLM is given the code (or diff) and asked to produce findings. The problem, fix, and evidence fields are LLM-generated.

A malicious code change could include a prompt injection that tricks the LLM into producing a finding like:
- Problem: "Performance: see [the docs](http://evil.com) for details"
- Fix: "Apply the [patch](http://evil.com/patch.sh)"

These would be rendered as clickable links in the comment and check run.

A user reading the comment might click the link, thinking it's part of the legitimate review. This is a phishing/social engineering attack.

The severity is medium because:
- The attacker needs to submit a PR.
- The reviewer (human) needs to be tricked into clicking.
- The mitigation is user education (don't click links in review comments).

But it's a real concern for a security-focused tool. The fix is straightforward: escape markdown in the problem/fix fields.

OK, let me also consider: is the `Suggested by: <executor>` field safe? The executor name is parsed from the evidence field's "fix by <name>" token. The `name` is whatever the LLM/executor put there. If the executor stage writes `fix by [opus](http://evil.com)`, the comment would render as "Suggested by: [opus](http://evil.com).", which is a link.

The `CutPrefix` and `TrimSpace` don't escape markdown. The name is inserted as-is.

So all three fields (problem, fix, executor name) are vulnerable to markdown injection.

Let me also think: is this attack realistic? The LLM produces structured output. The JSON format constrains the fields. But the content of the fields is free-form. A prompt injection could produce any content.

OK, I'll flag this as a medium security issue.

Let me also look at the cell function in render.go more carefully:

```go
func cell(s string) string {
    s = strings.ReplaceAll(s, "|", "/")
    s = strings.ReplaceAll(s, "\r\n", " ")
    s = strings.ReplaceAll(s, "\n", " ")
    s = strings.ReplaceAll(s, "\r", " ")
    return strings.TrimSpace(s)
}
```

Only pipes and newlines are escaped. The check run's `text` is rendered as markdown. Other markdown (links, emphasis, code, etc.) is not escaped. This is a real issue.

OK, let me write up the findings. I'll be focused and actionable.

Final list of issues:

1. **MEDIUM: Markdown injection in inline comment body** (`internal/ghaction/comments.go:43-55`)
   - Problem: `commentBody` inserts `Problem`, `Fix`, and `Suggested by` fields without escaping markdown. A prompt-injected finding could inject links, images, or mentions into the PR comment.
   - Fix: Escape or strip markdown (links, emphasis, etc.) from these fields before insertion.
   - Category: security
   - Evidence: `fmt.Fprintf(&b, "ATCR found: %s.", strings.TrimSpace(f.Problem))`

2. **MEDIUM: Markdown injection in check run output** (`internal/ghaction/render.go:74-80`)
   - Problem: `cell` only escapes `|`, `\n`, `\r`. A finding with markdown (e.g., `[text](url)`) would render as a link in the check run.
   - Fix: Escape markdown in the `cell` function, or use HTML entities.
   - Category: security
   - Evidence: `s = strings.ReplaceAll(s, "|", "/")` - only pipes escaped.

3. **LOW: Check run Summary duplicates Title** (`internal/ghaction/render.go:140`)
   - Problem: `BuildCheckOutput` sets `Summary: title`, causing the same string to appear in both fields.
   - Fix: Use a more informative summary string (e.g., the gate verdict).
   - Category: maintainability
   - Evidence: `return CheckOutput{Title: title, Summary: title, Text: b.String()}`

4. **LOW: actions/setup-go pinned to floating tag** (`action.yml:55`)
   - Problem: `actions/setup-go@v5` is a major version tag, not a SHA. Supply chain risk.
   - Fix: Pin to a specific commit SHA.
   - Category: security
   - Evidence: `uses: actions/setup-go@v5`

Actually, on reflection, I should be more careful about what's in scope. The sprint plan is about Epic 7.3 (GitHub Action / PR Integration). All my findings are about the new code in this diff. So they're in scope.

But let me also think about whether the markdown injection is a real concern. The findings are produced by the LLM review. The review is run on the consumer's PR. A malicious PR submitter could prompt-inject the LLM.

However, the markdown injection only affects the rendered output of the check run/comment. It doesn't give the attacker code execution or data exfiltration. The impact is limited to phishing/social engineering.

I'll keep the severity at medium.

Actually, let me reconsider. The action runs in the consumer's CI. The attacker (PR submitter) controls the code being reviewed. The findings are derived from the code via the LLM. The attacker can:
- Submit a PR with prompt injection.
- The LLM produces a finding with malicious markdown.
- The comment is posted to the PR.
- The PR author (or other reviewers) see the malicious comment.

The PR author is typically the developer who opened the PR. They might click the malicious link thinking it's part of the review.

This is a real phishing attack vector. The severity is medium.

OK, let me finalize.

Wait, I should also check the `fixAttributionPrefix` and `FixAttribution` for any issues.

```go
const fixAttributionPrefix = "fix by "

func FixAttribution(evidence string) string {
    for _, seg := range strings.Split(evidence, "; ") {
        seg = strings.TrimSpace(seg)
        if rest, ok := strings.CutPrefix(seg, fixAttributionPrefix); ok {
            if name := strings.TrimSpace(rest); name != "" {
                return name
            }
        }
    }
    return ""
}
```

This is used in `commentBody` to get the "Suggested by" name. The name is inserted as-is into the comment. If the name is `[opus](http://evil.com)`, the comment would render as a link.

This is the same markdown injection risk as above.

OK, let me write the findings.

Actually, I want to double-check the comment body for `Suggested by:`:

```go
if who := FixAttribution(f.Evidence); who != "" {
    fmt.Fprintf(&b, " Suggested by: %s.", who)
}
```

The `who` is the executor name from the evidence field. Inserted as-is. Markdown injection possible.

OK, final findings:

1. Markdown injection in comment body (problem, fix, executor name)
2. Markdown injection in check run output (problem field via cell)
3. Check run Summary duplicates Title
4. actions/setup-go not pinned to SHA

Let me also think about whether the issue of `head_sha` could be a problem.

In the action.yml:
```yaml
HEAD_SHA: ${{ github.event.pull_request.head.sha || github.sha }}
```

For a `pull_request` event, `github.event.pull_request.head.sha` is the PR head SHA. For other events (e.g., `push`), `github.sha` is the runner-provided SHA. The `||` falls back if the first is empty.

The check run's `head_sha` must be a commit in the repository. For a `pull_request` event, the PR head SHA is the head of the PR's branch. For a `push` event, `github.sha` is the commit being pushed. Both are valid.

Good. No issue.

OK, let me also think about whether the `atcr github` subcommand's `--api-url` is being properly used. The action.yml doesn't pass `--api-url`. The subcommand's `envOr(apiURLFlag, "GITHUB_API_URL")` falls back to the env var. The runner provides `GITHUB_API_URL` (e.g., `https://api.github.com` for github.com, or the Enterprise URL for GHE).

If the user is on GitHub Enterprise, `GITHUB_API_URL` is set to the Enterprise URL. The subcommand would use it. The `DefaultAPIURL` is only used if both the flag and env var are empty.

Good. No issue.

OK, I'm done analyzing. Let me write up the findings.

Actually, one more concern. Let me check the `checkBody` assertion in the integration test:

```go
mu.Lock()
defer mu.Unlock()

require.NotNil(t, checkBody, "a check run must be posted")
assert.Equal(t, "completed", checkBody["status"])
assert.Equal(t, "failure", checkBody["conclusion"])
assert.Equal(t, "headsha42", checkBody["head_sha"])
output, _ := checkBody["output"].(map[string]any)
require.NotNil(t, output)
assert.Contains(t, output["text"], "internal/auth/token.go:42")
```

The test asserts that the check run body contains "internal/auth/token.go:42" in the `output.text` field. This is the rendered check run output. The `BuildCheckOutput` function renders a table with the location. Good.

But the test also asserts:
```go
require.Len(t, comments, 2, "one inline comment per anchorable finding")
```

Two comments are expected. The fixture has two findings, both with valid file/line. So two comments are posted. Good.

OK, I'm really done now. Let me write up the findings.

Hmm, let me also think about whether there's an issue with the comment body's "Suggested by: opus" parsing. The fixture has `"evidence":"bruce: token, _ := jwt.Parse(raw); fix by opus"`. Splitting by "; " gives ["bruce: token, _ := jwt.Parse(raw)", "fix by opus"]. The second segment starts with "fix by ", so the prefix is cut, giving "opus". Good.

But wait, the test fixture has a semicolon in the problem description. Let me check:
```json
{"severity":"HIGH","file":"internal/auth/token.go","line":42,"problem":"JWT signature not verified before claims are read","fix":"Call jwt.Verify before decoding claims","category":"security","est_minutes":20,"evidence":"bruce: token, _ := jwt.Parse(raw); fix by opus","reviewers":["bruce","greta"],"confidence":"HIGH"}
```

The evidence is "bruce: token, _ := jwt.Parse(raw); fix by opus". Splitting by "; " gives ["bruce: token, _ := jwt.Parse(raw)", "fix by opus"]. The first segment doesn't start with "fix by ". The second segment starts with "fix by ", so the prefix is cut, giving "opus". Good.

OK, final findings:

1. **MEDIUM: Markdown injection in inline comment body** (`internal/ghaction/comments.go:43-55`)
2. **MEDIUM: Markdown injection in check run output** (`internal/ghaction/render.go:74-80`)
3. **LOW: Check run Summary duplicates Title** (`internal/ghaction/render.go:140`)
4. **LOW: actions/setup-go pinned to floating tag** (`action.yml:55`)

Let me also think about whether there are any other concerns I'm missing.

What about the case where the consumer's workflow has `permissions: read-all` (or no permissions block)? The `GITHUB_TOKEN` would have read-only permissions. The check run creation would fail. The job would fail with an error.

This is expected behavior. The docs/github-action.md says to set the correct permissions. Good.

What about the case where the consumer uses GitHub Enterprise Server with a self-hosted runner? The `GITHUB_API_URL` would be set to the Enterprise URL. The subcommand would use it. The `GITHUB_TOKEN` is a personal access token or GitHub App token. Both are supported by the REST API.

Good.

What about the case where the consumer's PR base is a fork? The PR head SHA is the fork's head. The check run's `head_sha` is the PR head SHA. The check run is created on the consumer's repo, not the fork's. The API should support this.

Good.

What about the case where the consumer's PR has merge conflicts? The PR is not mergeable. The check run can still be created. The merge gate is honored regardless of mergeability.

Good.

OK, I'm really done. Let me write up the findings.

Wait, I want to also consider: the `check-run` API endpoint is `/repos/{owner}/{repo}/check-runs`. The body includes `head_sha`, `name`, `status`, `conclusion`, `output`. The response is a 201 with the check run ID. The code doesn't use the response ID. So no follow-up actions are taken. Good.

The `pulls/{pr}/comments` endpoint is `/repos/{owner}/{repo}/pulls/{pr}/comments`. The body includes `body`, `commit_id`, `path`, `line`, `side`. The response is a 201 with the comment ID. The code doesn't use the response ID. Good.

OK, final findings.

Actually, let me also check: is there any issue with how the `Conclusion` function handles the case where the `failOn` threshold is invalid (not a valid severity)?

```go
func Conclusion(findings []reconcile.JSONFinding, failOn string) (string, int) {
    if strings.TrimSpace(failOn) == "" {
        return "neutral", 0
    }
    count := 0
    for _, f := range findings {
        if isRefuted(f) {
            continue
        }
        if reconcile.AtOrAbove(f.Severity, failOn) {
            count++
        }
    }
    if count > 0 {
        return "failure", count
    }
    return "success", 0
}
```

The `failOn` is assumed to be valid. If it's not (e.g., "BOGUS"), `reconcile.AtOrAbove` would return false for all findings, and the function would return "success". This is a silent failure - the user set an invalid threshold but the gate passes.

But the `runGithub` function validates `failOn` before calling `Conclusion`:
```go
if failOn != "" {
    canon, err := reconcile.ParseSeverity(failOn)
    if err != nil {
        return usageError(err)
    }
    failOn = canon
}
```

So `failOn` is always valid (or empty) when `Conclusion` is called. Good.

But `BuildCheckOutput` is called with the same `failOn`. It also calls `Conclusion` internally:
```go
conclusion, failCount := Conclusion(findings, failOn)
```

And it uses `reconcile.ParseSeverity(failOn)` for the title:
```go
threshold, _ := reconcile.ParseSeverity(failOn)
title = fmt.Sprintf("atcr — %d finding(s), %d at/above %s", total, failCount, threshold)
```

The error is ignored. But since `failOn` is validated in `runGithub`, the error would never occur here. Good.

OK, I'm really done. Let me write up the findings.

Hmm, one more thought. The `reconcile.AtOrAbove` function - is it case-sensitive? Let me check the existing code... The diff doesn't include this function. It's a pre-existing function. The `ParseSeverity` function canonicalizes the severity (e.g., "high" -> "HIGH"). So `failOn` is always uppercase. The `f.Severity` is also uppercase (per the JSON findings format). So `AtOrAbove` is called with uppercase strings. Good.

OK, final findings. Let me write them up.

Let me format them properly:

1. MEDIUM|internal/ghaction/comments.go:43|Markdown injection via Problem/Fix/executor fields|Backtick-escape or strip markdown in user-controlled fields before rendering|security|15|fmt.Fprintf(&b, "ATCR found: %s.", strings.TrimSpace(f.Problem)) inserts LLM output raw|reviewer

2. MEDIUM|internal/ghaction/render.go:74|Markdown injection in check run table cell|Backtick-escape or strip markdown (links, emphasis, code) in cell()|security|15|cell() only escapes | and newlines; [text](url) renders as link|reviewer

3. LOW|internal/ghaction/render.go:140|Check run Summary duplicates Title|Use a distinct summary string (e.g., gate verdict)|maintainability|5|return CheckOutput{Title: title, Summary: title, Text: b.String()}|reviewer

4. LOW|action.yml:55|actions/setup-go pinned to floating tag|Pin to a specific commit SHA for supply chain hardening|security|5|uses: actions/setup-go@v5|reviewer

Actually, for the markdown injection, I should be more specific about the fix. The standard way to escape markdown in GitHub is to:
- Wrap in backticks for code: `` `text` ``
- Use HTML entities: `&#91;text&#93;(url)` 
- Strip the markdown: just remove the brackets and parens

The simplest fix is to strip or backtick-escape. Let me suggest stripping or backtick-wrapping.

For the check run table cell, the simplest fix is to backtick-wrap the cell content. This is a common pattern in GitHub markdown tables.

For the comment body, the simplest fix is to backtick-escape the problem/fix/executor fields.

OK, let me finalize the findings.

Actually wait - I want to reconsider whether the markdown injection is a HIGH or MEDIUM issue.

The attack vector is:
1. Attacker submits a PR with prompt injection in the code.
2. The LLM is tricked into producing a finding with malicious markdown.
3. The check run/comment is posted with the malicious markdown.
4. The PR author (developer) clicks the malicious link, thinking it's part of the legitimate review.

The impact is:
- Phishing/social engineering: the developer might enter credentials on a fake login page.
- Malware delivery: the link might download malware.

The likelihood depends on:
- How often developers click links in review comments.
- How often PR submitters are attackers (most are legitimate contributors).
- How effective the prompt injection needs to be (the LLM needs to produce the malicious markdown in the structured output).

For a security-focused tool (ATCR reviews code for security issues), the irony of a security tool being used for phishing is notable. The severity should be MEDIUM at least.

OK, I'll go with MEDIUM.

Let me also consider: is there a way to validate the file path before posting a comment? The findings have a `File` field. The code doesn't validate the path. A finding with `File: "../../../etc/passwd"` or `File: "node_modules/../../malicious.js"` would be sent to the GitHub API. The API would likely reject it (since it's not in the PR's diff), but the code doesn't pre-validate.

This is a minor concern. The API's 422 response is handled gracefully. Not a real issue.

OK, final findings. Let me write them up.

Actually, I want to flag one more thing. The `Summary: title` duplication - let me re-read the code:

```go
return CheckOutput{Title: title, Summary: title, Text: b.String()}
```

The `Title` and `Summary` are the same string. The GitHub check run UI shows:
- Title: bold text at the top
- Summary: secondary text below the title
- Text: long-form markdown body

Having the same string in both title and summary is a minor UX issue. The summary should provide additional context (e.g., "1 finding blocks the merge" or "All findings below threshold").

This is a minor maintainability concern. I'll flag it as LOW.

OK, let me also look at the `// Set up Go` step:

```yaml
- name: Set up Go
  uses: actions/setup-go@v5
  with:
    go-version: ${{ inputs.go-version }}
    cache: true
    cache-dependency-path: ${{ github.action_path }}/go.sum
```

The `cache: true` enables Go module caching. The `cache-dependency-path` is the action's `go.sum`. This caches based on the action's dependencies, not the consumer's. The action's dependencies don't change between consumer invocations (they're the action's own deps). So the cache is effective.

But the consumer's code (which is reviewed by atcr) is not cached. This is fine - the consumer's code doesn't affect the build.

Good. No issue.

OK, I'm really done. Final findings:

1. MEDIUM|internal/ghaction/comments.go:43|Markdown injection via Problem/Fix/executor fields|Backtick-escape or strip markdown in user-controlled fields before rendering|security|15|fmt.Fprintf(&b, "ATCR found: %s.", f.Problem) inserts LLM output raw|reviewer

2. MEDIUM|internal/ghaction/render.go:74|Markdown injection in check run table cell|Backtick-escape or strip markdown in cell()|security|15|cell() only escapes | and newlines; [text](url) renders as link|reviewer

3. LOW|internal/ghaction/render.go:140|Check run Summary duplicates Title|Use a distinct summary string (e.g., gate verdict)|maintainability|5|return CheckOutput{Title: title, Summary: title, Text: b.String()}|reviewer

4. LOW|action.yml:55|actions/setup-go pinned to floating tag|Pin to a specific commit SHA for supply chain hardening|security|5|uses: actions/setup-go@v5|reviewer

Wait, the user said to use a specific format. Let me check the exact format:

```
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER
```

OK that's what I have. Let me also pick a REVIEWER name. The instructions say "Your agent name". I'll use "reviewer" or something descriptive.

Let me also check the EVIDENCE format - "< 200 chars" and "keep it under ~20 words". Let me make sure my evidence is concise.

1. Evidence: "fmt.Fprintf(&b, "ATCR found: %s.", f.Problem) inserts LLM output raw" - 9 words, OK.

2. Evidence: "cell() only escapes | and newlines; [text](url) renders as link" - 9 words, OK.

3. Evidence: "return CheckOutput{Title: title, Summary: title, Text: b.String()}" - 7 words, OK.

4. Evidence: "uses: actions/setup-go@v5" - 4 words, OK.

OK, my findings are well-formatted. Let me finalize.

Actually, I realize I should double-check the file paths. The diff has:
- `internal/ghaction/client.go`
- `internal/ghaction/client_test.go`
- `internal/ghaction/comments.go`
- `internal/ghaction/comments_test.go`
- `internal/ghaction/render.go`
- `internal/ghaction/render_test.go`

For my findings:
1. comments.go line 43 - let me verify. The `commentBody` function starts at line 40 (based on the diff). The first `fmt.Fprintf` is on line 43. Good.

Actually, looking at the diff more carefully:

```go
38	func commentBody(f reconcile.JSONFinding) string {
39		var b strings.Builder
40		fmt.Fprintf(&b, "ATCR found: %s.", strings.TrimSpace(f.Problem))
```

Line numbers might be off. Let me look at the actual diff:

```
+// commentBody assembles the inline-comment text for a single finding, following
+// the epic's AC3 contract: "ATCR found: <problem>. Fix: <fix>. Suggested by:
+// <executor>", with the Fix and attribution clauses omitted when their source
+// data is absent.
+func commentBody(f reconcile.JSONFinding) string {
+	var b strings.Builder
+	fmt.Fprintf(&b, "ATCR found: %s.", strings.TrimSpace(f.Problem))
+	if fix := strings.TrimSpace(f.Fix); fix != "" {
+		fmt.Fprintf(&b, " Fix: %s.", fix)
+	}
+	if who := FixAttribution(f.Evidence); who != "" {
+		fmt.Fprintf(&b, " Suggested by: %s.", who)
+	}
+	return b.String()
+}
```

The line numbers in the new file would be:
- Line 40: `func commentBody(...)`
- Line 41: `var b strings.Builder`
- Line 42: `fmt.Fprintf(&b, "ATCR found: %s.", ...)`

Actually I don't know the exact line numbers. The diff doesn't show line numbers for the new file. Let me just reference the function name or approximate line.

Looking at the diff, the new file `internal/ghaction/comments.go` is 79 lines. The `commentBody` function is roughly in the middle.

Let me use approximate line numbers. The function `commentBody` starts around line 39 in the new file. The first `fmt.Fprintf` is around line 42. The `Fix:` and `Suggested by:` insertions are around lines 44 and 47.

I'll use `internal/ghaction/comments.go:43` for the markdown injection in comment body.

For the cell function in render.go:

```
+// cell neutralizes a value for safe inclusion in a single markdown table cell:
+// a literal pipe would break the column grammar, and an embedded newline would
+// split the row across physical lines.
+func cell(s string) string {
+	s = strings.ReplaceAll(s, "|", "/")
+	s = strings.ReplaceAll(s, "\r\n", " ")
+	s = strings.ReplaceAll(s, "\n", " ")
+	s = strings.ReplaceAll(s, "\r", " ")
+	return strings.TrimSpace(s)
+}
```

The `cell` function is in render.go. The function starts around line 72. The `ReplaceAll` calls are around lines 75-78. I'll use `internal/ghaction/render.go:74` for the markdown injection in cell.

For the Summary duplication:

```
+	return CheckOutput{Title: title, Summary: title, Text: b.String()}
+}
```

This is at the end of `BuildCheckOutput`. The function is around line 80-140. The return statement is around line 138. I'll use `internal/ghaction/render.go:140` (close to the end).

For the action.yml:
```
+    - name: Set up Go
+      uses: actions/setup-go@v5
+      with:
+        go-version: ${{ inputs.go-version }}
+        cache: true
+        cache-dependency-path: ${{ github.action_path }}/go.sum
```

The `uses: actions/setup-go@v5` is around line 55-60 in the new file. I'll use `action.yml:55` (approximate).

OK, line numbers are approximate. The findings are still actionable.

Let me finalize the findings. I'll also use a consistent REVIEWER name. I'll use "atcr-reviewer" or just "reviewer". Looking at the example, it uses "bruce" (the agent name from the system prompt example). I'll use a simple agent name.

Actually, looking at the system prompt more carefully, there's no specific agent name. The example uses "bruce" but that's just an example. I'll use "atcr-reviewer" to be specific.

Wait, let me re-read the instructions. "REVIEWER: Your agent name". This is the name of the reviewing agent. Since I'm an AI doing the review, I could use "ai" or "claude" or "gpt" or something. But the example uses "bruce" which is a human name. Let me use "atcr-reviewer" to be specific to the context.

Actually, looking at the example more carefully: `HIGH|src/auth.go:42|Missing input validation|Add length check|security|5|user input passed directly to query|bruce`. The reviewer is "bruce". This is just a placeholder name. I'll use "atcr-reviewer".

OK, final findings:

1. MEDIUM|internal/ghaction/comments.go:43|Markdown injection via Problem/Fix/executor in comment body|Backtick-escape or strip markdown in user-controlled fields before rendering|security|15|fmt.Fprintf inserts LLM output (Problem/Fix) raw into markdown|atcr-reviewer

2. MEDIUM|internal/ghaction/render.go:74|Markdown injection in check run table cell|Backtick-escape or strip markdown in cell()|security|15|cell() only escapes | and newlines; [text](url) renders as link|atcr-reviewer

3. LOW|internal/ghaction/render.go:140|Check run Summary duplicates Title|Use a distinct summary string (e.g., gate verdict text)|maintainability|5|return CheckOutput{Title: title, Summary: title, Text: b.String()}|atcr-reviewer

4. LOW|action.yml:55|actions/setup-go pinned to floating tag|Pin to a specific commit SHA for supply chain hardening|security|5|uses: actions/setup-go@v5|atcr-reviewer

OK these are my findings. Let me write them out.
</think>

MEDIUM|internal/ghaction/comments.go:43|Markdown injection via Problem/Fix/executor fields in PR comment body|Backtick-escape or strip markdown from user-controlled fields before rendering|security|15|fmt.Fprintf inserts f.Problem/f.Fix raw into markdown rendered on PR|atcr-reviewer
MEDIUM|internal/ghaction/render.go:74|Markdown injection in check-run table cells via Problem/Confidence|Sanitize markdown in cell() (e.g., wrap in backticks, strip links/emphasis)|security|15|cell() only escapes | and newlines; [text](url) renders as clickable link|atcr-reviewer
LOW|internal/ghaction/render.go:140|Check-run Summary duplicates Title verbatim|Use a distinct summary (e.g., gate verdict line) so the check UI shows different text|maintainability|5|return CheckOutput{Title: title, Summary: title, Text: b.String()}|atcr-reviewer
LOW|action.yml:55|actions/setup-go pinned to floating major tag, not commit SHA|Pin to a full commit SHA to harden the composite action's supply chain|security|5|uses: actions/setup-go@v5|atcr-reviewer