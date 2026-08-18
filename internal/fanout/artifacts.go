package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
)

// Pool artifact layout under <reviewDir>/sources/pool (AC 01-03/04/05):
//
//	pool/
//	  raw/agent/<agent>/{review.md, findings.txt, status.json}  # per-agent
//	  findings.txt                                              # merged (REVIEWER per row)
//	  summary.json                                              # run stats
const (
	poolRawAgentDir = "raw/agent"
	reviewFile      = "review.md"
	findingsFile    = "findings.txt"
	statusFile      = "status.json"
	summaryFile     = "summary.json"
)

// PoolSummary is the fan-out run record written to sources/pool/summary.json:
// every agent's status plus the aggregate tally. Partial mirrors Summary.Partial
// (≥1 failed but ≥1 succeeded).
type PoolSummary struct {
	Agents        []AgentStatus `json:"agents"`
	Total         int           `json:"total"`
	Succeeded     int           `json:"succeeded"`
	Failed        int           `json:"failed"`
	Partial       bool          `json:"partial"`
	TotalFindings int           `json:"total_findings"`
	// TruncatedZeroFindings counts agents whose model response was truncated on
	// finish_reason "length" AND kept zero findings after grounding + per-source
	// constraints — the GROUNDED AgentStatus.FindingsCount, NOT the raw
	// ParseModelOutput count (Epic 19.5). It is the run-level tally of truncated
	// agents that contributed nothing to the merged pool; a truncated agent that
	// kept >=1 GROUNDED finding is NOT counted (its partial findings landed).
	// NOTE: this tally is a DIFFERENT signal from the per-attempt
	// truncation-failover guard (engine.go invokeSlot), which demotes on the RAW
	// parsed count before grounding. A truncated response that raw-parses >=1
	// finding but has them all dropped as ungrounded/below-min-severity stays
	// StatusOK (the guard does not fire) yet is counted here. Reconciling the two
	// is deferred TD, not addressed in this epic. Always present so a 0 is
	// distinguishable from an older summary.json that predates the field.
	TruncatedZeroFindings int `json:"truncated_zero_findings"`
	// FallbackCount is the run-level tally of agents served by a fallback model —
	// a slot whose configured primary model overflowed/failed and was routed to a
	// litellm any→any fallback, recorded via Result.FallbackUsed and threaded to
	// AgentStatus.FallbackUsed/FallbackFrom by statusFor for both the bulk
	// (single-call) and chunked-merged (mergeResultGroup union) paths. Like
	// TruncatedZeroFindings it is ALWAYS present (not omitempty), so a 0 is
	// distinguishable from an older summary.json that predates the field. It is a
	// pure count of run-level substitutions, unaffected by grounding/post-processing
	// since fallback state is fixed before findingsFor runs. A downstream reader
	// (or reconcile's fallback-aware de-weighting, Epic 19.10 F5) uses it as a cheap
	// "does this run contain any substitution worth reconciling" signal without
	// walking every Agents entry.
	FallbackCount int `json:"fallback_count"`
	// FailureMarker is true only when writeFailureSummary produced this record
	// after a WritePool I/O fault, never when WritePool wrote a real run. It
	// makes the summary unambiguously a best-effort marker: a write-phase
	// failure can leave Partial=false (every agent ran) while only a subset of
	// per-agent artifacts reached disk, so readers that walk the surviving
	// artifacts (reconcile via ReadManifestPartial) must treat such a run as
	// partial. omitempty keeps it absent from real summaries, so older readers
	// correctly see the zero value (false).
	FailureMarker bool `json:"failure_marker,omitempty"`
	// GroundingEnabled records whether the Epic 14.1 grounding gate was active for
	// this run (true) or disabled/fail-open (false) — the audit signal that a git
	// failure or a range-less diff-ingestion run let findings through without the
	// anti-hallucination check. A pointer so a rebuilt summary (RebuildPool cannot
	// know the run's grounding state from on-disk artifacts) and pre-14.1 readers
	// omit it rather than falsely asserting false.
	GroundingEnabled *bool `json:"grounding_enabled,omitempty"`
	// GroundingDisabledReason explains WHY grounding was off (a git failure vs.
	// intentional diff ingestion) when GroundingEnabled is false; empty when the
	// gate was enabled, so omitempty keeps it absent from grounded runs.
	GroundingDisabledReason string `json:"grounding_disabled_reason,omitempty"`
}

// WritePool persists every agent's artifacts under poolDir, the merged pool
// findings.txt, and summary.json, returning the aggregate Summary. It writes a
// full set even when every agent failed (artifacts are preserved on disk for
// inspection, AC 03-02); the all-agents-failed gate is the caller's via Outcome.
// Each file is written atomically (temp + rename) so a crash never leaves a
// half-written artifact; pool-level writing is not transactional, so an I/O
// failure mid-run surfaces as an error with whatever per-agent files already
// landed left intact for inspection. The merged findings.txt is intentionally
// placed at the pool root, above the per-agent raw/ files, so leaf-preference
// discovery treats the raw files as the inputs and never double-counts the
// merged aggregate.
func WritePool(poolDir string, results []Result, changed payload.ChangedLines) (Summary, error) {
	return writePool(context.Background(), poolDir, results, changed, "")
}

// writePool is WritePool with the grounding audit reason threaded in (empty when
// the gate was enabled or no reason was supplied). ExecuteReview calls it directly
// so summary.json records why grounding was disabled (a git failure vs. range-less
// diff ingestion); every other caller uses the WritePool wrapper.
func writePool(ctx context.Context, poolDir string, results []Result, changed payload.ChangedLines, groundingDisabledReason string) (Summary, error) {
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		return Summary{}, fmt.Errorf("creating pool dir: %w", err)
	}

	var merged []stream.Finding
	statuses := make([]AgentStatus, 0, len(results))
	seen := make(map[string]bool, len(results))

	for _, r := range results {
		dir, err := agentDirName(r.Agent)
		if err != nil {
			return Summary{}, err
		}
		// Two agents collapsing to the same on-disk dir would clobber each other
		// silently; reject rather than lose artifacts. Roster validation makes
		// names unique upstream, but the writer does not rely on that.
		if seen[dir] {
			return Summary{}, fmt.Errorf("duplicate agent directory %q (from agent %q)", dir, r.Agent)
		}
		seen[dir] = true

		fr := findingsFor(r, changed)
		merged = append(merged, fr.Findings...)

		if err := writeAgentArtifacts(poolDir, dir, r, fr); err != nil {
			return Summary{}, err
		}
		statuses = append(statuses, statusFor(r, fr))
	}

	// Findings metrics (Epic 4.4): count the raw findings the agents emitted, in
	// total and by severity, before they are merged to disk.
	recordFindingMetrics(merged)

	// Merged pool findings (8-col, REVIEWER per row) for downstream convenience.
	if err := writeFindings(filepath.Join(poolDir, findingsFile), merged); err != nil {
		return Summary{}, err
	}

	sum := summarize(results)
	groundingEnabled := len(changed) > 0
	truncatedZeroFindings, truncatedZeroAgents := tallyTruncatedZeroFindings(statuses)
	warnTruncatedZeroFindings(ctx, truncatedZeroFindings, truncatedZeroAgents, false)
	ps := PoolSummary{
		Agents:                  statuses,
		Total:                   sum.Total,
		Succeeded:               sum.Succeeded,
		Failed:                  sum.Failed,
		Partial:                 sum.Partial,
		TotalFindings:           len(merged),
		TruncatedZeroFindings:   truncatedZeroFindings,
		FallbackCount:           sum.FallbackCount,
		GroundingEnabled:        &groundingEnabled,
		GroundingDisabledReason: groundingDisabledReason,
	}
	if err := writeJSON(filepath.Join(poolDir, summaryFile), ps); err != nil {
		return Summary{}, err
	}
	return sum, nil
}

// tallyTruncatedZeroFindings counts the run-level runaways (Epic 19.5): agents whose
// response was truncated on finish_reason=length with zero parseable findings. Derived
// from the per-agent statuses so the count and the named markers cannot drift.
//
// Shared by writePool and the resume path's RebuildPool rather than duplicated: the
// rebuild reconstructs the pool from these same records, and having derived the tally in
// only one of the two is how a resumed review silently lost it.
func tallyTruncatedZeroFindings(statuses []AgentStatus) (int, []string) {
	count := 0
	agents := make([]string, 0, len(statuses))
	for _, st := range statuses {
		if st.ResponseTruncated && st.FindingsCount == 0 {
			count++
			agents = append(agents, st.Agent)
		}
	}
	return count, agents
}

// warnTruncatedZeroFindings emits the run-level runaway warning, or nothing at 0.
//
// The tally has existed since Epic 19.5 but reached only summary.json, so the sole
// console surface was a per-agent WARN line an operator had to go looking for — and a
// whole roster can truncate to nothing while the run reports success (observed
// 2026-08-14 across four models on one baseline scan). Named agents, because "3
// reviewers contributed nothing" is not actionable without knowing which. Deliberately
// NOT an exit-code change: failing a multi-hour run over a diagnostic discards the work
// it existed to produce, the rule warnDriftingReviewers already follows. Silent at 0 for
// the same reason.
//
// The remedy names its own cost. The output cap is subtracted from the SAME context
// window the diff is packed into (payload.EffectiveByteBudget), so raising it buys
// reasoning room by taking review material away — and a cap within the 4096-token prompt
// reserve of the resolved window leaves an input budget of zero, which the bulk path
// degrades to a single-file review that still exits 0. Stating the tradeoff only in the
// flag help and docs/registry.md puts it nowhere the operator is looking when this line
// fires.
//
// Emitted from BOTH writePool and RebuildPool: reviewers that contributed nothing do not
// become harmless because the review was resumed.
//
// cumulative distinguishes the two callers. writePool reports a run just performed, where
// every counted agent truncated in that run. RebuildPool tallies the union of all on-disk
// statuses, which includes agents completed by an EARLIER attempt and not re-run by this
// one — and since a counted agent can stay StatusOK (a truncated response whose findings
// were all dropped by the grounding gate), its status.json is never rewritten and every
// later resume re-prints it. Scoping the warning to re-run agents would suppress exactly
// what a resuming operator needs to know, so the wording marks it as a restatement
// instead of narrowing it.
// It writes through the CONTEXT LOGGER, not os.Stderr. cli.Main binds that logger to
// the caller-supplied stderr, so an embedded caller using MainWithHooks(ctx, stdout,
// stderr, hooks) receives this line along with every other diagnostic; writing to the
// process stderr directly escaped exactly that seam, which cli/main.go states as the
// convention. buildPayloads' byte-budget warning already takes this route on the same
// call graph.
//
// A ctx carrying no logger discards the line (log.FromContext returns a discard
// logger). That is the correct trade: every production entry point installs one, and a
// caller that supplied no logger has asked for no output.
func warnTruncatedZeroFindings(ctx context.Context, count int, agents []string, cumulative bool) {
	if count == 0 {
		return
	}
	// Only the scope clause varies. The remedy is written ONCE: two literals stating
	// the same fix drift, and they did — the cumulative copy silently lost the
	// on_overflow consequence, on the very path where it is the only message printed.
	scope := "to the pool"
	if cumulative {
		scope = "to the pool across this review, including agents this resume did not re-run"
	}
	restatement := ""
	if cumulative {
		restatement = " This restates the review's cumulative tally rather than reporting a new failure."
	}
	log.FromContext(ctx).Warn(
		fmt.Sprintf("%d reviewer(s) truncated (finish_reason=length) with zero surviving findings and contributed nothing %s.%s",
			count, scope, restatement),
		"agents", strings.Join(agents, ", "),
		"remedy", truncatedZeroRemedy)
}

// truncatedZeroRemedy is the operator action shared by both variants of the warning
// above. Kept as one constant so the fresh and resumed paths cannot state different
// fixes for the same condition.
const truncatedZeroRemedy = "Raise their output cap (--max-tokens, or a per-agent max_tokens declaration) — a thinking model spends that budget on reasoning before emitting any finding. Note the tradeoff: the cap is taken out of the same context window, so raising it shrinks that agent's input budget, and a cap within 4096 tokens of the model's resolved context window leaves no input budget at all (the review then degrades to a single file, or fails outright under on_overflow fail/fallback). If the agent's window is small, declare a larger context_window_tokens instead of only raising the cap."

// normalizeFilesDropped turns a nil FilesDropped into an empty slice, so the field
// always publishes as a MEASUREMENT ("nothing was dropped") rather than as null
// ("unmeasured"). AgentStatus' never-silent contract (AC 06-03) rests on that
// distinction.
//
// Shared by statusFor and RebuildPool rather than duplicated: they are the two writers
// of the same artifact pair, and the normalization living in only one of them is what
// let summary.json publish null beside status.json's []. A future third writer inherits
// it by calling this instead of restating the guard.
func normalizeFilesDropped(st *AgentStatus) {
	if st.FilesDropped == nil {
		st.FilesDropped = []string{}
	}
}

// ReadPoolSummary loads <reviewDir>/sources/pool/summary.json — the run record
// carrying every agent's AgentStatus (model, token usage, latency). The
// reconcile-time scorecard emitter reads it to source per-reviewer metadata. A
// missing file returns the raw os error (callers degrade to no usage data); a
// present-but-unparseable file is a parse error.
func ReadPoolSummary(reviewDir string) (PoolSummary, error) {
	path := filepath.Join(reviewDir, "sources", "pool", summaryFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return PoolSummary{}, err
	}
	var ps PoolSummary
	if err := json.Unmarshal(data, &ps); err != nil {
		return PoolSummary{}, fmt.Errorf("parsing %s: %w", summaryFile, err)
	}
	return ps, nil
}

// writeFailureSummary writes a best-effort summary.json from the real fan-out
// results so a post-fan-out persistence failure (a WritePool error in
// ExecuteReview) surfaces accurate tallies through the existing summary-derived
// reader path instead of an eternal in_progress. Passing the real results
// (rather than a hard-coded all-failed roster count) preserves any partial
// success: a run where some agents produced findings before the WritePool I/O
// error is recorded as partial rather than fabricated as a total failure.
// Write errors are logged to stderr: this is a last-resort marker while the
// normal path is already failing, so if this write also fails, stale inference
// promotes the review out of in_progress once the timeout elapses.
func writeFailureSummary(poolDir string, results []Result) {
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "atcr: warning: writeFailureSummary: mkdir %s: %v\n", poolDir, err)
		return
	}
	sum := summarize(results)
	ps := PoolSummary{Total: sum.Total, Succeeded: sum.Succeeded, Failed: sum.Failed, Partial: sum.Partial, FailureMarker: true}
	if err := writeJSON(filepath.Join(poolDir, summaryFile), ps); err != nil {
		fmt.Fprintf(os.Stderr, "atcr: warning: writeFailureSummary: write summary: %v\n", err)
	}
}

// findingsFor parses an agent's raw review content into findings, stamps the
// REVIEWER as the agent name itself — never trusting any model-supplied column
// (TD-016) — then applies the agent's per-source review guardrails (min_severity
// floor + max_findings cap, Epic 2.2). Enforcement runs after stamping so the
// reviewer attribution is intact on every kept finding. A failed agent (no
// content) yields no findings.
type findingsResult struct {
	Findings  []stream.Finding
	Dropped   int
	Truncated int
}

func findingsFor(r Result, changed payload.ChangedLines) findingsResult {
	if r.Content == "" {
		return findingsResult{}
	}
	// Reuse the cached count computed when the result was built. A cached zero
	// count lets us skip a redundant parse for truncated-empty responses that
	// invokeSlot already demoted (TD-019).
	if r.parsedFindingCountSet && r.parsedFindingCount == 0 {
		return findingsResult{}
	}
	findings := stream.ParseModelOutput([]byte(r.Content))
	for i := range findings {
		findings[i].Reviewer = r.Agent
	}
	// Epic 14.1 grounding gate: drop findings whose FILE:LINE is not anchored in
	// the patch (hallucinations) before per-source constraints apply, so the
	// max_findings cap ranks only real findings. Runs only when review-level
	// grounding data was supplied; a nil/absent map disables the gate (fail open).
	// The per-agent drop count is logged to stderr. Unlike the enforceConstraints
	// min_severity/max_findings drops — which are ALSO persisted to status.json as
	// DroppedByMinSeverity/TruncatedByMaxFindings — grounding drops are surfaced on
	// stderr only, not in status.json or summary.json. This is deliberate: the epic
	// 14.1 clarification accepted the per-agent stderr count as the observable
	// mechanism, so the count is visible but intentionally not persisted.
	grounded, ungrounded := groundFindings(findings, changed)
	if ungrounded > 0 {
		fmt.Fprintf(os.Stderr, "atcr: warning: agent %q: dropped %d ungrounded finding(s) not present in the patch\n", r.Agent, ungrounded)
	}
	f, dropped, truncated := enforceConstraints(grounded, r.Agent, r.MinSeverity, r.MaxFindings)
	return findingsResult{Findings: f, Dropped: dropped, Truncated: truncated}
}

// agentDirName reduces an agent name to a safe single path segment and rejects
// names that would escape or alias the pool: filepath.Base leaves "..", ".", and
// "" intact (Base("..")=="..", Base("")=="."), so those are rejected explicitly
// rather than silently writing one level up or into the shared raw/agent dir.
func agentDirName(agent string) (string, error) {
	base := filepath.Base(agent)
	if base == "." || base == ".." || base == "" || base == string(os.PathSeparator) {
		return "", fmt.Errorf("invalid agent name %q: not a usable directory name", agent)
	}
	return base, nil
}

// writeAgentArtifacts creates the agent's raw dir and writes review.md,
// findings.txt, and status.json. dir is the pre-sanitized single-segment name.
func writeAgentArtifacts(poolDir, dir string, r Result, fr findingsResult) error {
	agentDir := filepath.Join(poolDir, poolRawAgentDir, dir)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return fmt.Errorf("creating agent dir for '%s': %w", r.Agent, err)
	}
	if err := atomicWriteFile(filepath.Join(agentDir, reviewFile), []byte(r.Content)); err != nil {
		return fmt.Errorf("writing review.md for '%s': %w", r.Agent, err)
	}
	if err := writeFindings(filepath.Join(agentDir, findingsFile), fr.Findings); err != nil {
		return fmt.Errorf("writing findings.txt for '%s': %w", r.Agent, err)
	}
	st := statusFor(r, fr)
	return WriteStatus(filepath.Join(agentDir, statusFile), &st)
}

// statusFor builds the per-agent status.json record from a result.
func statusFor(r Result, fr findingsResult) AgentStatus {
	st := AgentStatus{
		Agent:                  r.Agent,
		Status:                 r.Status,
		FindingsCount:          len(fr.Findings),
		DurationMS:             r.DurationMS,
		PayloadMode:            r.PayloadMode,
		Truncated:              r.Truncation.Truncated,
		FilesDropped:           r.Truncation.FilesDropped,
		FallbackUsed:           r.FallbackUsed,
		FallbackFrom:           r.FallbackFrom,
		FallbackModel:          r.FallbackModel,
		DroppedByMinSeverity:   fr.Dropped,
		TruncatedByMaxFindings: fr.Truncated,
		ResponseTruncated:      r.ResponseTruncated,
		UnparseableResponse:    r.UnparseableResponse,
		CacheHit:               r.CacheHit,
		UnreviewedChunks:       r.UnreviewedChunks,
		// Diagnosability (Epic 19.10 F8): pure pass-through of the per-agent sizing /
		// degradation values invokeAgent stamped onto the Result from the serving
		// Agent — no recomputation of sizing/chunk/overflow math here. All zero/empty
		// for an unsized agent (bare fixture, pre-19.10 run), so the AgentStatus
		// omitempty tags keep status.json/summary.json byte-identical for those runs.
		EffectiveBudget:      r.EffectiveBudget,
		ResolvedWindow:       r.ResolvedWindow,
		ReservedOutputTokens: r.ReservedOutputTokens,
		ChunkCount:           r.ChunkCount,
		DegradationAction:    r.DegradationAction,
	}
	// Normalized HERE, not only in WriteStatus. Both published views of this record
	// are built from statusFor, but only status.json passes through WriteStatus —
	// writePool marshals PoolSummary.Agents from its own un-normalized statusFor call,
	// so a nil slice reaching this function publishes "files_dropped": null in
	// summary.json beside [] in status.json. Two artifacts disagreeing on whether the
	// shed list was MEASURED is the never-silent contract (AC 06-03) weakened at the
	// only seam both of them share. WriteStatus keeps its own guard as a backstop for
	// hand-built AgentStatus values.
	//
	// Through the shared helper, not an inline guard: RebuildPool needs the identical
	// rule on the resume path, and the two writers of this artifact pair drifting is
	// exactly how summary.json came to publish null beside status.json's [].
	normalizeFilesDropped(&st)
	if r.Err != nil {
		st.Error = r.Err.Error()
	}
	// Persist usage only when the provider reported token counts (Epic 3.3). A
	// zero-usage result (a failed agent, or a completer that reports no usage)
	// leaves the omitempty fields absent, so status.json stays byte-identical to
	// the pre-3.3 shape for those runs. The model is recorded alongside the
	// tokens it priced so a $0 cost remains auditable.
	if r.TokensIn > 0 || r.TokensOut > 0 {
		st.Model = r.Model
		st.TokensIn = r.TokensIn
		st.TokensOut = r.TokensOut
	}
	// Tool-loop accounting: emit the counters (as explicit, possibly-zero
	// pointers) only for tool-enabled agents, so a pure 1.x single-shot agent's
	// status.json is byte-for-byte unchanged (the pointers stay nil/omitted). A
	// degraded tool agent still reports zeros and tools_degraded (AC 02-04 EC3).
	if r.Tools {
		turns, calls, bytes := r.Turns, r.ToolCalls, r.ToolBytes
		st.Turns = &turns
		st.ToolCalls = &calls
		st.ToolBytes = &bytes
		st.ToolsDegraded = r.ToolsDegraded
		st.ToolsDegradedReason = r.ToolsDegradedReason
		st.ToolsRequested = r.ToolsRequested
		st.TrippedBudgets = r.TrippedBudgets
	}
	return st
}

// writeFindings serializes findings to path in the per-source 8-column v1 format
// (header + rows), written atomically.
func writeFindings(path string, findings []stream.Finding) error {
	var buf bytes.Buffer
	if err := stream.WriteSource(&buf, findings); err != nil {
		return fmt.Errorf("encoding findings: %w", err)
	}
	return atomicWriteFile(path, buf.Bytes())
}

// writeJSON serializes v to path as indented JSON, written atomically.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", filepath.Base(path), err)
	}
	return atomicWriteFile(path, append(data, '\n'))
}
