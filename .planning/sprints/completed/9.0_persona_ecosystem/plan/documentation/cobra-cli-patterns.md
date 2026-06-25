# Cobra CLI Patterns [CRITICAL]

## Overview

Cobra is a library for creating modern CLI applications with subcommand-based interfaces (`app verb noun --flag`), used by Kubernetes, Hugo, and the GitHub CLI. It provides POSIX-compliant flags via pflag, nested subcommands with global/local/cascading flags, intelligent command suggestions, and automatic help and shell-completion generation.

In the atcr codebase, each top-level subcommand lives in its own file under `cmd/atcr/` (e.g., `serve.go`, `review.go`). Each constructor returns `*cobra.Command` and is registered in `newRootCmd` via `root.AddCommand()`. Parent-subcommand nesting — such as `atcr personas install` — uses `AddCommand` on the parent command, not on root.

For the 9.0 persona ecosystem sprint, the `atcr personas` CLI (T2) introduces a parent command hosting six sub-subcommands: `install`, `remove`, `list`, `search`, `test`, and `upgrade`. The new file `cmd/atcr/personas.go` follows the same file-per-command convention established by `cmd/atcr/serve.go`, and `newPersonasCmd()` is registered in `newRootCmd` at `cmd/atcr/main.go:174-189`.

## Key Concepts

### Command struct

The `Command` struct is the central building block. Key fields include `Use`, `Short`, `Long`, `Example`, `Args`, and lifecycle hooks executed in order: `PersistentPreRunE`, `PreRunE`, `RunE`, `PostRunE`, `PersistentPostRunE`. The `E` suffix variants return an error and are preferred throughout atcr.

> Source: [cobra.md#Command struct (central building block)]

### Execution and hierarchy

`Execute()` / `ExecuteContext(ctx)` runs the root command against `os.Args`. `ExecuteContext` propagates a `context.Context` retrievable via `cmd.Context()` inside handlers. `AddCommand(cmds ...*Command)` attaches subcommands to any command, enabling arbitrarily deep nesting.

> Source: [cobra.md#Execution and hierarchy]

### Flags

Local flags are scoped to the command via `cmd.Flags()`; inherited flags use `cmd.PersistentFlags()`. Declarative flag relationships — `MarkFlagRequired`, `MarkFlagsMutuallyExclusive`, `MarkFlagsRequiredTogether`, `MarkFlagsOneRequired` — are validated by cobra before `RunE` fires.

> Source: [cobra.md#Flags (pflag-backed)]

### Subcommand file convention

Each top-level atcr subcommand lives in its own file under `cmd/atcr/`. The constructor returns `*cobra.Command` and is registered in `newRootCmd` (`cmd/atcr/main.go:128`, `AddCommand` at lines 174-189). Parent-subcommand nesting (`atcr personas install`) uses `AddCommand` on the parent command, not root.

> Source: [codebase-discovery.json#Cobra Subcommand File]

### Root command location

There is no `root.go` in `cmd/atcr/`. The root command lives in `main.go:newRootCmd` (line 128). New subcommands go in their own files (e.g., `cmd/atcr/personas.go`).

> Source: [codebase-discovery.json#Architecture note]

### Existing subcommand count

Currently 14 subcommands are registered in `newRootCmd`: `review`, `reconcile`, `verify`, `debate`, `report`, `github`, `range`, `status`, `init`, `serve`, `doctor`, `trust`, `scorecard`, `leaderboard`. `TestRootCmd_HasExactlyFourteenSubcommands` will fail when `personas` is added — update the test to 15.

> Source: [codebase-discovery.json#Architecture note]

### Output control

Use `cmd.OutOrStdout()`, `cmd.OutOrStderr()`, and `cmd.Printf()` rather than writing to `os.Stdout` directly. `SetOut()`/`SetErr()`/`SetIn()` route output through the command for testability.

> Source: [cobra.md#Output control]

### Exit-code semantics

cobra returns the error from `Execute()`; decide the process exit code in one place in `main()`. Map `--fail-on` threshold violations to a distinct exit code rather than calling `os.Exit` inside handlers.

> Source: [cobra.md#Integration Notes (atcr)]

## Code Examples

### Root and subcommand registration pattern

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

> Source: [cobra.md#Common Patterns]

## Quick Reference

| Concern | API / Convention |
|---|---|
| Root command location | `cmd/atcr/main.go:newRootCmd` (line 128) |
| Register subcommand | `root.AddCommand(newPersonasCmd())` at lines 174-189 |
| Nest sub-subcommands | `personasCmd.AddCommand(newInstallCmd(), ...)` |
| File convention | One file per top-level subcommand: `cmd/atcr/personas.go` |
| Test file convention | `cmd/atcr/personas_test.go` (based on `debate_test.go`) |
| Main logic hook | `RunE` (returns error; never call `os.Exit` in handler) |
| Context propagation | `ExecuteContext(ctx)` → `cmd.Context()` inside handler |
| Output (testable) | `cmd.OutOrStdout()` / `cmd.OutOrStderr()` |
| Mutual exclusion | `cmd.MarkFlagsMutuallyExclusive(names...)` |
| Required flag | `cmd.MarkFlagRequired(name)` |
| Positional args | `cobra.ExactArgs(n)`, `cobra.MinimumNArgs(n)`, etc. |
| Subcommand count | 14 → 15 after adding `personas`; update test assertion |

## Related Documentation

- [pkg.go.dev/github.com/spf13/cobra](https://pkg.go.dev/github.com/spf13/cobra) — full API reference
- [cobra.dev](https://cobra.dev/) — official documentation and guides
- [github.com/spf13/cobra](https://github.com/spf13/cobra) — source and changelog
- `cmd/atcr/main.go` — root command and `AddCommand` registration (lines 128, 174-189)
- `cmd/atcr/serve.go` — minimal subcommand scaffold to follow for `personas.go`
- `cmd/atcr/debate_test.go` — test pattern to follow for `personas_test.go`
