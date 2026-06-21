package debate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
// key, it writes the judge's verdict block (with the challenge-survived marker and
// the judge as the producing agent), recomputes confidence from the verdict, and —
// for a split ruling — overwrites the severity with the judge's settled value. A
// finding with no ruling is left untouched, so a non-debated finding's block is
// byte-identical.
func applyRulings(findings []reconcile.JSONFinding, rulings map[FindingKey]ruleApply) {
	for i := range findings {
		key := FindingKey{File: findings[i].File, Line: findings[i].Line, Problem: findings[i].Problem}
		ra, ok := rulings[key]
		if !ok {
			continue
		}
		if ra.severity != "" {
			findings[i].Severity = ra.severity
		}
		findings[i].Verification = &reconcile.Verification{
			Verdict:           ra.verdict,
			Skeptic:           ra.judge,
			Notes:             ra.reasoning,
			ChallengeSurvived: ra.survived,
		}
		findings[i].Confidence = reconcile.ConfidenceForVerdict(findings[i].Confidence, ra.verdict)
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

// writeFindings serializes the findings slice to reconciled/findings.json
// atomically (indented, trailing newline), mirroring the verify re-emit.
func writeFindings(reviewDir string, findings []reconcile.JSONFinding) error {
	path, data, err := computeFindingsBytes(reviewDir, findings)
	if err != nil {
		return err
	}
	return atomicfs.WriteFileAtomic(path, data)
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

// updateManifestStage appends "debate" to the manifest's stages list,
// idempotently. A manifest with no stages is seeded with "review" first. A missing
// manifest is returned as os.ErrNotExist; a malformed one as a parse error,
// leaving the file untouched. Mirrors verify.UpdateManifestStage.
func updateManifestStage(reviewDir string) error {
	path, data, err := computeManifestStageBytes(reviewDir)
	if err != nil {
		return err
	}
	if data == nil {
		return nil
	}
	return atomicfs.WriteFileAtomic(path, data)
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
