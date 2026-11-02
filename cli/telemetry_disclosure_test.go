package cli

import (
	"bytes"
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

// execTransmitting drives the command tree the way the shipped binary does: with
// a LIVE process telemetry client, the same one runMain builds.
//
// The disclosure is deliberately keyed on the client the process actually holds,
// so it is silent under the zero-argument NewRootCmd (which now carries no
// destinations — an embedder must opt in). Testing the notice therefore has to use
// the transmitting construction, or it would be asserting against a tree that has
// nothing to disclose. This helper is that construction, and using it keeps these
// tests aimed at the real user-facing path rather than at whichever constructor
// happens to be convenient.
func execTransmitting(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmdWithClient(newProcessTelemetryClient(""))
	root.SetArgs(args)
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	err := root.ExecuteContext(context.Background())
	return exitCode(err), outBuf.String(), errBuf.String()
}

// defaultNoticeMarker is what the first-run notice prints for the compiled-in
// destination. The notice shows scheme and host only, never the path, so the
// full defaultTelemetryEndpoint never appears on stderr; asserting its absence
// would pass whether or not the notice fired.
const defaultNoticeMarker = "anonymous usage ping to https://atcr.dev when"

// TestTelemetryDisclosure_PrintsOnceThenPersists is the core contract: the first
// command in a repo with telemetry enabled prints the notice on stderr, and the
// second prints nothing because the first persisted the fact.
func TestTelemetryDisclosure_PrintsOnceThenPersists(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "1")

	_, _, errOut := execTransmitting(t, "version")
	assert.Contains(t, errOut, defaultNoticeMarker,
		"the first run must name the destination it transmits to")
	assert.Contains(t, errOut, "ATCR_TELEMETRY=0",
		"the notice must name the opt-out, not merely announce the collection")

	_, _, errOut2 := execTransmitting(t, "version")
	assert.NotContains(t, errOut2, defaultNoticeMarker,
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

	_, _, errOut := execTransmitting(t, "version")
	require.Contains(t, errOut, defaultNoticeMarker, "first run must disclose")

	data, err := os.ReadFile(cfg)
	require.NoError(t, err, "the disclosure must create .atcr/config.yaml, not skip persisting")
	assert.Contains(t, string(data), "telemetry_notice_shown")

	_, _, errOut2 := execTransmitting(t, "version")
	assert.NotContains(t, errOut2, defaultNoticeMarker,
		"a created config must actually suppress the second notice")
}

// TestTelemetryDisclosure_SilentWhenOptedOut proves the notice is scoped to users
// who are actually transmitting. Someone who has already opted out has nothing to
// be disclosed to, and nagging them would be the wrong end of the consent model.
func TestTelemetryDisclosure_SilentWhenOptedOut(t *testing.T) {
	t.Run("env opt-out", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "0")
		_, _, errOut := execTransmitting(t, "version")
		assert.NotContains(t, errOut, defaultNoticeMarker)
	})
	t.Run("config opt-out", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "1")
		writeAtcrConfig(t, "agents: [bruce]\ntelemetry: false\n")
		_, _, errOut := execTransmitting(t, "version")
		assert.NotContains(t, errOut, defaultNoticeMarker)
	})
}

// TestTelemetryDisclosure_PreservesSiblingConfigKeys proves the persistence write
// mutates only its own key: a repo that already carries a roster and other
// settings must not have them dropped by the notice bookkeeping.
func TestTelemetryDisclosure_PreservesSiblingConfigKeys(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "1")
	writeAtcrConfig(t, "agents: [bruce]\npayload_mode: blocks\n")

	_, _, errOut := execTransmitting(t, "version")
	require.Contains(t, errOut, defaultNoticeMarker)

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
	_, _, errOut := execTransmitting(t, "review", "--base", "HEAD^", "--head", "HEAD")
	require.Contains(t, errOut, "anonymous usage ping to https://telemetry.test when", "precondition: the notice fired and wrote the key")

	data, err := os.ReadFile(filepath.Join(".atcr", "config.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(data), "telemetry_notice_shown", "precondition: the key is on disk")

	// Second run: the persisted key must parse cleanly through the strict roster load.
	_, _, errOut2 := execTransmitting(t, "review", "--base", "HEAD^", "--head", "HEAD")
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

// execTransmittingWithInput is execTransmitting with stdin wired, for the
// interactive scaffolding commands (quickstart prompts for a provider key).
func execTransmittingWithInput(t *testing.T, in string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := NewRootCmdWithClient(newProcessTelemetryClient(""))
	root.SetArgs(args)
	root.SetIn(strings.NewReader(in))
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	err := root.ExecuteContext(context.Background())
	return exitCode(err), outBuf.String(), errBuf.String()
}

// TestTelemetryDisclosure_FreshRepoQuickstartStillScaffolds is Epic 84.0 review
// finding F1. The notice used to persist its bookkeeping by creating a stub
// .atcr/config.yaml in the root PersistentPreRunE — before quickstart looked for
// an existing workspace. On a fresh repo, plain `atcr quickstart` (the README's
// headline step) then said "Using existing workspace", wrote no personas or
// roster, and `atcr review` stopped with "no agents selected".
func TestTelemetryDisclosure_FreshRepoQuickstartStillScaffolds(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	t.Setenv("ATCR_TELEMETRY", "1")

	code, _, errOut := execTransmittingWithInput(t, quickstartInput, "quickstart", "--offline")
	require.Equal(t, 0, code, "quickstart on a fresh repo must succeed; stderr:\n%s", errOut)
	assert.Contains(t, errOut, defaultNoticeMarker, "the first run must still disclose")
	assert.NotContains(t, errOut, "Using existing workspace",
		"the notice's bookkeeping must not masquerade as an existing workspace")

	assert.FileExists(t, filepath.Join(".atcr", "personas", "bruce.md"), "quickstart must scaffold the personas")
	cfg, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath("."))
	require.NoError(t, err, "the scaffolded config must carry a roster the strict loader accepts")
	assert.NotEmpty(t, cfg.Agents, "the scaffolded config must list the roster")

	data, err := os.ReadFile(filepath.Join(".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "telemetry_notice_shown",
		"the notice must still be recorded once scaffolding has run")
	_, _, errOut2 := execTransmitting(t, "version")
	assert.NotContains(t, errOut2, defaultNoticeMarker, "the notice stays one-time per repo")
}

// TestTelemetryDisclosure_FreshRepoInitStillScaffolds is F1 for `atcr init`,
// whose all-or-nothing writers refused to overwrite the notice's stub config.
func TestTelemetryDisclosure_FreshRepoInitStillScaffolds(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	t.Setenv("ATCR_TELEMETRY", "1")

	code, _, errOut := execTransmitting(t, "init")
	require.Equal(t, 0, code, "init on a fresh repo must succeed; stderr:\n%s", errOut)
	assert.Contains(t, errOut, defaultNoticeMarker, "the first run must still disclose")
	assert.FileExists(t, filepath.Join(".atcr", "personas", "bruce.md"))
	_, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath("."))
	require.NoError(t, err)

	_, _, errOut2 := execTransmitting(t, "version")
	assert.NotContains(t, errOut2, defaultNoticeMarker, "the notice stays one-time per repo")
}

// TestTelemetryDisclosure_StubFromEarlierCommandStillScaffolds is F1 on the path
// the clean-install run took: `atcr version` (or any command) runs first and
// records the notice in a stub config, then quickstart / init must still
// scaffold rather than take the stub for a returning user's workspace.
func TestTelemetryDisclosure_StubFromEarlierCommandStillScaffolds(t *testing.T) {
	for _, args := range [][]string{{"quickstart", "--offline"}, {"init"}} {
		t.Run(args[0], func(t *testing.T) {
			isolate(t)
			initGitRepo(t)
			t.Setenv("ATCR_TELEMETRY", "1")

			_, _, errOut := execTransmitting(t, "version")
			require.Contains(t, errOut, defaultNoticeMarker)
			_, err := os.Stat(filepath.Join(".atcr", "config.yaml"))
			require.NoError(t, err, "precondition: the notice left a stub config")

			code, _, errOut := execTransmittingWithInput(t, quickstartInput, args...)
			require.Equal(t, 0, code, "stderr:\n%s", errOut)
			assert.NotContains(t, errOut, "Using existing workspace")
			assert.NotContains(t, errOut, defaultNoticeMarker, "the stub's record of the notice must survive")
			assert.FileExists(t, filepath.Join(".atcr", "personas", "bruce.md"))
			cfg, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath("."))
			require.NoError(t, err)
			assert.NotEmpty(t, cfg.Agents)
			require.NotNil(t, cfg.TelemetryNoticeShown)
			assert.True(t, *cfg.TelemetryNoticeShown)
		})
	}
}

// TestTelemetryDisclosure_StubOptOutSurvivesScaffolding: a stub can also hold an
// opt-out (`atcr config set` on it). Replacing the stub must never re-enable
// telemetry or the quality signal.
func TestTelemetryDisclosure_StubOptOutSurvivesScaffolding(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	writeAtcrConfig(t, "telemetry_notice_shown: true\ntelemetry: false\nquality_signal: false\n")

	code, _, errOut := execTransmittingWithInput(t, quickstartInput, "quickstart", "--offline")
	require.Equal(t, 0, code, "stderr:\n%s", errOut)
	assert.FileExists(t, filepath.Join(".atcr", "personas", "bruce.md"))
	tel, err := registry.LoadTelemetrySetting(".")
	require.NoError(t, err)
	require.NotNil(t, tel)
	assert.False(t, *tel, "the recorded telemetry opt-out must survive")
	qs, err := registry.LoadQualitySignalSetting(".")
	require.NoError(t, err)
	require.NotNil(t, qs)
	assert.False(t, *qs, "the recorded quality-signal opt-out must survive")
}

// TestQuickstart_RealConfigWithoutPersonasIsStillExisting guards the other side:
// a config carrying anything beyond the stub keys is a real workspace, and
// quickstart keeps layering onto it rather than replacing it.
func TestQuickstart_RealConfigWithoutPersonasIsStillExisting(t *testing.T) {
	isolate(t)
	initGitRepo(t)
	writeAtcrConfig(t, "agents: [bruce]\ntelemetry_notice_shown: true\n")

	code, _, errOut := execTransmittingWithInput(t, quickstartInput, "quickstart", "--offline")
	require.Equal(t, 0, code, "stderr:\n%s", errOut)
	assert.Contains(t, errOut, "Using existing workspace")
	data, err := os.ReadFile(filepath.Join(".atcr", "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "agents: [bruce]\ntelemetry_notice_shown: true\n", string(data), "a real config is never rewritten")
}

// --- Epic 104 T1: the notice under `review --config-dir` ---------------------
//
// `review --config-dir` loads the operator's project config instead of the
// reviewed tree's, so an untrusted PR head cannot steer the run. The one-time
// notice must follow the same root: the opt-out and the marker are read from the
// config dir, and the marker is written there, never into the reviewed tree.

// configDirWith makes a --config-dir root holding content as its
// .atcr/config.yaml; empty content leaves the directory without a config.
func configDirWith(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if content != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".atcr"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".atcr", "config.yaml"), []byte(content), 0o644))
	}
	return dir
}

// TestTelemetryDisclosure_ConfigDirOptOutSilences: the operator's opt-out in the
// config dir silences the notice even though the reviewed tree carries none, and
// the reviewed tree gets no marker written into it.
func TestTelemetryDisclosure_ConfigDirOptOutSilences(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "1")
	dir := configDirWith(t, "telemetry: false\n")

	_, _, errOut := execTransmitting(t, "review", "--config-dir", dir)
	assert.NotContains(t, errOut, defaultNoticeMarker,
		"the config dir's opt-out must govern the notice under --config-dir")
	assert.NoFileExists(t, filepath.Join(".atcr", "config.yaml"),
		"the reviewed tree must not be written to under --config-dir")
}

// TestTelemetryDisclosure_ConfigDirMarkerIsReadAndWrittenThere: the notice fires
// once, records its marker in the config dir rather than the reviewed tree, and a
// marker already in the config dir keeps a fresh reviewed tree quiet.
func TestTelemetryDisclosure_ConfigDirMarkerIsReadAndWrittenThere(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TELEMETRY", "1")
	dir := configDirWith(t, "agents: [bruce]\n")

	_, _, errOut := execTransmitting(t, "review", "--config-dir", dir)
	require.Contains(t, errOut, defaultNoticeMarker, "first run under --config-dir must disclose")
	assert.NoFileExists(t, filepath.Join(".atcr", "config.yaml"),
		"the marker must not land in the reviewed tree")
	shown, err := registry.LoadTelemetryNoticeShown(dir)
	require.NoError(t, err)
	require.NotNil(t, shown, "the marker must be written to the config dir")
	assert.True(t, *shown)

	t.Chdir(t.TempDir()) // a different reviewed tree, same operator config
	_, _, errOut2 := execTransmitting(t, "review", "--config-dir", dir)
	assert.NotContains(t, errOut2, defaultNoticeMarker,
		"the config dir's marker must keep the notice one-time")
}

// TestTelemetryDisclosure_InvalidConfigDirSkipsNotice: an invalid --config-dir is
// a usage error the review run reports itself, so the notice is skipped rather
// than falling back to the reviewed tree's config. A directory with no
// .atcr/config.yaml is invalid too, and the notice must not create one there: a
// marker-only config would turn the next run's "config.yaml not found" into a
// later, vaguer failure.
func TestTelemetryDisclosure_InvalidConfigDirSkipsNotice(t *testing.T) {
	t.Run("not a directory", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "1")

		code, _, errOut := execTransmitting(t, "review", "--config-dir", filepath.Join(t.TempDir(), "missing"))
		assert.Equal(t, 2, code, "stderr:\n%s", errOut)
		assert.NotContains(t, errOut, defaultNoticeMarker,
			"an invalid --config-dir must skip the notice")
		assert.NoFileExists(t, filepath.Join(".atcr", "config.yaml"),
			"an invalid --config-dir must not fall back to writing the reviewed tree")
	})
	t.Run("no config.yaml", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_TELEMETRY", "1")
		dir := configDirWith(t, "")

		_, _, errOut := execTransmitting(t, "review", "--config-dir", dir)
		assert.NotContains(t, errOut, defaultNoticeMarker,
			"a --config-dir with no .atcr/config.yaml must skip the notice")
		assert.NoFileExists(t, filepath.Join(dir, ".atcr", "config.yaml"),
			"the notice must not create a config in the operator's config dir")
		assert.NoFileExists(t, filepath.Join(".atcr", "config.yaml"),
			"nor fall back to writing the reviewed tree")
	})
}
