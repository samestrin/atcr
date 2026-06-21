package report

import (
	"bytes"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flagged returns one finding carrying a hallucinated-path warning.
func flagged() []reconcile.JSONFinding {
	return []reconcile.JSONFinding{{
		Severity: "HIGH", File: "internal/auth/validator.go", Line: 12,
		Problem: "token never expires", Confidence: "MEDIUM",
		PathValid: false, PathWarning: stream.PathNotFoundWarning,
	}}
}

// TestRender_Markdown_ShowsPathWarning: the md report surfaces the warning and
// preserves the finding (AC3, AC4).
func TestRender_Markdown_ShowsPathWarning(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, Render(&b, flagged(), FormatMarkdown))

	out := b.String()
	assert.Contains(t, out, "⚠️ File not found: internal/auth/validator.go")
	assert.Contains(t, out, "token never expires")
}

// TestRender_Checklist_ShowsPathWarning: the checklist format flags the path too.
func TestRender_Checklist_ShowsPathWarning(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, Render(&b, flagged(), FormatChecklist))

	assert.Contains(t, b.String(), "⚠️ File not found: internal/auth/validator.go")
}

// TestRender_Markdown_RefutedShowsPathWarning: a refuted finding renders in its
// own collapsed section; the path warning must still appear there.
func TestRender_Markdown_RefutedShowsPathWarning(t *testing.T) {
	f := flagged()
	f[0].Verification = &reconcile.Verification{Verdict: reconcile.VerdictRefuted, Skeptic: "kai"}

	var b bytes.Buffer
	require.NoError(t, Render(&b, f, FormatMarkdown))

	out := b.String()
	require.Contains(t, out, "Refuted Findings")
	assert.Contains(t, out, "⚠️ File not found: internal/auth/validator.go")
}

// flaggedWithSuggestion returns one flagged finding that also carries a
// candidate-index correction (Epic 5.4).
func flaggedWithSuggestion() []reconcile.JSONFinding {
	f := flagged()
	f[0].PathSuggestion = "internal/auth/validate.go"
	return f
}

// TestRender_Markdown_ShowsPathSuggestion: the md report renders a "(did you
// mean …)" clause next to the warning, preserving the original cited path (AC6).
func TestRender_Markdown_ShowsPathSuggestion(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, Render(&b, flaggedWithSuggestion(), FormatMarkdown))

	out := b.String()
	assert.Contains(t, out, "⚠️ File not found: internal/auth/validator.go")
	assert.Contains(t, out, "did you mean")
	assert.Contains(t, out, "internal/auth/validate.go")
}

// TestRender_Checklist_ShowsPathSuggestion: the checklist view also carries the
// suggestion.
func TestRender_Checklist_ShowsPathSuggestion(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, Render(&b, flaggedWithSuggestion(), FormatChecklist))

	out := b.String()
	assert.Contains(t, out, "did you mean")
	assert.Contains(t, out, "internal/auth/validate.go")
}

// TestRender_Markdown_RefutedShowsPathSuggestion: the refuted collapsed section
// also renders the suggestion.
func TestRender_Markdown_RefutedShowsPathSuggestion(t *testing.T) {
	f := flaggedWithSuggestion()
	f[0].Verification = &reconcile.Verification{Verdict: reconcile.VerdictRefuted, Skeptic: "kai"}

	var b bytes.Buffer
	require.NoError(t, Render(&b, f, FormatMarkdown))

	out := b.String()
	require.Contains(t, out, "Refuted Findings")
	assert.Contains(t, out, "did you mean")
}

// TestRender_Markdown_NoSuggestionClauseWhenAbsent: a flagged finding without a
// suggestion renders the bare warning — no dangling "(did you mean …)".
func TestRender_Markdown_NoSuggestionClauseWhenAbsent(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, Render(&b, flagged(), FormatMarkdown))

	assert.NotContains(t, b.String(), "did you mean")
}

// TestRender_Markdown_NoWarningWhenValid: a valid path adds no warning, so a
// clean report is unchanged.
func TestRender_Markdown_NoWarningWhenValid(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity: "HIGH", File: "a.go", Line: 1, Problem: "x", Confidence: "MEDIUM", PathValid: true,
	}}

	var b bytes.Buffer
	require.NoError(t, Render(&b, findings, FormatMarkdown))

	assert.NotContains(t, b.String(), "File not found")
}
