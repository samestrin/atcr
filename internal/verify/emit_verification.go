package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/atomicfs"
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
//
// Skeptic names the participating voters: all voters on a tie, the lead voter
// on a decisive outcome, or a single name carried forward from an on-disk block
// when no new skeptic executed. Model names only the skeptics whose
// verdict produced the recorded outcome — a winners-only subset on a decisive vote,
// all participants on a tie, and "" when no skeptic executed. So for a multi-vote
// run Model may list fewer entries than Skeptic by design (see winningAttribution).
// DurationMs is the wall-clock of the run that produced the verdict; for a finding
// skipped on a later re-run it is carried forward unchanged, not recomputed.
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

// CountVerdicts tallies the three verdict outcomes across a verification result
// set, normalizing each verdict (lower-cased, trimmed) before counting so a
// non-canonical casing/whitespace is never silently dropped — the same
// normalization confidenceV2 applies, so the two never disagree. It is the
// single source of truth for the tally: both WriteVerification (for the
// verification.json verdictCounts) and the pipeline (for UpdateSummaryVerdicts)
// call it, so the summary and the verification file can never drift (TD-008).
func CountVerdicts(results []VerificationResult) VerdictCounts {
	var counts VerdictCounts
	for _, r := range results {
		switch strings.ToLower(strings.TrimSpace(r.Verdict)) {
		case verdictConfirmed:
			counts.Confirmed++
		case verdictRefuted:
			counts.Refuted++
		case verdictUnverifiable:
			counts.Unverifiable++
		}
	}
	return counts
}

// computeVerificationBytes builds the VerificationFile from results and counts,
// marshals it, and returns the path and bytes to write. counts is taken as a
// parameter so the pipeline can pass the already-computed tally rather than
// recount. VerifiedAt is stamped at call time (RFC 3339Nano, UTC). The
// reconciled/ directory is created if absent.
func computeVerificationBytes(reviewDir string, results []VerificationResult, counts VerdictCounts) (string, []byte, error) {
	out := make([]VerificationResult, len(results))
	for i, r := range results {
		if r.TrippedBudgets == nil {
			r.TrippedBudgets = []string{}
		}
		out[i] = r
	}
	vf := VerificationFile{
		VerifiedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Findings:      out,
		VerdictCounts: counts,
	}
	reconDir := filepath.Join(reviewDir, reconciledSubdir)
	if err := os.MkdirAll(reconDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("creating reconciled dir: %w", err)
	}
	path := filepath.Join(reconDir, "verification.json")
	data, err := json.MarshalIndent(vf, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return path, append(data, '\n'), nil
}

// ReadVerificationResults reads reviewDir/reconciled/verification.json and returns
// its per-finding records. A missing file returns (nil, nil): a first-ever verify
// has no prior file, so the caller treats absent priors as "no metadata to carry
// forward" rather than an error. A present-but-unparseable file returns an error.
// It is the read counterpart of computeVerificationBytes/WriteVerification, used by
// the skip-already-verified path to recover a prior run's Model/DurationMs/
// TrippedBudgets (AC4).
func ReadVerificationResults(reviewDir string) ([]VerificationResult, error) {
	path := filepath.Join(reviewDir, reconciledSubdir, "verification.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var vf VerificationFile
	if err := json.Unmarshal(data, &vf); err != nil {
		return nil, fmt.Errorf("parsing verification.json: %w", err)
	}
	return vf.Findings, nil
}

// WriteVerification writes reviewDir/reconciled/verification.json atomically (AC
// 03-02). VerdictCounts is derived from results via CountVerdicts so the tally
// can never drift from the records it counts. Each result's nil TrippedBudgets
// is normalized to [] so the field never serializes as null.
func WriteVerification(reviewDir string, results []VerificationResult) error {
	path, data, err := computeVerificationBytes(reviewDir, results, CountVerdicts(results))
	if err != nil {
		return err
	}
	if err := backupExistingVerification(reviewDir); err != nil {
		return err
	}
	return atomicfs.WriteFileAtomic(path, data)
}

// backupExistingVerification snapshots an existing reconciled/verification.json to
// verification.json.bak before a re-verify (e.g. --fresh) overwrites it (Epic 4.7
// AC5). A first-ever verify has no prior file, so it is a no-op. Only
// verification.json — the verify stage's exclusive output — is backed up here;
// the reconcile-owned findings.json/summary.json the stage annotates in place are
// already covered by reconciled.bak/ (AC4), so they are not re-backed-up.
func backupExistingVerification(reviewDir string) error {
	path := filepath.Join(reviewDir, reconciledSubdir, "verification.json")
	if _, err := atomicfs.BackupToDotBak(path); err != nil {
		return fmt.Errorf("backing up prior verification.json: %w", err)
	}
	return nil
}
