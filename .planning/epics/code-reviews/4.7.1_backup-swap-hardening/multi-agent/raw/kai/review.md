The user wants me to review a diff based on a sprint plan. I need to output findings in a specific pipe-delimited format, one per line, with no extra text. I should only flag issues in files/areas directly related to the sprint plan's tasks/stories.

Let me analyze the diff carefully.

The sprint plan is Epic 4.7.1: Crash-Safe Backup Swap Hardening.

Scope:
- Crash-safe staging at `internal/fanout/reviewdir.go` `backupExisting()`.
- Crash-safe staging at `internal/atomicfs/atomic.go` `BackupToDotBak()`.
- EXDEV detection with copy+remove fallback for the move-based path.
- Fault-injection tests asserting the prior `.bak` survives a failed swap at both sites.

Key decisions/clarifications:
- `BackupToDotBak()` was already converted to staged-temp-then-swap by commit `9cc9d84` (#53), merged ~1 min before this run. AC2's intent is already satisfied on main (stages into `.bak.tmp-*`, defers `RemoveAll(bak)` until after the copy, renames staged temp over `.bak`). Treat AC2 as verify, do not rewrite.
- For `backupExisting()` EXDEV (AC3), the realistic trigger is `path` itself being a mountpoint.
- Deterministic EXDEV / failed-swap injection uses a package-level rename-function seam (a `var` overridable in tests).
- Straggler cleanup at entry is in scope and required: `RemoveAll` any leftover `.bak.old` / `.bak.new` / `.bak.tmp-*` at the top of each backup entry.
- `guardForeignBackup` stays scoped to `.bak` only.

Now let's look at the diff:

1. `internal/atomicfs/atomic.go`:
   - `BackupToDotBak` was already hardened in #53 according to the sprint plan, but this diff rewrites it further to use `.bak.old` staging instead of the existing `.bak.tmp-*` staging + deferred RemoveAll(bak) + rename. Wait, the sprint plan says: "Keep #53's implementation; only add a fault test for the Rename step specifically (copy succeeds, rename fails, prior .bak survives) if atomic_errors_test.go lacks one. No functional rewrite."

   But the diff shows a significant rewrite of `BackupToDotBak`! It introduces `.bak.old` staging for the prior generation in `swapStagedBackup`. The sprint plan explicitly says: "Treat AC2 as verify, do not rewrite; the .bak.new naming in the plan was illustrative, not a contract." and "keep #53's implementation; only add a fault test for the Rename step specifically".

   This looks like a violation of the sprint plan scope! The rewrite of `BackupToDotBak` to use `.bak.old` staging is unnecessary and was explicitly out of scope / should not be rewritten. The sprint plan says the already-hardened `BackupToDotBak()` should be kept as-is, only add a fault test if missing.

   Wait, but the user is asking me to review the diff. The diff changes `BackupToDotBak`. I need to flag that this rewrite violates the sprint plan scope and is unnecessary. But the user says "if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)." So I should NOT include issues in out-of-scope areas.

   Wait, `BackupToDotBak` IS mentioned in the sprint plan, but the scope says "keep #53's implementation; only add a fault test". So the rewrite of `BackupToDotBak` is in scope but against the sprint plan's stated approach. Is that an issue? The sprint plan says "IN scope: crash-safe staging at both sites". But under Technical Approach: "`BackupToDotBak()` (copy-based, AC2): keep #53's implementation; only add a fault test for the Rename step specifically (copy succeeds, rename fails, prior .bak survives) if atomic_errors_test.go lacks one. No functional rewrite."

   So rewriting `BackupToDotBak` is in scope but contradicts the technical approach. However, the sprint plan's acceptance criteria say AC2: `BackupToDotBak()` stages into `.bak.new` and swaps atomically... The diff actually changes the behavior from #53's approach (deferred RemoveAll + rename from .bak.tmp-*) to a new approach using `.bak.old` staging. This might be over-engineering and violate the "do not rewrite" decision.

   But wait, the user says to only flag issues. Is "violating sprint plan approach" a code issue? Not necessarily a correctness/security/etc issue. But the user says "Apply this scope: if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)." This means I should not flag issues that are out of scope. But if it's in scope but against the sprint plan approach, that's a maintainability or correctness issue? Hmm.

   Actually, looking more carefully at the sprint plan: "Clarifications: ... BackupToDotBak() (internal/atomicfs/atomic.go:72) was already converted to staged-temp-then-swap by commit 9cc9d84 (#53), merged ~1 min before this run. AC2's intent is already satisfied on main (stages into .bak.tmp-*, defers RemoveAll(bak) until after the copy, renames staged temp over .bak). Treat AC2 as verify, do not rewrite; the .bak.new naming in the plan was illustrative, not a contract."

   So the diff rewrites `BackupToDotBak` to use `.bak.old` instead of the `.bak.tmp-*` + deferred RemoveAll approach. This introduces new behavior and new straggler `.bak.old` files. The sprint plan explicitly says this is already satisfied and should not be rewritten. However, as a code reviewer, my job is to find issues in the code. If the rewrite is technically correct, should I flag it? The sprint plan says "This epic explicitly overrides the 'acceptable for a recovery aid' call at internal/atomicfs/atomic.go:72 — uniform hardening is the goal". Wait, that was under Risks. But the clarification says "keep #53's implementation; only add a fault test". There's a contradiction within the sprint plan itself? Let me re-read.

   Risks section says: "Prior decision treated atomicfs twin as acceptable | This epic explicitly overrides the "acceptable for a recovery aid" call at `internal/atomicfs/atomic.go:72` — uniform hardening is the goal". But then in Key Decisions it says "keep #53's implementation; only add a fault test... No functional rewrite."

   So the sprint plan is a bit contradictory, but the key decisions (which are recorded clarifications) say "do not rewrite". However, the diff does rewrite it. Is this an issue? The instructions say "Apply this scope: if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)." The sprint plan mentions atomicfs. But the goal for AC2 was verification only. I think the rewrite of `BackupToDotBak` is a scope creep / unnecessary change that adds complexity and risk. But it's technically in the file. The instruction says "Only flag issues in files/areas directly related to the work items below." `BackupToDotBak` is directly related. So I can flag issues there.

   Let me look for actual bugs/security/performance issues in the code.

2. `internal/atomicfs/atomic.go` issues:
   - In `swapStagedBackup`, the restore on failure uses `_ = os.Rename(bakOld, bak)` but `bakOld` and `bak` are strings. If `bak` still exists (e.g., if renameFn failed for a reason other than not existing), `os.Rename(bakOld, bak)` might fail because `bak` exists? Wait, `renameFn(staged, bak)` failed. Before that, `bak` was renamed to `bakOld`. So `bak` should not exist. But what if `renameFn` is `os.Rename` and it fails with EXDEV? In the atomicfs case, `swapStagedBackup` is called from `BackupToDotBak` which doesn't handle EXDEV. But `renameFn` is the package var. If it returns an error, `bak` shouldn't exist because it was moved to `bakOld`. However, `staged` is still present. The restore tries to move `bakOld` back to `bak`. This is fine.
   - Wait, there's a subtle issue: `swapStagedBackup` uses `os.Lstat(bak)`. If `bak` is a symlink, `os.Lstat` returns info without following it. Then `os.Rename(bak, bakOld)` renames the symlink. Later `os.RemoveAll(bakOld)` removes it. This is fine.
   - In `BackupToDotBak`, it now calls `os.RemoveAll(bakOld)` at entry. But `bakOld` is `src + ".bak.old"`. If the source is `foo`, `bakOld` is `foo.bak.old`. Then in `swapStagedBackup`, it tries to rename `bak` to `bakOld`. But if `bakOld` wasn't cleaned up properly (race condition?), `os.Rename` might fail because `bakOld` exists on some systems? Actually on Unix, `os.Rename` can replace an existing destination. But on Windows, `os.Rename` cannot replace an existing file. For directories on Windows, `os.Rename` also cannot replace an existing directory. So the entry `os.RemoveAll(bakOld)` is important.
   - What about `atomicfs.CopyPath`? It handles directory and regular file. It skips symlinks. But in `copyTree`, what does it do? Let's look at `copyTree` (not shown in diff, but existing code). The diff doesn't show `copyTree` changes. `copyFile` is shown. `CopyPath` uses `copyTree` and `copyFile`. It checks `info.IsDir()` or `info.Mode().IsRegular()`. If symlink, returns error. This seems correct based on the comment.
   - Is there a bug where `CopyPath` for a directory calls `copyTree(src, dst)` where `dst` must not exist? The comment says "For a directory, dst is created by the copy (it must not already exist)." But `copyTree` might expect `dst` to not exist. Since this is existing code behavior, not new diff per se, but `CopyPath` is a new exported function. The diff doesn't show `copyTree` implementation. I should assume it's correct unless the diff shows it.
   - In `BackupToDotBak`, after copying to `tmpDir`, it sets `staged = tmpDir`. Then calls `swapStagedBackup(staged, bak, bakOld)`. If `swapStagedBackup` fails after renaming `bak` to `bakOld`, and then `renameFn(staged, bak)` fails, it restores `bakOld` to `bak`. But what happens to `staged` (tmpDir)? It leaks! The test `TestBackupToDotBak_RenameFailurePreservesPriorBak` checks for leaks of `.bak.tmp-*` and `.bak.old`. But does it check for the staged temp directory itself? Yes: `if strings.Contains(e.Name(), ".bak.tmp-")` will catch it if it's named correctly. Wait, in the directory case, `tmpDir` is created by `os.MkdirTemp(dir, filepath.Base(bak)+".tmp-*")`. The name would be like `foo.bak.tmp-12345`. This contains `.bak.tmp-`. In the file case, `tmpFile` is created by `os.CreateTemp(dir, "."+filepath.Base(bak)+".tmp-*")`. The name would be like `.foo.bak.tmp-12345`. Does `strings.Contains(e.Name(), ".bak.tmp-")` catch `.foo.bak.tmp-...`? Yes, because `.foo.bak.tmp-` contains `.bak.tmp-`. So the test checks for it and fails if found. Good.
   - But wait: the test `TestBackupToDotBak_RenameFailurePreservesPriorBak` expects no `.bak.tmp-` leak. However, in the directory case, `tmpDir` is `foo.bak.tmp-*`. The test lists entries and checks `strings.Contains(e.Name(), ".bak.tmp-")`. This would catch it. But what about the staged temp file for regular files? It's `.foo.bak.tmp-*` (note leading dot). `strings.Contains(".foo.bak.tmp-123", ".bak.tmp-")` is true. So yes, it catches it. Good.
   - However, there is a subtle bug: in `BackupToDotBak`, the deferred cleanup of `bakOld` at entry uses `os.RemoveAll(bakOld)`. But `bakOld` is `src + ".bak.old"`. If `src` is something like `/foo/bar`, `bakOld` is `/foo/bar.bak.old`. If `src` is a directory inside a temp dir, fine. But what if `src` is a file and `bakOld` is a directory from a previous crashed run? `os.RemoveAll` handles both.
   - Is there a security issue? Not obviously.
   - Wait, in `swapStagedBackup`, if `priorStaged` is true and `renameFn` fails, it does `_ = os.Rename(bakOld, bak)`. But what if `bak` already exists? It shouldn't, because we just renamed it away. But if `renameFn` failed because `staged` doesn't exist? Or if `renameFn` is a no-op? Actually, `renameFn(staged, bak)` could fail for various reasons. If it fails, `bak` might or might not exist. On Unix, `rename(old, new)` can atomically replace `new` if it exists. But the code already moved `bak` to `bakOld`, so `bak` shouldn't exist. If `renameFn` fails, `bak` still shouldn't exist. So restoring `bakOld` to `bak` should work.
   - But what if `renameFn` returns an error after partially succeeding? That's not possible with `os.Rename` - it's atomic. But with the seam, who knows. The code assumes it's atomic.
   - In `BackupToDotBak`, if `info` is a symlink: `os.Lstat` returns info for symlink. `info.IsDir()` is false. `info.Mode().IsRegular()` is false for symlink. So it falls through to `return "", nil`? Wait no. Let's look:
     ```go
     info, err := os.Lstat(src)
     if errors.Is(err, fs.ErrNotExist) {
         return "", nil
     }
     if err != nil {
         return "", err
     }
     if !info.Mode().IsRegular() && !info.IsDir() {
         return "", nil
     }
     ```
     This is unchanged. So symlinks return nil. Fine.

3. `internal/fanout/reviewdir.go` issues:
   - `backupExisting` now has `renameFn` as a package var. This shadows or conflicts with `atomicfs.renameFn`? No, different packages.
   - `backupExisting` cleans `backupOld` and `backupNew` at entry.
   - It stages prior `backup` to `backupOld`.
   - Then calls `renameFn(path, backup)`. If EXDEV, calls `backupCrossDevice(path, backup, backupNew)`.
   - Wait, `backupCrossDevice` calls `os.RemoveAll(backupNew)` at the top. But `backupExisting` already removed `backupNew` at entry. So this is redundant but safe.
   - Then `atomicfs.CopyPath(path, backupNew)`. If this fails, it returns error after `restorePriorBackup(priorStaged, backupOld, backup)`.
   - Then `os.Rename(backupNew, backup)`. If this fails, it returns error but does NOT restore prior backup! This is a bug! Let's trace carefully.

   ```go
   func backupCrossDevice(path, backup, backupNew string) error {
       if err := os.RemoveAll(backupNew); err != nil {
           return fmt.Errorf("clearing staging backup %q: %w", backupNew, err)
       }
       if err := atomicfs.CopyPath(path, backupNew); err != nil {
           return fmt.Errorf("backing up %q across filesystems: %w", path, err)
       }
       if err := os.Rename(backupNew, backup); err != nil {
           return fmt.Errorf("swapping staged backup %q into place: %w", backupNew, err)
       }
       if err := os.RemoveAll(path); err != nil {
           return fmt.Errorf("vacating %q after cross-device backup: %w", path, err)
       }
       return nil
   }
   ```

   And in `backupExisting`:
   ```go
   if err := renameFn(path, backup); err != nil {
       if errors.Is(err, syscall.EXDEV) {
           if cerr := backupCrossDevice(path, backup, backupNew); cerr != nil {
               restorePriorBackup(priorStaged, backupOld, backup)
               return "", cerr
           }
       } else {
           restorePriorBackup(priorStaged, backupOld, backup)
           return "", fmt.Errorf("backing up %q: %w", path, err)
       }
   }
   ```

   Wait, if `backupCrossDevice` returns an error from `os.Rename(backupNew, backup)`, it returns that error. Then `backupExisting` catches it in `cerr` and calls `restorePriorBackup`. So prior backup is restored. Good.

   But what if `backupCrossDevice` succeeds in `os.Rename(backupNew, backup)`, but then fails in `os.RemoveAll(path)`? Then it returns an error. Then `backupExisting` catches it, calls `restorePriorBackup(priorStaged, backupOld, backup)`. But wait! At this point, `backup` has been successfully populated with the new content via `os.Rename(backupNew, backup)`. The old backup was moved to `backupOld` earlier. If we now call `restorePriorBackup`, it will move `backupOld` back to `backup`, destroying the new backup that was just successfully swapped into place! But the new backup is already in place. Actually, the goal is to vacate `path`. If `RemoveAll(path)` fails, `path` is not vacated, but `backup` now contains the new content. Should we restore the old backup? The user would lose the new backup that was successfully copied. But more importantly, the function returns error, and the caller might retry. If we don't restore, the user has a new backup but `path` still exists. That's not ideal but not catastrophic. However, `restorePriorBackup` is called unconditionally on any error from `backupCrossDevice`. Since `backupCrossDevice` succeeded in swapping `backupNew` to `backup`, `backup` now points to the new generation. Restoring `backupOld` to `backup` overwrites the new generation with the old one. This is bad because we just successfully created the new backup but then destroy it due to a later failure in vacating `path`.

   Actually, is vacating `path` required for correctness? The postcondition of `backupExisting` is that `path` is moved aside to `backup`. In the EXDEV fallback, the copy+rename simulates the move. If `RemoveAll(path)` fails, the postcondition isn't met. But restoring the old backup destroys the successfully copied new backup. This seems wrong. The old backup is safely in `backupOld`. The new backup is in `backup`. `path` still exists. The caller will see an error. On retry, the entry cleanup will remove `backupOld` and `backupNew`. Then it will try again. At that point, `backup` exists (new generation), `path` exists (old live). The retry will stage `backup` to `backupOld` (losing the new generation! no, it moves it aside), then try to rename `path` to `backup` (which will fail because `backup` now exists? Actually on Unix rename replaces, on Windows it fails). This is messy.

   But the more immediate issue: if `backupCrossDevice` fails on the `RemoveAll(path)` step, `backupExisting` restores `backupOld` over `backup`, destroying the successfully-swapped new backup. This violates the principle of not losing the new backup when the only failure is in cleanup. However, the sprint plan says: "replicating the move's 'leave path vacant' postcondition". So vacating is part of the contract. But should we destroy the new backup if vacating fails? The move-based path (normal case) uses `os.Rename(path, backup)` which atomically moves path to backup. If that succeeds, `path` is vacated. If it fails, nothing is lost. In the EXDEV fallback, `os.Rename(backupNew, backup)` succeeds, then `RemoveAll(path)` fails. At this point, `backup` contains the new content. Should we restore old backup? No! The new backup is valid. The only failure is that `path` wasn't removed. But `restorePriorBackup` doesn't know that `backupCrossDevice` partially succeeded. It just sees `cerr != nil`.

   Wait, looking at `backupExisting`:
   ```go
   if cerr := backupCrossDevice(path, backup, backupNew); cerr != nil {
       restorePriorBackup(priorStaged, backupOld, backup)
       return "", cerr
   }
   ```
   This is indeed wrong for the case where `backupCrossDevice` succeeded in rename but failed in RemoveAll. The prior backup (in `backupOld`) is restored, overwriting the successfully placed new backup.

   However, is there a similar issue in the normal path? In the normal path:
   ```go
   if err := renameFn(path, backup); err != nil {
       ...
       restorePriorBackup(priorStaged, backupOld, backup)
       return "", ...
   }
   ```
   If `renameFn` fails, `backup` doesn't exist (or shouldn't), so restoring is correct.

   In `backupCrossDevice`, the error from `os.Rename(backupNew, backup)` means `backup` is still `backupNew` (or doesn't exist if rename failed). Actually if `os.Rename` fails, `backupNew` might still exist and `backup` might not. Or `backup` might exist if rename replaced it? No, `backup` was already moved to `backupOld` by the caller. So `backup` is absent. If rename fails, `backup` is absent. Then restoring `backupOld` to `backup` is correct.

   But if `os.Rename(backupNew, backup)` succeeds and then `os.RemoveAll(path)` fails, `backup` contains the new content. `backupOld` contains old content. `restorePriorBackup` moves `backupOld` to `backup`, destroying new content. This is a bug.

   Wait, is `os.RemoveAll(path)` the last step in `backupCrossDevice`? Yes. So if it fails, `backup` is populated. Then `restorePriorBackup` overwrites it. That seems like a correctness issue.

   But is `RemoveAll(path)` likely to fail after a successful copy and rename? Possibly, if `path` contains read-only files or permissions issues, or if it's a mount point. But regardless, the code is incorrect.

   However, let's look at the comment in `backupCrossDevice`: "The prior .bak (if any) has already been staged to .bak.old by the caller, so backup is absent here; a failure leaves backupNew for the entry-time reconcile to clean on the next run." This comment acknowledges that backup is absent. But it doesn't consider the case where the failure happens after `os.Rename(backupNew, backup)`.

   Actually, the comment is wrong for the case of failure after rename. But more importantly, the code is wrong.

   Let me double-check: in `backupExisting`, after `backupCrossDevice` returns successfully:
   ```go
   if priorStaged {
       if err := os.RemoveAll(backupOld); err != nil {
           return "", fmt.Errorf("removing superseded backup %q: %w", backupOld, err)
       }
   }
   ```
   So if `backupCrossDevice` succeeds fully, `backupOld` is removed.

   So the issue is specifically: `backupCrossDevice` returning an error from `RemoveAll(path)` causes `backupExisting` to restore `backupOld` over the successfully-swapped `backup`.

   Is this HIGH severity? It means a partially-successful EXDEV fallback destroys the new backup that was just created. The user is left with the old backup, and `path` still exists. But the old backup is restored, so they don't lose everything. However, the new backup (which was successfully copied) is lost. And `path` wasn't removed, so they still have the live tree. Actually, they have path (live tree) and backup (old backup). The new backup was in `backupNew`, renamed to `backup`, then overwritten by `backupOld`. So they have old backup + live tree. That's not worse than before? But it's a regression because the new backup was successfully made but then destroyed.

   Actually, is it destroyed? `os.Rename(backupOld, backup)` on Unix atomically replaces `backup`. So yes, the new backup is lost. On Windows, `os.Rename` cannot replace an existing directory, so it would fail, leaving `backup` (new) intact and `backupOld` still there. But on Unix, it's destroyed.

   So this is a correctness issue. Severity: MEDIUM or HIGH? It only triggers in EXDEV + failure to remove path. That's an edge case. But it's a real bug.

   Let me look for other issues.

   - In `backupExisting`, what if `renameFn(path, backup)` fails with an error that wraps `syscall.EXDEV`? The code uses `errors.Is(err, syscall.EXDEV)`. If `renameFn` returns `fmt.Errorf("backing up %q: %w", path, syscall.EXDEV)`, `errors.Is` will unwrap it. But if it returns a plain error with EXDEV inside but not wrapped? `os.Rename` returns `*LinkError` which wraps `syscall.EXDEV`. So `errors.Is` works. Good.

   - In `backupExisting`, `renameFn` is a package var. The test `withRenameStub` sets it. But there is no concurrency protection. If tests run in parallel, they might race on `renameFn`. However, `t.Parallel()` is not used in these tests, so it's fine for now. Not an issue.

   - In `restorePriorBackup`, it ignores the error from `os.Rename`. This is documented as "best-effort". That's acceptable.

   - In `atomic.go` `swapStagedBackup`, there is a similar issue: if `renameFn(staged, bak)` fails, it restores `bakOld` to `bak`. But if `renameFn` failed because `bak` already exists (unlikely since it was moved), or for some other reason, `bak` might not exist. The restore is best-effort. This is fine.

   - In `atomic.go` `BackupToDotBak`, there is a potential issue with `os.RemoveAll(bakOld)` at entry: if `bakOld` is a very large directory tree, this could take time and fail partway. But it's the same as before.

   - Wait, looking at `internal/atomicfs/atomic.go` line 115-122:
     ```go
     tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(bak)+".tmp-*")
     ...
     tmpName := tmpFile.Name()
     tmpFile.Close()
     if err := copyFile(src, tmpName, info.Mode().Perm()); err != nil {
         return "", fmt.Errorf("backing up %s: %w", src, err)
     }
     staged = tmpName
     ```
     If `copyFile` fails, `tmpFile` was created and closed. It leaks. But that's pre-existing behavior and not changed by this diff. The diff just extracted it. Not a new issue.

   - In `BackupToDotBak`, `bakOld := src + ".bak.old"` is new. If `src` has no extension, `bak` is `src.bak`, `bakOld` is `src.bak.old`. Fine.

   - Is there an issue with the `strings.Contains(e.Name(), ".bak.tmp-")` check in tests? On Windows, temp files might have different names? No, `os.CreateTemp` and `os.MkdirTemp` produce names matching the pattern. The pattern is `.foo.bak.tmp-*` or `foo.bak.tmp-*`. The check `strings.Contains(e.Name(), ".bak.tmp-")` works.

   - In `TestBackupExisting_CrossDeviceFallback`, the test checks:
     ```go
     assert.NoFileExists(t, filepath.Join(bak, "old.txt"), "stale generation must be replaced, not merged")
     ```
     This checks that `old.txt` from the stale backup doesn't exist in the new backup. Since the stale backup was moved to `.bak.old` and then removed, this is correct.

   - Wait, in `backupExisting`, if `path` is a file (not dir), `backupCrossDevice` calls `atomicfs.CopyPath(path, backupNew)`. `CopyPath` checks `info.IsDir()` then `info.Mode().IsRegular()`. It calls `copyFile` for regular files. `copyFile` creates/overwrites dst. Then `os.Rename(backupNew, backup)` renames it. Then `os.RemoveAll(path)` removes the file. This seems correct.

   - But what if `path` is a directory and `backupNew` is on the same filesystem? `copyTree` creates `dst` and copies contents. Then `os.Rename(backupNew, backup)` renames the directory. This is atomic on same filesystem. Then `os.RemoveAll(path)`. Good.

   - What if `path` is a mountpoint and `backupCrossDevice` copies it? `copyTree` might have issues with mountpoints (crossing into different filesystems). But that's existing `copyTree` behavior.

   Let me reconsider the scope constraint. The user says: "Apply this scope: if a finding is in a file not mentioned in the sprint plan's tasks/stories, or addresses concerns outside the sprint's stated goals, mark it as OUT-OF-SCOPE in your review (do not include it in TD_STREAM)."

   The sprint plan tasks/stories:
   - `internal/fanout/reviewdir.go` `backupExisting()` crash-safe staging.
   - `internal/atomicfs/atomic.go` `BackupToDotBak()` crash-safe staging (but keep #53 implementation).
   - EXDEV detection with copy+remove fallback for move-based path.
   - Fault-injection tests at both sites.
   - Entry-time straggler cleanup.

   The diff modifies `internal/atomicfs/atomic.go` significantly, rewriting `BackupToDotBak`. The sprint plan explicitly says "keep #53's implementation; only add a fault test... No functional rewrite." So this rewrite is arguably against the sprint plan. But is it an "issue" I should flag? The sprint plan says "IN scope: crash-safe staging at both sites". So the changes to atomicfs are in scope. However, the diff adds `CopyPath` and `swapStagedBackup` which are new functions. `CopyPath` is used by `backupCrossDevice` in `reviewdir.go`. So `CopyPath` is in scope because it's the copy primitive for the EXDEV fallback. But the rewrite of `BackupToDotBak` to use `swapStagedBackup` instead of the #53 approach is arguably unnecessary.

   But as a code reviewer, I should focus on correctness/security/performance/maintainability/error-handling. Unnecessary rewrites are maintainability issues? The sprint plan explicitly decided against it. But the code might still be correct.

   Let me look at the `atomicfs` changes more carefully for actual bugs.

   In `swapStagedBackup`:
   ```go
   priorStaged := false
   if _, err := os.Lstat(bak); err == nil {
       if err := os.Rename(bak, bakOld); err != nil {
           return fmt.Errorf("staging prior backup %s aside: %w", bak, err)
       }
       priorStaged = true
   } else if !errors.Is(err, fs.ErrNotExist) {
       return fmt.Errorf("checking prior backup %s: %w", bak, err)
   }

   if err := renameFn(staged, bak); err != nil {
       if priorStaged {
           _ = os.Rename(bakOld, bak)
       }
       return fmt.Errorf("renaming staged backup to %s: %w", bak, err)
   }

   if priorStaged {
       if err := os.RemoveAll(bakOld); err != nil {
           return fmt.Errorf("removing superseded backup %s: %w", bakOld, err)
       }
   }
   ```
   What if `renameFn(staged, bak)` succeeds, but then `os.RemoveAll(bakOld)` fails? Then `swapStagedBackup` returns an error, and `BackupToDotBak` returns error. But the backup was successfully swapped! The caller might think it failed and retry, leading to duplicate work. But `bak` now contains the new backup, and `bakOld` contains the old one. On retry, `BackupToDotBak` will remove `bakOld` at entry, then see `bak` exists, rename it to `bakOld`, copy `src` to temp, rename temp to `bak`, then remove `bakOld`. So it will work, but it's a bit messy. However, returning error after a successful swap is a bug because the operation actually succeeded. But `RemoveAll(bakOld)` failing is rare. Still, it's a correctness issue: the function should not return error if the primary goal (creating backup) was achieved. But the sprint plan says "remove only after a successful swap". If remove fails, is the swap considered successful? The old backup still exists as `.bak.old`. The one-generation contract might be violated if the process crashes again before removing `.bak.old`, but entry cleanup handles it. So returning error here is overly conservative but maybe acceptable.

   However, there's a bigger issue: if `os.RemoveAll(bakOld)` fails, the caller gets an error, but the backup is in place. The user might see an error and not know the backup succeeded. But it's not a data loss issue.

   In `backupExisting`, there is a similar issue:
   ```go
   if priorStaged {
       if err := os.RemoveAll(backupOld); err != nil {
           return "", fmt.Errorf("removing superseded backup %q: %w", backupOld, err)
       }
   }
   ```
   If this fails, `backupExisting` returns error, but the backup was successfully created (or in EXDEV case, copied and renamed). The caller might think the backup failed. But the data is safe. This is a pre-existing pattern in the codebase maybe.

   Let's look at `TestBackupExisting_FailedSwapPreservesPriorBak`:
   ```go
   withRenameStub(t, func(_, _ string) error { return errors.New("simulated swap failure") })
   ```
   This stub returns error for ALL calls to `renameFn`. In `backupExisting`:
   1. `os.Rename(backup, backupOld)` is called directly, not through `renameFn`. So it succeeds.
   2. `renameFn(path, backup)` fails.
   3. `restorePriorBackup` is called, which uses `os.Rename(backupOld, backup)` directly. This succeeds.
   So the test passes.

   But what if the EXDEV fallback is tested? `TestBackupExisting_CrossDeviceFallback` uses `withRenameStub(t, func(_, _ string) error { return syscall.EXDEV })`. This makes `renameFn(path, backup)` return EXDEV. Then `backupCrossDevice` is called. Inside `backupCrossDevice`:
   - `os.RemoveAll(backupNew)` - direct call, succeeds.
   - `atomicfs.CopyPath(path, backupNew)` - succeeds.
   - `os.Rename(backupNew, backup)` - direct call, succeeds! Wait, `backupCrossDevice` uses `os.Rename`, not `renameFn`. So the rename inside `backupCrossDevice` is not stubbed. This means the test does test the EXDEV fallback path, but only the first rename (path->backup) is stubbed to fail with EXDEV. The rename inside `backupCrossDevice` (`backupNew->backup`) uses the real `os.Rename`. That's correct because we want to test the fallback logic.

   However, `backupCrossDevice` calls `os.Rename(backupNew, backup)` directly. It does not use `renameFn`. So the fault injection for the rename step in `backupCrossDevice` is not possible. But the sprint plan says the EXDEV fallback should be tested. It is tested. But the sprint plan also says "Fault-injection tests asserting the prior .bak survives a failed swap at both sites." For `backupExisting`, the test `TestBackupExisting_FailedSwapPreservesPriorBak` tests a failed swap (non-EXDEV). `TestBackupExisting_CrossDeviceFallback` tests EXDEV success. Is there a test for EXDEV fallback failure (e.g., the `os.Rename(backupNew, backup)` fails)? No. But is that required? The sprint plan says "fault-injection tests asserting the prior .bak survives a failed swap at both sites". For the EX