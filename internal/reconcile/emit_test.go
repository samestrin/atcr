package reconcile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recAt() Options { return Options{ReconciledAt: time.Unix(1700000000, 0).UTC()} }

func TestReconcile_TwoReviewersAgreeHighConfidence(t *testing.T) {
	sources := []Source{
		{Name: "pool", Findings: []stream.Finding{
			mf("HIGH", "auth.go", 42, "token never expires here", "fix", "security", 15, "e", "greta"),
		}},
		{Name: "host", Findings: []stream.Finding{
			mf("HIGH", "auth.go", 43, "token never expires here", "fix", "security", 15, "e", "host"),
		}},
	}
	res := Reconcile(sources, recAt())
	require.Len(t, res.Findings, 1, "co-located identical findings collapse")
	assert.Equal(t, ConfHigh, res.Findings[0].Confidence)
	assert.Equal(t, []string{"greta", "host"}, res.Findings[0].Reviewers)
	assert.Equal(t, 1, res.Summary.ClustersCollapsed)
	assert.Equal(t, map[string]int{"pool": 1, "host": 1}, res.Summary.PerSourceCounts)
}

func TestReconcile_SortedBySeverityThenLocation(t *testing.T) {
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("LOW", "z.go", 5, "p1", "f", "style", 1, "e", "a"),
		mf("CRITICAL", "a.go", 1, "p2", "f", "sec", 9, "e", "b"),
		mf("MEDIUM", "m.go", 3, "p3", "f", "test", 4, "e", "c"),
	}}}
	res := Reconcile(sources, recAt())
	require.Len(t, res.Findings, 3)
	assert.Equal(t, "CRITICAL", res.Findings[0].Severity)
	assert.Equal(t, "MEDIUM", res.Findings[1].Severity)
	assert.Equal(t, "LOW", res.Findings[2].Severity)
}

func TestReconcile_AmbiguousAlwaysWrittenEvenWhenEmpty(t *testing.T) {
	res := Reconcile(nil, recAt())
	assert.NotNil(t, res.Ambiguous)
	assert.Empty(t, res.Ambiguous)
	assert.Equal(t, 0, res.Summary.TotalFindings)
}

func TestEmit_WritesAllFiveArtifacts(t *testing.T) {
	dir := t.TempDir()
	sources := []Source{
		{Name: "pool", Findings: []stream.Finding{
			mf("CRITICAL", "auth.go", 42, "token never expires", "guard it", "security", 15, "saw it", "greta"),
		}},
		{Name: "host", Findings: []stream.Finding{
			mf("LOW", "auth.go", 42, "token never expires", "guard it", "security", 15, "also", "host"),
		}},
	}
	res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	require.NoError(t, Emit(dir, res))

	for _, name := range []string{FindingsTxt, FindingsJSON, ReportMD, SummaryJSON, AmbiguousJSON} {
		assert.FileExists(t, filepath.Join(dir, name))
	}

	// findings.txt parses back as 9-col reconciled with REVIEWERS + CONFIDENCE.
	tdata, _ := os.ReadFile(filepath.Join(dir, FindingsTxt))
	parsed, err := stream.ParseReconciled(tdata)
	require.NoError(t, err)
	require.Len(t, parsed.Findings, 1)
	assert.Equal(t, "CRITICAL", parsed.Findings[0].Severity, "max severity")
	assert.ElementsMatch(t, []string{"greta", "host"}, parsed.Findings[0].Reviewers)
	assert.Equal(t, "HIGH", parsed.Findings[0].Confidence)
	assert.Contains(t, parsed.Findings[0].Evidence, "disagreement: LOW vs CRITICAL",
		"disagreement folded into evidence for the flat contract")

	// findings.json carries the disagreement as a structured field.
	jdata, _ := os.ReadFile(filepath.Join(dir, FindingsJSON))
	var jf []JSONFinding
	require.NoError(t, json.Unmarshal(jdata, &jf))
	require.Len(t, jf, 1)
	assert.Equal(t, "LOW vs CRITICAL", jf[0].Disagreement)
	assert.Equal(t, []string{"greta", "host"}, jf[0].Reviewers)

	// summary.json has the required fields.
	sdata, _ := os.ReadFile(filepath.Join(dir, SummaryJSON))
	var sum Summary
	require.NoError(t, json.Unmarshal(sdata, &sum))
	assert.Equal(t, 1, sum.TotalFindings)
	assert.Equal(t, 1, sum.SeverityDisagreements)
	assert.Equal(t, "2023-11-14T22:13:20Z", sum.ReconciledAt)
	assert.ElementsMatch(t, []string{"pool", "host"}, sum.SourcesScanned)
}

func TestSummary_SkippedSourcesRecorded(t *testing.T) {
	// Files Discover skipped (read error / bad header) must surface in
	// summary.json as skipped_sources + skipped_source_count (TD-020) — v1 is
	// warn-and-continue, so the record is the loud signal, not a non-zero exit.
	sources := []Source{
		{Name: "ci", SkippedFiles: []string{"sources/ci/findings.txt"}},
		{Name: "host", Findings: []stream.Finding{
			mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e", "host"),
		}},
	}
	res := Reconcile(sources, recAt())
	assert.Equal(t, []string{"sources/ci/findings.txt"}, res.Summary.SkippedSources)
	assert.Equal(t, 1, res.Summary.SkippedSourceCount)

	// Zero-skip runs serialize as an empty array, not null.
	res2 := Reconcile(nil, recAt())
	assert.NotNil(t, res2.Summary.SkippedSources)
	assert.Empty(t, res2.Summary.SkippedSources)
	assert.Equal(t, 0, res2.Summary.SkippedSourceCount)
}

func TestEmit_DeterministicOutput(t *testing.T) {
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a.go", 1, "alpha", "f", "sec", 10, "e", "greta"),
		mf("MEDIUM", "b.go", 9, "beta", "f", "test", 5, "e", "kai"),
	}}}
	d1, d2 := t.TempDir(), t.TempDir()
	require.NoError(t, Emit(d1, Reconcile(sources, recAt())))
	require.NoError(t, Emit(d2, Reconcile(sources, recAt())))
	for _, name := range []string{FindingsTxt, FindingsJSON, ReportMD, SummaryJSON} {
		a, _ := os.ReadFile(filepath.Join(d1, name))
		b, _ := os.ReadFile(filepath.Join(d2, name))
		assert.Equal(t, string(a), string(b), "%s must be byte-identical across runs", name)
	}
}

func TestRenderMarkdown_EscapesInjectionAndZeroFindings(t *testing.T) {
	var empty strings.Builder
	require.NoError(t, RenderMarkdown(&empty, Reconcile(nil, recAt())))
	assert.Contains(t, empty.String(), "No findings.")

	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a.go", 1, "<script>alert(1)</script>", "f", "sec", 10, "e", "greta"),
	}}}
	var b strings.Builder
	require.NoError(t, RenderMarkdown(&b, Reconcile(sources, recAt())))
	assert.NotContains(t, b.String(), "<script>", "HTML must be escaped")
	assert.Contains(t, b.String(), "&lt;script&gt;")
}

func TestRenderMarkdown_BacktickFilePathRendersInert(t *testing.T) {
	// A model-controlled File containing a backtick would close the code span
	// and let trailing text render as live markdown — the same injection class
	// AC 01-06 fixed in the report view (report/render.go codeSpan). Such paths
	// must fall back to HTML-escaped plain text instead of a code span.
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a`<i>.go", 1, "p", "f", "sec", 10, "e", "greta"),
	}}}
	var b strings.Builder
	require.NoError(t, RenderMarkdown(&b, Reconcile(sources, recAt())))
	out := b.String()
	assert.NotContains(t, out, "`a`", "backtick in path must not open/close a code span")
	assert.NotContains(t, out, "<i>", "HTML in path must be escaped")
	assert.Contains(t, out, "- a`&lt;i&gt;.go:1 — ", "falls back to escaped plain text, no span")
}

func TestRenderMarkdown_FlattensNewlineInjection(t *testing.T) {
	// A finding whose problem contains newlines must not inject markdown structure.
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a.go", 1, "line one\n## Forged Heading\n- forged bullet", "f", "sec", 10, "e", "greta"),
	}}}
	var b strings.Builder
	require.NoError(t, RenderMarkdown(&b, Reconcile(sources, recAt())))
	out := b.String()
	assert.NotContains(t, out, "\n## Forged Heading", "newlines flattened — no injected heading")
	assert.Contains(t, out, "line one ## Forged Heading - forged bullet")
}
