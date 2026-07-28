package fanout

import (
	"context"

	"github.com/samestrin/atcr/internal/log"
)

// uncoveredBaselineFiles reports which of a baseline run's reviewed files their chunk
// failed to cover — the set CommitBaselineIndex must exclude, so the next scan
// re-reviews exactly those instead of the whole repository. Returns nil when nothing
// is uncovered; the result is always a subset of reviewed.
//
// PRECONDITION: call on RAW, pre-merge results — mergeResultGroup discards chunk
// identity. Attribution is by INDEX (results[i] describes slots[i], pinned by
// TestEngineRun_ResultsMatchSlotInputOrder); a fallback-served slot is attributed to
// its primary's tag. Coverage is a UNION across personas: a file is covered once ANY
// succeeded chunk carried it. An UNTAGGED (nil chunkFiles) slot vouches for NOTHING,
// never "everything" — that polarity is what makes an unattributable slot degrade to
// re-review rather than to a silent skip. Full coverage is therefore established by
// the all-succeeded shortcut below, not by any per-slot sentinel.
//
// ctx carries the run logger: both defensive degradations below forfeit the whole
// incremental optimization, so neither may be silent.
func uncoveredBaselineFiles(ctx context.Context, slots []Slot, results []Result, reviewed map[string]string) map[string]struct{} {
	if len(reviewed) == 0 {
		return nil
	}
	if len(results) == 0 {
		// No slot ran, so nothing was reviewed. Report everything uncovered rather
		// than nil ("fully covered") — the caller then writes nothing at all.
		// Unreachable in practice (a baseline run always dispatches slots, and an
		// all-complete resume returns before the fan-out), but the safe direction.
		if len(slots) > 0 {
			log.FromContext(ctx).Warn("baseline coverage: the fan-out returned no results for a dispatched slot set; treating every reviewed file as uncovered, so the next scan re-reads the whole scope",
				"slots", len(slots), "reviewed_files", len(reviewed))
		}
		return allUncovered(reviewed)
	}
	if len(results) < len(slots) {
		// Engine.Run's one-result-per-slot contract is broken. The unmatched slots stay
		// uncovered (fail-open toward re-review), which silently forfeits coverage for
		// every file they carried.
		log.FromContext(ctx).Warn("baseline coverage: fewer results than dispatched slots — the Engine.Run index-correspondence contract is broken; the unmatched slots' files stay uncovered and will be re-scanned",
			"slots", len(slots), "results", len(results))
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
		if results[i].Status != StatusOK {
			continue
		}
		// An untagged (nil chunkFiles) slot iterates zero times here, so it
		// contributes NO coverage even when it succeeded — see the
		// sentinel-polarity note above.
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
