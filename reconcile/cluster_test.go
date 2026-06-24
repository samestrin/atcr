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
