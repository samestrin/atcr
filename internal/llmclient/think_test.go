package llmclient

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSplitThink pins every tag shape the strip contract names. wantSignal
// documents the strip's own blank-run rule — a consumed run that held only
// whitespace yields no reasoning worth reporting — and is derived from
// wantReasoning, so it restates the table rather than adding coverage.
func TestSplitThink(t *testing.T) {
	cases := []struct {
		name          string
		content       string
		wantAnswer    string
		wantReasoning string
		wantSignal    bool
	}{
		{name: "empty content", content: "", wantAnswer: "", wantReasoning: "", wantSignal: false},
		{name: "no tags at all", content: "just an answer", wantAnswer: "just an answer", wantReasoning: "", wantSignal: false},
		{name: "closed pair with text", content: "<think>plan the reply</think>answer",
			wantAnswer: "answer", wantReasoning: "plan the reply", wantSignal: true},
		// Whitespace before the leading run is dropped from BOTH returns, which is
		// why the two are not a partition of the input.
		{name: "whitespace before the leading run", content: "\n\n<think>a</think>answer",
			wantAnswer: "answer", wantReasoning: "a", wantSignal: true},
		// An empty pair is what a hybrid chat template emits when thinking is
		// correctly off: stripped, but not a signal.
		{name: "empty pair", content: "<think>\n\n</think>answer",
			wantAnswer: "answer", wantReasoning: "\n\n", wantSignal: false},
		// An empty first pair must not hide a real block after it: both pairs are
		// leading, so both are consumed.
		{name: "multiple leading pairs", content: "<think></think><think>real reasoning</think>answer",
			wantAnswer: "answer", wantReasoning: "real reasoning", wantSignal: true},
		{name: "whitespace between leading pairs", content: "<think>a</think>\n<think>b</think>answer",
			wantAnswer: "answer", wantReasoning: "ab", wantSignal: true},
		// Any Unicode space between pairs, not just ASCII: a template that joins
		// its blocks with a non-breaking space still has a leading run.
		{name: "unicode space between leading pairs", content: "<think>a</think> <think>b</think>answer",
			wantAnswer: "answer", wantReasoning: "ab", wantSignal: true},
		{name: "open-only with text", content: "<think>still going",
			wantAnswer: "", wantReasoning: "still going", wantSignal: true},
		{name: "open-only with blank remainder", content: "<think>   ",
			wantAnswer: "", wantReasoning: "   ", wantSignal: false},
		// The lone-closer rule (2026-09-29): a reasoning chat template can put the
		// opener in the prompt, so the reply starts mid-thought carrying only the
		// closer. Reversed from doctor's old "a stray closer is template noise".
		{name: "lone closer with no opener", content: "draft</think>answer",
			wantAnswer: "answer", wantReasoning: "draft", wantSignal: true},
		// Proves a downstream JSON-object parser (verdict, ruling) sees only the
		// real answer once T2/T3 apply this helper.
		{name: "draft JSON object inside a leading block", content: `<think>{"verdict":"fail"}</think>{"verdict":"pass"}`,
			wantAnswer: `{"verdict":"pass"}`, wantReasoning: `{"verdict":"fail"}`, wantSignal: true},
		// Leading-only scope: a tag after real answer text is a reviewer quoting
		// the tag, so the content is returned byte-for-byte unchanged.
		{name: "quoted tag after answer text", content: "answer text mentions <think> and </think> in a finding",
			wantAnswer: "answer text mentions <think> and </think> in a finding", wantReasoning: "", wantSignal: false},
		{name: "leading pair then a later closer reference", content: "<think>plan</think>real answer with a </think> reference",
			wantAnswer: "real answer with a </think> reference", wantReasoning: "plan", wantSignal: true},
		{name: "leading pair then a later opener", content: "<think>plan</think>answer <think>quoted",
			wantAnswer: "answer <think>quoted", wantReasoning: "plan", wantSignal: true},
		// A leading run that ends in an unclosed opener: everything after that
		// opener is reasoning.
		{name: "leading pair then unclosed opener", content: "<think>a</think><think>b",
			wantAnswer: "", wantReasoning: "ab", wantSignal: true},
		// The doctor probe's marker survives the strip. Doctor's own marker check
		// reads the raw content (classify, internal/doctor/run.go:732), so this
		// pins the helper's behavior, not a doctor coupling.
		{name: "marker survives a leading pair", content: "<think>plan the reply</think>\nATCR-MARKER",
			wantAnswer: "\nATCR-MARKER", wantReasoning: "plan the reply", wantSignal: true},
		{name: "marker survives a lone closer", content: "planning the reply</think>\nATCR-MARKER",
			wantAnswer: "\nATCR-MARKER", wantReasoning: "planning the reply", wantSignal: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer, reasoning := SplitThink(tc.content)
			assert.Equal(t, tc.wantAnswer, answer, "answer")
			assert.Equal(t, tc.wantReasoning, reasoning, "reasoning")
			assert.Equal(t, tc.wantSignal, strings.TrimSpace(reasoning) != "", "reasoning is a signal")
		})
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
// substitute one for the other. This pins the divergence itself: on these inputs
// the strip finds nothing to remove while the detector still reports thinking.
func TestHasThinkMarkup_BroaderThanSplitThink(t *testing.T) {
	for _, content := range []string{
		"answer <think>x</think> more",
		"ATCR-OK-n\n<think>let me double check",
	} {
		answer, reasoning := SplitThink(content)
		assert.Equal(t, content, answer, "the strip leaves a non-leading tag in place")
		assert.Empty(t, reasoning, "the strip removes nothing")
		assert.True(t, HasThinkMarkup(content), "the detector still reports thinking")
	}
}

// TestSplitThink_AcceptedLossyEdges pins two inputs on which the strip DOES eat
// real answer text. Both are documented, accepted edges of the leading-only
// scope (plan 35.16.11.2.2.4 Risk Mitigation, task-01: "out of scope for this
// task's fix, only for test awareness — do not attempt a code-fence heuristic
// here"). They are pinned so the blast radius is visible to whoever wires a lane
// onto the helper, and so a later narrowing of these cases is a deliberate
// change with a failing test, not a silent one. Filed as TD-001 and TD-002.
func TestSplitThink_AcceptedLossyEdges(t *testing.T) {
	// A bare </think> quoted in an answer with no opener anywhere is
	// indistinguishable from a reply that started mid-thought, so the whole
	// prefix is taken as reasoning.
	answer, reasoning := SplitThink(`{"findings":[{"note":"model emitted </think>"}]}`)
	assert.Equal(t, `"}]}`, answer, "the prefix before a quoted lone closer is lost")
	assert.NotEmpty(t, reasoning)

	// A variant closer is not a closer, so the leading opener reads as unclosed
	// and everything after it is reasoning.
	answer, reasoning = SplitThink("<think>reasoning</thinking>REAL ANSWER")
	assert.Empty(t, answer, "a variant closer leaves the opener unclosed")
	assert.Equal(t, "reasoning</thinking>REAL ANSWER", reasoning)
}

// SplitThink returns strings only. The signature already makes reaching the
// reasoning channel unrepresentable, so this documents the contract rather than
// guarding it; it does catch a future signature change to a *Completion.
func TestSplitThink_DoesNotTouchTheReasoningChannel(t *testing.T) {
	comp := Completion{Content: "<think>plan</think>answer"}
	answer, reasoning := SplitThink(comp.Content)

	assert.Equal(t, "answer", answer)
	assert.Equal(t, "plan", reasoning)
	assert.Equal(t, "<think>plan</think>answer", comp.Content, "the caller's content is not mutated")
	assert.Empty(t, comp.Reasoning, "SplitThink never writes Completion.Reasoning")
}
