# Auto-Fix Validation Contract (RunConfiguredValidation, ValidationResult) `[CRITICAL]`

## Overview

`internal/verify/localvalidate.go` is the post-apply, language-independent validation gate for the `--auto-fix` flow (Sprint 17.0). It runs the operator-configured validation command against the working tree AFTER a patch has been applied, directly on the host via `os/exec.CommandContext` — and it is the exact call site this plan re-routes through `internal/sandbox`.

> Source: internal/verify/localvalidate.go:14-22 (file-level comment): "localvalidate.go is the post-apply, language-independent validation gate for the --auto-fix flow (Sprint 17.0, Story 2). ... this runs a real operator-supplied command against the working tree AFTER a patch has been applied, and reports a conservative pass/fail. Exit code is the sole signal — no stdout/stderr content heuristics — so behavior is fully owned by whoever configures the command."

`RunConfiguredValidation` has exactly one production caller — `runAutoFix` at cmd/atcr/autofix.go:252 — so a signature change or sibling-function split touches a single call site.

> Source: codebase-discovery.json `build_from.suggested_approach`: "RunConfiguredValidation has exactly one production caller (cmd/atcr/autofix.go:252), so a signature change or sibling-function split touches a single call site."

The contract documented here is what the sandboxed path must preserve: `runAutoFix`'s failure handling distinguishes `verr != nil` ("cannot even validate" — fail closed, revert) from `!res.Passed()` ("validation failed" — revert), and that taxonomy must keep its meaning whether the command ran on the host or in a container.

## Key Concepts

### Error taxonomy: a non-zero exit is NOT a Go error

`RunConfiguredValidation` returns a non-nil Go error ONLY when the command could not start (or argv is empty) — the `StartError` case. A command that runs and exits non-zero, or that times out, is a completed result with a nil error and `Passed()==false`. The command is sourced entirely from argv; no shell interprets it, so no configured or injected value is expanded.

> Source: internal/verify/localvalidate.go:72-80 (`RunConfiguredValidation` doc comment).

This mirrors the sandbox side's own rule ("a non-zero program exit is NOT an error" on `sandbox.Backend.Run`), but the two taxonomies diverge on runtime faults: docker exit codes 125-127 and signal deaths (128+N) surface as Go errors from `sandbox.Backend.Run`, while the host path reports any exit code as a completed result. The adapter must decide where sandbox backend faults land — the natural mapping is the `StartError`/`verr` channel ("cannot even validate"), which already fails closed and reverts.

> Source: codebase-discovery.json integration_gaps ("No adapter between sandbox.RunResult and verify.ValidationResult"): "runAutoFix's failure branches (verr vs !Passed()) depend on that taxonomy. ... Build a deliberate translation in the sandboxed validation path (sibling function or Backend-parameterized variant), preserving the host path's contract byte-for-byte."

### `ValidationResult` fields

`ValidationResult` carries split `Stdout`/`Stderr` (raw bytes as-is, never sanitized — display-time sanitization is a reporting-boundary concern), per-stream truncation flags, `Duration`, `TimedOut`, and `StartError`. `TimedOut` and `StartError` are distinct fields rather than folded into a fabricated exit code.

> Source: internal/verify/localvalidate.go:47-62 (`ValidationResult` struct + doc comment).

`Passed()` is the sole conservative pass/fail gate: a run passes only when it started cleanly, did not time out, and exited zero — with no inspection of stdout/stderr content, closing off any injection vector via crafted validation-command output (AC 02-03).

> Source: internal/verify/localvalidate.go:64-70 (`Passed` doc comment + body).

### Output caps and timeout machinery

- `maxValidationOutputBytes = 1 << 20` (1 MiB) bounds each of stdout and stderr separately; over-cap output is discarded and flagged via `StdoutTruncated`/`StderrTruncated`, and the child is never blocked (the capturing `cappedBuffer` always reports a full consume).
  > Source: internal/verify/localvalidate.go:24-28 (`maxValidationOutputBytes`), :188-211 (`cappedBuffer`).
- `defaultValidationTimeout = 2 * time.Minute` applies when the caller passes a zero timeout, so a missing config value does not immediately fail every validation with `DeadlineExceeded`.
  > Source: internal/verify/localvalidate.go:42-45.
- `validationWaitGrace = 2 * time.Second` bounds post-timeout pipe-close waits; on unix, `configureProcessGroup` makes a timeout SIGKILL the whole process group so grandchildren spawned via `sh -c` are reaped, with `cmd.WaitDelay` as the platform-independent backstop.
  > Source: internal/verify/localvalidate.go:30-40 (`validationWaitGrace`), :100-106 (process-group setup).

### Validation command resolution

`ResolveValidateCommand` picks the effective argv: an operator-configured command always wins; otherwise the single built-in default `["go", "build", "./..."]` applies ONLY when a `go.mod` exists at repoRoot. Any other project with no configured command is a hard refusal, so `--auto-fix` never skips validation silently.

> Source: internal/verify/localvalidate.go:171-186 (`ResolveValidateCommand` doc comment + body).

### The translation gap the sandboxed path must bridge

`sandbox.RunResult` (combined `Output`, `ExitCode` with 124 as the conventional timeout code, `TimedOut`, `Command`) differs from `ValidationResult` on every axis below, per the verified shapes in internal/sandbox/sandbox.go:69-80 and internal/verify/localvalidate.go:53-62:

| Aspect | `sandbox.RunResult` (sandbox.go) | `verify.ValidationResult` (localvalidate.go) | Adapter decision needed |
|---|---|---|---|
| Output | Single combined stdout+stderr, truncated to backend budget with marker | Split `Stdout`/`Stderr`, per-stream 1 MiB caps | Combined output cannot be re-split; map to one stream or document the merge |
| Truncation | No flags; marker text in `Output` | `StdoutTruncated`/`StderrTruncated` bools | Derive or drop flags consciously |
| Timeout | `TimedOut` + conventional exit code 124 | `TimedOut` bool (no fabricated exit code) | Direct map; do not leak 124 into `ExitCode` semantics |
| Runtime faults | Go `error` (docker 125-127, signals 128+N, spawn failure) | `StartError` + non-nil `error` return | Map backend faults to the `verr`/`StartError` channel (fail closed) |
| Program exit | `ExitCode` (any non-zero is a result, not an error) | `ExitCode` (same rule) | Direct map |
| Duration | Not captured | `Duration time.Duration` | Adapter must measure wall-clock itself |
| Command echo | `Command` human-readable string | No equivalent field | Evidence/reporting concern, not result contract |

## Code Examples

Verbatim from `internal/verify/localvalidate.go`:

```go
type ValidationResult struct {
	ExitCode        int
	Stdout          string
	Stderr          string
	Duration        time.Duration
	TimedOut        bool
	StartError      error
	StdoutTruncated bool
	StderrTruncated bool
}
```

```go
func (r ValidationResult) Passed() bool {
	return r.StartError == nil && !r.TimedOut && r.ExitCode == 0
}
```

```go
func RunConfiguredValidation(ctx context.Context, argv []string, dir string, timeout time.Duration) (ValidationResult, error)
```

> Source: internal/verify/localvalidate.go:53-62 (`ValidationResult`), :68-70 (`Passed`), :80 (`RunConfiguredValidation` signature).

The single production call site (cmd/atcr/autofix.go:252):

```go
res, verr := verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
```

> Source: cmd/atcr/autofix.go:252 (inside `runAutoFix`).

## Quick Reference

| Aspect | Detail |
|---|---|
| File | `internal/verify/localvalidate.go` (host-only today via `os/exec.CommandContext`) |
| Signature | `RunConfiguredValidation(ctx context.Context, argv []string, dir string, timeout time.Duration) (ValidationResult, error)` |
| Non-nil error | ONLY the `StartError` case: empty argv, missing working dir, binary not found / not executable, or other failure to run |
| Nil error + `Passed()==false` | Command ran and exited non-zero, OR timed out (incl. parent-context cancel and `WaitDelay` unclean exit, both folded into `TimedOut`) |
| `Passed()` | `StartError == nil && !TimedOut && ExitCode == 0` — exit code is the sole signal |
| Output budget | `maxValidationOutputBytes` = 1 MiB per stream (stdout and stderr capped independently) |
| Default timeout | `defaultValidationTimeout` = 2m when caller passes zero |
| Kill semantics | unix: process-group SIGKILL on timeout (reaps `sh -c` grandchildren); `validationWaitGrace` = 2s pipe-close backstop |
| Command source | `auto_fix.validate_command` argv, else `["go", "build", "./..."]` only with a `go.mod`; otherwise hard refusal (`ResolveValidateCommand`) |
| Sole production caller | `runAutoFix`, cmd/atcr/autofix.go:252 |
| Contract pinned by | `internal/verify/localvalidate_test.go` — defines "no behavior change" for the host path |

## Related Documentation

- Plan overview: [../plan.md](../plan.md)
- Related category file in this same `documentation/` directory: `sandbox-backend-interface.md` — the `sandbox.RunResult` / `Backend.Run` side of the translation gap documented above.
- Related category file in this same `documentation/` directory: `autofix-gate-and-config.md` — the `validateAutoFixBackend` gate and `autoFixBackend` struct that carry the validation argv/timeout into `runAutoFix`.
- Related category file in this same `documentation/` directory: `sandbox-testing-patterns.md` — includes the `internal/verify/localvalidate_test.go` row pinning this contract.
