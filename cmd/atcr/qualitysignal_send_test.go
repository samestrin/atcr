package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	waitQualitySignalInFlight() // drain the detached build+send goroutine before the client (it registers only at dispatch)
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

// TestQualitySignalSend_UnrecognizedEnvValueWarnsViaCmdStderr proves the send
// path's opt-in gate surfaces the unrecognized-value warning on the COMMAND's
// stderr — where cmd-scoped tests can capture it — not the uncapturable global
// os.Stderr: with ATCR_QUALITY_SIGNAL misspelled (e.g. "ture"), a reconcile run
// resolves the gate disabled (the privacy fail-safe), transmits nothing, and
// the warning lands in the command's stderr buffer, mirroring the --preview
// path's TestPreview_UnrecognizedEnvValueWarnsViaCmdStderr.
func TestQualitySignalSend_UnrecognizedEnvValueWarnsViaCmdStderr(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_QUALITY_SIGNAL", "ture")
	t.Setenv("ATCR_TELEMETRY", "0")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	reconcileFixture(t)
	hits := countingDoRequest(t)

	logger, err := log.New("info", "text", io.Discard)
	require.NoError(t, err)
	client := telemetry.New(qsEndpoint)
	cmd := newReconcileCmd()
	cmd.SetContext(telemetry.NewContext(log.NewContext(context.Background(), logger), client))
	cmd.SetOut(io.Discard)
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	require.NoError(t, cmd.ParseFlags([]string{"r"}))
	_ = runReconcile(cmd, cmd.Flags().Args())
	waitQualitySignalInFlight()
	client.Wait()

	assert.Contains(t, errBuf.String(), `unrecognized ATCR_QUALITY_SIGNAL value "ture"`,
		"a misspelled opt-in must warn on the command's stderr on the send path, not the uncapturable global os.Stderr")
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "an unrecognized value fails safe to disabled: nothing may be sent")
}

// TestQualitySignalSend_PayloadBuildRunsDetached proves the O(n) debt-store read
// and aggregation run on the detached goroutine, off the run's critical path:
// maybeSendQualitySignal must return even while the payload builder is blocked,
// with only the synchronous opt-in gate resolved inline. The forced build error
// after release keeps the test hermetic (the send short-circuits, no client
// interaction).
func TestQualitySignalSend_PayloadBuildRunsDetached(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	t.Setenv("ATCR_TELEMETRY", "0")
	entered := make(chan struct{})
	release := make(chan struct{})
	prev := buildQualitySignalPayloadFn
	buildQualitySignalPayloadFn = func(string) ([]telemetry.QualitySignal, error) {
		close(entered)
		<-release // block until the test releases the builder
		return nil, errors.New("forced build error")
	}
	t.Cleanup(func() { buildQualitySignalPayloadFn = prev })
	released := false
	defer func() {
		if !released {
			close(release) // unblock a synchronous (pre-fix) build so the wrapper goroutine can exit
		}
	}()

	client := telemetry.New(qsEndpoint)
	logger, err := log.New("info", "text", io.Discard)
	require.NoError(t, err)
	ctx := telemetry.NewContext(log.NewContext(context.Background(), logger), client)

	done := make(chan struct{})
	go func() { maybeSendQualitySignal(ctx, io.Discard); close(done) }()

	<-entered // the builder was invoked
	select {
	case <-done:
		// returned without waiting for the blocked build — the build is detached
	case <-time.After(2 * time.Second):
		t.Fatal("maybeSendQualitySignal blocked on the payload build: the O(n) store read must run on the detached goroutine, off the run's critical path")
	}
	close(release)
	released = true
}

// reconcileFixture writes the standard one-finding reconcile fixture used by the
// send tests so runReconcileGated has a review dir to reconcile.
func reconcileFixture(t *testing.T) {
	t.Helper()
	fixtureReview(t, "r", map[string]string{"sources/host/findings.txt": "LOW|a.go:1|x|f|style|1|ev|host\n"})
}

// runReconcileSend drives runReconcile with the client injected and returns the
// run's resolved exit code, draining the send goroutine so any capture is
// race-free. Unlike runReconcileGated it surfaces the exit code, so a fail-open
// test can assert it is identical to the gate-disabled baseline.
func runReconcileSend(t *testing.T, client *telemetry.Client, args ...string) int {
	t.Helper()
	logger, err := log.New("info", "text", io.Discard)
	require.NoError(t, err)
	cmd := newReconcileCmd()
	cmd.SetContext(telemetry.NewContext(log.NewContext(context.Background(), logger), client))
	cmd.SetOut(io.Discard)
	cmd.SetErr(new(bytes.Buffer))
	require.NoError(t, cmd.ParseFlags(args))
	rerr := runReconcile(cmd, cmd.Flags().Args())
	waitQualitySignalInFlight() // drain the detached build+send goroutine before the client (it registers only at dispatch)
	client.Wait()
	return exitCode(rerr)
}

// --- AC 06-02: opted-in send transmits exactly one allowlisted payload --------

// TestQualitySignalSend_EnabledViaEnv_ExactlyOneRequest_CorrectCounts proves an
// env opt-in transmits exactly one request whose body unmarshals into Story 1's
// allowlisted payload with the hand-computed per-(persona, model) counts, and no
// key outside the allowlist (AC 06-02 Scenario 1). ATCR_TELEMETRY=0 silences the
// passive ping so the captured body is unambiguously the quality signal.
func TestQualitySignalSend_EnabledViaEnv_ExactlyOneRequest_CorrectCounts(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	t.Setenv("ATCR_TELEMETRY", "0")
	// bruce+claude-sonnet-4-6: 2 dismissed (wontfix) + 1 confirmed (resolved).
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "b.go")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "resolved", "c.go")
	reconcileFixture(t)
	bodies := captureSendBodies(t)

	runReconcileGated(t, telemetry.New(qsEndpoint), "r")

	q := qualitySendBodies(*bodies)
	require.Len(t, q, 1, "an opted-in run must transmit exactly one quality-signal request")

	var got []telemetry.QualitySignal
	require.NoError(t, json.Unmarshal([]byte(q[0]), &got))
	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].DismissedCount, "hand-computed dismissed count")
	assert.Equal(t, 1, got[0].ConfirmedCount, "hand-computed confirmed count")
	assert.Equal(t, "claude-sonnet-4-6", got[0].Model)
	assert.NotEmpty(t, got[0].PersonaIDHash)
	assert.NotContains(t, q[0], "bruce", "the raw persona name must never reach the wire")

	// Body carries no key outside the four-field allowlist.
	var raw []map[string]any
	require.NoError(t, json.Unmarshal([]byte(q[0]), &raw))
	require.Len(t, raw, 1)
	for k := range raw[0] {
		assert.Contains(t, []string{"persona_id_hash", "model", "dismissed_count", "confirmed_count"}, k,
			"no field outside Story 1's allowlist may appear on the wire")
	}
}

// TestQualitySignalSend_EnabledViaConfig_SameSingleSendBehavior proves a persisted
// quality_signal: true is as sufficient as an env opt-in: exactly one send (AC
// 06-02 Scenario 3).
func TestQualitySignalSend_EnabledViaConfig_SameSingleSendBehavior(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	t.Setenv("ATCR_TELEMETRY", "0")
	writeAtcrConfig(t, "agents: [bruce]\nquality_signal: true\n")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	reconcileFixture(t)
	bodies := captureSendBodies(t)

	runReconcileGated(t, telemetry.New(qsEndpoint), "r")

	assert.Len(t, qualitySendBodies(*bodies), 1, "config consent must transmit exactly one send")
}

// TestQualitySignalSend_SentBytesEqualPreviewBytes proves the transmitted bytes are
// byte-identical to the --preview rendering for the same fixture — the preview IS
// the send (AC 06-02 Scenario 2, complementing AC 03-03 from the send side).
func TestQualitySignalSend_SentBytesEqualPreviewBytes(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	t.Setenv("ATCR_TELEMETRY", "0")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "resolved", "b.go")
	seedQualityRecord(t, "diana", "gpt-5", "wontfix", "c.go")
	reconcileFixture(t)

	// Preview bytes (open fixture findings persisted by a reconcile run are
	// non-terminal, so they never change the aggregation).
	previewOut, err := runPreview(t, newReconcileCmd(), nil, "--preview")
	require.NoError(t, err)
	previewJSON, _ := splitPreview(previewOut)

	bodies := captureSendBodies(t)
	runReconcileGated(t, telemetry.New(qsEndpoint), "r")
	q := qualitySendBodies(*bodies)
	require.Len(t, q, 1)
	assert.Equal(t, strings.TrimSpace(previewJSON), strings.TrimSpace(q[0]),
		"sent bytes must be byte-identical to the --preview render")
}

// TestQualitySignalSend_PlaintextOrEmptyEndpointNoTransmission proves the transport
// no-ops on a non-HTTPS or empty endpoint even with the gate enabled — no plaintext
// transmission ever occurs (AC 06-02 EC2).
func TestQualitySignalSend_PlaintextOrEmptyEndpointNoTransmission(t *testing.T) {
	for _, endpoint := range []string{"", "http://telemetry.test/ingest"} {
		t.Run("endpoint="+endpoint, func(t *testing.T) {
			isolate(t)
			t.Setenv("ATCR_QUALITY_SIGNAL", "1")
			t.Setenv("ATCR_TELEMETRY", "0")
			seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
			reconcileFixture(t)
			hits := countingDoRequest(t)
			runReconcileGated(t, telemetry.New(endpoint), "r")
			assert.Equal(t, int32(0), atomic.LoadInt32(hits), "a non-HTTPS/empty endpoint must never transmit")
		})
	}
}

// TestQualitySignalSend_EmptyAggregation_DefinedBehaviorNoError proves a zero-row
// aggregation transmits nothing and errors nothing — the enabled branch
// short-circuits after aggregation (AC 06-02 EC1).
func TestQualitySignalSend_EmptyAggregation_DefinedBehaviorNoError(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	t.Setenv("ATCR_TELEMETRY", "0")
	// No terminal records seeded → aggregation is empty.
	reconcileFixture(t)
	hits := countingDoRequest(t)
	code := runReconcileSend(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "an empty aggregation must transmit nothing")
	assert.NotEqual(t, exitUsage, code, "an empty aggregation must not raise a usage error")
}

// --- AC 06-03: transport failure fails open -----------------------------------

// failingSeam installs a do-request seam that always returns err (no network), so a
// fail-open test can assert the run outcome is unchanged despite a transport error.
func failingSeam(t *testing.T, status int, err error) {
	t.Helper()
	restore := telemetry.SetDoRequestForTest(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	t.Cleanup(restore)
}

// TestQualitySignalSend_500Response_RunOutcomeUnchanged proves a 500 from the
// endpoint leaves the run's exit code identical to the gate-disabled baseline (AC
// 06-03 Scenario 1).
func TestQualitySignalSend_500Response_RunOutcomeUnchanged(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "0")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	reconcileFixture(t)

	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	baseline := runReconcileSend(t, telemetry.New(qsEndpoint), "r")

	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	failingSeam(t, http.StatusInternalServerError, nil)
	got := runReconcileSend(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, baseline, got, "a 500 on the quality-signal send must not change the run outcome")
}

// TestQualitySignalSend_DNSFailure_RunOutcomeUnchanged proves a transport/network
// error leaves the run outcome identical to baseline (AC 06-03 Scenario 2).
func TestQualitySignalSend_DNSFailure_RunOutcomeUnchanged(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "0")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	reconcileFixture(t)

	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	baseline := runReconcileSend(t, telemetry.New(qsEndpoint), "r")

	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	failingSeam(t, 0, errors.New("dns: no such host"))
	got := runReconcileSend(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, baseline, got, "a DNS/connection failure must not change the run outcome")
}

// TestQualitySignalSend_TimeoutDoesNotBlockRunCompletion proves the run returns
// promptly even when the endpoint never responds — the send is detached and never
// gates run completion (AC 06-03 Scenario 3).
func TestQualitySignalSend_TimeoutDoesNotBlockRunCompletion(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	t.Setenv("ATCR_TELEMETRY", "0")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	reconcileFixture(t)

	release := make(chan struct{})
	restore := telemetry.SetDoRequestForTest(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		<-release // hang until the test releases it
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	defer restore()

	client := telemetry.New(qsEndpoint)
	logger, err := log.New("info", "text", io.Discard)
	require.NoError(t, err)
	cmd := newReconcileCmd()
	cmd.SetContext(telemetry.NewContext(log.NewContext(context.Background(), logger), client))
	cmd.SetOut(io.Discard)
	cmd.SetErr(new(bytes.Buffer))
	require.NoError(t, cmd.ParseFlags([]string{"r"}))

	start := time.Now()
	_ = runReconcile(cmd, cmd.Flags().Args())
	elapsed := time.Since(start)
	waitQualitySignalInFlight() // the detached build must have dispatched before client.Wait can observe the send
	close(release)
	client.Wait()

	assert.Less(t, elapsed, 2*time.Second, "run completion must not block on a hung send")
}

// TestQualitySignalSend_PanicInSendPathContained proves a panic injected into the
// send path is recovered and never propagates to the run (AC 06-03 EC1).
func TestQualitySignalSend_PanicInSendPathContained(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "0")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	reconcileFixture(t)

	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	baseline := runReconcileSend(t, telemetry.New(qsEndpoint), "r")

	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	restore := telemetry.SetDoRequestForTest(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		panic("forced quality-signal panic")
	})
	defer restore()
	got := runReconcileSend(t, telemetry.New(qsEndpoint), "r")
	assert.Equal(t, baseline, got, "a panic in the send path must be contained, leaving the run outcome unchanged")
}

// TestQualitySignalSend_FailureOnOneRunDoesNotAffectNext proves no circuit-breaker
// or retry state is carried across runs: a first failing send does not suppress or
// duplicate the second run's healthy send (AC 06-03 EC3).
func TestQualitySignalSend_FailureOnOneRunDoesNotAffectNext(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	t.Setenv("ATCR_TELEMETRY", "0")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	reconcileFixture(t)

	// First run: endpoint 500s.
	failingSeam(t, http.StatusInternalServerError, nil)
	runReconcileSend(t, telemetry.New(qsEndpoint), "r")

	// Second run: healthy endpoint records exactly one send.
	bodies := captureSendBodies(t)
	runReconcileGated(t, telemetry.New(qsEndpoint), "r")
	assert.Len(t, qualitySendBodies(*bodies), 1, "the second run must send exactly once — no carried failure state")
}

// TestQualitySignalSend_FailureDiagnosticsNeverIncludePayloadBody proves a failing
// send's debug diagnostics never log the payload body (AC 06-03 DoD): the persona
// hash and count fields must not appear in the log stream.
func TestQualitySignalSend_FailureDiagnosticsNeverIncludePayloadBody(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	t.Setenv("ATCR_TELEMETRY", "0")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	reconcileFixture(t)
	failingSeam(t, http.StatusInternalServerError, nil)

	var logbuf bytes.Buffer
	logger, err := log.New("debug", "text", &logbuf)
	require.NoError(t, err)
	client := telemetry.New(qsEndpoint)
	cmd := newReconcileCmd()
	cmd.SetContext(telemetry.NewContext(log.NewContext(context.Background(), logger), client))
	cmd.SetOut(io.Discard)
	cmd.SetErr(new(bytes.Buffer))
	require.NoError(t, cmd.ParseFlags([]string{"r"}))
	_ = runReconcile(cmd, cmd.Flags().Args())
	waitQualitySignalInFlight() // drain the detached build+send goroutine before the client (it registers only at dispatch)
	client.Wait()

	logs := logbuf.String()
	assert.NotContains(t, logs, "persona_id_hash", "failure diagnostics must not include the payload body")
	assert.NotContains(t, logs, "dismissed_count", "failure diagnostics must not include the payload body")
}
