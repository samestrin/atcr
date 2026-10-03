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
// internal/scorecard's own copy of the four eligible values (trust.go) is
// reachable through scorecard.EligibleOutcomes(), and
// TestScorecardEligibleOutcomes_MatchBenchmarkConstants (cli/fanout_outcome_parity_test.go)
// pins those four directly against the benchmark constants — the TD that asked
// for an exported slice both sides iterate is closed on the scorecard side.
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
	// Salvaged sits with the other data-integrity signals, and BELOW
	// ResponseTruncated for the same reason payload truncation does: a cut-off
	// reply names the sharper cause.
	//
	// It needs its own arm because no signal above sees it. A salvaged reply is
	// StatusOK with content (the client promoted reasoning into it), so the failed
	// and truncated arms miss it; parseFindings refuses it, so it is not
	// unparseable either.
	//
	// WHOLE-PERSONA ONLY, via WholePersonaSalvaged. The refusal is PER BIN —
	// parseFindings skips only the refused bin and keeps its siblings' findings — so
	// keying this arm on the persona-wide OR-fold made one refused bin of eight
	// classify the entire persona "incomplete". internal/scorecard's outcomeEligible
	// excludes "incomplete", so that dropped a lens which produced seven bins of real
	// findings from the trust tally: the same leak the per-bin refusal was built to
	// stop one layer down (TD internal/scorecard/trust.go:1019 and
	// internal/fanout/revieweroutcome.go:90).
	case a.UnreviewedChunks > 0 || a.Truncated || WholePersonaSalvaged(a):
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
	// after the reviewer raised them — `raised` is read from the merged pool
	// findings file (findings.toon, else findings.txt)
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

// WholePersonaSalvaged reports whether a salvaged status cost the persona its WHOLE
// contribution, as opposed to one refused bin beside bins that committed real
// findings.
//
// The distinction is already on disk. `Salvaged` is an OR-fold over a chunked
// persona's bins (internal/fanout/status.go), so it cannot say which bin refused;
// `SalvagedChunks` names them, and `ChunkCount` says how many there were. A salvaged
// status with no bin index is the unchunked persona, whose entire reply is promoted
// chain-of-thought — a whole-persona refusal. A bin index covering every bin is the
// same loss, spelled per bin. Anything less is a PARTIAL loss: the clean siblings'
// findings are parsed, reconciled and shipped, so the persona got a fair attempt and
// must keep its trust standing (TD internal/scorecard/trust.go:1019).
//
// Deliberately no new outcome value. The vocabulary is fail-closed across versions
// at the export boundary (ValidReviewerOutcome, benchmark.ValidOutcome), and the
// per-bin signal already exists — minting a value here would pay that cost for
// information the record already carries.
func WholePersonaSalvaged(a AgentStatus) bool {
	if !a.Salvaged {
		return false
	}
	if len(a.SalvagedChunks) == 0 {
		// No bin index: the unchunked persona, or a chunked one whose refusal the
		// producer could not attribute. Either way nothing narrows it, so it is whole.
		return true
	}
	// A bin index that names every bin is the whole-persona loss. ChunkCount is the
	// denominator; when it is absent the index cannot be compared against a total, so
	// the safe direction is to withhold coverage (trust eligibility) rather than grant
	// it on an unmeasurable claim.
	if a.ChunkCount <= 0 {
		return true
	}
	return len(a.SalvagedChunks) >= a.ChunkCount
}

// ReviewerOutcomePrecedence returns ReviewerOutcome's precedence, highest first
// — the order of its switch, stated once as data so a consumer that must rank
// two outcomes (scorecard's repeated-agent dedup) derives the rank instead of
// re-spelling the order. It excludes OutcomeUnknown (""), which ranks below
// every value. Pinned to the vocabulary by
// TestReviewerOutcomePrecedence_CoversTheVocabulary. Returns a fresh slice.
func ReviewerOutcomePrecedence() []string {
	return []string{"failed", "unparseable", "truncated", "incomplete",
		"findings", "ungrounded", "filtered", "clean"}
}

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

// WholePersonaThinkSuppressed reports whether a think-suppressed status cost the
// persona its WHOLE contribution, as opposed to one bin the strip ate beside bins
// that committed real findings. The think-suppression twin of WholePersonaSalvaged,
// deliberately shaped the same way so the two cannot drift on what "contributed
// nothing" means.
//
// `ThinkSuppressed` is an OR-fold over a chunked persona's bins (chunker.go), so it
// cannot say WHICH bin the strip ate. No per-bin index is needed to find out:
// ThinkSuppressed is documented as set only alongside UnparseableResponse
// (engine.go:420-427), so `UnparseableChunks` already counts the bins that produced
// nothing a parser could use and `ChunkCount` is the denominator. A persona with
// bins left over has signal — their findings are parsed, reconciled and shipped —
// so it got a fair attempt and must keep its score and its trust standing.
//
// Reusing those two fields rather than minting a ThinkSuppressedChunks index is the
// point: the record already carries the answer, and a new status.json key would pay
// a cross-version compatibility cost for information that is already on disk.
//
// Fail-closed when the denominator is absent, exactly as WholePersonaSalvaged is:
// an index that cannot be compared against a total is an unmeasurable claim, and the
// safe direction is to withhold coverage rather than grant it (TD
// internal/scorecard/trust.go:1019).
func WholePersonaThinkSuppressed(a AgentStatus) bool {
	if !a.ThinkSuppressed {
		return false
	}
	if a.ChunkCount <= 0 {
		// No denominator: the unchunked persona, whose entire reply the strip
		// consumed. Either way nothing narrows it, so it is whole.
		return true
	}
	return a.UnparseableChunks >= a.ChunkCount
}
