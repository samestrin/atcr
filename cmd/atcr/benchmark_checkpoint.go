package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// runCheckpoint is the on-disk durable record of a benchmark run's completed,
// already-scored cases (Epic 10.3). `atcr benchmark run --checkpoint <file>` writes
// one entry per case as soon as it is scored — before the loop advances — so a run
// killed mid-suite leaves a checkpoint containing exactly the completed cases. A
// re-run over the same suite replays these entries into the score accumulator
// instead of re-executing (and re-paying for) them.
//
// The suite-identity triple (ReproHash + Suite + SuiteVersion) guards resume:
// validateCheckpoint fails closed if any of them drifted, so a checkpoint from a
// different or changed suite is never silently mixed into a new run (AC4). The
// format mirrors the fanout resume precedent (internal/fanout/resume.go) — a
// recorded-identity header plus the completed units — rather than inventing a new
// mechanism. The orchestration lives here in cmd/atcr (the composition root), not
// internal/benchmark, keeping that package a live-LLM-free suite-contract + scorer
// leaf.
type runCheckpoint struct {
	ReproHash    string `json:"repro_hash"`
	Suite        string `json:"suite"`
	SuiteVersion string `json:"suite_version"`
	// Roster is the sorted "agent=model" signature of the reviewer panel that
	// produced this checkpoint. ReproHash covers only suite CONTENT (cases + diffs),
	// not the panel, so a roster/model change would otherwise resume silently —
	// mixing stale checkpointed reviewers with freshly-executed ones. Recording the
	// roster lets validateCheckpointRoster fail closed on drift, mirroring fanout's
	// ErrRosterChanged precedent.
	Roster []string         `json:"roster"`
	Cases  []checkpointCase `json:"cases"`
}

// checkpointCase is one completed case's scored outcome, keyed by its index in the
// suite's case list (the same index the run loop iterates), so replay folds it back
// in the original case order — preserving the deterministic aggregation the
// reproducibility contract depends on (AC3).
type checkpointCase struct {
	Index     int                  `json:"index"`
	CaseID    string               `json:"case_id"`
	Reviewers []checkpointReviewer `json:"reviewers"`
}

// checkpointReviewer captures exactly the per-reviewer fields the run loop folds
// into a reviewerAcc for one case: identity (model/persona, locked at first
// sighting), the case score (expected vs raised categories), and the usage-gated
// cost/latency contribution. Storing the already-computed cost contribution (not the
// raw tokens) and replaying it in case order keeps the float sum and latency median
// byte-identical to an uninterrupted run.
type checkpointReviewer struct {
	Agent         string   `json:"agent"`
	Model         string   `json:"model"`
	Persona       string   `json:"persona"`
	Expected      []string `json:"expected"`
	Raised        []string `json:"raised"`
	UsageReported bool     `json:"usage_reported"`
	CostUSD       float64  `json:"cost_usd"`
	LatencyMS     int64    `json:"latency_ms"`
}

// errCheckpointSuiteMismatch reports that a checkpoint's recorded suite identity
// differs from the suite being run. Resume fails closed (the operator must remove
// the stale checkpoint to start fresh) rather than silently discarding or mixing
// it — mirroring fanout's ErrRangeChanged / ErrRosterChanged hard-abort contract.
var errCheckpointSuiteMismatch = errors.New("checkpoint suite identity changed since it was written")

// errCheckpointRosterMismatch reports that the configured reviewer roster (agent
// set and/or per-agent model) differs from the roster the checkpoint recorded.
// Resume fails closed rather than silently scoring different reviewers over
// different subsets of cases — mirroring fanout's ErrRosterChanged contract.
var errCheckpointRosterMismatch = errors.New("checkpoint reviewer roster changed since it was written")

// errCheckpointCorrupt reports that a checkpoint file parsed as JSON but failed
// internal self-consistency checks (duplicate indices, out-of-range indices, or
// empty case IDs). Resume fails closed so a hand-edited or damaged checkpoint
// cannot silently drop completed cases.
var errCheckpointCorrupt = errors.New("checkpoint is corrupt")

// errCheckpointCaseMismatch reports that a checkpoint entry's recorded case id
// no longer matches the suite's case at the same index. ReproHash is
// order-independent, so a reordered suite shares the hash but remaps indices;
// this sentinel lets callers distinguish per-index drift from suite-identity
// drift.
var errCheckpointCaseMismatch = errors.New("checkpoint case id changed since it was written")

// loadCheckpoint reads and parses a checkpoint file. A missing file returns
// (nil, nil): it is the legitimate first-run case (start fresh), not an error. A
// present-but-corrupt file surfaces a parse error rather than a guessed empty
// state, so a damaged checkpoint can never cause a silent full re-run.
func loadCheckpoint(path string) (*runCheckpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading checkpoint %s: %w", path, err)
	}
	var cp runCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("checkpoint %s is corrupt: %w", path, err)
	}
	if err := validateCheckpointIntegrity(&cp); err != nil {
		return nil, fmt.Errorf("checkpoint %s is corrupt: %w", path, err)
	}
	return &cp, nil
}

// validateCheckpointIntegrity rejects malformed-but-parseable checkpoints before
// they can silently corrupt replay. Duplicate indices are particularly dangerous:
// doneIndex would last-write-win, dropping the earlier completed case.
func validateCheckpointIntegrity(cp *runCheckpoint) error {
	seen := make(map[int]struct{}, len(cp.Cases))
	for i, c := range cp.Cases {
		if c.Index < 0 {
			return fmt.Errorf("%w: case %d has negative index %d", errCheckpointCorrupt, i, c.Index)
		}
		if c.CaseID == "" {
			return fmt.Errorf("%w: case %d has empty case_id", errCheckpointCorrupt, i)
		}
		if _, ok := seen[c.Index]; ok {
			return fmt.Errorf("%w: duplicate case index %d", errCheckpointCorrupt, c.Index)
		}
		seen[c.Index] = struct{}{}
	}
	return nil
}

// saveCheckpoint atomically writes the checkpoint to path (temp file + rename, via
// the shared writeExportFile helper), so a process killed mid-write leaves the
// previous valid checkpoint intact — the on-disk file always reflects a whole
// number of completed cases (AC1).
//
// The full file is rewritten after every scored case intentionally: serial LLM
// latency dominates the runtime by orders of magnitude, and the atomic
// temp-file+rename guarantee requires rewriting the whole checkpoint. An
// append-friendly format would add parsing/dedup complexity without a measurable
// win at realistic suite sizes.
func saveCheckpoint(path string, cp *runCheckpoint) error {
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("encoding checkpoint: %w", err)
	}
	return writeExportFile(path, data)
}

// doneIndex maps each completed case's index to its recorded entry, so the run loop
// can skip-and-replay a checkpointed case in O(1).
func (cp *runCheckpoint) doneIndex() map[int]checkpointCase {
	done := make(map[int]checkpointCase, len(cp.Cases))
	for _, c := range cp.Cases {
		done[c.Index] = c
	}
	return done
}

// validateCheckpoint enforces the suite-identity guard (AC4): the checkpoint may be
// resumed only when its recorded ReproHash, Suite, and SuiteVersion all match the
// suite currently being run. Any drift returns errCheckpointSuiteMismatch so the
// caller aborts rather than mixing inconsistent work.
func validateCheckpoint(cp *runCheckpoint, reproHash, suite, suiteVersion string) error {
	if cp.ReproHash != reproHash || cp.Suite != suite || cp.SuiteVersion != suiteVersion {
		return fmt.Errorf("%w: recorded suite %q version %q (hash %s), current suite %q version %q (hash %s); remove the checkpoint to start fresh",
			errCheckpointSuiteMismatch, cp.Suite, cp.SuiteVersion, shortHash(cp.ReproHash), suite, suiteVersion, shortHash(reproHash))
	}
	return nil
}

// validateCheckpointRoster enforces the roster guard (AC4): the configured reviewer
// panel must match the one recorded in the checkpoint. Both signatures are
// "agent=model" entries; they are compared as sorted sets so declaration order is
// irrelevant. Any added, removed, or model-changed reviewer returns
// errCheckpointRosterMismatch so the caller aborts rather than mixing panels.
func validateCheckpointRoster(cp *runCheckpoint, roster []string) error {
	recorded := sortedCopy(cp.Roster)
	current := sortedCopy(roster)
	if !equalStrings(recorded, current) {
		return fmt.Errorf("%w: recorded [%s], configured [%s]; remove the checkpoint to start fresh",
			errCheckpointRosterMismatch, strings.Join(recorded, " "), strings.Join(current, " "))
	}
	return nil
}

// sortedCopy returns a sorted copy of s without mutating the input.
func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// equalStrings reports whether two string slices are element-wise equal.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// shortHash trims a repro hash to 12 chars for legible diagnostics, leaving shorter
// values intact (mirrors fanout's shortRef).
func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
