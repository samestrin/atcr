package localdebt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// debtSubdir is the local TD store location under the repo root: <root>/.atcr/debt.
const debtSubdir = ".atcr/debt"

// monthRe validates the YYYY-MM month prefix derived from a run_id. The month is
// constrained to 01-12 so a structurally impossible month (e.g. 2026-99) is a clear
// error rather than a silently-empty lookup or a path-traversal vector.
var monthRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// DefaultDir returns the default local TD store directory (<root>/.atcr/debt). The
// directory is never created here — Append creates it lazily on first write so a
// suppressed run touches nothing. Root follows cmd/atcr/reconcile.go's Root: "."
// (CWD) convention; pass "." for the current repo root.
func DefaultDir(root string) string {
	return filepath.Join(root, debtSubdir)
}

// monthFromRunID derives the YYYY-MM month file stem from a run_id whose prefix is
// an RFC3339 timestamp (e.g. "2026-06-14T10:00:00Z-abc123" -> "2026-06"). The month
// drives monthly JSONL rotation, so all records from one run share a file. A run_id
// that does not start with a valid YYYY-MM prefix is rejected rather than silently
// bucketed into a wrong/empty month or used to escape the store directory.
func monthFromRunID(runID string) (string, error) {
	if len(runID) < 7 || !monthRe.MatchString(runID[:7]) {
		return "", fmt.Errorf("cannot derive month from run_id %q (expected YYYY-MM prefix)", runID)
	}
	return runID[:7], nil
}

// basePathErr reduces an *os.PathError's path to its base name so an absolute store
// path (which may embed a username) is not embedded in an error that could reach an
// unredacted diagnostics sink. The operational Op and underlying Err are preserved;
// non-PathError errors pass through unchanged. Mirrors internal/scorecard/store.go.
func basePathErr(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		clone := *pe
		clone.Path = filepath.Base(pe.Path)
		return &clone
	}
	return err
}
