package reconcile

import (
	"strconv"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
)

// The context pre-fetch byte ceiling is spelled TWICE —
// payload.DefaultMaxPrefetchBytes is the value the RangeBuilder falls back to
// when no option is supplied, and registry.DefaultMaxPrefetchBytes is the value
// max_prefetch_bytes resolves to when no tier sets it. They must agree, or an
// unconfigured project's pre-fetched context is bounded by one number while its
// documentation and its config template state another.
//
// The two cannot reference one another: internal/registry would have to import
// internal/payload (or the reverse) and neither dependency exists. This test is
// the seam that keeps them in lockstep instead — it lives here, in
// internal/reconcile, because that is where this repo keeps cross-package drift
// guards (see claim_ledger_default_test.go and
// justification_record_boundary_test.go), and because a test in either package
// would have to add exactly the import the split exists to avoid.
func TestPrefetchDefault_RegistryAndPayloadAgree(t *testing.T) {
	if registry.DefaultMaxPrefetchBytes != payload.DefaultMaxPrefetchBytes {
		t.Errorf("registry.DefaultMaxPrefetchBytes (%d) and payload.DefaultMaxPrefetchBytes (%d) must be equal: "+
			"an unconfigured project would otherwise retrieve context up to one bound while max_prefetch_bytes documents the other",
			registry.DefaultMaxPrefetchBytes, payload.DefaultMaxPrefetchBytes)
	}
}

// docs/registry.md is the published contract for max_prefetch_bytes, and its two
// load-bearing claims are the default value and the meaning of 0. A doc that
// says "disabled" while the code treats 0 as unlimited would invite an operator
// to switch the feature off and get an unbounded read of repository source
// instead — the failure direction that matters here, since the section's bytes
// are exempt from every byte budget.
func TestPrefetchDefault_DocumentedInRegistryDoc(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	if !strings.Contains(doc, "`max_prefetch_bytes`") {
		t.Fatal("docs/registry.md must document max_prefetch_bytes")
	}
	row := docRow(t, doc, "`max_prefetch_bytes`")
	// The comparison must track the CONSTANT, not a copy of it: hardcoding the
	// value here made the drift guard undetectable in exactly the direction it
	// exists to catch — bump the constant, leave the doc, and it stayed green.
	if !strings.Contains(row, strconv.FormatInt(registry.DefaultMaxPrefetchBytes, 10)) {
		t.Errorf("docs/registry.md must state the embedded default (%d) for max_prefetch_bytes; row was: %s",
			registry.DefaultMaxPrefetchBytes, row)
	}
	// The zero-disables contract is pinned as a PHRASE, not as independent
	// tokens: the row mentions 0 ("the injected section carries Size 0") and
	// "disabled" ("fails safe to disabled") in sentences unrelated to that
	// contract, so a token search stays green even with the load-bearing clause
	// deleted. Markdown emphasis is formatting, not contract — strip it before
	// matching, case-folded.
	plain := strings.ToLower(strings.NewReplacer("`", "", "*", "").Replace(row))
	if !strings.Contains(plain, "0 disables") {
		t.Errorf("docs/registry.md's max_prefetch_bytes row must state as a phrase that 0 disables the feature; row was: %s", row)
	}
}
