package verify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// reconciledSubdir is the review-dir child the verify stage re-emits into,
// matching internal/reconcile's (unexported) constant of the same name.
const reconciledSubdir = "reconciled"

// VerificationResult is one skeptic verdict record in verification.json (AC
// 03-02) — the verify stage's rich, per-finding audit record. It is distinct
// from reclib.Verification{Verdict,Skeptic,Notes}, the compact block embedded
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

	// DebateJudge/DebateReasoning name the judge that PRODUCED the recorded
	// verdict when internal/debate overturned or upheld it after the fact. They
	// are empty on every record the verify stage alone produced, which is what
	// makes them the marker the carry-forward guard in pipeline.go keys on: a
	// debate rewrite equalises Verdict with the findings.json block, so verdict
	// equality can no longer distinguish "the same skeptic run" from "a judge
	// replaced it". Skeptic/Model/DurationMs continue to describe the SUPERSEDED
	// skeptic run — this pair is what says so out loud, and what points a reader
	// at reconciled/debate.json for the full transcript.
	DebateJudge     string `json:"debateJudge,omitempty"`
	DebateReasoning string `json:"debateReasoning,omitempty"`

	// Extra holds every key of the on-disk record this struct does not model,
	// verbatim. reconciled/verification.json is re-emitted on every re-verify by
	// decoding it into this type and writing it back through
	// computeVerificationBytes, so without a catch-all any key added by another
	// stage, an older build, or a future field was silently dropped on the way —
	// internal/debate round-trips its own rewrite of this same file through
	// map[string]any for exactly that reason, leaving the stage that OWNS the file
	// the lossier of the two.
	//
	// It is not itself a field of the record: the marshaller merges it back in at
	// the top level, and modelled keys always win a collision so a stale extra can
	// never shadow a value this struct computed.
	Extra map[string]json.RawMessage `json:"-"`
}

// verificationResultFields is the set of JSON keys VerificationResult models,
// derived from the struct tags rather than listed by hand — a field added above
// without updating a hand-written list would otherwise be decoded twice (once
// typed, once into Extra) and then emitted from the stale copy.
var verificationResultFields = func() map[string]bool {
	out := map[string]bool{}
	rt := reflect.TypeOf(VerificationResult{})
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
}()

// verificationResultAlias strips the marshaller methods below so they can call
// encoding/json on the struct without recursing into themselves.
type verificationResultAlias VerificationResult

// MarshalJSON emits the modelled fields in struct order, then merges Extra back
// in. With no extras — every record this repo produces today — the output is
// byte-for-byte what the plain struct produced, so field order and omitempty are
// unchanged; only a record that actually carries unmodelled keys pays the
// map round-trip (and its alphabetical key order).
func (r VerificationResult) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(verificationResultAlias(r))
	if err != nil {
		return nil, err
	}
	if len(r.Extra) == 0 {
		return base, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	for k, v := range r.Extra {
		// Modelled keys win: UnmarshalJSON never puts one in Extra, but a
		// hand-built value could, and a stale extra must not shadow a computed field.
		if verificationResultFields[k] {
			continue
		}
		merged[k] = v
	}
	return json.Marshal(merged)
}

// UnmarshalJSON decodes the modelled fields normally and captures everything else
// into Extra so the re-emit above can put it back.
func (r *VerificationResult) UnmarshalJSON(data []byte) error {
	var alias verificationResultAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*r = VerificationResult(alias)
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for k := range all {
		if verificationResultFields[k] {
			delete(all, k)
		}
	}
	if len(all) > 0 {
		r.Extra = all
	}
	return nil
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
//
// NOTE: WriteVerification is a TEST-ONLY SEAM — it has no non-test callers.
// Production verify never routes through it: the pipeline's runVerify emits
// verification.json as part of a multi-file atomic group (atomicwrite.WriteGroup),
// composing the same pieces (computeVerificationBytes + backupExistingVerification)
// directly rather than through this wrapper. Tests that assert write or backup
// behavior through WriteVerification therefore exercise this wrapper, NOT the
// shipped path — do not infer production backup coverage from them.
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
// AC5). A first-ever verify has no prior file, so it is a no-op. By deliberate
// design (Epic 4.7 Clarifications) the verify stage backs up ONLY verification.json,
// its own exclusive output. The reconcile-owned findings.json/summary.json that the
// same flush also re-writes are NOT snapshotted here: their prior state is captured
// in reconciled.bak/ only by RunReconcile, never by runVerify. So on a standalone
// re-verify (atcr verify --fresh on an already-reconciled review, with no reconcile
// re-run in the same command) findings.json/summary.json are overwritten in place
// with no verify-stage backup — re-run reconcile to snapshot their prior state.
func backupExistingVerification(reviewDir string) error {
	path := filepath.Join(reviewDir, reconciledSubdir, "verification.json")
	if _, err := atomicfs.BackupToDotBak(path); err != nil {
		return fmt.Errorf("backing up prior verification.json: %w", err)
	}
	return nil
}
