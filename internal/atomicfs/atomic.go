package atomicfs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to a sibling temp file (0644) then renames it
// over path, so a reader never observes a partial write. The rename is atomic
// within a single POSIX filesystem. SIGKILL-orphaned .tmp-* files in the same
// directory are accepted by readers (readers use exact filenames, not globs).
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
// success. Garbage-collecting older .bak state is the caller's/user's job.
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

// copyFile copies a single regular file's bytes from src to dst with perm.
func copyFile(src, dst string, perm os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, perm)
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
