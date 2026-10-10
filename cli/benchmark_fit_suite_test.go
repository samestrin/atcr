package cli

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.11.2.2.8.1 T1: the fit-v1 suite's one case must reproduce the
// failure conditions it exists for — a payload a 128k-window agent reviews in at
// least two chunks — without overflowing the default byte budget, which would mark
// every lane incomplete and hide the fit signal. This is the tripwire for both:
// it fails if the case ever fits one chunk (window or chunker defaults grew) or
// trips the byte budget (the default budget shrank below the case's 502,071 bytes).

// fitSuitePath is the committed fit-v1 suite, relative to cli/.
const fitSuitePath = "../benchmarks/fit-v1"

func TestFitV1Suite_128kAgentSplitsCaseWithoutTruncation(t *testing.T) {
	// The model is absent from the static window table, so the 128k window can only
	// come from the declaration.
	cfg := benchCfg([3]string{"greta", "unlisted-fit-model", "greta"})
	window := 128000
	g := cfg.Registry.Agents["greta"]
	g.ContextWindowTokens = &window
	cfg.Registry.Agents["greta"] = g
	// Set here rather than inherited: the embedded default is bulk, under which a
	// 128k lane sheds files and reads incomplete (see benchmarks/fit-v1/NOTICE.md).
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.PayloadByteBudget = registry.DefaultPayloadByteBudget

	rr, err := executeBenchmarkRunReplicates(context.Background(), cfg, stubCompleter{}, fitSuitePath, replicatesGen, "", 1)
	require.NoError(t, err)

	assert.Equal(t, "fit-v1", rr.Suite)
	require.NotEmpty(t, rr.Fit, "a fit-v1 run must record reviewer_fit rows")
	for _, f := range rr.Fit {
		assert.GreaterOrEqual(t, f.ChunkCount, 2,
			"%s/%s on %s: a 128k-window agent must review the case in at least 2 chunks", f.Model, f.Persona, f.CaseID)
		assert.NotEqual(t, benchmark.OutcomeIncomplete, f.Outcome,
			"%s/%s on %s: the case must not trip the byte budget", f.Model, f.Persona, f.CaseID)
	}
}
