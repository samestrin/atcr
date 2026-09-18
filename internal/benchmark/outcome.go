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

	// OutcomeIncomplete marks a reviewer that saw only a FRACTION of the diff,
	// by either route its INPUT can be cut short:
	//   - chunking: fanout.AgentStatus.UnreviewedChunks counts the bins that failed
	//     while the persona still reported StatusOK;
	//   - payload truncation: fanout.AgentStatus.Truncated marks a byte-budget shed,
	//     by any of THREE routes — a per-agent shed to fit the model's window, a
	//     re-fit fallback under on_overflow=truncate, or the DIFF-WIDE shed
	//     (buildPayloads' global pass) promoted onto a chunked persona's merged
	//     record, which is the only one decided before the persona was sized at all —
	//     with FilesDropped naming the shed ENTRIES by path.
	//     Not "the paths that never arrived": the shed is accounted per entry and a
	//     diff can carry two sections for one path, so a listed path may still be
	//     present via its other occurrence (fanout.droppedPathsExcept documents the
	//     index-keyed contract).
	// Publishing either case as "clean" would assert "reviewed the whole diff and
	// correctly found nothing" about a reviewer that never saw most of it — a
	// positive false claim, the exact class this vocabulary exists to prevent.
	// Data-integrity, same class as truncated. Note OutcomeTruncated is the
	// OUTPUT-side signal (finish_reason "length") and is a distinct value.
	OutcomeIncomplete = "incomplete"

	// OutcomeUngrounded marks a reviewer that RAISED findings and had every one of
	// them discarded by the Epic 14.1 grounding gate — each cited a FILE:LINE the
	// patch does not contain (fanout.AgentStatus.DroppedByGrounding > 0 with nothing
	// surviving).
	//
	// It is its own value rather than a reuse of OutcomeIncomplete, which is the
	// INPUT-side signal: "saw only a fraction of the diff". This reviewer saw the
	// whole diff. Its OUTPUT was filtered afterwards, which is the opposite
	// direction, and folding the two together would make the incomplete doc above
	// false for half the rows carrying it.
	//
	// Publishing it as "clean" is the failure this value exists to prevent: clean
	// asserts "reviewed and correctly found nothing", and a reviewer that found
	// something the gate then rejected has not made that claim. The distinction is
	// the whole measurement on the repo-state-v1 tier, where the gate is live and an
	// out-of-diff finding survives only when pre-fetching retrieved the cited span.
	//
	// CROSS-VERSION NOTE: this value is new, so ValidOutcome in an OLDER binary
	// rejects a checkpoint carrying it. That is the fail-closed direction the
	// vocabulary is designed for — a stale reader refuses rather than silently
	// re-keying the tally — but it does mean a checkpoint written here cannot be
	// resumed by a pre-35.16.10 build.
	OutcomeUngrounded = "ungrounded"

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
