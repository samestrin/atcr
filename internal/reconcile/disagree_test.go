package reconcile

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
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

func TestEmit_WritesDisagreementsJSON(t *testing.T) {
	reviewDir := t.TempDir()
	reconDir := filepath.Join(reviewDir, "reconciled")
	sources := []Source{
		{Name: "pool", Findings: []stream.Finding{mf("CRITICAL", "a.go", 1, "boom", "f", "security", 10, "e", "greta")}},
		{Name: "host", Findings: []stream.Finding{mf("LOW", "a.go", 1, "boom", "f", "security", 10, "e", "host")}},
	}
	res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	require.NoError(t, Emit(reconDir, res))
	assert.FileExists(t, filepath.Join(reconDir, DisagreementsJSON))

	df, err := ReadDisagreements(reviewDir)
	require.NoError(t, err)
	assert.Equal(t, DisagreementsSchemaVersion, df.SchemaVersion)
	assert.Equal(t, IndependenceModelReviewerCount, df.IndependenceModel)
	require.Len(t, df.Items, 1)
	assert.Equal(t, KindSeveritySplit, df.Items[0].Kind)
	assert.Equal(t, "LOW vs CRITICAL", df.Items[0].Disagreement)
}

func TestReadDisagreements_MissingReturnsEmpty(t *testing.T) {
	df, err := ReadDisagreements(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, df.Items)
}

func TestReadDisagreements_MalformedIsError(t *testing.T) {
	reviewDir := t.TempDir()
	reconDir := filepath.Join(reviewDir, "reconciled")
	require.NoError(t, os.MkdirAll(reconDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(reconDir, DisagreementsJSON), []byte("{not json"), 0o644))
	_, err := ReadDisagreements(reviewDir)
	require.Error(t, err)
}

func TestBuildDisagreements_SchemaMetadata(t *testing.T) {
	df := BuildDisagreements(nil, nil)
	assert.Equal(t, DisagreementsSchemaVersion, df.SchemaVersion)
	assert.Equal(t, IndependenceModelReviewerCount, df.IndependenceModel)
	assert.Empty(t, df.Items)
}

// TestDisagreementsSchema_StableContract pins the literal schema version and the
// JSON field names Epic 6.0 (Cross-Examination) consumes. A rename or version
// bump here is a breaking change to a downstream contract — update Epic 6.0 and
// docs/disagreement-radar.md before changing this test.
func TestDisagreementsSchema_StableContract(t *testing.T) {
	assert.Equal(t, "1.0", DisagreementsSchemaVersion, "Epic 6.0 contract version")
	assert.Equal(t, "distinct-reviewer-count", IndependenceModelReviewerCount)

	df := BuildDisagreements([]JSONFinding{
		jf("CRITICAL", "a.go", 1, "p", []string{"greta", "kai"}, "LOW vs CRITICAL"),
	}, nil)
	data, err := json.MarshalIndent(df, "", "  ")
	require.NoError(t, err)
	out := string(data)
	for _, key := range []string{
		`"schemaVersion"`, `"independenceModel"`, `"items"`,
		`"kind"`, `"file"`, `"line"`, `"severity"`, `"problem"`,
		`"score"`, `"spread"`, `"independence"`, `"reviewers"`, `"disagreement"`,
	} {
		assert.Contains(t, out, key, "handoff schema must expose %s", key)
	}
}

func TestBuildDisagreements_GrayZoneMembersExcludedWhenProblemTextDiffers(t *testing.T) {
	// A gray-zone member may also be merged with a third finding, causing the
	// JSONFinding.Problem to become the merged longestField text while the
	// AmbiguousCluster.Findings retains the raw member's original problem. The
	// gray-zone exclusion must still apply — otherwise the location
	// double-surfaces as both gray_zone and solo/split.
	clusterFindings := []stream.Finding{
		mf("HIGH", "g.go", 7, "raw cluster problem A", "f", "security", 10, "e", "greta"),
		mf("LOW", "g.go", 8, "raw cluster problem B", "f", "security", 10, "e", "kai"),
	}
	clusters := []AmbiguousCluster{{ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55, Findings: clusterFindings}}
	// JSONFinding with problem text that differs from the cluster's raw members
	// (simulating the merge pipeline's longestField replacement).
	findings := []JSONFinding{
		jf("HIGH", "g.go", 7, "raw cluster problem A", []string{"greta"}, ""),
		jf("LOW", "g.go", 8, "MERGED LONGER REPLACEMENT TEXT", []string{"kai"}, ""),
	}
	df := BuildDisagreements(findings, clusters)
	assert.Empty(t, itemsByKind(df, KindSoloFinding),
		"gray-zone member must be excluded even when JSONFinding.Problem differs from cluster member's problem")
	assert.Len(t, itemsByKind(df, KindGrayZone), 1)
}

func TestBuildDisagreements_OutOfScopeNormalizationExcludesUntrimmedVariants(t *testing.T) {
	// Per-finding out-of-scope check uses exact match while allOutOfScope
	// lower-cases and trims — a finding with " Out-Of-Scope " entered the radar
	// via the per-finding tier but was excluded by the cluster guard.
	findings := []JSONFinding{
		{Severity: "CRITICAL", File: "pre.go", Line: 1, Problem: "pre-existing",
			Category: " Out-Of-Scope ", Reviewers: []string{"greta"}, Confidence: ConfMedium},
		jf("HIGH", "real.go", 2, "in the change", []string{"kai"}, ""),
	}
	df := BuildDisagreements(findings, nil)
	require.Len(t, df.Items, 1, "untrimmed/upper out-of-scope finding excluded from radar")
	assert.Equal(t, "real.go", df.Items[0].File)
}

func TestBuildDisagreements_EmptyReviewersNotPromotedToSolo(t *testing.T) {
	// A malformed finding with an empty Reviewers slice (len==0, data
	// corruption) must NOT be surfaced as a solo with fabricated Independence=1.
	findings := []JSONFinding{
		{Severity: "HIGH", File: "bad.go", Line: 1, Problem: "no reviewer",
			Reviewers: []string{}, Confidence: ConfMedium},
		jf("LOW", "real.go", 2, "solo", []string{"kai"}, ""),
	}
	df := BuildDisagreements(findings, nil)
	solos := itemsByKind(df, KindSoloFinding)
	for _, s := range solos {
		assert.NotEqual(t, "bad.go", s.File, "empty-reviewers finding must not surface as solo")
	}
}

func TestBuildDisagreements_EmptyClusterDoesNotPanic(t *testing.T) {
	// allOutOfScope returns false for empty clusters, so they reach grayZoneItem.
	// grayZoneItem must guard c.Findings[0] access with a len check.
	clusters := []AmbiguousCluster{
		{ID: "amb-empty", File: "e.go", Line: 1, Similarity: 0.5, Findings: []stream.Finding{}},
	}
	assert.NotPanics(t, func() {
		df := BuildDisagreements(nil, clusters)
		// Empty cluster must not appear in the radar.
		for _, it := range df.Items {
			assert.NotEqual(t, "e.go", it.File)
		}
	})
}

func TestBuildDisagreements_GrayZoneAllUnknownSeverityStillScoresAboveZero(t *testing.T) {
	// A gray-zone cluster whose members all carry unknown/blank severities must
	// not score 0 — otherwise a real tension cluster sorts below every solo LOW
	// finding. The cluster has two distinct reviewers and real ambiguity; it
	// deserves a floor score above a LOW solo (rank 1).
	clusterFindings := []stream.Finding{
		mf("", "u.go", 1, "unknown sev A", "f", "misc", 5, "e", "greta"),
		mf("", "u.go", 2, "unknown sev B", "f", "misc", 5, "e", "kai"),
	}
	clusters := []AmbiguousCluster{{ID: "amb-u", File: "u.go", Line: 1, Similarity: 0.5, Findings: clusterFindings}}
	df := BuildDisagreements(nil, clusters)
	gray := itemsByKind(df, KindGrayZone)
	require.Len(t, gray, 1)
	assert.Greater(t, gray[0].Score, 0.0, "unknown-severity cluster must still score above zero")
}

func TestScoreFor_LargeInputsNoIntOverflow(t *testing.T) {
	// scoreFor multiplies spread × independence. Both are int, and independence
	// is derived from len(f.Reviewers) — unbounded external input. When the int
	// product overflows, the float64 conversion yields a wrong score. The fix
	// converts operands to float64 before multiplying.
	big := int(math.MaxInt64)
	score := scoreFor(big, big, 0)
	// int64 overflow: MaxInt64*MaxInt64 wraps to 1 → float64(1) = 1.0 (wrong).
	// float64 first: float64(MaxInt64)*float64(MaxInt64) ≈ 8.5e37 (correct).
	assert.Greater(t, score, 1e30, "scoreFor must widen to float64 before multiplying to avoid int overflow")
}
