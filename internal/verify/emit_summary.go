package verify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/reconcile"
)

// computeSummaryVerdictsBytes reads reviewDir/reconciled/summary.json, sets the
// verdictCounts field, and returns the path and the bytes to write. All existing
// fields survive the round-trip (decoded via json.Decoder.UseNumber so large
// integers are preserved). A missing file returns os.ErrNotExist.
func computeSummaryVerdictsBytes(reviewDir string, counts VerdictCounts) (string, []byte, error) {
	path := filepath.Join(reviewDir, reconciledSubdir, reconcile.SummaryJSON)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	var summary map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&summary); err != nil {
		return "", nil, fmt.Errorf("parsing summary.json: %w", err)
	}
	if summary == nil {
		summary = map[string]any{}
	}
	summary["verdictCounts"] = counts
	out, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return path, append(out, '\n'), nil
}

// UpdateSummaryVerdicts adds (or replaces) the verdictCounts field in
// reviewDir/reconciled/summary.json, preserving every existing field (AC 03-04).
func UpdateSummaryVerdicts(reviewDir string, counts VerdictCounts) error {
	path, data, err := computeSummaryVerdictsBytes(reviewDir, counts)
	if err != nil {
		return err
	}
	return atomicfs.WriteFileAtomic(path, data)
}
