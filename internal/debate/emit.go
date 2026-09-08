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
	// debateBakSuffix names THIS stage's snapshot of verificationFile. It is
	// deliberately not ".bak": that name belongs to internal/verify
	// (backupExistingVerification), which keeps exactly one generation and would
	// lose its pre-verify one to every debate run that clears a caveat.
	debateBakSuffix = ".debate.bak"
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
// It returns the keys whose truncation caveat this call actually CLEARED — the
// findings that carried Verification.Truncated=true before the line below set it
// false. That is strictly narrower than "every ruling applied", and the narrower
// set is the one syncVerificationTruncation needs: once Truncated has been forced
// false there is no way to tell a caveat this run dropped from one that was never
// there, and a finding whose verdict internal/verify VOIDED under a declared
// ceiling is in the second group.
func applyRulings(findings []reconcile.JSONFinding, rulings map[FindingKey]ruleApply) map[FindingKey]ruleApply {
	clearedCaveats := map[FindingKey]ruleApply{}
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
			if v.Truncated {
				// Record the flip, not the ruling. A caveat this call did NOT clear is
				// one that was never there — and verification.json's tool_budget_bytes
				// entry then describes something else entirely (internal/verify's
				// voiding path records a DECLARED ceiling overruling the verdict, with
				// Truncated left false). Reading the post-apply flag downstream cannot
				// tell the two apart, because this line erases the difference.
				clearedCaveats[key] = ra
			}
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
	return clearedCaveats
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
//
// Residual, deliberately out of scope here: the verdict is corrected ONLY on the
// records whose caveat this call drops. A judge that overturns a finding whose
// verify verdict carried no tool_budget_bytes leaves this file's verdict stale,
// and internal/scorecard still counts it. That is not a regression this function
// introduced — it is the standing consequence of debate.go's scope note (the
// verify snapshot's verdicts are a point-in-time audit artifact) meeting a
// scorecard that reads verdicts from it anyway. Closing it means either debate
// rewriting every ruled verdict here, or the scorecard deriving settled verdicts
// from findings.json; both are larger decisions than this correction.
// ruledVerdict is the verdict a ruling settled on, together with the judge that
// produced it. Both travel to verification.json: the verdict alone would leave the
// record's skeptic/model/reasoning/durationMs — written by the verify stage and
// describing the run the judge replaced — claiming an outcome they did not produce.
type ruledVerdict struct {
	verdict   string
	judge     string
	reasoning string
}

func syncVerificationTruncation(reviewDir string, findings []reconcile.JSONFinding, clearedCaveats map[FindingKey]ruleApply, rulings map[FindingKey]ruleApply) (string, []byte, error) {
	// debate.go calls this unconditionally, including on a run that ruled nothing —
	// and on one whose every ruling left the caveat standing (applyRulings skips an
	// out-of-enum verdict). Neither changed how a recorded verdict was reached, so
	// neither is owed a correction; reading the snapshot at all would only risk one.
	if len(clearedCaveats) == 0 && len(rulings) == 0 {
		return "", nil, nil
	}
	// A correction is owed only where a RULING cleared the caveat, which is why the
	// set comes from applyRulings rather than being recomputed here. The post-apply
	// Truncated flag cannot express it: applyRulings forces it false on EVERY
	// ruling, so `!Truncated` was true by construction for every ruled finding.
	// internal/verify's voiding path records Verification{Verdict: unverifiable}
	// for a verdict a DECLARED ceiling overruled and never sets Truncated, so such
	// a finding walked into the set the moment it was ruled — and had the only
	// record of WHY its verdict was voided deleted, and the voided verdict itself
	// overwritten. envelope.go can only produce confirmed/refuted, so that flip is
	// one-directional: it turns an all-unverifiable file into a not-all-unverifiable
	// one and silently disables reconcile's all-unverifiable gate
	// (internal/reconcile/gate.go).
	//
	// The value is the verdict the correction has to carry with it.
	cleared := map[FindingKey]ruledVerdict{}
	for _, f := range findings {
		key := FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}
		ra, ok := clearedCaveats[key]
		if !ok || f.Verification == nil {
			continue
		}
		cleared[key] = ruledVerdict{verdict: f.Verification.Verdict, judge: ra.judge, reasoning: ra.reasoning}
	}
	// A SECOND debate over one review dir rules findings whose caveat run 1 already
	// cleared, so `cleared` is empty for them and the correction above never fires —
	// yet run 1's judge is still stamped on the record for a verdict run 2 replaced.
	// reruled carries those: a ruling applied this run whose caveat was NOT cleared
	// by it, i.e. exactly the rulings the set above cannot speak for.
	//
	// Whether such a record is owed a correction is decided per record below, on the
	// on-disk debateJudge — the marker that says a PRIOR debate already owns it. A
	// record the verify stage alone produced is left alone: a ruling that cleared no
	// caveat on a verify-owned record is the declared-ceiling voiding debate.go's
	// scope note keeps out of this stage.
	reruled := map[FindingKey]ruledVerdict{}
	for _, f := range findings {
		key := FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}
		if _, done := cleared[key]; done || f.Verification == nil {
			continue
		}
		ra, ok := rulings[key]
		if !ok || !validVerdict(ra.verdict) {
			continue
		}
		reruled[key] = ruledVerdict{verdict: f.Verification.Verdict, judge: ra.judge, reasoning: ra.reasoning}
	}
	if len(cleared) == 0 && len(reruled) == 0 {
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
	//
	// The round-trip is lossless in VALUE but not in LAYOUT: json.MarshalIndent
	// over map[string]any emits keys alphabetically at every level, so a
	// debate-written verification.json is key-sorted rather than in the struct
	// order internal/verify/emit_verification.go writes. Every in-tree reader goes
	// through encoding/json, so nothing breaks — but a verify-written and a
	// debate-written snapshot are NOT byte-comparable, which makes golden fixtures
	// and manual diffs across the two stages noisy. Do not add one. runDebate
	// takes a .bak before publishing this, so the pre-debate bytes remain
	// recoverable.
	//
	// Residual: a JSON number round-trips through float64 here, so an int64 field
	// added later above 2^53 would lose precision (9007199254740993 becomes
	// 9007199254740992). No current field is exposed to that; a future one would
	// need decoding with json.Number.
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
		file, _ := rec["file"].(string)
		problem, _ := rec["problem"].(string)
		line := 0
		if n, ok := rec["line"].(float64); ok {
			line = int(n)
		}
		key := FindingKey{File: file, Line: line, Problem: problem}
		if restated, ok := reruled[key]; ok {
			// Gated on an EXISTING debateJudge, not on the ruling alone. That field is
			// empty on every record the verify stage produced and non-empty only where a
			// prior debate rewrote one, so it is the one signal that says "this verdict
			// and its attribution are debate's to keep current". Without the gate this
			// branch would start stamping judges onto verify-owned records a ruling
			// merely touched — the recompute the scope note rules out, and the reverse of
			// the mis-attribution the stamp exists to prevent.
			if judge, _ := rec["debateJudge"].(string); judge != "" {
				rec["verdict"] = restated.verdict
				rec["debateJudge"] = restated.judge
				rec["debateReasoning"] = restated.reasoning
				changed = true
			}
			// reruled and cleared are disjoint by construction (the loop that builds
			// reruled skips every key already in cleared), so nothing below applies.
			continue
		}
		budgets, ok := rec["trippedBudgets"].([]any)
		if !ok || len(budgets) == 0 {
			continue
		}
		settled, ok := cleared[key]
		if !ok {
			continue
		}
		kept := make([]any, 0, len(budgets))
		dropped := false
		for _, b := range budgets {
			if s, ok := b.(string); ok && s == budgetToolBytes {
				dropped = true
				changed = true
				continue
			}
			kept = append(kept, b)
		}
		rec["trippedBudgets"] = kept
		if dropped {
			// The caveat and the verdict describe ONE verdict, and dropping the
			// caveat is what puts this finding back into survived_skeptic_rate.
			// internal/scorecard then reads the verdict from this same record, which
			// runDebate otherwise never rewrites — so on an OVERTURN the finding
			// re-entered the ratio under the verdict the judge had just replaced,
			// crediting the reviewer for a confirm that no longer exists. Before the
			// caveat was cleared at all it was excluded from both numerator and
			// denominator, so leaving this stale is strictly worse than the state
			// the sync was added to fix.
			//
			// Scoped deliberately to records whose caveat this call dropped. Writing
			// the verdict anywhere else would be the verification.json recompute
			// debate.go's atomic-group scope note rules out.
			rec["verdict"] = settled.verdict
			// The verdict does not travel alone. rec["skeptic"], rec["model"],
			// rec["reasoning"] and rec["durationMs"] were written by the verify stage
			// and describe the run this ruling REPLACED — emit_verification.go's
			// record contract says as much ("Model names only the skeptics whose
			// verdict produced the recorded outcome"). Rewriting the verdict without
			// saying who produced it published a refutation credited to the confirming
			// argument it overturned. Naming the judge here is what makes the record
			// readable again and points at reconciled/debate.json for the transcript;
			// it is also the one field a debate rewrite adds that verify never writes,
			// which is why internal/verify/pipeline.go's carry-forward guard keys on it.
			//
			// The verify-written fields are deliberately left in place rather than
			// overwritten: they are the audit trail of the superseded run, and the
			// radar reads Skeptic (reconcile.isVerificationTie) to detect
			// verification ties.
			rec["debateJudge"] = settled.judge
			rec["debateReasoning"] = settled.reasoning
		}
	}
	if !changed {
		return "", nil, nil
	}

	// Unreachable in practice, and deliberately left in rather than dropped: doc
	// came out of json.Unmarshal, so it holds only map/slice/string/float64/bool/nil
	// and re-marshals by construction. MarshalIndent fails on channels, funcs and
	// NaN — none of which json.Unmarshal can produce. Reaching it needs fault
	// injection, so it is covered by inspection, not by a test.
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return path, append(out, '\n'), nil
}
