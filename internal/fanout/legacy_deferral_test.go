package fanout

// legacyCeiling is the 64-bin baseline ceiling Epic 35.16.13 T2 deferred past.
// Epic 35.16.14 T5 replaced it with an up-front refusal (checkUnitBudget), so a
// new run never defers, but a chunks.json written before then still carries
// deferred bins and must keep resuming and counting as unreviewed.
const legacyCeiling = 64

// withLegacyDeferral rebuilds a baseline slot list the way the pre-T5 ceiling
// built it: each persona keeps its first legacyCeiling slots, and the file sets
// of the rest ride on its last kept slot as deferred bins. It lets the ledger,
// coverage and resume tests produce the deferred run an old ledger records.
// Slots of one persona are contiguous, as buildSlots emits them.
func withLegacyDeferral(slots []Slot) []Slot {
	var kept []Slot
	count := map[string]int{}
	last := map[string]int{}
	for _, s := range slots {
		name := s.Primary.Name
		if count[name] < legacyCeiling {
			kept = append(kept, s)
			last[name] = len(kept) - 1
		} else {
			i := last[name]
			kept[i].deferredBins = append(kept[i].deferredBins, s.Primary.chunkFiles)
		}
		count[name]++
	}
	return kept
}
