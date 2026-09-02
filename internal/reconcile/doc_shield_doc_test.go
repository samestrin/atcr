package reconcile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	reclib "github.com/samestrin/atcr/reconcile"
)

// TestDocShield_PublishedDocsMatchTheCode pins the serialized Tier 4 vocabulary
// against the two docs that teach it. The literal "doc_shield"
// (UnresolvedReasonDocShield, emit.go) is hard-coded in BOTH
// docs/code-review-backend.md and docs/scorecard.md, and the unresolved_state
// truth table in docs/code-review-backend.md enumerates the four published
// UnresolvedState* constants — none of it compiled against the code, so a rename
// would silently leave the docs teaching values no producer emits. The states
// are asserted as backticked tokens so ordinary English ("applied", "disabled")
// cannot satisfy them spuriously.
//
// This is a characterization test: it passes on the day it is written and earns
// its place by failing when the constants move.
func TestDocShield_PublishedDocsMatchTheCode(t *testing.T) {
	backend, err := os.ReadFile(filepath.Join("..", "..", "docs", "code-review-backend.md"))
	if err != nil {
		t.Fatalf("read docs/code-review-backend.md: %v", err)
	}
	scorecardDoc, err := os.ReadFile(filepath.Join("..", "..", "docs", "scorecard.md"))
	if err != nil {
		t.Fatalf("read docs/scorecard.md: %v", err)
	}

	for _, state := range []string{
		reclib.UnresolvedStateApplied,
		reclib.UnresolvedStateDisabled,
		reclib.UnresolvedStateUnavailable,
		reclib.UnresolvedStateIncomplete,
	} {
		want := "`" + state + "`"
		if !strings.Contains(string(backend), want) {
			t.Errorf("docs/code-review-backend.md truth table is missing %s (reclib.UnresolvedState*)", want)
		}
	}
	for name, doc := range map[string]string{
		"docs/code-review-backend.md": string(backend),
		"docs/scorecard.md":           string(scorecardDoc),
	} {
		if !strings.Contains(doc, UnresolvedReasonDocShield) {
			t.Errorf("%s is missing the %q literal (UnresolvedReasonDocShield, emit.go)", name, UnresolvedReasonDocShield)
		}
	}
}
