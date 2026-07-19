# CLI Command & Output Control Patterns (Cobra)

**Priority: Critical**

## Overview

Cobra is the library for creating modern CLI applications with subcommand-based interfaces (`app verb noun --flag`), used by projects like Kubernetes, Hugo, and the GitHub CLI. It provides POSIX-compliant flags via pflag, nested subcommands with global/local/cascading flags, intelligent command suggestions, and automatic help and shell-completion generation (Apache-2.0 licensed). atcr is built as one root `atcr` command with subcommands — cobra.md's "Integration Notes (atcr)" lists the Epic 1.0-era set (`review`, `reconcile`, `report`, `range`, `init`, `serve`, `status`, `anchor`, `doctor`, `trust`), which the live tree has since outgrown: `anchor` no longer exists as a subcommand, and `verify`, `debate`, `github`, `debt`, `models`, `personas`, `benchmark`, `audit-report`, `quality-report`, `leaderboard`, `scorecard`, `history`, `config`, `version`, and `quickstart` have been added (cmd/atcr/*.go `Use:` declarations). The staleness of that list does not weaken the mechanism this document describes — a root-level `PersistentFlags()` registration is inherited by every current *and future* subcommand — but it does mean the set of findings-emitting surfaces is larger than the spec's list implies.

For the `--axi` (Agent eXperience Interface) output mode, the relevant surface area of Cobra is narrower than the full library: flag registration through `Flags()`/`PersistentFlags()` (so `--axi` can be declared once and inherited by every subcommand that emits findings), the `PersistentPreRunE` lifecycle hook (the same mechanism atcr already uses to inject the root logger and telemetry client into `cmd.Context()`, per codebase-discovery.json), the output-control methods (`cmd.OutOrStdout()`, `SetOut()`/`SetErr()`) that let a command's output be redirected for testability rather than hard-coded to `os.Stdout`, and the exit-code decision pattern where Cobra returns the error from `Execute()` and the caller resolves the process exit code in one place in `main()`.

Cobra's documented pattern for atcr is to use `RunE` variants everywhere and return errors, map `--fail-on` threshold violations to a distinct exit code in `main()` (CI gate semantics) rather than calling `os.Exit` inside handlers, and use `ExecuteContext` so one root context carries the global timeout into the fan-out engine (cobra.md:"Integration Notes (atcr)"). This is the same context-threading mechanism the AXI plan needs: the axi output-mode value (and its `ATCR_AXI_MAX_LINES` companion) should be threaded through `cmd.Context()` alongside the existing logger and telemetry client, following the precedent set by `PersistentPreRunE` at cmd/atcr/main.go:230 (codebase-discovery.json).

## Key Concepts

### Flag registration: `Flags()` vs `PersistentFlags()`

```go
cmd.Flags()            // local flags
cmd.PersistentFlags()  // flags inherited by all children
```

Typed registration looks like `Flags().StringVar(&v, "base", "", "base ref")` or `IntP("timeout", "t", 900, "...")` (the `P`-suffix variant adds a short flag).

> Source: [cobra.md: Flags (pflag-backed)]

Because `--axi` needs to be available across every subcommand that emits review findings (not just one), it is a `PersistentFlags()` registration on the root command — the same category of flag documented for inheriting flags "by all children."

Cobra also supports declarative flag relationships validated before `RunE` runs: `MarkFlagRequired(name)`, `MarkFlagsMutuallyExclusive(names...)`, `MarkFlagsRequiredTogether(names...)`, `MarkFlagsOneRequired(names...)`.

> Source: [cobra.md: Declarative flag relationships (validated by cobra before RunE)]

### `PersistentPreRunE` for context injection

The `Command` struct's run lifecycle hooks execute in order:

```go
PersistentPreRunE  func(cmd *Command, args []string) error  // inherited by children
PreRunE            func(cmd *Command, args []string) error  // local only
RunE               func(cmd *Command, args []string) error  // main logic (E = returns error)
PostRunE           func(cmd *Command, args []string) error
PersistentPostRunE func(cmd *Command, args []string) error
```

> Source: [cobra.md: Command struct (central building block)]

atcr already uses `PersistentPreRunE` (cmd/atcr/main.go:230) to inject the root logger and telemetry client into `cmd.Context()`; every subcommand retrieves them via `FromContext` helpers (codebase-discovery.json). The axi output-mode value should be threaded the same way — resolved once in `PersistentPreRunE` (flag value plus `ATCR_AXI_MAX_LINES` env parsing, following the `telemetryEnabledFromEnv` precedent noted in codebase-discovery.json's `files_to_modify`) and stored on the context for every subcommand to read.

### Output control: `cmd.OutOrStdout()` / `SetOut()` / `SetErr()`

```
cmd.OutOrStdout(), cmd.OutOrStderr(), cmd.Printf(), SetOut()/SetErr()/SetIn()
```

route output through the command for testability instead of writing to `os.Stdout` directly.

> Source: [cobra.md: Output control]

This is the mechanism that keeps AXI output layered on top of atcr's existing stderr-only diagnostics contract rather than replacing it: AXI-mode findings are written via `cmd.OutOrStdout()`, while diagnostics continue through `cmd.OutOrStderr()`/`SetErr()`, so the two streams stay independently redirectable and testable.

### Exit-code decision in `main()`

Cobra returns the error from `Execute()`; the process exit code is decided in one place in `main()`.

> Source: [cobra.md: Integration Notes (atcr): "Exit-code semantics: cobra returns the error from `Execute()`; decide the process exit code in one place in `main()`."]

atcr's existing contract implements this: cmd/atcr/main.go defines `exitFailure=1`, `exitUsage=2`, `exitAuth=3` (lines 126-130), plus a `codedError` type implementing `ExitCode() int`; `exitCode(err)` walks the error chain via `errors.As` to resolve the process exit code (codebase-discovery.json). The AXI plan must layer onto this contract rather than replace it — Cobra's guidance to decide the exit code "in one place in `main()`" is exactly what atcr already does, so `--axi` output selection must not introduce a second exit-code resolution path.

atcr's `RunE` convention (return errors, no `os.Exit` in handlers) and its use of `MarkFlagsMutuallyExclusive` for declarative flag rules (e.g. `"base"`/`"merge-commit"`) are documented precedents for how a new `--axi` flag should integrate: register it as a typed, persistent flag; validate any mutual-exclusivity or format constraints declaratively where possible; and resolve its effect through the existing `RunE`/error-return/exit-code pipeline.

> Source: [cobra.md: Integration Notes (atcr)]

## Code Examples

The following are the only code examples present in the source document (cobra.md); no examples have been invented.

Command struct definition:

```go
type Command struct {
    Use     string          // one-line usage message, e.g. "review [id]"
    Short   string          // brief help text
    Long    string          // detailed help text
    Example string          // usage examples
    Args    PositionalArgs  // positional-argument validator

    // Run lifecycle hooks, in execution order:
    PersistentPreRunE  func(cmd *Command, args []string) error  // inherited by children
    PreRunE            func(cmd *Command, args []string) error  // local only
    RunE               func(cmd *Command, args []string) error  // main logic (E = returns error)
    PostRunE           func(cmd *Command, args []string) error
    PersistentPostRunE func(cmd *Command, args []string) error

    Version string  // enables --version
    Hidden  bool    // hide from help
}
```

> Source: [cobra.md: Core API > Command struct (central building block)]

Flags (pflag-backed):

```go
cmd.Flags()            // local flags
cmd.PersistentFlags()  // flags inherited by all children
```

> Source: [cobra.md: Core API > Flags (pflag-backed)]

Output control:

```
cmd.OutOrStdout(), cmd.OutOrStderr(), cmd.Printf(), SetOut()/SetErr()/SetIn() — route output through the command for testability instead of writing to os.Stdout directly.
```

> Source: [cobra.md: Core API > Output control]

Common pattern (full root + subcommand wiring, including mutually-exclusive flags and `RunE`):

```go
var rootCmd = &cobra.Command{Use: "atcr", Short: "Agent Team Code Review"}

var reviewCmd = &cobra.Command{
    Use:   "review [id]",
    Short: "Fan a change out to the reviewer pool",
    Args:  cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        return runReview(cmd.Context(), args)
    },
}

func init() {
    reviewCmd.Flags().String("base", "", "base ref")
    reviewCmd.Flags().String("merge-commit", "", "merge commit SHA")
    reviewCmd.MarkFlagsMutuallyExclusive("base", "merge-commit")
    rootCmd.AddCommand(reviewCmd)
}
```

> Source: [cobra.md: Common Patterns]

## Quick Reference

| Mechanism | API | Use for `--axi` |
|---|---|---|
| Persistent flag registration | `cmd.PersistentFlags()` | Declare `--axi` once on root so every findings-emitting subcommand inherits it |
| Local flag registration | `cmd.Flags()` | Not applicable to `--axi` itself (needs cross-command inheritance) |
| Typed flag helpers | `StringVar`, `IntP`, etc. | Bind `--axi` to a bool/string var; `ATCR_AXI_MAX_LINES` parsed separately per the `telemetryEnabledFromEnv` precedent (codebase-discovery.json) |
| Declarative flag constraints | `MarkFlagsMutuallyExclusive`, `MarkFlagRequired`, `MarkFlagsRequiredTogether`, `MarkFlagsOneRequired` | Validated by Cobra before `RunE` runs; use if `--axi` conflicts with another output flag |
| Cross-cutting context injection | `PersistentPreRunE` + `cmd.Context()` | Thread the resolved axi mode value the same way the root logger/telemetry client are injected today (cmd/atcr/main.go:230) |
| Context-threaded timeout | `ExecuteContext(ctx)` | Root context already carries the global timeout into the fan-out engine; axi mode rides the same context |
| Stdout routing (findings) | `cmd.OutOrStdout()` | Write AXI-formatted, token-dense findings here |
| Stderr routing (diagnostics) | `cmd.OutOrStderr()` / `SetErr()` | Diagnostics stay stderr-only, unchanged by AXI mode |
| Positional-argument validation | `cobra.MaximumNArgs(n)` and friends | Unaffected by `--axi`; existing subcommand arg contracts remain |
| Process exit code | Decided once in `main()` from `Execute()`'s returned error | AXI mode must not introduce a second exit-code path; layer onto atcr's existing `exitFailure=1`/`exitUsage=2`/`exitAuth=3` + `codedError`/`ExitCode()`/`errors.As` chain (cmd/atcr/main.go:126-130, codebase-discovery.json) |
| Format-enum dispatch (existing atcr pattern) | `internal/report/render.go`'s `Render(w, findings, format)` switch | AXI is a new format value in this same dispatch, validated via `report.ValidFormat()` (codebase-discovery.json) |

## Related Documentation

- Plan: [../plan.md](../plan.md)
- Source specification: [.planning/specifications/packages/cobra.md](/Users/samestrin/Documents/GitHub/atcr/.planning/specifications/packages/cobra.md)
