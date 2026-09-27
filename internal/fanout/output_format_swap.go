package fanout

import (
	"strings"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
)

// outputFormatHeading opens the persona section the response_format swap
// replaces. It is matched only as a whole line, and only in the text before the
// rendered payload, so a diff that repeats the heading is never touched.
const outputFormatHeading = "## Output Format"

// jsonObjectOutputFormat is the shared ## Output Format block a review agent
// declaring response_format: json_object is sent in place of its persona's own.
// Under json_object the API returns exactly one bare JSON object — no fence, no
// top-level array, no plain-text NO FINDINGS — so the persona's fenced-array
// contract cannot be honored; this block asks for the {"findings":[...]} wrapper
// that stream.ParseModelOutput and stream.IsNoFindings already accept. It keeps
// the persona sections' seven keys and rules, and names JSON because some
// providers reject json_object when no message mentions it.
const jsonObjectOutputFormat = `## Output Format
Reply with exactly one JSON object and nothing else — no code fence and no prose around it. The object has one key, "findings", holding an array of finding objects: {"findings":[...]}. Each finding object has exactly these keys:

"severity", "file_line", "problem", "fix", "category", "est_minutes", "evidence"

Rules: severity is one of CRITICAL, HIGH, MEDIUM, LOW; file_line is FILE:LINE copied exactly from the diff — never approximate, guess, or invent it; category is one lowercase word; est_minutes is an integer; evidence cites the offending code; quote code exactly as written, since JSON string escaping carries quotes, pipes, and newlines. This format replaces any earlier instruction in this prompt to emit a fenced JSON array or a plain-text clean-review line. If nothing is wrong, reply with exactly this object and nothing else: {"findings":[]}

Example:
{"findings":[{"severity": "HIGH", "file_line": "store/cache.go:88", "problem": "Get returns stale entry after Invalidate", "fix": "Delete key inside the same lock as Invalidate", "category": "correctness", "est_minutes": 20, "evidence": "invalidate releases lock before delete"}]}
`

// JsonObjectOutputFormat aliases the block for module-internal contract tests:
// doctor's response_format probe prompt restates this block's key and severity
// lists, and a drift test in internal/doctor pins the two together.
const JsonObjectOutputFormat = jsonObjectOutputFormat

// promptForResponseFormat returns prompt as the agent declaring responseFormat
// should see it: swapped for json_object, byte-identical otherwise. Callers pass
// the agent's OWN declaration — a fallback never inherits its primary's.
// payloadStart is where the rendered payload begins in prompt; the swap never
// reads or rewrites anything from there on.
func promptForResponseFormat(prompt string, payloadStart int, responseFormat string) string {
	swapped, _ := promptForResponseFormatWithSpan(prompt, payloadStart, responseFormat)
	return swapped
}

// promptForResponseFormatWithSpan also returns the swapSpan describing what the
// swap changed, so callers can rebuild the pre-swap text on demand instead of
// holding a second full copy of the prompt.
func promptForResponseFormatWithSpan(prompt string, payloadStart int, responseFormat string) (string, swapSpan) {
	if responseFormat != registry.ResponseFormatJSONObject {
		return prompt, swapSpan{}
	}
	return swapOutputFormatSectionWithSpan(prompt, payloadStart)
}

// swapSpan records what the response_format ## Output Format swap changed, so the
// pre-swap text can be rebuilt on demand instead of held as a second full copy of
// the prompt (payload included) on every declared agent for the whole run. The zero
// value is "no swap": the unswapped text IS the prompt (undeclared or hand-built
// agent).
type swapSpan struct {
	done       bool   // a swap was applied (declared agent)
	appended   bool   // append arm: the shared block was appended after the prompt
	start, end int    // replace arm: the persona's own section, as spans in the UNSWAPPED text
	section    string // replace arm: the persona's original section text
}

// rebuildUnswapped inverts the swap byte-for-byte: given the swapped prompt it
// returns the text promptForResponseFormat was called with.
func (s swapSpan) rebuildUnswapped(swapped string) string {
	if !s.done {
		return swapped
	}
	if s.appended {
		return swapped[:len(swapped)-len("\n"+jsonObjectOutputFormat)]
	}
	return swapped[:s.start] + s.section + swapped[s.start+len(jsonObjectOutputFormat):]
}

// swapOutputFormatSection replaces the first ## Output Format section in
// prompt[:payloadStart] — the heading line up to the next "## " heading or the
// payload, whichever comes first — with jsonObjectOutputFormat. With no such
// heading before the payload (a custom persona), the block is appended instead,
// so a declared agent always learns the shape the API will force.
func swapOutputFormatSection(prompt string, payloadStart int) string {
	swapped, _ := swapOutputFormatSectionWithSpan(prompt, payloadStart)
	return swapped
}

// swapOutputFormatSectionWithSpan is swapOutputFormatSection plus the swapSpan a
// caller needs to rebuild the unswapped text later.
func swapOutputFormatSectionWithSpan(prompt string, payloadStart int) (string, swapSpan) {
	payloadStart = max(0, min(payloadStart, len(prompt)))
	i := outputFormatHeadingAt(prompt[:payloadStart])
	if i < 0 {
		return prompt + "\n" + jsonObjectOutputFormat, swapSpan{done: true, appended: true}
	}
	end := payloadStart
	if j := strings.Index(prompt[i+len(outputFormatHeading):payloadStart], "\n## "); j >= 0 {
		end = i + len(outputFormatHeading) + j
	}
	// prompt[end:] keeps the blank line before the next heading.
	return prompt[:i] + jsonObjectOutputFormat + prompt[end:], swapSpan{done: true, start: i, end: end, section: prompt[i:end]}
}

// outputFormatHeadingAt returns the offset of the first line in s that is
// exactly the heading (trailing spaces or tabs allowed), or -1. A near-miss such
// as "### Output Format" or "##Output Format" is not the heading.
func outputFormatHeadingAt(s string) int {
	for off := 0; ; {
		k := strings.Index(s[off:], outputFormatHeading)
		if k < 0 {
			return -1
		}
		i := off + k
		lineEnd := strings.IndexByte(s[i:], '\n')
		if lineEnd < 0 {
			lineEnd = len(s) - i
		}
		if (i == 0 || s[i-1] == '\n') && strings.TrimRight(s[i+len(outputFormatHeading):i+lineEnd], " \t\r") == "" {
			return i
		}
		off = i + len(outputFormatHeading)
	}
}

// payloadMarker stands in for the payload when renderedPayloadStart re-renders a
// persona to find where its payload lands. Printable and escaper-resistant: the
// text/template escapers (%q, html, js, urlquery) wrap or leave alphanumeric
// markers alone rather than rewriting them to something unfindable, so a persona
// that transforms its payload still shows the marker core. The random-ish suffix
// keeps the core out of honest persona text and real diffs.
const payloadMarker = "atcr-payload-marker-v3x9q"

// renderedPayloadStart returns where ctx.Payload begins in prompt, the render of
// tmpl over ctx. It re-renders tmpl with a marker payload, since searching prompt
// for the payload text can match the persona's own words first. It returns 0 — the
// swap then only appends — whenever the marker cannot be found or the two renders
// disagree before it: a missing marker is indistinguishable from a transform that
// rewrote it, and falling back to len(prompt) would let the swap search (and
// rewrite) the diff itself.
func renderedPayloadStart(prompt, tmpl string, ctx payload.PayloadContext) int {
	ctx.Payload = payloadMarker
	marked, err := payload.RenderPrompt(tmpl, ctx)
	if err != nil {
		return 0
	}
	k := strings.Index(marked, payloadMarker)
	if k < 0 {
		return 0
	}
	if k > len(prompt) || prompt[:k] != marked[:k] {
		return 0
	}
	return k
}
