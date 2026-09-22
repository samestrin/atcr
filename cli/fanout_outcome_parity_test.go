package cli

import (
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is the drift pin sprint 36.0 clarification C6 requires, and it is
// load-bearing rather than decorative.
//
// internal/benchmark imports internal/scorecard AND internal/fanout, so neither
// of those packages can import internal/benchmark back — the classifier
// (fanout.ReviewerOutcome), its validator (fanout.ValidReviewerOutcome) and the
// trust-side eligibility allowlist all therefore spell the outcome vocabulary as
// plain string literals. internal/benchmark/outcome.go stays the vocabulary's
// single definition and is not edited.
//
// package cli is a legal importer of both, so this is the one place the two
// spellings can be compared. Without it the duplication is unguarded: renaming
// a benchmark.Outcome* VALUE would silently split the classifier from the
// vocabulary it claims to speak, and every other test in the repo would stay
// green while trust scoring quietly admitted nothing.

// TestFanoutOutcomeLiterals_MatchBenchmarkConstants pins the literal VALUES, not
// merely that both sides are non-empty.
func TestFanoutOutcomeLiterals_MatchBenchmarkConstants(t *testing.T) {
	tests := []struct {
		name   string
		status fanout.AgentStatus
		raised []string
		want   string
	}{
		{"failed", fanout.AgentStatus{Status: "error"}, nil, benchmark.OutcomeFailed},
		{"failed via error string", fanout.AgentStatus{Status: fanout.StatusOK, Error: "timeout"}, nil, benchmark.OutcomeFailed},
		{"unparseable", fanout.AgentStatus{Status: fanout.StatusOK, UnparseableResponse: true}, nil, benchmark.OutcomeUnparseable},
		{"truncated", fanout.AgentStatus{Status: fanout.StatusOK, ResponseTruncated: true}, []string{"correctness"}, benchmark.OutcomeTruncated},
		{"incomplete via unreviewed chunks", fanout.AgentStatus{Status: fanout.StatusOK, UnreviewedChunks: 2}, nil, benchmark.OutcomeIncomplete},
		{"incomplete via payload truncation", fanout.AgentStatus{Status: fanout.StatusOK, Truncated: true}, nil, benchmark.OutcomeIncomplete},
		{"findings", fanout.AgentStatus{Status: fanout.StatusOK}, []string{"correctness"}, benchmark.OutcomeFindings},
		{"ungrounded", fanout.AgentStatus{Status: fanout.StatusOK, DroppedByGrounding: 2}, nil, benchmark.OutcomeUngrounded},
		{"filtered", fanout.AgentStatus{Status: fanout.StatusOK, DroppedByMinSeverity: 2}, nil, benchmark.OutcomeFiltered},
		{"clean", fanout.AgentStatus{Status: fanout.StatusOK}, nil, benchmark.OutcomeClean},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, fanout.ReviewerOutcome(tc.status, tc.raised))
		})
	}
}

// TestFanoutReviewerOutcome_AlwaysReturnsAKnownValue is the structural half: for
// any reachable AgentStatus shape the classifier returns a member of the
// vocabulary, never an ad-hoc string. It is what lets scorecard's write-time
// guard be a validity check rather than a parse.
func TestFanoutReviewerOutcome_AlwaysReturnsAKnownValue(t *testing.T) {
	shapes := []fanout.AgentStatus{
		{},
		{Status: fanout.StatusOK},
		{Status: "error"},
		{Status: fanout.StatusOK, UnparseableResponse: true},
		{Status: fanout.StatusOK, ResponseTruncated: true},
		{Status: fanout.StatusOK, UnreviewedChunks: 1},
		{Status: fanout.StatusOK, Truncated: true},
		{Status: fanout.StatusOK, DroppedByGrounding: 1},
		{Status: fanout.StatusOK, DroppedByMinSeverity: 1},
		{Status: fanout.StatusOK, DroppedByGrounding: 1, DroppedByMinSeverity: 1},
	}
	for _, s := range shapes {
		for _, raised := range [][]string{nil, {"correctness"}} {
			got := fanout.ReviewerOutcome(s, raised)
			assert.True(t, benchmark.ValidOutcome(got),
				"classifier returned %q, which is outside benchmark's vocabulary", got)
			assert.NotEqual(t, benchmark.OutcomeUnknown, got,
				"a classified reviewer is never unknown; unknown means nobody classified it")
		}
	}
}

// TestFanoutValidReviewerOutcome_AgreesWithBenchmarkValidOutcome pins the
// replacement C6 names for AC 02-03's benchmark.ValidOutcome call. The two
// predicates must accept and reject exactly the same set, including the empty
// string: OutcomeUnknown IS a valid stored value (it means "nobody classified
// this"), and rejecting it here would make the write-time guard coerce a
// legitimate unknown into... unknown, masking a real bug behind a no-op.
func TestFanoutValidReviewerOutcome_AgreesWithBenchmarkValidOutcome(t *testing.T) {
	// Derived from the shipped vocabulary, not a hand-typed literal: a Len over a
	// slice literal declared three lines up is a tautology — it cannot notice a
	// tenth value. AllOutcomes() makes the count track internal/benchmark, so a
	// tenth value cannot be added without this site changing (the decision the
	// message demands).
	known := benchmark.AllOutcomes()
	require.Len(t, known, 9, "the vocabulary is nine values; a tenth needs a decision here")

	for _, s := range known {
		assert.True(t, benchmark.ValidOutcome(s), "precondition: %q is benchmark-valid", s)
		assert.True(t, fanout.ValidReviewerOutcome(s),
			"fanout rejects %q, which benchmark accepts — the literals have drifted", s)
	}

	for _, s := range []string{"banana", "FINDINGS", "clean ", "0", "unknown"} {
		assert.False(t, benchmark.ValidOutcome(s), "precondition: %q is benchmark-invalid", s)
		assert.False(t, fanout.ValidReviewerOutcome(s),
			"fanout accepts %q, which benchmark rejects — the literals have drifted", s)
	}
}
