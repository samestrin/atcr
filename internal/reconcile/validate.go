package reconcile

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/samestrin/atcr/internal/stream"
	reclib "github.com/samestrin/atcr/reconcile"
)

// tier4Resolver is the Tier 4 lookup capability validateFindingPaths consumes
// (Epic 35.16.6.5 T3): given a finding's extracted anchors, report whether the
// construct it describes lives in exactly one tracked file, in several, or
// nowhere at all. *lazySymbolIndex is the production implementation.
type tier4Resolver interface {
	resolve(ctx context.Context, primary, secondary []string) (string, tier4Outcome)
	// state reports what the index build achieved, for Summary.UnresolvedState.
	// It is only meaningful after resolve has forced the build, and asking must
	// never trigger one — the AC5 laziness is part of this contract.
	state() string
}

// newTier4Index constructs the per-run Tier 4 index. It is a package var so
// tests can substitute a scripted resolver and observe whether an index was
// constructed at all — the observable proxy for AC5's "a run with zero
// Tier-4-eligible findings never instantiates the wazero AST runtime".
//
// Because tests swap and restore this var, no test in this package may call
// t.Parallel — a parallel test would race on the swap (see the package doc).
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
// It also returns the Tier 4 state for Summary.UnresolvedState. An EMPTY state
// means "not recorded" — no root, or no findings — and is the only case the
// caller leaves unstamped.
func validateFindingPaths(ctx context.Context, findings []JSONFinding, root string) ([]int, string) {
	if root == "" {
		return nil, ""
	}
	if len(findings) == 0 {
		return nil, ""
	}
	idx := stream.BuildFileIndex(ctx, root)

	// Tier 4 is available only with a tracked file set to search and the AST
	// opt-out unset. The index itself is constructed lazily below, on the first
	// finding that actually needs it, so an all-valid run pays nothing (AC5).
	tier4Available := idx != nil && !astGroupingDisabled()
	var tier4 tier4Resolver

	// A run where Tier 4 is available but no finding ever needs it is APPLIED,
	// not unavailable: the check was in force and simply had nothing to
	// adjudicate, which is the healthy case a bare 0 count cannot express. The
	// index is never constructed on that path (AC5), so the state cannot come
	// from it — it is seeded here and overwritten below only if a build happened.
	tier4State := reclib.UnresolvedStateDisabled
	if tier4Available {
		tier4State = reclib.UnresolvedStateApplied
	}

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
		if !tier4Eligible(findings[i].File, idx) {
			continue
		}
		if tier4 == nil {
			tier4 = newTier4Index(root, idx.Paths())
		}
		problemAnchors, problemTruncated := extractAnchorSet(findings[i].Problem)
		fixAnchors, fixTruncated := extractAnchorSet(findings[i].Fix)
		if fixTruncated {
			// The FIX named more constructs than the anchor cap admits, so the
			// set that would be searched is a PREFIX of what it actually named —
			// the same partial-search condition the no-match direction below
			// refuses to accept. A partial search cannot ground a suggestion
			// either, so a truncated FIX contributes no secondary anchors.
			fixAnchors = nil
		}
		suggestion, outcome := tier4.resolve(ctx, problemAnchors, fixAnchors)
		switch {
		case outcome == tier4Resolved:
			findings[i].PathSuggestion = suggestion
		case outcome == tier4NoMatch && !problemTruncated:
			unresolved = append(unresolved, i)
		case outcome == tier4NoMatch:
			// The finding named more constructs than the anchor cap admits, so the
			// set searched was a PREFIX of what it actually named — and the one
			// anchor that would have matched may be among the dropped ones. A
			// partial search cannot produce a "found nothing" verdict.
		default:
			// tier4Inconclusive: the PROBLEM named no identifier, the index could
			// not be built or was incomplete, or the anchors matched real code
			// without localizing to one file (AC7). No suggestion, and emphatically
			// NOT sidecar-routed — "could not check" is not "checked and found
			// nothing".
		}
	}
	if tier4 != nil {
		// An index WAS built, so its own verdict on the build supersedes the seed.
		tier4State = tier4.state()
	}
	return unresolved, tier4State
}

// tier4Eligible reports whether a finding with an unresolved path may be judged
// by a content search at all. Both gates below decide whether a REAL finding can
// be routed out of the primary report, so both fail toward keeping it.
//
//   - No parser language for the cited file. A symbol index built from source
//     trees cannot adjudicate a citation of docs/x.md or config/app.yaml: the
//     construct such a finding names may live in prose or in configuration that
//     no parser reads. This mirrors the extension short-circuit lazyGrouper's
//     GroupKey/EnclosingSymbol already perform before touching the runtime.
//
//   - An AMBIGUOUS case-only mismatch. stream.CaseCorrection reports
//     mismatch=true with an EMPTY suggestion when several tracked files differ
//     from the citation only by case, which leaves PathWarning set and
//     PathSuggestion empty — indistinguishable, at this layer, from a path that
//     resolves to nothing. But the file demonstrably exists in the tracked tree;
//     the citation just spelled its case wrong, which is a Tier 3 concern and
//     never grounds for calling the finding fabricated.
func tier4Eligible(file string, idx *stream.FileIndex) bool {
	if astgroup.LanguageForExt(strings.ToLower(filepath.Ext(file))) == "" {
		return false
	}
	return len(idx.ByFold(file)) == 0
}
