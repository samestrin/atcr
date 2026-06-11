package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/payload"
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
			return "", fmt.Errorf("no review specified and no .atcr/latest pointer: run 'atcr review' first")
		}
		return filepath.Join(fanout.ReviewsRoot("."), id), nil
	}
	if filepath.IsAbs(arg) || strings.ContainsRune(arg, filepath.Separator) || arg == "." {
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

// readManifestPartial reads the partial flag from a review's manifest.json,
// defaulting to false when the manifest is absent or unreadable (best-effort
// provenance for the reconcile summary).
func readManifestPartial(reviewDir string) bool {
	data, err := os.ReadFile(filepath.Join(reviewDir, "manifest.json"))
	if err != nil {
		return false
	}
	var m payload.Manifest
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	return m.Partial
}
