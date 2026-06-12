package reconcile

import (
	"sort"

	"github.com/samestrin/atcr/internal/stream"
)

// lineProximity is the inclusive line distance that clusters two findings on the
// same file (AC 01-05: N and N+3 share a cluster, N and N+4 do not).
const lineProximity = 3

// Cluster groups findings into location clusters. Findings on the same file are
// clustered by single-linkage on line number: sorted ascending, a gap of more
// than lineProximity from the PREVIOUS finding starts a new cluster. This
// satisfies the AC's pairwise boundary (two findings at N and N+3 cluster; N and
// N+4 do not) without the adjacent-line split a fixed line/3 bucket suffers.
//
// Single-linkage is transitive: a dense chain (1,4,7,…) can span a cluster wider
// than 3 lines. That is intentional and safe — clustering only scopes which
// findings are compared; actual merging is gated by PROBLEM-text similarity in
// DedupeCluster, so a loose cluster never over-collapses dissimilar findings, it
// only widens the (bounded) comparison set.
//
// File-level findings (Line <= 0, "no specific line"; the parser emits Line 0
// for a missing/non-numeric line and never a negative) form one cluster per
// file, kept separate from line-specific clusters. Files are processed in sorted
// order for deterministic cluster ordering.
func Cluster(findings []stream.Finding) [][]stream.Finding {
	byFile := map[string][]stream.Finding{}
	for _, f := range findings {
		byFile[f.File] = append(byFile[f.File], f)
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)

	var clusters [][]stream.Finding
	for _, file := range files {
		fileLevel, lined := splitFileLevel(byFile[file])
		if len(fileLevel) > 0 {
			clusters = append(clusters, fileLevel)
		}
		// Stable sort by line so equal lines keep input order (deterministic).
		sort.SliceStable(lined, func(i, j int) bool { return lined[i].Line < lined[j].Line })

		var cur []stream.Finding
		prevLine := 0
		for _, f := range lined {
			switch {
			case len(cur) == 0:
				cur = []stream.Finding{f}
			case f.Line-prevLine <= lineProximity:
				cur = append(cur, f)
			default:
				clusters = append(clusters, cur)
				cur = []stream.Finding{f}
			}
			prevLine = f.Line
		}
		if len(cur) > 0 {
			clusters = append(clusters, cur)
		}
	}
	return clusters
}

// splitFileLevel partitions a file's findings into file-level (Line <= 0) and
// line-specific.
func splitFileLevel(findings []stream.Finding) (fileLevel, lined []stream.Finding) {
	for _, f := range findings {
		if f.Line <= 0 {
			fileLevel = append(fileLevel, f)
		} else {
			lined = append(lined, f)
		}
	}
	return fileLevel, lined
}
