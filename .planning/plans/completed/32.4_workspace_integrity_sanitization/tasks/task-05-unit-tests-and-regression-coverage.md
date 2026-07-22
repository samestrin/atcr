# Task 05: Unit Tests for pathguard + gitexec, and Six-Site Migration Regression Coverage

**Source:** Plan 32.4 – Debt Item #5
**Priority:** P1 | **Effort:** M | **Type:** Add

## Problem Statement
T1 (`internal/security/pathguard.go`) and T3 (`internal/gitexec` plus the migration of all six host git call sites) implement the two enforcement mechanisms this epic depends on, but neither is verified by tests yet. Without dedicated coverage, three acceptance criteria stay unproven: that `IsProtectedPath` matches consistently across canonical, relative, and symlink-traversal path forms (AC3); that `internal/gitexec` actually injects the hardening environment/flags (`GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `--no-ext-diff`) on every invocation (AC2); and, most importantly, that none of the six migrated call sites — or any other file in the tree — still constructs a bare `exec.Command("git", ...)` / `exec.CommandContext(ctx, "git", ...)` outside `internal/gitexec` (AC4). A single missed call site during T3's migration would silently reopen the exact subprocess-hijack gap this epic exists to close, and nothing today would catch that in CI.

## Solution Overview
Add `internal/security/pathguard_test.go` with table-driven unit tests covering `IsProtectedPath` across every blocklist category and path-format dimension called out in T1's Definition of Done (canonical, relative, `../`-traversal, and symlink-traversal forms), plus negative/boundary cases (`.gitignore`, `.githubx/`, empty string). Add `internal/gitexec/gitexec_test.go` with unit tests confirming the wrapper's constructor sets the three hardening env vars/flags on the resulting `*exec.Cmd`. Add one regression test (in either `internal/gitexec/gitexec_test.go` or a small top-level test file, whichever avoids import cycles — ground the final placement decision in what compiles) that walks the repository tree, greps `.go` source for `exec.Command(` / `exec.CommandContext(` calls whose first git-argument literal is `"git"`, excludes `internal/gitexec` itself plus the two confirmed-out-of-scope files (`internal/verify/localvalidate.go`, `internal/sandbox/docker.go`), and fails if any match remains — then asserts the six known call sites (`cmd/atcr/autofix.go`, `internal/fanout/review.go`, `internal/gitrange/resolver.go`, `internal/payload/diff.go`, `internal/personas/submit.go` twice for `runGit` and `gitHasStagedChanges`, `internal/stream/fileindex.go`) each reference `gitexec.` so the migration is positively confirmed, not just absence-of-bare-exec.

## Technical Implementation
### Steps
1. Read the finished `internal/security/pathguard.go` (T1) and `internal/gitexec/gitexec.go` (T3) to confirm exact exported names, signatures, and the final blocklist/env-var list before writing tests against them — do not guess at symbols.
2. Create `internal/security/pathguard_test.go` following the table-driven convention used in `internal/autofix/apply_test.go` (fixture consts, `testing.T` subtests via `t.Run`, `testify/assert` + `testify/require`):
   - One table per blocklist category (`.git/`, `.githooks/`, `.github/workflows/`, `.gitlab-ci.yml`/CI definitions, `.vscode/`, `.idea/`, `.env*`, `.planning/`, `.atcr`), each row covering: exact match, nested file under the dir, canonical absolute path, relative path (`./x`, bare `x`), a `../`-traversal path that resolves into the protected dir, and a symlink (created via `t.TempDir()` + `os.Symlink`) whose target resolves into a protected dir.
   - Negative-case rows: `.gitignore`, `.githubx/foo`, `.vscode-custom/bar`, `README.planning.md`, empty string, `.` — asserting `false`.
   - A dedicated symlink-traversal subtest group so AC3's "100% path matching across canonical, relative, and symlink-traversal path formats" is unambiguously demonstrated in one place reviewers can point to.
3. Create `internal/gitexec/gitexec_test.go`:
   - Unit test that the exported constructor (e.g. `gitexec.Command`/`gitexec.CommandContext` — confirm exact name from T3's implementation) returns a `*exec.Cmd` whose `Env` contains `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL=/dev/null`, and whose `Args` include `--no-ext-diff` where applicable per T3's design.
   - Use the package-var-indirection pattern already established in `internal/autofix/apply.go` (`removeFn`/`writeFileAtomicFn`) and `cmd/atcr/autofix.go` (`resolveHeadSHAFn`) if T3 exposes an equivalent swappable var for testability — otherwise test directly against the returned `*exec.Cmd` struct fields without spawning a real git process.
4. Add the AC4 regression test: read every `.go` file under the repo root (skip `vendor/`, `.git/`, and any build-tag-excluded files), pattern-match `exec.Command("git"` / `exec.Command(` + `"git"` as a following arg / `exec.CommandContext(<ctx>, "git"` occurrences (a simple regex or AST-based `go/parser` walk — prefer regex for simplicity unless it produces false positives, in which case fall back to `go/ast` inspection of `*ast.CallExpr` nodes), exclude `internal/gitexec/*.go` and the two confirmed-out-of-scope files, and `t.Fatal`/`t.Error` listing any remaining matches with file:line.
5. In the same regression test (or a sibling test), positively assert that each of the six known files contains a reference to the `gitexec` package (e.g. `strings.Contains(fileContents, "gitexec.")`) so the test fails loudly if a site was migrated away from `gitexec` back to a bare call, not just silently pass on an empty grep result.
6. Run `go vet ./internal/security/... ./internal/gitexec/...` and `gofmt -l` on both new test files.
7. Run the full test suite (`go test ./...`) to confirm the regression test passes against the current (post-T3) tree and does not have false positives against `internal/verify/localvalidate.go` or `internal/sandbox/docker.go`.

## Files to Create/Modify
- `internal/security/pathguard_test.go` – create
- `internal/gitexec/gitexec_test.go` – create

## Documentation Links
(none — no category docs were generated for this plan; source.md found no specification above relevance threshold)

## Related Files (from codebase-discovery.json)
- `internal/autofix/apply_test.go`

## Success Criteria
- [ ] `internal/security/pathguard_test.go` exists and exercises `IsProtectedPath` across canonical, relative, `../`-traversal, and symlink-traversal path formats for every blocklist category, plus negative/boundary cases.
- [ ] `internal/gitexec/gitexec_test.go` exists and confirms the wrapper injects `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, and `--no-ext-diff` (where applicable) on every constructed command.
- [ ] A regression test greps the tree (excluding `internal/gitexec`, `internal/verify/localvalidate.go`, `internal/sandbox/docker.go`) and fails if any bare `exec.Command("git",...)` / `exec.CommandContext(ctx, "git",...)` call site remains.
- [ ] The same regression test positively confirms all six migrated call sites (`cmd/atcr/autofix.go`, `internal/fanout/review.go`, `internal/gitrange/resolver.go`, `internal/payload/diff.go`, `internal/personas/submit.go` — both `runGit` and `gitHasStagedChanges` — `internal/stream/fileindex.go`) reference `internal/gitexec`.
- [ ] `go test ./internal/security/... ./internal/gitexec/...` passes; `go test ./...` passes tree-wide with no regression-test false positives.
- [ ] `go vet` and `gofmt -l` are clean for both new test files.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `pathguard_test.go`: table-driven per-category coverage (exact, nested, canonical absolute, relative, traversal, symlink) plus negative cases, mirroring the AC3 requirement verbatim.
- `gitexec_test.go`: constructor returns a `*exec.Cmd`/wrapper with the three hardening env vars/flags present; a case confirming the env is additive to (not a replacement of) the inherited process environment if that is T3's design (confirm against T3's actual implementation before asserting this).

**Integration Tests:**
- The AC4 regression test is itself an integration-style, whole-tree check (not scoped to a single package) and should be treated as the primary integration coverage for this task — it is the only test that spans all six call sites plus the exclusion list in one assertion.

**Test Files:**
- `internal/security/pathguard_test.go`
- `internal/gitexec/gitexec_test.go`

## Risk Mitigation
- **Regression test false positives on out-of-scope files:** `internal/verify/localvalidate.go` and `internal/sandbox/docker.go` both match a naive `exec.CommandContext(` grep despite not calling git. Mitigate by explicitly excluding these two files by path in the regression test (matching the exclusion list already confirmed in `codebase-discovery.json`) and adding a comment explaining why, so a future maintainer doesn't "fix" the exclusion and reintroduce a false failure.
- **Regression test false negatives (missed migrated site or a new call site added later without going through gitexec):** the six-site positive-assertion check (`gitexec.` string/reference present in each known file) guards against silent backsliding — if a site is reverted to bare `exec.Command`, both the negative grep and the positive per-file check fail.
- **Test writing against unstable/incomplete T1/T3 symbol names:** Step 1 requires reading the actual finished implementation files before writing test code against them — do not invent function signatures ahead of T1/T3 landing.
- **Symlink test flakiness across OSes:** use `t.TempDir()` + `os.Symlink` (skip test via `t.Skip` on platforms where symlinks require elevated privileges, e.g. some Windows CI runners, if `os.Symlink` returns a permission error) rather than hardcoding a symlink fixture path.

## Dependencies
- Task-01 (`internal/security/pathguard.go` must exist and be stable), Task-03 (`internal/gitexec/gitexec.go` plus the six-site migration must be complete)

## Definition of Done
- [ ] `internal/security/pathguard_test.go` implemented and passing, covering AC3's canonical/relative/symlink-traversal requirement in full.
- [ ] `internal/gitexec/gitexec_test.go` implemented and passing, covering AC2's env/flag hardening requirement.
- [ ] AC4 regression test implemented, passing against the current tree, and asserting both the negative (no stray bare git exec calls) and positive (all six sites reference gitexec) conditions.
- [ ] `go build ./...`, `go vet ./...`, and `gofmt -l` all pass with no new issues.
- [ ] `go test ./...` passes tree-wide.
- [ ] All three new/expanded test files committed alongside the rest of the epic's work.
