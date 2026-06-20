<think>
Let me analyze this diff carefully against the sprint plan scope and look for issues.

The sprint plan is for Epic 4.7.1: Crash-Safe Backup Swap Hardening. The scope covers:
- `internal/fanout/reviewdir.go` `backupExisting()` - crash-safe staging with `.bak.old`, EXDEV detection, copy+remove fallback
- `internal/atomicfs/atomic.go` `BackupToDotBak()` - crash-safe staging with `.bak.old`
- Fault-injection tests at both sites
- Straggler cleanup at entry

Let me review the diff systematically.

### 1. `internal/atomicfs/atomic.go` changes

**Doc comment update**: Good, explains the new behavior.

**`bakOld` variable**: Added for `.bak.old` path.

**Straggler cleanup at entry**: `os.RemoveAll(bakOld)` - good, but only cleans `.bak.old`. The plan says it should clean `.bak.tmp-*` too. Wait, `.bak.tmp-*` is the staging temp — those should have been renamed into place or removed. The sprint plan says to clean `.bak.tmp-*` stragglers. But actually, looking at the implementation, if a copy is interrupted, the `.bak.tmp-*` is left behind, but on the next call, `os.MkdirTemp` or `os.CreateTemp` will create a new one — the old one is a leak. The plan says "straggler cleanup at entry is in scope and required" and lists `.bak.old / .bak.new / .bak.tmp-*`. So there's a potential issue: the entry-time reconcile for `BackupToDotBak` only cleans `.bak.old`, not `.bak.tmp-*`.

Wait, let me re-read the sprint plan more carefully:

> Straggler cleanup at entry is in scope and required: RemoveAll any leftover .bak.old / .bak.new / .bak.tmp-* at the top of each backup entry.

So for `BackupToDotBak`, the entry should clean `.bak.old / .bak.new / .bak.tmp-*`. But the current implementation only cleans `.bak.old`. This is a gap.

**`swapStagedBackup` function**: The logic looks correct:
1. Lstat bak. If exists, rename to bakOld. If doesn't exist (fs.ErrNotExist), proceed. If other error, return.
2. Rename staged to bak. If failure and prior was staged, restore bakOld to bak.
3. On success, if prior was staged, RemoveAll bakOld.

Wait, there's a subtle issue: if `os.Rename(bak, bakOld)` succeeds but `renameFn(staged, bak)` fails and we restore `bakOld` to `bak`, the staged temp is still on disk. It won't be cleaned up. Let me check...

Looking at the code, after a failed swap, the function returns the error. The caller (`BackupToDotBak`) returns the error too. The `.bak.tmp-*` staged temp is left on disk. The next call would clean `.bak.old` at entry but NOT the `.bak.tmp-*` straggler. So this is a leak across crash-then-retry sequences.

Actually, let me look at this more carefully. The test `TestBackupToDotBak_RenameFailurePreservesPriorBak` checks for `.bak.tmp-` leaks and `.bak.old` leaks after a failed swap. It would find a `.bak.tmp-` leak!

Let me re-read the test:
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

So this test asserts no `.bak.tmp-` leak after a failed swap. If the test is correct, then the staging temp must be cleaned up somewhere. But looking at the code, after `renameFn(staged, bak)` fails, the function returns without cleaning up `staged`. So this test would actually FAIL.

Wait, let me re-read more carefully... In the test, the rename seam replaces `renameFn` to always return an error. So `os.Rename(staged, bak)` fails. The `staged` variable is the `.bak.tmp-*` directory or file. The function returns the error, leaving `staged` on disk. The test then checks for `.bak.tmp-` entries and would find the leaked temp.

Hmm, but the test is asserting the absence. This is a real issue. The `swapStagedBackup` function should clean up the staged temp on failure.

Actually wait, let me re-check. In the test, `os.ErrPermission` is returned by the stub. So `renameFn(staged, bak)` returns `os.ErrPermission`. The function then:
1. Checks `priorStaged` — true (since bak existed before)
2. Calls `_ = os.Rename(bakOld, bak)` — this is best-effort
3. Returns the error

But the `staged` variable (`.bak.tmp-*`) is never cleaned up. The test would catch this.

Actually, hold on. Let me re-read the test setup more carefully. The test does:
```go
orig := renameFn
renameFn = func(_, _ string) error { return os.ErrPermission }
defer func() { renameFn = orig }()
```

So all calls to `renameFn` return `os.ErrPermission`. This means:
1. In `BackupToDotBak`, the copy step happens first (staging into `.bak.tmp-*`).
2. Then `swapStagedBackup` is called.
3. In `swapStagedBackup`:
   a. `os.Lstat(bak)` succeeds (bak exists from seed).
   b. `os.Rename(bak, bakOld)` — this calls the real `os.Rename` because it's not `renameFn`! Wait, let me check...

Looking at the code:
```go
if _, err := os.Lstat(bak); err == nil {
    if err := os.Rename(bak, bakOld); err != nil {
```

This uses `os.Rename` directly, not `renameFn`. So the prior backup is staged to `.bak.old` successfully.
4. Then `renameFn(staged, bak)` is called, which returns `os.ErrPermission`.
5. The function tries to restore `bakOld` to `bak` using `os.Rename` (real), which succeeds.
6. Returns the error.

After this, the `.bak.tmp-*` staged temp is still on disk. The test checks for `.bak.tmp-` entries and would find this.

So this is a real bug: the staging temp leaks after a failed swap.

Hmm, but the test is in the diff being reviewed. If this is a known issue, it would be caught by the test. Let me look more carefully...

Actually, I need to check if the test would actually fail. The test uses `os.ReadDir(dir)` where `dir` is `t.TempDir()`. The `src` is `filepath.Join(dir, "review")`. The `.bak.tmp-*` would be in `dir` (same directory as `src`). So yes, `ReadDir(dir)` would find the leaked `.bak.tmp-*`.

Wait, but the test calls `BackupToDotBak(src)` which:
1. Calls `os.RemoveAll(bakOld)` — cleans any stale `.bak.old`.
2. Copies `src` to `.bak.tmp-*` staging.
3. Calls `swapStagedBackup(staged, bak, bakOld)`.

In `swapStagedBackup`:
1. `os.Lstat(bak)` — bak exists (from seed).
2. `os.Rename(bak, bakOld)` — succeeds (uses real os.Rename).
3. `renameFn(staged, bak)` — returns `os.ErrPermission`.
4. `priorStaged` is true, so `_ = os.Rename(bakOld, bak)` — restores.
5. Returns error.

The staged temp (`.bak.tmp-*`) is leaked. The test would fail.

So either:
- The test is wrong (but it's in the diff being reviewed)
- The implementation is missing cleanup of staged temp on failure

I think the implementation is missing cleanup. This is a HIGH severity issue.

But wait, let me check if there's a cleanup somewhere else. Let me re-read the full `BackupToDotBak` flow...

```go
func BackupToDotBak(src string) (string, error) {
    ...
    if info.IsDir() {
        tmpDir, err := os.MkdirTemp(dir, filepath.Base(bak)+".tmp-*")
        ...
        if err := copyTree(src, tmpDir); err != nil {
            return "", fmt.Errorf("backing up %s: %w", src, err)
        }
        staged = tmpDir
    } else {
        tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(bak)+".tmp-*")
        ...
        if err := copyFile(src, tmpName, info.Mode().Perm()); err != nil {
            return "", fmt.Errorf("backing up %s: %w", src, err)
        }
        staged = tmpName
    }

    if err := swapStagedBackup(staged, bak, bakOld); err != nil {
        return "", err
    }
    return bak, nil
}
```

No cleanup of `staged` on failure path. This is a real leak.

Actually, let me reconsider. Maybe the test is actually passing because of some other reason. Let me think about what happens when the rename stub returns `os.ErrPermission`:

The test checks for entries in `dir` (the t.TempDir). The entries would be:
- `review` (the src directory)
- `review.bak` (the prior backup, restored)
- `review.bak.tmp-XXXXXX` (the leaked staging temp)

So the test would find the `.bak.tmp-` entry and fail. This means the test as written would fail, which means either:
1. The implementation is wrong (missing cleanup)
2. I'm misreading the code

Let me re-read the test one more time...

```go
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

Yes, this would catch the leak. So the implementation is missing cleanup of the staged temp on failure. This is a HIGH severity issue.

Wait, but the test is in the diff being reviewed. If it's a new test, it would be run as part of CI. If it fails, the PR would be blocked. So either:
1. The implementation actually does clean up somewhere I haven't seen
2. The test is expected to fail and the reviewer should flag this

Let me look one more time at `swapStagedBackup`...

```go
func swapStagedBackup(staged, bak, bakOld string) error {
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
    return nil
}
```

No cleanup of `staged` on failure. The test would fail.

Actually, hmm. Let me reconsider. Maybe the test is designed to fail and the reviewer should flag it? No, that doesn't make sense for a sprint plan item.

OR, maybe the entry-time straggler cleanup should also handle `.bak.tmp-*`. The sprint plan says:
> Straggler cleanup at entry is in scope and required: RemoveAll any leftover .bak.old / .bak.new / .bak.tmp-* at the top of each backup entry.

For `BackupToDotBak`, the entry only cleans `.bak.old`, not `.bak.tmp-*`. So if a previous run left a `.bak.tmp-*` and a new run starts, the old `.bak.tmp-*` is not cleaned. This is a gap.

But the test is specifically about a failed swap, not about entry-time cleanup. The test checks for leaks after a single failed call, not across multiple calls.

I think the issue is: `swapStagedBackup` should clean up the staged temp on failure. The test asserts this. The implementation is missing this. This is a HIGH severity bug.

Let me also check the `backupExisting` function for the same issue...

```go
func backupExisting(path string) (string, error) {
    ...
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
    ...
}
```

For the non-EXDEV case, `renameFn(path, backup)` fails. `path` still exists (rename never moved it). `backup` doesn't exist (it was staged to `.bak.old`). The restore brings `.bak.old` back to `backup`. No staging temp to clean up (it's a move, not a copy). So this is fine.

For the EXDEV case, `backupCrossDevice` is called. If it fails, `restorePriorBackup` is called. The staging temp is `backupNew` (`.bak.new`). If the copy fails, `backupNew` doesn't exist (it was cleaned at the start of `backupCrossDevice`). If the rename fails, `backupNew` still exists. If the `RemoveAll(path)` fails, `backupNew` was already renamed to `backup`, so it's gone.

Let me trace through `backupCrossDevice`:
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

If `os.Rename(backupNew, backup)` fails, `backupNew` still exists. The caller (`backupExisting`) calls `restorePriorBackup` which restores `.bak.old` to `backup`. But `backupNew` is still on disk. The next entry would clean it (entry-time straggler cleanup includes `.bak.new`). So this is handled by the entry-time cleanup.

OK, so the main issue I see is:

1. **`swapStagedBackup` leaks `.bak.tmp-*` on failed swap** — the test asserts no leak, but the implementation doesn't clean up. This is a HIGH severity issue.

Wait, actually, let me reconsider. Maybe I'm wrong about the test. Let me look at the test one more time very carefully.

The test seeds a prior `.bak` (directory or file). Then it stubs `renameFn` to always return `os.ErrPermission`. Then it calls `BackupToDotBak(src)`.

In `BackupToDotBak`:
1. `os.Lstat(src)` — succeeds.
2. `os.RemoveAll(bakOld)` — bakOld is `.bak.old`, doesn't exist yet, so no-op.
3. Copy step:
   - If isDir: `os.MkdirTemp(dir, ...)` creates `.bak.tmp-XXXXXX`, then `copyTree(src, tmpDir)`.
   - If file: `os.CreateTemp(dir, ...)` creates `.bak.tmp-XXXXXX`, then `copyFile(src, tmpName, ...)`.
4. `swapStagedBackup(staged, bak, bakOld)`:
   - `os.Lstat(bak)` — succeeds (bak exists from seed).
   - `os.Rename(bak, bakOld)` — uses real os.Rename, succeeds.
   - `renameFn(staged, bak)` — returns `os.ErrPermission`.
   - `priorStaged` is true, so `_ = os.Rename(bakOld, bak)` — uses real os.Rename, succeeds.
   - Returns error.
5. `BackupToDotBak` returns the error.

After this, the directory `dir` contains:
- `review` (src)
- `review.bak` (restored prior backup)
- `review.bak.tmp-XXXXXX` (leaked staging temp)

The test then checks:
```go
entries, err := os.ReadDir(dir)
for _, e := range entries {
    if strings.Contains(e.Name(), ".bak.tmp-") {
        t.Errorf("staging temp leaked after failed swap: %s", e.Name())
    }
}
```

This would find `review.bak.tmp-XXXXXX` and fail. So the test as written would fail.

This confirms the issue: `swapStagedBackup` should clean up the staged temp on failure. The implementation is missing this.

But wait — the test is in the diff being reviewed. If it's a new test that would fail, that's a problem. But maybe the test is intentionally there to catch this bug, and the reviewer should flag it. OR maybe I'm misunderstanding the flow.

Actually, let me reconsider. Maybe the `MkdirTemp` / `CreateTemp` creates the temp in a different location? No, `dir` is `filepath.Dir(src)` which is the same as the t.TempDir. So the `.bak.tmp-*` would be in `dir`.

Hmm, but what if the copy step itself fails? No, the copy step uses real `copyTree` / `copyFile`, not the stubbed `renameFn`. The copy succeeds, and then the swap fails.

OK, I'm confident this is a bug. The `swapStagedBackup` function should clean up the staged temp on failure. This is a HIGH severity issue.

Let me also check for other issues:

2. **Entry-time straggler cleanup for `BackupToDotBak` only handles `.bak.old`, not `.bak.tmp-*`** — the sprint plan says to clean `.bak.tmp-*` too. This is a MEDIUM severity issue.

Wait, actually, for `BackupToDotBak`, the staging temp is `.bak.tmp-*`. If a previous run left a `.bak.tmp-*` and a new run starts, the old `.bak.tmp-*` is not cleaned. This could cause issues (e.g., disk space, confusion). The entry-time cleanup should also handle `.bak.tmp-*`.

But actually, `os.MkdirTemp` and `os.CreateTemp` generate random suffixes, so old temps don't conflict with new ones. The issue is just disk space / cleanup. The sprint plan says to clean them, so this is a gap.

3. **`swapStagedBackup` restore-on-failure uses `os.Rename` directly, not `renameFn`** — this is intentional because the restore should always use the real rename. No issue.

4. **The `swapStagedBackup` function's `renameFn` is a package-level var** — this is the test seam, intentional.

5. **`backupExisting` uses `renameFn` for the path→backup rename** — this is the test seam, intentional.

6. **EXDEV detection uses `errors.Is(err, syscall.EXDEV)`** — this is correct on Unix. On Windows, the error might be different, but the sprint plan is Unix-focused. No issue.

7. **`backupCrossDevice` cleans `backupNew` at entry** — this is correct (handles stragglers from prior crashed runs).

8. **`backupExisting` cleans `backupOld` and `backupNew` at entry** — this is correct.

9. **The `CopyPath` function is exported** — this is intentional (the fanout package needs to call it for the EXDEV fallback).

10. **Boundary change: `fanout` now imports `atomicfs`** — this is documented in the comment and is the only change needed for the EXDEV fallback.

11. **`guardForeignBackup` stays scoped to `.bak` only** — per the sprint plan clarification, this is correct.

12. **`renameFn` is defined in both `internal/atomicfs/atomic.go` and `internal/fanout/reviewdir.go`** — these are different packages, so no conflict. But it's a bit of code duplication. Not a bug, just a design choice.

13. **The test `TestBackupExisting_FailedSwapPreservesPriorBak` stubs `renameFn` to return a generic error** — the test then checks that the prior backup is restored. Let me trace through:
    - `os.RemoveAll(backupOld)` — no-op.
    - `os.RemoveAll(backupNew)` — no-op.
    - `os.Lstat(backup)` — backup exists.
    - `os.Rename(backup, backupOld)` — uses real os.Rename, succeeds.
    - `renameFn(path, backup)` — returns generic error.
    - `errors.Is(err, syscall.EXDEV)` — false.
    - `restorePriorBackup(true, backupOld, backup)` — `os.Rename(backupOld, backup)` — succeeds.
    - Returns error.
    
    Test checks:
    - `prior .bak` restored — yes.
    - `src` untouched — yes.
    - `.bak.old` doesn't exist — yes (restored to .bak).
    
    This test should pass. No issue.

14. **The test `TestBackupExisting_CrossDeviceFallback` stubs `renameFn` to return `syscall.EXDEV`** — let me trace:
    - `os.RemoveAll(backupOld)` — no-op.
    - `os.RemoveAll(backupNew)` — no-op.
    - `os.Lstat(backup)` — backup exists.
    - `os.Rename(backup, backupOld)` — succeeds.
    - `renameFn(path, backup)` — returns EXDEV.
    - `errors.Is(err, syscall.EXDEV)` — true.
    - `backupCrossDevice(path, backup, backupNew)`:
      - `os.RemoveAll(backupNew)` — no-op.
      - `atomicfs.CopyPath(path, backupNew)` — copies path to .bak.new.
      - `os.Rename(backupNew, backup)` — renames .bak.new to .bak.
      - `os.RemoveAll(path)` — removes path.
    - After swap, `priorStaged` is true, so `os.RemoveAll(backupOld)` — cleans .bak.old.
    - Returns backup.
    
    Test checks:
    - `bak` = stale (which is `src + ".bak"`) — yes.
    - `bak/marker.txt` = "current" — yes (copied from path).
    - `bak/old.txt` doesn't exist — yes (bak was replaced).
    - `src` doesn't exist — yes (vacated).
    - `.bak.new` doesn't exist — yes (renamed to .bak).
    - `.bak.old` doesn't exist — yes (cleaned after success).
    
    This test should pass. No issue.

15. **The test `TestBackupExisting_CleansStaleStagingStragglers`** — seeds `.bak.old` and `.bak.new`, then calls `backupExisting`. Since there's no prior `.bak` (the test doesn't seed one), the flow is:
    - Clean `.bak.old` and `.bak.new` at entry.
    - `os.Lstat(backup)` — backup doesn't exist (the test didn't seed `.bak`, only `.bak.old` and `.bak.new`).
    - So `priorStaged` is false.
    - `renameFn(path, backup)` — real os.Rename, succeeds.
    - No `.bak.old` to clean.
    
    Test checks:
    - `.bak.old` doesn't exist — yes (cleaned at entry).
    - `.bak.new` doesn't exist — yes (cleaned at entry).
    - `.bak/marker.txt` = "current" — yes.
    
    This test should pass. No issue.

16. **The test `TestBackupToDotBak_RenameFailurePreservesPriorBak`** — as I analyzed above, this test would fail because the `.bak.tmp-*` staging temp leaks. This is a HIGH severity issue.

Wait, let me double-check by looking at the test one more time...

The test stubs `renameFn` to return `os.ErrPermission`. The copy step succeeds (uses real `copyTree`/`copyFile`). The swap step calls `renameFn` which returns the error. The staged temp is leaked.

Actually, I want to be really sure. Let me look at the test's directory check:

```go
entries, err := os.ReadDir(dir)
```

Where `dir = t.TempDir()` and `src = filepath.Join(dir, "review")`. The `.bak.tmp-*` would be in `dir` (same directory as `src`). So yes, it would be found.

But wait — could it be that the test is actually correct and I'm wrong about the leak? Let me think about this differently.

If the test is in the diff and it's meant to pass, then either:
1. The implementation cleans up the staged temp on failure (but I don't see where).
2. The test is expected to fail and should be fixed.
3. I'm misreading the code.

I've been going back and forth. Let me just flag this as a potential issue. The reviewer should verify that the staged temp is cleaned up on failure, because the test asserts it but the implementation doesn't appear to do it.

Actually, you know what, let me re-examine the code one more time. Is there any place where `staged` is cleaned up?

```go
func swapStagedBackup(staged, bak, bakOld string) error {
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
    ...
}
```

No cleanup of `staged` anywhere. This is a real leak.

OK wait, I just realized something. The test checks for `.bak.tmp-` in the directory. But what if the test is actually correct because the `swapStagedBackup` function is supposed to clean up `staged` on failure, and the reviewer is supposed to flag this as a missing cleanup?

OR, what if the test is intentionally there to document the expected behavior, and the implementation is supposed to be fixed? In that case, the reviewer should flag it.

Either way, this is a real issue that should be flagged. The test asserts no `.bak.tmp-` leak, but the implementation doesn't clean up the staged temp on failure.

Let me also check: is there a case where the test would pass? What if `os.ErrPermission` causes `os.Rename(bak, bakOld)` to fail? No, that uses real `os.Rename`, not `renameFn`. So `os.Rename(bak, bakOld)` succeeds.

What if the copy step fails? No, the copy uses real `copyTree`/`copyFile`, not `renameFn`. So the copy succeeds.

I'm confident this is a bug. The `swapStagedBackup` function should clean up the staged temp on failure.

Now, let me also think about whether this is in scope. The sprint plan says:
> Fault-injection tests asserting the prior .bak survives a failed swap at both sites.

And:
> Stale atcr-owned staging artifacts (.bak.old/.bak.new/.bak.tmp-*) from a prior crashed swap are reconciled away at entry

So the sprint plan is aware of `.bak.tmp-*` staging artifacts. The entry-time cleanup should handle them. And the swap function should clean up the staged temp on failure.

The `BackupToDotBak` entry-time cleanup only handles `.bak.old`, not `.bak.tmp-*`. This is a gap.

The `swapStagedBackup` function doesn't clean up the staged temp on failure. This is a gap.

Both are in scope per the sprint plan.

Let me now think about other potential issues:

17. **`swapStagedBackup` error message says "renaming staged backup to %s" but the error could be from the restore** — no, the error is always from `renameFn(staged, bak)`. The restore is best-effort and its error is discarded. No issue.

18. **The `renameFn` stub in tests uses `os.ErrPermission`** — this is a reasonable error to simulate a permission failure. No issue.

19. **`backupCrossDevice` calls `atomicfs.CopyPath` which uses `copyTree`/`copyFile`** — these are the same primitives used by `BackupToDotBak`. No issue.

20. **The `CopyPath` function checks for non-regular sources** — this is correct (refuses symlinks, devices, etc.).

21. **`CopyPath` for directories calls `copyTree` which creates the destination** — this is correct.

22. **The boundary change adds `atomicfs` to `fanout`'s allowed imports** — this is documented and necessary for the EXDEV fallback. No issue.

23. **`forceBackupOutputDir` comment update** — the comment now correctly describes the crash-safe staging behavior. No issue.

24. **`guardForeignBackup` is not extended to `.bak.old`/`.bak.new`** — per the sprint plan clarification, this is intentional. No issue.

25. **The `restorePriorBackup` function is best-effort** — if the restore fails, the prior data survives under `.bak.old` for manual recovery. The comment explains this. No issue.

26. **The `backupCrossDevice` function's `os.RemoveAll(backupNew)` at entry** — this is correct (cleans stragglers from prior crashed runs).

27. **`backupCrossDevice` calls `atomicfs.CopyPath(path, backupNew)`** — this copies the live tree to the staging path. Correct.

28. **`backupCrossDevice` calls `os.Rename(backupNew, backup)`** — this is a same-fs rename (since `backupNew` is in the same directory as `backup`). Correct.

29. **`backupCrossDevice` calls `os.RemoveAll(path)`** — this vacates the path, matching the move's postcondition. Correct.

30. **`TestBackupExisting_CrossDeviceFallback` asserts `bak/old.txt` doesn't exist** — this is because the stale backup was replaced by the new one. But wait, the prior backup was staged to `.bak.old` first, then the copy happened, then the swap. So the `.bak` should contain the new content (from path), not the stale content. The test checks `bak/marker.txt` = "current" (new content) and `bak/old.txt` doesn't exist. This is correct.

31. **`TestBackupExisting_CrossDeviceFallback` asserts `src` doesn't exist** — the `RemoveAll(path)` at the end of `backupCrossDevice` vacates the path. Correct.

32. **`TestBackupExisting_CleansStaleStagingStragglers` doesn't seed a prior `.bak`** — it only seeds `.bak.old` and `.bak.new`. The test then calls `backupExisting(src)`. Since there's no prior `.bak`, the swap is just `renameFn(path, backup)`. The entry-time cleanup removes `.bak.old` and `.bak.new`. The swap succeeds. The test checks that the stragglers are gone and the new backup has the correct content. Correct.

OK, I think the main issues are:

1. **HIGH: `swapStagedBackup` leaks `.bak.tmp-*` on failed swap** — the test asserts no leak, but the implementation doesn't clean up.
2. **MEDIUM: `BackupToDotBak` entry-time cleanup doesn't handle `.bak.tmp-*`** — the sprint plan says to clean `.bak.tmp-*` at entry.

Wait, actually, for issue 2, let me reconsider. The sprint plan says:
> Straggler cleanup at entry is in scope and required: RemoveAll any leftover .bak.old / .bak.new / .bak.tmp-* at the top of each backup entry.

For `BackupToDotBak`, the staging temp is `.bak.tmp-*`. If a previous run left a `.bak.tmp-*` and a new run starts, the old `.bak.tmp-*` is not cleaned. But `os.MkdirTemp`/`os.CreateTemp` generates random suffixes, so the old temp doesn't conflict. The issue is just that it accumulates over time.

But the sprint plan explicitly says to clean `.bak.tmp-*` at entry. So this is a gap.

Actually, hmm, let me re-read the sprint plan clarification:
> Straggler cleanup at entry is in scope and required: RemoveAll any leftover .bak.old / .bak.new / .bak.tmp-* at the top of each backup entry.

This is about the `backupExisting` function in the fanout package. For `BackupToDotBak`, the staging temp is `.bak.tmp-*`. The entry-time cleanup for `BackupToDotBak` only handles `.bak.old`, not `.bak.tmp-*`.

But wait, the clarification also says:
> IN scope: ... entry-time straggler cleanup of atcr-owned .bak.old/.bak.new/.bak.tmp-*; ...

So `.bak.tmp-*` cleanup is in scope for both sites. The `BackupToDotBak` entry-time cleanup is missing this.

Let me flag both issues.

Actually, wait. Let me reconsider issue 1 one more time. The test `TestBackupToDotBak_RenameFailurePreservesPriorBak` checks for `.bak.tmp-` leak. If the implementation doesn't clean up, the test fails. But the test is in the diff being reviewed. If the test fails in CI, the PR would be blocked. So either:
1. The test is wrong (should be removed or modified)
2. The implementation is wrong (should clean up)
3. I'm misreading something

I've been going back and forth. Let me just flag it as a HIGH severity issue. The reviewer should verify that the staged temp is cleaned up on failure.

Hmm, actually, I just thought of something. What if the test is designed to pass because of a different reason? What if `os.ErrPermission` causes something else to fail?

Let me trace through one more time:
1. `os.Lstat(src)` — succeeds.
2. `os.RemoveAll(bakOld)` — bakOld is `.bak.old`, doesn't exist, no-op.
3. Copy step:
   - `os.MkdirTemp(dir, filepath.Base(bak)+".tmp-*")` — creates `.bak.tmp-XXXXXX`.
   - `copyTree(src, tmpDir)` — copies src to tmpDir.
   - `staged = tmpDir`.
4. `swapStagedBackup(staged, bak, bakOld)`:
   - `os.Lstat(bak)` — bak exists.
   - `os.Rename(bak, bakOld)` — uses real os.Rename, succeeds. `bakOld` now exists.
   - `renameFn(staged, bak)` — returns `os.ErrPermission`.
   - `priorStaged` is true, so `_ = os.Rename(bakOld, bak)` — uses real os.Rename, succeeds. `bak` is restored.
   - Returns error.
5. `BackupToDotBak` returns the error.

After this:
- `dir` contains: `review`, `review.bak`, `review.bak.tmp-XXXXXX`.
- The test checks for `.bak.tmp-` in `dir` and would find `review.bak.tmp-XXXXXX`.
- Test fails.

So the test would fail. This is a real bug.

OK, I'm going to flag this as HIGH severity.

Let me also think about whether there are any other issues I might have missed...

33. **`swapStagedBackup` uses `errors.Is(err, fs.ErrNotExist)`** — this is correct for checking if a file doesn't exist.

34. **`backupExisting` uses `errors.Is(err, fs.ErrNotExist)`** — this is correct.

35. **`swapStagedBackup` uses `errors.Is(err, syscall.EXDEV)`** — wait, no, it doesn't. The EXDEV check is in `backupExisting`,
</think>
