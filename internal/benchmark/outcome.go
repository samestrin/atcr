package benchmark

// The reviewer OUTCOME vocabulary: what actually happened when one reviewer met one
// case. It exists because a zero-finding result is otherwise ambiguous — a reviewer
// that read the diff and correctly found nothing, one that emitted prose no parser
// could use, and one whose call failed outright all record zero raised categories and
// therefore score identically. Identical scores, opposite meanings.
//
// The signal is not missing from the system; it is computed upstream and was
// discarded before scoring. fanout stamps UnparseableResponse, ResponseTruncated and
// the fallback triple onto each AgentStatus; the benchmark simply reads them at the
// existing pool-summary loop.
//
// It is a STRING ENUM, not a pair of booleans, for a reason that is easy to get
// wrong: two booleans (clean, unparseable) admit an impossible state, and — worse —
// both default to false. A checkpoint written before this vocabulary existed would
// then replay its zero-finding cases as "reviewed cleanly and found nothing", which
// is the exact false claim this vocabulary exists to prevent. Absence must be
// representable, and absence must not be clean. OutcomeUnknown is that absence, and
// it is the empty string so an older checkpoint decodes into it for free.
const (
	// OutcomeUnknown marks a case whose outcome was never recorded — a checkpoint
	// written before this field existed. It is NOT a claim about the review; it is
	// the explicit absence of one, and must never be read as OutcomeClean.
	OutcomeUnknown = ""

	// OutcomeFindings marks a reviewer that raised at least one parseable finding.
	OutcomeFindings = "findings"

	// OutcomeClean marks a reviewer that reviewed successfully and positively
	// signalled no findings (the clean-review sentinel). stream.IsNoFindings is the
	// authority on that sentinel and fanout already applies it — deliberately
	// excluding the sentinel from UnparseableResponse, because flagging it would
	// mark every clean review anomalous and destroy this very distinction. Consume
	// that decision; do not re-derive it against raw content.
	OutcomeClean = "clean"

	// OutcomeUnparseable marks a reviewer that returned content from which zero
	// findings could be parsed and which was not the clean-review sentinel. Not a
	// failure — fanout deliberately does not fail it over, since that would spend
	// the backup model on every plausible clean review — but not a clean review
	// either.
	OutcomeUnparseable = "unparseable"

	// OutcomeTruncated marks a response cut off on finish_reason "length" (the
	// model's output budget was exhausted mid-answer). Whatever it raised is
	// incomplete by construction.
	OutcomeTruncated = "truncated"

	// OutcomeIncomplete marks a chunked reviewer that saw only a FRACTION of the
	// diff: fanout.AgentStatus.UnreviewedChunks counts the bins that failed while
	// the persona still reported StatusOK. Publishing such a case as "clean" would
	// assert "reviewed the whole diff and correctly found nothing" about a reviewer
	// that never saw most of it — a positive false claim, the exact class this
	// vocabulary exists to prevent. Data-integrity, same class as truncated.
	OutcomeIncomplete = "incomplete"

	// OutcomeFailed marks a slot whose call did not succeed at all — the reviewer
	// never produced a reviewable response for this case.
	OutcomeFailed = "failed"
)

// OutcomeUnknownLabel is how OutcomeUnknown is spelled in a TALLY, as distinct from
// on the wire.
//
// OutcomeUnknown must be the empty string where it is stored, so a checkpoint written
// before the field existed decodes into it for free. But a tally is a JSON OBJECT
// keyed by outcome, and the empty string is a legal-but-awful key: a pre-epic run
// would serialize as {"": 17}, which a consumer renders as a blank label and cannot
// tell from a corrupt entry. OutcomeTallyKey maps the wire value to this label so the
// published artifact always names what it means.
const OutcomeUnknownLabel = "unknown"

// ValidOutcome reports whether s is a value the outcome vocabulary can legitimately
// STORE — one of the Outcome* wire values above, or OutcomeUnknown (the empty
// string, a pre-vocabulary checkpoint's recorded absence). It exists for the
// checkpoint-resume trust boundary: a checkpoint is operator-supplied JSON, and
// without this gate an arbitrary string in its outcome field would fold through
// OutcomeTallyKey into an arbitrary key in the published run-result.
//
// OutcomeUnknownLabel ("unknown") is deliberately NOT valid here: it is a TALLY
// spelling, never a stored one. Accepting it would make a fabricated outcome
// indistinguishable from genuine absence — the one distinction the enum exists to
// protect.
func ValidOutcome(s string) bool {
	switch s {
	case OutcomeUnknown, OutcomeFindings, OutcomeClean,
		OutcomeUnparseable, OutcomeTruncated, OutcomeIncomplete, OutcomeFailed:
		return true
	}
	return false
}

// OutcomeTallyKey maps a stored outcome value to the key it is tallied under.
func OutcomeTallyKey(outcome string) string {
	if outcome == OutcomeUnknown {
		return OutcomeUnknownLabel
	}
	return outcome
}
