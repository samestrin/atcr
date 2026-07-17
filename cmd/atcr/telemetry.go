package main

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/telemetry"
)

// defaultTelemetryEndpoint is the compiled-in usage-ping ingestion URL. It is
// intentionally empty for now: the external ingestion backend is owned outside
// this repository, and an empty endpoint makes every telemetry Send a safe no-op
// (zero network in dev, CI, and production alike). When a real endpoint is wired
// it MUST be an https:// URL — telemetry.Client refuses plaintext http.
const defaultTelemetryEndpoint = ""

// telemetryEnabled is the strict OR-disables combining function (Story 2): the
// anonymous usage ping runs ONLY when BOTH surfaces agree it should — the env
// var permits it AND the persisted config does not disable it. It is total and
// pure (no I/O), so the four-combination truth table is exhaustively testable
// and the client itself carries no precedence logic to get wrong.
//
//	envEnabled | cfgTelemetry | result
//	  true     |   nil        | enabled   (nothing disables)
//	  true     |   &true      | enabled
//	  true     |   &false     | disabled  (config opt-out)
//	  false    |   nil        | disabled  (env opt-out)
//	  false    |   &true      | disabled  (env wins — config NEVER overrides an env opt-out)
//	  false    |   &false     | disabled
//
// A nil config field is neutral: it contributes nothing to the OR and can never
// out-rank a disabling env var.
func telemetryEnabled(envEnabled bool, cfgTelemetry *bool) bool {
	return envEnabled && (cfgTelemetry == nil || *cfgTelemetry)
}

// telemetryGate resolves the final enabled/disabled state for one emitting run
// by combining the live ATCR_TELEMETRY env var with the persisted
// .atcr/config.yaml opt-out (resolved cwd-relative, matching how every other
// command locates project config). It is called once per review/reconcile run,
// guarding the Send call site so a disabled state short-circuits BEFORE any
// goroutine spawns or payload is built — not merely before the HTTP call.
//
// A malformed persisted telemetry value fails SAFE to disabled: a corrupt value
// can never re-enable a ping the user may have intended to disable. (On the
// review path the same corruption also surfaces loudly via the strict
// LoadProjectConfig roster load, aborting before Send is ever reached.)
//
// SCOPE — passive ping ONLY. This gate governs the anonymous, background usage
// ping. It MUST NOT gate the Phase-4 `--sync-cloud` push: that is an EXPLICIT,
// user-invoked action, so suppressing it via this passive-ping opt-out would
// silently no-op something the user explicitly requested — the wrong consent
// model. `--sync-cloud` gets its own opt-in surface (the presence of a valid
// ATCR_API_KEY plus the explicit flag), independent of telemetryGate.
func telemetryGate() bool {
	env := telemetryEnabledFromEnv()
	cfg, err := registry.LoadTelemetrySetting(".")
	if err != nil {
		return false
	}
	return telemetryEnabled(env, cfg)
}

// reviewTelemetryEvent builds the anonymous usage Event for a completed review
// from already-computed grounding data only: a changed-line count and a
// dominant-language label derived from file extensions. It never copies raw diff
// content, file paths, or findings text into the payload (AC 01-04) — only the
// four allowlisted, aggregate fields.
func reviewTelemetryEvent(prep *fanout.PreparedReview, status string) telemetry.Event {
	return telemetry.Event{
		Event:  "review_run",
		Lang:   dominantLang(prep.Changed),
		Lines:  changedLineCount(prep.Changed),
		Status: status,
	}
}

// reconcileTelemetryEvent builds the usage Event for a completed reconcile. A
// reconcile run spans every source and has no single language or line count, so
// lang is empty and lines is zero by an explicit, documented contract (TD-005) —
// deliberately minimal, never accidentally-empty values derived from content.
func reconcileTelemetryEvent(status string) telemetry.Event {
	return telemetry.Event{Event: "reconcile_run", Status: status}
}

// changedLineCount sums the changed head-side line count across all files in the
// review's grounding data — a pure aggregate count, never the line text itself.
func changedLineCount(changed payload.ChangedLines) int {
	n := 0
	for _, fc := range changed {
		n += len(fc.ChangedText)
	}
	return n
}

// dominantLang returns the file-extension label (e.g. "go") of the file with the
// most changed lines. The label is "" whenever that single dominant file has no
// extension (e.g. a Makefile or Dockerfile dominates the change set) — even when
// other changed files do carry extensions — as well as when no file is present
// at all. The output is an aggregate language classification — it leaks neither
// the path nor the content it was derived from.
func dominantLang(changed payload.ChangedLines) string {
	best, bestN := "", 0
	// Iterate sorted paths so ties are broken deterministically by the
	// lexicographically smallest path (and therefore its extension).
	paths := make([]string, 0, len(changed))
	for path := range changed {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		fc := changed[path]
		if n := len(fc.ChangedText); n > bestN {
			best = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
			bestN = n
		}
	}
	return best
}
