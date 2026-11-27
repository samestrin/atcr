package llmclient

import (
	"context"
	"slices"
	"sync"

	"github.com/samestrin/atcr/internal/payload"
)

// ScanCache holds the secret scan of each text Chat has scanned without error,
// keyed by the text itself, so a caller that re-sends the same messages under
// redact (the fanout tool loop, whose history keeps the original strings) scans
// each one once instead of on every call. It holds hits, never redacted text:
// each call still redacts from the caller's strings. A scan that failed is not
// stored, so the next call scans again. At most max texts are stored; once full
// it stores no more and a new text is scanned on every call, as without a
// cache. Safe for concurrent use; a nil *ScanCache caches nothing.
type ScanCache struct {
	mu   sync.Mutex
	max  int
	hits map[string][]payload.SecretHit
}

// NewScanCache returns a ScanCache that stores at most max texts.
func NewScanCache(max int) *ScanCache {
	return &ScanCache{max: max, hits: map[string][]payload.SecretHit{}}
}

// scan returns text's hits from the cache, or from scanSecrets, storing them
// when the scan succeeded and there is room. The returned slice is always the
// caller's own: a cached hit is cloned on the way out and a fresh scan is
// cloned on the way in, so the stored slice is never handed out. A caller that
// mutates its hits — or shares one cache across concurrent calls — cannot
// corrupt the stored scan or race another caller on the backing array.
func (c *ScanCache) scan(ctx context.Context, text string) ([]payload.SecretHit, error) {
	if c == nil {
		return scanSecrets(ctx, text, nil)
	}
	c.mu.Lock()
	hits, ok := c.hits[text]
	c.mu.Unlock()
	if ok {
		return slices.Clone(hits), nil
	}
	hits, err := scanSecrets(ctx, text, nil)
	if err != nil {
		return hits, err
	}
	c.mu.Lock()
	if len(c.hits) < c.max {
		c.hits[text] = slices.Clone(hits)
	}
	c.mu.Unlock()
	return hits, nil
}
