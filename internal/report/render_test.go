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
	assert.False(t, ValidFormat("xml"))
}

// goldenCases pins each renderer's full output to a checked-in golden file.
var goldenCases = []struct {
	name   string
	format string
	golden string
}{
	{"markdown", FormatMarkdown, "report.md"},
	{"json", FormatJSON, "findings.json"},
	{"checklist", FormatChecklist, "checklist.md"},
}

// TestRender_GoldenFiles compares each format's full render output byte-for-byte
// against testdata/ (AC 01-06: "Golden file tests pass for each format"). The
// inline TestRender_* tests below still cover behavioral edge cases (truncation,
// injection, unicode, zero findings); this test locks the exact canonical output
// so any formatting drift is caught. Regenerate with `-update`.
func TestRender_GoldenFiles(t *testing.T) {
	for _, tc := range goldenCases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			require.NoError(t, Render(&b, sample(), tc.format))
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
