package payload

import (
	"fmt"
	"math"
	"sort"
)

// FileEntry is one changed file's contribution to a payload, with its byte
// size precomputed so the budget pass needs no filesystem access.
type FileEntry struct {
	Path string
	Size int64
	Body string
	// Mode is the payload mode this file was actually rendered in. It equals the
	// run's configured mode unless the escalation heuristic promoted this file
	// above it (Epic 35.1). Callers record it in the manifest so downstream tools
	// read what a reviewer saw per file, not just per agent. Empty on entries
	// built outside the changed-file path (baseline/full-repo scans).
	Mode PayloadMode
}

// Truncation records what a byte-budget pass dropped. It is ALWAYS returned by
// ApplyByteBudget (never silent): Truncated=false with an empty FilesDropped
// means nothing was dropped. FilesDropped is sorted by path for stable output.
// AllDropped is true when the input was non-empty but every file was shed —
// callers should surface this as a distinct error rather than forwarding an
// empty payload that silently produces zero findings.
type Truncation struct {
	Truncated    bool     `json:"truncated"`
	FilesDropped []string `json:"files_dropped"`
	AllDropped   bool     `json:"all_dropped,omitempty"`
}

// ValidateBudget rejects a negative byte budget. Zero is valid and means
// unlimited. Callers surface the returned error as a usage error (exit 2)
// before any review work begins (AC 06-03 Error Scenario 1).
func ValidateBudget(budget int64) error {
	if budget < 0 {
		return fmt.Errorf("byte budget must be >= 0, got %d", budget)
	}
	return nil
}

// ApplyByteBudget keeps as many files as fit within budget bytes, dropping
// whole files deterministically: largest first, then by path alphabetically
// for equal sizes. Dropping largest-first maximizes the number of kept files,
// preferentially shedding huge generated files/lockfiles over many small
// source files. A budget of 0 means unlimited (nothing dropped). Kept files
// retain their original order; the dropped list is returned sorted by path so
// the same input always produces the same Truncation.
func ApplyByteBudget(entries []FileEntry, budget int64) (kept []FileEntry, t Truncation) {
	return applyByteBudgetOrdered(entries, budget, nil)
}

// applyByteBudgetOrdered is the shared budget pass. tier, when non-nil,
// partitions entries into drop-priority tiers: an entry in a LOWER tier is shed
// before one in a higher tier, and largest-first (then path) orders within a
// tier. A nil tier is plain largest-first across the whole payload.
func applyByteBudgetOrdered(entries []FileEntry, budget int64, tier func(FileEntry) int) (kept []FileEntry, t Truncation) {
	t = Truncation{Truncated: false, FilesDropped: []string{}}
	if budget <= 0 { // 0 = unlimited; negatives are rejected by ValidateBudget
		return copyEntries(entries), t
	}

	// Negative sizes are invalid input and must never offset real bytes, and
	// the sum must saturate rather than wrap negative — either would make
	// total <= budget hold spuriously and silently skip truncation.
	var total int64
	for _, e := range entries {
		sz := clampSize(e.Size)
		if total > math.MaxInt64-sz {
			total = math.MaxInt64
			break
		}
		total += sz
	}
	if total <= budget {
		return copyEntries(entries), t
	}

	// Drop order: lowest tier first (when tiered), then largest first, then
	// path-alphabetical tie-break. Index into the original slice so duplicate
	// paths are accounted for independently — keying on Path would over-drop and
	// miscount when two entries share a path.
	idx := make([]int, len(entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ei, ej := entries[idx[a]], entries[idx[b]]
		if tier != nil {
			if ti, tj := tier(ei), tier(ej); ti != tj {
				return ti < tj
			}
		}
		if ei.Size != ej.Size {
			return ei.Size > ej.Size
		}
		return ei.Path < ej.Path
	})

	dropped := make([]bool, len(entries))
	used := total
	for _, i := range idx {
		if used <= budget {
			break
		}
		dropped[i] = true
		used -= clampSize(entries[i].Size)
	}

	kept = make([]FileEntry, 0, len(entries))
	droppedPaths := make([]string, 0)
	for i, e := range entries {
		if dropped[i] {
			droppedPaths = append(droppedPaths, e.Path)
			continue
		}
		kept = append(kept, e)
	}
	sort.Strings(droppedPaths)

	return kept, Truncation{Truncated: true, FilesDropped: droppedPaths, AllDropped: len(kept) == 0}
}

// ApplyByteBudgetPreferEscalated is ApplyByteBudget with an escalation-aware
// drop order: files the escalation heuristic promoted above the run's configured
// mode are shed LAST, after every un-escalated file (Epic 35.1).
//
// Plain largest-first is self-defeating here. Escalating a file to a
// higher-context mode replaces its hunks with much more text, which makes that
// entry by far the largest in the payload — so largest-first puts the file the
// heuristic just identified as hardest to review at the TOP of the drop list,
// and a tight per-agent window sheds exactly the file the feature exists to give
// the reviewer more of.
//
// The exemption is bounded: if preferring the escalated files would drop
// everything (the escalated bytes alone exceed the budget), this falls back to
// plain largest-first, which keeps the smaller un-escalated files rather than
// handing the caller an empty payload. Trading one escalated file for the whole
// rest of the review is never the better deal.
//
// configured is the run's payload mode. Only entries rendered in a
// HIGHER-context mode count as escalated, so entries carrying no mode at all
// (baseline/full-repo scans) are never exempted.
func ApplyByteBudgetPreferEscalated(entries []FileEntry, budget int64, configured PayloadMode) (kept []FileEntry, t Truncation) {
	kept, t = applyByteBudgetOrdered(entries, budget, func(e FileEntry) int {
		if modeRank(e.Mode) > modeRank(configured) {
			return 1 // escalated: dropped only after every un-escalated file
		}
		return 0
	})
	if t.AllDropped {
		return ApplyByteBudget(entries, budget)
	}
	return kept, t
}

// clampSize treats negative sizes as zero for budget accounting so invalid
// input can neither offset real bytes nor inflate freed space when dropped.
func clampSize(s int64) int64 {
	if s < 0 {
		return 0
	}
	return s
}

// copyEntries returns a fresh slice so callers can mutate the result without
// aliasing the input's backing array.
func copyEntries(entries []FileEntry) []FileEntry {
	out := make([]FileEntry, len(entries))
	copy(out, entries)
	return out
}
