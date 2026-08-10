//go:build integration

package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMain sweeps stale host-side fixtures left by interrupted integration
// runs. Every fixture in these suites is cleaned up through t.Cleanup, which
// a `go test -timeout` kill, a SIGINT, or a panic in the sandboxed-run path
// never runs — and these suites exist to run code that tries to ESCAPE, so a
// leaked fixture set on a shared or CI host is the wrong residue
// (/var/tmp/atcr-forbidden-* contains a file shaped like an OpenSSH private
// key; /tmp carries atcr-snap-*, atcr-sbx-proof-*, atcr-scratch-canary-*,
// atcr-fallback-tmp-probe-* and similar). Sweeping before AND after the run
// makes the next invocation self-heal. Only entries older than an hour are
// removed, so a concurrently running suite's live fixtures survive.
func TestMain(m *testing.M) {
	sweepStaleIntegrationFixtures()
	code := m.Run()
	sweepStaleIntegrationFixtures()
	os.Exit(code)
}

// sweepStaleIntegrationFixtures removes atcr integration fixtures older than
// one hour from the host temp roots. Best-effort: a vanished or unreadable
// entry is skipped, never fatal.
func sweepStaleIntegrationFixtures() {
	cutoff := time.Now().Add(-time.Hour)
	for _, pattern := range []string{
		"/var/tmp/atcr-forbidden-*",
		"/tmp/atcr-*",
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}
			_ = os.RemoveAll(match)
		}
	}
}
