package fanout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
	return writePool(poolDir, results, changed, "")
}

// writePool is WritePool with the grounding audit reason threaded in (empty when
// the gate was enabled or no reason was supplied). ExecuteReview calls it directly
// so summary.json records why grounding was disabled (a git failure vs. range-less
// diff ingestion); every other caller uses the WritePool wrapper.
func writePool(poolDir string, results []Result, changed payload.ChangedLines, groundingDisabledReason string) (Summary, error) {
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
	ps := PoolSummary{
		Agents:                  statuses,
		Total:                   sum.Total,
		Succeeded:               sum.Succeeded,
		Failed:                  sum.Failed,
		Partial:                 sum.Partial,
		TotalFindings:           len(merged),
		GroundingEnabled:        &groundingEnabled,
		GroundingDisabledReason: groundingDisabledReason,
	}
	if err := writeJSON(filepath.Join(poolDir, summaryFile), ps); err != nil {
		return Summary{}, err
	}
	return sum, nil
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
		DroppedByMinSeverity:   fr.Dropped,
		TruncatedByMaxFindings: fr.Truncated,
		CacheHit:               r.CacheHit,
	}
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
