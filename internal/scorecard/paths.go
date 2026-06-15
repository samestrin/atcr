package scorecard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// storeSubdir is the scorecard store location under the user config dir:
// ~/.config/atcr/scorecard/ on Linux, the platform equivalent elsewhere.
const storeSubdir = "atcr/scorecard"

// monthRe validates the YYYY-MM month prefix derived from a run_id.
var monthRe = regexp.MustCompile(`^\d{4}-\d{2}$`)

// DefaultDir returns the default scorecard store directory
// (os.UserConfigDir()/atcr/scorecard). The directory is never created here —
// Append creates it lazily on first write so a suppressed run touches nothing.
func DefaultDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving user config dir: %w", err)
	}
	return filepath.Join(cfg, storeSubdir), nil
}

// resolveDir returns override when non-empty (tests pin a temp dir), else the
// default store directory.
func resolveDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return DefaultDir()
}

// monthFromRunID derives the YYYY-MM month file stem from a run_id whose prefix
// is an RFC3339 timestamp (e.g. "2026-06-14T10:00:00Z-abc123" -> "2026-06").
// The month drives monthly JSONL rotation, so all records from one run share a
// file. A run_id that does not start with YYYY-MM is rejected rather than
// silently bucketed into a wrong/empty month.
func monthFromRunID(runID string) (string, error) {
	if len(runID) < 7 || !monthRe.MatchString(runID[:7]) {
		return "", fmt.Errorf("cannot derive month from run_id %q (expected YYYY-MM prefix)", runID)
	}
	return runID[:7], nil
}
