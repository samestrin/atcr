//go:build !unix

package tools

import "os"

// openReadOnly opens path read-only. O_NOFOLLOW is unavailable on this platform,
// so the final-component symlink guard degrades to the jail's EvalSymlinks check.
func openReadOnly(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY, 0)
}
