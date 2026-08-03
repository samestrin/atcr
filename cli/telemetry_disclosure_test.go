package cli

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/telemetry"
)

// --- Epic 35.12 TD: first-run telemetry disclosure ---------------------------
//
// The usage ping is default-ON against a live endpoint, and until now the binary
// disclosed that nowhere at runtime: the only mentions were a comment in a
// generated config file and docs/telemetry.md. An existing user upgrading started
// transmitting without ever being told. These tests pin the disclosure contract:
// it fires once per repo, synchronously, before any run — never from the async
// send path, where nothing could observe or order it.

// TestTelemetryDisclosure_PrintsOnceThenPersists is the core contract: the first
// command in a repo with telemetry enabled prints the notice on stderr, and the
// second prints nothing because the first persisted the fact.
func TestTelemetryDisclosure_PrintsOnceThenPersists(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "1")

	_, _, errOut := execCmdSplit(t, "version")
	assert.Contains(t, errOut, defaultTelemetryEndpoint,
		"the first run must name the destination it transmits to")
	assert.Contains(t, errOut, "ATCR_TELEMETRY=0",
		"the notice must name the opt-out, not merely announce the collection")

	_, _, errOut2 := execCmdSplit(t, "version")
	assert.NotContains(t, errOut2, defaultTelemetryEndpoint,
		"the notice is one-time per repo: a second run must stay quiet")
}

// TestTelemetryDisclosure_CreatesConfigWhenAbsent covers the gap the answer flagged:
// a repo with NO .atcr/config.yaml is exactly where the ping already fires today,
// so the persistence write must CREATE the file. If it errored or silently no-oped,
// the notice would re-fire on every single invocation forever.
func TestTelemetryDisclosure_CreatesConfigWhenAbsent(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "1")

	cfg := filepath.Join(".atcr", "config.yaml")
	_, statErr := os.Stat(cfg)
	require.True(t, os.IsNotExist(statErr), "precondition: this repo has no config file")

	_, _, errOut := execCmdSplit(t, "version")
	require.Contains(t, errOut, defaultTelemetryEndpoint, "first run must disclose")

	data, err := os.ReadFile(cfg)
	require.NoError(t, err, "the disclosure must create .atcr/config.yaml, not skip persisting")
	assert.Contains(t, string(data), "telemetry_notice_shown")

	_, _, errOut2 := execCmdSplit(t, "version")
	assert.NotContains(t, errOut2, defaultTelemetryEndpoint,
		"a created config must actually suppress the second notice")
}

// TestTelemetryDisclosure_SilentWhenOptedOut proves the notice is scoped to users
// who are actually transmitting. Someone who has already opted out has nothing to
// be disclosed to, and nagging them would be the wrong end of the consent model.
func TestTelemetryDisclosure_SilentWhenOptedOut(t *testing.T) {
	t.Run("env opt-out", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "0")
		_, _, errOut := execCmdSplit(t, "version")
		assert.NotContains(t, errOut, defaultTelemetryEndpoint)
	})
	t.Run("config opt-out", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "1")
		writeAtcrConfig(t, "agents: [bruce]\ntelemetry: false\n")
		_, _, errOut := execCmdSplit(t, "version")
		assert.NotContains(t, errOut, defaultTelemetryEndpoint)
	})
}

// TestTelemetryDisclosure_PreservesSiblingConfigKeys proves the persistence write
// mutates only its own key: a repo that already carries a roster and other
// settings must not have them dropped by the notice bookkeeping.
func TestTelemetryDisclosure_PreservesSiblingConfigKeys(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "1")
	writeAtcrConfig(t, "agents: [bruce]\npayload_mode: blocks\n")

	_, _, errOut := execCmdSplit(t, "version")
	require.Contains(t, errOut, defaultTelemetryEndpoint)

	data, err := os.ReadFile(filepath.Join(".atcr", "config.yaml"))
	require.NoError(t, err)
	src := string(data)
	assert.Contains(t, src, "agents:", "the roster must survive the notice write")
	assert.Contains(t, src, "payload_mode: blocks", "sibling settings must survive the notice write")
	assert.Contains(t, src, "telemetry_notice_shown")
}

// TestSetTelemetryNoticeShown_CreatesConfig pins the registry primitive directly:
// unlike SetTelemetrySetting (whose missing-file error is the documented contract
// for `atcr config set`), the notice writer must create the file, because its
// caller is a background bookkeeping write in repos that by definition have no
// config yet.
func TestSetTelemetryNoticeShown_CreatesConfig(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, registry.SetTelemetryNoticeShown(root, true))

	got, err := registry.LoadTelemetryNoticeShown(root)
	require.NoError(t, err)
	require.NotNil(t, got, "the persisted key must read back")
	assert.True(t, *got)

	data, err := os.ReadFile(filepath.Join(root, ".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "telemetry_notice_shown"))
}

// TestSetTelemetrySetting_StillRequiresExistingConfig locks the contract that did
// NOT change: `atcr config set telemetry` on a repo with no config is an error, not
// a silent file creation. Only the notice writer creates.
func TestSetTelemetrySetting_StillRequiresExistingConfig(t *testing.T) {
	root := t.TempDir()
	err := registry.SetTelemetrySetting(root, false)
	require.Error(t, err, "config set must not silently create a config file")
}

// TestTelemetryDisclosure_DoesNotBreakStrictRosterLoad is the regression guard for
// the sharpest failure mode this feature can have. The roster load
// (registry.LoadProjectConfig) is STRICT — yaml KnownFields — so any key written
// into .atcr/config.yaml that is not declared on ProjectConfig makes every
// subsequent review fail outright with "field ... not found in type
// registry.ProjectConfig".
//
// That turns a privacy improvement into a hard outage, and only for users who
// were disclosed to: the first run writes the key, and every run after it is
// broken. A review must still succeed in a repo the notice has already touched.
func TestTelemetryDisclosure_DoesNotBreakStrictRosterLoad(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "1")
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	// This test drives a REAL review with the ping enabled, so it must retarget
	// the destinations: left at the compiled-in defaults the run posts at the live
	// host, which TestMain's transport guard reports as an escape.
	withTelemetryEndpoints(t, "https://telemetry.test/usage", "https://telemetry.test/quality")
	initGitRepoWithChange(t)
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)

	// First run: emits the notice and persists the key.
	_, _, errOut := execCmdSplit(t, "review", "--base", "HEAD^", "--head", "HEAD")
	require.Contains(t, errOut, defaultTelemetryEndpoint, "precondition: the notice fired and wrote the key")

	data, err := os.ReadFile(filepath.Join(".atcr", "config.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "telemetry_notice_shown", "precondition: the key is on disk")

	// Second run: the persisted key must parse cleanly through the strict roster load.
	_, _, errOut2 := execCmdSplit(t, "review", "--base", "HEAD^", "--head", "HEAD")
	assert.NotContains(t, errOut2, "failed to parse config.yaml",
		"the disclosure key must be a declared ProjectConfig field, or it breaks every later run")
	assert.NotContains(t, errOut2, "not found in type registry.ProjectConfig")
}

// TestDrainTelemetry_NoSendOutlivesTheDrain is the cross-package verification the
// timeout-coherence fix calls for: with the transport stubbed to sleep LONGER than
// the drain bound, a dispatched send must either land or never start — it must not
// sit half-finished past the drain, which is what a per-request budget exceeding
// telemetryDrainTimeout produced (a send slower than the drain was guaranteed to be
// abandoned after the user had already paid the full drain wait).
//
// The assertion is on the invariant rather than the constants: drainTelemetry must
// return within its own bound, and by the time it does, the client's own request
// budget must already have elapsed too — so no goroutine is left believing it may
// still succeed.
func TestDrainTelemetry_NoSendOutlivesTheDrain(t *testing.T) {
	var started, finished int32
	restore := telemetry.SetDoRequestForTest(func(_ *http.Client, req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&started, 1)
		// Sleep past the drain bound; the request context must cancel us first.
		select {
		case <-req.Context().Done():
			atomic.AddInt32(&finished, 1)
			return nil, req.Context().Err()
		case <-time.After(10 * time.Second):
			atomic.AddInt32(&finished, 1)
			return nil, errors.New("request outlived both budgets")
		}
	})
	t.Cleanup(restore)

	client := telemetry.NewSingleDestination("https://telemetry.test/ingest")
	client.Send(context.Background(), telemetry.Event{Event: "drain_probe"})

	start := time.Now()
	drainTelemetry(client, telemetryDrainTimeout)
	elapsed := time.Since(start)

	require.Equal(t, int32(1), atomic.LoadInt32(&started), "precondition: the send reached the transport")
	assert.LessOrEqual(t, elapsed, telemetryDrainTimeout+500*time.Millisecond,
		"drainTelemetry must return within its own bound")

	// The send's own budget must not outlive the drain: once the drain has given
	// up, the request must already be cancelled rather than still running.
	assert.Eventually(t, func() bool { return atomic.LoadInt32(&finished) == 1 },
		time.Second, 10*time.Millisecond,
		"the per-request budget must expire no later than the drain bound, or a send is abandoned mid-flight")
}
