package scorecard

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"sort"
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
			// Normalize ONCE and key on the canonical name — case-FOLDED as well
			// as trimmed, via normalizeReviewerName. Untrimmed, " bruce" and
			// "bruce" are two distinct trust keys; un-folded, "Bruce" and "bruce"
			// mint TWO reviewer records for one run, so trustPriorsSince sums
			// t.runs = 2 and one run buys two credits against
			// DefaultTrustMinRuns. Folding here AND in trimmedReviewers below is
			// what keeps the map key and Finding.Reviewers equal strings — the
			// invariant reviewerCounts' exact-string match depends on.
			name := normalizeReviewerName(a.Agent)
			if name == "" {
				continue
			}
			outcome := outcomeFor(a, opts.Diag)
			if prev, seen := reviewers[name]; seen && outcomeRank(prev.Outcome) > outcomeRank(outcome) {
				// A repeated Agent must not let the later, lower-precedence
				// entry overwrite the earlier one's outcome: a summary that
				// lists "bruce failed" and then "bruce ok" records bruce as
				// FAILED, so a crafted file cannot hide a failure behind a
				// later clean entry. The later entry still supplies the usage
				// metadata (model, tokens, latency) — only the OUTCOME, the
				// trust-load-bearing field, takes precedence.
				outcome = prev.Outcome
			}
			reviewers[name] = ReviewerMeta{
				Model:     a.Model,
				TokensIn:  a.TokensIn,
				TokensOut: a.TokensOut,
				LatencyMS: a.DurationMS,
				Outcome:   outcome,
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
			//
			// DEFERRED (2026-09-22 clarification): carrying the per-source
			// category alongside the modal one is NOT fixable here. Option (a)
			// threads it through reconcile.Merged — a published-module change
			// (tag-cut + pin-bump) reserved for TD-039's sprint per C22/D3.
			// Option (b), passing the pre-merge group to this bridge, is
			// impossible as stated: reconcile.Merged is struct{Finding} only
			// (reconcile/merge.go), so the pre-merge group is not retained. No
			// consumer needs per-source category today; this annotation is the
			// record of that deferral.
			Category: m.Category,
			// Threaded at THIS site ONLY, and the asymmetry with Category just
			// above is deliberate rather than an oversight. reviewerPairSignals
			// reads in.Findings and nothing else, so these two values are dead
			// weight on the other two streams: a Tier-4-routed phantom has no
			// co-reviewer to relate to (routing is what took it out of the
			// merged set), and the ambiguous stream is documented as
			// category-only precisely so it moves no count.
			//
			// Both carry reconcile.Merge's CLUSTER values, same as Category.
			// Severity is the cluster MAX; Disagreement is Merge's record that
			// the members did not agree on it ("<lo> vs <hi>"), which is what
			// BuildDisagreements calls a severity_split. The split lives in the
			// second field, never in the first.
			Severity:     m.Severity,
			Disagreement: m.Disagreement,
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

	// Third category stream: res.Ambiguous. The motivating route is the
	// consensus filter — under strict consensus an uncorroborated singleton is
	// routed here unless trustExempt spares it, and trustExempt is off for
	// precisely the lenses that have no prior yet, so reading only res.Findings
	// starves them (see EmitInput.AmbiguousFindings for the full loop).
	//
	// IT IS WIDER THAN THAT ROUTE, and the difference matters when reading a
	// stored record. reconcile/dedupe.go appends two other kinds of cluster on
	// EVERY run at EVERY consensus level: DBSCAN-isolated noise singletons, and
	// gray-zone PAIRS. Gray-pair members are guarded out of the noise exclusion,
	// so they also reach res.Findings — meaning one finding can contribute a
	// category here AND there. That is harmless to the value (CategoriesRaised is
	// a deduped set) but not to its meaning: the res.Findings copy carries
	// Merge's cluster-MODAL category while this copy carries the reviewer's own
	// raw word, so the set can gain both. The field's documented meaning is
	// modal; this mixed provenance is filed as TD-035.
	//
	// Reviewers are NOT registered from this stream, deliberately. A reviewer
	// reachable only through a filtered cluster gets no record: that would be a
	// new record minted by a change whose remit is categories, and the record
	// would carry a zero denominator. The stream widens what an EXISTING record
	// knows about its case; it never creates one.
	//
	// THE SINGULAR SHAPE IS THE PRODUCTION ONE. singletonAmbiguousCluster
	// (reconcile/dedupe.go) normalises a one-reviewer finding BACK to the raw
	// per-source shape — Reviewer set, Reviewers and Confidence cleared — and a
	// consensus-filtered finding is always one-reviewer, because both consensus
	// floors are HIGH/MEDIUM and ConfidenceFor only reaches those with 2+ distinct
	// reviewers. The plural read below is DEFENSIVE, for the gray-pair route and
	// for any future producer that does not normalise. Reading only the plural
	// would attribute the motivating route to nobody at all.
	ambiguous := make([]Finding, 0, len(res.Ambiguous))
	// Gray-zone pair extraction keeps the CLUSTER structure AmbiguousFindings
	// flattens away: one canonical pair key per cluster whose distinct reviewers
	// number exactly TWO (the charge rule's only unambiguous shape). Distinct
	// reviewers are counted across the WHOLE cluster's findings — two findings
	// by the same two reviewers are one cluster, one item.
	grayPairs := make([]string, 0, len(res.Ambiguous))
	for _, c := range res.Ambiguous {
		for _, f := range c.Findings {
			names := trimmedReviewers(f.Reviewers)
			if len(names) == 0 && strings.TrimSpace(f.Reviewer) != "" {
				names = []string{strings.TrimSpace(f.Reviewer)}
			}
			if len(names) == 0 {
				continue
			}
			ambiguous = append(ambiguous, Finding{
				File:      f.File,
				Line:      f.Line,
				Problem:   f.Problem,
				Reviewers: names,
				Category:  f.Category,
			})
		}
		members := make(map[string]struct{}, 2)
		for _, f := range c.Findings {
			names := trimmedReviewers(f.Reviewers)
			if len(names) == 0 && strings.TrimSpace(f.Reviewer) != "" {
				names = []string{normalizeReviewerName(f.Reviewer)}
			}
			for _, n := range names {
				if n != "" {
					members[n] = struct{}{}
				}
			}
		}
		if len(members) != 2 {
			continue // singleton or 3+: no canonical pair to charge
		}
		pair := make([]string, 0, 2)
		for m := range members {
			pair = append(pair, m)
		}
		sort.Strings(pair)
		if key, ok := PairKey(pair[0], pair[1]); ok {
			grayPairs = append(grayPairs, key)
		}
	}

	// RunID discriminates on the ABSOLUTE review directory, not just its basename:
	// two repositories checked out side by side produce sibling directories with
	// the same leaf name, and two runs landing in the same second would otherwise
	// share an id — opportunityUnions would then MERGE their category sets (the
	// exact "every lens permanently in-remit after one broad run" failure its
	// comment warns against) and pairTallies would count the two runs as one Case
	// with max-collapsed evidence. The hash covers the caller-supplied path —
	// the identity the run is anchored to; ReconciledAt + basename stay the
	// leading components so IsRunID/monthFromRunID/runIDTime keep parsing it
	// unchanged (paths.go's regex tolerates anything after the timestamp).
	absDir, err := filepath.Abs(reviewDir)
	if err != nil {
		absDir = reviewDir
	}
	pathHash := sha256.Sum256([]byte(absDir))
	runID :=
		res.Summary.ReconciledAt + "-" + filepath.Base(reviewDir) + "-" + hex.EncodeToString(pathHash[:4])
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
		AmbiguousFindings:  ambiguous,
		GrayZonePairs:      grayPairs,
		VerificationPath:   verPath,
	}, opts)
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
func outcomeFor(a fanout.AgentStatus, diag io.Writer) string {
	if a.FindingsCount < 0 {
		return ""
	}
	// The raised count goes through AS A COUNT: ReviewerOutcome's second parameter
	// is raisedCount int (2026-09-22, closing the raisedSlotsFor row), so the type
	// cannot carry fake contents the way the old one-element sentinel slice did.
	return coerceOutcome(fanout.ReviewerOutcome(a, a.FindingsCount), diag)
}

// outcomeRank ranks an outcome by the classifier's own precedence
// (internal/fanout/revieweroutcome.go: failed > unparseable > truncated >
// incomplete > findings > ungrounded > filtered > clean), so two AgentStatus
// entries sharing one agent name cannot let the later, lower-precedence entry
// overwrite the earlier one's outcome. Unknown ("") ranks below everything: it
// is the unclassifiable value, never a reason to discard a known one. The
// unlisted values are spelled as literals here for the same import-cycle reason
// fanout spells them that way; the cli/ parity test pins the agreement.
func outcomeRank(o string) int {
	switch o {
	case "failed":
		return 8
	case "unparseable":
		return 7
	case "truncated":
		return 6
	case "incomplete":
		return 5
	case outcomeFindings:
		return 4
	case outcomeUngrounded:
		return 3
	case outcomeFiltered:
		return 2
	case outcomeClean:
		return 1
	default:
		return 0
	}
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
// It is FAIL-NEUTRAL rather than an error return, and since 2026-09-22 it is no
// longer silent: the rejection is written to the injected diag writer as a
// MsgOutcomeCoerced substring (the MsgMalformedSkip/MsgWriteFailed convention),
// because a record silently dropped from trust scoring for the 180-day window
// with no trace anywhere was undebuggable. EmitForReconcile still has no error
// return by contract — scorecard emission never fails the caller's reconcile —
// so the only two choices remain "write something wrong" and "write unknown".
// Unknown is right: it is excluded from trust scoring downstream, so a
// classifier bug costs a lens nothing, whereas a garbage value persisted for the
// window could not be interpreted at all.
//
// The real classifier cannot return an invalid value (see
// TestFanoutReviewerOutcome_AlwaysReturnsAKnownValue), so this guards the case
// where that stops being true, which is exactly when a guard is worth having.
func coerceOutcome(o string, diag io.Writer) string {
	if !fanout.ValidReviewerOutcome(o) {
		if diag != nil {
			_, _ = fmt.Fprintf(diag, "scorecard: "+MsgOutcomeCoerced+": %q\n", o)
		}
		return ""
	}
	return o
}

// trimmedReviewers normalises a finding's reviewer list once, at the point the
// names enter this package — trimmed AND case-folded via
// normalizeReviewerName, the same function the pool-summary loop keys the
// reviewers map with, dropping blanks.
//
// It exists because the map key and Finding.Reviewers MUST be the same string.
// reviewerCounts matches them with exact-string equality (scorecard.go), so
// normalising only the key silently zeroes a padded or differently-cased
// reviewer's FindingsRaised, its corroboration rate and its skeptic verdicts
// while the record still carries a model, a cost and an outcome and therefore
// still looks healthy. Folding BOTH sides keeps the match and — the reason the
// fold moved here — stops "Bruce" (pool summary) and "bruce" (findings cell)
// from minting two records for one run. A padded or mis-cased name is one
// registry typo away: fanout copies the agent name into the finding verbatim,
// reconcile's merge strips commas but not spaces, and the stream parser assigns
// its reviewer column untrimmed.
//
// Returning a fresh slice rather than editing in place keeps res untouched —
// the caller's reconcile.Result is not this function's to mutate.
func trimmedReviewers(in []string) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		if name := normalizeReviewerName(r); name != "" {
			out = append(out, name)
		}
	}
	return out
}
