package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
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

// TestNewRootCmd_SendsNothingByDefault is the embedder-safety contract.
//
// NewRootCmd is an exported seam a downstream module builds the same tree from,
// and on that path the telemetry had neither disclosure nor delivery: the
// first-run notice and drainTelemetry both live on the runMain lifecycle, so an
// embedder transmitted to a live host undisclosed, and its sends were stranded at
// exit anyway. The zero-argument constructor therefore now carries NO
// destinations; an embedder that wants telemetry opts in explicitly via
// NewRootCmdWithClient.
//
// The destinations are overridden to test hosts here specifically so the
// assertion cannot pass for the trivial reason that the compiled-in values were
// unreachable — if NewRootCmd still built a live client, these overridden URLs
// would be exactly what it aimed at, and the recorder would see them.
func TestNewRootCmd_SendsNothingByDefault(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	t.Setenv("ATCR_TELEMETRY", "1")      // consent granted...
	t.Setenv("ATCR_QUALITY_SIGNAL", "1") // ...on both surfaces
	withTelemetryEndpoints(t, "https://usage.test/ingest", "https://quality.test/ingest")
	initGitRepoWithChange(t)
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")

	_, hits := recordingTransport(t)

	root := NewRootCmd()
	root.SetArgs([]string{"review", "--base", "HEAD^", "--head", "HEAD"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	_ = root.ExecuteContext(context.Background())
	waitQualitySignalInFlight()

	assert.Empty(t, hits(),
		"the zero-argument NewRootCmd must transmit nothing even with both consent surfaces enabled: an embedder opts in via NewRootCmdWithClient")
}

// TestNewRootCmd_SuppressesTheDisclosure is the other half of the default-off
// contract: a tree that cannot transmit must not announce that it does. Telling an
// embedder's users "atcr sends an anonymous usage ping to ..." when the client has
// no destination would be a false disclosure — worse than none, because it is
// unfalsifiable from the outside.
func TestNewRootCmd_SuppressesTheDisclosure(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "1")

	var out, errBuf bytes.Buffer
	root := NewRootCmd()
	root.SetArgs([]string{"version"})
	root.SetOut(&out)
	root.SetErr(&errBuf)
	_ = root.ExecuteContext(context.Background())

	assert.NotContains(t, errBuf.String(), defaultTelemetryEndpoint,
		"a no-destination tree must not print a disclosure for transmission it cannot perform")
}

// TestProcessTelemetryClient_HonorsOverriddenEndpoints is the former
// TestNewRootCmd_HonorsOverriddenEndpoints, retargeted.
//
// The property it pins — that the compiled-in destinations are an OVERRIDABLE seam
// (`-ldflags -X`, or in-process assignment before construction) rather than sealed
// constants — is unchanged and still load-bearing for forks, enterprise builds,
// and air-gapped deployments. What changed is WHERE it must hold: NewRootCmd no
// longer builds a live client, so the seam now lives on newProcessTelemetryClient,
// the single shared constructor that runMain uses and that still reads the vars.
func TestProcessTelemetryClient_HonorsOverriddenEndpoints(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	t.Setenv("ATCR_TELEMETRY", "1")
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	withTelemetryEndpoints(t, "https://collector.internal/usage", "https://collector.internal/quality")
	initGitRepoWithChange(t)
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")

	_, hits := recordingTransport(t)

	client := newProcessTelemetryClient()
	root := NewRootCmdWithClient(client)
	root.SetArgs([]string{"review", "--base", "HEAD^", "--head", "HEAD"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	_ = root.ExecuteContext(context.Background())
	waitQualitySignalInFlight()
	client.Wait()

	var urls []string
	for _, h := range hits() {
		urls = append(urls, h.URL)
	}
	assert.Contains(t, urls, "https://collector.internal/usage",
		"the usage ping must follow the overridden destination, not the compiled-in default")
	assert.Contains(t, urls, "https://collector.internal/quality",
		"the quality signal must follow the overridden destination, not the compiled-in default")
	for _, u := range urls {
		assert.NotContains(t, u, "atcr.dev",
			"no send may reach the upstream host once the destinations are overridden")
	}
}

// TestRootCmd_DefaultOnPostureSendsWithEnvUnset covers the posture the changelog
// actually advertises, end to end.
//
// Every other test in this file sets ATCR_TELEMETRY=1, which exercises the
// EXPLICIT OPT-IN branch of telemetryEnabledFromEnv — not the default-on branch,
// where an unset or blank value returns true. That distinction is the whole point
// of activating the endpoints: users who never set the variable are the population
// that transmits. Because the package TestMain pins ATCR_TELEMETRY=0 for
// hermeticity, no end-to-end test could reach the unset state, so the advertised
// default was covered only at the pure-function level in telemetry_gate_test.go —
// a real gap between "the gate function returns true" and "a run with no env var
// actually sends".
//
// unsetEnvForTest is the established way to reach a genuinely-unset variable here:
// it restores the pin on cleanup (and panics under t.Parallel), so relaxing it for
// one test introduces no new risk. The destinations are retargeted at unroutable
// .test hosts and the transport is stubbed, so exercising default-ON cannot reach
// the production host.
func TestRootCmd_DefaultOnPostureSendsWithEnvUnset(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	unsetEnvForTest(t, "ATCR_TELEMETRY") // the DEFAULT-ON branch: no value at all
	withTelemetryEndpoints(t, "https://usage.test/ingest", "https://quality.test/ingest")
	initGitRepoWithChange(t)
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)

	_, hits := recordingTransport(t)

	client := telemetry.NewWithQualitySignal(defaultTelemetryEndpoint, defaultQualitySignalEndpoint)
	root := NewRootCmdWithClient(client)
	root.SetArgs([]string{"review", "--base", "HEAD^", "--head", "HEAD"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	_ = root.ExecuteContext(context.Background())
	client.Wait()

	var pings int
	for _, h := range hits() {
		if h.URL == defaultTelemetryEndpoint && !strings.HasPrefix(strings.TrimSpace(h.Body), "[") {
			pings++
		}
	}
	assert.Equal(t, 1, pings,
		"a run with ATCR_TELEMETRY genuinely unset must send the usage ping — that is the default-on posture the endpoints were activated for")
}

// TestRootCmd_DefaultOnPostureIsStillRevocable is the other half: default-on must
// remain an OPT-OUT, not a mandate. Same unset-env starting point, but with the
// persisted config opt-out in place, nothing may leave.
func TestRootCmd_DefaultOnPostureIsStillRevocable(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	unsetEnvForTest(t, "ATCR_TELEMETRY")
	withTelemetryEndpoints(t, "https://usage.test/ingest", "https://quality.test/ingest")
	initGitRepoWithChange(t)
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)
	require.NoError(t, registry.SetTelemetrySetting(".", false))

	_, hits := recordingTransport(t)

	client := telemetry.NewWithQualitySignal(defaultTelemetryEndpoint, defaultQualitySignalEndpoint)
	root := NewRootCmdWithClient(client)
	root.SetArgs([]string{"review", "--base", "HEAD^", "--head", "HEAD"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	_ = root.ExecuteContext(context.Background())
	client.Wait()

	for _, h := range hits() {
		assert.NotEqual(t, defaultTelemetryEndpoint, h.URL,
			"a persisted telemetry: false must suppress the ping even from the default-on starting state")
	}
}

// TestRunMain_RoutesPingAndQualitySignalToDistinctEndpoints covers the OTHER
// entry point.
//
// cli/main.go has two paths that stand up the command tree: NewRootCmd (the
// embedder seam, covered below) and runMain (what every real atcr invocation
// reaches via Main/MainWithHooks). Only the first was ever tested, so a revert of
// runMain's client to a single destination would route the quality-signal array at
// the usage-ping handler for a 400 the fail-open path drops — the exact regression
// the sibling test names, undetected on the path that actually ships.
//
// Both now build through newProcessTelemetryClient, so this is belt-and-braces
// with that collapse: the structural fix removes the second place to get it wrong,
// and this test proves the shipping path is the one that got fixed. runMain owns
// its own drainTelemetry, so no test-side wait is needed.
func TestRunMain_RoutesPingAndQualitySignalToDistinctEndpoints(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	t.Setenv("ATCR_TELEMETRY", "1")
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	withTelemetryEndpoints(t, "https://usage.test/ingest", "https://quality.test/ingest")
	initGitRepoWithChange(t)
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")

	_, hits := recordingTransport(t)

	// runMain takes its args from os.Args (it calls ExecuteContext without SetArgs).
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"atcr", "review", "--base", "HEAD^", "--head", "HEAD"}

	_ = runMain(context.Background(), io.Discard, io.Discard)
	waitQualitySignalInFlight() // the detached build+send goroutine may register after runMain's drain

	var sawPing, sawQuality int
	for _, h := range hits() {
		if strings.HasPrefix(strings.TrimSpace(h.Body), "[") {
			if h.URL == defaultQualitySignalEndpoint {
				sawQuality++
			}
			assert.NotEqual(t, defaultTelemetryEndpoint, h.URL,
				"the quality signal must never POST to the usage-ping handler on the runMain path")
			continue
		}
		if h.URL == defaultTelemetryEndpoint {
			sawPing++
		}
		assert.NotEqual(t, defaultQualitySignalEndpoint, h.URL,
			"the usage ping must never POST to the quality-signal handler on the runMain path")
	}
	assert.Equal(t, 1, sawPing, "runMain must send exactly one usage ping to the usage-ping endpoint")
	assert.Equal(t, 1, sawQuality, "runMain must send exactly one quality signal to the quality-signal endpoint")
}

// TestRootCmd_RoutesPingAndQualitySignalToDistinctEndpoints is AC1b at the
// production wiring, not just at the Client: it drives the REAL command tree built
// by NewRootCmd — which constructs the process telemetry client itself — and proves
// the usage ping and the quality signal leave for different URLs. Its sibling above
// covers the runMain entry point; this one covers the embedder seam.
func TestRootCmd_RoutesPingAndQualitySignalToDistinctEndpoints(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	t.Setenv("ATCR_TELEMETRY", "1")      // usage ping on via EXPLICIT opt-in (not the default-on branch — see TestRootCmd_DefaultOnPostureSendsWithEnvUnset)
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
