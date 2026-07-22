# Task 04: Add `--allow-config-edits` Flag and Document Security Architecture

**Source:** Plan 32.4 – Debt Item #4
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
T2 wires `internal/security.IsProtectedPath` into `internal/autofix/apply.go`'s `applyOne` as a fail-closed gate: any `--auto-fix` patch touching `.git/`, `.githooks/`, `.github/workflows/`, `.gitlab-ci.yml`/CI definitions, `.vscode/`, `.idea/`, `.env*`, `.planning/`, or `.atcr` is refused by default (Indirect Sandbox Escape / Host Trust Transposition prevention). Some operators legitimately need `--auto-fix` to touch these paths — e.g. refactoring a CI workflow or editor config as part of an intentional change — and today there is no supported way to do that; the gate has no escape valve.

Separately, `docs/README.md` states it is "the single source of truth the website build consumes," and links every doc in `docs/`. The new `docs/security.md` (documenting pathguard, `internal/gitexec` hardening, and this flag) is worthless to a reader if it is not indexed there — it would exist on disk but stay invisible to the docs build and to anyone browsing the docs from the README entry point.

## Solution Overview
Add a `--allow-config-edits` bool flag to `cmd/atcr/autofix.go`'s `addAutoFixFlags`, following the exact `--no-sandbox` precedent already in that function (`cmd/atcr/autofix.go:55-60`): off by default, requires an explicit `true` to bypass the pathguard gate, documents the security implication in the flag's own help text, and fires an unconditional (non-memoized) stderr warning on every use — mirroring `warnNoSandbox` (`cmd/atcr/autofix.go:82-86`). The resolved flag value threads into `validateAutoFixBackend`/`autoFixBackend` and ultimately into T2's `ApplyPatch`/`applyOne` `AllowConfigEdits` gate.

Write `docs/security.md` documenting the workspace-integrity security architecture this epic builds (pathguard blocklist, `internal/gitexec` env hardening, `FlagsForReview` PR-body warnings, and `--allow-config-edits`), then add exactly one index entry for it in `docs/README.md` so it is reachable from the canonical doc list the website build consumes.

## Technical Implementation
### Steps
1. In `cmd/atcr/autofix.go`'s `addAutoFixFlags` (`cmd/atcr/autofix.go:44`), register a new flag immediately after `--no-sandbox`:
   ```go
   cmd.Flags().Bool("allow-config-edits", false,
       "DANGER: allows --auto-fix to create or modify files under protected host-execution/config paths "+
           "(.git/, .githooks/, .github/workflows/, .gitlab-ci.yml and other CI definitions, .vscode/, .idea/, "+
           ".env*, .planning/, .atcr). By default these paths are refused (see docs/security.md) because a patch "+
           "landing there can plant a trigger that executes on the host the next time git, a CI runner, or an "+
           "editor reads it — even though the validation step itself was sandboxed. Only pass this when you are "+
           "intentionally reviewing an --auto-fix change to a build/CI/editor config and accept that risk. "+
           "Meaningless without --auto-fix. Prints a security warning to stderr on every run.")
   ```
2. Add a `warnAllowConfigEdits(out io.Writer)` function next to `warnNoSandbox` (`cmd/atcr/autofix.go:82-86`), same non-memoized shape (no `sync.Once`, no state gate — fires on every invocation), writing to `cmd.ErrOrStderr()` so it never corrupts structured stdout payloads.
3. In `validateAutoFixBackend` (`cmd/atcr/autofix.go:158`), read the flag (`cmd.Flags().GetBool("allow-config-edits")`), call `warnAllowConfigEdits` when true, and store the resolved bool on `autoFixBackend` (new field, e.g. `allowConfigEdits bool`) so `runAutoFix`/`orchestrateAutoFix` can pass it through to `ApplyPatch` without re-reading the flag. Coordinate the exact field/threading shape with T2's `ApplyPatch`/`applyOne` signature — this task supplies the CLI-side value, T2 consumes it.
4. Write `docs/security.md` covering: (a) the Indirect Sandbox Escape / Host Trust Transposition threat model this epic addresses, (b) the pathguard blocklist and what it blocks by default, (c) `--allow-config-edits` and its security implications (mirroring the flag help text, expanded), (d) `internal/gitexec`'s environment hardening (`GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `--no-ext-diff`) and why every host git subprocess routes through it, (e) `FlagsForReview`'s non-blocking executable-bit/build-script PR warnings. Follow the existing doc style/tone in `docs/auto-fix.md` (same directory, same `--auto-fix` subject matter) rather than inventing a new format.
5. Add `docs/security.md` to `docs/README.md`'s index. Place it in the "Overview & configuration" section (`docs/README.md:8-16`), alongside `architecture.md`, since it documents cross-cutting security architecture rather than a single pipeline stage:
   ```markdown
   - [Security Architecture](security.md) — the pathguard protected-path blocklist,
     `--allow-config-edits`, `internal/gitexec` host-git hardening, and the
     non-blocking executable-bit/build-script review warnings.
   ```
6. Run `gofmt` and `go vet` on `cmd/atcr/autofix.go`; verify `docs/README.md` renders with a working relative link to `docs/security.md`.

## Files to Create/Modify
- `cmd/atcr/autofix.go` – modify (register `--allow-config-edits` flag, add `warnAllowConfigEdits`, thread the resolved value into `autoFixBackend`)
- `docs/security.md` – create
- `docs/README.md` – modify (add index entry)

## Documentation Links
(none — no category docs were generated for this plan; source.md found no specification above relevance threshold)

## Related Files (from codebase-discovery.json)
- `cmd/atcr/autofix.go`

## Success Criteria
- [ ] `--allow-config-edits` is registered in `addAutoFixFlags`, off by default, and absent from every existing `--auto-fix` invocation leaves behavior byte-identical.
- [ ] Passing `--allow-config-edits` prints the security warning to stderr on every invocation (non-memoized, matching `warnNoSandbox`'s unconditional behavior).
- [ ] The resolved flag value is available on `autoFixBackend` (or equivalent) for T2's `applyOne`/`ApplyPatch` gate to consume — no flag re-parsing needed downstream.
- [ ] `docs/security.md` exists and documents the pathguard blocklist, `--allow-config-edits`, `internal/gitexec` hardening, and `FlagsForReview`.
- [ ] `docs/README.md` links `docs/security.md` in its index — the doc is no longer invisible to the website build.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `--allow-config-edits` absent/false: flag defaults to `false`, no warning printed, `autoFixBackend.allowConfigEdits` (or equivalent field) is `false`.
- `--allow-config-edits` explicitly `true`: warning is printed to stderr exactly once per invocation (not deduplicated across multiple calls within one test, matching `warnNoSandbox`'s test pattern); resolved backend field is `true`.
- Flag registration does not collide with any existing flag name in `addAutoFixFlags`.

**Integration Tests:**
- None required for this task in isolation — T2's `apply_test.go` exercises the end-to-end `AllowConfigEdits` gate behavior once this task's flag value is threaded through.

**Test Files:**
- `cmd/atcr/autofix_test.go`

## Risk Mitigation
- **`--allow-config-edits` becoming a habitual bypass:** keep it off by default, pair every use with the mandatory non-memoized stderr warning (mirroring `--no-sandbox`), and document the risk explicitly in both the flag help text and `docs/security.md` so an operator cannot miss the implication of enabling it.
- **`docs/security.md` landing unlinked:** `docs/README.md` states it is the canonical index the website build consumes; adding the explicit index entry in this same task (rather than deferring it) prevents the doc from shipping invisible.
- **Flag threading drifting from T2's actual signature:** this task and T2 land together per the plan's Implementation Strategy; coordinate the exact `AllowConfigEdits` field/parameter name before finalizing so the CLI-side value and `apply.go`'s consumption side agree without a follow-up rename.

## Dependencies
- Task-02 (AllowConfigEdits threading target must exist)

## Definition of Done
- [ ] `--allow-config-edits` flag registered, off by default, help text documents the risk.
- [ ] `warnAllowConfigEdits` implemented and wired into `validateAutoFixBackend`, firing unconditionally on every `true` invocation.
- [ ] `docs/security.md` created covering pathguard, `--allow-config-edits`, `internal/gitexec`, and `FlagsForReview`.
- [ ] `docs/README.md` updated with a working index entry for `docs/security.md`.
- [ ] `go build ./...` and `go vet ./...` pass.
- [ ] `gofmt -l` reports no issues for `cmd/atcr/autofix.go`.
- [ ] All unit tests in `cmd/atcr/autofix_test.go` covering the new flag pass.
