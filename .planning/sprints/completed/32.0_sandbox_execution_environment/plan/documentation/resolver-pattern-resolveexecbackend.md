# Resolver Pattern — ResolveExecBackend (mirror for auto-fix)

`CRITICAL`

## Overview

`internal/verify.ResolveExecBackend` is the reference implementation for this epic's core mechanism: turning a raw `*registry.SandboxConfig` into a preflight-checked, ready-to-run `sandbox.Backend`. It was built for Epic 11.0's `--exec` reviewer-reproduction feature, but the resolve-and-preflight shape — build a default config, override from the registry config block, construct a `sandbox.NewDockerBackend`, run `Preflight`, return the ready backend — is exactly what the auto-fix pipeline's post-apply validation step needs.

> Source: internal/verify/exec.go — codebase-discovery.json `build_from.primary_file` reasoning: "ResolveExecBackend (internal/verify/exec.go:24-57) already implements the exact pattern this epic needs: given a `*registry.SandboxConfig`, build a `sandbox.DefaultDockerConfig()`, override it from the config block, construct a `sandbox.NewDockerBackend`, and run `Preflight` before returning a ready `sandbox.Backend`. It was built for the `--exec` reviewer-reproduction feature (Epic 11.0) but the resolve-and-preflight shape is identical to what auto-fix's validation step needs."

The function is also the model for the *gating* posture, not just the resolver mechanics. `ResolveExecBackend` is off-by-default: pass `execEnabled=false` and it returns `(nil, nil, 0, nil)` with no side effects — execution simply stays disabled, this is the normal path. When `execEnabled=true`, it refuses to proceed without a configured backend (`ErrExecNoBackend`) and refuses again if `Preflight` fails. For auto-fix, this plan inverts that default: sandboxed validation should be *on* by default, with an explicit `--no-sandbox` flag required to bypass it (carrying a security warning), rather than requiring an opt-in flag to turn sandboxing on. The refuse-on-failure discipline (hard error rather than silent fallback) is what should be preserved from this pattern.

> Source: codebase-discovery.json pattern "Opt-in execution gate with refuse-without-backend": "internal/verify.ResolveExecBackend(ctx, execEnabled, sc) returns (nil,nil,0,nil) when execEnabled is false, and hard-errors via ErrExecNoBackend when true but sc is nil, requiring Preflight() to pass before returning a ready sandbox.Backend. ... Follow for: building the analogous resolver for --auto-fix's validation step; also the model for how a --no-sandbox opt-out flag should invert the default (sandbox on by default, explicit flag to bypass, with a security warning) rather than mirroring --exec's off-by-default posture."

## Key Concepts

- **Refuse-without-backend gate.** `ErrExecNoBackend` is a sentinel error returned when execution is requested but no `[sandbox]` block is configured in `.atcr/config.yaml`. The doc comment explicitly frames this as "the refuse-without-backend gate (Epic 11.0 SC-1): the command must hard-error without executing anything."
  > Source: internal/verify/exec.go:14-17 (`ErrExecNoBackend` declaration and comment)

- **Never-implicit-enable contract.** The function's doc comment states it "never enables execution implicitly": `execEnabled=false` short-circuits to a no-op nil return; `execEnabled=true` always requires a fully-validated, preflighted backend before returning success.
  > Source: internal/verify/exec.go:19-24 (`ResolveExecBackend` doc comment)

- **Config-override-onto-defaults construction.** The resolver starts from `sandbox.DefaultDockerConfig()` and only overrides fields that are non-zero/non-nil on the `*registry.SandboxConfig` (`DockerPath`, `Image`, `Memory`, `CPUs`, `PidsLimit`, `TimeoutSecs`). This is the shape a sibling resolver should reuse for auto-fix's validation-sandbox config.
  > Source: internal/verify/exec.go:29-46 (config field overrides in `ResolveExecBackend`)

- **Preflight before hand-back.** The backend is constructed via `sandbox.NewDockerBackend(cfg)` and then `backend.Preflight(ctx)` is called; any preflight error is wrapped (`fmt.Errorf("--exec preflight failed: %w", err)`) and returned as the sole error, with all other return values zeroed. Only a passing preflight yields a usable backend.
  > Source: internal/verify/exec.go:47-52 (`NewDockerBackend` + `Preflight` call)

- **Suggested extension point.** The codebase-discovery notes recommend adding a sibling resolver file rather than overloading this one, so auto-fix's gate and `--exec`'s gate can diverge in default posture while sharing the resolve/preflight mechanics.
  > Source: codebase-discovery.json files_to_create: "internal/verify/autofix_exec.go (or similar), purpose: New resolver mirroring internal/verify/exec.go's ResolveExecBackend, specialized for auto-fix's validation-sandbox resolution and --no-sandbox handling. based_on: internal/verify/exec.go"

## Code Examples

Verbatim from `internal/verify/exec.go`:

```go
// ErrExecNoBackend is returned when `--exec` is requested but no sandbox backend
// is configured. It is the refuse-without-backend gate (Epic 11.0 SC-1): the
// command must hard-error without executing anything.
var ErrExecNoBackend = errors.New("--exec requires a [sandbox] block in .atcr/config.yaml (backend, image, test_command); none is configured")
```

```go
// ResolveExecBackend implements the execution gate. When execEnabled is false it
// returns nil (execution off — the normal path). When true it REQUIRES a
// configured sandbox backend that passes a preflight check; any failure is an
// error so the caller refuses the run. It never enables execution implicitly.
//
// Returns the ready backend, the resolved test command, and the per-run timeout.
func ResolveExecBackend(ctx context.Context, execEnabled bool, sc *registry.SandboxConfig) (sandbox.Backend, []string, time.Duration, error) {
	if !execEnabled {
		return nil, nil, 0, nil
	}
	if sc == nil {
		return nil, nil, 0, ErrExecNoBackend
	}
	cfg := sandbox.DefaultDockerConfig()
	if sc.DockerPath != "" {
		cfg.DockerPath = sc.DockerPath
	}
	if sc.Image != "" {
		cfg.Image = sc.Image
	}
	if sc.Memory != "" {
		cfg.Memory = sc.Memory
	}
	if sc.CPUs != "" {
		cfg.CPUs = sc.CPUs
	}
	if sc.PidsLimit != nil {
		cfg.PidsLimit = *sc.PidsLimit
	}
	timeout := cfg.Timeout
	if sc.TimeoutSecs != nil {
		timeout = time.Duration(*sc.TimeoutSecs) * time.Second
		cfg.Timeout = timeout
	}
	backend := sandbox.NewDockerBackend(cfg)
	if err := backend.Preflight(ctx); err != nil {
		return nil, nil, 0, fmt.Errorf("--exec preflight failed: %w", err)
	}
	return backend, sc.TestCommand, timeout, nil
}
```

> Source: internal/verify/exec.go (full function body, lines 24-57 per codebase-discovery.json line reference)

## Quick Reference

| Aspect | Detail |
|---|---|
| Signature | `ResolveExecBackend(ctx context.Context, execEnabled bool, sc *registry.SandboxConfig) (sandbox.Backend, []string, time.Duration, error)` |
| Param: `ctx` | Context passed through to `backend.Preflight(ctx)` |
| Param: `execEnabled` | Gate flag; `false` = execution off (normal path), `true` = execution required |
| Param: `sc` | `*registry.SandboxConfig` sourced from `.atcr/config.yaml`'s `[sandbox]` block; may be `nil` |
| Return: backend | Ready `sandbox.Backend` (via `sandbox.NewDockerBackend`) on success, else `nil` |
| Return: test command | `sc.TestCommand` on success, else `nil` |
| Return: timeout | Resolved `time.Duration` (from `sc.TimeoutSecs` or `cfg.Timeout` default), else `0` |
| Return: error | `nil` on success or on the `execEnabled=false` no-op path; non-nil otherwise |
| Branch: `execEnabled=false` | Returns `(nil, nil, 0, nil)` immediately — execution stays off, no side effects |
| Branch: `sc=nil` (with `execEnabled=true`) | Returns `(nil, nil, 0, ErrExecNoBackend)` — hard refusal, no backend built |
| Branch: `Preflight` failure | Returns `(nil, nil, 0, fmt.Errorf("--exec preflight failed: %w", err))` — backend constructed but not usable |
| Branch: success | Config built from `sandbox.DefaultDockerConfig()` overridden by `sc` fields, `Preflight` passes, returns `(backend, sc.TestCommand, timeout, nil)` |
| Config fields overridden from `sc` | `DockerPath`, `Image`, `Memory`, `CPUs`, `PidsLimit`, `TimeoutSecs` (each only if set/non-zero on `sc`) |
| Wiring point (existing `--exec` feature) | `cmd/atcr's` `--exec` flag on verify/review wires this via `resolveExec` in `cmd/atcr/verify.go:46` (call at `cmd/atcr/verify.go:54`), per codebase-discovery.json |

## Related Documentation

- Plan overview: [../plan.md](../plan.md)
- Related category file (same `documentation/` directory): `sandbox-backend-interface.md` — the `sandbox.Backend` interface and `sandbox.NewDockerBackend` construction that this resolver returns/consumes.
- Related category file (same `documentation/` directory): `docker-backend-implementation.md` — the concrete Docker backend implementation behind `sandbox.NewDockerBackend`, including its `Preflight` behavior referenced above.
- Related category file (same `documentation/` directory): `autofix-gate-and-config.md` — the `validateAutoFixBackend` gate and registry config surface the new auto-fix sibling resolver plugs into.
