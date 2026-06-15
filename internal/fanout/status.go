package fanout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/samestrin/atcr/internal/payload"
)

// Agent outcome values written to status.json. StatusFailed reflects a failed
// LLM call (transport, HTTP, or auth error) after fallback resolution; a
// successful HTTP response is StatusOK regardless of content shape —
// unparseable finding lines are silently skipped at parse time, so an ok agent
// whose response yielded nothing parseable legitimately records
// findings_count 0. StatusTimeout covers both deadline expiry and context
// cancellation (e.g. a user interrupt): classifyStatus deliberately maps
// context.Canceled to timeout, so persisted artifacts do not distinguish a
// cancelled run from an exhausted time budget (documented design decision,
// sprint-plan Phase 3).
const (
	StatusOK      = "ok"
	StatusFailed  = "failed"
	StatusTimeout = "timeout"
)

// Tripped-budget markers recorded in AgentStatus.TrippedBudgets and Result
// (Epic 2.0). They name which per-agent budget halted the tool loop so the
// status.json reader and report panel can attribute the early stop.
const (
	budgetMaxTurns    = "max_turns"
	budgetToolBytes   = "tool_budget_bytes"
	budgetTimeout     = "timeout_secs"
	budgetLoopHygiene = "loop_hygiene"
)

// Review run states reported by ReadReviewStatus (AC 04-04). in_progress holds
// until the fan-out writes summary.json, then completed (≥1 agent ok) or failed
// (all agents failed). stale is an inferred terminal state (Epic 1.5): summary
// is absent AND the effective timeout (plus a grace margin) has elapsed, so the
// fan-out is presumed dead rather than still running. stale is honestly labeled
// as inferred, not observed — a poll loop treats it as terminal.
const (
	RunInProgress = "in_progress"
	RunCompleted  = "completed"
	RunFailed     = "failed"
	RunStale      = "stale"
)

// staleGraceSecs is added atop the manifest's effective timeout before a
// summary-less review is inferred dead. It absorbs the synchronous post-deadline
// write path (per-agent renames + summary.json) and minor writer/reader clock
// skew, so stale never fires on a run that is merely finishing. The writer
// enforces the timeout itself, so false positives are bounded regardless.
//
// Bound justification: each per-agent atomicWriteFile does one temp Sync + one
// dir Sync; for the current max roster (~20 agents) and a pessimistic 500ms
// fsync per file, the post-deadline write path completes well within 60s. If
// the max roster grows or measured fsyncs exceed this budget, increase the
// constant or scale it with roster size. (Adjudicated per Epic 1.5 Q2: flat
// 60s is sufficient for current roster sizes.)
const staleGraceSecs = 60

// maxSummaryBytes caps the summary.json read in ReadReviewStatus and
// ReadManifestPartial. A real summary is kilobytes even for large rosters
// (~100 agents × ~1 KB per agent record ≈ 100 KB); 1 MiB is a generous
// bound that prevents an unexpectedly large file from allocating an unbounded
// heap slice before json.Unmarshal.
const maxSummaryBytes = 1 << 20 // 1 MiB

// EffectivePartial derives the authoritative partial flag from a PoolSummary,
// applying FailureMarker awareness that the raw Partial field lacks. When
// WritePool aborted mid-flush (FailureMarker=true) and at least one agent was
// in the roster (Total>0), the on-disk artifact set is an untrustworthy subset
// regardless of Succeeded/Failed counts — a timed-out agent may have flushed
// findings before the fault — so the run is always partial. ReadReviewStatus
// and ReadManifestPartial both delegate here so the two readers never drift.
func (ps PoolSummary) EffectivePartial() bool {
	return ps.Partial || (ps.FailureMarker && ps.Total > 0)
}

// nowFunc is the clock ReadReviewStatus consults for stale inference; a package
// var so tests can pin it for deterministic window assertions. Swapping nowFunc
// is NOT safe concurrent with readers: tests that reassign it must do so before
// reader goroutines start and restore it after they join, or guard access via
// atomic.Value. The current stale-clock tests are non-parallel by design to
// avoid this data race.
var nowFunc = time.Now

// ReviewStatus is a review's aggregated progress, derived from manifest.json
// (the roster) and sources/pool/summary.json (the completion signal). It is the
// shared shape returned to the `atcr status` CLI command and the atcr_status MCP
// tool so both report identical state.
//
// AgentsDone/AgentsPending are completion-granular, not live progress: WritePool
// persists every per-agent status and summary.json only after the whole fan-out
// returns, so while a run is in_progress AgentsDone reads 0 and AgentsPending
// reads the full roster size, then both jump to their final values when
// summary.json lands.
type ReviewStatus struct {
	ReviewID      string `json:"review_id"`
	Status        string `json:"status"`
	AgentCount    int    `json:"agent_count"`
	AgentsDone    int    `json:"agents_done"`
	AgentsPending int    `json:"agents_pending"`
	Partial       bool   `json:"partial"`
}

// ReadReviewStatus reports a review's progress without guessing. A missing
// manifest surfaces as os.ErrNotExist (the caller maps it to "review not
// found"); a present-but-corrupt manifest is a parse error (AC 04-04 Edge Case
// 6 — never a partial garbage result). summary.json is the completion signal:
// absent means the fan-out is still running (in_progress) — or, once its
// effective deadline has elapsed, stale; present means completed (≥1 agent
// succeeded) or failed (every agent failed).
//
// Read-pair invariant (TD-023). The manifest and summary are read in two steps,
// manifest-first, while a background fan-out may be writing both. This is
// torn-read-safe by construction, and Task 4's concurrency test pins it:
//   - Each file is written atomically (temp file + rename), so a reader sees a
//     whole file, never a half-written one — a corrupt parse is real corruption,
//     not a mid-write artifact.
//   - manifest.json exists from PrepareReview (scaffold time) through every
//     finalization rewrite; a genuinely absent manifest is os.ErrNotExist, never
//     a false in_progress.
//   - status, partial, and the agent counts derive solely from summary.json. The
//     only fields taken from the manifest (roster size, StartedAt, timeout_secs)
//     are written once at scaffold and are stable across the finalizing rewrite
//     (which mutates Partial and stamps CompletedAt — neither is read here), so
//     whichever manifest version a reader observes yields the same result. Therefore
//     no manifest/summary interleaving can produce a torn-pair misreport: every
//     concurrent read returns a valid state.
func ReadReviewStatus(reviewDir, id string) (*ReviewStatus, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, manifestFile))
	if err != nil {
		return nil, err // os.ErrNotExist → "review not found"; other I/O bubbles up
	}
	var m payload.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest.json is corrupt: %w", err)
	}

	st := &ReviewStatus{
		ReviewID:      id,
		Status:        RunInProgress,
		AgentCount:    len(m.Roster),
		AgentsPending: len(m.Roster),
	}

	// Cap the summary read to prevent an unexpectedly large file from allocating
	// an unbounded heap slice (item TD: reviewdir.go:340). Any read problem
	// (absent file, permission error, oversized) falls through to the same
	// stale-inference branch as a missing summary — past the deadline the review
	// is presumed dead regardless of the cause.
	var sdata []byte
	var serr error
	{
		sf, sfErr := os.Open(filepath.Join(reviewDir, "sources", "pool", summaryFile))
		if sfErr != nil {
			serr = sfErr
		} else {
			sdata, serr = io.ReadAll(io.LimitReader(sf, maxSummaryBytes+1))
			_ = sf.Close()
			if serr == nil && int64(len(sdata)) > maxSummaryBytes {
				serr = fmt.Errorf("summary.json exceeds read cap (%d B)", maxSummaryBytes)
			}
		}
	}
	if serr != nil {
		// No completion signal. The pair is read manifest-first then summary
		// (Task 4 read invariant); a genuinely absent summary means the fan-out
		// is either still running or dead. Infer stale when the manifest's
		// effective deadline (StartedAt + timeout_secs + grace) has passed —
		// otherwise it is honestly still in_progress. Any read error (absent
		// file, permission denied, I/O error) is treated the same way: past the
		// deadline the review is presumed dead regardless of why the completion
		// signal is unreadable; within the window it stays in_progress.
		if staleByDeadline(m) {
			st.Status = RunStale
		}
		return st, nil
	}
	var ps PoolSummary
	if err := json.Unmarshal(sdata, &ps); err != nil {
		return nil, fmt.Errorf("summary.json is corrupt: %w", err)
	}
	if ps.Total > 0 {
		st.AgentCount = ps.Total
	}
	st.AgentsDone = ps.Succeeded + ps.Failed
	st.AgentsPending = st.AgentCount - st.AgentsDone
	if st.AgentsPending < 0 {
		st.AgentsPending = 0
	}
	st.Partial = ps.EffectivePartial()
	if ps.Succeeded > 0 {
		st.Status = RunCompleted
	} else {
		st.Status = RunFailed
	}
	return st, nil
}

// staleByDeadline reports whether a summary-less review has exceeded its
// inferred deadline. It returns false when the deadline is unknowable — a zero
// StartedAt or an absent timeout_secs (old manifests, zero value) — so those
// reviews keep reporting in_progress rather than being guessed dead.
func staleByDeadline(m payload.Manifest) bool {
	if m.TimeoutSecs <= 0 || m.StartedAt.IsZero() {
		return false
	}
	totalSecs := m.TimeoutSecs + staleGraceSecs
	if totalSecs < 0 {
		// int overflow: timeout is beyond representation; deadline is effectively
		// infinite, so the review cannot be stale.
		return false
	}
	d := time.Duration(totalSecs) * time.Second
	if d < 0 {
		// Duration (int64 ns) overflow: totalSecs * 1e9 exceeded ~292 years.
		// Same conclusion — deadline is effectively infinite.
		return false
	}
	deadline := m.StartedAt.Add(d)
	return nowFunc().After(deadline)
}

// ErrReviewInProgress reports a reconcile/report attempt against a review whose
// fan-out has not written its completion signal (summary.json) yet.
var ErrReviewInProgress = errors.New("still in_progress")

// ErrReviewStale reports a reconcile/report attempt against a review inferred
// dead (Epic 1.5): its fan-out exceeded the effective timeout without writing a
// completion signal. Like in_progress it has no summary.json, so reconciling it
// would emit a complete-looking verdict from an incomplete (or empty) agent set
// — but unlike in_progress it will never complete, so the guidance is to re-run
// rather than poll.
var ErrReviewStale = errors.New("stale (fan-out exceeded its timeout without a completion signal)")

// EnsureReviewComplete rejects a fan-out-managed review that is still running,
// so a reconcile cannot read a partially-written agent set and emit
// complete-looking artifacts (and a pass verdict) from a subset of agents. A
// directory with no manifest.json is not fan-out-managed (e.g. a hand-assembled
// review anchored by path from the CLI) and passes the guard; a corrupt
// manifest or summary surfaces as its parse error, never a guessed state.
func EnsureReviewComplete(reviewDir, id string) error {
	st, err := ReadReviewStatus(reviewDir, id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if st.Status == RunInProgress {
		return fmt.Errorf("review %s is %w; poll atcr_status (or run `atcr status`) and reconcile after the fan-out completes", id, ErrReviewInProgress)
	}
	if st.Status == RunStale {
		return fmt.Errorf("review %s is %w; re-run the review", id, ErrReviewStale)
	}
	return nil
}

// AgentStatus is the per-agent status.json record. It is always written —
// regardless of outcome — so post-hoc analysis can see which reviewers
// participated. Truncated/FilesDropped record byte-budget truncation and are
// never silent (AC 06-03): when an agent's payload was truncated, Truncated is
// true and FilesDropped lists the dropped paths.
type AgentStatus struct {
	Agent         string   `json:"agent"`
	Status        string   `json:"status"`
	FindingsCount int      `json:"findings_count"`
	DurationMS    int64    `json:"duration_ms"`
	PayloadMode   string   `json:"payload_mode"`
	Truncated     bool     `json:"truncated"`
	FilesDropped  []string `json:"files_dropped"`
	FallbackUsed  bool     `json:"fallback_used,omitempty"`
	FallbackFrom  string   `json:"fallback_from,omitempty"`
	Error         string   `json:"error,omitempty"`

	// Post-processing enforcement counters (Epic 2.2). Always present so a
	// zero is distinguishable from an older status.json that predates the field.
	DroppedByMinSeverity   int `json:"dropped_by_min_severity"`
	TruncatedByMaxFindings int `json:"truncated_by_max_findings"`

	// Per-agent counters for the agentic stages (Epic 2.0 tool loop). Pointers +
	// omitempty so they are absent from every 1.x status.json (no tool loop ran),
	// yet a tool-enabled agent records an explicit zero even when it degraded or
	// tripped before any tool ran (statusFor sets them iff the agent was
	// tool-enabled). ToolsDegraded marks a tool agent that fell back to
	// single-shot; TrippedBudgets names every budget that halted the loop.
	Turns          *int     `json:"turns,omitempty"`
	ToolCalls      *int     `json:"tool_calls,omitempty"`
	ToolBytes      *int64   `json:"tool_bytes,omitempty"`
	ToolsDegraded  bool     `json:"tools_degraded,omitempty"`
	ToolsRequested bool     `json:"tools_requested,omitempty"`
	TrippedBudgets []string `json:"tripped_budgets,omitempty"`
}

// WriteStatus serializes s to path as indented JSON, writing atomically (temp
// file + rename) so a crash never leaves a half-written status. FilesDropped is
// normalized to a non-nil slice so truncation state is always explicit (never
// silent). Per AC 06-03 a write failure names the agent.
func WriteStatus(path string, s *AgentStatus) error {
	if s.FilesDropped == nil {
		s.FilesDropped = []string{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to write status.json for agent '%s': %w", s.Agent, err)
	}
	if err := atomicWriteFile(path, append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write status.json for agent '%s': %w", s.Agent, err)
	}
	return nil
}

// atomicWriteFile writes data to a sibling temp file then renames it over path.
// The temp is chmod'd to 0644 before the rename so artifacts land with the
// AC 01-03 file mode rather than os.CreateTemp's 0600 default. The temp is
// fsync'd before the rename and the parent directory after it, so a power-loss
// crash cannot leave the rename visible without the data (or lose the file
// entirely) on filesystems that defer metadata flushes.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// Best-effort: directory fsync is unsupported on some platforms (Windows
	// rejects FlushFileBuffers on a read-only directory handle), and the data
	// itself is already durable via the temp-file Sync above.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
