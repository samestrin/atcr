package fanout

import (
	"strings"

	"github.com/samestrin/atcr/internal/registry"
)

// outputFormatHeading opens the persona section the response_format swap
// replaces. Every embedded persona carries it before ## Payload, so on a rendered
// prompt the first match is the persona's own section, never diff text.
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

Rules: severity is one of CRITICAL, HIGH, MEDIUM, LOW; file_line is FILE:LINE copied exactly from the diff — never approximate, guess, or invent it; category is one lowercase word; est_minutes is an integer; evidence cites the offending code; quote code exactly as written, since JSON string escaping carries quotes, pipes, and newlines. If nothing is wrong, reply with exactly this object and nothing else: {"findings":[]}

Example:
{"findings":[{"severity": "HIGH", "file_line": "store/cache.go:88", "problem": "Get returns stale entry after Invalidate", "fix": "Delete key inside the same lock as Invalidate", "category": "correctness", "est_minutes": 20, "evidence": "invalidate releases lock before delete"}]}
`

// promptForResponseFormat returns prompt as the agent declaring responseFormat
// should see it: swapped for json_object, byte-identical otherwise. Callers pass
// the agent's OWN declaration — a fallback never inherits its primary's.
func promptForResponseFormat(prompt, responseFormat string) string {
	if responseFormat != registry.ResponseFormatJSONObject {
		return prompt
	}
	return swapOutputFormatSection(prompt)
}

// swapOutputFormatSection replaces the first ## Output Format section of a
// rendered prompt — the heading up to the next "## " heading, or end of prompt —
// with jsonObjectOutputFormat. A prompt with no such heading (a custom persona)
// gets the block appended instead, so a declared agent always learns the shape
// the API will force.
func swapOutputFormatSection(prompt string) string {
	i := strings.Index(prompt, outputFormatHeading)
	if i < 0 {
		return prompt + "\n" + jsonObjectOutputFormat
	}
	rest := prompt[i+len(outputFormatHeading):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		// rest[j:] keeps the blank line before the next heading.
		return prompt[:i] + jsonObjectOutputFormat + rest[j:]
	}
	return prompt[:i] + jsonObjectOutputFormat
}
