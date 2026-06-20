package atomicfs

import (
	"encoding/json"
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
