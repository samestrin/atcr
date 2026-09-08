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
	// debate.go calls this unconditionally, including on a run that ruled nothing.
	// A no-ruling run used to return here without reading anything, but the
	// reconciling pass below has to look at a run that ruled nothing THIS time to
	// find what a PRIOR run's failed publish left behind — so the cheap exit now
	// lives after the candidate set is built, and still fires before the snapshot
	// is opened whenever that set comes out empty.
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
	// pending carries the findings a ruling settled whose caveat THIS call did not
	// clear. Two different histories land here and both leave `cleared` empty:
	//
	//   - a SECOND debate over one review dir. Run 1 cleared the caveat, so run 2
	//     has none left to clear — yet the record still names run 1's judge for a
	//     verdict run 2 replaced.
	//   - a publish that failed part-way. WriteGroup renames in sequence with no
	//     rollback, so findings.json can land with the caveat cleared while this
	//     file keeps both its tool_budget_bytes entry and its pre-debate verdict.
	//     filterAlreadyDebated then excludes the finding from every later run, so
	//     no ruling ever revisits it.
	//
	// The judge is taken from THIS run's ruling when there is one, and otherwise
	// from the prior reconciled/debate.json. debate.json is the first entry of the
	// same atomic group, so a failure that lost this file left that one standing —
	// it is the only place the residue's judge survives.
	//
	// Which of the two a record actually is, and whether it is owed anything, is
	// decided per record below against what is on disk. Nothing here rewrites a
	// verdict on the strength of the ruling alone.
	priorRulings := priorDebateRulings(reviewDir)
	pending := map[FindingKey]ruledVerdict{}
	for _, f := range findings {
		key := FindingKey{File: f.File, Line: f.Line, Problem: f.Problem}
		// Truncated still set means no ruling cleared this finding's caveat and none
		// is owed; f.Verification nil means RunReconcile rebuilt the block away and
		// there is no standing verdict to carry.
		if _, done := cleared[key]; done || f.Verification == nil || f.Verification.Truncated {
			continue
		}
		var judge, reasoning string
		if ra, hit := rulings[key]; hit && validVerdict(ra.verdict) {
			judge, reasoning = ra.judge, ra.reasoning
		} else if pr, hit := priorRulings[key]; hit {
			judge, reasoning = pr.judge, pr.reasoning
		} else {
			continue
		}
		pending[key] = ruledVerdict{verdict: f.Verification.Verdict, judge: judge, reasoning: reasoning}
	}
	if len(cleared) == 0 && len(pending) == 0 {
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
		settled, ok := cleared[key]
		if !ok {
			// pending and cleared are disjoint by construction, so anything reaching
			// here is a ruling whose caveat this call did not clear. What it is owed
			// depends on the record in front of it, never on the ruling alone.
			p, hit := pending[key]
			if !hit {
				continue
			}
			switch {
			case isPartialWriteResidue(rec):
				// The record still carries the tool-bytes entry beside a verdict that
				// SURVIVED its trip, while findings.json says the caveat is gone. Only a
				// ruling clears that flag, so the ruling landed there and the write that
				// was owed here did not. Fall through to the drop path below and finish it.
				settled = p
			case recordedDebateJudge(rec) != "":
				// A prior debate already owns this record, and this ruling replaced the
				// verdict it names. Keep the attribution current — the verdict alone is
				// what makes report.md and the score credit the superseded judge.
				//
				// Gated on the EXISTING judge, not on the ruling: that field is empty on
				// every record the verify stage produced, so without the gate this branch
				// would stamp judges onto verify-owned records a ruling merely touched —
				// the recompute debate.go's scope note rules out.
				//
				// A record already saying all three is left strictly alone. This pass
				// draws its candidates from the prior debate.json, so every finding a
				// debate ever ruled stays a candidate forever; marking the file changed
				// for an identical value would republish verification.json — and mint a
				// fresh .debate.bak, spending the one snapshot generation that exists —
				// on every later `atcr debate`, with nothing to show for it.
				if sameRecordedString(rec, "verdict", p.verdict) &&
					sameRecordedString(rec, "debateJudge", p.judge) &&
					sameRecordedString(rec, "debateReasoning", p.reasoning) {
					continue
				}
				rec["verdict"] = p.verdict
				rec["debateJudge"] = p.judge
				rec["debateReasoning"] = p.reasoning
				changed = true
				continue
			default:
				continue
			}
		}
		budgets, ok := rec["trippedBudgets"].([]any)
		if !ok || len(budgets) == 0 {
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

// recordedDebateJudge reads a verification.json record's debateJudge. It is empty
// on every record the verify stage alone produced and non-empty only where a
// debate rewrote one, which makes it this stage's marker for "already mine".
func recordedDebateJudge(rec map[string]any) string {
	judge, _ := rec["debateJudge"].(string)
	return judge
}

// isPartialWriteResidue reports whether a verification.json record is what a
// WriteGroup publish that failed after findings.json leaves behind.
//
// The signature is a surviving verdict — confirmed or refuted — beside a
// tool_budget_bytes entry. Only the DERIVED-ceiling exemption produces that pair:
// the read was shortened but the skeptic's answer stood, and internal/verify sets
// Verification.Truncated alongside it. The caller has already established that
// findings.json no longer carries that flag, and applyRulings is the only thing
// that clears it — so the ruling landed there while the matching correction here
// did not.
//
// An `unverifiable` verdict beside the same entry is deliberately NOT residue. It
// is the DECLARED-ceiling voiding path, where internal/verify throws the answer
// out and records the trip as the only account of why (see
// TestSyncVerificationTruncation_LeavesARuledDeclaredBudgetVoidingAlone). On disk
// it is indistinguishable from a residue whose verdict happened to be
// unverifiable, so both are declined: deleting a real voiding record also flips an
// all-unverifiable file to not-all-unverifiable and silently disables reconcile's
// gate. That residual is accepted, not overlooked.
func isPartialWriteResidue(rec map[string]any) bool {
	verdict, _ := rec["verdict"].(string)
	if verdict != reclib.VerdictConfirmed && verdict != reclib.VerdictRefuted {
		return false
	}
	budgets, ok := rec["trippedBudgets"].([]any)
	if !ok {
		return false
	}
	for _, b := range budgets {
		if s, ok := b.(string); ok && s == budgetToolBytes {
			return true
		}
	}
	return false
}

// priorDebateRulings projects the on-disk reconciled/debate.json into the rulings
// a PREVIOUS run recorded, keyed the way findings are. It is the residue repair's
// only source for the judge: debate.json is the first entry of runDebate's atomic
// group, so a rename failure that lost verification.json left it on disk.
//
// Best-effort in the same spirit as the rest of the stage — an absent or
// unreadable file yields no rulings rather than an error. Items with no judge are
// skipped: there is no attribution to restore, and writing a verdict without one
// would leave internal/verify's carry-forward guard reading the record as
// verify-owned and lending the superseded skeptic's model to it.
func priorDebateRulings(reviewDir string) map[FindingKey]ruleApply {
	df, found, err := ReadDebateFile(reviewDir)
	if err != nil || !found {
		return nil
	}
	out := map[FindingKey]ruleApply{}
	for _, it := range df.Items {
		if it.Judge == "" {
			continue
		}
		out[FindingKey{File: it.File, Line: it.Line, Problem: it.Problem}] = ruleApply{
			judge: it.Judge, reasoning: it.Reasoning,
		}
	}
	return out
}

// sameRecordedString reports whether a verification.json record already holds want
// at key. A missing key compares equal only to the empty string, which keeps an
// absent omitempty field from counting as a difference worth republishing for.
func sameRecordedString(rec map[string]any, key, want string) bool {
	got, _ := rec[key].(string)
	return got == want
}
