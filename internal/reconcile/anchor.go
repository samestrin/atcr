package reconcile

// maxAnchorsPerFinding bounds how many anchors one finding contributes to a
// Tier 4 lookup. STUB value — see GREEN.
const maxAnchorsPerFinding = 8

// extractAnchors is the Tier 4 (Epic 35.16.6.5 T1) deterministic anchor
// extractor: given a finding's PROBLEM and FIX prose it returns the
// identifier-shaped tokens the finding appears to be talking about, which the
// repo-wide symbol index is then searched for.
//
// STUB — replaced by the real implementation in GREEN.
func extractAnchors(problem, fix string) []string {
	return nil
}
