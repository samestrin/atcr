package localdebt

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

// TestWithLockReclaimsStaleLockWithoutOverlap stresses the stale-reclaim path: many
// callers contend on a pre-seeded stale lock simultaneously. The racy
// RemoveAll+continue reclamation lets two waiters both delete the lock and both
// re-Mkdir — if one acquires and writes a fresh owner before another's RemoveAll
// runs, that RemoveAll clobbers the live lock and a second caller acquires too, so
// fn() runs concurrently and mutual exclusion is broken. Atomic reclamation (rename
// the stale dir aside; only the rename winner removes it) must keep fn strictly
// serialized. Run with -race to amplify detection of any overlap.
func TestWithLockReclaimsStaleLockWithoutOverlap(t *testing.T) {
	// Shrink timings (preserving lockWait >= lockStale) so the stress loop is fast.
	oldWait, oldStale := lockWait, lockStale
	lockWait, lockStale = 200*time.Millisecond, 100*time.Millisecond
	defer func() { lockWait, lockStale = oldWait, oldStale }()

	const iterations = 25
	const workers = 12
	for iter := 0; iter < iterations; iter++ {
		dir := t.TempDir()
		lockDir := filepath.Join(dir, lockSubdir)
		require.NoError(t, os.MkdirAll(lockDir, 0o700))
		// Pre-seed a stale owner (epoch far in the past) so every worker hits the
		// stale-reclaim path at once — the exact window the racy reclamation could let
		// two workers both acquire.
		staleEpoch := time.Now().Add(-time.Hour).Unix()
		require.NoError(t, os.WriteFile(filepath.Join(lockDir, lockOwner),
			[]byte(fmt.Sprintf("session=dead|epoch=%d", staleEpoch)), 0o600))

		var running, maxConcurrent atomic.Int32
		shared := 0 // unsynchronized on purpose: overlap loses increments and -race flags it
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- withLock(dir, "worker", func() error {
					cur := running.Add(1)
					for {
						m := maxConcurrent.Load()
						if cur <= m || maxConcurrent.CompareAndSwap(m, cur) {
							break
						}
					}
					shared++
					time.Sleep(time.Millisecond)
					running.Add(-1)
					return nil
				})
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err, "iter %d: every caller must acquire, none should time out", iter)
		}
		require.Equal(t, int32(1), maxConcurrent.Load(),
			"iter %d: withLock must never run fn concurrently during stale reclamation", iter)
		require.Equal(t, workers, shared,
			"iter %d: every worker ran exactly once under mutual exclusion", iter)
	}
}
