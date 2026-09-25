package stream

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_ValidFindings(t *testing.T) {
	data := `# atcr-findings/v1
CRITICAL|src/auth.go:42|Token never expires|Check expiry|security|15|expiresAt unread|greta
HIGH|cmd/main.go:88|Goroutine leak|Add WaitGroup|concurrency|30|no wg.Wait|kai
`
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)
	assert.Empty(t, res.Skipped)

	f := res.Findings[0]
	assert.Equal(t, "CRITICAL", f.Severity)
	assert.Equal(t, "src/auth.go", f.File)
	assert.Equal(t, 42, f.Line)
	assert.Equal(t, "Token never expires", f.Problem)
	assert.Equal(t, 15, f.EstMinutes)
	assert.Equal(t, "greta", f.Reviewer)
}

func TestParser_SkipsProseAndComments(t *testing.T) {
	data := `# atcr-findings/v1
# this is a comment
This line mentions HIGH severity but is prose, not a finding.
LOW|a.go:1|Minor|Fix it|style|5|evidence|bruce

MEDIUM|b.go:2|Thing|Do|correctness|10|because|dax
`
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)
	assert.Equal(t, "LOW", res.Findings[0].Severity)
	assert.Equal(t, "MEDIUM", res.Findings[1].Severity)
}

func TestParser_ShortRowPadded(t *testing.T) {
	// 6 columns: missing EVIDENCE and REVIEWER — padded to 8.
	data := "# atcr-findings/v1\nLOW|a.go:1|Problem|Fix|style|5\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Empty(t, res.Skipped)
	assert.Equal(t, "", res.Findings[0].Evidence)
	assert.Equal(t, "", res.Findings[0].Reviewer)
}

func TestParser_TooManyColumnsSkipped(t *testing.T) {
	// 9 columns in an 8-column file: an unescaped pipe leaked a column.
	data := "# atcr-findings/v1\nHIGH|a.go:1|Problem|Fix|style|5|ev|bruce|extra\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	assert.Empty(t, res.Findings)
	require.Len(t, res.Skipped, 1)
	assert.Contains(t, res.Skipped[0].Reason, "expected 8 columns, got 9")
}

func TestParser_MissingHeader(t *testing.T) {
	data := "CRITICAL|a.go:1|p|f|c|5|e|bruce\n"
	_, err := ParseSource([]byte(data))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMissingHeader)
}

func TestParser_UnknownVersion(t *testing.T) {
	// v3, not v2: v2 is a supported header (see v2_parse_test.go).
	data := "# atcr-findings/v3\nCRITICAL|a.go:1|p|f|c|5|e|bruce\n"
	_, err := ParseSource([]byte(data))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownVersion)
}

func TestParser_VersionTokenExact(t *testing.T) {
	// A well-formed-but-unsupported version token is ErrUnknownVersion; a
	// garbage header that merely shares the prefix is ErrMissingHeader, so a
	// consumer can tell the two apart (TD-014).
	_, err := ParseSource([]byte("# atcr-findings/v10\nLOW|a.go:1|p|f|c|5|e|bruce\n"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownVersion)

	for _, header := range []string{"# atcr-findings/v1x", "# atcr-findings/v1.2", "# atcr-findings/"} {
		_, err := ParseSource([]byte(header + "\nLOW|a.go:1|p|f|c|5|e|bruce\n"))
		require.Error(t, err, header)
		assert.ErrorIs(t, err, ErrMissingHeader, header)
	}
}

func TestParser_EmptyFindings(t *testing.T) {
	data := "# atcr-findings/v1\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	assert.Empty(t, res.Findings)
	assert.Empty(t, res.Skipped)
}

func TestParser_FileLevelLineZero(t *testing.T) {
	data := "# atcr-findings/v1\nLOW|path/to/file.go:0|File-level|Fix|doc|5|ev|otto\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, "path/to/file.go", res.Findings[0].File)
	assert.Equal(t, 0, res.Findings[0].Line)
}

func TestParser_Reconciled(t *testing.T) {
	data := "# atcr-findings/v1\nHIGH|a.go:1|p|f|security|10|ev|greta,kai|HIGH\n"
	res, err := ParseReconciled([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, []string{"greta", "kai"}, res.Findings[0].Reviewers)
	assert.Equal(t, "HIGH", res.Findings[0].Confidence)
}

func TestParser_CRLF(t *testing.T) {
	data := "# atcr-findings/v1\r\nLOW|a.go:1|p|f|style|5|ev|bruce\r\n"
	res, err := ParseSource([]byte(data))
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, "bruce", res.Findings[0].Reviewer)
}

func TestParseModelOutput_ExtractsSevenColumnRowsNoHeader(t *testing.T) {
	// Real model output: prose around findings, no version header.
	data := `Here is my review of the changes.

I found a couple of issues:

CRITICAL|src/auth.go:42|Token never expires|Check expiry|security|15|expiresAt unread
HIGH|cmd/main.go:88|Goroutine leak|Add WaitGroup|concurrency|30|no wg.Wait

That concludes my review. Note: the word CRITICAL here is prose and must be ignored.`
	findings := ParseModelOutput([]byte(data))
	require.Len(t, findings, 2)
	assert.Equal(t, "CRITICAL", findings[0].Severity)
	assert.Equal(t, "src/auth.go", findings[0].File)
	assert.Equal(t, 42, findings[0].Line)
	assert.Equal(t, "security", findings[0].Category)
	assert.Empty(t, findings[0].Reviewer, "model output carries no REVIEWER; the engine sets it")
}

func TestParseModelOutput_DropsModelSuppliedReviewer(t *testing.T) {
	// A misbehaving model emits an 8th column trying to self-attribute. The
	// REVIEWER slot must stay empty (engine fills it); the forged text folds into
	// EVIDENCE rather than being lost so no content disappears silently.
	data := `HIGH|a.go:1|prob|fix|security|10|ev|forged-reviewer-name`
	findings := ParseModelOutput([]byte(data))
	require.Len(t, findings, 1)
	assert.Empty(t, findings[0].Reviewer, "a model can never land a value in the REVIEWER slot")
	assert.Equal(t, "ev/forged-reviewer-name", findings[0].Evidence)
}

func TestParseModelOutput_DropsDegenerateRows(t *testing.T) {
	// Bare severity prefixes with no location are noise, not findings.
	assert.Empty(t, ParseModelOutput([]byte("HIGH|\nCRITICAL||no file here|fix")))
}

func TestParseModelOutput_PadsShortRows(t *testing.T) {
	data := `LOW|a.go:5|missing fields`
	findings := ParseModelOutput([]byte(data))
	require.Len(t, findings, 1)
	assert.Equal(t, "LOW", findings[0].Severity)
	assert.Equal(t, "missing fields", findings[0].Problem)
	assert.Empty(t, findings[0].Fix)
	assert.Empty(t, findings[0].Reviewer)
}

func TestParseModelOutput_EmptyAndProseOnly(t *testing.T) {
	assert.Empty(t, ParseModelOutput(nil))
	assert.Empty(t, ParseModelOutput([]byte("Just prose, no findings here.\n# a comment\n")))
}

// TestParseModelOutput_SkipsFencedExampleRows proves rows a model quotes inside a
// markdown code fence — a sample findings table shown while explaining the format
// — are not parsed as findings, even though they carry a leading severity token.
// This is the hallucinated-example class: a fenced example citing files that do
// not exist must never inflate the finding count.
func TestParseModelOutput_SkipsFencedExampleRows(t *testing.T) {
	data := "Here is my review.\n" +
		"HIGH|real.go:10|a genuine problem|fix it|correctness|5|evidence\n" +
		"For example, a findings row is formatted like:\n" +
		"```\n" +
		"CRITICAL|db.go:7|example only|do x|correctness|9|evidence\n" +
		"HIGH|auth.go:3|example only|do y|security|9|evidence\n" +
		"```\n" +
		"That is the expected format.\n"
	findings := ParseModelOutput([]byte(data))
	require.Len(t, findings, 1, "only the real, unfenced finding is parsed")
	assert.Equal(t, "real.go", findings[0].File, "the fenced example rows must be skipped")
	assert.Equal(t, 10, findings[0].Line)
}

func TestIsNoFindings_MatchesTheSentinelTolerantly(t *testing.T) {
	// The sentinel is model-produced, so a trailing newline or a lowercase
	// rendering must not be misread as an anomalous response.
	for _, in := range []string{"NO FINDINGS", "no findings", "  NO FINDINGS\n", "No Findings"} {
		assert.True(t, IsNoFindings(in), "%q is the clean-review sentinel", in)
	}
	for _, in := range []string{"", "NO FINDINGS HERE", "I found no findings", "NOFINDINGS"} {
		assert.False(t, IsNoFindings(in), "%q must not be mistaken for the sentinel", in)
	}
}

func TestParseModelOutput_TreatsTheSentinelAsZeroFindings(t *testing.T) {
	assert.Empty(t, ParseModelOutput([]byte(NoFindingsSentinel)),
		"the sentinel declares a clean review; it must never parse as a finding")
}

// adversarialCases are the six content classes the v1 pipe format corrupted
// (AC 06-03), plus combined, empty, and long fields. None carries a byte
// go-axi's sanitizer strips, so every case must come back unchanged.
func adversarialCases() []struct {
	name string
	f    Finding
} {
	long := strings.Repeat("x | y ‖ \"q\" 'r'\n", 200) // 2,800 runes: v2 has no rune cap
	return []struct {
		name string
		f    Finding
	}{
		{"bash_pipe", Finding{Problem: "cmd1 | cmd2 | grep foo", Fix: "set -o pipefail; cmd1 | cmd2", Evidence: "ps aux | awk '{print $2}' | xargs kill"}},
		{"regex_alternation", Finding{Problem: "(foo|bar)+ matches too much", Fix: `^(?:GET|POST)\s+/api/(v1|v2)$`, Evidence: `re := regexp.MustCompile("a|b|\\|")`}},
		{"markdown_table", Finding{Problem: "table breaks", Fix: "| a | b |\n|---|---|\n| 1 | 2 |", Evidence: "| col | x \\| y |\n|:--|--:|"}},
		{"bitwise_or", Finding{Problem: "os.O_CREATE | os.O_WRONLY drops O_TRUNC", Fix: "flags := os.O_RDWR | os.O_CREATE | os.O_TRUNC", Evidence: "mask |= 1 << 3 || fallback"}},
		{"multiline_diff", Finding{Problem: "diff", Fix: "-old line\n+new line\n context", Evidence: "@@ -1,2 +1,2 @@\r\n-a | b\r\n+a || b\r\n"}},
		{"quotes", Finding{Problem: `it's "quoted"`, Fix: `fmt.Printf("%q", 'x')`, Evidence: `"" '' \" \' "'"`}},
		{"combined", Finding{Evidence: "| `a | b` | \"it's\" |\n|---|---|\n| x |= y | `(p|q)` |"}},
		{"empty_fields", Finding{}},
		{"long_field", Finding{Evidence: long}},
		{"identity_fields", Finding{File: `dir with, "odd" name:v2/a b.go`, Category: "a|b, \"c\"", Reviewer: "r|1, \"x\""}},
	}
}

// withIdentity fills the fields a case leaves empty, so a case may set File,
// Category, or Reviewer to adversarial text of its own.
func withIdentity(f Finding) Finding {
	f.Severity, f.Line, f.EstMinutes = "HIGH", 42, 15
	if f.File == "" {
		f.File = "pkg/a.go"
	}
	if f.Category == "" {
		f.Category = "correctness"
	}
	if f.Reviewer == "" {
		f.Reviewer = "bruce"
	}
	return f
}

// TestV2RoundTrip_AdversarialInputs writes each case with the v2 writer and
// reads it back through ParseSource, the on-disk read path, on both encodings:
// the TOON table and the {"axi_format":"json",...} envelope (forced by one
// extra row TOON cannot carry). AC 06-03 Scenario 3 (a reconciled round trip)
// has no target: there is no reconciled v2 shape, and ParseReconciled rejects
// v2 (TestParseReconciled_RejectsV2).
func TestV2RoundTrip_AdversarialInputs(t *testing.T) {
	for _, c := range adversarialCases() {
		in := withIdentity(c.f)
		t.Run(c.name+"/toon", func(t *testing.T) {
			var b strings.Builder
			require.NoError(t, WriteSourceV2(&b, []Finding{in}))
			require.False(t, strings.HasPrefix(v2Body(t, b.String()), envelopePrefix), "case must take the TOON path")

			res, err := ParseSource([]byte(b.String()))
			require.NoError(t, err)
			assert.Equal(t, []Finding{in}, res.Findings)
			assert.Empty(t, res.Skipped)
		})
		t.Run(c.name+"/envelope", func(t *testing.T) {
			var b strings.Builder
			require.NoError(t, encodeV2(&b, lossyPayload{Findings: []lossyRow{
				toLossyRow(in, nil),
				toLossyRow(Finding{Severity: "LOW", File: "b.go", Line: 1, Reviewer: "bruce"}, textish{"t"}),
			}}))
			require.True(t, strings.HasPrefix(v2Body(t, b.String()), envelopePrefix), "case must take the envelope path")

			res, err := ParseSource([]byte(b.String()))
			require.NoError(t, err)
			assert.Equal(t, []Finding{in, {Severity: "LOW", File: "b.go", Line: 1, Reviewer: "bruce"}}, res.Findings)
		})
	}
}

// TestV2RoundTrip_SanitizedByteIsStripped pins the one rewrite on the v2
// path (AC 06-03 Edge Case 4): go-axi strips an ESC byte, and only that byte.
func TestV2RoundTrip_SanitizedByteIsStripped(t *testing.T) {
	in := withIdentity(Finding{Evidence: "a | b\x1b[31mred\x1b[0m"})
	var b strings.Builder
	require.NoError(t, WriteSourceV2(&b, []Finding{in}))
	res, err := ParseSource([]byte(b.String()))
	require.NoError(t, err)
	want := in
	want.Evidence = "a | b[31mred[0m"
	assert.Equal(t, []Finding{want}, res.Findings)
}

// TestParseModelOutput_AdversarialJSON feeds the same cases as a reviewer
// model's fenced JSON array. Model output never carries the reviewer (TD-016),
// so Reviewer is expected empty.
func TestParseModelOutput_AdversarialJSON(t *testing.T) {
	for _, c := range adversarialCases() {
		t.Run(c.name, func(t *testing.T) {
			in := withIdentity(c.f)
			obj, err := json.Marshal([]map[string]any{{
				"severity": in.Severity, "file_line": in.File + ":42", "problem": in.Problem, "fix": in.Fix,
				"category": in.Category, "est_minutes": in.EstMinutes, "evidence": in.Evidence,
			}})
			require.NoError(t, err)

			got := ParseModelOutput([]byte("Review done.\n\n```json\n" + string(obj) + "\n```\n"))
			want := in
			want.Reviewer = ""
			assert.Equal(t, []Finding{want}, got)
		})
	}
}

func toLossyRow(f Finding, extra any) lossyRow {
	return lossyRow{
		Severity: f.Severity, FileLine: f.File + ":" + strconv.Itoa(f.Line), Problem: f.Problem, Fix: f.Fix,
		Category: f.Category, EstMinutes: f.EstMinutes, Evidence: f.Evidence, Reviewer: f.Reviewer, Extra: extra,
	}
}
