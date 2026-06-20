package reconcile

import "github.com/samestrin/atcr/internal/stream"

// validateFindingPaths stamps each merged finding's PathValid/PathWarning by
// checking whether its file exists under root (Epic 5.0 AC1). An empty root
// disables validation — no base directory is configured — so the deterministic
// reconcile tests that build synthetic findings are never coupled to the
// filesystem. The check runs after merge (on the emitted records) rather than in
// the pure Reconcile pass, keeping Reconcile I/O-free.
func validateFindingPaths(findings []Merged, root string) {
	if root == "" {
		return
	}
	for i := range findings {
		stream.ValidatePath(&findings[i].Finding, root)
	}
}
