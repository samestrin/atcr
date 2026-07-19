# Acceptance Criteria: Resolver Builds and Preflights a Sandbox Backend

**Related User Story:** [02: Sandbox Resolution and Preflight Gate for Auto-Fix](../user-stories/02-sandbox-resolution-and-preflight-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go resolver function `verify.ResolveAutoFixSandbox` | New file `internal/verify/autofix_exec.go`, mirrors `ResolveExecBackend` (`internal/verify/exec.go:24-57`) |
| Test Framework | `go test` + `testify` (`assert`/`require`) + `fakeDocker` shim | Hermetic POSIX shell shim from `internal/verify/exec_test.go:15-23`, no live Docker daemon |
| Key Dependencies | `internal/sandbox` (`DefaultDockerConfig`, `NewDockerBackend`, `Backend.Preflight`), `internal/registry.SandboxConfig` | No new dependency; reuses Epic 11.0 APIs as-is |

## Related Files
- `internal/verify/autofix_exec.go` - create: `ResolveAutoFixSandbox` resolver function performing the field-override + build + preflight sequence.
- `internal/verify/autofix_exec_test.go` - create: table-driven tests covering full/partial field overrides and preflight success, using the `fakeDocker` shim.
- `internal/verify/exec.go` - reference (read-only): `ResolveExecBackend` (`:24-57`) is the structural template for the override pattern (DockerPath, Image, Memory, CPUs, PidsLimit, TimeoutSecs) and the "preflight before returning" discipline.
- `internal/registry/sandbox.go` - reference (read-only): `SandboxConfig` field definitions (`:20-38`) consumed by the resolver.
- `internal/sandbox/sandbox.go` - reference (read-only): `Backend` interface (`:84-95`), `DefaultDockerConfig`/`NewDockerBackend` construction contract.

## Happy Path Scenarios
**Scenario 1: Full field override is applied before preflight**
- **Given** a `*registry.SandboxConfig` with `DockerPath` pointing at a passing `fakeDocker` shim, and `Image`, `Memory`, `CPUs`, `PidsLimit`, and `TimeoutSecs` all explicitly set
- **When** `ResolveAutoFixSandbox(ctx, true, sc)` is called
- **Then** every one of the six fields overrides `sandbox.DefaultDockerConfig()`'s corresponding field exactly as `ResolveExecBackend` does, `backend.Preflight(ctx)` is invoked and succeeds against the shim, and the function returns a non-nil `sandbox.Backend` and a `nil` error.

**Scenario 2: Partial config inherits hardened defaults for unset fields**
- **Given** a `*registry.SandboxConfig` with only `Image` and `TestCommand` set (`DockerPath`, `Memory`, `CPUs`, `PidsLimit`, `TimeoutSecs` left at zero value)
- **When** `ResolveAutoFixSandbox` runs against a passing `fakeDocker` shim
- **Then** the unset fields retain `sandbox.DefaultDockerConfig()`'s hardened values (not zeroed out), `Preflight` succeeds, and a ready `Backend` is returned — matching `ResolveExecBackend`'s "override only when set" contract.

## Edge Cases
**Edge Case 1: Nil pointer fields are not treated as explicit zero overrides**
- **Given** `SandboxConfig.PidsLimit` and `SandboxConfig.TimeoutSecs` are both `nil`
- **When** the resolver builds the Docker config
- **Then** `cfg.PidsLimit` and `cfg.Timeout` keep `DefaultDockerConfig()`'s values — a `nil` pointer must never be dereferenced or treated as an override to zero, mirroring `ResolveExecBackend`'s `if sc.PidsLimit != nil` / `if sc.TimeoutSecs != nil` guards (`internal/verify/exec.go:44,48`).

**Edge Case 2: TimeoutSecs override is reflected in the resolved timeout**
- **Given** `SandboxConfig.TimeoutSecs` is set to `120`
- **When** the resolver runs
- **Then** the resolved timeout equals `120 * time.Second`, consistent with `ResolveExecBackend`'s `timeout := time.Duration(*sc.TimeoutSecs) * time.Second` conversion (`internal/verify/exec.go:48-51`).

**Edge Case 3: Preflight runs to completion before any success value is returned**
- **Given** a `fakeDocker` shim that fails only on the `info` subcommand (simulating a host-capability check failure mid-preflight)
- **When** the resolver runs
- **Then** the resolver does not return a "ready" backend based on partial preflight progress — the full `Preflight(ctx)` call must complete and succeed before `ResolveAutoFixSandbox` returns a non-nil backend.

## Error Conditions
**Error Scenario 1: Preflight failure produces a wrapped, non-nil error and no backend**
- **Given** a `fakeDocker` shim that exits `1` on every invocation (docker daemon unreachable)
- **When** `ResolveAutoFixSandbox(ctx, true, sc)` is called
- **Then** it returns a `nil` backend and a non-nil wrapped error
- Error message: must contain the substring `"preflight"` (e.g. `"auto-fix sandbox preflight failed: %w"`), matching the assertion style of `TestResolveExecBackend_PreflightFailureRefuses` (`internal/verify/exec_test.go:57-65`).
- HTTP status / error code: N/A (CLI usage error path; exit code 2 is asserted at the `validateAutoFixBackend` integration level, covered by AC 02-03).

**Error Scenario 2: Construction never panics on a zero-value Backend field**
- **Given** `SandboxConfig.Backend` is an empty string (the config-load-time `Validate()` already defaults/accepts this; resolver-level Backend selection only supports `"docker"`)
- **When** the resolver constructs the Docker config
- **Then** it proceeds without a panic or nil-pointer dereference; backend selection remains Docker-only, exactly as `ResolveExecBackend` behaves today.

## Performance Requirements
- **Response Time:** Preflight adds no overhead beyond what `sandbox.DockerBackend.Preflight` already costs (a handful of `docker` subprocess invocations); the resolver itself performs O(1) field copies.
- **Throughput:** N/A (single-call resolver, not a hot path); the full `autofix_exec_test.go` suite must run in well under 1 second per test via the `fakeDocker` shim (no live Docker daemon dependency).

## Security Considerations
- **Authentication/Authorization:** N/A — the resolver performs no authentication; it only shapes a `sandbox.DockerBackend` config from already-loaded, trusted local YAML.
- **Input Validation:** The resolver must not weaken any guarantee `internal/sandbox` already enforces (no-network, read-only mount, resource caps, non-root/no-new-privileges — `internal/sandbox/sandbox.go:1-14`); it performs no execution itself, only config construction and a `Preflight` call, so it introduces no new attack surface beyond what Epic 11.0 already ships.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `*registry.SandboxConfig` fixtures covering: all fields set, minimal fields set (Image + TestCommand only), nil `PidsLimit`/`TimeoutSecs`.
**Mock/Stub Requirements:** `fakeDocker(t, body string) string` shim (copy the exact helper from `internal/verify/exec_test.go:15-23`) standing in for the `docker` binary via `DockerConfig.DockerPath`; no live Docker daemon, no network access, no `testify` mock objects needed beyond `assert`/`require`.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `ResolveAutoFixSandbox` applies all six field overrides (DockerPath, Image, Memory, CPUs, PidsLimit, TimeoutSecs) only when set, leaving unset fields at `DefaultDockerConfig()` values
- [ ] `Preflight(ctx)` is called and must succeed before any non-nil backend is returned
- [ ] A preflight failure returns `(nil, <wrapped error containing "preflight">)`, never a partially-ready backend
- [ ] Test suite runs hermetically (no live Docker) via the `fakeDocker` shim pattern

**Manual Review:**
- [ ] Code reviewed and approved
