package verify

import (
	"encoding/json"
	reclib "github.com/samestrin/atcr/reconcile"
	"strings"
	"unicode/utf8"
)

// Verdict enum values — the only values reclib.Verification.Verdict may hold.
// parseVerdict validates every skeptic response against this set before it is
// persisted (the writer-validates contract documented on reclib.Verification).
const (
	verdictConfirmed    = "confirmed"
	verdictRefuted      = "refuted"
	verdictUnverifiable = "unverifiable"
)

// forEachJSONObject walks the candidate balanced JSON objects in s in order,
// stepping past an unbalanced leading brace exactly as every caller's own loop
// did, and calls fn on each. It stops when fn returns true or the input is
// exhausted.
//
// This walk used to be hand-copied into parseVerdict, carriesVerdict and
// parseExecutorResponse. The copies drifted — carriesVerdict learned to ITERATE
// past an out-of-enum decoy while parseVerdict still short-circuited on the first
// verdict-keyed object — and the divergence was a real defect: the predicate
// selected a section its own grader then refused to read, losing a committed
// verdict to `unverifiable` (TD internal/verify/invoke.go:782, :727, :733). One
// walk, one advance rule, so the three readers cannot disagree about what "the
// envelope" is.
//
// rest is advanced BEFORE fn runs, so a callback that only remembers state and
// asks to continue can never leave the offset unmoved — the infinite loop a
// callback-mutates-nothing shape would otherwise allow.
func forEachJSONObject(s string, fn func(obj string) bool) {
	rest := s
	for {
		obj := extractJSONObject(rest)
		if obj == "" {
			next := strings.IndexByte(rest, '{')
			if next < 0 {
				return
			}
			rest = rest[next+1:]
			continue
		}
		rest = rest[strings.Index(rest, obj)+len(obj):]
		if fn(obj) {
			return
		}
	}
}

// parseVerdict extracts a verdict + reasoning from a raw skeptic response into a
// reclib.Verification. It never fails on bad input: any unparseable, empty, or
// out-of-enum response degrades to an "unverifiable" verdict with a diagnostic
// Notes field that preserves the raw text, so a finding is never dropped because a
// skeptic produced garbage. The error return is reserved for a future signature
// symmetry and is always nil today.
//
// Extraction tolerates real LLM output: a bare JSON object, a JSON object wrapped
// in markdown fences, or one embedded in prose are all handled by scanning for the
// first balanced {...} object. Extra JSON fields are ignored (default unmarshal
// behavior).
func parseVerdict(response string) (*reclib.Verification, error) {
	if strings.TrimSpace(response) == "" {
		return &reclib.Verification{Verdict: verdictUnverifiable, Notes: "empty_response"}, nil
	}

	// Iterate candidate balanced JSON objects. Skip candidates that fail to
	// unmarshal or lack the "verdict" key — a decoy brace pair (Go struct{},
	// ${VAR}, example snippet) before the real verdict envelope should not
	// degrade the verdict to unverifiable. The walk itself lives in
	// forEachJSONObject so this reader, carriesVerdict and parseExecutorResponse
	// cannot diverge on what "the envelope" is (TD internal/verify/invoke.go:782).
	var result *reclib.Verification
	var invalidEnum *string
	forEachJSONObject(response, func(obj string) bool {
		// Use a pointer for Verdict so json.Unmarshal can distinguish a present
		// key (even empty) from an absent key — avoids a second unmarshal pass.
		var candidate struct {
			Verdict   *string `json:"verdict"`
			Reasoning string  `json:"reasoning"`
		}
		if json.Unmarshal([]byte(obj), &candidate) != nil || candidate.Verdict == nil {
			return false
		}
		normVerdict := strings.ToLower(strings.TrimSpace(*candidate.Verdict))
		switch normVerdict {
		case verdictConfirmed, verdictRefuted, verdictUnverifiable:
			// Notes rides into findings.json, verification.json and
			// DisagreementItem.Detail, and Reasoning is MODEL-CONTROLLED text, so the
			// documented notesRawCap must apply here too — it used to cover only the
			// three diagnostic Notes, leaving the runaway case it was written for
			// unbounded (TD internal/verify/verdict.go:61).
			result = &reclib.Verification{Verdict: normVerdict, Notes: truncateForNotes(candidate.Reasoning)}
			return true
		default:
			// An out-of-enum value is NOT necessarily the committed verdict: it is
			// just as often a quoted example ahead of the real one. Do not
			// short-circuit — remember it and keep walking, so a committed verdict
			// sitting behind a quoted out-of-enum example is still graded.
			if invalidEnum == nil {
				v := truncateForNotes(*candidate.Verdict)
				invalidEnum = &v
			}
			return false
		}
	})
	if result != nil {
		return result, nil
	}

	if invalidEnum != nil {
		// No usable verdict anywhere; report the FIRST out-of-enum value with the full
		// raw text, exactly as the short-circuit used to.
		return &reclib.Verification{
			Verdict: verdictUnverifiable,
			Notes:   "invalid_verdict: " + *invalidEnum + " (raw: " + truncateForNotes(response) + ")",
		}, nil
	}

	return &reclib.Verification{Verdict: verdictUnverifiable, Notes: "malformed_output: " + truncateForNotes(response)}, nil
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
