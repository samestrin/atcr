package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/telemetry"
)

// --- Story 6: Gated Quality-Signal Transmission (Phase 5) -----------------

// countingQualityBuilder wraps the payload-constructor seam so a test can assert
// whether the gated send call site built a quality-signal payload. It is the
// "payload-constructor seam" AC 06-01's strictness requirement names: a disabled
// gate must short-circuit BEFORE any construction, observable here rather than
// inferred from the absence of a request alone.
func countingQualityBuilder(t *testing.T) *int32 {
	t.Helper()
	var n int32
	prev := buildQualitySignalPayloadFn
	buildQualitySignalPayloadFn = func(root string) ([]telemetry.QualitySignal, error) {
		atomic.AddInt32(&n, 1)
		return prev(root)
	}
	t.Cleanup(func() { buildQualitySignalPayloadFn = prev })
	return &n
}

// captureSendBodies installs a do-request seam recording every outbound telemetry
// body (passive ping AND quality signal share the transport). A quality-signal
// body is a JSON array (starts with '['); a passive-ping body is a JSON object
// with an "event" key — so a test can tell the two surfaces apart at the wire.
func captureSendBodies(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	bodies := []string{}
	restore := telemetry.SetDoRequestForTest(func(_ *http.Client, req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	t.Cleanup(restore)
	return &bodies
}

// qualitySendBodies filters captured bodies to the quality-signal sends (JSON
// arrays), excluding the passive ping's object body.
func qualitySendBodies(bodies []string) []string {
	out := []string{}
	for _, b := range bodies {
		if strings.HasPrefix(strings.TrimSpace(b), "[") {
			out = append(out, b)
		}
	}
	return out
}

// runReviewSend drives runReview end-to-end against a hermetic mock findings
// backend (no real network), with the given telemetry client injected, then
// drains the fire-and-forget send goroutine so the count is race-free. It mirrors
// TestReview_TelemetryStatus_ReflectsGateOutcome's wiring. Caller must isolate(t)
// first (and seed any .atcr/debt records) so cwd is stable.
func runReviewSend(t *testing.T, client *telemetry.Client, args ...string) {
	t.Helper()
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	initGitRepoWithChange(t)
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)

	logger, err := log.New("info", "text", io.Discard)
	require.NoError(t, err)
	cmd := newReviewCmd()
	cmd.SetContext(telemetry.NewContext(log.NewContext(context.Background(), logger), client))
	cmd.SetOut(io.Discard)
	cmd.SetErr(new(bytes.Buffer))
	full := append([]string{"--base", "HEAD^", "--head", "HEAD"}, args...)
	require.NoError(t, cmd.ParseFlags(full))
	_ = runReview(cmd, cmd.Flags().Args())
	client.Wait()
}

const qsEndpoint = "https://telemetry.test/ingest"

// TestQualitySignalSend_GateDisabled_ZeroRequests_Review proves a completed review
// run with the opt-in gate disabled (the default) attempts zero quality-signal
// requests and builds no payload (AC 06-01 Scenario 1). ATCR_TELEMETRY=0 silences
// the passive ping so the request counter reflects the quality-signal path alone.
func TestQualitySignalSend_GateDisabled_ZeroRequests_Review(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	t.Setenv("ATCR_TELEMETRY", "0")
	hits := countingDoRequest(t)
	builds := countingQualityBuilder(t)
	runReviewSend(t, telemetry.New(qsEndpoint))
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "disabled gate: no quality-signal request may fire on review")
	assert.Equal(t, int32(0), atomic.LoadInt32(builds), "disabled gate: no payload may be constructed on review")
}

// TestQualitySignalSend_GateDisabled_ZeroRequests_Reconcile is the reconcile-path
// twin of the above (AC 06-01 Scenario 2).
func TestQualitySignalSend_GateDisabled_ZeroRequests_Reconcile(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	t.Setenv("ATCR_TELEMETRY", "0")
	fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
	hits := countingDoRequest(t)
	builds := countingQualityBuilder(t)
	runReconcileGated(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "disabled gate: no quality-signal request may fire on reconcile")
	assert.Equal(t, int32(0), atomic.LoadInt32(builds), "disabled gate: no payload may be constructed on reconcile")
}

// TestQualitySignalSend_GateDisabled_NoPayloadConstruction is the strictness core
// of AC 06-01: the disabled gate must short-circuit BEFORE payload construction,
// asserted via the constructor seam — not merely as an absent request.
func TestQualitySignalSend_GateDisabled_NoPayloadConstruction(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	t.Setenv("ATCR_TELEMETRY", "0")
	// Seed terminal records so that, were the gate wrongly consulted after building,
	// the payload would be non-empty — making an ordering bug unmistakable.
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
	builds := countingQualityBuilder(t)
	runReconcileGated(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, int32(0), atomic.LoadInt32(builds),
		"the disabled gate must short-circuit before any payload is constructed")
}

// TestQualitySignalSend_ExplicitlyDisabledConfig_ZeroRequests proves a persisted
// quality_signal: false resolves the gate disabled and attempts nothing (AC 06-01
// Scenario 2 variant).
func TestQualitySignalSend_ExplicitlyDisabledConfig_ZeroRequests(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	t.Setenv("ATCR_TELEMETRY", "0")
	writeAtcrConfig(t, "agents: [bruce]\nquality_signal: false\n")
	fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
	hits := countingDoRequest(t)
	builds := countingQualityBuilder(t)
	runReconcileGated(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "quality_signal: false must attempt no request")
	assert.Equal(t, int32(0), atomic.LoadInt32(builds), "quality_signal: false must build no payload")
}

// TestQualitySignalSend_UnrelatedTelemetrySurfacesUnaffected proves independence
// (AC 06-01 Scenario 3): with the passive ping enabled (its opt-out default) but
// the quality-signal gate disabled, the passive ping still fires while the
// quality-signal path stays dark — neither a build nor a quality-signal request.
func TestQualitySignalSend_UnrelatedTelemetrySurfacesUnaffected(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	_ = os.Unsetenv("ATCR_TELEMETRY") // passive ping enabled by default
	fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
	bodies := captureSendBodies(t)
	builds := countingQualityBuilder(t)
	runReconcileGated(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, int32(0), atomic.LoadInt32(builds), "quality-signal path must stay dark while telemetry is enabled")
	assert.Empty(t, qualitySendBodies(*bodies), "no quality-signal body may be transmitted")
	assert.NotEmpty(t, *bodies, "the unrelated passive ping must still fire — independence, not suppression")
}

// TestQualitySignalSend_EndpointReachableButGateDisabled_ZeroRequests proves
// endpoint availability never bypasses consent: a live https endpoint with the gate
// disabled still transmits nothing (AC 06-01 Edge Case 1).
func TestQualitySignalSend_EndpointReachableButGateDisabled_ZeroRequests(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	t.Setenv("ATCR_TELEMETRY", "0")
	fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
	hits := countingDoRequest(t)
	runReconcileGated(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "a reachable endpoint must not bypass a disabled opt-in gate")
}

// TestQualitySignalSend_PreviewWinsOverGateDisabled proves --preview renders the
// payload locally and reaches no send call site — the preview short-circuits at the
// top of the run before the gated send is registered (AC 06-01 Edge Case 2).
func TestQualitySignalSend_PreviewWinsOverGateDisabled(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	hits := countingDoRequest(t)
	builds := countingQualityBuilder(t)
	out, err := runPreview(t, newReconcileCmd(), telemetry.New(qsEndpoint), "--preview")
	require.NoError(t, err)
	assert.Contains(t, out, "persona_id_hash", "preview must render the payload locally")
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "preview must reach no send call site")
	assert.Equal(t, int32(0), atomic.LoadInt32(builds), "preview builds via its own path, not the gated send seam")
}

// TestQualitySignalSend_GateReEvaluatedFreshPerRun proves the gate is resolved
// fresh each run with no in-process cache (AC 06-01 Edge Case 3): a first disabled
// run builds nothing; after quality_signal: true is persisted, the next run resolves
// enabled and builds the payload — observed via the constructor seam.
func TestQualitySignalSend_GateReEvaluatedFreshPerRun(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	t.Setenv("ATCR_TELEMETRY", "0")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
	builds := countingQualityBuilder(t)

	runReconcileGated(t, telemetry.New(qsEndpoint), "r")
	require.Equal(t, int32(0), atomic.LoadInt32(builds), "first run, no opt-in: no payload built")

	writeAtcrConfig(t, "agents: [bruce]\nquality_signal: true\n")
	runReconcileGated(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, int32(1), atomic.LoadInt32(builds),
		"a freshly persisted opt-in must be observed on the next run — no stale gate cache")
}
