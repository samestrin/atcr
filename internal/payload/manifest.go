package payload

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Manifest is the per-review provenance record written to manifest.json so
// every downstream tool reads what reviewers saw from disk rather than
// re-deriving it. It records the resolved range, the default payload mode, the
// per-agent payload map (who saw what), the roster, timestamps, and the
// partial-success flag.
type Manifest struct {
	Base            string            `json:"base"`
	Head            string            `json:"head"`
	DetectionMode   string            `json:"detection_mode"`
	DefaultBranch   string            `json:"default_branch,omitempty"`
	CommitCount     int               `json:"commit_count"`
	PayloadMode     string            `json:"payload_mode"`
	PerAgentPayload map[string]string `json:"per_agent_payload"`
	Roster          []string          `json:"roster"`
	StartedAt       time.Time         `json:"started_at"`
	CompletedAt     time.Time         `json:"completed_at,omitempty"`
	Partial         bool              `json:"partial"`

	// Stages records which review stages ran. Reserved for the agentic stages
	// (Epics 3.0–5.0): 1.x records ["review"]; later runs append "verify",
	// "debate", etc. Optional so a manifest written without it parses cleanly.
	Stages []string `json:"stages,omitempty"`
}

// WriteManifest serializes m to path as indented JSON, writing atomically
// (temp file + rename) so a crash never leaves a half-written manifest. A
// write failure is surfaced with the AC-mandated message (06-03 Error
// Scenario 2). A nil PerAgentPayload is normalized to {} so the field
// marshals as an object, never null.
func WriteManifest(path string, m *Manifest) error {
	if m.PerAgentPayload == nil {
		m.PerAgentPayload = map[string]string{}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to write manifest.json: %w", err)
	}
	if err := atomicWriteFile(path, append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write manifest.json: %w", err)
	}
	return nil
}

// atomicWriteFile writes data to a sibling temp file then renames it over path,
// so a reader never observes a partially-written file. The temp is chmod'd to
// 0644 before the rename so the artifact lands with the AC 01-03 file mode
// (matching internal/fanout's copy) rather than os.CreateTemp's 0600 default.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
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
