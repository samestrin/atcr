package verify

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/jsonrepair"
	reclib "github.com/samestrin/atcr/reconcile"
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
	forEachJSONObjectAt(s, func(obj string, _ int) bool { return fn(obj) })
}

// forEachJSONObjectAt is forEachJSONObject that also hands fn the byte offset of
// obj in s, for the verdict repair pass, which must know where a candidate sits.
func forEachJSONObjectAt(s string, fn func(obj string, start int) bool) {
	off := 0
	for {
		rest := s[off:]
		obj := extractJSONObject(rest)
		if obj == "" {
			next := strings.IndexByte(rest, '{')
			if next < 0 {
				return
			}
			off += next + 1
			continue
		}
		start := off + strings.Index(rest, obj)
		off = start + len(obj)
		if fn(obj, start) {
			return
		}
	}
}

// parseCause is the STRUCTURAL reason a parseVerdict result is or is not usable.
// It exists so the envelope predicate (internal/verify/invoke.go's usableVerdict)
// can classify a result without inspecting the human-readable Notes string.
//
// Keying on Notes text was a real defect: on a real verdict Notes carries the
// MODEL'S OWN REASONING verbatim, so a reply whose reasoning began with
// "malformed_output:" or "invalid_verdict:" was misread as carrying no envelope.
// Worse than a lost signal — the candidate walk CONTINUES after a rejected
// candidate, so the committed verdict could be skipped and a later decoy graded:
// a silently wrong verdict (TD internal/verify/invoke.go:712).
//
// Deliberately unexported and internal to this package: reclib.Verification lives
// in the separate reconcile module, so adding a field there would force the
// tag-and-pin release procedure for a purely internal signal.
type parseCause int

const (
	// parseCauseUsable: an in-enum verdict was found. Notes is the reasoning.
	parseCauseUsable parseCause = iota
	// parseCauseMalformed: no verdict-keyed object was found at all. Notes is a
	// "malformed_output: ..." diagnostic.
	parseCauseMalformed
	// parseCauseInvalidEnum: an object carried a verdict key with an out-of-enum
	// value and nothing usable followed. Notes is an "invalid_verdict: ..."
	// diagnostic.
	parseCauseInvalidEnum
	// parseCauseEmpty: the input was blank, so there was nothing to parse. Notes is
	// "empty_response".
	parseCauseEmpty
)

// parseVerdictCause is parseVerdict plus the STRUCTURAL cause, for callers that
// must classify the result without reading Notes. parseVerdict is a thin wrapper
// over the same logic, so its signature — asserted require.NoError at every test
// call site — is unchanged.
func parseVerdictCause(response string) (*reclib.Verification, parseCause) {
	v, cause, _ := parseVerdictCore(response)
	return v, cause
}

// usableVerdictCause reports whether a structural cause is a real verdict rather
// than one of the three "nothing usable here" diagnostics. This is what the
// envelope predicate keys on.
func usableVerdictCause(c parseCause) bool { return c == parseCauseUsable }

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
	v, _ := parseVerdictCause(response)
	return v, nil
}

// jsonRepairedVerdictMetric counts verdicts accepted only after jsonrepair fixed
// their syntax. The marker is a count and nothing else: no reply text rides on
// it, and nothing is added to findings.json (epic 35.16.11.2.2.10).
const jsonRepairedVerdictMetric = `atcr_json_repaired_total{lane="verdict"}`

// parseVerdictCore does the parsing and reports the STRUCTURAL cause directly,
// so no caller has to recover it by pattern-matching the human-readable Notes
// string. See parseCause above for why that distinction is load-bearing.
//
// repaired reports that the usable verdict came from a candidate jsonrepair had
// to fix. It is false for every other result. The caller that accepts the
// verdict counts it (verdictFromAnswer); this function does not, because
// carriesVerdict asks it about sections of a reply while classifying it and
// would count one reply several times.
//
// Two passes, and repair is a last try across the WHOLE reply: the first pass
// reads every candidate strictly, and only when none of them yields a usable
// verdict does the second pass read the same candidates through
// jsonrepair.Repair. Repairing each candidate in place would let a loosely
// written quoted example ahead of the real verdict win, where strict parsing
// skips it. Even then the second pass reads only candidates that cannot be a
// prose example (repairPass). Candidate extraction is unchanged, so a reply missing a closing
// brace is still out of reach, and the truncation guard in invokeSkeptic refuses
// a cut-off reply before it gets here.
func parseVerdictCore(response string) (v *reclib.Verification, cause parseCause, repaired bool) {
	if strings.TrimSpace(response) == "" {
		return &reclib.Verification{Verdict: verdictUnverifiable, Notes: "empty_response"}, parseCauseEmpty, false
	}

	// Iterate candidate balanced JSON objects. Skip candidates that fail to
	// unmarshal or lack the "verdict" key — a decoy brace pair (Go struct{},
	// ${VAR}, example snippet) before the real verdict envelope should not
	// degrade the verdict to unverifiable. The walk itself lives in
	// forEachJSONObject so this reader, carriesVerdict and parseExecutorResponse
	// cannot diverge on what "the envelope" is (TD internal/verify/invoke.go:782).
	var result *reclib.Verification
	var invalidEnum *string
	read := func(obj string) *reclib.Verification {
		// Use a pointer for Verdict so json.Unmarshal can distinguish a present
		// key (even empty) from an absent key — avoids a second unmarshal pass.
		var candidate struct {
			Verdict   *string `json:"verdict"`
			Reasoning string  `json:"reasoning"`
		}
		if json.Unmarshal([]byte(obj), &candidate) != nil || candidate.Verdict == nil {
			return nil
		}
		normVerdict := strings.ToLower(strings.TrimSpace(*candidate.Verdict))
		switch normVerdict {
		case verdictConfirmed, verdictRefuted, verdictUnverifiable:
			// Notes rides into findings.json, verification.json and
			// DisagreementItem.Detail, and Reasoning is MODEL-CONTROLLED text, so the
			// documented notesRawCap must apply here too — it used to cover only the
			// three diagnostic Notes, leaving the runaway case it was written for
			// unbounded (TD internal/verify/verdict.go:61).
			return &reclib.Verification{Verdict: normVerdict, Notes: truncateForNotes(candidate.Reasoning)}
		default:
			// An out-of-enum value is NOT necessarily the committed verdict: it is
			// just as often a quoted example ahead of the real one. Do not
			// short-circuit — remember it and keep walking, so a committed verdict
			// sitting behind a quoted out-of-enum example is still graded.
			if invalidEnum == nil {
				v := truncateForNotes(*candidate.Verdict)
				invalidEnum = &v
			}
			return nil
		}
	}
	forEachJSONObject(response, func(obj string) bool {
		result = read(obj)
		return result != nil
	})
	if result == nil {
		result = repairPass(response, read)
		repaired = result != nil
	}
	if result != nil {
		return result, parseCauseUsable, repaired
	}

	if invalidEnum != nil {
		// No usable verdict anywhere; report the FIRST out-of-enum value with the full
		// raw text, exactly as the short-circuit used to.
		return &reclib.Verification{
			Verdict: verdictUnverifiable,
			Notes:   "invalid_verdict: " + *invalidEnum + " (raw: " + truncateForNotes(response) + ")",
		}, parseCauseInvalidEnum, false
	}

	return &reclib.Verification{Verdict: verdictUnverifiable, Notes: "malformed_output: " + truncateForNotes(response)}, parseCauseMalformed, false
}

// repairPass is parseVerdictCore's second pass. It reads candidates through
// jsonrepair.Repair, but only those that cannot be a loose example in the
// skeptic's prose, and returns the verdict read (nil when none is taken). read is
// parseVerdictCore's per-candidate reader.
//
// Repairing every candidate let a quoted example or schema echo such as
// `{'verdict': 'refuted', 'reasoning': 'example',}` become the graded verdict
// whenever no strict verdict followed it. Before repair that reply was
// malformed_output, which is unverifiable and blocks. So a repaired candidate is
// read only when it sits inside a ```json fence, or it is the single
// verdict-keyed object in the whole reply. A repaired `refuted` is held to the
// stricter of those: it must be fenced or be the whole reply, because `refuted`
// is the one grade that clears the gate (reconcile.IsFailing), the same policy
// verdictFromAnswer applies to sectionPrefixOnly. The accepted loss is a
// mis-written lone `refuted` with prose around it and no fence, which stays
// malformed_output as it did before repair.
//
// Repair returns ok=false for a candidate that is already valid JSON, so the
// strict pass's candidates are not read twice; they still count towards
// "single verdict-keyed object".
func repairPass(response string, read func(obj string) *reclib.Verification) *reclib.Verification {
	type candidate struct {
		obj, fixed string
		start      int
	}
	var loose []candidate
	keyed := 0
	forEachJSONObjectAt(response, func(obj string, start int) bool {
		fixed, ok := jsonrepair.Repair(obj)
		if ok {
			if hasVerdictKey(fixed) {
				keyed++
				loose = append(loose, candidate{obj: obj, fixed: fixed, start: start})
			}
		} else if hasVerdictKey(obj) {
			keyed++
		}
		return false
	})

	fences := jsonFenceSpans(response)
	whole := strings.TrimSpace(response)
	for _, c := range loose {
		fenced := false
		for _, f := range fences {
			if c.start >= f[0] && c.start+len(c.obj) <= f[1] {
				fenced = true
				break
			}
		}
		if !fenced && keyed != 1 {
			continue
		}
		v := read(c.fixed)
		if v == nil {
			continue
		}
		if v.Verdict == verdictRefuted && !fenced && c.obj != whole {
			continue
		}
		return v
	}
	return nil
}

// hasVerdictKey reports whether obj is a JSON object carrying a "verdict" key,
// whatever its value.
func hasVerdictKey(obj string) bool {
	var candidate struct {
		Verdict *string `json:"verdict"`
	}
	return json.Unmarshal([]byte(obj), &candidate) == nil && candidate.Verdict != nil
}

// jsonFenceSpans returns the [start, end) byte ranges of the contents of every
// closed ```json fence in s. The opener is "```json" at the start of a line; the
// contents run from the line after it to the next "```". An unclosed fence is not
// a span, so nothing in it counts as fenced.
func jsonFenceSpans(s string) [][2]int {
	var spans [][2]int
	off := 0
	for {
		i := strings.Index(s[off:], "```json")
		if i < 0 {
			return spans
		}
		open := off + i
		off = open + len("```json")
		if open > 0 && s[open-1] != '\n' {
			continue
		}
		nl := strings.IndexByte(s[off:], '\n')
		if nl < 0 {
			return spans
		}
		start := off + nl + 1
		end := strings.Index(s[start:], "```")
		if end < 0 {
			return spans
		}
		spans = append(spans, [2]int{start, start + end})
		off = start + end + len("```")
	}
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
