package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// docs/benchmark.md's slot_failures[] sentence calls an unmeasured slot a
// "wholly abandoned salvage" but never defines "abandoned" — a reader must
// infer from the later parenthetical that it means salvaged and NOT on-stop.
// The `incomplete` row further down defines it ("Only an abandoned salvage
// counts: a reply the model finished on a stop reason ... keeps its
// findings"), so the first use of the word in the doc must carry the same
// definition inline.
func TestBenchmarkDoc_SlotFailuresDefineAbandonedInline(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")

	line := docLineContaining(t, doc, "**wholly** abandoned salvage")
	assert.Contains(t, line, "abandoned = salvaged without `salvaged_on_stop`",
		"the slot_failures[] sentence uses \"abandoned\" before any definition; it must define it inline as salvaged without `salvaged_on_stop`")
}
