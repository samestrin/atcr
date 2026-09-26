//go:build unix

package stream

import "syscall"

// openNoFollowNonBlock makes an open fail on a symlink and return at once on a
// FIFO, so the handle check in readRegularFile decides what is read.
const openNoFollowNonBlock = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
