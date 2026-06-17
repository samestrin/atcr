# Task 07: Root Logger Construction and Context Storage

**Source:** Plan 4.0 – Debt Item #7
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
The flags from Task 06 need to be consumed: a logger must be constructed from the `LOG_LEVEL` and `--log-format` values, then stored in the command context so all subcommands and downstream packages can retrieve it via `log.FromContext`.

## Solution Overview
Add a `PersistentPreRunE` to the root command in `cmd/atcr/main.go` that:
1. Reads the `LOG_LEVEL` env var and `--log-format` flag.
2. Constructs a `*slog.Logger` via `log.New(level, format, os.Stderr)`.
3. Stores the logger in the command context via `log.NewContext(cmd.Context(), logger)`.
4. Uses `cobra.ExecuteContext` so the context (with the logger) propagates to all handlers.

This is the single point of logger construction. No subcommand constructs its own logger.

## Technical Implementation
### Steps
1. In `cmd/atcr/main.go:newRootCmd`, add a `PersistentPreRunE` function that:
   - Reads `LOG_LEVEL` from env (default `"info"`).
   - Reads `--log-format` from the persistent flag (default `"text"`).
   - Calls `log.New(level, format, os.Stderr)`.
   - On error (invalid level/format), returns a descriptive error.
   - Stores the logger via `log.NewContext(cmd.Context(), logger)` and sets it back on the command.
2. In `main()`, use `rootCmd.ExecuteContext(context.Background())` (or equivalent) so the root context is initialized.
3. In each subcommand, retrieve the logger via `log.FromContext(cmd.Context())`. In `cmd/atcr/review.go`, this retrieval is the baseline for Task 14, which then calls `log.WithReviewID` after `PrepareReview` resolves the review ID.

## Files to Create/Modify
- `cmd/atcr/main.go` — modify (add `PersistentPreRunE`, context storage)
- `cmd/atcr/review.go` — modify (retrieve logger from context)
- `cmd/atcr/serve.go` — modify (retrieve logger from context)

## Documentation Links
- [CLI and MCP Integration](../documentation/cli-mcp-integration.md)

## Related Files (from codebase-discovery.json)
- `cmd/atcr/main.go:newRootCmd` — root command where `PersistentPreRunE` is added
- `cmd/atcr/serve.go:newServeCmd` — currently builds a local logger; must use context logger
- `cmd/atcr/review.go:runReview` — must retrieve logger from context

## Success Criteria
- [ ] `PersistentPreRunE` constructs the root logger using `log.New`
- [ ] Logger is stored in command context via `log.NewContext`
- [ ] Subcommands retrieve the logger via `log.FromContext(cmd.Context())`
- [ ] `cmd/atcr/serve.go` no longer constructs a local `slog.Logger`
- [ ] Invalid `LOG_LEVEL` produces an error before any subcommand runs
- [ ] Invalid `--log-format` produces an error before any subcommand runs
- [ ] All existing tests continue to pass

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestPersistentPreRunE_ValidLevelAndFormat` — construct root cmd with `LOG_LEVEL=debug`, `--log-format=json`, assert `PersistentPreRunE` succeeds and context contains a logger
- `TestPersistentPreRunE_InvalidLevel` — set `LOG_LEVEL=bogus`, assert `PersistentPreRunE` returns an error
- `TestPersistentPreRunE_InvalidFormat` — set `--log-format=xml`, assert error
- `TestServeCmd_UsesContextLogger` — run `serve` subcommand, assert it uses the logger from context (not a local one)

**Test Files:**
- `cmd/atcr/main_test.go` (modify)

## Risk Mitigation
- **Single construction point**: Only `PersistentPreRunE` calls `log.New`. Subcommands only retrieve. This eliminates the scattered logger construction problem.
- **Context propagation**: Using `cmd.Context()` / `log.NewContext` ensures the logger flows through cobra's execution model without global state.
- **Stderr as default sink**: All diagnostic output goes to `os.Stderr`, which is correct for both CLI and MCP modes (MCP's stdio transport owns stdout).

## Dependencies
- Task 01 (core-logging-api) — `log.New`, `log.NewContext`, `log.FromContext`
- Task 04 (logging-package validation) — `internal/log` package is stable
- Task 06 (cli-flags) — `LOG_LEVEL` and `--log-format` are declared

## Definition of Done
- [ ] `PersistentPreRunE` constructs and stores the root logger
- [ ] `cmd/atcr/serve.go` uses context logger instead of local construction
- [ ] `go build ./cmd/atcr/...` succeeds
- [ ] `go test ./cmd/atcr/...` passes
- [ ] All existing tests continue to pass
