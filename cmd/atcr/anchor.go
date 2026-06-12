package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
)

// anchorDir resolves the single id-or-path anchor argument to a review
// directory. An empty arg falls back to .atcr/latest. An arg that looks like a
// path (absolute, contains a separator, or ".") is used verbatim — this is
// intentional: the user may point at a review directory anywhere on their own
// machine. Otherwise the arg is treated as a review id and is validated so a
// bare ".." can never resolve one level above .atcr/reviews/.
func anchorDir(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		id, err := fanout.ReadLatest(".")
		if err != nil {
			// Wrap rather than discard: ReadLatest distinguishes a missing file
			// from a corrupt/tampered pointer, and that cause must surface.
			return "", fmt.Errorf("no review specified and no usable .atcr/latest pointer (run 'atcr review' first): %w", err)
		}
		return filepath.Join(fanout.ReviewsRoot("."), id), nil
	}
	// Both '/' and '\\' mark an explicit path regardless of platform, so the
	// path-vs-id contract is uniform (a forward-slash path on Windows must not
	// fall into bare-id validation). ValidateReviewID rejects both anyway.
	if filepath.IsAbs(arg) || strings.ContainsAny(arg, `/\`) || arg == "." {
		return arg, nil // explicit path: user-provided, used verbatim
	}
	// Bare id: validate so "..", a leading dash, etc. cannot escape the reviews dir.
	if err := fanout.ValidateReviewID(arg); err != nil {
		return "", fmt.Errorf("invalid review id %q: %w", arg, err)
	}
	return filepath.Join(fanout.ReviewsRoot("."), arg), nil
}

// resolveReviewDir resolves the anchor and verifies the review directory holds a
// sources/ tree, so a missing/incomplete review surfaces as a clear usage error
// (exit 2) rather than a deep discovery failure.
func resolveReviewDir(arg string) (string, error) {
	dir, err := anchorDir(arg)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(filepath.Join(dir, "sources")); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("no sources found in %s: run 'atcr review' first", dir)
	}
	return dir, nil
}
