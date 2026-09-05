package reconcile

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/stream"
	reclib "github.com/samestrin/atcr/reconcile"
)

// tier4Resolver is the Tier 4 lookup capability validateFindingPaths consumes
// (Epic 35.16.6.5 T3): given a finding's extracted anchors, report whether the
// construct it describes lives in exactly one tracked file, in several, or
// nowhere at all. *lazySymbolIndex is the production implementation.
type tier4Resolver interface {
	// droppedSecondary carries the FIX anchors scanFixAnchors narrowed out of
	// secondary. They may not source a resolution — the glued reading of them
	// may not be what the reviewer wrote — but a dropped name declared in a
	// file other than the located one is the disagreement locate refuses on, so
	// narrowing may not silently complete a set it left incomplete.
	resolveWithDropped(ctx context.Context, primary, secondary, droppedSecondary []string) (string, tier4Outcome)
	// namedInDocs reports whether the doc-extension heuristic explains a no-match
	// over these anchors: at least one was named in a documentation file and
	// nowhere in source, and EVERY other anchor is accounted for somewhere in the
	// tree. One anchor absent from both sets was invented, and denies the answer
	// for the whole set — the shield waives a scorecard charge, so it may not be
	// bought with a single surviving changelog mention. It explains a no-match
	// after the fact — it never influences the verdict — and, like state, must not
	// trigger a build.
	namedInDocs(anchors []string) bool
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
		problemAnchors, problemScan := scanProblemAnchors(findings[i].Problem)
		problemTruncated := problemScan.truncated()
		// The FIX narrows rather than nils: extractFixAnchors drops the anchors
		// a call-scan fidelity loss actually touched, and abandons the set whole
		// for the cap OR for a fidelity loss that left no member behind (its
		// `capped` and `unaccounted` disjuncts) — in both cases what was dropped
		// is unknowable. See its doc for the full argument. Each loss increments
		// its own counter below, so a suggestion that never landed is
		// attributable after the fact (docs/metrics.md).
		fixAnchors, fixScan := scanFixAnchors(findings[i].Fix)
		switch {
		case fixScan.capped:
			metrics.Counter(tier4FixSetCappedMetric).Inc()
		case fixScan.unaccounted:
			metrics.Counter(tier4FixSetUnaccountedMetric).Inc()
		default:
			if dropped := len(fixScan.anchors) - len(fixAnchors); dropped > 0 {
				metrics.Counter(tier4FixAnchorDroppedMetric).Add(int64(dropped))
			}
		}
		// The narrowed-out members ride along as VETO evidence: they may not
		// source a suggestion, but a dropped name declared in another file is
		// the disagreement locate refuses on, so narrowing must not silently
		// complete a set it left incomplete (symbolIndex.resolve).
		suggestion, outcome := tier4.resolveWithDropped(ctx, problemAnchors, fixAnchors, fixScan.droppedFixAnchors())
		switch {
		case outcome == tier4Resolved && problemScan.unaccounted:
			// The PROBLEM set lost a member with no name to point at, and
			// locate() refuses when two precise anchors DISAGREE — so its
			// verdict rests on the set being COMPLETE, not merely faithful, and
			// the silenced span is exactly the member whose answer is unknown.
			// Resolving on the survivors alone converts that refusal into a
			// confident wrong answer: measured, `parse._解析() and `readTree``
			// points at readTree's file for a finding whose subject is declared
			// in _解析's.
			//
			// This is the completeness argument extractFixAnchors already makes
			// for the FIX side, applied to the other set feeding the same call.
			// Downgrading to tier4Inconclusive costs a suggestion and can never
			// route a finding out — no-match is a different arm — so it is the
			// same safe direction the FIX side takes.
			//
			// Scoped to `unaccounted`, NOT to `capped`, and the asymmetry with
			// the FIX side (which abandons on both) is deliberate. The cap
			// leaves the same kind of hole — a dropped member could have
			// disagreed — but it only fires on a PROBLEM naming more than
			// maxAnchorsPerFinding identifiers, so refusing there would cost the
			// suggestion on every densely-cited finding to close a shape nobody
			// has measured. Disclosed rather than folded in.
		case outcome == tier4Resolved:
			findings[i].PathSuggestion = suggestion
		case outcome == tier4NoMatch && !problemTruncated:
			unresolved = append(unresolved, i)
			if tier4.namedInDocs(problemAnchors) {
				// The subject IS named in the tree, just only in a file isDocExt
				// classified as prose. The routing stands — a construct is declared
				// in source, never in prose — but it rests on an extension
				// heuristic, so the scorecard must not durably charge it. Every
				// other consumer of a routed record can recover from a misfire by
				// reading unresolved.json; the scorecard cannot.
				findings[i].UnresolvedReason = UnresolvedReasonDocShield
			}
		case outcome == tier4NoMatch:
			// problemTruncated: the set searched is not a faithful reading of what
			// the PROBLEM named — see extractAnchorSet's doc for the losses the
			// flag covers, of which the anchor cap is only one. Whichever loss it
			// was, the one anchor that would have matched may be among what was
			// not faithfully recovered, and a partial search cannot produce a
			// "found nothing" verdict.
			//
			// Recorded trade: even an UNtruncated set is only a reading of the
			// tokens that carry an identifier signal. A subject token that
			// carries none (`解析` once its qualifier is stripped) is invisible
			// to this gate — it is never searched for at all — so a no-match
			// verdict can rest entirely on a co-cited anchor. That is the cost
			// hasIdentifierSignal's doc states for rejecting signal-less prose,
			// paid here.
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
