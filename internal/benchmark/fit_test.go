package benchmark

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The public submission must not carry the fit rows (Epic 35.16.11.2.2.8 T2): they
// judge call health for the operator, the board scores none of it, and leaving them
// out is what keeps `benchmark export` of a --replicates run byte-identical to an
// N=1 run.
//
// Reverse the decision HERE first, with a submission_schema bump, never as a silent
// schema change.
func TestBuildSubmission_DoesNotPublishFit(t *testing.T) {
	at := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	base := RunResult{Suite: "s", SuiteVersion: "1"}
	withFit := base
	withFit.Fit = []ReviewerFit{
		{Model: "m", Persona: "p", CaseID: "case-01", Replicate: 2, Outcome: OutcomeClean, ChunkCount: 1, SilentChunks: 1},
	}

	plain, err := json.Marshal(BuildSubmission(base, at))
	require.NoError(t, err)
	fit, err := json.Marshal(BuildSubmission(withFit, at))
	require.NoError(t, err)
	assert.Equal(t, string(plain), string(fit))
	assert.NotContains(t, string(fit), "reviewer_fit")
}

// The fit rows serialize under their own key, and a run-result without them omits
// it, so it is byte-identical to one written before the field existed.
func TestRunResultFitRoundTrip(t *testing.T) {
	clean, err := json.Marshal(RunResult{Suite: "s", SuiteVersion: "1"})
	require.NoError(t, err)
	assert.NotContains(t, string(clean), "reviewer_fit")

	data, err := json.Marshal(RunResult{Suite: "s", SuiteVersion: "1", Fit: []ReviewerFit{
		{Model: "m", Persona: "p", CaseID: "c", Replicate: 1, Outcome: OutcomeTruncated, Findings: 0, TokensOut: 32768, ChunkCount: 2, TimedOut: true},
	}})
	require.NoError(t, err)
	assert.Contains(t, string(data),
		`"reviewer_fit":[{"model":"m","persona":"p","case_id":"c","replicate":1,"outcome":"truncated","findings":0,"tokens_out":32768,"chunk_count":2,"silent_chunks":0,"timed_out":true}]`)

	var back RunResult
	require.NoError(t, json.Unmarshal(data, &back))
	require.Len(t, back.Fit, 1)
	assert.Equal(t, 32768, back.Fit[0].TokensOut)
	assert.True(t, back.Fit[0].TimedOut)
}
