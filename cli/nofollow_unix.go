//go:build !windows

package cli

import "syscall"

// openNoFollow makes an open fail (ELOOP) when the final path component is a
// symlink, so a link swapped in between a path check and the write is refused
// instead of followed. It is a POSIX open flag with no Windows equivalent —
// see nofollow_windows.go for what that platform falls back to.
const openNoFollow = syscall.O_NOFOLLOW
