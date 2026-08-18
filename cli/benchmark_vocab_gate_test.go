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
