package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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
