package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/telemetry"
	"github.com/spf13/cobra"
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
// warns to w (os.Stderr from the per-run qualitySignalGate check, the command's
// stderr on the --preview path) so a misspelled opt-in (e.g. "ture") is visible
// rather than silently ignored.
func qualitySignalEnabledFromEnv(w io.Writer) bool {
	v := strings.TrimSpace(os.Getenv("ATCR_QUALITY_SIGNAL"))
	if v == "" {
		return false
	}
	enabled, err := strconv.ParseBool(v)
	if err != nil {
		_, _ = fmt.Fprintf(w, "warning: unrecognized ATCR_QUALITY_SIGNAL value %q; treating as disabled\n", v)
		return false
	}
	return enabled
}

// qualitySignalGate resolves the final enabled/disabled state for one
// review/reconcile run by OR-combining the live ATCR_QUALITY_SIGNAL env var with
// the persisted .atcr/config.yaml quality_signal opt-in. The config is located
// via repo-root discovery — the SAME root `atcr config set quality_signal`
// persists to (runConfigSet) — so the gate and the write path agree on config
// location even when atcr runs from a repo subdirectory. If repo-root discovery
// itself fails, the gate falls back to the cwd-relative read rather than
// breaking. It is re-evaluated fresh per run — no in-process cache — guarding a
// future send call site so a disabled state short-circuits before any payload
// is built.
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
	// The warning writer is os.Stderr here: the gate's send-path call sites
	// (review.go/reconcile.go) pass only a context, and this plain UX string
	// mirrors telemetryEnabledFromEnv's stderr warning convention.
	env := qualitySignalEnabledFromEnv(os.Stderr)
	// Resolve the config via repo-root discovery so the gate reads the same
	// .atcr/config.yaml `config set` writes, from any subdirectory. On a
	// discovery failure (os.Getwd), fall back to the former cwd-relative read
	// rather than breaking the gate.
	root, rerr := repoRoot()
	if rerr != nil {
		root = "."
	}
	cfg, err := registry.LoadQualitySignalSetting(root)
	if err != nil {
		return false
	}
	return qualitySignalEnabled(env, cfg)
}

// qualitySignalNotSentMarker is the human-readable line printed after the
// --preview JSON. It is a DISTINCT line (never embedded in the payload) so a
// maintainer cannot mistake the preview for confirmation that data was sent — the
// epic's flagged "false sense of completion" risk (Story 3, AC 03-01 Scenario 2).
const qualitySignalNotSentMarker = "Preview only — nothing was transmitted. The quality signal is sent only after you explicitly opt in."

// buildQualitySignalPayload is the SINGLE source of the outbound quality-signal
// payload. It reads the append-only local-debt store under root, folds and
// aggregates it into per-(persona, model) rows (Story 1), and maps each row to an
// allowlisted telemetry.QualitySignal — hashing the persona at the payload
// boundary via NewQualitySignal so no raw name ever reaches the wire. Both the
// --preview branch (Story 3) and the real transport send (Story 6) build their
// payload here, so the preview can never drift from what is actually transmitted
// (AC 03-03). A missing store is not an error: ReadAll returns no records and the
// payload is a non-nil empty slice (marshals to []), so --preview on a fresh
// checkout prints an empty payload rather than failing (AC 03-01 EC1).
func buildQualitySignalPayload(root string) ([]telemetry.QualitySignal, error) {
	records, err := localdebt.ReadAll(localdebt.DefaultDir(root), localdebt.ReadOpts{Writer: io.Discard})
	if err != nil {
		return nil, err
	}
	rows := localdebt.AggregateQualitySignal(records)
	payload := make([]telemetry.QualitySignal, 0, len(rows))
	for _, r := range rows {
		payload = append(payload, telemetry.NewQualitySignal(r.Persona, r.Model, r.DismissedCount, r.ConfirmedCount))
	}
	return payload, nil
}

// renderQualitySignalPreview writes the pretty-printed payload JSON followed by
// the not-sent marker on its own line, to w. It is the presentation half of
// --preview; the payload itself comes from the shared buildQualitySignalPayload,
// so the printed JSON is exactly the marshal of what a real send would transmit.
func renderQualitySignalPreview(w io.Writer, payload []telemetry.QualitySignal) error {
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n%s\n", b, qualitySignalNotSentMarker)
	return err
}

// buildQualitySignalPayloadFn is the payload-constructor seam for the gated send
// call site (Story 6). The send path builds its payload THROUGH this var so a test
// can wrap it and assert the disabled opt-in gate short-circuits BEFORE any payload
// is constructed (AC 06-01) — the privacy line proven at the constructor, not merely
// inferred from the absence of a request. Production always uses the real
// buildQualitySignalPayload; only tests reassign it.
var buildQualitySignalPayloadFn = buildQualitySignalPayload

// maybeSendQualitySignal is the gated, fail-open send call site for the community
// prompt quality signal, invoked at review/reconcile completion adjacent to the
// passive-ping emission. It resolves the independent opt-in gate FRESH per run and
// short-circuits — before any payload construction, goroutine spawn, or client
// work — when disabled, so a non-opted-in run allocates nothing and transmits
// nothing (AC 06-01). When enabled it builds the payload via the SAME constructor
// --preview renders and, when the aggregation is non-empty, hands it to the
// fail-open transport (task 5.5). A zero-row aggregation transmits nothing (AC
// 06-02 EC1). Every failure — a debt-store read error, a marshal error, a
// non-2xx/DNS/timeout, or a panic inside the send path — is swallowed here or by
// the transport: the send never changes the run's exit code or stdout (AC 06-03).
func maybeSendQualitySignal(ctx context.Context) {
	// Fail-open absolute (AC 06-03): a panic anywhere on this best-effort path — the
	// synchronous aggregation build or the transport dispatch — is recovered here so
	// it can never alter the review/reconcile run's exit code or stdout. The transport
	// goroutine has its own recover; this guards the synchronous portion at the inline
	// call site, which is not itself deferred on the reconcile path.
	defer func() {
		if r := recover(); r != nil {
			log.FromContext(ctx).Debug("quality-signal: recovered from panic", "value", r)
		}
	}()
	// Gate FIRST: a disabled opt-in short-circuits before any payload is built,
	// proving the privacy line at the constructor, not merely at the network seam.
	if !qualitySignalGate() {
		return
	}
	payload, err := buildQualitySignalPayloadFn(".")
	if err != nil {
		return // fail-open: a read error never surfaces on the send path
	}
	if len(payload) == 0 {
		return // nothing to transmit — no empty/partial body is ever sent
	}
	// Hand the payload to the fail-open transport on its detached goroutine. The
	// same buildQualitySignalPayloadFn output feeds --preview, and SendQualitySignal
	// marshals it with the identical indentation the preview renders, so the sent
	// bytes are byte-identical to the preview (AC 06-02). A nil client no-ops.
	telemetry.FromContext(ctx).SendQualitySignal(ctx, payload)
}

// maybePreviewQualitySignal implements the --preview short-circuit for the host
// commands (review, reconcile). When --preview is set it builds the payload from
// the local debt store and renders it (pretty JSON + not-sent marker), returning
// handled=true so the caller returns from RunE BEFORE any opt-in gate check,
// transport or HTTP client construction, credential resolution, or --sync-cloud
// network precondition (AC 03-02) — so --preview never reads or requires
// ATCR_API_KEY and never sends, whether or not the user has opted in (AC 03-01
// EC2). It does NOT bypass cobra's pure flag-relationship validation (the range
// flags' PreRunE), which runs before RunE under Execute() but performs no I/O,
// network, gate, or credential access — so an invalid range-flag COMBINATION still
// fails as a usage error, while the AC-relevant guarantees (no send, no gate, no
// key) are unaffected. When --preview is unset it returns handled=false and the
// caller proceeds normally. A non-ENOENT local-debt read failure is surfaced as a
// usage error (exit 2), matching the host commands' config/range error convention.
//
// The opt-in env var is validated here too: --preview is where users experiment
// with opting in, so a misspelled ATCR_QUALITY_SIGNAL warns on the command's
// stderr on this path exactly as the gated send path warns on os.Stderr. The
// resolved value is deliberately discarded — the preview renders regardless of
// the gate outcome (AC 03-02).
func maybePreviewQualitySignal(cmd *cobra.Command) (handled bool, err error) {
	if !boolFlag(cmd, "preview") {
		return false, nil
	}
	_ = qualitySignalEnabledFromEnv(cmd.ErrOrStderr())
	payload, err := buildQualitySignalPayload(".")
	if err != nil {
		return true, usageError(fmt.Errorf("reading local debt store for --preview: %w", err))
	}
	return true, renderQualitySignalPreview(cmd.OutOrStdout(), payload)
}
