package reconcile

import (
	"strings"
	"testing"
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
// Asserted on the load-bearing nouns rather than on a whole sentence, following
// TestClaimLedgerDefault_DocumentedInRegistryDoc's idiom in this package, so an
// ordinary rewording passes while a factual regression fails.
func TestMaxContextLines_DocumentsTheDeliveredLineGate(t *testing.T) {
	row := docRow(t, readRepoFile(t, "../../docs/registry.md"), "`max_context_lines`")

	for _, must := range []struct{ token, why string }{
		{"delivered", "the cap is measured against a chunk's DELIVERED line count, preamble included — not against the named file's own diff"},
		{"max_claim_bytes", "when the warning reports engine-rendered preamble lines the lever is max_claim_bytes; this field is the wrong knob and the row must say so"},
	} {
		if !strings.Contains(strings.ToLower(row), strings.ToLower(must.token)) {
			t.Errorf("docs/registry.md's max_context_lines row must state %q: %s\nrow was: %s", must.token, must.why, row)
		}
	}

	// The code half of the drift guard. If the gate ever goes back to comparing
	// the named file's own line count, the row above becomes wrong again in the
	// opposite direction, and this is the test that says so.
	if !strings.Contains(readRepoFile(t, "../fanout/review.go"), "deliveredLines > ml") {
		t.Error("internal/fanout no longer gates the oversize warning on deliveredLines; " +
			"docs/registry.md's max_context_lines row describes a delivered-line cap the code no longer implements")
	}
}
