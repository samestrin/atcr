package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/registry"
)

// qualitySignalEnabled is the opt-IN OR-enables combining function (Story 2): the
// community prompt quality signal is transmitted ONLY when the user has explicitly
// opted in on EITHER surface — the env var OR the persisted config. It is the exact
// inverse of telemetryEnabled's opt-OUT AND-disables shape and MUST NOT be derived
// from it. It is total and pure (no I/O), so the six-combination truth table is
// exhaustively testable and the call site carries no precedence logic to get wrong.
//
//	envEnabled | cfgQualitySignal | result
//	  false    |   nil            | disabled  (nothing opts in — the default)
//	  false    |   &true          | enabled   (config alone is sufficient consent)
//	  false    |   &false         | disabled
//	  true     |   nil            | enabled   (env alone is sufficient consent)
//	  true     |   &true          | enabled
//	  true     |   &false         | enabled   (an explicit env opt-in is never revoked by a stale config false)
//
// A nil config field is neutral: it contributes nothing to the OR and can never
// out-rank a permitting env var.
func qualitySignalEnabled(envEnabled bool, cfgQualitySignal *bool) bool {
	return envEnabled || (cfgQualitySignal != nil && *cfgQualitySignal)
}

// qualitySignalEnabledFromEnv reads the ATCR_QUALITY_SIGNAL opt-IN env var. It
// names the ENABLED state directly and defaults OFF: unset, blank, or any
// unparseable value resolves to disabled — the privacy-preserving fail-safe, the
// inverse of ATCR_TELEMETRY's fail-OPEN-to-enabled posture. An unparseable value
// warns once (this is read once per run via qualitySignalGate) so a misspelled
// opt-in (e.g. "ture") is visible rather than silently ignored.
func qualitySignalEnabledFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("ATCR_QUALITY_SIGNAL"))
	if v == "" {
		return false
	}
	enabled, err := strconv.ParseBool(v)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: unrecognized ATCR_QUALITY_SIGNAL value %q; treating as disabled\n", v)
		return false
	}
	return enabled
}

// qualitySignalGate resolves the final enabled/disabled state for one
// review/reconcile run by OR-combining the live ATCR_QUALITY_SIGNAL env var with
// the persisted .atcr/config.yaml quality_signal opt-in (resolved cwd-relative,
// matching how every other command locates project config). It is re-evaluated
// fresh per run — no in-process cache — guarding a future send call site so a
// disabled state short-circuits before any payload is built.
//
// INDEPENDENCE — it shares NO state with telemetryGate/resolveSyncCloud: it
// neither reads nor calls either, funnels through no common precedence table, and
// touches no shared package variable, so an unrelated feature's setting (a
// telemetry opt-out, a valid ATCR_API_KEY, an enabled --sync-cloud plan) can never
// grant or revoke quality-signal consent.
//
// A malformed persisted quality_signal value fails SAFE to disabled: a corrupt
// value can never be interpreted as consent to transmit.
func qualitySignalGate() bool {
	env := qualitySignalEnabledFromEnv()
	cfg, err := registry.LoadQualitySignalSetting(".")
	if err != nil {
		return false
	}
	return qualitySignalEnabled(env, cfg)
}
