# Auto-Fix Gate & Config Surface (validateAutoFixBackend, SandboxConfig/AutoFixConfig, `--no-*` precedents) `[CRITICAL]`

## Overview

`cmd/atcr/autofix.go:validateAutoFixBackend` (:107) is the single, all-or-nothing CLI gate for `--auto-fix`. In one pass it resolves and shape-checks the required backend pieces — (1) an apply target, (2) a validation command, (3) GitHub token + owner/name repo — plus the validation timeout, collecting every problem into a `missing []string` and returning one usage error (exit 2) naming all of them, rather than failing on the first missing piece. It performs only local checks (env/flag reads, config-shape parsing, one `os.Stat`); it never makes a network call or executes the validation command.

> Source: cmd/atcr/autofix.go:89-106 (`validateAutoFixBackend` doc comment).

This plan adds sandbox backend resolution (and the `--no-sandbox` override) as a fourth piece in this same gate — following the established fail-fast, name-everything-missing style rather than introducing a new error-handling style.

> Source: codebase-discovery.json existing_patterns ("All-or-nothing CLI gate collecting every missing piece"): "follow_for: Adding sandbox backend resolution (and the --no-sandbox override) as a fourth piece in this same gate, consistent with the existing fail-fast, name-everything-missing style."

## Key Concepts

### The gate is already in scope for sandbox config — no new plumbing needed

`review` calls the gate as `validateAutoFixBackend(cmd, cfg.Project, ".")` at cmd/atcr/review.go:353, and `ProjectConfig` already carries both config blocks this plan's resolver reads: `Sandbox *SandboxConfig` (internal/registry/project.go:85) and `AutoFix *AutoFixConfig`. So `proj.Sandbox` is already in scope at the gate.

> Source: codebase-discovery.json `build_from.suggested_approach`: "validateAutoFixBackend already receives proj *registry.ProjectConfig (called with cfg.Project at cmd/atcr/review.go:353), so proj.Sandbox (internal/registry/project.go:85) is already in scope at the gate — no new config plumbing is needed."

### `autoFixBackend` is the carrier for the resolved backend

The gate returns a fully-resolved `autoFixBackend` struct so `runAutoFix` consumes it without re-resolving any piece (AC 06-03). The resolved `sandbox.Backend` (and the `--no-sandbox` decision) should ride this same struct into `runAutoFix`, whose call to `verify.RunConfiguredValidation` at cmd/atcr/autofix.go:252 becomes sandbox-routed when a backend is present.

> Source: cmd/atcr/autofix.go:57-67 (`autoFixBackend` struct + doc comment); codebase-discovery.json integration_points ("cmd/atcr/autofix.go:runAutoFix"): "Thread the backend through the autoFixBackend struct (:59)."

### Flag registration point: `addAutoFixFlags`

`addAutoFixFlags` (cmd/atcr/autofix.go:43) registers `--auto-fix` plus the GitHub credential flags (`--repo`, `--token`, `--api-url`), and is wired into `review` at cmd/atcr/review.go:92. The new `--no-sandbox` flag belongs here, with help text carrying the security warning.

> Source: codebase-discovery.json integration_points ("cmd/atcr/autofix.go:addAutoFixFlags"): "Register the new --no-sandbox flag here alongside the existing --auto-fix/--repo/--token/--api-url flags, with help text carrying the security warning."

### `--no-*` security opt-out precedents (and the stderr-warning precedent)

Existing flags that relax boundaries: `--no-ignore` (cmd/atcr/review.go:87, bypasses the .gitignore/.atcrignore payload filter), `--no-cache` (review.go:86), `--no-scorecard`/`--no-local-debt` (cmd/atcr/reconcile.go:48-49). None of them prints a warning today. The stderr security-warning precedent is the unrecognized-`ATCR_TELEMETRY` warning at cmd/atcr/main.go:348. Per plan.md Risk Mitigation, `--no-sandbox` must print its warning to stderr on EVERY run — strictly louder than any existing opt-out flag.

> Source: codebase-discovery.json existing_patterns ("Security-relevant opt-out `--no-*` flag").

### Registry config surface: two blocks, one open design decision

`internal/registry/sandbox.go:SandboxConfig` (the `sandbox:` block, Epic 11.0) fields: `Backend` (only `"docker"` supported), `Image`, `TestCommand`, `DockerPath`, `Memory`, `CPUs`, `PidsLimit`, `TimeoutSecs`. Its `Validate()` runs at config load and requires `Image` AND `TestCommand` unconditionally — `TestCommand` is an `--exec` concept (the `run_tests` tool), so reusing the block unmodified forces operators to set `test_command` even when they only use `--auto-fix`.

> Source: internal/registry/sandbox.go:20-38 (`SandboxConfig` struct), :43-74 (`Validate`); codebase-discovery.json integration_gaps ("Config surface mismatch for auto-fix sandboxing").

`internal/registry/autofix.go:AutoFixConfig` (the `auto_fix:` block, Sprint 17.0) fields: `ApplyTarget`, `ValidateCommand`, `ValidateTimeout` (a Go duration string, e.g. `"2m"`). Its `Validate()` runs at load time (no empty argv tokens; timeout must parse positive). This is the natural home for a config-level sandbox opt-out (e.g. `auto_fix.no_sandbox`) or a nested sandbox sub-block, if design chooses one — the epic allows "a --no-sandbox flag or config option".

> Source: internal/registry/autofix.go:10-30 (`AutoFixConfig` struct + doc comment), :36-55 (`Validate`); original-requirements.md Proposed Solution ("Opt-Out Fallback").

The open decision — reuse `sandbox:` with a relaxed `Validate()`, extend `SandboxConfig`, or add a parallel `auto_fix` sandbox sub-block — is explicitly deferred to `/design-sprint`, not assumed by the documentation.

> Source: codebase-discovery.json integration_gaps ("Config surface mismatch"): "Design decision: reuse `sandbox:` with a relaxed Validate(), extend SandboxConfig, or add a nested auto_fix sandbox/opt-out block validated in AutoFixConfig.Validate()."

### Timeout precedence to decide at design time

Auto-fix's validation timeout (`auto_fix.validate_timeout`, default 2m via cmd/atcr/autofix.go:36 `defaultValidationTimeout`) vs. the sandbox block's `timeout_secs` (default 60s via `sandbox.DefaultDockerConfig`). `RunSpec.Timeout` should carry the auto-fix value so sandboxing does not silently halve the operator's configured budget.

> Source: codebase-discovery.json architecture_notes ("Timeout precedence to decide at design time").

### Docs/ surface gap (Acceptance Criterion 2)

The `auto_fix:` config block is entirely undocumented in `docs/` today (only passing mentions of `--auto-fix` in docs/ci-integration.md and docs/agentic-consumption.md), the `sandbox:` block is documented only in docs/execution.md, and docs/registry.md covers neither block. The AC-required `--no-sandbox` security warnings have no natural home yet — extend docs/execution.md with an auto-fix section or create docs/auto-fix.md cross-linking it.

> Source: codebase-discovery.json integration_gaps ("No documentation home for the --auto-fix security posture") and architecture_notes ("Docs gap beyond the AC"); verified by searching docs/registry.md (no `sandbox`/`auto_fix` mentions).

## Code Examples

Verbatim from cmd/atcr/autofix.go:57-67:

```go
// autoFixBackend holds the fully-resolved backend the gate validated, so
// runAutoFix consumes it without re-resolving any piece (AC 06-03).
type autoFixBackend struct {
	applyTarget     string // absolute working-tree path the patch is applied to
	validateArgv    []string
	validateTimeout time.Duration
	owner           string
	repo            string
	token           string
	apiURL          string
}
```

The gate signature (cmd/atcr/autofix.go:107) and its wiring into `review` (cmd/atcr/review.go:353):

```go
func validateAutoFixBackend(cmd *cobra.Command, proj *registry.ProjectConfig, repoRoot string) (autoFixBackend, error)
```

```go
if autoFix {
	if afBackend, err = validateAutoFixBackend(cmd, cfg.Project, "."); err != nil {
		return err
	}
}
```

## Quick Reference

| Aspect | Detail |
|---|---|
| Gate | `validateAutoFixBackend(cmd, proj, repoRoot) (autoFixBackend, error)` — cmd/atcr/autofix.go:107 |
| Gate style | All-or-nothing: collects every missing piece into `missing []string`, returns one usageError (exit 2); local checks only |
| Current pieces | (1) apply target (existing dir, repo-root only, made absolute), (2) validation command (config or Go default), (3) GitHub token + owner/name repo (flags/env), plus validation timeout |
| New fourth piece | Sandbox backend resolution + `--no-sandbox` override (this plan) |
| Carrier struct | `autoFixBackend` (cmd/atcr/autofix.go:59) — add the resolved `sandbox.Backend` + no-sandbox decision here |
| Orchestration | `runAutoFix` (cmd/atcr/autofix.go:239); validation call at :252 is the sandbox-routing site |
| Flag registration | `addAutoFixFlags` (cmd/atcr/autofix.go:43), wired into `review` at cmd/atcr/review.go:92 |
| Review wiring | Gate called at cmd/atcr/review.go:353 with `cfg.Project` (so `proj.Sandbox` in scope); `orchestrateAutoFix` at review.go:661 |
| `sandbox:` block | `internal/registry/sandbox.go` — `Validate()` requires `Image` + `TestCommand` unconditionally; backend must be `"docker"` |
| `auto_fix:` block | `internal/registry/autofix.go` — `ApplyTarget`, `ValidateCommand`, `ValidateTimeout`; load-time `Validate()`; home for any config-level opt-out |
| `ProjectConfig` | Carries `Sandbox *SandboxConfig` (project.go:85) and `AutoFix *AutoFixConfig` — both blocks in scope at the gate |
| `--no-*` precedents | `--no-ignore` (review.go:87), `--no-cache` (review.go:86), `--no-scorecard`/`--no-local-debt` (reconcile.go:48-49) — none warns today |
| stderr warning precedent | cmd/atcr/main.go:348 (unrecognized `ATCR_TELEMETRY`); `--no-sandbox` must warn on EVERY run |
| Timeout precedence | `auto_fix.validate_timeout` (default 2m) should win over sandbox `timeout_secs` (default 60s) via `RunSpec.Timeout` — design-time decision |
| docs/ gap | `auto_fix:` undocumented; `sandbox:` only in docs/execution.md; docs/registry.md covers neither |

## Related Documentation

- Plan overview: [../plan.md](../plan.md)
- Related category file in this same `documentation/` directory: `resolver-pattern-resolveexecbackend.md` — the resolver pattern the new auto-fix sibling resolver mirrors, and the gating posture it inverts (sandbox on by default).
- Related category file in this same `documentation/` directory: `autofix-validation-contract.md` — the `RunConfiguredValidation` call this gate's resolved pieces feed at cmd/atcr/autofix.go:252.
- Related category file in this same `documentation/` directory: `sandbox-backend-interface.md` — the `sandbox.Backend` the resolved carrier struct will hold.
