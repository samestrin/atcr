package verify

import (
	"encoding/json"
	"strings"

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

	var parsed struct {
		Verdict   string `json:"verdict"`
		Reasoning string `json:"reasoning"`
	}
	obj := extractJSONObject(response)
	if obj == "" || json.Unmarshal([]byte(obj), &parsed) != nil {
		return &reconcile.Verification{Verdict: verdictUnverifiable, Notes: "malformed_output: " + response}, nil
	}

	switch parsed.Verdict {
	case verdictConfirmed, verdictRefuted, verdictUnverifiable:
		return &reconcile.Verification{Verdict: parsed.Verdict, Notes: parsed.Reasoning}, nil
	default:
		return &reconcile.Verification{
			Verdict: verdictUnverifiable,
			Notes:   "invalid_verdict: " + parsed.Verdict + " (raw: " + response + ")",
		}, nil
	}
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
