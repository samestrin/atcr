package reconcile

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
)

// The claim-ledger byte ceiling is spelled TWICE — payload.DefaultMaxClaimBytes
// is the value the RangeBuilder falls back to when no option is supplied, and
// registry.DefaultMaxClaimBytes is the value max_claim_bytes resolves to when no
// tier sets it. They must agree, or an unconfigured project's ledger is bounded
// by one number while its documentation and its config template state another.
//
// The two cannot reference one another: internal/registry would have to import
// internal/payload (or the reverse) and neither dependency exists today. This
// test is the seam that keeps them in lockstep instead — it lives here, in
// internal/reconcile, because that is where this repo keeps cross-package drift
// guards (see justification_record_boundary_test.go), and because a test in
// either package would have to add exactly the import the split exists to avoid.
func TestClaimLedgerDefault_RegistryAndPayloadAgree(t *testing.T) {
	if registry.DefaultMaxClaimBytes != payload.DefaultMaxClaimBytes {
		t.Errorf("registry.DefaultMaxClaimBytes (%d) and payload.DefaultMaxClaimBytes (%d) must be equal: "+
			"an unconfigured project would otherwise read commit text up to one bound while max_claim_bytes documents the other",
			registry.DefaultMaxClaimBytes, payload.DefaultMaxClaimBytes)
	}
}

// docs/registry.md is the published contract for max_claim_bytes, and its two
// load-bearing claims are the default value and the meaning of 0. A doc that
// says "disabled" while the code treats 0 as unlimited would invite an operator
// to switch the feature off and get an unbounded read instead — the failure
// direction that matters here, since the ledger's bytes are exempt from every
// byte budget.
func TestClaimLedgerDefault_DocumentedInRegistryDoc(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	if !strings.Contains(doc, "`max_claim_bytes`") {
		t.Fatal("docs/registry.md must document max_claim_bytes")
	}
	row := docRow(t, doc, "`max_claim_bytes`")
	if !strings.Contains(row, "8192") {
		t.Errorf("docs/registry.md must state the embedded default (%d) for max_claim_bytes; row was: %s",
			registry.DefaultMaxClaimBytes, row)
	}
	for _, must := range []string{"0", "disable"} {
		if !strings.Contains(strings.ToLower(row), must) {
			t.Errorf("docs/registry.md's max_claim_bytes row must state that %q disables the feature; row was: %s", must, row)
		}
	}
}

// docRow returns the single markdown table row whose first cell contains key.
func docRow(t *testing.T, doc, key string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "| "+key) {
			return line
		}
	}
	t.Fatalf("no table row starting with %q in the document", key)
	return ""
}
