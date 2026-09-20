package scorecard

import (
	"path/filepath"
	"strings"

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
			// Trim ONCE and key on the trimmed name, matching NewCloudSyncRecord.
			// Untrimmed, " bruce" and "bruce" are two distinct trust keys, and a
			// whitespace-only name would become a reviewer literally called "  ".
			//
			// Every OTHER use of a reviewer name below is trimmed the same way,
			// at the point the name enters this function. That is not tidiness:
			// reviewerCounts matches Finding.Reviewers against this map key by
			// exact string, so trimming the key alone silently zeroes a padded
			// reviewer's FindingsRaised, its corroboration rate and its skeptic
			// verdicts while the record itself still looks healthy.
			name := strings.TrimSpace(a.Agent)
			if name == "" {
				continue
			}
			reviewers[name] = ReviewerMeta{
				Model:     a.Model,
				TokensIn:  a.TokensIn,
				TokensOut: a.TokensOut,
				LatencyMS: a.DurationMS,
				Outcome:   outcomeFor(a),
			}
		}
	}
	// A path-anchored review with no fan-out pool summary still has reviewers in
	// the findings; ensure each non-blank reviewer gets a record even without
	// usage metadata.
	findings := make([]Finding, 0, len(res.Findings))
	for _, m := range res.Findings {
		names := trimmedReviewers(m.Reviewers)
		findings = append(findings, Finding{
			File:      m.File,
			Line:      m.Line,
			Problem:   m.Problem,
			Reviewers: names,
			// Carried, not interpreted here: Emit decides which values are
			// durable (see reviewerCategories' vocabulary gate), so the two
			// entry points cannot disagree about it.
			//
			// READ THE MEANING, it is not the obvious one. m is a
			// reconcile.Merged, whose Category is ModalCategory(group) — the
			// CLUSTER's modal value, not this reviewer's own word. In a cluster
			// where one lens said `security` and two said `performance`, the
			// merged category is `performance` and the first lens's own term is
			// unrecoverable from res.Findings. This threading therefore adopts
			// the cluster-modal meaning EXPLICITLY, which is sound for the only
			// consumer: opportunity-set membership unions the categories across
			// every reviewer on the case and asks "was this topic in play",
			// which the modal value answers faithfully. It would NOT be sound
			// for a per-reviewer claim about what that lens personally raised,
			// and nothing may read it as one.
			Category: m.Category,
		})
		for _, name := range names {
			if _, ok := reviewers[name]; !ok {
				// outcomeFindings: this reviewer has no AgentStatus (the run
				// wrote no pool summary, or wrote one this reviewer is absent
				// from) but it is NAMED ON A FINDING THAT SURVIVED RECONCILE.
				// "Raised at least one finding" is exactly what the shared
				// classifier means by findings, so this agrees with what the
				// benchmark path would record for the same reviewer instead of
				// diverging from it.
				//
				// It is NOT a security boundary, and an earlier attempt to treat
				// it as one was withdrawn after measurement. Withholding the
				// classification here does not stop a hand-authored review
				// directory from minting a trust prior: the pool summary that
				// would re-enable the classification lives in the SAME directory,
				// so the guard cost an attacker one extra JSON file and cost
				// every legitimate path-anchored install its trust priors
				// permanently. The forgery exposure is pre-existing and wider
				// than this line — reproduced unchanged at 3aad9a7d (yielding
				// map[attacker:1 sockpuppet:1]), before this sprint touched the file — and is filed as TD-021.
				reviewers[name] = ReviewerMeta{Outcome: outcomeFindings}
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
		names := trimmedReviewers(u.Reviewers)
		unresolved = append(unresolved, Finding{
			File:      u.File,
			Line:      u.Line,
			Problem:   u.Problem,
			Reviewers: names,
			// Carried, not interpreted here: Emit decides which reasons are
			// chargeable, so the two entry points cannot disagree about it.
			UnresolvedReason: u.UnresolvedReason,
			// Threaded at THIS site too, not only the primary one above. A
			// reviewer whose every finding was Tier-4 routed is reachable
			// nowhere else, so skipping it here would give exactly the lenses
			// that produced nothing but phantoms an empty category set — and an
			// empty set reads as out-of-remit, quietly excusing them from the
			// denominator this store exists to charge.
			Category: u.Category,
		})
		for _, name := range names {
			if _, ok := reviewers[name]; !ok {
				// outcomeFindings, same as the findings loop: a reviewer
				// reached only here DID raise findings — the Tier 4 content check
				// routed them, which is why they are not in res.Findings. The
				// phantom is charged through FindingsRaised, not through the
				// outcome, so the outcome only has to say what happened.
				//
				// Deliberately NOT outcomeUngrounded, which an earlier attempt
				// used. `ungrounded` already means fanout's DroppedByGrounding
				// gate (Epic 14.1); pointing one durable value at a second,
				// different gate would make the stored value ambiguous. It was
				// also backwards for the doc-shield carve-out, whose definition
				// is that the subject WAS named in the tree.
				reviewers[name] = ReviewerMeta{Outcome: outcomeFindings}
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

// outcomeFor classifies one agent's status for the durable record, refusing to
// classify a status that is not internally coherent.
//
// A negative FindingsCount is the case worth naming. It cannot happen from
// atcr's own fan-out, so a summary.json carrying one is corrupt or crafted —
// and internal/mcp reads that file from a CALLER-SUPPLIED directory. Passed
// straight through, it looks to the classifier exactly like "raised nothing" and
// lands on OutcomeClean, which is ELIGIBLE: a crafted file would mint durable,
// trust-scoring records asserting a successful clean review that never ran.
// Unknown is the honest answer for an incoherent status, and it is excluded.
func outcomeFor(a fanout.AgentStatus) string {
	if a.FindingsCount < 0 {
		return ""
	}
	return coerceOutcome(fanout.ReviewerOutcome(a, raisedSlotsFor(a)))
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

// trimmedReviewers normalises a finding's reviewer list once, at the point the
// names enter this package, dropping blanks.
//
// It exists because the map key and Finding.Reviewers MUST be the same string.
// reviewerCounts matches them with exact-string equality (scorecard.go), so
// trimming only the key silently zeroes a padded reviewer's FindingsRaised, its
// corroboration rate and its skeptic verdicts while the record still carries a
// model, a cost and an outcome and therefore still looks healthy. A padded name
// is one whitespace typo in a registry agent name away: fanout copies the agent
// name into the finding verbatim, reconcile's merge strips commas but not
// spaces, and the stream parser assigns its reviewer column untrimmed.
//
// Returning a fresh slice rather than editing in place keeps res untouched —
// the caller's reconcile.Result is not this function's to mutate.
func trimmedReviewers(in []string) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		if name := strings.TrimSpace(r); name != "" {
			out = append(out, name)
		}
	}
	return out
}
