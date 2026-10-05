package benchmark

import "testing"

// The slot-failure vocabulary carries two genuinely different facts under one array.
// Three members mean the infrastructure LOST the slot (a failed call, a timeout, a
// status this build cannot read). The fourth, SlotFailureUnmeasuredOK, means the
// opposite: the call SUCCEEDED and what came back was worthless. Its own doc says so
// — "the call succeeded".
//
// Every consumer that counts the array as a failure tally therefore needs to ask
// which half an entry belongs to. Without this predicate each of them re-derives the
// split, or (as cli/benchmark.go:311 did) assumes there is no split at all and exits
// non-zero on a healthy panel.
func TestSlotFailureIsInfrastructure(t *testing.T) {
	for reason, want := range map[string]bool{
		SlotFailureCall:          true,
		SlotFailureTimeout:       true,
		SlotFailureUnknownStatus: true,
		SlotFailureUnmeasuredOK:  false,
		// Not a member of the vocabulary at all. ValidSlotFailureReason rejects it at
		// the export boundary, and this predicate must not vouch for it either: an
		// unreadable reason is not evidence of an infrastructure failure.
		"":             false,
		"not_a_reason": false,
	} {
		if got := SlotFailureIsInfrastructure(reason); got != want {
			t.Errorf("SlotFailureIsInfrastructure(%q) = %v, want %v", reason, got, want)
		}
	}
}

// Total coverage of the vocabulary: every reason ValidSlotFailureReason accepts must
// get an answer from this predicate, so a member added later cannot slip through
// un-classified and be silently counted as a failure by every consumer.
func TestSlotFailureIsInfrastructure_ClassifiesEveryStorableReason(t *testing.T) {
	infra, unmeasured := 0, 0
	for _, r := range []string{SlotFailureCall, SlotFailureTimeout, SlotFailureUnknownStatus, SlotFailureUnmeasuredOK} {
		if !ValidSlotFailureReason(r) {
			t.Fatalf("%q is not storable; the vocabulary list in this test is stale", r)
		}
		if SlotFailureIsInfrastructure(r) {
			infra++
		} else {
			unmeasured++
		}
	}
	if infra != 3 || unmeasured != 1 {
		t.Errorf("vocabulary split = %d infrastructure / %d unmeasured, want 3/1; a new reason needs a "+
			"deliberate side and a consumer review, not a default", infra, unmeasured)
	}
}
