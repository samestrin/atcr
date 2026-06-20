package atomicfs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to a sibling temp file (0644) then renames it
// over path, so a reader never observes a partial write. The rename is atomic
// within a single POSIX filesystem. SIGKILL-orphaned .tmp-* files in the same
// directory are accepted by readers (readers use exact filenames, not globs).
//
// The atomicity guarantee is against concurrent readers, not power loss: neither
// the temp file nor the parent directory is fsynced, so a crash immediately after
// the rename can leave the rename durable while the data is not, yielding a
// zero-length or truncated file. This is intentional — the artifacts written
// through here (review/reconcile/verify output) are regenerable, so crash
// durability is deliberately out of scope and not worth the fsync cost.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// WriteJSON marshals v as two-space-indented JSON (with a trailing newline, so
// the artifact matches the reconcile/verify writers and is human-diffable) and
// writes it through WriteFileAtomic. A marshal failure returns before any file
// is touched, so a bad value can never truncate or partially overwrite path.
func WriteJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(data, '\n'))
}

// BackupToDotBak copies src to src+".bak", replacing any existing backup, so a
// stage that is about to overwrite its prior output can preserve one prior
// generation for recovery (Epic 4.7). src may be a regular file or a directory
// tree; a missing src is a no-op (returns "", nil) so callers need not pre-check
// existence. The copy is made in place — src is left untouched — so callers can
// still read the live tree after backing it up. Returns the backup path on
// success. Only regular files and directories are copied; symlinks and other
// non-regular entries are skipped. Garbage-collecting older .bak state is the
// caller's/user's job.
//
// The copy is staged into a temp sibling (<src>.bak.tmp-*) and then swapped over
// <src>.bak crash-safely (Epic 4.7.1): the prior generation is renamed aside to
// <src>.bak.old (not destroyed) before the staged copy is renamed into place, and
// is removed only after a successful swap / restored on a failed one. Staging the
// prior generation aside happens only after the copy completes, so a failed copy
// leaves the prior .bak intact, and restoring on a failed rename keeps it intact
// across a failed swap too — an interrupted backup never leaves the user with
// neither generation. A stale <src>.bak.old from a prior crashed swap is
// reconciled away at entry.
func BackupToDotBak(src string) (string, error) {
	info, err := os.Lstat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// A symlinked src is skipped (returns "", nil) rather than followed, so
		// the link's target bytes are not silently backed up under the link's
		// name. Lstat (not Stat) is what surfaces the symlink here.
		return "", nil
	}
	bak := src + ".bak"
	bakOld := src + ".bak.old"
	dir := filepath.Dir(src)

	// Clean up any .bak.tmp-* staging temps a prior SIGKILL'd run may have left.
	if matches, _ := filepath.Glob(filepath.Join(dir, "*"+filepath.Base(bak)+".tmp-*")); len(matches) > 0 {
		for _, m := range matches {
			_ = os.RemoveAll(m)
		}
	}

	// Reconcile a stale .bak.old a prior crashed swap may have left, so a retry
	// starts clean and the one-generation contract holds across crash-then-retry.
	if err := os.RemoveAll(bakOld); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("clearing stale staging backup %s: %w", bakOld, err)
	}

	// Stage the backup into a temp sibling. If the copy is interrupted, the prior
	// .bak is not touched (it is staged aside only after the copy completes).
	var staged string
	if info.IsDir() {
		tmpDir, err := os.MkdirTemp(dir, filepath.Base(bak)+".tmp-*")
		if err != nil {
			return "", fmt.Errorf("creating backup staging dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		if err := copyTree(src, tmpDir); err != nil {
			return "", fmt.Errorf("backing up %s: %w", src, err)
		}
		staged = tmpDir
	} else if info.Mode().IsRegular() {
		tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(bak)+".tmp-*")
		if err != nil {
			return "", fmt.Errorf("creating backup staging file: %w", err)
		}
		tmpName := tmpFile.Name()
		if err := tmpFile.Close(); err != nil {
			_ = os.Remove(tmpName)
			return "", fmt.Errorf("closing backup staging file: %w", err)
		}
		defer func() { _ = os.Remove(tmpName) }()

		if err := copyFile(src, tmpName, info.Mode().Perm()); err != nil {
			return "", fmt.Errorf("backing up %s: %w", src, err)
		}
		staged = tmpName
	} else {
		return "", fmt.Errorf("backup %s: not a regular file or directory", src)
	}

	if err := swapStagedBackup(staged, bak, bakOld); err != nil {
		return "", err
	}
	return bak, nil
}

// swapStagedBackup atomically replaces bak with the already-staged copy at
// staged while preserving the prior generation across a failed swap (Epic
// 4.7.1). The prior bak (if any) is renamed aside to bakOld rather than
// destroyed; on a successful rename the superseded bakOld is removed, and on a
// failed rename it is restored to bak so the caller is never left with neither
// generation. This mirrors backupExisting's move-based crash-safe swap for the
// copy-based path. renameFn is indirected through a package var so fault-injection
// tests can drive the failed-swap branch deterministically; in production it is
// os.Rename.
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
			// Best-effort restore. Even when restore also fails, the prior
			// generation survives under bakOld for the next entry-time reconcile.
			if restoreErr := os.Rename(bakOld, bak); restoreErr != nil {
				return fmt.Errorf("renaming staged backup to %s: %w (restore of prior also failed: %v)", bak, err, restoreErr)
			}
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

// renameFn is the swap primitive swapStagedBackup uses, indirected through a
// package var so fault-injection tests can drive the failed-swap branch
// deterministically. In production it is os.Rename.
var renameFn = os.Rename

// CopyPath copies the regular file or directory tree at src to dst, preserving
// per-entry permissions; non-regular entries (symlinks, devices) are skipped, as
// in BackupToDotBak. For a directory, dst is created by the copy (it must not
// already exist). This is the copy primitive a caller uses to replicate a move
// across a filesystem boundary when os.Rename returns EXDEV — the destination is
// expected to be a fresh same-filesystem staging path that is then renamed into
// place. A missing or non-regular/non-directory src is an error.
func CopyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyTree(src, dst)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("copy %s: not a regular file or directory", src)
	}
	return copyFile(src, dst, info.Mode().Perm())
}

// copyFile copies a single regular file's bytes from src to dst with perm.
func copyFile(src, dst string, perm os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return err
	}
	return dstFile.Close()
}

// copyTree recursively copies the directory tree rooted at src to dst, preserving
// per-entry permissions. Non-regular entries (symlinks, devices) are skipped:
// review artifacts are plain files and directories, so there is nothing to lose,
// and following links during a backup would be a surprising side effect.
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
