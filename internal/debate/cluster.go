package debate

import (
	"context"
	"strconv"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
)

// collisionSentinelID is stored in indexClusters when two distinct clusters share
// the same File+Line+display-problem key. It prevents a gray-zone item from
// resolving to an arbitrary cluster ID in that collision case.
const collisionSentinelID = "atcr:collision"

// locationKey is the file+line identity used to correlate a gray-zone cluster's
// members to their findings.json records. It is intentionally coarser than
// FindingKey: a member may have been further-merged with a third finding (≥0.7
// similarity), replacing its problem text via longestField, so the full triple
// would not match between the cluster's raw member and the reconciled record.
func locationKey(file string, line int) string {
	return file + "\x00" + strconv.Itoa(line)
}

// indexClusters maps each gray-zone cluster by the identity its radar item
// carries — File + Line + longest-member-problem — so a debated gray-zone
// DisagreementItem (it.File, it.Line, it.Problem) resolves to the AmbiguousCluster
// whose members must be unioned.
func indexClusters(clusters []reconcile.AmbiguousCluster) map[FindingKey]reconcile.AmbiguousCluster {
	out := make(map[FindingKey]reconcile.AmbiguousCluster, len(clusters))
	for _, c := range clusters {
		key := FindingKey{File: c.File, Line: c.Line, Problem: reconcile.ClusterDisplayProblem(c.Findings)}
		if _, exists := out[key]; exists {
			// Two distinct clusters share the same display key. Trust neither ID for
			// identity-keyed suppression or merge application; let both items pass
			// through and re-debate (an idempotent no-op for the merged one).
			out[key] = reconcile.AmbiguousCluster{ID: collisionSentinelID}
			continue
		}
		out[key] = c
	}
	return out
}

// filterMergedClusters drops gray-zone radar items whose cluster a prior debate
// already merged inline (Epic 6.1 AC4), so a re-run never re-debates or re-merges
// an already-applied cluster. A cluster is "already applied" when a findings.json
// record flagged ClusterMerged carries that cluster's stable ClusterID (Epic 6.2):
// idempotency is keyed on cluster identity, so a gray-zone item is suppressed only
// when its OWN cluster was merged — a second DISTINCT cluster co-located at the same
// canonical File+Line is no longer over-suppressed. Each item resolves to its
// cluster (and thus its ID) via clusterIdx, the same {File,Line,Problem} index
// runDebate uses to apply rulings. Non-gray-zone items pass through untouched.
// Returns items unchanged when no record is flagged, so a first-ever debate run does
// no extra work.
//
// Only non-empty ClusterIDs are matched. A legacy ClusterMerged survivor written by
// a pre-6.2 debate carries no ClusterID, so it suppresses nothing here — the cluster
// is re-debated once (an idempotent no-op via applyOneClusterMerge's already-merged
// guard) and self-heals as soon as it is re-stamped. An item whose cluster is not in
// clusterIdx (e.g. its representative problem drifted) likewise passes through and is
// re-debated rather than silently dropped.
func filterMergedClusters(ctx context.Context, items []reconcile.DisagreementItem, findings []reconcile.JSONFinding, clusterIdx map[FindingKey]reconcile.AmbiguousCluster) []reconcile.DisagreementItem {
	mergedIDs := map[string]bool{}
	mergedAtLoc := map[string]bool{}
	for _, f := range findings {
		if f.ClusterMerged && f.ClusterID != "" {
			mergedIDs[f.ClusterID] = true
			mergedAtLoc[locationKey(f.File, f.Line)] = true
		}
	}
	if len(mergedIDs) == 0 {
		return items
	}
	// The typical re-run suppresses 0-1 of N items, so out is allocated lazily on
	// the first suppression and back-filled with the already-kept prefix. When no
	// item is suppressed the original slice is returned without an allocation or
	// copy.
	var out []reconcile.DisagreementItem
	suppressed := 0
	for i, it := range items {
		suppress := false
		if it.Kind == reconcile.KindGrayZone {
			key := FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}
			c, ok := clusterIdx[key]
			switch {
			case ok && c.ID != "" && mergedIDs[c.ID]:
				// The c.ID != "" guard makes the consumer self-defending: mergedIDs is
				// built only from non-empty ClusterIDs (line 65), so a future producer
				// that admitted a blank key upstream can never make every empty-ID
				// cluster match mergedIDs[""] at once.
				suppress = true
			case !ok && clusterIdx != nil && mergedAtLoc[locationKey(it.File, it.Line)]:
				// Fallback for the drift case: the representative problem changed so the
				// item no longer matches clusterIdx, but a merged survivor with a real
				// ClusterID sits at the same location. Conservatively suppress rather than
				// re-debate a cluster that has already been applied.
				suppress = true
			}
		}
		if suppress {
			if out == nil {
				out = make([]reconcile.DisagreementItem, 0, len(items))
				out = append(out, items[:i]...)
			}
			suppressed++
			continue
		}
		if out != nil {
			out = append(out, it)
		}
	}
	if out == nil {
		return items
	}
	// Surface idempotency suppression so a wrong-ID match or unexpected pass-through
	// is observable in prod. Emitted only when something was actually suppressed
	// (out is non-nil iff suppressed > 0), keeping the no-op re-run path silent.
	log.FromContext(ctx).Debug("debate: suppressed already-merged gray-zone items",
		"suppressed", suppressed, "items", len(items))
	return out
}

// applyClusterMerges unions the member findings of each gray-zone cluster the
// judge ruled "merge" (Epic 6.1, Option A) directly in the findings slice. It
// returns the rewritten slice, the count of clusters actually applied, and the
// count of structurally unmergeable clusters (fewer than two members). The
// caller uses the skipped count to keep the "could not be applied" warning
// truthful: a one-member cluster is a no-op by definition, not a failed ruling.
// Each multi-member cluster is handled independently by applyOneClusterMerge.
func applyClusterMerges(findings []reconcile.JSONFinding, clusters []reconcile.AmbiguousCluster) ([]reconcile.JSONFinding, int, int) {
	applied := 0
	skipped := 0
	for _, c := range clusters {
		if len(c.Findings) < 2 {
			skipped++
			continue
		}
		var ok bool
		findings, ok = applyOneClusterMerge(findings, c)
		if ok {
			applied++
		}
	}
	return findings, applied, skipped
}

// firstClusterRulingCollision returns the first "File:Line" where a gray-zone
// cluster member coincides with a single-finding rulings key, or "" when the
// Epic 6.1 invariant holds: gray-zone items are classified into the cluster branch
// in runDebate and never enter the rulings map, so their member locations are
// disjoint from the rulings keyspace (the radar excludes gray members from the
// solo/split tiers today). It is a defensive tripwire — a future radar/selection
// change that let a gray member also surface as a solo/split tier item would
// otherwise let applyRulings and applyClusterMerges both mutate the same finding
// silently. Keyed on locationKey (File+Line), matching how the two apply paths
// would actually collide.
func firstClusterRulingCollision(rulings map[FindingKey]ruleApply, clusters []reconcile.AmbiguousCluster) string {
	if len(rulings) == 0 {
		return ""
	}
	ruledLocs := make(map[string]bool, len(rulings))
	for k := range rulings {
		ruledLocs[locationKey(k.File, k.Line)] = true
	}
	for _, c := range clusters {
		for _, mf := range c.Findings {
			if ruledLocs[locationKey(mf.File, mf.Line)] {
				return mf.File + ":" + strconv.Itoa(mf.Line)
			}
		}
	}
	return ""
}

// applyOneClusterMerge unions one cluster's members in findings and reports
// whether the merge was applied. Members are matched to findings.json records in
// two passes: first by exact File+Line+Problem, then — for members whose problem
// drifted (further-merged with a third finding) — by taking the sole still-
// unmatched record at that member's location. Per-location drift recovery handles
// the same-line case (two co-located members where one drifted): the drifted
// member is the lone unmatched record left at that location after the exact pass.
// More than one unmatched record at a member location is ambiguous, so none is
// taken — an unrelated co-located finding is never absorbed. At least one exact
// hit is required to anchor the cluster (so a cluster whose members were all
// refuted/removed cannot union purely fallback-matched, possibly unrelated,
// records). The matched records collapse via reconcile.MergeJSONFindings into one
// record at the cluster's canonical location, flagged ClusterMerged. Fewer than
// two matched records, or any matched record already flagged ClusterMerged (a
// re-run past the radar filter), is a strict no-op rather than a corruption.
func applyOneClusterMerge(findings []reconcile.JSONFinding, c reconcile.AmbiguousCluster) ([]reconcile.JSONFinding, bool) {
	// Invariant: a gray-zone cluster always carries a stable, content-addressed
	// AmbiguousCluster.ID (a non-empty sha256 hex) by construction. A blank ID can
	// only come from a hand-edited or corrupt ambiguous.json. Refuse to stamp a
	// ClusterMerged survivor with an empty ClusterID: filterMergedClusters would
	// treat it as a legacy record and never suppress it, so the cluster would be
	// re-debated every run with no path to self-heal (cluster.go:55-60). A strict
	// no-op is safer than writing a poisoned survivor.
	if c.ID == "" {
		return findings, false
	}
	memberExact := map[FindingKey]bool{}
	memberLocs := map[string]bool{}
	for _, mf := range c.Findings {
		memberExact[FindingKey{File: mf.File, Line: mf.Line, Problem: mf.Problem}] = true
		memberLocs[locationKey(mf.File, mf.Line)] = true
	}

	matched := map[int]bool{}
	exactHits := 0
	exactMatchedLocs := map[string]bool{}
	// Pass 1: exact member matches.
	for i, f := range findings {
		if memberExact[FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}] {
			matched[i] = true
			exactHits++
			exactMatchedLocs[locationKey(f.File, f.Line)] = true
		}
	}
	// Pass 2: drift recovery. Group the still-unmatched records by member location.
	// A member location with exactly one unmatched record contributes it ONLY if
	// another member already matched exactly at that same location (same-line
	// drift). This anchors recovery to the safe co-location case and prevents a
	// sole unrelated record at a different member line from being absorbed.
	// Two or more unmatched records at a location is ambiguous and contributes
	// nothing.
	unmatchedAtLoc := map[string][]int{}
	for i, f := range findings {
		if matched[i] {
			continue
		}
		lk := locationKey(f.File, f.Line)
		if memberLocs[lk] {
			unmatchedAtLoc[lk] = append(unmatchedAtLoc[lk], i)
		}
	}
	for lk, idxs := range unmatchedAtLoc {
		if exactMatchedLocs[lk] && len(idxs) == 1 {
			matched[idxs[0]] = true
		}
	}

	if exactHits == 0 || len(matched) < 2 {
		return findings, false // not anchored, or members not both present
	}
	group := make([]reconcile.JSONFinding, 0, len(matched))
	firstIdx := -1
	for i := range findings {
		if matched[i] {
			if findings[i].ClusterMerged {
				return findings, false // already applied: strict no-op
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
	// Stamp the source cluster's stable, content-addressed ID (Epic 6.2 AC2) so
	// filterMergedClusters can key idempotency on cluster identity, not File+Line.
	merged.ClusterID = c.ID

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
	return out, true
}
