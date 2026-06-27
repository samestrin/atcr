package fanout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/cache"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
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
		statusPath := filepath.Join(rawDir, e.Name(), statusFile)
		name, ok, err := agentStatusName(rawDir, statusPath)
		if err != nil {
			// An existing status.json that won't read or parse is a real anomaly
			// (distinct from a legitimately-pending agent): surface it so the
			// corruption is greppable instead of a silent re-run.
			slog.Warn("corrupt agent status", "path", statusPath, "err", err)
		}
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
//
// The returned error is non-nil only when an EXISTING status.json is unreadable
// or unparseable (corruption worth surfacing to an operator); a missing file
// (the legitimately-pending case), a symlink-escape, or a non-OK/empty record
// returns a nil error.
func agentStatusName(root, path string) (string, bool, error) {
	// Reject a status.json that resolves outside the review tree (symlink
	// traversal): an out-of-tree file the review never produced must never be
	// trusted as a completion record. Failing the containment check keeps the
	// agent pending, which is always safe (a pending agent simply re-runs).
	if !pathWithin(root, path) {
		return "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// A missing status.json is the legitimately-pending case (the agent
		// never started) — not corruption, so stay silent. Any other read error
		// means on-disk state exists but is unreadable: surface it.
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	var st AgentStatus
	if err := json.Unmarshal(data, &st); err != nil {
		return "", false, err
	}
	if st.Status != StatusOK || st.Agent == "" {
		return "", false, nil
	}
	return st.Agent, true, nil
}

// pathWithin reports whether path, with all symlinks resolved, stays inside
// root (also symlink-resolved). It guards artifact reads against symlink
// traversal — a path escaping the review tree returns false. A path that does
// not exist (or a broken symlink) also returns false: it cannot be read, so it
// is never trusted.
func pathWithin(root, path string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ReadManifest loads and parses a review's manifest.json. A missing manifest
// means the directory is not a fan-out-managed review (so it cannot be resumed);
// a present-but-corrupt manifest surfaces as a parse error rather than a guessed
// state.
func ReadManifest(reviewDir string) (*payload.Manifest, error) {
	data, err := os.ReadFile(filepath.Join(reviewDir, manifestFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s has no manifest.json: not a resumable review (run a fresh `atcr review`)", reviewDir)
		}
		return nil, err
	}
	var m payload.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest.json is corrupt: %w", err)
	}
	return &m, nil
}

// ClearInterrupted rewrites the review's manifest with Interrupted=false when it
// is currently set. A review whose every agent already finished but whose manifest
// still carries the interrupt marker (a signal that landed after the last agent
// wrote ok, before manifest finalization) would otherwise keep deriving to
// "interrupted" forever; clearing the marker when a resume confirms the roster is
// complete lets it report "completed" (epic 4.1.1 AC6). It is a no-op (and writes
// nothing) when the manifest is not marked interrupted.
func ClearInterrupted(reviewDir string) error {
	m, err := ReadManifest(reviewDir)
	if err != nil {
		return err
	}
	if !m.Interrupted {
		return nil
	}
	m.Interrupted = false
	return WriteManifest(reviewDir, m)
}

// ResumeInfo reports how a resume run partitioned the locked roster: the agents
// already completed (skipped) and the agents that will be re-run.
type ResumeInfo struct {
	Completed []string
	Pending   []string
}

// AllComplete reports whether every roster agent already finished, so the caller
// can skip the fan-out entirely and go straight to reconciliation (epic 4.1.1 AC2).
func (r *ResumeInfo) AllComplete() bool { return len(r.Pending) == 0 }

// filterPendingSlots keeps only the slots whose primary agent is not already in
// the completed set, so a resumed fan-out re-runs only the pending/failed agents
// (epic 4.1.1 AC4).
func filterPendingSlots(slots []Slot, done map[string]bool) []Slot {
	if len(slots) == 0 {
		return nil
	}
	pending := make([]Slot, 0, len(slots))
	for _, s := range slots {
		if !done[s.Primary.Name] {
			pending = append(pending, s)
		}
	}
	return pending
}

// PrepareResume validates an existing review directory against the current
// working tree and configured roster, then assembles a PreparedReview whose Dir
// is that existing directory and whose Slots are only the pending agents. The
// range and roster are locked: a changed git range (ErrRangeChanged) or a
// changed roster set (ErrRosterChanged) aborts before any agent runs, so a resume
// can never mix inconsistent contexts or silently run a different panel. Payloads
// are rebuilt from the (validated-identical) recorded range so pending agents see
// exactly what the completed agents reviewed. The returned ResumeInfo reports the
// completed/pending split; when AllComplete is true the Slots are empty and the
// caller reconciles without a fan-out.
func PrepareResume(ctx context.Context, cfg *ReviewConfig, reviewDir string, req ReviewRequest) (*PreparedReview, *ResumeInfo, error) {
	m, err := ReadManifest(reviewDir)
	if err != nil {
		return nil, nil, err
	}
	if err := ValidateResumeRange(m, req.Range); err != nil {
		return nil, nil, err
	}
	configured := rosterNames(cfg.Project)
	if err := ValidateResumeRoster(m, configured); err != nil {
		return nil, nil, err
	}

	payloads, err := buildPayloads(ctx, cfg, req.Repo, req.Range.Base, req.Range.Head)
	if err != nil {
		return nil, nil, err
	}
	slots, _, err := buildSlots(cfg, payloads, req.Range, "", "")
	if err != nil {
		return nil, nil, err
	}

	done, err := CompletedAgents(reviewDir)
	if err != nil {
		return nil, nil, err
	}

	info := &ResumeInfo{}
	for _, name := range configured {
		if done[name] {
			info.Completed = append(info.Completed, name)
		} else {
			info.Pending = append(info.Pending, name)
		}
	}

	p := &PreparedReview{
		ID:          filepath.Base(reviewDir),
		Dir:         reviewDir,
		Slots:       filterPendingSlots(slots, done),
		TimeoutSec:  cfg.Settings.TimeoutSecs,
		MaxParallel: cfg.Settings.MaxParallel,
		Repo:        req.Repo,
		Head:        req.Range.Head,
		manifest:    m,
		// Wire the diff cache for resumed agents too (Epic 5.2): a resumed agent's
		// fresh output is written back so a later full run benefits — matching the
		// "fresh results are always written" contract — rather than being re-called.
		cache:       cache.NewStore(filepath.Join(req.Root, ".atcr", "cache"), cfg.Settings.CacheMaxBytes),
		cacheNoRead: req.NoCache,
	}
	return p, info, nil
}

// ExecuteResume runs the pending slots, persists their per-agent artifacts (the
// already-completed agents' artifacts on disk are untouched), then rebuilds
// summary.json and the merged findings.txt over the FULL on-disk union so the
// aggregate reflects the whole roster — not just the re-run subset. The manifest
// is finalized with the union's partial flag and the interrupt marker: if the
// resume is itself interrupted (AC7), whatever pending agents completed are
// preserved and the run stays interrupted. The all-agents-failed gate is judged
// over the union, so a resume whose pending agents all fail again still returns
// success when an earlier completed agent succeeded.
func ExecuteResume(ctx context.Context, completer Completer, p *PreparedReview) (*ReviewResult, error) {
	poolDir := filepath.Join(p.Dir, "sources", "pool")

	results, resumedStage := runEngine(ctx, completer, p, poolDir)
	interrupted := errors.Is(ctx.Err(), context.Canceled)

	if err := writeResumedAgents(poolDir, results); err != nil {
		return nil, err
	}

	sum, statuses, err := RebuildPool(poolDir, p.manifest.Roster)
	if err != nil {
		return nil, err
	}

	// Recompute the Review stage from the union of on-disk statuses so the
	// manifest reflects the current state — not the original run's verbatim
	// stage. A tool agent that degraded in the original run but succeeded with
	// full tools on resume must not stay recorded as degraded.
	reviewStage := reviewStageFromStatuses(statuses)
	if reviewStage != nil {
		// Carry snapshot provenance (AC 03-02 / 03-03): prefer the resumed run's
		// values when the resume attempted a snapshot; otherwise preserve the
		// original manifest's snapshot fields (they're still authoritative for
		// the roster).
		if resumedStage != nil && resumedStage.SnapshotMode != "" {
			reviewStage.SnapshotMode = resumedStage.SnapshotMode
			reviewStage.HeadSHA = resumedStage.HeadSHA
			reviewStage.SnapshotWorktreePath = resumedStage.SnapshotWorktreePath
		} else if p.manifest.Review != nil {
			reviewStage.SnapshotMode = p.manifest.Review.SnapshotMode
			reviewStage.HeadSHA = p.manifest.Review.HeadSHA
			reviewStage.SnapshotWorktreePath = p.manifest.Review.SnapshotWorktreePath
		}
	} else if p.manifest.Review != nil {
		// No tool agents in the union: preserve the original stage (the roster
		// is locked and the original stage's membership is still authoritative).
		reviewStage = p.manifest.Review
	}

	// Finalize the manifest into a local copy (only adopted on a successful write).
	m := *p.manifest
	m.Partial = sum.Partial
	m.CompletedAt = time.Now().UTC()
	m.Interrupted = interrupted
	m.Review = reviewStage
	if err := WriteManifest(p.Dir, &m); err != nil {
		// Best-effort: stamp Interrupted on the existing manifest so the run is
		// not stuck in_progress when a resume is itself interrupted (AC7). Mirrors
		// the analogous fallback in ExecuteReview.
		if interrupted {
			p.manifest.Interrupted = true
			_ = WriteManifest(p.Dir, p.manifest)
		}
		return nil, err
	}
	p.manifest = &m

	res := &ReviewResult{ID: p.ID, Dir: p.Dir, Summary: sum}
	if sum.Total == 0 {
		return res, ErrEmptyRoster
	}
	if sum.Succeeded == 0 {
		return res, fmt.Errorf("%w: %s", ErrAllAgentsFailed, formatStatusFailures(statuses))
	}
	return res, nil
}

// writeResumedAgents persists the per-agent artifacts (review.md, findings.txt,
// status.json) for each re-run result. A re-run agent's prior (failed/timeout)
// artifacts are overwritten in place; completed agents are not in results, so
// their artifacts on disk are left untouched. A result whose error is a
// synthesized context cancellation/deadline (the engine stamps these for slots
// it never actually invoked because the parent ctx was cancelled first) is
// skipped so a previously-failed agent's original error message is preserved
// rather than overwritten with a generic "context canceled" timeout.
func writeResumedAgents(poolDir string, results []Result) error {
	for _, r := range results {
		// Detect a synthesized timeout: the engine never invoked this agent
		// (no content produced) and the error is a pure context cancellation
		// or deadline. These results exist only to satisfy the per-slot Result
		// contract; writing them would clobber a prior real failure's status.
		if r.Content == "" && (errors.Is(r.Err, context.Canceled) || errors.Is(r.Err, context.DeadlineExceeded)) {
			continue
		}
		dir, err := agentDirName(r.Agent)
		if err != nil {
			return err
		}
		fr := findingsFor(r)
		if err := writeAgentArtifacts(poolDir, dir, r, fr); err != nil {
			return err
		}
	}
	return nil
}

// maxAgentFileBytes caps a single per-agent findings.txt read during pool
// rebuild so a corrupt or pathologically large artifact cannot exhaust memory.
// It is a var (not const) so tests can shrink it.
var maxAgentFileBytes int64 = 32 << 20 // 32 MiB

// errFindingsTooLarge reports a per-agent findings.txt that exceeds
// maxAgentFileBytes; the rebuild fails loudly rather than reading it unbounded.
var errFindingsTooLarge = errors.New("findings file exceeds size limit")

// readFileLimited reads path but refuses files larger than limit bytes,
// returning errFindingsTooLarge instead. This bounds the pool rebuild's memory
// use against an unbounded read of an on-disk artifact.
func readFileLimited(path string, limit int64) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > limit {
		return nil, fmt.Errorf("%w: %s is %d bytes (limit %d)", errFindingsTooLarge, path, fi.Size(), limit)
	}
	return os.ReadFile(path)
}

// RebuildPool recomputes the merged pool findings.txt and summary.json from every
// per-agent artifact currently under poolDir/raw/agent (completed + newly
// resumed), returning the aggregate Summary and the union of per-agent statuses.
// roster supplies the manifest's agent ordering so the merged findings.txt rows
// follow the same order as a fresh WritePool (which iterates results in roster
// order); without it, os.ReadDir would yield lexicographic order and a resumed
// review's findings.txt would differ from an equivalent fresh run. An agent in
// the roster without an on-disk directory is skipped (it never completed); an
// agent directory not in the roster is also skipped (stale/orphan entry).
func RebuildPool(poolDir string, roster []string) (Summary, []AgentStatus, error) {
	rawDir := filepath.Join(poolDir, poolRawAgentDir)

	// Build an index of on-disk agent directories for O(1) lookup.
	onDisk := make(map[string]string, len(roster))
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		return Summary{}, nil, fmt.Errorf("reading agent artifacts: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			onDisk[e.Name()] = filepath.Join(rawDir, e.Name())
		}
	}

	var statuses []AgentStatus
	var merged []stream.Finding
	seen := make(map[string]bool, len(roster))
	// Iterate in roster order so the merged findings.txt rows match a fresh
	// WritePool's output (which iterates results in roster order).
	for _, agent := range roster {
		dirName, err := agentDirName(agent)
		if err != nil {
			continue
		}
		if seen[dirName] {
			return Summary{}, nil, fmt.Errorf("duplicate agent directory %q (from agent %q)", dirName, agent)
		}
		seen[dirName] = true
		agentDir, ok := onDisk[dirName]
		if !ok {
			continue // not on disk → never completed; not part of the union
		}
		sdata, rerr := os.ReadFile(filepath.Join(agentDir, statusFile))
		if rerr != nil {
			continue
		}
		var st AgentStatus
		if json.Unmarshal(sdata, &st) != nil {
			continue
		}
		statuses = append(statuses, st)
		fdata, ferr := readFileLimited(filepath.Join(agentDir, findingsFile), maxAgentFileBytes)
		if ferr != nil {
			// An oversize findings.txt is a corruption signal — fail the rebuild
			// rather than read it unbounded. A merely missing or unreadable
			// findings.txt stays tolerated: a completed agent whose status.json
			// landed but whose findings were never finalized contributes no
			// findings, exactly as the original lenient read did.
			if errors.Is(ferr, errFindingsTooLarge) {
				return Summary{}, nil, ferr
			}
			continue
		}
		pr, perr := stream.ParseSource(fdata)
		if perr != nil {
			// The findings.txt exists but does not parse: silently dropping it
			// would let the resumed aggregate diverge from the original run
			// (short summary.TotalFindings, missing merged rows) with no signal.
			// Fail loudly for OK agents; tolerate for already-failed agents
			// (their findings are empty/irrelevant by construction).
			if st.Status == StatusOK {
				return Summary{}, nil, fmt.Errorf("parsing findings for completed agent %q: %w", st.Agent, perr)
			}
			continue
		}
		merged = append(merged, pr.Findings...)
	}

	if err := writeFindings(filepath.Join(poolDir, findingsFile), merged); err != nil {
		return Summary{}, nil, err
	}
	sum := summarizeStatuses(statuses)
	ps := PoolSummary{
		Agents:        statuses,
		Total:         sum.Total,
		Succeeded:     sum.Succeeded,
		Failed:        sum.Failed,
		Partial:       sum.Partial,
		TotalFindings: len(merged),
	}
	if err := writeJSON(filepath.Join(poolDir, summaryFile), ps); err != nil {
		return Summary{}, nil, err
	}
	return sum, statuses, nil
}

// summarizeStatuses tallies a union of per-agent statuses into a Summary, mirroring
// summarize() (which works over []Result) for the resume rebuild path.
func summarizeStatuses(sts []AgentStatus) Summary {
	s := Summary{Total: len(sts)}
	for _, st := range sts {
		if st.Status == StatusOK {
			s.Succeeded++
		} else {
			s.Failed++
		}
	}
	s.Partial = s.Failed > 0 && s.Succeeded > 0
	return s
}

// formatStatusFailures renders "agent (reason), ..." for the all-failed resume
// error, sorted for deterministic output. The reason is the recorded error
// message when present, else the status string.
func formatStatusFailures(sts []AgentStatus) string {
	parts := make([]string, 0, len(sts))
	for _, st := range sts {
		if st.Status == StatusOK {
			continue
		}
		reason := st.Status
		if st.Error != "" {
			reason = st.Error
		}
		name := st.Agent
		if name == "" {
			name = "<unnamed>"
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", name, reason))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// reviewStageFromStatuses rebuilds the manifest's Review stage from the union of
// per-agent statuses (as returned by RebuildPool) via the shared
// reviewStageForAgents classifier — the same rule the fresh []Result path
// (reviewStageFor) uses, so the two manifest paths cannot silently diverge.
// Returns nil when no agent ran with tools.
func reviewStageFromStatuses(statuses []AgentStatus) *payload.ReviewStage {
	return reviewStageForAgents(statuses,
		func(s AgentStatus) bool { return s.ToolsRequested },
		func(s AgentStatus) bool { return s.ToolsDegraded },
		func(s AgentStatus) string { return s.Agent })
}
