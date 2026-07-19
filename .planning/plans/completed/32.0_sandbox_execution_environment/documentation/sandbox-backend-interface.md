# Sandbox Backend Interface (Backend, RunSpec, RunResult)

**Priority: CRITICAL**

## Overview

The `internal/sandbox` package provides an opt-in, container-isolated executor for running untrusted, model-authored commands and scripts during a review (Epic 11.0). As the package doc states, execution is "the last and most dangerous rung of the review ladder: it runs code an LLM wrote on the operator's machine," which is why containment is treated as a precondition rather than a feature.

> Source: internal/sandbox/sandbox.go (package doc)

The package defines three core building blocks: `RunSpec` (what to execute and where), `RunResult` (the captured outcome), and `Backend` (the pluggable interface that executes a `RunSpec` and produces a `RunResult`). `DockerBackend` is the only implementation shipped in Epic 11.0, but the `Backend` interface is deliberately kept narrow — three methods — so that Podman or a remote runner could be a drop-in replacement later.

> Source: internal/sandbox/sandbox.go: "// Backend is a pluggable sandbox executor. Docker is the only implementation in\n// Epic 11.0; the interface keeps Podman (or a remote runner) a drop-in later."

For the `--auto-fix` pipeline, this existing `Backend` interface is the sandbox to route `internal/verify.RunConfiguredValidation` through — the plan goal explicitly states no new sandbox package or backend is needed. Two integration gaps must be resolved deliberately when wiring this in: `RunSpec` currently hardcodes a read-only snapshot mount with no mount-mode option, and there is no existing adapter translating between `sandbox.RunResult` and `verify.ValidationResult`.

## Key Concepts

### Containment guarantees every Backend must provide

Every `Backend` implementation MUST guarantee, for every Run: no network access, a read-only view of the snapshot, resource caps (memory, CPU, PIDs), and non-root/dropped-capabilities/no-new-privileges execution.

> Source: internal/sandbox/sandbox.go (package doc): "Every Backend MUST guarantee, for every Run:\n//\n//   - no network (the run cannot exfiltrate or call out),\n//   - a read-only view of the snapshot (the run cannot mutate the work tree),\n//   - resource caps (memory, CPU, PIDs) so a run cannot exhaust the host,\n//   - non-root, dropped capabilities, and no-new-privileges."

### Opt-in only, no bare-metal fallback

The package never enables itself; callers must opt in via `--exec` combined with a backend that passes `Preflight`. Bare-metal execution is intentionally unsupported.

> Source: internal/sandbox/sandbox.go (package doc): "The package never enables itself: callers opt in via `--exec` and a backend\n// that passes Preflight. Bare-metal execution is intentionally unsupported."

### RunSpec: Command XOR Script, validated

`RunSpec` describes a single sandboxed execution request. Exactly one of `Command` (argv, no shell interpolation) or `Script` (fed to `/bin/sh -s` over stdin) must be set. `Timeout` bounds wall-clock duration (zero uses the backend default), and `SnapshotDir` is the host directory mounted read-only as the working tree.

> Source: internal/sandbox/sandbox.go:RunSpec struct + doc comments

The `validate()` method enforces: exactly one of Command/Script is set, `SnapshotDir` is non-empty, `SnapshotDir` is absolute, and `SnapshotDir` does not contain a colon (to prevent mount-spec injection).

> Source: internal/sandbox/sandbox.go:func (s RunSpec) validate() error — "A path containing ':' could inject extra mount options (e.g. strip :ro), so\n\t// reject it; require an absolute path so the mount source is unambiguous."

This validation logic is directly relevant to the auto-fix integration gap: RunSpec's mount is hardcoded read-only, so if a configured validation command needs to write into the (already-mutated) working tree, the current `SnapshotDir` mechanism cannot express that mode — a design decision is needed before wiring auto-fix through this interface.

> Source: internal/sandbox/docker.go:133 (`-v spec.SnapshotDir + ":/work:ro"` in `dockerRunArgs` — the referenced integration gap; the mount spec is interpolated from the `SnapshotDir` that `RunSpec.validate()` guards in internal/sandbox/sandbox.go:43-65)

### RunResult: captured outcome, non-zero exit is not a Go error

`RunResult` captures `Command` (human-readable rendering for the evidence_exec block/report), `ExitCode` (the container's exit status; 124 is the conventional timeout code), `Output` (combined stdout+stderr, truncated to the backend budget), and `TimedOut` (true if the run was killed after exceeding its deadline).

> Source: internal/sandbox/sandbox.go:RunResult struct + doc comments

This single combined-`Output`/exit-code-124-timeout shape differs from `verify.ValidationResult`, which has split Stdout/Stderr, per-stream truncation flags, Duration, and StartError — meaning the sandboxed auto-fix path needs a deliberate adapter/translation layer rather than a direct type reuse.

### Backend interface: Name, Preflight, Run

`Backend` is the pluggable executor interface with three methods: `Name()` identifies the backend for diagnostics and the evidence trail; `Preflight(ctx)` verifies the backend is usable (runtime installed, daemon reachable, base image present, trivial container runs to completion) and MUST pass before `Run` is used — the CLI refuses `--exec` otherwise; `Run(ctx, spec)` executes the spec in isolation, reserving the returned `error` for backend faults (spawn failure, malformed spec) since a non-zero program exit is explicitly NOT an error.

> Source: internal/sandbox/sandbox.go:Backend interface + doc comments

### Conventional timeout exit code

A package-level constant fixes the timeout exit status to match coreutils `timeout` behavior.

> Source: internal/sandbox/sandbox.go: "// timeoutExitCode is the conventional exit status for a killed-on-timeout run,\n// matching coreutils `timeout`.\nconst timeoutExitCode = 124"

### DockerBackend satisfies Backend

The package asserts at compile time that `DockerBackend` implements `Backend`.

> Source: internal/sandbox/sandbox.go: "var _ Backend = (*DockerBackend)(nil)"

## Code Examples

The following snippets are copied verbatim from `internal/sandbox/sandbox.go`.

RunSpec definition:

```go
type RunSpec struct {
	// Command is an argv executed directly inside the sandbox. No shell
	// interpolation is performed. Mutually exclusive with Script.
	Command []string
	// Script is a shell script body fed to `/bin/sh -s` over stdin and executed
	// in the writable scratch overlay. It is never interpolated into argv.
	// Mutually exclusive with Command.
	Script string
	// Timeout bounds the wall-clock duration of the run. Zero uses the backend
	// default.
	Timeout time.Duration
	// SnapshotDir is the host directory mounted read-only as the working tree.
	SnapshotDir string
}
```

RunSpec validation:

```go
func (s RunSpec) validate() error {
	hasCmd := len(s.Command) > 0
	hasScript := s.Script != ""
	switch {
	case hasCmd && hasScript:
		return errors.New("sandbox: RunSpec must set exactly one of Command or Script, not both")
	case !hasCmd && !hasScript:
		return errors.New("sandbox: RunSpec must set one of Command or Script")
	}
	if s.SnapshotDir == "" {
		return errors.New("sandbox: RunSpec.SnapshotDir is required")
	}
	// The snapshot dir is interpolated into the `-v <dir>:/work:ro` mount spec.
	// A path containing ':' could inject extra mount options (e.g. strip :ro), so
	// reject it; require an absolute path so the mount source is unambiguous.
	if !filepath.IsAbs(s.SnapshotDir) {
		return fmt.Errorf("sandbox: RunSpec.SnapshotDir must be absolute, got %q", s.SnapshotDir)
	}
	if strings.ContainsRune(s.SnapshotDir, ':') {
		return fmt.Errorf("sandbox: RunSpec.SnapshotDir must not contain ':' (mount-spec injection), got %q", s.SnapshotDir)
	}
	return nil
}
```

RunResult definition:

```go
type RunResult struct {
	// Command is a human-readable rendering of what was executed, suitable for
	// the evidence_exec block and the report.
	Command string
	// ExitCode is the container's exit status (the program's, since the entry
	// process is the program). 124 is the conventional timeout code.
	ExitCode int
	// Output is the combined stdout+stderr, truncated to the backend budget.
	Output string
	// TimedOut is true when the run exceeded its deadline and was killed.
	TimedOut bool
}
```

Backend interface definition:

```go
type Backend interface {
	// Name identifies the backend for diagnostics and the evidence trail.
	Name() string
	// Preflight verifies the backend is usable: runtime installed, daemon
	// reachable, base image present, and a trivial container runs to completion.
	// It MUST pass before Run is used; the CLI refuses `--exec` otherwise.
	Preflight(ctx context.Context) error
	// Run executes spec in isolation and returns the captured result. err is
	// reserved for backend faults (spawn failure, malformed spec); a non-zero
	// program exit is NOT an error.
	Run(ctx context.Context, spec RunSpec) (RunResult, error)
}
```

## Quick Reference

**RunSpec fields**

| Field | Type | Notes |
|---|---|---|
| `Command` | `[]string` | Argv executed directly, no shell interpolation. Mutually exclusive with `Script`. |
| `Script` | `string` | Shell script body fed to `/bin/sh -s` over stdin, never interpolated into argv. Mutually exclusive with `Command`. |
| `Timeout` | `time.Duration` | Bounds wall-clock duration. Zero uses the backend default. |
| `SnapshotDir` | `string` | Host directory mounted **read-only** as the working tree. Must be absolute; must not contain `:` (mount-spec injection guard). |

**RunResult fields**

| Field | Type | Notes |
|---|---|---|
| `Command` | `string` | Human-readable rendering of what was executed (evidence_exec block / report). |
| `ExitCode` | `int` | Container's/program's exit status; `124` = conventional timeout code. |
| `Output` | `string` | Combined stdout+stderr, truncated to backend budget. |
| `TimedOut` | `bool` | True if the run exceeded its deadline and was killed. |

**Backend methods**

| Method | Signature | Purpose |
|---|---|---|
| `Name` | `Name() string` | Identifies the backend for diagnostics and the evidence trail. |
| `Preflight` | `Preflight(ctx context.Context) error` | Verifies runtime/daemon/image/trivial-container readiness. Must pass before `Run`; CLI refuses `--exec` otherwise. |
| `Run` | `Run(ctx context.Context, spec RunSpec) (RunResult, error)` | Executes spec in isolation. `error` reserved for backend faults; non-zero program exit is not an error. |

## Related Documentation

- Plan: [../plan.md](../plan.md)
- Related category files in this same `documentation/` directory: `docker-backend-implementation.md` (the `DockerBackend` implementation of this `Backend` interface) and `resolver-pattern-resolveexecbackend.md` (the resolver pattern used to select/construct a `Backend` at call sites).
- Related category files in this same `documentation/` directory: `autofix-validation-contract.md` — the `verify.ValidationResult` host-path contract that a sandboxed `RunResult` must be translated back into; and `autofix-gate-and-config.md` — the CLI gate and registry config surface where the resolved `Backend` is wired in.
