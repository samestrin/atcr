package reconcile

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jf builds a findings.json record with the radar-relevant fields.
func jf(sev, file string, line int, problem string, reviewers []string, disagreement string) JSONFinding {
	conf := ConfMedium
	if len(reviewers) >= 2 {
		conf = ConfHigh
	}
	return JSONFinding{
		Severity: sev, File: file, Line: line, Problem: problem,
		Reviewers: reviewers, Confidence: conf, Disagreement: disagreement,
	}
}

func itemsByKind(df DisagreementsFile, kind string) []DisagreementItem {
	var out []DisagreementItem
	for _, it := range df.Items {
		if it.Kind == kind {
			out = append(out, it)
		}
	}
	return out
}

func TestBuildDisagreements_SeveritySplitScoredBySpreadTimesIndependence(t *testing.T) {
	findings := []JSONFinding{
		jf("CRITICAL", "a.go", 10, "split here", []string{"greta", "kai"}, "LOW vs CRITICAL"),
	}
	df := BuildDisagreements(findings, nil)

	splits := itemsByKind(df, KindSeveritySplit)
	require.Len(t, splits, 1)
	s := splits[0]
	assert.Equal(t, 3, s.Spread, "CRITICAL(4) - LOW(1) = 3")
	assert.Equal(t, 2, s.Independence, "two distinct reviewers")
	assert.Equal(t, 6.0, s.Score, "spread 3 x independence 2")
	assert.Equal(t, "LOW vs CRITICAL", s.Disagreement)
}

func TestBuildDisagreements_SoloScoredBySeverityRank(t *testing.T) {
	findings := []JSONFinding{
		jf("CRITICAL", "solo.go", 1, "only greta saw this", []string{"greta"}, ""),
		jf("LOW", "solo2.go", 2, "only kai saw this", []string{"kai"}, ""),
	}
	df := BuildDisagreements(findings, nil)

	solos := itemsByKind(df, KindSoloFinding)
	require.Len(t, solos, 2)
	// Highest-tension first: CRITICAL solo (rank 4) before LOW solo (rank 1).
	assert.Equal(t, "solo.go", df.Items[0].File)
	assert.Equal(t, 4.0, df.Items[0].Score)
	assert.Equal(t, 1, df.Items[0].Independence)
}

func TestBuildDisagreements_CriticalSoloOutranksLowMediumSplit(t *testing.T) {
	findings := []JSONFinding{
		jf("MEDIUM", "split.go", 5, "low-vs-medium split", []string{"greta", "kai"}, "LOW vs MEDIUM"),
		jf("CRITICAL", "solo.go", 9, "critical solo", []string{"greta"}, ""),
	}
	df := BuildDisagreements(findings, nil)

	require.GreaterOrEqual(t, len(df.Items), 2)
	// Per clarification 2026-06-14: a CRITICAL solo (score 4) outranks a
	// LOW-vs-MEDIUM split (spread 1 x independence 2 = 2).
	assert.Equal(t, KindSoloFinding, df.Items[0].Kind)
	assert.Equal(t, "solo.go", df.Items[0].File)
	assert.Equal(t, KindSeveritySplit, df.Items[1].Kind)
}

func TestBuildDisagreements_MultiReviewerConsensusIsNotSurfaced(t *testing.T) {
	// Two reviewers, same severity, no disagreement — consensus, not tension.
	findings := []JSONFinding{
		jf("HIGH", "agree.go", 3, "both agree", []string{"greta", "kai"}, ""),
	}
	df := BuildDisagreements(findings, nil)
	assert.Empty(t, df.Items, "agreed multi-reviewer findings are not disagreements")
}

func TestBuildDisagreements_GrayZoneClusterSurfacedWithPositions(t *testing.T) {
	clusters := []AmbiguousCluster{
		{
			ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55,
			Findings: []stream.Finding{
				mf("HIGH", "g.go", 7, "buffer overrun risk", "f", "security", 10, "e", "greta"),
				mf("LOW", "g.go", 8, "minor bounds note", "f", "security", 10, "e", "kai"),
			},
		},
	}
	df := BuildDisagreements(nil, clusters)
	gray := itemsByKind(df, KindGrayZone)
	require.Len(t, gray, 1)
	g := gray[0]
	assert.Equal(t, 2, len(g.Positions), "side-by-side model positions")
	assert.Equal(t, 2, g.Spread, "HIGH(3) - LOW(1) = 2")
	assert.Equal(t, 2, g.Independence)
	assert.Equal(t, 4.0, g.Score, "spread 2 x independence 2")
	assert.Contains(t, g.Detail, "0.55", "similarity recorded")
}

func TestBuildDisagreements_GrayZoneMembersNotDoubleSurfacedAsSolo(t *testing.T) {
	// The same two findings appear as singletons in findings.json AND as a
	// gray-zone pair; they must surface once (as the cluster), never as solos.
	clusterFindings := []stream.Finding{
		mf("HIGH", "g.go", 7, "buffer overrun risk", "f", "security", 10, "e", "greta"),
		mf("LOW", "g.go", 7, "minor bounds note", "f", "security", 10, "e", "kai"),
	}
	clusters := []AmbiguousCluster{{ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55, Findings: clusterFindings}}
	findings := []JSONFinding{
		jf("HIGH", "g.go", 7, "buffer overrun risk", []string{"greta"}, ""),
		jf("LOW", "g.go", 7, "minor bounds note", []string{"kai"}, ""),
	}
	df := BuildDisagreements(findings, clusters)
	assert.Empty(t, itemsByKind(df, KindSoloFinding), "gray-zone members excluded from solo tier")
	assert.Len(t, itemsByKind(df, KindGrayZone), 1)
}

func TestBuildDisagreements_ExcludesOutOfScope(t *testing.T) {
	findings := []JSONFinding{
		{Severity: "CRITICAL", File: "pre.go", Line: 1, Problem: "pre-existing",
			Category: CategoryOutOfScope, Reviewers: []string{"greta"}, Confidence: ConfMedium},
		jf("HIGH", "real.go", 2, "in the change", []string{"kai"}, ""),
	}
	df := BuildDisagreements(findings, nil)
	require.Len(t, df.Items, 1, "out-of-scope finding excluded from the radar")
	assert.Equal(t, "real.go", df.Items[0].File)
}

func TestBuildDisagreements_DeterministicOrdering(t *testing.T) {
	findings := []JSONFinding{
		jf("MEDIUM", "b.go", 2, "p2", []string{"kai"}, ""),
		jf("MEDIUM", "a.go", 1, "p1", []string{"greta"}, ""),
		jf("CRITICAL", "c.go", 3, "p3", []string{"greta", "kai"}, "LOW vs CRITICAL"),
	}
	first := BuildDisagreements(findings, nil)
	second := BuildDisagreements(findings, nil)
	require.Equal(t, first, second, "same input yields identical output")
	// Tie-break between the two MEDIUM solos (equal score 2) is by file asc.
	medSolos := itemsByKind(first, KindSoloFinding)
	require.Len(t, medSolos, 2)
	assert.Equal(t, "a.go", medSolos[0].File)
	assert.Equal(t, "b.go", medSolos[1].File)
}

func TestBuildDisagreements_VerificationTieSurfaced(t *testing.T) {
	findings := []JSONFinding{
		{Severity: "HIGH", File: "v.go", Line: 3, Problem: "contested validity",
			Reviewers: []string{"greta"}, Confidence: ConfMedium,
			Verification: &Verification{
				Verdict: VerdictUnverifiable,
				Skeptic: "skeptic-a, skeptic-b",
				Notes:   "skeptic-a: real | skeptic-b: not real",
			}},
	}
	df := BuildDisagreements(findings, nil)
	items := itemsByKind(df, KindVerificationDisagreement)
	require.Len(t, items, 1)
	assert.Equal(t, "skeptic-a, skeptic-b", items[0].Skeptics)
	assert.Equal(t, 3.0, items[0].Score, "no spread → scored by severity rank HIGH(3)")
	assert.Contains(t, items[0].Detail, "not real")
}

func TestBuildDisagreements_SingleSkepticUnverifiableIsNotTension(t *testing.T) {
	// One skeptic that simply could not verify is not a disagreement.
	findings := []JSONFinding{
		{Severity: "HIGH", File: "v.go", Line: 3, Problem: "couldn't check",
			Reviewers: []string{"greta", "kai"}, Confidence: ConfHigh,
			Verification: &Verification{Verdict: VerdictUnverifiable, Skeptic: "skeptic-a", Notes: "timeout"}},
	}
	df := BuildDisagreements(findings, nil)
	assert.Empty(t, itemsByKind(df, KindVerificationDisagreement))
}

func TestBuildDisagreements_RefutedNeverSurfaced(t *testing.T) {
	findings := []JSONFinding{
		{Severity: "CRITICAL", File: "r.go", Line: 1, Problem: "false alarm",
			Reviewers: []string{"greta"}, Confidence: ConfMedium,
			Verification: &Verification{Verdict: VerdictRefuted, Skeptic: "skeptic-a", Notes: "not a bug"}},
	}
	df := BuildDisagreements(findings, nil)
	assert.Empty(t, df.Items, "a refuted finding is not actionable tension")
}

func TestBuildDisagreements_VerificationTieAlsoSplitKeepsSpreadScore(t *testing.T) {
	// A finding that is both a severity split and a skeptic tie is labeled a
	// verification disagreement but keeps the stronger spread-based score.
	findings := []JSONFinding{
		{Severity: "CRITICAL", File: "v.go", Line: 4, Problem: "double tension",
			Reviewers: []string{"greta", "kai"}, Confidence: ConfHigh, Disagreement: "LOW vs CRITICAL",
			Verification: &Verification{Verdict: VerdictUnverifiable, Skeptic: "s-a, s-b", Notes: "split"}},
	}
	df := BuildDisagreements(findings, nil)
	require.Len(t, df.Items, 1)
	assert.Equal(t, KindVerificationDisagreement, df.Items[0].Kind)
	assert.Equal(t, 6.0, df.Items[0].Score, "spread 3 x independence 2 retained")
}

func TestRenderMarkdown_RadarSectionAboveFindings(t *testing.T) {
	// Two reviewers, same location/problem, different severity → merged into one
	// finding carrying a severity-disagreement annotation.
	sources := []Source{
		{Name: "pool", Findings: []stream.Finding{mf("CRITICAL", "a.go", 1, "boom", "f", "security", 10, "e", "greta")}},
		{Name: "host", Findings: []stream.Finding{mf("LOW", "a.go", 1, "boom", "f", "security", 10, "e", "host")}},
	}
	res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	var buf bytes.Buffer
	require.NoError(t, RenderMarkdown(&buf, res))
	out := buf.String()
	require.Contains(t, out, "## Disagreements")
	assert.Less(t, strings.Index(out, "## Disagreements"), strings.Index(out, "## Findings"),
		"radar section renders above consensus findings")
	assert.Contains(t, out, "LOW vs CRITICAL")
}

func TestRenderMarkdown_NoDisagreementsOmitsRadar(t *testing.T) {
	// Two reviewers agree on severity → consensus, no tension, no radar section.
	sources := []Source{
		{Name: "pool", Findings: []stream.Finding{mf("HIGH", "a.go", 1, "boom", "f", "security", 10, "e", "greta")}},
		{Name: "host", Findings: []stream.Finding{mf("HIGH", "a.go", 1, "boom", "f", "security", 10, "e", "host")}},
	}
	res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	var buf bytes.Buffer
	require.NoError(t, RenderMarkdown(&buf, res))
	assert.NotContains(t, buf.String(), "## Disagreements", "no disagreements → section omitted")
}

func TestBuildDisagreements_SchemaMetadata(t *testing.T) {
	df := BuildDisagreements(nil, nil)
	assert.Equal(t, DisagreementsSchemaVersion, df.SchemaVersion)
	assert.Equal(t, IndependenceModelReviewerCount, df.IndependenceModel)
	assert.Empty(t, df.Items)
}
