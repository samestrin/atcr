# Task 02: Wire pathguard into internal/autofix/apply.go's applyOne

**Source:** Plan 32.4 – Debt Item #2
**Priority:** P1 | **Effort:** S | **Type:** Refactor

## Problem Statement
`--auto-fix` applies LLM-generated patches directly to the host repository via `internal/autofix/apply.go`'s `applyOne`. `applyOne` already re-checks path containment (`containedPath`) as defense-in-depth against traversal, but nothing stops a patch whose target path is *inside* the working tree yet names a critical host-execution or config path — `.git/*`, `.githooks/*`, `.github/workflows/*`, `.gitlab-ci.yml`, `.vscode/*`, `.idea/*`, `.env*`, `.planning/`, `.atcr`. A patch that legitimately stays inside `root` can still overwrite a git hook, a CI workflow, or `.env` and have it execute with full host privileges after the sandboxed pass ends (Host Trust Transposition). `containedPath` answers "is this inside the tree?", not "is this a path we should never let an LLM patch write to?" — that second question has no answering check today.

## Solution Overview
Add a single, non-bypassable-by-default gate in `applyOne` immediately after `containedPath` succeeds and before `refuseSymlinkLeaf` / the delete-modify-create branches: call `pathguard.IsProtectedPath(e.Path)` (built in Task 01, package `internal/security`) and, if it reports true, refuse the entry with a security error — unless the caller has explicitly set an `AllowConfigEdits` bypass. Thread that bool from the CLI down to `ApplyPatch`, following the exact precedent `--no-sandbox` already established in `cmd/atcr/autofix.go` (an off-by-default `cmd.Flags().Bool(...)`, read into a backend/run struct field, read back out at the call site). This task only wires the gate and its plumbing; the CLI flag itself (`--allow-config-edits`) and its stderr warning are Task 04 — this task's `ApplyPatch`/`applyOne` signature change is what Task 04's flag will feed into.

## Technical Implementation
### Steps
1. In `internal/autofix/apply.go`, change `ApplyPatch`'s signature from `ApplyPatch(root string, entries []payload.FileEntry) (BackupMap, error)` to accept an `allowConfigEdits bool` parameter (or a small package-level options struct if a second such flag is anticipated soon — prefer the plain bool parameter unless Task 06's `FlagsForReview` wiring makes an options struct clearly cheaper; do not introduce a struct speculatively). Thread the same parameter into `applyOne(root string, e payload.FileEntry, allowConfigEdits bool) (absTarget, backupPath string, err error)`, updating the one internal call site inside `ApplyPatch`'s loop (`internal/autofix/apply.go:67`).
2. In `applyOne`, immediately after the existing `containedPath` call succeeds (`internal/autofix/apply.go:87-90`) and before `refuseSymlinkLeaf` (`internal/autofix/apply.go:91`), add:
   ```go
   if security.IsProtectedPath(e.Path) && !allowConfigEdits {
       return "", "", fmt.Errorf("autofix: refusing to write %q: path is protected by workspace-integrity policy (pass --allow-config-edits to override): %w", e.Path, security.ErrProtectedPath)
   }
   ```
   (Exact error text/sentinel depends on what Task 01 exports from `internal/security/pathguard.go` — if Task 01 exposes a typed sentinel error, wrap it with `%w` so callers/tests can `errors.Is` against it; if not, match Task 01's actual exported error shape instead of inventing one here.) Import `github.com/samestrin/atcr/internal/security` in `apply.go`'s import block (`internal/autofix/apply.go:9-20`).
3. Check `e.Path` (the diff-declared path), not the resolved absolute `abs` — `pathguard.IsProtectedPath` operates on repo-relative path segments per Task 01's contract, and checking the pre-join path keeps the gate symmetric with `containedPath`'s own `p` parameter.
4. Update `cmd/atcr/autofix.go:365` (`autofix.ApplyPatch(be.applyTarget, run.Entries)`) to pass the new parameter. For this task, wire a value that defaults to `false` (i.e. pass `false`, or a zero-value field read off `be`/`run` if Task 04 has not yet landed the flag) so existing behavior for every caller that doesn't pass `--allow-config-edits` is unchanged — do NOT add the `--allow-config-edits` cobra flag definition itself here; that is Task 04's scope. If Task 04 lands first in execution order, coordinate the field name (suggest `AllowConfigEdits` on `autoFixBackend`, mirroring `noSandbox` at `cmd/atcr/autofix.go:114`) so the two tasks converge on one signature rather than two.
5. Add a package-level indirection only if `internal/security.IsProtectedPath` needs to be swappable in `apply_test.go` the way `removeFn`/`writeFileAtomicFn` already are (`internal/autofix/apply.go:38,43`) — likely unnecessary since `IsProtectedPath` is a pure function with no I/O (per Task 01's design notes), so tests can call it directly with real protected/unprotected paths instead of needing a fake.
6. Update the `ApplyPatch` doc comment (`internal/autofix/apply.go:45-62`) to document the new parameter and the protected-path refusal behavior, following the existing style that documents `containedPath`'s defense-in-depth rationale.

## Files to Create/Modify
- `internal/autofix/apply.go` – add `security` import; extend `ApplyPatch`/`applyOne` signatures with `allowConfigEdits bool`; insert the `IsProtectedPath` gate between `containedPath` and `refuseSymlinkLeaf`; update doc comments.
- `cmd/atcr/autofix.go` – update the `autofix.ApplyPatch(be.applyTarget, run.Entries)` call site (line 365) to pass the new parameter (default `false` unless Task 04's flag/field already exists).

## Documentation Links
(none — no category docs were generated for this plan; source.md found no specification above relevance threshold)

## Related Files (from codebase-discovery.json)
- `internal/autofix/apply.go`
- `internal/atomicfs/`

## Success Criteria
- [x] `applyOne` calls `security.IsProtectedPath(e.Path)` immediately after `containedPath` succeeds and before `refuseSymlinkLeaf`/the delete-modify-create branches — verified by reading the diff, not just tests passing.
- [x] A patch entry whose `Path` matches a protected pattern (`.git/*`, `.githooks/*`, `.github/workflows/*`, `.gitlab-ci.yml`, `.vscode/*`, `.idea/*`, `.env*`, `.planning/`, `.atcr`) is refused with a security error when `allowConfigEdits` is `false`, for all three entry kinds (create, modify, delete).
- [x] The same protected-path entry succeeds (falls through to the existing delete/modify/create logic unchanged) when `allowConfigEdits` is `true`.
- [x] An entry targeting a non-protected path is completely unaffected — no new error, no new backup-cleanup path, byte-identical behavior to before this task for every existing passing test.
- [x] `ApplyPatch`'s existing per-entry error-isolation contract (one entry's rejection does not block or roll back sibling entries) holds for the new protected-path rejection exactly as it does for existing rejections (traversal, symlink-leaf, parse failure).
- [x] `go build ./...` and `go vet ./...` pass with the new `internal/security` import wired in (Task 01 must have already landed `IsProtectedPath`; this task depends on it and will not compile standalone).

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `applyOne`/`ApplyPatch` refuses a create entry targeting `.github/workflows/ci.yml` when `allowConfigEdits=false`, returning an error the caller can distinguish from a traversal/symlink-leaf rejection (e.g. via `errors.Is` against Task 01's sentinel, if one exists).
- Same refusal for a modify entry targeting `.env` and a delete entry targeting `.git/hooks/pre-commit`.
- The same three cases succeed (no refusal, normal apply proceeds) when `allowConfigEdits=true`.
- A non-protected path (e.g. `src/main.go`) is unaffected by the new check regardless of `allowConfigEdits` value — regression guard against over-broad matching.
- Batch case: `ApplyPatch` given a mix of one protected-path entry and one clean entry, with `allowConfigEdits=false`, applies the clean entry successfully and reports only the protected-path entry in the aggregated `errors.Join` error (per the existing per-file isolation contract at `internal/autofix/apply.go:63-79`).
- Ordering case: confirm the protected-path check fires before `refuseSymlinkLeaf` and before any `gitdiff.Parse`/backup/write call — assert via a protected-path entry with a deliberately malformed diff body that the returned error is the protected-path error, not a parse error (proves the gate is checked first, per the plan's stated choke-point placement).

**Integration Tests:**
- `cmd/atcr/autofix.go`'s existing `runAutoFix`-level tests (if any exercise `ApplyPatch` end-to-end) continue to pass unmodified with the new parameter wired to `false`, confirming the CLI-level default posture is unchanged until Task 04 adds the flag.

**Test Files:**
- `internal/autofix/apply_test.go`

## Risk Mitigation
- **Risk:** Changing `ApplyPatch`'s public signature is a breaking API change for the one known caller (`cmd/atcr/autofix.go:365`) and any future caller. **Mitigation:** this is the only external caller today (confirmed by grep); update it in the same change so the package and its sole consumer land atomically — do not leave `cmd/atcr` broken between commits.
- **Risk:** Checking `e.Path` instead of the resolved absolute `abs` could be bypassed by a path that normalizes differently than expected. **Mitigation:** `containedPath` has already cleaned/joined and validated `e.Path` against `root` by the time this gate runs, and Task 01's `IsProtectedPath` is expected to do its own segment-aware normalization (not naive prefix string matching) — verify Task 01's contract handles `./`, mixed-case (on case-insensitive filesystems), and trailing-slash variants before relying on a raw string check here.
- **Risk:** Landing this gate before Task 04's `--allow-config-edits` flag exists means there is a window where the parameter exists but nothing sets it to `true` — operators who need the legitimate bypass have no way to invoke it yet. **Mitigation:** acceptable within this plan's stated task order (T2 depends on T1; T4 is documented as tightly coupled to T2 but sequenced separately) — this task defaults `allowConfigEdits` to `false` at the one call site, which is the correct fail-closed posture until T4 lands the flag.

## Dependencies
- Task-01 (pathguard.IsProtectedPath must exist)

## Definition of Done
- [x] `internal/security.IsProtectedPath` is called at the specified choke point in `applyOne`, confirmed by reading the diff.
- [x] `ApplyPatch`/`applyOne` signatures carry the new `allowConfigEdits bool` parameter and the sole caller in `cmd/atcr/autofix.go` is updated to match.
- [x] All new and existing tests in `internal/autofix/apply_test.go` pass.
- [x] `go build ./...`, `go vet ./...`, and the project's standard lint pass with no new warnings.
- [x] No behavior change for any existing non-protected-path entry or any caller not yet passing `allowConfigEdits=true` (regression-safe merge point for Task 04).
