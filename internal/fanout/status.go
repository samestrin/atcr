package fanout

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/payload"
)

// Agent outcome values written to status.json. A malformed LLM response yields
// StatusFailed with the parse error recorded in AgentStatus.Error.
const (
	StatusOK      = "ok"
	StatusFailed  = "failed"
	StatusTimeout = "timeout"
)

// Review run states reported by ReadReviewStatus (AC 04-04): in_progress until
// the fan-out writes summary.json, then completed (≥1 agent ok) or failed (all
// agents failed).
const (
	RunInProgress = "in_progress"
	RunCompleted  = "completed"
	RunFailed     = "failed"
)

// ReviewStatus is a review's aggregated progress, derived from manifest.json
// (the roster) and sources/pool/summary.json (the completion signal). It is the
// shared shape returned to the `atcr status` CLI command and the atcr_status MCP
// tool so both report identical state.
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
// absent means the fan-out is still running (in_progress); present means
// completed (≥1 agent succeeded) or failed (every agent failed).
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

	sdata, serr := os.ReadFile(filepath.Join(reviewDir, "sources", "pool", summaryFile))
	if serr != nil {
		return st, nil // no pool summary yet → still in progress
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
	st.Partial = ps.Partial
	if ps.Succeeded > 0 {
		st.Status = RunCompleted
	} else {
		st.Status = RunFailed
	}
	return st, nil
}

// ErrReviewInProgress reports a reconcile/report attempt against a review whose
// fan-out has not written its completion signal (summary.json) yet.
var ErrReviewInProgress = errors.New("still in_progress")

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
