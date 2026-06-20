<think>
Let me carefully review this diff which is implementing Epic 4.7.1: Crash-Safe Backup Swap Hardening.

The scope is:
1. `internal/atomicfs/atomic.go` - `BackupToDotBak()` hardening
2. `internal/fanout/reviewdir.go` - `backupExisting()` hardening
3. Tests for both
4. Boundary import allowance update

Let me look at each change carefully:

**1. `internal/atomicfs/atomic.go` - `BackupToDotBak()`:**

The function now:
- Removes stale `.bak.old` at entry
- Stages the copy into a temp sibling
- Calls `swapStagedBackup(staged, bak, bakOld)` to do the atomic swap
- Returns the backup path

`swapStagedBackup`:
- Checks if prior bak exists
- If so, renames bak to bakOld (staging prior aside)
- Renames staged to bak (the swap)
- On failure, restores bakOld to bak
- On success, removes bakOld

`renameFn = os.Rename` - package var seam for tests

`CopyPath` - new exported function for EXDEV fallback in fanout

Let me think about issues:
- The `renameFn` is in the `atomicfs` package
- The `renameFn` is ALSO in the `fanout` package (different package, so they're separate vars)
- Wait, both packages have their own `renameFn = os.Rename`? That seems redundant but not a bug since they're separate packages.
- The `swapStagedBackup` ignores the error from the restore rename - that's documented and intentional.

Wait, let me look at the logic of `swapStagedBackup` more carefully:

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
```

This looks correct. If bak doesn't exist, priorStaged stays false.

```go
if err := renameFn(staged, bak); err != nil {
    if priorStaged {
        _ = os.Rename(bakOld, bak)
    }
    return fmt.Errorf("renaming staged backup to %s: %w", bak, err)
}
```

This looks correct. On failure, if we had staged the prior, restore it.

```go
if priorStaged {
    if err := os.RemoveAll(bakOld); err != nil {
        return fmt.Errorf("removing superseded backup %s: %w", bakOld, err)
    }
}
```

Wait - there's an issue here. After a successful rename, we have:
- `bak` contains the new staged content
- `bakOld` contains the prior content

If `os.RemoveAll(bakOld)` fails, we return an error. But at this point, the swap has actually succeeded. The caller (BackupToDotBak) will return the error and the caller may think the whole thing failed. But the new .bak IS in place. This is a minor concern - the error message is "removing superseded backup" which is clear. The user has the new backup; the old one is just a duplicate still around. This is acceptable behavior (the .bak.old will be cleaned up on the next entry-time reconcile, or by the user).

Wait, but this means the BackupToDotBak caller gets an error. Let me check:

```go
if err := swapStagedBackup(staged, bak, bakOld); err != nil {
    return "", err
}
return bak, nil
```

If swapStagedBackup returns an error from RemoveAll of bakOld, BackupToDotBak returns "" with error. But the swap actually succeeded! The new .bak is in place. The caller doesn't know that.

Hmm, but if the caller (e.g., forceBackupReviewDir) checks the error, it may incorrectly think the backup failed. Let me check the caller chain... actually the caller chain isn't in this diff. But this could be an issue.

Actually, looking more carefully, after a successful rename, the new content is at `bak`. If RemoveAll(bakOld) fails, we have a redundant .bak.old still around but the new .bak is correctly in place. The error returned to the caller is misleading. This is a maintainability concern.

Actually let me think about this more. The caller may:
1. Continue assuming the backup succeeded and delete the original
2. Not continue, leaving the original

In `forceBackupReviewDir` (not in this diff but called from reviewdir.go), the chain is:
- `forceBackupReviewDir(dir)` calls `forceBackupOutputDir(dir)` calls `backupExisting(dir)` (if there's a prior reviewdir)
- After backupExisting returns, the caller presumably scaffolds a fresh dir

If backupExisting returns an error, the caller (probably) bails out. So returning an error here is the safer default. The downside is that if the swap actually succeeded and only the cleanup failed, the caller doesn't know.

This is a subtle issue but might be worth flagging as MEDIUM or LOW. The error message is clear though.

**2. `internal/fanout/reviewdir.go` - `backupExisting()`:**

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

Wait - this is a bug! When EXDEV occurs and `backupCrossDevice` succeeds, the code falls through to the cleanup below:

```go
if priorStaged {
    if err := os.RemoveAll(backupOld); err != nil {
        return "", fmt.Errorf("removing superseded backup %q: %w", backupOld, err)
    }
}
```

This is correct for the EXDEV success case - the prior .bak.old needs to be removed.

But wait, in the EXDEV success case, `backupCrossDevice` calls `atomicfs.CopyPath(path, backupNew)` which COPIES the path content to backupNew, then renames backupNew to backup, then RemoveAll(path). The prior .bak is already in backupOld (staged by the earlier code). So the flow:
1. Stage prior .bak to .bak.old ✓
2. Try to rename path → backup: EXDEV
3. Call backupCrossDevice: copy path → backupNew, rename backupNew → backup, RemoveAll(path) ✓
4. Fall through to cleanup: RemoveAll(backupOld) ✓

That looks right.

But wait, I see a potential issue. After `backupCrossDevice` succeeds:
- `backup` now has the new content (from path)
- `backupOld` has the prior content
- `path` is removed (vacated)

The cleanup removes backupOld. ✓

Now let me look at the case where renameFn returns EXDEV but `backupCrossDevice` fails:
- `restorePriorBackup(priorStaged, backupOld, backup)` is called - this restores backupOld → backup ✓
- Returns the error

But wait, after `restorePriorBackup`, the prior is at backup. The path is still at path. backupNew may still exist if `backupCrossDevice` partially completed. This will be cleaned up on the next retry's entry-time reconcile. OK.

Now let me look at the case where renameFn returns a non-EXDEV error:
- `restorePriorBackup(priorStaged, backupOld, backup)` is called - this restores backupOld → backup ✓
- Returns the error

Path is still at path. ✓

But wait, there's an issue: in the EXDEV success path, the code falls through to the final cleanup. Let me re-read more carefully:

```go
if err := renameFn(path, backup); err != nil {
    if errors.Is(err, syscall.EXDEV) {
        if cerr := backupCrossDevice(path, backup, backupNew); cerr != nil {
            restorePriorBackup(priorStaged, backupOld, backup)
            return "", cerr
        }
        // EXDEV success case - falls through
    } else {
        restorePriorBackup(priorStaged, backupOld, backup)
        return "", fmt.Errorf("backing up %q: %w", path, err)
    }
}

// Swap succeeded - the prior generation staged in .bak.old is now superseded.
if priorStaged {
    if err := os.RemoveAll(backupOld); err != nil {
        return "", fmt.Errorf("removing superseded backup %q: %w", backupOld, err)
    }
}
return backup, nil
```

Actually wait - in the EXDEV success case, we fall through to the cleanup. But `restorePriorBackup` is only called on failure. On EXDEV success, we don't call restorePriorBackup. That's correct - we want to keep backupOld to be cleaned up by the post-success cleanup.

OK the logic seems correct.

Now let me look at `backupCrossDevice`:

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

Wait - the call to `os.RemoveAll(backupNew)` is redundant because `backupExisting` already removed it at entry. But it's defensive. OK.

But more importantly: `atomicfs.CopyPath` doesn't create a directory if `path` is a directory. Let me check... `CopyPath` does call `copyTree` for directories. Let me look at `copyTree`:

I don't have the code for copyTree in the diff. But the function works based on the existing usage in BackupToDotBak.

Actually wait - I see an issue. `atomicfs.CopyPath(path, backupNew)` - if path is a directory, this calls `copyTree`. But what if `path` is a file? It calls `copyFile`. OK, that handles both cases.

But here's the issue: in the EXDEV case, we want to swap path → backup. The caller (backupExisting) has already staged the prior backup to backupOld. So backup is empty (we moved it to backupOld). 

But wait - what if `os.Rename(backup, backupOld)` failed? Then backup is still there. We handle that in `backupExisting` by returning the error before trying to swap. ✓

Hmm, but what if the entry-time reconcile didn't fully complete? The reconcile only does `os.RemoveAll(backupOld)` and `os.RemoveAll(backupNew)`. If the prior run had a .bak in place and crashed before staging, the .bak would still be there. But that's fine - we handle it via the Lstat check.

OK, I think the logic is correct.

Now, let me think about other issues:

1. The `renameFn` is defined in BOTH the `atomicfs` and `fanout` packages. This is a duplication but acceptable since they need to be testable independently. Not a bug.

2. In `swapStagedBackup`, the variable `priorStaged` is used to track whether we successfully moved the prior aside. The logic looks correct.

3. The `errors.Is(err, fs.ErrNotExist)` check - is `fs.ErrNotExist` the right error for os.Lstat on a missing file? Yes, `os.Lstat` returns a `*PathError` wrapping `syscall.ENOENT` which satisfies `errors.Is(err, fs.ErrNotExist)`. ✓

4. Looking at the test for failed swap:
```go
priorPath := bak
if tc.isDir {
    priorPath = filepath.Join(bak, "prior.txt")
}
```

Wait, this is wrong! For directory case, `priorPath` should still be `bak` to check the directory exists, then read `prior.txt` from inside. But the code sets `priorPath` to `filepath.Join(bak, "prior.txt")` for the directory case. Then `os.ReadFile(priorPath)` reads the file inside. This is OK as a check, but the assertion is "the prior .bak survived" - if `bak` exists as a directory, that's the assertion. Reading the file inside is a more thorough check.

Actually wait, this is the test for `tc.isDir == true`:
```go
priorPath = filepath.Join(bak, "prior.txt")
got, err := os.ReadFile(priorPath)
if err != nil {
    t.Fatalf("prior .bak did not survive failed swap: %v", err)
}
```

This reads `bak/prior.txt` to check it survived. But for the file case:
```go
priorPath := bak  // (the default from before the if)
got, err := os.ReadFile(priorPath)
```

This reads `bak` directly. For a file at `bak`, this reads the file content. OK.

The test logic is correct.

5. The boundary import allowance update:
```go
"fanout":         {"...", "atomicfs"}, // ...
```

This adds `atomicfs` to the allowed imports for `fanout`. This is needed because `fanout` now imports `atomicfs.CopyPath`. ✓

6. The doc comment for `BackupToDotBak` says "stages the prior generation aside to <src>.bak.old" - but the new code only stages prior aside after the copy completes, and only if the prior exists. The comment says "Staging the prior generation aside happens only after the copy completes, so a failed copy leaves the prior .bak intact". This is correct.

7. The `renameFn` is a package-level var. This is a known concern because tests can race if they run in parallel. Let me check if the tests use `t.Parallel()`... looking at the test, it uses `withRenameStub` and `t.Cleanup`. The tests don't use `t.Parallel()`. But there's a potential issue: if multiple tests in the same package use `renameFn` simultaneously (in different goroutines), they could interfere. The `t.Cleanup` doesn't serialize tests. But the existing tests don't use t.Parallel() either, so this is consistent with the existing pattern.

Actually, wait. The `withRenameStub` function uses `t.Cleanup` to restore the original. But the swap may not be synchronized with other tests running in parallel. This is a known test isolation issue with package-level vars, but it's a documented pattern in the sprint plan ("Deterministic EXDEV / failed-swap injection uses a package-level rename-function seam").

8. In `restorePriorBackup`:
```go
func restorePriorBackup(staged bool, backupOld, backup string) {
    if !staged {
        return
    }
    _ = os.Rename(backupOld, backup)
}
```

This silently ignores the error. The doc comment says "Best-effort: a restore failure cannot un-fail the swap, but the prior data still survives under .bak.old for manual recovery, so the error is not propagated over the swap failure that is the caller's real concern." This is correct.

9. Let me check the logic of `backupExisting` for a subtle issue:

```go
priorStaged := false
if _, err := os.Lstat(backup); err == nil {
    if err := os.Rename(backup, backupOld); err != nil {
        return "", fmt.Errorf("staging prior backup %q aside: %w", backup, err)
    }
    priorStaged = true
} else if !errors.Is(err, fs.ErrNotExist) {
    return "", fmt.Errorf("checking prior backup %q: %w", backup, err)
}
```

If `Lstat` returns an error other than ErrNotExist, we return early. But the prior `RemoveAll(backupOld)` and `RemoveAll(backupNew)` at the start have already executed. That's fine because those are clean stragglers.

OK, I think the logic is correct.

10. Let me check the test `TestBackupExisting_FailedSwapPreservesPriorBak`:
```go
withRenameStub(t, func(_, _ string) error { return errors.New("simulated swap failure") })

_, err := backupExisting(src)
require.Error(t, err, "a failed swap must surface an error")
```

The test expects an error. ✓

```go
// No staging straggler left behind.
assert.NoDirExists(t, src+".bak.old", "staging artifact must not leak after a failed swap")
```

After a failed swap, the prior was staged to .bak.old and then restored to .bak. So .bak.old should not exist. ✓

11. Let me check the test `TestBackupExisting_CrossDeviceFallback`:
```go
withRenameStub(t, func(_, _ string) error { return syscall.EXDEV })

bak, err := backupExisting(src)
require.NoError(t, err, "EXDEV must fall back to copy+remove, not error out")
```

Wait - the stub returns EXDEV for ALL renames, not just the first one. In `backupCrossDevice`:
```go
if err := os.Rename(backupNew, backup); err != nil {  // This will also return EXDEV
```

This is a problem! The stub returns EXDEV for all renames, including the `os.Rename(backupNew, backup)` in `backupCrossDevice`. But `backupNew` and `backup` are in the same directory (same filesystem), so a real rename wouldn't return EXDEV.

But the stub returns EXDEV regardless. So the test would fail because `os.Rename(backupNew, backup)` would also return EXDEV (from the stub), causing `backupCrossDevice` to return an error.

Wait, let me re-read the stub:
```go
withRenameStub(t, func(_, _ string) error { return syscall.EXDEV })
```

This sets `renameFn` to a function that always returns EXDEV. But `backupCrossDevice` uses `os.Rename` directly, not `renameFn`:
```go
func backupCrossDevice(path, backup, backupNew string) error {
    if err := os.RemoveAll(backupNew); err != nil {
        return fmt.Errorf("clearing staging backup %q: %w", backupNew, err)
    }
    if err := atomicfs.CopyPath(path, backupNew); err != nil {
        return fmt.Errorf("backing up %q across filesystems: %w", path, err)
    }
    if err := os.Rename(backupNew, backup); err != nil {  // Direct os.Rename
        return fmt.Errorf("swapping staged backup %q into place: %w", backupNew, err)
    }
    if err := os.RemoveAll(path); err != nil {
        return fmt.Errorf("vacating %q after cross-device backup: %w", path, err)
    }
    return nil
}
```

OK so `backupCrossDevice` uses `os.Rename` directly, not the package `renameFn`. So the test would work.

But wait, that's a maintainability concern! If we ever want to test the EXDEV failure mode of the swap-within-`backupCrossDevice`, we can't because it uses `os.Rename` directly, not the package var.

But for the test in question, the stub only needs to return EXDEV for the first rename (the one in `backupExisting`). The subsequent renames in `backupCrossDevice` use `os.Rename` directly, which works fine for same-fs renames.

So the test passes. OK.

12. Let me look at the test `TestBackupToDotBak_RenameFailurePreservesPriorBak`:
```go
orig := renameFn
renameFn = func(_, _ string) error { return os.ErrPermission }
defer func() { renameFn = orig }()
```

This is in the `atomicfs` package, so it uses the `atomicfs.renameFn`. ✓

After the swap fails, the prior should be restored:
```go
priorPath := bak
if tc.isDir {
    priorPath = filepath.Join(bak, "prior.txt")
}
got, err := os.ReadFile(priorPath)
```

For the file case: reads `bak` directly. The prior was written as `[]byte("prior\n")`. So `got` should be `"prior\n"`. ✓
For the dir case: reads `bak/prior.txt`. The prior was written to `bak/prior.txt` as `[]byte("prior\n")`. So `got` should be `"prior\n"`. ✓

Wait, but in the dir case, after the swap fails, the prior should be restored to `bak`. But the restore renames `bakOld` to `bak`. If `bakOld` is a directory, this works. The test seeded `bak` as a directory with `prior.txt` inside, then staged it to `bakOld` (a directory), then swap failed, then restored `bakOld` to `bak` (a directory with `prior.txt` inside). ✓

13. The test for `TestCopyPath_NonRegularSourceErrors`:
```go
err := CopyPath(link, filepath.Join(dir, "dst"))
if err == nil {
    t.Fatal("expected error for symlink source, got nil")
}
if !strings.Contains(err.Error(), "not a regular file or directory") {
    t.Errorf("error = %v, want 'not a regular file or directory'", err)
}
```

This is correct.

14. Let me check the test for `TestBackupExisting_CleansStaleStagingStragglers`:
```go
bak, err := backupExisting(src)
require.NoError(t, err)
assert.Equal(t, src+".bak", bak)
```

Wait, there's no prior .bak in this test! The test only sets up `src`, `src+".bak.old"`, and `src+".bak.new"`. There's no `src+".bak"`. So `priorStaged` is false, and the swap just renames `src` to `src+".bak"`. ✓

But the assertion `assert.Equal(t, src+".bak", bak)` checks that the returned path is `src+".bak"`. The function returns `backup, nil` at the end. ✓

15. Let me think about edge cases:

What if `os.Rename(backup, backupOld)` fails because backupOld already exists (a straggler)? But the entry-time reconcile removes backupOld first. So backupOld should not exist when we get here. ✓

What if `os.Rename(backup, backupOld)` fails for another reason? We return the error. The stragglers cleanup already happened. ✓

What if `os.Rename(staged, bak)` fails (non-EXDEV)? We try to restore. But the staged file/dir is still at `staged`. This is a leak! The test checks for `.bak.tmp-*` and `.bak.old` stragglers, not for the staged file. But the staged file has a `.bak.tmp-*` name, which is checked.

Wait, the test in `TestBackupToDotBak_RenameFailurePreservesPriorBak`:
```go
for _, e := range entries {
    if strings.Contains(e.Name(), ".bak.tmp-") {
        t.Errorf("staging temp leaked after failed swap: %s", e.Name())
    }
    if strings.HasSuffix(e.Name(), ".bak.old") {
        t.Errorf(".bak.old straggler leaked after failed swap: %s", e.Name())
    }
}
```

So it checks for `.bak.tmp-*` and `.bak.old` stragglers. If the swap fails, the staged file (which has `.bak.tmp-*` in its name) still exists. The test would fail!

Wait, let me re-read the code. The staged file is created with `os.MkdirTemp(dir, filepath.Base(bak)+".tmp-*")` or `os.CreateTemp(dir, "."+filepath.Base(bak)+".tmp-*")`. So the staged name contains `.bak.tmp-`. If the swap fails, the staged file is still there. The test would catch this.

But is this a bug? The swap was supposed to either succeed (and move the staged to bak) or fail (and leave the staged alone). The current code doesn't clean up the staged on failure. This is a leak.

Wait, let me re-read `swapStagedBackup`:
```go
if err := renameFn(staged, bak); err != nil {
    if priorStaged {
        _ = os.Rename(bakOld, bak)
    }
    return fmt.Errorf("renaming staged backup to %s: %w", bak, err)
}
```

If the rename fails, we try to restore the prior. But we don't clean up the staged file. So the staged file is left at `staged`. This is a leak.

Hmm, but the test `TestBackupToDotBak_RenameFailurePreservesPriorBak` would catch this. Let me trace through:
1. Seed `bak` with prior content
2. Set `renameFn` to return error
3. Call `BackupToDotBak(src)`
4. Inside: stage copy to `tmpDir` (or `tmpFile`)
5. Call `swapStagedBackup(staged, bak, bakOld)`
6. Inside: rename `bak` to `bakOld` (succeeds via real os.Rename)
7. Call `renameFn(staged, bak)` (returns error)
8. Try to restore `bakOld` to `bak` (succeeds via real os.Rename)
9. Return error
10. `BackupToDotBak` returns the error
11. Test checks: staged file should not exist (but it does!)

So the test would fail! Unless... wait, let me re-check. The staged file has `.bak.tmp-*` in its name. The test checks for `.bak.tmp-` stragglers. If the swap fails, the staged file is still there. The test would catch it.

So this IS a bug. The staged file is leaked when the swap fails.

But wait, let me check if the test actually catches this. Let me trace through the test more carefully:

```go
orig := renameFn
renameFn = func(_, _ string) error { return os.ErrPermission }
defer func() { renameFn = orig }()
```

The stub returns `os.ErrPermission`. When `swapStagedBackup` calls `renameFn(staged, bak)`, it gets `os.ErrPermission`. Then it tries to restore `_ = os.Rename(bakOld, bak)`. This uses the real `os.Rename`, not the stub. So the restore succeeds.

After this, the staged file is still at `staged`. The test would fail.

But wait, is the test in the diff passing? Let me re-check...

Actually, let me re-read the test:
```go
// No atcr-owned staging artifact may leak.
entries, err := os.ReadDir(dir)
if err != nil {
    t.Fatalf("ReadDir: %v", err)
}
for _, e := range entries {
    if strings.Contains(e.Name(), ".bak.tmp-") {
        t.Errorf("staging temp leaked after failed swap: %s", e.Name())
    }
    if strings.HasSuffix(e.Name(), ".bak.old") {
        t.Errorf(".bak.old straggler leaked after failed swap: %s", e.Name())
    }
}
```

Yes, the test checks for `.bak.tmp-` and `.bak.old` stragglers. If the swap fails and the staged file is leaked, the test would catch it.

So either:
(a) The test is expected to fail (and the diff is broken)
(b) The staged file is cleaned up somewhere I'm not seeing
(c) My analysis is wrong

Let me re-read the code more carefully...

Actually, I notice that the test sets up `bak` with prior content. Then the stub is set. Then `BackupToDotBak(src)` is called.

In `BackupToDotBak`:
1. Lstat src - exists
2. RemoveAll bakOld - succeeds (doesn't exist)
3. Stage copy: if isDir, MkdirTemp + copyTree. If file, CreateTemp + write + copyFile.
4. Call swapStagedBackup(staged, bak, bakOld)

In `swapStagedBackup`:
1. Lstat bak - exists
2. os.Rename(bak, bakOld) - succeeds (real os.Rename, not stubbed)
3. renameFn(staged, bak) - returns os.ErrPermission (stubbed)
4. _ = os.Rename(bakOld, bak) - succeeds (real os.Rename)
5. Return error

In `BackupToDotBak`:
1. swapStagedBackup returns error
2. Return "", err

So the staged file is still there. The test would fail.

Unless... wait, let me check if the staged file is removed somewhere. In the original code (before the change), the staged file was cleaned up after the swap. But in the new code, on failure, the staged file is not cleaned up.

This is a real bug. The staged file is leaked when the swap fails.

Actually, hold on. Let me re-check the sprint plan. The sprint plan says:
> "Stale atcr-owned staging artifacts (.bak.old/.bak.new) from a prior crashed run are reconciled away at entry, so retries start clean and the one-generation contract holds across a crash-then-retry sequence."

And:
> "Straggler cleanup at entry is in scope and required: RemoveAll any leftover .bak.old / .bak.new / .bak.tmp-* at the top of each backup entry."

So the stragglers are cleaned at entry, not at the end of a failed swap. This means the staged file IS leaked after a failed swap, and would only be cleaned up on the next run.

This is a bug. The test should catch it, but it might not because... let me re-read the test once more.

```go
// No atcr-owned staging artifact may leak.
entries, err := os.ReadDir(dir)
if err != nil {
    t.Fatalf("ReadDir: %v", err)
}
for _, e := range entries {
    if strings.Contains(e.Name(), ".bak.tmp-") {
        t.Errorf("staging temp leaked after failed swap: %s", e.Name())
    }
    if strings.HasSuffix(e.Name(), ".bak.old") {
        t.Errorf(".bak.old straggler leaked after failed swap: %s", e.Name())
    }
}
```

Hmm, the test checks for `.bak.tmp-` stragglers. The staged file is created with `os.MkdirTemp(dir, filepath.Base(bak)+".tmp-*")` for dirs, or `os.CreateTemp(dir, "."+filepath.Base(bak)+".tmp-*")` for files. So the name would be something like `review.bak.tmp-123456789`. The test would catch this.

So either the test is expected to fail, or there's something I'm missing.

Wait, let me check the actual behavior. Maybe `os.CreateTemp` creates a file with a different pattern. Let me think...

`os.CreateTemp(dir, "."+filepath.Base(bak)+".tmp-*")` - the pattern is `.review.bak.tmp-*`. The `*` is replaced with random characters. So the file would be named something like `.review.bak.tmp-123456789`.

The test checks `strings.Contains(e.Name(), ".bak.tmp-")`. The name `.review.bak.tmp-123456789` contains `.bak.tmp-`. So the test would catch it.

Hmm, so this is a real bug. The staged file is leaked after a failed swap.

But wait, the test is in the diff, which means it's expected to pass. Let me look at the test more carefully...

Oh wait! I just realized. The test stub is set AFTER the staging happens. Let me re-trace:

```go
// 1. Seed src and bak
// 2. Set renameFn stub
// 3. Call BackupToDotBak(src)
```

Inside BackupToDotBak:
1. Lstat src
2. RemoveAll bakOld
3. Stage copy (creates staged file)
4. Call swapStagedBackup(staged, bak, bakOld)

In swapStagedBackup:
1. Lstat bak
2. os.Rename(bak, bakOld) - real os.Rename
3. renameFn(staged, bak) - stub returns error
4. Restore bakOld to bak
5. Return error

So the staged file is still there. The test would fail.

Unless... the test is expected to be updated to expect this leak? Or the staged file is cleaned up by the test framework somehow?

Wait, maybe I'm misreading the test. Let me look at it again:

```go
orig := renameFn
renameFn = func(_, _ string) error { return os.ErrPermission }
defer func() { renameFn = orig }()

if _, err := BackupToDotBak(src); err == nil {
    t.Fatal("expected error on injected swap-rename failure, got nil")
}
```

So the stub is set, then BackupToDotBak is called. The stub returns os.ErrPermission for the renameFn call in swapStagedBackup.

Hmm, I think the test would fail. This is a bug.

But wait, let me consider: maybe the test is actually correct and the bug is in my analysis. Let me think about what `os.MkdirTemp(dir, filepath.Base(bak)+".tmp-*")` does.

`filepath.Base(bak)` = `review.bak` (assuming src is `review` and bak is `review.bak`).
The pattern is `review.bak.tmp-*`.
The directory created would be `review.bak.tmp-123456789`.

The test checks `strings.Contains(e.Name(), ".bak.tmp-")`. The name `review.bak.tmp-123456789` contains `.bak.tmp-`. So the test would catch it.

So this is a real bug. The test would fail.

Unless... the test is designed to fail and the bug is being introduced? That doesn't make sense for a diff that adds a test.

Let me re-read the test once more, very carefully:

```go
func TestBackupToDotBak_RenameFailurePreservesPriorBak(t *testing.T) {
    for _, tc := range []struct {
        name  string
        isDir bool
    }{
        {"directory", true},
        {"file", false},
    } {
        t.Run(tc.name, func(t *testing.T) {
            dir := t.TempDir()
            src := filepath.Join(dir, "review")
            bak := src + ".bak"

            if tc.isDir {
                if err := os.MkdirAll(src, 0o755); err != nil {
                    t.Fatalf("seed src dir: %v", err)
                }
                if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("new\n"), 0o644); err != nil {
                    t.Fatalf("seed src content: %v", err)
                }
                if err := os.MkdirAll(bak, 0o755); err != nil {
                    t.Fatalf("seed prior .bak dir: %v", err)
                }
                if err := os.WriteFile(filepath.Join(bak, "prior.txt"), []byte("prior\n"), 0o644); err != nil {
                    t.Fatalf("seed prior .bak content: %v", err)
                }
            } else {
                if err := os.WriteFile(src, []byte("new\n"), 0o644); err != nil {
                    t.Fatalf("seed src file: %v", err)
                }
                if err := os.WriteFile(bak, []byte("prior\n"), 0o644); err != nil {
                    t.Fatalf("seed prior .bak file: %v", err)
                }
            }

            orig := renameFn
            renameFn = func(_, _ string) error { return os.ErrPermission }
            defer func() { renameFn = orig }()

            if _, err := BackupToDotBak(src); err == nil {
                t.Fatal("expected error on injected swap-rename failure, got nil")
            }

            // AC4: the prior .bak must survive the failed swap.
            priorPath := bak
            if tc.isDir {
                priorPath = filepath.Join(bak, "prior.txt")
            }
            got, err := os.ReadFile(priorPath)
            if err != nil {
                t.Fatalf("prior .bak did not survive failed swap: %v", err)
            }
            if string(got) != "prior\n" {
                t.Errorf("prior .bak corrupted after failed swap: %q", got)
            }

            // No atcr-owned staging artifact may leak.
            entries, err := os.ReadDir(dir)
            if err != nil {
                t.Fatalf("ReadDir: %v", err)
            }
            for _, e := range entries {
                if strings.Contains(e.Name(), ".bak.tmp-") {
                    t.Errorf("staging temp leaked after failed swap: %s", e.Name())
                }
                if strings.HasSuffix(e.Name(), ".bak.old") {
                    t.Errorf(".bak.old straggler leaked after failed swap: %s", e.Name())
                }
            }
        })
    }
}
```

OK so the test:
1. Seeds src (new content) and bak (prior content)
2. Stubs renameFn to return error
3
</think>
