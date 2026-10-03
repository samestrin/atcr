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
// HasThinkMarkup keeps the bare-closer rule, and must: it answers doctor's
// detection question ("did this model think at all"), not the enclosure question
// the verify, executor and debate lanes share. Its consumers are doctor's thinking
// verdict and the run-level thinking probe behind it — both send a fixed prompt
// containing no tag, so a block after the answer there is not a quote, it is the
// runaway thinker the verdict exists to name.
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

// IndexAfterUnopenedCloser returns the offset just past the LAST </think> that
// no <think> opened, or -1 when the content carries no such closer.
//
// An UNOPENED closer is the one tag shape neither of this file's other two
// routines acts on, and both abstentions are deliberate. SplitThink leaves it in
// place (decided 2026-09-30, see its doc) because a reply that merely NAMES the
// closer is the likeliest input in this repo and the old rule destroyed its whole
// prefix. HasEnclosingThinkBlock does not refuse on it because a lone closer
// encloses nothing, so no object after it can be a draft. Both are right. What
// falls between them is the object BEFORE such a closer: if the reply really did
// start mid-thought — a chat template having put the opener in the prompt — that
// object is reasoning the model abandoned, and a first-match parser takes it as
// the answer. That is the gap this offset closes.
//
// It returns a BOUNDARY, not a verdict, and that is what keeps the mid-thought
// reading from being a guess. A caller tries the text after the offset and keeps
// the whole answer when that text carries no envelope — so the two shapes are
// separated by evidence rather than by interpretation:
//
//	{draft} </think> {real}   → the suffix parses, so the suffix is the answer
//	{real} … prose "</think>" → the suffix parses to nothing, so the whole
//	                             answer stands and the 2026-09-30 reversal holds
//
// The LAST unopened closer wins: a reply can resume mid-thought more than once,
// and only the final committed section is the answer.
//
// Openers are matched by BALANCE, not by position, so an ordinary leading run
// (`<think>r</think>…`) and a nested one are not mistaken for a resume — only a
// closer with no opener left to match it counts. Matching is on the same exact
// lowercase literals SplitThink uses; a variant spelling is not a tag here
// either, for the reason stated there.
//
// The offset indexes BYTES, so content[i:] is the committed section. A caller
// that needs tags inside JSON string values ignored computes the offset on a
// masked copy and slices the unmasked original at it — MaskJSONStrings blanks
// bytes in place and preserves length, so the two stay aligned. ClassifyUnopenedCloser
// is that pattern packaged for every lane.
func IndexAfterUnopenedCloser(content string) int {
	depth, last := 0, -1
	for i := 0; i < len(content); {
		switch {
		case strings.HasPrefix(content[i:], thinkOpen):
			depth++
			i += len(thinkOpen)
		case strings.HasPrefix(content[i:], thinkClose):
			if depth == 0 {
				last = i + len(thinkClose)
			} else {
				depth--
			}
			i += len(thinkClose)
		default:
			i++
		}
	}
	return last
}

// MaskJSONStrings blanks every byte inside a JSON double-quoted string literal in
// s, preserving length and every byte outside a literal. String-awareness is the
// same rule extractJSONObject uses: a backslash escapes the next byte.
//
// A `"` only OPENS a literal where JSON can actually begin one — immediately after
// `{`, `[`, `,`, `:`, or at the very start of the input. A quote anywhere else is
// prose (an inch mark, a quotation in a sentence) and does NOT toggle the mask.
//
// That position rule is the whole fix. Pairing every `"` from offset 0 with no
// JSON-validity check made the in/out-of-string state a guess as soon as ONE stray
// prose quote appeared, and the wrong guess ran to end of input — so a think pair
// QUOTED inside an earlier, cleanly-closed JSON string value was un-masked and read
// as markup: refusal on legal input (`{"verdict":"confirmed","reasoning":"…
// <think>draft</think> …"} note: a 6" gap`), and a TRUNCATED reply is odd-quoted by
// construction. Discarding the whole masked copy on an unbalanced count did not help
// — it published the raw reply, un-masking those same cleanly-closed literals.
//
// Restricting the opener to JSON position keeps the ambiguous region from ever
// forming: a lone prose quote is ignored, every real string value is masked (so a
// tag confined to a value stays hidden), and a tag that is genuine markup — outside
// any literal — stays visible so every detection site still refuses it. That last
// property is load-bearing: HasEnclosingThinkBlock must see the abandoned draft in
// `He said "… <think>{draft}</think> {real}` or parseRuling takes the draft as the
// committed ruling and applyRulings writes it onto the finding durably.
//
// The result is for tag DETECTION only, never for parsing: a think tag that
// survives the mask is markup enclosing reply text, while one that appears solely
// inside a string value is a model QUOTING the tag (the likeliest input in this
// repo, where findings discuss think handling) and must not be read as thinking.
// Because the output is byte-aligned with the input, an offset computed on the
// masked copy is valid in the unmasked original — which is what lets a caller
// mask for detection and slice the original for parsing.
//
// It lives here, not in a lane, because internal/llmclient owns every tag rule:
// three lanes now share one definition of where a tag counts.
func MaskJSONStrings(s string) string {
	b := []byte(s)
	inStr, escaped := false, false
	// openCtx reports whether the byte just consumed leaves JSON in a position where
	// a string literal may begin. It starts true: a bare JSON string is a valid
	// document, so a leading `"` opens a literal.
	openCtx := true
	for i := 0; i < len(b); i++ {
		c := b[i]
		if !inStr {
			switch {
			case c == '"' && openCtx:
				inStr = true
				openCtx = false
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
				// Whitespace neither opens nor closes a JSON position.
			default:
				openCtx = c == '{' || c == '[' || c == ',' || c == ':'
			}
			continue
		}
		if escaped {
			escaped = false
			b[i] = ' '
			continue
		}
		switch c {
		case '\\':
			escaped = true
			b[i] = ' '
		case '"':
			inStr = false
			// A value just closed, so the next `"` is not itself a valid opener; only
			// an intervening `,`/`:` (or another container) restores JSON position.
			openCtx = false
		default:
			b[i] = ' '
		}
	}
	return string(b)
}

// CloserSection names which part of a STRIPPED answer holds the committed
// envelope when the reply carries a  response that no  thinking opened.
type CloserSection int

const (
	// SectionWholeAnswer: no unopened closer, or nothing after it parses. The
	// answer is read end to end.
	SectionWholeAnswer CloserSection = iota
	// SectionAfterCloser: only the text AFTER the closer carries an envelope, so
	// the reply began mid-thought and that text is the committed section.
	SectionAfterCloser
	// SectionAmbiguous: BOTH sides carry one. Nothing in the tag structure says
	// which is committed, so neither may be used.
	SectionAmbiguous
)

// ClassifyUnopenedCloser decides which part of a stripped answer to parse when a
// </think> appears that no  thinking opened. It is shared by every lane, so no
// lane can drift on the RULE; each lane still supplies its own hasEnvelope,
// because what counts as an envelope entirely depends on that lane's parser.
//
// Such a closer is the one tag shape neither earlier guard acts on, both
// deliberately: SplitThink leaves it in place and HasEnclosingThinkBlock does not
// refuse on it (IndexAfterUnopenedCloser documents why). What falls between them
// is the envelope BEFORE the closer — reasoning the model abandoned if the reply
// really did start mid-thought — which a first-match parser takes as the answer.
//
// Three outcomes, because the structure genuinely supports three cases and only
// two of them have a safe default:
//
//	{draft} </think> {real}   → ambiguous: so does {real} … "</think>" {example}
//	         </think> {real}   → after-closer: nothing before it to confuse
//	{real} … prose "</think>"  → whole answer: the suffix holds no envelope
//
// The ambiguous case is REFUSED by the caller rather than resolved. Taking the
// last section would let a quoted example override a real answer; taking the
// first is the defect this rule exists to close. The two shapes have identical
// tag structure, so no positional rule separates them.
//
// The offset is computed on the MASKED copy, so a closer quoted inside a JSON
// string value is not a boundary, and sliced out of the UNMASKED answer so the
// envelope reaches the parser intact. MaskJSONStrings blanks in place and
// preserves length, so one offset is valid in both.
func ClassifyUnopenedCloser(answer string, hasEnvelope func(string) bool) (CloserSection, string) {
	i := IndexAfterUnopenedCloser(MaskJSONStrings(answer))
	if i < 0 || i > len(answer) {
		return SectionWholeAnswer, answer
	}
	suffix := answer[i:]
	if !hasEnvelope(suffix) {
		return SectionWholeAnswer, answer
	}
	if hasEnvelope(answer[:i]) {
		return SectionAmbiguous, suffix
	}
	return SectionAfterCloser, suffix
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
