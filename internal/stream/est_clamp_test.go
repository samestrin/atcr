package stream

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// est_minutes must clamp to 0..maxModelEstMinutes on EVERY decode path, the
// same typo-guard the v1 pipe rows and the model-JSON path apply: a typo'd
// negative or absurd value from any writer must not reach a Finding, or
// reconcile's max-EST merge reads differently by file format.
func TestParseV2BodyClampsEstMinutes(t *testing.T) {
	// TOON table path: negative and absurd est_minutes. Build the body with the
	// real writer (so the TOON syntax is valid), then poison two est_minutes
	// cells. parseV2Body receives the body with the version header already
	// stripped (DecodeTabular reads the first non-blank line as the header).
	var b strings.Builder
	require.NoError(t, WriteSourceV2(&b, []Finding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "pa", Fix: "fa", Category: "correctness", EstMinutes: 5, Evidence: "ev", Reviewer: "rev"},
		{Severity: "LOW", File: "b.go", Line: 2, Problem: "pb", Fix: "fb", Category: "style", EstMinutes: 7, Evidence: "ev", Reviewer: "rev"},
	}))
	_, body, _ := strings.Cut(b.String(), "\n")
	body = strings.Replace(body, ",5,ev,rev", ",-5,ev,rev", 1)
	body = strings.Replace(body, ",7,ev,rev", ",99999999999,ev,rev", 1)
	res, err := parseV2Body(body)
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)
	require.Equal(t, 0, res.Findings[0].EstMinutes, "negative est_minutes clamps to 0 on the TOON path")
	require.Equal(t, maxModelEstMinutes, res.Findings[1].EstMinutes, "absurd est_minutes clamps to the bound on the TOON path")

	// Envelope path: same clamp.
	env := `{"axi_format":"json","axi_notice":"","data":{"findings":[` +
		`{"severity":"HIGH","file_line":"a.go:1","problem":"pa","fix":"fa","category":"correctness","est_minutes":-5,"evidence":"ev","reviewer":"rev"}]}}`
	res, err = parseV2Body(env)
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	require.Equal(t, 0, res.Findings[0].EstMinutes, "negative est_minutes clamps to 0 on the envelope path")
}
