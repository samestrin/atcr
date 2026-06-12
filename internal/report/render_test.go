package report

import (
	"encoding/json"
	"flag"
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
