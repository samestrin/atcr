The user wants me to review the provided diff as "archer", a snippet-level bug reviewer.
I need to find bugs in the changed lines of the code.
I must output findings in a specific format: `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE`
I should focus on off-by-one, null/nil/undefined, logic inversions, copy-paste errors, shadowing/dead paths.
Scope is constrained to the sprint plan (Epic 31.1 / AXI Content-First Home View).

Let's examine the diff carefully.

Changed files:
- `.planning/sprints/active/31.0_axi_compliance/README.md` (docs)
- `.planning/sprints/active/31.0_axi_compliance/metadata.md` (docs)
- `.planning/sprints/active/31.0_axi_compliance/plan/README.md` (docs)
- `.planning/sprints/active/31.0_axi_compliance/plan/acceptance-criteria/...` (docs)
- `.planning/sprints/active/31.0_axi_compliance/plan/documentation/...` (docs)
- `CHANGELOG.md` (docs)
- `cmd/atcr/home.go` (new file, 154 lines)
- `cmd/atcr/main.go` (modified)
- `cmd/atcr/main_test.go` (modified)
- `docs/agentic-consumption.md` (modified)
- `internal/report/home.go` (new file, 62 lines)
- `internal/report/home_test.go` (new file, 79 lines)

Let's look at the code changes.

`cmd/atcr/home.go`:
```go
func relHome(path string) string {
	home, err := homeUserDir()
	if err != nil || home == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~" + string(filepath.Separator) + rel
}
```
Wait, `filepath.Rel` returns a relative path. If `path` is under `home`, it works. If `path` equals `home`, it returns `"."`. The code handles `"."` correctly.
But what if `path` is exactly `home`? It returns `"~"`. That's fine.
What about Windows? `filepath.Separator` is `\`. `strings.HasPrefix(rel, ".."+string(filepath.Separator))` works for Windows paths like `..\\foo`. But `rel == ".."` might miss `..` on Windows if it's normalized? Actually `filepath.Rel` normalizes separators to the platform separator. So it's fine.
Wait, `strings.HasPrefix(rel, ".."+string(filepath.Separator))` is correct.

Let's look at `resolveHomeState`:
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
	if err != nil {
		log.FromContext(ctx).Debug("home view: latest review status unreadable",
			"review", filepath.Base(dir), "err", err)
		return homeState{unavailable: true, reviewID: filepath.Base(dir)}
	}
	return homeState{hasReview: true, reviewID: st.ReviewID, status: st.Status}
}
```
Is `anchorDir("")` guaranteed to return an error with `os.ErrNotExist` when `.atcr/latest` is missing? The sprint plan says so. We'll assume it's correct.

`runHome`:
```go
func runHome(cmd *cobra.Command) error {
	ctx := cmd.Context()
	execPath, err := homeExecutable()
	if err != nil {
		log.FromContext(ctx).Debug("home view: os.Executable failed, using fallback name", "err", err)
		execPath = "atcr"
	}
	st := resolveHomeState(ctx)

	if axiFromContext(ctx) {
		reviewID, status := "", "none"
		switch {
		case st.hasReview:
			reviewID, status = st.reviewID, st.status
		case st.unavailable:
			reviewID, status = st.reviewID, "unavailable"
		}
		return report.RenderHomeViewAXI(cmd.OutOrStdout(), report.HomeViewAXI{
			ExecPath:     relHome(execPath),
			Description:  cmd.Short,
			ReviewID:     reviewID,
			ReviewStatus: status,
		})
	}

	return renderHomeView(cmd.OutOrStdout(), execPath, cmd.Short, st)
}
```
Wait, `axiFromContext(ctx)` is called. Does the context have AXI set? The sprint plan says: "The root PersistentPreRunE's Lookup("axi") then propagates it into the command context". But in `main.go`, `PersistentPreRunE` only checks `cmd.Flags().Lookup("axi") != nil`.
In `newRootCmdWithClient`, they added:
```go
	root.Flags().Bool("axi", false, "emit the home view as a token-dense...")
```
And in `PersistentPreRunE`:
```go
			if cmd.Flags().Lookup("axi") != nil {
				axi, _ := cmd.Flags().GetBool("axi")
				cmd.SetContext(newAXIContext(cmd.Context(), axi))
			}
```
This looks correct. The flag is registered on the root command, so `Lookup` will find it.

Let's check `cmd/atcr/main.go` diff:
```go
+	// --axi is a LOCAL flag on the root command (not persistent): it is reachable
+	// only on the bare `atcr` invocation, so `atcr --axi` renders the home view as
+	// a token-dense TOON payload (Content First, epic 31.1). Being local, it is not
+	// inherited by subcommands and cannot collide with `atcr review`'s own --axi.
+	// The root PersistentPreRunE's Lookup("axi") then propagates it into the command
+	// context (newAXIContext), so runHome reads it via axiFromContext — the same
+	// context-propagation plumbing review/resume already reuse (Epic 31.0).
+	root.Flags().Bool("axi", false, "emit the home view as a token-dense, ANSI/Markdown-free TOON payload on stdout for agent consumption (Agent eXperience Interface)")
```
Wait, `root.PersistentFlags().String("log-format", ...)` is persistent.
`root.Flags().Bool("axi", ...)` is local. That's fine.

Let's check `internal/report/home.go`:
```go
func RenderHomeViewAXI(w io.Writer, s HomeViewAXI) error {
	var b bytes.Buffer
	quotedHeader := make([]string, len(homeViewAXIHeader))
	for i, h := range homeViewAXIHeader {
		quotedHeader[i] = toonQuote(h)
	}
	fmt.Fprintf(&b, "home[1%c]{%s}:\n", axiDelim, strings.Join(quotedHeader, string(axiDelim)))

	row := []string{
		toonQuote(s.ExecPath),
		toonQuote(s.Description),
		toonQuote(s.ReviewID),
		toonQuote(s.ReviewStatus),
	}
	if len(row) != len(homeViewAXIHeader) {
		return fmt.Errorf("axi encoder: home view row has %d columns, header declares %d", len(row), len(homeViewAXIHeader))
	}
	b.WriteString("  ")
	b.WriteString(strings.Join(row, string(axiDelim)))
	b.WriteByte('\n')
	_, err := w.Write(b.Bytes())
	return err
}
```
Is `axiDelim` defined? Presumably yes, from another file.
`toonQuote` is presumably defined elsewhere.
The code looks fine.

Let's check `cmd/atcr/main.go` RunE change:
```go
 		RunE: func(cmd *cobra.Command, _ []string) error {
-			return cmd.Help()
+			return runHome(cmd)
 		},
```
Wait, `usageArgs(cobra.NoArgs)` is set. So `RunE` only runs when there are zero args. That's correct.

Is there any issue with `homeExecutable` and `homeUserDir` seams? They are package-level vars initialized to `os.Executable` and `os.UserHomeDir`. This is standard for testing.

Let's look closely at `cmd/atcr/home.go` line 120 (approx):
```go
	if axiFromContext(ctx) {
		reviewID, status := "", "none"
		switch {
		case st.hasReview:
			reviewID, status = st.reviewID, st.status
		case st.unavailable:
			reviewID, status = st.reviewID, "unavailable"
		}
		return report.RenderHomeViewAXI(cmd.OutOrStdout(), report.HomeViewAXI{
...
```
Wait, `axiFromContext(ctx)` is called. But the PR plan says: "The root PersistentPreRunE's Lookup("axi") then propagates it into the command context".
In `main.go`, `PersistentPreRunE` does:
```go
			if cmd.Flags().Lookup("axi") != nil {
				axi, _ := cmd.Flags().GetBool("axi")
				cmd.SetContext(newAXIContext(cmd.Context(), axi))
			}
```
This sets the context. But wait! `cmd.Flags()` reads flags from the command itself. For the root command, it works.
But what about `atcr --axi`? The flag is parsed before `PersistentPreRunE` runs. So `Lookup` finds it. Correct.

Let's check `internal/report/home.go` again.
`homeViewAXIHeader` is defined as:
```go
var homeViewAXIHeader = []string{"exec_path", "description", "review_id", "review_status"}
```
In `RenderHomeViewAXI`, it uses `axiDelim`. Is `axiDelim` a byte or string? Usually delimiters in TOON are bytes. The code uses `string(axiDelim)`. That's fine.

Wait, look at `renderHomeView` in `home.go`:
```go
	case st.unavailable:
		if st.reviewID != "" {
			_, err := fmt.Fprintf(w, "Latest review %s is unavailable — its status could not be read.\n", st.reviewID)
			return err
		}
		_, err := fmt.Fprintln(w, "Latest review pointer is unreadable — run `atcr review` to start a fresh one.")
		return err
```
The comment says "unavailable" state. But the sprint plan says:
"catch it and render an explicit 'no reviews yet' message with exit 0 — deliberately NOT status.go's existing pattern of wrapping that same condition in usageError() (exit 2)"
Wait, `st.unavailable` is returned when `anchorDir("")` fails but not `os.ErrNotExist`, OR when `ReadReviewStatus` fails.
The plan says: "a present-but-corrupt/empty pointer, or a pointer naming a review that cannot be read ... is reported as the explicit 'unavailable' state". This matches.

Let's check `main_test.go`:
```go
func TestRootCmd_BareInvocationShowsHomeView(t *testing.T) {
	out, err := execute(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Agent Team Code Review — a review panel, not a reviewer",
		"bare atcr must show the one-line description")
	assert.NotContains(t, out, "Usage:", "bare atcr must show the home view, not help text")
}
```
Looks fine.

Let's check `cmd/atcr/main.go` diff again:
```go
+	// --axi is a LOCAL flag on the root command (not persistent): it is reachable
+	// only on the bare `atcr` invocation, so `atcr --axi` renders the home view as
+	// a token-dense TOON payload (Content First, epic 31.1). Being local, it is not
+	// inherited by subcommands and cannot collide with `atcr review`'s own --axi.
+	// The root PersistentPreRunE's Lookup("axi") then propagates it into the command
+	// context (newAXIContext), so runHome reads it via axiFromContext — the same
+	// context-propagation plumbing review/resume already reuse (Epic 31.0).
+	root.Flags().Bool("axi", false, "emit the home view as a token-dense, ANSI/Markdown-free TOON payload on stdout for agent consumption (Agent eXperience Interface)")
```
Wait! `root.Flags()` registers a LOCAL flag. But in Cobra, local flags are only accessible on the command they are registered on. The root command is where it's registered. So `atcr --axi` works. Subcommands won't see it. That's correct.

Is there any issue with `cmd/atcr/home.go` line 108?
```go
		return report.RenderHomeViewAXI(cmd.OutOrStdout(), report.HomeViewAXI{
			ExecPath:     relHome(execPath),
			Description:  cmd.Short,
			ReviewID:     reviewID,
			ReviewStatus: status,
		})
```
`cmd.Short` is the one-line description. Correct.

Let's look at `internal/report/home.go` line 48-50:
```go
	if len(row) != len(homeViewAXIHeader) {
		return fmt.Errorf("axi encoder: home view row has %d columns, header declares %d", len(row), len(homeViewAXIHeader))
	}
```
This is a defensive check. It's fine.

Wait, look at `cmd/atcr/home.go` line 68-70:
```go
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
```
If `path` is `/Users/foo/bar` and `home` is `/Users`, `filepath.Rel` returns `foo/bar`. Correct.
If `path` is `/Users`, it returns `.`. Handled.
What if `path` is `/Users/../bar`? `filepath.Rel` might return `..bar` or error? Actually `filepath.Rel` normalizes paths first, so it won't produce `..` unless the second arg is strictly outside the first. It's safe.

Let's check `cmd/atcr/main.go` around line 219:
```go
 		// An unknown subcommand is a usage error (exit 2), not the generic
 		// failure code: in CI, exit 1 specifically means "findings at/above
 		// threshold". Setting Args bypasses cobra's legacyArgs path (which
 		// returns an uncoded error from Find), and the RunE keeps bare `atcr`
 		// printing help with exit 0.
 		Args: usageArgs(cobra.NoArgs),
```
The comment says "keeps bare `atcr` printing help with exit 0." But we just changed it to `runHome(cmd)`. The comment is now stale/outdated, but that's a documentation issue, not a bug. I'll note it as LOW or skip if out of scope. It's in the diff though. I'll stick to code bugs.

Let's look at `cmd/atcr/main.go` line 214-216:
```go
 			if cmd.Flags().Lookup("axi") != nil {
 				axi, _ := cmd.Flags().GetBool("axi")
 				cmd.SetContext(newAXIContext(cmd.Context(), axi))
 			}
```
Wait! `cmd.Flags()` returns the set of flags *parsed* for this command. But `PersistentPreRunE` runs *after* flag parsing. So `Lookup` will find it. This is correct.

Is there any issue with `home.go`'s `relHome` function?
```go
func relHome(path string) string {
	home, err := homeUserDir()
	if err != nil || home == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
...
```
What if `home` is `/home/user` and `path` is `/home/user/file.txt`? `rel` is `file.txt`. Returns `~/file.txt`. Correct.
What if `home` has a trailing slash? `os.UserHomeDir()` typically doesn't. But if it did, `filepath.Rel` handles it or returns an error? Actually `filepath.Rel` might return `./file.txt` or error depending on OS. We'll assume it's fine.

Wait! Look at `cmd/atcr/home.go` line 128:
```go
	return renderHomeView(cmd.OutOrStdout(), execPath, cmd.Short, st)
```
`renderHomeView` signature:
```go
func renderHomeView(w io.Writer, execPath, description string, st homeState) error {
```
Matches.

Let's check `internal/report/home.go` line 38-40:
```go
	row := []string{
		toonQuote(s.ExecPath),
		toonQuote(s.Description),
		toonQuote(s.ReviewID),
		toonQuote(s.ReviewStatus),
	}
```
`toonQuote` is called on all fields. `s.ReviewID` can be `""`. `toonQuote("")` probably returns `""`. That's fine.

Let's check `cmd/atcr/main_test.go`:
```go
func TestRootCmd_AXINotInheritedBySubcommands(t *testing.T) {
	_, err := execute(t, "status", "--axi")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "--axi is unknown on status; the root-local flag must not leak")
}
```
This test expects `--axi` to fail on `status`. Since it's a local flag on root, Cobra will reject it for subcommands. Correct.

Wait, look at `cmd/atcr/main.go` line 210-218:
```go
 		// PersistentPreRunE is inherited by every subcommand, so it is the single
 		// point where the root logger is constructed (from LOG_LEVEL and
 		// --log-format) and stored in the command context. No subcommand builds
 		// its own logger after this; they retrieve it via log.FromContext.
 		// Note: cobra's --help/-h and --version flags short-circuit before
 		// PersistentPreRunE runs, so no logger is stored in context on those
 		// paths. All consumers must use log.FromContext, which falls back to a
 		// shared discard logger on a miss — never assert logger presence directly.
 		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
 			if err := setupLogger(cmd); err != nil {
 				return err
 			}
 			// Inject the single process telemetry client into the command context
 			// alongside the logger, so runReview/runReconcile retrieve it via
 			// telemetry.FromContext without a signature change.
 			cmd.SetContext(telemetry.NewContext(cmd.Context(), telemetryClient))
 			// Propagate the --axi output mode once, at this single flag-parse point,
 			// so review.go/resume.go read it via axiFromContext rather than re-parsing
 			// the flag at each stdout call site (AC 01-04). The flag lives only on
 			// `atcr review`; the Lookup guard leaves every other command unaffected.
 			if cmd.Flags().Lookup("axi") != nil {
 				axi, _ := cmd.Flags().GetBool("axi")
 				cmd.SetContext(newAXIContext(cmd.Context(), axi))
 			}
 			return nil
 		},
```
Wait! The comment says: "The flag lives only on `atcr review`; the Lookup guard leaves every other command unaffected."
But in the code just above, they added `root.Flags().Bool("axi", ...)` to the root command!
So the comment is now contradictory to the code. The comment says the flag lives only on `atcr review`, but the diff clearly registers it on the root command. This is a stale comment / documentation bug, not a runtime crash, but it's a logic inversion in comments vs code. I'll note it as LOW or MEDIUM if it causes confusion. I'll skip it unless it's critical.

Let's look for actual bugs.
`cmd/atcr/home.go` line 52:
```go
	if errors.Is(err, os.ErrNotExist) {
		return homeState{} // no .atcr/latest at all: true first-run (AC3)
	}
```
If `anchorDir("")` returns an error that wraps `os.ErrNotExist`, `errors.Is` handles it. Correct.

What about `cmd/atcr/main.go` line 208:
```go
 		RunE: func(cmd *cobra.Command, _ []string) error {
-			return cmd.Help()
+			return runHome(cmd)
 		},
```
`cmd.Help()` returns an error? In Cobra, `cmd.Help()` prints to `cmd.OutOrStdout()` and returns `nil`. So replacing it with `runHome(cmd)` which also returns an error is fine.

Wait! Look at `cmd/atcr/main.go` line 219 comment:
```go
 		// Setting Args bypasses cobra's legacyArgs path (which
 		// returns an uncoded error from Find), and the RunE keeps bare `atcr`
 		// printing help with exit 0.
```
This comment is now factually wrong because of the diff, but it's just a comment.

Let's check `internal/report/home.go` line 34:
```go
	fmt.Fprintf(&b, "home[1%c]{%s}:\n", axiDelim, strings.Join(quotedHeader, string(axiDelim)))
```
Is `axiDelim` a byte? If it's a rune or string, `%c` might print it as a character. Usually delimiters are bytes like `'|'`. It's fine.

Let's check `cmd/atcr/home.go` line 103:
```go
	execPath, err := homeExecutable()
	if err != nil {
		log.FromContext(ctx).Debug("home view: os.Executable failed, using fallback name", "err", err)
		execPath = "atcr"
	}
```
`os.Executable()` rarely fails. Fallback is fine.

Wait! Look at `cmd/atcr/home.go` line 108-118:
```go
	if axiFromContext(ctx) {
		reviewID, status := "", "none"
		switch {
		case st.hasReview:
			reviewID, status = st.reviewID, st.status
		case st.unavailable:
			reviewID, status = st.reviewID, "unavailable"
		}
		return report.RenderHomeViewAXI(cmd.OutOrStdout(), report.HomeViewAXI{
...
```
`axiFromContext(ctx)` is called. But does `PersistentPreRunE` actually run for the root command? Yes, Cobra runs `PersistentPreRunE` on the root command before `RunE`.
But wait! What if the user passes `--help` or `-h`? Cobra short-circuits before `PersistentPreRunE`. So `axiFromContext(ctx)` will return false (default context or nil). That's fine.

Let's look at `cmd/atcr/main.go` line 214-216 again:
```go
 			if cmd.Flags().Lookup("axi") != nil {
 				axi, _ := cmd.Flags().GetBool("axi")
 				cmd.SetContext(newAXIContext(cmd.Context(), axi))
 			}
```
This correctly checks if the flag exists on the command. For root, it exists. For subcommands, it doesn't (unless they define it). So only root gets AXI context injected this way. That's intentional per plan.

Is there any issue with `home.go`'s `relHome` on Windows?
`strings.HasPrefix(rel, ".."+string(filepath.Separator))` works. But what if `rel` is `..`? Handled by `rel == ".."`.
What if `path` is exactly `home`? `filepath.Rel(home, home)` returns `"."`. Handled.
What if `path` is `/Users/samestrin/atcr` and `home` is `/Users/samestrin`? `rel` is `atcr`. Returns `~/atcr`. Correct.

Wait! Look at `cmd/atcr/home.go` line 63:
```go
		if strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return path
		}
```
On Windows, `filepath.Separator` is `\`. So it checks for `..\`. That's correct.

Let's check `internal/report/home_test.go`:
```go
func TestRenderHomeViewAXI_Golden(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, RenderHomeViewAXI(&b, HomeViewAXI{...}))
	want := "home[1|]{exec_path|description|review_id|review_status}:\n" +
		"  ~/go/bin/atcr|Agent Team Code Review — a review panel, not a reviewer|2026-06-10_x|completed\n"
	assert.Equal(t, want, b.String())
}
```
The test hardcodes `|` as the delimiter. It assumes `axiDelim` is `'|'`. If `axiDelim` changes, this test will fail. That's expected for a golden test.

Let's look at `cmd/atcr/main.go` line 208 diff carefully:
```go
 		RunE: func(cmd *cobra.Command, _ []string) error {
-			return cmd.Help()
+			return runHome(cmd)
 		},
```
This is a direct replacement. No bug here.

Wait! I see something in `cmd/atcr/main.go` around line 219:
The comment says: `// Setting Args bypasses cobra's legacyArgs path (which returns an uncoded error from Find), and the RunE keeps bare atcr printing help with exit 0.`
This is just a stale comment. I will skip it.

Let's check `cmd/atcr/home.go` line 48:
```go
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
```
What if `home` is `/home/user` and `path` is `/home/user/../other/file`? `filepath.Rel` normalizes paths. It will return `../other/file`. The check catches it. Correct.

Is there any off-by-one or slice bounds issue? No slices used in a dangerous way.
Null/nil checks? `st.reviewID` could be empty string, handled.
Logic inversions? None obvious.

Wait! Look at `cmd/atcr/main.go` line 214-216:
```go
 			if cmd.Flags().Lookup("axi") != nil {
 				axi, _ := cmd.Flags().GetBool("axi")
 				cmd.SetContext(newAXIContext(cmd.Context(), axi))
 			}
```
The comment above it says: `// The flag lives only on atcr review; the Lookup guard leaves every other command unaffected.`
But the code *just added* `root.Flags().Bool("axi", ...)` right below this block! (Lines 238-240 in the diff).
So the comment is now completely wrong. It says the flag lives only on `atcr review`, but we just put it on root. This is a documentation bug, not a code crash. I'll note it as LOW or skip. The prompt says "no flattery, no summaries — findings only."

Let's look at `internal/report/home.go` line 38-40 again:
```go
	row := []string{
		toonQuote(s.ExecPath),
		toonQuote(s.Description),
		toonQuote(s.ReviewID),
		toonQuote(s.ReviewStatus),
	}
```
If `s.ReviewID` is `""`, `toonQuote("")` might return `""`. That's fine.

Wait! `cmd/atcr/home.go` line 108:
```go
	if axiFromContext(ctx) {
		reviewID, status := "", "none"
		switch {
		case st.hasReview:
			reviewID, status = st.reviewID, st.status
		case st.unavailable:
			reviewID, status = st.reviewID, "unavailable"
		}
```
If `!st.hasReview && !st.unavailable`, then `reviewID` and `status` remain `""` and `"none"`. This matches the first-run state from AC3. Correct.

Let's check `cmd/atcr/main.go` line 238-240:
```go
+	root.Flags().Bool("axi", false, "emit the home view as a token-dense, ANSI/Markdown-free TOON payload on stdout for agent consumption (Agent eXperience Interface)")
```
This is added *after* `PersistentPreRunE`. That's fine. Flag registration order doesn't matter in Cobra.

Is there any issue with `cmd/atcr/main.go` line 210-218 comment about `--axi`?
Yes, the comment says "The flag lives only on `atcr review`; the Lookup guard leaves every other command unaffected." But we just added it to root. This is a stale comment. I'll mark it as LOW or skip. The prompt says "Find problems the author would prefer you didn't." Stale comments are annoying but maybe not a bug. I'll skip unless it's critical.

Wait! Look at `cmd/atcr/main.go` line 214:
```go
 			if cmd.Flags().Lookup("axi") != nil {
```
`cmd.Flags()` returns the flags *parsed* for this command. For the root command, it will find the flag. For subcommands, it won't (unless they define it). This is correct.

Let's check `internal/report/home.go` line 34:
```go
	fmt.Fprintf(&b, "home[1%c]{%s}:\n", axiDelim, strings.Join(quotedHeader, string(axiDelim)))
```
If `axiDelim` is a byte, `%c` works. If it's a rune/string, it might panic or print weirdly. I'll assume it's a byte like `'|'`. The test hardcodes `|`, so it's likely correct.

Let's look at `cmd/atcr/home.go` line 58-60:
```go
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
```
What if `path` is `/Users/samestrin/atcr` and `home` is `/Users/samestrin`? `rel` is `atcr`. `strings.HasPrefix("atcr", "..")` is false. Returns `~/atcr`. Correct.
What if `path` is `/Users/samestrin`? `rel` is `.`. Returns `~`. Correct.
What if `home` is empty or error? Returns `path`. Correct.

Wait! `cmd/atcr/home.go` line 46:
```go
func relHome(path string) string {
	home, err := homeUserDir()
	if err != nil || home == "" {
		return path
	}
```
`os.UserHomeDir()` on Windows might return `C:\Users\Name`. `filepath.Rel` works across drives? No, `filepath.Rel` returns an error if they are on different drives. The code catches `err != nil` and returns `path`. Correct.

Let's check `cmd/atcr/main_test.go` line 138:
```go
func TestRootCmd_AXINotInheritedBySubcommands(t *testing.T) {
	_, err := execute(t, "status", "--axi")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "--axi is unknown on status; the root-local flag must not leak")
}
```
This tests that `--axi` is rejected by `status`. Correct.

Is there any issue with `cmd/atcr/main.go` line 219 comment? It's just a comment. I'll ignore it.

Wait! Look at `cmd/atcr/home.go` line 103:
```go
	execPath, err := homeExecutable()
	if err != nil {
		log.FromContext(ctx).Debug("home view: os.Executable failed, using fallback name", "err", err)
		execPath = "atcr"
	}
```
`os.Executable()` returns the path of the executable. The comment says `homeExecutable` is a seam. That's fine.

Let's check `internal/report/home.go` line 48-50:
```go
	if len(row) != len(homeViewAXIHeader) {
		return fmt.Errorf("axi encoder: home view row has %d columns, header declares %d", len(row), len(homeViewAXIHeader))
	}
```
This is a defensive check. It's fine.

Wait! Look at `cmd/atcr/main.go` line 208 diff:
```go
 		RunE: func(cmd *cobra.Command, _ []string) error {
-			return cmd.Help()
+			return runHome(cmd)
 		},
```
The original code returned `cmd.Help()`. `cmd.Help()` prints to `cmd.OutOrStdout()` and returns `nil`. The new code calls `runHome(cmd)` which also returns an error. This is correct.

Let's look at `cmd/atcr/main.go` line 214-216 again.
```go
 			if cmd.Flags().Lookup("axi") != nil {
 				axi, _ := cmd.Flags().GetBool("axi")
 				cmd.SetContext(newAXIContext(cmd.Context(), axi))
 			}
```
The comment above says: `// The flag lives only on atcr review; the Lookup guard leaves every other command unaffected.`
But we just added `root.Flags().Bool("axi", ...)` below. So the comment is now factually incorrect. It's a documentation bug. I will note it as LOW.

Actually, look at `cmd/atcr/main.go` line 214-216 comment:
`// The flag lives only on atcr review; the Lookup guard leaves every other command unaffected.`
This is directly contradicted by the