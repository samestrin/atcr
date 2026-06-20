Let me analyze the diff carefully against the sprint plan scope.

Sprint plan scope:
- `internal/atomicfs` - WriteJSON + unit tests ✓
- `cmd/atcr/review.go` - --force flag, collision error naming both --resume and --force, mutual exclusion ✓
- Review-dir backup to `<dir>.bak` ✓
- Reconcile `reconciled/` → `reconciled.bak/` backup before re-emit ✓
- Verify `verification.json` → `verification.json.bak` backup before re-write ✓
- Integration tests ✓

Let me review each file for issues.

**internal/atomicfs/atomic.go** - New `WriteJSON` and `BackupToDotBak`:

1. `copyFile` reads entire file into memory via `os.ReadFile` then `os.WriteFile`. For large files this could be a memory issue, but review artifacts are small. Not a real issue.

2. `BackupToDotBak` - when `src` is a directory, it uses `copyTree`. The `copyTree` function uses `filepath.WalkDir`. It creates directories with `os.MkdirAll`. But there's a subtle issue: when walking, for the root directory itself, `filepath.Rel(src, path)` returns "." and `filepath.Join(dst, ".")` = dst. Then `os.MkdirAll(target, info.Mode().Perm())` is called. That's fine.

3. `copyFile` doesn't preserve permissions correctly via `os.WriteFile` - actually it does pass perm. But `os.WriteFile` will create the file with the given perm (masked by umask). This could result in different actual permissions. Minor issue.

4. In `copyTree`, symlinks are skipped silently. The doc says so. OK.

5. **Potential issue**: `BackupToDotBak` for a directory does `os.RemoveAll(bak)` then `copyTree`. But if `bak` is a file (not a directory), `os.RemoveAll` handles both. OK.

6. **Race condition / TOCTOU**: `BackupToDotBak` stats src, then copies. Not really a concern here.

**internal/fanout/reviewdir.go** - `backupExisting`, `forceBackupReviewDir`, `forceBackupOutputDir`, `guardForeignBackup`, `looksLikeReviewTree`:

1. `backupExisting` does `os.RemoveAll(backup)` then `os.Rename(path, backup)`. This is not atomic - if the process is killed between RemoveAll and Rename, the backup is lost AND the original is still there. Actually wait - the original is still there until Rename. So if killed between RemoveAll and Rename, the backup is gone but original is intact. That's acceptable.

2. **Issue**: `forceBackupOutputDir` calls `guardForeignBackup` which checks if the backup looks like a review tree. But `looksLikeReviewTree` only checks for subdirs. If a user's backup happens to contain those subdirs (unlikely), it could be destroyed. This is a heuristic. Acceptable per design.

3. **Issue in `forceBackupOutputDir`**: It reads entries, checks if empty, then guards foreign backup, then calls `backupExisting`. But `backupExisting` does `os.RemoveAll(backup)` unconditionally. The guard checks the backup BEFORE calling backupExisting. So the flow is: guard checks backup is OK to remove → backupExisting removes it and renames. There's a TOCTOU but minor.

4. **Important issue**: In `forceBackupReviewDir`, it checks if dir exists, then calls `backupExisting`. But `ScaffoldReviewDir` is called AFTER, which does `os.Mkdir`. Between `backupExisting` (rename away) and `Mkdir`, if another process creates the dir, there's a race. Minor.

5. **`looksLikeReviewTree`** - checks all `reviewSubdirs` exist as directories. This is the marker. Reasonable.

**internal/fanout/review.go** - `PrepareReview`:

The `--force` logic:
- For OutputDir path: if Force, call `forceBackupOutputDir` then `ScaffoldOutputDir`.
- For IDOverride path: if Force, call `forceBackupReviewDir` then `ScaffoldReviewDir`.

Wait - there's a subtle issue. `ScaffoldOutputDir` checks if dir is non-empty and errors. After `forceBackupOutputDir` moves the dir aside to `.bak`, the dir no longer exists (it was renamed). So `ScaffoldOutputDir` will try to create it fresh. Let me check `ScaffoldOutputDir`:

```go
func ScaffoldOutputDir(dir string) (string, error) {
    if err := os.MkdirAll(dir, 0o755); err != nil { ... }
    entries, err := os.ReadDir(dir)
    ...
    if len(entries) > 0 {
        return "", fmt.Errorf("output directory %q already exists and is not empty; use --force to overwrite...")
    }
    ...
}
```

After `forceBackupOutputDir` renames `dir` to `dir.bak`, `dir` no longer exists. `MkdirAll` creates it empty. `ReadDir` returns empty. OK, works.

For IDOverride: `forceBackupReviewDir` renames the review dir to `.bak`. Then `ScaffoldReviewDir` does `os.Mkdir(dir, 0o755)`. Since dir was moved aside, Mkdir creates it fresh. OK.

**Issue**: But what if `forceBackupOutputDir` is called when the dir doesn't exist? It returns nil (no-op). Then `ScaffoldOutputDir` creates it. Fine.

**Potential issue**: `forceBackupOutputDir` reads entries. If dir exists but is empty, returns nil. Then ScaffoldOutputDir proceeds normally. Fine.

**internal/reconcile/gate.go**:

```go
if _, statErr := os.Stat(filepath.Join(reconDir, FindingsJSON)); statErr == nil {
    if _, err := atomicfs.BackupToDotBak(reconDir); err != nil {
        return Result{}, fmt.Errorf("backing up prior reconciled output: %w", err)
    }
}
```

This backs up `reconciled/` to `reconciled.bak/` if `findings.json` exists. Then the reconcile proceeds to re-emit. 

**Issue**: `BackupToDotBak` does a COPY (not move), so `reconciled/` still exists with old content. Then the reconcile `Emit` overwrites the files in place. That's the design (copy semantics so live dir stays for reads). OK.

But wait - does the reconcile actually overwrite in place, or does it re-create? Let me think. The reconcile reads from sources and emits to reconciled/. If it overwrites in place, the backup has the old generation and live has new. Good.

**internal/verify/pipeline.go** and **emit_verification.go**:

`runVerify` calls `BackupExistingVerification(reviewDir)` before `writeGroupAtomic`. 

`WriteVerification` ALSO calls `BackupExistingVerification` before writing. So if `runVerify` is the path used, backup happens once in `runVerify`. If `WriteVerification` is called directly (e.g. from another path), it also backs up.

**Potential issue**: If `runVerify` calls `BackupExistingVerification` and then later `WriteVerification` is called within the same flow, it would back up AGAIN (but the file was already overwritten by writeGroupAtomic? No - writeGroupAtomic writes verification.json). Let me check the flow.

Actually, looking at `runVerify`: it builds `artifacts` list, calls `BackupExistingVerification`, then `writeGroupAtomic(artifacts)`. The `writeGroupAtomic` writes verification.json atomically. So `BackupExistingVerification` in `runVerify` backs up the OLD verification.json before the group write. Good.

But does `runVerify` also call `WriteVerification`? Let me check. The diff doesn't show the full `runVerify`. `WriteVerification` is a separate function. If `runVerify` uses `writeGroupAtomic` directly (not `WriteVerification`), then `WriteVerification`'s own backup is only triggered when called from elsewhere. 

**Issue**: `WriteVerification` calls `BackupExistingVerification` AND `runVerify` calls `BackupExistingVerification`. If both are called in sequence (runVerify → writeGroupAtomic writes verification.json, then somewhere WriteVerification is called), the second backup would back up the NEW file (just written) as `.bak`. That would be wrong - backing up the new generation as if it were the old. But this depends on call flow. Without seeing the full code, this is a potential double-backup issue. However, the test `TestWriteVerification_BacksUpPriorOnReWrite` tests `WriteVerification` directly, and `runVerify` is the pipeline. They might be alternative paths. Let me flag this as a potential issue.

Actually, looking more carefully: `runVerify` builds a staging list and writes atomically as a group. `WriteVerification` is a standalone writer. If `runVerify` is the only caller that writes verification.json (via writeGroupAtomic), and `WriteVerification` is a separate API, then both backing up independently is fine as long as they're not both called in the same flow.

But there's a real concern: **double backup**. If `runVerify` calls `BackupExistingVerification` then `writeGroupAtomic` (which writes verification.json), and then somewhere `WriteVerification` is ALSO called (maybe in a different code path or retry), the second call would back up the just-written file. But this seems unlikely in normal flow.

Let me look for a more concrete issue.

**Concrete issue in `runVerify`**: The backup is called BEFORE `writeGroupAtomic`. But `writeGroupAtomic` writes multiple files atomically. The backup only protects `verification.json`. If the group write includes other files (like findings.json, summary.json), those are NOT backed up here. But per the design, those are reconcile-owned and covered by `reconciled.bak/` (AC4). So this is intentional.

**Issue**: In `runVerify`, `BackupExistingVerification` is called, but if `writeGroupAtomic` fails, the backup was already created (the old file is now in `.bak` and the original is... still there? No - `BackupToDotBak` COPIES, doesn't move). So if writeGroupAtomic fails, the original verification.json is still intact, AND there's a .bak with the same content. That's harmless redundancy.

Wait, let me re-read `BackupToDotBak` - it copies, leaving src in place. So after backup, both `verification.json` (old) and `verification.json.bak` (old copy) exist. Then `writeGroupAtomic` overwrites `verification.json` with new content. If it fails, `verification.json` might be partially written... but `writeGroupAtomic` presumably uses atomic writes. So it either succeeds or leaves the old file. If it fails, we have old file + old backup. Fine.

**Now let me look for real bugs:**

1. **`copyFile` uses `os.WriteFile` which doesn't preserve permissions exactly** - `os.WriteFile` applies umask. So a file with 0o600 might become 0o644 depending on umask. The test `TestBackupToDotBak_Directory` checks nested file with 0o600 but doesn't verify perms. This could be a correctness issue for permission-sensitive artifacts. MEDIUM.

Actually, let me reconsider. `os.WriteFile(dst, data, perm)` - the perm is used but masked by umask. So if umask is 0o022, a 0o600 file stays 0o600 (since 0o600 & ~0o022 = 0o600). A 0o755 file becomes 0o755. Actually 0o600 & ~0o022 = 0o600 (no group/other bits to clear). So for most cases it's fine. But for 0o777, it'd become 0o755. Minor.

2. **`copyTree` doesn't preserve directory permissions for the root** - Actually it does via `os.MkdirAll(target, info.Mode().Perm())`. But `MkdirAll` also applies umask. Minor.

3. **`guardForeignBackup` reads the backup dir entries. If the backup is a regular file (not a dir), `os.ReadDir` returns an error, and the code returns a "refusing" error.** That's correct behavior.

4. **`looksLikeReviewTree` - only checks for subdirs, not files.** A user directory that happens to have those exact subdirs would be treated as an atcr backup and destroyed. Unlikely but possible. The design accepts this heuristic.

5. **In `forceBackupOutputDir`**: The guard checks `dir + ".bak"`. But `backupExisting` is then called which does `os.RemoveAll(backup)` + `os.Rename(path, backup)`. The guard verified the backup is safe to remove. But there's a TOCTOU: between guard and RemoveAll, the backup could change. Minor, not a real concern.

6. **`forceBackupReviewDir` does NOT guard foreign backups!** It calls `backupExisting` directly, which does `os.RemoveAll(backup)` unconditionally. For the managed reviews tree (`ReviewsRoot(root)/<id>`), the `.bak` sibling is atcr-owned, so this is fine per design. But wait - is it? The managed tree is `.atcr/reviews/<id>` and `.bak` would be `.atcr/reviews/<id>.bak`. That's atcr-owned. OK, consistent with design.

7. **Error message in `ScaffoldReviewDir`**: The new message is `review directory %s already exists; use --resume %s to continue it or --force to overwrite`. This names both paths. AC1 satisfied.

8. **Error message in `ScaffoldOutputDir`**: `output directory %q already exists and is not empty; use --force to overwrite (or point --output-dir at a new or empty path)`. This names --force but NOT --resume. Per AC1, the --output-dir path doesn't have resume (resume is for managed reviews). The sprint plan says AC1 applies to the collision error. Let me re-read AC1:

"AC1: `atcr review` with an existing review directory and neither `--resume` nor `--force` fails with an error that names BOTH non-destructive resume (`--resume <id>`, Epic 4.1.1) and destructive overwrite (`--force`)"

The clarification says: "--force on an existing explicit --id dir" and "Same --force semantics for the --output-dir non-empty path." The collision error message for --output-dir only names --force, not --resume. Is that correct? The clarification says the collision error names both branches: "review directory ... already exists; use --resume to continue it or --force to overwrite". But for --output-dir, resume doesn't apply (resume is for managed reviews with IDs). So the --output-dir error only naming --force seems intentional. The test `TestScaffoldOutputDir_CollisionMessageNamesForce` only checks for --force. So this is by design.

9. **`WriteJSON` uses `json.MarshalIndent` with two spaces.** The existing reconcile/verify writers - do they use the same indentation? If they use different indentation, the artifacts won't be consistent. But this is a new helper; if it's used for new outputs, it's fine. The doc says "matches the reconcile/verify writers." Let me not flag this unless there's evidence of mismatch.

10. **`BackupExistingVerification` in `emit_verification.go`**: It calls `atomicfs.BackupToDotBak(path)` where path is `verification.json`. `BackupToDotBak` for a file does `copyFile`. But `copyFile` uses `os.ReadFile` + `os.WriteFile`. This is NOT atomic. If the process is killed during the copy, the `.bak` could be partial. But this is a backup, not the live file, so partial backup is acceptable (the live file is untouched). Minor.

11. **Double backup concern**: `runVerify` in `pipeline.go` calls `BackupExistingVerification`, and `WriteVerification` in `emit_verification.go` also calls it. If `runVerify`'s `writeGroupAtomic` writes `verification.json`, and then `WriteVerification` is called later (e.g., in a retry or different stage), the second call would back up the NEW file (just written by the group) as `.bak`, overwriting the real backup. This would lose the original backup. 

Let me check if `runVerify` calls `WriteVerification`. Looking at the diff, `runVerify` builds artifacts and calls `writeGroupAtomic`. It doesn't seem to call `WriteVerification` directly. But `WriteVerification` is a public function. If both paths are used in sequence... Let me flag this as a potential issue.

Actually, looking at the test `TestWriteVerification_BacksUpPriorOnReWrite` - it calls `WriteVerification` directly and expects backup. And `runVerify` is the pipeline path. These seem like alternative entry points. The concern is if `runVerify` writes verification.json via `writeGroupAtomic` AND `WriteVerification` is also called. Without seeing the full `runVerify`, I can't be 100% sure. But the fact that `runVerify` explicitly calls `BackupExistingVerification` before `writeGroupAtomic` suggests it's handling the backup itself, and `WriteVerification` is a separate API that handles its own backup. If they're never called in sequence for the same file, it's fine.

But there IS a concrete issue: **`runVerify` calls `BackupExistingVerification` and then `writeGroupAtomic` which writes verification.json. But `WriteVerification` is also defined to back up before writing. If `runVerify` is the active path, the backup in `WriteVerification` is dead code for that path. If someone calls `WriteVerification` after `runVerify` has already written, it backs up the new file.** This is a design smell but might be intentional (two independent entry points). Let me flag as LOW maintainability.

Hmm, actually let me reconsider. Looking at the pipeline.go diff:

```go
if err := BackupExistingVerification(reviewDir); err != nil {
    return Result{}, err
}
if err := writeGroupAtomic(artifacts); err != nil {
    return Result{}, err
}
```

This is in `runVerify`. The `writeGroupAtomic` writes the group including verification.json. So `runVerify` handles backup + group write. `WriteVerification` is a standalone function for writing just verification.json. They're separate paths. The double-backup concern is only if both are called in sequence, which seems unlikely. I'll note it as LOW.

12. **Missing `fmt` import in `emit_verification.go`?** The diff adds `fmt.Errorf` in `BackupExistingVerification`. Let me check if `fmt` is already imported. The diff shows the function added at the end of the file. The existing file likely imports `fmt` already (since it uses `filepath.Join` etc.). Actually, looking at the diff, the existing code already uses `atomicfs.WriteFileAtomic` and `filepath.Join`, so `fmt` might or might not be imported. Let me check - the diff doesn't show import changes in `emit_verification.go`. If `fmt` wasn't already imported, this would be a compile error. But since the tests pass (presumably), `fmt` must already be imported. Let me not flag this.

Wait, actually, let me look more carefully. The diff for `emit_verification.go`:
```go
+	if err := BackupExistingVerification(reviewDir); err != nil {
+		return err
+	}
 	return atomicfs.WriteFileAtomic(path, data)
 }
+
+func BackupExistingVerification(reviewDir string) error {
+	path := filepath.Join(reviewDir, reconciledSubdir, "verification.json")
+	if _, err := atomicfs.BackupToDotBak(path); err != nil {
+		return fmt.Errorf("backing up prior verification.json: %w", err)
+	}
+	return nil
+}
```

This uses `fmt.Errorf`. If `fmt` isn't imported, it won't compile. But the diff doesn't show an import addition. Let me check the existing file - `emit_verification.go` already uses `atomicfs` and `filepath`. Does it use `fmt`? Looking at the existing code in the diff context: `return atomicfs.WriteFileAtomic(path, data)` - no fmt visible. But the file could already import fmt for other uses. Since this is presumably tested and compiles, I'll assume fmt is already imported. Actually, let me flag this as a potential issue - if fmt is NOT already imported, it's a compile error. But typically CI would catch this. Let me check more carefully...

The file is `internal/verify/emit_verification.go`. The diff shows the existing function uses `json.Marshal` (implied by `data, err :=` before the shown context) and `atomicfs.WriteFileAtomic` and `filepath.Join`. The `fmt` package might not be imported. But without seeing the full file, I can't be sure. Since the diff is from a merge commit that presumably passed CI, I'll skip this.

13. **`copyTree` error handling for `filepath.Rel`**: If `filepath.Rel(src, path)` fails, it returns an error. This shouldn't happen since `path` is always under `src` during WalkDir. OK.

14. **`copyTree` - `d.Info()` can fail for some entries.** The code handles it with `if err != nil { return err }`. OK.

15. **Performance**: `copyFile` reads entire file into memory. For large artifacts (unlikely in review context), this could be an issue. LOW.

16. **`BackupToDotBak` for directory**: `os.RemoveAll(bak)` then `copyTree(src, bak)`. If `copyTree` fails partway, the backup is incomplete and the old backup is gone. The live dir is still intact (copy semantics). But the backup is lost. This is a minor issue - a failed backup leaves no backup. Could use a temp dir + rename for atomicity. MEDIUM.

Actually, this is a real concern. `BackupToDotBak` for a directory:
1. `os.RemoveAll(bak)` - removes old backup
2. `copyTree(src, bak)` - copies; if this fails partway, bak is partial and old backup is gone

If the copy fails, you've lost the previous backup AND have a partial one. The live data is still there (copy, not move), so the user can retry. But the previous backup generation is lost. For the reconcile use case, this means if backup fails, `RunReconcile` returns an error before overwriting, so the live `reconciled/` is still intact. The user can retry. So the impact is: lost previous backup, but live data is safe. Acceptable but could be better (copy to temp, rename). LOW-MEDIUM.

17. **`forceBackupOutputDir` - `guardForeignBackup` is only called for the output-dir path, not for the review-dir path.** This is by design (managed tree is atcr-owned). OK.

18. **In `review.go`**: The `--force` flag is passed to `ReviewRequest.Force`. But the mutual exclusion check is only in the `--resume` branch. What if someone passes `--force` without `--resume` and without `--id` or `--output-dir`? Then `Force` is true but there's nothing to overwrite (derived id auto-suffixes). The code says Force is a no-op for derived ids. That's fine but could be confusing to the user. LOW maintainability - maybe warn the user.

19. **Test coverage**: The tests look comprehensive. Good.

20. **`looksLikeReviewTree`** checks `reviewSubdirs`. What is `reviewSubdirs`? It's referenced but not shown in the diff. Presumably it's the list of subdirs created by `ScaffoldOutputDir`/`ScaffoldReviewDir`. If `reviewSubdirs` is empty or incomplete, the heuristic breaks. But it's existing code, not changed in this diff.

Let me now focus on the most impactful findings:

**Finding 1**: `copyFile` in `atomicfs/atomic.go` uses `os.ReadFile` + `os.WriteFile` which buffers entire file in memory. For large files this is inefficient, and `os.WriteFile` applies umask so permissions may not be exactly preserved. Could use `io.Copy` with opened file handles. But review artifacts are small. LOW.

**Finding 2**: `BackupToDotBak` for directories removes the old `.bak` BEFORE copying. If the copy fails, the previous backup is lost. Should copy to a temp directory then rename. MEDIUM.

**Finding 3**: Double backup in verify - `runVerify` calls `BackupExistingVerification` and `WriteVerification` also calls it. If `WriteVerification` is called after `runVerify` writes verification.json, it backs up the new generation as the old. LOW (depends on call flow).

**Finding 4**: `copyFile` via `os.WriteFile` doesn't faithfully preserve permissions (umask applied). For a backup that should be byte-identical, permissions could differ. LOW.

**Finding 5**: In `forceBackupOutputDir`, the `guardForeignBackup` check and `backupExisting` are not atomic (TOCTOU). Minor. LOW.

**Finding 6**: `WriteJSON` appends `'\n'` to marshaled data. If the JSON marshaling succeeds but `WriteFileAtomic` fails, no harm (atomic). OK.

Let me reconsider which are real, actionable issues within scope.

Actually, let me reconsider Finding 2 more carefully. `BackupToDotBak` is used in:
- `gate.go` RunReconcile: backs up `reconciled/` dir
- `emit_verification.go`: backs up `verification.json` file

For the directory case (reconcile), if `copyTree` fails partway:
- Old `reconciled.bak/` is gone (RemoveAll'd)
- New `reconciled.bak/` is partial
- Live `reconciled/` is intact (copy, not move)
- `RunReconcile` returns error, doesn't overwrite live dir
- User retries: `BackupToDotBak` removes partial `.bak`, tries again

So the impact is: temporary loss of backup, but live data is safe and retryable. The previous backup generation is lost though. MEDIUM seems right - it's a real data loss scenario for the backup, but not for the live data.

Let me also check: does `RunReconcile` check for `findings.json` existence before backing up? Yes:
```go
if _, statErr := os.Stat(filepath.Join(reconDir, FindingsJSON)); statErr == nil {
    if _, err := atomicfs.BackupToDotBak(reconDir); err != nil {
        return Result{}, fmt.Errorf("backing up prior reconciled output: %w", err)
    }
}
```

So if backup fails, reconcile aborts. Good - the live data is never overwritten if backup fails. The only loss is the previous `.bak` generation. MEDIUM.

Let me also look at the `--force` flow more carefully for a potential issue:

In `PrepareReview`:
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

If `forceBackupOutputDir` succeeds (moves dir to .bak) but `ScaffoldOutputDir` fails, the original dir is in .bak and the target is gone. The user's data is preserved in .bak but the review didn't start. This is acceptable - the user can rename .bak back. But there's no automatic rollback. MEDIUM? Actually, this is the expected behavior for --force - it backs up then scaffolds. If scaffold fails, the backup is there. The user can recover manually. LOW.

Actually wait - `ScaffoldOutputDir` creates the dir and subdirs. If it fails, the dir might be partially created. But the backup is safe in .bak. Acceptable.

Let me also check: in the IDOverride path:
```go
case req.IDOverride != "":
    if req.Force {
        if err = forceBackupReviewDir(req.Root, id); err != nil {
            return nil, err
        }
    }
    dir, err = ScaffoldReviewDir(req.Root, id)
```

`forceBackupReviewDir` renames `.atcr/reviews/<id>` to `.atcr/reviews/<id>.bak`. Then `ScaffoldReviewDir` creates `.atcr/reviews/<id>` fresh. If scaffold fails, the original is in .bak. Acceptable.

Now, one more issue: **`forceBackupReviewDir` does NOT call `guardForeignBackup`**. So if there's a pre-existing `<id>.bak` in the managed tree that's foreign (not atcr-created), it gets `RemoveAll`'d. But the managed tree is atcr-owned, so this is by design. However, what if a user manually placed important data in `<id>.bak`? The design says "managed tree is atcr-owned." OK, not flagging.

Let me also look at whether `--force` without `--id` or `--output-dir` (i.e., derived id) is handled. The code says Force is a no-op for derived ids. But the user passed `--force` expecting something. No warning. This could be confusing. LOW maintainability.

Let me finalize my findings:

1. **MEDIUM** - `internal/atomicfs/atomic.go:BackupToDotBak` - Directory backup removes old `.bak` before copying; if copy fails, previous backup generation is lost. Fix: copy to temp dir, then rename.

2. **LOW** - `internal/atomicfs/atomic.go:copyFile` - Uses `os.WriteFile` which applies umask; permissions may not be faithfully preserved. Fix: use `os.OpenFile` with exact perm, or `io.Copy`.

3. **LOW** - `internal/verify/pipeline.go:runVerify` + `emit_verification.go:WriteVerification` - Both call `BackupExistingVerification`; if `WriteVerification` is called after `runVerify` writes verification.json, it backs up the new generation as old. Fix: document that they're alternative entry points, or have `WriteVerification` skip backup if called from pipeline.

4. **LOW** - `cmd/atcr/review.go` - `--force` with derived id (no `--id`/`--output-dir`) is silently ignored. Fix: warn user that --force has no effect without --id or --output-dir.

Hmm, let me reconsider #3. Looking at the code flow:
- `runVerify` calls `BackupExistingVerification` then `writeGroupAtomic(artifacts)` which writes verification.json (among others).
- `WriteVerification` calls `BackupExistingVerification` then `atomicfs.WriteFileAtomic(path, data)`.

These are two separate write paths. If `runVerify` is used, it handles backup + group write. If `WriteVerification` is used (standalone), it handles backup + single write. They're not called in sequence for the same file. So the double-backup concern is not real unless there's a code path that calls both. Without evidence of such a path, I should not flag this as a definite issue. Let me drop it or make it very LOW.

Actually, wait. Let me re-read the pipeline.go diff more carefully:

```go
+	if err := BackupExistingVerification(reviewDir); err != nil {
+		return Result{}, err
+	}
 	if err := writeGroupAtomic(artifacts); err != nil {
 		return Result{}, err
 	}
```

This is in `runVerify`. So `runVerify` backs up, then writes the group (which includes verification.json). `WriteVerification` is a separate public function. The question is: does `runVerify` use `WriteVerification` internally, or does it build its own artifact list? From the diff context, `runVerify` builds `artifacts` (a `[]stagingEntry`) and calls `writeGroupAtomic`. It does NOT call `WriteVerification`. So they're independent paths. The backup in `WriteVerification` is for when it's called directly (e.g., from tests or other code). No double-backup issue. Dropping this finding.

Let me reconsider what to actually flag. Let me be strict about real issues:

1. **BackupToDotBak non-atomic directory backup** - This is a real issue. The old `.bak` is removed before the new copy completes. If the copy fails, the previous backup is lost. MEDIUM.

2. **copyFile umask** - Real but minor. Permissions might differ. LOW.

3. **--force silently ignored for derived ids** - Real UX issue. LOW.

Actually, let me reconsider #1. Is this really within scope? The sprint plan says "Backup before overwrite (`*.bak` files)." The `BackupToDotBak` function is the implementation. The non-atomicity of the backup is a correctness issue in the new code. Yes, in scope.

Let me also check: is there a test for the backup-failure case? No. The tests only test successful backups. The failure case (copy fails partway) is untested. But that's a test gap, not a code bug per se.

Let me also look for any issue in the error messages:

`ScaffoldReviewDir` error: `"review directory %s already exists; use --resume %s to continue it or --force to overwrite"` - uses `dir` (full path) and `id`. AC1 says "names BOTH non-destructive resume and destructive overwrite." ✓

`ScaffoldOutputDir` error: `"output directory %q already exists and is not empty; use --force to overwrite (or point --output-dir at a new or empty path)"` - names --force but not --resume. For --output-dir, resume doesn't apply (it's unmanaged). The sprint plan clarification says the collision error names both branches, but for --output-dir specifically, resume isn't applicable. The test only checks for --force. So this is by design. Not flagging.

Wait, actually AC1 says: "fails with an error that names BOTH non-destructive resume (`--resume <id>`, Epic 4.1.1) and destructive overwrite (`--force`)." Does this apply to the --output-dir path too? The clarification says: "Collision error message names both branches: 'review directory ... already exists; use --resume to continue it or --force to overwrite'." But for --output-dir, resume doesn't make sense (no ID to resume). The test `TestScaffoldOutputDir_CollisionMessageNamesForce` only checks for --force. So the team decided --output-dir only names --force. This seems like a reasonable design decision. Not flagging.

Let me finalize. I think the main real issues are:

1. **MEDIUM** - `BackupToDotBak` directory copy is non-atomic: old `.bak` removed before new copy completes, losing previous backup on copy failure.

2. **LOW** - `copyFile` uses `os.WriteFile` which applies umask to permissions.

3. **LOW** - `--force` silently ignored when used with derived IDs (no `--id`/`--output-dir`).

Actually, for #3, let me check the code flow. In `runReview`:
```go
req := ReviewRequest{
    ...
    Force: boolFlag(cmd, "force"),
}
```
Then `PrepareReview` is called. In `PrepareReview`, the switch:
- `case req.OutputDir != "":` - Force applies
- `case req.IDOverride != "":` - Force applies
- `default:` - derived id, Force is not checked

So `--force` with a derived id is silently ignored. The user might expect it to do something. This is a UX issue. But the sprint plan says "Derived-id auto-suffix (`claimReviewDir`) is a shipped, tested contract that never collides — it is left entirely unchanged." So ignoring --force for derived ids is by design. Not flagging.

OK, let me also look for any issue with the `--resume` + `--force` mutual exclusion. The check is:
```go
if cmd.Flags().Changed("resume") {
    if cmd.Flags().Changed("force") {
        return usageError(errors.New("--resume and --force are mutually exclusive"))
    }
    ...
}
```
This only checks when `--resume` is changed. If `--force` is changed but `--resume` is not, the check doesn't fire (which is correct - --force without --resume is fine). If both are changed, the check fires. ✓ AC1b.

But wait - what if `--force` is changed and `--resume` is NOT changed? Then we proceed to the normal review flow with `Force=true`. That's correct.

What about `--resume` changed and `--force` changed? The check fires. ✓.

OK, the mutual exclusion is correct.

Let me look at one more thing: the `forceBackupOutputDir` function. It reads entries, and if non-empty, guards the backup and calls `backupExisting`. But `backupExisting` renames `dir` to `dir.bak`. After this, `ScaffoldOutputDir` is called, which does `os.MkdirAll(dir, 0o755)`. Since `dir` was renamed away, `MkdirAll` creates it fresh. Then it checks