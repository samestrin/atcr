package fanout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samestrin/atcr/internal/payload"
)

// reviewIDRe is the positive allowlist for a review id: it must be a single
// path component starting with an alphanumeric. This rejects in one rule every
// escape vector — ".", "..", "", a leading "-" (flag injection), "/" and "\"
// separators, and absolute paths — without the brittle ".." substring heuristic,
// which both over-rejected legitimate ids (release-1..2) and under-rejected ".".
var reviewIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// manifestFile is the per-review provenance file at the review-dir root.
const manifestFile = "manifest.json"

// reviewSubdirs are the directories `atcr review` scaffolds. The per-agent
// sources/pool/raw/agent/<name>/ tree is created later by the fan-out engine
// (WritePool); scaffolding creates only the top-level trio (AC 01-03 Note).
var reviewSubdirs = []string{"payload", "sources", "reconciled"}

// branchPrefixes are stripped from a branch before slugifying so a review id is
// derived from the meaningful tail (feature/JIRA-123 → JIRA-123).
var branchPrefixes = []string{"feature/", "fix/", "bugfix/", "hotfix/", "release/", "chore/"}

// ReviewID derives the review id. An explicit override wins verbatim after a
// path-traversal check; otherwise the id is "<date>_<slug>" where slug is the
// sanitized branch ("detached" for a detached HEAD / empty branch, "review" when
// the branch sanitizes to nothing). When exists reports a collision, the
// HHMMSS-style collisionSuffix is appended (AC 01-03 Edge Case 1). exists may be
// nil to skip the collision probe.
func ReviewID(override, branch, date, collisionSuffix string, exists func(id string) bool) (string, error) {
	if s := strings.TrimSpace(override); s != "" {
		if err := validateReviewID(s); err != nil {
			return "", err
		}
		return s, nil
	}
	slug := slugifyBranch(branch)
	switch {
	case strings.TrimSpace(branch) == "":
		slug = "detached"
	case slug == "":
		slug = "review"
	}
	id := date + "_" + slug
	// Defense-in-depth: validate the computed id, not just user overrides — a
	// degenerate date or slug must never yield an unsafe component.
	if err := validateReviewID(id); err != nil {
		return "", err
	}
	if exists != nil {
		id = resolveCollision(id, collisionSuffix, exists)
	}
	return id, nil
}

// resolveCollision returns the first non-colliding id, appending the suffix then
// an incrementing counter so two reviews of the same branch within the same
// second never scaffold into one another's directory. The loop is bounded by a
// generous cap to avoid spinning on a pathological exists predicate.
func resolveCollision(id, suffix string, exists func(string) bool) string {
	if !exists(id) {
		return id
	}
	candidate := id + "-" + suffix
	for n := 2; exists(candidate) && n < 10000; n++ {
		candidate = fmt.Sprintf("%s-%s-%d", id, suffix, n)
	}
	return candidate
}

// validateReviewID rejects ids that could escape the reviews directory. The
// message is AC 01-03 Edge Case 4 verbatim.
func validateReviewID(id string) error {
	if !reviewIDRe.MatchString(id) {
		return fmt.Errorf("invalid review id: must not contain path separators or '..'")
	}
	return nil
}

// ValidateReviewID is the exported guard the CLI applies to a bare review-id
// anchor argument (so "..", "/...", or a leading dash can never resolve to a
// directory outside .atcr/reviews/).
func ValidateReviewID(id string) error { return validateReviewID(id) }

// slugifyBranch strips a known git-flow prefix then collapses every run of
// characters outside [A-Za-z0-9._-] into a single '-', preserving case and
// existing separators (feature/JIRA-123-add-auth → JIRA-123-add-auth). Leading
// and trailing '-' are trimmed.
func slugifyBranch(branch string) string {
	b := strings.TrimSpace(branch)
	for _, p := range branchPrefixes {
		if strings.HasPrefix(b, p) {
			b = b[len(p):]
			break
		}
	}
	var sb strings.Builder
	prevDash := false
	for _, r := range b {
		if isSlugChar(r) {
			sb.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			sb.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(sb.String(), "-")
	// A slug that is only dots ("." / "..") would form an unsafe component; treat
	// it as empty so the caller falls back to "review".
	if strings.Trim(slug, ".") == "" {
		return ""
	}
	return slug
}

func isSlugChar(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	default:
		return false
	}
}

// ReviewsRoot returns .atcr/reviews under root.
func ReviewsRoot(root string) string {
	return filepath.Join(root, ".atcr", "reviews")
}

// ReviewExists reports whether a review directory with id already exists under
// root — the probe ReviewID's collision check uses.
func ReviewExists(root, id string) bool {
	_, err := os.Stat(filepath.Join(ReviewsRoot(root), id))
	return err == nil
}

// ScaffoldReviewDir creates .atcr/reviews/<id>/ and its top-level subdirs (0755),
// returning the review-dir path. Parent directories are created as needed
// (AC 01-03 Edge Case 3). A creation failure carries the AC 01-03 message.
func ScaffoldReviewDir(root, id string) (string, error) {
	dir := filepath.Join(ReviewsRoot(root), id)
	for _, sub := range reviewSubdirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", fmt.Errorf("failed to create review directory: %w", err)
		}
	}
	return dir, nil
}

// WriteLatest writes the review id (one line) to .atcr/latest so later commands
// default to it. The .atcr directory is created if absent.
func WriteLatest(root, id string) error {
	dir := filepath.Join(root, ".atcr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating .atcr dir: %w", err)
	}
	return atomicWriteFile(filepath.Join(dir, "latest"), []byte(id+"\n"))
}

// ReadLatest reads and validates the review id recorded in .atcr/latest. An
// empty or malformed pointer is an error rather than a silent "" that would
// resolve to the reviews root downstream.
func ReadLatest(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".atcr", "latest"))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return "", errors.New("empty .atcr/latest pointer: run 'atcr review' first")
	}
	if err := validateReviewID(id); err != nil {
		return "", fmt.Errorf(".atcr/latest: %w", err)
	}
	return id, nil
}

// WriteManifest writes m into <reviewDir>/manifest.json, centralizing the
// provenance-file path. It delegates the atomic encode to payload.WriteManifest.
func WriteManifest(reviewDir string, m *payload.Manifest) error {
	return payload.WriteManifest(filepath.Join(reviewDir, manifestFile), m)
}
