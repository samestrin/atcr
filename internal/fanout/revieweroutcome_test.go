package fanout

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

// TestReviewerOutcomePrecedence_CoversTheVocabulary pins the exported order to
// the vocabulary: every non-empty value ValidReviewerOutcome accepts appears
// exactly once, so a new outcome cannot be added to the classifier without a
// rank, where it would silently rank below "clean" in scorecard's dedup.
func TestReviewerOutcomePrecedence_CoversTheVocabulary(t *testing.T) {
	want := []string{"failed", "unparseable", "truncated", "incomplete",
		"findings", "ungrounded", "filtered", "clean"}
	got := ReviewerOutcomePrecedence()
	if len(got) != len(want) {
		t.Fatalf("ReviewerOutcomePrecedence() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ReviewerOutcomePrecedence() = %v, want %v (ReviewerOutcome's switch order)", got, want)
		}
		if !ValidReviewerOutcome(got[i]) {
			t.Errorf("%q is ranked but not in the vocabulary", got[i])
		}
	}
	got[0] = "mutated"
	if ReviewerOutcomePrecedence()[0] != "failed" {
		t.Error("callers must get a copy, not the package's slice")
	}
}

// A chunked persona's refusal is PER BIN, so its outcome must be too. The salvage
// arm keyed on the persona-wide OR-fold (`a.Salvaged`), which internal/fanout/status.go
// documents as unable to say WHICH bin refused — so one refused bin of eight beside
// seven that produced real findings classified the WHOLE persona as "incomplete",
// and internal/scorecard's outcomeEligible excludes "incomplete", dropping the lens's
// entire record from the trust tally. That is the same leak the per-bin refusal was
// built to stop at the findings parser, one layer up (TD internal/scorecard/trust.go:1019).
//
// Only a WHOLE-persona refusal is incomplete: an unchunked salvage whose reply was
// promoted chain-of-thought, or a chunked one whose SalvagedChunks covers every bin.
// SalvagedChunks is already on disk, so this needs no new outcome value — minting one
// would reopen the closed cross-version vocabulary instead.
func TestReviewerOutcome_PartialSalvageStaysEligible(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   AgentStatus
		want string
	}{
		{
			name: "one salvaged bin of eight is not a persona-wide refusal",
			// Seven bins produced real findings; only bin 3 was refused.
			st:   AgentStatus{Status: StatusOK, Salvaged: true, SalvagedChunks: []int{3}, ChunkCount: 8},
			want: "findings",
		},
		{
			name: "salvaged bins covering every bin IS a whole-persona refusal",
			st:   AgentStatus{Status: StatusOK, Salvaged: true, SalvagedChunks: []int{0, 1, 2}, ChunkCount: 3},
			want: "incomplete",
		},
		{
			name: "an unchunked salvage is a whole-persona refusal",
			st:   AgentStatus{Status: StatusOK, Salvaged: true},
			want: "incomplete",
		},
		{
			name: "a salvaged bin beside a failed one still names the incomplete half",
			st:   AgentStatus{Status: StatusOK, Salvaged: true, SalvagedChunks: []int{2}, ChunkCount: 4, UnreviewedChunks: 1},
			want: "incomplete",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// raisedCount is 1 for the partial case: the clean siblings landed findings.
			raised := 0
			if tc.want == "findings" {
				raised = 1
			}
			assert.Equal(t, tc.want, ReviewerOutcome(tc.st, raised))
		})
	}
}
