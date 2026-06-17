# Task 12: Documentation and Configuration Updates

**Source:** Plan 4.0 – Debt Item #12
**Priority:** P2 | **Effort:** S | **Type:** Add

## Problem Statement
After implementing structured logging, the user-facing documentation and sprint configuration need to reflect the new capabilities. `LOG_LEVEL` is currently documented as "optional" in sprint-config; this ambiguity must be removed. Users need to know how to use `LOG_LEVEL` and `--log-format` to debug failing reviews.

## Solution Overview
1. Update CLI help text (cobra command descriptions) to document `LOG_LEVEL` and `--log-format`.
2. Update or create user-facing documentation in `docs/` to describe:
   - `LOG_LEVEL` environment variable (values: `debug`, `info`, `warn`, `error`; default: `info`)
   - `--log-format` flag (values: `text`, `json`; default: `text`)
   - How to debug a failing review: `LOG_LEVEL=debug atcr review ...`
3. Update `.planning/.config/sprint-config.md` to remove the "optional" qualifier from `LOG_LEVEL`.
4. Create `internal/errors/README.md` documenting the error classification API.

## Technical Implementation
### Steps
1. In `cmd/atcr/main.go`, update the root command's `Long` description to mention `LOG_LEVEL` and `--log-format`.
2. Create or update `docs/logging.md` (or equivalent) with usage instructions.
3. In `.planning/.config/sprint-config.md`, find the `LOG_LEVEL` entry and change "optional/documented" to "implemented".
4. Create `internal/errors/README.md` with a brief API reference:
   - Classification constants and their meanings
   - Constructor functions
   - `IsRetryable` usage
   - Example: wrapping an error and checking retryability

## Files to Create/Modify
- `cmd/atcr/main.go` — modify (update help text)
- `docs/` — create or modify (logging usage docs)
- `.planning/.config/sprint-config.md` — modify (remove "optional" from LOG_LEVEL)
- `internal/errors/README.md` — create (error classification API reference)

## Documentation Links
- [Core Logging Package](../documentation/core-logging-package.md)
- [Error Classification System](../documentation/error-classification-system.md)

## Related Files (from codebase-discovery.json)
- `cmd/atcr/main.go:newRootCmd` — root command help text
- `.planning/.config/sprint-config.md` — LOG_LEVEL status entry

## Success Criteria
- [ ] `atcr --help` mentions `LOG_LEVEL` and `--log-format`
- [ ] `atcr review --help` shows inherited `--log-format` flag
- [ ] User-facing docs describe `LOG_LEVEL` and `--log-format` usage
- [ ] `.planning/.config/sprint-config.md` reflects LOG_LEVEL as implemented
- [ ] `internal/errors/README.md` documents the classification API
- [ ] No broken links in documentation

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Validation:**
- `atcr --help` output includes `LOG_LEVEL` and `--log-format`
- Documentation files exist and are well-formed
- No "optional" qualifier remains for `LOG_LEVEL` in sprint-config

**Test Files:**
- None (documentation only)

## Risk Mitigation
- **Low risk**: Documentation changes do not affect runtime behavior.
- **Help text accuracy**: The help text must match the actual implementation. Verify against Task 06's flag definitions.

## Dependencies
- Task 06 (cli-flags) — flags are defined
- Task 07 (cli-logger-construction) — logger construction is wired
- Task 11 (llmclient-migration) — error classification is in use

## Definition of Done
- [ ] CLI help text updated
- [ ] User-facing docs created/updated
- [ ] sprint-config.md updated
- [ ] internal/errors/README.md created
- [ ] All documentation reviewed for accuracy
