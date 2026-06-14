package verify

import (
	"path/filepath"

	"github.com/samestrin/atcr/internal/reconcile"
)

// FindingKey identifies a finding by location + problem text, the key a skeptic
// verdict is matched back to its finding by (AC 03-03). The reconciler
// deduplicates before verification, so this triple is effectively unique across
// reconciled findings; the documented assumption is that two findings sharing
// file+line+problem are the same finding and receive the same verdict.
type FindingKey struct {
	File    string
	Line    int
	Problem string
}

// ReEmitFindings loads reviewDir/reconciled/findings.json, applies the skeptic
// verdicts keyed by FindingKey, recomputes each matched finding's confidence via
// confidenceV2, and re-writes findings.json atomically (AC 03-03). A refuted
// finding is demoted to LOW but retained — verification never deletes a finding,
// so a wrong refutation stays visible to the human. Findings without a verdict
// keep their v1 confidence and a nil Verification (omitempty drops the block). An
// empty verdict map re-writes the file unchanged. A missing or malformed
// findings.json is propagated from reconcile.ReadReconciledFindings (os.ErrNotExist
// for a missing file).
func ReEmitFindings(reviewDir string, verdicts map[FindingKey]*reconcile.Verification) error {
	findings, err := reconcile.ReadReconciledFindings(reviewDir)
	if err != nil {
		return err // includes os.ErrNotExist and parse errors
	}
	for i := range findings {
		key := FindingKey{File: findings[i].File, Line: findings[i].Line, Problem: findings[i].Problem}
		v, ok := verdicts[key]
		if !ok || v == nil {
			continue
		}
		findings[i].Verification = v
		findings[i].Confidence = confidenceV2(findings[i].Confidence, v.Verdict)
	}
	path := filepath.Join(reviewDir, reconciledSubdir, reconcile.FindingsJSON)
	return writeJSONAtomic(path, findings)
}
