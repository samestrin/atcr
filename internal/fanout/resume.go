package fanout

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// CompletedAgents scans a review's per-agent status records and returns the set
// of agent names that finished successfully (status == StatusOK), so a resumed
// run can skip them. An agent is treated as PENDING — and therefore re-run —
// when its status.json is missing, unreadable, corrupt, or records a non-OK
// outcome (StatusFailed / StatusTimeout). This is the authoritative completion
// signal: WritePool stamps StatusOK regardless of findings count, so a clean
// reviewer that found nothing is correctly "complete", while a failed agent —
// which writes an identical empty findings.txt — is correctly "pending"
// (resolves epic 4.1.1 Open Question #1).
//
// A missing pool tree (a review scaffolded but never fanned out) yields an empty
// set with no error: every roster agent is pending.
func CompletedAgents(reviewDir string) (map[string]bool, error) {
	rawDir := filepath.Join(reviewDir, "sources", "pool", poolRawAgentDir)
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	done := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name, ok := agentStatusName(filepath.Join(rawDir, e.Name(), statusFile))
		if ok {
			done[name] = true
		}
	}
	return done, nil
}

// agentStatusName reads a per-agent status.json and returns the agent name when
// the record is readable, parseable, and reports StatusOK. Any failure to read
// or parse, or any non-OK outcome, returns ok=false so the agent stays pending
// — re-running an agent is always safe, so an untrustworthy record never causes
// a skip. The name comes from the record's Agent field (the engine's
// authoritative value), not the directory name (which is a sanitized basename).
func agentStatusName(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var st AgentStatus
	if json.Unmarshal(data, &st) != nil {
		return "", false
	}
	if st.Status != StatusOK || st.Agent == "" {
		return "", false
	}
	return st.Agent, true
}
