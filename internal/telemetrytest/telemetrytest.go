// Package telemetrytest provides the shared test-suite hermeticity guard for
// atcr's telemetry surfaces: it pins the consent env vars to their off state and
// intercepts any outbound send aimed at a non-test host, so no test in any package
// can transmit at the live atcr.dev endpoints.
//
// It is an ORDINARY (non-test) package on purpose. The guard originally lived in
// package cli's github_test.go, and Go's build rules make symbols declared in a
// _test.go file structurally unimportable from another package — so cmd/atcr,
// which calls cli.Main from a different package, could not reuse it and ran with
// no guard at all. Today cmd/atcr's only test drives the version subcommand, which
// reaches neither telemetry Send call site, so this closes no active leak; it
// removes the standing invitation for the next test added there to open one.
//
// Nothing in the shipped binary imports this package — only _test.go files do — so
// its dependency on the testing package is never linked into cmd/atcr.
package telemetrytest

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/samestrin/atcr/internal/telemetry"
)

// escapeLog records the URLs the transport guard intercepts. Sends are
// fire-and-forget goroutines, so all access is mutex-guarded.
type escapeLog struct {
	mu      sync.Mutex
	escapes []string
}

func (l *escapeLog) record(url string) {
	l.mu.Lock()
	l.escapes = append(l.escapes, url)
	l.mu.Unlock()
}

func (l *escapeLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.escapes...)
}

func (l *escapeLog) reset() {
	l.mu.Lock()
	l.escapes = nil
	l.mu.Unlock()
}

// guard is the process-wide escape log for GuardHosts.
var guard escapeLog

// Escapes returns the URLs intercepted so far. Exported for the guard's own
// assertion tests.
func Escapes() []string { return guard.snapshot() }

// ResetEscapes clears the escape log. Exported for the guard's own assertion
// tests, which must not leave their deliberate escape behind for Run to report.
func ResetEscapes() { guard.reset() }

// IsAllowlistedTestHost classifies telemetry destinations the structural guard
// lets through: loopback (httptest servers) and the .test TLD, which RFC 2606
// reserves for documentation and testing so it can never resolve to a real host —
// the repo's blackhole endpoints (telemetry.test) are therefore structurally
// incapable of reaching a live destination, unlike a typo'd or future production
// URL, which the guard exists to catch.
func IsAllowlistedTestHost(host string) bool {
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return strings.HasSuffix(host, ".test")
}

// GuardHosts is the transport seam Run installs: allowlisted test hosts (see
// IsAllowlistedTestHost) pass through to real networking untouched; anything
// else — above all the live atcr.dev endpoints the compiled-in production client
// carries — is intercepted, recorded, and answered with a synthetic 202 so the
// fail-open send path completes quietly and Run's escape report is the single
// loud signal.
func GuardHosts(client *http.Client, req *http.Request) (*http.Response, error) {
	if IsAllowlistedTestHost(req.URL.Hostname()) {
		return client.Do(req)
	}
	guard.record(req.URL.String())
	return &http.Response{
		StatusCode: http.StatusAccepted,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

// PinConsentEnv forces every telemetry consent surface to its off state for the
// whole test binary.
//
// The pin is deliberately UNCONDITIONAL: honouring an ATCR_TELEMETRY or
// ATCR_QUALITY_SIGNAL already exported by a developer shell or a CI job would
// re-open exactly the hole this closes. The usage ping is default-ON (an unset
// ATCR_TELEMETRY means enabled) and the quality signal is opt-IN via its own var,
// so without this any test driving a review or reconcile to completion through the
// real command tree would transmit for real. ATCR_API_KEY is neutralized too —
// test isolation helpers do not clear it, and the --sync-cloud precondition tests
// depend on its absence.
//
// Tests needing a surface enabled opt in per-test via t.Setenv (auto-restored), so
// overriding the ambient value here costs them nothing.
func PinConsentEnv() {
	_ = os.Setenv("ATCR_TELEMETRY", "0")
	_ = os.Setenv("ATCR_QUALITY_SIGNAL", "0")
	_ = os.Setenv("ATCR_API_KEY", "")
}

// Run is the TestMain body every package whose tests can reach a telemetry Send
// call site should delegate to: it pins the consent env vars, installs the
// transport guard for the whole run (never restored — per-test
// SetDoRequestForTest overrides sit above it and their restore puts it back, so
// the guard only ever sees traffic no test claimed), runs the suite, and returns
// a non-zero code naming every escape.
//
// The env pin alone is an ENVIRONMENTAL guard; the transport seam is the
// STRUCTURAL half, catching a send from a future test that constructs the
// production client, a targeted `go test -run`, or a straggler goroutine that
// outlived a per-test seam.
//
// Callers own os.Exit so they can do package-specific setup first:
//
//	func TestMain(m *testing.M) { os.Exit(telemetrytest.Run(m)) }
func Run(m *testing.M) int {
	PinConsentEnv()
	telemetry.SetDoRequestForTest(GuardHosts) // installed for the whole run; never restored
	code := m.Run()
	if escapes := guard.snapshot(); len(escapes) > 0 {
		fmt.Fprintf(os.Stderr, "\ntelemetry transport guard: %d send(s) escaped the test-host allowlist — a test reached a non-test telemetry host:\n", len(escapes))
		for _, u := range escapes {
			fmt.Fprintf(os.Stderr, "  - %s\n", u)
		}
		return 1
	}
	return code
}
