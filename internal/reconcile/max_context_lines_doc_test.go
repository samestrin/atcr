package reconcile

import (
	"strconv"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
)

// docs/registry.md's max_context_lines row is the ONLY documentation of the
// oversize-chunk warning — the stderr text has no machine reader, and no other
// page describes when it fires. The row's closing sentence used to be true:
// "a single file larger than this cap is sent as its own oversized chunk with a
// warning". It is not true any more. The gate in internal/fanout compares the
// chunk's DELIVERED line count — engine-rendered preamble included — against
// the cap, so the warning also fires for a file comfortably INSIDE the cap when
// preamble lines push the chunk over it.
//
// That direction matters because the claim ledger is the one production source
// of a pre-first-marker preamble, and its lever is max_claim_bytes, not this
// field. An operator who reads the old sentence tunes max_context_lines, the
// warning does not go away, and nothing tells them which knob was theirs.
//
// Built on this package's doc-row idiom — docRow, shared with
// TestClaimLedgerDefault_DocumentedInRegistryDoc — but asserted on load-bearing
// PHRASES rather than on the bare tokens that sibling anchors on, so an ordinary
// rewording passes while a factual regression fails.
//
// A bare noun was not enough, and it failed in the one direction this test
// exists to catch. strings.Contains(row, "delivered") passes on a row that says
// "the cap is NOT measured against delivered lines", and on a row where the word
// survives in an unrelated clause while the load-bearing sentence is reverted. A
// phrase carries the claim's verb and subject with it, so a negation or a revert
// no longer slips through, while a reworded paragraph around it still passes.
func TestMaxContextLines_DocumentsTheDeliveredLineGate(t *testing.T) {
	row := docRow(t, readRepoFile(t, "../../docs/registry.md"), "`max_context_lines`")

	for _, must := range []struct{ token, why string }{
		{"measured against a chunk's **delivered** line count", "the cap is measured against a chunk's DELIVERED line count, preamble included — not against the named file's own diff. The whole phrase, not the bare word: \"delivered\" alone survives a negation of this very clause"},
		{"the lever that shrinks them is `max_claim_bytes`", "when the warning reports engine-rendered preamble lines the lever is max_claim_bytes; this field is the wrong knob and the row must say so. The whole phrase, so a row that merely mentions the key in some other clause does not satisfy it"},
		{"whose lever is `max_prefetch_bytes`", "context-aware pre-fetching (Epic 35.16.8) adds a SECOND preamble section, with a larger default (16384) than the claim ledger's. An operator who reads only the max_claim_bytes sentence tunes the smaller of the two knobs, the warning does not go away, and nothing tells them the other section exists"},
	} {
		if !strings.Contains(strings.ToLower(row), strings.ToLower(must.token)) {
			t.Errorf("docs/registry.md's max_context_lines row must state %q: %s\nrow was: %s", must.token, must.why, row)
		}
	}

	// The phrase above pins the KEY name; the same sentence also restates its
	// default as a bare number — a second doc site for
	// registry.DefaultMaxPrefetchBytes alongside the max_prefetch_bytes row. Pin
	// the number to the constant, not to a literal: a default change must turn
	// this row red, or both doc sites go stale with a green suite.
	wantDefault := "`" + strconv.FormatInt(registry.DefaultMaxPrefetchBytes, 10) + "` by default"
	if !strings.Contains(row, wantDefault) {
		t.Errorf("docs/registry.md's max_context_lines row must restate the prefetch default as %s "+
			"(code-derived from registry.DefaultMaxPrefetchBytes, not a hardcoded literal)\nrow was: %s", wantDefault, row)
	}

	// The code half of the drift guard asserts the LINK to the behavioural owner,
	// not the gate's own source text.
	//
	// It used to grep ../fanout/review.go for the literal
	// `fileCount == 1 && deliveredLines > ml`. That added no falsification power a
	// genuine regression does not already have — the gate is pinned behaviourally,
	// in its own package, by the test named below and its sibling, and those fail
	// too — while adding a false-alarm surface. Reordering the conjuncts, extracting
	// the predicate into a named helper, hoisting `ml`, or a pure rename each broke
	// it with a message blaming docs/registry.md, sending the reader to the wrong
	// file over a change that altered no behaviour.
	//
	// A test NAME is a far stabler token than a predicate's source text. What is
	// left worth guarding is that the behavioural owner still exists: if it is
	// renamed or deleted, the row above loses its backing and this must say so.
	const behaviouralOwner = "TestBuildSlots_ChunkedWarnsWhenPreamblePushesDeliveredTotalOverBudget"
	if !strings.Contains(readRepoFile(t, "../fanout/chunker_warnings_test.go"), behaviouralOwner) {
		t.Errorf("%s is gone from internal/fanout. It is the behavioural owner of the delivered-line gate "+
			"that docs/registry.md's max_context_lines row describes, so that row now has no test behind it. "+
			"If it was renamed, re-point this guard; if it was deleted, the row's claim is unpinned.", behaviouralOwner)
	}
}
