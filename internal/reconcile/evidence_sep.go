package reconcile

import (
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"
)

// ClusterEvidenceSep is the delimiter joinEvidence uses to concatenate the
// EVIDENCE of findings collapsed into one record. The published reconcile module
// joins with the same " / " literal (mergeEvidence in reconcile/merge.go), and
// TestClusterEvidenceSep_MatchesThePublishedModulesJoinLiteral reads that source
// to keep the two equal. The constant sits here rather than beside
// reclib.EvidenceSep because that module is versioned separately and was held
// out of epic 54's scope. Every reader that splits Evidence into segments
// (internal/verify's fix-attribution guards, ghaction.FixAttribution) must split
// on both, through SplitEvidence, or a cluster-merged "Found by kai / fix by
// opus" reads as one segment and its attribution goes unseen.
const ClusterEvidenceSep = " / "

// SplitEvidence splits an Evidence field into segments on every separator it is
// assembled with: reclib.EvidenceSep (pipeline and fix-attribution joins) and
// ClusterEvidenceSep (cluster merges). Both are multi-character on purpose; a bare
// "/" is not a separator, so "path/to/fix by hand" stays one segment. Segments are
// returned in order and untrimmed.
func SplitEvidence(evidence string) []string {
	var out []string
	for _, seg := range strings.Split(evidence, ClusterEvidenceSep) {
		out = append(out, strings.Split(seg, reclib.EvidenceSep)...)
	}
	return out
}
