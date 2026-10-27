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

// afterLockOpen runs between tryFlock's open and its flock. It is a var so a
// test can replace .flock inside that window; production leaves it empty.
var afterLockOpen = func() {}

// withLock takes an exclusive flock on <dir>/.flock and executes fn while
// holding it. The kernel releases the lock when the holder's descriptor closes,
// including when the process dies, so there is no staleness heuristic and no
// reclaim step for two waiters to race through. The lock is not reentrant: each
// call opens its own descriptor, so a nested withLock on the same dir waits for
// the outer one and times out. session is unused; it is kept for the call shape
// shared with the !unix lock.
//
// The holder removes .flock before it unlocks, so a store tracked in git never
// gains an untracked lock file. A waiter that was blocked on the removed file
// then holds a lock nobody else can see, so every acquire checks that the file
// it locked is still the one at lockPath and starts over if not.
func withLock(dir, session string, fn func() error) error {
	// Ensure the parent store directory exists — WITHOUT recreating a repo root
	// that has since vanished (ensureStoreDir; TD internal/localdebt/root.go:123).
	if err := ensureStoreDir(dir); err != nil {
		return fmt.Errorf("creating localdebt dir for lock: %w", err)
	}

	lockPath := filepath.Join(dir, flockName)
	deadline := time.Now().Add(lockWait)
	for {
		f, err := tryFlock(lockPath)
		if err != nil {
			return fmt.Errorf("acquire localdebt lock: %w", err)
		}
		if f != nil {
			defer func() {
				// Remove while still holding the lock, then unlock by closing.
				_ = os.Remove(lockPath)
				_ = f.Close()
			}()
			return fn()
		}
		if time.Now().After(deadline) {
			// Redact to the base name: an absolute path can embed a username
			// (/Users/<name>/...), and the package's SECURITY contract (store.go:26-31)
			// requires surfaced errors carry only the base name, like basePathErr.
			return fmt.Errorf("timeout acquiring localdebt lock at %s", flockName)
		}
		time.Sleep(lockSleep)
	}
}

// tryFlock makes one non-blocking attempt at the lock. It returns the open,
// locked file on success, nil and no error when the lock is held (or the file
// it locked was removed or replaced meanwhile), and an error, carrying only the
// base name, for anything else.
func tryFlock(lockPath string) (*os.File, error) {
	// O_NOFOLLOW: a symlink planted at .flock must not redirect the open (and
	// its O_CREATE) to a file outside the store.
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, basePathErr(err)
	}
	afterLockOpen()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, &os.PathError{Op: "flock", Path: flockName, Err: err}
	}
	// The previous holder may have removed lockPath between our open and our
	// flock; the file we hold is then not the lock anyone else will open.
	held, herr := f.Stat()
	onDisk, derr := os.Lstat(lockPath)
	if herr != nil || derr != nil || !os.SameFile(held, onDisk) {
		_ = f.Close()
		return nil, nil
	}
	return f, nil
}
