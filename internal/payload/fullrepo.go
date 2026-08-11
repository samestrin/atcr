package payload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/cache"
	"github.com/samestrin/atcr/internal/stream"
)

// ErrNoEffectiveByteBudget is returned by PartitionByBudget when the per-chunk
// budget is non-positive. The budget is machine-derived from the model window via
// sizing.EffectiveByteBudget, so 0 unambiguously means the model cannot fit any
// review payload — an error, NOT ApplyByteBudget's "0 = unlimited" convention.
// NOTE: the shipped caller (PrepareReviewFromRepo in internal/fanout/review.go)
// pre-guards `if chunkBudget > 0` before calling PartitionByBudget, so the
// sentinel is unreachable through that path today — it is a direct-call contract,
// not a wrapped user-facing error.
var ErrNoEffectiveByteBudget = errors.New("full-repo scan: no effective byte budget for a review payload (context window too small for the configured output reservation)")

// errTrackedFileTooLarge marks a tracked file that exceeds maxTrackedFileReadBytes
// (TD-002). readTrackedFile wraps it so enumerateRepoFiles can distinguish
// "skip this one over-cap file" from a genuine read failure (which aborts the walk
// per AC 01-02).
var errTrackedFileTooLarge = errors.New("full-repo scan: tracked file exceeds the per-file read ceiling")

// maxTrackedFileReadBytes is the per-file read ceiling for the baseline walker
// (TD-002). readTrackedFile slurps each tracked file into memory, so a single
// pathological blob (multi-GB binary, a materialized Git-LFS pointer) could exhaust
// memory before the byte-budget partitioner ever runs; files past the ceiling are
// skipped with a Warn rather than OOMing the process. 32 MiB sits far above any
// legitimate source file (per-chunk budgets are context-window-derived, typically
// well under 1 MiB) while still bounding the worst-case allocation. A var (not a
// const) so tests can pin the boundary with small fixtures.
var maxTrackedFileReadBytes int64 = 32 << 20

// DefaultMaxRepoBytes caps the TOTAL bytes a baseline (--all/--dir) scan reads
// into memory during enumeration (TD-005) — the whole-repo counterpart of
// ingest.go's DefaultMaxDiffBytes fail-loud input guard. It mirrors that
// guard's PATTERN (a hard-coded, pre-processing, fail-loud ceiling), not its
// literal 10 MiB value: the baseline feature exists to scan repositories larger
// than one model window by partitioning them across chunks, so the ceiling is
// a genuine OOM tripwire (512 MiB), not a routine shaping mechanism —
// PartitionByBudget already handles normal large-repo sizing, and the per-file
// maxTrackedFileReadBytes caps any single read. Exceeding the total fails the
// scan with a plain error before any payload work starts. A var (not a const)
// so tests can pin the boundary with small fixtures.
var DefaultMaxRepoBytes int64 = 512 << 20

// fullrepo.go hosts the full-repository / directory-scoped ("baseline") payload
// path for `atcr review --all` and `atcr review --dir <path>` (Sprint 35.0). It
// is a SIBLING to builder.go's diff-range dispatch, never a new PayloadMode case
// inside it: baseline mode enumerates tracked files (stream.BuildFileIndex),
// ignore-filters them (newIgnoreMatcher), and partitions the survivors into
// byte-budget-bounded chunks — it does not consume a git diff range.
//
// Exposes two same-package primitives: enumerateRepoFiles (AC 01-02 tracked-file
// walk + ignore filter) and PartitionByBudget (AC 01-03 byte-budget chunking).

// BuildRepoEntries is the exported baseline payload entry point (Sprint 35.0): it
// returns the enumerated, ignore-filtered git-tracked files under root as
// []FileEntry, for internal/fanout's PrepareReviewFromRepo to assemble into a
// whole-repo review payload. It wraps the same enumerateRepoFiles walker the
// package tests cover directly. noIgnore bypasses the ignore filter (the
// --no-ignore flag), for parity with diff-mode. scope, when non-empty and not ".",
// restricts the walk to the tracked files nested under that repo-root-relative
// directory (the --dir <path> flag, Story 2); "" / "." mean the whole repository.
//
// idx and fresh drive the incremental re-scan skip (Sprint 35.0, Story 4/5): after
// ignore-filtering, applyHashSkip drops any candidate whose content is byte-for-byte
// unchanged since idx last recorded it, UNLESS fresh forces a full re-scan. Pass a
// nil idx (or fresh=true) to disable the skip — the whole-repo scan then behaves as
// a first-ever run. The skip is strictly after ignore-filtering and strictly before
// the byte-budget partitioner (AC 04-02).
func BuildRepoEntries(ctx context.Context, root string, logger *slog.Logger, noIgnore bool, scope string, idx *FileHashIndex, fresh bool) ([]FileEntry, error) {
	entries, _, err := BuildRepoEntriesWithStats(ctx, root, logger, noIgnore, scope, idx, fresh)
	return entries, err
}

// RepoScanStats reports how a baseline scan's candidate set was reduced between
// enumeration and chunking (TD-008/TD-010), so a caller handed ZERO entries can
// distinguish WHY: every candidate ignore-filtered (recoverable with
// --no-ignore), every candidate hash-skipped (nothing changed since the last
// review — a successful no-op), or a genuinely empty repository/scope.
type RepoScanStats struct {
	// IgnoreFiltered is the number of in-scope tracked candidates dropped by
	// .gitignore/.atcrignore (always zero when noIgnore bypassed the filter).
	IgnoreFiltered int
	// HashSkipped is the number of ignore-surviving candidates dropped by the
	// incremental file-hash index as byte-for-byte unchanged since last review
	// (always zero for a --fresh/nil-index scan, which skips nothing).
	HashSkipped int
}

// BuildRepoEntriesWithStats is BuildRepoEntries plus the reduction counters a
// caller needs to classify an empty result (see RepoScanStats). The two entry
// points share the single enumerate → hash-skip pipeline so their candidate
// sets can never diverge.
func BuildRepoEntriesWithStats(ctx context.Context, root string, logger *slog.Logger, noIgnore bool, scope string, idx *FileHashIndex, fresh bool) ([]FileEntry, RepoScanStats, error) {
	entries, ignoreFiltered, err := enumerateRepoFiles(ctx, root, logger, noIgnore, scope)
	if err != nil {
		return nil, RepoScanStats{}, err
	}
	kept, hashSkipped := applyHashSkip(entries, idx, fresh)
	return kept, RepoScanStats{IgnoreFiltered: ignoreFiltered, HashSkipped: hashSkipped}, nil
}

// applyHashSkip drops candidates whose content hash matches the recorded index entry
// (Sprint 35.0, AC 04-02) — the pre-chunking incremental re-scan skip. It runs on the
// already-ignore-filtered []FileEntry enumerateRepoFiles returns, so an ignored file
// never reaches it (ignore-filtering runs first, AC 04-02 Edge Case 2), and it runs
// before PartitionByBudget, so a skipped file never lands in any chunk. It also
// returns the number of candidates it dropped, so an empty result can be
// classified as "all unchanged since last review" (TD-010).
//
// fresh (the --fresh/--force bypass, AC 04-04/05-02) or a nil idx returns the input
// unchanged — every candidate is treated as unreviewed. Otherwise each candidate's
// content is hashed via cache.HashText (the canonical "sha256:<hex>" digest, the sole
// internal/cache dependency — never cache.Store) and dropped only on a definite
// index MATCH.
//
// Fail-open by construction: the digest is computed over the FileEntry.Body already
// read into memory by enumerateRepoFiles (a read failure there aborts earlier per AC
// 01-02), so hashing here cannot fail — a file is only ever dropped on a positive
// hash match, never silently dropped due to a hashing error (AC 04-02 Error Scenario
// 1 holds vacuously). The lookup is O(1) per candidate (idx is a path-keyed map).
func applyHashSkip(entries []FileEntry, idx *FileHashIndex, fresh bool) ([]FileEntry, int) {
	// Bypass the whole pass — including the per-file SHA-256 hashing — when there is
	// nothing to skip against: --fresh/--force, a nil index, or an empty index (the
	// normal first-run state, since Load always returns a non-nil-but-empty index for
	// a missing/empty/corrupt file). Hashing every file for guaranteed-miss lookups on
	// the one run that can never benefit is pure waste (4.5.A LOW).
	if fresh || idx == nil || len(idx.entries) == 0 {
		return entries, 0
	}
	out := make([]FileEntry, 0, len(entries))
	skipped := 0
	for _, e := range entries {
		if idx.Unchanged(e.Path, cache.HashText(e.Body)) {
			skipped++
			continue // byte-for-byte unchanged since last review → skip pre-chunking
		}
		out = append(out, e)
	}
	return out, skipped
}

// TrackedInScope returns the git-tracked, repo-root-relative, slash-normalized paths
// under root restricted to the --dir scope (Sprint 35.0), WITHOUT reading any file
// content — just stream.BuildFileIndex's git ls-files enumeration plus the scope
// filter. It is the cheap keep-set the baseline write-back uses to self-trim the
// file-hash index (a path no longer tracked is a deleted file whose entry must be
// pruned, AC 04-01 Edge Case 1) without a second body-reading walk. Ignore rules are
// intentionally NOT applied: an ignored-but-tracked file's stale index entry is
// harmless (it is never looked up), and over-keeping it is safer than pruning. A
// non-git-repo / git-unavailable root returns nil (BuildFileIndex degrades to nil).
func TrackedInScope(ctx context.Context, root, scope string) []string {
	idx := stream.BuildFileIndex(ctx, root)
	if idx == nil {
		return nil
	}
	return filterByScope(idx.Paths(), scope)
}

// NormalizeScope canonicalizes a --dir scope for comparison: backslashes become
// forward slashes and any trailing separator is trimmed, so ""/ "." (and forms
// like "./" or `.\`) all mean the whole repository. ReplaceAll (not
// filepath.ToSlash, a no-op on Unix) forces backslash→slash on every platform so
// the result is deterministic in tests. Every whole-repo/scoped interpreter
// (filterByScope, enumerateRepoFiles' zero-match guard, the baseline write-back's
// self-trim gate) must normalize through this one helper so they cannot
// disagree on what a non-CLI caller's raw scope string means.
func NormalizeScope(scope string) string {
	return strings.TrimRight(strings.ReplaceAll(scope, `\`, "/"), "/")
}

// filterByScope narrows a slash-normalized, repo-root-relative tracked-path set to
// the --dir <path> scope (Sprint 35.0, AC 02-02). An empty scope or "." means the
// whole repository (--all / --dir . degenerate), so the input is returned
// unchanged. Otherwise a path is kept only when it equals the scope or is nested
// under it as a FULL path segment — the containment test appends a "/" separator
// (scope+"/") so a sibling directory sharing a lexical prefix is never cross-matched
// (internal/fan must not pull in internal/fanout). It is a pure O(n) pass over the
// in-memory tracked list — no filepath.Walk, no per-file os.Stat (AC 02-02
// Performance / the story's second-traversal non-goal).
func filterByScope(paths []string, scope string) []string {
	// Defense-in-depth normalization (does not depend on AC 02-01's validated
	// slash-form/no-trailing-slash guarantee): the tracked paths are always
	// forward-slash form (stream.toSlashKeys), so normalize scope to match and trim
	// a stray trailing separator before comparing — otherwise a backslash-cleaned
	// (Windows filepath.Clean) or trailing-slashed scope would silently match zero
	// files (3.5.A LOW). Shared via NormalizeScope so every whole-repo interpreter
	// agrees on the canonical form.
	scope = NormalizeScope(scope)
	if scope == "" || scope == "." {
		return paths
	}
	prefix := scope + "/"
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == scope || strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out
}

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
//   - A non-empty --dir scope matching ZERO tracked files (while the repo has
//     tracked files elsewhere) is a scope-specific error naming the scope
//     (TD-007), so it is never confused with an empty repository.
//   - A walk whose in-memory total exceeds DefaultMaxRepoBytes fails loudly
//     mid-enumeration (TD-005) — the whole-repo counterpart of
//     DefaultMaxDiffBytes, tripping only on a genuine OOM-scale repository
//     (PartitionByBudget handles normal large-repo sizing).
//   - Untracked working-tree files are excluded by construction: the candidate set
//     is git ls-files only (Edge Case 5), an explicit non-goal of this epic.
//   - Enumeration order is unspecified (FileIndex.Paths() iterates a map);
//     deterministic chunk ordering is PartitionByBudget's responsibility, not the
//     walker's.
//
// No new subprocess is spawned per file (AC 01-02 Performance): the one git
// ls-files call is inside BuildFileIndex; contents come from disk reads.
// noIgnore bypasses the .gitignore/.atcrignore filter for this scan (the
// --no-ignore flag), matching diff-mode's payload.WithoutIgnoreFilter so baseline
// honors the flag identically — otherwise the on-disk manifest would record
// NoIgnore while files were in fact filtered, a provenance lie.
//
// The second return counts the candidates the ignore filter dropped, so a
// caller handed an empty slice can distinguish "every candidate
// ignore-filtered" (recoverable with --no-ignore, TD-008) from a genuinely
// empty repository/scope.
func enumerateRepoFiles(ctx context.Context, root string, logger *slog.Logger, noIgnore bool, scope string) ([]FileEntry, int, error) {
	idx := stream.BuildFileIndex(ctx, root)
	if idx == nil {
		return nil, 0, errors.New("full-repo scan: could not enumerate tracked files (not a git repository or git unavailable)")
	}
	// A nil matcher's match() is a no-op (returns false), so --no-ignore includes
	// every tracked file — the same effect as diff-mode's WithoutIgnoreFilter.
	var matcher *ignoreMatcher
	if !noIgnore {
		matcher = newIgnoreMatcher(root, logger)
	}
	// Scope filter (--dir <path>, AC 02-02) sits between FileIndex construction and
	// the read/ignore loop: narrow the tracked-path set to the requested subtree
	// before any file is read or ignore-matched. "" / "." = whole repo.
	tracked := idx.Paths()
	paths := filterByScope(tracked, scope)
	// TD-007: a scoped scan matching ZERO tracked files while the repo has tracked
	// files elsewhere gets a scope-specific diagnostic — otherwise the caller
	// surfaces the generic "no reviewable tracked files" message, the same one an
	// entirely empty repository produces. The normalization mirrors filterByScope's
	// (inlined here so the whole-repo "" / "." test sees the same form it does).
	if len(paths) == 0 && len(tracked) > 0 {
		if normalized := NormalizeScope(scope); normalized != "" && normalized != "." {
			return nil, 0, fmt.Errorf("full-repo scan: --dir %q matched no tracked files", normalized)
		}
	}
	entries := make([]FileEntry, 0, len(paths))
	ignoreFiltered := 0
	var totalBytes int64
	for _, rel := range paths {
		// TD-003: ctx bounded the git ls-files inside BuildFileIndex but nothing
		// bounded this loop — honor cancellation mid-walk so a cancelled/timed-out
		// context interrupts enumeration of a very large repository.
		if err := ctx.Err(); err != nil {
			return nil, 0, fmt.Errorf("full-repo scan: enumeration interrupted: %w", err)
		}
		if matcher.match(rel) {
			ignoreFiltered++
			continue // .gitignore/.atcrignore parity with diff-mode
		}
		entry, err := readTrackedFile(root, rel)
		if err != nil {
			if errors.Is(err, errTrackedFileTooLarge) {
				// TD-002: skip + flag an over-cap file instead of slurping it into
				// memory (OOM vector); the rest of the walk proceeds.
				if logger != nil {
					logger.Warn("full-repo scan: skipping over-cap tracked file", "path", rel, "err", err)
				}
				continue
			}
			return nil, 0, err
		}
		// TD-005: bound the TOTAL in-memory assembly, not just each file — many
		// individually-under-cap files (or many per-file-skipped binaries around
		// them) still sum to an OOM on a huge repository. Fail loudly before any
		// payload work, mirroring DefaultMaxDiffBytes's input guard. Only files
		// actually read into memory count: a per-file-over-cap skip (above) never
		// entered memory, so it must not trip the total.
		totalBytes += entry.Size
		if totalBytes > DefaultMaxRepoBytes {
			return nil, 0, fmt.Errorf("full-repo scan: tracked files total at least %d bytes exceeds the %d-byte in-memory assembly cap; scope the scan with --dir or review a diff range", totalBytes, DefaultMaxRepoBytes)
		}
		entries = append(entries, entry)
	}
	return entries, ignoreFiltered, nil
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
// with its raw byte size (Edge Case 3). A regular file larger than
// maxTrackedFileReadBytes is NOT read: it returns errTrackedFileTooLarge so the
// caller can skip + flag it instead of exhausting memory (TD-002).
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
	// Per-file size guard (TD-002): refuse to slurp a file past the read ceiling —
	// it is skipped + Warn-flagged by the caller rather than OOMing the process.
	if fi.Size() > maxTrackedFileReadBytes {
		return FileEntry{}, fmt.Errorf("%w: %q is %d bytes (max %d)", errTrackedFileTooLarge, rel, fi.Size(), maxTrackedFileReadBytes)
	}
	f, err := os.Open(abs)
	if err != nil {
		return FileEntry{}, fmt.Errorf("full-repo scan: reading tracked file %q: %w", rel, err)
	}
	defer func() { _ = f.Close() }()
	// Bound the read independently of the pre-read stat so a file that grows
	// between stat and read still cannot exceed the cap (TOCTOU defense — the same
	// "+1 over-read, then recheck" pair ingest.go's readCapped uses).
	data, err := io.ReadAll(io.LimitReader(f, maxTrackedFileReadBytes+1))
	if err != nil {
		return FileEntry{}, fmt.Errorf("full-repo scan: reading tracked file %q: %w", rel, err)
	}
	if int64(len(data)) > maxTrackedFileReadBytes {
		return FileEntry{}, fmt.Errorf("%w: %q grew past %d bytes during the read", errTrackedFileTooLarge, rel, maxTrackedFileReadBytes)
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

// PartitionByBudget groups an already-enumerated, already-ignore-filtered
// []FileEntry into N byte-budget-bounded chunks for baseline (--all / --dir)
// review, so a large repository is fanned out across several context-limited LLM
// calls instead of one payload that would overflow the model window.
//
// DESIGN NOTE (Phase 1, Decision 1 — pinned so Phase 2 task 2.8 does not
// re-litigate it). Resolves AC 01-03 (01-03-byte-budget-chunk-partitioning.md)
// without contradiction against every Happy Path, Edge Case, and Error Scenario:
//
//	Signature:
//	    func PartitionByBudget(entries []FileEntry, chunkBudget int64) ([][]FileEntry, error)
//	  The abbreviated signature in the sprint-plan (no error return) is widened to
//	  return an error because AC 01-03 Error Scenario 1 requires the zero-budget
//	  case to "fail fast ... at entry, before any bin-packing work" with a usage
//	  error (CLI exit 2). A [][]FileEntry-only signature cannot express that, so
//	  the error is returned here rather than smuggled through a panic or a sentinel
//	  empty result. chunkBudget is supplied by the caller from sizing.go's
//	  EffectiveByteBudget(model, declared, outputTokens) — no new sizing constant is
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
//	  ErrNoEffectiveByteBudget. The shipped caller (PrepareReviewFromRepo) pre-
//	  guards `if chunkBudget > 0` and falls through to the bulk path on non-
//	  positive budgets, so the sentinel is a direct-call contract today — no
//	  caller wraps it into a model-qualified usage error.
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
func PartitionByBudget(entries []FileEntry, chunkBudget int64) ([][]FileEntry, error) {
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
