package debate

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/stream"
)

// grayFinding builds a findings.json record for a gray-zone cluster member.
func grayFinding(file string, line int, problem, sev, reviewer string) reconcile.JSONFinding {
	return reconcile.JSONFinding{
		Severity: sev, File: file, Line: line, Problem: problem, Fix: "fix",
		Category: "correctness", EstMinutes: 15, Evidence: "ev-" + reviewer,
		Reviewers: []string{reviewer}, Confidence: "MEDIUM",
	}
}

// reviewDirWithGray seeds reconciled/findings.json + ambiguous.json + manifest so
// the radar surfaces the given gray-zone cluster as a debate item.
func reviewDirWithGray(t *testing.T, findings []reconcile.JSONFinding, clusters ...reconcile.AmbiguousCluster) string {
	t.Helper()
	dir := t.TempDir()
	recon := filepath.Join(dir, reconciledSubdir)
	require.NoError(t, os.MkdirAll(recon, 0o755))
	fdata, err := json.MarshalIndent(findings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(recon, reconcile.FindingsJSON), append(fdata, '\n'), 0o644))
	adata, err := json.MarshalIndent(clusters, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(recon, reconcile.AmbiguousJSON), append(adata, '\n'), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile),
		[]byte(`{"base":"a","head":"deadbeef","stages":["review","verify"]}`), 0o644))
	return dir
}

// grayCluster builds a two-member ambiguous cluster co-located at file/line.
func grayCluster(id, file string, line int, probA, sevA, revA, probB, sevB, revB string) reconcile.AmbiguousCluster {
	return reconcile.AmbiguousCluster{
		ID: id, File: file, Line: line, Similarity: 0.5,
		Findings: []stream.Finding{
			{Severity: sevA, File: file, Line: line, Problem: probA, Reviewer: revA},
			{Severity: sevB, File: file, Line: line, Problem: probB, Reviewer: revB},
		},
	}
}

// grayJudgeTurns scripts proposer/challenger/judge turns where the judge returns a
// gray-zone ruling with the given cluster_decision.
func grayJudgeTurns(decision string) []chatTurn {
	return []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"uphold","cluster_decision":"` + decision + `","settled_severity":"HIGH","reasoning":"same root cause"}`},
	}
}

// TestRunDebate_GrayZoneMergeUnionsFindings: a judge "merge" ruling physically
// unions the cluster's two member findings into one record in findings.json
// (AC1), unioning reviewers and flagging the survivor cluster_merged.
func TestRunDebate_GrayZoneMergeUnionsFindings(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "off by one in loop", "MEDIUM", "alice"),
		grayFinding("a.go", 10, "loop boundary error causes overflow", "HIGH", "bob"),
	}
	cluster := grayCluster("amb-1", "a.go", 10,
		"off by one in loop", "MEDIUM", "alice",
		"loop boundary error causes overflow", "HIGH", "bob")
	dir := reviewDirWithGray(t, findings, cluster)

	cc := &fakeChatCompleter{turns: grayJudgeTurns("merge")}
	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	require.Equal(t, 1, res.Selected)

	f := readFindings(t, dir)
	require.Len(t, f, 1, "the two gray-zone members must be unioned into one record")
	assert.True(t, f[0].ClusterMerged, "the surviving merged record must be flagged cluster_merged")
	assert.Equal(t, "amb-1", f[0].ClusterID, "the survivor must carry the source cluster's stable ID (Epic 6.2 AC2)")
	assert.Equal(t, []string{"alice", "bob"}, f[0].Reviewers)
	assert.Equal(t, "HIGH", f[0].Severity)
	assert.Equal(t, 10, f[0].Line)
	assert.Equal(t, "loop boundary error causes overflow", f[0].Problem,
		"the merged survivor keeps the longest member problem (the union's representative)")
}

// TestRunDebate_GrayZoneSeparateLeavesUnmerged: a judge "separate" ruling leaves
// the cluster's members unmerged in findings.json (AC2) and sets no flag.
func TestRunDebate_GrayZoneSeparateLeavesUnmerged(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "off by one in loop", "MEDIUM", "alice"),
		grayFinding("a.go", 10, "loop boundary error causes overflow", "HIGH", "bob"),
	}
	cluster := grayCluster("amb-1", "a.go", 10,
		"off by one in loop", "MEDIUM", "alice",
		"loop boundary error causes overflow", "HIGH", "bob")
	dir := reviewDirWithGray(t, findings, cluster)

	cc := &fakeChatCompleter{turns: grayJudgeTurns("separate")}
	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	require.Equal(t, 1, res.Selected)

	f := readFindings(t, dir)
	require.Len(t, f, 2, "a separate ruling must leave both members in findings.json")
	for _, x := range f {
		assert.False(t, x.ClusterMerged, "no record may be flagged cluster_merged on a separate ruling")
		assert.Empty(t, x.ClusterID, "no record may carry a ClusterID on a separate ruling")
	}
}

// TestRunDebate_GrayZoneMergeDriftAndNoOverCapture: cross-line drift recovery is
// no longer allowed. A member whose problem drifted to a different line and is
// the sole record there is NOT absorbed, even if that different line is a member
// location, because no other member anchored exactly at that line. An unrelated
// finding at a yet-different line is also untouched.
func TestRunDebate_GrayZoneMergeDriftAndNoOverCapture(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "alpha problem text", "MEDIUM", "alice"),
		grayFinding("a.go", 12, "longest merged problem text from a third finding", "HIGH", "bob"),
		grayFinding("a.go", 50, "totally unrelated finding", "LOW", "carol"),
	}
	// Cluster member B's raw problem ("beta problem text") no longer matches the
	// drifted record at a.go:12, and no member matched exactly at a.go:12, so
	// recovery is disabled by the exactMatchedLocs guard.
	cluster := grayCluster("amb-1", "a.go", 10,
		"alpha problem text", "MEDIUM", "alice",
		"beta problem text", "HIGH", "bob")
	cluster.Findings[1].Line = 12
	dir := reviewDirWithGray(t, findings, cluster)

	cc := &fakeChatCompleter{turns: grayJudgeTurns("merge")}
	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	require.Equal(t, 1, res.Selected)

	f := readFindings(t, dir)
	require.Len(t, f, 3, "cross-line drift is disabled; all three original findings remain separate")
	for i := range f {
		assert.False(t, f[i].ClusterMerged, "no finding should be merged when cross-line drift recovery is disabled")
	}
}

// TestRunDebate_GrayZoneMergeDoesNotAbsorbUnrelatedAtMemberLoc: member A matches
// exactly, member B's location holds a single unrelated finding, and no member
// matched exactly at B's line. The unrelated finding must be preserved and the
// cluster must not merge (per the cluster.go:132 acceptance test).
func TestRunDebate_GrayZoneMergeDoesNotAbsorbUnrelatedAtMemberLoc(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "alpha problem text", "MEDIUM", "alice"),
		grayFinding("a.go", 12, "unrelated finding at member B's line", "HIGH", "bob"),
	}
	cluster := grayCluster("amb-1", "a.go", 10,
		"alpha problem text", "MEDIUM", "alice",
		"beta problem text", "HIGH", "bob")
	cluster.Findings[1].Line = 12
	dir := reviewDirWithGray(t, findings, cluster)

	cc := &fakeChatCompleter{turns: grayJudgeTurns("merge")}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	f := readFindings(t, dir)
	require.Len(t, f, 2, "the unrelated finding at a.go:12 must not be absorbed")
	for i := range f {
		assert.False(t, f[i].ClusterMerged, "no merge should occur when member B's line holds only an unrelated finding")
	}
	assert.Equal(t, "unrelated finding at member B's line", f[1].Problem)
}

// TestRunDebate_GrayZoneMergeSameLineDrift: two members co-located at the SAME
// line where one member's problem drifted (further-merged with a third finding).
// Per-location drift recovery unions the drifted member (the lone unmatched record
// at that line after the exact-match pass), anchored by the other member's exact
// match.
func TestRunDebate_GrayZoneMergeSameLineDrift(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "alpha problem text", "MEDIUM", "alice"),
		grayFinding("a.go", 10, "longest merged problem text from a third finding", "HIGH", "bob"),
	}
	// Member B's raw problem ("beta problem text") drifted; it is the lone unmatched
	// record at a.go:10 after member A matches exactly.
	cluster := grayCluster("amb-1", "a.go", 10,
		"alpha problem text", "MEDIUM", "alice",
		"beta problem text", "HIGH", "bob")
	dir := reviewDirWithGray(t, findings, cluster)

	cc := &fakeChatCompleter{turns: grayJudgeTurns("merge")}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	f := readFindings(t, dir)
	require.Len(t, f, 1, "the anchored member plus the same-line drifted member union to one")
	assert.True(t, f[0].ClusterMerged)
	assert.Equal(t, []string{"alice", "bob"}, f[0].Reviewers)
}

// TestRunDebate_GrayZoneMergeRequiresExactAnchor: when NO cluster member matches a
// findings.json record by exact File+Line+Problem (e.g. both members were
// refuted/removed and unrelated findings now sit at those lines), the merge is a
// strict no-op — a group of purely fallback-matched records is never unioned.
func TestRunDebate_GrayZoneMergeRequiresExactAnchor(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "unrelated record now at line 10", "MEDIUM", "alice"),
		grayFinding("a.go", 12, "unrelated record now at line 12", "HIGH", "bob"),
	}
	cluster := grayCluster("amb-1", "a.go", 10,
		"original member A text", "MEDIUM", "alice",
		"original member B text", "HIGH", "bob")
	cluster.Findings[1].Line = 12
	dir := reviewDirWithGray(t, findings, cluster)

	cc := &fakeChatCompleter{turns: grayJudgeTurns("merge")}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	f := readFindings(t, dir)
	require.Len(t, f, 2, "no exact member anchor: nothing may be unioned")
	for _, x := range f {
		assert.False(t, x.ClusterMerged)
	}
}

// TestRunDebate_GrayZoneMergeIsIdempotent: a second debate run over an
// already-applied cluster does not re-debate, re-merge, or corrupt it (AC4). The
// merged survivor carries cluster_merged, so the radar filters the gray-zone item
// out of selection on the re-run.
func TestRunDebate_GrayZoneMergeIsIdempotent(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "off by one in loop", "MEDIUM", "alice"),
		grayFinding("a.go", 10, "loop boundary error causes overflow", "HIGH", "bob"),
	}
	cluster := grayCluster("amb-1", "a.go", 10,
		"off by one in loop", "MEDIUM", "alice",
		"loop boundary error causes overflow", "HIGH", "bob")
	dir := reviewDirWithGray(t, findings, cluster)

	// Run 1 applies the merge.
	cc1 := &fakeChatCompleter{turns: grayJudgeTurns("merge")}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc1))
	require.NoError(t, err)
	after1 := readFindings(t, dir)
	require.Len(t, after1, 1)
	require.True(t, after1[0].ClusterMerged)

	// Run 2: the cluster is already applied — it must be filtered out of selection.
	cc2 := &fakeChatCompleter{turns: grayJudgeTurns("merge")}
	res2, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc2))
	require.NoError(t, err)
	assert.Equal(t, 0, res2.Selected, "an already-merged cluster must not be re-selected")

	after2 := readFindings(t, dir)
	require.Len(t, after2, 1, "re-run must not duplicate or re-split the merged record")
	assert.True(t, after2[0].ClusterMerged)
	assert.Equal(t, []string{"alice", "bob"}, after2[0].Reviewers)
}

// TestRunDebate_GrayZoneMergeLeavesAdjudicationArtifactsUntouched: inline gray-zone
// application (Option A) and the authored adjudication.json path are independent
// (AC3). A debate merge rewrites only findings.json/debate.json/manifest.json — it
// must not rewrite the reconcile-time ambiguous.json sidecar, nor synthesize an
// adjudication.json or ambiguous.original.json (the authored path's artifacts).
func TestRunDebate_GrayZoneMergeLeavesAdjudicationArtifactsUntouched(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "off by one in loop", "MEDIUM", "alice"),
		grayFinding("a.go", 10, "loop boundary error causes overflow", "HIGH", "bob"),
	}
	cluster := grayCluster("amb-1", "a.go", 10,
		"off by one in loop", "MEDIUM", "alice",
		"loop boundary error causes overflow", "HIGH", "bob")
	dir := reviewDirWithGray(t, findings, cluster)

	ambPath := filepath.Join(dir, reconciledSubdir, reconcile.AmbiguousJSON)
	before, err := os.ReadFile(ambPath)
	require.NoError(t, err)

	cc := &fakeChatCompleter{turns: grayJudgeTurns("merge")}
	_, err = runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	after, err := os.ReadFile(ambPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "debate must not rewrite the reconcile-time ambiguous.json")

	for _, name := range []string{reconcile.AdjudicationJSON, reconcile.OriginalAmbiguousJSON} {
		_, statErr := os.Stat(filepath.Join(dir, reconciledSubdir, name))
		assert.True(t, os.IsNotExist(statErr), "debate must not create the authored-path artifact %s", name)
	}
}

// TestRunDebate_GrayZoneSingleMemberMergeDoesNotWarn: a single-member gray-zone
// cluster is structurally unmergeable (a merge needs at least two members). The
// merge must be a silent no-op and must NOT be reported as a ruling that "could
// not be applied", which would mislead operators.
func TestRunDebate_GrayZoneSingleMemberMergeDoesNotWarn(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "only member", "MEDIUM", "alice"),
	}
	cluster := reconcile.AmbiguousCluster{
		ID: "amb-1", File: "a.go", Line: 10, Similarity: 0.5,
		Findings: []stream.Finding{
			{Severity: "MEDIUM", File: "a.go", Line: 10, Problem: "only member", Reviewer: "alice"},
		},
	}
	dir := reviewDirWithGray(t, findings, cluster)

	var buf bytes.Buffer
	logger, err := log.New("warn", "text", &buf)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), logger)

	cc := &fakeChatCompleter{turns: grayJudgeTurns("merge")}
	_, err = runDebate(ctx, dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	assert.NotContains(t, buf.String(), "could not be applied",
		"single-member cluster must not trigger the 'could not be applied' warning")
}

// TestRunDebate_CorruptAmbiguousJSONLogsWarning: a present-but-unparseable
// ambiguous.json must be logged at WARN so the operator knows gray-zone merge
// rulings are being dropped, rather than silently degrading to zero clusters.
func TestRunDebate_CorruptAmbiguousJSONLogsWarning(t *testing.T) {
	// No selectable findings — we only want to assert the ambiguous.json warning.
	findings := []reconcile.JSONFinding{
		{Severity: "MEDIUM", File: "a.go", Line: 10, Problem: "x", Reviewers: []string{}},
	}
	cluster := grayCluster("amb-1", "a.go", 10, "a", "MEDIUM", "alice", "b", "MEDIUM", "bob")
	dir := reviewDirWithGray(t, findings, cluster)

	// Corrupt the sidecar after the helper wrote a valid one.
	ambPath := filepath.Join(dir, reconciledSubdir, reconcile.AmbiguousJSON)
	require.NoError(t, os.WriteFile(ambPath, []byte("not-json"), 0o644))

	var buf bytes.Buffer
	logger, err := log.New("warn", "text", &buf)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), logger)

	cc := &fakeChatCompleter{turns: []chatTurn{}}
	_, err = runDebate(ctx, dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "ambiguous.json unreadable", "corrupt ambiguous.json must be logged")
	assert.Contains(t, buf.String(), "gray-zone merges disabled", "warning must explain the consequence")
}

// TestIndexClusters_RoundTripsBuildDisagreementsProblem pins the cross-package
// round-trip the clusterIdx lookup depends on: the gray-zone radar item's Problem is
// produced by reconcile.ClusterDisplayProblem inside BuildDisagreements, and
// indexClusters keys the cluster by that same reconcile.ClusterDisplayProblem.
// runDebate looks a debated DisagreementItem up in the clusterIdx by
// {File, Line, Problem}; collapsing the two former representative-problem impls
// (reconcile.longestProblem and debate.clusterDisplayProblem) into one shared helper
// makes the agreement structural, but this test still guards the round-trip end to
// end — including the equal-length-problem tie the TD item called out (strict
// greater-than, so the FIRST member wins).
func TestIndexClusters_RoundTripsBuildDisagreementsProblem(t *testing.T) {
	cases := []struct {
		name         string
		probA, probB string
	}{
		// Member B is strictly longer: both sides pick B.
		{"distinct lengths", "short problem", "a substantially longer problem description"},
		// Equal-length problems (differ only in the final token) force the tie:
		// strict greater-than means the FIRST member (A) wins on both sides.
		{"equal-length tie", "duplicate finding text AAAA", "duplicate finding text BBBB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := grayCluster("amb-1", "a.go", 10,
				tc.probA, "MEDIUM", "alice",
				tc.probB, "HIGH", "bob")

			// The radar item's Problem comes from reconcile.ClusterDisplayProblem.
			df := reconcile.BuildDisagreements(nil, []reconcile.AmbiguousCluster{cluster})
			var item reconcile.DisagreementItem
			found := false
			for _, it := range df.Items {
				if it.Kind == reconcile.KindGrayZone {
					item, found = it, true
					break
				}
			}
			require.True(t, found, "BuildDisagreements must surface the cluster as a gray_zone radar item")

			// indexClusters keys on reconcile.ClusterDisplayProblem; the debated item resolves
			// back by its Problem only if the two representatives agree.
			idx := indexClusters([]reconcile.AmbiguousCluster{cluster})
			got, ok := idx[FindingKey{File: item.File, Line: item.Line, Problem: item.Problem}]
			require.True(t, ok, "the radar item's Problem must resolve back to its cluster via indexClusters")
			assert.Equal(t, cluster.ID, got.ID)
		})
	}
}

// TestFirstClusterRulingCollision_GuardsRulingsKeyspace pins the Epic 6.1
// invariant that gray-zone cluster members never share a location with a
// single-finding rulings key (gray-zone items are classified into the cluster
// branch in runDebate and never enter the rulings map). The guard returns the
// colliding "File:Line" when the invariant is broken — a tripwire for a future
// radar change that let a gray member also surface as a solo/split tier item — and
// "" when it holds (TD cluster.go:applyClusterMerges).
func TestFirstClusterRulingCollision_GuardsRulingsKeyspace(t *testing.T) {
	clusters := []reconcile.AmbiguousCluster{
		grayCluster("c1", "a.go", 10, "p1", "HIGH", "alice", "p2", "MEDIUM", "bob"),
	}

	// Invariant holds: the rulings key is at a different location than any member.
	rulings := map[FindingKey]ruleApply{
		{File: "b.go", Line: 99, Problem: "unrelated single-finding"}: {verdict: "confirmed"},
	}
	assert.Empty(t, firstClusterRulingCollision(rulings, clusters), "no collision when rulings and cluster members are at distinct locations")

	// Invariant broken: a rulings key collides with a cluster member's location.
	rulings[FindingKey{File: "a.go", Line: 10, Problem: "leaked into rulings"}] = ruleApply{verdict: "confirmed"}
	assert.Equal(t, "a.go:10", firstClusterRulingCollision(rulings, clusters), "a gray member location that also keys the rulings map must be reported")
}

// TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID inverts the former
// over-suppression pin (Epic 6.2 AC4): filterMergedClusters now keys idempotency on
// cluster identity (ClusterID), not File+Line alone. Two DISTINCT gray-zone clusters
// sharing one canonical File+Line — merging cluster #1 (its survivor carries
// ClusterMerged + the cluster's stable ID) must suppress ONLY cluster #1's radar
// item on a re-run; cluster #2, never merged, must still be processed.
func TestFilterMergedClusters_CoLocatedDistinctClustersKeyedByID(t *testing.T) {
	// Two DISTINCT clusters at the same canonical File+Line, with stable IDs. Each
	// member B is strictly longer so ClusterDisplayProblem is deterministic and the
	// radar item resolves back to its cluster via indexClusters.
	c1 := grayCluster("amb-1", "a.go", 10, "c1a", "MEDIUM", "alice", "cluster one longer problem", "HIGH", "bob")
	c2 := grayCluster("amb-2", "a.go", 10, "c2a", "MEDIUM", "carol", "cluster two longer problem", "HIGH", "dave")
	clusterIdx := indexClusters([]reconcile.AmbiguousCluster{c1, c2})

	// Two DISTINCT gray-zone radar items sharing one canonical File+Line, each
	// carrying its cluster's representative (longest-member) problem.
	items := []reconcile.DisagreementItem{
		{Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "cluster one longer problem"},
		{Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "cluster two longer problem"},
	}

	// First run: no ClusterMerged record exists yet — both items pass through.
	firstRun := filterMergedClusters(items, []reconcile.JSONFinding{
		{File: "a.go", Line: 10, Problem: "c1a"},
	}, clusterIdx)
	assert.Len(t, firstRun, 2, "first run (no ClusterMerged record) must not filter either co-located cluster")

	// Re-run: cluster #1 was merged (its survivor sits at a.go:10 flagged
	// ClusterMerged with ClusterID amb-1). Identity-keyed filtering suppresses ONLY
	// cluster #1; cluster #2 (amb-2), never merged, is still processed.
	reRun := filterMergedClusters(items, []reconcile.JSONFinding{
		{File: "a.go", Line: 10, Problem: "merged survivor", ClusterMerged: true, ClusterID: "amb-1"},
	}, clusterIdx)
	require.Len(t, reRun, 1, "only the merged cluster's item is suppressed; the co-located distinct cluster survives")
	assert.Equal(t, "cluster two longer problem", reRun[0].Problem, "the surviving item must be cluster #2 (amb-2)")
}

// TestFilterMergedClusters_LegacyEmptyClusterIDDoesNotSuppress pins the
// backward-compat path (Epic 6.2): a ClusterMerged survivor written by a pre-6.2
// debate carries no ClusterID. Identity-keyed filtering matches only non-empty IDs,
// so such a legacy record suppresses nothing — the cluster is re-debated once (an
// idempotent no-op) and self-heals, rather than a blank ID silently matching a
// blank-keyed item.
func TestFilterMergedClusters_LegacyEmptyClusterIDDoesNotSuppress(t *testing.T) {
	t.Run("single cluster legacy survivor", func(t *testing.T) {
		c1 := grayCluster("amb-1", "a.go", 10, "c1a", "MEDIUM", "alice", "cluster one longer problem", "HIGH", "bob")
		clusterIdx := indexClusters([]reconcile.AmbiguousCluster{c1})
		items := []reconcile.DisagreementItem{
			{Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "cluster one longer problem"},
		}

		// A legacy merged survivor: ClusterMerged set, but no ClusterID (pre-6.2).
		out := filterMergedClusters(items, []reconcile.JSONFinding{
			{File: "a.go", Line: 10, Problem: "merged survivor", ClusterMerged: true},
		}, clusterIdx)
		require.Len(t, out, 1, "a ClusterMerged record with no ClusterID must not suppress any item")
		assert.Equal(t, "cluster one longer problem", out[0].Problem)
	})

	t.Run("mixed legacy and new-ID survivors", func(t *testing.T) {
		c1 := grayCluster("amb-1", "a.go", 10, "c1a", "MEDIUM", "alice", "cluster one longer problem", "HIGH", "bob")
		c2 := grayCluster("amb-2", "b.go", 20, "c2a", "MEDIUM", "carol", "cluster two longer problem", "HIGH", "dave")
		clusterIdx := indexClusters([]reconcile.AmbiguousCluster{c1, c2})
		items := []reconcile.DisagreementItem{
			{Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "cluster one longer problem"},
			{Kind: reconcile.KindGrayZone, File: "b.go", Line: 20, Problem: "cluster two longer problem"},
		}

		// One legacy merged record (no ClusterID) plus one new merged record (with ClusterID).
		out := filterMergedClusters(items, []reconcile.JSONFinding{
			{File: "a.go", Line: 10, Problem: "legacy survivor", ClusterMerged: true},
			{File: "b.go", Line: 20, Problem: "new survivor", ClusterMerged: true, ClusterID: "amb-2"},
		}, clusterIdx)
		require.Len(t, out, 1, "only the new-ID cluster's item may be suppressed; the legacy record suppresses nothing")
		assert.Equal(t, "cluster one longer problem", out[0].Problem, "the legacy cluster's item must survive")
	})
}

// TestFilterMergedClusters_NilClusterIdxPassesThrough documents that callers may
// pass a nil cluster index (e.g. when no gray-zone clusters exist) without
// triggering a panic, and that gray-zone items pass through unchanged.
func TestFilterMergedClusters_NilClusterIdxPassesThrough(t *testing.T) {
	items := []reconcile.DisagreementItem{
		{Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "gray item"},
	}
	out := filterMergedClusters(items, []reconcile.JSONFinding{
		{File: "a.go", Line: 10, Problem: "merged survivor", ClusterMerged: true, ClusterID: "amb-1"},
	}, nil)
	require.Len(t, out, 1, "nil clusterIdx must not suppress items")
	assert.Equal(t, "gray item", out[0].Problem)
}
