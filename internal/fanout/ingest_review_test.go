package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// looseDiff is a two-file loose unified diff (the suite-fixture format, no
// `diff --git` header) used across the ingestion tests.
const looseDiff = "--- a/pay.go\n" +
	"+++ b/pay.go\n" +
	"@@ -1,2 +1,2 @@\n" +
	" func total(items []*Item) int {\n" +
	"-\treturn items[0].Price\n" +
	"+\treturn sum(items)\n" +
	"--- a/query.go\n" +
	"+++ b/query.go\n" +
	"@@ -1,1 +1,1 @@\n" +
	"-safe\n" +
	"+unsafe\n"

// diffReq builds a range-less ReviewRequest writing to an explicit OutputDir, so
// the scaffold does not depend on a repo root having an .atcr tree.
func diffReq(root, out string) ReviewRequest {
	return ReviewRequest{
		Root:       root,
		OutputDir:  out,
		Branch:     "feature/test",
		Date:       "2026-06-10",
		TimeSuffix: "120000",
		StartedAt:  time.Unix(1000, 0).UTC(),
	}
}

// PrepareReviewFromDiff must scaffold a runnable PreparedReview from a loose diff
// and force every agent to diff mode — even when the project's configured mode is
// "blocks" — writing a single payload/diff.txt and a manifest whose per-agent map
// records "diff" for all agents. The manifest's range fields are empty (no git
// range), and the payload content is the ingested diff verbatim.
func TestPrepareReviewFromDiff_ScaffoldsAndForcesDiffMode(t *testing.T) {
	cfg := twoAgentConfig("http://unused") // PayloadMode: "blocks"
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.NoError(t, err)
	require.NotNil(t, prep)
	assert.Equal(t, out, prep.Dir)
	assert.Equal(t, 2, prep.AgentCount(), "both roster agents become slots")

	// Only the diff payload artifact is written (no blocks/files).
	assert.FileExists(t, filepath.Join(out, "payload", "diff.txt"))
	assert.NoFileExists(t, filepath.Join(out, "payload", "blocks.txt"))
	diffBytes, err := os.ReadFile(filepath.Join(out, "payload", "diff.txt"))
	require.NoError(t, err)
	assert.Equal(t, looseDiff, string(diffBytes), "payload is the ingested diff verbatim")

	// Manifest: diff mode forced for every agent, range fields empty.
	mdata, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.Equal(t, "diff", m.PayloadMode)
	assert.Equal(t, "diff", m.PerAgentPayload["greta"], "configured blocks mode is overridden to diff")
	assert.Equal(t, "diff", m.PerAgentPayload["kai"])
	assert.Empty(t, m.Base, "a range-less diff has no base")
	assert.Empty(t, m.Head, "a range-less diff has no head")
	assert.ElementsMatch(t, []string{"greta", "kai"}, m.Roster)
}

// An empty diff is rejected before any scaffold, mapped to ErrNoReviewableContent.
func TestPrepareReviewFromDiff_EmptyDiffRejected(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	_, err := PrepareReviewFromDiff(context.Background(), cfg, req, "   \n")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoReviewableContent)
	assert.NoDirExists(t, out, "no scaffold for an empty diff")
}

// A byte budget below every file's size drops all files and must surface
// ErrPayloadFullyDropped rather than forwarding an empty payload.
func TestPrepareReviewFromDiff_BudgetDropsAllErrors(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 1
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	_, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPayloadFullyDropped)
}

// An empty roster short-circuits before scaffolding, like PrepareReview.
func TestPrepareReviewFromDiff_EmptyRosterRejected(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project.Agents = nil
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	_, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.ErrorIs(t, err, ErrEmptyRoster)
}
