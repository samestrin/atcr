# Acceptance Criteria: Script-Mode Stdin-Prepend Injection

**Related User Story:** [03: Ephemeral-Copy Setup Injection](../user-stories/03-ephemeral-copy-setup-injection.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go stdin construction (`DockerBackend.Run`'s Script-mode `cmd.Stdin`) | `internal/sandbox/docker.go:205-210`, `cmd.Stdin = strings.NewReader(spec.Script)` |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Matches existing `internal/sandbox` test style; reuses `writeFakeDocker` shim pattern where an end-to-end `Run` assertion is needed |
| Key Dependencies | `strings.NewReader` (stdlib) | No new dependency; only the reader's source string changes |

## Related Files
- `internal/sandbox/docker.go` - modify: in `Run`, when `spec.Script != "" && spec.Writable`, build `cmd.Stdin` from `"cp -a /src/. /work/ && cd /work\n" + spec.Script` instead of `spec.Script` alone; when `spec.Writable` is `false`, `cmd.Stdin` stays `strings.NewReader(spec.Script)` exactly as today
- `internal/sandbox/sandbox_test.go` - modify: add a test asserting the copy-step line precedes the script body in stdin content for `Writable:true`, and extend/verify `TestDockerRunArgs_ScriptUsesStdinShell` (line 83) continues to pass unmodified for the `Writable:false` (zero-value) case — this pins that argv stays `-i <image> /bin/sh -s` regardless of `Writable`, since the setup step travels over stdin, never argv

### Related Files (from codebase-discovery.json)
- `internal/sandbox/docker.go:161,205-210` (`DockerBackend.Run` — Script-mode `cmd.Stdin` construction) — modify: build the reader from `"cp -a /src/. /work/ && cd /work\n" + spec.Script` when `spec.Script != "" && spec.Writable`
- `internal/sandbox/docker.go:142-144` (`dockerRunArgs` Script-mode argv branch) — reference-only (pinned unchanged): argv stays `-i <image> /bin/sh -s` for both `Writable` values; the setup step never enters argv
- `internal/sandbox/docker.go:153-158` (`renderCommand`) — reference-only: display-only evidence string; the prepend must NOT be added here (execution never reads it)
- `internal/sandbox/sandbox_test.go:83` (`TestDockerRunArgs_ScriptUsesStdinShell`) — reference-only: pattern anchor that must pass unmodified for the `Writable:false` zero-value case
- `internal/sandbox/sandbox_test.go:24` (`writeFakeDocker`) — reference/reuse: existing docker-CLI shim for the optional end-to-end `Run` stdin assertion
- `internal/sandbox/docker_test.go` — extend (optional): home for a fakeDocker-based end-to-end `Run` test whose shim `cat`s stdin to stdout so the prepended content can be asserted on captured output, per T5(b)'s pattern
- `internal/verify/sandboxvalidate_test.go:71` — reference-only: `--auto-fix` never populates `Script`; this mode's correctness protects `--exec`'s script path and future Script-mode callers, not the `--auto-fix` path

Reference documentation: [Docker tmpfs and read-only mounts](../documentation/docker-tmpfs-and-read-only-mounts.md), [Current sandbox guarantees](../documentation/current-sandbox-guarantees.md).

## Happy Path Scenarios
**Scenario 1: Writable:true prepends the copy step to stdin content**
- **Given** a `RunSpec{Script: "echo hi\nexit 3\n", SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `Run` constructs `cmd.Stdin`
- **Then** the stdin reader's content is `"cp -a /src/. /work/ && cd /work\necho hi\nexit 3\n"` — the setup line first, followed by `spec.Script` verbatim, with the copy step never appearing in `dockerRunArgs`'s returned argv

**Scenario 2: Writable:false (or zero value) leaves Script mode's stdin unchanged**
- **Given** a `RunSpec{Script: "echo hi\nexit 3\n", SnapshotDir: "/tmp/snap"}` (Writable omitted, zero value `false`)
- **When** `Run` constructs `cmd.Stdin`
- **Then** the stdin reader's content is `spec.Script` verbatim (`"echo hi\nexit 3\n"`), with no `cp`/`cd` prefix — identical to current behavior

## Edge Cases
**Edge Case 1: Argv shape is untouched by the Writable flag in Script mode**
- **Given** `RunSpec{Script: "...", SnapshotDir: "/tmp/snap", Writable: true}` vs the same spec with `Writable: false`
- **When** `dockerRunArgs` builds argv for each
- **Then** both produce identical argv (`-i <image> /bin/sh -s`) — `Writable` only affects `Run`'s stdin construction, never `dockerRunArgs`'s Script-mode argv branch (`docker.go:142-144`), preserving `TestDockerRunArgs_ScriptUsesStdinShell`'s pinned shape for both flag values

**Edge Case 2: Empty script body with Writable:true**
- **Given** `spec.validate()` already rejects a `RunSpec` with neither `Command` nor `Script` set
- **When** `Run` is called with `Writable: true` and an empty `Script`
- **Then** `dockerRunArgs` (called first inside `Run`) returns `validate()`'s existing error before stdin construction is reached — this AC does not add or change any validation error

**Edge Case 3: cd /work restores the same working directory the container already starts in**
- **Given** `dockerRunArgs` always sets `--workdir /work` regardless of mode (`docker.go:139`)
- **When** the prepended `cd /work` runs after the copy step in a `Writable:true` script
- **Then** the net effective working directory for the user's script body is unchanged from today (`/work`) — now populated and writable rather than empty/read-only, so no cwd-dependent script behavior regresses

**Edge Case 4: The prepend is exactly one `&&`-chained literal line, placed before any user-script directives**
- **Given** a `Writable:true` script whose body begins with its own directives (e.g. `set -euo pipefail`, a `cd` elsewhere, or comments)
- **When** `Run` builds the stdin content
- **Then** the prepended content is exactly the single literal line `cp -a /src/. /work/ && cd /work` plus one `\n`, inserted before the user body verbatim — this AC adds no `set -e`, no error handling, and no other lines around it (the snippet is pinned byte-for-byte by the epic's T3); if the copy step itself fails, the `&&` skips the `cd` and the failure surfaces through the script's own subsequent behavior against an unpopulated `/work`, which is the accepted, documented failure mode for an undersized `WorkSize` (plan.md Risk Mitigation), not something this AC re-engineers

## Error Conditions
**Error Scenario 1: N/A — no new error paths**
- This AC is pure stdin-content construction gated by an existing bool field; it introduces no new error returns. `Run` continues to return only pre-existing errors (`dockerRunArgs`'s `validate()` errors, backend faults from `cmd.Run()`).
- Error message: no new error message is introduced by this AC.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

## Performance Requirements
- **Response Time:** No measurable change on the `Writable:false` path (identical `strings.NewReader(spec.Script)` call). On `Writable:true`, one string concatenation (`"cp -a /src/. /work/ && cd /work\n" + spec.Script`) is added before the reader is constructed — negligible relative to script execution time.
- **Throughput:** Not affected by this AC; the `cp -a` copy's runtime cost is the mount/overlay mechanism's concern (Story 2/epic), not this stdin-construction story.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go package, no external auth surface.
- **Input Validation:** The prepended setup line is a fixed Go string literal (`"cp -a /src/. /work/ && cd /work\n"`) containing no data derived from `spec.Command` or any external input; it is concatenated only with `spec.Script`, which was already trusted to be delivered as raw script *content* over stdin (never argv) before this story, per `sandbox.go:32-34`'s doc comment ("Script is ... fed to `/bin/sh -s` over stdin ... never interpolated into argv"). This AC must not introduce any path where the setup line or `spec.Script` is passed through argv or a `-c` string — it is stdin data only, both before and after the prepend.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** In-memory `RunSpec` literals with `Script`, `SnapshotDir`, and `Writable` set as needed. Asserting stdin *content* (not argv) requires either exposing/refactoring stdin construction into a small testable helper, or driving `Run` end-to-end with `writeFakeDocker` (`sandbox_test.go:24`) and having the fake docker shim `cat` its stdin to stdout so the test can assert on captured output — follow whichever pattern keeps `dockerRunArgs` pure and argv-only per its documented contract.
**Mock/Stub Requirements:** `writeFakeDocker` fake-docker shell shim (existing pattern) if an end-to-end `Run` assertion is used instead of a unit-level stdin-builder helper; no real Docker daemon.
**Example Assertions:** for `Script: "echo hi\nexit 3\n"` + `Writable:true`, assert the stdin content equals `"cp -a /src/. /work/ && cd /work\necho hi\nexit 3\n"` byte-for-byte (`assert.Equal` — the literal setup line, one `\n`, then the script verbatim), and assert `strings.Join(args, " ")` does not contain `cp -a` (setup step never in argv); for `Writable:false`, assert the stdin content equals `spec.Script` verbatim. All assertions are stdin/argv-level and daemon-free — never inferred from a passing end-to-end container run.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `Run`'s Script-mode stdin content is `"cp -a /src/. /work/ && cd /work\n" + spec.Script` when `spec.Writable` is `true`
- [ ] `Run`'s Script-mode stdin content is `spec.Script` verbatim when `spec.Writable` is `false` (zero value), verified against `TestDockerRunArgs_ScriptUsesStdinShell` passing unmodified
- [ ] `dockerRunArgs`'s Script-mode argv (`-i <image> /bin/sh -s`) is identical regardless of `spec.Writable` — the setup step never appears in argv
- [ ] The setup step's `cd /work` follows the `cp -a` copy so the script body runs from the populated, writable `/work` directory
- [ ] The prepended setup line is a fixed Go string literal (`"cp -a /src/. /work/ && cd /work\n"`) containing no data derived from `spec.Script`, `spec.Command`, or any external input
- [ ] `renderCommand` (`docker.go:153-158`) remains display-only — the prepend is not added there; it still returns only the human-readable evidence string

**Manual Review:**
- [ ] Code reviewed and approved
