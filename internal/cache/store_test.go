package cache

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_PutThenGet_RoundTrips(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	k := Key(HashText("p"), "m", HashText("persona"))

	content, hit, err := s.Get(k)
	require.NoError(t, err)
	assert.False(t, hit, "cold cache must miss")
	assert.Empty(t, content)

	require.NoError(t, s.Put(k, "the review body"))

	content, hit, err = s.Get(k)
	require.NoError(t, err)
	assert.True(t, hit, "after Put the same key must hit")
	assert.Equal(t, "the review body", content)
}

func TestStore_DistinctKeysDoNotCollide(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	k1 := Key(HashText("p1"), "m", HashText("x"))
	k2 := Key(HashText("p2"), "m", HashText("x"))
	require.NoError(t, s.Put(k1, "one"))
	require.NoError(t, s.Put(k2, "two"))

	c1, hit1, _ := s.Get(k1)
	c2, hit2, _ := s.Get(k2)
	assert.True(t, hit1)
	assert.True(t, hit2)
	assert.Equal(t, "one", c1)
	assert.Equal(t, "two", c2)
}

func TestStore_MissOnUnknownKey(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	_, hit, err := s.Get(Key(HashText("nope"), "m", HashText("x")))
	require.NoError(t, err)
	assert.False(t, hit)
}

// TestStore_CorruptEntrySelfHeals: a garbage file at the key's path is treated
// as a miss (not an error) and removed so the next run can repopulate it.
func TestStore_CorruptEntrySelfHeals(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	s := NewStore(dir, 0)
	k := Key(HashText("p"), "m", HashText("x"))
	// Write a corrupt entry directly at the key's file path.
	path := filepath.Join(dir, fileNameFor(k))
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	_, hit, err := s.Get(k)
	require.NoError(t, err, "corrupt entry must not surface as an error")
	assert.False(t, hit)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "corrupt entry should be removed to self-heal")
}

// TestStore_CorruptEntryRemovalFailureSurfacesError: when a corrupt entry is
// found but cannot be removed (e.g. the cache dir is not writable), the removal
// error is surfaced to the caller rather than silently discarded — a real IO
// failure must not be masked as a clean miss.
func TestStore_CorruptEntryRemovalFailureSurfacesError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root bypasses directory permission checks")
	}
	dir := filepath.Join(t.TempDir(), "cache")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	s := NewStore(dir, 0)
	k := Key(HashText("p"), "m", HashText("x"))
	// Write a corrupt entry directly at the key's file path.
	path := filepath.Join(dir, fileNameFor(k))
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	// Make the cache dir non-writable so os.Remove of the corrupt entry fails.
	require.NoError(t, os.Chmod(dir, 0o555))
	defer func() { _ = os.Chmod(dir, 0o755) }() // restore so t.TempDir cleanup succeeds

	_, hit, err := s.Get(k)
	assert.Error(t, err, "failure to remove a corrupt entry must surface as an error")
	assert.False(t, hit)
}

// TestStore_EvictsOldestWhenOverCap writes several entries past the byte cap and
// asserts the least-recently-used (oldest mtime) entries are evicted while the
// newest survive.
func TestStore_EvictsOldestWhenOverCap(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	// Each entry's JSON is ~500 bytes (71-char key digest + 400-char body +
	// envelope); a 1200-byte cap therefore holds at most two entries.
	s := NewStore(dir, 1200)

	body := func(n string) string { return n + ":" + strings.Repeat("x", 400) }

	keys := []string{
		Key(HashText("a"), "m", HashText("x")),
		Key(HashText("b"), "m", HashText("x")),
		Key(HashText("c"), "m", HashText("x")),
		Key(HashText("d"), "m", HashText("x")),
	}
	for _, k := range keys {
		require.NoError(t, s.Put(k, body(k)))
		// Stagger mtimes so eviction order is deterministic (oldest first).
		time.Sleep(10 * time.Millisecond)
	}

	// Total on disk must be within the cap.
	total := dirSize(t, dir)
	assert.LessOrEqual(t, total, int64(1200), "store must evict down to the cap")

	// The most recently written key must still be present.
	_, hit, err := s.Get(keys[len(keys)-1])
	require.NoError(t, err)
	assert.True(t, hit, "newest entry must survive eviction")

	// The oldest key must have been evicted.
	_, hit0, err := s.Get(keys[0])
	require.NoError(t, err)
	assert.False(t, hit0, "oldest entry must be evicted first")
}

// TestStore_GetRefreshesRecency: reading an entry bumps its mtime so it is not
// the first victim of a later eviction.
func TestStore_GetRefreshesRecency(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	// ~500 bytes/entry; a 1200-byte cap holds two entries, so a third forces a
	// single eviction — exercising recency ordering precisely.
	s := NewStore(dir, 1200)
	body := strings.Repeat("x", 400)

	kA := Key(HashText("a"), "m", HashText("x"))
	kB := Key(HashText("b"), "m", HashText("x"))
	require.NoError(t, s.Put(kA, body))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, s.Put(kB, body))
	time.Sleep(10 * time.Millisecond)

	// Touch A so it becomes most-recently-used.
	_, hit, _ := s.Get(kA)
	require.True(t, hit)
	time.Sleep(10 * time.Millisecond)

	// A third entry forces eviction; B (now oldest) should go, A should stay.
	kC := Key(HashText("c"), "m", HashText("x"))
	require.NoError(t, s.Put(kC, body))

	_, hitA, _ := s.Get(kA)
	_, hitB, _ := s.Get(kB)
	assert.True(t, hitA, "recently-read entry must survive")
	assert.False(t, hitB, "untouched older entry must be evicted")
}

// TestStore_RejectsMalformedKeys hardens against path traversal: a key whose
// digest is not 64-char hex must be refused by Put and miss on Get, never joined
// into a filesystem path that could escape the cache dir.
func TestStore_RejectsMalformedKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	s := NewStore(dir, 0)

	bad := []string{
		"sha256:../../etc/passwd",
		"sha256:" + strings.Repeat("z", 64), // right length, non-hex
		"sha256:abc",                        // too short
		"notadigest",                        // missing prefix
		"sha256:",                           // empty digest
	}
	for _, k := range bad {
		err := s.Put(k, "x")
		assert.Error(t, err, "Put must refuse malformed key %q", k)
		_, hit, gerr := s.Get(k)
		assert.NoError(t, gerr)
		assert.False(t, hit, "Get must miss on malformed key %q", k)
	}
	// Nothing escaped the cache dir (it should not even exist — no valid Put ran).
	_, statErr := os.Stat(filepath.Join(t.TempDir(), "etc"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestStore_ConcurrentPutGet(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "cache"), 0)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k := Key(HashText(string(rune('a'+i))), "m", HashText("x"))
			require.NoError(t, s.Put(k, "body"))
			_, hit, err := s.Get(k)
			require.NoError(t, err)
			assert.True(t, hit)
		}(i)
	}
	wg.Wait()
}

func dirSize(t *testing.T, dir string) int64 {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		require.NoError(t, err)
		total += info.Size()
	}
	return total
}
