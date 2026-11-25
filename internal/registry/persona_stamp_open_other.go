//go:build !unix

package registry

// openNoFollowNonBlock is zero off unix: syscall has no O_NOFOLLOW or
// O_NONBLOCK there, so readPersonaStampFile relies on its Lstat and identity
// checks alone.
const openNoFollowNonBlock = 0
