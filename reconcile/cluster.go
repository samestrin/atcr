package reconcile

import "sort"

// LineProximity is the inclusive line distance that clusters two findings on the
// same file (N and N+3 share a cluster, N and N+4 do not). Exported so the
// grounding gate (internal/fanout) can bind its tolerance to this exact value
// instead of duplicating the literal, keeping the two constants in lockstep.
const LineProximity = 3

// Cluster groups findings into location clusters. Findings on the same file are
// clustered by single-linkage on line number: sorted ascending, a gap of more
// than LineProximity from the PREVIOUS finding starts a new cluster. This
// satisfies the pairwise boundary (two findings at N and N+3 cluster; N and N+4
// do not) without the adjacent-line split a fixed line/3 bucket suffers.
//
// Single-linkage is transitive: a dense chain (1,4,7,…) can span a cluster wider
// than 3 lines. That is intentional and safe — clustering only scopes which
// findings are compared; actual merging is gated by PROBLEM-text similarity in
// DedupeCluster, so a loose cluster never over-collapses dissimilar findings, it
// only widens the (bounded) comparison set.
//
// File-level findings (Line <= 0, "no specific line") form one cluster per file,
// kept separate from line-specific clusters. Files are processed in sorted order
// for deterministic cluster ordering.
func Cluster(findings []Finding) [][]Finding {
	byFile := map[string][]Finding{}
	for _, f := range findings {
		byFile[f.File] = append(byFile[f.File], f)
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)

	var clusters [][]Finding
	for _, file := range files {
		fileLevel, lined := splitFileLevel(byFile[file])
		if len(fileLevel) > 0 {
			clusters = append(clusters, fileLevel)
		}
		// Stable sort by line so equal lines keep input order (deterministic).
		sort.SliceStable(lined, func(i, j int) bool { return lined[i].Line < lined[j].Line })
		clusters = append(clusters, proximityClusters(lined)...)
	}
	return clusters
}

// splitFileLevel partitions a file's findings into file-level (Line <= 0) and
// line-specific.
func splitFileLevel(findings []Finding) (fileLevel, lined []Finding) {
	for _, f := range findings {
		if f.Line <= 0 {
			fileLevel = append(fileLevel, f)
		} else {
			lined = append(lined, f)
		}
	}
	return fileLevel, lined
}
