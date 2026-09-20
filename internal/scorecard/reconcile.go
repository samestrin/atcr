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
// opts threads emission control through unchanged: the CLI passes the
// --no-scorecard flag here (Story 5), which Emit honors as its first gate
// (returning before any I/O); the MCP handler passes EmitOpts{Diag: os.Stderr}
// (explicit stderr, the documented default for the cmd-less path) so the
// agentic path keeps emitting. It is fully best-effort: a missing pool summary
// degrades to finding-only records (reviewers recovered from the findings), and
// Emit logs its own write failures, so scorecard emission never fails the
// caller's reconcile.
func EmitForReconcile(reviewDir string, res reconcile.Result, opts EmitOpts) {
	// Honor suppression before any work: a --no-scorecard run must do truly zero
	// I/O, so gate here — ahead of the pool-summary read below — not only at
	// Emit's store gate. Emit keeps its own first-line gate for direct callers.
	if opts.NoScorecard {
		return
	}

	reviewers := map[string]ReviewerMeta{}
	if ps, err := fanout.ReadPoolSummary(reviewDir); err == nil {
		for _, a := range ps.Agents {
			reviewers[a.Agent] = ReviewerMeta{
				Model:     a.Model,
				TokensIn:  a.TokensIn,
				TokensOut: a.TokensOut,
				LatencyMS: a.DurationMS,
				Outcome:   coerceOutcome(fanout.ReviewerOutcome(a, raisedSlotsFor(a))),
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

	// The Tier 4 content check (Epic 35.16.6.5) routed these OUT of res.Findings
	// before this bridge ran, so they are invisible above. Carry them separately:
	// they belong in FindingsRaised (they ARE findings the reviewer raised) and
	// never in FindingsCorroborated. Registering their reviewers here matters as
	// much as the counts — a reviewer whose every finding was routed appears in
	// neither res.Findings nor the pool summary of a path-anchored review, and
	// would otherwise get no record at all: no rate, and so no trust penalty for
	// a run that produced nothing but phantoms.
	unresolved := make([]Finding, 0, len(res.Unresolved))
	for _, u := range res.Unresolved {
		unresolved = append(unresolved, Finding{
			File:      u.File,
			Line:      u.Line,
			Problem:   u.Problem,
			Reviewers: u.Reviewers,
			// Carried, not interpreted here: Emit decides which reasons are
			// chargeable, so the two entry points cannot disagree about it.
			UnresolvedReason: u.UnresolvedReason,
		})
		for _, rev := range u.Reviewers {
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
		RunID:     runID,
		Findings:  findings,
		Reviewers: reviewers,
		// The counts above come from res.Findings, the POST-consensus-filter set,
		// so they are only comparable across runs at the same level. Recording the
		// level is what lets TrustPriors restrict itself to the strict runs its
		// historical semantics assume, instead of letting an exploratory off or
		// lenient run durably depress the priors later strict runs read.
		ConsensusLevel:     res.Summary.ConsensusLevel,
		UnresolvedFindings: unresolved,
		VerificationPath:   verPath,
	}, opts)
}

// raisedSlotsFor supplies ReviewerOutcome's `raised` parameter on the reconcile
// path. The classifier reads only whether the slice is non-empty, so a slice of
// that length carries everything it needs.
//
// The COUNT is the load-bearing part, and it comes from the agent's own status
// rather than from reconcile.Result. That is AC 02-02 Scenario 0, and it is the
// parity test's blind spot: feeding the same function from two call sites proves
// nothing if one of them computes its input differently. The benchmark path
// reads `raised` from the merged findings.txt written AFTER enforceConstraints,
// and AgentStatus.FindingsCount is that same post-enforcement, post-grounding
// number. res.Findings is not — it is reconcile's post-merge output, so a
// reviewer whose findings all clustered under another reviewer's name is absent
// from it entirely and would classify CLEAN: "read the diff and found nothing",
// asserted about a lens that raised several.
func raisedSlotsFor(a fanout.AgentStatus) []string {
	if a.FindingsCount <= 0 {
		return nil
	}
	// One sentinel element, never make([]string, a.FindingsCount). FindingsCount
	// is decoded straight out of sources/pool/summary.json — a file on disk that
	// the MCP handler will read from a CALLER-SUPPLIED directory — so sizing an
	// allocation from it hands an attacker the length. A count near math.MaxInt
	// panics makeslice with "len out of range", and this function sits inside
	// EmitForReconcile, whose whole contract is that scorecard emission never
	// fails the caller's reconcile. It would have taken the process down instead.
	//
	// Only len(raised) > 0 is ever read, so one element is all the information
	// the classifier can use. Returning exactly one also keeps the allocation
	// O(1) per reviewer rather than O(findings).
	return []string{""}
}

// coerceOutcome is the reconcile path's guard on Record.Outcome: a value that is
// not a member of the vocabulary is replaced with unknown before Emit sees it.
//
// It is NOT the write boundary, and the difference matters. Emit and Append are
// both exported and copy meta.Outcome into the record unvalidated, so a future
// caller that builds its own ReviewerMeta bypasses this entirely. Moving the
// check down into Emit would close that, and is filed as TD rather than done
// here because it changes behaviour for every Emit caller, not just this one.
//
// It is deliberately SILENT and fail-neutral rather than an error return.
// EmitForReconcile has no error return by contract — scorecard emission never
// fails the caller's reconcile — so the only two choices here are "write
// something wrong" and "write unknown". Unknown is right: it is excluded from
// trust scoring downstream, so a classifier bug costs a lens nothing, whereas a
// garbage value persisted for the 180-day window could not be interpreted at
// all.
//
// The real classifier cannot return an invalid value (see
// TestFanoutReviewerOutcome_AlwaysReturnsAKnownValue), so this guards the case
// where that stops being true, which is exactly when a guard is worth having.
func coerceOutcome(o string) string {
	if !fanout.ValidReviewerOutcome(o) {
		return ""
	}
	return o
}
