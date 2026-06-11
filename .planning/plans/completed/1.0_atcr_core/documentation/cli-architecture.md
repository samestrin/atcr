# CLI Architecture & Commands <span style="background: #e11d48; color: white; padding: 2px 8px; border-radius: 3px; font-size: 0.9em;">CRITICAL</span>

## Overview

The atcr (Agent Team Code Review) CLI is built using the [Cobra](https://cobra.dev/) framework, providing a modern command-line interface with nested subcommands, flag validation, and context propagation. The application follows a `atcr verb noun --flag` pattern where the root command `atcr` serves as the entry point for six primary subcommands: `review`, `reconcile`, `report`, `range`, `init`, and `serve`.

All subcommands leverage Cobra's `RunE` lifecycle hooks to return errors rather than calling `os.Exit` directly, enabling centralized exit-code logic in `main()`. This design supports CI gate semantics where threshold violations map to distinct process exit codes. The entire CLI tree shares a single root `context.Context` via `ExecuteContext`, ensuring global timeout propagation into the fan-out concurrency engine.

Flag relationships are enforced declaratively using Cobra's built-in validators such as `MarkFlagsMutuallyExclusive`, which prevents conflicting options like `--base` and `--merge-commit` from being specified together. Output routing uses command-scoped writers (`cmd.OutOrStdout()`, `cmd.OutOrStderr()`) for testability instead of direct writes to `os.Stdout`.

> Source: [.planning/specifications/packages/cobra.md:Overview]

## Key Concepts

### Command Structure and Lifecycle

Every atcr subcommand is defined as a `*cobra.Command` struct with typed fields for usage text, argument validators, and lifecycle hooks. Execution follows a strict order: `PersistentPreRunE` (inherited by children), `PreRunE` (local only), `RunE` (main logic), `PostRunE`, and `PersistentPostRunE`. The `RunE` variant is used exclusively so that errors bubble up to the root for unified handling.

> Source: [.planning/specifications/packages/cobra.md:Core API]

### Context Propagation

The root command invokes `ExecuteContext(ctx)` instead of plain `Execute()`, propagating a `context.Context` through the entire command tree. Subcommands retrieve this context via `cmd.Context()` and pass it into downstream operations such as HTTP requests and git invocations. This ensures that global timeouts cancel in-flight operations across all subsystems.

> Source: [.planning/specifications/packages/cobra.md:Integration Notes (atcr)]
> Source: [.planning/specifications/packages/standard-library.md:context + sync — fan-out engine]

### Flag Validation and Relationships

Cobra's pflag-backed flag system provides typed registration via `Flags().StringVar()`, `IntP()`, and similar methods. Declarative relationships enforce business rules before handlers execute:
- `MarkFlagRequired(name)` — mandate presence
- `MarkFlagsMutuallyExclusive(names...)` — prevent co-occurrence
- `MarkFlagsRequiredTogether(names...)` — require joint specification
- `MarkFlagsOneRequired(names...)` — ensure at least one is set

> Source: [.planning/specifications/packages/cobra.md:Command struct (central building block)]

### Exit Code Semantics

Exit codes are determined centrally in `main()` after `Execute()` returns. Handler functions return errors from `RunE`; the root maps these errors and threshold violations (via `--fail-on`) to distinct process exit codes. This avoids scattering `os.Exit` calls throughout handlers and keeps CI gate logic in one place.

> Source: [.planning/specifications/packages/cobra.md:Integration Notes (atcr)]

### Git Interaction via os/exec

Git operations (rev-parse, symbolic-ref, merge-base, rev-list, diff, function-context extraction) shell out via `exec.CommandContext(ctx, "git", "-C", repo, ...)`. Passing argv directly avoids shell injection risks. Stdout and stderr are captured separately; non-zero exits surface trimmed stderr messages. All git commands respect context cancellation to prevent orphaned processes.

> Source: [.planning/specifications/packages/standard-library.md:os/exec — git interaction]

### HTTP Client with Retry Logic

The OpenAI-compatible client uses plain `net/http` POST requests to `${base_url}/chat/completions`. Requests are built with `http.NewRequestWithContext(ctx, ...)` to tie transport lifecycle to context deadlines. A retry policy handles 429/500/502/503/504 responses with ~500ms initial delay and 1.5× backoff. Response bodies are always drained and closed to enable connection reuse.

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

### Persona Prompt Rendering

Persona prompts (`<agent>.md`, `_base.md`, embedded defaults) are rendered using `text/template` (never `html/template`). Templates receive payload vars such as `{{.Payload}}`, `{{.PayloadMode}}`, `{{.FileCount}}`, `{{.BaseRef}}`, `{{.HeadRef}}`, and `{{.AgentName}}`. Parse errors demote to fallback templates rather than aborting runs, preserving the base system's resolution chain behavior.

> Source: [.planning/specifications/packages/standard-library.md:text/template — persona prompt rendering]

### Fan-Out Concurrency Engine

A single `context.WithTimeout` wraps the entire review operation; each agent invocation derives its own per-agent timeout from this parent. The parallel lane spawns goroutines coordinated via `sync.WaitGroup`, which is always drained even on context cancellation to prevent goroutine leaks. The serial lane checks `ctx.Err()` before each invocation, skipping remaining agents once the deadline passes. Results flow into a mutex-guarded slice or buffered channel sized to agent count.

> Source: [.planning/specifications/packages/standard-library.md:context + sync — fan-out engine]

## Code Examples

### Root Command with Subcommands

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

> Source: [.planning/specifications/packages/cobra.md:Common Patterns]

### Context-Aware Git Invocation

```go
exec.CommandContext(ctx, "git", "-C", repo, "diff", base+".."+head)
```

> Source: [.planning/specifications/packages/standard-library.md:os/exec — git interaction]

### HTTP Request with Context and Auth

```go
req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", body)
if err != nil {
    return err
}
req.Header.Set("Authorization", "Bearer "+key)
resp, err := client.Do(req)
if err != nil {
    return err
}
defer resp.Body.Close()
```

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

### Template Rendering with Fallback

```go
tmpl, err := template.New(name).Option("missingkey=error").Parse(src)
if err != nil {
    // Demote to fallback template rather than aborting
    return useDefaultPrompt()
}
```

> Source: [.planning/specifications/packages/standard-library.md:text/template — persona prompt rendering]

### WaitGroup Drain on Cancellation

```go
var wg sync.WaitGroup
for _, agent := range agents {
    wg.Add(1)
    go func(a Agent) {
        defer wg.Done()
        // ... agent logic respects ctx.Done()
    }(agent)
}
wg.Wait() // Always called, even if ctx.Err() != nil
```

> Source: [.planning/specifications/packages/standard-library.md:context + sync — fan-out engine]

## Quick Reference

| Concept | Tool / Package | Purpose |
|---------|---------------|---------|
| CLI framework | `github.com/spf13/cobra` | Subcommand structure, flag validation, help generation |
| Global timeout | `context.WithTimeout` + `ExecuteContext` | Propagate deadline across all subsystems |
| Git ops | `os/exec` with `CommandContext` | Range resolution, diffs, function-context extraction |
| HTTP client | `net/http` + `encoding/json` | OpenAI-compatible chat API calls with retry |
| Prompt rendering | `text/template` | Persona prompt injection with payload vars |
| Concurrency | `sync.WaitGroup` + `context` | Parallel/serial fan-out lanes with timeout enforcement |
| Testing mocks | `testing` + `net/http/httptest` | Provider simulation for fan-out unit tests |
| Argument validation | `cobra.PositionalArgs` | `ExactArgs`, `MinimumNArgs`, `MaximumNArgs`, `RangeArgs` |
| Flag relationships | `MarkFlagsMutuallyExclusive`, etc. | Declarative flag conflict detection |
| Exit codes | Centralized in `main()` | Map `--fail-on` violations to process exit codes |

## Related Documentation

- [Plan Document](../plan.md) — Overall implementation plan for v1.0 atcr_core
- [Cobra Framework Spec](../../../../specifications/packages/cobra.md) — Detailed CLI framework reference
- [Standard Library Spec](../../../../specifications/packages/standard-library.md) — Consolidated stdlib subsystems reference
- [Source Index](source.md) — Auto-generated index of all documentation sources
