package llmclient

import (
	"strings"
	"unicode"
)

// Reasoning is the chain-of-thought text SplitThink removed from a reply.
// It is a distinct named type (not a bare string) so the two returns cannot
// be transposed at a call site without a compile error: the pair's order
// deliberately inverts their order in the input, and every real consumer of
// the answer wants a string. Convert with string(r) when the reasoning text
// itself is needed.
type Reasoning string

const thinkOpen, thinkClose = "<think>", "</think>"

// SplitThink splits inline <think> reasoning markup off the front of a reply,
// returning the answer text and the reasoning that was removed. It strips only
// a LEADING run — a run of <think>…</think> pairs at the start of the content
// (whitespace between them allowed), or a trailing unclosed opener that begins
// that run — unless a closer-like token follows the unclosed opener, in which
// case nothing is stripped (see the unclosed-opener branch). Everything else is
// the answer: a tag that appears after real answer
// text has started, and a bare </think> with no opener anywhere, are both left
// in place.
//
// Leading-only, because a tag after answer text is the model quoting the tag —
// a reviewer writing a finding about <think> handling, or a debate seat citing
// one — and eating it would delete real answer text.
//
// A bare closer with no opener is NOT stripped (decided 2026-09-30, reversing
// the rule this function shipped with on 2026-09-29). The old rule read it as a
// reply that started mid-thought because a chat template had put the opener in
// the prompt, and took everything before the closer as reasoning. That is
// position-blind and unbounded, so a verdict naming the bare closer —
// `{"verdict":"confirmed","reasoning":"never looks for </think>"}` — lost its
// whole prefix and parsed as malformed. The mid-thought shape is speculative;
// an answer that names the closer is the likeliest input in this repo, since
// findings and TD rows discuss </think> handling directly. Keeping the prefix
// costs at most an unstripped reasoning run reaching a parser that scans for a
// keyed JSON object anyway; the old rule silently destroyed real verdicts.
//
// The DETECTOR keeps the bare-closer rule — see HasThinkMarkup. Detecting that
// a reply thought is a different question from deciding what to remove, and the
// doctor verdict is the only consumer that rule ever governed.
//
// Use this to decide what to PARSE or re-send. To decide whether a reply thought
// at all, use HasThinkMarkup: detection is position-blind where a strip cannot
// be, and its doc comment says why.
//
// Malformed markup never errors and never panics: it degrades to stripping the
// leading run only, or to stripping nothing.
//
// Each pair in the run ends at the FIRST </think> after its opener — unless a NESTED opener intervenes, in which case balance
// picks the matching closer (openers outnumbering closers keep the run going),
// so nesting never leaves a stray closer in the answer. So reasoning
// that QUOTES the closer cuts its own block early, and the tail of that reasoning
// survives into the answer — a draft object in that tail can then win a
// first-match parse. That loss is accepted, not overlooked (2026-09-30, sprint
// 35.16.11.2.2.4 Phase 5 review; TD-022). It cannot be fixed here, because
// `<think>a </think> b</think>answer` and `<think>a</think>answer naming </think>`
// have the same tag structure — one opener, two closers — and no positional rule
// separates them. Ending the run at the LAST closer instead, or refusing to strip
// when an unmatched closer remains, was measured against the suite: each one
// breaks the "leading pair then a later closer reference" row in TestSplitThink
// and relocates the draft-wins failure rather than removing it. Both shapes are
// pinned — the stripped one there, this one in
// TestSplitThink_AcceptedLossyEdges — so a future narrowing is a deliberate
// change with a failing test.
//
// Matching is on the exact lowercase literals only. A variant opener or closer
// (<THINK>, <think type=x>, </thinking>) is not recognized as a tag, because
// guessing at variants is how a strip starts eating answer text. A variant
// OPENER means the content is not thinking markup at all: it survives
// whole, draft object and all, so a first-match parser takes the draft — the
// accepted cost of refusing to guess at variants (pinned by the variant-opener
// row in TestSplitThink). A variant CLOSER after an unclosed opener is the one
// variant acted on, and only in the conservative direction: the run falls back
// to stripping nothing rather than classifying the whole answer as reasoning.
//
// The two returns are NOT a partition of the input: the tag bytes, the
// whitespace before the leading run, and the whitespace between consumed pairs
// are dropped from both, so the original cannot be reconstructed from them.
// The asymmetry runs one way only: whitespace AFTER the run is KEPT, so a
// stripped answer may begin with `\n` or whitespace — the answer is everything
// from the first byte after the last consumed closer, deliberately, so it stays
// a suffix of the input. Any caller that branches on the answer's first byte
// (e.g. `answer[0] == '{'`) must trim first. A caller that needs the raw reply
// MUST keep its own copy: strip into a local or onto a copied message, never
// over the field you read.
//
// SplitThink returns the answer as a plain string and the removed text as
// Reasoning (a distinct string type — see its declaration) so a transposed
// destructure fails to compile. It never writes to
// Completion.Reasoning or ChatResponse.Reasoning — those stay the only reasoning
// channel, populated by reasoningOf/reasoningText — so a caller that wants the
// removed text must keep it itself.
func SplitThink(content string) (answer string, reasoning Reasoning) {
	// The reasoning is assembled lazily: the first consumed pair's text is a
	// contiguous slice of the input and is kept as one, so the overwhelmingly
	// common single-pair strip never allocates the Builder. The Builder is
	// promoted only when a SECOND pair is consumed and the pieces must be
	// concatenated (every current call site discards the reasoning, so the
	// common case must not pay for a throwaway buffer).
	var thought strings.Builder
	firstReasoning, sawFirst, promoted := "", false, false
	rest, stripped := content, false
	for {
		lead := strings.TrimLeftFunc(rest, unicode.IsSpace)
		if !strings.HasPrefix(lead, thinkOpen) {
			break
		}
		stripped = true
		rest = lead[len(thinkOpen):]
		// Consume to the MATCHING closer: a nested opener inside the run means the
		// next closer belongs to that inner block, so openers outnumbering closers
		// keep the run going until the balance returns to zero. The scan reads
		// slices only, and a single matching span stays contiguous, so the
		// no-alloc single-pair path is unchanged.
		depth, offset, end := 1, 0, -1
		for {
			tail := rest[offset:]
			nextClose := strings.Index(tail, thinkClose)
			if nextClose < 0 {
				break
			}
			if nextOpen := strings.Index(tail, thinkOpen); nextOpen >= 0 && nextOpen < nextClose {
				depth++
				offset += nextOpen + len(thinkOpen)
				continue
			}
			if depth--; depth == 0 {
				end = offset + nextClose
				break
			}
			offset += nextClose + len(thinkClose)
		}
		if end < 0 {
			// The run ends in an unclosed opener. If a closer-LIKE token follows —
			// anything beginning with the closer constant minus its final byte, i.e.
			// a variant closer such as </thinking> — the opener was real but its
			// closer was spelled differently, so fall back to stripping NOTHING:
			// returning the reply whole beats classifying the entire answer as
			// reasoning (TD-002, 2026-09-30 — supersedes the accepted-loss this
			// branch shipped with; an exact closer here would already have matched
			// end above). A reply genuinely cut off mid-thought has no closer-like
			// token and stays the everything-is-reasoning case below.
			if strings.Contains(rest, thinkClose[:len(thinkClose)-1]) {
				return content, ""
			}
			// The run ends in an unclosed opener: the reply was cut off
			// mid-thought, so everything after the opener is reasoning. A
			// pair may already have been consumed — concatenate, never replace
			// (the lazy path kept the first piece outside the Builder).
			if sawFirst {
				thought.WriteString(firstReasoning)
			}
			thought.WriteString(rest)
			return "", Reasoning(thought.String())
		}
		if !sawFirst {
			firstReasoning, sawFirst = rest[:end], true
		} else {
			if !promoted {
				thought.WriteString(firstReasoning)
				promoted = true
			}
			thought.WriteString(rest[:end])
		}
		rest = rest[end+len(thinkClose):]
	}
	if stripped {
		if promoted {
			return rest, Reasoning(thought.String())
		}
		return rest, Reasoning(firstReasoning)
	}
	// Nothing led, so nothing is removed. This covers both an opener quoted
	// inside the answer and a bare closer with no opener anywhere: neither is a
	// leading run, and a strip has no way to tell a mid-thought reply from an
	// answer that merely names the tag.
	return content, ""
}

// HasEnclosingThinkBlock reports whether the content carries an OPENER-ANCHORED
// think block holding text: a <think>…</think> pair with non-blank inner text, or
// a trailing unclosed <think> with a non-blank remainder. It is HasThinkMarkup
// minus the bare-closer rule, and the difference is the whole point.
//
// Use this to decide whether a reply might be hiding a DISCARDED DRAFT before its
// committed answer. A draft needs a block to sit in, and a block needs an opener;
// a lone </think> opens nothing, so no object after it is enclosed by anything and
// none of them can be a draft. If anything, a lone closer means the reply started
// mid-thought because a chat template put the opener in the prompt — in which case
// the object AFTER it is the committed answer, the exact opposite of a draft.
//
// The verify and executor lanes refuse a reply this returns true for, because
// their strip is leading-only: one character of prose before a block defeats it
// and leaves the draft inside it as the first keyed object a parser will take.
// They previously called HasThinkMarkup, which inherited the bare-closer rule and
// therefore threw away a committed verdict from any reply whose PROSE merely named
// </think> — the likeliest input in this repo, since findings here discuss think
// handling directly. SplitThink's own doc records that rule being reversed for the
// STRIP on 2026-09-30 for the same reason; this is the same reversal for the
// refusal that re-adopted it.
//
// HasThinkMarkup keeps the bare-closer rule, and must: its consumer is doctor's
// thinking verdict, which asks "did this model think at all" from a fixed prompt
// containing no tag. That is a detection question, not an enclosure question.
func HasEnclosingThinkBlock(content string) bool {
	rest := content
	for {
		open := strings.Index(rest, thinkOpen)
		if open < 0 {
			return false
		}
		rest = rest[open+len(thinkOpen):]
		end := strings.Index(rest, thinkClose)
		if end < 0 {
			// Left open: everything after the opener is reasoning-in-progress.
			return strings.TrimSpace(rest) != ""
		}
		if strings.TrimSpace(rest[:end]) != "" {
			return true
		}
		// An empty pair holds no draft, but must not hide a later block that does.
		rest = rest[end+len(thinkClose):]
	}
}

// HasThinkMarkup reports whether the content carries inline think markup holding
// text ANYWHERE in it: a <think>…</think> pair with non-blank inner text at any
// position, a trailing unclosed opener with a non-blank remainder, or a </think>
// with NO OPENER BEFORE IT and non-blank text before it. That last rule is
// positional and is re-applied to the remainder after each consumed pair, so
// neither a later opener nor an earlier empty pair can hide a mid-thought reply. An empty pair is not
// markup holding text — that is what a hybrid chat template emits when thinking
// is correctly off.
//
// On content that is ENTIRELY markup the two functions therefore disagree by
// design: SplitThink consumes the leading empty pair (or bare opener) and
// returns an empty answer, while this detector returns false. Neither is wrong —
// the strip answers "what do I remove", the detector "did it think", and an
// empty pair is removable yet carries no signal. Both sides are pinned
// (TestSplitThink and TestHasThinkMarkup) so the divergence cannot drift into an
// accident.
//
// This is a DETECTION question, and NEITHER predicate implies the other: the
// detector is broader for a tag after answer text (the strip refuses it) but
// NARROWER for content that is entirely markup with a blank body, where
// HasThinkMarkup is false while SplitThink still consumes the leading run
// (`<think>` alone, `<think>   `, and an empty pair as the whole content are the
// three inputs where the strip is the broader one; see
// TestHasThinkMarkup_AndSplitThink_AreIndependent). The two are different
// questions and cannot share one answer. A strip must be
// leading-only or it eats real answer text that merely quotes the tag (a
// reviewer writing a finding about <think> handling). A detector must be
// position-blind, because its caller — the doctor thinking probe — sends a fixed
// prompt that provably contains no tag (Prompt, internal/doctor/run.go:99), so a
// block AFTER the answer there is not a quote: it is the runaway thinker the
// verdict exists to name (decided 2026-09-30 at the sprint 35.16.11.2.2.4
// Phase 1 review, reversing the plan's original single-predicate shape).
//
// The lone-closer rule lives here too, which is where doctor's older "a stray
// closer is template noise" rule was reversed (decided 2026-09-29): the verdict
// is the only consumer that rule ever governed.
//
// It lives beside SplitThink on the same tag constants so internal/llmclient
// stays the repo's only place that knows what a think tag looks like.
func HasThinkMarkup(content string) bool {
	rest := content
	for {
		open := strings.Index(rest, thinkOpen)
		closer := strings.Index(rest, thinkClose)
		// The lone-closer rule is POSITIONAL: what makes a closer "lone" is that no
		// opener precedes it, not that no opener exists anywhere. Gating it on the
		// whole content let ANY later opener suppress the signal — an empty pair
		// included — so a mid-thought reply that then emitted an empty pair reported
		// clean, which is exactly the hybrid-template shape the empty-pair rule below
		// exists to defend against (TD internal/doctor/run.go:1055).
		if closer >= 0 && (open < 0 || closer < open) {
			return strings.TrimSpace(rest[:closer]) != ""
		}
		if open < 0 {
			return false
		}
		rest = rest[open+len(thinkOpen):]
		end := strings.Index(rest, thinkClose)
		if end < 0 {
			// Left open: everything after the opener is reasoning-in-progress.
			return strings.TrimSpace(rest) != ""
		}
		if strings.TrimSpace(rest[:end]) != "" {
			return true
		}
		// An empty pair must not hide a real block after it — nor a mid-thought
		// closer, which is why the loop re-enters at the positional rule above
		// instead of only looking for the next opener.
		rest = rest[end+len(thinkClose):]
	}
}
