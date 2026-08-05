package localdebt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// debtSubdir is the local TD store location under the repo root: <root>/.atcr/debt.
const debtSubdir = ".atcr/debt"

// monthRe validates the YYYY-MM month prefix derived from a run_id. The month is
// constrained to 01-12 so a structurally impossible month (e.g. 2026-99) is a clear
// error rather than a silently-empty lookup or a path-traversal vector.
var monthRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// DefaultDir returns the default local TD store directory (<root>/.atcr/debt). The
// directory is never created here — Append creates it lazily on first write so a
// suppressed run touches nothing. Root follows cli/reconcile.go's Root: "."
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

// ManualRunID builds the synthetic run_id for an item filed by `atcr debt add`,
// which has no reconcile run behind it to supply one. It mirrors the resolution
// path's existing <RFC3339>-<suffix> construction (cli/debt_resolve.go), so for any
// timestamp in the four-digit-year range the result carries the YYYY-MM prefix
// monthFromRunID requires and a manual entry lands in the correct month shard
// instead of failing the append outright.
//
// Precondition: ts must fall in years 1-9999. RFC3339 formats a year outside that
// range as "10000-01-…" or "-0001-01-…", neither of which matches monthRe, so
// Append would reject the record; and the zero time.Time formats as "0001-01-…",
// which does match and would silently create a phantom 0001-01 shard. Callers
// validate their input before calling (see the tech-debt note filed against this
// helper), because T2's `atcr debt add` is where a user-supplied timestamp
// actually originates.
//
// ts is normalized to UTC before formatting, so a local instant just past midnight
// on the 1st is filed under the month it actually belongs to. The -manual suffix
// keeps a manual entry's provenance legible in the raw JSONL as well as in the
// record's origin field, where a reconcile run_id instead ends in the review
// directory's base name.
func ManualRunID(ts time.Time) string {
	return ts.UTC().Format(time.RFC3339) + "-manual"
}

// basePathErr reduces an *os.PathError's path to its base name so an absolute store
// path (which may embed a username) is not embedded in an error that could reach an
// unredacted diagnostics sink. The operational Op and underlying Err are preserved;
// non-PathError errors pass through unchanged. Mirrors internal/scorecard/store.go.
// RedactPathErr is basePathErr for callers outside this package that build their
// own store read paths (cli's two-pass debt-resolve hydration). The package's
// SECURITY contract — a surfaced *os.PathError is reduced to its base name so an
// absolute, username-bearing path never reaches a diagnostic sink or an error
// string (see ReadOpts in store.go) — binds those callers too, and re-implementing
// the reduction outside the package is how it drifts.
func RedactPathErr(err error) error {
	return basePathErr(err)
}

func basePathErr(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		clone := *pe
		clone.Path = filepath.Base(pe.Path)
		return &clone
	}
	return err
}
