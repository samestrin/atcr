//go:build unix

package tools

import (
	"os"
	"syscall"
)

// openReadOnly opens path read-only and refuses to follow a final-component
// symlink (O_NOFOLLOW). This closes the EvalSymlinks->Open TOCTOU window: an
// attacker who swaps a regular file for a symlink after the jail resolved it is
// blocked at the open with ELOOP.
func openReadOnly(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
