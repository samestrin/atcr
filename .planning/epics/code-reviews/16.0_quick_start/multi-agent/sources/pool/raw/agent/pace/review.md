We are reviewing the diff for performance issues only. We must output findings in the specified format.

We are only allowed to report performance findings (CATEGORY must be `performance`). Each finding must name a concrete, measurable runtime cost.

We are to review only the changed regions (the diff). We must not report on unchanged code.

We are to emit at most ~15 findings, highest impact first.

Let's go through the diff and look for performance issues.

We'll break down the diff by file and look for:

1. O(n²) or worse: nested loops over collections, repeated linear searches
2. Unnecessary allocations: objects built in hot loops, string concatenation in loops
3. Repeated work: same computation done multiple times, missing memoization
4. N+1 queries: database/network calls inside loops, missing batch operations
5. Large copies: copying large structs/arrays where references would work; blocking I/O in async paths; holding locks too long; unbounded caches and other leaks; wrong container for the access pattern

We must avoid micro-optimizations and speculative "could be faster" without a hot-path or scale argument.

We must not report on test files unless it's a real performance problem (e.g., a fixture build that dominates the suite).

We must report each distinct issue once, and if a pattern repeats, we put the count in EVIDENCE.

We must not flatter or summarize.

Let's examine the changed files:

1. .github/workflows/refresh-synthetic-manifest.yml (new file) - This is a GitHub Actions workflow. We are to look for performance issues in the code that is being run. However, note that this is a workflow file (YAML) and not Go code. The performance issues we are looking for are in the runtime of the software, not in the workflow itself. The workflow is just setting up and running a command. We are to review the code that is being changed, but note that the workflow is not the software we are reviewing for performance? Actually, the task says: "You are pace, the panel's performance reviewer". We are reviewing the atcr project. The workflow is part of the project, but it's a CI workflow. However, the performance we care about is the runtime of the atcr tool, not the CI workflow. But note: the workflow runs `go run ./cmd/refresh-manifest` and then moves a file. We are to look for performance issues in the atcr tool, but the workflow is not the tool. However, the workflow is part of the project and we are to review the changed regions. But note the hard rule: we are to report performance findings only. The workflow file is YAML and does not contain Go code. We cannot find performance issues in YAML that affect the runtime of the atcr tool? Actually, the workflow is just setting up the environment and running a command. The performance of the workflow is not the concern of the atcr tool's performance. However, note that the workflow is part of the project and we are to review the changed code. But the performance we are reviewing is for the atcr tool. The workflow is not the tool. Therefore, we should not look for performance issues in the workflow file because they are not about the atcr tool's runtime. However, note that the workflow runs a Go command (`go run ./cmd/refresh-manifest`). We are to look at the Go code that is being run by that command? But note: the workflow is new, and the Go code it runs is in `cmd/refresh-manifest/main.go` (which is also new). We should look at that Go file for performance issues.

But note: the scope is "Review only the changed regions". The changed regions include the workflow file and the Go file. We are to look for performance issues in the changed Go code.

So let's look at the new Go files:

- cmd/refresh-manifest/main.go
- internal/quickstart/manifest.go
- internal/quickstart/manifest_test.go
- internal/quickstart/refresh.go
- internal/quickstart/refresh_test.go
- internal/quickstart/synthetic.json (JSON data, not code)
- internal/quickstart/workflow.go
- internal/quickstart/workflow_test.go
- cmd/atcr/quickstart.go
- cmd/atcr/quickstart_test.go
- cmd/atcr/main.go (added a new subcommand)
- cmd/atcr/main_test.go (updated test)
- .github/workflows/refresh-synthetic-manifest.yml (workflow, not Go)
- .planning/epics/active/16.0_quick_start.md (markdown, not code)
- .planning/technical-debt/README.md (markdown, not code)
- CHANGELOG.md (markdown)
- README.md (markdown)
- internal/boundaries_test.go (test file, but we are to avoid test files unless there's a real performance problem)

We are to focus on the Go code that is production code (not test files) and look for performance issues.

Let's examine the new production Go files:

1. cmd/refresh-manifest/main.go: very short, just calls `quickstart.RunRefresh`. We'll look at the function it calls.

2. internal/quickstart/manifest.go: contains the manifest loading and validation.

3. internal/quickstart/refresh.go: contains the refresh logic.

4. internal/quickstart/workflow.go: generates the workflow YAML.

5. cmd/atcr/quickstart.go: the main quickstart command.

We are to look for performance issues in these files.

Let's go through each:

### cmd/refresh-manifest/main.go
```go
func main() {
	os.Exit(quickstart.RunRefresh(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
```
This is just a wrapper. No performance issue here.

### internal/quickstart/manifest.go
- `LoadManifest`: unmarshals JSON and validates. The validation loops over the models to check for empty strings and control characters. This is O(n) in the number of models. The number of models is small (3 in the embedded manifest). So not a hot path and not O(n²). 
- `SignupLink`: string concatenation, but called once per use? Not in a loop.
- `RegistryYAML`: builds a string by iterating over the roster. The roster is the list of persona names (from the project config). The number of personas is fixed (6 by default). So O(n) with n=6. Not a hot path? This is called when generating the registry for quickstart, which is a one-time setup. Not a performance issue in the hot path of the tool.

### internal/quickstart/refresh.go
- `BuildManifestFromModels`: unmarshals the JSON response, then loops over the models to extract IDs and check for empty strings. Then validates and marshals. O(n) in the number of models. The number of models from the API is expected to be small (maybe tens or hundreds). Not O(n²).
- `RunRefresh`: reads the entire input, then calls the above function. The input is the JSON response from the API. The size of the input is proportional to the number of models. So O(n) in the number of models. Not a hot path? This is run by a scheduled GitHub Action (once a week). Not a performance issue in the tool's hot path.

### internal/quickstart/workflow.go
- `WorkflowYAML`: builds a string by formatting. No loops, just string operations. Not a performance issue.

### cmd/atcr/quickstart.go
This is the main quickstart command. Let's look for performance issues.

We see:
- `runQuickstart`: calls `runInit` (if needed), then `LoadManifest`, `writeSyntheticRegistry`, `keyEnvFlow`, `scaffoldWorkflow`.
- `writeSyntheticRegistry`: writes the registry file. It loops over the roster (again, fixed size) to generate the agents. O(n) with n=number of personas (fixed).
- `keyEnvFlow`: reads from stdin (user input) and does some string operations. Not a loop over large data.
- `scaffoldWorkflow`: writes the workflow file. No loops.

We also see:
- `profileIsAtcrOwned`: called in `keyEnvFlow` when the user provides a shell profile. It does:
   - `resolveProfilePath`: which may call `os.UserHomeDir` and `filepath.Abs` (but note: the profile string is from user input, so it's one call).
   - Then it computes the absolute path of the .atcr directory and the registry path, and checks if the resolved profile path is equal to or has prefix of those.

This is O(1) per call. Not a loop.

However, note that in `keyEnvFlow`, we have:
```go
scanner := bufio.NewScanner(o.in)
readLine := func(prompt string) string {
	_, _ = fmt.Fprint(o.out, prompt)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}
```
This is called a few times (for the key and the profile). Not a loop over large data.

But note: the `scanner` is reading from `o.in` (which is stdin, typically the terminal). The user input is small (a key and a path). So no performance issue.

However, we must look for any loops that might be over large data. The only loops we see are over the roster (which is fixed and small) and over the models (which is small).

But wait: in `writeSyntheticRegistry`, we have:
```go
	for i, persona := range roster {
		model := m.Models[i%len(m.Models)]
		fmt.Fprintf(&b, "  %s:\n", persona)
		fmt.Fprintf(&b, "    persona: %s\n", persona)
		fmt.Fprintf(&b, "    provider: %s\n", m.Provider.Name)
		fmt.Fprintf(&b, "    model: %s\n", model)
	}
```
This is O(n) in the length of the roster. The roster is the list of persona names from the project config. In the quickstart command, the roster is obtained by `builtins.Names()` (which returns the built-in persona names). The number of built-in personas is fixed (6). So this loop runs 6 times. Not a performance issue.

Similarly, in `RegistryYAML` (in manifest.go) we have the same loop.

Now, let's look for any potential O(n²) or repeated work.

We see in `internal/quickstart/manifest.go` in the `validate` function:
```go
	for i, model := range m.Models {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("synthetic manifest: models[%d] is empty", i)
		}
		// Model ids are emitted verbatim as `model: <id>` lines in the generated
		// registry.yaml. A control character (esp. a newline) in an id sourced from
		// the live /models endpoint could forge YAML structure, so reject them at
		// the load boundary — defense-in-depth against a hostile refresh response.
		if strings.IndexFunc(model, func(r rune) bool { return unicode.IsControl(r) }) >= 0 {
			return fmt.Errorf("synthetic manifest: models[%d] contains a control character", i)
		}
	}
```
This is O(n) * O(m) where n is the number of models and m is the length of each model string? Actually, `strings.IndexFunc` is O(m) for each model. So total O(n * m). But n is the number of models (small) and m is the length of each model string (also small, because model IDs are short strings). So not a problem.

But note: the `unicode.IsControl` function is called for each rune in the string. The string length is the length of the model ID. Model IDs are typically short (like "gpt-4", etc.). So it's negligible.

However, we must consider if this is in a hot path. The `LoadManifest` function is called:
- In `cmd/refresh-manifest/main.go` (via `quickstart.RunRefresh`) which is run by the scheduled GitHub Action (once a week).
- In `cmd/atcr/quickstart.go` (in `runQuickstart`) which is run once per user when they run `atcr quickstart`.

So it's not in the hot path of the tool (like during a review). Therefore, even if it were O(n²) with large n, it wouldn't be a problem because n is small and it's not called frequently in the hot path.

But note: the problem says we are to look for performance issues that accumulate into slow software. We are to focus on measurable runtime impact. The hot path of the tool is during `atcr review`, `atcr reconcile`, etc. The quickstart and manifest refresh are not in the hot path.

Therefore, we might not find any performance issues in the changed code that affect the hot path of the tool.

But wait: we must also look at the changes to existing files. The diff shows changes to:

- cmd/atcr/main.go (added a new subcommand)
- cmd/atcr/main_test.go (updated test)
- README.md (added a line about quickstart)
- CHANGELOG.md (added a section)

These are not performance issues.

However, note that the diff also shows changes to `.planning/technical-debt/README.md` (which is markdown) and we see two new entries in the technical debt table. But these are not code changes, they are just updates to a markdown file. We are to ignore non-code changes for performance? Actually, the payload shows the change in context, but we are to review only the changed regions. The changed regions in the markdown file are the lines that were added. But we are to look for performance issues in the code, not in the markdown. And the markdown file does not contain code.

But note: the technical debt file is listing issues, but we are not to report on the technical debt file itself as a performance issue? We are to look for performance issues in the code that is being changed. The technical debt file is not code.

However, we see in the technical debt file two new entries that are marked as LOW severity and are about performance? Let's look:

In the technical debt file, we see:

```
+| U | [ ] | LOW | internal/quickstart/workflow.go:34 | Scaffolded consumer CI workflow installs atcr via `go install ...@latest` (unpinned) so a breaking or compromised future release lands silently in consumer CI | Pin the install to a released tag (e.g. @v1) once a stable release exists, or switch the scaffold to the pinned composite action | SECURITY | 15 | execute-epic-stage3
+| U | [ ] | LOW | internal/quickstart/workflow.go:37 | Scaffolded CI workflow runs `go install ...@latest`, pinning consumer review CI to a floating version that can change behavior or break without any consumer change | Pin the generated workflow to a released tag (e.g. @v1) rather than @latest | UNDER_ENGINEERING | 15 | execute-epic-independent
```

These are in the file `internal/quickstart/workflow.go` (which is new). But note: the technical debt file is being updated to record these issues. However, the actual code in `internal/quickstart/workflow.go` is what we are to review.

Let's look at `internal/quickstart/workflow.go`:

```go
func WorkflowYAML(m *Manifest) string {
	env := m.Provider.APIKeyEnv
	var b strings.Builder
	b.WriteString("# .github/workflows/atcr.yml — scaffolded by `atcr quickstart`.\n")
	b.WriteString("# Minimal review-only workflow: installs atcr and runs `atcr review` against\n")
	b.WriteString("# the PR, wired to the synthetic provider via the secret below.\n")
	b.WriteString("#\n")
	b.WriteString("# NOTE: intentionally minimal. It does NOT run `atcr reconcile` or `atcr github`,\n")
	b.WriteString("# so it posts no PR check and is not a merge gate. For the full PR-check pipeline\n")
	b.WriteString("# use the published composite action (samestrin/atcr@v1); see docs/github-action.md.\n")
	b.WriteString("name: atcr review\n")
	b.WriteString("on:\n")
	b.WriteString("  pull_request:\n")
	b.WriteString("permissions:\n")
	b.WriteString("  contents: read\n")
	b.WriteString("jobs:\n")
	b.WriteString("  atcr:\n")
	b.WriteString("    runs-on: ubuntu-latest\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - uses: actions/checkout@v4\n")
	b.WriteString("        with:\n")
		b.WriteString("          fetch-depth: 0   # atcr needs full history to resolve the merge-base\n")
	b.WriteString("      - uses: actions/setup-go@v5\n")
	b.WriteString("        with:\n")
	b.WriteString("          go-version: '1.25'\n")
	b.WriteString("      - name: Install atcr\n")
	b.WriteString("        run: go install github.com/samestrin/atcr/cmd/atcr@latest\n")
	b.WriteString("      - name: Review\n")
	b.WriteString("        env:\n")
	fmt.Fprintf(&b, "          %s: ${{ secrets.%s }}\n", env, env)
	b.WriteString("        run: atcr review --base \"origin/${{ github.base_ref }}\"\n")
	return b.String()
}
```

The two lines mentioned in the technical debt are:
- Line 34: the run step for installing atcr: `go install github.com/samestrin/atcr/cmd/atcr@latest`
- Line 37: the same line? Actually, the technical debt says line 34 and 37. Let's count:

The file has:
1: package quickstart
2: 
3: import (
4: 	"fmt"
5: 	"strings"
6: )
7: 
8: // WorkflowYAML renders the .github/workflows/atcr.yml scaffold: a minimal
9: // review-only CI workflow that installs atcr and runs `atcr review` wired to the
10: // synthetic provider's API-key secret. It is intentionally review-only — it does
11: // NOT run `atcr reconcile`/`atcr github`, so it posts no PR check and is not a
12: // merge gate; the published composite action (samestrin/atcr@v1) is the path for
13: // the full PR-check pipeline. The secret name tracks the manifest's api_key_env so
14: // the scaffold stays consistent with the generated registry.
15: func WorkflowYAML(m *Manifest) string {
16: 	env := m.Provider.APIKeyEnv
17: 	var b strings.Builder
18: 	b.WriteString("# .github/workflows/atcr.yml — scaffolded by `atcr quickstart`.\n")
19: 	b.WriteString("# Minimal review-only workflow: installs atcr and runs `atcr review` against\n")
20: 	b.WriteString("# the PR, wired to the synthetic provider via the secret below.\n")
21: 	b.WriteString("#\n")
22: 	b.WriteString("# NOTE: intentionally minimal. It does NOT run `atcr reconcile` or `atcr github`,\n")
23: 	b.WriteString("# so it posts no PR check and is not a merge gate. For the full PR-check pipeline\n")
24: 	b.WriteString("# use the published composite action (samestrin/atcr@v1); see docs/github-action.md.\n")
25: 	b.WriteString("name: atcr review\n")
26: 	b.WriteString("on:\n")
27: 	b.WriteString("  pull_request:\n")
28: 	b.WriteString("permissions:\n")
29: 	b.WriteString("  contents: read\n")
30: 	b.WriteString("jobs:\n")
31: 	b.WriteString("  atcr:\n")
32: 	b.WriteString("    runs-on: ubuntu-latest\n")
33: 	b.WriteString("    steps:\n")
34: 	b.WriteString("      - uses: actions/checkout@v4\n")
35: 	b.WriteString("        with:\n")
36: 	b.WriteString("          fetch-depth: 0   # atcr needs full history to resolve the merge-base\n")
37: 	b.WriteString("      - uses: actions/setup-go@v5\n")
38: 	b.WriteString("        with:\n")
39: 	b.WriteString("          go-version: '1.25'\n")
40: 	b.WriteString("      - name: Install atcr\n")
41: 	b.WriteString("        run: go install github.com/samestrin/atcr/cmd/atcr@latest\n")
42: 	b.WriteString("      - name: Review\n")
43: 	b.WriteString("        env:\n")
44: 	fmt.Fprintf(&b, "          %s: ${{ secrets.%s }}\n", env, env)
45: 	b.WriteString("        run: atcr review --base \"origin/${{ github.base_ref }}\"\n")
46: 	return b.String()
```

So line 34 is the step for checkout, and line 37 is the step for setup-go. The technical debt comments are about the install step (line 41) and they mention two lines? Actually, the technical debt says:

- Line 34: ... (but line 34 is the checkout step) -> this must be a mistake in the technical debt file? Or perhaps the line numbers in the technical debt file are off because the file was changed? But note: the technical debt file is being updated in this same commit, so the line numbers in the technical debt file refer to the new version of the file.

In the new version of `internal/quickstart/workflow.go`, the install step is at line 41. The technical debt says line 34 and 37. This doesn't match.

But note: the technical debt file is not code, so we are not to look for performance issues in it. We are to look for performance issues in the code that is being changed. The code in `internal/quickstart/workflow.go` is what we are to review.

Now, the technical debt entries are pointing out two issues in the workflow:

1. The workflow uses `go install ...@latest` which is unpinned and could break.
2. The same thing again? Actually, two entries that are very similar.

But note: these are not performance issues. They are about reliability and security (using an unpinned version). The technical debt categorizes them as SECURITY and UNDER_ENGINEERING. We are only to report performance issues.

Therefore, we ignore these.

However, note that the workflow runs `go install` which downloads and builds the atcr binary. This is a one-time setup in the CI workflow. It is not part of the atcr tool's runtime. So even if it were slow, it's not the tool's performance.

But wait: the workflow is part of the project and we are to review the changed code for performance issues that affect the tool. The workflow is not the tool. The tool's performance is what we care about.

Therefore, we should not report on the workflow's performance.

But note: the workflow runs `atcr review`. The performance of `atcr review` is what we care about. However, the workflow does not change the performance of `atcr review`; it just runs it.

So we are not to look for performance issues in the workflow file.

Now, let's look for any performance issues in the actual atcr tool code that was changed in this diff.

We have not found any in the new code. But note: the diff also shows changes to existing files? Let's check:

- cmd/atcr/main.go: added a new subcommand (quickstart). This is just adding a command to the root command. No performance issue.
- cmd/atcr/main_test.go: updated the test to expect 18 subcommands. No performance issue.
- README.md: added a line about quickstart. No code.
- CHANGELOG.md: added a section. No code.

We also see changes to `.planning/epics/active/16.0_quick_start.md` and `.planning/technical-debt/README.md` (markdown).

And we see changes to `internal/boundaries_test.go` (adding an entry for "quickstart" in the allowedInternalImports). This is a test file, but we are to avoid test files unless there's a real performance problem. This is just adding an allowed import, no performance issue.

Therefore, we have not found any performance issues in the changed code.

But wait: we must look at the entire diff again. We might have missed something.

Let's look at the diff for `internal/boundaries_test.go`:

```diff
@@ -22,43 +22,44 @@ const modulePath = "github.com/samestrin/atcr"
 // allowedInternalImports maps every top-level internal package to the set of
 // internal packages it may inherit (subpackages inherit their top-level
 // entry). Absence of a directory here fails the completeness check.
 var allowedInternalImports = map[string][]string{
 	"atomicfs":       {},
 	"atomicwrite":    {"atomicfs"},
 	"cache":          {"atomicfs"}, // diff cache leaf; atomicfs for atomic entry writes (epic 5.2)
 	"stream":         {"metrics"},  // metrics: observability counters for a git-unavailable file index and indeterminate/unresolvable path validation (stdlib-only leaf, no cycle)
 	"gitrange":       {},
 	"log":            {},                                       // single diagnostic sink; stdlib-only (epic 4.0)
 	"errors":         {},                                       // error-classification taxonomy; stdlib-only (epic 4.0)
 	"registry":       {"stream"},                               // stream is the canonical zero-dependency severity leaf (epic 3.5)
 	"tools":          {"sandbox"},                              // sandbox: run_tests/run_script execute in the container backend (epic 11.0, opt-in --exec)
 	"sandbox":        {"log"},                                  // container-isolated executor for --exec reproduction; log: structured audit line per sandbox run (epic 11.0)
 	"repro":          {"reconcile", "sandbox"},                 // 2-run determinism + evidence_exec write-back: runs the sandbox, stamps reconcile findings (epic 11.0)
 	"metrics":        {},                                       // in-process metrics collector; stdlib-only leaf (epic 4.4)
 	"version":        {},                                       // build-version holder (atcr_version in the leaderboard submission); stdlib-only leaf, no imports (epic 10.0)
 	"circuitbreaker": {"metrics"},                              // per-provider breaker; pushes state to the metrics gauge (epic 4.5)
 	"validation":     {},                                       // user-input validators; stdlib-only leaf (epic 4.3)
 	"tdmigrate":      {},                                       // technical-debt storage migrator; yaml.v3 + stdlib only, imports no internal package (epic 12.1)
 	"payload":        {"gitrange", "atomicfs", "log"},          // log: single diagnostic sink, injected via context (epic 4.0 phase 4.1)
 	"llmclient":      {"registry", "errors", "circuitbreaker"}, // circuitbreaker: per-provider fail-fast on the API call path (epic 4.5)
 	"doctor":         {"llmclient", "registry"},
 	"fanout":         {"llmclient", "registry", "stream", "payload", "tools", "log", "metrics", "circuitbreaker", "validation", "atomicfs", "cache"}, // log: WithAgent per-agent correlation (epic 4.0 phase 4.2); metrics: fan-out instrumentation (epic 4.4); circuitbreaker: provider threaded onto the call context (epic 4.5); validation: engine-level --output-dir system-path reject for non-CLI callers (stdlib-only leaf); atomicfs: CopyPath for the EXDEV copy-fallback in backupExisting's crash-safe swap, the shared low-level fs leaf reconcile/verify already import (epic 4.7.1); cache: diff-cache replay on the single-shot review path (epic 5.2)
 	"reconcile":      {"stream", "atomicfs", "astgroup"},                                                                                             // astgroup: AST-isomorphism grouper wired as the primary clustering signal (epic 13.1)
 	"scorecard":      {"llmclient", "reconcile", "fanout", "version"},                                                                                // version: atcr_version stamped into the public submission envelope (epic 10.0)
 	"personas":       {"registry", "payload"},                                                                                                        // community persona lifecycle: validates fetched YAML via registry.ValidateAgentYAML; built-in roster from top-level personas/ (non-internal) (epic 9.0); payload: TemplateFixtureRunner calls RenderPrompt to validate built-in templates against embedded fixtures (TD-012)
 	"report":         {"stream", "reconcile"},
 	"ghaction":       {"reconcile"},                                                                                                                        // GitHub Action renderer/client: reads reconciled findings, posts check runs (epic 7.3)
 	"verify":         {"reconcile", "stream", "registry", "fanout", "payload", "tools", "llmclient", "atomicfs", "atomicwrite", "log", "sandbox", "repro"}, // log: skeptic-failure routing (epic 4.0 phase 4.2); atomicwrite: shared group-write helper; sandbox: --exec backend resolution + dispatcher wiring (epic 11.0); repro: evidence_exec write-back stamping (epic 11.0)
 	"debate":         {"reconcile", "stream", "registry", "fanout", "payload", "tools", "llmclient", "atomicfs", "atomicwrite", "log"},                     // cross-examination stage; mirrors verify's harness; atomicwrite shared group-write helper (epic 6.0)
 	"mcp":            {"gitrange", "payload", "registry", "llmclient", "fanout", "stream", "reconcile", "report", "verify", "debate", "scorecard", "log", "metrics"},
 	"benchmark":      {"scorecard", "version"}, // standard-suite contract + suite-tagged submission envelope; reuses scorecard.PublicRecord for one public reviewer schema, version for atcr_version (epic 10.0)
 	"astgroup":       {},                       // AST-isomorphism grouper: wazero host + embedded .wasm parsers; imports the external reconcile library (Grouper seam) + wazero only, no internal package (epic 13.1)
+	"quickstart":     {},                       // `atcr quickstart` data layer: synthetic manifest + registry.yaml/workflow generators + refresh transform; stdlib + embed only, no internal package (epic 16.0)
 	// integration holds only end-to-end _test.go files (no production code).
 	// The dependency-direction walk skips _test.go, so this entry exists to
 	// satisfy the allowlist-completeness check; it records the packages those
 	// tests exercise (epic 4.0 phase 5.2).
 	"integration": {"fanout", "llmclient", "log", "errors", "registry", "mcp"},
 }
```

This is just adding a new entry to the map. No performance issue.

Now, let's look at the diff for `cmd/atcr/main.go`:

```diff
@@ -126,79 +126,80 @@ func usageArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
 // newRootCmd constructs the atcr command tree. All subcommands use RunE so
 // errors bubble up to main() for centralized exit-code mapping.
 func newRootCmd() *cobra.Command {
 	root := &cobra.Command{
 		Use:   "atcr",
 		Short: "Agent Team Code Review — a review panel, not a reviewer",
 		// Setting Version makes cobra auto-register the --version flag, which
 		// short-circuits before PersistentPreRunE (matching the comments on that
 		// hook below). A peer `version` subcommand is also registered for the
 		// `atcr version` convention; both render the same string.
 		Version: atcrVersion(),
 		Long: "atcr fans a code change out to a panel of heterogeneous LLM reviewer personas,\n" +
 			"then deterministically reconciles their findings into a single deduplicated,\n" +
 			"confidence-scored deliverable.\n\n" +
 			"Logging:\n" +
 			"  LOG_LEVEL      environment variable: debug, info, warn, error (default info).\n" +
 			"                 Set LOG_LEVEL=debug to diagnose a failing review.\n" +
 			"  --log-format   log output format: text or json (default text).\n" +
 			"                 Use json for machine-readable, newline-delimited logs in CI.",
 		SilenceUsage:  true,
 		SilenceErrors: true,
 		// An unknown subcommand is a usage error (exit 2), not the generic
 		// failure code: in CI, exit 1 specifically means "findings at/above
 		// threshold". Setting Args bypasses cobra's legacyArgs path (which
 		// returns an uncoded error from Find), and the RunE keeps bare `atcr`
 		// printing help with exit 0.
 		Args: usageArgs(cobra.NoArgs),
 		// PersistentPreRunE is inherited by every subcommand, so it is the single
 		// point where the root logger is constructed (from LOG_LEVEL and
 		// --log-format) and stored in the command context. No subcommand builds
 		// its own logger after this; they retrieve it via log.FromContext.
 		// Note: cobra's --help/-h and --version flags short-circuit before
 		// PersistentPreRunE runs, so no logger is stored in context on those
 		// paths. All consumers must use log.FromContext, which falls back to a
 		// shared discard logger on a miss — never assert logger presence directly.
 		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
 			return setupLogger(cmd)
 		},
 		RunE: func(cmd *cobra.Command, _ []string) error {
 			return cmd.Help()
 		},
 	}
 
 	// --log-format is a persistent flag so every subcommand inherits it; LOG_LEVEL
 	// is read from the environment (see logLevelFromEnv). Both feed setupLogger.
 	root.PersistentFlags().String("log-format", "text", "log output format: text or json")
 
 	// Flag-parse errors (unknown flags, bad values, violated flag groups)
 	// are usage errors: exit 2.
 	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
 		return usageError(err)
 	})
 
 	root.AddCommand(
 		newReviewCmd(),
 		newReconcileCmd(),
 		newVerifyCmd(),
 		newDebateCmd(),
 		newReportCmd(),
 		newGithubCmd(),
 		newRangeCmd(),
 		newStatusCmd(),
 		newInitCmd(),
+		newQuickstartCmd(),
 		newServeCmd(),
 		newDoctorCmd(),
 		newTrustCmd(),
 		newScorecardCmd(),
 		newLeaderboardCmd(),
 		newBenchmarkCmd(),
 		newPersonasCmd(),
 		newVersionCmd(),
 	)
 	return root
 }
```

This is just adding a new subcommand. No performance issue.

Now, let's look at the diff for `cmd/atcr/main_test.go`:

```diff
@@ -43,19 +43,19 @@ func TestRootCmd_HelpListsAllSubcommands(t *testing.T) {
 	}
 }
 
-func TestRootCmd_HasExactlySeventeenSubcommands(t *testing.T) {
-	// The sixteen prior commands plus `version`, which reports the binary
-	// version for the `atcr version` convention (peer to the --version flag).
+func TestRootCmd_HasExactlyEighteenSubcommands(t *testing.T) {
+	// The seventeen prior commands plus `quickstart`, the interactive onboarding
+	// wizard (peer to the non-interactive `init`).
 	root := newRootCmd()
 	names := map[string]bool{}
 	for _, c := range root.Commands() {
 		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
 			continue
 		}
 		names[c.Name()] = true
 	}
