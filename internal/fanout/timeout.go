package fanout

import "github.com/samestrin/atcr/internal/registry"

// Timeout scaling (Epic 19.10 F6). Task 03's window-aware chunking fans a small-
// context-window model's diff into multiple chunk-Slots for the SAME persona. On a
// serial lane those chunk-Slots run one at a time, so the persona's aggregate
// wall-clock is roughly chunkCount x per-call duration — even though each chunk
// fits inside its own per-agent timeout, the SUM across chunks can blow the single
// flat runEngine deadline (the confirmed greta/vera/brad 600s wall). These helpers
// scale both deadline seams from the already-resolved base timeout and the chunk
// count Task 03 threaded onto Agent.ChunkTotal — without touching internal/
// registry's resolvers, reading only its schema-validated clamp ceiling.

// chunkTimeoutCeilingFactor caps how much a persona's per-call and aggregate
// deadlines can grow with its chunk count. A conservative estimate, not a live
// measurement (mirroring the byte/token ratio framing of Tasks 01-03): a persona
// fanned into many chunks on a slow local backend needs proportionally more wall
// clock, but the growth is capped so a pathological chunk count cannot inflate the
// deadline without bound. 8x covers the observed worst case — a 32k-window model
// over the confirmed 6,429-insertion diff bin-packs into ~6-8 chunks — with
// headroom, while still clamping to registry.MaxTimeoutSecs above that.
const chunkTimeoutCeilingFactor = 8

// scaledTimeoutSecs scales a resolved base timeout (seconds) by how many chunks a
// persona's diff was split into (Epic 19.10 F6). It is deterministic from
// (baseSecs, chunkTotal) alone — no live/network input — monotonically non-
// decreasing in chunkTotal, a no-op at chunkTotal <= 1 (the unchunked/bulk case,
// zero regression for every non-chunked caller), and clamped to
// registry.MaxTimeoutSecs so a pathological chunk count can never produce an
// unbounded deadline. baseSecs <= 0 ("global deadline only", an unset per-agent
// timeout) returns unchanged so chunking never conjures a per-call deadline where
// there was none.
func scaledTimeoutSecs(baseSecs, chunkTotal int) int {
	if baseSecs <= 0 || chunkTotal <= 1 {
		return baseSecs
	}
	factor := chunkTotal
	if factor > chunkTimeoutCeilingFactor {
		factor = chunkTimeoutCeilingFactor
	}
	scaled := baseSecs * factor
	// Clamp to the schema ceiling; also treat any integer overflow (scaled wrapping
	// below baseSecs) as "over the ceiling" defensively.
	if scaled > registry.MaxTimeoutSecs || scaled < baseSecs {
		return registry.MaxTimeoutSecs
	}
	return scaled
}

// maxLaneChunkTotal returns the largest Agent.ChunkTotal across ALL slots (serial
// AND parallel), feeding runEngine's aggregate-deadline scaling (Epic 19.10 F6).
// Every chunk-Slot of a persona already carries that persona's full chunk count on
// Primary.ChunkTotal (not 1-per-slot), so the max is the largest single-persona
// chunk count without needing to group/sum.
//
// Why parallel slots are INCLUDED (they were not in the first cut): the per-call
// deadline in invokeAgent is a child of this aggregate runCtx, and a child
// context.WithTimeout can only ever SHORTEN a deadline, never extend it past its
// parent — so scaling the per-call deadline alone cannot lift a parallel chunked
// persona above the flat aggregate wall. The aggregate parent must therefore be
// large enough for the worst chunked persona regardless of lane (the production
// roster runs serial_agents: [], so the confirmed greta/vera/brad timeouts are all
// parallel). This is safe for unrelated non-chunked agents: each is still bounded
// to its OWN per-call deadline (ChunkTotal <= 1 → scaledTimeoutSecs is a no-op),
// so a larger aggregate never lets a non-chunked agent hang longer — only agents
// that legitimately chunked get the extra room. Returns 0 when nothing is chunked,
// making scaledTimeoutSecs a no-op (the flat deadline is preserved).
func maxLaneChunkTotal(slots []Slot) int {
	maxChunks := 0
	for _, s := range slots {
		if s.Primary.ChunkTotal > maxChunks {
			maxChunks = s.Primary.ChunkTotal
		}
	}
	return maxChunks
}

// aggregateTimeoutFactor derives the run-wide deadline scaling factor for runEngine
// (Epic 19.10 TD-005). maxLaneChunkTotal alone models only the worst SINGLE persona's
// serial chunk sum; it underestimates wall-clock when MANY personas' chunk-Slots
// contend for a limited parallel lane. The engine's shared semaphore admits
// maxParallel non-serial slots at a time, so N parallel slots complete in
// ceil(N/maxParallel) waves. This returns the LARGER of the serial-lane component
// and the parallel-lane wave count, so neither lane is under-covered (over-scaling a
// deadline is safe under the Conservatism NFR; under-scaling is the timeout bug this
// fixes). A non-positive maxParallel is an unbounded lane — every slot runs at once,
// one wave — leaving the serial component to govern. Serial slots carry their own
// summed per-call deadlines and are not parallel-lane load, so they are excluded from
// the wave numerator.
func aggregateTimeoutFactor(slots []Slot, maxParallel int) int {
	factor := maxLaneChunkTotal(slots)
	if maxParallel > 0 {
		parallelSlots := 0
		for _, s := range slots {
			if !s.Serial {
				parallelSlots++
			}
		}
		if waves := (parallelSlots + maxParallel - 1) / maxParallel; waves > factor {
			factor = waves
		}
	}
	return factor
}
