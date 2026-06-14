package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadVerificationFixture reads testdata/findings-with-verification.json — four
// findings covering every verdict path: confirmed→VERIFIED, refuted→LOW,
// unverifiable→unchanged, and a v1 finding with no verification block.
func loadVerificationFixture(t *testing.T) []reconcile.JSONFinding {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "findings-with-verification.json"))
	require.NoError(t, err)
	var findings []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal(data, &findings))
	require.Len(t, findings, 4)
	return findings
}

// TestRenderWithVerification pins the full v2 markdown render against the
// testdata/report-v2.md golden file (AC 06-01 Scenario 5). Regenerate with
// `go test ./internal/report -update`.
func TestRenderWithVerification(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, loadVerificationFixture(t), FormatMarkdown))
	got := b.String()
	path := filepath.Join("testdata", "report-v2.md")

	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}

	want, err := os.ReadFile(path)
	require.NoErrorf(t, err, "missing golden %s — run: go test ./internal/report -update", path)
	assert.Equalf(t, string(want), got, "v2 render drifted from golden report-v2.md; if intended, run -update")
}

// TestRenderReport_SkepticSection — a confirmed finding renders a Skeptic section
// with the skeptic name, verdict, and reasoning (AC 06-01 Scenario 1).
func TestRenderReport_SkepticSection(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, loadVerificationFixture(t), FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "Skeptic: otto — confirmed")
	assert.Contains(t, out, "expiresAt is parsed but never compared", "skeptic reasoning rendered")
}

// TestRenderReport_VerifiedTier — VERIFIED is rendered distinctly from v1 tiers,
// both on the finding line and as a column in the summary grid (AC 06-01 Scenario 3).
func TestRenderReport_VerifiedTier(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, loadVerificationFixture(t), FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "VERIFIED conf", "summary grid has a VERIFIED column when verification ran")
	assert.Contains(t, out, "confidence VERIFIED", "VERIFIED finding shows the VERIFIED tier label")
}

// TestRenderReport_RefutedSection — refuted findings appear ONLY in a collapsed
// <details> section at the bottom, not in the main Findings list (AC 06-01
// Scenario 2 + Edge Case 2).
func TestRenderReport_RefutedSection(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, loadVerificationFixture(t), FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "<details>")
	assert.Contains(t, out, "<summary>Refuted Findings (1)</summary>")
	assert.Contains(t, out, "</details>")
	assert.Contains(t, out, "the input is bound, not concatenated", "refuted skeptic reasoning shown in the section")

	// The refuted finding must not appear in the main Findings list — it lives
	// only in the collapsed section. Split on the Refuted marker and assert the
	// refuted file path is absent from the body above it.
	idx := strings.Index(out, "## Refuted Findings")
	require.Greater(t, idx, 0, "Refuted Findings section present")
	body := out[:idx]
	assert.NotContains(t, body, "db/query.go:13", "refuted finding excluded from the main Findings list")
}

// TestRenderReport_UnverifiableAnnotation — an unverifiable finding keeps its v1
// confidence (MEDIUM) and gains an annotation that the skeptic could not verify
// it (AC 06-01 Scenario 4).
func TestRenderReport_UnverifiableAnnotation(t *testing.T) {
	var b strings.Builder
	require.NoError(t, Render(&b, loadVerificationFixture(t), FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "confidence MEDIUM", "unverifiable finding retains its MEDIUM tier")
	assert.Contains(t, out, "Skeptic: otto — unverifiable", "skeptic verdict shown")
	assert.Contains(t, out, "could not verify", "annotation that the skeptic could not verify")
}

// TestRenderReport_NoRefutedSectionOmitted — when no finding is refuted the
// collapsed section is omitted entirely, no empty <details> block (AC 06-01 Edge
// Case 1).
func TestRenderReport_NoRefutedSectionOmitted(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "CRITICAL", File: "a.go", Line: 1, Problem: "p", Confidence: "VERIFIED",
			Verification: &reconcile.Verification{Verdict: "confirmed", Skeptic: "otto", Notes: "ok"}},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.NotContains(t, out, "Refuted Findings")
	assert.NotContains(t, out, "<details>")
}

// TestRenderReport_EmptyNotes — a verified finding with empty Notes renders the
// Skeptic line without a Reasoning sub-line (AC 06-01 Edge Case 3).
func TestRenderReport_EmptyNotes(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "VERIFIED",
			Verification: &reconcile.Verification{Verdict: "confirmed", Skeptic: "otto", Notes: ""}},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "Skeptic: otto — confirmed")
	assert.NotContains(t, out, "Reasoning:", "no reasoning line when notes are empty")
}

// TestRenderReport_MixedVerifiedAndUnverified — a finding with a verification
// block shows the Skeptic section; a finding without one renders as plain v1 (no
// skeptic info) (AC 06-01 Edge Case 2 / AC 06-02 Scenario 3).
func TestRenderReport_MixedVerifiedAndUnverified(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "verified.go", Line: 1, Problem: "p1", Confidence: "VERIFIED",
			Verification: &reconcile.Verification{Verdict: "confirmed", Skeptic: "otto", Notes: "confirmed it"}},
		{Severity: "HIGH", File: "plain.go", Line: 2, Problem: "p2", Confidence: "MEDIUM"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "`verified.go:1`")
	assert.Contains(t, out, "Skeptic: otto — confirmed")
	// The plain finding must render without any skeptic annotation.
	plainIdx := strings.Index(out, "`plain.go:2`")
	require.Greater(t, plainIdx, 0)
	assert.NotContains(t, out[plainIdx:], "Skeptic:", "v1 finding renders without a skeptic section")
}

// TestRenderReport_SkepticTextEscaped — skeptic name and notes are free text and
// must be HTML-escaped + newline-flattened so they cannot inject markup or escape
// the collapsed Refuted section (AC 06-01 Security Considerations).
func TestRenderReport_SkepticTextEscaped(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "LOW",
			Verification: &reconcile.Verification{
				Verdict: "refuted", Skeptic: "otto",
				Notes: "<script>alert(1)</script>\n</details>\n## Forged Heading"}},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.NotContains(t, out, "<script>", "skeptic notes HTML-escaped")
	assert.NotContains(t, out, "\n## Forged Heading", "newline-injected heading flattened")
	// The escaped </details> must not prematurely close the collapsed section: the
	// real closing tag is the LAST </details> in the output.
	assert.Equal(t, strings.LastIndex(out, "</details>"), strings.Index(out, "</details>"),
		"exactly one real </details> tag — injected one was escaped")
}

// TestRenderReport_AllRefuted — when every finding is refuted the main Findings
// list is empty (a note points to the collapsed section) and all findings appear
// in the Refuted section. The body never goes silently blank.
func TestRenderReport_AllRefuted(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p1", Confidence: "LOW",
			Verification: &reconcile.Verification{Verdict: "refuted", Skeptic: "otto", Notes: "n1"}},
		{Severity: "LOW", File: "b.go", Line: 2, Problem: "p2", Confidence: "LOW",
			Verification: &reconcile.Verification{Verdict: "refuted", Skeptic: "greta", Notes: "n2"}},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "All findings were refuted")
	assert.Contains(t, out, "<summary>Refuted Findings (2)</summary>")
}

// TestRenderReport_VerifiedConfidenceWithoutBlock — a finding carrying VERIFIED
// confidence but no verification block (a writer contract violation) must still
// appear in the summary grid: the VERIFIED column shows and counts it, so the
// grid sum reconciles with "Total findings" rather than the finding silently
// vanishing from every column (5.2.A MEDIUM fix).
func TestRenderReport_VerifiedConfidenceWithoutBlock(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "CRITICAL", File: "a.go", Line: 1, Problem: "p", Confidence: "VERIFIED"},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	out := b.String()
	assert.Contains(t, out, "VERIFIED conf", "VERIFIED column shown when a finding has VERIFIED confidence")
	assert.Contains(t, out, "| CRITICAL | 1 | 0 | 0 | 0 |", "the VERIFIED finding is counted, not lost")
}

// TestRenderReport_RefutedEmptySkeptic — a refuted finding with no skeptic name
// renders "(unknown)" in the Refuted section rather than an empty attribution.
func TestRenderReport_RefutedEmptySkeptic(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "LOW",
			Verification: &reconcile.Verification{Verdict: "refuted", Skeptic: "", Notes: "n"}},
	}
	var b strings.Builder
	require.NoError(t, Render(&b, findings, FormatMarkdown))
	assert.Contains(t, b.String(), "skeptic: (unknown)")
}

// TestRenderV1Findings — findings WITHOUT verification blocks render byte-identical
// to the pre-Epic-3.0 golden files (AC 06-02 Scenario 1). Reuses sample() (no
// verification) against the unchanged report.md / checklist.md goldens.
func TestRenderV1Findings(t *testing.T) {
	for _, tc := range []struct{ format, golden string }{
		{FormatMarkdown, "report.md"},
		{FormatChecklist, "checklist.md"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			var b strings.Builder
			require.NoError(t, Render(&b, sample(), tc.format))
			want, err := os.ReadFile(filepath.Join("testdata", tc.golden))
			require.NoError(t, err)
			assert.Equal(t, string(want), b.String(),
				"v1 findings (no verification) must render identically to the pre-Epic-3.0 golden")
		})
	}
}
