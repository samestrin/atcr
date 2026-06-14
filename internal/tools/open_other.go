//go:build !unix

package tools

import (
	"os"
)

// openReadOnly opens path read-only. O_NOFOLLOW is unavailable on this platform,
// so a post-open os.SameFile inode check guards against TOCTOU symlink injection.
// Non-unix platforms remain less secure than unix for untrusted snapshots;
// use a unix host for security-critical deployments.
func openReadOnly(path string) (*os.File, error) {
	preStat, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	postStat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !os.SameFile(preStat, postStat) {
		_ = f.Close()
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
	}
	return f, nil
}
