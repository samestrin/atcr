//go:build windows

package localdebt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

const (
	// flockName is the lock file inside the store dir, the same name the unix
	// lock uses (lock_unix.go), so a store shared across platforms has one lock
	// file. It is not ".lock": an older binary's mkdir lock left a DIRECTORY of
	// that name in existing stores, and this lock never reads or removes it.
	flockName = ".flock"
	lockSleep = 10 * time.Millisecond

	lockfileFailImmediately = 0x00000001        // LOCKFILE_FAIL_IMMEDIATELY
	lockfileExclusiveLock   = 0x00000002        // LOCKFILE_EXCLUSIVE_LOCK
	errorLockViolation      = syscall.Errno(33) // ERROR_LOCK_VIOLATION
)

// The standard library syscall package does not export LockFileEx or
// UnlockFileEx, so they are loaded from kernel32 directly.
var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

// lockWait is how long withLock polls for a held lock before it gives up. It is
// a var (not a const) so tests can shrink it to keep lock-contention cases fast.
var lockWait = 45 * time.Second

// afterLockOpen runs between tryLockFile's open and its LockFileEx. It is a var
// so a test can replace .flock inside that window; production leaves it empty.
var afterLockOpen = func() {}

// withLock takes an exclusive LockFileEx lock on <dir>/.flock and executes fn
// while holding it. It is the Windows twin of the unix flock lock and keeps the
// same guarantees: the kernel releases the lock when the holder's handle closes,
// including when the process is killed, so there is no staleness heuristic and
// no reclaim step for two waiters to race through. The lock is not reentrant.
// session is unused; it is kept for the call shape shared with the other locks.
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
		f, err := tryLockFile(lockPath)
		if err != nil {
			return fmt.Errorf("acquire localdebt lock: %w", err)
		}
		if f != nil {
			defer func() {
				// Remove while still holding the lock, then unlock and close.
				_ = os.Remove(lockPath)
				_ = unlockFile(f)
				_ = f.Close()
			}()
			return fn()
		}
		if time.Now().After(deadline) {
			// Redact to the base name: an absolute path can embed a username
			// (C:\Users\<name>\...), and the package's SECURITY contract (store.go:26-31)
			// requires surfaced errors carry only the base name, like basePathErr.
			return fmt.Errorf("timeout acquiring localdebt lock at %s", flockName)
		}
		time.Sleep(lockSleep)
	}
}

// tryLockFile makes one non-blocking attempt at the lock. It returns the open,
// locked file on success, nil and no error when the lock is held (or the file
// it locked was removed or replaced meanwhile), and an error, carrying only the
// base name, for anything else.
func tryLockFile(lockPath string) (*os.File, error) {
	f, err := openLockFile(lockPath)
	if err != nil {
		// A removed .flock that another handle still has open is "delete
		// pending" on filesystems without POSIX delete semantics, and opening
		// it fails with ERROR_ACCESS_DENIED until that handle closes. Treat
		// that as held. A directory at .flock, or a .flock that does not exist
		// (the store dir refused the create), is a real error.
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) && deletePending(lockPath) {
			return nil, nil
		}
		return nil, err
	}
	afterLockOpen()
	locked, err := lockFileExclusive(f)
	if err != nil || !locked {
		_ = f.Close()
		return nil, err
	}
	// The previous holder may have removed lockPath between our open and our
	// lock; the file we hold is then not the lock anyone else will open.
	held, herr := f.Stat()
	onDisk, derr := os.Lstat(lockPath)
	if herr != nil || derr != nil || !os.SameFile(held, onDisk) {
		_ = unlockFile(f)
		_ = f.Close()
		return nil, nil
	}
	return f, nil
}

// deletePending reports whether an access-denied open of lockPath can be a
// removed-but-still-open .flock: the name still resolves (or cannot even be
// stat'd, which a delete-pending file also does) and is not a directory.
func deletePending(lockPath string) bool {
	fi, err := os.Lstat(lockPath)
	if err != nil {
		return !os.IsNotExist(err)
	}
	return !fi.IsDir()
}

// openLockFile opens or creates lockPath for locking. Unlike os.OpenFile it
// shares delete access, so the holder can remove .flock while waiters have it
// open, and it opens a reparse point itself rather than following it: a
// symlink planted at .flock must not redirect the open (and its create) to a
// file outside the store, so one is refused. Errors carry only the base name.
func openLockFile(lockPath string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: flockName, Err: err}
	}
	h, err := syscall.CreateFile(name,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: flockName, Err: err}
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		_ = syscall.CloseHandle(h)
		return nil, &os.PathError{Op: "stat", Path: flockName, Err: err}
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = syscall.CloseHandle(h)
		return nil, &os.PathError{Op: "open", Path: flockName, Err: errors.New("lock file is a symlink or reparse point")}
	}
	return os.NewFile(uintptr(h), lockPath), nil
}

// lockFileExclusive makes one non-blocking LockFileEx attempt on the first byte
// of f. It reports false and no error when another handle holds the lock.
func lockFileExclusive(f *os.File) (bool, error) {
	var ol syscall.Overlapped
	r, _, errno := procLockFileEx.Call(f.Fd(),
		lockfileExclusiveLock|lockfileFailImmediately, 0, 1, 0,
		uintptr(unsafe.Pointer(&ol)))
	if r != 0 {
		return true, nil
	}
	if errors.Is(errno, errorLockViolation) {
		return false, nil
	}
	return false, &os.PathError{Op: "LockFileEx", Path: flockName, Err: errno}
}

// unlockFile releases the byte lockFileExclusive took. Closing the handle also
// releases it, but Windows documents that as eventual, so the holder unlocks
// explicitly first.
func unlockFile(f *os.File) error {
	var ol syscall.Overlapped
	r, _, errno := procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		return errno
	}
	return nil
}
