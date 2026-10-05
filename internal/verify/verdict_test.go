package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVerdict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		response    string
		wantVerdict string
		wantNotes   string // exact when non-empty-sensitive; otherwise checked via wantNotesContains
	}{
		{"confirmed", `{"verdict": "confirmed", "reasoning": "evidence holds up"}`, verdictConfirmed, "evidence holds up"},
		{"refuted", `{"verdict": "refuted", "reasoning": "code path unreachable"}`, verdictRefuted, "code path unreachable"},
		{"unverifiable", `{"verdict": "unverifiable", "reasoning": "insufficient context"}`, verdictUnverifiable, "insufficient context"},
		{"extra fields ignored", `{"verdict": "confirmed", "reasoning": "ok", "extra_field": "ignored", "confidence": 0.9}`, verdictConfirmed, "ok"},
		{"empty reasoning", `{"verdict": "confirmed", "reasoning": ""}`, verdictConfirmed, ""},
		{"missing reasoning", `{"verdict": "confirmed"}`, verdictConfirmed, ""},
		{"fenced json", "```json\n{\"verdict\": \"confirmed\", \"reasoning\": \"ok\"}\n```", verdictConfirmed, "ok"},
		{"prose embedded json", `Here is my verdict: {"verdict": "refuted", "reasoning": "wrong file"} — hope that helps.`, verdictRefuted, "wrong file"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, err := parseVerdict(tt.response)
			require.NoError(t, err)
			require.NotNil(t, v)
			assert.Equal(t, tt.wantVerdict, v.Verdict)
			assert.Equal(t, tt.wantNotes, v.Notes)
		})
	}
}

func TestParseVerdict_ErrorConditions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		response     string
		wantVerdict  string
		wantNotesHas string
	}{
		{"malformed json", `{verdict: confirmed}`, verdictUnverifiable, "malformed_output: {verdict: confirmed}"},
		{"invalid verdict enum", `{"verdict": "maybe", "reasoning": "unclear"}`, verdictUnverifiable, "invalid_verdict: maybe"},
		{"empty verdict enum", `{"verdict": "", "reasoning": "no opinion"}`, verdictUnverifiable, "invalid_verdict:"},
		{"empty response", "", verdictUnverifiable, "empty_response"},
		{"whitespace only response", "   \n\t ", verdictUnverifiable, "empty_response"},
		{"no json object prose", `I cannot determine the verdict at this time.`, verdictUnverifiable, "malformed_output: I cannot determine the verdict at this time."},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, err := parseVerdict(tt.response)
			require.NoError(t, err)
			require.NotNil(t, v)
			assert.Equal(t, tt.wantVerdict, v.Verdict)
			assert.Contains(t, v.Notes, tt.wantNotesHas)
		})
	}
}

// TestParseVerdict_VerdictCaseAndSpaceTolerance verifies that mixed-case and
// whitespace-padded verdict values are normalised before the enum switch so
// "Confirmed", " refuted " and "UNVERIFIABLE" are not degraded to unverifiable.
func TestParseVerdict_VerdictCaseAndSpaceTolerance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
	}{
		{`{"verdict": "Confirmed", "reasoning": "ok"}`, verdictConfirmed},
		{`{"verdict": " refuted ", "reasoning": "ok"}`, verdictRefuted},
		{`{"verdict": "UNVERIFIABLE", "reasoning": "ok"}`, verdictUnverifiable},
		{`{"verdict": "Refuted", "reasoning": "ok"}`, verdictRefuted},
	}
	for _, c := range cases {
		c := c
		t.Run(c.raw, func(t *testing.T) {
			t.Parallel()
			v, err := parseVerdict(c.raw)
			require.NoError(t, err)
			assert.Equal(t, c.want, v.Verdict,
				"verdict must be case/space normalised before enum check")
		})
	}
}

// TestParseVerdict_InvalidEnumPreservesRaw asserts the full raw response is kept
// in Notes for an invalid enum so the human can audit the skeptic's actual output.
func TestParseVerdict_InvalidEnumPreservesRaw(t *testing.T) {
	t.Parallel()
	raw := `{"verdict": "maybe", "reasoning": "unclear"}`
	v, err := parseVerdict(raw)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "(raw: "+raw+")")
}

// TestParseVerdict_BracesAndQuotesInReasoning ensures the string-aware brace
// scanner does not truncate a reasoning value that itself contains braces and
// escaped quotes.
func TestParseVerdict_BracesAndQuotesInReasoning(t *testing.T) {
	t.Parallel()
	raw := `{"verdict": "confirmed", "reasoning": "the block { ... } and a \"quote\" are fine"}`
	v, err := parseVerdict(raw)
	require.NoError(t, err)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, `the block { ... } and a "quote" are fine`, v.Notes)
}

// TestParseVerdict_UnbalancedBrace covers the no-closing-brace path: an opening
// brace with no match yields no extractable object → malformed_output.
func TestParseVerdict_UnbalancedBrace(t *testing.T) {
	t.Parallel()
	raw := `here is an opening { but it never closes`
	v, err := parseVerdict(raw)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "malformed_output")
}

// TestParseVerdict_TruncatesRunawayMalformed ensures an oversized non-JSON
// response is capped in Notes (the LOW hardening from 2.2.A) while still flagged
// malformed.
func TestParseVerdict_TruncatesRunawayMalformed(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("x", notesRawCap+500) // no JSON object -> malformed path
	v, err := parseVerdict(big)
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "malformed_output: ")
	assert.Contains(t, v.Notes, "…[truncated]")
	assert.Less(t, len(v.Notes), len(big), "oversized raw text must be truncated in Notes")
}

// TestParseVerdict_MalformedFixture exercises the testdata malformed corpus.
func TestParseVerdict_MalformedFixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "malformed-response.txt"))
	require.NoError(t, err)
	v, err := parseVerdict(string(data))
	require.NoError(t, err)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
}

// TestParseVerdict_DecoyBracesBeforeVerdict checks that a decoy balanced brace
// pair before the real verdict envelope does not degrade the verdict to
// unverifiable. Regression for extractJSONObject returning the first '{...}'
// regardless of whether it contains a verdict key.
func TestParseVerdict_DecoyBracesBeforeVerdict(t *testing.T) {
	t.Parallel()
	raw := `{} {"verdict": "confirmed", "reasoning": "ok"}`
	v, err := parseVerdict(raw)
	require.NoError(t, err)
	assert.Equal(t, verdictConfirmed, v.Verdict)
	assert.Equal(t, "ok", v.Notes)
}

// TestParseVerdict_UnbalancedLeadingBraceFollowedByValidEnvelope checks that
// an unbalanced leading '{' (e.g. prose containing a Go struct example) does
// not swallow the real verdict envelope that appears later in the response.
func TestParseVerdict_UnbalancedLeadingBraceFollowedByValidEnvelope(t *testing.T) {
	t.Parallel()
	raw := `here is an unbalanced { brace followed by {"verdict": "refuted", "reasoning": "no evidence"}`
	v, err := parseVerdict(raw)
	require.NoError(t, err)
	assert.Equal(t, verdictRefuted, v.Verdict)
	assert.Equal(t, "no evidence", v.Notes)
}

// TestParseVerdict_IsTagUnaware pins parseVerdict's own contract: it is a
// POSITION-BLIND, tag-unaware first-match parser. It scans for the first balanced
// object carrying a "verdict" key and does not know what a think tag is.
//
// This is deliberately not a think-handling test — internal/llmclient owns every
// tag rule, and the invoke.go call site owns the strip and the non-leading refusal.
// What this pins is WHY that call site must exist: given markup-wrapped input,
// parseVerdict alone takes whatever keyed object comes first, so a draft inside a
// leading block would be graded as the skeptic's answer. The rows below are the
// parser's real behavior on those inputs; if parseVerdict ever grows tag awareness,
// these fail on purpose.
func TestParseVerdict_IsTagUnaware(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		response    string
		wantVerdict string
		wantNotes   string
	}{
		{
			// The parser takes the DRAFT: this is the vulnerability the invoke.go
			// guard neutralises before the call.
			name:        "a draft verdict inside a leading block is what the parser alone takes",
			response:    `<think>{"verdict": "confirmed", "reasoning": "draft, wrong"}</think>{"verdict": "refuted", "reasoning": "real answer"}`,
			wantVerdict: verdictConfirmed,
			wantNotes:   "draft, wrong",
		},
		{
			// No opener: the parser skips the stray closer text and reads the object.
			name:        "a lone closer with no opener does not stop the parse",
			response:    `draft</think>{"verdict": "refuted", "reasoning": "real answer"}`,
			wantVerdict: verdictRefuted,
			wantNotes:   "real answer",
		},
		{
			name:        "tags quoted inside a reasoning string are preserved verbatim",
			response:    `{"verdict": "confirmed", "reasoning": "the code never looks for </think> at all"}`,
			wantVerdict: verdictConfirmed,
			wantNotes:   "the code never looks for </think> at all",
		},
		{
			name:        "both tags quoted after real answer text survive intact",
			response:    `{"verdict": "confirmed", "reasoning": "the handler drops text between <think> and </think>"}`,
			wantVerdict: verdictConfirmed,
			wantNotes:   "the handler drops text between <think> and </think>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, err := parseVerdict(tt.response)
			require.NoError(t, err)
			assert.Equal(t, tt.wantVerdict, v.Verdict)
			assert.Equal(t, tt.wantNotes, v.Notes)
		})
	}
}

// Sprint 35.16.11.2.1 AC 03-02: under response_format json_object the API returns
// a bare object with no fence and no prose, although the skeptic prompt asks for
// a fenced one. The parser must read that shape, which is why this lane needs no
// Output Format swap.
//
// TD (35.16.11.2.1): the JSON-mode-relevant case is a reasoning value that
// itself embeds braces or a markdown fence — exactly what a brace matcher that
// is NOT string-aware would truncate. The original bare {"verdict":"refuted",
// "reasoning":"x"} form was already covered verbatim by the TestParseVerdict
// table above and could not fail independently, so it only looked like evidence.
func TestParseVerdict_BareJSONModeObject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		response string
		notes    string
	}{
		// An UNBALANCED } inside the string: a brace matcher that is not
		// string-aware closes the object early and json.Unmarshal fails. A
		// balanced {"a":1} pair would survive that mutation (depth returns to 0
		// at the real end either way), so the unbalanced form is the one that
		// actually pins string-awareness.
		{"reasoning embeds an unbalanced brace", `{"verdict":"refuted","reasoning":"a literal } inside prose does not end the object"}`, "a literal } inside prose does not end the object"},
		{"reasoning embeds a markdown fence", "{\"verdict\":\"refuted\",\"reasoning\":\"the reply's ```json block is not the verdict\"}", "the reply's ```json block is not the verdict"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v, err := parseVerdict(tt.response)
			require.NoError(t, err)
			require.NotNil(t, v)
			assert.Equal(t, verdictRefuted, v.Verdict)
			assert.Equal(t, tt.notes, v.Notes)
		})
	}
}

// TD internal/verify/invoke.go:782: carriesVerdict was taught to iterate past an
// out-of-enum decoy, but parseVerdict — verdictFromAnswer's actual grader — still
// SHORT-CIRCUITED on the first verdict-keyed object, in-enum or not. The predicate
// and the grader therefore disagreed about what "the envelope" is, so for a reply
// whose suffix holds a quoted out-of-enum example BEFORE the real verdict,
// carriesVerdict(suffix) returned true, classifyUnopenedCloser picked
// SectionAfterCloser, and parseVerdict then graded the DECOY — losing the
// committed verdict to `unverifiable` and embedding only the post-closer fragment.
// Either direction: iterate past the out-of-enum object the way
// carriesVerdict/parseExecutorResponse do, so predicate and grader cannot disagree.
func TestParseVerdict_IteratesPastAnOutOfEnumDecoy(t *testing.T) {
	t.Parallel()
	raw := `{"verdict":"maybe","reasoning":"an example"} ` + "\n" +
		`{"verdict":"confirmed","reasoning":"REAL"}`

	v, err := parseVerdict(raw)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict,
		"the committed verdict sits BEHIND the quoted example and must be graded")
	assert.Equal(t, "REAL", v.Notes)
	assert.NotContains(t, v.Notes, "(raw:",
		"the real verdict was graded, so no diagnostic raw-text embed should appear")
}

// The single-object shape must be unchanged: an out-of-enum verdict with nothing
// usable after it still reports invalid_verdict, preserving the diagnostic an
// operator reads.
func TestParseVerdict_SoleOutOfEnumObjectStillDiagnosesInvalidVerdict(t *testing.T) {
	t.Parallel()
	v, err := parseVerdict(`{"verdict": "maybe", "reasoning": "unclear"}`)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict)
	assert.Contains(t, v.Notes, "invalid_verdict: maybe")
}

// The end-to-end shape from the finding, through verdictFromAnswer: a bare closer
// with nothing usable before it, a quoted out-of-enum example after it, and the
// real committed verdict last. The predicate must select that section and the
// grader must read the committed verdict out of it.
func TestVerdictFromAnswer_GradesPastAnOutOfEnumDecoyAfterTheCloser(t *testing.T) {
	t.Parallel()
	answer := "weighing two approaches \n " + string(rune(0x3c)) + "/think" + string(rune(0x3e)) + " \n" +
		` An example is {"verdict":"maybe"}.` + "\n" +
		`{"verdict":"refuted","reasoning":"REAL"}`

	v, ambiguous, _ := verdictFromAnswer(answer)
	require.False(t, ambiguous, "both sides carry a verdict, so the section is not ambiguous")
	require.NotNil(t, v)
	assert.Equal(t, verdictRefuted, v.Verdict,
		"the committed refuted must not be lost to the quoted out-of-enum example")
	assert.Equal(t, "REAL", v.Notes)
	assert.NotContains(t, v.Notes, "(raw:",
		"the post-closer fragment alone must not be the only text kept")
}
