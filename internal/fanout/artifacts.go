package fanout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
func WritePool(poolDir string, results []Result) (Summary, error) {
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

		fr := findingsFor(r)
		merged = append(merged, fr.Findings...)

		if err := writeAgentArtifacts(poolDir, dir, r, fr); err != nil {
			return Summary{}, err
		}
		statuses = append(statuses, statusFor(r, fr))
	}

	// Merged pool findings (8-col, REVIEWER per row) for downstream convenience.
	if err := writeFindings(filepath.Join(poolDir, findingsFile), merged); err != nil {
		return Summary{}, err
	}

	sum := summarize(results)
	ps := PoolSummary{
		Agents:        statuses,
		Total:         sum.Total,
		Succeeded:     sum.Succeeded,
		Failed:        sum.Failed,
		Partial:       sum.Partial,
		TotalFindings: len(merged),
	}
	if err := writeJSON(filepath.Join(poolDir, summaryFile), ps); err != nil {
		return Summary{}, err
	}
	return sum, nil
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

func findingsFor(r Result) findingsResult {
	if r.Content == "" {
		return findingsResult{}
	}
	findings := stream.ParseModelOutput([]byte(r.Content))
	for i := range findings {
		findings[i].Reviewer = r.Agent
	}
	f, dropped, truncated := enforceConstraints(findings, r.Agent, r.MinSeverity, r.MaxFindings)
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
	}
	if r.Err != nil {
		st.Error = r.Err.Error()
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
