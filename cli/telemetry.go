package cli

import (
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/telemetry"
)

// defaultTelemetryEndpoint is the compiled-in usage-ping ingestion URL, and
// defaultQualitySignalEndpoint its quality-signal sibling. Both are live: the
// atcr.dev backend serves them as SEPARATE handlers behind the /api/v1/* rewrite,
// each validating against its own closed key allowlist (strict set equality — an
// extra or missing key is a 400, not a warning). They are therefore never
// interchangeable: the usage ping is a single JSON object, the quality signal a
// JSON array, and posting either at the other's handler is rejected and then
// dropped silently by the fail-open send path.
//
// Both MUST stay https:// — telemetry.Client refuses plaintext http and would
// no-op instead. An empty value is a silent per-path no-op, which is what allows
// either constant to be deactivated independently without disturbing the other.
//
// Transmission is still governed entirely by the consent gates, which these
// values do not touch: the usage ping by telemetryGate (default-on, opt-out via
// ATCR_TELEMETRY / config) and the quality signal by its own opt-IN gate.
//
// They are package VARS, not consts, and that is deliberate: NewRootCmd — the only
// advertised zero-argument constructor, and the seam the private atcr-enterprise
// module builds the same tree from — reads them at construction. As consts an
// embedder, a fork, or an air-gapped deployment had no way to redirect telemetry
// at all short of total disablement, and `-ldflags -X` could not touch them either
// (it cannot set a const). As vars, a build can retarget either destination with
//
//	go build -ldflags "-X github.com/samestrin/atcr/cli.defaultTelemetryEndpoint=https://collector.internal/usage"
//
// and an in-process embedder can assign them before constructing the tree. Setting
// one to "" deactivates only its own surface (dispatch no-ops on a non-HTTPS
// endpoint), which is what allows the two to be turned off independently.
//
// Production code must never MUTATE these after startup: the client is constructed
// once per process and reads them at that moment, so a later write is both a data
// race and a no-op for the already-built client.
var (
	defaultTelemetryEndpoint     = "https://atcr.dev/api/v1/telemetry"
	defaultQualitySignalEndpoint = "https://atcr.dev/api/v1/quality-signal"
)

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
// .atcr/config.yaml opt-out. The config is located via repo-root discovery —
// the SAME root `atcr config set telemetry` persists to (runConfigSet) — so the
// gate and the write path agree on config location even when atcr runs from a
// repo subdirectory. If repo-root discovery itself fails, the gate falls back
// to the former cwd-relative read rather than breaking. It is called once per
// review/reconcile run, guarding the Send call site so a disabled state
// short-circuits BEFORE any goroutine spawns or payload is built — not merely
// before the HTTP call.
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
func telemetryGate(w io.Writer) bool {
	env := telemetryEnabledFromEnv(w)
	// Resolve the config via repo-root discovery so the gate reads the same
	// .atcr/config.yaml `config set` writes, from any subdirectory. On a
	// discovery failure (os.Getwd), fall back to the former cwd-relative read
	// rather than breaking the gate.
	root, rerr := repoRoot()
	if rerr != nil {
		root = "."
	}
	cfg, err := registry.LoadTelemetrySetting(root)
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
