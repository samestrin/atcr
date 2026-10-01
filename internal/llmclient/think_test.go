package llmclient

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSplitThink pins every tag shape the strip contract names. The table's
// own blank-run rule — a consumed run that held only whitespace yields no
// reasoning worth reporting — is pinned once in
// TestSplitThink_BlankRunYieldsNoReasoningSignal rather than restated as a
// per-row column derived from wantReasoning.
func TestSplitThink(t *testing.T) {
	cases := []struct {
		name          string
		content       string
		wantAnswer    string
		wantReasoning string
	}{
		{name: "empty content", content: "", wantAnswer: "", wantReasoning: ""},
		{name: "no tags at all", content: "just an answer", wantAnswer: "just an answer", wantReasoning: ""},
		{name: "closed pair with text", content: "<think>plan the reply</think>answer",
			wantAnswer: "answer", wantReasoning: "plan the reply"},
		// Whitespace before the leading run is dropped from BOTH returns, which is
		// why the two are not a partition of the input.
		{name: "whitespace before the leading run", content: "\n\n<think>a</think>answer",
			wantAnswer: "answer", wantReasoning: "a"},
		// An empty pair is what a hybrid chat template emits when thinking is
		// correctly off: stripped, but not a signal.
		{name: "empty pair", content: "<think>\n\n</think>answer",
			wantAnswer: "answer", wantReasoning: "\n\n"},
		// Content that is ONLY an empty pair: the strip consumes the leading run
		// and returns an empty answer, while HasThinkMarkup reports false (an
		// empty pair holds no text). The divergence is deliberate — the strip
		// answers "what do I remove", the detector "did it think" — and is pinned
		// here and in TestHasThinkMarkup so a future alignment in either
		// direction fails a test first (TD row think.go:108).
		{name: "empty pair as the entire content", content: "<think>\n\n</think>",
			wantAnswer: "", wantReasoning: "\n\n"},
		// An empty first pair must not hide a real block after it: both pairs are
		// leading, so both are consumed.
		{name: "multiple leading pairs", content: "<think></think><think>real reasoning</think>answer",
			wantAnswer: "answer", wantReasoning: "real reasoning"},
		{name: "whitespace between leading pairs", content: "<think>a</think>\n<think>b</think>answer",
			wantAnswer: "answer", wantReasoning: "ab"},
		// Any Unicode space between pairs, not just ASCII: a template that joins
		// its blocks with a non-breaking space still has a leading run.
		{name: "unicode space between leading pairs", content: "<think>a</think> <think>b</think>answer",
			wantAnswer: "answer", wantReasoning: "ab"},
		{name: "open-only with text", content: "<think>still going",
			wantAnswer: "", wantReasoning: "still going"},
		{name: "open-only with blank remainder", content: "<think>   ",
			wantAnswer: "", wantReasoning: "   "},
		// A bare closer with no opener is NOT stripped (2026-09-30, reversing the
		// rule this helper shipped with). Reading it as a mid-thought reply was
		// position-blind and unbounded, so an answer that merely NAMES the closer
		// lost its whole prefix. The detector keeps the rule; the strip does not.
		{name: "lone closer with no opener is left alone", content: "draft</think>answer",
			wantAnswer: "draft</think>answer", wantReasoning: ""},
		// The shape that forced the reversal: a real verdict naming the bare
		// closer used to strip down to ` at all"}` and parse as malformed.
		{name: "a keyed JSON answer naming the bare closer survives whole",
			content:       `{"verdict":"confirmed","reasoning":"never looks for </think>"}`,
			wantAnswer:    `{"verdict":"confirmed","reasoning":"never looks for </think>"}`,
			wantReasoning: ""},
		// Proves a downstream JSON-object parser (verdict, ruling) sees only the
		// real answer once T2/T3 apply this helper.
		{name: "draft JSON object inside a leading block", content: `<think>{"verdict":"fail"}</think>{"verdict":"pass"}`,
			wantAnswer: `{"verdict":"pass"}`, wantReasoning: `{"verdict":"fail"}`},
		// Leading-only scope: a tag after real answer text is a reviewer quoting
		// the tag, so the content is returned byte-for-byte unchanged.
		{name: "quoted tag after answer text", content: "answer text mentions <think> and </think> in a finding",
			wantAnswer: "answer text mentions <think> and </think> in a finding", wantReasoning: ""},
		{name: "leading pair then a later closer reference", content: "<think>plan</think>real answer with a </think> reference",
			wantAnswer: "real answer with a </think> reference", wantReasoning: "plan"},
		{name: "leading pair then a later opener", content: "<think>plan</think>answer <think>quoted",
			wantAnswer: "answer <think>quoted", wantReasoning: "plan"},
		// A nested opener INSIDE the run is the one shape balance separates: the
		// run consumes to the matching closer, so no stray closer reaches the
		// answer. The reasoning keeps the inner pair's raw bytes (the span
		// between the outer opener and its matching closer is taken verbatim).
		{name: "nested opener consumes to the matching closer", content: "\x3cthink\x3eouter \x3cthink\x3einner\x3c/think\x3e\x3c/think\x3eanswer",
			wantAnswer: "answer", wantReasoning: "outer \x3cthink\x3einner\x3c/think\x3e"},
		// A leading run that ends in an unclosed opener: everything after that
		// opener is reasoning.
		{name: "leading pair then unclosed opener", content: "<think>a</think><think>b",
			wantAnswer: "", wantReasoning: "ab"},
		// An unclosed opener followed by a closer-LIKE token (a variant closer
		// such as </thinking>): the opener was real but its closer was spelled
		// differently, so the strip falls back to stripping nothing - returning
		// the reply whole beats classifying the entire answer as reasoning.
		{name: "unclosed opener followed by a variant closer strips nothing", content: "\x3cthink\x3ereasoning\x3c/thinking\x3eREAL ANSWER",
			wantAnswer: "\x3cthink\x3ereasoning\x3c/thinking\x3eREAL ANSWER", wantReasoning: ""},
		// The doctor probe's marker survives the strip. Doctor's own marker check
		// reads the raw content (classify, internal/doctor/run.go:732), so this
		// pins the helper's behavior, not a doctor coupling.
		{name: "marker survives a leading pair", content: "<think>plan the reply</think>\nATCR-MARKER",
			wantAnswer: "\nATCR-MARKER", wantReasoning: "plan the reply"},
		{name: "marker survives an unstripped lone closer", content: "planning the reply</think>\nATCR-MARKER",
			wantAnswer: "planning the reply</think>\nATCR-MARKER", wantReasoning: ""},
		// Whitespace AFTER the run is kept: a stripped answer may begin with `\n`.
		// Pins the doc's asymmetry sentence — a future TrimLeft on the answer path
		// cannot land silently. (The marker row above happens to show it; this row
		// names the contract.)
		{name: "whitespace after the run survives into the answer", content: "<think>plan</think>\n\n answer",
			wantAnswer: "\n\n answer", wantReasoning: "plan"},
		// A variant opener is not thinking markup (doc comment: matching is on the
		// exact lowercase literals). The content survives whole — draft object and
		// all — so a first-match parser takes the draft. That spoof is the accepted
		// cost of refusing to guess at variants.
		{name: "variant opener means not thinking markup", content: `<THINK>{"verdict":"draft"}</THINK>{"verdict":"real"}`,
			wantAnswer: `<THINK>{"verdict":"draft"}</THINK>{"verdict":"real"}`, wantReasoning: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer, reasoning := SplitThink(tc.content)
			assert.Equal(t, tc.wantAnswer, answer, "answer")
			// reasoning is the named Reasoning type (non-interchangeable with the
			// answer by design); convert for the string-literal comparisons.
			assert.Equal(t, tc.wantReasoning, string(reasoning), "reasoning")
		})
	}
}

// TestSplitThink_BlankRunYieldsNoReasoningSignal pins the table's blank-run
// rule once, on the two inputs whose consumed run holds only whitespace, rather
// than restating it as a per-row column derived from wantReasoning.
func TestSplitThink_BlankRunYieldsNoReasoningSignal(t *testing.T) {
	for _, content := range []string{
		" thinking   ",                 // unclosed opener, blank remainder
		" thinking\n\n responseanswer", // empty pair
	} {
		_, reasoning := SplitThink(content)
		assert.Empty(t, strings.TrimSpace(string(reasoning)),
			"a whitespace-only consumed run must yield no reasoning signal: %q", content)
	}
}

// TestHasThinkMarkup pins the detector as deliberately BROADER than the strip:
// the rows that differ from TestSplitThink are the point of having two
// functions, so each one names the position it proves.
func TestHasThinkMarkup(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "empty content", content: "", want: false},
		{name: "no tags at all", content: "just an answer", want: false},
		{name: "leading pair with text", content: "<think>plan</think>answer", want: true},
		{name: "empty leading pair", content: "<think>\n\n</think>answer", want: false},
		// Entirely-markup content: the strip consumes it, the detector denies it.
		// Deliberate divergence, pinned here and in TestSplitThink (TD row
		// think.go:108).
		{name: "empty pair as the entire content", content: "<think>\n\n</think>", want: false},
		{name: "empty pair then a real block", content: "<think></think><think>real</think>answer", want: true},
		{name: "unclosed leading opener with text", content: "<think>still going", want: true},
		{name: "unclosed leading opener with blank remainder", content: "<think>   ", want: false},
		{name: "lone closer with no opener", content: "draft</think>answer", want: true},
		{name: "lone closer with blank prefix", content: "  </think>answer", want: false},
		// The three rows below are where the detector and the strip diverge. Each
		// is NOT a signal under SplitThink's leading-only rule, and must be one here.
		{name: "pair after answer text", content: "answer <think>x</think> more", want: true},
		{name: "unclosed opener after answer text", content: "ATCR-OK-n\n<think>let me double check", want: true},
		{name: "empty pair after answer text is still not a signal", content: "answer <think></think> more", want: false},
		// Accepted limit: the lone-closer rule is gated on no opener ANYWHERE, so a
		// later empty pair suppresses the mid-thought signal. Pinned as false rather
		// than fixed — it matches doctor's pre-migration behavior, and a template
		// that starts mid-thought and then emits an empty pair is not a real shape.
		{name: "lone closer then a later empty pair", content: "draft</think>answer<think></think>", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, HasThinkMarkup(tc.content))
		})
	}
}

// The detector answers a different question than the strip, so a caller cannot
// substitute one for the other, and neither predicate implies the other. This
// pins the divergence: on the first set the strip finds nothing to remove while
// the detector still reports thinking; on the second (the reverse, which the old
// name TestHasThinkMarkup_BroaderThanSplitThink wrongly excluded) the strip acts
// while the detector denies. Inputs compose the tag constants so the angle
// brackets are never hand-typed.
func TestHasThinkMarkup_AndSplitThink_AreIndependent(t *testing.T) {
	// Direction 1: the strip leaves the tag in place, the detector still reports it.
	for _, content := range []string{
		"answer " + thinkOpen + "x" + thinkClose + " more",
		"ATCR-OK-n\n" + thinkOpen + "let me double check",
		// The bare closer joined this list on 2026-09-30: the strip stopped
		// removing it, the detector still reports it.
		"draft" + thinkClose + "answer",
	} {
		answer, reasoning := SplitThink(content)
		assert.Equal(t, content, answer, "the strip leaves a non-leading tag in place")
		assert.Empty(t, reasoning, "the strip removes nothing")
		assert.True(t, HasThinkMarkup(content), "the detector still reports thinking")
	}

	// Direction 2 (the reverse): the strip consumes the leading run while the
	// detector, which requires NON-BLANK markup holding text, denies it. The
	// detector is therefore not a superset of the strip - the name this test used
	// to carry asserted an invariant that is false.
	for _, content := range []string{
		thinkOpen,                       // unclosed opener, blank remainder
		thinkOpen + "   ",               // unclosed opener, whitespace remainder
		thinkOpen + "\n\n" + thinkClose, // empty pair as the entire content
	} {
		answer, _ := SplitThink(content)
		assert.Empty(t, answer, "the strip consumes a leading run with no answer text")
		assert.False(t, HasThinkMarkup(content), "the detector denies markup that holds no text")
	}
}

// TestSplitThink_AcceptedLossyEdges pins the inputs on which the strip gets the
// boundary wrong, so the blast radius stays visible to whoever wires a lane onto
// the helper and a later narrowing is a deliberate change with a failing test
// rather than a silent one. Filed as TD-022. TD-002's variant-closer case was
// fixed on 2026-09-30 — an unclosed opener followed by a closer-like token now
// strips nothing — and moved to TestSplitThink as a normal row.
//
// TD-001's case — a bare </think> with no opener taking the whole prefix — is
// no longer here: the rule that caused it was removed on 2026-09-30 and its
// inputs now appear in TestSplitThink as survive-whole rows.
func TestSplitThink_AcceptedLossyEdges(t *testing.T) {

	// TD-022, the opposite direction: reasoning that QUOTES the closer cuts its
	// own block early, so the tail of the reasoning survives into the answer and
	// a draft object in that tail wins a first-match parse. Unfixable here — the
	// shape is structurally identical to the stripped "leading pair then a later
	// closer reference" row above (one opener, two closers), and every rule that
	// catches this one breaks that one. See SplitThink's doc comment.
	const quotedCloserInsideTheBlock = `<think>the code searches for </think> in the content. ` +
		`Draft: {"verdict":"confirmed","reasoning":"draft, wrong"} no wait</think>` +
		`{"verdict":"refuted","reasoning":"real answer"}`
	answer, reasoning := SplitThink(quotedCloserInsideTheBlock)
	assert.Equal(t, "the code searches for ", string(reasoning),
		"the run ends at the quoted closer, so only the prefix is taken as reasoning")
	assert.Contains(t, answer, `{"verdict":"confirmed"`,
		"the discarded draft survives into the answer ahead of the real verdict")
	assert.Contains(t, answer, "</think>",
		"the real closer is left behind in the answer, which is the visible tell")
}

// The one-pair shape — by far the most common — must not pay a Builder
// allocation for reasoning that every current call site discards with `_`:
// the reasoning of a single consumed pair is a contiguous slice of the input.
func TestSplitThink_NoBuilderAllocForSinglePair(t *testing.T) {
	content := "<think>reasoning text</think>answer"
	answer, reasoning := SplitThink(content)
	require.Equal(t, "answer", answer)
	require.Equal(t, "reasoning text", string(reasoning))
	allocs := testing.AllocsPerRun(100, func() {
		a, r := SplitThink(content)
		if len(a) < 0 || len(r) < 0 {
			t.Fatal("unreachable")
		}
	})
	assert.Zero(t, allocs, "single-pair strip allocated a throwaway reasoning buffer")
}

// SplitThink returns strings only. The signature already makes reaching the
// reasoning channel unrepresentable, so this documents the contract rather than
// guarding it; it does catch a future signature change to a *Completion.
func TestSplitThink_DoesNotTouchTheReasoningChannel(t *testing.T) {
	comp := Completion{Content: "<think>plan</think>answer"}
	answer, reasoning := SplitThink(comp.Content)

	assert.Equal(t, "answer", answer)
	assert.Equal(t, "plan", string(reasoning))
	assert.Equal(t, "<think>plan</think>answer", comp.Content, "the caller's content is not mutated")
	assert.Empty(t, comp.Reasoning, "SplitThink never writes Completion.Reasoning")
}
