package verify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/reconcile"
)

// UpdateSummaryVerdicts adds (or replaces) the verdictCounts field in
// reviewDir/reconciled/summary.json, preserving every existing field (AC 03-04).
// The summary is decoded into a generic map so fields this stage does not know
// about survive the round-trip untouched; only verdictCounts is set. The result
// is written atomically. A missing summary.json is returned as os.ErrNotExist; a
// malformed one as a parse error, leaving the file untouched.
func UpdateSummaryVerdicts(reviewDir string, counts VerdictCounts) error {
	path := filepath.Join(reviewDir, reconciledSubdir, reconcile.SummaryJSON)
	data, err := os.ReadFile(path)
	if err != nil {
		return err // includes os.ErrNotExist
	}
	var summary map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&summary); err != nil {
		return fmt.Errorf("parsing summary.json: %w", err)
	}
	if summary == nil {
		summary = map[string]any{}
	}
	summary["verdictCounts"] = counts
	return writeJSONAtomic(path, summary)
}
