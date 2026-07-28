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
// SENTINEL POLARITY — an UNTAGGED (nil chunkFiles) slot contributes NO coverage.
// This direction is deliberate and load-bearing. buildSlots has several slot-creating
// paths, and not all of them can attribute files to a slot: the review_strategy=chunked
// branch splits the payload TEXT on diff markers, and a files-mode baseline payload can
// reach it (isDiffFileMarker also matches `=== FILE:`, and tracked content such as
// *.patch fixtures carries real `diff --git` lines). If nil meant "covers everything",
// one succeeded untagged chunk would vouch for files a sibling chunk failed to review,
// and those files would be recorded and then SILENTLY SKIPPED on the next scan — the one
// outcome this index must never produce. With this polarity, any path that does not (or
// cannot) tag its slots degrades to re-review, never to skip.
//
// Full coverage is therefore established by the all-succeeded shortcut below rather than
// by a sentinel: when every dispatched slot succeeded, the whole payload was reviewed by
// definition, whatever shape the fan-out took. That is what keeps the zero-failure path
// byte-identical to pre-35.2 behavior (AC2) without trusting any individual slot's tag.
//
// The result is always a subset of reviewed: a path a chunk carried but the global byte
// budget dropped is absent from reviewed and never surfaces here, because there is
// nothing to exclude. Returns nil when nothing is uncovered.
func uncoveredBaselineFiles(slots []Slot, results []Result, reviewed map[string]string) map[string]struct{} {
	if len(reviewed) == 0 {
		return nil
	}
	if len(results) == 0 {
		// No slot ran, so nothing was reviewed. Report everything uncovered rather
		// than nil ("fully covered") — the caller then writes nothing at all.
		// Unreachable in practice (a baseline run always dispatches slots, and an
		// all-complete resume returns before the fan-out), but the safe direction.
		return allUncovered(reviewed)
	}
	// Every dispatched slot succeeded → the whole payload was covered regardless of
	// how it was partitioned, so no attribution is needed and nothing is excluded.
	// Requires an outcome for EVERY slot: with fewer results than slots some slot has
	// no outcome at all, and with MORE some result has no corresponding slot, so in
	// either direction "all succeeded" is not established and the per-slot
	// attribution below (which leaves unmatched slots uncovered) must run instead.
	allOK := len(results) == len(slots)
	for _, r := range results {
		if r.Status != StatusOK {
			allOK = false
			break
		}
	}
	if allOK {
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
		// An untagged slot cannot vouch for any file even when it succeeded — see the
		// sentinel-polarity note above.
		if results[i].Status != StatusOK {
			continue
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

// allUncovered returns every reviewed path as uncovered — the fail-open answer when
// no coverage evidence exists at all.
func allUncovered(reviewed map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(reviewed))
	for p := range reviewed {
		out[p] = struct{}{}
	}
	return out
}
