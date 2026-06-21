package cache

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// entry is the on-disk cache record: the full key (for a defensive match on
// read, guarding against filename truncation/collision bugs) and the cached
// reviewer output. Deliberately minimal — LRU recency is tracked by file mtime,
// not a stored timestamp.
type entry struct {
	Key     string `json:"key"`
	Content string `json:"content"`
}

// Store is a content-addressed, size-bounded cache of reviewer outputs on disk.
// Each entry is one JSON file named by the key digest under dir. Total on-disk
// size is capped at maxBytes via least-recently-used eviction (oldest mtime
// first); maxBytes <= 0 disables eviction (unbounded), matching the codebase's
// "0 = unlimited" escape-hatch idiom for byte budgets.
//
// All operations are guarded by a single mutex. Contention is negligible in
// practice — a review fans out to a handful of agents and each touches the cache
// at most twice (one Get, one Put) around a slow LLM call — so the simpler
// fully-serialized design is preferred over finer-grained locking.
type Store struct {
	dir      string
	maxBytes int64
	mu       sync.Mutex
}

// NewStore returns a Store rooted at dir with the given size cap. The directory
// is created lazily on the first Put, so constructing a Store is cheap and never
// fails.
func NewStore(dir string, maxBytes int64) *Store {
	return &Store{dir: dir, maxBytes: maxBytes}
}

// Get returns the cached content for key. hit is false (with a nil error) on a
// cold miss, a corrupt entry (which is removed to self-heal), or a defensive
// key mismatch. A read hit refreshes the entry's mtime so it is treated as
// recently used by a later eviction. A genuine IO error (other than not-exist)
// is surfaced so the caller can log it and fall back to a live call.
func (s *Store) Get(key string) (string, bool, error) {
	name, ok := fileName(key)
	if !ok {
		return "", false, nil // malformed key — treat as a miss
	}
	path := filepath.Join(s.dir, name)

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var e entry
	if jerr := json.Unmarshal(data, &e); jerr != nil || e.Key != key {
		// Corrupt or mismatched entry: drop it so the next run repopulates a
		// clean record, and report a miss rather than an error. A failure to
		// remove the entry is itself a genuine IO error, so surface it (per the
		// contract above) instead of masking it as a clean miss.
		if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			return "", false, rerr
		}
		return "", false, nil
	}
	// Refresh recency for LRU eviction (best-effort: a touch failure only makes
	// the entry a slightly earlier eviction candidate, never a correctness bug).
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	return e.Content, true, nil
}

// Put stores content under key (atomic write) and then enforces the size cap by
// evicting least-recently-used entries. A write error is returned; eviction is
// best-effort and never fails a Put (a transient eviction error only defers
// reclaiming disk to the next Put).
func (s *Store) Put(key, content string) error {
	name, ok := fileName(key)
	if !ok {
		return errors.New("cache: refusing to store under a malformed key")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.dir, name)
	if err := atomicfs.WriteJSON(path, entry{Key: key, Content: content}); err != nil {
		return err
	}
	s.evict()
	return nil
}

// evict deletes least-recently-used entries until the total on-disk size is at
// or under maxBytes. maxBytes <= 0 disables eviction. Caller must hold s.mu.
func (s *Store) evict() {
	if s.maxBytes <= 0 {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return // best-effort; reclaim on a later Put
	}
	type fileInfo struct {
		path  string
		size  int64
		mtime time.Time
	}
	var files []fileInfo
	var total int64
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{filepath.Join(s.dir, de.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= s.maxBytes {
		return
	}
	// Oldest first — least-recently-used (Get refreshes mtime).
	sort.Slice(files, func(i, j int) bool { return files[i].mtime.Before(files[j].mtime) })
	for _, f := range files {
		if total <= s.maxBytes {
			break
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
		}
	}
}

// fileName maps a cache key to its on-disk filename. The key MUST be a canonical
// "sha256:<64-hex>" digest (as produced by Key); the filename is "<hex>.json".
//
// Security: the hex digest is validated to be exactly 64 lowercase hex chars
// before it is used as a path segment. Get/Put are exported, so a crafted or
// buggy key like "sha256:../../etc/passwd" must never reach filepath.Join — the
// strict hex check makes path traversal impossible regardless of caller input.
func fileName(key string) (string, bool) {
	digest := strings.TrimPrefix(key, "sha256:")
	if digest == key || !isHex64(digest) {
		return "", false
	}
	return digest + ".json", true
}

// isHex64 reports whether s is exactly 64 lowercase hexadecimal characters — the
// shape of a sha256 hex digest. Rejecting anything else keeps cache filenames to
// a fixed, traversal-proof character set.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// fileNameFor is the test-facing form of fileName that returns just the name,
// panicking on a malformed key (tests always pass well-formed keys).
func fileNameFor(key string) string {
	name, ok := fileName(key)
	if !ok {
		panic("cache: malformed key in test: " + key)
	}
	return name
}
