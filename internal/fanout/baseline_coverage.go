package fanout

// uncoveredBaselineFiles reports which of a baseline (--all/--dir) run's reviewed
// files were left UNREVIEWED because the chunk carrying them failed (Epic 35.2 /
// TD-013). The result is the set CommitBaselineIndex must exclude from the
// incremental file-hash write-back, so the next scan re-reviews exactly those files
// instead of the whole repository.
//
// It must be called on the RAW, pre-merge results: mergeResultGroup collapses a
// persona's per-chunk outcomes into a bare UnreviewedChunks count and discards chunk
// identity entirely, after which WHICH files went unreviewed is unrecoverable.
// Attribution is by INDEX: Engine.Run returns one Result per Slot in input order (a
// contract pinned by TestEngineRun_ResultsMatchSlotInputOrder), so results[i]
// describes slots[i] and the slot's chunkFiles tag names the files that result
// covered. A fallback-served slot is attributed to its primary's tag, which is
// correct — a fallback reviews the same chunk as the primary it substitutes for.
//
// Coverage is UNION across personas (clarified 2026-07-27): every persona partitions
// the repo against its OWN model window, so a file sits in different chunks per
// persona, and it counts as covered once ANY succeeded chunk carried it. That matches
// what the pre-35.2 gate already did — a persona whose chunks ALL failed reports
// UnreviewedChunks==0 and never blocked the write-back — so this is a strict
// improvement rather than a behavior change. Intersection semantics would newly
// discard files that are correctly recorded today.
//
// A nil chunkFiles tag means "this slot covers the whole payload" (every
// non-baseline slot, plus the baseline bulk fall-through for a single chunk or a
// non-positive per-agent chunk budget). A SUCCEEDED whole-payload slot therefore
// covers everything and the uncovered set is empty — which is what keeps the
// zero-failure path byte-identical to pre-35.2 behavior. A FAILED whole-payload slot
// covers nothing.
//
// The result is always a subset of reviewed: a path a chunk carried but the global
// byte budget dropped is absent from reviewed and never surfaces here, because there
// is nothing to exclude. Returns nil when nothing is uncovered.
func uncoveredBaselineFiles(slots []Slot, results []Result, reviewed map[string]string) map[string]struct{} {
	if len(reviewed) == 0 {
		return nil
	}
	covered := make(map[string]struct{}, len(reviewed))
	for i, s := range slots {
		// Defensive bound: Engine.Run always returns one result per slot, but an
		// index mismatch must leave the unmatched slots UNCOVERED (fail-open toward
		// re-review) rather than panic or silently over-record.
		if i >= len(results) {
			break
		}
		if results[i].Status != StatusOK {
			continue
		}
		if s.Primary.chunkFiles == nil {
			// Whole-payload slot succeeded: every reviewed file is covered, so no
			// further inspection can shrink the covered set.
			return nil
		}
		for _, p := range s.Primary.chunkFiles {
			covered[p] = struct{}{}
		}
	}
	var uncovered map[string]struct{}
	for path := range reviewed {
		if _, ok := covered[path]; ok {
			continue
		}
		if uncovered == nil {
			uncovered = make(map[string]struct{})
		}
		uncovered[path] = struct{}{}
	}
	return uncovered
}
