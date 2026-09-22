package fanout

import "testing"

// TestValidReviewerOutcome_AcceptsExactlyReviewerOutcomesShapeSet is the
// reverse-direction check cli/fanout_outcome_parity_test.go cannot run from
// inside this package: it builds the exact set of values ReviewerOutcome can
// produce (plus the empty string, OutcomeUnknown), then asserts
// ValidReviewerOutcome accepts every one of them and rejects a set of
// near-miss strings. A tenth literal added to either switch fails here even
// though it would pass a test that only samples a handful of strings.
func TestValidReviewerOutcome_AcceptsExactlyReviewerOutcomesShapeSet(t *testing.T) {
	shapes := []AgentStatus{
		{Status: "error"},
		{Status: StatusOK, Error: "timeout"},
		{Status: StatusOK, UnparseableResponse: true},
		{Status: StatusOK, ResponseTruncated: true},
		{Status: StatusOK, UnreviewedChunks: 1},
		{Status: StatusOK, Truncated: true},
		{Status: StatusOK, DroppedByGrounding: 1},
		{Status: StatusOK, DroppedByMinSeverity: 1},
		{Status: StatusOK},
	}

	// OutcomeUnknown ("") is a legitimate stored value meaning "nobody
	// classified this run" — ReviewerOutcome itself never returns it.
	accepted := map[string]bool{"": true}
	for _, s := range shapes {
		for _, raisedCount := range []int{0, 1} {
			accepted[ReviewerOutcome(s, raisedCount)] = true
		}
	}
	if len(accepted) != 9 {
		t.Fatalf("expected 9 accepted outcomes (8 classifier outputs + unknown), got %d: %v", len(accepted), accepted)
	}

	for s := range accepted {
		if !ValidReviewerOutcome(s) {
			t.Errorf("ValidReviewerOutcome(%q) = false, want true (produced by ReviewerOutcome or is OutcomeUnknown)", s)
		}
	}

	for _, s := range []string{"banana", "FINDINGS", "clean ", "0", "unknown", "skipped", " "} {
		if accepted[s] {
			t.Fatalf("test bug: negative probe %q collides with an accepted outcome", s)
		}
		if ValidReviewerOutcome(s) {
			t.Errorf("ValidReviewerOutcome(%q) = true, want false — not a value ReviewerOutcome produces or OutcomeUnknown", s)
		}
	}
}
