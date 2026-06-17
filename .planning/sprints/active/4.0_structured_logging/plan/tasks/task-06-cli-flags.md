# Task 06: CLI Flags for LOG_LEVEL and --log-format

**Source:** Plan 4.0 – Debt Item #6
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
ATCR has no runtime level control for diagnostic output. `LOG_LEVEL` is documented but not implemented. Operators cannot enable debug output for a failing review without recompiling. The `--log-format` flag does not exist, so structured JSON output (needed for CI parsing) is unavailable.

## Solution Overview
Add two persistent flags to the root command in `cmd/atcr/main.go:newRootCmd`:
- `LOG_LEVEL` — read from the `LOG_LEVEL` environment variable (default: `info`). Supported values: `debug`, `info`, `warn`, `error`.
- `--log-format` — CLI flag (default: `text`). Supported values: `text`, `json`.

These flags are declared on the root command so they are inherited by all subcommands. The actual logger construction happens in Task 07 (`PersistentPreRunE`).

## Technical Implementation
### Steps
1. In `cmd/atcr/main.go:newRootCmd`, add a `--log-format` persistent string flag with default value `"text"`. Bind it to a package-level or struct field variable.
2. In the same function, read the `LOG_LEVEL` environment variable using `os.Getenv("LOG_LEVEL")`. Default to `"info"` when empty.
3. Validate both values early (in `PersistentPreRunE`, Task 07) using `log.LevelFromString` and a format validation check. Invalid values produce a clear error message and non-zero exit.

## Files to Create/Modify
- `cmd/atcr/main.go` — modify (add persistent flag and env var reading in `newRootCmd`)

## Documentation Links
- [CLI and MCP Integration](../documentation/cli-mcp-integration.md)
- [Core Logging Package](../documentation/core-logging-package.md)

## Related Files (from codebase-discovery.json)
- `cmd/atcr/main.go:newRootCmd` — root command definition (no `root.go` exists)
- `cmd/atcr/main.go:PersistentPreRunE` — logger construction site (Task 07)

## Success Criteria
- [ ] `LOG_LEVEL` environment variable is read (default: `info`)
- [ ] `--log-format` persistent flag is declared (default: `text`)
- [ ] Both values are available to `PersistentPreRunE` for logger construction
- [ ] Invalid `LOG_LEVEL` produces a clear error in `PersistentPreRunE` (Task 07)
- [ ] Invalid `--log-format` produces a clear error in `PersistentPreRunE` (Task 07)
- [ ] All subcommands inherit the flags (persistent, not local)

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestRootCmd_LogFormatDefault` — assert `--log-format` defaults to `"text"`
- `TestRootCmd_LogLevelFromEnv` — set `LOG_LEVEL=debug`, run root command, assert level is `debug`
- `TestRootCmd_LogLevelEnvEmptyDefaultsToInfo` — unset `LOG_LEVEL`, assert level defaults to `"info"`

**Test Files:**
- `cmd/atcr/main_test.go` (create or modify)

## Risk Mitigation
- **No runtime behavior change yet**: This task only declares flags and reads env vars. Logger construction happens in Task 07, so no existing behavior changes.
- **Persistent flags**: Using persistent (not local) flags ensures all subcommands inherit `--log-format`.
- **Environment variable precedence**: `LOG_LEVEL` env var is the documented interface. The CLI flag `--log-format` complements it; no `--log-level` flag is added to avoid dual interfaces for the same setting.

## Dependencies
- Task 01 (core-logging-api) — provides `log.LevelFromString` for validation
- Task 04 (logging-package validation) — confirms `internal/log` is stable

## Definition of Done
- [ ] `--log-format` persistent flag declared on root command
- [ ] `LOG_LEVEL` env var read with `info` default
- [ ] Values available for `PersistentPreRunE` consumption
- [ ] `go build ./cmd/atcr/...` succeeds
- [ ] Existing tests continue to pass
