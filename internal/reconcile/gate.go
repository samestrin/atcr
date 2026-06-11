package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sourcesSubdir / reconciledSubdir are the review-dir children the reconcile
// pipeline reads from and writes to.
const (
	sourcesSubdir    = "sources"
	reconciledSubdir = "reconciled"
)

// ParseSeverity normalizes a severity threshold (case-insensitive) to its
// canonical uppercase form, erroring on an unknown value. It performs no I/O and
// is meant to be called before any file access so an invalid --fail-on value
// fails fast (exit 2). The message is AC 03-01 Edge Case 1 verbatim.
func ParseSeverity(s string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case SevCritical:
		return SevCritical, nil
	case SevHigh:
		return SevHigh, nil
	case SevMedium:
		return SevMedium, nil
	case SevLow:
		return SevLow, nil
	default:
		return "", fmt.Errorf("invalid severity threshold: %q (must be CRITICAL, HIGH, MEDIUM, or LOW)", s)
	}
}

// AtOrAbove reports whether severity sits at or above threshold in the
// CRITICAL > HIGH > MEDIUM > LOW ordering. It is the per-finding predicate the
// count/filter helpers share. An unknown threshold ranks 0; rather than letting
// every finding pass an unrecognized gate (a fail-all footgun for an
// unvalidated caller), an unknown threshold returns false.
func AtOrAbove(severity, threshold string) bool {
	tr, ok := severityRank[threshold]
	if !ok {
		return false
	}
	return severityRank[severity] >= tr
}

// CountAtOrAbove returns how many findings have severity at or above threshold.
// threshold must be a canonical severity (validated via ParseSeverity). The
// ordering is CRITICAL > HIGH > MEDIUM > LOW, so --fail-on HIGH counts HIGH and
// CRITICAL. This is the pure helper the centralized exit-code logic uses.
func CountAtOrAbove(findings []Merged, threshold string) int {
	n := 0
	for _, f := range findings {
		if AtOrAbove(f.Severity, threshold) {
			n++
		}
	}
	return n
}

// RunReconcile discovers sources under reviewDir/sources, runs the deterministic
// pipeline, and writes the artifacts to reviewDir/reconciled, returning the
// Result. allow restricts which immediate source children are read (empty = open
// discovery). It is the single engine entry the CLI and MCP both call. ctx is
// checked between the Discover, Reconcile, and Emit stages so a client cancel or
// server shutdown aborts the pipeline without emitting partial artifacts.
//
// Adjudication re-invocation: if reviewDir/reconciled/adjudication.json exists
// (written by the Skill), its decisions are validated against the prior
// ambiguous.json, the merge decisions are applied, and the pre-adjudication
// ambiguous.json is preserved as ambiguous.original.json before re-emit (AC
// 05-04). An unknown cluster id or a malformed decisions file is a hard error.
func RunReconcile(ctx context.Context, reviewDir string, allow []string, opts Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sources, err := Discover(filepath.Join(reviewDir, sourcesSubdir), allow)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	reconDir := filepath.Join(reviewDir, reconciledSubdir)
	adjudicating := false
	if _, statErr := os.Stat(filepath.Join(reconDir, AdjudicationJSON)); statErr == nil {
		adj, err := LoadAdjudication(filepath.Join(reconDir, AdjudicationJSON))
		if err != nil {
			return Result{}, err
		}
		// Validate decision ids against the baseline the Skill authored against:
		// the preserved original sidecar once a prior adjudication ran, else the
		// current ambiguous.json. Validating against the live (post-merge) sidecar
		// would make re-invocation non-idempotent — after a merge shrinks the gray
		// set, the still-present decision id would be wrongly rejected as unknown.
		baseline := filepath.Join(reconDir, OriginalAmbiguousJSON)
		if _, err := os.Stat(baseline); err != nil {
			baseline = filepath.Join(reconDir, AmbiguousJSON)
		}
		baseData, err := os.ReadFile(baseline)
		if err != nil {
			return Result{}, fmt.Errorf("reading ambiguous baseline for adjudication: %w", err)
		}
		known, err := AmbiguousIDsFromBytes(baseData)
		if err != nil {
			return Result{}, fmt.Errorf("reading ambiguous baseline for adjudication: %w", err)
		}
		if len(known) == 0 {
			return Result{}, fmt.Errorf("no clusters to adjudicate: %s has no ambiguous clusters", baseline)
		}
		// TD-024: refuse a decisions file authored against a different
		// ambiguous.json generation — content-addressed ids may still match a
		// stale prior-session file and would re-merge silently.
		if got := HashBytes(baseData); got != adj.BaselineHash {
			return Result{}, fmt.Errorf("adjudication baseline mismatch: decisions were authored against a different ambiguous.json generation (decisions carry %s, baseline %s is %s)", adj.BaselineHash, baseline, got)
		}
		if err := ValidateDecisions(adj, known); err != nil {
			return Result{}, err
		}
		opts.Merges = adj.MergeSet()
		adjudicating = true
	}

	res := Reconcile(sources, opts)

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	// Preserve the pre-adjudication sidecar before Emit overwrites ambiguous.json,
	// so the audit chain (original gray-zone clusters) survives the re-invocation.
	if adjudicating {
		if err := preserveOriginalAmbiguous(reconDir); err != nil {
			return Result{}, err
		}
	}
	if err := Emit(reconDir, res); err != nil {
		return Result{}, err
	}
	return res, nil
}
