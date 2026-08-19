package cli

import (
	"bytes"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RoutingValues is the discriminator that closes the all-`other` blind spot, and the
// export gate never looked at it — not for negativity, not against its own
// denominator. The one predicate that reads it (warnRoutingOnlyReviewers) tests exact
// equality with Findings, so a row claiming MORE routing values than findings is both
// arithmetically impossible AND silently suppresses the warning it should trigger.
//
// The gate already rejects exactly this shape for Drifted; RoutingValues is the same
// kind of count against the same denominator and gets the same treatment.
func TestValidateReviewerVocabulary_RejectsImpossibleRoutingValues(t *testing.T) {
	cases := []struct {
		name string
		row  benchmark.ReviewerVocabulary
		want string
	}{
		{
			name: "negative routing values",
			row:  benchmark.ReviewerVocabulary{Model: "m", Persona: "p", Findings: 4, Drifted: 0, Rate: vocabRate(0), RoutingValues: -1},
			want: "routing_values",
		},
		{
			name: "routing values exceed findings",
			row:  benchmark.ReviewerVocabulary{Model: "m", Persona: "p", Findings: 4, Drifted: 0, Rate: vocabRate(0), RoutingValues: 5},
			want: "routing_values",
		},
		{
			name: "every finding both drifted and routed is a contradiction",
			// Routing values are taxonomy MEMBERS, so a routed finding is by definition
			// in vocabulary and cannot also be drift. Both counts at the denominator
			// therefore describe no run.
			row:  benchmark.ReviewerVocabulary{Model: "m", Persona: "p", Findings: 4, Drifted: 4, Rate: vocabRate(1), RoutingValues: 4},
			want: "cannot both",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := benchmark.RunResult{Vocabulary: []benchmark.ReviewerVocabulary{c.row}}
			err := validateReviewerVocabulary(&bytes.Buffer{}, rr, "run.json")
			require.Error(t, err, "a row that describes no run must be rejected at the publication gate")
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

// The negative-count rejection labels its pair "findings/drifted" and has to render them
// in that order. The two sibling messages added in the same block get their order right,
// which is exactly what makes a transposed one read as authoritative: an operator
// debugging a hand-written file is told the wrong field is negative and looks in the
// wrong place.
//
// Asserted on the rendered NUMBERS, not on the message prefix — a prefix assertion cannot
// see an argument swap, which is why this survived.
func TestValidateReviewerVocabulary_NegativeCountMessageMatchesItsOwnLabel(t *testing.T) {
	rr := benchmark.RunResult{Vocabulary: []benchmark.ReviewerVocabulary{
		{Model: "m", Persona: "p", Findings: -3, Drifted: 0, RoutingValues: 0},
	}}

	err := validateReviewerVocabulary(&bytes.Buffer{}, rr, "run.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "(-3/0)",
		"the label reads findings/drifted, so findings (-3) must render first; "+
			"printing (0/-3) tells the operator that drifted is the negative one")
}

// The quotient tolerance's own comment promises it "admits a hand-assembled but honest
// file that rounded the value (1/3 as 0.333333)" — true only at six decimal places. A
// two-decimal rounding is the normal shape for a hand-authored file, which the docs
// explicitly invite, and 1e-6 rejects the WHOLE submission for it.
//
// The same function argues a length/order mismatch must only warn "because nothing on
// this path reads it today". That argument applies identically here: this array gates
// nothing and is not carried into the submission.
func TestValidateReviewerVocabulary_AdmitsAHandRoundedRate(t *testing.T) {
	rr := benchmark.RunResult{Vocabulary: []benchmark.ReviewerVocabulary{
		{Model: "m", Persona: "p", Findings: 3, Drifted: 1, Rate: vocabRate(0.33), RoutingValues: 0},
	}}

	require.NoError(t, validateReviewerVocabulary(&bytes.Buffer{}, rr, "run.json"),
		"0.33 is 1/3 rounded to the precision a human writes; rejecting the whole "+
			"submission for it fails a legitimate hand-authored file")
}

// A rate that genuinely describes no run must still be rejected — loosening the
// tolerance must not disarm the check.
func TestValidateReviewerVocabulary_StillRejectsARateThatDescribesNoRun(t *testing.T) {
	rr := benchmark.RunResult{Vocabulary: []benchmark.ReviewerVocabulary{
		{Model: "m", Persona: "p", Findings: 3, Drifted: 1, Rate: vocabRate(0.9), RoutingValues: 0},
	}}

	err := validateReviewerVocabulary(&bytes.Buffer{}, rr, "run.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match its own counts")
}

// The tolerance's stated purpose is to "admit TWO-decimal rounding, which is the
// precision a human actually writes". A two-decimal rounding's worst case is an error of
// EXACTLY 0.005, and a strict `>` against 5e-3 rejects it — in float64 the difference
// lands a few ULP above the bound. Every eighth-denominator quotient hits that worst
// case, so for `findings: 8` there was NO two-decimal value a hand-author could legally
// write: 1/8 rejected both 0.12 and 0.13.
//
// `findings: 8` is the value in docs/benchmark.md's own canonical example, and that doc
// explicitly sanctions supplying a run-result by hand — so the boundary is on the path
// the gate is most likely to meet.
func TestValidateReviewerVocabulary_AdmitsATwoDecimalRoundingAtTheExactBoundary(t *testing.T) {
	for _, rate := range []float64{0.12, 0.13} {
		rr := benchmark.RunResult{Vocabulary: []benchmark.ReviewerVocabulary{
			{Model: "m", Persona: "p", Findings: 8, Drifted: 1, Rate: vocabRate(rate), RoutingValues: 0},
		}}
		require.NoError(t, validateReviewerVocabulary(&bytes.Buffer{}, rr, "run.json"),
			"1/8 is 0.125; %v is it rounded to two decimals in one of the only two "+
				"directions available, and both must be admitted", rate)
	}
}

// The accept side pinned against the CONSTANT rather than a literal, so a future
// re-derivation has to move this test with it: a difference of exactly one tolerance is
// the two-decimal worst case, and is the largest error the gate promises to admit.
func TestValidateReviewerVocabulary_AdmitsADifferenceOfExactlyOneTolerance(t *testing.T) {
	rr := benchmark.RunResult{Vocabulary: []benchmark.ReviewerVocabulary{
		{Model: "m", Persona: "p", Findings: 4, Drifted: 1, Rate: vocabRate(0.25 + vocabularyRateTolerance), RoutingValues: 0},
	}}
	require.NoError(t, validateReviewerVocabulary(&bytes.Buffer{}, rr, "run.json"),
		"a difference of exactly vocabularyRateTolerance is inside the bound, not outside it")
}

// The reject side, and the case that pins the bound's MAGNITUDE. The pre-existing
// rejection test used 1/3 against 0.9 — a difference of 0.567, which survives even a
// hundredfold loosening of the constant, so nothing failed when the tolerance was
// mutated from 5e-3 to 5e-1. This one is off by exactly one whole finding (1/4 published
// as 2/4), a difference of 0.25: outside 5e-3 and inside 5e-1, so it dies with the bound.
func TestValidateReviewerVocabulary_RejectsARateNamingADifferentFindingCount(t *testing.T) {
	rr := benchmark.RunResult{Vocabulary: []benchmark.ReviewerVocabulary{
		{Model: "m", Persona: "p", Findings: 4, Drifted: 1, Rate: vocabRate(0.50), RoutingValues: 0},
	}}
	err := validateReviewerVocabulary(&bytes.Buffer{}, rr, "run.json")
	require.Error(t, err, "0.50 on 1-of-4 names two drifted findings, not one — a contradiction, not a rounding")
	assert.Contains(t, err.Error(), "does not match its own counts")
}

// unicode.IsControl covers ESC and the C1 escapes, which is the line-erasure threat the
// sanitizer was written for. It does NOT cover category Cf — the bidi overrides and
// zero-width formatters — and those survive scorecard.scrubField too, whose only
// whitespace pass is strings.Fields over unicode.IsSpace. U+202E reverses the rendering
// of everything after it, so a hostile realized model name can make the drift warning
// read as a different reviewer.
func TestStripTerminalControlRunes_DropsBidiAndZeroWidthFormatters(t *testing.T) {
	// Written as escapes, never as literals: a bare U+FEFF is an illegal byte order
	// mark in Go source, and the rest are invisible in an editor — exactly the property
	// that makes them worth stripping.
	for _, r := range []rune{
		'\u202e', // RIGHT-TO-LEFT OVERRIDE
		'\u202d', // LEFT-TO-RIGHT OVERRIDE
		'\u200e', // LEFT-TO-RIGHT MARK
		'\u200f', // RIGHT-TO-LEFT MARK
		'\u2066', // LEFT-TO-RIGHT ISOLATE
		'\u2069', // POP DIRECTIONAL ISOLATE
		'\u200b', // ZERO WIDTH SPACE
		'\ufeff', // ZERO WIDTH NO-BREAK SPACE
	} {
		got := stripTerminalControlRunes("brad" + string(r) + "x")
		assert.Equal(t, "bradx", got,
			"format rune %U must be stripped before an untrusted identity reaches the terminal", r)
	}
}

// The runes it already handled must keep being handled, and ordinary identities must
// survive untouched.
func TestStripTerminalControlRunes_KeepsPrintableIdentitiesIntact(t *testing.T) {
	assert.Equal(t, "bradx", stripTerminalControlRunes("brad\x1bx"), "ESC is still dropped")
	assert.Equal(t, "bradx", stripTerminalControlRunes("brad\u009bx"), "C1 CSI is still dropped")
	assert.Equal(t, "qwen3.8-max/brad", stripTerminalControlRunes("qwen3.8-max/brad"))
	assert.Equal(t, "bedrock@us-east-1/claude", stripTerminalControlRunes("bedrock@us-east-1/claude"))
}

// vocabRate is a local *float64 helper; the package already has a ptrF, so this one
// is named for what it carries rather than for its type.
func vocabRate(f float64) *float64 { return &f }
