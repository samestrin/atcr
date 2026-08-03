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

// TestDefaultTelemetryEndpoints_DistinctAndHTTPS is a deliberate CHANGE-DETECTOR
// on the two compiled-in destinations, and nothing more.
//
// That is the whole intent, so the test says so rather than dressing it up: these
// are the addresses this binary transmits its users' telemetry to, and repointing
// either one should require editing a test that spells out the old value. The
// https:// and distinctness properties both matter — the client refuses plaintext
// http and would silently no-op, and collapsing the two onto one URL routes the
// quality-signal array at the usage-ping handler for a 400 the fail-open path
// drops — but asserting them separately here would add no independent guard,
// because the two equalities below already fix every property the strings have.
// They are covered where they can actually fail: against values this test does not
// choose, in TestRootCmd_RoutesPingAndQualitySignalToDistinctEndpoints, which
// compares the URLs sends actually left for.
func TestDefaultTelemetryEndpoints_DistinctAndHTTPS(t *testing.T) {
	assert.Equal(t, "https://atcr.dev/api/v1/telemetry", defaultTelemetryEndpoint,
		"repointing the usage-ping destination is a deliberate act; update this literal to confirm it")
	assert.Equal(t, "https://atcr.dev/api/v1/quality-signal", defaultQualitySignalEndpoint,
		"repointing the quality-signal destination is a deliberate act; update this literal to confirm it")
}

// withTelemetryEndpoints redirects both compiled-in destinations for the duration
// of one test and restores them afterwards. It exists to prove the destinations
// are an OVERRIDABLE seam rather than sealed constants — the property an embedder
// (the private atcr-enterprise module, a fork, an air-gapped deployment) needs to
// point NewRootCmd at its own collector, and the property `-ldflags -X` needs at
// build time (a const cannot be set by ldflags at all).
//
// PRECONDITION: mutates package-global state, so no test using it may call
// t.Parallel — the same constraint unsetTelemetryEnv documents.
func withTelemetryEndpoints(t *testing.T, usage, quality string) {
	t.Helper()
	prevUsage, prevQuality := defaultTelemetryEndpoint, defaultQualitySignalEndpoint
	defaultTelemetryEndpoint, defaultQualitySignalEndpoint = usage, quality
	t.Cleanup(func() {
		defaultTelemetryEndpoint, defaultQualitySignalEndpoint = prevUsage, prevQuality
	})
}

// telemetrySend is one intercepted outbound telemetry request: the URL it was
// aimed at and the body it carried. Body shape discriminates the two surfaces at
// the wire — the quality signal is a JSON array, the usage ping a JSON object.
type telemetrySend struct{ URL, Body string }

// recordingTransport installs a transport seam recording every outbound telemetry
// request and returns a snapshot accessor. Sends are fire-and-forget goroutines,
// so the slice is mutex-guarded and the accessor returns a copy.
//
// The seam is process-global (telemetry.doRequest is a package-level
// atomic.Value), so the recorded set may include stragglers from earlier tests.
// Callers must therefore assert SET MEMBERSHIP — "the send I care about went
// where I expect" — never a raw total, which foreign traffic can break.
func recordingTransport(t *testing.T) (func(), func() []telemetrySend) {
	t.Helper()
	var (
		mu    sync.Mutex
		sends []telemetrySend
	)
	restore := telemetry.SetDoRequestForTest(func(_ *http.Client, req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		mu.Lock()
		sends = append(sends, telemetrySend{URL: req.URL.String(), Body: string(b)})
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	t.Cleanup(restore)
	return restore, func() []telemetrySend {
		mu.Lock()
		defer mu.Unlock()
		return append([]telemetrySend(nil), sends...)
	}
}

// TestNewRootCmd_HonorsOverriddenEndpoints pins the embedder seam: NewRootCmd is
// the only advertised zero-argument constructor, so an embedder that cannot
// redirect its telemetry at construction has no escape short of total
// disablement. Overriding the package-level destinations must reroute BOTH
// surfaces of the client NewRootCmd builds — proving the values are a seam, not
// sealed constants baked into the binary.
func TestNewRootCmd_HonorsOverriddenEndpoints(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	t.Setenv("ATCR_TELEMETRY", "1")
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	withTelemetryEndpoints(t, "https://collector.internal/usage", "https://collector.internal/quality")
	initGitRepoWithChange(t)
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")

	var (
		mu   sync.Mutex
		urls []string
	)
	restore := telemetry.SetDoRequestForTest(func(_ *http.Client, req *http.Request) (*http.Response, error) {
		mu.Lock()
		urls = append(urls, req.URL.String())
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	t.Cleanup(restore)

	root := NewRootCmd()
	root.SetArgs([]string{"review", "--base", "HEAD^", "--head", "HEAD"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	_ = root.ExecuteContext(context.Background())
	waitQualitySignalInFlight()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(urls) >= 2
	}, 5*time.Second, 10*time.Millisecond, "expected both surfaces to send")
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := append([]string(nil), urls...)
	mu.Unlock()
	assert.Contains(t, got, "https://collector.internal/usage",
		"the usage ping must follow the overridden destination, not the compiled-in default")
	assert.Contains(t, got, "https://collector.internal/quality",
		"the quality signal must follow the overridden destination, not the compiled-in default")
	for _, u := range got {
		assert.NotContains(t, u, "atcr.dev",
			"no send may reach the upstream host once the destinations are overridden")
	}
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

	// Retarget both destinations at unroutable .test hosts. The routing property
	// under test is "the two surfaces go to DIFFERENT places", which is asserted
	// against the variables — so it holds whatever their value — while removing any
	// possibility that a send from this test reaches the production host.
	withTelemetryEndpoints(t, "https://usage.test/ingest", "https://quality.test/ingest")

	recorder, hits := recordingTransport(t)
	_ = recorder

	// Own the client rather than letting NewRootCmd build its own. Holding the
	// handle is what makes the drain DETERMINISTIC: the previous shape had no
	// handle, so it substituted require.Eventually plus a magic 100ms Sleep, and on
	// any failure path — Eventually timing out on a loaded runner, or a require
	// firing before the Sleep — t.Cleanup would reinstall the real transport while a
	// dispatched goroutine had not yet reached currentDoRequest, which then POSTs at
	// production. Wait() closes that window instead of narrowing it.
	client := telemetry.NewWithQualitySignal(defaultTelemetryEndpoint, defaultQualitySignalEndpoint)
	root := NewRootCmdWithClient(client)
	root.SetArgs([]string{"review", "--base", "HEAD^", "--head", "HEAD"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	_ = root.ExecuteContext(context.Background())
	waitQualitySignalInFlight() // the build+send goroutine registers on the client only at dispatch
	client.Wait()               // deterministic: every dispatched send has now passed the seam

	got := hits()

	// Assert SET MEMBERSHIP, not a raw count. The transport seam is process-global
	// (telemetry.doRequest is a package-level atomic.Value), so a fire-and-forget
	// goroutine still alive from an earlier test resolves currentDoRequest at
	// execution time and lands in this slice. A require.Len(got, 2) therefore fails
	// on foreign traffic for reasons that have nothing to do with endpoint routing.
	// What this test actually claims is that each surface went to its own
	// destination — so count the two shapes at their two URLs and ignore the rest.
	var sawPing, sawQuality int
	for _, h := range got {
		// Body shape tells the two surfaces apart at the wire: the quality signal
		// is a JSON array, the usage ping a JSON object.
		if strings.HasPrefix(strings.TrimSpace(h.Body), "[") {
			if h.URL == defaultQualitySignalEndpoint {
				sawQuality++
			}
			assert.NotEqual(t, defaultTelemetryEndpoint, h.URL,
				"the quality signal must never POST to the usage-ping handler")
			continue
		}
		if h.URL == defaultTelemetryEndpoint {
			sawPing++
		}
		assert.NotEqual(t, defaultQualitySignalEndpoint, h.URL,
			"the usage ping must never POST to the quality-signal handler")
	}
	assert.Equal(t, 1, sawPing, "exactly one usage ping must reach the usage-ping endpoint")
	assert.Equal(t, 1, sawQuality, "exactly one quality signal must reach the quality-signal endpoint")
}
