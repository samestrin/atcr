package debate

import (
	"strconv"

	"github.com/samestrin/atcr/internal/reconcile"
)

// locationKey is the file+line identity used to correlate a gray-zone cluster's
// members to their findings.json records. It is intentionally coarser than
// FindingKey: a member may have been further-merged with a third finding (≥0.7
// similarity), replacing its problem text via longestField, so the full triple
// would not match between the cluster's raw member and the reconciled record.
func locationKey(file string, line int) string {
	return file + "\x00" + strconv.Itoa(line)
}

// clusterDisplayProblem returns the longest PROBLEM among a cluster's members —
// the same representative BuildDisagreements writes as the gray-zone radar item's
// Problem, so a debated DisagreementItem can be correlated back to its cluster.
func clusterDisplayProblem(c reconcile.AmbiguousCluster) string {
	best := ""
	for _, f := range c.Findings {
		if len(f.Problem) > len(best) {
			best = f.Problem
		}
	}
	return best
}

// indexClusters maps each gray-zone cluster by the identity its radar item
// carries — File + Line + longest-member-problem — so a debated gray-zone
// DisagreementItem (it.File, it.Line, it.Problem) resolves to the AmbiguousCluster
// whose members must be unioned.
func indexClusters(clusters []reconcile.AmbiguousCluster) map[FindingKey]reconcile.AmbiguousCluster {
	out := make(map[FindingKey]reconcile.AmbiguousCluster, len(clusters))
	for _, c := range clusters {
		out[FindingKey{File: c.File, Line: c.Line, Problem: clusterDisplayProblem(c)}] = c
	}
	return out
}

// applyClusterMerges unions the member findings of each gray-zone cluster the
// judge ruled "merge" (Epic 6.1, Option A) directly in the findings slice and
// returns the rewritten slice. For each cluster it gathers the records at the
// cluster's member locations — matching a member by its exact File+Line+Problem,
// or, when that member's problem drifted (further-merged with a third finding),
// by location alone IF that location holds exactly one record (so an unrelated
// co-located finding is never absorbed). The matched records are collapsed via
// reconcile.MergeJSONFindings into one record placed at the cluster's canonical
// location and flagged ClusterMerged. A cluster with fewer than two matched
// records, or any matched record already flagged ClusterMerged (a re-run that
// slipped past the radar filter), is left untouched — the merge is a strict no-op
// rather than a corruption.
func applyClusterMerges(findings []reconcile.JSONFinding, clusters []reconcile.AmbiguousCluster) []reconcile.JSONFinding {
	for _, c := range clusters {
		findings = applyOneClusterMerge(findings, c)
	}
	return findings
}

func applyOneClusterMerge(findings []reconcile.JSONFinding, c reconcile.AmbiguousCluster) []reconcile.JSONFinding {
	memberExact := map[FindingKey]bool{}
	memberLocs := map[string]bool{}
	for _, mf := range c.Findings {
		memberExact[FindingKey{File: mf.File, Line: mf.Line, Problem: mf.Problem}] = true
		memberLocs[locationKey(mf.File, mf.Line)] = true
	}

	countAtLoc := map[string]int{}
	for _, f := range findings {
		countAtLoc[locationKey(f.File, f.Line)]++
	}

	matched := map[int]bool{}
	exactHits := 0
	for i, f := range findings {
		lk := locationKey(f.File, f.Line)
		if !memberLocs[lk] {
			continue
		}
		switch {
		case memberExact[FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}]:
			matched[i] = true
			exactHits++
		case countAtLoc[lk] == 1:
			// Drift: the sole record at a member location is that member, now
			// carrying a further-merged problem text — safe to union.
			matched[i] = true
		}
	}

	// Require at least one member to match by exact File+Line+Problem before
	// unioning. Without this, a cluster whose members were all refuted/removed
	// post-reconcile could union a group composed purely of fallback-matched
	// records that merely happen to be the sole findings at those lines — absorbing
	// unrelated findings. One exact hit proves the cluster genuinely maps here.
	if exactHits == 0 || len(matched) < 2 {
		return findings // members not both present (or not anchored): nothing to union
	}
	group := make([]reconcile.JSONFinding, 0, len(matched))
	firstIdx := -1
	for i := range findings {
		if matched[i] {
			if findings[i].ClusterMerged {
				return findings // already applied: strict no-op
			}
			if firstIdx < 0 {
				firstIdx = i
			}
			group = append(group, findings[i])
		}
	}

	merged := reconcile.MergeJSONFindings(group)
	merged.File, merged.Line = c.File, c.Line
	merged.ClusterMerged = true

	out := make([]reconcile.JSONFinding, 0, len(findings)-len(group)+1)
	for i := range findings {
		switch {
		case i == firstIdx:
			out = append(out, merged)
		case matched[i]:
			// dropped — folded into merged
		default:
			out = append(out, findings[i])
		}
	}
	return out
}
