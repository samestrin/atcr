# Task 09: Remove Payload slog.Default() Fallback

**Source:** Plan 4.0 – Debt Item #9
**Priority:** P1 | **Effort:** S | **Type:** Refactor

## Problem Statement
`internal/payload/diff.go` falls back to `slog.Default()` when no logger is injected into `gitRunner`. This uses global state that cannot be captured in tests and bypasses the shared logger configured in the CLI layer. It is explicitly called out in the plan as a pattern to reject.

## Solution Overview
1. Remove the `slog.Default()` fallback in `internal/payload/diff.go:log` method.
2. Replace with a nil-safe discard logger fallback (matching the `internal/mcp/handlers.go` pattern).
3. Update `internal/payload/builder.go:BuildEntries` and `ChangedFileCount` to inject the context logger into `gitRunner`.
4. Update all `gitRunner` tests to inject a discard logger where they currently omit the logger field.

## Technical Implementation
### Steps
1. In `internal/payload/diff.go`, modify the `log()` method on `gitRunner`:
   - Replace `slog.Default()` with `slog.New(slog.NewTextHandler(io.Discard, nil))` when `r.log` is nil.
2. In `internal/payload/builder.go:BuildEntries`:
   - Retrieve the logger from the context parameter: `logger := log.FromContext(ctx)`.
   - Pass it to the `gitRunner` constructor.
3. In `internal/payload/builder.go:ChangedFileCount`:
   - Same injection as `BuildEntries`.
4. Update test files that construct `gitRunner{}` without a logger field:
   - `internal/payload/diff_test.go`
   - `internal/payload/builder_test.go`
   - `internal/payload/pipeline_test.go`
   - Add a discard logger to each test construction.

## Files to Create/Modify
- `internal/payload/diff.go` — modify (remove `slog.Default()` fallback, replace with discard)
- `internal/payload/builder.go` — modify (inject context logger into `gitRunner`)
- `internal/payload/diff_test.go` — modify (inject discard logger in tests)
- `internal/payload/builder_test.go` — modify (inject discard logger in tests)
- `internal/payload/pipeline_test.go` — modify (inject discard logger in tests)

## Documentation Links
- [Core Logging Package](../documentation/core-logging-package.md)
- [Testing Patterns](../documentation/testing-patterns.md)

## Related Files (from codebase-discovery.json)
- `internal/payload/diff.go:log` (line 196) — falls back to `slog.Default()`
- `internal/payload/builder.go:BuildEntries` (line 83) — creates `gitRunner` without logger
- `internal/payload/builder.go:ChangedFileCount` — same issue
- `internal/mcp/handlers.go:logger` (line 63) — nil-safe pattern to replicate

## Success Criteria
- [ ] `slog.Default()` no longer appears in `internal/payload/diff.go`
- [ ] `gitRunner.log()` returns a discard logger when no logger is injected (no panic)
- [ ] `BuildEntries` injects the context logger into `gitRunner`
- [ ] `ChangedFileCount` injects the context logger into `gitRunner`
- [ ] All `gitRunner` tests inject a discard logger and pass
- [ ] `go test ./internal/payload/...` passes
- [ ] `go test ./...` passes (no regressions)

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- Existing payload tests are updated to inject a discard logger; they verify the code still works with an explicit logger.
- `TestGitRunner_NilLogger_NoPanic` — construct `gitRunner` with nil logger, call `log()`, verify discard logger is returned (no panic)

**Test Files:**
- `internal/payload/diff_test.go` (modify)
- `internal/payload/builder_test.go` (modify)
- `internal/payload/pipeline_test.go` (modify)

## Risk Mitigation
- **Test breakage**: Every test that constructs `gitRunner{}` without a logger must be updated. A search for `gitRunner{` across the test files identifies all sites.
- **Behavioral change**: The discard logger fallback matches the existing `internal/mcp` pattern. When a logger IS injected (production path), behavior is unchanged — the injected logger is used.
- **No new dependencies**: `internal/payload` imports `internal/log` for `FromContext`, which is stdlib-only.

## Dependencies
- Task 01 (core-logging-api) — `log.FromContext`
- Task 04 (logging-package validation) — `internal/log` is stable
- Task 07 (cli-logger-construction) — context logger is available in subcommands

## Definition of Done
- [ ] `slog.Default()` removed from `internal/payload/diff.go`
- [ ] Context logger injected in `BuildEntries` and `ChangedFileCount`
- [ ] All payload tests updated and passing
- [ ] `go test ./internal/payload/...` passes
- [ ] `go vet ./internal/payload/...` clean
- [ ] No `slog.Default()` remains in production code (`grep -r 'slog.Default()' internal/`)
