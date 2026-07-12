# CLI Dispatcher Conventions

**Priority:** `[CRITICAL]`

## Overview

atcr's CLI surface is built with `github.com/spf13/cobra`, which structures the tool as a subcommand-based interface (`atcr verb noun --flag`): one root `atcr` command with nested subcommands, each declared as a `cobra.Command` with its own `Use` string, argument validators, flags, and `RunE` handler. This category covers the conventions that govern how that command tree is built and registered, because the standalone skill dispatcher being written in this plan (`skill/SKILL.md`) is a `/atcr <command>` UX that must route to those exact subcommands rather than reinvent its own command vocabulary.

For this plan, the stakes are direct: subcommands are registered centrally in `newRootCmd` (`cmd/atcr/main.go:185-208`) with exact `Use` strings, and that registration is the canonical, single source of truth for atcr's command surface. Any drift between the dispatcher's documented commands and the actual Cobra command tree produces a skill that tells users to run commands that don't exist, or that omits commands that do. The dispatcher rewrite must therefore treat the Cobra command inventory as ground truth to mirror, not as inspiration for new aliases.

> Source: [codebase-discovery.json:existing_patterns] — "Subcommands are registered centrally in newRootCmd (cmd/atcr/main.go:185-208) with exact Use strings and cobra.Command structs. This is the canonical command surface the dispatcher must mirror."

## Key Concepts

### The Command struct and its Run lifecycle

Every Cobra command is a `Command` struct carrying its usage metadata (`Use`, `Short`, `Long`, `Example`, `Args`) plus a chain of lifecycle hooks executed in order: `PersistentPreRunE` (inherited by child commands), `PreRunE` (local only), `RunE` (the command's main logic, returning an error rather than calling `os.Exit`), `PostRunE`, and `PersistentPostRunE`. The `E`-suffixed variants are the ones atcr should use throughout, since they let errors propagate up to a single exit-code decision point in `main()` instead of each handler deciding process exit behavior independently.

> Source: [cobra.md:Core API > Command struct (central building block)]
> Source: [cobra.md:Integration Notes (atcr)] — "Use `RunE` variants everywhere and return errors; map `--fail-on` threshold violations to a distinct exit code in `main()` (CI gate semantics) rather than calling `os.Exit` inside handlers."

### Execution and command-tree navigation

The root command is run via `Execute()` or `ExecuteContext(ctx)` against `os.Args`; `ExecuteContext` propagates a `context.Context` retrievable inside handlers via `cmd.Context()`. Subcommands attach to a parent with `AddCommand(cmds ...*Command)`, and the tree can be inspected at runtime with `Root()`, `Parent()`, and `Find(args)`. atcr uses `ExecuteContext` specifically so a single root context can carry the global timeout into the fan-out review engine.

> Source: [cobra.md:Core API > Execution and hierarchy]
> Source: [cobra.md:Integration Notes (atcr)] — "Use `ExecuteContext` so one root context carries the global timeout into the fan-out engine."

### Flag registration and declarative flag relationships

Flags are pflag-backed and registered as either local (`cmd.Flags()`) or persistent/inherited (`cmd.PersistentFlags()`), using typed setters such as `StringVar(&v, "base", "", "base ref")` or `IntP("timeout", "t", 900, "...")` (the `P` suffix adds a short flag alias). Cobra validates declarative flag relationships before `RunE` ever runs: `MarkFlagRequired(name)`, `MarkFlagsMutuallyExclusive(names...)`, `MarkFlagsRequiredTogether(names...)`, and `MarkFlagsOneRequired(names...)`. atcr uses `MarkFlagsMutuallyExclusive("base", "merge-commit")` (and similar) to express range-resolution flag rules declaratively rather than with manual `if` checks inside handlers.

> Source: [cobra.md:Core API > Flags (pflag-backed)]
> Source: [cobra.md:Integration Notes (atcr)] — "`MarkFlagsMutuallyExclusive(\"base\", \"merge-commit\")` and friends express the range-resolution flag rules declaratively."

### Positional-argument validators and output routing

Argument counts are validated declaratively via `Args`, using validators like `ExactArgs(n)`, `MinimumNArgs(n)`, `MaximumNArgs(n)`, `RangeArgs(min, max)`, `NoArgs`, `ArbitraryArgs`, or a composite via `MatchAll(...)`. Output should be routed through the command (`cmd.OutOrStdout()`, `cmd.OutOrStderr()`, `cmd.Printf()`, `SetOut()/SetErr()/SetIn()`) rather than written directly to `os.Stdout`, which keeps commands testable.

> Source: [cobra.md:Core API > Positional-argument validators] and [cobra.md:Core API > Output control]

### The atcr-specific command inventory the dispatcher must mirror

atcr's Cobra root command registers its subcommands centrally in `newRootCmd` at `cmd/atcr/main.go:185-208`. That registration is the canonical, single source of truth for the command surface; the `/atcr` dispatcher in `skill/SKILL.md` must route to the exact command names registered there and must not invent aliases that drift from the CLI.

The current root command tree (`cmd/atcr/main.go:185-208`) registers the following top-level commands:

- `review` — Fan a code change out to the reviewer pool.
- `reconcile [id-or-path]` — Merge findings from all sources into reconciled artifacts.
- `verify [id-or-path]` — Run adversarial skeptics over reconciled findings.
- `debate [id-or-path]` — Cross-examine disputed findings (proposer/challenger/judge).
- `report [id-or-path]` — Render md, json, or checklist views over reconciled findings.
- `github [id-or-path]` — Post reconciled findings to a GitHub pull request as a check run.
- `range` — Resolve the review range and print resolution JSON.
- `status [id-or-path]` — Print a review's fan-out progress as JSON (backs the `atcr_status` MCP tool).
- `init` — Write `.atcr/config.yaml` and editable persona files.
- `quickstart` — Interactive onboarding: scaffold config, provider, and a CI workflow.
- `serve` — Run the MCP stdio server over the review engine.
- `doctor` — Self-test every configured model endpoint.
- `trust [provider...]` — Authorize project-defined providers from `.atcr/registry.yaml`.
- `scorecard [id-or-path]` — Display the per-reviewer scorecard for a single reconcile run.
- `leaderboard` — Aggregate scorecard records across runs, ranked by corroboration rate.
- `benchmark` — Parent for benchmark-suite commands (`verify`, `run`, `export`).
- `personas` — Parent for community persona lifecycle commands (`install`, `list`, `search`, `remove`, `test`, `submit`, `upgrade`).
- `models` — Parent for model binding commands (`check`, `refresh`).
- `debt` — Parent for technical-debt commands (`list`, `add`, `dashboard`).
- `history` — Show finding history over time as a markdown table.
- `audit-report` — Render a one-page compliance report for a PR's review runs.
- `version` — Print the atcr version.

> Source: [codebase-discovery.json:existing_patterns] — "Subcommands are registered centrally in newRootCmd (cmd/atcr/main.go:185-208) with exact Use strings and cobra.Command structs. This is the canonical command surface the dispatcher must mirror."
> Source: `cmd/atcr/main.go:185-208` — authoritative command registration site.

Note: `cobra.md`'s "Integration Notes (atcr)" section references an older command inventory (including a non-existent `anchor` subcommand and omitting several newer commands such as `verify`, `github`, `benchmark`, `personas`, etc.). The dispatcher must treat the live `cmd/atcr/main.go:185-208` registration as ground truth, not the spec snapshot.

## Code Examples

The following code appears verbatim in the source document (`cobra.md`, "Common Patterns" section):

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

> Source: [cobra.md:Common Patterns]

The `Command` struct definition, also verbatim from the source:

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

> Source: [cobra.md:Core API > Command struct (central building block)]

## Quick Reference

| Subcommand | Purpose |
|---|---|
| `review` | Fan a code change out to the reviewer pool |
| `reconcile [id-or-path]` | Merge findings from all sources into reconciled artifacts |
| `verify [id-or-path]` | Run adversarial skeptics over reconciled findings |
| `debate [id-or-path]` | Cross-examine disputed findings (proposer/challenger/judge) |
| `report [id-or-path]` | Render md, json, or checklist views over reconciled findings |
| `github [id-or-path]` | Post reconciled findings to a GitHub pull request as a check run |
| `range` | Resolve the review range and print resolution JSON |
| `status [id-or-path]` | Print a review's fan-out progress as JSON (backs the `atcr_status` MCP tool) |
| `init` | Write `.atcr/config.yaml` and editable persona files |
| `quickstart` | Interactive onboarding: scaffold config, provider, and a CI workflow |
| `serve` | Run the MCP stdio server over the review engine |
| `doctor` | Self-test every configured model endpoint |
| `trust [provider...]` | Authorize project-defined providers from `.atcr/registry.yaml` |
| `scorecard [id-or-path]` | Display the per-reviewer scorecard for a single reconcile run |
| `leaderboard` | Aggregate scorecard records across runs, ranked by corroboration rate |
| `benchmark` | Benchmark-suite tooling: `verify`, `run`, `export` |
| `personas` | Manage community reviewer personas: `install`, `list`, `search`, `remove`, `test`, `submit`, `upgrade` |
| `models` | Inspect model bindings: `check`, `refresh` |
| `debt` | Query and report on technical debt: `list`, `add`, `dashboard` |
| `history` | Show finding history over time as a markdown table |
| `audit-report` | Render a one-page compliance report for a PR's review runs |
| `version` | Print the atcr version |

> Source: `cmd/atcr/main.go:185-208` and per-command `Use`/`Short` strings in `cmd/atcr/*.go`

## Related Documentation

- Source specification: [`../../../../specifications/packages/cobra.md`](../../../../specifications/packages/cobra.md)
- Canonical command registration site: `cmd/atcr/main.go:185-208` (`newRootCmd`) — the authoritative list of `Use` strings and `cobra.Command` structs that the `/atcr` dispatcher in `skill/SKILL.md` must mirror exactly.
