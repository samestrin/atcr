package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/telemetry"
)

// TestQualitySignalEnabled_SixCellMatrix locks the opt-IN OR-enables truth table
// (Story 2, AC 02-02): the quality signal is enabled when EITHER surface has
// explicitly opted in, disabled only when neither has. This is the exact inverse
// of telemetryEnabled's opt-OUT AND-disables shape and must not be copied from it.
// A nil config field is neutral and can never out-rank a permitting env var.
func TestQualitySignalEnabled_SixCellMatrix(t *testing.T) {
	cases := []struct {
		name       string
		envEnabled bool
		cfg        *bool
		want       bool
	}{
		{"env disabled, config nil -> disabled (the default)", false, nil, false},
		{"env disabled, config true -> enabled (config alone is sufficient)", false, boolPtr(true), true},
		{"env disabled, config false -> disabled", false, boolPtr(false), false},
		{"env enabled, config nil -> enabled (env alone is sufficient)", true, nil, true},
		{"env enabled, config true -> enabled", true, boolPtr(true), true},
		{"env enabled, config false -> enabled (env opt-in never revoked by stale config false)", true, boolPtr(false), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, qualitySignalEnabled(tc.envEnabled, tc.cfg))
		})
	}
}

// TestQualitySignalEnabledFromEnv covers ATCR_QUALITY_SIGNAL parsing: unset/blank
// and unparseable values fail SAFE to disabled (the privacy-preserving default —
// the inverse of ATCR_TELEMETRY's fail-open posture); the strconv.ParseBool truthy
// set enables.
func TestQualitySignalEnabledFromEnv(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
		want bool
	}{
		{"unset defaults disabled", false, "", false},
		{"blank defaults disabled", true, "", false},
		{"whitespace defaults disabled", true, "   ", false},
		{"one enables", true, "1", true},
		{"true enables", true, "true", true},
		{"True enables", true, "True", true},
		{"TRUE enables", true, "TRUE", true},
		{"zero disables", true, "0", false},
		{"false disables", true, "false", false},
		{"unparseable fails safe to disabled", true, "maybe", false},
		{"enabled-word fails safe to disabled", true, "enabled", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("ATCR_QUALITY_SIGNAL", tc.val)
			} else {
				_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
			}
			assert.Equal(t, tc.want, qualitySignalEnabledFromEnv())
		})
	}
}

// TestQualitySignalGate_DisabledWithNoEnvNoConfig is the epic's AC1 floor
// ("nothing sent by default") for the quality-signal payload: with no env var and
// no persisted config key, the gate resolves disabled.
func TestQualitySignalGate_DisabledWithNoEnvNoConfig(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	assert.False(t, qualitySignalGate(), "quality signal must be disabled with no env var and no config")
}

// TestQualitySignalGate_DisabledWithUnrelatedConfigKeysOnly proves an absent
// quality_signal key is neutral, not an implicit opt-in — a config carrying only
// unrelated keys (and even telemetry: true) still resolves the gate disabled.
func TestQualitySignalGate_DisabledWithUnrelatedConfigKeysOnly(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	writeAtcrConfig(t, "agents: [bruce]\ntelemetry: true\npayload_mode: blocks\n")
	assert.False(t, qualitySignalGate(), "an absent quality_signal key must be neutral, not an implicit opt-in")
}

// TestQualitySignalGate_IndependentFromTelemetrySetting proves the two surfaces
// disagree freely: telemetry: false + quality_signal: true resolves the quality
// gate enabled while the telemetry gate is disabled, and the converse. Neither
// key's persisted value influences the other's resolution.
func TestQualitySignalGate_IndependentFromTelemetrySetting(t *testing.T) {
	t.Run("quality on, telemetry off", func(t *testing.T) {
		isolate(t)
		_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
		_ = os.Unsetenv("ATCR_TELEMETRY")
		writeAtcrConfig(t, "agents: [bruce]\ntelemetry: false\nquality_signal: true\n")
		assert.True(t, qualitySignalGate(), "quality_signal: true must enable the quality gate")
		assert.False(t, telemetryGate(), "telemetry: false must keep the telemetry gate disabled — independently")
	})
	t.Run("quality off, telemetry on", func(t *testing.T) {
		isolate(t)
		_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
		_ = os.Unsetenv("ATCR_TELEMETRY")
		writeAtcrConfig(t, "agents: [bruce]\ntelemetry: true\nquality_signal: false\n")
		assert.False(t, qualitySignalGate(), "quality_signal: false must keep the quality gate disabled")
		assert.True(t, telemetryGate(), "telemetry: true (no env opt-out) must keep the telemetry gate enabled — independently")
	})
}

// TestQualitySignalGate_IndependentFromSyncCloud proves a valid ATCR_API_KEY (the
// --sync-cloud opt-in surface) has no bearing on the quality-signal gate: with a
// key present but no quality_signal opt-in, the gate still resolves disabled.
func TestQualitySignalGate_IndependentFromSyncCloud(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	t.Setenv("ATCR_API_KEY", "a-valid-looking-key")
	assert.False(t, qualitySignalGate(), "a valid ATCR_API_KEY must not enable the quality-signal gate")
}

// TestQualitySignalGate_MalformedConfigFailsSafeToDisabled is a privacy release
// gate: a corrupt persisted quality_signal value (e.g. a hand-edited
// `quality_signal: maybe`) must resolve the gate to DISABLED, never be silently
// coerced to consent — even when an env opt-in is present, a malformed config
// still fails safe because LoadQualitySignalSetting surfaces the parse error and
// the gate maps any load error to disabled.
func TestQualitySignalGate_MalformedConfigFailsSafeToDisabled(t *testing.T) {
	t.Run("malformed config, no env -> disabled", func(t *testing.T) {
		isolate(t)
		_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
		writeAtcrConfig(t, "agents: [bruce]\nquality_signal: maybe\n")
		assert.False(t, qualitySignalGate(), "a corrupt quality_signal value must never be interpreted as consent to transmit")
	})
	t.Run("malformed config overrides an env opt-in -> disabled", func(t *testing.T) {
		isolate(t)
		t.Setenv("ATCR_QUALITY_SIGNAL", "1")
		writeAtcrConfig(t, "agents: [bruce]\nquality_signal: maybe\n")
		assert.False(t, qualitySignalGate(), "a corrupt config must fail safe to disabled even with an env opt-in present")
	})
}

// TestQualitySignalGate_ReEvaluatedFreshPerInvocation proves there is no stale
// in-process cache: the gate re-reads env + config every call, so a change to
// either surface between calls flips the result.
func TestQualitySignalGate_ReEvaluatedFreshPerInvocation(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	assert.False(t, qualitySignalGate(), "first call: disabled by default")
	assert.False(t, qualitySignalGate(), "repeated call with no change: still disabled")

	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	assert.True(t, qualitySignalGate(), "a fresh env opt-in must be observed on the next call, not masked by a cache")
}

// --- Story 3: Local --preview surface (Phase 3) --------------------------

// seedQualityRecord appends one terminal local-debt record to the isolated
// .atcr/debt store so the --preview aggregation has data to render. Each call
// uses a distinct file/problem so StampID yields a distinct ID — records sharing
// an ID would fold together and undercount.
func seedQualityRecord(t *testing.T, persona, model, status, file string) {
	t.Helper()
	rec := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion,
		RunID:         "2026-07-01T10:00:00Z-seed",
		Timestamp:     "2026-07-01T10:00:00Z",
		Severity:      "LOW",
		File:          file,
		Line:          1,
		Problem:       "problem-" + file,
		Reviewers:     []string{persona},
		Model:         model,
		Status:        status,
	}
	rec.StampID()
	require.NoError(t, localdebt.Append(localdebt.DefaultDir("."), rec))
}

// expectedQualityPayload builds the payload the preview must print, from the same
// on-disk store the command reads. It is the equivalence target for the
// byte-identical and round-trip assertions — mirroring what the shared
// payload-construction helper does, so a preview that diverges is caught.
func expectedQualityPayload(t *testing.T) []telemetry.QualitySignal {
	t.Helper()
	recs, err := localdebt.ReadAll(localdebt.DefaultDir("."), localdebt.ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	rows := localdebt.AggregateQualitySignal(recs)
	ps := make([]telemetry.QualitySignal, 0, len(rows))
	for _, r := range rows {
		ps = append(ps, telemetry.NewQualitySignal(r.Persona, r.Model, r.DismissedCount, r.ConfirmedCount))
	}
	return ps
}

// runPreview drives a host command's RunE directly with args (bypassing the root
// PersistentPreRunE), capturing stdout. A non-nil client is injected so any stray
// send is observable via the doRequest seam; it is drained before returning.
func runPreview(t *testing.T, cmd *cobra.Command, client *telemetry.Client, args ...string) (string, error) {
	t.Helper()
	logger, err := log.New("info", "text", io.Discard)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), logger)
	if client != nil {
		ctx = telemetry.NewContext(ctx, client)
	}
	cmd.SetContext(ctx)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(new(bytes.Buffer))
	if perr := cmd.ParseFlags(args); perr != nil {
		return out.String(), perr
	}
	runErr := cmd.RunE(cmd, cmd.Flags().Args())
	if client != nil {
		client.Wait()
	}
	return out.String(), runErr
}

// splitPreview separates the JSON payload region from the trailing "not sent"
// marker line, so a test can assert on each independently.
func splitPreview(out string) (jsonPart, marker string) {
	idx := strings.Index(out, "Preview only")
	if idx < 0 {
		return out, ""
	}
	return out[:idx], out[idx:]
}

// TestPreview_PrintsAllowlistedJSONPayload proves `--preview` prints pretty JSON
// carrying exactly the four allowlisted quality-signal fields and exits 0 (AC 03-01
// Scenario 1).
func TestPreview_PrintsAllowlistedJSONPayload(t *testing.T) {
	isolate(t)
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "b.go")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "resolved", "c.go")

	out, err := runPreview(t, newReviewCmd(), nil, "--preview")
	require.NoError(t, err)

	jsonPart, _ := splitPreview(out)
	var got []telemetry.QualitySignal
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(jsonPart)), &got))
	require.Len(t, got, 1)
	assert.Equal(t, expectedQualityPayload(t), got)

	var raw []map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(jsonPart)), &raw))
	require.Len(t, raw, 1)
	keys := make([]string, 0, len(raw[0]))
	for k := range raw[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	assert.Equal(t, []string{"confirmed_count", "dismissed_count", "model", "persona_id_hash"}, keys,
		"preview must print only the allowlisted quality-signal fields")
}

// TestPreview_IncludesNotSentMarker proves the output carries an explicit
// "nothing was transmitted" line, distinct from the JSON payload (AC 03-01
// Scenario 2 — the false-sense-of-completion guard).
func TestPreview_IncludesNotSentMarker(t *testing.T) {
	isolate(t)
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	out, err := runPreview(t, newReviewCmd(), nil, "--preview")
	require.NoError(t, err)
	jsonPart, marker := splitPreview(out)
	assert.Contains(t, marker, "nothing was transmitted", "preview must carry an explicit not-sent marker")
	assert.NotContains(t, jsonPart, "transmitted", "the marker must be distinct from the JSON, not embedded in it")
}

// TestPreview_EmptyAggregationPrintsEmptyPayloadNotError proves an empty store
// prints an empty JSON array and exits 0, never an error (AC 03-01 EC1).
func TestPreview_EmptyAggregationPrintsEmptyPayloadNotError(t *testing.T) {
	isolate(t)
	out, err := runPreview(t, newReviewCmd(), nil, "--preview")
	require.NoError(t, err, "empty aggregation must print an empty payload, not error")
	jsonPart, marker := splitPreview(out)
	var got []telemetry.QualitySignal
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(jsonPart)), &got))
	assert.Empty(t, got)
	assert.Contains(t, marker, "nothing was transmitted")
}

// TestPreview_TakesPrecedenceOverSyncCloud proves `--preview` + `--sync-cloud`
// prints the payload and pushes nothing — the preview short-circuit fires before
// resolveSyncCloud, so a missing ATCR_API_KEY never turns into an exit-3 (AC 03-01
// EC2).
func TestPreview_TakesPrecedenceOverSyncCloud(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_API_KEY")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	hits := countingDoRequest(t)
	out, err := runPreview(t, newReviewCmd(), telemetry.New("https://telemetry.test/ingest"),
		"--preview", "--sync-cloud")
	require.NoError(t, err, "--preview must take precedence over --sync-cloud, never exit 3 on a missing key")
	assert.Contains(t, out, "persona_id_hash")
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "no send may fire on the --preview path")
}

// TestPreview_ZeroHTTPCalls_GateDisabled proves zero outbound requests with the
// opt-in gate disabled (the default) (AC 03-02 Scenario 1).
func TestPreview_ZeroHTTPCalls_GateDisabled(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	hits := countingDoRequest(t)
	_, err := runPreview(t, newReviewCmd(), telemetry.New("https://telemetry.test/ingest"), "--preview")
	require.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(hits))
}

// TestPreview_ZeroHTTPCalls_GateEnabled proves an enabled gate does not cause
// `--preview` to also send — the payload is still only printed (AC 03-02 Scenario 2).
func TestPreview_ZeroHTTPCalls_GateEnabled(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_QUALITY_SIGNAL", "1")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	hits := countingDoRequest(t)
	_, err := runPreview(t, newReviewCmd(), telemetry.New("https://telemetry.test/ingest"), "--preview")
	require.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(hits), "gate enabled must not cause --preview to send")
}

// TestPreview_WorksWithNoAPIKey proves `--preview` succeeds with no ATCR_API_KEY —
// it short-circuits before any credential resolution (AC 03-02 EC1).
func TestPreview_WorksWithNoAPIKey(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_API_KEY")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	out, err := runPreview(t, newReviewCmd(), nil, "--preview")
	require.NoError(t, err)
	assert.Contains(t, out, "persona_id_hash")
}

// TestPreview_UnaffectedByMalformedConfig proves a malformed persisted
// quality_signal value has no bearing on `--preview` — the gate is never consulted
// on this path (AC 03-02 EC2).
func TestPreview_UnaffectedByMalformedConfig(t *testing.T) {
	isolate(t)
	_ = os.Unsetenv("ATCR_QUALITY_SIGNAL")
	writeAtcrConfig(t, "agents: [bruce]\nquality_signal: maybe\n")
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	out, err := runPreview(t, newReviewCmd(), nil, "--preview")
	require.NoError(t, err, "a malformed quality_signal value must not affect --preview")
	jsonPart, _ := splitPreview(out)
	var got []telemetry.QualitySignal
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(jsonPart)), &got))
	assert.Equal(t, expectedQualityPayload(t), got)
}

// TestPreview_RegisteredOnReconcile proves the flag is hosted on `atcr reconcile`
// too, and that its short-circuit fires before resolveReviewDir (which would
// otherwise error with no review present) — the two-host-command contract.
func TestPreview_RegisteredOnReconcile(t *testing.T) {
	isolate(t)
	seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
	out, err := runPreview(t, newReconcileCmd(), nil, "--preview")
	require.NoError(t, err, "reconcile must host --preview and short-circuit before resolving a review dir")
	assert.Contains(t, out, "persona_id_hash")
	_, marker := splitPreview(out)
	assert.Contains(t, marker, "nothing was transmitted")
}

// TestPreview_ByteIdenticalToRealSendMarshal locks the preview JSON to the marshal
// of the same payload the real send would serialize, across 3+ fixtures — the
// marshal-path drift guard (AC 03-03 Scenario 1).
func TestPreview_ByteIdenticalToRealSendMarshal(t *testing.T) {
	cases := []struct {
		name string
		seed func(t *testing.T)
	}{
		{"empty aggregation", func(t *testing.T) {}},
		{"single row", func(t *testing.T) {
			seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
		}},
		{"multiple personas and models", func(t *testing.T) {
			seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
			seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "resolved", "b.go")
			seedQualityRecord(t, "diana", "gpt-5", "wontfix", "c.go")
			seedQualityRecord(t, "clark", "claude-opus-4-8", "resolved", "d.go")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			tc.seed(t)
			want := expectedQualityPayload(t)
			wantJSON, err := json.MarshalIndent(want, "", "  ")
			require.NoError(t, err)

			out, err := runPreview(t, newReviewCmd(), nil, "--preview")
			require.NoError(t, err)
			jsonPart, _ := splitPreview(out)
			assert.Equal(t, string(wantJSON), strings.TrimSpace(jsonPart),
				"preview JSON must be byte-identical to the real-send marshal of the same payload")
		})
	}
}

// TestPreview_GoldenRoundTrip proves the pretty-printed preview unmarshals back
// DeepEqual to the payload the real send would marshal, across fixtures (AC 03-03
// Scenario 2).
func TestPreview_GoldenRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		seed func(t *testing.T)
	}{
		{"empty", func(t *testing.T) {}},
		{"single row", func(t *testing.T) {
			seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
		}},
		{"multiple rows", func(t *testing.T) {
			seedQualityRecord(t, "bruce", "claude-sonnet-4-6", "wontfix", "a.go")
			seedQualityRecord(t, "diana", "gpt-5", "resolved", "c.go")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			tc.seed(t)
			want := expectedQualityPayload(t)

			out, err := runPreview(t, newReviewCmd(), nil, "--preview")
			require.NoError(t, err)
			jsonPart, _ := splitPreview(out)

			var got []telemetry.QualitySignal
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(jsonPart)), &got))
			if len(want) == 0 && len(got) == 0 {
				return // both empty: nil-vs-empty-slice normalization
			}
			assert.True(t, reflect.DeepEqual(want, got),
				"preview output must round-trip DeepEqual to the payload the real send would marshal: want %+v got %+v", want, got)
		})
	}
}
