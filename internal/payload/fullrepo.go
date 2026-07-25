package payload

// fullrepo.go hosts the full-repository / directory-scoped ("baseline") payload
// path for `atcr review --all` and `atcr review --dir <path>` (Sprint 35.0). It
// is a SIBLING to builder.go's diff-range dispatch, never a new PayloadMode case
// inside it: baseline mode enumerates tracked files (stream.BuildFileIndex),
// ignore-filters them (newIgnoreMatcher), and partitions the survivors into
// byte-budget-bounded chunks — it does not consume a git diff range.
//
// This file currently contains only Phase 1's design-spike stub; the GREEN
// implementation lands in Phase 2 (tasks 2.5 / 2.8). The stub compiles so
// Phase 2's RED tests (AC 01-02 / AC 01-03) have a symbol to reference.

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
//	  chunkBudget <= 0 returns, at entry and before any packing work, the error
//	    "full-repo scan: model %q has no effective byte budget for a review
//	     payload (context window too small for the configured output reservation)"
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
//
// Phase 2 task 2.8 implements this body against AC 01-03's RED tests (task 2.7).
func partitionByBudget(entries []FileEntry, chunkBudget int64) ([][]FileEntry, error) {
	panic("partitionByBudget: not implemented — Phase 1 design-note stub; GREEN lands in Sprint 35.0 task 2.8")
}
