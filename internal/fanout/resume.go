package fanout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/payload"
)

// ErrRangeChanged reports that the working tree's resolved git range no longer
// matches the range the interrupted review recorded in manifest.json. Resuming
// would fan the pending agents out at a different base/head than the completed
// ones reviewed, mixing inconsistent contexts — so resume aborts (exit 2) and
// the user must start a fresh `atcr review` (epic 4.1.1 AC3).
var ErrRangeChanged = errors.New("the working tree changed since the interrupted review (git base/head differ)")

// ErrRosterChanged reports that the currently configured agent roster differs
// from the roster the interrupted review recorded. The panel configuration is
// locked for a resume (epic 4.1.1 Open Question #2 / out-of-scope): a changed
// roster aborts (exit 2) rather than silently resuming a different panel.
var ErrRosterChanged = errors.New("the configured roster changed since the interrupted review")

// ValidateResumeRange verifies the manifest's recorded range matches the range
// resolved from the current working tree. Base and head are compared as the
// already-resolved SHAs gitrange.Resolve produced (manifest.json stores them
// verbatim), so an equal pair proves the pending agents will review exactly what
// the completed agents did.
func ValidateResumeRange(m *payload.Manifest, cur ReviewRange) error {
	if m.Base != cur.Base || m.Head != cur.Head {
		return fmt.Errorf("%w: recorded %s..%s, current %s..%s; start a fresh `atcr review`",
			ErrRangeChanged, shortRef(m.Base), shortRef(m.Head), shortRef(cur.Base), shortRef(cur.Head))
	}
	return nil
}

// ValidateResumeRoster verifies the configured roster is the same SET of agent
// names the interrupted review recorded (order-independent — manifest.Roster is
// an ordered snapshot but the roster is semantically a set). Any added, removed,
// or swapped agent fails closed.
func ValidateResumeRoster(m *payload.Manifest, configured []string) error {
	recorded := nameSet(m.Roster)
	current := nameSet(configured)
	if len(recorded) != len(current) {
		return rosterMismatch(m.Roster, configured)
	}
	for name := range recorded {
		if !current[name] {
			return rosterMismatch(m.Roster, configured)
		}
	}
	return nil
}

// nameSet collapses a roster slice to a presence set.
func nameSet(names []string) map[string]bool {
	s := make(map[string]bool, len(names))
	for _, n := range names {
		s[n] = true
	}
	return s
}

// rosterMismatch renders the ErrRosterChanged error with both rosters sorted so
// the diff is legible regardless of declaration order.
func rosterMismatch(recorded, configured []string) error {
	r := append([]string(nil), recorded...)
	c := append([]string(nil), configured...)
	sort.Strings(r)
	sort.Strings(c)
	return fmt.Errorf("%w: recorded [%s], configured [%s]; start a fresh `atcr review`",
		ErrRosterChanged, strings.Join(r, " "), strings.Join(c, " "))
}

// shortRef trims a git SHA to 12 chars for diagnostics, leaving shorter or
// symbolic refs intact.
func shortRef(ref string) string {
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}

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
