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
	// Reject non-regular files (directories, devices, etc.) before OpenFile.
	// On non-unix platforms without O_NOFOLLOW, OpenFile(O_RDONLY) succeeds on
	// directories, returning an *os.File that a content-reader later fails on.
	if !preStat.Mode().IsRegular() {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrInvalid}
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
