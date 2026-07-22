# Task 01: Build `internal/security/pathguard.go` Protected-Path Blocklist

**Source:** Plan 32.4 – Debt Item #1
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
ATCR's `--auto-fix` pipeline applies LLM-generated patches to the host repository via `atomicfs` and then runs host-side `git` commands. Nothing today stops a patch from writing to host configuration paths that execute automatically on the developer's machine after the sandboxed review completes — `.git/config` (core.hooksPath, url.insteadOf rewrites), `.githooks/` and `.git/hooks/` (pre-commit/post-checkout triggers), `.github/workflows/` and `.gitlab-ci.yml` (CI definitions that run on next push), `.vscode/` and `.idea/` (editor tasks/launch configs that auto-execute), `.env*` (secrets), `.planning/` (ATCR's own planning state), and `.atcr` (ATCR's own config). This is an Indirect Sandbox Escape / Host Trust Transposition vector: the sandbox boundary is respected during execution, but the host is compromised the moment the patch is committed and the corresponding tool (git, CI runner, editor) later reads the modified file.

There is currently no reusable, centralized predicate for identifying these protected paths. Downstream gating logic (T2's `apply.go` write-path check, T6's non-blocking `FlagsForReview` warning) both need this predicate to exist first — this task builds the foundational package with no other dependencies.

## Solution Overview
Create a new package `internal/security/` containing `pathguard.go`, which exports `IsProtectedPath(path string) bool`. The function normalizes the input path (resolve `./`, `../`, and symlink components against a stable base) and then checks it against a denylist of protected repo-relative directories/files using prefix-boundary matching — mirroring the shape of `internal/validation/validation.go`'s `FilePath` function (pure string matching, no I/O for the matching logic itself, denylist of directory prefixes, boundary-safe comparison so `.gitignore` doesn't false-positive against a `.git` block). Unlike `FilePath`, which blocks absolute system directories, `pathguard.go` blocks repo-relative configuration directories, so it lives in its own package rather than being added to `internal/validation`.

## Technical Implementation
### Steps
1. Create `internal/security/pathguard.go` with a package doc comment describing its purpose (indirect sandbox escape / host trust transposition prevention) and scope (repo-relative path blocklist, not absolute system dirs — that's `internal/validation.FilePath`'s job).
2. Define the blocklist as a package-level slice/set of protected path prefixes, covering at minimum:
   - `.git/` (and bare `.git` file for worktrees/submodules) — hooks, config, refs
   - `.githooks/` — custom hooksPath target
   - `.github/workflows/` — GitHub Actions CI definitions
   - `.gitlab-ci.yml` and other CI definition files/dirs (e.g. `.circleci/`, `Jenkinsfile` if in scope per plan — ground exact CI list against `codebase-discovery.json`/plan.md; do not invent beyond what plan.md specifies)
   - `.vscode/` — editor tasks.json/launch.json auto-execution
   - `.idea/` — JetBrains editor run configs
   - `.env` and `.env*` (e.g. `.env.local`, `.env.production`)
   - `.planning/` — ATCR's own planning state
   - `.atcr` — ATCR's own config file
3. Implement `IsProtectedPath(path string) bool`:
   - Reject empty path as not-protected (`false`) unless plan intent says otherwise — confirm against T2's expected call sites; default to safe behavior (empty/invalid path treated cautiously, but do not invent new error-return semantics since the signature is `bool`, not `(bool, error)`).
   - Normalize the path: use `filepath.Clean` to collapse `./` and resolve `../` segments lexically (matching `validation.FilePath`'s traversal-rejection precedent), then use `filepath.EvalSymlinks` where the path exists on disk to catch symlink-traversal cases (AC3 requirement) — fall back to the lexically-cleaned path when the file does not yet exist (e.g. a new file being written for the first time, which is the common `--auto-fix` case) so the function still works for not-yet-created paths.
   - Compare the normalized path against each blocklist entry using directory-boundary matching (exact match or `prefix + "/"` match), the same pattern as `windowsSystemPath` and the `sysDir` loop in `validation.go`'s `FilePath` — never a bare `strings.HasPrefix` that would false-positive on `.gitignore` vs `.git`.
   - Handle both relative (`./githooks/x`, `githooks/x`) and absolute paths (resolve relative-to-repo-root inputs consistently; document the assumption that callers pass paths relative to repo root, matching how `atomicfs`/`apply.go` already operate).
4. Add package-level doc comments on `IsProtectedPath` explaining exactly what it does and does not guard (no I/O side effects beyond `EvalSymlinks` read, not a substitute for OS-level permission enforcement).
5. Run `gofmt` and `go vet` on the new file before finishing.

## Files to Create/Modify
- `internal/security/pathguard.go` – new file; package `security`; exports `IsProtectedPath(path string) bool` and the blocklist.

## Documentation Links
(none — no category docs were generated for this plan; source.md found no specification above relevance threshold)

## Related Files (from codebase-discovery.json)
- `internal/validation/validation.go`

## Success Criteria
- [x] `internal/security/pathguard.go` exists, compiles, and exports `IsProtectedPath(path string) bool`.
- [x] Blocklist covers `.git/`, `.githooks/`, `.github/workflows/`, `.gitlab-ci.yml`/CI definitions, `.vscode/`, `.idea/`, `.env*`, `.planning/`, and `.atcr`.
- [x] `IsProtectedPath` correctly matches canonical paths, relative paths (`./`, bare relative), path-traversal forms (`../`), and symlink-traversal cases (a symlink pointing into a protected dir resolves to `true`).
- [x] Directory-boundary matching prevents false positives (e.g. `.gitignore`, `.githubx/`, `.vscode-custom/` are NOT flagged as protected).
- [x] No I/O performed except the necessary `filepath.EvalSymlinks` resolution; function has no side effects.
- [x] `go vet ./internal/security/...` and `gofmt -l internal/security/pathguard.go` are clean.

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- Table-driven tests covering each blocklist category with: exact match, nested file under the dir, canonical absolute path, relative path (`./x`, `x` bare), path with `..` traversal that resolves into a protected dir, and a symlink whose target resolves into a protected dir.
- Negative cases: paths that look similar but are not protected (`.gitignore`, `.githubx`, `.vscode2/foo`, `README.planning.md`, a plain `.env.example.txt` if scoping excludes it — confirm exact `.env*` scope against plan.md), and a normal source file path.
- Empty-string and `.` input handling.
- Case sensitivity behavior documented and tested (git is case-sensitive on Linux; confirm and test the chosen behavior explicitly rather than leaving it implicit).

**Integration Tests:**
- None required for this task — T2 and T6 integration tests will exercise `IsProtectedPath` via `apply.go` and `FlagsForReview` respectively once this package exists.

**Test Files:**
- `internal/security/pathguard_test.go`

## Risk Mitigation
- **Symlink resolution on non-existent paths:** `filepath.EvalSymlinks` errors when the path doesn't exist yet (common for `--auto-fix` creating new files). Mitigate by resolving the deepest existing ancestor directory's symlinks and rejoining the remaining path components, so protection still applies to not-yet-created files inside a protected (possibly symlinked) directory.
- **Over-blocking legitimate paths:** Prefix-boundary matching (not bare substring matching) avoids false positives like `.gitignore` vs `.git/`. Verify with explicit negative-case unit tests before T2 wires in enforcement.
- **Scope creep on CI file list:** Do not invent additional CI providers/paths beyond what plan.md and codebase-discovery.json specify — grep the plan for the authoritative CI list before hardcoding beyond `.gitlab-ci.yml`/`.github/workflows/`.

## Dependencies
- None (foundational task)

## Definition of Done
- [x] `internal/security/pathguard.go` implemented and committed.
- [x] `internal/security/pathguard_test.go` implemented with full coverage of AC3 (canonical, relative, symlink-traversal path formats).
- [x] `go build ./...` and `go vet ./...` pass.
- [x] `gofmt -l` reports no issues for the new files.
- [x] All unit tests pass (`go test ./internal/security/...`).
- [x] Package doc comment and function doc comments follow the style of `internal/validation/validation.go`.
