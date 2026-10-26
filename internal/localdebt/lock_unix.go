//go:build unix

package localdebt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	// flockName is the lock file inside the store dir. It is not ".lock": an
	// older binary's mkdir lock left a DIRECTORY of that name in existing
	// stores, and this lock never reads or removes it.
	flockName = ".flock"
	lockSleep = 10 * time.Millisecond
)

// lockWait is how long withLock polls for a held lock before it gives up. It is
// a var (not a const) so tests can shrink it to keep lock-contention cases fast.
var lockWait = 45 * time.Second

// withLock takes an exclusive flock on <dir>/.flock and executes fn while
// holding it. The kernel releases the lock when the holder's descriptor closes,
// including when the process dies, so there is no staleness heuristic and no
// reclaim step for two waiters to race through. The lock is not reentrant: each
// call opens its own descriptor, so a nested withLock on the same dir waits for
// the outer one and times out. session is unused; it is kept for the call shape
// shared with the !unix lock.
func withLock(dir, session string, fn func() error) error {
	// Ensure the parent store directory exists — WITHOUT recreating a repo root
	// that has since vanished (ensureStoreDir; TD internal/localdebt/root.go:123).
	if err := ensureStoreDir(dir); err != nil {
		return fmt.Errorf("creating localdebt dir for lock: %w", err)
	}

	lockPath := filepath.Join(dir, flockName)
	// O_NOFOLLOW: a symlink planted at .flock must not redirect the open (and
	// its O_CREATE) to a file outside the store.
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("acquire localdebt lock: %w", basePathErr(err))
	}
	// Closing the descriptor releases the flock; nothing is deleted, so a
	// release can never touch a successor's lock.
	defer func() { _ = f.Close() }()
	fd := int(f.Fd())

	deadline := time.Now().Add(lockWait)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("acquire localdebt lock: %w", &os.PathError{Op: "flock", Path: flockName, Err: err})
		}
		if time.Now().After(deadline) {
			// Redact to the base name: an absolute path can embed a username
			// (/Users/<name>/...), and the package's SECURITY contract (store.go:26-31)
			// requires surfaced errors carry only the base name, like basePathErr.
			return fmt.Errorf("timeout acquiring localdebt lock at %s", flockName)
		}
		time.Sleep(lockSleep)
	}
	return fn()
}
