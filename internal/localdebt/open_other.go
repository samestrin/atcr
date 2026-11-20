//go:build !unix

package localdebt

// openNoFollowNonBlock is zero off unix: syscall has no O_NOFOLLOW or
// O_NONBLOCK there, so readSummary relies on its handle check alone. Same shape
// as internal/stream/findingsfile_other.go.
const openNoFollowNonBlock = 0

// openNoFollow is zero off unix; ensureItemsGitignore's O_EXCL create still
// fails on any entry, a symlink included, at the name.
const openNoFollow = 0
