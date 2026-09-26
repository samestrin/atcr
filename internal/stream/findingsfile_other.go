//go:build !unix

package stream

// openNoFollowNonBlock is zero off unix: syscall has no O_NOFOLLOW or
// O_NONBLOCK there, so readRegularFile relies on its handle check alone.
const openNoFollowNonBlock = 0
