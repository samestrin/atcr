package ghaction

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
)

func TestCell_MarkdownSanitization(t *testing.T) {
	// Pipes become / so they cannot break the table column grammar.
	assert.Equal(t, "`a / b`", cell("a | b"))
	// Newlines are collapsed to spaces.
	assert.Equal(t, "`line1 line2`", cell("line1\nline2"))
	// Markdown links: only the display text is kept.
	assert.Equal(t, "`click here`", cell("[click here](http://evil.example.com)"))
	assert.Equal(t, "`see docs`", cell("[see docs](https://internal/docs)"))
	// Emphasis markers are inert inside the code span.
	assert.Equal(t, "`*bold*`", cell("*bold*"))
	assert.Equal(t, "`_italic_`", cell("_italic_"))
	// Security: cell content is wrapped in a backtick code span so that
	// headings, HTML tags, and other markdown render as inert literal text.
	assert.Equal(t, "`# injected heading`", cell("# injected heading"))
	assert.Equal(t, "`<b>bold via HTML</b>`", cell("<b>bold via HTML</b>"))
	// Embedded backticks are replaced to prevent premature code span close.
	assert.Equal(t, "`let x = 'y'`", cell("let x = `y`"))
}

func TestFixAttribution(t *testing.T) {
	cases := []struct {
		name     string
		evidence string
		want     string
	}{
		{"present", "bruce: token, _ := jwt.Parse(raw); fix by opus", "opus"},
		{"present_simple", "Found by bruce; fix by greta", "greta"},
		{"absent", "bruce: c.entries[k] = v // never deleted", ""},
		{"empty_name", "Found by bruce; fix by ", ""},
		{"prose_mention", "reviewer suggested a fix by hand", ""},
		{"last_segment_wins", "fix by hand; Found by bruce; fix by opus", "opus"},
		{"empty_evidence", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, FixAttribution(tc.evidence))
		})
	}
}

func TestConclusion(t *testing.T) {
	high := reconcile.JSONFinding{Severity: "HIGH"}
	medium := reconcile.JSONFinding{Severity: "MEDIUM"}
	refutedHigh := reconcile.JSONFinding{
		Severity:     "HIGH",
		Verification: &reconcile.Verification{Verdict: reconcile.VerdictRefuted},
	}

	t.Run("no threshold is neutral", func(t *testing.T) {
		c, n := Conclusion([]reconcile.JSONFinding{high}, "")
		assert.Equal(t, "neutral", c)
		assert.Equal(t, 0, n)
	})
	t.Run("blocking finding fails", func(t *testing.T) {
		c, n := Conclusion([]reconcile.JSONFinding{high, medium}, "HIGH")
		assert.Equal(t, "failure", c)
		assert.Equal(t, 1, n)
	})
	t.Run("below threshold passes", func(t *testing.T) {
		c, n := Conclusion([]reconcile.JSONFinding{medium}, "HIGH")
		assert.Equal(t, "success", c)
		assert.Equal(t, 0, n)
	})
	t.Run("refuted finding never blocks", func(t *testing.T) {
		c, n := Conclusion([]reconcile.JSONFinding{refutedHigh}, "HIGH")
		assert.Equal(t, "success", c)
		assert.Equal(t, 0, n)
	})
	t.Run("refuted verdict with whitespace never blocks", func(t *testing.T) {
		f := reconcile.JSONFinding{
			Severity:     "HIGH",
			Verification: &reconcile.Verification{Verdict: " refuted "},
		}
		c, n := Conclusion([]reconcile.JSONFinding{f}, "HIGH")
		assert.Equal(t, "success", c)
		assert.Equal(t, 0, n)
	})
	t.Run("out-of-scope finding never blocks even at CRITICAL", func(t *testing.T) {
		f := reconcile.JSONFinding{
			Severity: "CRITICAL",
			Category: reconcile.CategoryOutOfScope,
		}
		c, n := Conclusion([]reconcile.JSONFinding{f}, "CRITICAL")
		assert.Equal(t, "success", c)
		assert.Equal(t, 0, n)
	})
}

// TestBuildCheckOutputReturnsConclusionAndFailCount pins Item 2 of epic 7.6:
// BuildCheckOutput surfaces the conclusion and failCount it already computes
// internally, so runGithub can consume them instead of calling Conclusion a
// second time. The returned values must match a direct Conclusion call for the
// same inputs, including the empty-findings early-return branch.
func TestBuildCheckOutputReturnsConclusionAndFailCount(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "HIGH"},
		{Severity: "MEDIUM", File: "b.go", Line: 2, Problem: "q", Confidence: "MEDIUM"},
	}
	t.Run("with findings matches Conclusion", func(t *testing.T) {
		_, conclusion, failCount := BuildCheckOutput(findings, "HIGH")
		wantC, wantN := Conclusion(findings, "HIGH")
		assert.Equal(t, wantC, conclusion)
		assert.Equal(t, wantN, failCount)
		assert.Equal(t, ConclusionFailure, conclusion)
		assert.Equal(t, 1, failCount)
	})
	t.Run("non-empty findings without threshold is neutral", func(t *testing.T) {
		_, conclusion, failCount := BuildCheckOutput(findings, "")
		assert.Equal(t, ConclusionNeutral, conclusion)
		assert.Equal(t, 0, failCount)
	})
	t.Run("empty findings with threshold reports success and zero", func(t *testing.T) {
		_, conclusion, failCount := BuildCheckOutput(nil, "HIGH")
		wantC, wantN := Conclusion(nil, "HIGH")
		assert.Equal(t, wantC, conclusion)
		assert.Equal(t, wantN, failCount)
		assert.Equal(t, ConclusionSuccess, conclusion)
		assert.Equal(t, 0, failCount)
	})
	t.Run("empty findings without threshold is neutral", func(t *testing.T) {
		_, conclusion, failCount := BuildCheckOutput(nil, "")
		assert.Equal(t, ConclusionNeutral, conclusion)
		assert.Equal(t, 0, failCount)
	})
}

func TestBuildCheckOutputSummaryDistinctFromTitle(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "HIGH"},
	}
	out, _, _ := BuildCheckOutput(findings, "HIGH")
	assert.NotEqual(t, out.Title, out.Summary)
	assert.Contains(t, strings.ToLower(out.Summary), "gate")
}

func TestBuildCheckOutputNormalizesSeverityCase(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "critical", File: "a.go", Line: 1, Problem: "p", Confidence: "HIGH"},
	}
	out, _, _ := BuildCheckOutput(findings, "HIGH")
	assert.Contains(t, out.Text, "CRITICAL")
	assert.NotContains(t, out.Text, "critical")
}

func TestBuildCheckOutputInvalidThresholdRendersRaw(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p", Confidence: "HIGH"},
	}
	out, _, _ := BuildCheckOutput(findings, "bogus")
	assert.Contains(t, out.Title, "bogus")
	assert.Contains(t, strings.ToLower(out.Text), "gate passed")
}

func TestBuildCheckOutput(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "internal/auth/token.go", Line: 42,
			Problem: "JWT signature not verified", Confidence: "HIGH"},
		{Severity: "MEDIUM", File: "internal/store/cache.go", Line: 88,
			Problem: "Unbounded map | grows", Confidence: "MEDIUM"},
	}

	t.Run("with threshold", func(t *testing.T) {
		out, _, _ := BuildCheckOutput(findings, "HIGH")
		assert.Contains(t, out.Title, "2")
		assert.Contains(t, out.Title, "HIGH")
		assert.Contains(t, out.Text, "internal/auth/token.go:42")
		assert.Contains(t, out.Text, "JWT signature not verified")
		// A literal pipe inside a cell must be neutralized so it can't break the table.
		assert.NotContains(t, out.Text, "Unbounded map | grows")
		assert.Contains(t, out.Text, "Unbounded map / grows")
	})

	t.Run("refuted findings are demoted in table", func(t *testing.T) {
		refuted := []reconcile.JSONFinding{
			{Severity: "HIGH", File: "internal/auth/token.go", Line: 42,
				Problem: "JWT signature not verified", Confidence: "HIGH",
				Verification: &reconcile.Verification{Verdict: reconcile.VerdictRefuted, Skeptic: "skeptic-a"}},
		}
		out, _, _ := BuildCheckOutput(refuted, "HIGH")
		assert.Contains(t, strings.ToLower(out.Text), "gate passed")
		assert.Contains(t, out.Text, "(refuted)")
	})

	t.Run("empty findings", func(t *testing.T) {
		out, _, _ := BuildCheckOutput(nil, "HIGH")
		assert.Contains(t, strings.ToLower(out.Title), "no findings")
	})

	t.Run("oversized text is truncated below the GitHub limit", func(t *testing.T) {
		many := make([]reconcile.JSONFinding, 4000)
		for i := range many {
			many[i] = reconcile.JSONFinding{
				Severity: "LOW", File: "internal/some/very/long/path/to/a/file.go", Line: i,
				Problem: "a reasonably detailed problem statement that takes up space", Confidence: "LOW",
			}
		}
		out, _, _ := BuildCheckOutput(many, "HIGH")
		assert.LessOrEqual(t, len(out.Text), maxCheckTextBytes)
		assert.Contains(t, out.Text, "truncated")
	})

	t.Run("truncation count tracks rows actually shown", func(t *testing.T) {
		oversized := []reconcile.JSONFinding{
			{Severity: "LOW", File: "a.go", Line: 1,
				Problem: strings.Repeat("x", 65000), Confidence: "LOW"},
		}
		out, _, _ := BuildCheckOutput(oversized, "HIGH")
		assert.Contains(t, out.Text, "0 of 1 findings shown")
	})
}
