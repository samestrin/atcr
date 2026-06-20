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
// The copy is staged into a temp sibling (<src>.bak.tmp-*) and then renamed
// over <src>.bak, with the destructive RemoveAll(<src>.bak) deferred until just
// before the rename. A crash or interruption during the copy therefore leaves
// the prior .bak generation intact.
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
	dir := filepath.Dir(src)

	// Stage the backup into a temp sibling, then atomically swap it over bak.
	// If the copy is interrupted, the prior .bak remains untouched.
	if info.IsDir() {
		tmpDir, err := os.MkdirTemp(dir, filepath.Base(bak)+".tmp-*")
		if err != nil {
			return "", fmt.Errorf("creating backup staging dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		if err := copyTree(src, tmpDir); err != nil {
			return "", fmt.Errorf("backing up %s: %w", src, err)
		}
		if err := os.RemoveAll(bak); err != nil {
			return "", fmt.Errorf("removing stale backup %s: %w", bak, err)
		}
		if err := os.Rename(tmpDir, bak); err != nil {
			return "", fmt.Errorf("renaming staged backup to %s: %w", bak, err)
		}
	} else {
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
		if err := os.RemoveAll(bak); err != nil {
			return "", fmt.Errorf("removing stale backup %s: %w", bak, err)
		}
		if err := os.Rename(tmpName, bak); err != nil {
			return "", fmt.Errorf("renaming staged backup to %s: %w", bak, err)
		}
	}
	return bak, nil
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
