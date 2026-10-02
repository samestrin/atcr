package llmclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIndexAfterUnopenedCloser pins the offset rule the verify and executor
// lanes slice on.
//
// An UNOPENED closer is a </think> that no <think> opened. SplitThink
// deliberately leaves it in place (decided 2026-09-30), and
// HasEnclosingThinkBlock deliberately does not refuse on it — a lone closer
// encloses nothing. Neither decision is wrong, and together they left the first
// balanced JSON object in front of such a closer reachable by a first-match
// parser, which is the object a mid-thought reply has NOT committed to.
//
// The offset is what lets a lane prefer the suffix WITHOUT guessing: it tries
// the text after the closer and keeps the whole answer when that text carries no
// envelope. So this function only has to locate the boundary; it never decides
// which side is the answer.
func TestIndexAfterUnopenedCloser(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   string
		want int
	}{
		{"no markup at all", `{"verdict":"confirmed"}`, -1},
		{"opened and closed is not unopened", `<think>r</think>{"verdict":"confirmed"}`, -1},
		{"unclosed opener has no closer to find", `<think>still thinking`, -1},

		// The shape that motivated this: a draft before a bare closer.
		{"bare closer after a draft", `{"a":1}` + "\n" + `</think>` + "\n" + `{"b":2}`, 16},
		{"bare closer at the very start", `</think>x`, 8},

		// The LAST unopened closer wins: a reply can resume mid-thought more than
		// once, and only the final committed section is the answer.
		{"two unopened closers take the last", `a</think>b</think>c`, 18},

		// Nesting must not be mistaken for an unopened closer, or an ordinary
		// leading run would be treated as a mid-thought resume.
		{"balanced pair then unopened closer", `<think>r</think>a</think>b`, 25},
		{"nested pair is balanced", `<think>a<think>b</think></think>z`, -1},

		// A closer that an opener already matched is consumed by that opener, so a
		// later closer with no opener left IS unopened.
		{"opener consumes first closer only", `<think>a</think></think>tail`, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IndexAfterUnopenedCloser(tc.in))
		})
	}
}

// TestIndexAfterUnopenedCloser_OffsetIsSliceableOnTheInput guards the property
// the callers depend on: the return is a byte offset INTO the content, so
// content[i:] is the committed section. The lanes compute the offset on a
// maskJSONStrings copy (same length, bytes blanked in place) and slice the
// unmasked answer at it, so an off-by-one here would hand a parser a truncated
// envelope.
func TestIndexAfterUnopenedCloser_OffsetIsSliceableOnTheInput(t *testing.T) {
	t.Parallel()
	in := `{"fix":"DRAFT"}` + "\n" + `</think>` + "\n" + `{"fix":"REAL"}`

	i := IndexAfterUnopenedCloser(in)

	assert.GreaterOrEqual(t, i, 0, "the bare closer must be found")
	assert.LessOrEqual(t, i, len(in))
	assert.Equal(t, "\n"+`{"fix":"REAL"}`, in[i:],
		"the suffix must start immediately after the closer, whitespace included")
}
