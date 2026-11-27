package localdebt

// The legacy mkdir lock. Only lock_other.go (builds that are neither unix nor
// Windows) uses it, but this file carries no build tag so its reclaim logic
// compiles and is tested on the platforms CI runs. It keeps the known faults
// Epic 38.0 removed from the kernel locks: two waiters can both reclaim a stale
// lock and run fn at once, a killed holder blocks every caller until the stale
// age passes, and a late reclaimer can delete its successor's lock.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	legacyLockSubdir = ".lock"
	legacyLockOwner  = "owner.txt"
)

// legacyLock is the mkdir lock: holding it means having created <dir>/.lock,
// with the holder's session and acquire epoch in .lock/owner.txt. wait is how
// long a caller polls before it gives up, stale the owner-epoch age past which
// a waiter reclaims the lock, and sleep the poll interval. wait must be >=
// stale, or the reclaim is only reachable for locks already stale on arrival.
type legacyLock struct {
	wait, stale, sleep time.Duration
}

// with acquires the lock directory under dir and executes fn.
func (l legacyLock) with(dir, session string, fn func() error) error {
	lockDir := filepath.Join(dir, legacyLockSubdir)
	ownerFile := filepath.Join(lockDir, legacyLockOwner)

	// Ensure the parent store directory exists — WITHOUT recreating a repo root
	// that has since vanished (ensureStoreDir; TD internal/localdebt/root.go:123).
	if err := checkStoreContainment(dir); err != nil {
		return err
	}
	afterContainmentCheck()
	if err := ensureStoreDir(dir); err != nil {
		return fmt.Errorf("creating localdebt dir for lock: %w", err)
	}

	deadline := time.Now().Add(l.wait)
	for {
		err := os.Mkdir(lockDir, 0o700)
		if err == nil {
			// Acquired. Write owner metadata.
			epoch := time.Now().Unix()
			_ = os.WriteFile(ownerFile, []byte(fmt.Sprintf("session=%s|epoch=%d", session, epoch)), 0o600)
			defer func() { _ = os.RemoveAll(lockDir) }()
			if err := checkStoreContainment(dir); err != nil {
				return err
			}
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("acquire localdebt lock: %w", basePathErr(err))
		}

		// Lock is held. Reclaim it if its owner is stale.
		if data, err := os.ReadFile(ownerFile); err == nil && legacyOwnerStale(data, time.Now(), l.stale) {
			_ = os.RemoveAll(lockDir)
			continue
		}

		if time.Now().After(deadline) {
			// Redact to the base name: an absolute lockDir can embed a username
			// (/Users/<name>/...), and the package's SECURITY contract (store.go:26-31)
			// requires surfaced errors carry only the base name, like basePathErr.
			return fmt.Errorf("timeout acquiring localdebt lock at %s", filepath.Base(lockDir))
		}
		time.Sleep(l.sleep)
	}
}

// legacyOwnerStale is the reclaim decision: the owner file's epoch is more than
// stale before now. An owner file with no readable epoch is never stale, so a
// holder caught between its mkdir and its owner write is not reclaimed.
func legacyOwnerStale(data []byte, now time.Time, stale time.Duration) bool {
	e := parseOwnerEpoch(data)
	return e > 0 && now.Sub(time.Unix(e, 0)) > stale
}

func parseOwnerEpoch(data []byte) int64 {
	s := strings.TrimSpace(string(data))
	const prefix = "epoch="
	i := strings.Index(s, prefix)
	if i < 0 {
		return 0
	}
	n, err := strconv.ParseInt(s[i+len(prefix):], 10, 64)
	if err != nil {
		return 0
	}
	return n
}
