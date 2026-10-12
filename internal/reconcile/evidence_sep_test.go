package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"

	reclib "github.com/samestrin/atcr/reconcile"
)

// TD internal/verify/executor.go:757: joinEvidence joined with a bare " / "
// literal, so nothing tied the producer to the readers that split on it
// (internal/verify's attribution guards, ghaction.FixAttribution). It joins with
// the exported ClusterEvidenceSep now, which those readers consume.
func TestJoinEvidence_UsesClusterEvidenceSep(t *testing.T) {
	got := joinEvidence([]JSONFinding{{Evidence: "Found by kai"}, {Evidence: "fix by opus"}})
	assert.Equal(t, "Found by kai"+ClusterEvidenceSep+"fix by opus", got)
	assert.Equal(t, " / ", ClusterEvidenceSep)
}

// SplitEvidence's documented contract, pinned directly: it splits on both
// ClusterEvidenceSep and reclib.EvidenceSep, never on a bare "/", and returns the
// segments in order, untrimmed, with empty ones kept.
func TestSplitEvidence(t *testing.T) {
	cases := []struct {
		name     string
		evidence string
		want     []string
	}{
		{"empty string is one empty segment", "", []string{""}},
		{"cluster separator only", ClusterEvidenceSep, []string{"", ""}},
		{"pipeline separator only", reclib.EvidenceSep, []string{"", ""}},
		{"cluster merge", "Found by kai" + ClusterEvidenceSep + "fix by opus", []string{"Found by kai", "fix by opus"}},
		{"pipeline join", "Found by kai" + reclib.EvidenceSep + "fix by opus", []string{"Found by kai", "fix by opus"}},
		{
			"mixed separators keep their order",
			"a" + reclib.EvidenceSep + "b" + ClusterEvidenceSep + "c" + reclib.EvidenceSep + "d",
			[]string{"a", "b", "c", "d"},
		},
		{"adjacent cluster separators keep the empty segment", "a" + ClusterEvidenceSep + ClusterEvidenceSep + "b", []string{"a", "", "b"}},
		{"adjacent pipeline separators keep the empty segment", "a" + reclib.EvidenceSep + reclib.EvidenceSep + "b", []string{"a", "", "b"}},
		{"adjacent mixed separators keep the empty segment", "a" + ClusterEvidenceSep + reclib.EvidenceSep + "b", []string{"a", "", "b"}},
		{"segments are untrimmed", "  a " + ClusterEvidenceSep + " b\t" + reclib.EvidenceSep + "c ", []string{"  a ", " b\t", "c "}},
		{"a bare slash is not a separator", "path/to/fix by hand", []string{"path/to/fix by hand"}},
		{"a slash without both spaces is not a separator", "a /b" + ClusterEvidenceSep + "c/ d", []string{"a /b", "c/ d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SplitEvidence(tc.evidence))
		})
	}
}
