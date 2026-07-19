# Acceptance Criteria: Inverted Default Posture (Sandbox-On-By-Default) and the SandboxConfig.Validate() Tension

**Related User Story:** [02: Sandbox Resolution and Preflight Gate for Auto-Fix](../user-stories/02-sandbox-resolution-and-preflight-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go resolver function `verify.ResolveAutoFixSandbox`, `enabled bool` parameter + sentinel error | `internal/verify/autofix_exec.go`; signature `(ctx context.Context, enabled bool, sc *registry.SandboxConfig) (sandbox.Backend, error)` |
| Test Framework | `go test` + `testify` (`assert`/`require`) + `fakeDocker` shim | Same hermetic pattern as AC 02-01 |
| Key Dependencies | `internal/registry.SandboxConfig` (incl. `Validate()`, `internal/registry/sandbox.go:43-74`), `errors` (sentinel definition) | No changes to `SandboxConfig.Validate()` itself — behavior is asserted, not altered |

## Related Files
- `internal/verify/autofix_exec.go` - create: `enabled bool` parameter and the inverted-default branching (`sc == nil` under `enabled == true` is a hard error; `enabled == false` is a no-op, mirroring `ResolveExecBackend`'s disabled-path shape for call-site symmetry).
- `internal/verify/autofix_exec_test.go` - create: tests asserting the disabled-is-noop path, the unconfigured-is-hard-error path, and a regression test pinning `SandboxConfig.Validate()`'s current unconditional `Image`+`TestCommand` requirement (documenting the open tension, not resolving it).
- `internal/verify/exec.go` - reference (read-only): `ResolveExecBackend`'s opt-in default (`execEnabled=false` → `(nil, nil, 0, nil)`, `internal/verify/exec.go:25-27`) is the mirror-image contrast this story inverts.
- `internal/registry/sandbox.go` - reference (read-only): `Validate()` (`:43-74`) unconditionally requires `Image` (`:50-52`) and `TestCommand` (`:53-55`) regardless of whether the operator ever uses `--exec` — the constraint this story must surface as an explicit open design question, not silently work around.

### Related Files (from codebase-discovery.json)

- `internal/verify/autofix_exec.go` — create: the `enabled bool` parameter, inverted-default branching, and the distinct unconfigured-sentinel error (discovery `files_to_create`, based on `internal/verify/exec.go:24-57`).
- `internal/verify/autofix_exec_test.go` — create: posture tests (disabled-is-noop, unconfigured-is-hard-error) plus the regression pin asserting `internal/registry/sandbox.go:43-74`'s `Validate()` keeps requiring `Image` + `TestCommand` unconditionally (discovery `files_to_create`, based on `internal/verify/exec_test.go:15-23`).

## Happy Path Scenarios
**Scenario 1: Explicitly disabled sandboxing is a clean no-op, symmetric with `ResolveExecBackend`**
- **Given** `enabled = false` is passed (the shape Story 3's future `--no-sandbox` flag will produce) and any `*registry.SandboxConfig` (including `nil`)
- **When** `ResolveAutoFixSandbox(ctx, false, sc)` is called
- **Then** it returns `(nil, nil)` immediately, performing no field-override work and no `Preflight` call — identical in shape to `ResolveExecBackend(ctx, false, sc)`'s disabled path, so Story 3 can wire `--no-sandbox` into this parameter without reshaping the call.

**Scenario 2: Default-enabled call site with a fully valid config resolves normally**
- **Given** an auto-fix call site passes the literal default `enabled = true` (this story hard-codes `true`; Story 3 makes it conditional on the future flag) and a `*registry.SandboxConfig` that already passed `Validate()` at config-load time
- **When** the resolver runs against a passing `fakeDocker` shim
- **Then** it proceeds through the same build+preflight sequence as AC 02-01 and returns a ready backend — the inverted default changes only the "no config" and "preflight failure" paths, not the success path.

## Edge Cases
**Edge Case 1: Unconfigured sandbox block is a hard refusal, not a silent skip**
- **Given** `enabled = true` (the auto-fix default posture) and `sc == nil` (no `[sandbox]` block in `.atcr/config.yaml`)
- **When** `ResolveAutoFixSandbox(ctx, true, nil)` is called
- **Then** it returns a `nil` backend and a non-nil sentinel error (e.g. `ErrAutoFixSandboxUnconfigured`, mirroring `ErrExecNoBackend`'s pattern at `internal/verify/exec.go:16`) — inverted from `ResolveExecBackend`'s `execEnabled=false` no-op, because for auto-fix the "no config" state under the default posture must refuse, never fall back to unsandboxed execution.

**Edge Case 2: A project configuring `sandbox:` only for `--auto-fix` is still forced through `--exec`'s unconditional `TestCommand` requirement**
- **Given** an operator who never uses `--exec` adds a `sandbox:` block solely so `--auto-fix`'s validation step can be sandboxed, and omits `test_command` (irrelevant to their use case — `auto_fix.validate_command` is the config `--auto-fix` actually runs)
- **When** `.atcr/config.yaml` loads and `SandboxConfig.Validate()` runs
- **Then** config load still fails with `"sandbox.test_command is required when a sandbox block is present"` (`internal/registry/sandbox.go:54`) — this AC pins that CURRENT behavior with a regression test (asserting `Validate()` is unchanged) rather than relaxing it, and the resolver's own code/comments must document this as an open design question for a follow-up (e.g. a parallel light-validation path for auto-fix-only sandbox blocks) instead of silently loosening `--exec`'s existing contract.

**Edge Case 3: `enabled` parameter name and position must not collide with `ResolveExecBackend`'s `execEnabled` semantics**
- **Given** a future caller reads both resolvers' signatures side by side
- **When** comparing `ResolveExecBackend(ctx, execEnabled bool, sc)` (opt-in: `false` is the safe default) against `ResolveAutoFixSandbox(ctx, enabled bool, sc)` (opt-out: `true` is the safe default)
- **Then** the parameter name and doc comment on `ResolveAutoFixSandbox` must explicitly state the inverted polarity (documented as a comment stating "unlike ResolveExecBackend, enabled defaults to true for auto-fix call sites") so a future reader does not assume symmetric defaults.

**Edge Case 4: `--exec`'s own resolver and gate remain byte-for-byte untouched by this story**
- **Given** the new resolver ships in a new sibling file (`internal/verify/autofix_exec.go`), not inside `internal/verify/exec.go`
- **When** this story's changes land
- **Then** `internal/verify/exec.go` (`ResolveExecBackend` at `:24-57`, `ErrExecNoBackend` at `:16`) carries no modifications and the existing `internal/verify/exec_test.go` suite (`TestResolveExecBackend_DisabledIsNoOp`, `_RefusesWithoutBackend`, `_BuildsAndPreflights`, `_PreflightFailureRefuses` — `exec_test.go:25-65`) passes without any edit, proving the `--exec` (Epic 11.0) behavior is unaffected outside the auto-fix validation call site, per plan.md's Planning Success Criteria.

## Error Conditions
**Error Scenario 1: Unconfigured sandbox under the default posture**
- Error message: `"--auto-fix requires a [sandbox] block in .atcr/config.yaml for its validation step (or an explicit opt-out); none is configured"` (exact wording TBD at implementation, but MUST be a distinct sentinel from `ErrExecNoBackend` since the two features have different default polarities and this story's error must be independently `errors.Is`-checkable in tests).
- HTTP status / error code: N/A (CLI usage error, surfaced as part of AC 02-03's combined exit-2 gate).

**Error Scenario 2: Preflight failure under the default posture (cross-reference AC 02-01)**
- Error message: contains `"preflight"`, matching AC 02-01 Error Scenario 1 — restated here only to confirm the hard-refusal discipline holds identically whether the failure is "unconfigured" or "configured but failing preflight."

## Performance Requirements
- **Response Time:** The `enabled == false` branch must short-circuit before any Docker config construction or `Preflight` call — O(1), no subprocess spawned.
- **Throughput:** N/A (single-call resolver).

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** The inverted-default branch must never silently degrade to unsandboxed execution when `enabled == true` and configuration/preflight fails — this is the core fail-closed guarantee the epic depends on; a test must assert that no code path returns a non-nil error alongside a non-nil backend (partial success is never valid).

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `enabled=true`/`enabled=false` combined with `sc=nil`, `sc=<valid>`, `sc=<preflight-failing>`.
**Mock/Stub Requirements:** `fakeDocker` shim (same as AC 02-01); no mock needed for the `Validate()` regression test — it exercises the real `registry.SandboxConfig.Validate()` against a fixture missing `test_command`.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `enabled=false` returns `(nil, nil)` with zero Docker/Preflight work, symmetric with `ResolveExecBackend`'s disabled path
- [x] `enabled=true` with `sc=nil` returns a non-nil sentinel error distinct from `ErrExecNoBackend`
- [x] A regression test pins `SandboxConfig.Validate()`'s current unconditional `Image`+`TestCommand` requirement (proving this story did not silently relax it)
- [x] The existing `internal/verify/exec_test.go` suite (`TestResolveExecBackend_*`) passes unmodified, proving the `--exec` path is untouched
- [x] The resolver's doc comment explicitly states the inverted default polarity relative to `ResolveExecBackend`

**Manual Review:**
- [ ] Code reviewed and approved
