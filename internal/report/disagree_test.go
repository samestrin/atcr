package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderDisagreements_EmptyIsExplicit(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderDisagreements(&buf, reconcile.DisagreementsFile{
		SchemaVersion: reconcile.DisagreementsSchemaVersion,
	}))
	out := buf.String()
	assert.Contains(t, out, "Disagreement Radar")
	assert.Contains(t, out, "No disagreements")
}

func TestRenderDisagreements_RanksHighestTensionFirst(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "MEDIUM", File: "split.go", Line: 5, Problem: "low-vs-medium split",
			Reviewers: []string{"greta", "kai"}, Confidence: "HIGH", Disagreement: "LOW vs MEDIUM"},
		{Severity: "CRITICAL", File: "big.go", Line: 9, Problem: "critical-vs-low split",
			Reviewers: []string{"greta", "kai"}, Confidence: "HIGH", Disagreement: "LOW vs CRITICAL"},
	}
	df := reconcile.BuildDisagreements(findings, nil)

	var buf bytes.Buffer
	require.NoError(t, RenderDisagreements(&buf, df))
	out := buf.String()

	// Highest score (CRITICAL-vs-LOW, spread 3 x indep 2 = 6) renders before the
	// LOW-vs-MEDIUM split (spread 1 x indep 2 = 2).
	assert.Less(t, strings.Index(out, "big.go:9"), strings.Index(out, "split.go:5"))
	assert.Contains(t, out, "LOW vs CRITICAL")
	assert.Contains(t, out, "score 6")
}

func TestRenderDisagreements_GrayZoneShowsPositionsSideBySide(t *testing.T) {
	clusters := []reconcile.AmbiguousCluster{{
		ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55,
		Findings: []stream.Finding{
			{Severity: "HIGH", File: "g.go", Line: 7, Problem: "buffer overrun risk", Reviewer: "greta"},
			{Severity: "LOW", File: "g.go", Line: 8, Problem: "minor bounds note", Reviewer: "kai"},
		},
	}}
	df := reconcile.BuildDisagreements(nil, clusters)

	var buf bytes.Buffer
	require.NoError(t, RenderDisagreements(&buf, df))
	out := buf.String()
	assert.Contains(t, out, "greta")
	assert.Contains(t, out, "kai")
	assert.Contains(t, out, "buffer overrun risk")
	assert.Contains(t, out, "minor bounds note")
	assert.Contains(t, out, "0.55")
}

func TestRenderDisagreements_EscapesFreeText(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "x.go", Line: 1, Problem: "<script>alert(1)</script>",
			Reviewers: []string{"a"}, Confidence: "MEDIUM"},
	}
	df := reconcile.BuildDisagreements(findings, nil)
	var buf bytes.Buffer
	require.NoError(t, RenderDisagreements(&buf, df))
	assert.NotContains(t, buf.String(), "<script>", "free text must be HTML-escaped")
}
