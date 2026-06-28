package reconcile

import "sort"

// Grouper supplies a structural grouping key for a finding, overriding the
// default line-proximity pre-filter. It is the seam the AST-isomorphism grouper
// (root module, wazero-backed) plugs into without adding any dependency to this
// zero-dependency library: the library knows only this stdlib-only interface.
//
// GroupKey returns a stable key such that two findings sharing a non-empty key
// belong in the same comparison cluster regardless of their line distance. An
// empty string means "no structural key available for this finding" (e.g. no
// parser for its language, an unparseable file, or a file-level finding); such
// findings fall back to ±lineProximity grouping. A key is expected to be
// file-scoped so findings in different files never collide.
type Grouper interface {
	GroupKey(f Finding) string
}

// ClusterWith groups findings using g as the primary signal: findings sharing a
// non-empty key cluster together (any line distance), and findings with an empty
// key fall back to the legacy line-proximity clustering. A nil g reproduces
// Cluster exactly, so the AST path is strictly opt-in and the default pipeline is
// unchanged.
//
// Determinism: files are processed in sorted order; within a file, keyed clusters
// are emitted in ascending-first-line order (keys are first seen while scanning
// line-sorted findings), then the proximity clusters of the unkeyed remainder.
func ClusterWith(findings []Finding, g Grouper) [][]Finding {
	if g == nil {
		return Cluster(findings)
	}

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
		sort.SliceStable(lined, func(i, j int) bool { return lined[i].Line < lined[j].Line })

		keyed := map[string][]Finding{}
		var keyOrder []string
		var unkeyed []Finding
		for _, f := range lined {
			k := g.GroupKey(f)
			if k == "" {
				unkeyed = append(unkeyed, f)
				continue
			}
			if _, seen := keyed[k]; !seen {
				keyOrder = append(keyOrder, k)
			}
			keyed[k] = append(keyed[k], f)
		}
		// Keyed clusters are unbounded by design: same-key findings cluster
		// regardless of line distance (the Grouper contract), so a large single
		// function can collapse into one big cluster. DedupeCluster is O(n^2) in
		// cluster size (see dedupe.go), but the cost is negligible at practical
		// finding volumes and is accepted here. If a future benchmark ever shows a
		// real hotspot, the remedy is a hard-abort or log+skip on oversized
		// clusters — NOT a proximity fallback, which would silently violate the
		// same-key grouping invariant this function exists to honor.
		for _, k := range keyOrder {
			clusters = append(clusters, keyed[k])
		}
		clusters = append(clusters, proximityClusters(unkeyed)...)
	}
	return clusters
}

// proximityClusters groups already-line-sorted findings by single-linkage on line
// number (a gap greater than lineProximity starts a new cluster). It is the
// shared core of Cluster and the ClusterWith fallback.
func proximityClusters(lined []Finding) [][]Finding {
	var clusters [][]Finding
	var cur []Finding
	prevLine := 0
	for _, f := range lined {
		switch {
		case len(cur) == 0:
			cur = []Finding{f}
		case f.Line-prevLine <= lineProximity:
			cur = append(cur, f)
		default:
			clusters = append(clusters, cur)
			cur = []Finding{f}
		}
		prevLine = f.Line
	}
	if len(cur) > 0 {
		clusters = append(clusters, cur)
	}
	return clusters
}
