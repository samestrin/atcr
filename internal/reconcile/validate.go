package reconcile

import (
	"context"

	"github.com/samestrin/atcr/internal/stream"
)

// validateFindingPaths stamps each reconciled finding's PathValid/PathWarning
// and, when a hallucinated path has a confident correction, PathSuggestion (Epic
// 5.0 AC1 + Epic 5.4). It operates on the JSONFinding records rather than the
// merged findings because the extracted library Merged no longer carries
// path-validation fields (Epic 8.0 Phase 2 Clarification Q1) — path validation is
// ATCR-internal. An empty root disables validation — no base directory is
// configured — so the deterministic reconcile tests that build synthetic findings
// are never coupled to the filesystem. The check runs after merge (on the emitted
// records) rather than in the pure Reconcile pass, keeping Reconcile I/O-free.
//
// The candidate file index is built ONCE here (Epic 5.4 AC1) from `git ls-files`
// and shared across every finding, never rebuilt per-finding. A nil index
// (root is not a git repo, or git is unavailable) degrades to existence-only
// validation with no suggestion.
func validateFindingPaths(ctx context.Context, findings []JSONFinding, root string) {
	if root == "" {
		return
	}
	if len(findings) == 0 {
		return
	}
	idx := stream.BuildFileIndex(ctx, root)
	for i := range findings {
		// ValidatePath reads File and writes PathValid/PathWarning/PathSuggestion on
		// a stream.Finding. Bridge through a scratch finding so the stamping stays in
		// the ATCR stream type while the result rides on the JSONFinding record.
		sf := stream.Finding{File: findings[i].File, Line: findings[i].Line}
		stream.ValidatePath(&sf, root, idx)
		findings[i].PathValid = sf.PathValid
		findings[i].PathWarning = sf.PathWarning
		findings[i].PathSuggestion = sf.PathSuggestion
	}
}
