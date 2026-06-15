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
