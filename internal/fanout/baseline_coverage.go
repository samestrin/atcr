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
	// Every dispatched slot succeeded AND every slot reviewed its full tagged set →
	// the whole payload was covered regardless of how it was partitioned, so no
	// attribution is needed and nothing is excluded.
	// Requires an outcome for EVERY slot: with fewer results than slots some slot has
	// no outcome at all, and with MORE some result has no corresponding slot, so in
	// either direction "all succeeded" is not established and the per-slot
	// attribution below (which leaves unmatched slots uncovered) must run instead.
	//
	// The second condition is Epic 35.16.5.4 T3a, and it is not defensive — it is
	// the arm that fires. A slot served by a successful fallback returns StatusOK
	// (invokeSlot returns the fallback's own Result), so the scenario this epic
	// creates — primary fails, fallback re-packs the payload to its own budget and
	// succeeds — is an ALL-OK run. Under the pre-epic inference it short-circuited
	// here and the files the fallback SHED were recorded as reviewed, then skipped
	// by the next scan: a file nothing ever read, never read again. A re-packed
	// serving agent therefore forces the per-slot attribution below, which reads
	// what each slot's server actually reviewed.
	//
	// COST, deliberately accepted: ONE re-packed slot disables the short-circuit
	// for the WHOLE run, forcing the O(slots × tagged files) attribution pass
	// below instead of the whole-payload inference. Coverage itself does not
	// change: every baseline slot is tagged by construction (the chunk-partition
	// and bulk paths tag every slot; the only untagged-slot producer is the
	// chunkDiff path, gated on ReviewStrategy == chunked && !baseline), so the
	// untagged-but-succeeded degradation is defensive only — it fires on a
	// broken engine contract, not on any payload this epic creates, and when it
	// does fire it fails open (a needless re-scan, never a silent skip). Gating
	// per-slot instead would mean trusting the short-circuit's whole-payload
	// inference for some slots while denying it for others, which is not a
	// coherent middle.
	allOK := len(results) == len(slots)
	for _, r := range results {
		if r.Status != StatusOK || r.servedRePacked {
			allOK = false
			break
		}
	}
	if allOK {
		return nil
	}
	covered := make(map[string]struct{}, len(reviewed))
	for i := range slots {
		// Defensive bound: Engine.Run always returns one result per slot, but an
		// index mismatch must leave the unmatched slots UNCOVERED (fail-open toward
		// re-review) rather than panic or silently over-record.
		if i >= len(results) {
			break
		}
		if results[i].Status != StatusOK {
			continue
		}
		// The tag of the agent that SERVED this slot (Epic 35.16.5.4 T3b), not the
		// slot's Primary. They are the same list unless a fallback re-packed the
		// payload against its own budget, in which case only the files it kept were
		// actually reviewed. Reading the Primary's tag there would record the shed
		// files as covered, and CommitBaselineIndex would skip them next scan.
		//
		// An untagged server iterates zero times here, so it contributes NO coverage
		// even when it succeeded — see the sentinel-polarity note above. That
		// polarity is unchanged; it now hangs off the serving agent's tag.
		for _, p := range servedCoverage(slots[i], results[i]) {
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

// servedCoverage returns the files the agent that actually served this slot
// reviewed — the only set it may vouch for (Epic 35.16.5.4 T3).
//
// invokeSlot stamps the served tag onto every succeeding result, so in production
// the SECOND return is always the operative one for a tagged slot. The fall-back
// to the slot's Primary covers a result built OUTSIDE that path (the engine's
// synthesized panic/cancel results, and direct construction in tests), where
// "which agent served" was never recorded and the primary is the only agent it
// could have been. That preserves the pre-epic contract for such a result rather
// than silently making it vouch for nothing.
//
// The guard is what keeps that safe: the fall-back is available ONLY when the
// result is known not to have re-packed. A re-packed server always carries its own
// tag (invokeSlot stamps both fields from the same agent, in the same block), so a
// re-pack can never reach the primary's full tag through here — which is the exact
// falsification this function exists to prevent. A re-packed result with no tag
// vouches for nothing, the safe direction.
//
// The nil test is deliberate, not a shortcut for len() == 0: an EMPTY-but-non-nil
// tag vouches for NOTHING, where nil falls back to the primary's whole tag. The
// empty case is unreachable from the re-fit arm — its keepSmallestEntry/AllDropped
// handling guarantees a non-empty kept set when it succeeds — so the polarity
// above never has to arbitrate it, but the distinction is load-bearing if a future
// producer ever stamps an empty tag.
func servedCoverage(s Slot, r Result) []string {
	if r.servedChunkFiles == nil && !r.servedRePacked {
		return s.Primary.chunkFiles
	}
	return r.servedChunkFiles
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
