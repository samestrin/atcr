package reconcile

import "testing"

// fnd builds a finding with the fields clustering/dedupe care about.
func fnd(file string, line int, problem, reviewer string) Finding {
	return Finding{File: file, Line: line, Problem: problem, Reviewer: reviewer}
}

func TestCluster_SameFileWithinThreeShareCluster(t *testing.T) {
	clusters := Cluster([]Finding{
		fnd("main.go", 42, "a", "x"),
		fnd("main.go", 44, "b", "y"), // gap 2 → same cluster
	})
	length(t, clusters, 1, "one cluster")
	length(t, clusters[0], 2, "two findings in it")
}

func TestCluster_BoundaryDeltaThreeVsFour(t *testing.T) {
	// N and N+3 share; N and N+4 do not.
	same := Cluster([]Finding{fnd("f.go", 10, "a", "x"), fnd("f.go", 13, "b", "y")})
	length(t, same, 1, "lines 10 and 13 (gap 3) share a cluster")

	apart := Cluster([]Finding{fnd("f.go", 10, "a", "x"), fnd("f.go", 14, "b", "y")})
	length(t, apart, 2, "lines 10 and 14 (gap 4) are separate clusters")
}

func TestCluster_DifferentFilesNeverCluster(t *testing.T) {
	clusters := Cluster([]Finding{
		fnd("a.go", 10, "x", "r1"),
		fnd("b.go", 10, "x", "r2"),
	})
	length(t, clusters, 2, "different files never cluster")
}

func TestCluster_SingleLinkageChains(t *testing.T) {
	chain := Cluster([]Finding{fnd("f.go", 1, "a", "x"), fnd("f.go", 4, "b", "y"), fnd("f.go", 7, "c", "z")})
	length(t, chain, 1, "1,4,7 chain into one cluster")
	length(t, chain[0], 3, "all three")

	split := Cluster([]Finding{fnd("f.go", 1, "a", "x"), fnd("f.go", 4, "b", "y"), fnd("f.go", 8, "c", "z")})
	length(t, split, 2, "1,4,8 splits 8 off")
}

func TestCluster_FileLevelSeparateFromLineSpecific(t *testing.T) {
	clusters := Cluster([]Finding{
		fnd("f.go", 0, "file-level issue", "x"), // Line 0 → file-level
		fnd("f.go", 10, "line issue", "y"),
	})
	length(t, clusters, 2, "file-level and line-specific cluster separately")
}

func TestCluster_DeterministicFileOrder(t *testing.T) {
	in := []Finding{fnd("z.go", 1, "a", "x"), fnd("a.go", 1, "b", "y")}
	c1 := Cluster(in)
	c2 := Cluster(in)
	deepEq(t, c1, c2, "deterministic")
	eq(t, c1[0][0].File, "a.go", "files processed in sorted order")
}

// clusterFixtures are the inputs of the Cluster tests above, reused to pin that
// ClusterWithin at LineProximity is exactly Cluster.
func clusterFixtures() map[string][]Finding {
	return map[string][]Finding{
		"within three":   {fnd("main.go", 42, "a", "x"), fnd("main.go", 44, "b", "y")},
		"gap three":      {fnd("f.go", 10, "a", "x"), fnd("f.go", 13, "b", "y")},
		"gap four":       {fnd("f.go", 10, "a", "x"), fnd("f.go", 14, "b", "y")},
		"different file": {fnd("a.go", 10, "x", "r1"), fnd("b.go", 10, "x", "r2")},
		"chain":          {fnd("f.go", 1, "a", "x"), fnd("f.go", 4, "b", "y"), fnd("f.go", 7, "c", "z")},
		"chain split":    {fnd("f.go", 1, "a", "x"), fnd("f.go", 4, "b", "y"), fnd("f.go", 8, "c", "z")},
		"file level":     {fnd("f.go", 0, "file-level issue", "x"), fnd("f.go", 10, "line issue", "y")},
		"file order":     {fnd("z.go", 1, "a", "x"), fnd("a.go", 1, "b", "y")},
		"empty":          nil,
	}
}

func TestClusterWithin_LineProximityEqualsCluster(t *testing.T) {
	for name, in := range clusterFixtures() {
		deepEq(t, ClusterWithin(in, 3), Cluster(in), name)
		deepEq(t, ClusterWithin(in, LineProximity), Cluster(in), name)
	}
}

func TestClusterWithin_ToleranceZeroSplitsAdjacentLines(t *testing.T) {
	apart := ClusterWithin([]Finding{fnd("f.go", 10, "a", "x"), fnd("f.go", 11, "b", "y")}, 0)
	length(t, apart, 2, "tolerance 0: lines 10 and 11 are separate clusters")

	same := ClusterWithin([]Finding{fnd("f.go", 10, "a", "x"), fnd("f.go", 10, "b", "y")}, 0)
	length(t, same, 1, "tolerance 0: two findings on line 10 share a cluster")
}

func TestClusterWithin_ToleranceFiveJoinsGapFive(t *testing.T) {
	same := ClusterWithin([]Finding{fnd("f.go", 10, "a", "x"), fnd("f.go", 15, "b", "y")}, 5)
	length(t, same, 1, "tolerance 5: lines 10 and 15 share a cluster")

	apart := ClusterWithin([]Finding{fnd("f.go", 10, "a", "x"), fnd("f.go", 16, "b", "y")}, 5)
	length(t, apart, 2, "tolerance 5: lines 10 and 16 are separate clusters")
}

func TestClusterWithin_NoLineFindingKeepsItsOwnPerFileCluster(t *testing.T) {
	// A wide tolerance must not pull a no-line finding into a line cluster, and
	// no-line findings of different files stay apart.
	clusters := ClusterWithin([]Finding{
		fnd("f.go", 0, "file-level", "x"),
		fnd("f.go", 1, "line one", "y"),
		fnd("f.go", 0, "also file-level", "z"),
		fnd("g.go", 0, "other file", "x"),
	}, 100)
	length(t, clusters, 3, "f.go no-line, f.go line 1, g.go no-line")
	length(t, clusters[0], 2, "both f.go no-line findings together")
	eq(t, clusters[0][0].Line, 0, "first cluster is the no-line one")
	eq(t, clusters[1][0].Line, 1, "line finding alone")
	eq(t, clusters[2][0].File, "g.go", "g.go no-line cluster")
}
