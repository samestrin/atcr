<think>
Let me analyze this diff systematically for Epic 4.7: Idempotency and Safe Retry. I need to focus on the files in scope and check for logic errors, edge cases, invariant violations, and type safety issues.

Let me trace through the changes:

1. **README.md** - Documentation changes for --force flag. Looks reasonable.

2. **cmd/atcr/review.go** - Adds --force flag and mutual exclusion check with --resume. The check fires before the resume short-circuit.

3. **cmd/atcr/review_test.go** - Tests for the new flag and mutual exclusion.

4. **internal/atomicfs/atomic.go** - New WriteJSON and BackupToDotBak functions with copyFile and copyTree helpers.

5. **internal/atomicfs/atomic_test.go** - New tests.

6. **internal/fanout/review.go** - Adds Force field to ReviewRequest and uses it in PrepareReview.

7. **internal/fanout/review_test.go** - New integration test for force backup.

8. **internal/fanout/reviewdir.go** - Modified error messages and new backup functions including guardForeignBackup to prevent destroying user data.

9. **internal/fanout/reviewdir_test.go** - New tests.

10. **internal/reconcile/gate.go** - Backs up reconciled/ before re-emit.

11. **internal/reconcile/gate_test.go** - New test for backup.

12. **internal/verify/emit_verification.go** - New BackupExistingVerification function.

13. **internal/verify/emit_test.go** - New test for backup.

14. **internal/verify/pipeline.go** - Calls BackupExistingVerification in runVerify.

Let me look for issues:

### Issue 1: WriteJSON doesn't sanitize temp file or handle the case where path is a directory

In `WriteJSON`, it uses `WriteFileAtomic`, which uses `os.CreateTemp` in the same directory. The behavior looks correct for files but what if the target path is a directory? Let me check WriteFileAtomic...

Looking at the diff for atomic.go, the existing `WriteFileAtomic` uses `os.CreateTemp` in the same directory. If path is a directory, `os.Rename(tmpName, path)` would fail because renaming a file onto a non-empty directory doesn't work the same way. Actually, `os.Rename` would fail if the target is a directory and exists. So this should be safe enough for the intended use cases.

### Issue 2: BackupToDotBak symlink handling

The code says it skips non-regular files. But there's a subtle issue: if `src` is a symlink to a directory, `os.Stat` follows the symlink and returns info about the target directory. Then it calls `copyTree` which uses `filepath.WalkDir`. `WalkDir` doesn't follow symlinks by default for directory entries, so symlinks would appear as symlinks and be skipped. This seems fine.

Wait, but `os.Stat` follows symlinks, so if `src` is a symlink to a directory, `info.IsDir()` returns true, and then `copyTree` is called with `src` being the symlink path. Inside `copyTree`, `filepath.WalkDir(src, ...)` will walk the symlink as a directory (since it resolves to a directory). Let me think...

Actually `filepath.WalkDir` on a symlink to a directory does walk the linked directory. This is a potential issue - if `src` is a symlink to a directory, the backup will copy through the symlink, which could be surprising behavior. However, the doc says "Non-regular entries (symlinks, devices) are skipped" referring to entries inside the tree, not the root.

Hmm, looking at `os.Stat` - it follows symlinks. So if `src` is a symlink, `info.IsDir()` checks the target. If the target is a directory, then `copyTree(src, bak)` is called. Inside `copyTree`, `filepath.WalkDir(src, ...)` is called. If `src` is a symlink to a directory, `WalkDir` follows it and walks the linked directory. This might or might not be intended.

But this is probably out of scope for the review because the use cases don't typically involve symlinks at the root.

### Issue 3: Race condition in collision check

In `cmd/atcr/review.go`, the order is:
1. Check if --resume flag is set
2. If so, check --force
3. If both, error
4. Otherwise, run resume

But this is fine because flag parsing is done before any concurrent execution. The check is sequential and deterministic.

### Issue 4: In gate.go, the backup of reconciled/ happens before adjudicating the existing file

Looking at the diff:
```go
reconDir := filepath.Join(reviewDir, reconciledSubdir)
+ if _, statErr := os.Stat(filepath.Join(reconDir, FindingsJSON)); statErr == nil {
+     if _, err := atomicfs.BackupToDotBak(reconDir); err != nil {
+         return Result{}, fmt.Errorf("backing up prior reconciled output: %w", err)
+     }
+ }
adjudicating := false
if _, statErr := os.Stat(filepath.Join(reconDir, AdjudicationJSON)); statErr == nil {
    adj, err := LoadAdjudication(filepath.Join(reconDir, AdjudicationJSON))
```

Wait, this is a problem! The backup happens BEFORE the adjudication is loaded from `AdjudicationJSON`. After the backup, `reconDir` is unchanged (BackupToDotBak copies, doesn't move), so the adjudication file is still there. So this might be OK.

Actually let me re-read: `BackupToDotBak` is a copy, not a move. So `reconDir` still has `AdjudicationJSON` after the backup. So the subsequent `os.Stat` and `LoadAdjudication` calls work correctly. Good.

But wait - there's a subtle issue. The backup creates `reconDir.bak/` which contains the prior `FindingsJSON`. Then `Emit` later overwrites the live `FindingsJSON`. The prior `FindingsJSON` is preserved in the backup. OK.

But what about `AdjudicationJSON`? If adjudication is in the live reconDir, will the backup include it? Yes, because the backup copies the whole tree. But the backup is only triggered if `FindingsJSON` exists. If `FindingsJSON` exists but no `AdjudicationJSON`, the backup will not include adjudication. If both exist, the backup includes both. This seems fine.

### Issue 5: WriteJSON error path - what if the marshal succeeds but the file write fails?

Looking at WriteJSON:
```go
func WriteJSON(path string, v interface{}) error {
    data, err := json.MarshalIndent(v, "", "  ")
    if err != nil {
        return err
    }
    return WriteFileAtomic(path, append(data, '\n'))
}
```

If MarshalIndent succeeds but WriteFileAtomic fails, the existing file is left intact (atomic rename). Good.

### Issue 6: writeGroupAtomic vs BackupExistingVerification in pipeline.go

Looking at the diff for pipeline.go:
```go
+ if err := BackupExistingVerification(reviewDir); err != nil {
+     return Result{}, err
+ }
if err := writeGroupAtomic(artifacts); err != nil {
    return Result{}, err
}
```

The backup happens before the atomic write. If the atomic write fails, the backup is in place but the live file is unchanged. If the atomic write succeeds, the backup holds the prior generation. Good.

But wait - what if the backup succeeds but then the atomic write fails? The backup exists and the live file is unchanged. The user has a backup of what was there, but the live file is the same. This is OK behavior.

But there's a subtle concern: what if there are other files being written by the same atomic group, and they don't all succeed? `writeGroupAtomic` is presumably atomic as a group. If the group fails, are any files written? Looking at the name, it suggests atomicity. Let me check... I don't have visibility into the implementation, but assuming it uses a similar pattern (write to temp, then rename), the behavior would be consistent.

### Issue 7: looksLikeReviewTree is incomplete

The function checks for every `reviewSubdirs` subdirectory. But what if a user has a directory structure that happens to match all the review subdirs? That's a false positive that would allow atcr to delete their data. This is a security concern, but the probability is low.

Also, what if `reviewSubdirs` changes in the future and old backups don't match? Then the user's old backup would be considered foreign and refused. This could be a problem for users with valid prior backups from an older atcr version.

Wait, looking at the function more carefully - if the directory has all the required subdirs, it's considered a review tree. If even one is missing, it's not. So a partial review tree (e.g., from a failed scaffold) would be considered foreign. Hmm.

Actually let me think about this more. The function `looksLikeReviewTree` is used to check if `<dir>.bak` is a genuine prior atcr backup. If so, it's OK to replace. If not, refuse to avoid destroying user data.

If a user has a real atcr backup that for some reason is missing one subdir, this would refuse. That's a false negative but it's the safe side (refuses to delete potentially important data).

### Issue 8: race condition in scaffold + backup

In `cmd/atcr/review.go`, the flow is:
1. Parse flags
2. If --resume, check --force, then run resume
3. Otherwise, create review, etc.

The actual backup and scaffold happen in `PrepareReview` in `internal/fanout/review.go`. Let me check the order:

```go
if req.Force {
    if err = forceBackupOutputDir(req.OutputDir); err != nil {
        return nil, err
    }
}
dir, err = ScaffoldOutputDir(req.OutputDir)
```

The backup happens before scaffold. Good. After backup, the directory is renamed to `.bak`, so the original path is vacant. Then ScaffoldOutputDir creates a fresh directory. This works.

Same for the IDOverride path:
```go
if req.Force {
    if err = forceBackupReviewDir(req.Root, id); err != nil {
        return nil, err
    }
}
dir, err = ScaffoldReviewDir(req.Root, id)
```

### Issue 9: BackupToDotBak uses os.RemoveAll unconditionally

The code does:
```go
bak := src + ".bak"
if err := os.RemoveAll(bak); err != nil {
    return "", fmt.Errorf("removing stale backup %s: %w", bak, err)
}
```

This unconditionally removes the existing backup. If a user has important data at `src.bak`, this destroys it. The `forceBackupOutputDir` guards against this with `guardForeignBackup`, but `BackupToDotBak` itself does not have this guard. This is a leaky abstraction - the user of `BackupToDotBak` must remember to check.

However, looking at where BackupToDotBak is used:
- `internal/reconcile/gate.go`: backs up `reconciled/` to `reconciled.bak/`. The `.bak` is inside the review dir, which is atcr-managed. Safe to remove.
- `internal/verify/emit_verification.go`: backs up `reconciled/verification.json` to `reconciled/verification.json.bak`. Same - inside review dir, atcr-managed. Safe to remove.

So in both cases, the .bak is inside a managed tree, so the unconditional remove is safe. But it's a brittle design.

### Issue 10: Potential issue with gate.go backup

In gate.go, the backup happens before the adjudication read. The backup copies the entire reconDir, including AdjudicationJSON. But adjudication is not overwritten during re-emit, so the backup containing it is just a snapshot.

Wait, is adjudication overwritten? Let me think... Actually I don't have full context, but the comment says "re-emit every reconciled/ artifact" so it could include adjudication. But the backup is taken before the re-emit, so it captures the prior state. Good.

But there's a potential issue: if the backup takes a long time (large reconDir), and adjudication is read from the live reconDir during the backup... no, actually the backup is a copy, so the live dir is unchanged during the backup. Adjudication reads would see the same data. OK.

### Issue 11: looking at `looksLikeReviewTree` more carefully

```go
func looksLikeReviewTree(dir string) bool {
    for _, sub := range reviewSubdirs {
        fi, err := os.Stat(filepath.Join(dir, sub))
        if err != nil || !fi.IsDir() {
            return false
        }
    }
    return true
}
```

This requires ALL review subdirs to exist. If a prior backup is partial (some subdirs missing), it's considered foreign. This could cause legitimate atcr backups to be refused. But again, the safe side is to refuse.

### Issue 12: `ScaffoldOutputDir` modification

The error message was changed from:
```
"output directory %q is not empty: refusing to overwrite (point --output-dir at a new or empty path)"
```
to:
```
"output directory %q already exists and is not empty; use --force to overwrite (or point --output-dir at a new or empty path)"
```

Good change - matches the AC1 requirement.

### Issue 13: `ScaffoldReviewDir` modification

The error message was changed to mention --resume and --force. Good.

### Issue 14: Potential issue with copyFile and large files

`copyFile` reads the entire file into memory. For large files (e.g., a large reconciliation output), this could be a memory issue. But for the use case (review artifacts), files are typically small. Not a real issue.

### Issue 15: BackupExistingVerification error wrapping

```go
if err := BackupExistingVerification(reviewDir); err != nil {
    return err
}
return atomicfs.WriteFileAtomic(path, data)
```

This is in WriteVerification. But `BackupExistingVerification` does:
```go
if _, err := atomicfs.BackupToDotBak(path); err != nil {
    return fmt.Errorf("backing up prior verification.json: %w", err)
}
```

OK, error wrapping is fine.

### Issue 16: RunReview test

```go
res1, err := RunReview(context.Background(), llmclient.New(), cfg, req)
require.NoError(t, err)
reviewDir := res1.Dir

marker := filepath.Join(reviewDir, "MARKER")
require.NoError(t, os.WriteFile(marker, []byte("gen1"), 0o644))
```

The test plants a marker after the first run. Then it runs again. The collision check should fail and the marker should remain. Then with --force, the backup should contain the marker. This looks correct.

### Issue 17: TestRunReview_ForceBacksUpExistingDir - test order

The test runs:
1. First run: creates review, returns res1.Dir
2. Plants marker
3. Re-runs without --force: expects error
4. Re-runs with --force: expects backup

But step 3 might leave state that affects step 4. Actually no, step 3 fails at collision check (ScaffoldReviewDir), so the dir is unchanged. Step 4 then backs up the unchanged dir (with marker) and creates a new one. Good.

### Issue 18: Pipeline.go backup placement

```go
+ if err := BackupExistingVerification(reviewDir); err != nil {
+     return Result{}, err
+ }
if err := writeGroupAtomic(artifacts); err != nil {
    return Result{}, err
}
```

Wait, the backup is called BEFORE writeGroupAtomic. If writeGroupAtomic writes to multiple files, the backup of verification.json is taken first. Then writeGroupAtomic does the atomic write of all files including verification.json. This is correct.

But there's a subtle issue: `writeGroupAtomic` presumably writes the new verification.json atomically. If it succeeds, the live file is the new version. If it fails, the live file is the old version. Either way, the backup holds the old version. Good.

### Issue 19: writeGroupAtomic itself might do backup?

I don't see the implementation, but the function is named `writeGroupAtomic`. If it includes backup logic, there could be double-backup. But based on the call pattern, I assume it's just an atomic write of multiple files. Not a real issue based on the diff.

### Issue 20: check for guardForeignBackup edge case

```go
func guardForeignBackup(backup string) error {
    entries, err := os.ReadDir(backup)
    if errors.Is(err, fs.ErrNotExist) {
        return nil
    }
    if err != nil {
        return fmt.Errorf("refusing --force: %q exists and was not created by atcr; move or remove it first", backup)
    }
    if len(entries) == 0 || looksLikeReviewTree(backup) {
        return nil
    }
    return fmt.Errorf("refusing --force: %q already exists and does not look like an atcr backup; move or remove it first", backup)
}
```

What if `backup` exists but is a regular file (not a directory)? `os.ReadDir` would fail. The function would return an error refusing to clobber. Good - safer than RemoveAll.

What if `backup` exists but is a symlink to a directory? `os.ReadDir` follows the symlink. Hmm, this could be an issue if the symlink points to important data. But this is a corner case.

### Issue 21: gate.go stat check is racy with backup

In gate.go:
```go
if _, statErr := os.Stat(filepath.Join(reconDir, FindingsJSON)); statErr == nil {
    if _, err := atomicfs.BackupToDotBak(reconDir); err != nil {
        ...
    }
}
```

There's a TOCTOU race: between the stat and the BackupToDotBak call, the file could be deleted. But this is a single-process operation, so the race is unlikely to matter.

### Issue 22: BackupToDotBak for a symlink

If `src` is a symlink, `os.Stat` follows the symlink. If the target is a file, `copyFile` is called, which reads the file content. So a symlink to a file is backed up as a copy of the file content. If the target is a directory, `copyTree` is called, which uses `filepath.WalkDir`. `WalkDir` does NOT follow symlinks for the walked entries, but the root symlink is followed because `WalkDir` resolves it. So the entire linked directory is walked and copied.

This might be surprising but is a corner case.

### Issue 23: Test for forceBackupOutputDir with empty dir

What if --output-dir exists but is empty? Then `ScaffoldOutputDir` would not error in the non-force case. And `forceBackupOutputDir` would not backup (since `len(entries) == 0`). So the dir is left as-is and `ScaffoldOutputDir` scaffolds subdirs into it. This matches the intent.

### Issue 24: Test for guardForeignBackup with file

What if `<dir>.bak` exists as a regular file (not a directory)? `os.ReadDir` would return an error. `guardForeignBackup` returns the "refusing" error. So `forceBackupOutputDir` returns the error. Good.

But wait - what about the case where the .bak is a file? `os.ReadDir` returns an error like "not a directory". The code returns an error. The user has to manually move/remove the .bak. This is correct behavior.

### Issue 25: Newline handling in WriteJSON

`WriteJSON` adds `\n` at the end. But `json.MarshalIndent` produces output that doesn't end with newline. So `append(data, '\n')` adds a trailing newline. This is consistent with the comment.

But this is a slight behavioral change from `WriteFileAtomic` - the latter is bytes-as-given. So if someone uses WriteFileAtomic for a non-JSON file, they get exactly what they pass. WriteJSON always adds newline. This is fine for the intended use case.

### Issue 26: Test for WriteJSON with marshal error

The test uses `make(chan int)` which is unmarshalable. Good edge case test.

### Issue 27: Potential issue with BackupToDotBak and existing target backup directory

If `src.bak` is a file (not a directory), `os.RemoveAll` removes the file. Then `os.Rename` moves `src` to `src.bak`. But `os.Rename` of a directory to a file path... let me think.

Actually, `os.Rename` can fail if the target is not a directory and the source is a directory. Let me check...

`os.Rename` semantics: "Rename renames (moves) oldpath to newpath. If newpath already exists and is not a directory, Rename replaces it. OS-specific restrictions may apply when oldpath and newpath are in different directories. Even within the same directory, on non-Unix platforms Rename is not an atomic operation."

Hmm, "If newpath already exists and is not a directory, Rename replaces it" - this is the old behavior. Newer Go versions: "If newpath already exists and is not a directory, Rename replaces it. OS-specific restrictions may apply when oldpath and newpath are different directories. Even within the same directory, on non-Unix platforms Rename is not an atomic operation."

So if `src.bak` is a regular file and we `os.Rename(src, src.bak)` where `src` is a directory, the behavior is OS-dependent. On Unix, `rename()` fails with `EISDIR` if newpath is an existing non-directory file.

So if `src` is a directory and `src.bak` is a regular file, `os.RemoveAll(src.bak)` removes the file. Then `os.Rename(src, src.bak)` works.

If `src` is a file and `src.bak` is a regular file, `os.RemoveAll(src.bak)` removes the file. Then `os.Rename(src, src.bak)` replaces the file. OK.

If `src` is a file and `src.bak` is a directory, `os.RemoveAll(src.bak)` removes the directory. Then `os.Rename(src, src.bak)` works.

So the RemoveAll handles all cases. Good.

But wait, in `forceBackupOutputDir`, there's the guard:
```go
if err := guardForeignBackup(dir + ".bak"); err != nil {
    return err
}
_, err = backupExisting(dir)
```

`guardForeignBackup` checks if `<dir>.bak` exists and is foreign. If foreign, it refuses. Otherwise, it allows the backup. But `backupExisting` then calls `os.RemoveAll(backup)`. So if `guardForeignBackup` says it's safe (empty or looks like review tree), the code removes it. OK.

But `guardForeignBackup` returns an error if the backup is a regular file. The error message says "exists and was not created by atcr". This handles the case of a foreign regular file.

### Issue 28: gate.go - adjudicate interaction with backup

If adjudication is loaded from the live reconDir, and the backup is taken, the adjudication is also in the backup. Then Emit re-emits the reconDir. Does Emit preserve adjudication? Or does it overwrite?

If Emit overwrites adjudication, then the prior adjudication is in the backup, and the new adjudication (if any) is in the live dir. OK.

If Emit doesn't touch adjudication, then both backup and live have the same adjudication. The backup is just a redundant copy.

This is probably fine - the backup is a snapshot, not a guarantee of behavior.

### Issue 29: Potential issue with backup in pipeline.go

In pipeline.go:
```go
+ if err := BackupExistingVerification(reviewDir); err != nil {
+     return Result{}, err
+ }
if err := writeGroupAtomic(artifacts); err != nil {
    return Result{}, err
}
```

`BackupExistingVerification` backs up `reconciled/verification.json` to `reconciled/verification.json.bak`. Then `writeGroupAtomic` writes the new `reconciled/verification.json` (presumably). The backup is taken before the write. Good.

But wait - is `writeGroupAtomic` atomic? If yes, the write either fully succeeds or fully fails. If it fails, the live file is unchanged. If it succeeds, the live file is the new version. The backup holds the prior version. Good.

But what about other files in the group? If they have their own backup semantics, they might backup AFTER `writeGroupAtomic` is called. But based on the diff, only verification.json has a backup in the verify pipeline. So this is OK.

Hmm, but what about findings.json and summary.json? The diff says "the reconcile-owned findings.json/summary.json verify annotates in-place are already covered by reconciled.bak/". So when reconcile runs, it backs up the whole reconciled/ dir. The verify stage only backs up verification.json. This is correct based on the AC definition.

### Issue 30: Error message consistency

The error messages use different formats:
- `"review directory %s already exists; use --resume %s to continue it or --force to overwrite"`
- `"output directory %q already exists and is not empty; use --force to overwrite (or point --output-dir at a new or empty path)"`

The first uses `%s`, the second uses `%q`. Minor inconsistency.

Also, the first names the id, the second doesn't. The first uses `dir` (full path), the second uses `dir` (the user-provided dir). Different semantics.

But this is a minor issue and doesn't affect functionality.

### Issue 31: Potential issue with output-dir that points inside .atcr

The `validateOutputDirRoot` function is called before the backup. So if the output dir is inside .atcr, it errors out before the backup. Good.

### Issue 32: BackupToDotBak when src is a symlink to a non-existent target

`os.Stat` follows symlinks. If the target doesn't exist, `os.Stat` returns `ErrNotExist`. So `BackupToDotBak` returns `("", nil)` - a no-op. But the symlink itself is "broken" and would point to nowhere. Should this be an error? Probably not a critical issue.

### Issue 33: CopyTree skips directory permissions

```go
if d.IsDir() {
    return os.MkdirAll(target, info.Mode().Perm())
}
```

`info.Mode().Perm()` extracts just the permission bits. If the source directory has special bits (setuid, setgid, sticky), they are NOT preserved. For atcr review artifacts, this is fine. For user-provided data, this could be a surprise. But the use case is review artifacts, so it's fine.

### Issue 34: copyTree doesn't preserve timestamps

`os.MkdirAll` and `os.WriteFile` don't preserve modification times. For backup purposes, this might matter for forensic analysis. Minor concern.

### Issue 35: TestRunReconcile_BacksUpPriorReconciledOnReRun - missing reconciled dir check

After the first reconcile, `reconciled/` exists with `findings.json`. The test checks that no `reconciled.bak/` exists. Then it re-runs, and checks that `reconciled.bak/findings.json` exists with the first generation's content.

But what about other files in `reconciled/`? The test only checks `findings.json`. If the backup should include the whole tree, the test should verify more. But this is a test scope issue, not a code issue.

### Issue 36: Pipeline.go - BackupExistingVerification might be called twice

In `WriteVerification` (called from emit_verification.go):
```go
if err := BackupExistingVerification(reviewDir); err != nil {
    return err
}
return atomicfs.WriteFileAtomic(path, data)
```

In `runVerify` (called from pipeline.go):
```go
if err := BackupExistingVerification(reviewDir); err != nil {
    return Result{}, err
}
if err := writeGroupAtomic(artifacts); err != nil {
    return Result{}, err
}
```

Are these both called in the same verify flow? Let me check... `writeGroupAtomic` presumably calls `WriteVerification` for each artifact. If so, the backup is called twice: once explicitly in `runVerify`, and once inside `WriteVerification` via `writeGroupAtomic`. The second call would backup the (now-replaced) verification.json of the new generation. Wait no - `writeGroupAtomic` is atomic, so the live file isn't replaced until the end. Hmm, but the backup was already done.

Actually, let me re-read:
1. `runVerify` calls `BackupExistingVerification` (backs up gen1 to .bak)
2. `runVerify` calls `writeGroupAtomic` which writes the new files atomically
3. `writeGroupAtomic` internally calls `WriteVerification` for verification.json
4. `WriteVerification` calls `BackupExistingVerification` (backs up the (still old, since not yet replaced) verification.json to .bak, replacing the prior .bak)

Wait, no. Let me re-check the order. If `runVerify` calls backup first, then `writeGroupAtomic`:
- After backup: live verification.json is gen1, .bak is gen1
- `writeGroupAtomic` writes new files. If it's truly atomic, it doesn't touch the live file until the end.
- Inside `writeGroupAtomic`, `WriteVerification` is called. It calls `BackupExistingVerification` again. This backs up live (still gen1) to .bak. So .bak is still gen1.
- Then `WriteVerification` writes the new verification.json atomically. Live is now gen2.

So we end up with live=gen2, .bak=gen1. Same result as if we only had one backup call. The double backup is redundant but not incorrect.

But this is wasteful. The double backup copies gen1 twice. For a large file, this is wasted I/O. Not a correctness issue.

Actually wait - let me reconsider. If `writeGroupAtomic` is "atomic" as a group, all files are written via temp + rename. The `WriteVerification` call inside `writeGroupAtomic`:
1. Calls `BackupExistingVerification` - backs up live (gen1) to .bak. .bak = gen1.
2. Calls `atomicfs.WriteFileAtomic(path, data)` - writes to temp, then renames. After this, live = gen2.

So the order is: backup live -> write live. Inside the group, the temp files are created. The live files are not yet replaced (they're replaced only when all temp files are renamed). Hmm, actually I don't know the implementation of `writeGroupAtomic`.

Let me look at the code structure: `writeGroupAtomic(artifacts)` is called. If it writes all files via temp + rename in sequence, then:
- Write artifact 1: create temp1, rename temp1 to live1
- Write artifact 2: create temp2, rename temp2 to live2
- ...

For verification.json, the call would be:
- `WriteVerification` is called for verification.json
- Inside, `BackupExistingVerification` is called: live is still old, .bak is created with old content
- `WriteFileAtomic(path, data)` is called: temp created, renamed to path. Now live is new.

So the backup captures the old (pre-write) content, then the write happens. The backup correctly holds gen1.

But there's still a redundancy: `runVerify` called `BackupExistingVerification` first, then `writeGroupAtomic` called it again inside `WriteVerification`. The first call made .bak = gen1. The second call: live is still gen1 (not yet replaced), so .bak is overwritten with gen1 again. No semantic difference, just wasted I/O.

This is a minor inefficiency, not a correctness issue. But it's worth noting.

Wait, but I need to check if `writeGroupAtomic` is called for verification.json specifically. Looking at the diff:
```go
if !mfNoOp {
    artifacts = append(artifacts, stagingEntry{path: mfPath, data: mfData})
}
+ if err := BackupExistingVerification(reviewDir); err != nil {
+     return Result{}, err
+ }
if err := writeGroupAtomic(artifacts); err != nil {
    return Result{}, err
}
```

The `artifacts` slice has a `stagingEntry` for `mfPath` (the manifest?). What about `verification.json`? If `verification.json` is in `artifacts`, then `writeGroupAtomic` would call `WriteVerification` for it. If not, the explicit `WriteVerification` call happens elsewhere.

Looking at the test in emit_test.go, `WriteVerification` is tested directly. In pipeline.go, the artifacts list doesn't seem to include verification.json. So `WriteVerification` must be called explicitly somewhere.

Hmm, looking at the diff for `runVerify`, the artifacts list is shown partially. The verification.json write might be called via `WriteVerification` somewhere else, or via a different mechanism. Without the full code, I can't tell.

But if `WriteVerification` is called separately from `writeGroupAtomic`, then the explicit `BackupExistingVerification` in `runVerify` is the only backup, and the one inside `WriteVerification` is also there. So if both are called, there's a double backup.

If `WriteVerification` is called inside `writeGroupAtomic` (as part of the atomic group), then the explicit call in `runVerify` is the first, and the one in `WriteVerification` is the second. Double backup.

Either way, double backup is possible. Wasted I/O, not correctness.

### Issue 37: gate.go - missed backup condition

In gate.go:
```go
if _, statErr := os.Stat(filepath.Join(reconDir, FindingsJSON)); statErr == nil {
    if _, err := atomicfs.BackupToDotBak(reconDir); err != nil {
        return Result{}, fmt.Errorf("backing up prior reconciled output: %w", err)
    }
}
```

The backup is only triggered if `findings.json` exists. What if `reconciled/` exists with other files but no `findings.json`? The backup is skipped. Is this correct?

If reconcile has been run before, `findings.json` should exist. If `reconciled/` exists but is empty, it's a scaffold-only state, no need to backup. If `reconciled/` has other files but no `findings.json`, that's an inconsistent state. The current code skips backup in this case. This is probably fine.

### Issue 38: The --force flag has no effect for derived IDs

The docstring in `Force` says "derived ids never collide (claimReviewDir auto-suffixes) so Force is a no-op there." So Force is a no-op for derived IDs. But the user might still pass --force with a derived ID. The code doesn't error, it just doesn't do anything. This is silent. Could be a usability concern but not a bug.

### Issue 39: README documentation accuracy

The README says:
> Re-running an explicit `--id` (or a non-empty `--output-dir`) whose directory already exists is rejected; the error names the two ways forward — `--resume <id>` to continue it non-destructively, or `--force` to back the prior tree up to `<dir>.bak` and start fresh.

This is accurate.

> `--resume` and `--force` are mutually exclusive (opposite collision resolutions).

This is accurate.

### Issue 40: ScaffoldReviewDir error message format

```go
return "", fmt.Errorf("review directory %s already exists; use --resume %s to continue it or --force to overwrite", dir, id)
```

The `dir` includes the full path (e.g., `.atcr/reviews/2026-06-10_dup`), and `id` is the bare id. The user gets both, which is informative. But the message could be a bit confusing because it mentions both the full path and the bare id. Minor concern.

### Issue 41: BackupToDotBak doesn't follow symlinks for tree, but does for root

As I noted earlier, `os.Stat` follows symlinks. So if `src` is a symlink to a directory, `copyTree` is called with `src` being the symlink. `filepath.WalkDir(src, ...)` follows the symlink and walks the linked directory. Then for each entry inside, the symlink is checked via `d.Info()` and `info.Mode().IsRegular()`. If an entry is a symlink, it's skipped. So the root symlink is followed, but inner symlinks are skipped. This is slightly inconsistent but probably fine.

### Issue 42: Possible issue with BackupToDotBak preserving source permissions

`copyFile` uses `info.Mode().Perm()` from the source's `Stat`. This is the source file's permission, not the destination's. For backup purposes, this is correct - preserve the source's permissions.

### Issue 43: Test for failure mode where marshal fails

The test `TestWriteJSON_UnmarshalableValueErrorsAndWritesNothing` uses `make(chan int)` which can't be marshaled. The test verifies that no file is created. Good edge case.

### Issue 44: Test for failure mode in WriteFileAtomic with missing parent dir

The test `TestWriteFileAtomic_MissingParentDirErrors` writes to a non-existent parent dir. The test expects an error. Good edge case.

### Summary of potential issues:

After thorough analysis, the main issues I can identify are:

1. **MEDIUM: Double backup in verify pipeline** - `BackupExistingVerification` may be called twice in the verify flow, leading to wasted I/O.

2. **LOW: Inconsistent
</think>
