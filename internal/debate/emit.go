package debate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	reclib "github.com/samestrin/atcr/reconcile"
	"os"
	"path/filepath"
	"strconv"

	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/reconcile"
)

// Debate artifact constants.
const (
	// DebateSchemaVersion versions the reconciled/debate.json contract.
	DebateSchemaVersion = "1.0"
	// DebateJSON is the per-run debate ruling artifact under reconciled/.
	DebateJSON = "debate.json"
	// debateSubdir holds the per-item transcripts, at the review-dir root.
	debateSubdir = "debate"
	// reconciledSubdir is the reconciled artifact directory (matches reconcile).
	reconciledSubdir = "reconciled"
	// verificationFile is the verify stage's snapshot under reconciled/. This
	// stage does not recompute it; syncVerificationTruncation corrects exactly one
	// entry a ruling invalidates.
	verificationFile = "verification.json"
	// manifestFile is the provenance file at the review-dir root.
	manifestFile = "manifest.json"
	// debateStage is the stage name a debate run records in the manifest.
	debateStage = "debate"
)

// FindingKey identifies a finding by location + problem text — the key a ruling is
// matched back to its finding by, the same triple verify uses.
type FindingKey struct {
	File    string
	Line    int
	Problem string
}

// ruleApply is the resolved effect of a ruling on one finding: the verdict to
// write, whether it survived challenge, the settled severity (split only; ""
// leaves severity unchanged), and the judge that produced it.
type ruleApply struct {
	verdict   string
	survived  bool
	severity  string
	judge     string
	reasoning string
}

// ItemResult is one debated item's recorded outcome (reconciled/debate.json).
type ItemResult struct {
	File              string `json:"file"`
	Line              int    `json:"line"`
	Kind              string `json:"kind"`
	Problem           string `json:"problem,omitempty"`
	Outcome           string `json:"outcome"`
	Reason            string `json:"reason,omitempty"`
	OriginalSeverity  string `json:"original_severity,omitempty"`
	SettledSeverity   string `json:"settled_severity,omitempty"`
	ClusterDecision   string `json:"cluster_decision,omitempty"`
	ChallengeSurvived bool   `json:"challenge_survived,omitempty"`
	SingleModel       bool   `json:"single_model,omitempty"`
	Proposer          string `json:"proposer,omitempty"`
	Challenger        string `json:"challenger,omitempty"`
	Judge             string `json:"judge,omitempty"`
	Reasoning         string `json:"reasoning,omitempty"`
	Transcript        string `json:"transcript,omitempty"`
}

// OverflowItem is a disputed item that matched a trigger but exceeded the
// max_items cap — recorded so the report can disclose what was not debated
// (never silent).
type OverflowItem struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
}

// DebateFile is the reconciled/debate.json document: every debated item's ruling
// plus the recorded overflow.
type DebateFile struct {
	SchemaVersion string         `json:"schemaVersion"`
	Items         []ItemResult   `json:"items"`
	Overflow      []OverflowItem `json:"overflow"`
}

// itemID is the stable directory id for a debated item's transcript. It hashes the
// location, kind, and problem so the same item maps to the same transcript dir
// across runs and distinct items never collide.
func itemID(item reconcile.DisagreementItem) string {
	h := sha256.Sum256([]byte(item.File + "\x00" + strconv.Itoa(item.Line) + "\x00" + item.Kind + "\x00" + item.Problem))
	return "item-" + hex.EncodeToString(h[:8])
}

// applyRulings mutates the findings slice in place: for each finding matched by
// key, it records the judge's verdict and challenge-survived marker, recomputes
// confidence from the verdict, and — for a split ruling — overwrites the severity
// with the judge's settled value. A finding already verified (Epic 3.0) keeps its
// existing Verification.Skeptic (the multi-voter list) and Notes (verify reasoning)
// as the audit trail — only the verdict/survived marker is updated; the judge and
// reasoning are recorded separately in reconciled/debate.json. A finding with no
// prior verification gets a fresh block with the judge as the producing agent. A
// finding with no ruling is left untouched, so a non-debated finding's block is
// byte-identical.
func applyRulings(findings []reconcile.JSONFinding, rulings map[FindingKey]ruleApply) {
	for i := range findings {
		key := FindingKey{File: findings[i].File, Line: findings[i].Line, Problem: findings[i].Problem}
		ra, ok := rulings[key]
		if !ok {
			continue
		}
		// reclib.Verification contract: the writing stage MUST validate Verdict
		// against the enum before persisting — an empty or out-of-enum verdict is a
		// contract violation downstream consumers choke on. Skip the whole ruling
		// (severity included) rather than persisting a malformed verification block.
		if !validVerdict(ra.verdict) {
			continue
		}
		if ra.severity != "" {
			findings[i].Severity = ra.severity
		}
		if v := findings[i].Verification; v != nil {
			// The finding already carries a verify-stage verification (Epic 3.0):
			// Skeptic is the comma-joined multi-voter list and Notes the original
			// verify reasoning — the audit trail the radar keys verification_disagreement
			// on (reconcile.isVerificationTie reads Skeptic). Record only the debate
			// outcome and preserve that provenance; the judge + reasoning are recorded
			// separately in reconciled/debate.json (ItemResult.Judge/Reasoning).
			v.Verdict = ra.verdict
			v.ChallengeSurvived = ra.survived
			// Truncated describes how the RECORDED verdict was reached, and this
			// verdict is now the judge's, produced from the judge's own read. The
			// block deliberately keeps the original skeptic's name as provenance,
			// but carrying its truncation caveat onto a verdict it did not produce
			// would attach "answered from a truncated read" to the wrong agent's
			// answer — in the report and in the precision-ratio exclusion alike.
			v.Truncated = false
		} else {
			// No prior verification (debate ran standalone): the judge is the only
			// agent that produced this verdict, so record it as the skeptic with its
			// reasoning as notes — the only audit trail available on this path.
			findings[i].Verification = &reclib.Verification{
				Verdict:           ra.verdict,
				Skeptic:           ra.judge,
				Notes:             ra.reasoning,
				ChallengeSurvived: ra.survived,
			}
		}
		findings[i].Confidence = reclib.ConfidenceForVerdict(findings[i].Confidence, ra.verdict)
	}
}

// validVerdict reports whether v is a canonical reconcile verdict. applyRulings
// gates on this before persisting so a malformed verdict (empty or out-of-enum) is
// never written into a Verification block (reclib.Verification's writer contract).
func validVerdict(v string) bool {
	switch v {
	case reclib.VerdictConfirmed, reclib.VerdictRefuted, reclib.VerdictUnverifiable:
		return true
	default:
		return false
	}
}

// computeFindingsBytes serializes the findings slice to indented JSON with a
// trailing newline and returns the target path plus bytes. It mirrors the verify
// re-emit format.
func computeFindingsBytes(reviewDir string, findings []reconcile.JSONFinding) (string, []byte, error) {
	path := filepath.Join(reviewDir, reconciledSubdir, reconcile.FindingsJSON)
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return path, append(data, '\n'), nil
}

// computeDebateBytes serializes the debate document to indented JSON with a
// trailing newline and returns the target path plus bytes.
func computeDebateBytes(reviewDir string, df DebateFile) (string, []byte, error) {
	if df.Items == nil {
		df.Items = []ItemResult{}
	}
	if df.Overflow == nil {
		df.Overflow = []OverflowItem{}
	}
	path := filepath.Join(reviewDir, reconciledSubdir, DebateJSON)
	data, err := json.MarshalIndent(df, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return path, append(data, '\n'), nil
}

// writeDebateFile serializes the debate document to reconciled/debate.json
// atomically.
func writeDebateFile(reviewDir string, df DebateFile) error {
	path, data, err := computeDebateBytes(reviewDir, df)
	if err != nil {
		return err
	}
	return atomicfs.WriteFileAtomic(path, data)
}

// computeManifestStageBytes appends "debate" to the manifest's stages list,
// idempotently, and returns the target path plus the updated JSON bytes. A
// manifest with no stages is seeded with "review" first. A missing manifest is
// returned as os.ErrNotExist; a malformed one as a parse error, leaving the file
// untouched. Mirrors verify.UpdateManifestStage.
func computeManifestStageBytes(reviewDir string) (string, []byte, error) {
	path := filepath.Join(reviewDir, manifestFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", nil, fmt.Errorf("parsing manifest.json: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	rawStages, _ := m["stages"].([]any)
	stages := make([]string, 0, len(rawStages))
	for _, s := range rawStages {
		if str, ok := s.(string); ok {
			stages = append(stages, str)
		}
	}
	for _, s := range stages {
		if s == debateStage {
			// Already recorded: return a no-op marker so the atomic group can skip
			// re-writing this file.
			return path, nil, nil
		}
	}
	if len(stages) == 0 {
		stages = []string{"review"}
	}
	m["stages"] = append(stages, debateStage)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return path, append(out, '\n'), nil
}

// ReadDebateFile reads reviewDir/reconciled/debate.json. It returns found=false
// (no error) when the file is absent — a review that never ran the debate stage —
// so callers (the report view) can render conditionally. A present-but-malformed
// file is an error.
func ReadDebateFile(reviewDir string) (df DebateFile, found bool, err error) {
	data, rerr := os.ReadFile(filepath.Join(reviewDir, reconciledSubdir, DebateJSON))
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return DebateFile{}, false, nil
		}
		return DebateFile{}, false, rerr
	}
	if err := json.Unmarshal(data, &df); err != nil {
		return DebateFile{}, false, fmt.Errorf("parsing %s: %w", DebateJSON, err)
	}
	return df, true, nil
}

// overflowItems projects the selector's overflow into the recorded shape.
func overflowItems(items []reconcile.DisagreementItem) []OverflowItem {
	out := make([]OverflowItem, 0, len(items))
	for _, it := range items {
		out = append(out, OverflowItem{File: it.File, Line: it.Line, Kind: it.Kind, Severity: it.Severity})
	}
	return out
}

// budgetToolBytes is the tripped-budget marker for the tool-output ceiling.
// internal/verify and internal/fanout each keep their own unexported copy; this
// is a third, for the same reason theirs are duplicated — what the stages share
// is verification.json's on-disk shape, not a Go symbol.
const budgetToolBytes = "tool_budget_bytes"

// syncVerificationTruncation returns the rewritten reconciled/verification.json
// for a debate run whose rulings invalidated a recorded truncation caveat, or
// ("", nil, nil) when nothing is owed.
//
// This is NOT the recompute debate.go rules out. That prohibition is about
// verdicts and tallies: verification.json is a point-in-time record of what the
// VERIFY stage concluded, and rewriting its verdicts would destroy the audit
// trail findings.json and debate.json already supersede. What is corrected here
// is a single entry that a ruling made factually false, on exactly the findings
// that were ruled.
//
// applyRulings clears Verification.Truncated on a ruling because the recorded
// verdict is now the judge's, produced from the judge's own read. The matching
// tool_budget_bytes entry in verification.json describes the SAME fact about the
// SAME verdict, and leaving it made report.md and the reviewer's durable
// survived_skeptic_rate disagree: the report showed a ruling with no caveat while
// the score still dropped the finding.
//
// It has to be this artifact rather than findings.json. internal/scorecard is
// emitted from EmitForReconcile, which runs after RunReconcile — and RunReconcile
// rebuilds findings.json from sources/, stripping every verification block on the
// way. verification.json is the only record of a verdict still standing by then.
//
// Best-effort in the same spirit as the rest of the stage: an absent or
// unparseable snapshot yields no rewrite rather than an error, so a debate over a
// review that was never verified still completes.
func syncVerificationTruncation(reviewDir string, findings []reconcile.JSONFinding) (string, []byte, error) {
	// Only findings whose recorded verdict now carries NO caveat can owe a
	// correction. A finding the judge left alone keeps whatever verify concluded.
	cleared := map[FindingKey]bool{}
	for _, f := range findings {
		if f.Verification != nil && !f.Verification.Truncated {
			cleared[FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}] = true
		}
	}
	if len(cleared) == 0 {
		return "", nil, nil
	}

	path := filepath.Join(reviewDir, reconciledSubdir, verificationFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, nil
	}
	// Decoded into a generic map so every field this stage does not understand —
	// present or added later — survives the rewrite byte-for-byte in value. Only
	// the one entry below is touched.
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return "", nil, nil
	}
	raw, ok := doc["findings"].([]any)
	if !ok {
		return "", nil, nil
	}

	changed := false
	for _, item := range raw {
		rec, ok := item.(map[string]any)
		if !ok {
			continue
		}
		budgets, ok := rec["trippedBudgets"].([]any)
		if !ok || len(budgets) == 0 {
			continue
		}
		file, _ := rec["file"].(string)
		problem, _ := rec["problem"].(string)
		line := 0
		if n, ok := rec["line"].(float64); ok {
			line = int(n)
		}
		if !cleared[FindingKey{File: file, Line: line, Problem: problem}] {
			continue
		}
		kept := make([]any, 0, len(budgets))
		for _, b := range budgets {
			if s, ok := b.(string); ok && s == budgetToolBytes {
				changed = true
				continue
			}
			kept = append(kept, b)
		}
		rec["trippedBudgets"] = kept
	}
	if !changed {
		return "", nil, nil
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return path, append(out, '\n'), nil
}
