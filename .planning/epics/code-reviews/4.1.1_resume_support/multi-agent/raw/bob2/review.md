We are reviewing the diff for the addition of the `--resume` flag to the `atcr review` command.

We must only flag issues that are in scope according to the sprint plan. The sprint plan is about adding resume functionality.

Let's go through the changes:

1. README.md: Added documentation for the `--resume` flag. This is in scope.

2. cmd/atcr/resume.go: New file implementing the resume logic. This is in scope.

3. cmd/atcr/resume_test.go: New file with tests for the resume functionality. This is in scope.

4. cmd/atcr/review.go: Modified to add the `--resume` flag and branch to the resume flow. This is in scope.

5. cmd/atcr/review_test.go: Modified to update the interrupt message to mention the resume flag. This is in scope.

6. internal/fanout/resume.go: New file with the core resume logic (PrepareResume, ExecuteResume, etc.). This is in scope.

7. internal/fanout/resume_test.go: New file with tests for the resume logic. This is in scope.

8. internal/fanout/review.go: Modified to extract `runEngine` from `ExecuteReview` so that it can be reused by `ExecuteResume`. This is in scope because it's a refactor to support resume.

Now, we must look for any issues in these changes that are in scope.

Let's examine each file for potential issues:

### README.md
- The documentation looks correct and matches the sprint plan.

### cmd/atcr/resume.go
We should check for:
- Proper error handling
- Correct usage of flags
- Adherence to the sprint plan (e.g., AC2, AC3, etc.)

Looking at the code:

In `runResume`:
- It checks for `--id` and `--output-dir` flags and returns an error if they are set with `--resume`. This is correct per the plan (AC2 and the note that `--resume` cannot be combined with `--id` or `--output-dir`).
- It checks for `--fail-on`, `--verify`, `--require-verified` and returns an error if set. This is correct because the plan says these are out of scope for resume (they are for the one-shot gate).
- It resolves the resume directory and then the git range.
- It loads the review config.
- It calls `fanout.PrepareResume` which does the validation (range and roster) and prepares the slots for pending agents.
- If all agents are complete, it clears the interrupted flag and runs reconciliation.
- It runs the pending agents and then reconciles.

Potential issues:
1. In the `runResume` function, when checking for the flags `--fail-on`, `--verify`, `--require-verified`, it returns a usage error (exit 2). However, note that the sprint plan says that the `--resume` flag does not support these. This is correct.

2. The function `usageError` is used to return an error that will be treated as a usage error (exit 2). We must check if `usageError` is defined. It is not defined in this file, but it is likely defined elsewhere (maybe in the same package). Since we don't see the definition, we assume it's present. However, note that the sprint plan does not require us to check unrelated code. But if it's missing, that would be a problem. However, looking at the diff, we don't see the definition of `usageError` in this file. But note that the file `cmd/atcr/resume.go` is new and we don't see the function. However, in the same directory, there might be a helper. Since we are only reviewing the diff, and the diff doesn't show the definition, we must assume it's defined in the same package or imported. But wait, the file does not import any local package for `usageError`. Let's see the imports:

```go
import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/gitrange"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/spf13/cobra"
)
```

We don't see `usageError` imported. However, note that the function `runResume` is in the `main` package (since it's in `cmd/atcr`). The `usageError` function might be defined in the same file? But we don't see it in the diff. Alternatively, it might be defined in another file in the same package that is not shown in the diff. Since we are only reviewing the diff, we cannot see if it's defined elsewhere. However, note that the same error handling pattern is used in the existing `runReview` function (in `review.go`). We can assume that `usageError` is defined in the same package (maybe in `review.go` or a shared file). But since we are not given the entire codebase, we must rely on the diff.

Alternatively, note that in `runResume` we see:

```go
	if cmd.Flags().Changed("id") || cmd.Flags().Changed("output-dir") {
		return usageError(errors.New("--resume cannot be combined with --id or --output-dir"))
	}
```

And similarly for the other flags. If `usageError` is not defined, this would be a compilation error. However, the sprint plan does not require us to check for compilation errors? Actually, we are to look for issues in the code. A missing function would be an issue.

But wait, let's look at the existing `review.go` file (which we have in the diff). In the diff for `review.go`, we don't see `usageError` defined there either. However, note that the `runReview` function in `review.go` (which is not changed in this diff except for the added resume branch) might be using `usageError` from somewhere else.

Given that we are only reviewing the diff, and the diff does not show the definition of `usageError`, we cannot confirm if it's present. However, note that the same pattern is used in the `resume_test.go` file? We don't see it there either.

But observe: in the `resume_test.go` file, we see calls to `execResume` which in turn calls `root.ExecuteContext`. The tests are checking for exit code 2 and containing certain strings. This suggests that the `usageError` function is defined and returns an error that causes the program to exit with code 2 and print the error.

Since we are not given the entire codebase, and the sprint plan does not require us to check for missing dependencies (unless they are in the scope of the sprint), we might let this pass. However, note that the sprint plan says we are to add the `--resume` flag and related functionality. If the `usageError` function is missing, then the code won't compile. But we are not seeing the entire codebase.

Alternatively, note that the `cmd/atcr` package might have a `usageError` function defined in another file that is not part of this diff. Since we are only reviewing the diff, we cannot see it. Therefore, we must assume that it is present because the existing code (like in `review.go`) might be using it and we are not changing that.

However, let's look at the diff for `review.go`: we don't see any usage of `usageError` in the diff. But note that the `runReview` function in `review.go` (which is not changed in this diff) might be using it. We are not given the entire `review.go` file, only the diff. So we cannot be sure.

Given the constraints, we will not flag this as an issue because we cannot confirm from the diff alone that `usageError` is missing. It might be defined elsewhere in the same package.

### cmd/atcr/resume_test.go
This file contains tests for the resume functionality. We should check for:
- Correct test setup
- Proper assertions

Looking at the tests, they seem to be testing the expected behavior (e.g., incompatible flags, range mismatch, roster mismatch, all complete, pending agent, etc.). They appear to be correct.

### cmd/atcr/review.go
Changes:
- Added the `--resume` flag.
- In `runReview`, added a branch to call `runResume` if the `--resume` flag is changed.

We should check:
- The flag is added correctly.
- The branching is correct.

The flag is added as:
```go
	cmd.Flags().String("resume", "", "resume an interrupted/failed review (latest | <id> | <path>): run only pending agents into the existing directory, then reconcile")
```

In `runReview`:
```go
	if cmd.Flags().Changed("resume") {
		anchor, _ := cmd.Flags().GetString("resume")
		return runResume(cmd, anchor)
	}
```

This looks correct.

### cmd/atcr/review_test.go
Changed the interrupt message test to expect the resume flag to be mentioned. This is correct per the sprint plan (epic 4.1.1 makes --resume real).

### internal/fanout/resume.go
This is the core of the resume functionality. We must check for:
- Correct validation of range and roster (AC3)
- Correct handling of completed agents (AC4)
- Correct handling of all complete (AC2)
- Correct handling of interruption during resume (AC7)
- Correct updating of manifest and status (AC6)
- Correct reconciliation after resume (AC1, AC5, etc.)

Let's look for potential issues:

1. In `PrepareResume`:
   - It reads the manifest.
   - Validates the range and roster.
   - Builds payloads and slots.
   - Then it calls `CompletedAgents` to get the set of completed agents.
   - Then it builds the `ResumeInfo` by iterating over the configured roster and checking if each agent is in the completed set.

   However, note: the configured roster might have changed? But we already validated the roster in `ValidateResumeRoster`. So the configured roster is the same set as the manifest's roster.

   But note: the `CompletedAgents` function returns a map of agent names that have a status.json with StatusOK. Then we build the `ResumeInfo` by:
   - For each name in the configured roster, if it's in the completed set, add to Completed; else, add to Pending.

   This is correct.

2. In `ExecuteResume`:
   - It runs the engine on the pending slots.
   - Then it writes the results for the resumed agents (via `writeResumedAgents`).
   - Then it rebuilds the pool (via `RebuildPool`) which reads all agent artifacts (completed and newly resumed) and rebuilds the summary.json and findings.txt.
   - Then it updates the manifest (with the new CompletedAt, Partial, and Interrupted flags) and writes it.

   This seems correct.

3. In `writeResumedAgents`:
   - It writes the agent artifacts (review.md, findings.txt, status.json) for each result in the resumed agents.

   This is correct.

4. In `RebuildPool`:
   - It reads all agent directories under `poolDir/raw/agent`.
   - For each, it reads the status.json. If it's missing or unparseable, it skips the agent (so it's not included in the union).
   - If the status.json is present and parseable, it adds the status to the statuses list.
   - Then, if the findings.txt is present and parseable, it adds the findings to the merged list.

   This is correct because we want to rebuild the pool from the union of all on-disk agent artifacts (completed and newly resumed).

5. The function `CompletedAgents`:
   - It reads the `sources/pool/raw/agent` directory.
   - For each subdirectory (agent), it tries to read the status.json and checks if the status is StatusOK.
   - If the directory doesn't exist, it returns an empty map (no error).

   This is correct per the sprint plan: an agent is complete only if status.json records "ok".

6. The function `agentStatusName`:
   - It reads the status.json and returns the agent name only if the status is StatusOK and the agent name is non-empty.

   This is correct.

7. The function `ValidateResumeRange` and `ValidateResumeRoster`:
   - They return errors if the range or roster has changed.

   This is correct per AC3 and the roster lock.

8. The function `ClearInterrupted`:
   - It clears the interrupted flag in the manifest if set.

   This is used when all agents are complete to change the status from interrupted to completed.

   This is correct per AC6.

9. The function `ResumeInfo.AllComplete`:
   - Returns true if there are no pending agents.

   This is used in `runResume` to skip the fan-out and go straight to reconciliation.

   This is correct per AC2.

Now, let's look for specific issues:

### Potential issue in `PrepareResume`:
In the loop that builds the `ResumeInfo`:
```go
	for _, name := range configured {
		if done[name] {
			info.Completed = append(info.Completed, name)
		} else {
			info.Pending = append(info.Pending, name)
		}
	}
```
This is correct because `configured` is the roster from the config (which we validated is the same set as the manifest's roster).

### Potential issue in `ExecuteResume`:
After running the engine on the pending slots, we call `writeResumedAgents` to write the artifacts for the resumed agents. Then we call `RebuildPool` to rebuild the pool summary and findings.

However, note that the `RebuildPool` function reads the status.json and findings.txt for every agent. This includes the agents that were already completed (whose artifacts were not touched) and the resumed agents (whose artifacts we just wrote). This is correct.

### Potential issue in `RebuildPool`:
It skips agents that don't have a readable/parseable status.json. This is correct because such agents are not considered completed.

But note: what if an agent's status.json is present and parseable but the findings.txt is missing or unparseable? Then we skip the findings for that agent (so it doesn't contribute to the merged findings) but we still count the agent in the statuses (for the summary). This is correct because the agent completed (status ok) but had no findings (or we couldn't read the findings). The sprint plan says that an agent that found nothing is complete.

### Potential issue in `writeResumedAgents`:
It calls `agentDirName` to get the directory name for the agent. We must check if this function is defined and correct. However, we don't see the definition of `agentDirName` in this file. But note that this function is likely defined elsewhere in the fanout package. Since we are not given the entire fanout package, we assume it's present.

Alternatively, note that in the existing `fanout/review.go` (which we see in the diff) there is a function `agentDirName`? We don't see it in the diff for `review.go`. But we see in the diff for `resume.go` that we are using `agentDirName`. We must check if it's defined.

Looking at the diff for `internal/fanout/resume.go`, we don't see the function `agentDirName`. However, we see that it is used in `writeResumedAgents`:

```go
	dir, err := agentDirName(r.Agent)
	if err != nil {
		return err
	}
```

This function is not defined in the diff. But note that the same function might be defined in the existing `fanout/review.go` or another file. However, we are not given the entire `fanout/review.go` in the diff, only a part of it.

But wait, in the diff for `internal/fanout/review.go`, we see:
```go
	// Wire the read-only tool harness only when a slot is tool-enabled: a snapshot
	// of the repo at head → path jail → dispatcher, shared across the run, plus a
	// per-agent transcript writer under the pool raw dir. Best-effort: a snapshot
	// or jail failure logs and leaves tool agents to degrade to single-shot
	// (tools_degraded) rather than failing the whole review.
```

We don't see `agentDirName` there.

However, note that the function `agentDirName` might be defined in the same file (`resume.go`) but outside the diff we are seeing? But the diff for `resume.go` shows the entire file (since it's a new file). We don't see the function.

This is a problem: the function `agentDirName` is used but not defined in the file. Therefore, the code will not compile.

Let me check the diff again for `internal/fanout/resume.go`: we see the entire file because it's a new file. The function `agentDirName` is not defined.

Therefore, we have found an issue: missing function `agentDirName`.

But wait, note that in the same file, we see a function `findingsFor` and `writeAgentArtifacts` are used. We don't see their definitions either. However, note that the file `internal/fanout/resume.go` does define `writeAgentArtifacts`? Let's look:

We see:
```go
	// writeResumedAgents persists the per-agent artifacts (review.md, findings.txt,
	// status.json) for each re-run result. A re-run agent's prior (failed/timeout)
	// artifacts are overwritten in place; completed agents are not in results, so
	// their artifacts on disk are left untouched.
+func writeResumedAgents(poolDir string, results []Result) error {
+	for _, r := range results {
+		dir, err := agentDirName(r.Agent)
+		if err != nil {
+			return err
+		}
+		fr := findingsFor(r)
+		if err := writeAgentArtifacts(poolDir, dir, r, fr); err != nil {
+			return err
+		}
+	}
+	return nil
+}
```

We don't see the definitions of `agentDirName`, `findingsFor`, or `writeAgentArtifacts` in this file.

However, note that the file `internal/fanout/review.go` (which we see in the diff) might have these functions? We are given a diff for `review.go` but it only shows a portion of the file (the changes). We don't see the entire file.

But note: the function `writeAgentArtifacts` is likely defined in the fanout package and used in the existing `ExecuteReview` function. Since we are not changing that, we assume it exists.

However, the function `agentDirName` and `findingsFor` are not visible in the diff for `review.go` either.

Given that we are only reviewing the diff, and we don't see these functions defined in the resume.go file, and we don't see them in the review.go diff (which only shows a part of the file), we cannot be sure.

But note: the sprint plan does not require us to write new helper functions? We are to implement the resume functionality. If these helper functions are missing, then the code is incomplete.

Alternatively, note that the existing `fanout/review.go` might have these functions. Let's look at the diff for `internal/fanout/review.go` again: we see that the function `ExecuteReview` is changed to use `runEngine`. We don't see the helper functions.

However, we do see in the diff for `internal/fanout/review.go` the following line (in the context of the old `ExecuteReview`):
```go
	poolDir := filepath.Join(p.Dir, "sources", "pool")
```

And then later:
```go
	// Write the per-agent artifacts (review.md, findings.txt, status.json) for each agent.
+	if err := writeAgentArtifacts(poolDir, dir, r, fr); err != nil {
+		return err
+	}
```

Wait, no, we don't see that in the diff for `review.go`. Actually, the diff for `review.go` only shows the changes we made. We don't see the entire function.

Given the ambiguity, we must rely on the fact that the same helper functions are used in the existing code and are therefore present. However, the function `agentDirName` is not obviously present.

But note: in the `resume_test.go` file, we see:
```go
	// writeAgentStatusFixture scaffolds sources/pool/raw/agent/<agent>/status.json with
	// the given outcome so resume-state tests exercise the real on-disk layout.
+func writeAgentStatusFixture(t *testing.T, reviewDir, agent, status string) {
+	t.Helper()
+	dir := filepath.Join(reviewDir, "sources", "pool", poolRawAgentDir, agent)
+	if err := os.MkdirAll(dir, 0o755); err != nil {
+		t.Fatal(err)
+	}
+	st := &AgentStatus{Agent: agent, Status: status}
+	if err := WriteStatus(filepath.Join(dir, statusFile), st); err != nil {
+		t.Fatal(err)
+	}
+}
```

This shows that the path for an agent's directory is: `filepath.Join(reviewDir, "sources", "pool", poolRawAgentDir, agent)`

And we see that `poolRawAgentDir` is defined in the resume.go file? We don't see it in the diff for resume.go, but note that in the same file we see:
```go
	const (
		manifestFile   = "manifest.json"
		statusFile     = "status.json"
		findingsFile   = "findings.txt"
		reviewFile     = "review.md"
		poolRawAgentDir = "raw/agent"
	)
```

Wait, we do see `poolRawAgentDir` defined in the resume.go file? Let me check the diff for `internal/fanout/resume.go`:

We see:
```go
+package fanout
+
+import (
+	"context"
+	"encoding/json"
+	"errors"
+	"fmt"
+	"io/fs"
+	"os"
+	"path/filepath"
+	"sort"
+	"strings"
+	"time"
+
+	"github.com/samestrin/atcr/internal/payload"
+	"github.com/samestrin/atcr/internal/stream"
+)
+
+// ErrRangeChanged reports that the working tree's resolved git range no longer
+// matches the range the interrupted review recorded in manifest.json. Resuming
+// would fan the pending agents out at a different base/head than the completed
+// ones reviewed, mixing inconsistent contexts — so resume aborts (exit 2) and
+// the user must start a fresh `atcr review` (epic 4.1.1 AC3).
+var ErrRangeChanged = errors.New("the working tree changed since the interrupted review (git base/head differ)")
+
+// ErrRosterChanged reports that the currently configured agent roster differs
+// from the roster the interrupted review recorded. The panel configuration is
+// locked for a resume (epic 4.1.1 Open Question #2 / out-of-scope): a changed
+// roster aborts (exit 2) rather than silently resuming a different panel.
+var ErrRosterChanged = errors.New("the configured roster changed since the interrupted review")
+
+// ValidateResumeRange verifies the manifest's recorded range matches the range
+// resolved from the current working tree. Base and head are compared as the
+// already-resolved SHAs gitrange.Resolve produced (manifest.json stores them
+// verbatim), so an equal pair proves the pending agents will review exactly what
+// the completed agents did.
+func ValidateResumeRange(m *payload.Manifest, cur ReviewRange) error {
+	if m.Base != cur.Base || m.Head != cur.Head {
+		return fmt.Errorf("%w: recorded %s..%s, current %s..%s; start a fresh `atcr review`",
+			ErrRangeChanged, shortRef(m.Base), shortRef(m.Head), shortRef(cur.Base), shortRef(cur.Head))
+	}
+	return nil
+}
+
+// ValidateResumeRoster verifies the configured roster is the same SET of agent
+// names the interrupted review recorded (order-independent — manifest.Roster is
+// an ordered snapshot but the roster is semantically a set). Any added, removed,
+// or swapped agent fails closed.
+func ValidateResumeRoster(m *payload.Manifest, configured []string) error {
+	recorded := nameSet(m.Roster)
+	current := nameSet(configured)
+	if len(recorded) != len(current) {
+		return rosterMismatch(m.Roster, configured)
+	}
+	for name := range recorded {
+		if !current[name] {
+			return rosterMismatch(m.Roster, configured)
+		}
+	}
+	return nil
+}
+
+// nameSet collapses a roster slice to a presence set.
+func nameSet(names []string) map[string]bool {
+	s := make(map[string]bool, len(names))
+	for _, n := range names {
+		s[n] = true
+	}
+	return s
+}
+
+// rosterMismatch renders the ErrRosterChanged error with both rosters sorted so
+// the diff is legible regardless of declaration order.
+func rosterMismatch(recorded, configured []string) error {
+	r := append([]string(nil), recorded...)
+	c := append([]string(nil), configured...)
+	sort.Strings(r)
+	sort.Strings(c)
+	return fmt.Errorf("%w: recorded [%s], configured [%s]; start a fresh `atcr review`",
+		ErrRosterChanged, strings.Join(r, " "), strings.Join(c, " "))
+}
+
+// shortRef trims a git SHA to 12 chars for diagnostics, leaving shorter or
+// symbolic refs intact.
+func shortRef(ref string) string {
+	if len(ref) > 12 {
+		return ref[:12]
+	}
+	return ref
+}
+
+// CompletedAgents scans a review's per-agent status records and returns the set
+// of agent names that finished successfully (status == StatusOK), so a resumed
+// run can skip them. An agent is treated as PENDING — and therefore re-run —
+// when its status.json is missing, unreadable, corrupt, or records a non-OK
+// outcome (StatusFailed / StatusTimeout). This is the authoritative completion
+// signal: WritePool stamps StatusOK regardless of findings count, so a clean
+// reviewer that found nothing is correctly "complete", while a failed agent —
+// which writes an identical empty findings.txt — is correctly "pending"
+// (resolves epic 4.1.1 Open Question #1).
+//
+// A missing pool tree (a review scaffolded but never fanned out) yields an empty
+// set with no error: every roster agent is pending.
+func CompletedAgents(reviewDir string) (map[string]bool, error) {
+	rawDir := filepath.Join(reviewDir, "sources", "pool", poolRawAgentDir)
+	entries, err := os.ReadDir(rawDir)
+	if err != nil {
+		if errors.Is(err, fs.ErrNotExist) {
+			return map[string]bool{}, nil
+		}
+		return nil, err
+	}
+	done := make(map[string]bool, len(entries))
+	for _, e := range entries {
+		if !e.IsDir() {
+			continue
+		}
+		name, ok := agentStatusName(filepath.Join(rawDir, e.Name(), statusFile))
+		if ok {
+			done[name] = true
+		}
+	}
+	return done, nil
+}
+
+// agentStatusName reads a per-agent status.json and returns the agent name when
+// the record is readable, parseable, and reports StatusOK. Any failure to read
+// or parse, or any non-OK outcome, returns ok=false so the agent stays pending
+// — re-running an agent is always safe, so an untrustworthy record never causes
+// a skip. The name comes from the record's Agent field (the engine's
+// authoritative value), not the directory name (which is a sanitized basename).
+func agentStatusName(path string) (string, bool) {
+	data, err := os.ReadFile(path)
+	if err != nil {
+		return "", false
+	}
+	var st AgentStatus
+	if json.Unmarshal(data, &st) != nil {
+		return "", false
+	}
+	if st.Status != StatusOK || st.Agent == "" {
+		return "", false
+	}
+	return st.Agent, true
+}
+
+// ReadManifest loads and parses a review's manifest.json. A missing manifest
+// means the directory is not a fan-out-managed review (so it cannot be resumed);
+// a present-but-corrupt manifest surfaces as a parse error rather than a guessed
+// state.
+func ReadManifest(reviewDir string) (*payload.Manifest, error) {
+	data, err := os.ReadFile(filepath.Join(reviewDir, manifestFile))
+	if err != nil {
+		if errors.Is(err, fs.ErrNotExist) {
+			return nil, fmt.Errorf("%s has no manifest.json: not a resumable review (run a fresh `atcr review`)", reviewDir)
+		}
+		return nil, err
+	}
+	var m payload.Manifest
+	if err := json.Unmarshal(data, &m); err != nil {
+		return nil, fmt.Errorf("manifest.json is corrupt: %w", err)
+	}
+	return &m, nil
+}
+
+// ClearInterrupted rewrites the review's manifest with Interrupted=false when it
+// is currently set. A review whose every agent already finished but whose manifest
+// still carries the interrupt marker (a signal that landed after the last agent
+// wrote ok, before manifest finalization) would otherwise keep deriving to
+// "interrupted" forever; clearing the marker when a resume confirms the roster is
+// complete lets it report "completed" (epic 4.1.1 AC6). It is a no-op (and writes
+// nothing) when the manifest is not marked interrupted.
+func ClearInterrupted(reviewDir string) error {
+	m, err := ReadManifest(reviewDir)
+	if err != nil {
+		return err
+	}
+	if !m.Interrupted {
+		return nil
+	}
+	m.Interrupted = false
+	return WriteManifest(reviewDir, m)
+}
+
+// ResumeInfo reports how a resume run partitioned the locked roster: the agents
+// already completed (skipped) and the agents that will be re-run.
+type ResumeInfo struct {
+	Completed []string
+	Pending   []string
+}
+
+// AllComplete reports whether every roster agent already finished, so the caller
+// can skip the fan-out entirely and go straight to reconciliation (epic 4.1.1 AC2).
+func (r *ResumeInfo) AllComplete() bool { return len(r.Pending) == 0 }
+
+// filterPendingSlots keeps only the slots whose primary agent is not already in
+// the completed set, so a resumed fan-out re-runs only the pending/failed agents
+// (epic 4.1.1 AC4).
+func filterPendingSlots(slots []Slot, done map[string]bool) []Slot {
+	pending := make([]Slot, 0, len(slots))
+	for _, s := range slots {
+		if !done[s.Primary.Name] {
+			pending = append(pending, s)
+		}
+	}
+	return pending
+}
+
+// PrepareResume validates an existing review directory against the current
+// working tree and configured roster, then assembles a PreparedReview whose Dir
+// is that existing directory and whose Slots are only the pending agents. The
+// range and roster are locked: a changed git range (ErrRangeChanged) or a
+// changed roster set (ErrRosterChanged) aborts before any agent runs, so a resume
+// can never mix inconsistent contexts or silently run a different panel. Payloads
+// are rebuilt from the (validated-identical) recorded range so pending agents see
+// exactly what the completed agents reviewed. The returned ResumeInfo reports the
+// completed/pending split; when AllComplete is true the Slots are empty and the
+// caller reconciles without a fan-out.
+func PrepareResume(ctx context.Context, cfg *ReviewConfig, reviewDir string, req ReviewRequest) (*PreparedReview, *ResumeInfo, error) {
+	m, err := ReadManifest(reviewDir)
+	if err != nil {
+		return nil, nil, err
+	}
+	if err := ValidateResumeRange(m, req.Range); err != nil {
+		return nil, nil, err
+	}
+	configured := rosterNames(cfg.Project)
+	if err := ValidateResumeRoster(m, configured); err != nil {
+		return nil, nil, err
+	}
+
+	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
+	if err != nil {
+		return nil, nil, err
+	}
+	slots, _, err := buildSlots(cfg, payloads, req.Range)
+	if err != nil {
+		return nil, nil, err
+	}
+
+	done, err := CompletedAgents(reviewDir)
+	if err != nil {
+		return nil, nil, err
+	}
+
+	info := &ResumeInfo{}
+	for _, name := range configured {
+		if done[name] {
+			info.Completed = append(info.Completed, name)
+		} else {
+			info.Pending = append(info.Pending, name)
+		}
+	}
+
+	p := &PreparedReview{
+		ID:          filepath.Base(reviewDir),
+		Dir:         reviewDir,
+		Slots:       filterPendingSlots(slots, done),
+		TimeoutSec:  cfg.Settings.TimeoutSecs,
+		MaxParallel: cfg.Settings.MaxParallel,
+		Repo:        req.Repo,
+		Head:        req.Range.Head,
+		manifest:    m,
+	}
+	return p, info, nil
+}
+
+// ExecuteResume runs the pending slots, persists their per-agent artifacts (the
+// already-completed agents' artifacts on disk are untouched), then rebuilds
+// summary.json and the merged findings.txt over the FULL on-disk union so the
+// aggregate reflects the whole roster — not just the re-run subset. The manifest
+// is finalized with the union's partial flag and the interrupt marker: if the
+// resume is itself interrupted (AC7), whatever pending agents completed are
+// preserved and the run stays interrupted. The all-agents-failed gate is judged
+// over the union, so a resume whose pending agents all fail again still returns
+// success when an earlier completed agent succeeded.
+func ExecuteResume(ctx context.Context, completer Completer, p *PreparedReview) (*ReviewResult, error) {
+	poolDir := filepath.Join(p.Dir, "sources", "pool")
+
+	results, _ := runEngine(ctx, completer, p, poolDir)
+	interrupted := errors.Is(ctx.Err(), context.Canceled)
+
+	if err := writeResumedAgents(poolDir, results); err != nil {
+		return nil, err
+	}
+
+	sum, statuses, err := RebuildPool(poolDir)
+	if err != nil {
+		return nil, err
+	}
+
+	// Finalize the manifest into a local copy (only adopted on a successful write).
+	// m.Review is preserved from the original run: the roster is locked, so the
+	// original review stage already lists every tool agent (reviewStageFor records
+	// ToolsRequested even on a failed agent), and the resumed subset cannot add new
+	// members.
+	m := *p.manifest
+	m.Partial = sum.Partial
+	m.CompletedAt = time.Now().UTC()
+	m.Interrupted = interrupted
+	if err := WriteManifest(p.Dir, &m); err != nil {
+		return nil, err
+	}
+	p.manifest = &m
+
+	res := &ReviewResult{ID: p.ID, Dir: p.Dir, Summary: sum}
+	if sum.Total == 0 {
+		return res, ErrEmptyRoster
+	}
+	if sum.Succeeded == 0 {
+		return res, fmt.Errorf("%w: %s", ErrAllAgentsFailed, formatStatusFailures(statuses))
+	}
+	return res, nil
+}
+
+// writeResumedAgents persists the per-agent artifacts (review.md, findings.txt,
+// status.json) for each re-run result