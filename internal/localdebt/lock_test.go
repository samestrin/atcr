package localdebt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLockWaitKeeps45Seconds locks the acquisition window: the flock rework
// dropped the wall-clock staleness threshold, but a waiter still gives up after
// the same 45s it always did.
func TestLockWaitKeeps45Seconds(t *testing.T) {
	assert.Equal(t, 45*time.Second, lockWait)
}

// TestWithLockTimeoutErrorRedactsPath locks the SECURITY contract (store.go:26-31):
// a surfaced error must not embed the absolute store path, which can carry a
// username segment. The lock timeout error must carry only the base name, like
// every other store path error (basePathErr).
func TestWithLockTimeoutErrorRedactsPath(t *testing.T) {
	oldWait := lockWait
	lockWait = 50 * time.Millisecond
	defer func() { lockWait = oldWait }()

	// An absolute dir with a username-shaped segment the error must never leak.
	dir := filepath.Join(t.TempDir(), "Users", "secretuser", "project", ".atcr", "debt")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	// A live holder keeps the lock for longer than the waiter's lockWait.
	held, release := make(chan struct{}), make(chan struct{})
	holderErr := make(chan error, 1)
	go func() {
		holderErr <- withLock(dir, "live", func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	err := withLock(dir, "waiter", func() error { return nil })
	close(release)
	require.NoError(t, <-holderErr)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout acquiring localdebt lock")
	assert.NotContains(t, err.Error(), "secretuser",
		"timeout error must not leak the username-bearing absolute path")
	assert.NotContains(t, err.Error(), dir,
		"timeout error must not contain the full absolute lock path")
	assert.False(t, strings.Contains(err.Error(), string(filepath.Separator)+"Users"+string(filepath.Separator)),
		"timeout error must carry only the base name, not path segments")
}

// TestWithLockReclaimsStaleLockWithoutOverlap stresses the stale-reclaim path: many
// callers contend at once on a store a dead holder left behind. The old mkdir
// lock's RemoveAll+continue reclaim let two waiters both delete the lock and both
// re-Mkdir, so fn() ran concurrently. The flock lock has no reclaim step — the
// kernel drops a dead holder's lock — so fn must stay strictly serialized. Run
// with -race to amplify detection of any overlap.
func TestWithLockReclaimsStaleLockWithoutOverlap(t *testing.T) {
	// Shrink lockWait so a broken run fails fast instead of stalling 45s per worker.
	oldWait := lockWait
	lockWait = 2 * time.Second
	defer func() { lockWait = oldWait }()

	const iterations = 25
	const workers = 12
	for iter := 0; iter < iterations; iter++ {
		dir := t.TempDir()
		// Pre-seed what a dead holder leaves behind: the lock file it opened, and
		// a legacy mkdir lock directory whose owner epoch is far in the past.
		// Every worker starts at once against that leftover state — the window
		// the old RemoveAll reclaim let two workers both acquire.
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".flock"), nil, 0o600))
		legacy := filepath.Join(dir, ".lock")
		require.NoError(t, os.MkdirAll(legacy, 0o700))
		staleEpoch := time.Now().Add(-time.Hour).Unix()
		require.NoError(t, os.WriteFile(filepath.Join(legacy, "owner.txt"),
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
