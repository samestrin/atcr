package benchmark

// The per-SLOT infrastructure-failure vocabulary: what went wrong when one
// reviewer could not be shown one case that the rest of the panel reviewed fine.
//
// IT IS A THIRD AXIS, distinct from both Outcome* and CaseFailure*, and the three
// answer different questions:
//
//   - Outcome* describes what a reviewer slot PRODUCED on a case it was shown.
//   - CaseFailure* describes a case NO reviewer could be shown — the whole panel
//     missed it, so the case is unmeasured for everyone.
//   - SlotFailure* describes ONE reviewer missing ONE case the others got. The case
//     is measured; that reviewer's row is short by it.
//
// The middle axis cannot express the third. CaseFailures is validated at export
// against "no reviewer scored this case" (validateCaseFailures' scored-and-failed
// arm), which is exactly false here — the surviving reviewers did score it. Filing a
// slot failure there would make every such run read as malformed.
//
// WHY IT EXISTS AT ALL. The runner skips a non-OK slot from the score, the covered
// set and the outcome tally together, so the reviewer is charged nothing for a case
// it was never shown. That is correct, and it left the shortfall with no cause
// recorded anywhere: the run exited 0 with no warning, the deferred cleanup took the
// clean-run branch and deleted the review dirs holding each slot's status.json, and
// `benchmark export` then rejected the finished run-result as short while labelling
// the gap "missing" — the label reserved for a truncated or hand-assembled file.
//
// RUN-RESULT ONLY, like CaseFailures and for the same reason: it explains a
// shortfall to an operator, and answers no question the public board scores.
// BuildSubmission does not carry it, locked by
// TestBuildSubmission_DoesNotPublishSlotFailures.
//
// The values mirror fanout's agent status enum, which has exactly three members
// (StatusOK, StatusFailed, StatusTimeout) — the skip fires on the two that are not
// OK. They are NAMESPACED rather than reusing those spellings: bare "failed" is
// already OutcomeFailed, and the two vocabularies share a run-result document, so a
// shared spelling would invite the conflation TestSlotFailureReasonsDoNotCollide
// exists to prevent.
const (
	// SlotFailureCall marks a slot whose LLM call did not succeed — transport, HTTP,
	// or auth error after fallback resolution (fanout.StatusFailed).
	SlotFailureCall = "call_failed"

	// SlotFailureTimeout marks a slot whose call hit the deadline or was cancelled
	// (fanout.StatusTimeout). fanout maps context.Canceled here too, so an operator
	// interrupt that races one slot lands on this value rather than on
	// SlotFailureCall.
	SlotFailureTimeout = "call_timeout"

	// SlotFailureUnknownStatus marks a slot whose status this build does not
	// recognize — a value fanout's enum grew after this binary was built.
	//
	// It exists so the channel never invents a reason it cannot justify. The
	// alternative, mapping an unknown status onto SlotFailureCall, would assert a
	// transport failure about a slot that may have failed some other way, and the
	// export validator would accept it because the spelling is legal.
	SlotFailureUnknownStatus = "call_status_unknown"
)

// SlotFailure records one reviewer that could not be shown one case, and why.
//
// The identity is the PUBLIC (post-scrub) one, so it joins to the reviewer_coverage
// row it explains. Recording the pre-scrub key instead would produce a channel that
// names identities appearing nowhere else in the document — the coverage rows are
// emitted scrubbed, and the export diagnostic that reads this array matches on them.
//
// It carries no error text, for the reason CaseFailure gives: a Go error on this path
// routinely embeds a provider message or a $TMPDIR path, and a run-result is a file
// operators hand to other people. The full error is logged at the failure site.
type SlotFailure struct {
	Model   string `json:"model"`
	Persona string `json:"persona"`
	CaseID  string `json:"case_id"`
	Reason  string `json:"reason"`
}

// ValidSlotFailureReason reports whether s is a value the slot-failure vocabulary
// can legitimately STORE. It is the export trust boundary for this channel, the role
// ValidCaseFailureReason plays for the case-level one.
//
// The empty string is REJECTED on the same grounds: the array is new and omitempty,
// every element is written by the producer together with its reason, so an element
// carrying none cannot have come from the producer.
func ValidSlotFailureReason(s string) bool {
	switch s {
	case SlotFailureCall, SlotFailureTimeout, SlotFailureUnknownStatus:
		return true
	}
	return false
}

// SlotFailureReasonForStatus maps a fanout agent status to the reason recorded for a
// slot skipped on it.
//
// Total by construction: an unrecognized status maps to SlotFailureUnknownStatus
// rather than to a default that would misattribute it. StatusOK has no mapping
// because an OK slot is never skipped — callers must not reach here with one.
func SlotFailureReasonForStatus(status string) string {
	switch status {
	case "failed":
		return SlotFailureCall
	case "timeout":
		return SlotFailureTimeout
	}
	return SlotFailureUnknownStatus
}
