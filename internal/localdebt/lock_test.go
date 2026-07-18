package localdebt

import (
	"testing"
)

// TestLockWaitAtLeastStale locks the acquisition-window invariant: a waiter must be
// able to outlast the staleness threshold. If lockWait < lockStale, a waiter's own
// deadline fires before it could ever declare a healthy holder's lock stale, so a
// legitimate critical section running between lockWait and lockStale makes every
// contending caller return a spurious "timeout acquiring localdebt lock" while the
// holder is fine — the stale branch is only reachable for locks already stale on
// arrival, making the two thresholds mutually inconsistent.
func TestLockWaitAtLeastStale(t *testing.T) {
	if lockWait < lockStale {
		t.Fatalf("lockWait (%s) must be >= lockStale (%s): a waiter has to be able to wait until it could declare a lock stale", lockWait, lockStale)
	}
}
