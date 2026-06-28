package reconcile

import "testing"

// fakeGrouper keys findings by line number, standing in for the AST-isomorphism
// grouper without pulling a wasm dependency into this zero-dep library's tests.
type fakeGrouper struct{ keys map[int]string }

func (g fakeGrouper) GroupKey(f Finding) string { return g.keys[f.Line] }

func TestClusterWith_SameKeyGroupsAcrossWideGap(t *testing.T) {
	fs := []Finding{fnd("a.go", 10, "x", "r1"), fnd("a.go", 40, "y", "r2")}
	g := fakeGrouper{keys: map[int]string{10: "a.go\x00H", 40: "a.go\x00H"}}
	clusters := ClusterWith(fs, g)
	length(t, clusters, 1, "same AST key clusters despite a 30-line gap")
	length(t, clusters[0], 2, "both findings in the one cluster")
}

func TestClusterWith_DifferentKeysSplitWithinProximity(t *testing.T) {
	fs := []Finding{fnd("a.go", 10, "x", "r1"), fnd("a.go", 12, "y", "r2")}
	g := fakeGrouper{keys: map[int]string{10: "a.go\x00H1", 12: "a.go\x00H2"}}
	clusters := ClusterWith(fs, g)
	length(t, clusters, 2, "distinct AST keys split even within ±3 lines")
}

func TestClusterWith_EmptyKeyFallsBackToProximity(t *testing.T) {
	fs := []Finding{fnd("a.go", 10, "x", "r1"), fnd("a.go", 12, "y", "r2"), fnd("a.go", 40, "z", "r3")}
	g := fakeGrouper{keys: map[int]string{}} // no parser/AST available → all empty keys
	clusters := ClusterWith(fs, g)
	length(t, clusters, 2, "unkeyed findings use ±3 proximity: {10,12} and {40}")
}

func TestClusterWith_MixedKeyedAndUnkeyed(t *testing.T) {
	fs := []Finding{
		fnd("a.go", 10, "x", "r1"),
		fnd("a.go", 41, "y", "r2"),
		fnd("a.go", 11, "z", "r3"), // unkeyed, near 10 but 10 is keyed → its own proximity cluster
	}
	g := fakeGrouper{keys: map[int]string{10: "K", 41: "K"}}
	clusters := ClusterWith(fs, g)
	// {10,41} keyed cluster; {11} unkeyed proximity cluster.
	length(t, clusters, 2, "one keyed cluster + one unkeyed cluster")
}

func TestClusterWith_NilGrouperMatchesLegacyCluster(t *testing.T) {
	fs := []Finding{fnd("a.go", 10, "x", "r1"), fnd("a.go", 40, "y", "r2"), fnd("b.go", 0, "f", "r3")}
	deepEq(t, ClusterWith(fs, nil), Cluster(fs), "nil grouper is identical to legacy Cluster")
}
