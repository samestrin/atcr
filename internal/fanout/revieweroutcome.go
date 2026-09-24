package fanout

// NOTE ON THE FILENAME: outcome.go in this package is already taken, and by a
// DIFFERENT meaning of the word — it holds the run-level Summary and the
// all-agents-failed gate. What lives here is the per-reviewer classification of
// what one lens produced on one case. The two are unrelated; keeping them in
// separate files keeps that obvious.
//
// WHY THE VALUES ARE STRING LITERALS AND NOT benchmark.Outcome* CONSTANTS:
// internal/benchmark imports this package, so importing it back would close a
// cycle. internal/benchmark/outcome.go remains the vocabulary's single
// definition and is not edited; this file speaks the same nine values by value.
// That duplication is pinned — not merely hoped for — by
// cli/fanout_outcome_parity_test.go, which is a legal importer of both packages
// and asserts every literal in THIS file equals its benchmark.Outcome*
// counterpart. If that test is deleted, this file can drift in silence.
//
// It does NOT reach internal/scorecard's own copy of the four eligible values
// (trust.go), which are unexported and invisible to cli. Those are pinned only
// indirectly, by internal/scorecard's independently-written literals plus its
// coercion test. Filed as TD: the durable fix is for internal/benchmark to
// export the vocabulary as a slice both sides iterate, so a tenth value cannot
// be added without every site changing.
//
// Relocated here from cli/benchmark_run.go by sprint 36.0 (AC 02-02) so the
// benchmark path and the reconcile path classify identically instead of one
// re-implementing the other. internal/fanout is the required home rather than
// internal/benchmark: internal/benchmark already imports internal/scorecard from
// four files, so exporting the classifier from there would close a
// scorecard -> benchmark -> scorecard cycle the moment scorecard called it.
// internal/fanout imports neither, which is what makes it the safe leaf.

// ReviewerOutcome classifies what actually happened when one reviewer met one case,
// reading signals fanout already computed and stamped onto the AgentStatus. Nothing
// here re-derives them: UnparseableResponse in particular encodes a decision about
// the clean-review sentinel (stream.IsNoFindings) that must not be re-implemented
// against raw content, because excluding the sentinel is exactly what preserves the
// clean-vs-garbage distinction.
//
// PRECEDENCE — failed > unparseable > truncated > incomplete > findings > ungrounded
// > filtered > clean.
// The signals are not mutually exclusive on the wire (a truncated response can also
// raise findings; a failed slot has no findings either way), so the order is a
// decision rather than an implication, and this switch is its single statement of
// record.
//
// Data-integrity signals outrank volume signals throughout. A truncated response that
// raised five findings reports "truncated", not "findings", because the
// incompleteness is the load-bearing fact about that row — the five categories it did
// raise are still recorded in the score, so nothing is lost by saying so. A reviewer
// whose INPUT was cut short reports "incomplete" for the same reason: it may have
// raised nothing, but only about the fraction it read. Both routes to a partial input
// map to that one value — a chunked persona whose bins failed (UnreviewedChunks) and a
// byte-budget shed of the payload itself (Truncated, with FilesDropped naming the
// shed entries by path). Reusing "incomplete" for the second route rather than minting
// a value for it is deliberate: the vocabulary is fail-closed at the checkpoint and
// coverage trust boundaries, so an older binary reading a newer run's outcome must find
// a value it already knows.
//
// That is a statement about THOSE TWO ROUTES, not a rule against new values — the
// ungrounded and filtered arms below both mint one, and took the opposite side of the
// same tradeoff knowingly. The difference is what the reuse would cost: a chunked
// persona's failed bins and a shed payload are the same fact about a row (it saw a
// fraction of the input), so one value describes both without lying, whereas publishing
// a gate-wiped reviewer as "clean" asserts something false. Where reuse is free, take
// it; where it would falsify the row, pay the skew instead — and say so, which is what
// the CROSS-VERSION NOTEs on internal/benchmark/outcome.go's OutcomeUngrounded and
// OutcomeFiltered constants do (cited by name, not line — every insertion into
// outcome.go's doc blocks would otherwise re-aim a line-number citation).
func ReviewerOutcome(a AgentStatus, raisedCount int) string {
	switch {
	case a.Status != StatusOK || a.Error != "":
		return "failed"
	case a.UnparseableResponse:
		return "unparseable"
	case a.ResponseTruncated:
		return "truncated"
	// A ledger-shed reviewer — len(a.FilesDropped) > 0 with a.Truncated false —
	// is deliberately NOT caught by this arm; it falls through toward
	// findings/ungrounded/filtered/clean instead. status.go's AgentStatus doc
	// (Truncated answers "was REVIEWABLE content dropped"; FilesDropped is the
	// one that names a ledger-only shed) and review.go's keepSmallestEntry note
	// (dropping the exempt ledger while keeping the file "still reads
	// Truncated=false", a deliberate trade) both treat that pair as a valid
	// record of a REVIEWABLE-complete run, not a corrupt one: the claim ledger
	// is exempt from every shed precisely because its absence says nothing
	// about how much of the reviewable payload the agent actually saw.
	// budget.go separately calls identical ledger delivery load-bearing for
	// fairness across reviewers — that is a real, still-open tradeoff this arm
	// does not resolve, only chooses not to reclassify as incomplete.
	case a.UnreviewedChunks > 0 || a.Truncated:
		return "incomplete"
	case raisedCount > 0:
		return "findings"
	// Below here the reviewer raised nothing that survived. A non-zero grounding
	// drop count is what separates "found nothing" from "found things the Epic 14.1
	// gate rejected" — the two shapes are otherwise identical at this call site
	// (StatusOK, UnparseableResponse false, zero categories), which is exactly how
	// the second one used to publish as clean.
	//
	// It sits BELOW findings deliberately: a reviewer that raised four and kept one
	// reviewed successfully and has a finding to show for it, so only a total wipe
	// is the ungrounded outcome. It sits below the data-integrity signals for the
	// same reason they outrank each other — a failed call's drop count says nothing
	// about the review.
	//
	// Unreachable on the standard-v1 diff path: that path supplies no Range, so
	// groundFindings fails open and DroppedByGrounding is always 0. No historical
	// standard-v1 row changes outcome.
	case a.DroppedByGrounding > 0:
		return "ungrounded"
	// The grounding gate's SIBLING, and the wider of the two. Both discard findings
	// after the reviewer raised them — `raised` is read from the merged findings.txt
	// written after enforceConstraints — so both leave a reviewer that found things
	// looking identical here to one that found nothing. Grounding is repo-state-only;
	// min_severity is any registry agent on either tier (internal/fanout/engine.go,
	// loop.go), so this arm is reachable where the one above never fires.
	//
	// It sits BELOW ungrounded, and that ordering is a decision rather than an
	// implication: the two counters can both be non-zero on one row, and only one
	// value can be published. Grounding wins because it answers whether the reviewer
	// cited code the patch actually contains — the measurement the repo-state tier
	// exists for — whereas the floor is an operator preference applied to whatever
	// survived that gate. Pinned by
	// TestReviewerOutcome_GroundingOutranksMinSeverityWhenBothFire.
	case a.DroppedByMinSeverity > 0:
		return "filtered"
	default:
		return "clean"
	}
}

// ReviewerOutcomePrecedence is a stub.
func ReviewerOutcomePrecedence() []string { return nil }

// ValidReviewerOutcome reports whether s is a member of the outcome vocabulary,
// including the empty string — OutcomeUnknown is a legitimate STORED value
// meaning "nobody classified this run", distinct from a corrupt one.
//
// It is the replacement for benchmark.ValidOutcome at call sites that cannot
// import internal/benchmark (see the package note above), and it exists beside
// ReviewerOutcome deliberately: producer and validator over one closed
// vocabulary belong in one file, so a tenth value is one edit rather than two
// that can quietly disagree. Its agreement with benchmark.ValidOutcome is pinned
// by cli/fanout_outcome_parity_test.go.
func ValidReviewerOutcome(s string) bool {
	switch s {
	case "", "findings", "clean", "unparseable", "truncated",
		"incomplete", "ungrounded", "filtered", "failed":
		return true
	}
	return false
}
