<think>
Let me analyze this diff systematically, focusing on the Epic 11.0 scope (sandbox backend, --exec opt-in, evidence_exec block, repro write-back, report badge).

Let me trace through the changes carefully:

1. **`cmd/atcr/review.go`** - Added `--exec` flag, preflight check before review, threaded backend through to verify stage
2. **`cmd/atcr/verify.go`** - Added `--exec` flag, `resolveExec` function, threaded backend through
3. **`internal/sandbox/sandbox.go`** - Backend interface, RunSpec validation, truncate helper
4. **`internal/sandbox/docker.go`** - DockerBackend implementation
5. **`internal/tools/exec_tools.go`** - run_tests/run_script tools
6. **`internal/verify/exec.go`** - ResolveExecBackend
7. **`internal/repro/repro.go`** - Verdict logic, Reproduce, Stamp
8. **`internal/reconcile/emit.go`** - EvidenceExec struct
9. **`internal/report/render.go`** - Reproduced badge
10. **`internal/registry/sandbox.go`** - SandboxConfig validation

Let me look for issues within scope:

### Issue 1: `renderCommand` doesn't reflect the actual command
In `internal/sandbox/docker.go`:
```go
func renderCommand(spec RunSpec) string {
    if spec.Script != "" {
        return "/bin/sh -s <<'EOF'\n" + spec.Script + "\nEOF"
    }
    return strings.Join(spec.Command, " ")
}
```
For Script mode, this is `/bin/sh -s <<'EOF'\n...` but the actual docker command being run uses `-i` flag and pipes the script via stdin. The rendered command doesn't include the docker prefix. But this is the human-readable "what was executed" - it represents what's executed inside the container, so this is probably fine.

### Issue 2: Semaphore `b.sem` could be nil but not nil-checked
Looking at `Run`:
```go
if b.sem != nil {
    select {
    case b.sem <- struct{}{}:
        defer func() { <-b.sem }()
    ...
```
The semaphore is always created in `NewDockerBackend`, so it's never nil. But the nil check is defensive. Not really an issue.

### Issue 3: `--exec` without `--verify` returns early, but `resolveExec` is called before `verifyMinSeverity`
Looking at `cmd/atcr/review.go`:
```go
if execFlag, _ := cmd.Flags().GetBool("exec"); execFlag && !verifyFlag {
    return usageError(errors.New("--exec requires --verify (execution reproduction runs in the adversarial verify stage)"))
}
verifyMinSev := ""
var execBackend sandbox.Backend
var execTestCmd []string
var execTimeout time.Duration
if verifyFlag {
    if verifyMinSev, err = verifyMinSeverity(cmd); err != nil {
        return err
    }
    if execBackend, execTestCmd, execTimeout, err = resolveExec(cmd); err != nil {
        return err // refuse-without-backend / preflight failure (exit 2)
    }
}
```
The `--exec` flag is registered but `--verify` flag value is checked with `verifyFlag, _ := cmd.Flags().GetBool("verify")` — but `verifyFlag` is read before the exec check. Actually looking at the code, `verifyFlag` was already declared above. So this is fine.

But wait — the `cmd/atcr/review.go` file already had `verifyFlag` declared earlier (line 130). Let me re-check... yes, it's declared before. So that's fine.

### Issue 4: `resolveExec` in `cmd/atcr/review.go` ignores `cmd.Context()` for preflight
Actually no, it uses `cmd.Context()`:
```go
backend, testCmd, timeout, err := verify.ResolveExecBackend(cmd.Context(), true, proj.Sandbox)
```
This is fine.

### Issue 5: ProjectConfig loading in resolveExec may happen twice
In `cmd/atcr/review.go`, `resolveExec` calls `registry.LoadProjectConfig(registry.DefaultProjectConfigPath("."))`, but the project config may have already been loaded earlier in `runReview`. This would cause duplicate file I/O. But this is a minor performance issue.

### Issue 6: Docker --user with UID:GID format
The Docker `--user` flag with UID:GID format like `65534:65534` — is this guaranteed to exist in the container? The container image might not have a `nobody:nogroup` user, but using a UID bypasses user lookup and works regardless. This is fine.

### Issue 7: `MaxConcurrent` semaphore blocking in Run
Looking at this:
```go
if b.sem != nil {
    select {
    case b.sem <- struct{}{}:
        defer func() { <-b.sem }()
    case <-ctx.Done():
        return RunResult{}, ctx.Err()
    }
}
```
This blocks when the semaphore is full. If ctx is cancelled, returns. Otherwise waits for slot. This is fine.

### Issue 8: Error message visibility in `renderCommand` for script
Actually, looking again at `renderCommand`:
```go
if spec.Script != "" {
    return "/bin/sh -s <<'EOF'\n" + spec.Script + "\nEOF"
}
```
The full script is included in the rendered command. This is fine but could include user-controlled content (script body). The downstream `writeReproducedBlock` calls `esc(e.Command)` which will escape HTML. Good.

### Issue 9: Resource caps not validated
In `sandbox.go`:
```go
func (s *SandboxConfig) Validate() error {
    if s == nil {
        return nil
    }
    if b := strings.TrimSpace(s.Backend); b != "" && b != SandboxBackendDocker {
        return fmt.Errorf("sandbox.backend %q is unsupported (only %q)", s.Backend, SandboxBackendDocker)
    }
    if strings.TrimSpace(s.Image) == "" {
        return errors.New("sandbox.image is required when a sandbox block is present (a base image carrying the toolchain your test_command needs)")
    }
    if len(s.TestCommand) == 0 {
        return errors.New("sandbox.test_command is required when a sandbox block is present")
    }
    ...
}
```
`Memory`, `CPUs` are not validated. A user could put `--memory 999999999t` or other invalid values. These would only fail at docker execution time. This is a minor issue.

### Issue 10: `MaxOutputBytes` check after construction
In `NewDockerBackend`:
```go
if cfg.MaxOutputBytes <= 0 {
    cfg.MaxOutputBytes = 64 * 1024
}
```
This is fine.

### Issue 11: Output truncation - `truncate` doesn't return a Go error, but `Run` is a Go function
Just noting that the API is consistent.

### Issue 12: `runScriptHandler` accepts content but doesn't validate against max length
A malicious model could pass an extremely large `content` string. While the script is fed over stdin (no length cap), the memory consumption could be problematic. But this is somewhat outside the strict scope - the content is fed to docker directly.

### Issue 13: Potential issue with `cmd.Stdin` setup for script
Looking at:
```go
if spec.Script != "" {
    args = append(args, "-i", cfg.Image, "/bin/sh", "-s")
} else {
    args = append(args, cfg.Image)
    args = append(args, spec.Command...)
}
...
cmd := exec.CommandContext(runCtx, b.cfg.DockerPath, args...)
if spec.Script != "" {
    cmd.Stdin = strings.NewReader(spec.Script)
}
```
For Script, `-i` is added to docker args, and `cmd.Stdin` is set. But for Command, stdin is NOT set. This means the container will have an empty stdin (Docker default is interactive TTY for run command without `-i`). Actually for `docker run` without `-i`, stdin is connected to /dev/null by default. This might cause issues if the command tries to read stdin, but it's the expected behavior.

Actually wait — for the script case, `-i` is added to the docker args. But this also enables interactive mode (keeping STDIN open). For non-script (command) case, `-i` is NOT added. This is intentional and correct.

### Issue 14: `truncate` function may produce invalid UTF-8
```go
func truncate(s string, limit int) string {
    if limit <= 0 || len(s) <= limit {
        return s
    }
    marker := fmt.Sprintf("\n[...truncated %d bytes...]", len(s)-limit)
    keep := limit
    for keep > 0 && (s[keep]&0xC0) == 0x80 { // back up off a UTF-8 continuation byte
        keep--
    }
    return s[:keep] + marker
}
```
This trims back to a UTF-8 boundary. The marker is ASCII. Good. But `len(s)-limit` in the marker message is incorrect when keep has been decremented (we trimmed fewer bytes than limit actually). Minor issue.

### Issue 15: `Reproduce` doesn't truncate `OutputExcerpt`
Looking at:
```go
ev = &reconcile.EvidenceExec{
    Command:       r2.Command,
    ExitCode:      r2.ExitCode,
    OutputExcerpt: r2.Output,
}
```
The Output is already truncated by the backend (`truncate(buf.String(), b.cfg.MaxOutputBytes)`), so this is fine.

### Issue 16: `Verdict` rule - exit codes disagree
Looking at:
```go
if r1.ExitCode != r2.ExitCode {
    return reclib.VerdictUnverifiable
}
if r1.ExitCode != 0 {
    return reclib.VerdictConfirmed
}
return reclib.VerdictUnverifiable
```
The "both zero" case returns unverifiable. Comment says "weak repro must not bury the finding". But this means a finding that was previously confirmed by a different skeptic might get downgraded? No, `Stamp` only overwrites if verdict rank is higher. Unverifiable is rank 2, confirmed is rank 3. So a prior confirmed (rank 3) is NOT downgraded to unverifiable (rank 2). Good.

But wait, if a finding had no prior verdict (rank 0) and we run a repro that passes (exit 0), we set it to unverifiable with skeptic="repro". The comment says "a weak repro must not bury the finding". But setting unverifiable on a finding that previously had no verdict does affect the gate. Hmm, but unverifiable is rank 2. It would be a new verdict (no prior), so it's set. This is the expected behavior.

### Issue 17: `writeReproducedBlock` is placed between `writePathWarning` and `writeSkepticBlock` — but the doc comment says "labelled 'Judge' so it is not mistaken for a skeptic-produced verdict"
Looking at:
```go
// writeReproducedBlock renders the execution-reproduction evidence (Epic 11.0)
...
func writeReproducedBlock(b *bytes.Buffer, e *reconcile.EvidenceExec) {
    fmt.Fprintf(b, "  - ✅ Reproduced: `%s` (exit %d)\n", esc(e.Command), e.ExitCode)
    ...
}

func writeSkepticBlock(b *bytes.Buffer, v *reclib.Verification) {
```
There's a stray comment block between these functions - the "labelled 'Judge'..." comment seems to belong to `writeSkepticBlock` but is placed before `writeReproducedBlock`. This is a minor formatting/organization issue.

Actually looking again:
```go
// writePathWarning(b *bytes.Buffer, f reconcile.JSONFinding) {
// labelled "Judge" so it is not mistaken for a skeptic-produced verdict.
// All free text is HTML-escaped and newline-flattened so reviewer-controlled
// fields cannot inject markup or escape the section.
// writeReproducedBlock renders the execution-reproduction evidence (Epic 11.0)
```
Wait, this seems weird. The diff context shows:
```
// writePathWarning(b *bytes.Buffer, f reconcile.JSONFinding) {
 // labelled "Judge" so it is not mistaken for a skeptic-produced verdict.
 // All free text is HTML-escaped and newline-flattened so reviewer-controlled
 // fields cannot inject markup or escape the section.
+// writeReproducedBlock renders the execution-reproduction evidence (Epic 11.0)
```

So it looks like a stray dangling comment that was there before. The diff just adds a new function comment without removing the old dangling comment. This is a minor issue (likely a pre-existing issue from before the diff). Let me not flag this.

### Issue 18: `cmd.Stdin = strings.NewReader(spec.Script)` doesn't check Script size
For an extremely large script, the NewReader will hold it in memory. But this is bounded by what the model sends.

### Issue 19: Potential race condition with `sem` channel
The semaphore `sem chan struct{}` is created once and shared across all Run calls. The select/case structure is goroutine-safe. No issue.

### Issue 20: Concurrent map access?
In `EnableExecution`:
```go
func (d *Dispatcher) EnableExecution(backend sandbox.Backend, testCmd []string, timeout time.Duration) {
    d.execBackend = backend
    d.execTestCmd = append([]string(nil), testCmd...)
    d.execTimeout = timeout
    d.mustRegister("run_tests", runTestsHandler, pathSpec{})
    d.mustRegister("run_script", runScriptHandler, pathSpec{})
}
```
This modifies the dispatcher state. If `EnableExecution` is called concurrently with another tool lookup, there could be a race. But typically dispatchers are configured once before use. The comment says "It is called once, during construction".

Looking at `mustRegister` — it accesses `d.handlers` map. If this is called concurrently with Execute, there's a race. But this is a documentation/usage issue, not a code defect.

### Issue 21: The Exec agent flag is per-agent, but the verify pipeline uses `opts.ExecBackend != nil` to determine exec for all skeptics
Looking at `verifyFinding`:
```go
ver, vr := verifyFinding(ctx, jb.finding, jb.skeptics, cc, disp, opts.ExecBackend != nil)
```
And `buildSkepticAgent`:
```go
agent := buildSkepticAgent(skeptic, prompt, exec)
```

This means ALL skeptics in the run get the exec flag set, even non-execution-enabled agents. This might be a design choice — at verify stage, all skeptics become "exec" if `--exec` is on. The sprint plan says:
> New tools (execution-enabled agents only): run_tests(target?) ...

But looking at the fanout engine:
```go
toolDefs:   wireToolDefs(a.Exec),
```
The exec flag is set per agent. So all skeptics get exec tools when `--exec` is on. This is by design per the implementation.

Hmm, but the sprint plan says "execution-enabled agents only". The skeptic is an agent role, and in --exec mode, all skeptics are essentially "execution-enabled". This seems consistent.

### Issue 22: `verify.go` doesn't check that `--exec` is paired with a backend the same way as review.go
Wait, actually `resolveExec` is the function. It checks both. Both `verify.go` and `review.go` call it. So they're consistent.

### Issue 23: `cmd/atcr/review.go` doesn't have the `--exec` flag registered
Wait, let me check... actually I see:
```go
cmd.Flags().Bool("exec", false, "with --verify: let skeptics reproduce findings in a sandbox (requires a [sandbox] block in .atcr/config.yaml that passes preflight); refuses otherwise")
