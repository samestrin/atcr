package verify

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/reconcile"
)

// Verdict enum values — the only values reconcile.Verification.Verdict may hold.
// parseVerdict validates every skeptic response against this set before it is
// persisted (the writer-validates contract documented on reconcile.Verification).
const (
	verdictConfirmed    = "confirmed"
	verdictRefuted      = "refuted"
	verdictUnverifiable = "unverifiable"
)

// parseVerdict extracts a verdict + reasoning from a raw skeptic response into a
// reconcile.Verification. It never fails on bad input: any unparseable, empty, or
// out-of-enum response degrades to an "unverifiable" verdict with a diagnostic
// Notes field that preserves the raw text, so a finding is never dropped because a
// skeptic produced garbage. The error return is reserved for a future signature
// symmetry and is always nil today.
//
// Extraction tolerates real LLM output: a bare JSON object, a JSON object wrapped
// in markdown fences, or one embedded in prose are all handled by scanning for the
// first balanced {...} object. Extra JSON fields are ignored (default unmarshal
// behavior).
func parseVerdict(response string) (*reconcile.Verification, error) {
	if strings.TrimSpace(response) == "" {
		return &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "empty_response"}, nil
	}

	// Iterate candidate balanced JSON objects. Skip candidates that fail to
	// unmarshal or lack the "verdict" key — a decoy brace pair (Go struct{},
	// ${VAR}, example snippet) before the real verdict envelope should not
	// degrade the verdict to unverifiable. On extractJSONObject returning ""
	// (unbalanced leading brace), advance past the first '{' and retry.
	rest := response
	for {
		obj := extractJSONObject(rest)
		if obj == "" {
			next := strings.IndexByte(rest, '{')
			if next < 0 {
				break
			}
			rest = rest[next+1:]
			continue
		}
		// Use a pointer for Verdict so json.Unmarshal can distinguish a present
		// key (even empty) from an absent key — avoids a second unmarshal pass.
		var candidate struct {
			Verdict   *string `json:"verdict"`
			Reasoning string  `json:"reasoning"`
		}
		if json.Unmarshal([]byte(obj), &candidate) == nil && candidate.Verdict != nil {
			normVerdict := strings.ToLower(strings.TrimSpace(*candidate.Verdict))
			switch normVerdict {
			case verdictConfirmed, verdictRefuted, verdictUnverifiable:
				return &reconcile.Verification{Verdict: normVerdict, Notes: candidate.Reasoning}, nil
			default:
				return &reconcile.Verification{
					Verdict: verdictUnverifiable,
					Notes:   "invalid_verdict: " + truncateForNotes(*candidate.Verdict) + " (raw: " + truncateForNotes(response) + ")",
				}, nil
			}
		}
		idx := strings.Index(rest, obj)
		rest = rest[idx+len(obj):]
	}

	return &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "malformed_output: " + truncateForNotes(response)}, nil
}

// notesRawCap bounds how much raw skeptic text is embedded in a Verification.Notes
// diagnostic. The raw output flows into findings.json and the rendered report, so
// a runaway response must not bloat the artifacts. The cap is generous enough to
// preserve a normal malformed response in full.
const notesRawCap = 2000

// truncateForNotes returns s capped at notesRawCap bytes (on a rune boundary),
// appending an explicit elision marker so a truncated diagnostic is never mistaken
// for the model's complete output.
func truncateForNotes(s string) string {
	if len(s) <= notesRawCap {
		return s
	}
	cut := notesRawCap
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…[truncated]"
}

// extractJSONObject returns the first balanced {...} object in s, or "" when none
// exists. Brace matching is string-aware: braces inside a JSON string literal (and
// escaped quotes) do not affect depth, so a reasoning value containing braces does
// not truncate the object early. The returned slice is still validated by the
// caller's json.Unmarshal — extraction only locates a candidate.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
