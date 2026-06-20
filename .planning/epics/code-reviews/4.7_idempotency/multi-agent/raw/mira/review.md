<think>
Let me carefully analyze this diff for issues within the scope of the sprint plan (Epic 4.7: Idempotency and Safe Retry).

The sprint plan covers:
- Atomic file writes (`internal/atomicfs` package)
- Review directory collision detection in `cmd/atcr/review.go`
- `--force` flag to overwrite existing review directory
- Backup before overwrite (`*.bak` files)
- Error message when output directory already exists
- Unit tests for atomic writes
- Integration test: run review twice, verify backup created

Let me look at each file in the diff:

1. **README.md** - Documentation update, adding --force flag description. Looks fine.

2. **cmd/atcr/review.go** - Adding --force flag, mutual exclusion check with --resume. Let me look more carefully...
   - Line 87-95: The mutual exclusion check fires before the resume short-circuit. This is correct per AC1b.
   - Line 176: Force flag is passed to the ReviewRequest. Good.

3. **cmd/atcr/review_test.go** - Tests for the new flag. Looks good.

4. **internal/atomicfs/atomic.go** - New functions WriteJSON, BackupToDotBak, copyFile, copyTree.
   - **Issue I see**: In `BackupToDotBak`, when the source is a file, `copyFile` reads the entire file into memory. For large files (e.g., large reconciled outputs), this could be a performance concern. But for typical review artifacts, this should be fine.
   - **Another issue**: In `copyTree`, the directory copy uses `os.MkdirAll` but then uses `copyFile` to write files. The `copyFile` uses `os.ReadFile` (reads entire file) and `os.WriteFile`. This means large files are read entirely into memory. This is a performance/memory concern.
   - **Permission issue**: `copyFile` uses `info.Mode().Perm()` for the destination, but when copying directories, the original directory might have different permissions than 0o755. This is preserved.
   - **Symlink handling**: The comment says symlinks are skipped. Let me check the code... yes, `if !info.Mode().IsRegular() { return nil }` skips them. Good.
   - **TOCTOU race**: `BackupToDotBak` checks if src exists with `os.Stat`, then `os.RemoveAll(bak)`, then does the copy. Between the stat and copy, the source could disappear. This is a minor concern.

5. **internal/atomicfs/atomic_test.go** - New tests. Good coverage.

6. **internal/fanout/review.go** - Adding Force field and backup logic. The Force field is properly handled.

7. **internal/fanout/review_test.go** - New test TestRunReview_ForceBacksUpExistingDir. Good.

8. **internal/fanout/reviewdir.go** - 
   - **Issue**: In `ScaffoldReviewDir`, the new error message uses `id` instead of `dir` in the resume path: `use --resume %s to continue it`. This should probably show the id since the user would pass the id to --resume. This looks correct.
   - **Issue**: In `ScaffoldOutputDir`, error message is updated. The new message references --force but the message says "use --force to overwrite" without mentioning --resume. AC1 states the error should name BOTH --resume and --force. Looking at the test `TestScaffoldOutputDir_CollisionMessageNamesForce`, it only checks for --force. The new error message for output-dir doesn't mention --resume. Is --resume applicable to --output-dir? Looking at the plan and code, --resume applies to a review by id or path, and --output-dir is also a path... Let me re-read the AC1.
   
   AC1 says: "fails with an error that names BOTH non-destructive resume (`--resume <id>`, Epic 4.1.1) and destructive overwrite (`--force`)"
   
   The error for --id path now mentions both. But for --output-dir, the new error only mentions --force. This might be an inconsistency. However, --output-dir is more of an external orchestrator path, and --resume applies to id-based reviews. So this could be intentional. But it's worth flagging.
   
   - **Issue with `guardForeignBackup`**: The function `looksLikeReviewTree` only checks if the directory contains the review subdirs. But these subdirs might exist in a user's directory by coincidence. The check is somewhat weak.
   
   - **Issue with backupExisting**: It uses `os.Rename` which is atomic on the same filesystem. The plan said the managed reviews tree uses `os.Rename` for backup (per the clarifications), and BackupToDotBak uses copy. There's inconsistency in approach: `backupExisting` uses rename, but `BackupToDotBak` uses copy. This is by design per the plan: managed reviews tree uses rename to keep the path vacant for fresh scaffold; reconcile/verify outputs use copy because the live dir is read during the operation. But this is a minor inconsistency.

   - **Issue with `guardForeignBackup`**: When `os.ReadDir(backup)` returns an error other than ErrNotExist, it returns an error saying the file exists and was not created by atcr. But if the entry is a regular file (not a directory), the original `os.ReadDir` would error out. Actually, in the function, if backup is a regular file, the error is returned with a message that may be confusing. This is a minor issue.

9. **internal/fanout/reviewdir_test.go** - New tests for the changes.

10. **internal/reconcile/gate.go** - 
    - **Issue**: The backup happens before any reconciliation work. If reconciliation fails after backup, the original is still safe in `.bak` but the live dir might be in a partial state. Actually, the backup is COPY semantics (per `BackupToDotBak`), so the original is still there. Good.
    - **Issue**: The check `if _, statErr := os.Stat(filepath.Join(reconDir, FindingsJSON)); statErr == nil` - this only checks for findings.json. What if the prior dir has adjudication.json but no findings.json (e.g., a partial prior run)? Then no backup is created. The comment says "Only a directory that already holds reconcile output (findings.json) is backed up — the empty scaffolded dir on a first run is skipped." This is intentional, but might miss edge cases.

11. **internal/reconcile/gate_test.go** - New test for backup behavior.

12. **internal/verify/emit_test.go** - New test for verification backup.

13. **internal/verify/emit_verification.go** - 
    - **Issue**: `BackupExistingVerification` is called from BOTH `WriteVerification` and `runVerify`. Looking at `runVerify`, it does an atomic group write via `writeGroupAtomic(artifacts)`. And then `WriteVerification` would also be called somewhere else that triggers `BackupExistingVerification`. This would mean the backup is created twice, or worse, the second call might back up the just-written file. Let me check... Actually, `runVerify` doesn't seem to call `WriteVerification` based on this diff. So this is fine. But wait, I need to look at the broader code to see if `WriteVerification` is called from `runVerify` or vice versa.

   Looking at the diff: `runVerify` calls `BackupExistingVerification` directly, then `writeGroupAtomic(artifacts)`. So if `WriteVerification` is also called (perhaps by something else), it would also call `BackupExistingVerification`. This could double-back-up. This is a potential issue.

   Actually, looking more carefully, the comment in `BackupExistingVerification` says "before a re-verify (e.g. --fresh) overwrites it". And `runVerify` is the verify pipeline. The `writeGroupAtomic(artifacts)` in `runVerify` would write verification.json as part of the atomic group. Then `WriteVerification` might also be called. If `WriteVerification` is called, it would also back up, but the just-written file would be backed up (not the prior one). This is a logic issue.

   Let me check by looking at how this is used. Without seeing the full code, I can only see from the diff that `WriteVerification` is exported and presumably used externally, and `runVerify` is internal. The diff shows both call `BackupExistingVerification`. If `runVerify` calls `WriteVerification` (which we don't know from the diff), there would be a double backup issue.

14. **internal/verify/pipeline.go** - runVerify calls BackupExistingVerification.

Let me focus on what's clearly a bug or issue in the diff:

**Issue 1: Double backup risk in verify**
In `internal/verify/emit_verification.go`, `WriteVerification` calls `BackupExistingVerification`. And in `internal/verify/pipeline.go`, `runVerify` also calls `BackupExistingVerification` before its own atomic write. If `runVerify` internally calls `WriteVerification`, the backup would happen twice. If it doesn't, then it's fine. But the function `WriteVerification` is exported and might be called externally. This is at minimum an architectural concern.

**Issue 2: TOCTOU in BackupToDotBak**
The stat-then-act pattern in `BackupToDotBak` could race with other processes. For backup purposes, this might be acceptable, but it's a minor concern.

**Issue 3: Inconsistency in output-dir error message**
The new error message for `ScaffoldOutputDir` only mentions --force, not --resume. AC1 says the error should name both. This might be intentional if --output-dir reviews can't be resumed, but it's worth flagging.

**Issue 4: Permission preservation in copyFile/copyTree**
When copying files, `copyFile` uses `info.Mode().Perm()` to set permissions. But the `os.WriteFile` is called with this perm. The destination's umask might affect this, but in Go's `os.WriteFile`, the perm is the file mode, not affected by umask. This is fine.

Wait, but `os.MkdirAll(target, info.Mode().Perm())` is called for directories. This is correct.

**Issue 5: Race condition between backup and overwrite in WriteVerification**
If `WriteVerification` is called and `runVerify` is also called, the backup might happen between the atomic write completing and the backup being made for the next write. But this is a minor concern.

**Issue 6: `BackupToDotBak` for a directory does a full copy into memory**
For large directory trees, this loads everything into memory. The `copyFile` function reads the entire file into memory. For a large reconciled dir, this could be a memory issue. This is a performance concern, not a correctness issue.

**Issue 7: Missing error wrapping context in guardForeignBackup**
When `os.ReadDir` returns a non-NotExist error (e.g., permission denied), the function returns a generic error message that may not reflect the actual cause. Minor.

**Issue 8: In `WriteVerification`, the backup happens AFTER the marshal but BEFORE the atomic write**
Looking at the diff for `WriteVerification`:
```go
data, err := json.MarshalIndent(...)
if err != nil {
    return err
}
if err := BackupExistingVerification(reviewDir); err != nil {
    return err
}
return atomicfs.WriteFileAtomic(path, data)
```

The backup happens after the marshal. If the marshal is slow, the window between backup and atomic write exists. If a process dies in this window, the original is in .bak and the live file is unchanged. This is actually a feature (the original is preserved). So this is fine.

**Issue 9: The backup location for verify is wrong**
The backup is to `verification.json.bak` in the `reconciled/` directory. But the path is `filepath.Join(reviewDir, reconciledSubdir, "verification.json")`, and `BackupToDotBak` adds `.bak` to that. So the backup is at `reconciled/verification.json.bak`. This matches the test. Good.

**Issue 10: `WriteVerification` backs up AFTER the marshal, but `runVerify` backs up BEFORE the atomic write of multiple files**
In `runVerify`, the backup is called once, and then `writeGroupAtomic(artifacts)` writes multiple files atomically. This is correct.

**Issue 11: The README change has "backing the prior tree up to <dir>.bak first"**
This is consistent with the implementation. Good.

**Issue 12: The `looksLikeReviewTree` check in `guardForeignBackup`**
The check requires all `reviewSubdirs` to exist. But what if a user's directory has these subdirs by coincidence? The check is weak. This is a minor security/correctness concern.

**Issue 13: Symlinks in copyTree**
The code skips symlinks. But on Windows, symlinks might be different. The test `TestBackupToDotBak_DirectorySkipsNonRegularFiles` skips on symlink failure. Good.

**Issue 14: Error message inconsistency in `ScaffoldReviewDir`**
The new error message says `use --resume %s to continue it or --force to overwrite`. The format uses the id in the resume path, but the actual path is the full directory. This could be confusing if the user thinks the id is the directory. Actually, the id IS what the user would pass to --resume. So this is correct.

**Issue 15: Missing check in `runReview`**
The diff adds the --force/--resume mutual exclusion check. But what about other potentially conflicting flags? The plan doesn't mention other conflicts, so this is fine.

**Issue 16: The new `Force` field in ReviewRequest is documented as no-op for derived ids**
The implementation in `internal/fanout/review.go` only handles Force for the IDOverride and OutputDir cases. The derived id case (default) doesn't check Force. The comment says "derived ids never collide (claimReviewDir auto-suffixes) so Force is a no-op there." This is correct per the plan.

**Issue 17: The `runVerify` calls `BackupExistingVerification` but the `runVerify` is internal**
Looking at the diff, `runVerify` is a method on the pipeline. It's called internally. The `WriteVerification` is exported. If `runVerify` is the only caller of the atomic group write that includes verification.json, then there's no double-backup. But if `WriteVerification` is also called by `runVerify` or vice versa, there could be an issue.

Looking at the code flow more carefully:
- `runVerify` does its work, builds artifacts (including verification.json data), backs up, then writes atomically.
- `WriteVerification` is a separate exported function that marshals and writes verification.json (with backup).

If `runVerify` builds the verification.json data and writes it as part of the group, then `WriteVerification` is NOT called from `runVerify`. So the backup in `WriteVerification` would only be called externally. This is fine.

But wait, the test `TestWriteVerification_BacksUpPriorOnReWrite` calls `WriteVerification` directly. This tests the `WriteVerification` path. And `runVerify` is tested elsewhere. So there's no double backup.

But this is a maintenance concern: if someone later calls `WriteVerification` from within `runVerify`, there would be a double backup. This is a design issue, not a current bug.

**Issue 18: Missing context for backup failure in `runVerify`**
The error from `BackupExistingVerification` is returned as-is. It's not wrapped with context about which review dir this is. Minor.

**Issue 19: The `BackupExistingVerification` function is exported but used internally**
It's exported (capital B), but only used internally in the verify package. This is fine for testability.

**Issue 20: Race in `BackupToDotBak` for directories**
The function copies the directory tree. If files are being written to the source during the copy, the backup might be inconsistent. This is a known limitation of file-based backups. Not a bug per se.

**Issue 21: The `runReview` test for Force is at the cmd level, but the actual Force logic is in the fanout package**
The test `TestReviewCmd_ResumeAndForceMutuallyExclusive` tests the cmd-level mutual exclusion. The actual backup logic is tested in the fanout package. Good separation.

**Issue 22: Missing test for the --force on --output-dir case**
The integration test `TestRunReview_ForceBacksUpExistingDir` only tests the --id case. There's no integration test for the --output-dir case with --force. This is a test coverage gap.

**Issue 23: The new error in `ScaffoldReviewDir` is not tested for the --output-dir case with --resume**
The plan says --resume is not combinable with --output-dir. So this is correct.

**Issue 24: Potential issue with `BackupToDotBak` when source is a symlink**
If the source is a symlink, `os.Stat` follows the link. So the function would follow the symlink and copy the target. This might not be desired behavior. Minor.

**Issue 25: The `copyTree` function doesn't preserve special files like named pipes**
The comment says "Non-regular entries (symlinks, devices) are skipped". This is intentional. But named pipes (FIFOs) would also be skipped. This is fine for review artifacts.

**Issue 26: In `runVerify`, the backup is called even when there's no prior verification.json**
`BackupExistingVerification` -> `BackupToDotBak` -> checks if source exists, returns nil if not. So this is a no-op for first runs. Good.

**Issue 27: The `--force` flag is added to the review command, but what about the `atcr reconcile` and `atcr verify` commands?**
The plan mentions that reconcile and verify have their own backup logic (not --force). The --force flag is only for the review command. This is per the plan.

**Issue 28: Missing validation in `runReview` for --force without --id or --output-dir**
If the user passes --force without --id or --output-dir, the Force field is set but never used (because the derived id case doesn't check Force). The user might be confused why --force didn't do anything. This is a UX issue, not a bug. The plan says derived ids never collide, so --force is a no-op there. But the user might not know that.

**Issue 29: The `guardForeignBackup` check for `looksLikeReviewTree` is weak**
If a user's directory happens to have subdirectories named exactly like the review subdirs, the check would pass and the backup would be replaced. This is a false positive risk. But the likelihood is very low.

**Issue 30: The `BackupToDotBak` for a file doesn't preserve xattrs or other metadata**
Only the file content and permissions are preserved. This is a known limitation.

**Issue 31: The `WriteFileAtomic` function (existing) is not modified**
The diff only adds new functions. The existing function is unchanged. Good.

**Issue 32: The new `WriteJSON` function doesn't handle nil**
`json.MarshalIndent(nil, "", "  ")` returns `null\n`. This is valid JSON. So this is fine.

**Issue 33: The `BackupToDotBak` function's error from `os.RemoveAll` is not checked for the case where the backup is a file**
If the backup is a regular file, `os.RemoveAll` would remove it. This is the desired behavior. Good.

**Issue 34: Potential issue with the `--force` flag in `runReview` when `--resume` is not changed**
The check is `if cmd.Flags().Changed("resume")`. If --resume is not changed (default), the check is skipped. Then the --force flag is passed to the ReviewRequest. The fanout package checks Force for the IDOverride and OutputDir cases. If neither is set, Force is a no-op. This is correct.

**Issue 35: The `runReview` test `TestReviewCmd_ResumeAndForceMutuallyExclusive` uses `execCmdCapture` which captures output**
The test checks for the specific error message. Good.

**Issue 36: The `internal/fanout/review_test.go` test `TestRunReview_ForceBacksUpExistingDir` uses `t.Setenv`**
This is the correct way to set environment variables in tests. Good.

**Issue 37: The `internal/atomicfs/atomic_test.go` test `TestWriteJSON_OverwritesAtomicallyNoTempLeftover` checks for temp file leaks**
The temp file prefix is `.<base>.tmp-`. The test checks for this prefix. Good.

**Issue 38: The `internal/atomicfs/atomic_test.go` test `TestWriteFileAtomic_OrphanedTempDoesNotCorruptTarget` simulates a process kill**
This is a good test for the AC6 requirement. Good.

**Issue 39: The `internal/reconcile/gate_test.go` test `TestRunReconcile_BacksUpPriorReconciledOnReRun` checks the backup**
Good test coverage for AC4.

**Issue 40: The `internal/verify/emit_test.go` test `TestWriteVerification_BacksUpPriorOnReWrite` checks the backup**
Good test coverage for AC5.

Now let me focus on the most important issues:

1. **Double backup risk in verify**: `WriteVerification` backs up, and `runVerify` also backs up. If `WriteVerification` is called from within `runVerify` or vice versa, there could be a double backup. This is a design concern.

2. **Output-dir error message doesn't mention --resume**: AC1 says the error should name both --resume and --force. The new error for --output-dir only mentions --force. This might be intentional if --output-dir reviews can't be resumed, but it's worth flagging.

3. **Weak `looksLikeReviewTree` check**: The check is based on directory names, which could collide with user data. Minor security concern.

4. **Memory usage in `copyFile`**: Large files are read entirely into memory. Performance concern.

5. **Missing integration test for --force on --output-dir**: Test coverage gap.

6. **UX issue with --force on derived ids**: --force is silently ignored when neither --id nor --output-dir is set. User might be confused.

Let me also check the code for actual bugs:

In `internal/fanout/reviewdir.go`, the `guardForeignBackup` function:
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

If `backup` is a regular file (not a directory), `os.ReadDir` would return an error. The function returns a message saying "exists and was not created by atcr". But the user might have a regular file named `<dir>.bak` that they want to keep. The error message is appropriate. Good.

In `internal/atomicfs/atomic.go`, the `BackupToDotBak` function:
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

This unconditionally removes the existing backup. For the `WriteVerification` case, this is called for `verification.json` (a file), and the backup is `verification.json.bak`. The function removes the existing `.bak` file and copies the current file to `.bak`. This is correct.

But wait, in `runVerify`, `BackupExistingVerification` is called before the atomic write. Then `writeGroupAtomic(artifacts)` writes multiple files including `verification.json`. The backup is created, then the live file is written. This is correct.

But if `WriteVerification` is also called (externally), it would also call `BackupExistingVerification`, which would back up the just-written file. This is a double backup.

Looking at the test `TestWriteVerification_BacksUpPriorOnReWrite`, it calls `WriteVerification` twice. The first call writes `verification.json` with no prior file, so no backup. The second call backs up the first file (now at `verification.json`) to `verification.json.bak`, then writes the new file. This is correct.

But the concern is: if `runVerify` is called and it writes `verification.json` as part of the atomic group, and then `WriteVerification` is called externally with the same data, the second call would back up the just-written file. This is a design issue.

Actually, looking more carefully, `runVerify` is the internal pipeline. `WriteVerification` is exported. They are separate code paths. The double backup would only happen if someone calls `WriteVerification` after `runVerify` has already written. This is unlikely in normal flow.

Let me also check: in `runVerify`, the backup is called before `writeGroupAtomic`. The `writeGroupAtomic` writes multiple files atomically. If one of those files is `verification.json`, then the backup is for the prior `verification.json`, and the atomic write replaces it. This is correct.

In `WriteVerification`, the backup is called before `WriteFileAtomic`. This is also correct.

The potential issue is if `runVerify` internally calls `WriteVerification`. From the diff, I don't see that. So this is not a current bug.

Now, let me think about what's most important to flag:

1. **HIGH: Double backup risk in verify** - If `WriteVerification` is called from within `runVerify` or if both are called in sequence, the backup might be for the wrong generation.

2. **MEDIUM: Output-dir error message doesn't mention --resume** - AC1 compliance issue.

3. **MEDIUM: Memory usage in copyFile for large files** - Performance concern.

4. **LOW: Weak looksLikeReviewTree check** - Minor security concern.

5. **LOW: Missing integration test for --force on --output-dir** - Test coverage gap.

6. **LOW: UX issue with --force on derived ids** - User confusion.

Let me also look for actual bugs:

In `internal/fanout/reviewdir.go`, the `forceBackupOutputDir` function:
```go
func forceBackupOutputDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking output directory before --force backup: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}
	if err := guardForeignBackup(dir + ".bak"); err != nil {
		return err
	}
	_, err = backupExisting(dir)
	return err
}
```

This calls `backupExisting` which does `os.RemoveAll(bak)` and then `os.Rename(dir, bak)`. The `guardForeignBackup` is called before `backupExisting`. But `backupExisting` also calls `os.RemoveAll(bak)`. So the guard is redundant in a way, but it provides a better error message for the foreign case. This is fine.

Wait, but `guardForeignBackup` is called before `backupExisting`. If the guard passes (backup is either non-existent, empty, or looks like a review tree), then `backupExisting` removes the backup and renames the dir. If the guard fails (backup is foreign), `backupExisting` is not called, so the foreign backup is preserved. This is correct.

But there's a subtle issue: `guardForeignBackup` only checks the backup directory. It doesn't check if the backup is a regular file. If `dir + ".bak"` is a regular file, `os.ReadDir` would return an error, and `guardForeignBackup` would return an error saying "exists and was not created by atcr". This is correct behavior.

However, if `dir + ".bak"` is a symlink to a directory, `os.ReadDir` would follow the symlink and read the target. The `looksLikeReviewTree` check would then check the target. This might not be the desired behavior. Minor.

Let me also check the `runVerify` change:
```go
if err := BackupExistingVerification(reviewDir); err != nil {
    return Result{}, err
}
if err := writeGroupAtomic(artifacts); err != nil {
    return Result{}, err
}
```

The backup is called before the atomic write. If the atomic write fails, the backup is already created. This means the original is preserved in `.bak`, and the live file is unchanged. This is actually a feature (AC6: partial writes don't corrupt existing files). But it means a failed re-verify leaves a `.bak` file even if nothing changed. This is a minor side effect.

Actually, wait. The backup is a COPY (via `BackupToDotBak`). So the original is still in place. The `.bak` is a copy. After the atomic write, the live file is updated. If the atomic write fails, the live file is unchanged (atomic write doesn't touch the target on failure), and the `.bak` is a copy of the unchanged live file. So a failed write leaves an extra `.bak` file. This is a minor side effect, not a bug.

Let me also check the `runReview` test for the `Force` field:
The test `TestRunReview_ForceBacksUpExistingDir` sets `req.Force = true` and expects the backup. This is correct.

But the test doesn't check the case where `--force` is set but the directory doesn't exist. The plan says "A non-existent directory is a no-op". The code handles this in `forceBackupReviewDir` and `forceBackupOutputDir`. Good.

Let me also look for any issues with the `runReview` cmd change:
```go
if cmd.Flags().Changed("resume") {
    if cmd.Flags().Changed("force") {
        return usageError(errors.New("--resume and --force are mutually exclusive"))
    }
    anchor, _ := cmd.Flags().GetString("resume")
    return runResume(cmd, anchor)
}
```

The check fires before the resume short-circuit. This is correct per AC1b.

But what if `--force` is set without `--resume`? Then the check is skipped, and the code continues. The `Force` field is passed to the ReviewRequest. The fanout package handles it. This is correct.

Let me also check the `runReview` test:
```go
func TestReviewCmd_ResumeAndForceMutuallyExclusive(t *testing.T) {
	isolate(t)
	code, out := execCmdCapture(t, "review", "--resume", "latest", "--force")
	require.Equal(t, 2, code, "AC1b: --resume + --force is a usage error (exit 2)")
	require.Contains(t, out, "--resume and --force are mutually exclusive")
}
```

This test uses `execCmdCapture` which captures output. The test checks for exit code 2 and the error message. Good.

Now, let me think about the most important issues to report:

1. **Double backup risk in verify**: The `WriteVerification` function backs up, and `runVerify` also backs up. If both are called, the second backup would be for the just-written file. This is a design concern.

2. **Output-dir error message doesn't mention --resume**: AC1 says the error should name both. The new error for --output-dir only mentions --force. This is a compliance issue.

3. **Memory usage in copyFile**: Large files are read entirely into memory. This is a performance concern.

4. **Weak looksLikeReviewTree check**: The check is based on directory names. Minor security concern.

5. **Missing integration test for --force on --output-dir**: Test coverage gap.

Let me also check for any issues I might have missed:

In `internal/reconcile/gate.go`:
```go
if _, statErr := os.Stat(filepath.Join(reconDir, FindingsJSON)); statErr == nil {
    if _, err := atomicfs.BackupToDotBak(reconDir); err != nil {
        return Result{}, fmt.Errorf("backing up prior reconciled output: %w", err)
    }
}
```

The backup is only created if `findings.json` exists. But what if the prior run has `adjudication.json` but no `findings.json` (e.g., a partial run)? Then no backup is created. This might miss edge cases. But the comment explains the intent. This is acceptable.

In `internal/verify/emit_verification.go`:
```go
func BackupExistingVerification(reviewDir string) error {
	path := filepath.Join(reviewDir, reconciledSubdir, "verification.json")
	if _, err := atomicfs.BackupToDotBak(path); err != nil {
		return fmt.Errorf("backing up prior verification.json: %w", err)
	}
	return nil
}
```

This is a thin wrapper. The error wrapping is good.

In `internal/verify/pipeline.go`:
```go
if err := BackupExistingVerification(reviewDir); err != nil {
    return Result{}, err
}
```

The error is returned as-is, not wrapped. Minor.

Let me finalize my findings. I think the most important issues are:

1. **MEDIUM: Double backup risk in verify** - If `WriteVerification` is called after `runVerify`, or if `runVerify` internally calls `WriteVerification`, the backup would be for the wrong generation.

2. **MEDIUM: Output-dir error message doesn't mention --resume** - AC1 compliance issue.

3. **LOW: Memory usage in copyFile for large files** - Performance concern.

4. **LOW: Weak looksLikeReviewTree check** - Minor security concern.

5. **LOW: Missing integration test for --force on --output-dir** - Test coverage gap.

Actually, let me reconsider the double backup issue. Looking at the code flow:

`runVerify` is called from the verify pipeline. It builds artifacts and writes them atomically. The backup is called before the atomic write.

`WriteVerification` is an exported function. It's called from... I don't know from the diff. It might be called from tests or from external code.

If `runVerify` and `WriteVerification` are never called together, there's no double backup. But the design is fragile.

Actually, looking at the test `TestWriteVerification_BacksUpPriorOnReWrite`, it calls `WriteVerification` directly. This is a unit test for the exported function. In production, `runVerify` is the main entry point, and it might or might not call `WriteVerification`.

From the diff, I can't tell if `runVerify` calls `WriteVerification`. But the fact that both have backup logic suggests they are separate code paths. If they are, there's no double backup. If they're not, there is.

Given the uncertainty, I'll flag this as a MEDIUM concern.

Let me also check the `runReview` change for any issues:
```go
if cmd.Flags().Changed("force") {
    return usageError(errors.New("--resume and --force are mutually exclusive"))
}
```

Wait, the variable is `force`, but the flag is `force`. Let me re-read:
```go
if cmd.Flags().Changed("force") {
    return usageError(errors.New("--resume and --force are mutually exclusive"))
}
```

Yes, this is correct. The flag is `force` (lowercase).

Actually, wait. Let me re-read the diff:
```go
+		if cmd.Flags().Changed("force") {
+			return usageError(errors.New("--resume and --force are mutually exclusive"))
+		}
```

Yes, this is checking if the `force` flag was changed. Correct.

Now, let me also check the `runReview` test for any issues:
```go
func TestReviewCmd_ForceFlagRegistered(t *testing.T) {
	cmd := newReviewCmd()
	require.NotNil(t, cmd.Flags().Lookup("force"), "review must define --force")
	v, err := cmd.Flags().GetBool("force")
	require.NoError(t, err)
	require.False(t, v, "--force defaults to false")
}
```

This test checks that the `force` flag exists and defaults to false. Good.

OK, I think I have a good list of findings. Let me format them:

1. **MEDIUM: Double backup risk in verify** - `WriteVerification` and `runVerify` both call `BackupExistingVerification`. If both are called, the second backup would be for the just-written file.

2. **MEDIUM: Output-dir error message doesn't mention --resume** - AC1 says the error should name both. The new error for --output-dir only mentions --force.

3. **LOW: Memory usage in copyFile for large files** - `copyFile` reads the entire file into memory. For large files, this is a
</think>
