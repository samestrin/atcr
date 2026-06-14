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
//
// Verification verdicts (Epic 3.0) refine the count via Verification.Verdict,
// read directly off each finding:
//   - A refuted finding is never counted, at any severity — a skeptic disproved
//     it, so it must not block CI (it is retained in the artifacts, just demoted).
//   - When requireVerified is true, only a confirmed finding counts — the
//     strictest gate (--fail-on <sev> --require-verified). A v1 finding (nil
//     Verification), an unverifiable one, and an empty-verdict one are all NOT
//     VERIFIED and therefore excluded under requireVerified, but DO count under
//     the default (requireVerified=false) since they are not refuted.
//
// Findings annotated out-of-scope never count (AC 06-04) and that exclusion takes
// precedence over any verdict: a pre-existing CRITICAL the change never touched
// must not fail CI even if a skeptic confirmed it; the annotation and the
// summary.json out_of_scope count are the audit trail.
func CountAtOrAbove(findings []Merged, threshold string, requireVerified bool) int {
	n := 0
	for _, f := range findings {
		if IsFailing(f.Severity, f.Category, f.Verification, threshold, requireVerified) {
			n++
		}
	}
	return n
}

// IsFailing is the single per-finding gate predicate the count helpers and the
// MCP failing-list builder all share, so the CLI, the MCP handlers, and the
// verify stage can never diverge on what counts (AC 04-03/05-02). A finding fails
// the gate when it is in scope, not refuted, at or above threshold, and — under
// requireVerified — confirmed. category is the finding's category, v its optional
// verification block (nil for a v1 finding).
// All three input axes are normalized (lower/upper + trim) before comparison so a
// non-canonical casing/whitespace in a hand-edited or externally-produced
// findings.json — which the JSON-gate path reads without validation — cannot flip
// a gate decision: smuggle a refuted finding past the exclusion, mask an
// out-of-scope finding into the count, drop a confirmed one from the strict count,
// or un-gate a finding via a lower-cased severity.
func IsFailing(severity, category string, v *Verification, threshold string, requireVerified bool) bool {
	if strings.ToLower(strings.TrimSpace(category)) == CategoryOutOfScope {
		return false // out-of-scope never counts, and this takes precedence over any verdict
	}
	verdict := ""
	if v != nil {
		verdict = strings.ToLower(strings.TrimSpace(v.Verdict))
	}
	if verdict == VerdictRefuted {
		return false // a skeptic disproved it: retained but never blocks CI
	}
	if !AtOrAbove(strings.ToUpper(strings.TrimSpace(severity)), threshold) {
		return false
	}
	if requireVerified && verdict != VerdictConfirmed {
		return false // strictest gate: only a confirmed (VERIFIED) finding counts
	}
	return true
}

// CountFailingJSON mirrors CountAtOrAbove over the re-readable JSONFinding records
// the verify stage gates on its own re-emitted findings.json. It applies the same
// IsFailing predicate, so a verify-stage gate decision is identical to the CLI
// reconcile gate for the same findings (AC 04-03 Scenario 3).
func CountFailingJSON(findings []JSONFinding, threshold string, requireVerified bool) int {
	n := 0
	for _, f := range findings {
		if IsFailing(f.Severity, f.Category, f.Verification, threshold, requireVerified) {
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
