package reconcile

import "testing"

// TestScaffold is the Phase-1 smoke test: it references the package so the
// module must compile. Real behavior is exercised by the corpus moved in
// Phase 2; this only proves the nested module scaffold + replace wiring builds.
func TestScaffold(t *testing.T) {
	var _ Source
	var _ Finding
	var _ Verification
	if VerdictConfirmed != "confirmed" || VerdictRefuted != "refuted" || VerdictUnverifiable != "unverifiable" {
		t.Fatalf("verdict constants drifted: %q %q %q", VerdictConfirmed, VerdictRefuted, VerdictUnverifiable)
	}
}
