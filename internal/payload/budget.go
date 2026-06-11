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
}

// Truncation records what a byte-budget pass dropped. It is ALWAYS returned by
// ApplyByteBudget (never silent): Truncated=false with an empty FilesDropped
// means nothing was dropped. FilesDropped is sorted by path for stable output.
type Truncation struct {
	Truncated    bool     `json:"truncated"`
	FilesDropped []string `json:"files_dropped"`
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
// whole files deterministically: smallest first, then by path alphabetically
// for equal sizes. A budget of 0 means unlimited (nothing dropped). Kept files
// retain their original order; the dropped list is returned sorted by path so
// the same input always produces the same Truncation.
func ApplyByteBudget(entries []FileEntry, budget int64) (kept []FileEntry, t Truncation) {
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

	// Drop order: smallest first, path-alphabetical tie-break. Index into the
	// original slice so duplicate paths are accounted for independently — keying
	// on Path would over-drop and miscount when two entries share a path.
	idx := make([]int, len(entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ei, ej := entries[idx[a]], entries[idx[b]]
		if ei.Size != ej.Size {
			return ei.Size < ej.Size
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

	return kept, Truncation{Truncated: true, FilesDropped: droppedPaths}
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
