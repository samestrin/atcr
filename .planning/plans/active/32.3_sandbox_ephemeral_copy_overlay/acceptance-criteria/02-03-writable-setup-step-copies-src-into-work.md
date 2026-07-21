# Acceptance Criteria: Writable Setup Step Populates /work Before the Real Payload Runs

**Related User Story:** [02: Conditional Writable /work Mount](../user-stories/02-conditional-writable-work-mount.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function (setup-step injection in `dockerRunArgs`/`Run`, `internal/sandbox/docker.go`) | `cp -a /src/. /work/ && cd /work` prepended ahead of the caller's payload |
| Test Framework | `go test` + `testify` (`assert`/`require`); `writeFakeDocker` shim for `Run`-level assertions | Matches existing `internal/sandbox` test style (`sandbox_test.go:24`) |
| Key Dependencies | `/bin/sh -c` (Command-path wrapping), `/bin/sh -s` (existing Script-path stdin delivery) | Wrapping only applies when `spec.Writable` is `true` |

## Related Files
- `internal/sandbox/docker.go` - modify: `dockerRunArgs` (`Script != ""` branch, line 142) prepends the setup step as an additional shell line ahead of the script body; the `Command` (argv) branch (line 145) wraps in `/bin/sh -c` only when `spec.Writable` is `true`, passing the setup step and the original command as distinct, non-interpolated shell tokens (e.g. via `exec "$@"` after `--`) rather than string-concatenating `spec.Command` into shell source
- `internal/sandbox/docker.go` - modify: `renderCommand` (line 153-158) is explicitly NOT modified — it must keep rendering the caller's original command/script as evidence, not the internal `cp -a` setup step, so the evidence trail stays readable
- `internal/sandbox/sandbox_test.go` or `internal/sandbox/docker_test.go` - modify: new test(s) asserting the setup step appears in the constructed argv/stdin only when `Writable` is `true`, and that `renderCommand`'s output is unaffected

## Happy Path Scenarios
**Scenario 1: Command path gets the setup step via a wrapped shell invocation**
- **Given** `RunSpec{Command: []string{"npm", "run", "build"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** the argv wraps execution in `/bin/sh -c` with the `cp -a /src/. /work/ && cd /work` setup step and the original command passed as distinct, non-interpolated tokens (not string-concatenated), so a real run's `npm run build` writing into `dist/` succeeds instead of failing with `EROFS`

**Scenario 2: Script path gets the setup step prepended to the stdin body**
- **Given** `RunSpec{Script: "npm test", SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs`/`Run` prepare the script delivered over stdin to `/bin/sh -s`
- **Then** the effective script body executed is `cp -a /src/. /work/ && cd /work` followed by the caller's original script (`npm test`), so tool state written during the script (e.g. `cargo build` → `target/`) lands on the writable `/work` tmpfs

**Scenario 3: renderCommand evidence is unaffected by the setup step**
- **Given** `RunSpec{Command: []string{"npm", "run", "build"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `renderCommand(spec)` is called for evidence formatting
- **Then** its output is exactly `"npm run build"` (the caller's original command), with no `cp -a` setup-step text appended — evidence stays display-only and readable

## Edge Cases
**Edge Case 1: /src remains read-only for the container's entire lifetime**
- **Given** `Writable: true` and the setup step has run
- **When** any subsequent step in the container (the copy, or the caller's real command) attempts to write to `/src`
- **Then** the write fails with `EROFS`, because `/src` is mounted `:ro` (Scenario in AC 02-02) and the container's `--read-only` rootfs flag is unchanged — no host file under `SnapshotDir` is ever mutated, since `/src` is the only path backed by the host bind mount

**Edge Case 2: Writable:false path never invokes the /bin/sh -c wrapping**
- **Given** `RunSpec{Command: [...], SnapshotDir: "/tmp/snap"}` with `Writable` left at its zero value (`false`)
- **When** `dockerRunArgs` builds the argv
- **Then** the `Command` path is unwrapped — `cfg.Image` followed by `spec.Command...` verbatim, exactly as before this story — so the general (non-opt-in) case never gains a shell-interpolation surface

**Edge Case 3: Setup step failure surfaces as a normal non-zero exit, not a silent no-op**
- **Given** the `cp -a /src/. /work/` step fails inside the container (e.g. disk pressure on the `/work` tmpfs, though `size=<cfg.WorkSize>` bounds this)
- **When** the shell wrapper runs `cp -a ... && cd /work` before `exec`-ing the caller's real command
- **Then** the `&&` chain short-circuits — the caller's command never executes — and the container exits non-zero, which `Run`'s existing exit-code handling (`docker.go:250-273`) reports as the run's `ExitCode`, not silently swallowed

## Error Conditions
**Error Scenario 1: cp -a failure propagates as a non-zero exit code**
- Error message: whatever `cp` emits on stderr (e.g. `cp: cannot copy ... No space left on device`), captured in `RunResult.Output` via the existing combined stdout/stderr capture (`docker.go:216-217`)
- HTTP status / error code: not applicable — surfaces as `RunResult.ExitCode` (a non-zero program exit, not a Go `error`, per the `Backend.Run` contract at `sandbox.go:91-93`)

**Error Scenario 2: Shell-wrapping must not reintroduce command injection**
- Error message: N/A — this is a security property, not a runtime error path
- HTTP status / error code: not applicable — verified structurally: `spec.Command` elements must be passed as distinct argv/`"$@"` tokens to the wrapping shell, never concatenated into the `-c` script string, so a `spec.Command` element containing shell metacharacters (e.g. `; rm -rf /`) cannot escape its token boundary

## Performance Requirements
- **Response Time:** The `cp -a /src/. /work/` step adds I/O proportional to `SnapshotDir`'s size before the real payload starts; this is inherent to the ephemeral-copy strategy (documented plan tradeoff) and not separately budgeted by this AC beyond staying within the run's existing `Timeout`/`b.cfg.Timeout` wall-clock budget (`docker.go:194-197`).
- **Throughput:** No change to concurrent-run capacity — the setup step runs inside the existing per-run container and does not affect `MaxConcurrent` semaphore behavior (`docker.go:179-193`).

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go package, no external auth surface.
- **Input Validation:** The `/bin/sh -c` wrapping introduced for the `Writable:true` Command path is a narrower, opt-in surface than the general case (per the story's documented risk mitigation) — the setup step string (`cp -a /src/. /work/ && cd /work`) is a fixed, non-caller-controlled literal, and `spec.Command` tokens are passed distinctly (e.g. after `--`, consumed via `exec "$@"`) so no caller-controlled text is ever concatenated into the shell source string.
- **No host mutation:** `/src` stays `:ro` for the container's full lifetime (Edge Case 1); the only writable surface is the `/work` tmpfs, which is memory-backed and destroyed with the container (`--rm`), so no run — successful or failed — can leave a mutation on the host filesystem under `SnapshotDir`.

## Test Implementation Guidance
**Test Type:** UNIT (argv/stdin construction) with an optional INTEGRATION-flavored case using `writeFakeDocker` (`sandbox_test.go:24`) to assert `Run` actually invokes the wrapped shell form end-to-end without a real daemon.
**Test Data Requirements:** `RunSpec{Command: [...], SnapshotDir: "/tmp/snap", Writable: true}` and a matching `Script`-based case; a `Writable:false` control case to confirm no wrapping occurs (shared fixture with AC 02-01).
**Mock/Stub Requirements:** `writeFakeDocker` shim (existing helper) for any `Run`-level assertion; `dockerRunArgs`-only assertions need no shim since the function is pure.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `Writable:true` Command path wraps in `/bin/sh -c`, injecting `cp -a /src/. /work/ && cd /work` ahead of the caller's command, with command tokens passed distinctly (no string concatenation)
- [ ] `Writable:true` Script path prepends the setup step as an additional shell line ahead of the script body delivered over stdin
- [ ] `renderCommand` output is unchanged — it renders only the caller's original command/script, never the internal setup step
- [ ] `Writable:false` Command path is never wrapped in `/bin/sh -c` (unchanged from pre-story behavior)

**Manual Review:**
- [ ] Code reviewed and approved
