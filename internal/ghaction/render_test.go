package ghaction

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
)

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
}

func TestBuildCheckOutput(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "internal/auth/token.go", Line: 42,
			Problem: "JWT signature not verified", Confidence: "HIGH"},
		{Severity: "MEDIUM", File: "internal/store/cache.go", Line: 88,
			Problem: "Unbounded map | grows", Confidence: "MEDIUM"},
	}

	t.Run("with threshold", func(t *testing.T) {
		out := BuildCheckOutput(findings, "HIGH")
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
		out := BuildCheckOutput(refuted, "HIGH")
		assert.Contains(t, strings.ToLower(out.Text), "gate passed")
		assert.Contains(t, out.Text, "(refuted)")
	})

	t.Run("empty findings", func(t *testing.T) {
		out := BuildCheckOutput(nil, "HIGH")
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
		out := BuildCheckOutput(many, "HIGH")
		assert.LessOrEqual(t, len(out.Text), maxCheckTextBytes)
		assert.Contains(t, out.Text, "truncated")
	})
}
