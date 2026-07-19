// Package report provides tests for the report rendering layer.
package report

import (
	"encoding/json"
	"flag"
	reclib "github.com/samestrin/atcr/reconcile"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// update regenerates the testdata/ golden files from sample() instead of
// comparing against them. Run `go test ./internal/report -update` after an
// intentional renderer change, then review the diff.
var update = flag.Bool("update", false, "regenerate golden files in testdata/")

func sample() []reconcile.JSONFinding {
	return []reconcile.JSONFinding{
		{Severity: "CRITICAL", File: "auth.go", Line: 42, Problem: "token never expires",
			Fix: "check expiry", Category: "security", EstMinutes: 15, Evidence: "expiresAt unread",
			Reviewers: []string{"greta", "host"}, Confidence: "HIGH"},
		{Severity: "LOW", File: "util.go", Line: 7, Problem: "unused var",
			Category: "style", Reviewers: []string{"otto"}, Confidence: "MEDIUM"},
	}
}

// A finding carrying a fix-generation warning (e.g. the Epic 7.1 invalid_syntax
// flag) must surface that warning in the markdown report so the user sees the fix
// was flagged and why.
func TestRenderMarkdown_ShowsFixWarning(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Fix: "func x() {",
			Confidence: "HIGH", Reviewers: []string{"rev"},
			FixWarning: "invalid_syntax: 2:1: expected '}'"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "Fix warning:", "the fix-warning label must surface in the markdown report")
	// The message text is HTML-escaped by the renderer; assert the unescaped portion.
	assert.Contains(t, out, "invalid_syntax: 2:1: expected",
		"the fix warning message must surface in the markdown report")
}

func TestValidFormat(t *testing.T) {
	assert.True(t, ValidFormat("md"))
	assert.True(t, ValidFormat("json"))
	assert.True(t, ValidFormat("checklist"))
	assert.True(t, ValidFormat("sarif"))
	assert.True(t, ValidFormat("axi"))
	assert.False(t, ValidFormat("xml"))
	// The format enum stays case-sensitive (AC 01-01 Edge Case 1): no
	// normalization is introduced for sarif that the other formats lack.
	assert.False(t, ValidFormat("SARIF"))
}

// goldenCases pins each renderer's full output to a checked-in golden file.
var goldenCases = []struct {
	name     string
	format   string
	golden   string
	findings []reconcile.JSONFinding // nil → use sample()
}{
	{"markdown", FormatMarkdown, "report.md", nil},
	{"json", FormatJSON, "findings.json", nil},
	{"checklist", FormatChecklist, "checklist.md", nil},
	{"sarif", FormatSarif, "report.sarif.json", sampleSarif()},
	{"axi", FormatAXI, "report.axi", nil},
}

// TestRender_GoldenFiles compares each format's full render output byte-for-byte
// against testdata/ (AC 01-06: "Golden file tests pass for each format"). The
// inline TestRender_* tests below still cover behavioral edge cases (truncation,
// injection, unicode, zero findings); this test locks the exact canonical output
// so any formatting drift is caught. Regenerate with `-update`.
func TestRender_GoldenFiles(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			findings := tc.findings
			if findings == nil {
				findings = sample()
			}
			var b strings.Builder
			require.NoError(t, Render(&b, findings, tc.format))
			got := b.String()
			path := filepath.Join("testdata", tc.golden)

			if *update {
				require.NoError(t, os.MkdirAll("testdata", 0o755))
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
				return
			}

			want, err := os.ReadFile(path)
			require.NoErrorf(t, err, "missing golden %s — run: go test ./internal/report -update", path)
			assert.Equalf(t, string(want), got, "render output drifted from golden %s; if intended, run -update", tc.golden)
		})
	}
}

// sampleWithFixWarning is a dedicated fixture whose finding carries a FixWarning so a
// golden file can lock the 7.1 fix-warning line (glyph, indentation, position) byte-
// for-byte. Kept separate from sample() so the clean-output goldens and the tests
// that assert no fix_warning key (TestRender_FixWarningJSONOmitemptyAndRoundTrip)
// stay valid.
func sampleWithFixWarning() []reconcile.JSONFinding {
	return []reconcile.JSONFinding{
		{Severity: "HIGH", File: "auth.go", Line: 42, Problem: "token never expires",
			Fix: "func checkExpiry() {", Category: "security", EstMinutes: 15,
			Evidence: "expiresAt unread", Reviewers: []string{"greta"}, Confidence: "HIGH",
			FixWarning: "invalid_syntax: 2:1: expected '}'"},
	}
}

// TestRender_FixWarningGolden locks the 7.1 fix-warning markdown line byte-for-byte so
// a reformat, glyph change, or repositioning of the warning line is caught (the
// markdown golden driven by sample() carries no FixWarning, so it cannot). Regenerate
// with `-update`.
func TestRender_FixWarningGolden(t *testing.T) {
	path := filepath.Join("testdata", "fix_warning.md")
	var b strings.Builder
	require.NoError(t, Render(&b, sampleWithFixWarning(), FormatMarkdown))
	got := b.String()

	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}

	want, err := os.ReadFile(path)
	require.NoErrorf(t, err, "missing golden %s — run: go test ./internal/report -update", path)
	assert.Equalf(t, string(want), got, "fix-warning render drifted from golden %s; if intended, run -update", path)
}

// The Epic 7.0/7.1 back-compat invariant: fix_warning is omitempty, so a finding
// without a warning serializes byte-identically to a pre-7.0 finding (no fix_warning
// key), and a set warning round-trips through Unmarshal. Mirrors the verification-
// field omitted-when-absent precedent (TestRender_VerificationBlockAddsSkepticSection).
func TestRender_FixWarningJSONOmitemptyAndRoundTrip(t *testing.T) {
	t.Run("omitted-when-empty", func(t *testing.T) {
		var b strings.Builder
		require.NoError(t, Render(&b, sample(), FormatJSON))
		assert.NotContains(t, b.String(), "fix_warning", "an empty FixWarning must not emit a fix_warning key")
	})

	t.Run("round-trips-when-set", func(t *testing.T) {
		var b strings.Builder
		require.NoError(t, Render(&b, sampleWithFixWarning(), FormatJSON))
		assert.Contains(t, b.String(), "fix_warning", "a set FixWarning must emit the fix_warning key")
		var got []reconcile.JSONFinding
		require.NoError(t, json.Unmarshal([]byte(b.String()), &got))
		require.Len(t, got, 1)
		assert.Equal(t, "invalid_syntax: 2:1: expected '}'", got[0].FixWarning, "FixWarning must round-trip through Unmarshal")
	})
}

func TestRender_JSONRoundTrips(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, sample(), FormatJSON))
	var got []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal([]byte(b.String()), &got))
	require.Len(t, got, 2)
	assert.Equal(t, "auth.go", got[0].File)
	assert.Equal(t, []string{"greta", "host"}, got[0].Reviewers)
}

func TestRender_MarkdownGroupsBySeverity(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, sample(), FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "# atcr Review Report")
	assert.Contains(t, out, "Total findings: 2")
	assert.Contains(t, out, "### CRITICAL")
	assert.Contains(t, out, "### LOW")
	assert.Contains(t, out, "`auth.go:42`")
	assert.Contains(t, out, "reviewers: greta, host")
}

func TestRender_ChecklistItemsNoNumbering(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, sample(), FormatChecklist))
	out := b.String()
	assert.Contains(t, out, "- [ ] **CRITICAL** `auth.go:42` — token never expires (confidence: HIGH)")
	assert.Contains(t, out, "- [ ] **LOW** `util.go:7`")
	assert.NotContains(t, out, "1.", "checklist has no global numbering")
}

func TestRender_ZeroFindingsMessage(t *testing.T) {
	for _, format := range []string{FormatMarkdown, FormatChecklist} {
		var b strings.Builder
		require.NoError(t, Render(&b, nil, format))
		assert.Contains(t, b.String(), "No findings.", "format %s", format)
	}
	// json zero findings → empty array, not null.
	var jb strings.Builder
	require.NoError(t, Render(&jb, nil, FormatJSON))
	assert.Equal(t, "[]", strings.TrimSpace(jb.String()))
}

func TestRender_LongTextTruncatedInMdNotJSON(t *testing.T) {
	long := strings.Repeat("x", 600)
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: long, Confidence: "MEDIUM"},
	}
	var md strings.Builder
	require.NoError(t, Render(&md, findings, FormatMarkdown))
	assert.Contains(t, md.String(), "...", "md truncates long text")
	assert.NotContains(t, md.String(), long, "full 600-char text not in md")

	var js strings.Builder
	require.NoError(t, Render(&js, findings, FormatJSON))
	assert.Contains(t, js.String(), long, "json is never truncated")
}

func TestRender_UnicodeFilePathByteIdentical(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "src/café/main.go", Line: 3, Problem: "p", Confidence: "MEDIUM"},
	}
	for _, format := range []string{FormatMarkdown, FormatChecklist, FormatJSON} {
		var b strings.Builder
		require.NoError(t, Render(&b, findings, format))
		assert.Contains(t, b.String(), "src/café/main.go", "unicode path preserved in %s", format)
	}
}

func TestRender_HTMLInjectionEscapedInMarkdown(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Confidence: "MEDIUM",
			Problem: "<script>alert(1)</script>\n## Forged"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.NotContains(t, out, "<script>")
	assert.NotContains(t, out, "\n## Forged", "newline-injected heading flattened")
}

// FixWarning is parser-derived text that can echo attacker-controlled fix content, so
// the markdown renderer must escape and truncate it via escTrunc (render.go:167) like
// every other free-text field. Mirrors TestRender_HTMLInjectionEscapedInMarkdown but
// for FixWarning, and additionally exercises the >maxTextLen truncation path.
func TestRender_FixWarningInjectionAndTruncationEscapedInMarkdown(t *testing.T) {
	long := strings.Repeat("x", 600) // exceeds maxTextLen (500) to force truncation
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "MEDIUM",
			FixWarning: "<script>alert(1)</script>\n## Forged `code` " + long},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "Fix warning:", "the fix-warning line must render")
	assert.NotContains(t, out, "<script>", "a script tag in FixWarning must be HTML-escaped")
	assert.NotContains(t, out, "\n## Forged", "a newline-injected heading in FixWarning must be flattened")
	assert.Contains(t, out, "...", "an over-maxTextLen FixWarning must be truncated with an ellipsis")
}

func TestCodeSpan_BacktickPathCannotBreakOut(t *testing.T) {
	// A path with a backtick must not close the code span and inject HTML.
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a`<script>`b.go", Line: 1, Problem: "p", Confidence: "MEDIUM"},
	}
	for _, format := range []string{FormatMarkdown, FormatChecklist} {
		var b strings.Builder
		require.NoError(t, Render(&b, findings, format))
		out := b.String()
		assert.NotContains(t, out, "<script>", "backtick path must not inject HTML in %s", format)
		assert.NotContains(t, out, "`a`<script>`b.go", "no raw backtick breakout in %s", format)
	}
}

func TestTruncate_BoundaryAndSmallN(t *testing.T) {
	exactly := strings.Repeat("y", maxTextLen)
	assert.Equal(t, exactly, truncate(exactly, maxTextLen), "exactly maxTextLen runes is not truncated")
	over := strings.Repeat("y", maxTextLen+1)
	assert.True(t, strings.HasSuffix(truncate(over, maxTextLen), "..."), "one over is truncated")

	// n < 3 must not panic (guarded).
	assert.NotPanics(t, func() { _ = truncate("abcdef", 2) })
	assert.NotPanics(t, func() { _ = truncate("abcdef", 0) })
}

func TestTruncate_MultibyteNotSplit(t *testing.T) {
	// 600 multibyte runes truncated → result is valid UTF-8 (no split rune).
	s := strings.Repeat("é", 600)
	out := truncate(s, maxTextLen)
	assert.True(t, len([]rune(out)) <= maxTextLen)
	assert.Equal(t, out, string([]rune(out)), "result is valid UTF-8")
}

func TestRender_UnknownFormatErrors(t *testing.T) {
	var b strings.Builder
	err := Render(&b, sample(), "xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown format")
}

// TestRender_MarkdownSortsSeverities — findings arriving in non-canonical
// severity order are rendered under one header per severity, in canonical
// descending order, rather than producing duplicate headers as the arrival
// order changes.
func TestRender_MarkdownSortsSeverties(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "LOW", File: "a.go", Line: 1, Problem: "p1", Confidence: "MEDIUM"},
		{Severity: "CRITICAL", File: "b.go", Line: 2, Problem: "p2", Confidence: "HIGH"},
		{Severity: "HIGH", File: "c.go", Line: 3, Problem: "p3", Confidence: "MEDIUM"},
		{Severity: "LOW", File: "d.go", Line: 4, Problem: "p4", Confidence: "LOW"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	critIdx := strings.Index(out, "### CRITICAL")
	highIdx := strings.Index(out, "### HIGH")
	lowIdx := strings.Index(out, "### LOW")
	require.Greater(t, critIdx, 0)
	require.Greater(t, highIdx, critIdx)
	require.Greater(t, lowIdx, highIdx)
	assert.Equal(t, 1, strings.Count(out, "### CRITICAL"), "one CRITICAL header")
	assert.Equal(t, 1, strings.Count(out, "### HIGH"), "one HIGH header")
	assert.Equal(t, 1, strings.Count(out, "### LOW"), "one LOW header")
}

// TestRender_UnknownSeverityReconcilesGrid — findings with non-canonical
// severities are still counted in the summary grid so the grid's per-row sum
// matches "Total findings".
func TestRender_UnknownSeverityReconcilesGrid(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p1", Confidence: "MEDIUM"},
		{Severity: "weird", File: "b.go", Line: 2, Problem: "p2", Confidence: "LOW"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "Total findings: 2")
	assert.Contains(t, out, "| OTHER |", "unknown severity gets a grid row so the grid sums to the total")
	assert.Contains(t, out, "### weird", "unknown severity still renders its own body header")
}

// TestRender_MixedCaseConfidenceNormalized — mixed-case and unknown confidence
// values are normalized before tallying: "Verified" counts as VERIFIED, "High"
// as HIGH, and unrecognized values land in an explicit OTHER confidence bucket
// instead of being silently folded into LOW.
func TestRender_MixedCaseConfidenceNormalized(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p1", Confidence: "Verified"},
		{Severity: "HIGH", File: "b.go", Line: 2, Problem: "p2", Confidence: "High"},
		{Severity: "HIGH", File: "c.go", Line: 3, Problem: "p3", Confidence: "unknown"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "VERIFIED conf", "VERIFIED column shown for mixed-case Verified")
	assert.Contains(t, out, "| HIGH | 1 | 1 | 0 | 0 | 1 |", "Verified→VERIFIED, High→HIGH, unknown→OTHER")
	assert.Contains(t, out, "OTHER conf", "unknown confidence gets an OTHER column")
}

// --- Epic 3.0 Phase 5: the verification block is now rendered (it was inert/
// reserved in Epic 1.1). A NIL block still renders byte-identically to v1 (the
// backward-compat guarantee, AC 06-02); a NON-NIL block now adds skeptic info. ---

// TestRender_VerificationBlockAddsSkepticSection supersedes the Epic 1.1
// "identical with or without verification" test: now a non-nil verification block
// changes the markdown render (Skeptic section / VERIFIED tier), while a nil block
// leaves output unchanged and JSON omits the verification key.
func TestRender_VerificationBlockAddsSkepticSection(t *testing.T) {
	without := sample()
	with := sample()
	with[0].Confidence = "VERIFIED"
	with[0].Verification = &reclib.Verification{Verdict: "confirmed", Skeptic: "otto", Notes: "reproduced"}

	t.Run("markdown-differs-and-shows-skeptic", func(t *testing.T) {
		var a, b strings.Builder
		require.NoError(t, Render(&a, without, FormatMarkdown))
		require.NoError(t, Render(&b, with, FormatMarkdown))
		assert.NotEqual(t, a.String(), b.String(), "a non-nil verification block now changes markdown output")
		assert.NotContains(t, a.String(), "Skeptic: otto")
		assert.Contains(t, b.String(), "Skeptic: otto — confirmed")
	})

	t.Run("json-omitted-when-absent", func(t *testing.T) {
		var a strings.Builder
		require.NoError(t, Render(&a, without, FormatJSON))
		assert.NotContains(t, a.String(), "verification")
	})
}

// TestSeverityRankOf_MatchesCanonical — the report view and the reconcile radar
// must agree on severity ordering. After unifying on reclib.SeverityRank (the
// single source of truth), a finding ranks identically whether it is sorted by
// BuildDisagreements or grouped by Render.
func TestSeverityRankOf_MatchesCanonical(t *testing.T) {
	for sev, rank := range reclib.SeverityRank {
		assert.Equal(t, rank, severityRankOf(sev), "severity %s must rank identically in report and reconcile", sev)
	}
	assert.Equal(t, 0, severityRankOf("unknown"), "unknown severity must rank 0 in report view")
}

// TestSeverityRankOf_NormalizesCasing proves the report view ranks a mixed-case
// or whitespace-padded severity by its canonical rank rather than dropping it to
// 0, so the report sort agrees with reconcile and fanout on non-canonical input.
func TestSeverityRankOf_NormalizesCasing(t *testing.T) {
	assert.Equal(t, 4, severityRankOf(" critical "), "mixed-case/padded severity must rank by its canonical value")
}

// TestRender_MixedCaseSeverityGridBucketing — a finding with a lowercase severity
// string must land in its canonical summary-grid bucket, not in the OTHER row.
func TestRender_MixedCaseSeverityGridBucketing(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "high", File: "a.go", Line: 1, Problem: "p", Confidence: "MEDIUM"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.NotContains(t, out, "| OTHER |", "lowercase 'high' must bucket into HIGH, not OTHER")
}

// TestEscDelegatesToReconcileEsc pins that report.esc is not a separate
// implementation: for a representative set of inputs it must agree exactly with
// reconcile.Esc, so the two packages cannot silently drift apart.
func TestEscDelegatesToReconcileEsc(t *testing.T) {
	cases := []string{
		"plain text",
		"line\none\ntwo",
		"<script>alert(1)</script>",
		"back`tick",
		"mixed\n<script>`code`</script>",
	}
	for _, c := range cases {
		assert.Equal(t, reconcile.Esc(c), esc(c), "input %q must match reconcile.Esc exactly", c)
	}
}

// --- AC 01-01: FormatAXI render dispatch (token-dense TOON payload) ---

// TestRenderAXI_ZeroFindings pins the empty-container form: a review with no
// reconciled findings emits a well-formed empty TOON array (findings[0]:) rather
// than an error or a human-oriented "No findings." sentence (AC 01-01 Edge Case 1).
func TestRenderAXI_ZeroFindings(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, nil, FormatAXI))
	assert.Equal(t, "findings[0]:", strings.TrimSpace(b.String()),
		"zero findings must render the TOON empty-array header, not a human sentence")
}

// TestRenderAXI_EscapesDelimiterColonNewline exercises AC 01-01 Edge Case 2: a
// field carrying the active delimiter (pipe), a colon, an embedded newline, and a
// comma must be quoted and escaped per TOON's must-quote rules so the original
// text is round-trippable and the one-row-per-line structure is never broken by a
// raw delimiter/newline (contrast with atcr-findings/v1's lossy |→/ munging).
func TestRenderAXI_EscapesDelimiterColonNewline(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Confidence: "MEDIUM",
			Problem: "a|b:c\nd", Fix: "x,y"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatAXI))
	out := b.String()
	// The whole problem value is quoted; the pipe/colon ride inside the quotes and
	// the newline is the \n TOON escape (a literal backslash-n, not a raw newline).
	assert.Contains(t, out, `"a|b:c\nd"`, "delimiter/colon/newline field must be quoted and newline-escaped")
	// One header line + exactly one data row: the embedded newline did not split a row.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Len(t, lines, 2, "an embedded newline must not add a physical row")
}

// TestRenderAXI_UnicodePreserved mirrors the markdown renderer's unicode-path
// guarantee (AC 01-01 Edge Case 3): multi-byte UTF-8 in a file path or finding
// text survives byte-for-byte with no mojibake or truncation.
func TestRenderAXI_UnicodePreserved(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "src/café/main.go", Line: 3, Problem: "naïve façade", Confidence: "MEDIUM"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatAXI))
	out := b.String()
	assert.Contains(t, out, "src/café/main.go", "unicode file path preserved")
	assert.Contains(t, out, "naïve façade", "unicode finding text preserved")
}

// TestRenderAXI_CapsOversizeCell pins the AXI payload byte-safety bound (TD from
// sprint 31.0): a single reviewer-controlled free-text field (LLM-generated,
// potentially adversarial) must not render as one unbounded physical line that the
// line-count pagination never trims. Each free-text cell is rune-capped to
// maxTextLen like the md/checklist views (escTrunc), so a multi-megabyte Problem
// cannot blow an agent consumer's context budget.
func TestRenderAXI_CapsOversizeCell(t *testing.T) {
	huge := strings.Repeat("A", maxTextLen*4)
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Confidence: "MEDIUM", Problem: huge},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatAXI))
	out := b.String()

	// The full oversize field must never reach stdout, and the single data row must
	// be bounded near maxTextLen (plus modest per-row structural overhead), not the
	// 4×maxTextLen the reviewer supplied.
	assert.NotContains(t, out, huge, "the full multi-megabyte field must not reach stdout")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2, "header + one data row")
	assert.LessOrEqual(t, len(lines[1]), maxTextLen+200,
		"oversize free-text cell must be rune-capped, not emitted whole")
}

// TestRenderAXI_NoANSINoMarkdown enforces the AC 01-01 story requirement and DoD:
// axi stdout carries zero \x1b[ ANSI escape sequences (even when a finding field
// contains a raw ANSI sequence — TOON has no \x escape, so it is stripped, not
// smuggled) and zero Markdown table (|---|) or heading (#) syntax.
func TestRenderAXI_NoANSINoMarkdown(t *testing.T) {
	findings := append(sample(),
		reconcile.JSONFinding{Severity: "HIGH", File: "x.go", Line: 9, Confidence: "LOW",
			Problem: "\x1b[31mred\x1b[0m text"})
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatAXI))
	out := b.String()
	assert.NotContains(t, out, "\x1b[", "no raw ANSI escape sequence may reach axi stdout")
	assert.NotContains(t, out, "|---|", "no markdown table separator")
	assert.NotContains(t, out, "# ", "no markdown heading")
	assert.Contains(t, out, "red", "the visible text survives; only the control bytes are stripped")
}

// TestRenderAXI_ReservedAndNumericQuoted pins the remaining TOON must-quote
// conditions (toon-format-reference.md): a field value that equals a reserved
// token (true/false/null), looks like a number, or starts with '-' must be quoted
// so a conforming TOON parser reads it back as the original string rather than a
// bool/null/number — the round-trip guarantee renderAXI promises. (Surfaced by
// the 1.2.A adversarial review; leading-'-' is common in diff-style Fix text.)
func TestRenderAXI_ReservedAndNumericQuoted(t *testing.T) {
	cases := []struct{ name, val, wantQuoted string }{
		{"number", "42", `"42"`},
		{"leading-zero-number", "05", `"05"`},
		{"float-and-dash", "-3.14", `"-3.14"`},
		{"bool-true", "true", `"true"`},
		{"bool-false", "false", `"false"`},
		{"null", "null", `"null"`},
		{"leading-dash-text", "- drop the call", `"- drop the call"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings := []reconcile.JSONFinding{
				{Severity: "HIGH", File: "a.go", Line: 1, Confidence: "MEDIUM", Category: c.val, Fix: c.val},
			}
			var b strings.Builder
			require.NoError(t, Render(&b, findings, FormatAXI))
			out := b.String()
			assert.Contains(t, out, c.wantQuoted, "%s value must be quoted so it round-trips as a string", c.name)
			// The bare token must never appear as an unquoted column value (which a
			// TOON parser would misread as a non-string type).
			bare := string(axiDelim) + c.val + string(axiDelim)
			assert.NotContains(t, out, bare, "%s must not appear as a bare column value", c.name)
		})
	}
}

// TestRenderAXI_AllEscapeSequences pins the full TOON escape contract for the
// security-critical quoting path (AC 01-01 Security): backslash, quote, CR and
// tab are emitted as their two-character escapes, while control bytes with no
// valid TOON escape (a raw ANSI \x1b, the U+2028 line separator) are stripped —
// never smuggled through as raw bytes.
func TestRenderAXI_AllEscapeSequences(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Confidence: "MEDIUM",
			Problem: "back\\slash \"q\" \r\t\x1b[0m end\u2028sep"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatAXI))
	out := b.String()
	assert.Contains(t, out, `\\`, "backslash escaped")
	assert.Contains(t, out, `\"`, "double quote escaped")
	assert.Contains(t, out, `\r`, "carriage return escaped")
	assert.Contains(t, out, `\t`, "tab escaped")
	assert.NotContains(t, out, "\x1b", "raw ANSI escape byte must be stripped, not emitted")
	assert.NotContains(t, out, "\u2028", "U+2028 line separator must be stripped")
	assert.Contains(t, out, "endsep", "visible text around stripped control chars survives contiguously")
}

// --- AC 01-02: AXI schema reconciled with atcr-findings/v1 + TOON conventions ---

// axiHeaderFields returns the tabular-array header's declared field list (the
// tokens between `{` and `}` on the first output line), for the field-count
// invariant checks. Fixtures used with it must not embed the pipe delimiter in a
// value, so a naive split is exact.
func axiHeaderFields(t *testing.T, out string) []string {
	t.Helper()
	line := strings.SplitN(out, "\n", 2)[0]
	i := strings.Index(line, "{")
	j := strings.LastIndex(line, "}")
	require.Truef(t, i >= 0 && j > i, "axi header must carry a {field} list: %q", line)
	return strings.Split(line[i+1:j], string(axiDelim))
}

// TestRenderAXI_PipeHeaderAndV1FieldSet pins AC 01-02 Scenarios 1 & 2: the header
// declares the pipe delimiter (findings[N|]{...}:) and its field list mirrors the
// atcr-findings/v1 reconciled 9-column contract field-for-field, so the axi
// surface converges with the existing machine format instead of fragmenting it.
func TestRenderAXI_PipeHeaderAndV1FieldSet(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, sample(), FormatAXI))
	header := strings.SplitN(b.String(), "\n", 2)[0]
	assert.True(t, strings.HasPrefix(header, "findings[2|]{"), "header declares count and pipe delimiter: %q", header)
	assert.Contains(t, header,
		`severity|"file:line"|problem|fix|category|est_minutes|evidence|reviewers|confidence`,
		"header field list mirrors the atcr-findings/v1 9-column contract")
}

// TestRenderAXI_VerificationEvidenceRoundTrip is AC 01-02 Edge Cases 1-3: a
// finding carrying a Verification block and an EvidenceExec block must surface
// them as additive columns (no signal dropped vs --format json), and a value that
// collides with a reserved token ("true") must be quoted.
func TestRenderAXI_VerificationEvidenceRoundTrip(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "auth.go", Line: 7, Problem: "p", Fix: "f",
			Category: "true", EstMinutes: 5, Evidence: "e",
			Reviewers: []string{"greta"}, Confidence: "VERIFIED",
			Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "otto", Notes: "reproduced locally"},
			EvidenceExec: &reconcile.EvidenceExec{Command: "go test ./x", ExitCode: 1, OutputExcerpt: "FAIL"}},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatAXI))
	out := b.String()
	header := axiHeaderFields(t, out)
	for _, want := range []string{
		"verification.verdict", "verification.skeptic", "verification.notes",
		"evidence_exec.command", "evidence_exec.exit_code", "evidence_exec.output_excerpt",
	} {
		assert.Contains(t, header, want, "additive column for the verification/evidence block")
	}
	for _, want := range []string{"confirmed", "otto", "reproduced locally", "go test ./x", "FAIL"} {
		assert.Contains(t, out, want, "verification/evidence value carried into the row")
	}
	assert.Contains(t, out, `"true"`, "a reserved-token-looking Category value must be quoted")
}

// TestRenderAXI_DisagreementAndChallengeSurvived closes the two lossy-subset gaps
// the 1.5.A adversarial review found: the severity `disagreement` annotation and
// `verification.challenge_survived` are both populated JSON signals that must
// survive into the axi payload for it to be a true superset of the JSON form (not
// just the 9-column text stream). A disagreement column appears only when a
// finding carries one; challenge_survived rides the verification block.
func TestRenderAXI_DisagreementAndChallengeSurvived(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "MEDIUM", File: "a.go", Line: 1, Problem: "p", Confidence: "MEDIUM",
			Disagreement: "LOW vs MEDIUM",
			Verification: &reclib.Verification{Verdict: "confirmed", Skeptic: "judge", ChallengeSurvived: true}},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatAXI))
	out := b.String()
	header := axiHeaderFields(t, out)
	assert.Contains(t, header, "disagreement", "a severity disagreement must surface as an additive column")
	assert.Contains(t, header, "verification.challenge_survived", "the judge-upheld signal must not be dropped")
	assert.Contains(t, out, "LOW vs MEDIUM", "the disagreement value must be carried into the row")
	// challenge_survived is a bare TOON boolean.
	assert.Contains(t, out, string(axiDelim)+"true\n", "challenge_survived=true emitted as a bare boolean at row end")
	// A no-disagreement payload must NOT declare the column (omitempty discipline).
	var b2 strings.Builder
	require.NoError(t, Render(&b2, sample(), FormatAXI))
	assert.NotContains(t, axiHeaderFields(t, b2.String()), "disagreement",
		"a payload with no disagreement must not declare the column")
}

// TestRenderAXI_FieldCountInvariant is AC 01-02 Error Scenario 1: every emitted
// data row must carry exactly as many columns as the header declares — including a
// mixed payload where only some findings carry a verification/evidence block, so
// the absent-block padding is proven correct rather than silently short-rowed.
func TestRenderAXI_FieldCountInvariant(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p1", Confidence: "MEDIUM"},
		{Severity: "LOW", File: "b.go", Line: 2, Problem: "p2", Confidence: "LOW",
			Verification: &reclib.Verification{Verdict: "refuted", Skeptic: "otto"},
			EvidenceExec: &reconcile.EvidenceExec{Command: "cmd", ExitCode: 2, OutputExcerpt: "out"}},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatAXI))
	out := b.String()
	want := len(axiHeaderFields(t, out))
	rows := strings.Split(strings.TrimRight(out, "\n"), "\n")[1:] // drop header line
	require.Len(t, rows, 2, "one row per finding")
	for i, r := range rows {
		got := len(strings.Split(strings.TrimSpace(r), string(axiDelim)))
		assert.Equalf(t, want, got, "row %d column count must equal header field count", i)
	}
}
