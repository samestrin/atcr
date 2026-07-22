# Task 03: Build `internal/gitexec` and Migrate All Host Git Subprocess Call Sites

**Source:** Plan 32.4 – Debt Item #3
**Priority:** P1 | **Effort:** M | **Type:** Refactor

## Problem Statement
`internal/git/` does not exist. Host `git` subprocess execution is scattered across six production call sites, each constructing its own `exec.Command`/`exec.CommandContext` directly: `cmd/atcr/autofix.go`'s `resolveHeadSHAFn` (line ~508), `internal/fanout/review.go`'s `resolveHeadSHA` (line ~1611), `internal/gitrange/resolver.go`'s `gitRunner.run` (line ~162), `internal/payload/diff.go`'s `gitRunner.output` (line ~188), `internal/personas/submit.go`'s `runGit` (line ~392) and `gitHasStagedChanges` (line ~435, a direct `exec.CommandContext` that does NOT go through `runGit`), and `internal/stream/fileindex.go`'s `BuildFileIndex` (line ~51).

None of these six sites neutralizes the host's system or global git configuration before invoking git. An earlier `--auto-fix` run (or any other write into the working tree) can leave behind a poisoned `.git/config`, a machine-wide `/etc/gitconfig`, or a user global config with a malicious `core.pager`, `diff.external`, alias, or `credential.helper` entry. Every one of these six call sites will pick that poisoned config up on its next invocation — this is the "host git subprocess hijack" pillar of the Indirect Sandbox Escape threat this epic addresses, and it is orthogonal to (independent of) the path-blocklist work in T1/T2.

## Solution Overview
Create a new package `internal/gitexec` that exposes a single hardened way to construct git subprocesses: `CommandFn`/`CommandContextFn`, package-level function vars (mirroring the existing `resolveHeadSHAFn`/`newAutoFixGitHubFn`/`removeFn`/`writeFileAtomicFn` testability pattern already used in this codebase) that build an `*exec.Cmd` for `git <args...>` and unconditionally inject `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL=/dev/null` into the child's environment via `cmd.Environ()` (additive, so callers that already customize `cmd.Env`, e.g. `LC_ALL=C`/`LANG=C` in `gitrange`/`payload`, or `-c credential.helper=...` in `personas/submit.go`, keep working unchanged). Migrate all six call sites to build their `*exec.Cmd` through `gitexec.CommandFn`/`gitexec.CommandContextFn` instead of calling `exec.Command`/`exec.CommandContext` directly. For the two call sites that run git-diff-family subcommands (`internal/payload/diff.go`'s `gitRunner.output`, which runs `git -C <dir> -c core.quotePath=false diff|show ...`, and `internal/personas/submit.go`'s `gitHasStagedChanges`, which runs `git diff --cached --quiet`), add `--no-ext-diff` to the argument list at the call site so no `diff.external` entry in a poisoned config can substitute an attacker-controlled diff driver. `--no-ext-diff` is a diff-command-specific flag, not a global one, so it is added directly to the affected call sites' argv rather than baked into the generic wrapper — the other four call sites (`rev-parse`, `ls-files`, `checkout`/`add`/`commit`/`push`/`clone`) never invoke a diff driver and do not need it.

## Technical Implementation
### Steps
1. Create `internal/gitexec/gitexec.go` with a package doc comment explaining the threat this package closes (poisoned `.git/config`/system/global git config hijacking a host git subprocess left running after an earlier `--auto-fix` or other repo write) and the invariant it enforces (every host git subprocess in this codebase is constructed through this package; `AC4` — no bare `exec.Command("git",...)`/`exec.CommandContext(ctx, "git",...)` may remain outside it). Implement:
   - `var CommandFn = func(arg ...string) *exec.Cmd` — builds `exec.Command("git", arg...)` and hardens its environment.
   - `var CommandContextFn = func(ctx context.Context, arg ...string) *exec.Cmd` — builds `exec.CommandContext(ctx, "git", arg...)` and hardens its environment.
   - A private `hardenEnv(cmd *exec.Cmd) *exec.Cmd` helper both call, appending `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL=/dev/null` onto `cmd.Environ()` (never onto a nil/empty slice, so the child still inherits `PATH`, `HOME`, etc.) and returning `cmd` so call sites can chain `.Env = append(...)` afterward for their own additions (`LC_ALL=C`, `-c` flags already baked into `arg`, etc.).
   - Exported as `var`, not `func`, specifically so a test can substitute a call-recording fake without spawning real git — the same pattern `cmd/atcr/autofix.go`'s `resolveHeadSHAFn` already establishes.
2. Migrate `cmd/atcr/autofix.go`'s `resolveHeadSHAFn` (line ~508): replace `exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")` with `gitexec.CommandContextFn(ctx, "-C", dir, "rev-parse", "HEAD")`. Keep the surrounding var-indirection and error handling unchanged — this preserves the existing test substitution point.
3. Migrate `internal/fanout/review.go`'s `resolveHeadSHA` (line ~1611): replace `exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")` with `gitexec.CommandFn("-C", repo, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")`. Note this call site currently has no context/cancellation — do not add one as part of this migration; only swap the constructor.
4. Migrate `internal/gitrange/resolver.go`'s `gitRunner.run` (line ~162): replace `exec.CommandContext(g.ctx, "git", full...)` with `gitexec.CommandContextFn(g.ctx, full...)`, then keep the existing `cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C")` line as-is — it runs after `gitexec` has already set `cmd.Env` to the hardened base, so `cmd.Environ()` at that point already includes the two `GIT_CONFIG_*` vars and the append is additive, not a replacement.
5. Migrate `internal/payload/diff.go`'s `gitRunner.output` (line ~188): replace `exec.CommandContext(g.ctx, "git", full...)` with `gitexec.CommandContextFn(g.ctx, full...)`; keep the existing `-c core.quotePath=false` prefix in `full` and the subsequent `cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C")` line unchanged (same additive-append reasoning as step 4). Additionally, insert `--no-ext-diff` into `full` immediately after the `-C <dir>` / `-c core.quotePath=false` prefix and before the caller-supplied diff/show subcommand args, so every invocation through this method (diff payload construction) is protected against a `diff.external` hijack.
6. Migrate `internal/personas/submit.go`'s two call sites:
   - `runGit` (line ~392): replace `exec.CommandContext(ctx, "git", gitInvocation(args...)...)` with `gitexec.CommandContextFn(ctx, gitInvocation(args...)...)`. `gitInvocation` continues to prepend the `-c credential.helper=...` flags exactly as before.
   - `gitHasStagedChanges` (line ~435): replace the direct `exec.CommandContext(ctx, "git", "diff", "--cached", "--quiet")` with `gitexec.CommandContextFn(ctx, "diff", "--no-ext-diff", "--cached", "--quiet")`, adding `--no-ext-diff` since this is a diff invocation and was previously NOT routed through `runGit` at all (confirm the exit-code-1-means-true handling in the surrounding code is untouched by the constructor swap).
7. Migrate `internal/stream/fileindex.go`'s `BuildFileIndex` (line ~51): replace `exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z")` with `gitexec.CommandContextFn(ctx, "-C", root, "ls-files", "-z")`.
8. Remove the now-unused `"os/exec"` import from any of the six files where `gitexec.CommandFn`/`CommandContextFn` fully replaces the only `exec.*` usage in that file (verify per-file — some files may retain other `exec` usages and must keep the import).
9. Run `grep -rn 'exec\.Command(ctx\|exec\.Command("git"\|exec\.CommandContext(.*"git"' --include='*.go'` (excluding `internal/gitexec/`) across the repo to confirm zero remaining bare git subprocess constructions (AC4), and separately confirm `internal/verify/localvalidate.go` and `internal/sandbox/docker.go` are untouched (out of scope, per codebase-discovery.json — they invoke `validate_command`/`docker`, not git).
10. Run `gofmt` and `go vet` across all seven touched files (`internal/gitexec/gitexec.go` plus the six migrated files) before finishing.

## Files to Create/Modify
- `internal/gitexec/gitexec.go` – create
- `cmd/atcr/autofix.go` – modify
- `internal/fanout/review.go` – modify
- `internal/gitrange/resolver.go` – modify
- `internal/payload/diff.go` – modify
- `internal/personas/submit.go` – modify
- `internal/stream/fileindex.go` – modify

## Documentation Links
(none — no category docs were generated for this plan; source.md found no specification above relevance threshold)

## Related Files (from codebase-discovery.json)
- `internal/autofix/apply.go` (package-var testability precedent — `removeFn`/`writeFileAtomicFn`)

## Success Criteria
- [x] `internal/gitexec/gitexec.go` exists, compiles, and exports `CommandFn(arg ...string) *exec.Cmd` and `CommandContextFn(ctx context.Context, arg ...string) *exec.Cmd` as package-level vars.
- [x] Every `*exec.Cmd` returned by `CommandFn`/`CommandContextFn` carries `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL=/dev/null` in `cmd.Env`, additively over `cmd.Environ()` (AC2).
- [x] All six call sites (`cmd/atcr/autofix.go`, `internal/fanout/review.go`, `internal/gitrange/resolver.go`, `internal/payload/diff.go`, `internal/personas/submit.go` ×2, `internal/stream/fileindex.go`) construct their git subprocess exclusively through `gitexec.CommandFn`/`gitexec.CommandContextFn`.
- [x] `internal/payload/diff.go`'s `gitRunner.output` and `internal/personas/submit.go`'s `gitHasStagedChanges` include `--no-ext-diff` in their git invocation argv.
- [x] A repo-wide grep for `exec.Command("git"` / `exec.CommandContext(ctx, "git"` (and equivalents) outside `internal/gitexec/` returns zero matches (AC4).
- [x] `internal/verify/localvalidate.go` and `internal/sandbox/docker.go` are unmodified.
- [x] All pre-existing behavior (working directory via `-C`, `LC_ALL=C`/`LANG=C`, `-c credential.helper=...`, exit-code-1-is-true semantics in `gitHasStagedChanges`) is preserved exactly.
- [x] Existing tests for all six call sites pass unmodified (or with only the minimal changes required by the constructor swap, not behavior changes).

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `internal/gitexec/gitexec_test.go`: `CommandFn`/`CommandContextFn` produce an `*exec.Cmd` whose `Env` contains both `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL=/dev/null` alongside the inherited process environment (assert both are present, not that they are the only entries).
- `internal/gitexec/gitexec_test.go`: confirm `CommandFn`/`CommandContextFn` are swappable package vars (a test replaces one with a stub and restores it, proving the indirection point works for downstream call-site tests).
- Per migrated call site: existing unit/integration tests (`internal/fanout/review_test.go`'s `resolveHeadSHA` cases, `internal/personas/submit_test.go`'s `gitHasStagedChanges` cases, `internal/stream/fileindex_test.go`'s `BuildFileIndex` cases, `internal/gitrange`/`internal/payload` diff/resolver test suites) continue to pass against a real temp git repo, proving the hardened env does not break legitimate git operations (clean local repo, no system/global config needed for `rev-parse`/`ls-files`/`diff`/`checkout`/`commit`/`push` against a local remote).

**Integration Tests:**
- A poisoned-config regression test (new or added to an existing suite): write a malicious `~/.gitconfig`-equivalent or system-config-equivalent (e.g. a `core.pager` or `diff.external` entry) into a location `GIT_CONFIG_GLOBAL=/dev/null`/`GIT_CONFIG_NOSYSTEM=1` should neutralize, confirm the migrated call site's git invocation does NOT pick it up (e.g. `--no-ext-diff` invocation ignores a poisoned `diff.external`, and `GIT_CONFIG_GLOBAL=/dev/null` invocation ignores a poisoned global `core.pager`/alias).
- End-to-end `--auto-fix` flow test (if one already exists) confirming the full pipeline still resolves HEAD SHA, checks staged changes, and computes diffs correctly after migration.

**Test Files:**
- `internal/gitexec/gitexec_test.go`

## Risk Mitigation
- **A missed call site leaves exactly the gap this epic exists to close.** Mitigated by the repo-wide grep in Step 9 run as a hard gate before marking this task done — AC4 is binary (zero remaining bare `exec.Command("git",...)`/`exec.CommandContext(ctx, "git",...)` outside `internal/gitexec/`), not a judgment call.
- **Silently breaking an existing test's environment assumptions** (e.g. a test that relies on inheriting the running user's global git config, such as `user.name`/`user.email` for commit tests) — mitigated by additive `cmd.Environ()` composition (only `GIT_CONFIG_NOSYSTEM`/`GIT_CONFIG_GLOBAL` are forced; nothing else in the child's environment is removed) and by running the full existing test suite for all six migrated packages before considering the task complete. If a commit-authoring test fails because it relied on a global `user.name`/`user.email`, that test must set those via `-c user.name=...`/`-c user.email=...` or repo-local config rather than reverting the hardening.
- **`--no-ext-diff` placement in the wrong argv position** for `git -C <dir> -c core.quotePath=false diff|show ...` — mitigated by verifying `--no-ext-diff` lands in a position git accepts for both `diff` and `show` subcommands (immediately after the subcommand name is always valid) and by running `internal/payload/diff_test.go`'s existing suite unmodified.
- **Confusing this task's scope with T1/T2's path-blocklist work** — this task only hardens git's own config resolution against a poisoned repo/system/global config; it does not prevent a patch from writing into `.git/` in the first place (that is T1/T2's `IsProtectedPath` gate). The two hardening pillars are complementary and independently shippable.

## Dependencies
- None (independent of T1/T2)

## Definition of Done
- [x] `internal/gitexec/gitexec.go` created and compiles cleanly.
- [x] All six call sites migrated to `gitexec.CommandFn`/`gitexec.CommandContextFn`; no behavior change beyond the added environment hardening and the two `--no-ext-diff` insertions.
- [x] Repo-wide grep confirms zero remaining bare `exec.Command("git",...)`/`exec.CommandContext(ctx, "git",...)` call sites outside `internal/gitexec/` (AC4).
- [x] `internal/verify/localvalidate.go` and `internal/sandbox/docker.go` confirmed untouched.
- [x] `internal/gitexec/gitexec_test.go` added and passing.
- [x] Full existing test suites for `cmd/atcr`, `internal/fanout`, `internal/gitrange`, `internal/payload`, `internal/personas`, and `internal/stream` pass unmodified (or with only mechanical constructor-swap updates, never assertion-weakening changes).
- [x] `gofmt -l` and `go vet ./...` clean across all seven touched files.
