//go:build unix

package localdebt

import "syscall"

// openNoFollowNonBlock makes an open fail on a symlink and return at once on a
// FIFO, so the handle check in readSummary decides what is read. Same shape as
// internal/stream/findingsfile_unix.go.
const openNoFollowNonBlock = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
