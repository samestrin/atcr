//go:build windows

package cli

// openNoFollow is zero on Windows: syscall exposes no O_NOFOLLOW, and the flag
// has no direct equivalent in the CreateFile flags os.OpenFile maps to. Writes
// that use it therefore keep the same behavior they had before — the path-level
// symlink resolution plus the generated-by marker check still apply — and only
// the last-moment link swap goes unguarded there.
const openNoFollow = 0
