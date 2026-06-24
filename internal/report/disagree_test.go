package report

import (
	"bytes"
	"encoding/json"
	reclib "github.com/samestrin/atcr/reconcile"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderDisagreementsJSON_RoundTripAndTrailingNewline(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "x.go", Line: 1, Problem: "p",
			Reviewers: []string{"a"}, Confidence: "MEDIUM"},
	}
	df := reconcile.BuildDisagreements(findings, nil)

	var buf bytes.Buffer
	require.NoError(t, RenderDisagreementsJSON(&buf, df))
	out := buf.String()

	require.True(t, strings.HasSuffix(out, "\n"), "JSON output must end with a trailing newline")

	var roundTrip reconcile.DisagreementsFile
	require.NoError(t, json.Unmarshal([]byte(out), &roundTrip))
	assert.Equal(t, df.SchemaVersion, roundTrip.SchemaVersion)
	assert.Len(t, roundTrip.Items, len(df.Items))
}

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
	clusters := []reclib.AmbiguousCluster{{
		ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55,
		Findings: []reconcile.Finding{
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

func TestRenderMarkdownWithDisagreements_RadarAboveFindings(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "CRITICAL", File: "a.go", Line: 1, Problem: "boom",
			Reviewers: []string{"greta", "host"}, Confidence: "HIGH", Disagreement: "LOW vs CRITICAL"},
	}
	df := reconcile.BuildDisagreements(findings, nil)
	var buf bytes.Buffer
	require.NoError(t, RenderMarkdownWithDisagreements(&buf, findings, df))
	out := buf.String()
	require.Contains(t, out, "## Disagreements")
	require.Contains(t, out, "## Findings")
	assert.Less(t, strings.Index(out, "## Disagreements"), strings.Index(out, "## Findings"))
}

func TestRenderMarkdownWithDisagreements_EmptyMatchesPlainRender(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p",
			Reviewers: []string{"greta", "host"}, Confidence: "HIGH"},
	}
	df := reconcile.BuildDisagreements(findings, nil) // no tension → empty
	var withRadar, plain bytes.Buffer
	require.NoError(t, RenderMarkdownWithDisagreements(&withRadar, findings, df))
	require.NoError(t, Render(&plain, findings, FormatMarkdown))
	assert.Equal(t, plain.String(), withRadar.String(),
		"no disagreements → byte-identical to the plain markdown report")
}

func TestRenderDisagreements_ShowsVerificationSkepticSplit(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "v.go", Line: 3, Problem: "contested",
			Reviewers: []string{"greta"}, Confidence: "MEDIUM",
			Verification: &reclib.Verification{
				Verdict: reclib.VerdictUnverifiable, Skeptic: "skeptic-a, skeptic-b", Notes: "disagreed"}},
	}
	df := reconcile.BuildDisagreements(findings, nil)
	var buf bytes.Buffer
	require.NoError(t, RenderDisagreements(&buf, df))
	out := buf.String()
	assert.Contains(t, out, "Skeptics split: skeptic-a, skeptic-b")
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

func TestRenderDisagreements_EscapesBackticksInFreeText(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "x.go", Line: 1, Problem: "problem",
			Reviewers: []string{"reviewer`name"}, Confidence: "MEDIUM", Disagreement: "a`b"},
	}
	df := reconcile.BuildDisagreements(findings, nil)
	var buf bytes.Buffer
	require.NoError(t, RenderDisagreements(&buf, df))
	out := buf.String()
	// Backticks in reviewer-controlled free text must be escaped so they cannot
	// open an inline code span; file-path code spans still use literal backticks.
	assert.Contains(t, out, "&#96;", "backtick must be HTML-escaped")
	assert.NotContains(t, out, "reviewer`name", "literal backtick must not appear in free text")
	assert.NotContains(t, out, "a`b", "literal backtick must not appear in free text")
}

func TestRenderDisagreements_GrayZoneEscapesAndTruncates(t *testing.T) {
	longProblem := strings.Repeat("A", 600)
	clusters := []reclib.AmbiguousCluster{{
		ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55,
		Findings: []reconcile.Finding{
			{Severity: "HIGH", File: "g.go", Line: 7, Problem: "<script>alert(1)</script>buffer overrun risk", Reviewer: "gre`ta"},
			{Severity: "LOW", File: "g.go", Line: 8, Problem: "minor bounds note", Reviewer: "kai"},
		},
	}}
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "s.go", Line: 2, Problem: longProblem,
			Reviewers: []string{"a"}, Confidence: "MEDIUM", Disagreement: "a`b",
			Verification: &reclib.Verification{
				Verdict: reclib.VerdictUnverifiable, Skeptic: "skep`tic", Notes: "disagreed"}},
	}
	df := reconcile.BuildDisagreements(findings, clusters)

	var buf bytes.Buffer
	require.NoError(t, RenderDisagreements(&buf, df))
	out := buf.String()

	// Injection defense: script tags and backticks must not render literally in
	// free-text fields (file-path code spans legitimately contain backticks).
	assert.NotContains(t, out, "<script>", "free text must be HTML-escaped")
	assert.NotContains(t, out, "gre`ta", "literal backtick must not appear in free text")
	assert.NotContains(t, out, "skep`tic", "literal backtick must not appear in free text")
	assert.NotContains(t, out, "a`b", "literal backtick must not appear in free text")

	// escTrunc caps long fields at maxTextLen=500 total runes (497 content + "...").
	assert.Contains(t, out, strings.Repeat("A", 497)+"...", "escTrunc caps at 500 total runes")
	assert.NotContains(t, out, strings.Repeat("A", 498), "problem content must not exceed 497 runes before the ellipsis")

	// Structural pairing: each position line pairs reviewer, severity, and problem.
	assert.Contains(t, out, "gre&#96;ta — HIGH:", "reviewer must be paired with severity on one line")
}
