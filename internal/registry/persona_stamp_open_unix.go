//go:build unix

package registry

import "syscall"

// openNoFollowNonBlock makes the stamp open fail on a symlink and return at
// once on a FIFO, so a stamp swapped after the Lstat cannot block the open and
// the identity check in readPersonaStampFile decides what is read.
const openNoFollowNonBlock = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
