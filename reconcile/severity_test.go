package reconcile

import (
	"strings"
	"testing"
)

// TestSeverityRank_AllKeysUppercase verifies that every key in SeverityRank is
// uppercase, matching the NormalizeSeverity contract. If a lowercase key is
// added, NormalizeSeverity("lowercase") would return "LOWERCASE" which wouldn't
// match the map key, causing silent lookup failures.
func TestSeverityRank_AllKeysUppercase(t *testing.T) {
	for key := range SeverityRank {
		if key != strings.ToUpper(key) {
			t.Errorf("SeverityRank key %q is not uppercase — NormalizeSeverity lookups will fail", key)
		}
	}
}
