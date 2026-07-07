# CLI Flag Wiring for Model-Aware Search (Cobra) [CRITICAL]

## Overview

The `atcr personas search` subcommand needs a way to filter results by the model or provider a user already has, so the search flow can surface community personas whose structured `provider`/`model` metadata matches what the user runs locally. Cobra is the library atcr already builds its CLI on — "a library for creating modern CLI applications with subcommand-based interfaces (`app verb noun --flag`)" — and it exposes flags through a pflag-backed API attached to each `*cobra.Command` (> Source: [cobra.md]). Adding `--model` and `--provider` to the search subcommand is a matter of registering two more typed flags on that command's `Flags()` set, the same mechanism already used elsewhere in the CLI.

In atcr's existing codebase, `cmd/atcr/personas.go`'s `newPersonasCmd` registers six subcommands — install/list/search/remove/test/upgrade — each a thin `RunE` that delegates to a matching function in `internal/personas` (Install, List, Search, Remove, TestPersona, Upgrade), using package-level `personasClient`/`personasDir`/`personasFixtureRunner` vars for test injection. The search subcommand specifically is built by `newPersonasSearchCmd`, and its output is rendered by `renderPersonaSearch`, which currently formats `index.json` search hits as a 3-column table (Name/Version/Description). This is the integration point for the plan's model-aware search goal: `--model`/`--provider` string flags get added to `newPersonasSearchCmd` following the same typed flag-registration pattern already used on `newPersonasListCmd()` (which uses `cmd.Flags().Bool` for `--scores`), and `renderPersonaSearch` gains Provider/Model columns to display the new structured metadata once a hit matches the filter.

Cobra's design separates flag *declaration* (on `cmd.Flags()`) from flag *validation* (via declarative helpers like `MarkFlagsMutuallyExclusive`) from flag *consumption* (inside `RunE`), which maps cleanly onto how `newPersonasSearchCmd`/`renderPersonaSearch` are already structured in this repo — flags declared at command-construction time, read inside the `RunE` closure, and passed down into the `internal/personas.Search` call and its rendering function (> Source: [cobra.md]).

## Key Concepts

- **Command struct is the central building block.** Every cobra command is a `*Command` value with `Use`, `Short`, `Long`, `Args`, and a `RunE` lifecycle hook that does the main logic and returns an error rather than exiting directly. `newPersonasSearchCmd` is one such `*Command` in atcr's tree. > Source: [cobra.md]
- **Flags are pflag-backed and typed.** `cmd.Flags()` returns the local flag set for a command; `cmd.PersistentFlags()` returns flags inherited by children. Typed registration looks like `Flags().StringVar(&v, "base", "", "base ref")` or `IntP("timeout", "t", 900, "...")` (the `P`-suffix form also registers a short flag). This is the same API surface `newPersonasSearchCmd` would use to add `--model`/`--provider` as `String` flags, following the same registration pattern already used for `--scores` on `newPersonasListCmd()`. > Source: [cobra.md]
- **Declarative flag relationships are validated by cobra before `RunE` runs.** Helpers include `MarkFlagRequired(name)`, `MarkFlagsMutuallyExclusive(names...)`, `MarkFlagsRequiredTogether(names...)`, and `MarkFlagsOneRequired(names...)`. These are relevant if `--model` and `--provider` need any relationship (e.g., requiring at least one, or preventing conflicting combinations) enforced without hand-written checks inside `RunE`. > Source: [cobra.md]
- **`RunE` returns errors instead of calling `os.Exit`.** atcr's integration notes specify using `RunE` variants everywhere and returning errors, with exit-code decisions centralized in `main()` — consistent with how `personas.go`'s six subcommands already delegate to `internal/personas` functions and propagate their errors. > Source: [cobra.md]
- **Output routes through the command, not directly to stdout.** `cmd.OutOrStdout()`, `cmd.OutOrStderr()`, `cmd.Printf()`, and `SetOut()/SetErr()/SetIn()` let output be redirected for testability — relevant to how `renderPersonaSearch` should emit its (soon to be 5-column) table. > Source: [cobra.md]

## Code Examples

The following code appears verbatim in the source documentation:

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

> Source: [cobra.md]

**Note:** This plan's actual flags (`--model` and `--provider` on `atcr personas search`) are NOT shown anywhere in the source above and must not be invented wholesale. They should follow the same typed-registration pattern shown above — e.g. `cmd.Flags().String("model", "", "...")` / `cmd.Flags().String("provider", "", "...")` declared inside the `newPersonasSearchCmd` constructor, read inside its `RunE`, and threaded into `internal/personas.Search` and `renderPersonaSearch` — rather than a pattern pulled from outside this source.

## Quick Reference

| Item | Detail | Source |
|------|--------|--------|
| Flag declaration | `cmd.Flags().StringVar(&v, name, default, usage)` or `IntP(name, shorthand, default, usage)` | [cobra.md] |
| Flag validation helpers | `MarkFlagRequired`, `MarkFlagsMutuallyExclusive`, `MarkFlagsRequiredTogether`, `MarkFlagsOneRequired` | [cobra.md] |
| Main logic hook | `RunE func(cmd *Command, args []string) error` — returns error, does not call `os.Exit` | [cobra.md] |
| Output routing | `cmd.OutOrStdout()`, `cmd.Printf()`, `SetOut()/SetErr()/SetIn()` | [cobra.md] |
| atcr integration point (existing) | `cmd/atcr/personas.go`: `newPersonasCmd` registers install/list/search/remove/test/upgrade via `RunE` calling `internal/personas` functions | codebase pattern |
| atcr integration point (this plan) | `newPersonasSearchCmd` / `renderPersonaSearch` — add `--model`/`--provider` flags and Provider/Model table columns | codebase pattern |

## Related Documentation

- [../../../../specifications/packages/cobra.md](../../../../specifications/packages/cobra.md)
