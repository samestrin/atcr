 The user wants me to review a code diff based on the provided sprint plan. I need to act as Kai, an architecture and design reviewer, focusing on boundaries, coupling, contracts, extensibility, and duplication.

First, let me understand the scope:
- Epic 31.1: AXI Content-First Home View
- Changes are in `cmd/atcr/home.go`, `cmd/atcr/main.go`, `cmd/atcr/main_test.go`, `docs/agentic-consumption.md`, `internal/report/home.go`, `internal/report/home_test.go`, and planning files.
- The sprint plan shows this is about adding a home view to bare `atcr` invocation, with `--axi` support.

Let me analyze the actual code changes:

1. **`cmd/atcr/home.go`** (new file):
   - Defines `homeExecutable` and `homeUserDir` as vars for test seams.
   - `relHome(path string) string`: Relativizes path to home using `filepath.Rel` + fallback.
   - `resolveHomeState(ctx context.Context) homeState`: Calls `anchorDir("")`, then `fanout.ReadReviewStatus`. Distinguishes `os.ErrNotExist` (first-run) from other errors (unavailable).
   - `renderHomeView(w io.Writer, execPath, description string, st homeState) error`: Prints human-readable home view.
   - `runHome(cmd *cobra.Command) error`: Gets exec path, resolves home state, checks `axiFromContext(ctx)`, dispatches to AXI or human renderer.

2. **`cmd/atcr/main.go`**:
   - `RunE` changed from `return cmd.Help()` to `return runHome(cmd)`.
   - Added local `--axi` flag on root command: `root.Flags().Bool("axi", false, ...)`.
   - The `PersistentPreRunE` already had logic to propagate `--axi` via `cmd.Flags().Lookup("axi")` - this was presumably added in Epic 31.0 but note the comment says "The flag lives only on `atcr review`" which is now outdated since we added it to root too. Wait, looking closely at the diff, the `PersistentPreRunE` code already exists (it's in the context lines), but the comment says "The flag lives only on `atcr review`". Now with root also having `--axi`, the `Lookup` will find it on bare `atcr` too. The comment is now stale but that's a minor issue.

3. **`cmd/atcr/main_test.go`**:
   - Renamed `TestRootCmd_BareInvocationShowsHelp` to `TestRootCmd_BareInvocationShowsHomeView`.
   - Added `TestRootCmd_AXIFlagRegisteredOnRoot`.
   - Added `TestRootCmd_HelpAndVersionUnaffected`.
   - Added `TestRootCmd_AXINotInheritedBySubcommands`.

4. **`internal/report/home.go`** (new file):
   - `HomeViewAXI` struct.
   - `RenderHomeViewAXI(w io.Writer, s HomeViewAXI) error`: Renders TOON format.

5. **`internal/report/home_test.go`** (new file):
   - Tests for `RenderHomeViewAXI`.

Now let me look for architecture/design issues:

**Potential Issues:**

1. **Seam design in `cmd/atcr/home.go`**:
   `homeExecutable` and `homeUserDir` are package-level mutable vars. This is a common Go testing pattern, but it's global mutable state. Two issues:
   - Race conditions if tests run in parallel (though `cmd/atcr` tests likely use `execute(t)` which runs commands, parallel execution might mutate these).
   - Any other package code could theoretically mutate these, but since they're unexported in `main`, only tests in the same package can change them.
   Wait, the tests shown don't actually override these vars. But the design sets a precedent. Is this a boundary violation? Not really, it's just test seam design.
   
   However, looking at `resolveHomeState`, it calls `anchorDir("")` which is from `anchor.go` (same package). Good.

2. **Error handling in `resolveHomeState`**:
   The function returns `homeState` and never errors. It logs at Debug level for errors. This is fine per AC3.

3. **`anchorDir` and `fanout.ReadReviewStatus` coupling**:
   `resolveHomeState` uses `anchorDir("")` and `fanout.ReadReviewStatus`. This mirrors `status.go`. The sprint plan explicitly mentions this is read-only reuse and deliberate.

4. **`renderHomeView` returning error**:
   The function returns `error` from `fmt.Fprint` calls. But `runHome` doesn't transform this error - it just returns it. If stdout is broken (e.g., pipe closed), this will return a non-nil error which in `main` (not shown but implied by cobra) would result in a non-zero exit code. AC3 says "never an error", but stdout write failures are legitimate runtime errors. The sprint plan says "The home view must never error on a first-run/no-reviews state" which is satisfied. But what about other write errors? `cmd.Help()` also returns an error from writing to stdout, so this is consistent with prior behavior.

5. **`runHome` fallbacks**:
   If `os.Executable()` fails, it logs and falls back to `"atcr"`. Good.

6. **AXI context propagation**:
   `runHome` checks `axiFromContext(ctx)`. The context is populated in `PersistentPreRunE` which does `cmd.Flags().Lookup("axi")`. Since root now has local `--axi`, on bare `atcr --axi` the Lookup will succeed. But wait: if `PersistentPreRunE` does `cmd.Flags().Lookup("axi")`, on a subcommand like `atcr review --axi`, `cmd.Flags()` in `PersistentPreRunE` refers to the local flags of the command being executed. Actually, in cobra, `cmd.Flags()` in `PersistentPreRunE` returns the flags of the command that's about to run. For root bare invocation, `cmd` is root. For `atcr review`, `cmd` is the review subcommand. So the Lookup will check the review subcommand's flags. Since root's `--axi` is local (not persistent), it's not inherited, so review's own `--axi` must exist (which it does from Epic 31.0). The propagation mechanism works.

   Wait, there's a subtle issue. The `PersistentPreRunE` comment says: "The flag lives only on `atcr review`; the Lookup guard leaves every other command unaffected." But now the flag ALSO lives on root. However, for other subcommands (status, etc.), they don't have `--axi`, so Lookup returns nil, and they're unaffected. For root bare invocation, Lookup finds the root's own local flag. This is fine. The comment is stale but the code works.

7. **Boundary: `internal/report/home.go` depends on `toonQuote` and `axiDelim`**:
   These are presumably defined in `internal/report/render.go` or similar. The new file uses them directly. This is within the same package (`internal/report`), so it's fine. It reuses the encoder.

8. **`RenderHomeViewAXI` defensive width check**:
   It checks `if len(row) != len(homeViewAXIHeader)`. Since both are fixed at compile time (4 columns), this check is defensive but will never trigger on valid code. If someone adds a column to the struct but not the header, it catches it. This is good practice.

9. **Duplication concern - `relHome` vs existing path relativization**:
   The sprint plan advisory mentions `relDisplay` and `relativizePaths` in other packages. `relHome` is specifically home-dir relativization. The plan says to follow the `filepath.Rel`-plus-fallback idiom, which it does. However, is there duplication with `expandHome` in `cmd/atcr/quickstart.go`? No, `expandHome` does `~` → full path (inverse operation). `relHome` does full path → `~`. They are complementary.

10. **Test seam exposure**:
    `homeExecutable` and `homeUserDir` are package-level vars. If tests want to make them deterministic, they have to mutate global state. This can cause test pollution if not cleaned up, or prevent parallel tests (`t.Parallel()`). Given that `main_test.go` tests don't appear to use `t.Parallel()`, and these are unexported vars, this is a localized coupling issue. Severity: LOW or MEDIUM? It's a hidden dependency / shared mutable state pattern. The fix would be to accept these as parameters or use an interface, but for a small CLI tool this might be overkill. However, as Kai, I should flag hidden dependencies and shared mutable state.

    Wait, looking at `home_test.go` (there is no `home_test.go` in `cmd/atcr/`, only `internal/report/home_test.go`). The `cmd/atcr/home.go` seams are currently unused by any visible tests. But the design introduces them. Should I flag this? The test file only shows `main_test.go` changes. There are no tests shown for `cmd/atcr/home.go` itself (like testing `relHome`, `resolveHomeState`, `renderHomeView`). But the vars are there for future tests.

    Is this a design flaw? Package-level mutable vars for dependencies are classic Go test seam pattern, but it's still shared mutable state. I'll flag it as LOW since it's localized to `main` package tests and unexported.

11. **`resolveHomeState` taking `context.Context` but passing it to `anchorDir`**:
    `anchorDir` signature isn't shown, but based on `anchor.go:18` mentioned in plan, it likely takes a string argument only, not ctx. In the code: `dir, err := anchorDir("")`. It doesn't pass ctx. That's fine, anchorDir probably doesn't need it. Then `fanout.ReadReviewStatus(dir, filepath.Base(dir))` - this likely doesn't take ctx either. The `ctx` is only used for logging: `log.FromContext(ctx)`. This is fine.

12. **Error semantics in `homeState`**:
    `unavailable` state has two sub-cases in `renderHomeView`:
    - `st.unavailable && st.reviewID != ""`: "Latest review %s is unavailable..."
    - `st.unavailable && st.reviewID == ""`: "Latest review pointer is unreadable..."
    
    But in `resolveHomeState`, the `unavailable` case with `reviewID == ""` happens when `anchorDir("")` errors with something other than `os.ErrNotExist`. The log message says ".atcr/latest pointer unreadable". Then if `ReadReviewStatus` fails, we get `unavailable: true` with `reviewID` set to `filepath.Base(dir)`. Wait, `dir` is returned by `anchorDir("")`, so `filepath.Base(dir)` is the review ID. If it fails, `reviewID` is set.

    Actually wait, looking closely:
    ```go
    if err != nil {
        if errors.Is(err, os.ErrNotExist) {
            return homeState{}
        }
        log.FromContext(ctx).Debug(...)
        return homeState{unavailable: true}  // reviewID is empty here
    }
    st, err := fanout.ReadReviewStatus(dir, filepath.Base(dir))
    if err != nil {
        log.FromContext(ctx).Debug(...)
        return homeState{unavailable: true, reviewID: filepath.Base(dir)}
    }
    ```
    So the `unavailable: true, reviewID: ""` case means anchorDir errored (not NotExist) but we don't have a review ID. The render says "Latest review pointer is unreadable — run `atcr review` to start a fresh one." That's reasonable.

13. **TOON format in `RenderHomeViewAXI`**:
    Uses `home[1%c]{%s}:` where `%c` is `axiDelim`. The `1` in `home[1|]` indicates 1 row? But what if there are no reviews? It still emits 1 row. The header says `home[1|]...` but the semantics of `[1]` in TOON might mean the array has 1 element. However, the docs say: "When no review has run yet, review_id is a quoted empty string ("") and review_status is none — the home view is never an error and never empty, even on a first run". So it's always 1 row. That seems consistent with the design decision.

    Wait, but `axiDelim` - what type is it? Probably a byte or rune. `string(axiDelim)` is used in `strings.Join`. If `axiDelim` is a byte, `fmt.Fprintf(&b, "home[1%c]{%s}:\n", axiDelim, ...)` works. If it's a rune, also works. Fine.

14. **No findings**:
    The prompt says if nothing is wrong, emit nothing. But I need to be thorough.

Let me reconsider if there are any CRITICAL or HIGH issues.

**Potential CRITICAL/HIGH:**

- **Contract issue with `RenderHomeViewAXI`**: The function writes to `w` using `w.Write(b.Bytes())`. If `w.Write` returns an error, it's returned. This is standard.

- **Root `--axi` flag collision**:
  `root.Flags().Bool("axi", false, ...)` adds a LOCAL flag.
  `newReviewCmd()` presumably also adds a local `--axi` flag.
  Since root's `--axi` is local and not persistent, subcommands don't inherit it.
  But what about `atcr --axi review`? In cobra, if you pass a flag before a subcommand, and that flag is local to root, does cobra parse it? Actually, Cobra's behavior: flags before subcommands are generally parsed by the root command if they're not persistent and the subcommand doesn't accept them... No, actually Cobra handles flags positionally. If you do `atcr --axi review`, since `--axi` is a local flag on root, and `review` is a subcommand, Cobra will parse `--axi` as belonging to root, then find `review` as the subcommand. But wait, Cobra's flag parsing interleaves. Actually, Cobra parses flags until it hits a non-flag, which might be the subcommand. So `atcr --axi review` should work: root parses `--axi`, then `review` is the subcommand.
  
  But what about `atcr review --axi`? This should be parsed by `review`'s flags, not root's. Since root's `--axi` is local and not persistent, `review` has its own `--axi`. This is fine.
  
  However, what if both root and review have `--axi` with potentially different defaults or behaviors? They both default to `false`. The `PersistentPreRunE` does:
  ```go
  if cmd.Flags().Lookup("axi") != nil {
      axi, _ := cmd.Flags().GetBool("axi")
      cmd.SetContext(newAXIContext(cmd.Context(), axi))
  }
  ```
  For `atcr review --axi`, `cmd` is the review subcommand. `cmd.Flags().Lookup("axi")` finds review's flag. `GetBool` returns true. Context gets AXI=true.
  For bare `atcr --axi`, `cmd` is root. `cmd.Flags().Lookup("axi")` finds root's flag. `GetBool` returns true. Context gets AXI=true.
  This is fine.
  
  But is there a risk that `atcr --axi review` causes root's PersistentPreRunE to set AXI context, then review's RunE also checks its own flag? Wait, PersistentPreRunE runs before the subcommand's RunE. But does it run for the root when a subcommand is invoked? Yes, PersistentPreRunE on root runs for all subcommands. So for `atcr --axi review`:
  1. Root's PersistentPreRunE runs. `cmd` is the root command (or is it the review subcommand? In Cobra, when running a subcommand, the subcommand is `cmd`, and its parents' PersistentPreRunE are executed. Actually, in Cobra's `ExecuteC`, when executing a subcommand, the subcommand is returned as the command to execute. The `PersistentPreRunE` of parents is executed, but `cmd` in the closure refers to the command being executed? Let me recall: In Cobra, inside `PersistentPreRunE`, `cmd` is the command that is currently being executed (the leaf subcommand), not the parent where PersistentPreRunE is defined. Wait no, `PersistentPreRunE` is defined on root, but when invoked for a subcommand, `cmd` is the subcommand. Actually, looking at Cobra docs: "PersistentPreRun... will be run before the command's Run method and also before all child commands' Run methods." And `cmd` in the hook is the command being executed. So for `atcr review`, `cmd` in root's `PersistentPreRunE` is the `review` command. Therefore `cmd.Flags()` refers to review's flags.
  
  But what about `atcr --axi` (bare)? `cmd` is root. `cmd.Flags()` is root's flags.
  
  So for `atcr --axi review`, since `--axi` is before the subcommand, how does Cobra parse it? Actually, Cobra parses flags greedily. For `atcr --axi review`, the parser sees `--axi`, checks if root accepts it (yes, local flag), parses it, then sees `review` as the subcommand. Then it executes `review`'s PersistentPreRunE (root's) and RunE. In root's PersistentPreRunE, `cmd` is the `review` subcommand. `cmd.Flags().Lookup("axi")` looks up review's flags. But `--axi` was parsed by root, not review! So review's flags won't have it set. Wait, does Cobra propagate parsed flags to subcommands? Local flags are not inherited, but if a flag is parsed by root before the subcommand is identified, is it stored in root's flagset or passed down?
  
  Actually, Cobra's flag parsing behavior with positional flags before subcommands: Flags are parsed before arguments. The root command processes flags it knows. Once it encounters a non-flag (or `--`), the remaining args are passed to the subcommand. But `--axi` is a flag. So root parses `--axi` into its own flagset. Then `review` is encountered as a positional arg (subcommand). Then Cobra looks up `review` and executes it. When root's PersistentPreRunE runs, `cmd` is `review`. But `cmd.Flags()` is review's flag set, which does NOT contain `--axi` (it's local to root, not persistent). So `Lookup("axi")` returns nil, and AXI context is NOT set. But root parsed `--axi`! The user explicitly passed `--axi`, expecting AXI mode, but because it's local to root and not persistent, and the subcommand is `review`, the context won't have AXI set. This is actually correct behavior if we don't want root's `--axi` to leak to subcommands. But the user might expect `atcr --axi review` to work the same as `atcr review --axi`. Does it?
  
  Wait, in standard Cobra, `atcr --log-format json review` works because `--log-format` is a `PersistentFlag`. `atcr --axi review` where `--axi` is local to root: Cobra might actually error with "unknown flag --axi" because after identifying the subcommand, it reparses? No, Cobra doesn't reparse. Let me think. Actually, Cobra interleaves parsing. It parses flags for the current command until it finds one it doesn't know, but actually it parses all flags before positional args.
  
  Let's check Cobra behavior. If root has local flag `--axi`, and you run `root --axi subcommand`, Cobra parses `--axi` on root. Then it sees `subcommand`. It invokes `subcommand`. Root's PersistentPreRunE runs. Does the subcommand have access to root's local flags? No. So `atcr --axi review` would set root's `--axi` to true, but review's `PersistentPreRunE` (which is root's) checks `cmd.Flags()` which is review's flags, not root's. So AXI context is false. This means `atcr --axi review` would NOT enable AXI mode. But `atcr review --axi` would.
  
  Is this a problem? The flag on root is specifically for bare invocation `atcr --axi`. For `atcr review`, the user should use `atcr review --axi`. The documentation says: `atcr` (no subcommand) | `--axi` flag. It doesn't say `atcr --axi review`. So this is acceptable.
  
  However, the `PersistentPreRunE` comment says: "The flag lives only on `atcr review`; the Lookup guard leaves every other command unaffected." This comment is now stale/wrong because root also has it. But the code is correct. Not a critical issue.

Wait, I should look for any actual contract breakage.

**CRITICAL Check**:
- Does `runHome` break the existing contract for bare `atcr`? Previously it showed Help. Now it shows home view. The AC explicitly requires this change. The tests confirm `--help`/`-h`/`--version` still work.
- Does `runHome` return errors that break exit codes? If `renderHomeView` returns a write error, it's returned. Previously `cmd.Help()` also returned write errors. So exit code behavior for stdout errors is unchanged.
- What about `resolveHomeState` logging at debug? It uses `log.FromContext(ctx)`. If ctx has no logger, it falls back to discard. This is fine.

**HIGH Check**:
- Is there a design flaw that forces rework?
- The `homeExecutable` and `homeUserDir` seams: if parallel tests run, they could race. But `t.Parallel()` isn't used. Still, package-level mutable vars are a design smell. I'll flag as MEDIUM.
- `relHome` allocates `[]string` for `strings.HasPrefix` etc. Not a design issue.

Wait, I see something in `cmd/atcr/home.go`:
```go
func resolveHomeState(ctx context.Context) homeState {
	dir, err := anchorDir("")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return homeState{} // no .atcr/latest at all: true first-run (AC3)
		}
		log.FromContext(ctx).Debug("home view: .atcr/latest pointer unreadable", "err", err)
		return homeState{unavailable: true}
	}
	st, err := fanout.ReadReviewStatus(dir, filepath.Base(dir))
```

What if `anchorDir("")` returns a directory that exists, but `filepath.Base(dir)` is empty or "."? If `anchorDir` returns something like `/path/to/.atcr/latest` which is a symlink to a directory, then `dir` might be the review directory, and `filepath.Base(dir)` would be the review ID. But what if `anchorDir` returns "/"? `filepath.Base("/")` is "/". That's fine.

But more importantly, `anchorDir` comes from the same package. If its behavior changes, `resolveHomeState` is tightly coupled to it. That's within `cmd/atcr` package, so acceptable.

Let me look at `RenderHomeViewAXI` more carefully.

```go
func RenderHomeViewAXI(w io.Writer, s HomeViewAXI) error {
	var b bytes.Buffer
	quotedHeader := make([]string, len(homeViewAXIHeader))
	for i, h := range homeViewAXIHeader {
		quotedHeader[i] = toonQuote(h)
	}
	fmt.Fprintf(&b, "home[1%c]{%s}:\n", axiDelim, strings.Join(quotedHeader, string(axiDelim)))

	row := []string{...}
	if len(row) != len(homeViewAXIHeader) {
		return fmt.Errorf(...)
	}
	b.WriteString("  ")
	b.WriteString(strings.Join(row, string(axiDelim)))
	b.WriteByte('\n')
	_, err := w.Write(b.Bytes())
	return err
}
```

This is very similar to `RenderReviewSummaryAXI` presumably. Is there duplication that could be extracted? The sprint plan says to reuse the encoder, and this does reuse `toonQuote`/`axiDelim`. However, the header+row rendering logic is duplicated between this and `RenderReviewSummaryAXI` and `renderAXI`. This is a MEDIUM/LOW coupling issue. But since I don't see the existing code in the diff (only in context), and the scope says review only changed regions... wait, `internal/report/home.go` is new. Is there duplication of responsibility? The file reuses `toonQuote` and `axiDelim`, but implements its own TOON array serialization. If there are already existing functions in `internal/report` that render TOON arrays, this is duplication. I don't have the existing `renderAXI` code visible though. The plan mentions `RenderReviewSummaryAXI` (`internal/report/render.go:266-342`). The new `RenderHomeViewAXI` likely duplicates the serialization pattern. But the scope constraint says I should only review changed regions. `internal/report/home.go` is changed (new). If the duplication is with pre-existing code, that's a pre-existing pattern, but the new code adds to it. Should I flag it? The scope says "Review only the changed regions" and "a finding whose FILE:LINE falls outside the changed lines will be discarded". But if I cite `internal/report/home.go:45` (the line with row/header logic) and say it duplicates logic from `render.go:266`, that's outside changed lines for `render.go`, but my finding is IN the changed file `home.go`. So it should be valid. Let me see if this rises to the level of a finding.

The plan explicitly says the home view should reuse "Epic 31.0's FormatAXI/pagination/context-propagation plumbing — this is not a second, parallel output mode." The new `RenderHomeViewAXI` reuses the TOON encoder (`toonQuote`/`axiDelim`), which is good. But if the array-rendering logic is copy-pasted from `RenderReviewSummaryAXI`, that's parallel code paths that will drift. However, looking at the code, it's only 4 columns, fixed. The defensive width check is good. It might be okay.

Actually, looking at the sprint plan's "Advisory observations", it mentions: "Existing path-relativization pattern to follow". It doesn't flag the TOON rendering as a concern. Given the explicit goal to reuse the shared encoder (`toonQuote`/`axiDelim`), and the fact that `RenderHomeViewAXI` is structurally similar to `RenderReviewSummaryAXI` by design (same package, same conventions), I won't flag this as duplication unless it's clearly a second divergent serializer. The plan says "reusing the shared `toonQuote`/`axiDelim` encoder — mirroring the `ReviewSummaryAXI`/`RenderReviewSummaryAXI` precedent". So this is intentional mirroring, not drift risk.

**More subtle issue**: In `cmd/atcr/home.go`, `renderHomeView` takes `execPath` and passes it through `relHome` in `runHome` for AXI, but for the human view it passes the raw `execPath` to `renderHomeView`, which then... wait, no. Looking at `runHome`:

```go
execPath, err := homeExecutable()
...
st := resolveHomeState(ctx)
if axiFromContext(ctx) {
    ...
    return report.RenderHomeViewAXI(cmd.OutOrStdout(), report.HomeViewAXI{
        ExecPath:     relHome(execPath),
        ...
    })
}
return renderHomeView(cmd.OutOrStdout(), execPath, cmd.Short, st)
```

Then `renderHomeView`:
```go
func renderHomeView(w io.Writer, execPath, description string, st homeState) error {
    if _, err := fmt.Fprintln(w, relHome(execPath)); err != nil {
        return err
    }
```

So for human view, `relHome` is called inside `renderHomeView`, but for AXI view, `relHome` is called in `runHome`. This is inconsistent. It means `renderHomeView` is self-contained (applies its own relativization), while `RenderHomeViewAXI` expects pre-relativized input. This isn't necessarily a bug, but it's an asymmetric contract. Is it a problem? The AXI struct expects a relativized path, which is documented by the field name? Not really, `ExecPath` is just `ExecPath`. The asymmetry means if someone calls `RenderHomeViewAXI` with an absolute path, it stays absolute. But for the human view, `renderHomeView` always relativizes. This is a bit inconsistent but not critical.

However, looking at `runHome`, when `os.Executable()` fails, it falls back to `"atcr"`, and then `relHome("atcr")` is called. If home is `/home/user`, `filepath.Rel("/home/user", "atcr")` will likely error because `"atcr"` is relative, and `relHome` will fall back to `"atcr"`. That's fine.

**Major issue I almost missed**: `resolveHomeState` uses `log.FromContext(ctx)`. In `PersistentPreRunE`, the logger is set up and the context is enriched. But for bare `atcr`, `PersistentPreRunE` does run. However, what about `SilenceUsage: true` and `SilenceErrors: true`? Those are set. The root command has `Args: usageArgs(cobra.NoArgs)`. With `cobra.NoArgs`, if args are passed, it errors before `RunE`. Good.

Wait, is there any issue with `cmd.OutOrStdout()` in `runHome` being used while also potentially being redirected by `--axi` context? No, `cmd.OutOrStdout()` is standard cobra.

Let me check the test file `cmd/atcr/main_test.go`:

```go
func TestRootCmd_BareInvocationShowsHomeView(t *testing.T) {
    out, err := execute(t)
    require.NoError(t, err)
    assert.Contains(t, out, "Agent Team Code Review — a review panel, not a reviewer",
        "bare atcr must show the one-line description")
    assert.NotContains(t, out, "Usage:", "bare atcr must show the home view, not help text")
}
```

`execute(t)` likely runs the root command. Since `cmd/atcr/home.go` defines `homeExecutable = os.Executable`, and `execute` might be running in a test binary, `os.Executable()` returns the test binary path. `relHome` will try to relativize it. This might be flaky if the test binary is in a temp dir, but `relHome` falls back to verbatim path if not under home. So it's fine.

But wait: `homeExecutable` and `homeUserDir` are mutable vars, but no test in the diff actually mutates them. The seams are "so tests can pin...", but the visible tests don't use them. Are they dead code? They are there for future tests. Not an architectural issue per se, but introducing global mutable seams that aren't used is slight cargo-cult. I'll skip this.

**Anything about AXI flag registration?**
```go
root.Flags().Bool("axi", false, "emit the home view as a token-dense, ANSI/Markdown-free TOON payload on stdout for agent consumption (Agent eXperience Interface)")
```

This is a local flag. The help text mentions "home view" which is accurate. But if someone runs `atcr --help`, they'll see `--axi` in the root flags section. Previously, `--axi` was only on `review`. Now it's also on root. That's expected per the design.

Is there a risk of confusion? The root `--axi` is for home view. The review `--axi` is for review summary. The docs/agentic-consumption.md clarifies this. Good.

Let me check `docs/agentic-consumption.md` changes:
It adds the bare `atcr` row to the table and a new section explaining the home payload. No issues there.

**Potential pre-existing issue**: The `PersistentPreRunE` comment says "The flag lives only on `atcr review`". This is now false. But this is in unchanged code (the comment is part of the existing block shown in context around line 230 of main.go). The changed lines include the new `--axi` flag registration below. Should I flag the stale comment? The comment is inside the `PersistentPreRunE` block, which is shown in the diff context but the diff doesn't modify it. The changed lines in main.go are:
- `RunE: func...` changed
- Added `root.Flags().Bool("axi"...)`
- Other lines are context.

Since the stale comment is in unchanged lines, if I flag it, FILE:LINE would be outside changed lines and discarded during grounding. Unless I can cite it as part of the changed file context? The rules say: "a finding whose FILE:LINE falls outside the changed lines will be discarded". So I should not flag this, or if I do, use CATEGORY out-of-scope and know it will be annotated rather than discarded. But I should probably just skip it.

Now, looking for genuine architecture flaws in the changed code.

Wait, I see a coupling issue in `cmd/atcr/home.go`:
`resolveHomeState` calls `anchorDir("")` and `fanout.ReadReviewStatus`. These are the same calls `status.go` makes. The sprint plan explicitly says: "deliberately NOT `status.go`'s existing pattern of wrapping that same condition in `usageError()` (exit 2), since the home view must never error on a first-run/no-reviews state." So `status.go` has a parallel code path for resolving status, and `home.go` duplicates that logic but with different error handling. This is a classic duplication of responsibility / two sources of truth risk. If `status.go`'s way of resolving the review directory changes (e.g., it starts using a different function than `anchorDir`, or `anchorDir`'s signature changes), `home.go` might drift. The sprint plan acknowledges this is read-only reuse, but the *orchestration* of `anchorDir` + `ReadReviewStatus` is duplicated between `status.go` and `home.go`.

Is this flaggable? Yes. The plan says in OUT OF SCOPE: "Any change to `atcr status`'s existing behavior when explicitly invoked with an id/path argument." It doesn't say the home view can't reuse the resolution logic. But architecturally, having two places that know the sequence "call anchorDir, then ReadReviewStatus, then interpret errors" is duplication of responsibility. It would be better if there was a shared helper that resolves the state, returning a structured result that both `status.go` and `home.go` consume. However, `home.go` needs to distinguish `os.ErrNotExist` as a non-error (first-run), while `status.go` treats it as a usage error. So the error handling *must* differ. But the resolution *path* could be shared.

Given the explicit sprint plan guidance to call `anchorDir("")` directly and that this mirrors `status.go`, and the small scale of this change, I'll flag this as MEDIUM: coupling/duplication that will rot if not addressed. Specifically, `resolveHomeState` duplicates the anchor resolution + status reading sequence from `status.go` with divergent error handling.

Let me formulate the finding:
SEVERITY: MEDIUM
FILE:LINE: `cmd/atcr/home.go:78` (around `anchorDir("")` call)
PROBLEM: `resolveHomeState` duplicates the anchorDir+ReadReviewStatus sequence from status.go with parallel error-handling semantics; changes to review resolution in one will silently drift from the other
FIX: Extract a shared `resolveReviewState(dir string) (*fanout.ReviewStatus, error)` from status.go that returns structured errors (e.g., ErrNoLatest, ErrUnreadable) so home.go and status.go share the resolution path but apply their own exit semantics
CATEGORY: duplication
EST_MINUTES: 20
EVIDENCE: `anchorDir("")` followed by `fanout.ReadReviewStatus(dir, filepath.Base(dir))` mirrors status.go's resolution chain but manually bifurcates error handling

Actually, looking at the diff, I don't have `status.go`'s lines in the changed regions, so I cannot point to status.go lines. But I can point to the home.go lines and mention it mirrors status.go. The EVIDENCE should cite the offending code in the changed region.

Another issue: `cmd/atcr/home.go` line 25-28:
```go
var (
	homeExecutable = os.Executable
	homeUserDir    = os.UserHomeDir
)
```
This is shared mutable state for test seams. It enables tests to mutate package globals, which is a hidden dependency. If tests run in parallel, they race. Even if not, it couples test state to package state. Severity: LOW or MEDIUM? The prompt says "hidden dependencies, shared mutable state" is a focus under Coupling. I'll flag it.

SEVERITY: LOW
FILE:LINE: `cmd/atcr/home.go:25`
PROBLEM: Package-level mutable vars for os.Executable/os.UserHomeDir introduce shared mutable test seams that prevent parallel execution and leak test state
FIX: Make resolveHomeState accept the resolver functions as parameters, or use a small homeResolver interface passed into runHome
CATEGORY: coupling
EST_MINUTES: 15
EVIDENCE: `var ( homeExecutable = os.Executable; homeUserDir = os.UserHomeDir )`

Another potential issue: `runHome` returns `cmd.Help()` vs `runHome(cmd)`. Wait, the old code returned `cmd.Help()`. The new code returns `runHome(cmd)`. `runHome` can return errors from `renderHomeView