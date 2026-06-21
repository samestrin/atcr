package reconcile

import "github.com/samestrin/atcr/internal/stream"

// validateFindingPaths stamps each merged finding's PathValid/PathWarning and,
// when a hallucinated path has a confident correction, PathSuggestion (Epic 5.0
// AC1 + Epic 5.4). An empty root disables validation — no base directory is
// configured — so the deterministic reconcile tests that build synthetic
// findings are never coupled to the filesystem. The check runs after merge (on
// the emitted records) rather than in the pure Reconcile pass, keeping Reconcile
// I/O-free.
//
// The candidate file index is built ONCE here (Epic 5.4 AC1) from `git ls-files`
// and shared across every finding, never rebuilt per-finding. A nil index
// (root is not a git repo, or git is unavailable) degrades to existence-only
// validation with no suggestion.
func validateFindingPaths(findings []Merged, root string) {
	if root == "" {
		return
	}
	idx := stream.BuildFileIndex(root)
	for i := range findings {
		stream.ValidatePath(&findings[i].Finding, root, idx)
	}
}
