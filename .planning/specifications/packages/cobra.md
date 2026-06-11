# github.com/spf13/cobra

**Version:** v1.10.2 (December 3, 2025)
**Registry:** [pkg.go.dev/github.com/spf13/cobra](https://pkg.go.dev/github.com/spf13/cobra)
**Official Docs:** [cobra.dev](https://cobra.dev/) · [GitHub](https://github.com/spf13/cobra)
**Tier:** Critical
**Last Updated:** June 10, 2026

---

## Overview

Cobra is a library for creating modern CLI applications with subcommand-based interfaces (`app verb noun --flag`), used by Kubernetes, Hugo, and the GitHub CLI. It provides POSIX-compliant flags via pflag, nested subcommands with global/local/cascading flags, intelligent command suggestions, and automatic help and shell-completion generation. License: Apache-2.0.

## Installation

```bash
go get -u github.com/spf13/cobra@latest
```

## Core API

### Command struct (central building block)

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

### Execution and hierarchy

- `Execute()` / `ExecuteContext(ctx)` — run the root command against `os.Args`. `ExecuteContext` propagates a `context.Context` retrievable via `cmd.Context()` inside handlers.
- `AddCommand(cmds ...*Command)` — attach subcommands to the root.
- `Root()`, `Parent()`, `Find(args)` — navigate the command tree.

### Flags (pflag-backed)

```go
cmd.Flags()            // local flags
cmd.PersistentFlags()  // flags inherited by all children
```

Typed registration: `Flags().StringVar(&v, "base", "", "base ref")`, `IntP("timeout", "t", 900, "...")` (P-suffix adds a short flag).

Declarative flag relationships (validated by cobra before RunE):

- `MarkFlagRequired(name)`
- `MarkFlagsMutuallyExclusive(names...)`
- `MarkFlagsRequiredTogether(names...)`
- `MarkFlagsOneRequired(names...)`

### Positional-argument validators

`ExactArgs(n)`, `MinimumNArgs(n)`, `MaximumNArgs(n)`, `RangeArgs(min, max)`, `NoArgs`, `ArbitraryArgs`, `MatchAll(validators...)`.

### Output control

`cmd.OutOrStdout()`, `cmd.OutOrStderr()`, `cmd.Printf()`, `SetOut()/SetErr()/SetIn()` — route output through the command for testability instead of writing to `os.Stdout` directly.

## Common Patterns

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

## Integration Notes (atcr)

- One root `atcr` command with subcommands `review`, `reconcile`, `report`, `range`, `init`, `serve` (Epic 1.0 task 1).
- Use `RunE` variants everywhere and return errors; map `--fail-on` threshold violations to a distinct exit code in `main()` (CI gate semantics) rather than calling `os.Exit` inside handlers.
- Use `ExecuteContext` so one root context carries the global timeout into the fan-out engine.
- `MarkFlagsMutuallyExclusive("base", "merge-commit")` and friends express the range-resolution flag rules declaratively.
- Exit-code semantics: cobra returns the error from `Execute()`; decide the process exit code in one place in `main()`.

---
**Source:** Extracted from pkg.go.dev on June 10, 2026.
