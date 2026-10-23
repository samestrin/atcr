package fanout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingScanner swaps scanSecrets for a fake that reports every guardAWSKey
// occurrence as an aws-access-token hit (and fails with err when set), and
// counts its invocations per scanned text. It is safe for concurrent agents;
// read the counts once they are done. The cache is reset on both sides so no
// other test's scans leak in or out.
func countingScanner(t *testing.T, err error) map[string]int {
	t.Helper()
	calls := map[string]int{}
	var mu sync.Mutex
	old := scanSecrets
	scanSecrets = func(_ context.Context, text string, _ *slog.Logger) ([]payload.SecretHit, error) {
		mu.Lock()
		calls[text]++
		mu.Unlock()
		var hits []payload.SecretHit
		for off := 0; ; {
			i := strings.Index(text[off:], guardAWSKey)
			if i < 0 {
				break
			}
			start := off + i
			hits = append(hits, payload.SecretHit{Kind: "aws-access-token", Start: start, End: start + len(guardAWSKey)})
			off = start + len(guardAWSKey)
		}
		return hits, err
	}
	resetSecretScanCache()
	t.Cleanup(func() {
		scanSecrets = old
		resetSecretScanCache()
	})
	return calls
}

func totalCalls(calls map[string]int) int {
	n := 0
	for _, c := range calls {
		n += c
	}
	return n
}

// panelSegments is what renderAgent hands guardSecrets for one agent: the
// shared scope constraint and the shared payload.
func panelSegments(scope, text string) []guardSegment {
	return []guardSegment{
		{text: scope, file: secretScopeLabel},
		{text: text, file: "payload", mode: "blocks"},
	}
}

var panel = []string{"greta", "kai", "lena", "omar", "pia"}

// Row secretguard.go:80: a panel sharing one payload scans each distinct segment
// once, not once per agent, and the warning prints once.
func TestGuardSecrets_PanelScansEachDistinctSegmentOnce(t *testing.T) {
	calls := countingScanner(t, nil)
	scope := "SCOPE CONSTRAINT: only config.go\n"
	text := guardPayload()

	stderr := captureStderr(t, func() {
		for _, agent := range panel {
			out, err := guardSecrets(secretGuardWarn, agent, panelSegments(scope, text)...)
			require.NoError(t, err)
			assert.Equal(t, []string{scope, text}, out, "warn sends every agent's text unchanged")
		}
	})

	assert.Equal(t, 1, calls[text], "the shared payload is scanned once for the whole panel")
	assert.Equal(t, 1, calls[scope], "the shared scope constraint is scanned once for the whole panel")
	assert.Equal(t, 2, totalCalls(calls), "one scan per distinct segment")
	assert.Equal(t, 1, strings.Count(stderr, "1 secret(s) [aws-access-token] in config.go"), "the warn line prints once:\n%s", stderr)
	assert.NotContains(t, stderr, guardAWSKey)
}

// A panel member with a payload of its own (another mode, another chunk) gets
// its own scan and its own warning; the shared text is still scanned once.
func TestGuardSecrets_DistinctPayloadIsScannedAndReportedOnItsOwn(t *testing.T) {
	calls := countingScanner(t, nil)
	shared := guardPayload()
	other := "diff --git a/env.go b/env.go\n@@ -1 +1 @@\n+key := \"" + guardAWSKey + "\"\n"

	stderr := captureStderr(t, func() {
		for _, agent := range panel {
			_, err := guardSecrets(secretGuardWarn, agent, panelSegments("", shared)...)
			require.NoError(t, err)
		}
		_, err := guardSecrets(secretGuardWarn, "zed", panelSegments("", other)...)
		require.NoError(t, err)
	})

	assert.Equal(t, 1, calls[shared])
	assert.Equal(t, 1, calls[other])
	assert.Equal(t, 2, totalCalls(calls), "the empty scope constraint is never scanned")
	assert.Equal(t, 1, strings.Count(stderr, "in config.go"), stderr)
	assert.Equal(t, 1, strings.Count(stderr, "in env.go"), stderr)
}

// Redact reuses the cached hits: every agent's prompt is redacted, not only the
// first one's.
func TestGuardSecrets_RedactAppliesCachedHitsToEveryAgent(t *testing.T) {
	calls := countingScanner(t, nil)
	text := guardPayload()

	stderr := captureStderr(t, func() {
		for _, agent := range panel {
			out, err := guardSecrets(secretGuardRedact, agent, panelSegments("", text)...)
			require.NoError(t, err)
			assert.NotContains(t, out[1], guardAWSKey, "agent %s: the key must not reach the provider", agent)
			assert.Contains(t, out[1], payload.SecretPlaceholder)
		}
	})

	assert.Equal(t, 1, calls[text])
	assert.Equal(t, 1, strings.Count(stderr, "redacted 1 secret(s)"), stderr)
}

// block still fails closed for EVERY agent when the scan errored, though the
// scanner ran once: a cached failure is a failure, never a clean result.
func TestGuardSecrets_BlockFailsClosedOnACachedScanError(t *testing.T) {
	scanErr := fmt.Errorf("%w: exit status 2", payload.ErrSecretScanFailed)
	calls := countingScanner(t, scanErr)
	clean := "diff --git a/main.go b/main.go\n@@ -1 +1 @@\n+port := 8080\n"

	stderr := captureStderr(t, func() {
		for _, agent := range panel {
			out, err := guardSecrets(secretGuardBlock, agent, panelSegments("", clean)...)
			require.Error(t, err, "agent %s must not be sent an unchecked prompt", agent)
			assert.Nil(t, out)
			assert.True(t, errors.Is(err, ErrSecretGuardBlocked), "agent %s: %v", agent, err)
			assert.True(t, errors.Is(err, payload.ErrSecretScanFailed), "agent %s: %v", agent, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("agent %q", agent), "the error names the agent it blocked")
		}
	})

	assert.Equal(t, 1, calls[clean], "the failed scan is not retried per agent")
	assert.Equal(t, 1, strings.Count(stderr, "using the built-in ruleset"), "the scan-failure warning prints once:\n%s", stderr)
}

// block on a hit fails every agent, not only the first one to scan.
func TestGuardSecrets_BlockFailsEveryAgentOnACachedHit(t *testing.T) {
	calls := countingScanner(t, nil)
	text := guardPayload()
	for _, agent := range panel {
		_, err := guardSecrets(secretGuardBlock, agent, panelSegments("", text)...)
		require.Error(t, err, "agent %s", agent)
		assert.True(t, errors.Is(err, ErrSecretGuardBlocked))
		assert.Contains(t, err.Error(), "config.go")
	}
	assert.Equal(t, 1, calls[text])
}

// An agent asking again for the segments it already had guarded starts a new
// run (a second review in the same `atcr serve` process): the warning prints
// again. A clean scan depends only on the text and the scanner, so it is reused.
func TestGuardSecrets_RepeatAgentStartsANewRun(t *testing.T) {
	calls := countingScanner(t, nil)
	text := guardPayload()

	stderr := captureStderr(t, func() {
		for run := 0; run < 2; run++ {
			for _, agent := range panel {
				_, err := guardSecrets(secretGuardWarn, agent, panelSegments("", text)...)
				require.NoError(t, err)
			}
		}
	})

	assert.Equal(t, 1, calls[text], "a successful scan is reused by the next run")
	assert.Equal(t, 2, strings.Count(stderr, "in config.go"), "one warning per run:\n%s", stderr)
}

// A failed scan is not carried into the next run: the transient failure is
// retried, and block still fails closed in both runs while it persists.
func TestGuardSecrets_NewRunRetriesAFailedScan(t *testing.T) {
	calls := countingScanner(t, fmt.Errorf("%w: timeout", payload.ErrSecretScanFailed))
	text := guardPayload()

	captureStderr(t, func() {
		for run := 0; run < 2; run++ {
			for _, agent := range panel {
				_, err := guardSecrets(secretGuardBlock, agent, panelSegments("", text)...)
				require.ErrorIs(t, err, ErrSecretGuardBlocked)
			}
		}
	})

	assert.Equal(t, 2, calls[text], "one scan per run")
}

// One agent's chunks share its scope constraint; it is scanned once, not once
// per chunk, while each chunk is scanned and reported on its own.
func TestGuardSecrets_ChunksShareOneScopeScan(t *testing.T) {
	calls := countingScanner(t, nil)
	scope := "SCOPE CONSTRAINT: only config.go\n"
	chunks := []string{guardPayload(), "diff --git a/b.go b/b.go\n@@ -1 +1 @@\n+x := 1\n"}

	stderr := captureStderr(t, func() {
		for _, agent := range panel {
			for _, ck := range chunks {
				_, err := guardSecrets(secretGuardWarn, agent, panelSegments(scope, ck)...)
				require.NoError(t, err)
			}
		}
	})

	assert.Equal(t, 1, calls[scope])
	assert.Equal(t, 1, calls[chunks[0]])
	assert.Equal(t, 1, calls[chunks[1]])
	assert.Equal(t, 1, strings.Count(stderr, "in config.go"), stderr)
}

// A cached scan expires, so a long-lived process never reuses a stale result or
// keeps suppressing a warning for a later run with a different panel.
func TestGuardSecrets_CachedScanExpires(t *testing.T) {
	calls := countingScanner(t, nil)
	now := time.Unix(1_000_000, 0)
	oldNow := secretScanNow
	secretScanNow = func() time.Time { return now }
	t.Cleanup(func() { secretScanNow = oldNow })
	text := guardPayload()

	stderr := captureStderr(t, func() {
		_, err := guardSecrets(secretGuardWarn, "greta", panelSegments("", text)...)
		require.NoError(t, err)
		now = now.Add(secretScanTTL + time.Second)
		_, err = guardSecrets(secretGuardWarn, "kai", panelSegments("", text)...)
		require.NoError(t, err)
	})

	assert.Equal(t, 2, calls[text], "an expired scan is redone")
	assert.Equal(t, 2, strings.Count(stderr, "in config.go"), stderr)
}

// The cached result is only valid for the scanner that produced it. Pointing
// gitleaks at another config (or putting another binary on PATH) rescans.
func TestGuardSecrets_ScannerChangeRescans(t *testing.T) {
	calls := countingScanner(t, nil)
	text := guardPayload()
	t.Setenv("GITLEAKS_CONFIG", "")
	captureStderr(t, func() {
		_, err := guardSecrets(secretGuardWarn, "greta", panelSegments("", text)...)
		require.NoError(t, err)
		t.Setenv("GITLEAKS_CONFIG", "/etc/gitleaks-strict.toml")
		_, err = guardSecrets(secretGuardWarn, "kai", panelSegments("", text)...)
		require.NoError(t, err)
	})
	assert.Equal(t, 2, calls[text], "a scan under another gitleaks config is not reused")
}

// off never scans, so it never fills the cache either.
func TestGuardSecrets_OffNeverScans(t *testing.T) {
	calls := countingScanner(t, nil)
	for _, agent := range panel {
		out, err := guardSecrets(secretGuardOff, agent, panelSegments("", guardPayload())...)
		require.NoError(t, err)
		assert.Contains(t, out[1], guardAWSKey)
	}
	assert.Zero(t, totalCalls(calls))
}

// The cache is bounded: a process scanning many distinct texts (a chunked run,
// a long `atcr serve`) never holds more than secretScanCacheCap entries.
func TestGuardSecrets_CacheIsBounded(t *testing.T) {
	countingScanner(t, nil)
	for i := 0; i < 3*secretScanCacheCap; i++ {
		_, err := guardSecrets(secretGuardWarn, "greta", guardSegment{text: fmt.Sprintf("chunk %d\n", i), file: "payload"})
		require.NoError(t, err)
	}
	assert.LessOrEqual(t, secretScanCacheLen(), secretScanCacheCap)
}

// gateScanner holds every scan until release is called, so agents started
// together all reach the cache while the first scan is still running. started
// reports how many scans have begun.
func gateScanner(t *testing.T) (started *atomic.Int32, release func()) {
	t.Helper()
	started = new(atomic.Int32)
	gate := make(chan struct{})
	inner := scanSecrets
	scanSecrets = func(ctx context.Context, text string, log *slog.Logger) ([]payload.SecretHit, error) {
		started.Add(1)
		<-gate
		return inner(ctx, text, log)
	}
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return started, release
}

// runConcurrently starts one goroutine per agent on a cold cache, lets them
// pile up behind the first scan, then releases it. errs[i] is agents[i]'s
// guardSecrets error.
func runConcurrently(t *testing.T, policy string, agents []string, segs []guardSegment) []error {
	t.Helper()
	started, release := gateScanner(t)
	errs := make([]error, len(agents))
	var wg sync.WaitGroup
	for i, agent := range agents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = guardSecrets(policy, agent, segs...)
		}()
	}
	require.Eventually(t, func() bool { return started.Load() > 0 }, 5*time.Second, time.Millisecond)
	time.Sleep(50 * time.Millisecond) // every agent reaches the cache before the scan returns
	release()
	wg.Wait()
	return errs
}

func manyAgents(n int) []string {
	agents := make([]string, n)
	for i := range agents {
		agents[i] = fmt.Sprintf("agent-%02d", i)
	}
	return agents
}

// Finding F1.1: agents rendering concurrently on a cold cache (review.go runs
// guardSecrets per agent in parallel) wait for one scan per distinct segment
// instead of each starting gitleaks. Run with -race.
func TestGuardSecrets_ConcurrentPanelScansEachDistinctSegmentOnce(t *testing.T) {
	calls := countingScanner(t, nil)
	scope := "SCOPE CONSTRAINT: only config.go\n"
	text := guardPayload()

	var errs []error
	stderr := captureStderr(t, func() {
		errs = runConcurrently(t, secretGuardWarn, manyAgents(16), panelSegments(scope, text))
	})

	for i, err := range errs {
		require.NoError(t, err, "agent %d", i)
	}
	assert.Equal(t, 1, calls[text], "the shared payload is scanned once for the concurrent panel")
	assert.Equal(t, 1, calls[scope], "the shared scope constraint is scanned once for the concurrent panel")
	assert.Equal(t, 2, totalCalls(calls))
	assert.Equal(t, 1, strings.Count(stderr, "1 secret(s) [aws-access-token] in config.go"), "the warn line prints once:\n%s", stderr)
}

// block still fails closed for every concurrent agent that waited on a scan
// that errored.
func TestGuardSecrets_ConcurrentBlockFailsClosedOnAScanError(t *testing.T) {
	calls := countingScanner(t, fmt.Errorf("%w: exit status 2", payload.ErrSecretScanFailed))
	clean := "diff --git a/main.go b/main.go\n@@ -1 +1 @@\n+port := 8080\n"

	var errs []error
	captureStderr(t, func() {
		errs = runConcurrently(t, secretGuardBlock, manyAgents(16), panelSegments("", clean))
	})

	for i, err := range errs {
		require.ErrorIs(t, err, ErrSecretGuardBlocked, "agent %d must not be sent an unchecked prompt", i)
		assert.ErrorIs(t, err, payload.ErrSecretScanFailed, "agent %d", i)
	}
	assert.Equal(t, 1, calls[clean])
}

// Finding F1.2: two reviews interleaved in one process (an MCP server) each
// warn once. Starting the second review's run must not drop the first
// review's claims, or the first review's next agent warns again.
func TestGuardSecrets_InterleavedRunsKeepEachOthersWarningClaims(t *testing.T) {
	countingScanner(t, nil)
	text := guardPayload()

	stderr := captureStderr(t, func() {
		for _, agent := range []string{"greta", "greta", "kai", "kai", "lena", "lena"} {
			_, err := guardSecrets(secretGuardWarn, agent, panelSegments("", text)...)
			require.NoError(t, err)
		}
	})

	assert.Equal(t, 2, strings.Count(stderr, "in config.go"), "one warning per review:\n%s", stderr)
}

// The same for a failed scan: each interleaved review retries it once, not
// once per agent after the other review's retry replaced its claims.
func TestGuardSecrets_InterleavedRunsKeepEachOthersScanClaims(t *testing.T) {
	calls := countingScanner(t, fmt.Errorf("%w: timeout", payload.ErrSecretScanFailed))
	text := guardPayload()

	captureStderr(t, func() {
		for _, agent := range []string{"greta", "greta", "kai", "kai", "lena", "lena"} {
			_, err := guardSecrets(secretGuardBlock, agent, panelSegments("", text)...)
			require.ErrorIs(t, err, ErrSecretGuardBlocked, "agent %s", agent)
		}
	})

	assert.Equal(t, 2, calls[text], "one scan per review")
}

func secretScansLen() int {
	secretScanCache.Lock()
	defer secretScanCache.Unlock()
	return len(secretScanCache.scans)
}

func chunkSeg(i int) guardSegment {
	return guardSegment{text: fmt.Sprintf("chunk %d\n", i), file: "payload"}
}

// Finding F1.3: crossing the cap evicts one entry, the least recently used,
// not the whole cache: the shared payload every agent reuses keeps its scan.
func TestGuardSecrets_CrossingTheCapEvictsOneEntry(t *testing.T) {
	calls := countingScanner(t, nil)
	shared := guardSegment{text: "shared payload\n", file: "payload"}

	guard := func(agent string, s guardSegment) {
		t.Helper()
		_, err := guardSecrets(secretGuardWarn, agent, s)
		require.NoError(t, err)
	}
	guard("greta", chunkSeg(0))
	guard("greta", shared)
	for i := 1; i < secretScanCacheCap-1; i++ {
		guard("greta", chunkSeg(i))
	}
	require.Equal(t, secretScanCacheCap, secretScansLen(), "the cache is full")
	guard("kai", shared) // reused, so it is no longer the oldest

	guard("greta", chunkSeg(secretScanCacheCap)) // crosses the cap

	assert.Equal(t, secretScanCacheCap, secretScansLen(), "one entry is evicted, not the whole cache")
	guard("lena", shared)
	guard("kai", chunkSeg(1))
	assert.Equal(t, 1, calls[shared.text], "the shared payload keeps its scan")
	assert.Equal(t, 1, calls[chunkSeg(1).text], "the other entries keep theirs")
	guard("kai", chunkSeg(0))
	assert.Equal(t, 2, calls[chunkSeg(0).text], "the least recently used entry was the one evicted")
}

// An expired entry is evicted before a live one, even a live one used less
// recently.
func TestGuardSecrets_CrossingTheCapEvictsAnExpiredEntryFirst(t *testing.T) {
	calls := countingScanner(t, nil)
	now := time.Unix(1_000_000, 0)
	oldNow := secretScanNow
	secretScanNow = func() time.Time { return now }
	t.Cleanup(func() { secretScanNow = oldNow })

	guard := func(agent string, s guardSegment) {
		t.Helper()
		_, err := guardSecrets(secretGuardWarn, agent, s)
		require.NoError(t, err)
	}
	guard("greta", chunkSeg(0)) // scanned first, so it expires first
	now = now.Add(secretScanTTL / 2)
	for i := 1; i < secretScanCacheCap; i++ {
		guard("greta", chunkSeg(i))
	}
	guard("kai", chunkSeg(0)) // the most recently used, but its scan is the oldest
	now = now.Add(secretScanTTL/2 + time.Second)

	guard("greta", chunkSeg(secretScanCacheCap)) // crosses the cap

	assert.Equal(t, secretScanCacheCap, secretScansLen())
	guard("kai", chunkSeg(1))
	assert.Equal(t, 1, calls[chunkSeg(1).text], "the least recently used live entry is kept")
}
