# Acceptance Criteria: Command-Mode Shell-Wrap Injection

**Related User Story:** [03: Ephemeral-Copy Setup Injection](../user-stories/03-ephemeral-copy-setup-injection.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go argv construction (`dockerRunArgs` Command-mode branch) | `internal/sandbox/docker.go:145-147` |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Matches existing `internal/sandbox` test style (`TestDockerRunArgs_HardeningFlagsPresent`) |
| Key Dependencies | none (stdlib only) | `dockerRunArgs` remains pure/I-O-free so argv can be asserted without a daemon |

## Related Files
- `internal/sandbox/docker.go` - modify: replace the unconditional `else { args = append(args, cfg.Image); args = append(args, spec.Command...) }` branch (`docker.go:145-147`) with a conditional on `spec.Writable`: when true, append `cfg.Image, "/bin/sh", "-c", "cp -a /src/. /work/ && cd /work && exec \"$@\"", "--"` followed by `spec.Command...`; when false, keep today's unwrapped `cfg.Image` + `spec.Command...` form byte-for-byte
- `internal/sandbox/sandbox_test.go` - modify: add `TestDockerRunArgs_CommandModeWritableWrapsInShell` (or equivalent) asserting the wrapped argv shape for `Writable:true` and the unchanged shape for `Writable:false`, alongside the existing `TestDockerRunArgs_HardeningFlagsPresent` which pins the `Writable:false` default
- `internal/sandbox/sandbox.go` - reference only (not modified by this AC): `RunSpec.Writable` (added by Story 1) is the gating field this branch reads; `validate()` is untouched

### Related Files (from codebase-discovery.json)
- `internal/sandbox/docker.go:145-147` (`dockerRunArgs` Command-mode `else` branch) — modify: append `cfg.Image, "/bin/sh", "-c", <fixed literal>, "--"` followed by `spec.Command...` when `spec.Writable` is true; keep today's unwrapped form when false
- `internal/sandbox/docker.go:153-158` (`renderCommand`) — reference-only: display-only evidence/log string (`res.Command`), never read by execution; must NOT receive injection logic (confirmed by two `/refine-epic --deep` passes and a prior Epic 11.0 code review)
- `internal/sandbox/docker.go:161` (`Run`) — reference-only for this AC: consumes the wrapped argv unchanged; its Script-mode stdin construction (`docker.go:205-210`) is AC 03-02's scope
- `internal/sandbox/sandbox.go:28-41` (`RunSpec`) — reference-only: `Writable` gating field added by Story 1; `validate()` (`sandbox.go:43`) untouched
- `internal/sandbox/sandbox_test.go` — extend: new `TestDockerRunArgs_<Scenario>` wrap-shape tests alongside `TestDockerRunArgs_HardeningFlagsPresent` (`sandbox_test.go:35`), which stays unmodified as the `Writable:false` regression anchor
- `internal/verify/sandboxvalidate_test.go:71` — reference-only: pins that `--auto-fix` builds Command-mode-only `RunSpec`s ("Script must never be populated on the argv path"), i.e. this AC's wrap is the exact path that unlocks `--auto-fix`

Reference documentation: [Docker tmpfs and read-only mounts](../documentation/docker-tmpfs-and-read-only-mounts.md), [Current sandbox guarantees](../documentation/current-sandbox-guarantees.md).

## Happy Path Scenarios
**Scenario 1: Writable:true wraps Command mode in a shell that copies then execs**
- **Given** a `RunSpec{Command: []string{"npm", "test"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs` builds the argv
- **Then** the argv, after the image, contains `/bin/sh -c "cp -a /src/. /work/ && cd /work && exec \"$@\"" --` followed by `npm test` as two separate trailing argv elements (`npm`, `test`), not a single joined string

**Scenario 2: Writable:false (or zero value) leaves Command mode unwrapped**
- **Given** a `RunSpec{Command: []string{"go", "test", "./..."}, SnapshotDir: "/tmp/snap"}` (Writable omitted, zero value `false`)
- **When** `dockerRunArgs` builds the argv
- **Then** the argv after the image is exactly `spec.Command...` verbatim — no `/bin/sh`, `-c`, or `cp` tokens appear anywhere in argv, matching today's behavior and the existing `TestDockerRunArgs_HardeningFlagsPresent` assertion `assert.Contains(t, joined, "go test ./...")`

## Edge Cases
**Edge Case 1: Empty Command with Writable:true**
- **Given** `spec.validate()` already rejects a `RunSpec` with neither `Command` nor `Script` set (`sandbox.go:43-51`)
- **When** `dockerRunArgs` is called with `Writable: true` and no `Command`/`Script`
- **Then** `validate()` returns its existing error before the wrap branch is ever reached — this AC does not add or change any validation error

**Edge Case 2: Command containing an already-quoted or multi-word token**
- **Given** `spec.Command = []string{"sh", "-c", "echo hi"}` (a command whose own tokens happen to look like shell syntax) with `Writable: true`
- **When** `dockerRunArgs` builds the argv
- **Then** each element of `spec.Command` is appended as a distinct argv entry after `--`, exactly as received, with no re-quoting, re-splitting, or concatenation performed by the wrap logic

**Edge Case 3: Preflight's trivial-run RunSpec is unaffected**
- **Given** `Preflight` (`docker.go:308`) builds `RunSpec{Command: []string{"true"}, SnapshotDir: tmpDir}` with `Writable` left at its zero value
- **When** `dockerRunArgs` processes it
- **Then** the trivial-run argv stays unwrapped (`Writable: false` path), so `Preflight`'s existing behavior and test coverage require no changes

**Edge Case 4: Validation image lacks `/bin/sh` or a `cp` supporting `-a` (distroless/scratch)**
- **Given** an operator validation image that ships no POSIX shell or no `cp -a` (e.g. distroless, scratch) with `Writable: true` — the wrap newly requires both, where today's unwrapped Command mode requires neither
- **When** the wrapped argv executes in the container
- **Then** the run fails at container start/exec and surfaces through the existing backend-fault classification — this AC adds no new error path or detection logic; the constraint is a documented, diagnosable limitation (carried in the `Writable` field doc comment from Story 1 and `docs/auto-fix.md` per T6), matching `codebase-discovery.json`'s `integration_gaps` entry. This AC's argv-level tests do not gate on image capabilities; they pin the wrap shape to exactly the fixed literal so the failure mode above remains the *only* way a shell-less image can fail

## Error Conditions
**Error Scenario 1: N/A — no new error paths**
- This AC is pure argv construction gated by an existing bool field; it introduces no new error returns. `dockerRunArgs` continues to return only the pre-existing `spec.validate()` errors.
- Error message: no new error message is introduced by this AC.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

## Performance Requirements
- **Response Time:** No measurable change — argv construction remains a fixed-size slice append with a handful of extra elements only on the `Writable:true` path; no loops, I/O, or allocation growth beyond the existing `args` slice.
- **Throughput:** No change to `Writable:false` runs (identical code path); `Writable:true` runs add a `cp -a` of the snapshot tree at container start, whose cost is out of scope for this AC (mount/copy performance is Story 2's/epic's concern, not this argv-construction story).

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go package, no external auth surface.
- **Input Validation:** The wrap's `-c` script text (`"cp -a /src/. /work/ && cd /work && exec \"$@\""`) is a fixed Go string literal containing no data from `spec.Command`; `spec.Command`'s elements are appended as separate `[]string` argv entries after `--`, never concatenated into the `-c` string. This is the load-bearing property this AC protects — see AC 03-03 for the dedicated adversarial injection-safety test — but this AC's own scenarios must also confirm the wrap shape holds for ordinary (non-adversarial) commands like `npm test`.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** In-memory `RunSpec` literals with `Command`, `SnapshotDir`, and `Writable` set as needed; no fixtures or filesystem needed (mirrors `TestDockerRunArgs_HardeningFlagsPresent`'s pattern of asserting on `strings.Join(args, " ")` and on individual `args` slice elements where ordering/separation matters).
**Mock/Stub Requirements:** None — `dockerRunArgs` is documented pure/I-O-free specifically so hardening and wrap-shape flags can be asserted without a daemon.
**Example Assertions:** for `Command: ["npm", "test"]` + `Writable:true`, assert the six trailing argv elements are exactly `"/bin/sh", "-c", "cp -a /src/. /work/ && cd /work && exec \"$@\"", "--", "npm", "test"` via `require.Equal` on a sub-slice of `args` — exact-element equality, not substring matching on the joined string; the `-c` element in particular is asserted byte-equal to the fixed literal. Substring checks on `strings.Join(args, " ")` are reserved for the `Writable:false` absence case (e.g. `assert.NotContains(t, joined, "/bin/sh")`). All assertions are argv-level and daemon-free — never inferred from a passing end-to-end container run.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `dockerRunArgs`'s Command-mode branch wraps argv in `/bin/sh -c "cp -a /src/. /work/ && cd /work && exec \"$@\"" --` followed by `spec.Command...` when `spec.Writable` is `true`
- [ ] `dockerRunArgs`'s Command-mode branch is byte-for-byte unchanged from current behavior when `spec.Writable` is `false` (zero value), verified against `TestDockerRunArgs_HardeningFlagsPresent` passing unmodified
- [ ] `spec.Command`'s tokens are appended as separate argv elements after `--`, never joined into the `-c` script string
- [ ] `Preflight`'s trivial-run `RunSpec` (Writable unset) continues to produce unwrapped argv
- [ ] The `-c` argv element is asserted byte-equal to the fixed literal `cp -a /src/. /work/ && cd /work && exec "$@"` (exact-element equality, not merely substring containment)
- [ ] `renderCommand` (`docker.go:153-158`) remains display-only — no injection logic is added there; it still returns only the human-readable evidence string
- [ ] The implicit image requirement (`/bin/sh` + `cp -a`; false for distroless/scratch) is acknowledged as a diagnosable constraint consistent with the `Writable` field doc comment (Story 1) — no runtime detection added by this AC

**Manual Review:**
- [ ] Code reviewed and approved
