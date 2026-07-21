The user wants me to review the provided diff for bugs.
I need to look for:
1. Off-by-one errors
2. Null/nil/undefined dereferences
3. Logic inversions
4. Copy-paste errors
5. Shadowing and dead paths

Focus on changed regions. Output format is strict: `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE`

Let's examine the diff carefully.

**File: `cmd/atcr/autofix.go`**
- Changes to `autoFixBackend` struct adding `noSandbox bool`.
- Changes to `validateAutoFixBackend`: sets `be.noSandbox = true` when `noSandbox` flag is true.
- Changes to `runAutoFix`: replaces `if be.sandboxBackend != nil { ... } else { ... }` with a `switch` statement:
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(...)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(...)
	default:
		// Fail closed...
	}
```
Wait, look at the `default` case in `runAutoFix`:
```go
	default:
		// Fail closed: a nil sandbox backend WITHOUT the --no-sandbox opt-out must
		// never silently run unsandboxed on the host. ...
		if rerr := autofix.RevertPatch(ctx, bm); rerr != nil {
			return fmt.Errorf("auto-fix: refusing unsandboxed host validation without --no-sandbox AND revert failed: %w", rerr)
		}
		return fmt.Errorf("auto-fix: refusing to run validation unsandboxed on the host without an explicit --no-sandbox opt-out (no sandbox backend was resolved); working tree reverted, no GitHub changes made")
```
This looks correct. The logic inverts from `if nil { host } else { sandbox }` to `switch`. Wait, originally it was:
```go
	if be.sandboxBackend != nil {
		res, verr = verify.RunSandboxedValidation(...)
	} else {
		res, verr = verify.RunConfiguredValidation(...)
	}
```
Now it's a switch. It correctly routes to host only if `noSandbox` is true. The `default` handles the refusal. Looks solid.

**File: `cmd/atcr/autofix_integration_test.go`**
- Adds `noSandbox: true` to test structs. All good.

**File: `cmd/atcr/autofix_test.go`**
- Adds `noSandbox: true` to many test structs.
- Updates `TestRunAutoFix_NilSandboxUsesHostPath` to require `noSandbox: true`.
- Adds `TestRunAutoFix_NilSandboxWithoutOptOutRefuses`. All good.

**File: `internal/sandbox/docker.go`**
- Task 2: Fold `context.Canceled` into `TimedOut`.
```go
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
		res.TimedOut = true
		res.ExitCode = timeoutExitCode
		// ... kill container ...
		return res, nil
	}
```
Wait, `runCtx` is created with `context.WithTimeout(ctx, timeout)`. If `ctx` (the parent context) gets canceled, `runCtx.Err()` will return `context.Canceled`. This works.
But look at the kill logic:
```go
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if killErr := exec.CommandContext(killCtx, b.cfg.DockerPath, "kill", containerName).Run(); killErr != nil {
			logger.Warn(...)
		}
		killCancel()
```
Is there an issue here? `context.Background()` is fine.
Wait, what about the `runCtx` being dead? The `cmd.Run()` already returned because `runCtx` was canceled. So `cmd` might have exited or been killed by Go's `exec` package. The code then tries to kill the container explicitly. This is intended.

Let's check `docker_test.go`:
```go
func TestDockerBackend_Run_CancellationClassIsTimedOut(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T) context.Context
		timeout time.Duration
	}{
		{
			name:    "deadline exceeded",
			setup:   func(*T) context.Context { return context.Background() },
			timeout: 150 * time.Millisecond,
		},
		{
			name: "context canceled",
			setup: func(t *T) context.Context { ... }
			timeout: 5 * time.Second,
		},
```
Wait, `t` is shadowed in the closure? `func(t *testing.T) context.Context` vs `func(*testing.T)`. The first case uses `*testing.T`, second uses `t *testing.T`. That's fine.

Look at `docker.go` line ~232 (in diff):
```go
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
```
Is there a bug here? `runCtx` is derived from `ctx`. If `ctx` is canceled, `runCtx.Err()` returns `context.Canceled`. That's correct.
But wait, what if `cmd.Run()` fails for another reason *at the exact same time* as context cancellation? `cmd.Run()` returns an error if the process exits non-zero or fails to start. If context cancels, Go's `exec` package kills the process and `cmd.Run()` returns a `*exec.ExitError` (usually exit code 137 for SIGKILL) OR it might return `context.Canceled` directly? Actually, `exec.CommandContext` returns an error wrapping `context.Canceled` or `context.DeadlineExceeded` if the context expires, but sometimes it returns an `*exec.ExitError` with signal death. The code checks `runCtx.Err()` first. That's fine.

Wait, look at this part in `docker.go`:
```go
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if killErr := exec.CommandContext(killCtx, b.cfg.DockerPath, "kill", containerName).Run(); killErr != nil {
			logger.Warn("sandbox exec kill-on-timeout failed", ...
		}
```
If `b.cfg.DockerPath` is empty or invalid, this will panic or fail. But it's just a warn.

Let's check the diff for `internal/sandbox/docker.go` carefully:
```go
-	if runCtx.Err() == context.DeadlineExceeded {
+	if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
```
This is correct.

Now, look at `cmd/atcr/autofix.go`:
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		// Host path — reachable ONLY via the explicit --no-sandbox opt-out...
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
		// Fail closed...
```
Wait! `be.noSandbox` is a boolean. If `be.sandboxBackend != nil` is true, it takes the first case. That's fine.
But what if `be.noSandbox` is true and `be.sandboxBackend` is also not nil? The switch checks `be.sandboxBackend != nil` first, so it will use sandboxed validation. That matches the intent: "sandbox backend present -> sandboxed; else if noSandbox -> host; else refuse". This is correct.

Let's look at `validateAutoFixBackend`:
```go
	noSandbox, _ := cmd.Flags().GetBool("no-sandbox")
	if noSandbox {
		be.noSandbox = true
		warnNoSandbox(cmd.ErrOrStderr())
	} else if len(missing) == 0 {
		// Resolve/preflight the sandbox...
```
Wait, `cmd.Flags().GetBool("no-sandbox")` returns `(true, false)` if the flag is set to true. The `_` discards the "known" boolean. This is fine.

Is there any issue with `be.noSandbox` initialization? In Go, struct fields default to zero value (`false`). So if a test constructs `autoFixBackend{}` without setting `noSandbox`, it defaults to `false`. The tests explicitly set it to `true` where needed. This is correct.

Let's check `cmd/atcr/autofix_integration_test.go`:
```go
		noSandbox: true, // explicit --no-sandbox opt-out (host path)
```
All good.

Check `cmd/atcr/autofix_test.go`:
```go
		noSandbox: true, // explicit --no-sandbox opt-out (host path)
```
All good.

Wait, look at `docker.go` line ~230:
```go
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
		res.TimedOut = true
		res.ExitCode = timeoutExitCode
		// The killed CLI does not stop the container; kill it explicitly so its
		// caps are reclaimed. --rm (in args) removes it once it stops. Use a fresh
		// background context so the kill is not itself cut short by the dead runCtx.
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if killErr := exec.CommandContext(killCtx, b.cfg.DockerPath, "kill", containerName).Run(); killErr != nil {
			logger.Warn("sandbox exec kill-on-timeout failed", "backend", "docker", "container", containerName, "error", killErr)
		}
		killCancel()
```
Is `b.cfg.DockerPath` guaranteed to be non-empty here? It's checked in `Preflight`. But if someone constructs `DockerBackend` directly without running Preflight, it might be empty. `exec.CommandContext(killCtx, "", ...)` would run an empty string as a command, which is fine (it fails). Not a crash.

Wait, look at this line in `docker.go`:
```go
		if killErr := exec.CommandContext(killCtx, b.cfg.DockerPath, "kill", containerName).Run(); killErr != nil {
```
If `b.cfg.DockerPath` is empty, it runs an empty command. Not a crash.

Let's check the switch logic in `runAutoFix` again.
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
```
Is there any case where `be.validateArgv` is nil or empty when calling `RunConfiguredValidation`? In `validateAutoFixBackend`, it sets `be.validateArgv` from the gate. The gate ensures it's not empty (or returns an error). So it's safe.

Wait, look at `cmd/atcr/autofix.go` diff around line 380:
```go
	case be.noSandbox:
		// Host path — reachable ONLY via the explicit --no-sandbox opt-out, which is
		// the sole setter of be.noSandbox (validateAutoFixBackend). AC 01-03/03-02.
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
		// Fail closed...
```
This looks correct.

Let's check `internal/sandbox/docker_test.go` carefully.
```go
func TestDockerBackend_Run_CancellationClassIsTimedOut(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T) context.Context
		timeout time.Duration
	}{
		{
			name:    "deadline exceeded",
			setup:   func(*T) context.Context { return context.Background() }, // Typo here!
			timeout: 150 * time.Millisecond,
		},
```
Wait! `func(*T) context.Context` -> `*T` is not defined. It should be `*testing.T`.
Let's check the diff exactly:
```diff
+		setup:   func(*T) context.Context { return context.Background() },
```
Yes, `*T` instead of `*testing.T`. This will cause a compile error or panic if used (though it won't be used since `deadline exceeded` case uses `context.Background()` anyway, but it's invalid Go syntax). Actually, Go will reject `func(*T)` because `T` is not defined in scope. It should be `func(*testing.T)`.
Wait, the diff says:
```go
+		setup:   func(*T) context.Context { return context.Background() },
```
This is a clear typo. It will fail to compile.

Let's verify the exact line in the payload:
```diff
+			name:    "deadline exceeded",
+			setup:   func(*T) context.Context { return context.Background() },
+			timeout: 150 * time.Millisecond,
```
Yes, `*T` is a typo for `*testing.T`. This is a compile error.

Severity: CRITICAL (or HIGH/LOW? It's a syntax error in the test file, so it won't compile. I'll mark it as HIGH or CRITICAL. Since it's in `_test.go`, it prevents tests from running, but doesn't affect production. I'll call it HIGH because it breaks the build/tests.)
Actually, it's a typo in a struct literal's function parameter type. Go will complain: `undefined: T`.

Let's check if there are any other typos.
In `docker_test.go`:
```go
+		setup: func(t *T) context.Context {
```
Wait, the second case also has `t *T`? Let's look at the diff:
```diff
+			setup: func(t *testing.T) context.Context {
```
The second case correctly uses `*testing.T`. The first case uses `*T`. This is definitely a typo.

Let's check `cmd/atcr/autofix.go` for any other issues.
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
```
Is `be.validateArgv` guaranteed to be non-nil? In Go, zero value is nil. If it's passed to a function expecting `[]string`, it might be fine, but sometimes functions check `len() == 0`. The gate ensures it's populated if the backend is valid. So it's fine.

Check `internal/sandbox/docker.go`:
```go
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
```
This is correct.

What about `cmd/atcr/autofix_test.go`?
```go
+// TestRunAutoFix_NilSandboxWithoutOptOutRefuses: a backend with NO sandbox and NO
+// explicit --no-sandbox opt-out (noSandbox=false) must fail closed — refuse before
...
+			noSandbox:      false, // and no explicit opt-out
```
This is correct.

Let's double check the `*T` typo in `docker_test.go`.
Line ~248 in the new file: `setup: func(*T) context.Context { return context.Background() },`
Yes, definitely a typo. It will cause a compilation error for tests.
I will report it.

Are there any other issues?
In `cmd/atcr/autofix.go`:
```go
	default:
		// Fail closed...
		if rerr := autofix.RevertPatch(ctx, bm); rerr != nil {
			return fmt.Errorf("auto-fix: refusing unsandboxed host validation without --no-sandbox AND revert failed: %w", rerr)
		}
		return fmt.Errorf("auto-fix: refusing to run validation unsandboxed on the host without an explicit --no-sandbox opt-out (no sandbox backend was resolved); working tree reverted, no GitHub changes made")
```
This is fine.

In `cmd/atcr/autofix_integration_test.go`:
```go
		noSandbox: true, // explicit --no-sandbox opt-out (host path)
```
All good.

Check `internal/sandbox/docker.go` line ~230 again:
```go
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
```
Wait, `runCtx` is created with `context.WithTimeout(ctx, timeout)`. If `ctx` is canceled, `runCtx.Err()` returns `context.Canceled`. This works.
But what if `cmd.Run()` returns an error *before* checking `runCtx.Err()`? The code checks `runCtx.Err()` immediately after `cmd.Run()`, so it's fine.

One minor thing: `errors.Is(runCtx.Err(), context.Canceled)` might be redundant if `context.DeadlineExceeded` covers timeouts, but the epic explicitly asks to fold `context.Canceled` into TimedOut. So it's intentional.

Let's look at `cmd/atcr/autofix.go` switch statement again:
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
```
Is there a case where `be.sandboxBackend` is nil and `be.noSandbox` is false? Yes, the default case handles it.
What if `be.sandboxBackend` is not nil but `be.noSandbox` is also true? The first case matches, so sandboxed validation runs. This matches the intent: "sandbox backend present -> sandboxed". The epic says: "host execution requires be.noSandbox; a nil backend without the opt-out refuses". It doesn't explicitly say what happens if both are set, but prioritizing sandbox is logical and safe.

Wait, look at `validateAutoFixBackend`:
```go
	noSandbox, _ := cmd.Flags().GetBool("no-sandbox")
	if noSandbox {
		be.noSandbox = true
		warnNoSandbox(cmd.ErrOrStderr())
	} else if len(missing) == 0 {
		// Resolve/preflight the sandbox...
```
If `--no-sandbox` is passed, it sets `be.noSandbox = true` and skips sandbox resolution. `be.sandboxBackend` remains nil. This is correct.

Check `cmd/atcr/autofix_test.go` for `TestRunAutoFix_SandboxTimeoutReportsTimeoutNotExitZero`:
The diff doesn't show changes there, but it's fine.

Let's check the typo in `docker_test.go` again.
```diff
+			name:    "deadline exceeded",
+			setup:   func(*T) context.Context { return context.Background() },
+			timeout: 150 * time.Millisecond,
```
This is definitely a compile error. `T` is not defined. Should be `*testing.T`.

I will report this as HIGH severity (build break in tests) or CRITICAL? It's in `_test.go`, so it won't affect production binaries, but it breaks the test suite. I'll mark it HIGH. Category: correctness/syntax. Actually, "syntax" isn't in the allowed categories, but "correctness" is fine. Or "build". The prompt says CATEGORY is one lowercase word. I'll use `correctness`.

Wait, are there any other issues?
In `cmd/atcr/autofix.go`:
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
```
Is `be.validateArgv` guaranteed to be non-nil? In Go, if it's nil, passing it to a function that expects `[]string` is usually fine unless the function does `len(be.validateArgv) > 0` or iterates. The gate ensures it's set if valid. So it's fine.

Check `internal/sandbox/docker.go`:
```go
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if killErr := exec.CommandContext(killCtx, b.cfg.DockerPath, "kill", containerName).Run(); killErr != nil {
			logger.Warn("sandbox exec kill-on-timeout failed", "backend", "docker", "container", containerName, "error", killErr)
		}
		killCancel()
```
If `b.cfg.DockerPath` is empty, `exec.CommandContext` runs an empty command. It will fail with `exec: "" must be an executable path`. That's fine, it's caught by `killErr != nil` and logged as a warning. Not a crash.

What about `cmd/atcr/autofix_integration_test.go`:
```go
		be := autoFixBackend{
			applyTarget:     applyDir,
			validateArgv:    []string{"true"}, // validation passes
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
			apiURL:    srv.URL,
			noSandbox: true, // explicit --no-sandbox opt-out (host path)
		}
```
All good.

Let's verify the `*T` typo line number in `docker_test.go`. The diff shows it around line 248. I'll cite the exact snippet.

One more thing: In `cmd/atcr/autofix.go`, the switch statement:
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
```
Wait, `be.noSandbox` is a boolean. If it's true, it routes to host. But what if `be.sandboxBackend` is also not nil? The first case matches. This is correct.
But look at the comment in the diff for `runAutoFix`:
```go
 	// Route validation through the resolved sandbox backend when one is present
-	// (the default sandboxed-on posture), else run it directly on the host — the
-	// --no-sandbox opt-out, which leaves be.sandboxBackend nil (AC 01-03/03-02).
+	// (the default sandboxed-on posture), else run it directly on the host ONLY when
+	// the operator explicitly opted out via --no-sandbox (be.noSandbox). A nil
+	// backend WITHOUT that opt-out fails closed rather than falling back to the host:
```
This matches the code.

Is there any issue with `errors.Is(runCtx.Err(), context.Canceled)` in `docker.go`?
`runCtx` is created via `context.WithTimeout(ctx, timeout)`. If `ctx` is canceled, `runCtx.Err()` returns `context.Canceled`. This works correctly.

Wait, look at `docker_test.go` again:
```diff
+		setup:   func(*T) context.Context { return context.Background() },
```
This is definitely a typo. I will report it.

Are there any other typos or bugs?
In `cmd/atcr/autofix_test.go`:
```go
+			noSandbox:      false, // and no explicit opt-out
```
This is fine.

In `cmd/atcr/autofix_integration_test.go`:
```go
		be := autoFixBackend{
...
			noSandbox: true, // explicit --no-sandbox opt-out (host path)
```
All good.

Let's check `validateAutoFixBackend` again:
```go
	noSandbox, _ := cmd.Flags().GetBool("no-sandbox")
	if noSandbox {
		be.noSandbox = true
		warnNoSandbox(cmd.ErrOrStderr())
	} else if len(missing) == 0 {
```
If `--no-sandbox` is false, it goes to `else if len(missing) == 0`. This is correct.

What about `cmd/atcr/autofix.go` line ~380:
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
```
Is `be.validateArgv` guaranteed to be non-nil? If `af.ValidateCommand` is empty and `ResolveValidateCommand` returns an error, `missing` gets populated and the function returns early. So if we reach `runAutoFix`, `be.validateArgv` is definitely populated (or at least not nil, depending on `ResolveValidateCommand` implementation, but typically it returns a non-nil slice or error). The gate ensures validity.

One potential issue: `errors.Is(runCtx.Err(), context.Canceled)` might match `context.DeadlineExceeded` if the deadline exceeded wraps canceled? No, `context.WithTimeout` sets a timer. If it fires, it returns `context.DeadlineExceeded`. If parent cancels, it returns `context.Canceled`. They are distinct. The code handles both correctly.

Wait, in `docker_test.go`:
```diff
+			name:    "deadline exceeded",
+			setup:   func(*T) context.Context { return context.Background() },
+			timeout: 150 * time.Millisecond,
```
The typo `*T` will cause a compile error. I'll report it as HIGH|internal/sandbox/docker_test.go:248|Typo in function parameter type causes compilation failure|Change *T to *testing.T|correctness|1|setup: func(*T) context.Context { return context.Background() },

Are there any other issues?
Let's check `cmd/atcr/autofix.go` around the switch:
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
```
What if `be.noSandbox` is true but `be.sandboxBackend` is also not nil? The first case catches it. This is fine.
What if `be.sandboxBackend` is nil and `be.noSandbox` is false? Default case catches it. Fine.

Check `cmd/atcr/autofix_integration_test.go`:
```go
		be := autoFixBackend{
			applyTarget:     applyDir,
			validateArgv:    []string{"true"}, // validation passes
			validateTimeout: 5 * time.Second,
			owner:           "o", repo: "r", token: "tok",
			apiURL:    srv.URL,
			noSandbox: true, // explicit --no-sandbox opt-out (host path)
		}
```
All good.

Check `cmd/atcr/autofix_test.go`:
```go
+// TestRunAutoFix_NilSandboxWithoutOptOutRefuses: a backend with NO sandbox and NO
...
+			noSandbox:      false, // and no explicit opt-out
```
All good.

One thing: In `cmd/atcr/autofix.go`, the comment says:
```go
 	// (4) Sandbox backend — the validation step's container isolation, resolved and
 	// preflighted here so an unconfigured/unreachable sandbox refuses at the gate
 	// alongside the other pieces rather than surfacing mid-run. Under the default
 	// sandboxed-on posture `enabled` is true, so a nil/failing sandbox is a hard
 	// refusal that joins the same `missing` slice (append-or-assign, no early
 	// return) — mirroring resolveExec's ResolveExecBackend call but inverting the
 	// polarity (verify.go:54). Preflight shells out to the local `docker` binary
```
This is just a comment.

Let's verify the `*T` typo line number precisely from the diff.
The diff shows:
```diff
+			name:    "deadline exceeded",
+			setup:   func(*T) context.Context { return context.Background() },
+			timeout: 150 * time.Millisecond,
```
It's inside `TestDockerBackend_Run_CancellationClassIsTimedOut`. The line number in the new file would be around 248. I'll cite the exact snippet.

Is there any other bug?
In `internal/sandbox/docker.go`:
```go
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
		res.TimedOut = true
		res.ExitCode = timeoutExitCode
		// ...
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if killErr := exec.CommandContext(killCtx, b.cfg.DockerPath, "kill", containerName).Run(); killErr != nil {
			logger.Warn(...)
		}
		killCancel()
```
If `b.cfg.DockerPath` is empty, it runs an empty command. This might be a minor issue but not a crash. I'll skip it unless it's critical. The prompt says "CRITICAL: will crash or corrupt on a common path". Empty docker path is unlikely in production since Preflight checks it.

What about `cmd/atcr/autofix.go` switch statement logic inversion?
Original:
```go
	if be.sandboxBackend != nil {
		res, verr = verify.RunSandboxedValidation(...)
	} else {
		res, verr = verify.RunConfiguredValidation(...)
	}
```
New:
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(...)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(...)
	default:
		// fail closed
	}
```
This correctly implements the epic's requirement. No logic inversion.

Check `cmd/atcr/autofix_test.go` for `TestRunAutoFix_NilSandboxUsesHostPath`:
```go
 		sandboxBackend: nil,  // no sandbox resolved
			noSandbox:      true, // explicit --no-sandbox opt-out authorizes the host path
```
Correct.

Check `cmd/atcr/autofix_integration_test.go` for `TestAutoFixIntegration_HappyPathOpensPR`:
```go
 		apiURL:    srv.URL,
			noSandbox: true, // explicit --no-sandbox opt-out (host path)
```
Correct.

I think the only bug is the typo `*T` in `docker_test.go`.
Wait, let's look at `cmd/atcr/autofix.go` line ~380 again.
```go
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
```
Is `be.validateArgv` guaranteed to be non-nil? If the gate passes, yes. But what if a test constructs `autoFixBackend{}` directly without setting `validateArgv`? The tests all set it. So it's fine.

One more check: `errors.Is(runCtx.Err(), context.Canceled)` in `docker.go`.
`runCtx` is derived from `ctx`. If `ctx` is canceled, `runCtx.Err()` returns `context.Canceled`. This is correct.
But wait, `context.WithTimeout` creates a context that expires when the timer fires OR when its parent is canceled. So `runCtx.Err()` could be `context.DeadlineExceeded` or `context.Canceled`. The code handles both. Correct.

Is there any issue with `cmd/atcr/autofix.go`'s `switch` falling through to `default`? Yes, it returns an error and reverts the patch. This is intended.

Let's format the finding for the typo:
`HIGH|internal/sandbox/docker_test.go:248|Typo in function parameter type causes compilation failure|Change *T to *testing.T|correctness|1|setup: func(*T) context.Context { return context.Background() },`

Wait, the prompt says "replace literal | in any field with /". I'll do that.
Also, check line numbers from the diff. The diff shows `@@ -240,3 +240,63 @@`. So the new code starts at line 244 in `docker_test.go`. The typo is a few lines down. I'll estimate line ~248 or just cite the snippet.

Let's verify the exact line in the payload:
```diff
+			name:    "deadline exceeded",
+			setup:   func(*T) context.Context { return context.Background() },
+			timeout: 150 * time.Millisecond,
```
It's inside `TestDockerBackend_Run_CancellationClassIsTimedOut`. I'll cite the snippet.

Are there any other issues?
In `cmd/atcr/autofix.go`:
```go
	default:
		// Fail closed: a nil sandbox backend WITHOUT the --no-sandbox opt-out must
		// never silently run unsandboxed on the host. ...
		if rerr := autofix.RevertPatch(ctx, bm); rerr != nil {
			return fmt.Errorf("auto-fix: refusing unsandboxed host validation without --no-sandbox AND revert failed: %w", rerr)
		}
		return fmt.Errorf("auto-fix: refusing to run validation unsandboxed on the host without an explicit --no-sandbox opt-out (no sandbox backend was resolved); working tree reverted, no GitHub changes made")
```
This is correct.

What about `validateAutoFixBackend`:
```go
	noSandbox, _ := cmd.Flags().GetBool("no-sandbox")
	if noSandbox {
		be.noSandbox = true
		warnNoSandbox(cmd.ErrOrStderr())
	} else if len(missing) == 0 {
```
If `--no-sandbox` is false, it checks `len(missing) == 0`. This is correct.

One thing: In `cmd/atcr/autofix.go`, the comment says:
```go
 	// (4) Sandbox backend — the validation step's container isolation, resolved and
 	// preflighted here so an unconfigured/unreachable sandbox refuses at the gate
 	// alongside the other pieces rather than surfacing mid-run. Under the default
 	// sandboxed-on posture `enabled` is true, so a nil/failing sandbox is a hard
 	// refusal that joins the same `missing` slice