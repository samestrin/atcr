package registry

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// DeprecationWriter receives the one-per-run warning for a deprecated config
// key. It is stderr so a --json or --axi stdout stream never carries it.
var DeprecationWriter io.Writer = os.Stderr

var (
	deprecationMu     sync.Mutex
	deprecationWarned = map[string]bool{}
)

// warnDeprecatedKey prints one warning per process for old → replacement.
func warnDeprecatedKey(old, replacement string) {
	deprecationMu.Lock()
	defer deprecationMu.Unlock()
	if deprecationWarned[old] {
		return
	}
	deprecationWarned[old] = true
	_, _ = fmt.Fprintf(DeprecationWriter, "warning: config key '%s' is deprecated; use '%s'\n", old, replacement)
}

// foldCacheMaxBytes moves a deprecated cache_max_bytes into max_cache_bytes.
func foldCacheMaxBytes(current, legacy **int64) error {
	if *legacy == nil {
		return nil
	}
	if *current != nil {
		return fmt.Errorf("both max_cache_bytes and its deprecated alias cache_max_bytes are set; keep max_cache_bytes only")
	}
	warnDeprecatedKey("cache_max_bytes", "max_cache_bytes")
	*current, *legacy = *legacy, nil
	return nil
}

// foldDeprecatedKeys rewrites deprecated spellings to their current fields.
func (r *Registry) foldDeprecatedKeys() error {
	if err := foldCacheMaxBytes(&r.CacheMaxBytes, &r.LegacyCacheMaxBytes); err != nil {
		return err
	}
	if e := r.Executor; e != nil && e.LegacyTimeoutSecs != nil {
		if e.TimeoutSecs != nil {
			return fmt.Errorf("executor: both fix_timeout_secs and its deprecated alias fix_timeout are set; keep fix_timeout_secs only")
		}
		warnDeprecatedKey("executor.fix_timeout", "executor.fix_timeout_secs")
		e.TimeoutSecs, e.LegacyTimeoutSecs = e.LegacyTimeoutSecs, nil
	}
	return nil
}

func (c *ProjectConfig) foldDeprecatedKeys() error {
	return foldCacheMaxBytes(&c.CacheMaxBytes, &c.LegacyCacheMaxBytes)
}
