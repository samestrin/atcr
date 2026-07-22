# Task 06: Non-blocking `FlagsForReview` — Executable-Bit and Build-Script PR Warnings

**Source:** Plan 32.4 – Debt Item #6
**Priority:** P2 | **Effort:** M | **Type:** Add

## Problem Statement
T1/T2 make `internal/autofix/apply.go`'s `applyOne` fail closed on patches that write to protected host-execution paths (`.git/`, `.githooks/`, `.github/workflows/`, etc.) unless `--allow-config-edits` is passed. That gate is binary: a path is either refused or it silently applies. It has no answer for a narrower but still elevated-risk case — a patch that legitimately applies (either because the path isn't on the protected blocklist at all, or because `--allow-config-edits` was explicitly passed) but still does something a human reviewer should notice before approving: it flips a file's executable bit, or it touches a build/CI script (`Makefile`, `*.sh`, `package.json`, `Dockerfile`, a CI config living outside `.github/`) that runs with elevated trust the next time it's invoked. Today nothing surfaces that signal — the generated `--auto-fix` PR looks identical whether it touched `README.md` or flipped `deploy.sh` to executable.

`--auto-fix` already never auto-merges: `cmd/atcr/autofix.go:runAutoFix` always terminates at `gh.CreatePullRequest`/`gh.UpdatePullRequest`, never a merge call, so human review is already the terminal gate for every `--auto-fix` change. This task does not add a new gate or a new reviewer role — it makes elevated-risk patches more visible inside the review that already has to happen, by appending a warning section to the PR body.

## Solution Overview
Add a second, non-blocking check to `internal/security/pathguard.go`: `FlagsForReview(path string, oldMode, newMode int) (bool, reason string)`. It flags two independent, advisory-only conditions — an executable-bit change between `oldMode`/`newMode` (the `os.FileMode` values go-gitdiff's parsed `File.OldMode`/`File.NewMode` already carry at the T2 choke point, converted to `int`), and a match against a soft build-script path list (`Makefile`, `*.sh`, `package.json`, `Dockerfile`, CI configs living outside `.github/`). Unlike T1's `IsProtectedPath`, this check never returns an error and never blocks the apply — it only reports.

Thread the result out of `internal/autofix.ApplyPatch` as a new collected slice alongside the existing `BackupMap`, collected inside `applyOne` at the same choke point T2 uses (immediately after the parsed `gitdiff.File` is available, since `FlagsForReview` needs `f.OldMode`/`f.NewMode`, which do not exist until after `gitdiff.Parse` succeeds — this is one step later than `containedPath`/`IsProtectedPath`, which run on the raw entry before parsing). In `cmd/atcr/autofix.go:runAutoFix`, when the returned flag slice is non-empty, append a visible warning section to the PR body before the `CreatePullRequest`/`UpdatePullRequest` call; when it's empty, the PR body is unchanged from today.

## Technical Implementation
### Steps
1. In `internal/security/pathguard.go`, add a package-level, advisory-only soft list distinct from T1's blocking blocklist — `buildScriptPaths` covering: exact basename `Makefile`, glob suffix `*.sh`, exact basename `package.json`, exact basename `Dockerfile`, and CI config paths living outside `.github/` (reuse the same CI name set T1 already grounded — e.g. `.gitlab-ci.yml`, `.circleci/`, `Jenkinsfile` — do not invent new CI providers beyond what T1's blocklist already established). Note in a comment that a path already on T1's `IsProtectedPath` blocklist (e.g. `.gitlab-ci.yml`, `.github/workflows/*`) only ever reaches `FlagsForReview` if `--allow-config-edits` let it through the earlier gate — `FlagsForReview` is not a second copy of that gate, it is advisory visibility for whatever the protected-path gate already let apply (protected-and-allowed, or never-protected).
2. Implement `FlagsForReview(path string, oldMode, newMode int) (bool, string)` in `internal/security/pathguard.go`:
   - Executable-bit check: flag when `(oldMode&0111) != (newMode&0111)` — the execute-permission bits differ between old and new mode (covers a bit being added OR removed; go-gitdiff's `OldMode`/`NewMode` for a create diff have `OldMode == 0`, so a newly-created executable file is also caught since `0&0111 == 0 != newMode&0111`).
   - Build-script path check: match `path` (after `filepath.Clean`) against `buildScriptPaths` using the same boundary-safe matching style as `IsProtectedPath` (exact basename match or directory-prefix match, never a bare substring match) — reuse or factor out the matching helper T1 wrote rather than duplicating it.
   - Return `(true, reason)` if either condition is true, with `reason` naming which condition(s) fired (e.g. `"executable bit changed (0644 -> 0755)"`, `"build-script path"`, or both joined) so the PR-body warning can be specific rather than generic. Return `(false, "")` otherwise.
3. In `internal/autofix/apply.go`, add `type ReviewFlag struct { Path string; Reason string }` next to the `BackupMap` type definition, with a doc comment mirroring `BackupMap`'s style (what it records, keyed by what, populated when).
4. Thread flag collection through `applyOne` without changing the arity of its many existing `return` statements: change `applyOne`'s signature to accept an additional `flags *[]ReviewFlag` parameter (`func applyOne(root string, e payload.FileEntry, allowConfigEdits bool, flags *[]ReviewFlag) (absTarget, backupPath string, err error)`), and immediately after `f := files[0]` (the point right after `gitdiff.Parse` succeeds, before the `if f.IsDelete` branch) add:
   ```go
   if flagged, reason := security.FlagsForReview(e.Path, int(f.OldMode), int(f.NewMode)); flagged {
       *flags = append(*flags, ReviewFlag{Path: e.Path, Reason: reason})
   }
   ```
   This is the minimal-diff option: it avoids touching every one of `applyOne`'s existing `return "", "", err` statements to add a fourth return value, since the flag is purely additive bookkeeping via the passed-in pointer, not part of the success/error contract.
5. In `ApplyPatch`, declare `var flags []ReviewFlag` alongside `bm := make(BackupMap)`, pass `&flags` into every `applyOne` call in the loop, change the return type to `(BackupMap, []ReviewFlag, error)`, and return `flags` as the second value on both the success and aggregated-error paths (a flagged-but-failed entry should not appear in `flags` — only successfully-applied entries can be flagged, since the flag call sits after parse but the loop only reaches `flags` append for entries that got far enough to parse; an entry that later fails mid-write still keeps its flag entry recorded — decide and document whichever behavior is simpler to reason about: recommend only counting a flag once `applyOne` returns success, by moving the flag append to fire only when `applyOne`'s `err == nil`, since a PR body warning about a file that failed to apply and was reverted would be misleading).
6. Update the `ApplyPatch` doc comment to document the new `[]ReviewFlag` return value and its non-blocking nature, following the existing style used for `BackupMap`.
7. Update `cmd/atcr/autofix.go:365`'s call site (`bm, applyErr := autofix.ApplyPatch(be.applyTarget, run.Entries, ...)`) to capture the new middle return value: `bm, flags, applyErr := autofix.ApplyPatch(...)`.
8. In `runAutoFix`, immediately before the `prReq := ghaction.PullRequestRequest{...}` construction (`cmd/atcr/autofix.go:468`), build a warning section and append it to the body when `len(flags) > 0`:
   ```go
   body := run.Body
   if len(flags) > 0 {
       var b strings.Builder
       b.WriteString(body)
       b.WriteString("\n\n## Review Warnings\n\nThe following change(s) are flagged for extra reviewer attention (non-blocking):\n\n")
       for _, f := range flags {
           fmt.Fprintf(&b, "- `%s` — %s\n", f.Path, f.Reason)
       }
       body = b.String()
   }
   prReq := ghaction.PullRequestRequest{Head: run.Branch, Base: run.Base, Title: run.Title, Body: body}
   ```
   Use `body` (not `run.Body`) in the `prReq` literal. `strings` is already imported in `cmd/atcr/autofix.go`; confirm before adding a duplicate import.
9. Run `gofmt` and `go vet` across all three modified files.

## Files to Create/Modify
- `internal/security/pathguard.go` – modify (add `FlagsForReview`)
- `internal/autofix/apply.go` – modify (thread flagged entries out of `ApplyPatch`)
- `cmd/atcr/autofix.go` – modify (append PR-body warning section)

## Documentation Links
(none — no category docs were generated for this plan; source.md found no specification above relevance threshold)

## Related Files (from codebase-discovery.json)
- `internal/autofix/apply.go`
- `cmd/atcr/autofix.go`

## Success Criteria
- [x] `internal/security.FlagsForReview(path string, oldMode, newMode int) (bool, string)` exists, compiles, and never returns an error or panics for any input — purely advisory.
- [x] An executable-bit change (`oldMode&0111 != newMode&0111`) is flagged, including the create-diff case where `oldMode == 0` and the new file is executable.
- [x] A path matching the soft build-script list (`Makefile`, `*.sh`, `package.json`, `Dockerfile`, non-`.github` CI configs) is flagged, using boundary-safe matching (no false positive on e.g. `not-a-Makefile.txt` or `mySh.go`).
- [x] A patch with neither condition is not flagged.
- [x] `ApplyPatch`'s new `[]ReviewFlag` return value contains one entry per successfully-applied, flagged path — never for a path whose apply failed.
- [x] `applyOne`'s call to `security.IsProtectedPath` (T2) still fires before `FlagsForReview`, and `FlagsForReview` only ever evaluates a path that already passed T2's gate (fell through as unprotected, or protected-and-allowed via `--allow-config-edits`) — verified by reading the diff, not just tests passing.
- [x] A generated `--auto-fix` PR whose entries include at least one flagged path has a `## Review Warnings` section in the PR body naming each flagged path and its reason; a run with zero flagged paths leaves the PR body byte-identical to today's static body.
- [x] `go build ./...` and `go vet ./...` pass.

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `FlagsForReview` table-driven cases: executable bit added (`0644 -> 0755`), executable bit removed (`0755 -> 0644`), no mode change, create-diff of an executable (`oldMode=0, newMode=0100755`), each soft-list category (`Makefile`, `deploy.sh`, `package.json`, `Dockerfile`, a non-`.github` CI config path) both flagged-positive and near-miss negative (`not-a-Makefile.txt`, `foo.shell`, `nested/package.json.bak`), and a path/mode combination matching neither condition.
- Combined case: a path that is both a build-script match AND has an executable-bit change — assert the returned reason names both.

**Integration Tests:**
- `ApplyPatch` given one flagged entry (executable-bit change on a non-protected path) and one clean entry: both apply successfully, and the returned `[]ReviewFlag` contains exactly one entry for the flagged path.
- `ApplyPatch` given a flagged entry whose apply fails mid-write (e.g. via the existing `writeFileAtomicFn` test-failure indirection): confirm the failed entry does NOT appear in the returned `[]ReviewFlag`.
- `runAutoFix`-level test (or the closest existing harness exercising the `ApplyPatch` → PR-body path) confirming: (a) zero flagged entries produces the existing static PR body unchanged, (b) one or more flagged entries appends a `## Review Warnings` section naming the path(s) and reason(s).

**Test Files:**
- `internal/security/pathguard_test.go`
- `internal/autofix/apply_test.go`
- `cmd/atcr/autofix_test.go`

## Risk Mitigation
- **Risk:** The soft build-script list could over-warn (noise fatigue on every `*.sh` touch) or under-warn (missing a real risk path outside the named categories). **Mitigation:** scope strictly to the epic's named set (`Makefile`, `*.sh`, `package.json`, `Dockerfile`, non-`.github` CI configs) per plan.md's Risk Mitigation section — this is explicitly advisory, so a miss degrades to "no extra visibility," never a false refusal; do not expand the list speculatively.
- **Risk:** Overlap between T1's blocking blocklist (which already includes `.gitlab-ci.yml`/CI definitions) and T6's soft list (which also names "CI configs living outside `.github/`") could read as redundant or confusing. **Mitigation:** document explicitly (in the `pathguard.go` comment and this task) that `FlagsForReview` only ever sees a path after T2's gate already let it through — so for an already-protected CI path, the warning only appears when `--allow-config-edits` was used, which is exactly the case where extra reviewer visibility matters most.
- **Risk:** Adding a fourth return value to `applyOne` would force editing every one of its dozen existing `return` statements, a much larger diff than necessary for a purely additive, non-blocking signal. **Mitigation:** use the `flags *[]ReviewFlag` out-parameter pattern instead (Step 4) — touches only the one new append site, consistent with "touch only what you must."
- **Risk:** `os.FileMode`-to-`int` conversion of go-gitdiff's `OldMode`/`NewMode` could be misread as a portability concern. **Mitigation:** go-gitdiff's `parseMode` (`file_header.go`) stores the raw octal git mode number directly into `os.FileMode` (e.g. `0100644`), not Go's `os.FileMode` bit-flag encoding — so `int(f.OldMode)&0111` correctly reads the Unix executable bits; no additional masking or reinterpretation needed.

## Dependencies
- Task-01 (pathguard.go must exist), Task-02 (applyOne integration point and `allowConfigEdits` threading must exist)

## Definition of Done
- [x] `internal/security.FlagsForReview` implemented and exported, called from `applyOne` at the documented choke point (after `f := files[0]`, before the delete/modify/create branches).
- [x] `ApplyPatch`'s signature carries the new `[]ReviewFlag` return value; `cmd/atcr/autofix.go`'s sole call site is updated to match.
- [x] `runAutoFix`'s PR-body construction appends a `## Review Warnings` section exactly when the returned flag slice is non-empty, and leaves the body unchanged otherwise.
- [x] All new and existing tests in `internal/security/pathguard_test.go`, `internal/autofix/apply_test.go`, and `cmd/atcr/autofix_test.go` pass.
- [x] `go build ./...`, `go vet ./...`, and the project's standard lint pass with no new warnings.
- [x] `gofmt -l` reports no issues for the three modified files.
