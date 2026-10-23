package fanout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingScanner swaps scanSecrets for a fake that reports every guardAWSKey
// occurrence as an aws-access-token hit (and fails with err when set), and
// counts its invocations per scanned text. The cache is reset on both sides so
// no other test's scans leak in or out.
func countingScanner(t *testing.T, err error) map[string]int {
	t.Helper()
	calls := map[string]int{}
	old := scanSecrets
	scanSecrets = func(_ context.Context, text string, _ *slog.Logger) ([]payload.SecretHit, error) {
		calls[text]++
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

// An agent asking again for text it already had scanned starts a new run (a
// second review in the same `atcr serve` process): the text is rescanned, so a
// transient scan failure is retried, and the warning prints again.
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

	assert.Equal(t, 2, calls[text], "one scan per run")
	assert.Equal(t, 2, strings.Count(stderr, "in config.go"), "one warning per run:\n%s", stderr)
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
