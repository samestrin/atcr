package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maxAdvisedReplicates is the ceiling `benchmark run` warns above, and two prose
// surfaces name it: the --replicates flag help and docs/benchmark.md. Raising the
// const without them leaves an operator told "above 20" by a binary that warns above
// some other number. The help is built from the const, so it cannot drift; the doc is
// prose, so this test reads it and fails until it is edited to match.
func TestBenchmarkRun_ReplicatesCeilingNamedByHelpAndDocs(t *testing.T) {
	ceiling := fmt.Sprint(maxAdvisedReplicates)

	flag := newBenchmarkRunCmd().Flags().Lookup("replicates")
	require.NotNil(t, flag, "benchmark run has no --replicates flag")
	assert.Contains(t, flag.Usage, "above "+ceiling+" the run warns and continues",
		"the --replicates help must name maxAdvisedReplicates (%s), the value the run warns above", ceiling)

	raw, err := os.ReadFile(filepath.Join("..", "docs", "benchmark.md"))
	require.NoError(t, err)
	// The doc is hard-wrapped, so a sentence can break anywhere; collapse whitespace.
	doc := strings.Join(strings.Fields(string(raw)), " ")
	for _, want := range []string{
		"Above " + ceiling + " replicates the command logs a",
		"`--replicates is above " + ceiling + "` warning",
	} {
		assert.Contains(t, doc, want,
			"docs/benchmark.md must name maxAdvisedReplicates (%s) in its --replicates section", ceiling)
	}
}
