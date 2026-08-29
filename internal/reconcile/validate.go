package reconcile

import (
	"context"

	"github.com/samestrin/atcr/internal/stream"
)

// tier4Resolver is the Tier 4 lookup capability validateFindingPaths consumes
// (Epic 35.16.6.5 T3): given a finding's extracted anchors, report whether the
// construct it describes lives in exactly one tracked file, in several, or
// nowhere at all. *lazySymbolIndex is the production implementation.
type tier4Resolver interface {
	resolve(anchors []string) (string, tier4Outcome)
}

// newTier4Index constructs the per-run Tier 4 index. It is a package var so
// tests can substitute a scripted resolver and observe whether an index was
// constructed at all — the observable proxy for AC5's "a run with zero
// Tier-4-eligible findings never instantiates the wazero AST runtime".
var newTier4Index = func(root string, paths []string) tier4Resolver {
	return newLazySymbolIndex(root, paths)
}

// validateFindingPaths stamps each reconciled finding's PathValid/PathWarning
// and, when a hallucinated path has a confident correction, PathSuggestion (Epic
// 5.0 AC1 + Epic 5.4 + Epic 35.16.6.5). It operates on the JSONFinding records
// rather than the merged findings because the extracted library Merged no longer
// carries path-validation fields (Epic 8.0 Phase 2 Clarification Q1) — path
// validation is ATCR-internal. An empty root disables validation — no base
// directory is configured — so the deterministic reconcile tests that build
// synthetic findings are never coupled to the filesystem. The check runs after
// merge (on the emitted records) rather than in the pure Reconcile pass, keeping
// Reconcile I/O-free.
//
// The candidate file index is built ONCE here (Epic 5.4 AC1) from `git ls-files`
// and shared across every finding, never rebuilt per-finding. A nil index
// (root is not a git repo, or git is unavailable) degrades to existence-only
// validation with no suggestion — and, since there is then no tracked file set
// to have searched, skips Tier 4 entirely.
//
// # Tier 4 (Epic 35.16.6.5)
//
// Tiers 1-3 all match on the FILENAME string, so none of them can resolve a real
// issue attributed to a file with a dissimilar name. For a finding they left
// without a suggestion, Tier 4 searches the tracked tree's actual AST content for
// the construct the finding's prose describes. It runs ONLY on that remainder —
// it never revisits or overwrites a Tier 1-3 answer — and it never rewrites
// finding.File, keeping 5.4's suggest-only contract (AC7 of 5.4) intact.
//
// It returns the INDICES of findings that exhausted all four tiers with zero
// symbol correspondence anywhere in the tracked tree. Those are the sidecar
// candidates (T4); every other finding, including one Tier 4 could not judge,
// stays in the primary stream. The caller decides what to do with them —
// this function never drops a finding itself.
//
// ATCR_DISABLE_AST_GROUPING disables Tier 4 exactly as it disables AST
// clustering (AC6): validation degrades to 5.4's Tier 1-3-only behavior, and the
// returned slice is empty. With no Tier 4 there is no search to have exhausted,
// so there is no evidence any finding is fabricated.
func validateFindingPaths(ctx context.Context, findings []JSONFinding, root string) []int {
	if root == "" {
		return nil
	}
	if len(findings) == 0 {
		return nil
	}
	idx := stream.BuildFileIndex(ctx, root)

	// Tier 4 is available only with a tracked file set to search and the AST
	// opt-out unset. The index itself is constructed lazily below, on the first
	// finding that actually needs it, so an all-valid run pays nothing (AC5).
	tier4Available := idx != nil && !astGroupingDisabled()
	var tier4 tier4Resolver

	var unresolved []int
	for i := range findings {
		// ValidatePath reads File and writes PathValid/PathWarning/PathSuggestion on
		// a stream.Finding. Bridge through a scratch finding so the stamping stays in
		// the ATCR stream type while the result rides on the JSONFinding record.
		sf := stream.Finding{File: findings[i].File, Line: findings[i].Line}
		stream.ValidatePath(&sf, root, idx)
		findings[i].PathValid = sf.PathValid
		findings[i].PathWarning = sf.PathWarning
		findings[i].PathSuggestion = sf.PathSuggestion

		if !tier4Available || sf.PathWarning == "" || sf.PathSuggestion != "" {
			continue // path resolved, or Tiers 1-3 already answered: Tier 4 is out of scope
		}
		if tier4 == nil {
			tier4 = newTier4Index(root, idx.Paths())
		}
		suggestion, outcome := tier4.resolve(extractAnchors(findings[i].Problem, findings[i].Fix))
		switch outcome {
		case tier4Resolved:
			findings[i].PathSuggestion = suggestion
		case tier4NoMatch:
			unresolved = append(unresolved, i)
		default:
			// tier4Inconclusive: the finding named no identifier, the index could
			// not be built, or the anchors matched more than one file (AC7). No
			// suggestion, and emphatically NOT sidecar-routed — "could not check"
			// is not "checked and found nothing".
		}
	}
	return unresolved
}
