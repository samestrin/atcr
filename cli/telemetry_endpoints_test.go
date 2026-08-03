package cli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/telemetry"
)

// --- Epic 35.12: compiled-in endpoint activation -----------------------------

// TestDefaultTelemetryEndpoints_DistinctAndHTTPS covers AC1: both compiled-in
// telemetry constants name their real atcr.dev host, both are https:// (the
// telemetry client refuses plaintext http, so a non-https value would silently
// no-op every send), and — the load-bearing part — they are DISTINCT. The backend
// serves the two payloads as separate handlers with separate closed key
// allowlists; collapsing them back onto one URL is the regression that earns a 400
// the fail-open path drops silently.
func TestDefaultTelemetryEndpoints_DistinctAndHTTPS(t *testing.T) {
	assert.Equal(t, "https://atcr.dev/api/v1/telemetry", defaultTelemetryEndpoint)
	assert.Equal(t, "https://atcr.dev/api/v1/quality-signal", defaultQualitySignalEndpoint)

	for name, ep := range map[string]string{
		"defaultTelemetryEndpoint":     defaultTelemetryEndpoint,
		"defaultQualitySignalEndpoint": defaultQualitySignalEndpoint,
	} {
		assert.Truef(t, strings.HasPrefix(ep, "https://"),
			"%s must be https:// — the telemetry client refuses plaintext http and would no-op", name)
	}

	assert.NotEqual(t, defaultTelemetryEndpoint, defaultQualitySignalEndpoint,
		"the two surfaces must not share a destination")
}

// TestRootCmd_RoutesPingAndQualitySignalToDistinctEndpoints is AC1b at the
// production wiring, not just at the Client: it drives the REAL command tree built
// by NewRootCmd — which constructs the process telemetry client itself — and proves
// the usage ping and the quality signal leave for different URLs. This is the test
// that catches cli/main.go's client construction being left single-destination;
// because the send path is fail-open, that regression is otherwise silent.
func TestRootCmd_RoutesPingAndQualitySignalToDistinctEndpoints(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	t.Setenv("ATCR_TELEMETRY", "1")      // usage ping on (default-on posture, pinned explicitly)
	t.Setenv("ATCR_QUALITY_SIGNAL", "1") // quality signal is opt-IN
	initGitRepoWithChange(t)
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")

	var (
		mu   sync.Mutex
		hits []struct{ URL, Body string }
	)
	restore := telemetry.SetDoRequestForTest(func(_ *http.Client, req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		mu.Lock()
		hits = append(hits, struct{ URL, Body string }{req.URL.String(), string(b)})
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	t.Cleanup(restore)

	root := NewRootCmd()
	root.SetArgs([]string{"review", "--base", "HEAD^", "--head", "HEAD"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	_ = root.ExecuteContext(context.Background())
	waitQualitySignalInFlight() // the build+send goroutine registers on the client only at dispatch

	// The sends are fire-and-forget goroutines and this path has no drain hook
	// (main() owns that), so wait for both to land rather than sampling once.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(hits) >= 2
	}, 5*time.Second, 10*time.Millisecond, "expected both a usage ping and a quality signal")

	// Settle before the cleanup restores the real transport: a straggler send
	// arriving after the seam is uninstalled would POST at the live host from CI.
	// Both expected sends have already passed the seam by the time Eventually
	// returns, so this only guards an unexpected third.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := append([]struct{ URL, Body string }(nil), hits...)
	mu.Unlock()
	require.Len(t, got, 2, "exactly two sends expected; an extra one would escape the transport seam")

	var sawPing, sawQuality bool
	for _, h := range got {
		// Body shape tells the two surfaces apart at the wire: the quality signal
		// is a JSON array, the usage ping a JSON object.
		if strings.HasPrefix(strings.TrimSpace(h.Body), "[") {
			sawQuality = true
			assert.Equal(t, defaultQualitySignalEndpoint, h.URL,
				"the quality signal must POST to the quality-signal endpoint, not the usage-ping handler")
			continue
		}
		sawPing = true
		assert.Equal(t, defaultTelemetryEndpoint, h.URL, "the usage ping must POST to the usage-ping endpoint")
	}
	assert.True(t, sawPing, "expected a usage ping")
	assert.True(t, sawQuality, "expected a quality signal")
}
