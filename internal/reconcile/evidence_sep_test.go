package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
