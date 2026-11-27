//go:build !unix && !windows

// The legacy mkdir lock (lock_legacy.go), kept only as the fallback for builds
// that are neither unix nor Windows (js/wasm, wasip1, plan9), none of which atcr
// ships. Unix builds use the flock lock in lock_unix.go and Windows builds the
// LockFileEx lock in lock_windows.go. This file only wires it in.

package localdebt

import "time"

const lockSleep = 10 * time.Millisecond

// lockWait and lockStale are vars (not consts) so tests can shrink them to keep
// lock-contention cases fast; production keeps the invariant below.
//
// INVARIANT: lockWait MUST be >= lockStale. A waiter has to be able to wait at least
// until it could declare a holder's lock stale — otherwise its own deadline fires
// first and a legitimate critical section running between lockWait and lockStale
// turns every contending caller into a spurious "timeout acquiring localdebt lock"
// while the holder is healthy, and the stale-reclaim branch is only ever reachable
// for locks already stale on arrival. lockWait is set strictly greater than lockStale
// so the stale-reclaim path stays reachable inside the wait window. Guarded by
// TestLockWaitAtLeastStale.
var (
	lockWait  = 45 * time.Second
	lockStale = 30 * time.Second
)

// A waiter reclaims this lock from a holder whose owner epoch is lockStale old,
// even while that holder is still writing, so the reclaimer's event temp sweep
// must spare temps younger than that (eventTempSweepMinAge, events.go).
func init() { eventTempSweepMinAge = lockStale }

// withLock acquires a cross-process lock directory under dir and executes fn.
func withLock(dir, session string, fn func() error) error {
	return legacyLock{wait: lockWait, stale: lockStale, sleep: lockSleep}.with(dir, session, fn)
}
