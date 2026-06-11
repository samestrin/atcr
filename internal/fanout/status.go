package fanout

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Agent outcome values written to status.json. A malformed LLM response yields
// StatusFailed with the parse error recorded in AgentStatus.Error.
const (
	StatusOK      = "ok"
	StatusFailed  = "failed"
	StatusTimeout = "timeout"
)

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
// AC 01-03 file mode rather than os.CreateTemp's 0600 default.
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
