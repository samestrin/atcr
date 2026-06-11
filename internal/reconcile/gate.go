package reconcile

import (
	"fmt"
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

// CountAtOrAbove returns how many findings have severity at or above threshold.
// threshold must be a canonical severity (validated via ParseSeverity). The
// ordering is CRITICAL > HIGH > MEDIUM > LOW, so --fail-on HIGH counts HIGH and
// CRITICAL. This is the pure helper the centralized exit-code logic uses.
func CountAtOrAbove(findings []Merged, threshold string) int {
	t := severityRank[threshold]
	n := 0
	for _, f := range findings {
		if severityRank[f.Severity] >= t {
			n++
		}
	}
	return n
}

// RunReconcile discovers sources under reviewDir/sources, runs the deterministic
// pipeline, and writes the artifacts to reviewDir/reconciled, returning the
// Result. allow restricts which immediate source children are read (empty = open
// discovery). It is the single engine entry the CLI and MCP both call.
func RunReconcile(reviewDir string, allow []string, opts Options) (Result, error) {
	sources, err := Discover(filepath.Join(reviewDir, sourcesSubdir), allow)
	if err != nil {
		return Result{}, err
	}
	res := Reconcile(sources, opts)
	if err := Emit(filepath.Join(reviewDir, reconciledSubdir), res); err != nil {
		return Result{}, err
	}
	return res, nil
}
