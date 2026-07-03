# Tech Debt Captured — Sprint 17.0 (Auto-Merged Fixes)

Deferred MEDIUM/LOW findings surfaced during `/execute-sprint`. Read by
`/execute-code-review` and pre-seeded into the adversarial TD stream.

## TD-001 — GitHub token `contents: write` scope unvalidated by the stub (MEDIUM)
**Origin:** Phase 1, task 1.LAST gate review, 2026-07-03
**Issue:** Spike 1.2 drove auth against an `httptest.Server` that accepts any bearer token, so the "auth flows on every call" finding validates header plumbing only, not that a real `GITHUB_TOKEN` carries `contents: write`. Creating blobs/trees/commits/refs 403s with a default read-only token or on fork PRs — an integration failure the mock hides.
**Why accepted:** Phase 1 is a stubbed spike by design; the plan never intends a live GitHub call. Story 4/6 own the real precondition.
**Fix in:** Phase 4/5 — record `contents: write` as an explicit `--auto-fix` backend precondition in Story 6's gate (`validateAutoFixBackend`) and document the required token scope; optionally add a permission smoke check.

## TD-002 — Git Data API request/response shapes retained only as prose (MEDIUM)
**Origin:** Phase 1, task 1.LAST gate review, 2026-07-03
**File:** .planning/sprints/active/17.0_auto_merged_fixes/phase1-spike-findings.md:88
**Issue:** The 1.2 spike httptest driver was deleted, so the exact request-body keys (blob/tree/commit/ref) and the 422 `Message == "Reference already exists"` extraction survive only as prose in the findings note, not as an executable fixture. (Contrast 1.1, whose contract is retained as `internal/autofix/gitdiff_contract_test.go`.)
**Why accepted:** Phase 4 RED (task 4.1, AC 04-03) writes these `httptest`-driven tests against method+path routing anyway, re-establishing executable fixtures; the prose note + the retained stub-routing pattern are sufficient scaffolding until then.
**Fix in:** Phase 4 — Phase 4.1 RED tests supersede the prose by encoding the 4 request/response bodies and the 422 path as real assertions.
**Resolved:** 2026-07-03 — Story-4 tests (`TestCreateCommitSingleFile`, `TestCreateBranchRefAlreadyExists`, et al.) now encode the blob/tree/commit/ref bodies and the 422 collision path as executable assertions.

## TD-003 — Findings note overstates plumbing; 422 contract is POST-path-dependent (LOW)
**Origin:** Phase 1, task 1.LAST gate review, 2026-07-03
**File:** .planning/sprints/active/17.0_auto_merged_fixes/phase1-spike-findings.md:88
**Issue:** The note header says "via existing `postDo`/`get`" but the 1.2 spike exercised only `postDo`. `get` returns a plain `fmt.Errorf`, not `*APIError`, so the AC 04-02 422-collision contract (`errors.As(err, *APIError)`) holds only because all 4 Git Data calls are POST — a silent trap if any is later switched to GET.
**Why accepted:** Cosmetic/documentation nuance; all 4 Git Data calls are POST by design and Story 4 has no GET on the mutation path.
**Fix in:** Phase 4 — when Story 4 lands, confirm no mutation-path call uses `get`, or extend `get` to also return `*APIError` if one is ever needed there.
**Resolved:** 2026-07-03 — Story 4's `CreateCommit` reads the parent commit via `get` (a mutation-path GET), so `get` was extended to return `*APIError` on non-2xx (mirroring `sendDo`); a missing base SHA now surfaces as an inspectable typed error (`TestCreateCommitParentReadErrorIsTyped`).

## TD-004 — Create entry silently overwrites an existing target (MEDIUM)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-07-03
**File:** internal/autofix/apply.go:108
**Issue:** A create entry (`f.IsNew`) whose target already exists is applied against empty content and atomically overwritten, rather than rejected the way `git apply` refuses a create over an existing file. The clobber is silent.
**Why accepted:** Not data loss — the pre-existing content is captured by `atomicfs.BackupToDotBak` before the write and recorded in `BackupMap` with a non-empty backup path, so Story 3's Revert restores it. Only the git-apply-style "refuse create over existing" nicety is missing; the applied result is correct and revertible.
**Fix in:** A later hardening pass — stat the target in the `IsNew` branch and fail the entry with an "already exists" per-file error, or route it through the modify path against the real on-disk content.

## TD-005 — BackupMap empty-value sentinel is overloaded (Story 1→3 handoff) (MEDIUM)
**Origin:** Phase 2, tasks 2.2.A adversarial + 2.8 gate reviews, 2026-07-03
**File:** internal/autofix/apply.go:129
**Issue:** `BackupMap`'s empty backup-path value is documented to mean "file created by this run → Revert removes it," but `applyOne` also emits an empty value for (a) a modify/delete entry whose in-tree target is a **symlink** — `atomicfs.BackupToDotBak` Lstat-skips symlinks and returns `("", nil)`, then `WriteFileAtomic` replaces the link with a regular file (or a delete unlinks it) — and (b) an **already-gone delete** (the idempotent branch). Story 3's Revert, told to delete empty-value entries, would delete a pre-existing symlinked target instead of restoring it, defeating the revert safety net for that case. The write-boundary re-check guards a symlinked *directory component* (closed in 2.3) but not the *target leaf* itself.
**Why accepted:** Symlinked patch targets are outside the technical-debt-fix use case; the security-relevant escape (symlinked directory component) is closed. This is a Story-1→Story-3 contract-clarity issue, best resolved where Revert is implemented and the created-vs-restore decision actually lives.
**Fix in:** Phase 3 (Story 3, Revert) precondition — disambiguate the states rather than overloading `""`: either add an explicit per-entry kind (created/modified/deleted) to the handoff, or reject an in-tree symlink target at the write boundary so an empty backup path unambiguously means "created." Add a symlinked-target round-trip-through-Revert regression test.

## TD-008 — No default validation-timeout constant / config key (LOW)
**Origin:** Phase 2, task 2.8 gate review, 2026-07-03
**File:** internal/verify/localvalidate.go:130
**Issue:** `ResolveValidateCommand` establishes the command default (`go build ./...`) and hard refusal, but there is no matching constant or resolver for the ~2 min default validation timeout the sprint-design Performance table specifies. The timeout is a bare param on `RunConfiguredValidation`; a Phase-5 caller that passes `0` gets `context.WithTimeout(ctx, 0)` → immediate `DeadlineExceeded` → every validation reports `TimedOut` (fail-closed, but silently mislabeled).
**Why accepted:** Fails closed. Phase 5 owns the `--auto-fix` config surface and gate wiring, where the default naturally lives; Phase 2's runner correctly treats the timeout as a caller-supplied parameter.
**Fix in:** Phase 5 (Story 6 wiring) — add a `defaultValidationTimeout` (~2 min) constant + resolver (e.g. `ResolveValidateTimeout`) and document the `validate_command`/timeout config keys so the gate inherits a defined default rather than relying on each call site.

## TD-006 — Validation timeout leaves orphaned grandchild processes (MEDIUM)
**Origin:** Phase 2, task 2.5.A adversarial review, 2026-07-03
**File:** internal/verify/localvalidate.go:81
**Issue:** `exec.CommandContext` SIGKILLs only the direct child on timeout. A configured validation command that spawns subprocesses (e.g. `sh -c "make ..."`) leaves grandchildren orphaned and still running after the deadline; `cmd.WaitDelay` only force-closes the parent's pipe fds so `Run` returns, it does not reap the process group. A timed-out build/test validation can leave subprocesses holding CPU or locks.
**Mitigation this sprint:** `cmd.WaitDelay = 2s` guarantees `RunConfiguredValidation` itself returns promptly and never stalls `--auto-fix` indefinitely — the core Story-2 requirement is met; only orphan reaping is missing.
**Fix in:** A later hardening pass — set `cmd.SysProcAttr` `Setpgid: true` and kill the whole group (`syscall.Kill(-pid, SIGKILL)`) on the cancel/timeout path (unix build-tagged) so grandchildren are reaped too.

## TD-007 — StartError class polluted by ErrWaitDelay / context.Canceled (LOW)
**Origin:** Phase 2, task 2.5.A adversarial review, 2026-07-03
**File:** internal/verify/localvalidate.go:106
**Issue:** A command that exits 0 but whose child lingers past `WaitDelay` makes `cmd.Run` return `exec.ErrWaitDelay` (not `*exec.ExitError`); a parent-context `context.Canceled` (non-deadline) is likewise not `DeadlineExceeded`. Both fall through the `errors.As(*exec.ExitError)` guard into the catch-all StartError branch and are reported as "command not found or not executable", polluting the StartError class (meant to distinguish "cannot validate" from "validation failed").
**Why accepted:** Fails closed (`Passed()==false`, no unsafe behavior). Not reachable via the `--auto-fix` bounded-timeout path — a deadline hit is caught as `TimedOut` before this branch; it requires a zero-exit command that backgrounds a pipe-holding child, or an external parent-ctx cancel. LOW.
**Fix in:** A later pass — treat `exec.ErrWaitDelay` as a completed-but-failed run and handle `context.Canceled` alongside `DeadlineExceeded`; reserve StartError for genuine start failures (`errors.Is` `exec.ErrNotFound` / `os.ErrPermission`).

## TD-009 — Revert restores file content but not file mode (LOW)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-07-03
**File:** internal/autofix/revert.go:41
**Issue:** `RevertPatch` restores pre-patch bytes via `atomicfs.CopyPath`, but `copyFile` opens the existing target with `O_TRUNC` and ignores its `perm` argument for an already-existing file, so a file whose original mode was 0755 (executable) or 0600 comes back as the 0644 the apply step wrote. The `.bak` does carry the original mode (`BackupToDotBak` copies with `info.Mode().Perm()`), so the information to restore it is available; it is simply not re-applied.
**Why accepted:** Out of AC scope — ACs 03-02/03-04 specify byte-for-byte *content* restoration, which holds. The auto-fix target corpus is 0644 Go source, so a mode regression is not reachable for the intended use case. LOW.
**Fix in:** A later hardening pass — after a successful `copyPathFn` restore, `os.Chmod(target, bakMode)` from the backup's mode (or stat the `.bak`); add an executable-fixture mode-fidelity regression test.

## TD-011 — Non-idempotent POST retry can yield a spurious 422 collision (LOW)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-07-03
**File:** internal/ghaction/client.go:180
**Issue:** `sendDo` retries on transport error / 5xx for all verbs. If `CreateBranch`'s `POST /git/refs` (or a `CreateCommit` sub-POST) succeeds server-side but its response is lost, the retry receives GitHub's 422 "Reference already exists" — indistinguishable from a genuine name collision. The Story-6 caller's collision policy (suffix-and-retry) could then create a redundant branch, or a retried `POST /git/commits` could leave an orphan duplicate commit object.
**Why accepted:** Inherent to the reused `postDo`/`sendDo` retry plumbing shared by every existing mutating endpoint (check-runs, comments) — not introduced by Story 4. The window (lost response on a first-try success) is narrow, orphan blob/commit objects are inert and GitHub-GC'd, and AC 04-02 already delegates collision policy to the caller. Redesigning retry idempotency (e.g. dedup by pre-checking ref existence) is out of Phase 4 scope.
**Fix in:** A later hardening pass — document the spurious-422 possibility on `CreateBranch`'s contract, or gate retry-on-5xx to idempotent verbs and have the Story-6 orchestrator pre-check ref existence before a suffix-retry so a lost-response 422 is not misread as a real collision.

## TD-010 — RevertPatch does not re-check path containment at the revert boundary (LOW)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-07-03
**File:** internal/autofix/revert.go:41
**Issue:** `RevertPatch` is exported and trusts every path in the `BackupMap` it is handed, calling `copyPathFn`/`removeFn` with no independent containment re-check. Its safety depends entirely on the upstream invariant that the map was produced by `ApplyPatch` (whose `containedPath` validates every target stays inside root). A hand-built or corrupted map with a target/backup outside the working-tree root would copy or delete outside it.
**Why accepted:** In the real flow the map is always apply-produced and already `containedPath`-validated; this is defense-in-depth, not a reachable bug. The write-side already carries the belt-and-suspenders re-check (`apply.go` `containedPath`). LOW.
**Fix in:** A later pass — either re-assert `contains(root, target)` at the revert boundary (mirroring apply's defense-in-depth, which requires threading `root` into `RevertPatch`), or document `RevertPatch`'s "apply-produced map only" precondition explicitly on the exported signature.
