package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fallback-provenance de-weighting (Epic 19.10 F5, Task 07). A reviewer slot
// served by a shared litellm fallback model is not an independent voice; these
// tests prove distinctReviewerCount collapses it and that the provenance threads
// discovery → stamp → JSONFinding → independence score end-to-end.

func TestDistinctReviewerCount(t *testing.T) {
	tests := []struct {
		name      string
		reviewers []string
		fallback  map[string]string
		want      int
	}{
		{"no fallback data → raw count", []string{"a", "b"}, nil, 2},
		{"empty fallback map → raw count", []string{"a", "b"}, map[string]string{}, 2},
		{"two reviewers, same fallback target → 1", []string{"a", "b"}, map[string]string{"a": "net", "b": "net"}, 1},
		{"two reviewers, different fallback targets → 2", []string{"a", "b"}, map[string]string{"a": "net1", "b": "net2"}, 2},
		{"one fallback + one primary → 2", []string{"a", "b"}, map[string]string{"a": "net"}, 2},
		{"three reviewers, two share a target → 2", []string{"a", "b", "c"}, map[string]string{"a": "net", "b": "net", "c": "other"}, 2},
		{"single reviewer → 1", []string{"a"}, map[string]string{"a": "net"}, 1},
		{"zero reviewers → 0 (atLeastOne floors elsewhere)", nil, map[string]string{"a": "net"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, distinctReviewerCount(tt.reviewers, tt.fallback))
		})
	}
}

// TestSeveritySplitItem_FallbackCollapsesIndependence: a severity split whose two
// reviewers both fell back to the same model scores independence 1, not 2 —
// halving the spread×independence score versus the non-fallback baseline.
func TestSeveritySplitItem_FallbackCollapsesIndependence(t *testing.T) {
	base := jf("CRITICAL", "a.go", 10, "split", []string{"greta", "kai"}, "LOW vs CRITICAL")

	// Baseline: no fallback → independence 2, score = spread(3) × 2 = 6.
	got := severitySplitItem(base)
	assert.Equal(t, 2, got.Independence)
	assert.Equal(t, 6.0, got.Score)

	// Both reviewers fell back to the same net model → independence collapses to 1.
	withFB := base
	withFB.FallbackReviewers = map[string]string{"greta": "net", "kai": "net"}
	gotFB := severitySplitItem(withFB)
	assert.Equal(t, 1, gotFB.Independence, "shared-fallback reviewers count as one voice")
	assert.Equal(t, 3.0, gotFB.Score, "spread(3) × independence(1)")
	// Reviewer identity is preserved — de-weighting must not hide the substitution.
	assert.Equal(t, []string{"greta", "kai"}, gotFB.Reviewers)
}

// TestSoloItem_FallbackAwareIndependence: distinct-target fallbacks still count
// individually (no spurious collapse).
func TestSoloItem_FallbackAwareIndependence(t *testing.T) {
	f := jf("HIGH", "a.go", 1, "p", []string{"greta", "kai"}, "")
	f.FallbackReviewers = map[string]string{"greta": "net1", "kai": "net2"}
	got := soloItem(f)
	assert.Equal(t, 2, got.Independence, "different fallback targets remain independent")
}

// TestVerificationItem_FallbackCollapsesIndependence: the verification tier is
// fallback-aware too.
func TestVerificationItem_FallbackCollapsesIndependence(t *testing.T) {
	f := jf("HIGH", "a.go", 1, "p", []string{"greta", "kai"}, "")
	f.Verification = &Verification{Verdict: "unverifiable", Skeptic: "s1,s2"}
	f.FallbackReviewers = map[string]string{"greta": "net", "kai": "net"}
	got := verificationItem(f)
	assert.Equal(t, 1, got.Independence)
}

// TestBuildDisagreements_GrayZoneFallbackCollapse: a gray-zone cluster whose two
// members both fell back to the same model collapses to independence 1. The
// cluster's own library findings carry no provenance, so the reviewer→target map
// is derived from the merged findings' FallbackReviewers.
func TestBuildDisagreements_GrayZoneFallbackCollapse(t *testing.T) {
	clusters := []AmbiguousCluster{{
		ID: "amb-1", File: "g.go", Line: 7, Similarity: 0.55,
		Findings: []Finding{
			mfL("HIGH", "g.go", 7, "buffer overrun risk", "f", "security", 10, "e", "greta"),
			mfL("LOW", "g.go", 8, "minor bounds note", "f", "security", 10, "e", "kai"),
		},
	}}
	// Merged findings carry the fallback provenance for greta & kai (both → "net").
	findings := []JSONFinding{
		func() JSONFinding {
			f := jf("HIGH", "other.go", 1, "unrelated", []string{"greta"}, "")
			f.FallbackReviewers = map[string]string{"greta": "net"}
			return f
		}(),
		func() JSONFinding {
			f := jf("LOW", "other2.go", 2, "unrelated2", []string{"kai"}, "")
			f.FallbackReviewers = map[string]string{"kai": "net"}
			return f
		}(),
	}
	df := BuildDisagreements(findings, clusters)
	gray := itemsByKind(df, KindGrayZone)
	require.Len(t, gray, 1)
	assert.Equal(t, 1, gray[0].Independence, "gray-zone reviewers sharing a fallback collapse to one voice")
}

// TestJSONFindings_DerivedPathLeavesFallbackEmpty: the no-I/O derived path (a
// Result built without RunReconcile stamping) leaves FallbackReviewers empty,
// matching the PathValid derived-path behavior.
func TestJSONFindings_DerivedPathLeavesFallbackEmpty(t *testing.T) {
	m := Merged{Finding: Finding{Severity: "HIGH", File: "a.go", Line: 1, Reviewers: []string{"greta"}}}
	got := Result{Findings: []Merged{m}}.JSONFindings()
	require.Len(t, got, 1)
	assert.Nil(t, got[0].FallbackReviewers, "derived path carries no fallback provenance")
}

// TestReadSourceFallback covers the fail-closed branches: a missing, malformed,
// or non-fallback status.json all yield "" (an independent, non-fallback voice).
func TestReadSourceFallback(t *testing.T) {
	dir := t.TempDir()
	findings := filepath.Join(dir, "findings.txt") // sibling anchor; need not exist

	// No status.json → "".
	assert.Equal(t, "", readSourceFallback(findings), "missing status.json is fail-closed")

	write := func(body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "status.json"), []byte(body), 0o644))
	}

	// Malformed JSON → "".
	write("{not json")
	assert.Equal(t, "", readSourceFallback(findings), "malformed status.json is fail-closed")

	// fallback_used=false → "" even when fallback_model is present.
	write(`{"fallback_used":false,"fallback_model":"net"}`)
	assert.Equal(t, "", readSourceFallback(findings), "no substitution → empty provenance")

	// fallback_used=true → the served-model value.
	write(`{"fallback_used":true,"fallback_model":"net"}`)
	assert.Equal(t, "net", readSourceFallback(findings))
}

// writeStatus writes a per-agent status.json sibling with fallback provenance,
// using the fallback_model field the fanout engine actually writes (the served
// net model — statusFor copies Result.FallbackModel). Two personas backed by the
// same net model therefore share this value, which is what makes the collapse
// fire — faithfully mirroring engine output (the engine-side recording is proven
// in internal/fanout's TestE2E_Fallback_RecordedInArtifacts).
func writeStatus(t *testing.T, sourcesDir, relDir, fallbackModel string) {
	t.Helper()
	dir := filepath.Join(sourcesDir, relDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := `{"agent":"` + relDir + `","status":"ok","fallback_used":true,"fallback_model":"` + fallbackModel + `"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status.json"), []byte(body), 0o644))
}

// TestRunReconcile_FallbackDeWeightsIndependenceEndToEnd is the AC5 integration
// proof: two personas whose slots both fell back to the same model, reporting a
// severity-split finding at the same location, reconcile to independence 1 (not
// 2) — while both original persona names are still listed (the substitution
// de-weights CONFIDENCE, it does not hide the reviewers).
func TestRunReconcile_FallbackDeWeightsIndependenceEndToEnd(t *testing.T) {
	reviewDir := t.TempDir()
	sources := filepath.Join(reviewDir, "sources")

	// Two agents, same file:line + same problem so they MERGE into one finding with
	// a severity disagreement; both status.json report a fallback to "net".
	writeFindings(t, sources, "greta/findings.txt",
		"CRITICAL|a.go:10|shared concern|fix|security|10|ev|greta\n")
	writeStatus(t, sources, "greta", "net")
	writeFindings(t, sources, "kai/findings.txt",
		"LOW|a.go:10|shared concern|fix|security|10|ev|kai\n")
	writeStatus(t, sources, "kai", "net")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1, "the two personas merge into one finding")

	jf := res.JSONFindings()
	require.Len(t, jf, 1)
	// Provenance stamped end-to-end: both reviewers map to the shared fallback.
	assert.Equal(t, map[string]string{"greta": "net", "kai": "net"}, jf[0].FallbackReviewers)
	// Reviewer identity preserved — de-weight does not collapse the roster.
	assert.ElementsMatch(t, []string{"greta", "kai"}, jf[0].Reviewers)

	df := BuildDisagreements(jf, res.Ambiguous)
	splits := itemsByKind(df, KindSeveritySplit)
	require.Len(t, splits, 1, "a severity split surfaced")
	assert.Equal(t, 1, splits[0].Independence,
		"AC5: two personas on the same fallback model count as one independent voice")
}

// TestRunReconcile_NoFallbackKeepsFullIndependence is the regression control: the
// SAME fixture without any fallback status.json keeps independence 2 — proving the
// collapse is driven by provenance, not an unconditional change.
func TestRunReconcile_NoFallbackKeepsFullIndependence(t *testing.T) {
	reviewDir := t.TempDir()
	sources := filepath.Join(reviewDir, "sources")
	writeFindings(t, sources, "greta/findings.txt",
		"CRITICAL|a.go:10|shared concern|fix|security|10|ev|greta\n")
	writeFindings(t, sources, "kai/findings.txt",
		"LOW|a.go:10|shared concern|fix|security|10|ev|kai\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	jf := res.JSONFindings()
	assert.Nil(t, jf[0].FallbackReviewers, "no status.json → no provenance stamped")

	df := BuildDisagreements(jf, res.Ambiguous)
	splits := itemsByKind(df, KindSeveritySplit)
	require.Len(t, splits, 1)
	assert.Equal(t, 2, splits[0].Independence, "no fallback → two distinct reviewers")
}
