package llmclient

import (
	"strings"
	"unicode"
)

const thinkOpen, thinkClose = "<think>", "</think>"

// SplitThink splits inline <think> reasoning markup off the front of a reply,
// returning the answer text and the reasoning that was removed. It strips only
// a LEADING run — a run of <think>…</think> pairs at the start of the content
// (whitespace between them allowed), a trailing unclosed opener that begins that
// run, or, when no opener appears anywhere, the text before the first lone
// </think>. A tag that appears after real answer text has started is left in
// place and belongs to the answer.
//
// Leading-only, because a tag after answer text is the model quoting the tag —
// a reviewer writing a finding about <think> handling, or a debate seat citing
// one — and eating it would delete real answer text. A lone closer with no
// opener anywhere counts as reasoning, because a reasoning-style chat template
// can put the opener in the prompt, so the reply starts mid-thought and carries
// only the closer (decided 2026-09-29).
//
// Use this to decide what to PARSE or re-send. To decide whether a reply thought
// at all, use HasThinkMarkup: detection is position-blind where a strip cannot
// be, and its doc comment says why.
//
// Malformed markup never errors and never panics: it degrades to stripping the
// leading run only, or to stripping nothing.
//
// Matching is on the exact lowercase literals only. A variant opener or closer
// (<THINK>, <think type=x>, </thinking>) is not recognized as a tag, because
// guessing at variants is how a strip starts eating answer text; a variant
// closer therefore reads as "the leading opener was never closed".
//
// The two returns are NOT a partition of the input: the tag bytes, the
// whitespace before the leading run, and the whitespace between consumed pairs
// are dropped from both, so the original cannot be reconstructed from them. A
// caller that needs the raw reply MUST keep its own copy: strip into a local or
// onto a copied message, never over the field you read.
//
// SplitThink returns strings and nothing else. It never writes to
// Completion.Reasoning or ChatResponse.Reasoning — those stay the only reasoning
// channel, populated by reasoningOf/reasoningText — so a caller that wants the
// removed text must keep it itself.
func SplitThink(content string) (answer, reasoning string) {
	var thought strings.Builder
	rest, stripped := content, false
	for {
		lead := strings.TrimLeftFunc(rest, unicode.IsSpace)
		if !strings.HasPrefix(lead, thinkOpen) {
			break
		}
		stripped = true
		rest = lead[len(thinkOpen):]
		end := strings.Index(rest, thinkClose)
		if end < 0 {
			// The run ends in an unclosed opener: the reply was cut off
			// mid-thought, so everything after the opener is reasoning.
			thought.WriteString(rest)
			return "", thought.String()
		}
		thought.WriteString(rest[:end])
		rest = rest[end+len(thinkClose):]
	}
	if stripped {
		return rest, thought.String()
	}
	if strings.Contains(content, thinkOpen) {
		// An opener exists but does not lead: the tag is quoted inside the
		// answer, so the content is returned untouched.
		return content, ""
	}
	if end := strings.Index(content, thinkClose); end >= 0 {
		return content[end+len(thinkClose):], content[:end]
	}
	return content, ""
}

// HasThinkMarkup reports whether the content carries inline think markup holding
// text ANYWHERE in it: a <think>…</think> pair with non-blank inner text at any
// position, a trailing unclosed opener with a non-blank remainder, or a lone
// </think> with no opener and non-blank text before it. An empty pair is not
// markup holding text — that is what a hybrid chat template emits when thinking
// is correctly off.
//
// This is a DETECTION question, deliberately broader than SplitThink's strip.
// The two are different questions and cannot share one answer. A strip must be
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
	if !strings.Contains(content, thinkOpen) {
		// No opener anywhere: a lone closer still marks a reply that started
		// mid-thought, so the text before it is reasoning.
		if end := strings.Index(content, thinkClose); end >= 0 {
			return strings.TrimSpace(content[:end]) != ""
		}
		return false
	}
	rest := content
	for {
		start := strings.Index(rest, thinkOpen)
		if start < 0 {
			return false
		}
		rest = rest[start+len(thinkOpen):]
		end := strings.Index(rest, thinkClose)
		if end < 0 {
			// Left open: everything after the opener is reasoning-in-progress.
			return strings.TrimSpace(rest) != ""
		}
		if strings.TrimSpace(rest[:end]) != "" {
			return true
		}
		// An empty pair must not hide a real block after it: keep scanning.
		rest = rest[end+len(thinkClose):]
	}
}
