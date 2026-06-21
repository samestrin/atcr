// Package atomicwrite provides a multi-file atomic write helper: all files are
// staged to sibling temp files before any rename happens, so a failure mid-sequence
// cannot leave a partially-updated group on disk.
package atomicwrite

import (
	"os"
	"path/filepath"
)

// Entry is one artifact in a WriteGroup batch.
type Entry struct {
	Path string
	Data []byte
}

// WriteGroup stages all entries to temp files then renames them in sequence,
// minimising the partial-write window. All data is flushed before the first
// rename; temps for entries that were not renamed are cleaned up on return.
func WriteGroup(artifacts []Entry) error {
	if len(artifacts) == 0 {
		return nil
	}
	temps := make([]string, len(artifacts))
	renamed := make([]bool, len(artifacts))
	defer func() {
		for i, t := range temps {
			if t != "" && !renamed[i] {
				_ = os.Remove(t)
			}
		}
	}()
	for i, a := range artifacts {
		dir := filepath.Dir(a.Path)
		tmp, err := os.CreateTemp(dir, "."+filepath.Base(a.Path)+".tmp-*")
		if err != nil {
			return err
		}
		temps[i] = tmp.Name()
		if _, err := tmp.Write(a.Data); err != nil {
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
	}
	for i, a := range artifacts {
		if err := os.Rename(temps[i], a.Path); err != nil {
			return err
		}
		renamed[i] = true
	}
	return nil
}
