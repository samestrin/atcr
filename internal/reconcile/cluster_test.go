package reconcile

import (
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fnd builds a finding with the fields clustering/dedupe care about.
func fnd(file string, line int, problem, reviewer string) stream.Finding {
	return stream.Finding{File: file, Line: line, Problem: problem, Reviewer: reviewer}
}

func TestCluster_SameFileWithinThreeShareCluster(t *testing.T) {
	clusters := Cluster([]stream.Finding{
		fnd("main.go", 42, "a", "x"),
		fnd("main.go", 44, "b", "y"), // gap 2 → same cluster
	})
	require.Len(t, clusters, 1)
	assert.Len(t, clusters[0], 2)
}

func TestCluster_BoundaryDeltaThreeVsFour(t *testing.T) {
	// N and N+3 share; N and N+4 do not (AC 01-05 Edge Case 6).
	same := Cluster([]stream.Finding{fnd("f.go", 10, "a", "x"), fnd("f.go", 13, "b", "y")})
	require.Len(t, same, 1, "lines 10 and 13 (gap 3) share a cluster")

	apart := Cluster([]stream.Finding{fnd("f.go", 10, "a", "x"), fnd("f.go", 14, "b", "y")})
	require.Len(t, apart, 2, "lines 10 and 14 (gap 4) are separate clusters")
}

func TestCluster_DifferentFilesNeverCluster(t *testing.T) {
	clusters := Cluster([]stream.Finding{
		fnd("a.go", 10, "x", "r1"),
		fnd("b.go", 10, "x", "r2"),
	})
	assert.Len(t, clusters, 2)
}

func TestCluster_SingleLinkageChains(t *testing.T) {
	// 1,4,7 chain (each gap 3) into one cluster; 1,4,8 splits 8 off.
	chain := Cluster([]stream.Finding{fnd("f.go", 1, "a", "x"), fnd("f.go", 4, "b", "y"), fnd("f.go", 7, "c", "z")})
	require.Len(t, chain, 1)
	assert.Len(t, chain[0], 3)

	split := Cluster([]stream.Finding{fnd("f.go", 1, "a", "x"), fnd("f.go", 4, "b", "y"), fnd("f.go", 8, "c", "z")})
	require.Len(t, split, 2)
}

func TestCluster_FileLevelSeparateFromLineSpecific(t *testing.T) {
	clusters := Cluster([]stream.Finding{
		fnd("f.go", 0, "file-level issue", "x"), // Line 0 → file-level
		fnd("f.go", 10, "line issue", "y"),
	})
	require.Len(t, clusters, 2, "file-level and line-specific findings cluster separately")
}

func TestCluster_DeterministicFileOrder(t *testing.T) {
	in := []stream.Finding{fnd("z.go", 1, "a", "x"), fnd("a.go", 1, "b", "y")}
	c1 := Cluster(in)
	c2 := Cluster(in)
	require.Equal(t, c1, c2)
	assert.Equal(t, "a.go", c1[0][0].File, "files processed in sorted order")
}
