package stream

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteSourceV2_RejectsOverBoundEstMinutes pins the host write-path guard
// from TD internal/verify/severity.go:40: WriteSourceV2 is a HOST write path,
// so an est_minutes above maxModelEstMinutes must be rejected at write time —
// the decode-side clamp would otherwise turn a typo'd 20000 into exactly
// 10080, indistinguishable from a genuine week-long estimate, and a verify
// ceiling AT the bound would then treat the clamped row as autofix-eligible.
// The boundary itself is legal; in-bound findings are written unchanged.
func TestWriteSourceV2_RejectsOverBoundEstMinutes(t *testing.T) {
	err := WriteSourceV2(&strings.Builder{}, []Finding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "pa", Fix: "fa",
			Category: "correctness", EstMinutes: maxModelEstMinutes + 1, Evidence: "ev", Reviewer: "rev"},
	})
	require.Error(t, err, "an est above the clamp bound must be rejected at the write path")
	require.Contains(t, err.Error(), "est_minutes")

	// The boundary and everything below write fine.
	var b strings.Builder
	require.NoError(t, WriteSourceV2(&b, []Finding{
		{Severity: "HIGH", File: "a.go", Line: 1, Problem: "pa", Fix: "fa",
			Category: "correctness", EstMinutes: maxModelEstMinutes, Evidence: "ev", Reviewer: "rev"},
		{Severity: "LOW", File: "b.go", Line: 2, Problem: "pb", Fix: "fb",
			Category: "style", EstMinutes: 5, Evidence: "ev", Reviewer: "rev"},
	}))
}
