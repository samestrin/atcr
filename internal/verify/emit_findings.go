package verify

import (
	"encoding/json"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"path/filepath"

	"github.com/samestrin/atcr/internal/atomicfs"
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

// checkVERIFIEDGuard returns an error if any finding in verdicts already carries
// Confidence=VERIFIED but has no trusted Verification block (inconsistent state).
// A --fresh re-run always supplies a proper block (hasTrustedVerdict=true), so
// the guard is skipped for legitimate re-verification. Callers invoke this before
// mutating findings so the error surfaces before any state change.
func checkVERIFIEDGuard(findings []reconcile.JSONFinding, verdicts map[FindingKey]*reclib.Verification) error {
	for i := range findings {
		key := FindingKey{File: findings[i].File, Line: findings[i].Line, Problem: findings[i].Problem}
		if _, ok := verdicts[key]; ok &&
			findings[i].Confidence == ConfidenceVerified &&
			!hasTrustedVerdict(findings[i].Verification) {
			return fmt.Errorf("ReEmitFindings: finding %s:%d already VERIFIED — re-run with --fresh to re-verify",
				findings[i].File, findings[i].Line)
		}
	}
	return nil
}

// computeFindingsBytes marshals a pre-computed findings slice to indented JSON
// with a trailing newline and returns the canonical path and bytes. The caller
// is responsible for applying verdicts before calling this.
func computeFindingsBytes(findings []reconcile.JSONFinding, reviewDir string) (string, []byte, error) {
	path := filepath.Join(reviewDir, reconciledSubdir, reconcile.FindingsJSON)
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return path, append(data, '\n'), nil
}

// computeReEmitFindingsBytes loads reviewDir/reconciled/findings.json, runs the
// VERIFIED guard, applies verdicts, recomputes confidence, and returns the
// updated findings slice, canonical path, and serialized bytes. The pipeline
// uses the returned findings slice to avoid a disk round-trip; ReEmitFindings
// uses it for the standalone write path.
func computeReEmitFindingsBytes(reviewDir string, verdicts map[FindingKey]*reclib.Verification) ([]reconcile.JSONFinding, string, []byte, error) {
	findings, err := reconcile.ReadReconciledFindings(reviewDir)
	if err != nil {
		return nil, "", nil, err
	}
	if err := checkVERIFIEDGuard(findings, verdicts); err != nil {
		return nil, "", nil, err
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
	path, data, err := computeFindingsBytes(findings, reviewDir)
	if err != nil {
		return nil, "", nil, err
	}
	return findings, path, data, nil
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
func ReEmitFindings(reviewDir string, verdicts map[FindingKey]*reclib.Verification) error {
	_, path, data, err := computeReEmitFindingsBytes(reviewDir, verdicts)
	if err != nil {
		return err
	}
	return atomicfs.WriteFileAtomic(path, data)
}
