package payload

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
)

// ErrNoEffectiveByteBudget is returned by partitionByBudget when the per-chunk
// budget is non-positive. The budget is machine-derived from the model window via
// sizing.EffectiveByteBudget, so 0 unambiguously means the model cannot fit any
// review payload — an error, NOT ApplyByteBudget's "0 = unlimited" convention. The
// caller (which knows the model name) wraps this into AC 01-03 Error Scenario 1's
// exact `model %q ...` usage-error message.
var ErrNoEffectiveByteBudget = errors.New("full-repo scan: no effective byte budget for a review payload (context window too small for the configured output reservation)")

// fullrepo.go hosts the full-repository / directory-scoped ("baseline") payload
// path for `atcr review --all` and `atcr review --dir <path>` (Sprint 35.0). It
// is a SIBLING to builder.go's diff-range dispatch, never a new PayloadMode case
// inside it: baseline mode enumerates tracked files (stream.BuildFileIndex),
// ignore-filters them (newIgnoreMatcher), and partitions the survivors into
// byte-budget-bounded chunks — it does not consume a git diff range.
//
// Exposes two same-package primitives: enumerateRepoFiles (AC 01-02 tracked-file
// walk + ignore filter) and partitionByBudget (AC 01-03 byte-budget chunking).

// enumerateRepoFiles is the baseline (--all / --dir) tracked-file walker (Sprint
// 35.0, AC 01-02). It enumerates the repository's git-tracked files via
// stream.BuildFileIndex — the SAME git ls-files primitive diff-mode reconciliation
// uses (Epic 5.4), no second traversal — filters each through the existing
// ignoreMatcher (.gitignore/.atcrignore parity with diff-mode, ignore.go), and
// reads the survivors' contents into []FileEntry for the byte-budget partitioner.
//
// Contract:
//   - A nil FileIndex (empty root, not a git repo, or git unavailable —
//     BuildFileIndex's documented degrade-to-nil) is a clear error, never a
//     nil-pointer panic (AC 01-02 Edge Case 2 / Error Scenario 1).
//   - A repo with zero tracked files returns an empty, non-nil slice with no
//     error, so the caller surfaces "no reviewable content" upstream (Edge Case 1).
//   - Untracked working-tree files are excluded by construction: the candidate set
//     is git ls-files only (Edge Case 5), an explicit non-goal of this epic.
//   - Enumeration order is unspecified (FileIndex.Paths() iterates a map);
//     deterministic chunk ordering is partitionByBudget's responsibility, not the
//     walker's.
//
// No new subprocess is spawned per file (AC 01-02 Performance): the one git
// ls-files call is inside BuildFileIndex; contents come from disk reads.
func enumerateRepoFiles(ctx context.Context, root string, logger *slog.Logger) ([]FileEntry, error) {
	idx := stream.BuildFileIndex(ctx, root)
	if idx == nil {
		return nil, errors.New("full-repo scan: could not enumerate tracked files (not a git repository or git unavailable)")
	}
	matcher := newIgnoreMatcher(root, logger)
	paths := idx.Paths()
	entries := make([]FileEntry, 0, len(paths))
	for _, rel := range paths {
		if matcher.match(rel) {
			continue // .gitignore/.atcrignore parity with diff-mode
		}
		entry, err := readTrackedFile(root, rel)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// readTrackedFile reads one tracked file's content into a FileEntry. rel is a
// slash-normalized, repo-root-relative path straight from git ls-files (trusted,
// no traversal), joined under root. A tracked symlink captures its LITERAL target
// string via os.Readlink — the exact content git stores for the symlink object —
// and is NEVER resolved or followed, so a link pointing outside root cannot cause
// a read escape (AC 01-02 Edge Case 4 / Security Considerations). Intermediate
// path components of a git ls-files path are always real directories (git tracks a
// symlink as its own leaf entry, never traverses through one), so os.ReadFile of a
// regular file stays rooted at root. Binary/non-UTF8 content is captured verbatim
// with its raw byte size (Edge Case 3).
func readTrackedFile(root, rel string) (FileEntry, error) {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(abs)
	if err != nil {
		return FileEntry{}, fmt.Errorf("full-repo scan: reading tracked file %q: %w", rel, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(abs)
		if err != nil {
			return FileEntry{}, fmt.Errorf("full-repo scan: reading tracked file %q: %w", rel, err)
		}
		return FileEntry{Path: rel, Size: int64(len(target)), Body: target}, nil
	}
	// Defense-in-depth rooted read (AC 01-02 Security): resolve every symlink in the
	// path and refuse a read whose real target escapes root, before os.ReadFile
	// follows it. Lstat only protects the leaf; an intermediate directory git tracked
	// as a real dir may since have been replaced by a symlink pointing outside root.
	if err := ensureWithinRoot(root, abs, rel); err != nil {
		return FileEntry{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return FileEntry{}, fmt.Errorf("full-repo scan: reading tracked file %q: %w", rel, err)
	}
	return FileEntry{Path: rel, Size: int64(len(data)), Body: string(data)}, nil
}

// ensureWithinRoot rejects a regular-file read whose real (symlink-resolved) path
// lands outside root. It mirrors ingest.go's rejectDiffSymlinkEscape but roots at a
// caller-supplied directory instead of os.Getwd(): both root and the target are
// symlink-resolved before comparison so the check holds on platforms whose temp /
// working dirs sit behind a symlink (macOS /var -> /private/var). A target that does
// not resolve surfaces as a read error naming the path (Error Scenario 2 parity).
func ensureWithinRoot(root, abs, rel string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("full-repo scan: resolving repository root: %w", err)
	}
	realAbs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("full-repo scan: reading tracked file %q: %w", rel, err)
	}
	r, err := filepath.Rel(realRoot, realAbs)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return fmt.Errorf("full-repo scan: refusing tracked file %q: resolves outside the repository root via a symlink", rel)
	}
	return nil
}

// partitionByBudget groups an already-enumerated, already-ignore-filtered
// []FileEntry into N byte-budget-bounded chunks for baseline (--all / --dir)
// review, so a large repository is fanned out across several context-limited LLM
// calls instead of one payload that would overflow the model window.
//
// DESIGN NOTE (Phase 1, Decision 1 — pinned so Phase 2 task 2.8 does not
// re-litigate it). Resolves AC 01-03 (01-03-byte-budget-chunk-partitioning.md)
// without contradiction against every Happy Path, Edge Case, and Error Scenario:
//
//	Signature:
//	    func partitionByBudget(entries []FileEntry, chunkBudget int64) ([][]FileEntry, error)
//	  The abbreviated signature in the sprint-plan (no error return) is widened to
//	  return an error because AC 01-03 Error Scenario 1 requires the zero-budget
//	  case to "fail fast ... at entry, before any bin-packing work" with a usage
//	  error (CLI exit 2). A [][]FileEntry-only signature cannot express that, so
//	  the error is returned here rather than smuggled through a panic or a sentinel
//	  empty result. chunkBudget is supplied by the caller from sizing.go's
//	  EffectiveByteBudget(model, outputTokens) — no new sizing constant is
//	  introduced (AC 01-03 Story-Specific DoD).
//
//	Ordering / determinism rule (AC 01-03 Edge Case 2, Story-Specific DoD):
//	  Stable sort by size-descending, path-ascending tie-break — the SAME
//	  determinism convention ApplyByteBudget already uses (budget.go:75-81) — then
//	  greedy next-fit bin-pack in that canonical order. The (size,path) key is
//	  total (tracked paths are unique), so repeated runs over identical input
//	  produce byte-for-byte identical chunk membership and ordering, with zero
//	  map-iteration-order leakage. NOTE: task 1.1's "preserve original git ls-files
//	  order (no size-desc resort)" phrasing is intentionally NOT adopted — it
//	  conflicts with AC 01-03 Edge Case 2 and with task 2.8's own "stable sort
//	  (size-desc/path-asc tie-break)" text, and git ls-files order is in any case
//	  lost the moment paths pass through FileIndex.Paths()'s map, so a canonical
//	  re-sort is mandatory for determinism regardless. Reviewability grouping is a
//	  soft goal subordinate to the AC's explicit determinism mechanism. This
//	  divergence from the sprint-plan parenthetical is flagged for confirmation at
//	  the Phase 1 gate.
//
//	Oversized-single-file rule (AC 01-03 Happy Path Scenario 3):
//	  A single entry whose clampSize(Size) alone exceeds chunkBudget becomes its
//	  OWN whole chunk — never split, never dropped — mirroring chunkDiff's
//	  single-file-never-split convention (chunker.go:106-108) and deliberately
//	  diverging from ApplyByteBudget's drop-to-fit behavior, so the "zero files
//	  silently omitted" contract holds even for over-budget singletons.
//
//	Empty-input rule (AC 01-03 Edge Case 1):
//	  Zero entries returns (nil, nil) — zero chunks, NOT one empty chunk — so the
//	  caller's upstream "no reviewable content" guard fires correctly.
//
//	Zero-effective-budget fail-fast (AC 01-03 Edge Case 3 / Error Scenario 1):
//	  chunkBudget <= 0 returns, at entry and before any packing work, the sentinel
//	  ErrNoEffectiveByteBudget. This function does not know the model name, so the
//	  CALLER (PrepareReviewFromRepo) wraps the sentinel into AC 01-03 Error Scenario
//	  1's exact user-facing "full-repo scan: model %q has no effective byte budget
//	  ..." message (CLI exit 2).
//	  This DELIBERATELY diverges from ApplyByteBudget's `budget <= 0` == "unlimited"
//	  convention: here the budget is machine-derived from the model window, so 0
//	  unambiguously means the model cannot fit any payload — an error, not an
//	  unbounded single chunk. The partitioner must never loop indefinitely nor emit
//	  one-chunk-per-file when the budget is non-positive.
//
//	Size hygiene (AC 01-03 Security / Input Validation):
//	  Reuses clampSize (budget.go:109) so a negative/corrupt FileEntry.Size can
//	  neither inflate nor deflate a chunk's accounted budget.
//
//	Complexity (AC 01-03 Performance): O(n log n) for the single sort plus O(n) for
//	  the greedy pack — sort once, assign in sorted order, never re-sort per chunk.
func partitionByBudget(entries []FileEntry, chunkBudget int64) ([][]FileEntry, error) {
	// Empty input → zero chunks (not one empty chunk), so the caller's
	// "no reviewable content" guard fires upstream (AC 01-03 Edge Case 1).
	if len(entries) == 0 {
		return nil, nil
	}
	// Non-positive budget → fail fast at entry, before any packing work
	// (AC 01-03 Edge Case 3 / Error Scenario 1). Never loop, never one-per-file.
	if chunkBudget <= 0 {
		return nil, ErrNoEffectiveByteBudget
	}

	// Canonical order: size-descending, path-ascending tie-break — the SAME
	// determinism convention ApplyByteBudget uses (budget.go) — via an index sort
	// so the caller's slice is never mutated. clampSize in the key so a
	// negative/corrupt size cannot skew ordering. The (clampedSize, path) key is
	// total over unique tracked paths, so repeated runs are byte-for-byte identical
	// (AC 01-03 Edge Case 2), with zero map-iteration-order leakage.
	idx := make([]int, len(entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ei, ej := entries[idx[a]], entries[idx[b]]
		si, sj := clampSize(ei.Size), clampSize(ej.Size)
		if si != sj {
			return si > sj
		}
		return ei.Path < ej.Path
	})

	var chunks [][]FileEntry
	var current []FileEntry
	var used int64
	flush := func() {
		if len(current) > 0 {
			chunks = append(chunks, current)
			current = nil
			used = 0
		}
	}
	for _, i := range idx {
		e := entries[i]
		sz := clampSize(e.Size)
		if sz > chunkBudget {
			// Oversized singleton: its OWN whole chunk, never split, never dropped
			// (AC 01-03 Happy Path 3) — mirrors chunkDiff's single-file-never-split
			// convention, diverging from ApplyByteBudget's drop-to-fit.
			flush()
			chunks = append(chunks, []FileEntry{e})
			continue
		}
		// Greedy next-fit: close the current chunk only when this entry would
		// overflow it, then open a new one (sort once, assign in order — O(n log n)
		// + O(n), no per-chunk re-sort). The overflow test is written as
		// `sz > chunkBudget-used` (both operands non-negative: sz<=chunkBudget here,
		// 0<=used<=chunkBudget) rather than `used+sz > chunkBudget`, so a pathological
		// exabyte-scale budget cannot wrap the sum negative — matching ApplyByteBudget's
		// saturation discipline (budget.go).
		if len(current) > 0 && sz > chunkBudget-used {
			flush()
		}
		current = append(current, e)
		used += sz
	}
	flush()
	return chunks, nil
}
