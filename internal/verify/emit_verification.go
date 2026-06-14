package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// reconciledSubdir is the review-dir child the verify stage re-emits into,
// matching internal/reconcile's (unexported) constant of the same name.
const reconciledSubdir = "reconciled"

// VerificationResult is one skeptic verdict record in verification.json (AC
// 03-02) — the verify stage's rich, per-finding audit record. It is distinct
// from reconcile.Verification{Verdict,Skeptic,Notes}, the compact block embedded
// back into findings.json: this record additionally carries the skeptic's model
// (the different-model rule's evidence), the full reasoning, and the cost/outcome
// metadata (duration, tripped budgets) a human needs to judge a verdict.
type VerificationResult struct {
	File           string   `json:"file"`
	Line           int      `json:"line"`
	Problem        string   `json:"problem"`
	Verdict        string   `json:"verdict"`
	Skeptic        string   `json:"skeptic"`
	Model          string   `json:"model"`
	Reasoning      string   `json:"reasoning"`
	DurationMs     int      `json:"durationMs"`
	TrippedBudgets []string `json:"trippedBudgets"`
}

// VerdictCounts tallies the three verdict outcomes across a verification run.
type VerdictCounts struct {
	Confirmed    int `json:"confirmed"`
	Refuted      int `json:"refuted"`
	Unverifiable int `json:"unverifiable"`
}

// VerificationFile is the reconciled/verification.json top-level schema (AC
// 03-02). MinSeverity/Fresh/Thorough are the run-metadata fields the Phase 4 CLI
// wiring populates; in the Phase 3 writer they serialize as their zero values.
type VerificationFile struct {
	VerifiedAt    string               `json:"verifiedAt"`
	MinSeverity   string               `json:"minSeverity"`
	Fresh         bool                 `json:"fresh"`
	Thorough      bool                 `json:"thorough"`
	Findings      []VerificationResult `json:"findings"`
	VerdictCounts VerdictCounts        `json:"verdictCounts"`
}

// WriteVerification writes reviewDir/reconciled/verification.json atomically (AC
// 03-02). VerifiedAt is stamped at call time (RFC 3339, UTC). VerdictCounts is
// derived from results so the tally can never drift from the records it counts.
// Each result's nil TrippedBudgets is normalized to [] so the field never
// serializes as null. The reconciled/ directory is created if absent.
func WriteVerification(reviewDir string, results []VerificationResult) error {
	out := make([]VerificationResult, len(results))
	var counts VerdictCounts
	for i, r := range results {
		if r.TrippedBudgets == nil {
			r.TrippedBudgets = []string{}
		}
		out[i] = r
		switch r.Verdict {
		case verdictConfirmed:
			counts.Confirmed++
		case verdictRefuted:
			counts.Refuted++
		case verdictUnverifiable:
			counts.Unverifiable++
		}
	}
	vf := VerificationFile{
		VerifiedAt:    time.Now().UTC().Format(time.RFC3339),
		Findings:      out,
		VerdictCounts: counts,
	}
	reconDir := filepath.Join(reviewDir, reconciledSubdir)
	if err := os.MkdirAll(reconDir, 0o755); err != nil {
		return fmt.Errorf("creating reconciled dir: %w", err)
	}
	return writeJSONAtomic(filepath.Join(reconDir, "verification.json"), vf)
}

// writeJSONAtomic marshals v as 2-space-indented JSON with a trailing newline
// (matching reconcile's renderIndentedJSON) and writes it to path atomically.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// writeFileAtomic writes data to a sibling temp file (0644) then renames it over
// path, so a reader never observes a partial write. It mirrors the temp-file +
// rename pattern in internal/reconcile and internal/payload — duplicated here
// because both of those copies are unexported. The rename is atomic within a
// single POSIX filesystem.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
