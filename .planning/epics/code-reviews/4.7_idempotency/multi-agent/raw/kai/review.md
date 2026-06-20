The user wants me to review a diff and output findings in a specific pipe-delimited format. Let me analyze the diff carefully against the sprint plan scope.

Scope constraint: Only flag issues in files/areas directly related to the work items in the sprint plan. The sprint plan covers:
- Atomic file writes (`internal/atomicfs` package, extend existing)
- Review directory collision detection in `cmd/atcr/review.go`
- `--force` flag to overwrite existing review directory
- Backup before overwrite (`*.bak` files)
- Error message when output directory already exists
- Unit tests for atomic writes
- Integration test: run review twice, verify backup created
- `internal/reconcile/reconcile.go` to backup `reconciled/` before overwrite (actually `internal/reconcile/gate.go` based on diff)
- `internal/verify/verify.go` to backup `verified.json` before overwrite (actually `internal/verify/emit_verification.go` and `internal/verify/pipeline.go` based on diff)

Files in diff:
1. `README.md` - documentation, not code issues
2. `cmd/atcr/review.go` - in scope
3. `cmd/atcr/review_test.go` - in scope
4. `internal/atomicfs/atomic.go` - in scope
5. `internal/atomicfs/atomic_test.go` - in scope
6. `internal/fanout/review.go` - in scope (review directory collision detection)
7. `internal/fanout/review_test.go` - in scope
8. `internal/fanout/reviewdir.go` - in scope
9. `internal/fanout/reviewdir_test.go` - in scope
10. `internal/reconcile/gate.go` - in scope
11. `internal/reconcile/gate_test.go` - in scope
12. `internal/verify/emit_test.go` - in scope
13. `internal/verify/emit_verification.go` - in scope
14. `internal/verify/pipeline.go` - in scope

Now let me look for issues:

1. `cmd/atcr/review.go`:
   - Line 176: `Force: boolFlag(cmd, "force")` - this is fine.
   - The mutual exclusion check at line 90-92 looks correct.
   - No obvious issues here.

2. `internal/atomicfs/atomic.go`:
   - `WriteJSON` function: `append(data, '\n')` - this creates a new slice but it's fine. However, `append(data, '\n')` could potentially reallocate and copy, but it's going to `WriteFileAtomic` which takes `[]byte`. This is fine.
   - `BackupToDotBak`: 
     - Uses `os.RemoveAll(bak)` which can be dangerous but is documented.
     - Uses `copyTree` which uses `filepath.WalkDir`.
     - In `copyTree`, when it encounters a directory, it does `os.MkdirAll(target, info.Mode().Perm())`. This looks correct.
     - But wait: `filepath.WalkDir` visits the root `src` itself. For `src`, `rel` will be `.` and `target` will be `filepath.Join(dst, ".")` which is `dst`. Then if it's a dir, it does `os.MkdirAll(dst, perm)`. That's fine.
     - However, there's a subtle issue: `copyTree` doesn't preserve the exact permissions of the root directory itself? Wait, `os.MkdirAll` uses the perm, so it should be fine.
     - Actually, looking at `BackupToDotBak` for directories: it calls `copyTree(src, bak)`. `copyTree` walks `src`, computes `rel`, makes `target`. For the root dir, `rel="."`, `target=filepath.Join(dst, ".") = dst`. It then calls `os.MkdirAll(target, info.Mode().Perm())` which is `os.MkdirAll(dst, perm)`. That's correct.
     - Wait, I see a potential issue in `copyTree`: the root directory `src` is visited first. `rel` is `"."`. `target` is `dst`. `d.IsDir()` is true. It calls `os.MkdirAll(dst, info.Mode().Perm())`. Good.
     - But what about files? `copyFile` uses `os.WriteFile(dst, data, perm)`. However, if the parent directory doesn't exist (which shouldn't happen in `copyTree` since WalkDir visits dirs before files), but `os.WriteFile` will fail if parent doesn't exist. In `filepath.WalkDir`, dirs are visited before their contents, so parent dirs are created before files. Good.
     - One issue: `copyFile` reads entire file into memory. For large files this could be a problem, but review artifacts are small. The sprint plan says review artifacts are plain files and directories.
     - Is there a security issue? `os.RemoveAll(bak)` in `BackupToDotBak` and `backupExisting` - this is by design.
     - Wait, `backupExisting` in `reviewdir.go` uses `os.RemoveAll(backup)` then `os.Rename(path, backup)`. This is a TOCTOU issue? Not really, it's sequential.
     - Actually, looking at `internal/atomicfs/atomic.go` `BackupToDotBak`: it checks if source exists. If source is a symlink, `os.Stat` follows it. Then `info.IsDir()` could be true if it points to a dir, and it would call `copyTree` on the symlink target. But wait, `copyTree` starts with `src` which is the symlink target path, and `WalkDir` on a symlink target... actually `src` is the resolved path if `Stat` followed the link. So it copies the target content. This seems okay.
     - However, in `copyTree`, `d.Info()` might return info about the symlink if `WalkDir` is on a tree containing symlinks. But the sprint plan says non-regular entries are skipped. In `copyTree`, after `d.IsDir()` check, it checks `if !info.Mode().IsRegular() { return nil }`. For symlinks, `IsRegular()` is false, so they are skipped. Good.

3. `internal/fanout/reviewdir.go`:
   - `backupExisting` function: uses `os.RemoveAll(backup)` then `os.Rename(path, backup)`. 
   - Wait, if `path` is a directory and `backup` exists as a directory, `os.Rename` on most Unix systems can replace an empty directory, but cannot replace a non-empty directory. That's why they `RemoveAll` first. But between `RemoveAll` and `Rename`, something else could create `backup`. However, this is a narrow window and the tool is single-user.
   - More importantly: `forceBackupOutputDir` calls `guardForeignBackup(dir + ".bak")`. But `backupExisting` unconditionally calls `os.RemoveAll(backup)`. So if `guardForeignBackup` passes, then `backupExisting` removes it. This is intended behavior.
   - `guardForeignBackup`: checks if backup exists but was not created by atcr. If it's empty or looks like review tree, it's allowed. If it has entries but doesn't look like review tree, it errors.
   - However, `looksLikeReviewTree` checks `for _, sub := range reviewSubdirs { fi, err := os.Stat(filepath.Join(dir, sub)); if err != nil || !fi.IsDir() { return false } }`. What if a foreign directory happens to have subdirs with the same names? The names are things like "sources", "agents", "reconciled" etc. It's possible but unlikely. The sprint plan accepts this risk.
   - Wait, I see an issue in `forceBackupReviewDir`: 
     ```go
     func forceBackupReviewDir(root, id string) error {
         dir := filepath.Join(ReviewsRoot(root), id)
         if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
             return nil
         } else if err != nil {
             return fmt.Errorf("checking review directory before --force backup: %w", err)
         }
         _, err := backupExisting(dir)
         return err
     }
     ```
     The `else if err != nil` handles the error case, but then `_, err := backupExisting(dir)` uses `err` which shadows? No, `err` is redeclared with `:=` but that's fine. Actually, the `err` from `os.Stat` is checked, then `backupExisting` is called. Looks correct.
   - But wait: in `ScaffoldReviewDir`, the error message changed from old to new. The new message is `fmt.Errorf("review directory %s already exists; use --resume %s to continue it or --force to overwrite", dir, id)`. This names both `--resume` and `--force`. Good.
   - In `ScaffoldOutputDir`, the error message changed to `fmt.Errorf("output directory %q already exists and is not empty; use --force to overwrite (or point --output-dir at a new or empty path)", dir)`. Good.
   - However, looking at `PrepareReview` in `review.go` (fanout):
     ```go
     case req.OutputDir != "":
         if err = validateOutputDirRoot(req.OutputDir, req.Root); err != nil {
             return nil, err
         }
         if req.Force {
             if err = forceBackupOutputDir(req.OutputDir); err != nil {
                 return nil, err
             }
         }
         dir, err = ScaffoldOutputDir(req.OutputDir)
     ```
     If `req.Force` is true, it calls `forceBackupOutputDir`. But `forceBackupOutputDir` only backups if entries > 0. Then `ScaffoldOutputDir` is called. `ScaffoldOutputDir` checks if dir exists and is non-empty. If `forceBackupOutputDir` moved it aside (renamed to .bak), then `ScaffoldOutputDir` will see it doesn't exist, and create it. Wait, but `forceBackupOutputDir` uses `backupExisting` which does `os.Rename(path, backup)` - so the dir is moved, not copied. So the path is vacant. Then `ScaffoldOutputDir` sees it doesn't exist and creates it. Good.
     - But for managed review dirs (`case req.IDOverride != ""`), it calls `forceBackupReviewDir` (which uses `backupExisting` = rename), then `ScaffoldReviewDir` (which calls `os.Mkdir`). Since the dir was moved aside, `os.Mkdir` succeeds. Good.

4. `internal/reconcile/gate.go`:
   - Uses `atomicfs.BackupToDotBak(reconDir)` which copies (not moves) the directory. This is different from `backupExisting` which moves. The comment says "Copy (not move) so the live dir stays intact for the adjudication reads and Emit below." This is intentional and correct because reconcile reads from the directory during Emit.
   - However, there's a subtle issue: `BackupToDotBak` copies the tree. But between the copy and the Emit, the live directory is still there. The copy is a point-in-time snapshot, but if files are modified during the copy, the backup could be inconsistent. But this is a best-effort backup and the sprint plan accepts this.
   - Wait, `atomicfs.BackupToDotBak` calls `os.RemoveAll(bak)` first. If `bak` is a directory and contains files, `RemoveAll` deletes them. Then `copyTree` copies from `src` to `bak`. During this copy, if the process dies, `bak` could be partially populated. But that's acceptable for a backup.
   - Actually, looking at `BackupToDotBak`: it first `os.Stat(src)`, then `os.RemoveAll(bak)`, then copies. There's a race condition if `src` is modified between `os.Stat` and copy, but again, single-user tool, best effort.

5. `internal/verify/emit_verification.go`:
   - `WriteVerification` now calls `BackupExistingVerification(reviewDir)` before `atomicfs.WriteFileAtomic(path, data)`.
   - But wait, `BackupExistingVerification` calls `atomicfs.BackupToDotBak(path)`. `BackupToDotBak` does `os.Stat(src)`. If exists, it copies to `.bak`. This is fine.
   - However, in `internal/verify/pipeline.go`, `runVerify` also calls `BackupExistingVerification(reviewDir)` before `writeGroupAtomic(artifacts)`. But `writeGroupAtomic` might write `verification.json` as part of the atomic group. And `WriteVerification` (called from elsewhere?) also calls `BackupExistingVerification`. 
   - Wait, the diff shows changes to both `emit_verification.go` and `pipeline.go`. In `pipeline.go`, the backup is done before `writeGroupAtomic`. In `emit_verification.go`, `WriteVerification` also calls `BackupExistingVerification`. This means if `WriteVerification` is called from `pipeline.go`'s `writeGroupAtomic`... let me check.
   - Actually, looking at `pipeline.go`:
     ```go
     if !mfNoOp {
         artifacts = append(artifacts, stagingEntry{path: mfPath, data: mfData})
     }
     if err := BackupExistingVerification(reviewDir); err != nil {
         return Result{}, err
     }
     if err := writeGroupAtomic(artifacts); err != nil {
         return Result{}, err
     }
     ```
     And `emit_verification.go` has `WriteVerification` which includes the backup call. If `pipeline.go` uses `writeGroupAtomic` to write verification.json, does it also call `WriteVerification`? Or does `WriteVerification` stand alone?
     - Looking at `emit_verification.go`, `WriteVerification` marshals and writes. In `pipeline.go`, it seems to be using a different mechanism (`writeGroupAtomic`). The diff added `BackupExistingVerification` call in `pipeline.go` before `writeGroupAtomic`. But `BackupExistingVerification` is also called inside `WriteVerification`. So if some caller uses `WriteVerification`, it gets backed up. If `pipeline.go` uses its own write mechanism, it also gets backed up via the explicit call.
     - But wait, is there a double-backup possibility? If `pipeline.go` calls `BackupExistingVerification`, then later some code calls `WriteVerification`, it would backup again. But that's fine, the second backup would overwrite the first `.bak` with the same content (or the content from before the first backup, which is also the same). Actually, no: first backup copies live to .bak. Then writeGroupAtomic writes new content. Then if WriteVerification is called, it would backup the new content to .bak, overwriting the prior backup. But are they called in sequence like that? Probably not. The tests seem to call `WriteVerification` directly. And `pipeline.go` uses `writeGroupAtomic`. So there might be two entry points, both guarded. This seems correct.

6. Looking for real issues:
   - In `cmd/atcr/review.go` line 90: `if cmd.Flags().Changed("force")` - this checks if the flag was changed. But what if force is set to true via default or environment? The flag is defined as `Bool` with default false. `Changed` only returns true if the user explicitly set it. This is correct for detecting mutual exclusion.
   - Wait, but `runReview` uses `boolFlag(cmd, "force")` at line 180. `boolFlag` probably returns the boolean value. Since default is false, this is fine.

   - In `internal/fanout/reviewdir.go`, `backupExisting` function:
     ```go
     func backupExisting(path string) (string, error) {
         backup := path + ".bak"
         if err := os.RemoveAll(backup); err != nil {
             return "", fmt.Errorf("removing stale backup %q: %w", backup, err)
         }
         if err := os.Rename(path, backup); err != nil {
             return "", fmt.Errorf("backing up %q: %w", path, err)
         }
         return backup, nil
     }
     ```
     If `path` doesn't exist, `os.Rename` will return an error. But `forceBackupReviewDir` checks existence before calling `backupExisting`. `forceBackupOutputDir` also checks existence (entries > 0). So `backupExisting` is only called when path exists. Good.

   - Wait! In `internal/fanout/reviewdir.go`, `guardForeignBackup`:
     ```go
     func guardForeignBackup(backup string) error {
         entries, err := os.ReadDir(backup)
         if errors.Is(err, fs.ErrNotExist) {
             return nil
         }
         if err != nil {
             // Exists but is not a readable directory (e.g. a regular file): treat as
             // foreign and refuse rather than RemoveAll it.
             return fmt.Errorf("refusing --force: %q exists and was not created by atcr; move or remove it first", backup)
         }
         if len(entries) == 0 || looksLikeReviewTree(backup) {
             return nil
         }
         return fmt.Errorf("refusing --force: %q already exists and does not look like an atcr backup; move or remove it first", backup)
     }
     ```
     If `backup` is a symlink to a directory, `os.ReadDir` will follow it and read the target's entries. `looksLikeReviewTree` uses `os.Stat` which follows symlinks. This might be acceptable, but if `backup` is a symlink to something else, it could be followed. However, the sprint plan doesn't mention symlink handling specifically.

   - In `internal/reconcile/gate.go`:
     ```go
     if _, statErr := os.Stat(filepath.Join(reconDir, FindingsJSON)); statErr == nil {
         if _, err := atomicfs.BackupToDotBak(reconDir); err != nil {
             return Result{}, fmt.Errorf("backing up prior reconciled output: %w", err)
         }
     }
     ```
     The backup copies the entire `reconciled/` directory. This includes `AdjudicationJSON` if present. But then later in the same function:
     ```go
     adjudicating := false
     if _, statErr := os.Stat(filepath.Join(reconDir, AdjudicationJSON)); statErr == nil {
         adj, err := LoadAdjudication(...)
         ...
     }
     ```
     Wait, the backup happens before checking adjudication. But `reconDir` still exists (since BackupToDotBak copies, not moves). So `LoadAdjudication` will still find the file. Good.
     - However, there's a subtle bug: `BackupToDotBak` creates `reconciled.bak/`. But if `reconciled/` contains a subdirectory `reconciled.bak/` (impossible since it's a sibling name with .bak suffix), but no, `BackupToDotBak` uses `src + ".bak"`. For `reconciled/`, it becomes `reconciled.bak/`. This is fine.

   - In `internal/verify/emit_verification.go`:
     ```go
     func BackupExistingVerification(reviewDir string) error {
         path := filepath.Join(reviewDir, reconciledSubdir, "verification.json")
         if _, err := atomicfs.BackupToDotBak(path); err != nil {
             return fmt.Errorf("backing up prior verification.json: %w", err)
         }
         return nil
     }
     ```
     `atomicfs.BackupToDotBak` returns `("", nil)` if source doesn't exist. But `BackupExistingVerification` ignores the return value. That's fine, it's a no-op.

   - In `internal/atomicfs/atomic_test.go`: The tests look thorough. But wait:
     `TestWriteFileAtomic_OrphanedTempDoesNotCorruptTarget` creates an orphan temp file. It checks that the target is untouched. Then it calls `WriteFileAtomic` and checks the result. This is fine.

   - Is there any issue with the `copyTree` function in `atomic.go`? Let me re-examine.
     ```go
     func copyTree(src, dst string) error {
         return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
             if err != nil {
                 return err
             }
             rel, err := filepath.Rel(src, path)
             if err != nil {
                 return err
             }
             target := filepath.Join(dst, rel)
             info, err := d.Info()
             if err != nil {
                 return err
             }
             if d.IsDir() {
                 return os.MkdirAll(target, info.Mode().Perm())
             }
             if !info.Mode().IsRegular() {
                 return nil
             }
             return copyFile(path, target, info.Mode().Perm())
         })
     }
     ```
     `filepath.Rel(src, path)` where `path` is `src` itself gives `"."`. `target` becomes `filepath.Join(dst, ".")` which is `dst` (with a possible trailing separator difference, but `filepath.Join` should normalize it). So for the root directory, it creates `dst`. Good.
     But what if `dst` already exists? `os.MkdirAll` won't error if it exists. Good.
     However, there's a bug: `filepath.WalkDir` visits the root `src` first. At that point, `d.IsDir()` is true, so it calls `os.MkdirAll(dst, perm)`. Then it visits the children. This is correct.
     Wait, but `os.MkdirAll` takes a perm argument, but also applies umask. The original directory might have different permissions than intended after umask. But `MkdirAll` creates all directories with the given perm (masked by umask). This is generally acceptable.
     
     Actually, I see a potential correctness issue in `copyTree`: if `src` is a directory containing a file with no read permission for the current user, `copyFile` will fail to `os.ReadFile` it. But that's expected behavior.

     Wait, is there an issue with `WriteFileAtomic`? The existing function:
     ```go
     func WriteFileAtomic(path string, data []byte) error {
         dir := filepath.Dir(path)
         tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
         ...
         return os.Rename(tmpName, path)
     }
     ```
     On Windows, `os.Rename` might not be atomic if the target exists. But the sprint plan mentions atomic rename, and Go's `os.Rename` on Windows replaces the target atomically in recent Go versions? Actually, on Windows, renaming over an existing file is not atomic in older Windows versions, but in Go 1.5+ it's implemented with MoveFileExW which is atomic for files on the same volume. For directories, it's different. But `WriteFileAtomic` writes files, not directories. So it's fine.

Let me look for issues that are more concrete and in scope:

1. **Race condition / TOCTOU in `backupExisting`**: In `reviewdir.go`, `backupExisting` does `os.RemoveAll(backup)` then `os.Rename(path, backup)`. If `path` is a directory and between `RemoveAll` and `Rename`, another process creates `backup`, the `Rename` might fail. But more importantly, if `backup` is recreated as a non-empty directory after `RemoveAll`, `os.Rename` will fail. However, this is a CLI tool, not a server, so concurrency is minimal.

2. **In `forceBackupOutputDir`**: It checks `len(entries) == 0` to skip empty dirs. But what if the dir exists and contains only a `.bak` directory or hidden files? `os.ReadDir` returns all entries. If the user has a `.gitignore` in the output dir, `len(entries) > 0`, so it will try to backup. That's correct.

3. **In `internal/fanout/review.go` `PrepareReview`**:
   Wait, I see something. In the `case req.IDOverride != ""` branch:
   ```go
   if req.Force {
       if err = forceBackupReviewDir(req.Root, id); err != nil {
           return nil, err
       }
   }
   dir, err = ScaffoldReviewDir(req.Root, id)
   ```
   And in `case req.OutputDir != ""`:
   ```go
   if req.Force {
       if err = forceBackupOutputDir(req.OutputDir); err != nil {
           return nil, err
       }
   }
   dir, err = ScaffoldOutputDir(req.OutputDir)
   ```
   These look correct.

4. **In `cmd/atcr/review.go`**: 
   The mutual exclusion check:
   ```go
   if cmd.Flags().Changed("force") {
       return usageError(errors.New("--resume and --force are mutually exclusive"))
   }
   ```
   This is inside the `if cmd.Flags().Changed("resume")` block. So it checks if both are changed. Good.

5. **In `internal/verify/pipeline.go`**:
   ```go
   if err := BackupExistingVerification(reviewDir); err != nil {
       return Result{}, err
   }
   if err := writeGroupAtomic(artifacts); err != nil {
       return Result{}, err
   }
   ```
   But `writeGroupAtomic` probably writes multiple files atomically as a group. Does it write `verification.json`? The comment says "a re-verify (e.g. --fresh) overwrites verification.json as part of this atomic group". So yes, `BackupExistingVerification` is called before the group write. Good.

6. **Missing error handling / bug in `WriteJSON`**:
   `WriteJSON` does:
   ```go
   data, err := json.MarshalIndent(v, "", "  ")
   if err != nil {
       return err
   }
   return WriteFileAtomic(path, append(data, '\n'))
   ```
   This is fine. But note that `append(data, '\n')` might reallocate. However, it's passed to `WriteFileAtomic`. No issue.

7. **Issue in `BackupToDotBak`**: 
   ```go
   func BackupToDotBak(src string) (string, error) {
       info, err := os.Stat(src)
       if errors.Is(err, fs.ErrNotExist) {
           return "", nil
       }
       if err != nil {
           return "", err
       }
       bak := src + ".bak"
       if err := os.RemoveAll(bak); err != nil {
           return "", fmt.Errorf("removing stale backup %s: %w", bak, err)
       }
       if info.IsDir() {
           if err := copyTree(src, bak); err != nil {
               return "", fmt.Errorf("backing up %s: %w", src, err)
           }
       } else {
           if err := copyFile(src, bak, info.Mode().Perm()); err != nil {
               return "", fmt.Errorf("backing up %s: %w", src, err)
           }
       }
       return bak, nil
   }
   ```
   Wait! `copyFile` uses `os.WriteFile(dst, data, perm)`. But `os.WriteFile` truncates if the file exists, or creates it with the given permissions. However, if `dst`'s parent directory doesn't exist, `os.WriteFile` will fail. In `BackupToDotBak`, for files, `bak` is `src + ".bak"`. If `src` is a file in a directory, `bak` is in the same directory. So parent exists. Good.
   But for `copyTree`, what about file permissions? It passes `info.Mode().Perm()` to `copyFile`. `copyFile` uses `os.WriteFile(dst, data, perm)`. On Unix, this creates the file with `perm & ^umask`. But the original file might have had its permissions set explicitly after creation. However, `info.Mode().Perm()` is the current permission, so it should be preserved as best as possible. This is acceptable.

8. **Potential data loss in `backupExisting`**:
   In `reviewdir.go`, `backupExisting` uses `os.Rename(path, backup)`. This is a move, not a copy. If the process crashes after the rename but before scaffolding the new dir, the review directory is gone (moved to .bak). But that's the intended behavior for `--force`: the user asked to overwrite. However, for `OutputDir`, the user might not realize it's a move. But the sprint plan says "backs up existing directory to `<dir>.bak`". `os.Rename` effectively moves it. The comment says "moves path aside to path+.bak". This is fine.

9. **Issue: `guardForeignBackup` logic flaw?**
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
   If `backup` is a file (not directory), `os.ReadDir` returns an error. Then it returns the "refusing --force" error. Good.
   But what if `backup` is a directory with entries, and `looksLikeReviewTree` returns false because it's missing one subdir. Then it refuses. Good.
   What if `looksLikeReviewTree` encounters an error inside `os.Stat`? It returns false, which causes refusal. That might be too strict but safe.

10. **Issue in `ScaffoldOutputDir`**:
    The error message says `already exists and is not empty`. But if `dir` exists and is a file, `os.ReadDir` will return an error? Actually, `os.ReadDir` works on files? No, `os.ReadDir` returns entries of a directory. If `dir` is a file, `os.ReadDir` returns an error. But the code:
    ```go
    entries, readErr := os.ReadDir(dir)
    if readErr != nil {
        if errors.Is(readErr, fs.ErrNotExist) {
            return "", fmt.Errorf("failed to create review directory: %w", readErr)
        }
        return "", fmt.Errorf("failed to create review directory: %w", readErr)
    }
    ```
    If `dir` is a file, `ReadDir` returns an error like "not a directory", and it returns that error. This is acceptable.

11. **Real issue found**: In `internal/verify/pipeline.go`, there is a call to `BackupExistingVerification` before `writeGroupAtomic`. But there is also `WriteVerification` in `emit_verification.go` which includes its own backup call. If `pipeline.go`'s `writeGroupAtomic` calls `WriteVerification` internally, there could be a double backup. But looking at the diff, `writeGroupAtomic` is not shown, so I can't tell. However, `emit_verification.go`'s `WriteVerification` function is exported and likely used by tests and possibly other callers. The duplication of backup logic in both `WriteVerification` and `pipeline.go` might be intentional to ensure all paths are covered, but it could lead to confusion.

Actually, looking more carefully at `emit_verification.go`:
```go
func WriteVerification(reviewDir string, results []VerificationResult) error {
    ...
    if err := BackupExistingVerification(reviewDir); err != nil {
        return err
    }
    return atomicfs.WriteFileAtomic(path, data)
}
```

And `pipeline.go` calls `BackupExistingVerification` explicitly, then presumably writes verification.json through `writeGroupAtomic`, not through `WriteVerification`. So the backup in `WriteVerification` is for callers of `WriteVerification` (like tests), and the backup in `pipeline.go` is for the pipeline path. This is acceptable duplication since they are different entry points.

Wait, but is there an issue where `pipeline.go` backups, then writes via `writeGroupAtomic`, but if `writeGroupAtomic` fails, the `.bak` file remains with the old content, and the live file might be in an inconsistent state (since `writeGroupAtomic` is atomic for the group, but the backup is already done). This is fine; the backup preserves the previous state.

12. **Potential high-severity issue in `copyTree`**: 
    `copyTree` uses `filepath.WalkDir(src, ...)`. `filepath.WalkDir` is recursive. It passes `path` which is the full path to the file. `rel, err := filepath.Rel(src, path)`. If `src` is `/a/b` and `path` is `/a/b/c/d`, `rel` is `c/d`. `target` is `filepath.Join(dst, rel)`. Good.
    However, `filepath.Rel` can return an error if `src` and `path` are not on the same root or if one is not a prefix of the other. But since `WalkDir` walks under `src`, this shouldn't happen unless `src` contains `.` or `..` components? Actually, `WalkDir` calls the function with cleaned paths. `filepath.Rel` should work.
    
    But wait: what if `src` is relative and contains symlinks? `WalkDir` doesn't follow symlinks (by default), but if `src` itself is a symlink to a directory, `WalkDir` follows it? Actually, `WalkDir` walks the file tree rooted at root, which evaluates the root but not symlinks encountered during the walk. If `src` is a symlink, `WalkDir` might not follow it depending on the OS. But `BackupToDotBak` does `os.Stat(src)`, which follows symlinks. If `src` is a symlink to a directory, `info.IsDir()` is true, and `copyTree` is called with the symlink target path? No, `src` is still the symlink path. `WalkDir` on a symlink might or might not follow it. On Unix, `WalkDir` calls `lstat` on the root, so if root is a symlink, it's not followed? Actually, I need to check Go docs. `filepath.WalkDir` uses `os.Lstat` for the root and `os.ReadDir` for directories. `os.ReadDir` returns `DirEntry` values. `DirEntry.Info()` calls `lstat` or `stat` depending on whether the info is already cached. For the root, if it's a symlink, `WalkDir` will see it's a symlink (not a dir) and won't traverse it. But `BackupToDotBak` only calls `copyTree` when `info.IsDir()` is true, which means `os.Stat` followed the symlink and found a directory. But `WalkDir` might still not follow the root if it's a symlink. This is a subtle edge case, but the sprint plan says review artifacts are plain files and directories.

13. **Issue in `internal/atomicfs/atomic.go` `copyFile`**:
    ```go
    func copyFile(src, dst string, perm os.FileMode) error {
        data, err := os.ReadFile(src)
        if err != nil {
            return err
        }
        return os.WriteFile(dst, data, perm)
    }
    ```
    This reads the entire file into memory. For very large files, this could cause OOM. But review artifacts are JSON/text files, expected to be small. However, this is a general utility in `atomicfs`. The sprint plan says "review artifacts are plain files and directories, so there is nothing to lose", but `atomicfs` is a general package. Still, since it's in scope and used for backups, large files could be problematic.
    - Actually, is this a real issue? The sprint plan explicitly mentions atomic writes for JSON files and backups for review directories. The review directories are small. So maybe not worth flagging.

14. **Issue in `backupExisting` error handling**:
    In `reviewdir.go`:
    ```go
    func backupExisting(path string) (string, error) {
        backup := path + ".bak"
        if err := os.RemoveAll(backup); err != nil {
            return "", fmt.Errorf("removing stale backup %q: %w", backup, err)
        }
        if err := os.Rename(path, backup); err != nil {
            return "", fmt.Errorf("backing up %q: %w", path, err)
        }
        return backup, nil
    }
    ```
    If `path` is a directory and `backup` is a file (or vice versa), `os.Rename` behavior is platform-dependent. On Unix, you can rename a directory over an empty directory, but not over a file. But `RemoveAll` already removed `backup`. So after `RemoveAll`, `backup` doesn't exist. Then `Rename` should work regardless of whether path is a file or directory.
    However, if `path` is a file and `backup` is a directory, `RemoveAll` removes the directory. Then `Rename(path, backup)` renames the file to the directory name. This creates a file named `backup`. That's fine.
    So no issue there.

15. **Looking at `cmd/atcr/review_test.go`**:
    ```go
    func TestReviewCmd_ResumeAndForceMutuallyExclusive(t *testing.T) {
        isolate(t)
        code, out := execCmdCapture(t, "review", "--resume", "latest", "--force")
        require.Equal(t, 2, code, "AC1b: --resume + --force is a usage error (exit 2)")
        require.Contains(t, out, "--resume and --force are mutually exclusive")
    }
    ```
    This test doesn't explicitly test the reverse order (`--force` then `--resume`), but the code checks `Changed("resume")` first, then `Changed("