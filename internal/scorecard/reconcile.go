package scorecard

import (
	"path/filepath"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
)

// EmitForReconcile builds and writes the per-run scorecard for a completed
// reconcile. It is the single shared bridge every reconcile entry point invokes
// — the CLI `atcr reconcile` and the MCP atcr_reconcile handler — so the two
// cannot diverge: a reconcile through either path produces the same scorecard
// records (TD-005). Per-reviewer model/token/latency metadata comes from the
// fan-out's persisted pool summary.json; finding counts come from res; the
// conditional skeptic fields come from reconciled/verification.json when present.
//
// It is fully best-effort: a missing pool summary degrades to finding-only
// records (reviewers recovered from the findings), and Emit logs its own write
// failures, so scorecard emission never fails the caller's reconcile.
func EmitForReconcile(reviewDir string, res reconcile.Result) {
	reviewers := map[string]ReviewerMeta{}
	if ps, err := fanout.ReadPoolSummary(reviewDir); err == nil {
		for _, a := range ps.Agents {
			reviewers[a.Agent] = ReviewerMeta{
				Model:     a.Model,
				TokensIn:  a.TokensIn,
				TokensOut: a.TokensOut,
				LatencyMS: a.DurationMS,
			}
		}
	}

	// A path-anchored review with no fan-out pool summary still has reviewers in
	// the findings; ensure each non-blank reviewer gets a record even without
	// usage metadata.
	findings := make([]Finding, 0, len(res.Findings))
	for _, m := range res.Findings {
		findings = append(findings, Finding{
			File:      m.File,
			Line:      m.Line,
			Problem:   m.Problem,
			Reviewers: m.Reviewers,
		})
		for _, rev := range m.Reviewers {
			if rev == "" {
				continue
			}
			if _, ok := reviewers[rev]; !ok {
				reviewers[rev] = ReviewerMeta{}
			}
		}
	}

	runID := res.Summary.ReconciledAt + "-" + filepath.Base(reviewDir)
	verPath := filepath.Join(reviewDir, "reconciled", "verification.json")
	// Emit is best-effort and logs its own failures; ignore the return so
	// reconcile never fails on a scorecard write.
	_ = Emit(EmitInput{
		RunID:            runID,
		Findings:         findings,
		Reviewers:        reviewers,
		VerificationPath: verPath,
	}, EmitOpts{})
}
