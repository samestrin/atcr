//go:build !unix

package localdebt

// openNoFollowNonBlock is zero off unix: syscall has no O_NOFOLLOW or
// O_NONBLOCK there, so readSummary relies on its handle check alone. Same shape
// as internal/stream/findingsfile_other.go.
const openNoFollowNonBlock = 0
